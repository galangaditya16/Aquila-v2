import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";

// Jalankan kode UI dengan DOM dan binding tiruan; tidak membuka browser/Wails.
const source = (await readFile(new URL("../src/main.js", import.meta.url), "utf8"))
  .replace(/^import[\s\S]*?;\n/gm, "")
  .replace(/\ninit\(\);\s*$/, "\nglobalThis.ready = init();");

async function boot(overrides = {}) {
  const nodes = new Map();
  const node = (selector) => {
    if (!nodes.has(selector)) {
      nodes.set(selector, {
        innerHTML: "", textContent: "", value: "", disabled: false,
        style: {}, dataset: {}, listeners: {},
        classList: { toggle() {} },
        addEventListener(name, handler) { this.listeners[name] = handler; },
        querySelectorAll() { return []; },
        focus() {},
      });
    }
    return nodes.get(selector);
  };
  const config = {
    path: "/config/print-server.json", listen_host: "127.0.0.1", listen_port: 17891,
    version: "1.0.4", json: "{}", created: false,
    origins: [{ origin: "https://existing.example.test", scheme: "https",
      host: "existing.example.test", port: 443, type: "subdomain" }],
  };
  const context = vm.createContext({
    document: { querySelector: node, addEventListener() {}, querySelectorAll() { return []; } },
    location: { search: "" }, URLSearchParams, console,
    window: { setInterval() { return 1; } }, clearInterval() {},
    Events: { On() {} }, AquilaState: "state", AquilaLog: "log",
    GetConfig: async () => config,
    GetState: async () => ({ status: "stopped", listen: "127.0.0.1:17891" }),
    GetSettings: async () => ({}), GetLogs: async () => [],
    GetAutostart: async () => false, GetPrinterCount: async () => 1,
    ...overrides,
  });
  vm.runInContext(source, context);
  await context.ready;
  return { context, node, config };
}

test("alamat JSON langsung terlihat walau log gagal dimuat", async () => {
  const { node } = await boot({ GetLogs: async () => { throw new Error("logs unavailable"); } });
  assert.match(node("#origin-list").innerHTML, /https:\/\/existing\.example\.test/);
  assert.match(node("#origin-summary").textContent, /1 alamat/);
  assert.equal(node("#btn-origin-save").disabled, false);
});

test("submit ganda hanya menyimpan sekali dan menyegarkan daftar serta JSON", async () => {
  let calls = 0;
  const { context, node, config } = await boot({
    AddOrigin: async () => {
      calls++;
      await Promise.resolve();
      config.origins.push({ origin: "https://new.example.test", type: "subdomain" });
      config.json = '{"allowed_origins":["https://existing.example.test","https://new.example.test"]}';
    },
  });
  node("#origin-host").value = "new.example.test";
  node("#origin-scheme").value = "https";
  await Promise.all([context.doOriginSave(), context.doOriginSave()]);
  assert.equal(calls, 1);
  assert.match(node("#origin-list").innerHTML, /https:\/\/new\.example\.test/);
  assert.match(node("#cfg-json").textContent, /https:\/\/new\.example\.test/);
  assert.equal(node("#btn-origin-save").disabled, false);
  const button = node("#app").innerHTML.match(/<button[^>]*id="btn-origin-save"[^>]*>/)[0];
  assert.doesNotMatch(button, /data-action/);
});

test("alamat duplikat tetap terlihat dengan pesan biasa", async () => {
  const { context, node } = await boot({
    AddOrigin: async () => {
      const err = new Error("alamat https://existing.example.test sudah terdaftar");
      err.name = "RuntimeError";
      throw err;
    },
  });
  node("#origin-host").value = "existing.example.test";
  await context.doOriginSave();
  assert.equal(node("#origin-banner").textContent, "alamat https://existing.example.test sudah terdaftar");
  assert.match(node("#origin-list").innerHTML, /existing\.example\.test/);
});

test("konfigurasi gagal dibaca tidak dianggap daftar kosong", async () => {
  const { node } = await boot({ GetConfig: async () => { throw new Error("file tidak bisa dibaca"); } });
  assert.match(node("#origin-list").textContent, /belum berhasil dimuat/);
  assert.equal(node("#btn-origin-save").disabled, true);
  assert.equal(node("#origin-banner").textContent, "file tidak bisa dibaca");
});

test("galat layanan tampil pada panel", async () => {
  const { context, node } = await boot();
  vm.runInContext('state.service = { status: "error", last_error: "port sedang dipakai" }; renderStatus()', context);
  assert.equal(node("#service-error").textContent, "port sedang dipakai");
  assert.match(node("#service-error").className, /show err/);
});
