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
      setConnectionDot(connectionState);
      const addDialog = document.querySelector("#add-dialog[data-open-on-load]");
      if (addDialog instanceof HTMLDialogElement && !addDialog.open) {
        addDialog.showModal();
      }
    }
  });

})();
