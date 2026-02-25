(function () {
  const CONNECTION_STATES = ["online", "offline"];

  let connectionState = "offline";

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

  function showErrorMessage(text) {
    const box = messageBoxEl();
    const textEl = messageTextEl();
    if (!box || !textEl) {
      return;
    }

    const normalizedText = String(text || "").trim();
    textEl.textContent = normalizedText;
    box.classList.remove("message-info", "message-ok", "message-error");
    if (normalizedText === "") {
      box.classList.add("is-hidden");
    } else {
      box.classList.remove("is-hidden");
    }
    box.classList.add("message-error");
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
    syncAddButtonState();
  }

  document.body.addEventListener("htmx:sseOpen", function (event) {
    if (event.target?.closest?.("#dashboard")) {
      setConnectionState("online");
    }
  });

  document.body.addEventListener("htmx:sseError", function (event) {
    if (event.target?.closest?.("#dashboard")) {
      setConnectionState("offline");
      showErrorMessage("Connection error: SSE stream disconnected");
    }
  });

  document.body.addEventListener("htmx:responseError", function (event) {
    const target = event.detail?.target;
    if (target?.closest?.("#dashboard")) {
      setConnectionState("offline");
      showErrorMessage(requestErrorMessage(event));
    }
  });

  document.body.addEventListener("htmx:afterSwap", function (event) {
    const target = event.detail?.target;
    if (target?.id === "dashboard") {
      syncDashboard();
    }
  });

  document.addEventListener("input", function (event) {
    if (event.target?.id === "magnet") {
      syncAddButtonState();
    }
  });

  document.addEventListener("change", function (event) {
    if (event.target?.id === "torrent" || event.target?.id === "magnet") {
      syncAddButtonState();
    }
  });

})();
