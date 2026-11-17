const { BrowserWindow, WebContentsView } = require("electron");
const { readFile } = require("node:fs/promises");
const path = require("node:path");
const { startRuntime, connectRuntime } = require("./dbeaver-runtime.cjs");

let embedded;
const detached = new Map();
const detachedWindows = new Set();

async function dispose(state) {
  if (state.disposal) return state.disposal;
  state.disposal = (async () => {
    state.abort.abort();
    if (state.view) {
      if (state.host && !state.host.isDestroyed())
        state.host.contentView.removeChildView(state.view);
      if (!state.view.webContents.isDestroyed()) state.view.webContents.close();
    }
    if (state.detached && !state.detached.isDestroyed())
      state.detached.destroy();
    const runtime = state.runtime ?? (await state.starting?.catch(() => null));
    await runtime?.stop();
  })();
  return state.disposal;
}

async function closeDBeaver(session) {
  if (session) {
    if (embedded?.session !== session) return;
    const state = embedded;
    embedded = undefined;
    await dispose(state);
    return;
  }
  const states = [embedded, ...detached.values()].filter(Boolean);
  embedded = undefined;
  detached.clear();
  await Promise.all(states.map(dispose));
}

function layoutDBeaver(session, bounds) {
  if (embedded?.session !== session || !bounds) return;
  const { x, y, width, height } = bounds;
  if (![x, y, width, height].every(Number.isSafeInteger)) return;
  const [viewportWidth, viewportHeight] = embedded.window.getContentSize();
  if (x < 0 || y < 56 || width < 0 || height < 0) return;
  embedded.bounds = {
    x,
    y,
    width: Math.max(0, Math.min(width, viewportWidth - x)),
    height: Math.max(0, Math.min(height, viewportHeight - y)),
  };
  if (embedded.view) {
    embedded.view.setBounds(embedded.bounds);
    embedded.view.setVisible(
      embedded.bounds.width > 0 && embedded.bounds.height > 0,
    );
  }
}

async function openDBeaver(
  window,
  session,
  database,
  getConnection,
  bundle,
  fonts,
) {
  const existing = detached.get(database);
  if (existing) {
    if (existing.detached.isDestroyed()) {
      detached.delete(database);
      await dispose(existing);
      return openDBeaver(
        window,
        session,
        database,
        getConnection,
        bundle,
        fonts,
      );
    }
    existing.detached.show();
    existing.detached.focus();
    return "detached";
  }
  const closing = embedded ? closeDBeaver(embedded.session) : Promise.resolve();
  const state = { session, database, window, abort: new AbortController() };
  embedded = state;
  const signal = state.abort.signal;
  try {
    await closing;
    signal.throwIfAborted();
    const connection = await getConnection();
    signal.throwIfAborted();
    state.starting = startRuntime(connection, bundle, signal);
    const runtime = await state.starting;
    state.runtime = runtime;
    signal.throwIfAborted();
    const cookies = await connectRuntime(runtime, signal);
    signal.throwIfAborted();
    const view = new WebContentsView({
      webPreferences: {
        partition: `cloudforge-dbeaver-${session}`,
        contextIsolation: true,
        nodeIntegration: false,
        sandbox: true,
        webSecurity: true,
        spellcheck: false,
      },
    });
    state.view = view;
    const contents = view.webContents;
    const { origin } = runtime;
    const allow = (url) => new URL(url).origin === origin;
    contents.setWindowOpenHandler(() => ({ action: "deny" }));
    contents.on("will-navigate", (event, url) => {
      if (!allow(url)) event.preventDefault();
    });
    contents.on("will-redirect", (event, url) => {
      if (!allow(url)) event.preventDefault();
    });
    contents.session.setPermissionRequestHandler(
      (_contents, _permission, respond) => respond(false),
    );
    contents.session.setPermissionCheckHandler(() => false);
    contents.session.webRequest.onBeforeRequest(({ url }, callback) => {
      const target = new URL(url);
      if (target.protocol === "ws:") target.protocol = "http:";
      callback({
        cancel:
          !["blob:", "data:"].includes(target.protocol) && !allow(target.href),
      });
    });
    await Promise.all(
      cookies.map(([name, value]) =>
        contents.session.cookies.set({
          url: origin,
          name,
          value,
          httpOnly: true,
          sameSite: "strict",
        }),
      ),
    );
    const css = await readFile(
      path.join(__dirname, "dbeaver-theme.css"),
      "utf8",
    );
    const fontCSS = await Promise.all(
      fonts.map(async ({ file, family, weight }) => {
        const data = await readFile(file);
        return `@font-face { font-family: "${family}"; font-weight: ${weight}; src: url(data:font/woff2;base64,${data.toString("base64")}) format("woff2"); }`;
      }),
    );
    signal.throwIfAborted();
    view.setBackgroundColor("#0b0d11");
    view.setVisible(false);
    window.contentView.addChildView(view);
    state.host = window;
    await contents.loadURL(origin);
    await contents.insertCSS(`${fontCSS.join("\n")}\n${css}`, {
      cssOrigin: "user",
    });
    signal.throwIfAborted();
    layoutDBeaver(session, state.bounds);
    return "ready";
  } catch (error) {
    if (embedded === state) await closeDBeaver(session);
    else await dispose(state);
    throw error;
  }
}

