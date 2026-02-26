const CONNECTION_STATES = ["online", "offline"];
const STORAGE_KEYS = {
  filter: "gtorrent.view.filter",
  sort: "gtorrent.view.sort",
  dir: "gtorrent.view.dir",
  visibleColumns: "gtorrent.table.visible-columns.v1",
  columnWidths: "gtorrent.table.columns.v1",
};
const DEFAULT_COLUMN_MIN_WIDTH = 48;
const state = {
  connection: "offline",
  dismissedClientError: "",
  columnResize: {
    active: null,
  },
};

function logUiError(action, error) {
  console.error(`[gtorrent-ui] ${action}`, error);
}

function messageBoxEl() {
  return document.querySelector("#form-message");
}

function messageTextEl() {
  return document.querySelector("#form-message-text");
}

function setConnectionDot(stateValue) {
  const dot = document.querySelector("#connection-dot");
  if (!dot) {
    return;
  }
  for (const value of CONNECTION_STATES) {
    dot.classList.remove(value);
  }
  dot.classList.add(stateValue);
}

function requestErrorMessage(event) {
  const detail = event.detail || {};
  const xhr = detail.xhr;
  if (!xhr) {
    return "Connection error";
  }
  const code = xhr.status || 0;
  if (code > 0) {
    return `Connection error: HTTP ${String(code)}`;
  }
  return "Connection error";
}

function showErrorMessage(text) {
  const box = messageBoxEl();
  const textEl = messageTextEl();
  if (!box || !textEl) {
    return;
  }

  const normalizedText = String(text || "").trim();
  if (normalizedText !== "" && state.dismissedClientError === normalizedText) {
    return;
  }

  textEl.textContent = normalizedText;
  box.setAttribute("data-client-error", "true");
  box.classList.remove("message-info", "message-ok", "message-error");
  if (normalizedText === "") {
    box.classList.add("is-hidden");
  } else {
    box.classList.remove("is-hidden");
  }
  box.classList.add("message-error");
}

function renderConnection() {
  setConnectionDot(state.connection);
}

function setConnectionState(nextState) {
  state.connection = nextState;
  if (nextState === "online") {
    state.dismissedClientError = "";
  }
  renderConnection();
}

function isDashboardTarget(target) {
  return Boolean(target?.closest?.("#dashboard"));
}

function writeCookie(key, value) {
  try {
    const encodedValue = encodeURIComponent(value);
    document.cookie = `${key}=${encodedValue}; Path=/; Max-Age=31536000; SameSite=Lax`;
  } catch (error) {
    logUiError(`failed to persist cookie for key "${key}"`, error);
  }
}

function tableColumnKeys() {
  const keys = [];
  const seen = new Set();

  const checkboxList = document.querySelectorAll('#columns-form input[name="visibleCol"]');
  for (const checkbox of checkboxList) {
    const key = String(checkbox.value || "").trim();
    if (key === "" || seen.has(key)) {
      continue;
    }
    seen.add(key);
    keys.push(key);
  }

  if (keys.length > 0) {
    return keys;
  }

  const tableCols = document.querySelectorAll("#torrent-table colgroup col[data-col]");
  for (const col of tableCols) {
    const key = String(col.dataset.col || "").trim();
    if (key === "" || seen.has(key)) {
      continue;
    }
    seen.add(key);
    keys.push(key);
  }

  return keys;
}

function normalizeVisibleColumnsValue(raw) {
  const columnKeys = tableColumnKeys();
  if (columnKeys.length === 0) {
    return "";
  }

  const seen = new Set();
  const visible = [];
  const value = String(raw || "").trim();
  if (value !== "") {
    const tokens = value.split(",");
    for (const token of tokens) {
      const key = String(token || "").trim();
      if (!columnKeys.includes(key) || seen.has(key)) {
        continue;
      }
      seen.add(key);
      visible.push(key);
    }
  }

  if (visible.length === 0 || visible.length === columnKeys.length) {
    return "";
  }
  return columnKeys.filter(function (key) {
    return seen.has(key);
  }).join(",");
}

function visibleColumnsFromValue(raw) {
  const normalized = normalizeVisibleColumnsValue(raw);
  const columnKeys = tableColumnKeys();
  if (normalized === "") {
    return new Set(columnKeys);
  }
  return new Set(normalized.split(","));
}

function readStoredVisibleColumns() {
  try {
    const raw = localStorage.getItem(STORAGE_KEYS.visibleColumns);
    if (raw === null) {
      return null;
    }
    return normalizeVisibleColumnsValue(raw);
  } catch (error) {
    logUiError(`failed to read visible columns from localStorage key "${STORAGE_KEYS.visibleColumns}"`, error);
    return null;
  }
}

