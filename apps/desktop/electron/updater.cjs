const { spawn } = require("node:child_process");
const CHECK_INTERVAL = 2 * 60 * 60 * 1000;

function startWindowsInstaller(file, launch = spawn) {
  return new Promise((resolve, reject) => {
    const child = launch(file, ["--updated", "--force-run"], {
      detached: true,
      stdio: "ignore",
    });
    child.once("error", reject);
    child.once("spawn", () => {
      child.unref();
      resolve();
    });
  });
}

function createUpdater({
  updater,
  currentVersion,
  enabled,
  publish,
  prepareInstall,
  recover,
  launchInstaller,
  pending,
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
  async function restoreAfterFailure() {
    await pending?.clear().catch(() => {});
    await recover().catch(() => {});
  }
  updater.on("error", (error) => {
    const interrupted = state.status === "installing";
    fail(error);
    if (interrupted) void restoreAfterFailure();
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
    if (
      !enabled ||
      installing ||
      [
        "installing",
        "restarting",
        "installed",
        "restart-failed",
        "interrupted",
      ].includes(state.status)
    )
      return Promise.resolve(state);
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
    if (
      !enabled ||
      !state.version ||
      !["available", "error"].includes(state.status)
    )
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
      let preparing = false;
      try {
        const files = await updater.downloadUpdate();
        if (!files?.length)
          throw new Error("O download não foi concluído. Tente novamente.");
        set({ status: "installing", percent: 100 });
        preparing = true;
        await prepareInstall();
        await pending?.save({
          fromVersion: currentVersion,
          version: state.version,
        });
        if (launchInstaller) await launchInstaller(files[0]);
        else updater.quitAndInstall(false, true);
      } catch (error) {
        fail(error);
        if (preparing) await restoreAfterFailure();
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
    async resume(launchedByInstaller) {
      if (!enabled) return false;
      const saved = await pending?.load().catch(() => undefined);
      if (saved?.version === currentVersion || launchedByInstaller) {
        set({
          status: "restarting",
          version: currentVersion,
          previousVersion:
            saved?.version === currentVersion ? saved.fromVersion : undefined,
        });
        return true;
      }
      if (saved?.fromVersion === currentVersion) {
        set({ status: "interrupted", version: saved.version });
      }
      return false;
    },
    restarting() {
      if (state.status === "restart-failed")
        set({ status: "restarting", error: undefined });
    },
    async finishRestart(error) {
      if (state.status !== "restarting") return;
      if (error) {
        set({
          status: "restart-failed",
          error: error.message || String(error),
        });
      } else {
        await pending?.clear().catch(() => {});
        set({ status: "installed" });
      }
    },
    async dismiss() {
      if (!["installed", "interrupted"].includes(state.status)) return;
      if (state.status === "interrupted")
        await pending?.clear().catch(() => {});
      set({ status: "idle", version: undefined, previousVersion: undefined });
      void check();
    },
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
module.exports = { createUpdater, startWindowsInstaller, CHECK_INTERVAL };
