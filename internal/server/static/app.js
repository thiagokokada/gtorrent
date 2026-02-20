const bodyEl = document.querySelector("#torrents-body");
const tableEl = document.querySelector("#torrent-table");
const messageEl = document.querySelector("#form-message");
const statusEl = document.querySelector("#status");
const statsEl = document.querySelector("#global-stats");
const refreshBtn = document.querySelector("#refresh");
const openAddBtn = document.querySelector("#open-add");
const toggleSelectedBtn = document.querySelector("#toggle-selected");
const removeSelectedBtn = document.querySelector("#remove-selected");
const rowTemplate = document.querySelector("#row-template");
const filterContainer = document.querySelector("#filters");
const sortButtons = document.querySelectorAll(".sort-btn");

const addDialog = document.querySelector("#add-dialog");
const addForm = document.querySelector("#add-form");
const cancelAddBtn = document.querySelector("#cancel-add");
const addBtn = document.querySelector("#add-btn");

const REFRESH_INTERVAL_MS = 4000;
const COLUMN_STORAGE_KEY = "gtorrent.column.widths.v1";
const COLUMN_ORDER = [
  "name",
  "state",
  "addedAt",
  "progress",
  "etaSeconds",
  "ratio",
  "peers",
  "seeds",
  "downRate",
  "upRate",
  "sizeBytes",
];
const COLUMN_DEFAULT_WIDTHS = {
  name: 340,
  state: 96,
  addedAt: 144,
  progress: 152,
  etaSeconds: 88,
  ratio: 78,
  peers: 72,
  seeds: 72,
  downRate: 102,
  upRate: 102,
  sizeBytes: 176,
};
const COLUMN_MIN_WIDTHS = {
  name: 180,
  state: 80,
  addedAt: 110,
  progress: 110,
  etaSeconds: 72,
  ratio: 60,
  peers: 56,
  seeds: 56,
  downRate: 78,
  upRate: 78,
  sizeBytes: 120,
};

const columnEls = new Map(
  Array.from(tableEl.querySelectorAll("colgroup col[data-col]"), (col) => [col.dataset.col, col]),
);
const columnWidths = {};

let resizeState = null;

const state = {
  torrents: [],
  loading: false,
  selectedHash: "",
  filter: "all",
  sortKey: "addedAt",
  sortDir: -1,
};

const formatBytes = (bytes) => {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / Math.pow(1024, i);
  return `${value.toFixed(i > 1 ? 1 : 0)} ${units[i]}`;
};

const formatRate = (bytesPerSec) => `${formatBytes(bytesPerSec)}/s`;

const formatRatio = (ratio) => {
  const n = Number(ratio);
  if (!Number.isFinite(n) || n < 0) return "0.00";
  return n.toFixed(2);
};

const formatETA = (seconds) => {
  const n = Number(seconds);
  if (!Number.isFinite(n) || n < 0) return "∞";
  if (n === 0) return "Done";

  const d = Math.floor(n / 86400);
  const h = Math.floor((n % 86400) / 3600);
  const m = Math.floor((n % 3600) / 60);
  const s = Math.floor(n % 60);

  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
};

const formatAdded = (value) => {
  if (!value) return "-";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "-";
  const date = d.toLocaleDateString(undefined, { year: "numeric", month: "2-digit", day: "2-digit" });
  const time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  return `${date} ${time}`;
};

const safeNumber = (value) => {
  const n = Number(value);
  return Number.isFinite(n) ? n : 0;
};

const parseAdded = (value) => {
  if (!value) return 0;
  const n = Date.parse(value);
  return Number.isFinite(n) ? n : 0;
};

const getNormalizedState = (torrent) => {
  const raw = String(torrent.state || "unknown").toLowerCase();
  if (["downloading", "seeding", "complete", "stopped"].includes(raw)) {
    return raw;
  }
  return "unknown";
};

const setFormMessage = (text, isError = false) => {
  messageEl.textContent = text;
  messageEl.style.color = isError ? "var(--error-text)" : "var(--muted)";
};

const setStatus = (text, isError = false) => {
  statusEl.textContent = text;
  statusEl.style.background = isError ? "var(--error-bg)" : "var(--success-bg)";
  statusEl.style.color = isError ? "var(--error-text)" : "var(--ok-text)";
};

const parseJSON = async (response) => {
  try {
    return await response.json();
  } catch {
    return {};
  }
};

