const CONNECTION_STATES = ["online", "offline"];
const FILTER_VALUES = ["all", "downloading", "seeding", "complete", "stopped"];
const SORT_VALUES = ["addedAt", "name", "state", "progress", "etaSeconds", "ratio", "peers", "seeds", "downRate", "upRate", "sizeBytes"];
const STORAGE_KEYS = {
  filter: "gtorrent.view.filter",
  sort: "gtorrent.view.sort",
  dir: "gtorrent.view.dir",
  columns: "gtorrent.table.columns.v1",
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

function readStoredColumnWidths() {
  try {
    const raw = localStorage.getItem(STORAGE_KEYS.columns);
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
    logUiError(`failed to read column widths from localStorage key "${STORAGE_KEYS.columns}"`, error);
    return {};
  }
}

function writeStoredColumnWidths(widths) {
  try {
    localStorage.setItem(STORAGE_KEYS.columns, JSON.stringify(widths));
  } catch (error) {
    logUiError(`failed to write column widths to localStorage key "${STORAGE_KEYS.columns}"`, error);
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

  if (FILTER_VALUES.includes(filter)) {
    writeCookie(STORAGE_KEYS.filter, filter);
  }
  if (SORT_VALUES.includes(sort)) {
    writeCookie(STORAGE_KEYS.sort, sort);
  }
  if (dir === "asc" || dir === "desc") {
    writeCookie(STORAGE_KEYS.dir, dir);
  }
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
      ensureColumnResizers();
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

  document.body.addEventListener("pointerdown", onColumnResizeStart);
  document.body.addEventListener("pointermove", onColumnResizeMove);
  document.body.addEventListener("pointerup", onColumnResizeEnd);
  document.body.addEventListener("pointercancel", onColumnResizeEnd);

  ensureColumnResizers();
}

initDashboardUi();
