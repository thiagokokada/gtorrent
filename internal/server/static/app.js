const bodyEl = document.querySelector("#torrents-body");
const addForm = document.querySelector("#add-form");
const messageEl = document.querySelector("#form-message");
const statusEl = document.querySelector("#status");
const statsEl = document.querySelector("#global-stats");
const refreshBtn = document.querySelector("#refresh");
const removeSelectedBtn = document.querySelector("#remove-selected");
const rowTemplate = document.querySelector("#row-template");
const filterContainer = document.querySelector("#filters");
const sortButtons = document.querySelectorAll(".sort-btn");

const detailsEmptyEl = document.querySelector("#details-empty");
const detailsContentEl = document.querySelector("#details-content");
const detailsNameEl = document.querySelector("#details-name");
const detailsHashEl = document.querySelector("#details-hash");
const detailsStateEl = document.querySelector("#details-state");
const detailsProgressEl = document.querySelector("#details-progress");
const detailsProgressTextEl = document.querySelector("#details-progress-text");
const detailsDownloadedEl = document.querySelector("#details-downloaded");
const detailsSizeEl = document.querySelector("#details-size");
const detailsDownRateEl = document.querySelector("#details-down-rate");
const detailsUpRateEl = document.querySelector("#details-up-rate");

const REFRESH_INTERVAL_MS = 4000;

const state = {
  torrents: [],
  loading: false,
  selectedHash: "",
  filter: "all",
  sortKey: "name",
  sortDir: 1,
};

const formatBytes = (bytes) => {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / Math.pow(1024, i);
  return `${value.toFixed(i > 1 ? 1 : 0)} ${units[i]}`;
};

const formatRate = (bytesPerSec) => `${formatBytes(bytesPerSec)}/s`;

const safeNumber = (value) => {
  const n = Number(value);
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

function updateGlobalStats() {
  const torrents = getVisibleTorrents();
  const down = torrents.reduce((acc, t) => acc + safeNumber(t.downRate), 0);
  const up = torrents.reduce((acc, t) => acc + safeNumber(t.upRate), 0);
  const label = torrents.length === 1 ? "torrent" : "torrents";
  statsEl.textContent = `${torrents.length} ${label} | ↓ ${formatRate(down)} | ↑ ${formatRate(up)}`;
}

function getVisibleTorrents() {
  const filtered = state.filter === "all"
    ? state.torrents
    : state.torrents.filter((torrent) => getNormalizedState(torrent) === state.filter);

  return [...filtered].sort((a, b) => {
    const dir = state.sortDir;
    const key = state.sortKey;
    if (key === "name" || key === "state") {
      const av = String(a[key] || "").toLowerCase();
      const bv = String(b[key] || "").toLowerCase();
      return av.localeCompare(bv) * dir;
    }
    return (safeNumber(a[key]) - safeNumber(b[key])) * dir;
  });
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
    bodyEl.innerHTML = '<tr><td colspan="6" class="placeholder">No torrents in this view</td></tr>';
  } else {
    for (const torrent of torrents) {
      const row = rowTemplate.content.firstElementChild.cloneNode(true);
      const stateText = getNormalizedState(torrent);
      const progress = safeNumber(torrent.progress);

      row.dataset.hash = torrent.hash;
      row.querySelector(".name").textContent = torrent.name || torrent.hash;
      row.querySelector(".name").title = torrent.hash;

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

  removeSelectedBtn.disabled = !state.selectedHash;
  renderDetails();
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
    case "progress": return "Done";
    case "downRate": return "Down";
    case "upRate": return "Up";
    case "sizeBytes": return "Size";
    default: return key;
  }
}

function renderDetails() {
  const selected = state.torrents.find((torrent) => torrent.hash === state.selectedHash);
  if (!selected) {
    detailsEmptyEl.style.display = "block";
    detailsContentEl.classList.add("hidden");
    detailsStateEl.textContent = "None";
    return;
  }

  detailsEmptyEl.style.display = "none";
  detailsContentEl.classList.remove("hidden");

  const stateText = getNormalizedState(selected);
  const progress = safeNumber(selected.progress);

  detailsNameEl.textContent = selected.name || selected.hash;
  detailsHashEl.textContent = selected.hash;
  detailsStateEl.textContent = stateText;
  detailsProgressEl.value = progress;
  detailsProgressTextEl.textContent = `${Math.round(progress * 100)}%`;
  detailsDownloadedEl.textContent = formatBytes(safeNumber(selected.doneBytes));
  detailsSizeEl.textContent = formatBytes(safeNumber(selected.sizeBytes));
  detailsDownRateEl.textContent = formatRate(safeNumber(selected.downRate));
  detailsUpRateEl.textContent = formatRate(safeNumber(selected.upRate));
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

addForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  setFormMessage("");

  const submitBtn = addForm.querySelector("#add-btn");
  submitBtn.disabled = true;

  const magnet = addForm.magnet.value.trim();
  const file = addForm.torrent.files[0];

  if (!magnet && !file) {
    setFormMessage("Provide a magnet link or select a .torrent file", true);
    submitBtn.disabled = false;
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
    setFormMessage("Torrent added");
    await fetchTorrents();
  } catch (error) {
    setFormMessage(error.message, true);
  } finally {
    submitBtn.disabled = false;
  }
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

refreshBtn.addEventListener("click", () => fetchTorrents());
removeSelectedBtn.addEventListener("click", () => removeSelectedTorrent());

fetchTorrents();
setInterval(fetchTorrents, REFRESH_INTERVAL_MS);
