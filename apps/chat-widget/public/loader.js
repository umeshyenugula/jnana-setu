/* JñānaSetu chat widget loader (public, no build step).
 *
 * Usage:
 *   <script src="https://widget.example.com/loader.js"
 *           data-widget-id="PUBLIC_WIDGET_ID" defer></script>
 *
 * The loader reads the public widget ID (not a secret), creates a launcher
 * button and an iframe hosting the React widget, and relays open/close
 * commands. All chat traffic happens inside the iframe against the Module 6
 * chat API; the host page never sees messages or session IDs.
 *
 * Security: postMessage targets use the exact widget origin (never "*");
 * inbound iframe messages are accepted only from that origin with the
 * documented "jnana-widget" namespace and payload shape.
 */
(function () {
  "use strict";

  var NAMESPACE = "jnana-widget";

  function currentScript() {
    if (document.currentScript) return document.currentScript;
    var scripts = document.getElementsByTagName("script");
    return scripts[scripts.length - 1];
  }

  function widgetOrigin(script) {
    try {
      return new URL(script.src).origin;
    } catch (e) {
      return null;
    }
  }

  function guardOutbound(event, expectedOrigin) {
    if (event.origin !== expectedOrigin) return null;
    var data = event.data;
    if (!data || typeof data !== "object" || data.namespace !== NAMESPACE) return null;
    if (data.type === "jnana-opened" || data.type === "jnana-closed") return { type: data.type };
    if (
      data.type === "jnana-resize" &&
      typeof data.height === "number" &&
      data.height > 0 &&
      data.height < 2000
    ) {
      return { type: data.type, height: data.height };
    }
    return null;
  }

  function init() {
    var script = currentScript();
    var widgetId = script.getAttribute("data-widget-id");
    if (!widgetId || widgetId.length > 128) return;
    var origin = widgetOrigin(script);
    if (!origin) return;
    if (document.querySelector('iframe[data-jnana-widget="' + widgetId + '"]')) return;

    var apiBase = script.getAttribute("data-api-base") || "";
    var src =
      origin +
      "/?widgetId=" +
      encodeURIComponent(widgetId) +
      (apiBase ? "&apiBase=" + encodeURIComponent(apiBase) : "");

    var iframe = document.createElement("iframe");
    iframe.setAttribute("data-jnana-widget", widgetId);
    iframe.src = src;
    iframe.title = "Support chat";
    iframe.setAttribute("aria-label", "Support chat");
    iframe.style.cssText =
      "position:fixed;right:0;bottom:0;width:420px;height:600px;max-width:100vw;max-height:100vh;border:0;z-index:2147483647;background:transparent;";
    iframe.setAttribute("sandbox", "allow-scripts allow-same-origin allow-forms allow-popups");
    document.body.appendChild(iframe);

    window.addEventListener("message", function (event) {
      var message = guardOutbound(event, origin);
      if (!message) return;
      if (message.type === "jnana-resize") {
        iframe.style.height = Math.round(message.height) + "px";
      }
    });

    // Optional host-driven toggle: window.postMessage({namespace, type}, origin)
    // is intentionally NOT exposed — the in-iframe launcher owns open state.
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
