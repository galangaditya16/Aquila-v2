import { Events } from "@wailsio/runtime";
import {
  AddOrigin,
  AquilaLog,
  AquilaState,
  ClearLogs,
  CopyServerAddress,
  DiagnosePort,
  ExportConfig,
  GetAutostart,
  GetConfig,
  GetLogs,
  GetPrinterCount,
  GetSettings,
  GetState,
  ImportConfig,
  OpenConfigFolder,
  OpenServerURL,
  QuitApp,
  ReloadConfig,
  RemoveOrigin,
  ResetConfig,
  RestartService,
  SaveSettings,
  SetAutostart,
  SetListen,
  ShowPanel,
  StartService,
  StopService,
  TestConnection,
  ToggleService,
  UninstallApp,
  UpdateOrigin,
} from "./api.js";
import "./style.css";

// Batas baris log di DOM. Sama dengan maxLogRows di sisi Go: memori UI tetap
// datar walau layanan berjalan berhari-hari.
const MAX_LOG_LINES = 200;

const isPopup = new URLSearchParams(location.search).has("popup");

// State ringkas di memori; hanya objek kecil yang disimpan, tanpa cache besar.
const state = {
  service: null,
  config: null,
  settings: null,
  origins: [],
  autostart: false,
  printers: -1, // -1 = belum dimuat
  editingIndex: -1,
  savingOrigin: false,
  autoscroll: true,
  uptimeTimer: 0,
};

