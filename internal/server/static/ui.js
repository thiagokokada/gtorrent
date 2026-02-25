(function () {
  const CONNECTION_STATES = ["connecting", "online", "offline"];

  let connectionState = "connecting";
  let connectionError = "";
  let backendStatusKind = "";
  let backendStatusMessage = "";
  let currentMessageToken = "";
  let dismissedMessageToken = "";
  let transientMessage = null;
  let transientTimer = 0;
  let messageHideTimer = 0;
  let backendStatusStream = null;

  function inputEl(id) {
    return document.querySelector("#" + id);
  }

  function messageBoxEl() {
    return document.querySelector("#form-message");
  }

  function messageTextEl() {
    return document.querySelector("#form-message-text");
  }

  function selectedHash() {
    const input = inputEl("selected-input");
    return input ? input.value.trim() : "";
  }

  function hideMessageBox() {
    const box = messageBoxEl();
    if (!box) {
      return;
    }
    box.classList.add("is-hidden");
  }

  function setConnectionDot(state) {
    const dot = document.querySelector("#connection-dot");
    if (!dot) {
      return;
    }
    for (const value of CONNECTION_STATES) {
      dot.classList.remove(value);
    }
    dot.classList.add(state);
  }

  function parseStatusPayload(raw) {
    if (!raw || typeof raw !== "string") {
      return { kind: "", message: "" };
    }
    try {
      const parsed = JSON.parse(raw);
      return {
        kind: typeof parsed.kind === "string" ? parsed.kind : "",
        message: typeof parsed.message === "string" ? parsed.message : "",
      };
    } catch (_err) {
      return { kind: "", message: "" };
    }
  }

  function clearTransientMessage() {
    transientMessage = null;
    if (transientTimer !== 0) {
      window.clearTimeout(transientTimer);
      transientTimer = 0;
    }
  }

  function setTransientMessage(text, kind, durationMs) {
    if (!text) {
      return;
    }
    clearTransientMessage();
    transientMessage = {
      text,
      kind: kind || "info",
    };
    if (transientMessage.kind !== "error") {
      transientTimer = window.setTimeout(function () {
        clearTransientMessage();
        renderStatusMessage();
      }, durationMs || 4500);
    }
    dismissedMessageToken = "";
    renderStatusMessage();
  }

  function clearMessageHideTimer() {
    if (messageHideTimer !== 0) {
      window.clearTimeout(messageHideTimer);
      messageHideTimer = 0;
    }
  }

  function scheduleMessageHide(token, durationMs) {
    clearMessageHideTimer();
    if (!durationMs || durationMs <= 0) {
      return;
    }
    messageHideTimer = window.setTimeout(function () {
      if (currentMessageToken !== token) {
        return;
      }
      dismissedMessageToken = token;
      hideMessageBox();
    }, durationMs);
  }

  function setBackendStatus(kind, message) {
    const nextKind = kind === "ok" || kind === "error" ? kind : "";
    const nextMessage = (message || "").trim();

    if (backendStatusKind === nextKind && backendStatusMessage === nextMessage) {
      return;
    }
    backendStatusKind = nextKind;
    backendStatusMessage = nextMessage;
    dismissedMessageToken = "";
    renderStatusMessage();
  }

  function syncBackendFromStats() {
    const stats = document.querySelector("#global-stats");
    const table = document.querySelector("#torrents-body");

    const kind = ((table && table.dataset.backendStatusKind) || (stats && stats.dataset.backendStatusKind) || "").trim();
    const message = ((table && table.dataset.backendStatusMessage) || (stats && stats.dataset.backendStatusMessage) || "").trim();
    setBackendStatus(kind, message);
  }

  function backendStatusFromHTML(html) {
    if (!html || typeof html !== "string") {
      return { kind: "", message: "" };
    }

    const tpl = document.createElement("template");
    tpl.innerHTML = html.trim();
    const source = tpl.content.querySelector("#torrents-body, #global-stats");
    if (!source) {
      return { kind: "", message: "" };
    }

    const kind = (
      source.dataset.backendStatusKind ||
      ""
    ).trim();
    const message = (
      source.dataset.backendStatusMessage ||
      ""
    ).trim();
    return { kind, message };
  }

  function setConnectionState(state, err) {
    connectionState = state;
    connectionError = state === "offline" ? (err || "Connection error") : "";
    if (state !== "offline") {
      dismissedMessageToken = "";
    }
    setConnectionDot(connectionState);
    renderStatusMessage();
  }

  function resolveStatusMessage() {
    if (connectionState === "offline") {
      const text = connectionError || "Connection error";
      return {
        text,
        kind: "error",
        token: "conn:offline:" + text,
        autoHideMs: 0,
      };
    }

    if (backendStatusKind === "error" && backendStatusMessage !== "") {
      return {
        text: backendStatusMessage,
        kind: "error",
        token: "backend:error:" + backendStatusMessage,
        autoHideMs: 0,
      };
    }

    if (transientMessage) {
      return {
        text: transientMessage.text,
        kind: transientMessage.kind,
        token: "transient:" + transientMessage.kind + ":" + transientMessage.text,
        autoHideMs: transientMessage.kind === "error" ? 0 : 4500,
      };
    }

    if (backendStatusKind === "ok" && backendStatusMessage !== "") {
      return {
        text: backendStatusMessage,
        kind: "ok",
        token: "backend:ok:" + backendStatusMessage,
        autoHideMs: 4000,
      };
    }

    if (connectionState === "connecting") {
      return {
        text: "Connecting...",
        kind: "info",
        token: "conn:connecting",
        autoHideMs: 4000,
      };
    }

    return {
      text: "Connected",
      kind: "ok",
      token: "conn:online",
      autoHideMs: 4000,
    };
  }

  function renderStatusMessage() {
    const box = messageBoxEl();
    const textEl = messageTextEl();
    if (!box || !textEl) {
      return;
    }

    const msg = resolveStatusMessage();
    currentMessageToken = msg.token;

    if (dismissedMessageToken === msg.token) {
      clearMessageHideTimer();
      hideMessageBox();
      return;
    }

    textEl.textContent = msg.text;
    box.dataset.flash = "0";
    box.classList.remove("is-hidden", "message-info", "message-ok", "message-error");
    box.classList.add("message-" + msg.kind);
    scheduleMessageHide(msg.token, msg.autoHideMs || 0);
  }

  function captureFlashMessage() {
    const box = messageBoxEl();
    const textEl = messageTextEl();
    if (!box || !textEl) {
      return false;
    }
    if (box.dataset.flash !== "1") {
      return false;
    }

    const text = textEl.textContent.trim();
    if (text === "") {
      box.dataset.flash = "0";
      return false;
    }

    let kind = "info";
    if (box.classList.contains("message-error")) {
      kind = "error";
    } else if (box.classList.contains("message-ok")) {
      kind = "ok";
    }

    box.dataset.flash = "0";
    setTransientMessage(text, kind, 5500);
    return true;
  }

  function closeAddDialog() {
    const dialog = document.querySelector("#add-dialog");
    if (!dialog) {
      return;
    }
    if (typeof dialog.close === "function") {
      dialog.close();
      return;
    }
    dialog.removeAttribute("open");
  }

  function openAddDialog() {
    const dialog = document.querySelector("#add-dialog");
    if (!dialog) {
      return;
    }
    if (typeof dialog.showModal === "function") {
      dialog.showModal();
      return;
    }
    dialog.setAttribute("open", "open");
  }

  function selectedRow() {
    const hash = selectedHash();
    if (hash === "") {
      return null;
    }
    const rows = document.querySelectorAll("#torrents-body tr[data-hash]");
    for (const row of rows) {
      if (row.dataset.hash === hash) {
        return row;
      }
    }
    return null;
  }

  function setSelectedHash(hash) {
    const input = inputEl("selected-input");
    if (!input) {
      return;
    }
    input.value = hash || "";
    syncSelection();
    syncActionButtons();
  }

  function syncSelection() {
    const hash = selectedHash();
    let found = false;

    const rows = document.querySelectorAll("#torrents-body tr[data-hash]");
    for (const row of rows) {
      const active = hash !== "" && row.dataset.hash === hash;
      row.classList.toggle("active", active);
      if (active) {
        found = true;
      }
    }

    if (!found && hash !== "") {
      const input = inputEl("selected-input");
      if (input) {
        input.value = "";
      }
    }
  }

  function resetActionButton(button, label) {
    if (!button) {
      return;
    }
    button.disabled = true;
    button.textContent = label;
    button.removeAttribute("hx-post");
  }

  function processHtmx(element) {
    if (!window.htmx || !element) {
      return;
    }
    window.htmx.process(element);
  }

  function syncActionButtons() {
    const toggleBtn = document.querySelector("#toggle-selected");
    const recheckBtn = document.querySelector("#recheck-selected");
    const removeBtn = document.querySelector("#remove-selected");

    const row = selectedRow();
    if (!row) {
      resetActionButton(toggleBtn, "Start");
      resetActionButton(recheckBtn, "Recheck");
      resetActionButton(removeBtn, "Remove");
      return;
    }

    const hash = row.dataset.hash || "";
    const hashPath = encodeURIComponent(hash);
    const isRunning = row.dataset.running === "1";

    if (toggleBtn) {
      toggleBtn.disabled = false;
      toggleBtn.textContent = isRunning ? "Stop" : "Start";
      toggleBtn.setAttribute("hx-post", "/ui/torrents/" + hashPath + "/" + (isRunning ? "stop" : "start"));
      processHtmx(toggleBtn);
    }
    if (recheckBtn) {
      recheckBtn.disabled = false;
      recheckBtn.textContent = "Recheck";
      recheckBtn.setAttribute("hx-post", "/ui/torrents/" + hashPath + "/recheck");
      processHtmx(recheckBtn);
    }
    if (removeBtn) {
      removeBtn.disabled = false;
      removeBtn.textContent = "Remove";
      removeBtn.setAttribute("hx-post", "/ui/torrents/" + hashPath + "/remove");
      processHtmx(removeBtn);
    }
  }

  function syncDashboard() {
    syncSelection();
    syncActionButtons();
    syncBackendFromStats();
    setConnectionDot(connectionState);
    if (!captureFlashMessage()) {
      renderStatusMessage();
    }
  }

  function startBackendStatusStream() {
    if (!("EventSource" in window)) {
      return;
    }

    if (backendStatusStream) {
      backendStatusStream.close();
      backendStatusStream = null;
    }

    try {
      backendStatusStream = new EventSource("/ui/backend-status/stream");
    } catch (_err) {
      return;
    }

    backendStatusStream.addEventListener("status", function (event) {
      const payload = parseStatusPayload(event.data);
      setBackendStatus(payload.kind, payload.message);
    });
  }

  function eventElement(event) {
    return event.target instanceof Element ? event.target : null;
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

  document.addEventListener("click", function (event) {
    const target = eventElement(event);
    if (!target) {
      return;
    }

    const row = target.closest("#torrents-body tr[data-hash]");
    if (row) {
      setSelectedHash(row.dataset.hash || "");
      return;
    }

    if (target.closest("#open-add")) {
      openAddDialog();
      return;
    }

    if (target.closest("#cancel-add")) {
      closeAddDialog();
      return;
    }

    if (target.closest("#form-message-close")) {
      dismissedMessageToken = currentMessageToken;
      hideMessageBox();
    }
  });

  document.addEventListener("submit", function (event) {
    const form = event.target;
    if (!(form instanceof HTMLFormElement) || form.id !== "add-form") {
      return;
    }

    const magnetInput = form.querySelector("#magnet");
    const magnet = magnetInput instanceof HTMLInputElement ? magnetInput.value : "";
    const fileInput = form.querySelector("#torrent");
    const hasFile = fileInput && fileInput.files && fileInput.files.length > 0;

    if (magnet.trim() !== "" || hasFile) {
      return;
    }

    event.preventDefault();
    setTransientMessage("provide a magnet link or a .torrent file", "error", 7000);
  });

  document.body.addEventListener("htmx:sseOpen", function (event) {
    const target = eventElement(event);
    if (target && target.closest("#dashboard")) {
      setConnectionState("online");
    }
  });

  document.body.addEventListener("htmx:sseError", function (event) {
    const target = eventElement(event);
    if (target && target.closest("#dashboard")) {
      setConnectionState("offline", "Connection error: SSE stream disconnected");
    }
  });

  document.body.addEventListener("htmx:sseClose", function (event) {
    const target = eventElement(event);
    if (target && target.closest("#dashboard")) {
      setConnectionState("offline", "Connection error: SSE stream closed");
    }
  });

  document.body.addEventListener("htmx:responseError", function (event) {
    const target = event.detail && event.detail.target;
    if (target && target.closest && target.closest("#dashboard")) {
      setConnectionState("offline", requestErrorMessage(event));
    }
  });

  document.body.addEventListener("htmx:beforeRequest", function (event) {
    const target = event.detail && event.detail.target;
    if (target && target.closest && target.closest("#dashboard")) {
      setConnectionState("connecting");
    }
  });

  document.body.addEventListener("htmx:afterSwap", function (event) {
    const target = event.detail && event.detail.target;
    if (!target) {
      return;
    }

    if (target.id === "global-stats") {
      syncBackendFromStats();
      return;
    }

    if (target.id === "dashboard" || target.id === "torrents-body") {
      syncDashboard();
    }
  });

  document.body.addEventListener("htmx:sseMessage", function (event) {
    const detail = event.detail;
    if (detail && typeof detail.data === "string") {
      const status = backendStatusFromHTML(detail.data);
      if (status.kind !== "" || status.message !== "") {
        setBackendStatus(status.kind, status.message);
        return;
      }
    }
    syncBackendFromStats();
  });

  document.addEventListener("DOMContentLoaded", function () {
    syncDashboard();
    startBackendStatusStream();
  });

  window.addEventListener("beforeunload", function () {
    if (backendStatusStream) {
      backendStatusStream.close();
      backendStatusStream = null;
    }
  });
})();