function writeStoredVisibleColumns(value) {
  try {
    const normalized = normalizeVisibleColumnsValue(value);
    if (normalized === "") {
      localStorage.removeItem(STORAGE_KEYS.visibleColumns);
      return;
    }
    localStorage.setItem(STORAGE_KEYS.visibleColumns, normalized);
  } catch (error) {
    logUiError(`failed to write visible columns to localStorage key "${STORAGE_KEYS.visibleColumns}"`, error);
  }
}

function columnsInputEl() {
  return document.querySelector("#cols-input");
}

function viewStateValues() {
  const form = document.querySelector("#view-state");
  if (!form) {
    return {};
  }
  const values = {};
  const formData = new FormData(form);
  for (const [key, value] of formData.entries()) {
    values[String(key)] = String(value);
  }
  return values;
}

function refreshDashboardFromViewState() {
  const dashboard = document.querySelector("#dashboard");
  if (!dashboard || typeof window.htmx === "undefined") {
    return;
  }
  window.htmx.ajax("GET", "/ui/dashboard", {
    target: "#dashboard",
    swap: "outerHTML",
    values: viewStateValues(),
  });
}

function syncColumnsDialogFromInputValue(value) {
  const form = document.querySelector("#columns-form");
  if (!form) {
    return;
  }
  const visible = visibleColumnsFromValue(value);
  const checkboxes = form.querySelectorAll('input[name="visibleCol"]');
  for (const checkbox of checkboxes) {
    checkbox.checked = visible.has(String(checkbox.value || ""));
  }
}

function syncStoredVisibleColumnsToViewState() {
  const colsInput = columnsInputEl();
  if (!colsInput) {
    return;
  }
  const current = normalizeVisibleColumnsValue(colsInput.value);
  const stored = readStoredVisibleColumns();
  if (stored === null) {
    colsInput.value = current;
    syncColumnsDialogFromInputValue(current);
    return;
  }
  if (stored !== current) {
    colsInput.value = stored;
    syncColumnsDialogFromInputValue(stored);
    refreshDashboardFromViewState();
    return;
  }
  syncColumnsDialogFromInputValue(current);
}

function readStoredColumnWidths() {
  try {
    const raw = localStorage.getItem(STORAGE_KEYS.columnWidths);
    if (!raw) {
      return {};
    }

    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object") {
      return {};
    }

    const widths = {};
    for (const [name, value] of Object.entries(parsed)) {
      const width = Number(value);
      if (Number.isFinite(width) && width > 0) {
        widths[name] = Math.round(width);
      }
    }
    return widths;
  } catch (error) {
    logUiError(`failed to read column widths from localStorage key "${STORAGE_KEYS.columnWidths}"`, error);
    return {};
  }
}

function writeStoredColumnWidths(widths) {
  try {
    localStorage.setItem(STORAGE_KEYS.columnWidths, JSON.stringify(widths));
  } catch (error) {
    logUiError(`failed to write column widths to localStorage key "${STORAGE_KEYS.columnWidths}"`, error);
  }
}

function setColumnWidthPx(col, width) {
  col.style.width = `${Math.round(width)}px`;
}

function applyStoredColumnWidths() {
  const table = document.querySelector("#torrent-table");
  if (!table) {
    return;
  }

  const widths = readStoredColumnWidths();
  if (Object.keys(widths).length === 0) {
    return;
  }

  const cols = table.querySelectorAll("colgroup col[data-col]");
  for (const col of cols) {
    const key = String(col.dataset.col || "");
    const width = widths[key];
    if (typeof width === "number") {
      setColumnWidthPx(col, width);
    }
  }
}

function ensureColumnResizers() {
  const table = document.querySelector("#torrent-table");
  if (!table) {
    return;
  }

  applyStoredColumnWidths();
  const headers = table.querySelectorAll("thead th[data-col]");
  for (const header of headers) {
    if (header.querySelector(".col-resizer")) {
      continue;
    }
    const handle = document.createElement("span");
    handle.className = "col-resizer";
    handle.setAttribute("aria-hidden", "true");
    header.appendChild(handle);
  }
}

function activeColumnResize() {
  return state.columnResize.active;
}

function clearActiveColumnResize() {
  const active = activeColumnResize();
  if (!active) {
    return;
  }
  active.handle.classList.remove("active");
  state.columnResize.active = null;
}

function columnMinWidth(header, startWidth) {
  const minWidth = Number(header.dataset.minWidth);
  if (Number.isFinite(minWidth) && minWidth > 0) {
    return Math.round(minWidth);
  }

  return Math.max(DEFAULT_COLUMN_MIN_WIDTH, Math.round(startWidth));
}

