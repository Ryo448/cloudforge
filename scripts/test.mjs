import { spawnSync } from "node:child_process";
import path from "node:path";
import { root, go, npmCLI } from "./util.mjs";
for (const [cmd, args, cwd] of [
  [
    go,
    ["test", "-timeout", "180s", "-count=1", "./..."],
    path.join(root, "services/core"),
  ],
  [process.execPath, [npmCLI, "run", "typecheck"], root],
]) {
  const result = spawnSync(cmd, args, { cwd, stdio: "inherit" });
  if (result.status) process.exit(result.status);
}
