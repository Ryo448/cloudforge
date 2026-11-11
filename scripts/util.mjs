import { existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
export const root = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
export const go = existsSync(
  path.join(
    root,
    ".tools/go/bin",
    process.platform === "win32" ? "go.exe" : "go",
  ),
)
  ? path.join(
      root,
      ".tools/go/bin",
      process.platform === "win32" ? "go.exe" : "go",
    )
  : "go";
export const npmCLI =
  process.env.npm_execpath ||
  path.join(path.dirname(process.execPath), "node_modules/npm/bin/npm-cli.js");
