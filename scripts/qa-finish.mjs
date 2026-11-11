import { readFile } from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";
import { root } from "./util.mjs";
const qa = JSON.parse(
  await readFile(
    path.join(
      root,
      ".local",
      process.env.CLOUDFORGE_QA_SESSION || "qa-session.json",
    ),
    "utf8",
  ),
);
const key = (await readFile(path.join(qa.dataDir, "admin.key"), "utf8")).trim();
async function api(method, route, body) {
  const response = await fetch(
    (qa.adminURL || "http://127.0.0.1:28931") + route,
    {
      method,
      headers: {
        Authorization: "Bearer " + key,
        "Content-Type": "application/json",
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    },
  );
  const result = await response.json();
  assert.ok(response.ok, result.error);
  return result;
}
assert.equal((await api("GET", "/api/health")).service, "cloudforge");
const initial = await api("GET", "/api/state");
assert.equal(
  initial.projects.length,
  1,
  "A instalação já não contém apenas o projeto de QA.",
);
assert.equal(
  initial.projects[0].id,
  qa.projectID,
  "Projeto ativo não é o fixture esperado.",
);
assert.equal(
  initial.cloudflare.tunnelId,
  "",
  "A instalação contém um túnel configurado; não encerrar como QA.",
);
const page = await fetch(`http://127.0.0.1:${qa.port}`);
assert.equal(await page.text(), "Cloudforge integration verified");
console.log("PASS: Core e aplicação HTTP de QA estão ativos.");
await api("POST", `/api/projects/${qa.projectID}/action`, { action: "stop" });
let stopped = false;
for (let i = 0; i < 100; i++) {
  const state = await api("GET", "/api/state");
  const project = state.projects.find((p) => p.id === qa.projectID);
  if (project?.execution.actualState === "offline") {
    stopped = true;
    break;
  }
  await new Promise((r) => setTimeout(r, 200));
}
assert.ok(stopped, "projeto temporário não parou");
let reachable = false;
try {
  await fetch(`http://127.0.0.1:${qa.port}`, {
    signal: AbortSignal.timeout(1000),
  });
  reachable = true;
} catch {}
assert.equal(reachable, false, "porta do projeto permaneceu ativa");
console.log("PASS: árvore de processos de QA encerrada e porta liberada.");
