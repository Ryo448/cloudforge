const { WebContentsView } = require("electron");
const { readFile } = require("node:fs/promises");
const path = require("node:path");
const { startRuntime, connectRuntime } = require("./dbeaver-runtime.cjs");

let current;
async function closeDBeaver(session) {
  if (!current || (session && current.session !== session)) return;
  const state = current;
  current = undefined;
  state.abort.abort();
  if (state.view) {
    if (!state.window.isDestroyed())
      state.window.contentView.removeChildView(state.view);
    if (!state.view.webContents.isDestroyed()) state.view.webContents.close();
  }
  const runtime = state.runtime ?? (await state.starting?.catch(() => null));
  await runtime?.stop();
}

function layoutDBeaver(session, bounds) {
  if (current?.session !== session || !bounds) return;
  const { x, y, width, height } = bounds;
  if (![x, y, width, height].every(Number.isSafeInteger)) return;
  const [viewportWidth, viewportHeight] = current.window.getContentSize();
  if (x < 0 || y < 56 || width < 0 || height < 0) return;
  current.bounds = {
    x,
    y,
    width: Math.max(0, Math.min(width, viewportWidth - x)),
    height: Math.max(0, Math.min(height, viewportHeight - y)),
  };
  if (current.view) {
    current.view.setBounds(current.bounds);
    current.view.setVisible(
      current.bounds.width > 0 && current.bounds.height > 0,
    );
  }
}

async function openDBeaver(window, session, getConnection, bundle, fonts) {
  const closing = closeDBeaver();
  const state = { session, window, abort: new AbortController() };
  current = state;
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
    await contents.loadURL(origin);
    await contents.insertCSS(`${fontCSS.join("\n")}\n${css}`, {
      cssOrigin: "user",
    });
    signal.throwIfAborted();
    layoutDBeaver(session, state.bounds);
  } catch (error) {
    if (current === state) await closeDBeaver(session);
    else await state.runtime?.stop();
    throw error;
  }
}

module.exports = {
  openDBeaver,
  layoutDBeaver,
  closeDBeaver,
  hasDBeaver: () => Boolean(current),
};
