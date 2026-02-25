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

  function messageToken(kind, text) {
    return kind + ":" + text;
  }

  function currentMessageToken() {
    const box = messageBoxEl();
    const textEl = messageTextEl();
    if (!box || !textEl) {
      return "";
    }

    let kind = "info";
    if (box.classList.contains("message-error")) {
      kind = "error";
    } else if (box.classList.contains("message-ok")) {
      kind = "ok";
    }

    return messageToken(kind, textEl.textContent.trim());
  }

  function applyDismissedMessage() {
    const box = messageBoxEl();
    if (!box) {
      return;
    }

    const token = currentMessageToken();
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

  function syncDashboard() {
    setConnectionDot(connectionState);
    applyDismissedMessage();
  }

  document.addEventListener("click", function (event) {
    const target = eventElement(event);
    if (!target) {
      return;
    }

    if (target.closest("#form-message-close")) {
      dismissedMessageToken = currentMessageToken();
      const box = messageBoxEl();
      if (box) {
        box.classList.add("is-hidden");
      }
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
    dismissedMessageToken = "";
    showMessage("error", "provide a magnet link or a .torrent file");
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
      setConnectionState("offline");
      dismissedMessageToken = "";
      showMessage("error", "Connection error: SSE stream disconnected");
    }
  });

  document.body.addEventListener("htmx:sseClose", function (event) {
    const target = eventElement(event);
    if (target && target.closest("#dashboard")) {
      setConnectionState("offline");
      dismissedMessageToken = "";
      showMessage("error", "Connection error: SSE stream closed");
    }
  });

  document.body.addEventListener("htmx:responseError", function (event) {
    const target = event.detail && event.detail.target;
    if (target && target.closest && target.closest("#dashboard")) {
      setConnectionState("offline");
      dismissedMessageToken = "";
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
    }
  });

  document.addEventListener("DOMContentLoaded", function () {
    syncDashboard();
  });
})();