function clampColumnWidth(key, width) {
  const min = COLUMN_MIN_WIDTHS[key] || 56;
  const next = Number(width);
  if (!Number.isFinite(next)) return min;
  return Math.max(min, Math.round(next));
}

function persistColumnWidths() {
  try {
    localStorage.setItem(COLUMN_STORAGE_KEY, JSON.stringify(columnWidths));
  } catch {
    // Ignore storage failures (private mode or blocked storage).
  }
}

function loadColumnWidths() {
  try {
    const raw = localStorage.getItem(COLUMN_STORAGE_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object") return {};
    return parsed;
  } catch {
    return {};
  }
}

function setColumnWidth(key, width, persist = false) {
  const col = columnEls.get(key);
  if (!col) return;

  const clamped = clampColumnWidth(key, width);
  columnWidths[key] = clamped;
  col.style.width = `${clamped}px`;

  if (persist) {
    persistColumnWidths();
  }
}

function syncTableWidth() {
  const wrap = tableEl.closest(".table-wrap");
  const total = COLUMN_ORDER.reduce((sum, key) => sum + (columnWidths[key] || COLUMN_DEFAULT_WIDTHS[key] || 0), 0);
  const wrapWidth = wrap ? wrap.clientWidth : total;
  tableEl.style.width = `${Math.max(total, wrapWidth)}px`;
}

function startColumnResize(event, key, thEl, handleEl) {
  if (event.button !== undefined && event.button !== 0) return;
  event.preventDefault();
  event.stopPropagation();

  const widthFromCol = parseFloat(columnEls.get(key)?.style.width || "");
  const startWidth = Number.isFinite(widthFromCol) ? widthFromCol : thEl.getBoundingClientRect().width;

  resizeState = {
    key,
    startX: event.clientX,
    startWidth,
    handleEl,
  };

  handleEl.classList.add("active");
  document.body.classList.add("col-resizing");
}

function onColumnResizeMove(event) {
  if (!resizeState) return;
  const delta = event.clientX - resizeState.startX;
  const nextWidth = resizeState.startWidth + delta;
  setColumnWidth(resizeState.key, nextWidth, false);
  syncTableWidth();
}

function onColumnResizeEnd() {
  if (!resizeState) return;
  resizeState.handleEl.classList.remove("active");
  resizeState = null;
  document.body.classList.remove("col-resizing");
  persistColumnWidths();
}

function initializeResizableColumns() {
  const saved = loadColumnWidths();

  for (const key of COLUMN_ORDER) {
    const configured = saved[key] ?? COLUMN_DEFAULT_WIDTHS[key];
    setColumnWidth(key, configured, false);
  }
  syncTableWidth();

  const thEls = tableEl.querySelectorAll("thead th[data-col]");
  for (const thEl of thEls) {
    if (thEl.querySelector(".col-resizer")) continue;
    const key = thEl.dataset.col;
    const handleEl = document.createElement("span");
    handleEl.className = "col-resizer";
    handleEl.setAttribute("role", "separator");
    handleEl.setAttribute("aria-orientation", "vertical");
    handleEl.setAttribute("aria-label", `Resize ${keyLabel(key)} column`);
    handleEl.addEventListener("pointerdown", (event) => startColumnResize(event, key, thEl, handleEl));
    thEl.appendChild(handleEl);
  }
}

function updateGlobalStats() {
  const torrents = getVisibleTorrents();
  const down = torrents.reduce((acc, t) => acc + safeNumber(t.downRate), 0);
  const up = torrents.reduce((acc, t) => acc + safeNumber(t.upRate), 0);
  const label = torrents.length === 1 ? "torrent" : "torrents";
  statsEl.textContent = `${torrents.length} ${label} | ↓ ${formatRate(down)} | ↑ ${formatRate(up)}`;
}

function getSortValue(torrent, key) {
  switch (key) {
    case "name":
    case "state":
      return String(torrent[key] || "").toLowerCase();
    case "addedAt":
      return parseAdded(torrent.addedAt);
    case "etaSeconds": {
      const eta = safeNumber(torrent.etaSeconds);
      return eta < 0 ? Number.MAX_SAFE_INTEGER : eta;
    }
    case "ratio":
      return safeNumber(torrent.ratio);
    default:
      return safeNumber(torrent[key]);
  }
}

function getVisibleTorrents() {
  const filtered = state.filter === "all"
    ? state.torrents
    : state.torrents.filter((torrent) => getNormalizedState(torrent) === state.filter);

  return [...filtered].sort((a, b) => {
    const dir = state.sortDir;
    const key = state.sortKey;
    const av = getSortValue(a, key);
    const bv = getSortValue(b, key);

    if (typeof av === "string" && typeof bv === "string") {
      return av.localeCompare(bv) * dir;
    }
    return (av - bv) * dir;
  });
}

function canStopTorrent(torrent) {
  const torrentState = getNormalizedState(torrent);
  return torrentState === "downloading" || torrentState === "seeding";
}

function updateSelectedControls() {
  const selected = state.torrents.find((torrent) => torrent.hash === state.selectedHash);
  if (!selected) {
    toggleSelectedBtn.disabled = true;
    toggleSelectedBtn.textContent = "Start";
    removeSelectedBtn.disabled = true;
    return;
  }

  toggleSelectedBtn.disabled = false;
  toggleSelectedBtn.textContent = canStopTorrent(selected) ? "Stop" : "Start";
  removeSelectedBtn.disabled = false;
}

async function fetchTorrents() {
  if (state.loading) return;
  state.loading = true;
  try {
    const response = await fetch("/api/torrents");
    const payload = await parseJSON(response);
    if (!response.ok) {
      throw new Error(payload.error || "Failed to load torrents");
    }

    state.torrents = Array.isArray(payload.torrents) ? payload.torrents : [];

    if (state.selectedHash && !state.torrents.find((item) => item.hash === state.selectedHash)) {
      state.selectedHash = "";
    }

    setStatus("Connected");
    render();
  } catch (error) {
    setStatus(`Connection error: ${error.message}`, true);
  } finally {
    state.loading = false;
  }
}

function render() {
  const torrents = getVisibleTorrents();
  bodyEl.innerHTML = "";

  if (!torrents.length) {
    bodyEl.innerHTML = '<tr><td colspan="11" class="placeholder">No torrents in this view</td></tr>';
  } else {
    for (const torrent of torrents) {
      const row = rowTemplate.content.firstElementChild.cloneNode(true);
      const stateText = getNormalizedState(torrent);
      const progress = safeNumber(torrent.progress);

      row.dataset.hash = torrent.hash;
      row.querySelector(".name").textContent = torrent.name || torrent.hash;
      row.querySelector(".name").title = torrent.hash;
      row.querySelector(".added").textContent = formatAdded(torrent.addedAt);
      row.querySelector(".eta").textContent = formatETA(torrent.etaSeconds);
      row.querySelector(".ratio").textContent = formatRatio(torrent.ratio);
      row.querySelector(".peers").textContent = String(safeNumber(torrent.peers));
      row.querySelector(".seeds").textContent = String(safeNumber(torrent.seeds));

      const stateEl = row.querySelector(".state");
      stateEl.textContent = stateText;
      stateEl.classList.add(stateText);

      row.querySelector(".progress").value = progress;
      row.querySelector(".percent").textContent = `${Math.round(progress * 100)}%`;
      row.querySelector(".down").textContent = formatRate(safeNumber(torrent.downRate));
      row.querySelector(".up").textContent = formatRate(safeNumber(torrent.upRate));
      row.querySelector(".size").textContent = `${formatBytes(safeNumber(torrent.doneBytes))} / ${formatBytes(safeNumber(torrent.sizeBytes))}`;

      if (torrent.hash === state.selectedHash) {
        row.classList.add("active");
      }

      row.addEventListener("click", () => {
        state.selectedHash = torrent.hash;
        render();
      });

      bodyEl.appendChild(row);
    }
  }

  updateSelectedControls();
  updateGlobalStats();
  renderSortHeaders();
}

function renderSortHeaders() {
  for (const button of sortButtons) {
    const key = button.dataset.sort;
    if (key === state.sortKey) {
      button.textContent = `${keyLabel(key)} ${state.sortDir === 1 ? "▲" : "▼"}`;
    } else {
      button.textContent = keyLabel(key);
    }
  }
}

function keyLabel(key) {
  switch (key) {
    case "name": return "Name";
    case "state": return "State";
    case "addedAt": return "Added";
    case "progress": return "Done";
    case "etaSeconds": return "ETA";
    case "ratio": return "Ratio";
    case "peers": return "Peers";
    case "seeds": return "Seeds";
    case "downRate": return "Down";
    case "upRate": return "Up";
    case "sizeBytes": return "Size";
    default: return key;
  }
}

async function removeSelectedTorrent() {
  const selected = state.torrents.find((torrent) => torrent.hash === state.selectedHash);
  if (!selected) return;

  if (!confirm(`Remove torrent \"${selected.name || selected.hash}\"?`)) {
    return;
  }

  removeSelectedBtn.disabled = true;
  try {
    const response = await fetch(`/api/torrents/${encodeURIComponent(selected.hash)}`, {
      method: "DELETE",
    });
    const payload = await parseJSON(response);
    if (!response.ok) {
      throw new Error(payload.error || "Remove failed");
    }
    setFormMessage("Torrent removed");
    state.selectedHash = "";
    await fetchTorrents();
  } catch (error) {
    setFormMessage(error.message, true);
    removeSelectedBtn.disabled = false;
  }
}

async function toggleSelectedTorrent() {
  const selected = state.torrents.find((torrent) => torrent.hash === state.selectedHash);
  if (!selected) return;

  const stop = canStopTorrent(selected);
  const action = stop ? "stop" : "start";
  const verb = stop ? "stopped" : "started";

  toggleSelectedBtn.disabled = true;
  try {
    const response = await fetch(`/api/torrents/${encodeURIComponent(selected.hash)}/${action}`, {
      method: "POST",
    });
    const payload = await parseJSON(response);
    if (!response.ok) {
      throw new Error(payload.error || `${action} failed`);
    }
    setFormMessage(`Torrent ${verb}`);
    await fetchTorrents();
  } catch (error) {
    setFormMessage(error.message, true);
    updateSelectedControls();
  }
}

function openAddDialog() {
  if (typeof addDialog.showModal === "function") {
    addDialog.showModal();
  } else {
    addDialog.setAttribute("open", "open");
  }
}

function closeAddDialog() {
  if (typeof addDialog.close === "function") {
    addDialog.close();
  } else {
    addDialog.removeAttribute("open");
  }
}

addForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  setFormMessage("");

  addBtn.disabled = true;

  const magnet = addForm.magnet.value.trim();
  const file = addForm.torrent.files[0];

  if (!magnet && !file) {
    setFormMessage("Provide a magnet link or select a .torrent file", true);
    addBtn.disabled = false;
    return;
  }

  try {
    let response;
    if (file) {
      const formData = new FormData();
      if (magnet) {
        formData.set("magnet", magnet);
      }
      formData.set("torrent", file);
      response = await fetch("/api/torrents", {
        method: "POST",
        body: formData,
      });
    } else {
      response = await fetch("/api/torrents", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ magnet }),
      });
    }

    const payload = await parseJSON(response);
    if (!response.ok) {
      throw new Error(payload.error || "Add failed");
    }

    addForm.reset();
    closeAddDialog();
    setFormMessage("Torrent added");
    await fetchTorrents();
  } catch (error) {
    setFormMessage(error.message, true);
  } finally {
    addBtn.disabled = false;
  }
});

