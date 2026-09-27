// gextto-ui.js — small client for the server-rendered UI (see uiweb.go).
// It only fills authenticated partials and forwards actions to the existing
// JSON APIs; it is intentionally not a SPA.
(function () {
  "use strict";

  var page = document.getElementById("ui-page");
  if (!page) return;
  var view = page.getAttribute("data-view") || "dashboard";

  function token() {
    try {
      return localStorage.getItem("gextto_api_token") || "";
    } catch (error) {
      return "";
    }
  }

  function api(path, method, body) {
    var headers = { "Content-Type": "application/json" };
    var value = token();
    if (value) headers["X-Gextto-Token"] = value;
    return fetch(path, {
      method: method || "GET",
      headers: headers,
      body: body ? JSON.stringify(body) : undefined,
      credentials: "same-origin"
    }).then(function (response) {
      if (response.status === 401) {
        throw new Error("token API mancante o non valido");
      }
      if (!response.ok) {
        throw new Error("HTTP " + response.status);
      }
      var type = response.headers.get("content-type") || "";
      return type.indexOf("application/json") >= 0 ? response.json() : response.text();
    });
  }

  var partials = {
    dashboard: "/ui/partial/dashboard",
    downloads: "/ui/partial/torrents"
  };

  function load() {
    var url = partials[view];
    if (!url) return Promise.resolve();
    return api(url, "GET")
      .then(function (html) {
        page.innerHTML = html;
      })
      .catch(function (error) {
        page.innerHTML = '<div class="view"><div class="alert">Impossibile caricare i dati: ' +
          String(error.message) + "</div></div>";
      });
  }

  var timer = null;
  function schedule() {
    if (timer) clearInterval(timer);
    var interval = view === "downloads" ? 3000 : view === "dashboard" ? 10000 : 0;
    if (interval > 0) {
      timer = setInterval(function () {
        if (document.visibilityState === "visible") load();
      }, interval);
    }
  }

  document.addEventListener("click", function (event) {
    var element = event.target.closest("[data-action]");
    if (!element) return;
    var action = element.getAttribute("data-action");
    var hash = element.getAttribute("data-hash") || "";

    if (action === "refresh") {
      if (partials[view]) { load(); } else { location.reload(); }
      return;
    }
    if (action === "run-cycle") {
      element.disabled = true;
      api("/api/run_now", "POST", {})
        .then(function () { if (partials[view]) { load(); } else { location.reload(); } })
        .catch(function (error) { alert("Ciclo non avviato: " + error.message); })
        .then(function () { element.disabled = false; });
      return;
    }
    if (!hash) return;

    if (action === "remove" && !confirm("Rimuovere il torrent dalla sessione? I file restano su disco.")) {
      return;
    }
    var actions = {
      pause: ["/api/torrents/" + hash + "/pause", "POST", {}],
      resume: ["/api/torrents/" + hash + "/resume", "POST", {}],
      recheck: ["/api/torrents/" + hash + "/recheck", "POST", {}],
      remove: ["/api/torrents/" + hash + "/remove", "POST", { delete_files: false }]
    };
    var entry = actions[action];
    if (!entry) return;
    element.disabled = true;
    api(entry[0], entry[1], entry[2])
      .then(load)
      .catch(function (error) { alert("Azione non riuscita: " + error.message); })
      .then(function () { element.disabled = false; });
  });

  document.addEventListener("visibilitychange", schedule);
  schedule();
})();
