const { execFile, spawn } = require("node:child_process");
const { createHash, randomBytes, randomUUID } = require("node:crypto");
const { access } = require("node:fs/promises");
const path = require("node:path");
const { setTimeout: delay } = require("node:timers/promises");
const { promisify } = require("node:util");
const distribution = require("./dbeaver-release.json");

const execute = promisify(execFile);
const workspace = "/opt/cloudbeaver/workspace";
const caPath = `${workspace}/cloudforge-ca.crt`;
const trustPath = `${workspace}/cloudforge-trust.p12`;
const admin = "cloudforge";
const bootstrap = `
set -eu
umask 077
cd /opt/cloudbeaver
mkdir -p workspace/GlobalConfiguration/.dbeaver
IFS= read -r CA
printf '%s' "$CA" | base64 -d > '${caPath}'
IFS= read -r SOURCE
printf '%s' "$SOURCE" | base64 -d > workspace/GlobalConfiguration/.dbeaver/data-sources.json
IFS= read -r CB_ADMIN_PASSWORD
export CB_ADMIN_PASSWORD
if [ "$CF_DRIVER" = mysql8 ]; then
  keytool -importcert -noprompt -alias cloudforge -file '${caPath}' -storetype PKCS12 -keystore '${trustPath}' -storepass changeit >/dev/null 2>&1
fi
exec ./run-cloudbeaver-server.sh
`;

async function localDocker() {
  let host =
    process.platform === "win32"
      ? "npipe:////./pipe/docker_engine"
      : "unix:///var/run/docker.sock";
  const runtimeDir = process.env.XDG_RUNTIME_DIR;
  if (process.platform === "linux" && path.isAbsolute(runtimeDir || "")) {
    const socket = path.join(runtimeDir, "docker.sock");
    if (
      await access(socket).then(
        () => true,
        () => false,
      )
    )
      host = `unix://${socket}`;
  }
  const env = Object.fromEntries(
    Object.entries(process.env).filter(
      ([name]) =>
        !/^(DOCKER_CONTEXT|DOCKER_HOST|DOCKER_TLS|DOCKER_TLS_VERIFY|DOCKER_CERT_PATH)$/i.test(
          name,
        ),
    ),
  );
  let binary = "docker";
  if (process.platform === "win32") {
    const bundled = path.join(
      process.env.ProgramFiles || "C:/Program Files",
      "Docker/Docker/resources/bin/docker.exe",
    );
    if (
      await access(bundled).then(
        () => true,
        () => false,
      )
    )
      binary = bundled;
  }
  const docker = (args, options = {}) =>
    execute(binary, ["--host", host, ...args], {
      env,
      windowsHide: true,
      timeout: 120_000,
      ...options,
    });
  const attach = (name) =>
    spawn(binary, ["--host", host, "start", "-ai", name], {
      env,
      windowsHide: true,
      stdio: ["pipe", "ignore", "ignore"],
    });
  return { docker, attach };
}

let importing;
async function ensureImage(docker, bundle) {
  const exact = async () => {
    const { stdout } = await docker([
      "image",
      "inspect",
      "--format",
      "{{.Id}}",
      distribution.image,
    ]);
    return stdout.trim();
  };
  const matches = (id) =>
    [distribution.id, distribution.manifestId, distribution.configId].includes(
      id,
    );
  const present = await exact().catch(() => null);
  if (matches(present)) return present;
  if (!importing) {
    importing = (async () => {
      await access(bundle).catch(() => {
        throw new Error(
          "O pacote local do DBeaver está ausente. Use o instalador completo do Cloudforge.",
        );
      });
      await docker(["image", "load", "--input", bundle]).catch(() => {
        throw new Error(
          "Não foi possível carregar o pacote local do DBeaver no Docker.",
        );
      });
      const loaded = await exact();
      if (!matches(loaded))
        throw new Error(
          "A imagem local do DBeaver não corresponde ao pacote do Cloudforge.",
        );
      return loaded;
    })().finally(() => {
      importing = undefined;
    });
  }
  return importing;
}

function dataSource(connection) {
  const postgres = connection.engine === "postgresql";
  const provider = postgres ? "postgresql" : "mysql";
  const driver = postgres ? "postgres-jdbc" : "mysql8";
  const properties = postgres
    ? { sslmode: "verify-full", sslrootcert: caPath }
    : {
        sslMode: "VERIFY_IDENTITY",
        trustCertificateKeyStoreUrl: `file:${trustPath}`,
        trustCertificateKeyStoreType: "PKCS12",
        trustCertificateKeyStorePassword: "changeit",
        fallbackToSystemTrustStore: "false",
      };
  return {
    provider,
    driver,
    name: connection.name,
    "save-password": true,
    configuration: {
      host: connection.host,
      port: String(connection.port),
      database: connection.database,
      url: `jdbc:${provider}://${connection.host}:${connection.port}/${connection.database}`,
      configurationType: "MANUAL",
      properties,
      "auth-model": "native",
      "auth-properties": {
        user: connection.user,
        password: connection.password,
      },
    },
  };
}

