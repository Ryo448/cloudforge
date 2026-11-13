import { readFile, writeFile, mkdir } from "node:fs/promises";
import { spawnSync } from "node:child_process";
import path from "node:path";
import { root, go } from "./util.mjs";
const heading =
  "Cloudforge — avisos de terceiros\nGerado a partir das licenças distribuídas nas dependências. O Cloudforge é licenciado sob MIT (consulte LICENSE na raiz do projeto).\n";
let frontend = heading;
for (const name of [
  "react",
  "react-dom",
  "scheduler",
  "ws",
  "lucide-react",
  "@fontsource/poppins",
  "@fontsource/ibm-plex-sans",
  "@fontsource/ibm-plex-mono",
]) {
  const directory = path.join(root, "node_modules", name);
  const pkg = JSON.parse(
    await readFile(path.join(directory, "package.json"), "utf8"),
  );
  frontend += `\n${"=".repeat(72)}\n${name} ${pkg.version}\n${"=".repeat(72)}\n`;
  frontend += await readFile(path.join(directory, "LICENSE"), "utf8");
  frontend += "\n";
}
let all = frontend;
const result = spawnSync(
  go,
  ["list", "-m", "-f", "{{.Path}}\t{{.Version}}\t{{.Dir}}", "all"],
  { cwd: path.join(root, "services/core"), encoding: "utf8" },
);
if (result.status !== 0)
  throw new Error(result.stderr || "Não foi possível listar módulos Go.");
for (const line of result.stdout.trim().split("\n")) {
  const [name, version, directory] = line.trim().split("\t");
  if (!directory || !version) continue;
  let license = "";
  for (const candidate of [
    "LICENSE",
    "LICENSE.md",
    "LICENSE.txt",
    "COPYING",
    "COPYRIGHT",
  ]) {
    try {
      license +=
        `\n${candidate}\n` +
        (await readFile(path.join(directory, candidate), "utf8")) +
        "\n";
    } catch {}
  }
  if (license)
    all += `\n${"=".repeat(72)}\n${name} ${version}\n${"=".repeat(72)}\n${license}`;
}
for (const [directory, text] of [
  ["apps/desktop/public", all],
  ["apps/web/public", frontend],
  ["docs", all],
]) {
  await mkdir(path.join(root, directory), { recursive: true });
  await writeFile(
    path.join(
      root,
      directory,
      directory === "docs"
        ? "THIRD_PARTY_NOTICES.txt"
        : "third-party-notices.txt",
    ),
    text,
  );
}
console.log("Avisos de terceiros gerados para desktop, web e documentação.");