function onColumnResizeStart(event) {
  const handle = event.target?.closest?.(".col-resizer");
  if (!handle) {
    return;
  }

  const header = handle.closest("th[data-col]");
  const table = handle.closest("table");
  if (!header || !table) {
    return;
  }

  const colName = String(header.dataset.col || "");
  if (colName === "") {
    return;
  }
  const col = table.querySelector(`colgroup col[data-col="${colName}"]`);
  if (!col) {
    return;
  }

  event.preventDefault();
  event.stopPropagation();

  clearActiveColumnResize();

  const startWidth = header.getBoundingClientRect().width;
  state.columnResize.active = {
    pointerId: event.pointerId,
    colName: colName,
    col: col,
    handle: handle,
    startX: event.clientX,
    startWidth: startWidth,
    minWidth: columnMinWidth(header, startWidth),
    width: startWidth,
  };
  handle.classList.add("active");
  handle.setPointerCapture(event.pointerId);
}

function onColumnResizeMove(event) {
  const active = activeColumnResize();
  if (!active || active.pointerId !== event.pointerId) {
    return;
  }

  const delta = event.clientX - active.startX;
  const nextWidth = Math.max(active.minWidth, Math.round(active.startWidth + delta));
  setColumnWidthPx(active.col, nextWidth);
  active.width = nextWidth;
}

function onColumnResizeEnd(event) {
  const active = activeColumnResize();
  if (!active || active.pointerId !== event.pointerId) {
    return;
  }

  try {
    active.handle.releasePointerCapture(active.pointerId);
  } catch (error) {
    logUiError(`failed to release pointer capture for column "${active.colName}"`, error);
  }

  const widths = readStoredColumnWidths();
  widths[active.colName] = active.width;
  writeStoredColumnWidths(widths);
  clearActiveColumnResize();
}

function persistCurrentViewState() {
  const filter = String(document.querySelector("#filter-input")?.value || "").trim();
  const sort = String(document.querySelector("#sort-input")?.value || "").trim();
  const dir = String(document.querySelector("#dir-input")?.value || "").trim();
  const colsInput = columnsInputEl();
  const cols = normalizeVisibleColumnsValue(colsInput?.value || "");

  if (colsInput) {
    colsInput.value = cols;
  }

  writeCookie(STORAGE_KEYS.filter, filter);
  writeCookie(STORAGE_KEYS.sort, sort);
  writeCookie(STORAGE_KEYS.dir, dir);
  writeStoredVisibleColumns(cols);
}

function initDashboardUi() {
  document.body.addEventListener("htmx:sseOpen", function (event) {
    if (isDashboardTarget(event.target)) {
      setConnectionState("online");
    }
  });

  document.body.addEventListener("htmx:sseError", function (event) {
    if (isDashboardTarget(event.target)) {
      setConnectionState("offline");
      showErrorMessage("Connection error: SSE stream disconnected");
    }
  });

  document.body.addEventListener("htmx:responseError", function (event) {
    const target = event.detail?.target;
    if (isDashboardTarget(target)) {
      setConnectionState("offline");
      showErrorMessage(requestErrorMessage(event));
    }
  });

  document.body.addEventListener("htmx:afterSwap", function (event) {
    const target = event.detail?.target;
    if (target?.id === "dashboard") {
      renderConnection();
      syncStoredVisibleColumnsToViewState();
      ensureColumnResizers();
    }
    if (target?.id === "controls-panel") {
      syncStoredVisibleColumnsToViewState();
    }
    if (target?.id === "file-list") {
      ensureColumnResizers();
    }
  });

  document.body.addEventListener("htmx:afterSettle", function (event) {
    if (isDashboardTarget(event.detail?.target)) {
      persistCurrentViewState();
    }
  });

  document.body.addEventListener("click", function (event) {
    const closeButton = event.target?.closest?.("#form-message-close");
    if (!closeButton) {
      return;
    }

    const box = messageBoxEl();
    const textEl = messageTextEl();
    if (!box || !textEl) {
      return;
    }
    if (box.getAttribute("data-client-error") !== "true") {
      return;
    }

    event.preventDefault();
    state.dismissedClientError = String(textEl.textContent || "").trim();
    textEl.textContent = "";
    box.classList.add("is-hidden");
    box.removeAttribute("data-client-error");
  });

  document.body.addEventListener("submit", function (event) {
    const form = event.target;
    if (!form || form.id !== "columns-form") {
      return;
    }

    const selected = [];
    const checkboxes = form.querySelectorAll('input[name="visibleCol"]:checked');
    for (const checkbox of checkboxes) {
      selected.push(String(checkbox.value || ""));
    }

    const encoded = normalizeVisibleColumnsValue(selected.join(","));
    const colsInput = columnsInputEl();
    if (colsInput) {
      colsInput.value = encoded;
    }
    writeStoredVisibleColumns(encoded);
  });

  document.body.addEventListener("pointerdown", onColumnResizeStart);
  document.body.addEventListener("pointermove", onColumnResizeMove);
  document.body.addEventListener("pointerup", onColumnResizeEnd);
  document.body.addEventListener("pointercancel", onColumnResizeEnd);

  syncStoredVisibleColumnsToViewState();
  ensureColumnResizers();
}

initDashboardUi();
