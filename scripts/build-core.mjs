import { mkdirSync } from "node:fs";
import { spawnSync } from "node:child_process";
import path from "node:path";
import { root, go } from "./util.mjs";
mkdirSync(path.join(root, "dist/core"), { recursive: true });
const windows =
  (process.env.GOOS || process.platform) === "windows" ||
  (!process.env.GOOS && process.platform === "win32");
const target = path.join(
  root,
  "dist/core",
  windows ? "cloudforge-core.exe" : "cloudforge-core",
);
const flags = windows ? "-s -w -H=windowsgui" : "-s -w";
const result = spawnSync(
  go,
  ["build", "-trimpath", "-ldflags", flags, "-o", target, "./cmd/cloudforge"],
  { cwd: path.join(root, "services/core"), stdio: "inherit" },
);
process.exit(result.status ?? 1);
