(function () {
  const CONNECTION_STATES = ["connecting", "online", "offline"];

  let connectionState = "connecting";
  let connectionError = "";
  let backendError = "";
  let currentMessageToken = "";
  let dismissedMessageToken = "";
  let transientMessage = null;
  let transientTimer = 0;
  let messageHideTimer = 0;

  function dashboardEl() {
    return document.querySelector("#dashboard");
  }

  function viewStateForm() {
    return document.querySelector("#view-state");
  }

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
    transientTimer = window.setTimeout(function () {
      clearTransientMessage();
      renderStatusMessage();
    }, durationMs || 4500);
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

  function setBackendError(text) {
    const next = (text || "").trim();
    if (backendError === next) {
      return;
    }
    backendError = next;
    dismissedMessageToken = "";
    renderStatusMessage();
  }

  function clearBackendError() {
    if (backendError === "") {
      return;
    }
    backendError = "";
    renderStatusMessage();
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
    if (transientMessage) {
      return {
        text: transientMessage.text,
        kind: transientMessage.kind,
        token: "transient:" + transientMessage.kind + ":" + transientMessage.text,
        autoHideMs: 4500,
      };
    }

    if (connectionState === "offline") {
      const text = connectionError || "Connection error";
      return {
        text,
        kind: "error",
        token: "conn:offline:" + text,
        autoHideMs: 7000,
      };
    }

    if (backendError !== "") {
      const text = "rTorrent error: " + backendError;
      return {
        text,
        kind: "error",
        token: "backend:error:" + backendError,
        autoHideMs: 7000,
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

  function viewValues() {
    const form = viewStateForm();
    if (!form) {
      return {};
    }

    const values = {};
    const data = new FormData(form);
    for (const pair of data.entries()) {
      const key = pair[0];
      const value = pair[1];
      if (typeof value === "string") {
        values[key] = value;
      }
    }
    return values;
  }

  function refreshDashboard() {
    if (!window.htmx || !dashboardEl()) {
      return;
    }
    setConnectionState("connecting");
    window.htmx.ajax("GET", "/ui/dashboard", {
      target: "#dashboard",
      swap: "outerHTML",
      values: viewValues(),
    });
  }

  function labelForSort(key) {
    switch (key) {
      case "name":
        return "Name";
      case "state":
        return "State";
      case "addedAt":
        return "Added";
      case "progress":
        return "Done";
      case "etaSeconds":
        return "ETA";
      case "ratio":
        return "Ratio";
      case "peers":
        return "Peers";
      case "seeds":
        return "Seeds";
      case "downRate":
        return "Down";
      case "upRate":
        return "Up";
      case "sizeBytes":
        return "Size";
      default:
        return key;
    }
  }

  function defaultDir(sortKey) {
    if (sortKey === "name" || sortKey === "state") {
      return "asc";
    }
    return "desc";
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

  function syncFilterButtons() {
    const filter = inputEl("filter-input");
    const current = filter ? filter.value : "all";
    const buttons = document.querySelectorAll("#filters button[data-filter]");
    for (const button of buttons) {
      button.classList.toggle("active", button.dataset.filter === current);
    }
  }

  function syncSortLabels() {
    const sortInput = inputEl("sort-input");
    const dirInput = inputEl("dir-input");
    const sort = sortInput ? sortInput.value : "addedAt";
    const dir = dirInput ? dirInput.value : "desc";

    const buttons = document.querySelectorAll("#torrent-table .sort-btn[data-sort]");
    for (const button of buttons) {
      const key = button.dataset.sort || "";
      const label = labelForSort(key);
      if (key === sort) {
        button.textContent = label + " " + (dir === "asc" ? "▲" : "▼");
      } else {
        button.textContent = label;
      }
    }
  }

  function syncDashboard() {
    syncSelection();
    syncActionButtons();
    syncFilterButtons();
    syncSortLabels();
    setConnectionDot(connectionState);
    if (!captureFlashMessage()) {
      renderStatusMessage();
    }
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

    const sortBtn = target.closest("#torrent-table .sort-btn[data-sort]");
    if (sortBtn) {
      event.preventDefault();
      const nextSort = sortBtn.dataset.sort || "addedAt";
      const sortInput = inputEl("sort-input");
      const dirInput = inputEl("dir-input");
      if (!sortInput || !dirInput) {
        return;
      }

      if (sortInput.value === nextSort) {
        dirInput.value = dirInput.value === "asc" ? "desc" : "asc";
      } else {
        sortInput.value = nextSort;
        dirInput.value = defaultDir(nextSort);
      }
      refreshDashboard();
      return;
    }

    const filterBtn = target.closest("#filters button[data-filter]");
    if (filterBtn) {
      event.preventDefault();
      const filterInput = inputEl("filter-input");
      if (!filterInput) {
        return;
      }
      filterInput.value = filterBtn.dataset.filter || "all";
      refreshDashboard();
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

    if (target.id === "backend-error-sink") {
      const text = target.textContent.trim();
      setBackendError(text || "unable to query rTorrent");
      return;
    }

    if (target.id === "backend-ok-sink") {
      clearBackendError();
      return;
    }

    if (target.id === "dashboard" || target.id === "torrents-body") {
      syncDashboard();
    }
  });

  document.addEventListener("DOMContentLoaded", function () {
    syncDashboard();
  });
})();
