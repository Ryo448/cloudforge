import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import { root } from "./util.mjs";
const require = createRequire(import.meta.url);
const env = { ...process.env };
delete env.ELECTRON_RUN_AS_NODE;
const child = spawn(require("electron"), ["apps/desktop/electron/main.cjs"], {
  cwd: root,
  stdio: "inherit",
  env,
  windowsHide: true,
});
child.on("error", (error) => {
  console.error(error.message);
  process.exitCode = 1;
});
child.on("exit", (code) => process.exit(code ?? 1));
