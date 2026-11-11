// Disposable, real integration installation. Never used by production startup.
import { mkdir, writeFile, readFile, mkdtemp, open } from "node:fs/promises";
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import net from "node:net";
import path from "node:path";
import { root } from "./util.mjs";
const adminAddress = process.env.CLOUDFORGE_QA_ADMIN || "127.0.0.1:28931";
const publicAddress = process.env.CLOUDFORGE_QA_PUBLIC || "127.0.0.1:28932";
const adminURL = "http://" + adminAddress;
const publicURL = "http://" + publicAddress;
const sessionFile = process.env.CLOUDFORGE_QA_SESSION || "qa-session.json";
if (!/^[a-zA-Z0-9_.-]+$/.test(sessionFile))
  throw new Error("Nome de sessão QA inválido");
await mkdir(path.join(root, ".local"), { recursive: true });
const dir = await mkdtemp(path.join(root, ".local/qa-"));
const data = path.join(dir, "data");
const projectPath = path.join(dir, "project");
await mkdir(projectPath);
const port = await new Promise((resolve) => {
  const s = net.createServer();
  s.listen(0, "127.0.0.1", () => {
    const p = s.address().port;
    s.close(() => resolve(p));
  });
});
await writeFile(
  path.join(projectPath, "package.json"),
  JSON.stringify({
    name: "cloudforge-qa",
    version: "1.0.0",
    private: true,
    scripts: { start: "node server.cjs" },
  }),
);
await writeFile(
  path.join(projectPath, "server.cjs"),
  `const http=require('node:http'); console.log('Cloudforge: aplicação real de verificação iniciada.'); const server=http.createServer((req,res)=>{console.log(req.method+' '+req.url);res.end('Cloudforge integration verified');});server.listen(${port},'127.0.0.1',()=>console.log('Servidor HTTP ouvindo em 127.0.0.1:${port}'));setInterval(()=>console.log('Heartbeat · processo ativo'),15000);process.on('SIGTERM',()=>server.close(()=>process.exit(0)));`,
);
const log = await open(path.join(dir, "core.log"), "a");
const binary = path.join(
  root,
  "dist/core",
  process.platform === "win32" ? "cloudforge-core.exe" : "cloudforge-core",
);
const child = spawn(
  binary,
  [
    "--data",
    data,
    "--web-dir",
    path.join(root, "apps/web/dist"),
    "--admin",
    adminAddress,
    "--public",
    publicAddress,
    "--development",
  ],
  { detached: true, stdio: ["ignore", log.fd, log.fd], windowsHide: true },
);
child.unref();
await log.close();
let key;
for (let i = 0; i < 80; i++) {
  try {
    key = (await readFile(path.join(data, "admin.key"), "utf8")).trim();
    const h = await fetch(adminURL + "/api/health", {
      headers: { Authorization: "Bearer " + key },
    });
    if (h.ok) break;
  } catch {}
  await new Promise((r) => setTimeout(r, 250));
}
async function api(method, route, body) {
  const response = await fetch(adminURL + route, {
    method,
    headers: {
      Authorization: "Bearer " + key,
      "Content-Type": "application/json",
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const result = await response.json();
  if (!response.ok) throw new Error(result.error);
  return result;
}
const password =
  process.env.CLOUDFORGE_QA_PASSWORD || randomBytes(24).toString("base64url");
await api("POST", "/api/setup", {
  username: "qa-admin",
  password,
  confirmation: password,
});
const project = await api("POST", "/api/projects", {
  project: {
    name: "Verificação Node.js",
    runtime: "node",
    path: projectPath,
    workingDirectory: projectPath,
    startCommand: ["npm", "run", "start"],
    restartPolicy: "on-failure",
    port,
  },
  approved: true,
});
await api("POST", `/api/projects/${project.id}/action`, { action: "start" });
let online = false;
for (let i = 0; i < 120; i++) {
  try {
    const r = await fetch(`http://127.0.0.1:${port}`);
    if (r.ok) {
      online = true;
      break;
    }
  } catch {}
  await new Promise((r) => setTimeout(r, 250));
}
if (!online)
  throw new Error(
    "QA application did not become available. Inspect " +
      path.join(dir, "core.log"),
  );
await writeFile(
  path.join(root, ".local", sessionFile),
  JSON.stringify(
    {
      dataDir: data,
      adminURL,
      publicURL,
      dir,
      pid: child.pid,
      projectID: project.id,
      port,
      username: "qa-admin",
    },
    null,
    2,
  ),
  { mode: 0o600 },
);
console.log(
  JSON.stringify({
    dataDir: data,
    adminURL,
    publicURL,
    pid: child.pid,
    projectID: project.id,
    port,
    publicURL,
    status: "real application online",
  }),
);
