const {
  app,
  BrowserWindow,
  ipcMain,
  dialog,
  shell,
  clipboard,
} = require("electron");
const path = require("node:path");
const fs = require("node:fs/promises");
const { spawn } = require("node:child_process");
const { StringDecoder } = require("node:string_decoder");
const WebSocket = require("ws");
const { pathToFileURL } = require("node:url");
app.setName("Cloudforge");
const root = path.resolve(__dirname, "../../..");
const dataDir =
  process.env.CLOUDFORGE_DATA ||
  path.join(app.getPath("appData"), "Cloudforge");
const profileDir = path.join(dataDir, "electron");
require("node:fs").mkdirSync(profileDir, { recursive: true });
app.setPath("userData", profileDir);
const address = "http://127.0.0.1:28931";
const coreAPIVersion = 3;
const terminalSessions = new Map();
let window,
  key,
  socket,
  retry,
  watchedProject = "",
  closing = false;
if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on("second-instance", () => {
    window?.show();
    window?.focus();
  });
  app.whenReady().then(async () => {
    try {
      await ensureCore();
    } catch (error) {
      console.error("Core:", error.message);
    }
    createWindow();
    connectEvents();
  });
}
async function request(method, route, body) {
  if (!key)
    key = (await fs.readFile(path.join(dataDir, "admin.key"), "utf8")).trim();
  const response = await fetch(address + route, {
    method,
    headers: {
      Authorization: `Bearer ${key}`,
      "Content-Type": "application/json",
    },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(route.includes("/install") ? 250000 : 120000),
  });
  const responseText = await response.text();
  let result;
  try {
    result = JSON.parse(responseText);
  } catch {
    const detail = responseText.trim().slice(0, 500);
    const status = `HTTP ${response.status}`;
    if (
      response.status === 404 &&
      method === "DELETE" &&
      /^\/api\/projects\/[A-Za-z0-9_-]+$/.test(route)
    )
      throw new Error(
        "O Core em execução não reconhece a exclusão de projetos. Reinicie o Windows e abra o Cloudforge novamente para carregar o Core atualizado.",
      );
    if (response.ok) throw new Error(`Resposta inválida do Core (${status})`);
    throw new Error(
      detail
        ? `O Core respondeu ${status}: ${detail}`
        : `O Core respondeu sem JSON (${status})`,
    );
  }
  if (!response.ok)
    throw new Error(result.error || `Erro HTTP ${response.status}`);
  return result;
}
function openTuiWindow(project) {
  const terminal = new BrowserWindow({
    width: 1000,
    height: 700,
    minWidth: 600,
    minHeight: 400,
    backgroundColor: "#0d1510",
    title: `Terminal · ${project.name}`,
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, "terminal-preload.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      webSecurity: true,
    },
  });
  terminal.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  terminal.webContents.on("will-navigate", (event) => event.preventDefault());
  const session = {
    terminal,
    projectId: project.id,
    ready: false,
    socket: null,
    retry: null,
    cols: 100,
    rows: 30,
  };
  const webContentsId = terminal.webContents.id;
  terminalSessions.set(webContentsId, session);
  terminal.on("closed", () => {
    terminalSessions.delete(webContentsId);
    clearTimeout(session.retry);
    session.socket?.close();
  });
  if (process.env.CLOUDFORGE_DEV === "1")
    terminal.loadURL("http://127.0.0.1:5173/#tui");
  else
    terminal.loadFile(path.join(__dirname, "../dist/index.html"), {
      hash: "tui",
    });
}
function connectTerminal(session) {
  if (!session.ready || session.terminal.isDestroyed() || closing) return;
  clearTimeout(session.retry);
  if (session.socket) {
    session.socket.removeAllListeners();
    session.socket.on("error", () => {});
    session.socket.close();
  }
  const ws = new WebSocket(
    address.replace("http:", "ws:") +
      `/api/projects/${session.projectId}/terminal`,
    {
      headers: { Authorization: `Bearer ${key}` },
      maxPayload: 2 * 1024 * 1024,
    },
  );
  const decoder = new StringDecoder("utf8");
  session.socket = ws;
  ws.on("open", () => {
    if (session.terminal.isDestroyed()) return;
    session.terminal.webContents.send("terminal:reset");
    ws.send(
      JSON.stringify({
        type: "resize",
        cols: session.cols,
        rows: session.rows,
      }),
    );
  });
  ws.on("message", (data) => {
    if (!session.terminal.isDestroyed())
      session.terminal.webContents.send("terminal:data", decoder.write(data));
  });
  ws.on("error", () => {});
  ws.on("close", () => {
    decoder.end();
    if (session.socket === ws) session.socket = null;
    if (!session.terminal.isDestroyed() && !closing)
      session.retry = setTimeout(() => connectTerminal(session), 1500);
  });
}
function terminalSession(event) {
  const session = terminalSessions.get(event.sender.id);
  if (!session || event.senderFrame !== session.terminal.webContents.mainFrame)
    throw new Error("Origem do terminal inválida");
  return session;
}
async function ensureCore() {
  let health;
  try {
    health = await request("GET", "/api/health");
  } catch {}
  if (health?.service === "cloudforge") {
    if (health.apiVersion === coreAPIVersion) return;
    throw new Error(
      "O Core em execução está desatualizado. Reinicie o Windows e abra o Cloudforge novamente para carregar o Core atualizado.",
    );
  }
  const binary = app.isPackaged
    ? path.join(
        process.resourcesPath,
        process.platform === "win32"
          ? "cloudforge-core.exe"
          : "cloudforge-core",
      )
    : path.join(
        root,
        "dist/core",
        process.platform === "win32"
          ? "cloudforge-core.exe"
          : "cloudforge-core",
      );
  const web = app.isPackaged
    ? path.join(process.resourcesPath, "web")
    : path.join(root, "apps/web/dist");
  await fs.access(binary);
  const child = spawn(binary, ["--data", dataDir, "--web-dir", web], {
    detached: true,
    stdio: "ignore",
    windowsHide: true,
  });
  child.on("error", (error) =>
    console.error("Não foi possível iniciar o Core:", error.message),
  );
  child.unref();
  for (let attempt = 0; attempt < 80; attempt++) {
    await new Promise((resolve) => setTimeout(resolve, 250));
    try {
      key = undefined;
      const health = await request("GET", "/api/health");
      if (
        health.service === "cloudforge" &&
        health.apiVersion === coreAPIVersion
      )
        return;
    } catch {}
  }
  throw new Error(
    "O Core não respondeu. Confira as portas 28931 e 28932 e os logs do serviço.",
  );
}
function createWindow() {
  window = new BrowserWindow({
    width: 1320,
    height: 880,
    minWidth: 900,
    minHeight: 650,
    backgroundColor: "#111715",
    title: "Cloudforge",
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, "preload.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      webSecurity: true,
    },
  });
  const dev = process.env.CLOUDFORGE_DEV === "1";
  if (dev) window.loadURL("http://127.0.0.1:5173");
  else window.loadFile(path.join(__dirname, "../dist/index.html"));
  window.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  window.webContents.on("will-navigate", (event) => event.preventDefault());
  window.webContents.session.setPermissionRequestHandler(
    (_wc, _permission, callback) => callback(false),
  );
  window.on("closed", () => {
    window = undefined;
  });
}
function checkSender(event) {
  if (
    !window ||
    event.sender !== window.webContents ||
    event.senderFrame !== window.webContents.mainFrame
  )
    throw new Error("Origem IPC inválida");
  const expected =
    process.env.CLOUDFORGE_DEV === "1"
      ? "http://127.0.0.1:5173/"
      : pathToFileURL(path.join(__dirname, "../dist/index.html")).href;
  if (event.senderFrame.url !== expected)
    throw new Error("Documento IPC não autorizado");
}
const routes = [
  [
    "GET",
    /^\/api\/(health|state|projects|sessions|cloudflare\/(domains|zones|status))$/,
  ],
  [
    "POST",
    /^\/api\/(setup|projects|projects\/inspect|password|autostart|cloudflare\/(login|cancel-login|provision|verify|recover|panel-domain))$/,
  ],
  ["GET", /^\/api\/projects\/[A-Za-z0-9_-]+\/(logs|files|file)(\?[^#]*)?$/],
  ["POST", /^\/api\/projects\/[A-Za-z0-9_-]+\/(action|route)$/],
  ["PATCH", /^\/api\/projects\/[A-Za-z0-9_-]+\/(port|terminal-enabled)$/],
  ["DELETE", /^\/api\/projects\/[A-Za-z0-9_-]+(?:\/route)?$/],
  ["DELETE", /^\/api\/sessions\/[A-Za-z0-9_-]+$/],
];
ipcMain.handle("core:request", async (event, method, route, body) => {
  checkSender(event);
  if (
    typeof method !== "string" ||
    typeof route !== "string" ||
    route.length > 8192 ||
    !routes.some(([m, re]) => m === method && re.test(route))
  )
    throw new Error("Operação não permitida");
  if (body !== undefined && JSON.stringify(body).length > 32768)
    throw new Error("Dados excedem o limite");
  return request(method, route, body);
});
ipcMain.handle("core:reconnect", async (event) => {
  checkSender(event);
  await ensureCore();
  connectEvents();
  return true;
});
ipcMain.handle("project:choose", async (event) => {
  checkSender(event);
  const result = await dialog.showOpenDialog(window, {
    title: "Selecionar pasta do projeto",
    properties: ["openDirectory"],
  });
  return result.canceled ? null : result.filePaths[0];
});
ipcMain.handle("project:open", async (event, id) => {
  checkSender(event);
  if (typeof id !== "string") throw new Error("Projeto inválido");
  const projects = await request("GET", "/api/projects");
  const project = projects.find((p) => p.id === id);
  if (!project) throw new Error("Projeto não encontrado");
  const error = await shell.openPath(project.path);
  if (error) throw new Error(error);
});
ipcMain.handle("project:terminal", async (event, id) => {
  checkSender(event);
  if (typeof id !== "string" || !/^[A-Za-z0-9_-]{1,64}$/.test(id))
    throw new Error("Projeto inválido");
  const projects = await request("GET", "/api/projects");
  const project = projects.find((item) => item.id === id);
  if (!project) throw new Error("Projeto não encontrado");
  if (!project.terminalEnabled)
    throw new Error("Ative o terminal deste projeto nas configurações");
  if (
    project.execution.actualState === "online" &&
    !project.execution.terminalReady
  )
    throw new Error(
      "Reinicie o projeto uma vez para ativar o terminal nesta execução.",
    );
  if (
    !["online", "starting", "restarting"].includes(
      project.execution.actualState,
    )
  )
    await request("POST", `/api/projects/${id}/action`, { action: "start" });
  openTuiWindow(project);
});
ipcMain.on("terminal:ready", (event) => {
  const session = terminalSession(event);
  session.ready = true;
  connectTerminal(session);
});
ipcMain.on("terminal:write", (event, data) => {
  const session = terminalSession(event);
  if (
    typeof data === "string" &&
    data.length <= 65536 &&
    session.socket?.readyState === WebSocket.OPEN
  )
    session.socket.send(JSON.stringify({ type: "input", data }));
});
ipcMain.on("terminal:resize", (event, cols, rows) => {
  const session = terminalSession(event);
  if (
    Number.isInteger(cols) &&
    Number.isInteger(rows) &&
    cols >= 20 &&
    cols <= 500 &&
    rows >= 5 &&
    rows <= 200
  ) {
    session.cols = cols;
    session.rows = rows;
    if (session.socket?.readyState === WebSocket.OPEN)
      session.socket.send(JSON.stringify({ type: "resize", cols, rows }));
  }
});
ipcMain.handle("external:open", async (event, url) => {
  checkSender(event);
  if (
    typeof url !== "string" ||
    url.length > 4096 ||
    new URL(url).protocol !== "https:"
  )
    throw new Error("Endereço HTTPS inválido");
  await shell.openExternal(url);
});
ipcMain.handle("clipboard:copy", async (event, text) => {
  checkSender(event);
  if (typeof text !== "string" || text.length > 1024 * 1024)
    throw new Error("Texto inválido");
  clipboard.writeText(text);
});
ipcMain.handle("core:watch", async (event, id) => {
  checkSender(event);
  if (
    typeof id !== "string" ||
    (id !== "" && !/^[A-Za-z0-9_-]{1,64}$/.test(id))
  )
    throw new Error("Projeto inválido");
  if (id) {
    const projects = await request("GET", "/api/projects");
    if (!projects.some((p) => p.id === id))
      throw new Error("Projeto não encontrado");
  }
  watchedProject = id;
  connectEvents();
});
function connectEvents() {
  if (closing || !window || !key) return;
  clearTimeout(retry);
  if (socket) {
    socket.removeAllListeners();
    socket.on("error", () => {});
    socket.close();
  }
  socket = new WebSocket(
    address.replace("http:", "ws:") +
      "/api/events" +
      (watchedProject ? "?project=" + encodeURIComponent(watchedProject) : ""),
    {
      headers: { Authorization: `Bearer ${key}` },
      maxPayload: 4 * 1024 * 1024,
    },
  );
  socket.on("message", (data) => {
    try {
      window?.webContents.send("core:state", JSON.parse(data.toString()));
    } catch {}
  });
  socket.on("error", () => {});
  socket.on("close", () => {
    if (!closing && window) retry = setTimeout(connectEvents, 3000);
  });
}
app.on("window-all-closed", () => app.quit());
app.on("before-quit", () => {
  closing = true;
  clearTimeout(retry);
  socket?.close();
});