async function startRuntime(connection, bundle, signal) {
  const { docker, attach } = await localDocker();
  const image = await ensureImage(docker, bundle);
  signal.throwIfAborted();
  const name = `cloudforge-dbeaver-${randomUUID()}`;
  const password = `Cf9${randomBytes(24).toString("hex")}`;
  const source = dataSource(connection);
  const connectionId = `${source.provider}-${randomUUID()}`;
  let child;
  let stopped;
  const stop = () => {
    stopped ??= docker(["rm", "--force", name], { timeout: 10_000 })
      .catch(() => {})
      .finally(() => child?.kill());
    return stopped;
  };
  try {
    await docker([
      "create",
      "--rm",
      "-i",
      "--pull",
      "never",
      "--name",
      name,
      "--network",
      connection.host,
      "--publish",
      "127.0.0.1::8978",
      "--user",
      "8978:8978",
      "--security-opt",
      "no-new-privileges",
      "--cap-drop",
      "ALL",
      "--log-driver",
      "none",
      "--tmpfs",
      `${workspace}:rw,nosuid,nodev,uid=8978,gid=8978,mode=0700`,
      "--env",
      `CB_ADMIN_NAME=${admin}`,
      "--env",
      "CB_SERVER_NAME=Cloudforge",
      "--env",
      "CLOUDBEAVER_APP_ANONYMOUS_ACCESS_ENABLED=false",
      "--env",
      "CLOUDBEAVER_CORE_THEMING_THEME=dark",
      "--env",
      "CLOUDBEAVER_CORE_LOCALIZATION=pt_BR",
      "--env",
      `CF_DRIVER=${source.driver}`,
      "--entrypoint",
      "/bin/sh",
      image,
      "-c",
      bootstrap,
    ]);
    signal.throwIfAborted();
    child = attach(name);
    let ended = false;
    child.once("error", () => {
      ended = true;
    });
    child.once("exit", () => {
      ended = true;
    });
    child.stdin.on("error", () => {
      ended = true;
    });
    const config = JSON.stringify({ connections: { [connectionId]: source } });
    const wire = [connection.ca, config].map((value) =>
      Buffer.from(value).toString("base64"),
    );
    child.stdin.end(`${wire.join("\n")}\n${password}\n`);
    const deadline = Date.now() + 65_000;
    let origin;
    while (Date.now() < deadline && !ended) {
      signal.throwIfAborted();
      if (!origin) {
        const binding = await docker(["port", name, "8978/tcp"], {
          timeout: 3800,
        }).catch(() => null);
        const port = /^127\.0\.0\.1:(\d+)\s*$/.exec(binding?.stdout ?? "")?.[1];
        if (port) origin = `http://127.0.0.1:${port}`;
      }
      if (origin) {
        const response = await fetch(`${origin}/`, {
          redirect: "error",
          signal: AbortSignal.any([signal, AbortSignal.timeout(2000)]),
        }).catch(() => null);
        if (response?.ok)
          return { origin, admin, password, connectionId, stop };
      }
      await delay(400, undefined, { signal });
    }
    throw new Error(
      "O motor do DBeaver não iniciou. Verifique se o Docker e o banco estão online e tente novamente.",
    );
  } catch (error) {
    await stop();
    if (signal.aborted) throw signal.reason;
    if (error.cmd)
      throw new Error(
        "Não foi possível iniciar o DBeaver na rede privada do banco.",
      );
    throw error;
  }
}

async function connectRuntime(runtime, signal) {
  const cookies = new Map();
  const query = async (document, variables, failure) => {
    const response = await fetch(`${runtime.origin}/api/gql`, {
      method: "POST",
      redirect: "error",
      headers: {
        "Content-Type": "application/json",
        Origin: runtime.origin,
        Cookie: [...cookies]
          .map(([name, value]) => `${name}=${value}`)
          .join("; "),
      },
      body: JSON.stringify({ query: document, variables }),
      signal: AbortSignal.any([signal, AbortSignal.timeout(30_000)]),
    }).catch(() => null);
    const body = await response?.json().catch(() => null);
    if (!response?.ok || !body?.data || body.errors?.length)
      throw new Error(failure);
    for (const header of response.headers.getSetCookie()) {
      const pair = /^([^=]+)=([^;]*)/.exec(header);
      if (pair) cookies.set(pair[1], pair[2]);
    }
    return body.data;
  };
  const sessionFailure =
    "Não foi possível autenticar a sessão local do DBeaver.";
  await query(
    'mutation { openSession(defaultLocale: "pt_BR") { valid } }',
    {},
    sessionFailure,
  );
  const login = await query(
    'query ($credentials: Object) { authLogin(provider: "local", credentials: $credentials) { authStatus } }',
    {
      credentials: {
        user: runtime.admin,
        // The CloudBeaver local auth provider expects its legacy uppercase digest.
        password: createHash("md5")
          .update(runtime.password)
          .digest("hex")
          .toUpperCase(),
      },
    },
    sessionFailure,
  );
  if (
    login.authLogin?.authStatus !== "SUCCESS" ||
    !cookies.get("cb-session-id")
  )
    throw new Error(sessionFailure);
  const connectionFailure =
    "O DBeaver não conseguiu conectar ao banco usando TLS. Confirme que a instância está online e tente novamente.";
  const { userConnections } = await query(
    "query ($id: ID) { userConnections(id: $id) { id projectId } }",
    { id: runtime.connectionId },
    connectionFailure,
  );
  const connection = userConnections.find(
    ({ id }) => id === runtime.connectionId,
  );
  if (!connection) throw new Error(connectionFailure);
  const { initConnection } = await query(
    "mutation ($id: ID!, $projectId: ID) { initConnection(id: $id, projectId: $projectId) { connected } }",
    { id: connection.id, projectId: connection.projectId },
    connectionFailure,
  );
  if (!initConnection?.connected) throw new Error(connectionFailure);
  return [...cookies].filter(([, value]) => value);
}

module.exports = { startRuntime, connectRuntime };
