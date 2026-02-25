(function () {
  const CONNECTION_STATES = ["connecting", "online", "offline"];

  let connectionState = "connecting";
  let dismissedMessageToken = "";

  function messageBoxEl() {
    return document.querySelector("#form-message");
  }

  function messageTextEl() {
    return document.querySelector("#form-message-text");
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

  function currentMessageToken() {
    const box = messageBoxEl();
    const textEl = messageTextEl();
    if (!box || !textEl) {
      return "";
    }

    const text = textEl.textContent.trim();
    if (text === "") {
      return "";
    }

    let kind = "info";
    if (box.classList.contains("message-error")) {
      kind = "error";
    } else if (box.classList.contains("message-ok")) {
      kind = "ok";
    }
    return kind + ":" + text;
  }

  function applyDismissedMessage() {
    const box = messageBoxEl();
    if (!box) {
      return;
    }

    const token = currentMessageToken();
    if (token === "") {
      box.classList.add("is-hidden");
      return;
    }

    if (token !== "" && token === dismissedMessageToken) {
      box.classList.add("is-hidden");
      return;
    }

    box.classList.remove("is-hidden");
  }

  function showMessage(kind, text) {
    const box = messageBoxEl();
    const textEl = messageTextEl();
    if (!box || !textEl) {
      return;
    }

    const normalizedKind = kind === "error" || kind === "ok" ? kind : "info";
    const normalizedText = String(text || "").trim();
    textEl.textContent = normalizedText;
    box.classList.remove("is-hidden", "message-info", "message-ok", "message-error");
    box.classList.add("message-" + normalizedKind);

    applyDismissedMessage();
  }

  function setConnectionState(state) {
    connectionState = state;
    setConnectionDot(connectionState);
  }

  function syncAddButtonState() {
    const addBtn = document.querySelector("#add-btn");
    if (!(addBtn instanceof HTMLButtonElement)) {
      return;
    }

    const magnetInput = document.querySelector("#magnet");
    const fileInput = document.querySelector("#torrent");
    const hasMagnet = magnetInput instanceof HTMLInputElement && magnetInput.value.trim() !== "";
    const hasFile = fileInput instanceof HTMLInputElement && fileInput.files && fileInput.files.length > 0;

    addBtn.disabled = !(hasMagnet || hasFile);
  }

  function syncDashboard() {
    setConnectionDot(connectionState);
    applyDismissedMessage();
    syncAddButtonState();
  }

  document.addEventListener("click", function (event) {
    if (!(event.target instanceof Element)) {
      return;
    }

    if (event.target.closest("#form-message-close")) {
      dismissedMessageToken = currentMessageToken();
      const box = messageBoxEl();
      if (box) {
        box.classList.add("is-hidden");
      }
    }
  });

  document.body.addEventListener("htmx:sseOpen", function (event) {
    if (event.target instanceof Element && event.target.closest("#dashboard")) {
      setConnectionState("online");
    }
  });

  document.body.addEventListener("htmx:sseError", function (event) {
    if (event.target instanceof Element && event.target.closest("#dashboard")) {
      setConnectionState("offline");
      showMessage("error", "Connection error: SSE stream disconnected");
    }
  });

  document.body.addEventListener("htmx:sseClose", function (event) {
    if (event.target instanceof Element && event.target.closest("#dashboard")) {
      setConnectionState("offline");
      showMessage("error", "Connection error: SSE stream closed");
    }
  });

  document.body.addEventListener("htmx:responseError", function (event) {
    const target = event.detail && event.detail.target;
    if (target && target.closest && target.closest("#dashboard")) {
      setConnectionState("offline");
      showMessage("error", requestErrorMessage(event));
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

    if (target.id === "form-message") {
      applyDismissedMessage();
      return;
    }

    if (target.id === "dashboard") {
      syncDashboard();
      return;
    }

    if (target.closest && target.closest("#dashboard")) {
      syncDashboard();
    }
  });

  document.addEventListener("input", function (event) {
    if (!(event.target instanceof Element)) {
      return;
    }
    if (event.target.id === "magnet") {
      syncAddButtonState();
    }
  });

  document.addEventListener("change", function (event) {
    if (!(event.target instanceof Element)) {
      return;
    }
    if (event.target.id === "torrent" || event.target.id === "magnet") {
      syncAddButtonState();
    }
  });

  document.addEventListener("DOMContentLoaded", function () {
    syncDashboard();
  });
})();
