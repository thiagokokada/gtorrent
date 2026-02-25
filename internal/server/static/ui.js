const CONNECTION_STATES = ["online", "offline"];
const FILTER_VALUES = ["all", "downloading", "seeding", "complete", "stopped"];
const SORT_VALUES = ["addedAt", "name", "state", "progress", "etaSeconds", "ratio", "peers", "seeds", "downRate", "upRate", "sizeBytes"];
const STORAGE_KEYS = {
  filter: "gtorrent.view.filter",
  sort: "gtorrent.view.sort",
  dir: "gtorrent.view.dir",
};
const state = {
  connection: "offline",
  dismissedClientError: "",
};

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
    return "Connection error: HTTP " + String(code);
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
    document.cookie = key + "=" + encodedValue + "; Path=/; Max-Age=31536000; SameSite=Lax";
  } catch (_) {
    // Ignore persistence failures.
  }
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
}

initDashboardUi();
