import { spawn, spawnSync } from "node:child_process";
import path from "node:path";
import { root, npmCLI } from "./util.mjs";
for (const args of [["run", "build", "--workspace", "@cloudforge/web"]]) {
  const result = spawnSync(process.execPath, [npmCLI, ...args], {
    cwd: root,
    stdio: "inherit",
  });
  if (result.status) process.exit(result.status);
}
const build = spawnSync(process.execPath, ["scripts/build-core.mjs"], {
  cwd: root,
  stdio: "inherit",
});
if (build.status) process.exit(build.status);
const vite = spawn(
  process.execPath,
  [
    path.join(root, "node_modules/vite/bin/vite.js"),
    "--host",
    "127.0.0.1",
    "--port",
    "5173",
    "--strictPort",
  ],
  { cwd: path.join(root, "apps/desktop"), stdio: "inherit" },
);
let electron;
for (let i = 0; i < 80; i++) {
  try {
    const r = await fetch("http://127.0.0.1:5173");
    if (r.ok) break;
  } catch {}
  await new Promise((r) => setTimeout(r, 250));
}
const electronEnv = { ...process.env, CLOUDFORGE_DEV: "1" };
delete electronEnv.ELECTRON_RUN_AS_NODE;
electron = spawn(
  path.join(
    root,
    "node_modules/electron/dist",
    process.platform === "win32" ? "electron.exe" : "electron",
  ),
  ["apps/desktop/electron/main.cjs"],
  { cwd: root, stdio: "inherit", env: electronEnv },
);
function cleanup() {
  vite.kill();
  electron?.kill();
}
process.on("SIGINT", cleanup);
process.on("SIGTERM", cleanup);
electron.on("exit", () => {
  vite.kill();
  process.exit();
});
