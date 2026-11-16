// Sends the page's log messages and errors to the dview server's log when
// dview runs with -x. This is a classic script that loads before app.js, so
// it also catches errors that stop app.js from loading at all.
(function () {
  "use strict";
  var queue = [];
  var enabled = null; // unknown until /api/config answers

  function post(entry) {
    try {
      fetch("/api/log", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(entry),
        keepalive: true,
      }).catch(function () {});
    } catch (e) { /* nothing more we can do */ }
  }

  // dviewLog(level, msg, data): level is "debug", "warn" or "error".
  window.dviewLog = function (level, msg, data) {
    if (enabled === false) return;
    var entry = { level: level, msg: String(msg), data: data || {} };
    if (enabled === null) queue.push(entry); else post(entry);
  };

  // Script errors, and (in the capture phase) resources that fail to load,
  // such as a script the browser refuses to run.
  window.addEventListener("error", function (ev) {
    var t = ev.target;
    if (t && t !== window && (t.src || t.href)) {
      window.dviewLog("error", "resource failed to load", { tag: t.tagName, url: t.src || t.href });
      return;
    }
    window.dviewLog("error", ev.message || "script error", {
      file: ev.filename, line: ev.lineno, column: ev.colno,
      stack: ev.error && ev.error.stack ? String(ev.error.stack) : "",
    });
  }, true);

  window.addEventListener("unhandledrejection", function (ev) {
    var r = ev.reason;
    window.dviewLog("error", "unhandled promise rejection", {
      reason: String(r), stack: r && r.stack ? String(r.stack) : "",
    });
  });

  fetch("/api/config")
    .then(function (resp) { return resp.json(); })
    .then(function (cfg) {
      enabled = !!cfg.debug;
      var pending = queue;
      queue = [];
      if (enabled) pending.forEach(post);
    })
    .catch(function () { enabled = false; queue = []; });

  window.dviewLog("debug", "page loading", { agent: navigator.userAgent, url: location.href });
})();
