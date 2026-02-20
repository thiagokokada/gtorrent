const bodyEl = document.querySelector("#torrents-body");
const addForm = document.querySelector("#add-form");
const messageEl = document.querySelector("#form-message");
const statusEl = document.querySelector("#status");
const refreshBtn = document.querySelector("#refresh");
const rowTemplate = document.querySelector("#row-template");

const state = {
  torrents: [],
  loading: false,
};

const formatBytes = (bytes) => {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / Math.pow(1024, i);
  return `${value.toFixed(i > 1 ? 1 : 0)} ${units[i]}`;
};

const formatRate = (bytesPerSec) => `${formatBytes(bytesPerSec)}/s`;

const setFormMessage = (text, isError = false) => {
  messageEl.textContent = text;
  messageEl.style.color = isError ? "#9f2b2b" : "#59655c";
};

const setStatus = (text, isError = false) => {
  statusEl.textContent = text;
  statusEl.style.background = isError ? "#f6cece" : "#cde8d8";
  statusEl.style.color = isError ? "#7f2323" : "#154a2f";
};

async function fetchTorrents() {
  if (state.loading) return;
  state.loading = true;
  try {
    const response = await fetch("/api/torrents");
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "Failed to load torrents");
    state.torrents = payload.torrents || [];
    setStatus("Connected");
    render();
  } catch (error) {
    setStatus(`Connection error: ${error.message}`, true);
  } finally {
    state.loading = false;
  }
}

function render() {
  bodyEl.innerHTML = "";
  if (!state.torrents.length) {
    bodyEl.innerHTML = '<tr><td colspan="7" class="placeholder">No torrents yet</td></tr>';
    return;
  }

  for (const torrent of state.torrents) {
    const row = rowTemplate.content.firstElementChild.cloneNode(true);
    row.querySelector(".name").textContent = torrent.name || torrent.hash;
    row.querySelector(".name").title = torrent.hash;
    row.querySelector(".state").textContent = torrent.state || "unknown";
    row.querySelector(".progress").value = Number(torrent.progress || 0);
    row.querySelector(".percent").textContent = `${Math.round(Number(torrent.progress || 0) * 100)}%`;
    row.querySelector(".down").textContent = formatRate(Number(torrent.downRate || 0));
    row.querySelector(".up").textContent = formatRate(Number(torrent.upRate || 0));
    row.querySelector(".size").textContent = `${formatBytes(Number(torrent.doneBytes || 0))} / ${formatBytes(Number(torrent.sizeBytes || 0))}`;

    const removeBtn = row.querySelector(".remove");
    removeBtn.addEventListener("click", async () => {
      if (!confirm(`Remove torrent \"${torrent.name || torrent.hash}\"?`)) return;
      removeBtn.disabled = true;
      try {
        const response = await fetch(`/api/torrents/${encodeURIComponent(torrent.hash)}`, {
          method: "DELETE",
        });
        const payload = await response.json();
        if (!response.ok) throw new Error(payload.error || "Remove failed");
        setFormMessage("Torrent removed");
        await fetchTorrents();
      } catch (error) {
        setFormMessage(error.message, true);
      } finally {
        removeBtn.disabled = false;
      }
    });

    bodyEl.appendChild(row);
  }
}

addForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  setFormMessage("");

  const submitBtn = addForm.querySelector("button[type='submit']");
  submitBtn.disabled = true;

  const magnet = addForm.magnet.value.trim();
  const file = addForm.torrent.files[0];

  try {
    let response;
    if (file) {
      const formData = new FormData();
      if (magnet) formData.set("magnet", magnet);
      formData.set("torrent", file);
      response = await fetch("/api/torrents", { method: "POST", body: formData });
    } else {
      response = await fetch("/api/torrents", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ magnet }),
      });
    }

    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || "Add failed");

    addForm.reset();
    setFormMessage("Torrent added");
    await fetchTorrents();
  } catch (error) {
    setFormMessage(error.message, true);
  } finally {
    submitBtn.disabled = false;
  }
});

refreshBtn.addEventListener("click", () => fetchTorrents());

fetchTorrents();
setInterval(fetchTorrents, 4000);
