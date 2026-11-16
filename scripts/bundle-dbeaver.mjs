import { execFileSync, spawn } from "node:child_process";
import { createWriteStream } from "node:fs";
import { mkdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { pipeline } from "node:stream/promises";
import { createGzip } from "node:zlib";
import { root } from "./util.mjs";

const release = JSON.parse(
  await readFile(
    path.join(root, "apps/desktop/electron/dbeaver-release.json"),
    "utf8",
  ),
);
const output = path.join(root, "dist/dbeaver");
const target = path.join(output, "runtime.tar.gz");
const state = path.join(output, "image-id");
const matches = (id) =>
  [release.id, release.manifestId, release.configId].includes(id);
const inspect = () => {
  try {
    return execFileSync(
      "docker",
      ["image", "inspect", "--format", "{{.Id}}", release.image],
      { stdio: ["ignore", "pipe", "ignore"] },
    )
      .toString()
      .trim();
  } catch {
    return null;
  }
};

await mkdir(output, { recursive: true });
if (
  (await readFile(state, "utf8").catch(() => null)) === release.id &&
  (await stat(target).catch(() => null))?.size > 0
)
  process.exit(0);
if (!matches(inspect())) {
  const immutable = `dbeaver/cloudbeaver@${release.id}`;
  execFileSync("docker", ["pull", "--platform", "linux/amd64", immutable], {
    stdio: "inherit",
  });
  execFileSync("docker", ["tag", immutable, release.image], {
    stdio: "inherit",
  });
}
if (!matches(inspect()))
  throw new Error("A imagem do DBeaver não corresponde à revisão fixada.");

const temporary = `${target}.${process.pid}.partial`;
const child = spawn(
  "docker",
  ["image", "save", "--platform", "linux/amd64", release.image],
  {
    stdio: ["ignore", "pipe", "inherit"],
  },
);
const completed = new Promise((resolve, reject) => {
  child.once("error", reject);
  child.once("close", (code) =>
    code === 0
      ? resolve()
      : reject(new Error(`Falha ao exportar o DBeaver (${code}).`)),
  );
});
try {
  await Promise.all([
    completed,
    pipeline(
      child.stdout,
      createGzip({ level: 3 }),
      createWriteStream(temporary, { flags: "wx" }),
    ),
  ]);
  await rename(temporary, target);
  await writeFile(state, release.id);
  console.log(`DBeaver ${release.version} incluído no pacote offline.`);
} finally {
  if (child.exitCode === null) child.kill();
  await rm(temporary, { force: true });
}
