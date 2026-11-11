import { createHash } from "node:crypto";
import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { root } from "./util.mjs";

if (process.platform !== "win32") process.exit(0);

const manifest = JSON.parse(
  await readFile(
    path.join(root, "services/core/internal/cloudflare/connector-release.json"),
    "utf8",
  ),
);
const output = path.join(root, "dist/cloudflared");
await mkdir(output, { recursive: true });
const hash = (buffer) => createHash("sha256").update(buffer).digest("hex");
const binary = "cloudflared-windows-amd64.exe";

await Promise.all([
  prepare(
    binary,
    "cloudflared.exe",
    `https://github.com/cloudflare/cloudflared/releases/download/${manifest.version}/${binary}`,
  ),
  prepare(
    "LICENSE",
    "LICENSE",
    `https://raw.githubusercontent.com/cloudflare/cloudflared/${manifest.version}/LICENSE`,
  ),
]);
console.log(
  `cloudflared ${manifest.version} incluído no pacote Windows com SHA-256 verificado.`,
);

async function prepare(asset, filename, url) {
  const destination = path.join(output, filename);
  const current = await readFile(destination).catch(() => null);
  if (current && hash(current) === manifest.sha256[asset]) return;
  const response = await fetch(url, { signal: AbortSignal.timeout(180_000) });
  if (!response.ok)
    throw new Error(
      `Download oficial de ${asset} falhou (HTTP ${response.status}).`,
    );
  const contents = Buffer.from(await response.arrayBuffer());
  if (hash(contents) !== manifest.sha256[asset])
    throw new Error(`SHA-256 inválido para ${asset}.`);
  const temporary = `${destination}.${process.pid}.download`;
  try {
    await writeFile(temporary, contents, { flag: "wx" });
    await rename(temporary, destination);
  } finally {
    await rm(temporary, { force: true });
  }
}
