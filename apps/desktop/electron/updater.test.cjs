const { test } = require("node:test");
const assert = require("node:assert/strict");
const { EventEmitter } = require("node:events");
const { createUpdater, CHECK_INTERVAL } = require("./updater.cjs");
const tick = () => new Promise((resolve) => setImmediate(resolve));
function fixture(overrides = {}) {
  const calls = [];
  const updater = new EventEmitter();
  updater.checkForUpdates = async () => {
    calls.push("check");
    updater.emit("update-available", { version: "2.2.0" });
  };
  updater.downloadUpdate = async () => {
    calls.push("download");
    updater.emit("download-progress", {
      percent: 42,
      total: 100,
      transferred: 42,
      bytesPerSecond: 10,
    });
    return ["installer.exe"];
  };
  updater.quitAndInstall = (silent, restart) => {
    calls.push(["install", silent, restart]);
  };
  let interval;
  const states = [];
  const controller = createUpdater({
    updater,
    currentVersion: "2.1.2",
    enabled: true,
    publish: (state) => states.push(state),
    prepareInstall: async () => {
      calls.push("stop-core");
    },
    recover: async () => {
      calls.push("recover");
    },
    schedule: (callback, delay) => {
      interval = { callback, delay };
      return { unref() {} };
    },
    ...overrides,
  });
  return { updater, controller, calls, states, interval: () => interval };
}
test("checks on launch and every two hours without downloading before consent", async () => {
  const f = fixture();
  f.controller.start();
  await tick();
  assert.deepEqual(f.calls, ["check"]);
  assert.equal(f.interval().delay, CHECK_INTERVAL);
  assert.equal(CHECK_INTERVAL, 7200000);
  assert.equal(f.updater.autoDownload, false);
  assert.equal(f.updater.autoInstallOnAppQuit, false);
  f.interval().callback();
  await tick();
  assert.deepEqual(f.calls, ["check", "check"]);
});
test("confirmed update reports progress, stops core and opens installer once", async () => {
  const f = fixture();
  f.controller.start();
  await tick();
  const first = f.controller.install();
  assert.equal(f.controller.install(), first);
  await first;
  assert.deepEqual(f.calls, [
    "check",
    "download",
    "stop-core",
    ["install", true, true],
  ]);
  assert.equal(f.states.find((s) => s.percent === 42).transferred, 42);
});
test("download failure remains retryable and does not install", async () => {
  const f = fixture();
  f.controller.start();
  await tick();
  f.updater.downloadUpdate = async () => {
    throw new Error("offline");
  };
  await f.controller.install();
  assert.equal(f.controller.state().status, "error");
  assert.equal(f.controller.state().version, "2.2.0");
  assert.ok(!f.calls.includes("stop-core"));
  assert.ok(!f.calls.some(Array.isArray));
  f.updater.downloadUpdate = async () => ["installer.exe"];
  await f.controller.install();
  assert.equal(f.controller.state().status, "installing");
});
test("shutdown failure recovers core and prevents installation", async () => {
  const f = fixture({
    prepareInstall: async () => {
      throw new Error("busy");
    },
  });
  f.controller.start();
  await tick();
  await f.controller.install();
  assert.equal(f.controller.state().status, "error");
  assert.ok(f.calls.includes("recover"));
  assert.ok(!f.calls.some(Array.isArray));
});
test("unpackaged apps do not check or install", async () => {
  const f = fixture({ enabled: false });
  f.controller.start();
  await tick();
  assert.deepEqual(f.calls, []);
  assert.throws(() => f.controller.install());
});
test("asynchronous installer errors restore the core", async () => {
  const f = fixture();
  f.controller.start();
  await tick();
  await f.controller.install();
  f.updater.emit("error", new Error("installer failed"));
  await tick();
  assert.equal(f.controller.state().status, "error");
  assert.ok(f.calls.includes("recover"));
});
test("confirmation waits for an ongoing release check before downloading", async () => {
  const f = fixture();
  let finishCheck;
  f.updater.checkForUpdates = async () => {
    f.calls.push("check");
    f.updater.emit("update-available", { version: "2.2.0" });
    await new Promise((resolve) => {
      finishCheck = resolve;
    });
  };
  f.controller.start();
  await tick();
  const installation = f.controller.install();
  assert.deepEqual(f.calls, ["check"]);
  finishCheck();
  await installation;
  assert.deepEqual(f.calls, [
    "check",
    "download",
    "stop-core",
    ["install", true, true],
  ]);
});
