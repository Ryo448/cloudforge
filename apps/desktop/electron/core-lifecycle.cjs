const fs = require("node:fs/promises");
const { createHash } = require("node:crypto");
const { spawn, execFile } = require("node:child_process");
const { promisify } = require("node:util");
const exec = promisify(execFile);
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function matchesCore(health, version, hash, apiVersion) {
  return (
    health?.service === "cloudforge" &&
    health.apiVersion === apiVersion &&
    health.version === version &&
    health.binarySHA256 === hash
  );
}

// Used only to migrate releases predating the authenticated shutdown endpoint.
async function stopLegacyCore(binary, listenPort = 28931) {
  if (!Number.isInteger(listenPort) || listenPort < 1 || listenPort > 65535)
    throw new Error("Porta administrativa inválida.");
  if (process.platform === "win32") {
    const script = `$ErrorActionPreference='Stop'; $owners=@(Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort $env:CLOUDFORGE_CORE_PORT -State Listen | Select-Object -ExpandProperty OwningProcess -Unique); if ($owners.Count -ne 1) { throw 'Core listener not found' }; $coreProcess=Get-Process -Id $owners[0]; if (-not [string]::Equals($coreProcess.Path, $env:CLOUDFORGE_EXPECTED_CORE, [StringComparison]::OrdinalIgnoreCase)) { throw 'Unexpected core executable' }; & (Join-Path $env:SystemRoot 'System32/taskkill.exe') /PID $coreProcess.Id /T /F; if ($LASTEXITCODE -ne 0) { throw 'Core shutdown failed' }`;
    await exec(
      "powershell.exe",
      ["-NoProfile", "-NonInteractive", "-Command", script],
      {
        windowsHide: true,
        timeout: 20000,
        env: {
          ...process.env,
          CLOUDFORGE_EXPECTED_CORE: binary,
          CLOUDFORGE_CORE_PORT: String(listenPort),
        },
      },
    );
    return;
  }
  // Linux: locate the exact listener through procfs, then verify its executable.
  const port = listenPort.toString(16).toUpperCase().padStart(4, "0");
  const tcp = await fs.readFile("/proc/net/tcp", "utf8");
  const inodes = new Set(
    tcp
      .split("\n")
      .map((line) => line.trim().split(/\s+/))
      .filter((cols) => cols[1] === `0100007F:${port}` && cols[3] === "0A")
      .map((cols) => cols[9]),
  );
  for (const pid of await fs.readdir("/proc")) {
    if (!/^\d+$/.test(pid)) continue;
    try {
      const executable = (await fs.readlink(`/proc/${pid}/exe`)).replace(
        / \(deleted\)$/,
        "",
      );
      if (executable !== binary) continue;
      for (const fd of await fs.readdir(`/proc/${pid}/fd`)) {
        const socket = await fs
          .readlink(`/proc/${pid}/fd/${fd}`)
          .catch(() => "");
        if (inodes.has(socket.match(/^socket:\[(\d+)\]$/)?.[1])) {
          process.kill(Number(pid), "SIGTERM");
          return Number(pid);
        }
      }
    } catch (error) {
      if (error.code !== "ENOENT" && error.code !== "EACCES") throw error;
    }
  }
  throw new Error("Não foi possível identificar com segurança o Core antigo.");
}

function createCoreManager({
  binary,
  web,
  dataDir,
  version,
  apiVersion,
  request,
  resetKey,
  launch = spawn,
  stopLegacy = stopLegacyCore,
  pause = sleep,
  hashBinary = async () =>
    createHash("sha256")
      .update(await fs.readFile(binary))
      .digest("hex"),
}) {
  let pending;
  const health = () =>
    request("GET", "/api/health", undefined, 2500).catch(() => undefined);
  async function stop() {
    const running = await health();
    if (running?.service !== "cloudforge") return;
    let pid = running.pid;
    if (running.lifecycle)
      await request("POST", "/api/shutdown", undefined, 5000);
    else pid = await stopLegacy(binary);
    // Wait for cleanup and lock release, not just the HTTP listener to close.
    for (let i = 0; i < 240; i++) {
      await pause(250);
      const available = await health();
      let alive = false;
      if (Number.isInteger(pid) && pid > 0) {
        try {
          process.kill(pid, 0);
          alive = true;
        } catch (error) {
          alive = error.code !== "ESRCH";
        }
      }
      if (!available && !alive) return;
    }
    throw new Error(
      "O Core está demorando para encerrar. Aguarde e tente novamente.",
    );
  }
  async function ensure() {
    const hash = await hashBinary();
    const running = await health();
    if (matchesCore(running, version, hash, apiVersion)) return;
    if (running?.service === "cloudforge") await stop();
    resetKey();
    let spawnError;
    const child = launch(binary, ["--data", dataDir, "--web-dir", web], {
      detached: true,
      stdio: "ignore",
      windowsHide: true,
    });
    child.on("error", (error) => {
      spawnError = error;
    });
    child.unref();
    for (let i = 0; i < 160; i++) {
      await pause(250);
      if (spawnError) throw spawnError;
      resetKey();
      if (matchesCore(await health(), version, hash, apiVersion)) return;
    }
    throw new Error(
      "O Core da versão atual não respondeu. Confira as portas 28931 e 28932 e os logs do serviço.",
    );
  }
  return {
    ensure() {
      if (!pending)
        pending = ensure().finally(() => {
          pending = undefined;
        });
      return pending;
    },
    stop,
  };
}
module.exports = { createCoreManager, matchesCore, stopLegacyCore };