async function detachDBeaver(session, name, icon, titlebarURL, onClosed) {
  if (embedded?.session !== session || !embedded.view || !embedded.runtime)
    throw new Error("O DBeaver ainda não está pronto para destacar.");
  const state = embedded;
  const detachedWindow = new BrowserWindow({
    width: 1200,
    height: 800,
    minWidth: 700,
    minHeight: 500,
    backgroundColor: "#0b0d11",
    icon,
    title: `DBeaver · ${name}`,
    frame: false,
    autoHideMenuBar: true,
    show: false,
    webPreferences: {
      preload: path.join(__dirname, "dbeaver-window-preload.cjs"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      webSecurity: true,
    },
  });
  detachedWindows.add(detachedWindow);
  detachedWindow.on("closed", () => detachedWindows.delete(detachedWindow));
  detachedWindow.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  detachedWindow.webContents.on("will-navigate", (event) =>
    event.preventDefault(),
  );
  try {
    await detachedWindow.loadURL(titlebarURL);
  } catch (error) {
    detachedWindow.destroy();
    throw error;
  }
  if (embedded !== state || state.abort.signal.aborted) {
    detachedWindow.destroy();
    throw new Error(
      "A sessão do DBeaver foi encerrada antes de abrir a janela.",
    );
  }
  try {
    state.window.contentView.removeChildView(state.view);
    detachedWindow.contentView.addChildView(state.view);
    state.host = detachedWindow;
  } catch (error) {
    state.window.contentView.addChildView(state.view);
    state.host = state.window;
    detachedWindow.destroy();
    throw error;
  }
  state.detached = detachedWindow;
  embedded = undefined;
  detached.set(state.database, state);
  const layout = () => {
    if (detachedWindow.isDestroyed()) return;
    const [width, height] = detachedWindow.getContentSize();
    state.view.setBounds({
      x: 0,
      y: 56,
      width,
      height: Math.max(0, height - 56),
    });
    state.view.setVisible(true);
  };
  detachedWindow.on("resize", layout);
  detachedWindow.on("closed", () => {
    if (detached.get(state.database) !== state) return;
    detached.delete(state.database);
    void dispose(state)
      .catch((error) => console.error("Falha ao encerrar DBeaver:", error))
      .finally(() => onClosed(state.database));
  });
  layout();
  detachedWindow.show();
  return detachedWindow;
}

function detachedDBeaverWindow(event) {
  for (const target of detachedWindows) {
    if (event.sender !== target.webContents) continue;
    if (event.senderFrame !== target.webContents.mainFrame)
      throw new Error("Origem da janela do DBeaver inválida");
    return target;
  }
  return null;
}

module.exports = {
  openDBeaver,
  layoutDBeaver,
  closeDBeaver,
  detachDBeaver,
  detachedDBeaverWindow,
  hasDBeaver: () => Boolean(embedded || detached.size),
};
