const CHECK_INTERVAL = 2 * 60 * 60 * 1000;

function createUpdater({
  updater,
  currentVersion,
  enabled,
  publish,
  prepareInstall,
  recover,
  schedule = setInterval,
}) {
  let state = { status: "idle", currentVersion };
  let checking;
  let installing;
  let timer;
  updater.autoDownload = false;
  updater.autoInstallOnAppQuit = false;
  updater.allowPrerelease = false;
  updater.allowDowngrade = false;
  const set = (patch) => {
    state = { ...state, ...patch };
    publish(state);
  };
  const fail = (error) =>
    set({ status: "error", error: error.message || String(error) });
  updater.on("error", (error) => {
    const interrupted = state.status === "installing";
    fail(error);
    if (interrupted) void recover().catch(() => {});
  });
  updater.on("update-available", (info) =>
    set({
      status: "available",
      version: info.version,
      error: undefined,
      releaseNotes:
        typeof info.releaseNotes === "string" ? info.releaseNotes : undefined,
    }),
  );
  updater.on("update-not-available", () =>
    set({ status: "idle", version: undefined, error: undefined }),
  );
  updater.on("download-progress", (progress) =>
    set({
      status: "downloading",
      percent: progress.percent,
      transferred: progress.transferred,
      total: progress.total,
      bytesPerSecond: progress.bytesPerSecond,
    }),
  );
  function check() {
    if (!enabled || installing) return Promise.resolve(state);
    if (checking) return checking;
    set({ status: "checking", error: undefined });
    checking = Promise.resolve()
      .then(() => updater.checkForUpdates())
      .catch(fail)
      .finally(() => {
        checking = undefined;
      });
    return checking;
  }
  function install() {
    if (installing) return installing;
    if (checking) return checking.then(() => install());
    if (!enabled || !state.version)
      throw new Error("Nenhuma atualização disponível.");
    set({
      status: "downloading",
      error: undefined,
      percent: 0,
      transferred: 0,
      total: 0,
      bytesPerSecond: 0,
    });
    installing = (async () => {
      try {
        const files = await updater.downloadUpdate();
        if (!files?.length)
          throw new Error("O download não foi concluído. Tente novamente.");
        set({ status: "installing", percent: 100 });
        await prepareInstall();
        updater.quitAndInstall(true, true);
      } catch (error) {
        fail(error);
        await recover().catch(() => {});
      } finally {
        installing = undefined;
      }
      return state;
    })();
    return installing;
  }
  return {
    state: () => state,
    install,
    start() {
      if (!enabled || timer) return;
      void check();
      timer = schedule(() => void check(), CHECK_INTERVAL);
      timer.unref?.();
    },
    dispose() {
      clearInterval(timer);
    },
  };
}
module.exports = { createUpdater, CHECK_INTERVAL };
