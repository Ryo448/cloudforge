import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import { root } from "./util.mjs";
const renderer = path.join(root, "apps/desktop/dist/index.html");
if (!fs.existsSync(renderer)) {
  console.error(
    "A interface do Cloudforge ainda não foi compilada. Execute `npm run build` antes de `npm run start`.",
  );
  process.exit(1);
}
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
