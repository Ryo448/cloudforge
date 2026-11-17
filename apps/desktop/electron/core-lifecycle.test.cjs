const { test } = require("node:test");
const assert = require("node:assert/strict");
const { EventEmitter } = require("node:events");
const { createCoreManager, matchesCore } = require("./core-lifecycle.cjs");
const current = {
  service: "cloudforge",
  version: "2.1.2",
  apiVersion: 8,
  binarySHA256: "current",
  lifecycle: true,
};
function fixture(initial, overrides = {}) {
  let running = initial;
  const calls = [];
  const manager = createCoreManager({
    binary: "core",
    web: "web",
    dataDir: "data",
    version: "2.1.2",
    apiVersion: 8,
    hashBinary: async () => "current",
    pause: async () => {},
    resetKey() {},
    request: async (method) => {
      if (method === "POST") {
        calls.push("shutdown");
        running = undefined;
        return;
      }
      if (!running) throw new Error("offline");
      return running;
    },
    launch() {
      calls.push("launch");
      running = current;
      const child = new EventEmitter();
      child.unref = () => {};
      return child;
    },
    stopLegacy: async () => {
      calls.push("legacy-stop");
      running = undefined;
    },
    ...overrides,
  });
  return { manager, calls };
}
test("requires API, app version and binary hash to match", () => {
  assert.ok(matchesCore(current, "2.1.2", "current", 8));
  for (const change of [
    { version: "2.1.1" },
    { apiVersion: 7 },
    { binarySHA256: "old" },
    { binarySHA256: undefined },
  ]) {
    assert.ok(!matchesCore({ ...current, ...change }, "2.1.2", "current", 8));
  }
});
test("reuses matching core", async () => {
  const f = fixture(current);
  await f.manager.ensure();
  assert.deepEqual(f.calls, []);
});
test("replaces outdated core even when API version matches", async () => {
  const f = fixture({ ...current, version: "2.1.1" });
  await f.manager.ensure();
  assert.deepEqual(f.calls, ["shutdown", "launch"]);
});
test("replaces a different build with the same version", async () => {
  const f = fixture({ ...current, binarySHA256: "old" });
  await f.manager.ensure();
  assert.deepEqual(f.calls, ["shutdown", "launch"]);
});
test("migrates legacy core and serializes concurrent startup attempts", async () => {
  const f = fixture({ service: "cloudforge", apiVersion: 8, version: "2.1.2" });
  await Promise.all([
    f.manager.ensure(),
    f.manager.ensure(),
    f.manager.ensure(),
  ]);
  assert.deepEqual(f.calls, ["legacy-stop", "launch"]);
});
test("refuses to launch if old core cannot be stopped", async () => {
  const f = fixture(
    { ...current, version: "2.0.0", lifecycle: false },
    {
      stopLegacy: async () => {
        throw new Error("unexpected executable");
      },
    },
  );
  await assert.rejects(f.manager.ensure(), /unexpected executable/);
  assert.deepEqual(f.calls, []);
});