const esc = (s) =>
  String(s ?? "").replace(/[&<>"']/g, (c) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  }[c]));

const $ = (sel) => document.querySelector(sel);
const errorMessage = (err) => err?.message || String(err);

// ------------------------------------------------------------------ tampilan

const STATUS_TEXT = {
  stopped: "Layanan berhenti",
  starting: "Layanan memulai…",
  running: "Layanan berjalan",
  error: "Layanan bermasalah",
};

function statusClass(st) {
  if (!st) return "";
  if (st.status === "error") return "error";
  if (st.status === "starting") return "starting";
  if (st.status === "running") return st.health_ok ? "running" : "error";
  return "";
}

function statusLabel(st) {
  if (!st) return "…";
  if (st.status === "running" && !st.health_ok) return "Layanan tidak merespons";
  return STATUS_TEXT[st.status] || st.status;
}

function formatUptime(seconds) {
  if (!seconds || seconds < 1) return "—";
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0) return `${h} jam ${m} mnt`;
  if (m > 0) return `${m} mnt ${s} dtk`;
  return `${s} dtk`;
}

function formatTime(time) {
  return String(time || "").split(" ").pop();
}

function toggleLabel(st) {
  return st && (st.status === "running" || st.status === "starting")
    ? "Hentikan Layanan"
    : "Mulai Layanan";
}

// ------------------------------------------------------------------ render

function renderStatus() {
  const st = state.service;
  const cls = statusClass(st);
  const label = statusLabel(st);

  const serviceError = $("#service-error");
  if (serviceError) {
    serviceError.textContent = st?.status === "error"
      ? st.last_error || "Layanan gagal dimulai."
      : "";
    serviceError.className = `banner${st?.status === "error" ? " show err" : ""}`;
  }

  const pillDot = $("#pill-dot");
  const pillText = $("#pill-text");
  if (pillDot && pillText) {
    pillDot.className = `dot ${cls}`;
    pillText.textContent = label;
  }

  const ppDot = $("#pp-dot");
  const ppStatus = $("#pp-status");
  if (ppDot && ppStatus) {
    ppDot.className = `big-dot ${cls}`;
    ppStatus.textContent = label;
    $("#pp-listen").textContent = st ? `http://${st.listen || "…"}` : "…";
    $("#pp-uptime").textContent = st ? `Aktif selama ${formatUptime(st.uptime_seconds)}` : "";
    $("#pp-health").textContent = st
      ? st.status === "running"
        ? st.health_ok
          ? "Koneksi sehat"
          : "Tidak merespons"
        : "—"
      : "";
    $("#pp-toggle").textContent = toggleLabel(st);
    $("#pp-toggle").disabled = !st || st.status === "starting";
  }

  const btnToggle = $("#btn-toggle");
  if (btnToggle) {
    btnToggle.textContent = toggleLabel(st);
    btnToggle.disabled = !st || st.status === "starting";
    const btnRestart = $("#btn-restart");
    if (btnRestart) btnRestart.disabled = !st || st.status === "stopped";
    const btnTest = $("#btn-test");
    if (btnTest) btnTest.disabled = !st || st.status !== "running";
    const btnOpen = $("#btn-open-url");
    if (btnOpen) btnOpen.disabled = !st || st.status !== "running";
  }

  renderBeranda();
  startUptimeTick();
}

function renderBeranda() {
  const st = state.service;
  const cfg = state.config;
  if (!$("#bd-status")) return;
  $("#bd-status").textContent = statusLabel(st);
  $("#bd-listen").textContent = st ? st.listen || "—" : "—";
  $("#bd-health").textContent = st
    ? st.status === "running"
      ? st.health_ok
        ? "Sehat"
        : "Tidak merespons"
      : "—"
    : "—";
  $("#bd-uptime").textContent =
    st && st.status === "running" ? formatUptime(st.uptime_seconds) : "—";
  $("#bd-printers").textContent =
    state.printers < 0 ? "Memuat…" : `${state.printers} printer`;
  $("#bd-path").textContent = cfg ? cfg.path : "—";
}

function renderOrigins() {
  const list = $("#origin-list");
  if (!list) return;
  $("#btn-origin-save").disabled = !state.config || state.savingOrigin;
  if (!state.config) {
    list.innerHTML = '<p class="muted">Memuat alamat dari konfigurasi…</p>';
    return;
  }
  if (state.origins.length === 0) {
    list.innerHTML =
      '<p class="muted">Belum ada alamat yang diizinkan. Tambahkan alamat aplikasi farmasi Anda.</p>';
  } else {
    const typeLabel = {
      local: "lokal",
      ip: "IP",
      domain: "domain",
      subdomain: "subdomain",
    };
    list.innerHTML = state.origins
      .map(
        (o, i) => `
      <div class="origin-item">
        <span class="origin-text">${esc(o.origin)}</span>
        <span class="tag">${esc(typeLabel[o.type] || o.type)}</span>
        <button class="icon-btn" data-action="origin-edit" data-index="${i}">Ubah</button>
        <button class="icon-btn danger" data-action="origin-remove" data-index="${i}">Hapus</button>
      </div>`
      )
      .join("");
  }
  const summary = $("#origin-summary");
  if (summary) {
    summary.textContent = `${state.origins.length} alamat diizinkan. Hanya origin persis (skema + host + port) yang diterima.`;
  }
}

function renderSettings() {
  const cfg = state.config;
  const st = state.settings;
  if (!$("#listen-host")) return;
  if (cfg) {
    $("#listen-host").value = cfg.listen_host || "127.0.0.1";
    $("#listen-port").value = String(cfg.listen_port || 17891);
  }
  if (st) {
    $("#set-start-service").checked = !!st.start_service_on_launch;
    $("#set-open-window").checked = !!st.open_window_on_launch;
    $("#set-hide-close").checked = !!st.hide_window_on_close;
    $("#set-notify").checked = !!st.notify_on_error;
    $("#set-confirm-quit").checked = !!st.confirm_quit;
    $("#set-autostart").checked = !!state.autostart;
  }
  const json = $("#cfg-json");
  if (json && cfg) json.textContent = cfg.json || "(kosong)";
  const ver = $("#cfg-version");
  if (ver && cfg) ver.textContent = cfg.version || "—";
}

function renderAll() {
  renderStatus();
  renderOrigins();
  renderSettings();
}

// ------------------------------------------------------------------- banner

function showBanner(id, message, ok) {
  const el = $(id);
  if (!el) return;
  el.textContent = message;
  el.className = `banner show ${ok ? "ok" : "err"}`;
}

function hideBanner(id) {
  const el = $(id);
  if (el) el.className = "banner";
}

// --------------------------------------------------------------------- log

function appendLog(entry) {
  const view = $("#log-view");
  if (!view || !entry) return;
  const line = document.createElement("div");
  line.className = `log-line ${esc(entry.level || "INFO")}`;
  const t = document.createElement("span");
  t.className = "t";
  t.textContent = formatTime(entry.time);
  const lv = document.createElement("span");
  lv.className = "lv";
  lv.textContent = entry.level || "INFO";
  const m = document.createElement("span");
  m.textContent = entry.message || "";
  line.append(t, lv, m);
  view.appendChild(line);
  // Buang baris tertua agar DOM tetap 200 baris (memori terikat).
  while (view.childElementCount > MAX_LOG_LINES) {
    view.removeChild(view.firstElementChild);
  }
  if (state.autoscroll) {
    view.scrollTop = view.scrollHeight;
  }
}

// ------------------------------------------------------------------ uptime

// Satu-satunya timer di UI: hanya memperbarui teks uptime selama layanan
// berjalan, dan otomatis mati saat berhenti.
function startUptimeTick() {
  const running = state.service && state.service.status === "running";
  if (running && !state.uptimeTimer) {
    state.uptimeTimer = window.setInterval(() => {
      if (state.service) state.service.uptime_seconds += 1;
      renderBeranda();
    }, 1000);
  } else if (!running && state.uptimeTimer) {
    clearInterval(state.uptimeTimer);
    state.uptimeTimer = 0;
  }
}

// ------------------------------------------------------------------- aksi

async function withBusy(button, fn) {
  if (button) button.disabled = true;
  try {
    await fn();
  } catch (err) {
    console.error(err);
  } finally {
    if (button) button.disabled = false;
    renderStatus();
  }
}

async function doToggleService(button) {
  await withBusy(button, async () => {
    await ToggleService();
  });
}

async function doTestConnection() {
  try {
    const result = await TestConnection();
    showBanner(
      "#banner",
      `${result.message} (${result.latency_ms} ms)`,
      result.ok
    );
  } catch (err) {
    showBanner("#banner", String(err), false);
  }
}

async function doDiagnosePort(button) {
  await withBusy(button, async () => {
    try {
      const result = await DiagnosePort();
      const detail = result.in_use
        ? `${result.message} Tutup aplikasi tersebut atau ubah port layanan.`
        : result.message;
      showBanner("#banner", detail, !result.in_use);
    } catch (err) {
      showBanner("#banner", `Pemeriksaan pemakai port gagal: ${errorMessage(err)}`, false);
    }
  });
}

async function doOriginSave() {
  if (!state.config || state.savingOrigin) return;
  state.savingOrigin = true;
  $("#btn-origin-save").disabled = true;
  const scheme = $("#origin-scheme").value;
  const host = $("#origin-host").value.trim();
  const port = $("#origin-port").value.trim();
  hideBanner("#origin-banner");
  try {
    if (state.editingIndex >= 0) {
      await UpdateOrigin(state.editingIndex, scheme, host, port);
    } else {
      await AddOrigin(scheme, host, port);
    }
    resetOriginForm();
    await reloadConfigData();
    showBanner("#origin-banner", "Alamat disimpan.", true);
  } catch (err) {
    await reloadConfigData().catch(() => {});
    showBanner("#origin-banner", errorMessage(err), false);
  } finally {
    state.savingOrigin = false;
    renderOrigins();
  }
}

function resetOriginForm() {
  state.editingIndex = -1;
  $("#origin-form-title").textContent = "Tambah Alamat Diizinkan";
  $("#btn-origin-save").textContent = "Tambah";
  $("#btn-origin-cancel").style.display = "none";
  $("#origin-scheme").value = "https";
  $("#origin-host").value = "";
  $("#origin-port").value = "";
}

function doOriginEdit(index) {
  const o = state.origins[index];
  if (!o) return;
  state.editingIndex = index;
  $("#origin-form-title").textContent = "Ubah Alamat";
  $("#btn-origin-save").textContent = "Simpan Perubahan";
  $("#btn-origin-cancel").style.display = "";
  $("#origin-scheme").value = o.scheme;
  $("#origin-host").value = o.host;
  $("#origin-port").value = o.port ? String(o.port) : "";
  hideBanner("#origin-banner");
  $("#origin-host").focus();
}

async function doOriginRemove(index) {
  hideBanner("#origin-banner");
  try {
    await RemoveOrigin(index);
    resetOriginForm();
    await reloadConfigData();
  } catch (err) {
    showBanner("#origin-banner", String(err), false);
  }
}

async function doSaveListen() {
  const host = $("#listen-host").value.trim();
  const port = parseInt($("#listen-port").value.trim(), 10);
  hideBanner("#settings-banner");
  try {
    await SetListen(host, port, true);
    state.config = await GetConfig();
    state.service = await GetState();
    renderAll();
    showBanner("#settings-banner", "Alamat dengarkan disimpan.", true);
  } catch (err) {
    showBanner("#settings-banner", String(err), false);
  }
}

async function doSaveSettings() {
  hideBanner("#settings-banner");
  try {
    const settings = {
      start_service_on_launch: $("#set-start-service").checked,
      open_window_on_launch: $("#set-open-window").checked,
      hide_window_on_close: $("#set-hide-close").checked,
      notify_on_error: $("#set-notify").checked,
      confirm_quit: $("#set-confirm-quit").checked,
    };
    await SaveSettings(settings);
    state.settings = settings;
    showBanner("#settings-banner", "Preferensi disimpan.", true);
  } catch (err) {
    showBanner("#settings-banner", String(err), false);
  }
}

async function doSetAutostart() {
  hideBanner("#settings-banner");
  try {
    await SetAutostart($("#set-autostart").checked);
    state.autostart = await GetAutostart();
    $("#set-autostart").checked = !!state.autostart;
  } catch (err) {
    $("#set-autostart").checked = !!state.autostart;
    showBanner("#settings-banner", String(err), false);
  }
}

// ------------------------------------------------------------------ event

function bindEvents() {
  // Delegasi satu listener untuk seluruh tombol ber-data-action.
  document.addEventListener("click", (ev) => {
    const btn = ev.target.closest("[data-action]");
    if (!btn || btn.disabled) return;
    const index = parseInt(btn.dataset.index || "-1", 10);
    switch (btn.dataset.action) {
      case "toggle-service":
        doToggleService(btn);
        break;
      case "restart-service":
        withBusy(btn, () => RestartService());
        break;
      case "test-connection":
        doTestConnection();
        break;
      case "diagnose-port":
        doDiagnosePort(btn);
        break;
      case "copy-address":
        withBusy(btn, async () => {
          await CopyServerAddress();
          showBanner("#banner", "Alamat disalin ke papan klip.", true);
        });
        break;
      case "open-url":
        withBusy(btn, () => OpenServerURL());
        break;
      case "open-folder":
        withBusy(btn, () => OpenConfigFolder());
        break;
      case "refresh-origins":
        withBusy(btn, async () => {
          try {
            await ReloadConfig();
            resetOriginForm();
            await reloadConfigData();
            hideBanner("#origin-banner");
          } catch (err) {
            showBanner("#origin-banner", errorMessage(err), false);
          }
        });
        break;
      case "origin-cancel":
        resetOriginForm();
        hideBanner("#origin-banner");
        break;
      case "origin-edit":
        doOriginEdit(index);
        break;
      case "origin-remove":
        doOriginRemove(index);
        break;
      case "save-listen":
        doSaveListen();
        break;
      case "save-settings":
        doSaveSettings();
        break;
      case "import-config":
        withBusy(btn, async () => {
          await ImportConfig();
          await reloadConfigData();
          showBanner("#settings-banner", "Konfigurasi diimpor.", true);
        });
        break;
      case "export-config":
        withBusy(btn, () => ExportConfig());
        break;
      case "reload-config":
        withBusy(btn, async () => {
          await ReloadConfig();
          await reloadConfigData();
          showBanner("#settings-banner", "Konfigurasi dimuat ulang.", true);
        });
        break;
      case "reset-config":
        withBusy(btn, async () => {
          await ResetConfig();
          await reloadConfigData();
          showBanner("#settings-banner", "Konfigurasi dikembalikan ke default.", true);
        });
        break;
      case "clear-logs":
        withBusy(btn, async () => {
          await ClearLogs();
          $("#log-view").replaceChildren();
        });
        break;
      case "show-panel":
        withBusy(btn, () => ShowPanel());
        break;
      case "quit":
        QuitApp();
        break;
      case "uninstall":
        withBusy(btn, async () => {
          try {
            await UninstallApp();
          } catch (err) {
            showBanner("#uninstall-banner", errorMessage(err), false);
          }
        });
        break;
    }
  });

  // Enter di form origin langsung menyimpan.
  $("#form-origin")?.addEventListener("submit", (ev) => {
    ev.preventDefault();
    doOriginSave();
  });

  $("#set-autostart")?.addEventListener("change", doSetAutostart);

  $("#log-autoscroll")?.addEventListener("change", (ev) => {
    state.autoscroll = ev.target.checked;
  });

  // Tab switching (khusus tampilan panel).
  $("#tabs")?.addEventListener("click", (ev) => {
    const btn = ev.target.closest("[data-tab]");
    if (!btn) return;
    const tab = btn.dataset.tab;
    $("#tabs").querySelectorAll("button").forEach((b) => {
      b.classList.toggle("active", b.dataset.tab === tab);
    });
    document.querySelectorAll(".tab-pane").forEach((p) => {
      p.classList.toggle("active", p.id === `pane-${tab}`);
    });
    hideBanner("#banner");
    hideBanner("#origin-banner");
    hideBanner("#settings-banner");
  });
}

async function reloadConfigData() {
  state.config = await GetConfig();
  state.origins = state.config.origins || [];
  renderAll();
}

// ------------------------------------------------------------------ shell

function renderMainShell() {
  $("#app").innerHTML = `
    <div class="topbar">
      <div class="logo" aria-hidden="true"><img src="/aquila-icon.png" alt=""></div>
      <h1>Aquila</h1>
      <div class="pill"><span class="dot" id="pill-dot"></span><span id="pill-text">…</span></div>
    </div>
    <div class="tabs" id="tabs">
      <button data-tab="beranda" class="active">Beranda</button>
      <button data-tab="origin">Alamat Diizinkan</button>
      <button data-tab="settings">Pengaturan</button>
      <button data-tab="log">Log</button>
      <button data-tab="about">Tentang Aquila</button>
    </div>
    <div class="content">
      <div class="tab-pane active" id="pane-beranda">
        <div class="card">
          <h2>Status Layanan</h2>
          <div class="kv"><span class="k">Status</span><span class="v" id="bd-status">…</span></div>
          <div class="kv"><span class="k">Alamat dengarkan</span><span class="v" id="bd-listen">…</span></div>
          <div class="kv"><span class="k">Kesehatan</span><span class="v" id="bd-health">…</span></div>
          <div class="kv"><span class="k">Aktif selama</span><span class="v" id="bd-uptime">—</span></div>
          <div class="kv"><span class="k">Printer terdeteksi</span><span class="v" id="bd-printers">Memuat…</span></div>
          <div class="kv"><span class="k">File konfigurasi</span><span class="v" id="bd-path">…</span></div>
          <div class="banner" id="service-error" role="alert"></div>
        </div>
        <div class="card">
          <h2>Kendali</h2>
          <div class="banner" id="banner"></div>
          <div class="row" style="margin-bottom:8px">
            <button class="primary" id="btn-toggle" data-action="toggle-service">…</button>
            <button id="btn-restart" data-action="restart-service">Muat Ulang Layanan</button>
            <button id="btn-test" data-action="test-connection">Tes Koneksi</button>
            <button id="btn-diagnose-port" data-action="diagnose-port">Cek Pemakai Port</button>
          </div>
          <div class="row">
            <button data-action="copy-address">Salin Alamat</button>
            <button id="btn-open-url" data-action="open-url">Buka /health di Peramban</button>
            <button data-action="open-folder">Buka Folder Konfigurasi</button>
          </div>
        </div>
        <div class="card">
          <h2>Cara Pakai Singkat</h2>
          <p class="muted">Aplikasi farmasi mengirim dokumen PDF ke print server ini lewat HTTP,
          lalu diteruskan ke antrean printer PC. Pastikan alamat aplikasi farmasi sudah terdaftar di
          tab <b>Alamat Diizinkan</b>. Tombol <b>Tes Koneksi</b> memastikan print server merespons
          sebelum dipakai.</p>
        </div>
      </div>

      <div class="tab-pane" id="pane-origin">
        <div class="card">
          <div class="section-heading"><h2>Daftar Alamat</h2><button data-action="refresh-origins">Muat Ulang</button></div>
          <p class="muted" id="origin-summary"></p>
          <div id="origin-list" style="margin-top:10px"></div>
        </div>
        <div class="card">
          <h2 id="origin-form-title">Tambah Alamat Diizinkan</h2>
          <form id="form-origin">
            <div class="row grow">
              <select id="origin-scheme" aria-label="Skema">
                <option value="https">https</option>
                <option value="http">http</option>
              </select>
              <input id="origin-host" placeholder="contoh: farmasi.example.test atau 192.168.10.25" aria-label="Host" />
              <input id="origin-port" placeholder="port (opsional)" inputmode="numeric" style="width:120px" aria-label="Port" />
            </div>
            <div class="row" style="margin-top:10px">
              <button type="submit" class="primary" id="btn-origin-save" disabled>Tambah</button>
              <button type="button" id="btn-origin-cancel" data-action="origin-cancel" style="display:none">Batal</button>
            </div>
          </form>
          <div class="banner" id="origin-banner" style="margin-top:10px"></div>
        </div>
        <div class="card">
          <h2>Catatan</h2>
          <p class="muted">Alamat asal (origin) adalah skema + host + port aplikasi yang boleh mengakses print server. Contoh:</p>
          <ul class="muted origin-notes">
            <li><code>https://farmasi.example.test</code> — domain atau subdomain</li>
            <li><code>http://localhost:4200</code> — localhost dengan port</li>
            <li><code>http://192.168.10.25:8080</code> — alamat IP dengan port</li>
          </ul>
          <p class="muted">Perubahan berlaku langsung tanpa restart.</p>
        </div>
      </div>

      <div class="tab-pane" id="pane-settings">
        <div class="card">
          <h2>Alamat Dengarkan</h2>
          <div class="row grow">
            <input id="listen-host" placeholder="127.0.0.1" aria-label="Host" />
            <input id="listen-port" placeholder="17891" inputmode="numeric" style="width:110px" aria-label="Port" />
            <button class="primary" data-action="save-listen">Simpan</button>
          </div>
          <p class="muted" style="margin-top:8px">Hanya localhost yang diizinkan agar print server
          tidak bisa diakses dari jaringan lain. Layanan otomatis dimuat ulang bila sedang berjalan.</p>
          <div class="banner" id="settings-banner" style="margin-top:10px"></div>
        </div>
        <div class="card">
          <h2>Preferensi Aplikasi</h2>
          <label class="check">
            <input type="checkbox" id="set-start-service" />
            <div>
              <div class="check-label">Mulai layanan saat aplikasi dibuka</div>
              <div class="check-desc">Print server langsung aktif begitu Aquila berjalan.</div>
            </div>
          </label>
          <label class="check">
            <input type="checkbox" id="set-open-window" />
            <div>
              <div class="check-label">Buka jendela panel saat aplikasi dibuka</div>
              <div class="check-desc">Bila dimatikan, Aquila langsung bersemayam di baki sistem.</div>
            </div>
          </label>
          <label class="check">
            <input type="checkbox" id="set-hide-close" />
            <div>
              <div class="check-label">Sembunyikan ke tray saat jendela ditutup</div>
              <div class="check-desc">Layanan tetap berjalan di latar belakang.</div>
            </div>
          </label>
          <label class="check">
            <input type="checkbox" id="set-notify" />
            <div>
              <div class="check-label">Beri tahu bila terjadi kesalahan</div>
            </div>
          </label>
          <label class="check">
            <input type="checkbox" id="set-confirm-quit" />
            <div>
              <div class="check-label">Konfirmasi sebelum keluar</div>
            </div>
          </label>
          <div class="row" style="margin-top:10px">
            <button class="primary" data-action="save-settings">Simpan Preferensi</button>
          </div>
        </div>
        <div class="card">
          <h2>Startup Sistem</h2>
          <label class="check">
            <input type="checkbox" id="set-autostart" />
            <div>
              <div class="check-label">Jalankan Aquila saat login</div>
              <div class="check-desc">Aquila muncul di baki sistem setiap komputer dinyalakan.</div>
            </div>
          </label>
        </div>
        <div class="card">
          <h2>File Konfigurasi</h2>
          <div class="row">
            <button data-action="import-config">Impor…</button>
            <button data-action="export-config">Ekspor…</button>
            <button data-action="reload-config">Muat Ulang</button>
            <button class="danger" data-action="reset-config">Kembalikan Default</button>
          </div>
          <p class="muted" style="margin-top:10px">Isi print-server.json saat ini:</p>
          <pre class="log-view" id="cfg-json" style="min-height:110px;max-height:200px;margin-top:8px"></pre>
        </div>
        <div class="card">
          <h2>Uninstall Aquila</h2>
          <p class="muted">Hentikan layanan dan hapus aplikasi dari komputer ini. Konfigurasi serta daftar alamat tetap disimpan agar dapat dipakai saat instal ulang.</p>
          <button class="danger" data-action="uninstall" style="margin-top:12px">Uninstall Aquila…</button>
          <div class="banner" id="uninstall-banner" role="status" style="margin-top:10px"></div>
        </div>
      </div>

      <div class="tab-pane" id="pane-about">
        <div class="card about-card">
          <div class="logo about-logo" aria-hidden="true"><img src="/aquila-icon.png" alt=""></div>
          <h2>AQUILA</h2>
          <p class="about-subtitle">Print Server</p>
          <p class="about-version">Versi <span id="cfg-version">…</span></p>
          <div class="about-author">
            <p class="muted">Dikembangkan oleh</p>
            <p class="about-company">CV ANAK BANGSA</p>
            <address>Jl. Ki Jagapati No. 22, RT. 02 RW. 01<br>Kel. Krandegan, Kec. / Kab. Banjarnegara</address>
          </div>
          <p class="about-copyright">© 2026 CV ANAK BANGSA.<br>Seluruh hak cipta dilindungi.</p>
        </div>
      </div>

      <div class="tab-pane" id="pane-log">
        <div class="card" style="display:flex;flex-direction:column;flex:1">
          <h2>Log Layanan</h2>
          <div class="row">
            <button data-action="clear-logs">Bersihkan Log</button>
            <label class="check" style="padding:0">
              <input type="checkbox" id="log-autoscroll" checked />
              <div class="check-label">Gulir otomatis</div>
            </label>
          </div>
          <div class="log-view" id="log-view" style="margin-top:12px"></div>
        </div>
      </div>
    </div>`;
}

function renderPopupShell() {
  $("#app").innerHTML = `
    <div class="popup">
      <div class="status-block" data-wails-drag>
        <div class="big-dot" id="pp-dot"></div>
        <div>
          <div class="status-text" id="pp-status">…</div>
          <div class="muted" id="pp-listen">…</div>
        </div>
      </div>
      <div class="meta">
        <div id="pp-uptime"></div>
        <div id="pp-health"></div>
        <div id="pp-printers"></div>
      </div>
      <div class="actions">
        <button class="primary" id="pp-toggle" data-action="toggle-service">…</button>
        <button data-action="show-panel">Buka Panel</button>
        <button class="danger" data-action="quit">Keluar</button>
      </div>
    </div>`;
}

// ------------------------------------------------------------------- init

async function init() {
  if (isPopup) {
    document.title = "Aquila";
    renderPopupShell();
  } else {
    renderMainShell();
  }
  bindEvents();
  renderOrigins();

  // Event dari Go: hanya perbarui bagian yang berubah, tanpa polling.
  Events.On(AquilaState, (ev) => {
    state.service = ev.data;
    renderStatus();
  });
  Events.On(AquilaLog, (ev) => {
    appendLog(ev.data);
  });

  // Konfigurasi tetap tampil meski pemuatan log atau preferensi gagal.
  await Promise.allSettled([
    reloadConfigData().catch((err) => {
      const list = $("#origin-list");
      if (list) list.textContent = "Daftar alamat belum berhasil dimuat. Klik Muat Ulang untuk mencoba lagi.";
      showBanner("#origin-banner", errorMessage(err), false);
    }),
    GetState().then((svc) => { state.service = svc; renderStatus(); }),
    GetSettings().then((settings) => { state.settings = settings; renderSettings(); }),
    GetLogs().then((logs) => { for (const entry of logs) appendLog(entry); }),
  ]);

  if (!isPopup) {
    try {
      state.autostart = await GetAutostart();
    } catch {
      /* autostart tidak didukung di platform ini */
    }
    if (state.config && state.config.created) {
      showBanner(
        "#banner",
        `File konfigurasi baru dibuat di ${state.config.path}. Periksa tab Alamat Diizinkan.`,
        true
      );
    }
    // Muat jumlah printer di latar belakang (bisa butuh beberapa detik).
    GetPrinterCount()
      .then((n) => {
        state.printers = n;
        renderBeranda();
      })
      .catch(() => {
        state.printers = 0;
        renderBeranda();
      });
    renderSettings();
  } else {
    GetPrinterCount()
      .then((n) => {
        state.printers = n;
        const el = $("#pp-printers");
        if (el) el.textContent = `${n} printer terdeteksi`;
      })
      .catch(() => {});
  }

  renderStatus();
}

init();