cancelAddBtn.addEventListener("click", () => {
  closeAddDialog();
});

openAddBtn.addEventListener("click", () => {
  openAddDialog();
});

filterContainer.addEventListener("click", (event) => {
  const button = event.target.closest("button[data-filter]");
  if (!button) {
    return;
  }

  const filter = button.dataset.filter;
  state.filter = filter;

  for (const element of filterContainer.querySelectorAll("button[data-filter]")) {
    element.classList.toggle("active", element.dataset.filter === filter);
  }

  render();
});

for (const button of sortButtons) {
  button.addEventListener("click", () => {
    const key = button.dataset.sort;
    if (state.sortKey === key) {
      state.sortDir *= -1;
    } else {
      state.sortKey = key;
      state.sortDir = key === "name" || key === "state" ? 1 : -1;
    }
    render();
  });
}

window.addEventListener("pointermove", onColumnResizeMove);
window.addEventListener("pointerup", onColumnResizeEnd);
window.addEventListener("pointercancel", onColumnResizeEnd);
window.addEventListener("blur", onColumnResizeEnd);
window.addEventListener("resize", syncTableWidth);

refreshBtn.addEventListener("click", () => fetchTorrents());
toggleSelectedBtn.addEventListener("click", () => toggleSelectedTorrent());
removeSelectedBtn.addEventListener("click", () => removeSelectedTorrent());

initializeResizableColumns();
fetchTorrents();
setInterval(fetchTorrents, REFRESH_INTERVAL_MS);
