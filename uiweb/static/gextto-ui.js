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

  // ---- generic list pages -------------------------------------------------
  function esc(value) {
    return String(value === null || value === undefined ? "" : value)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }
  function humanBytes(value) {
    var n = Number(value);
    if (!isFinite(n) || n <= 0) return "0 B";
    var units = ["B", "KB", "MB", "GB", "TB", "PB"];
    var index = 0;
    while (n >= 1024 && index < units.length - 1) { n /= 1024; index++; }
    return (index === 0 ? n.toFixed(0) : n.toFixed(1)) + " " + units[index];
  }
  function fmt(value, format) {
    if (value === null || value === undefined || value === "") return "";
    switch (format) {
      case "bool": return value ? "Sì" : "No";
      case "bytes": return humanBytes(value);
      case "rate": return humanBytes(value) + "/s";
      case "percent": return Number(value).toFixed(1) + "%";
      default: return String(value);
    }
  }
  function renderTable(container) {
    var panel = container.closest(".panel");
    var table = panel.querySelector("table");
    var thead = panel.querySelector("[data-ui-head]");
    var tbody = panel.querySelector("[data-ui-body]");
    var count = panel.querySelector("[data-ui-count]");
    var searchInput = panel.querySelector("[data-ui-search]");
    var endpoint = container.getAttribute("data-endpoint");
    var itemsKey = container.getAttribute("data-items") || "items";
    var columns = JSON.parse(container.getAttribute("data-columns") || "[]");
    var actions = JSON.parse(container.getAttribute("data-actions") || "[]");
    var empty = container.getAttribute("data-empty") || "Nessun elemento.";
    var searchParam = container.getAttribute("data-search") || "";
    function fetchAndRender() {
      var url = endpoint;
      if (searchParam && searchInput && searchInput.value) {
        url += (url.indexOf("?") >= 0 ? "&" : "?") + searchParam + "=" + encodeURIComponent(searchInput.value);
      }
      api(url, "GET").then(function (data) {
        var items = itemsKey ? (data[itemsKey] || []) : (Array.isArray(data) ? data : []);
        if (!columns.length && items.length) {
          columns = Object.keys(items[0]).filter(function (key) {
            var value = items[0][key];
            return value === null || typeof value !== "object";
          }).slice(0, 8).map(function (key) { return { key: key, label: key }; });
        }
        var colspan = columns.length + (actions.length ? 1 : 0);
        thead.innerHTML = "<tr>" + columns.map(function (column) {
          return "<th>" + esc(column.label) + "</th>";
        }).join("") + (actions.length ? "<th>Azioni</th>" : "") + "</tr>";
        if (!items.length) {
          tbody.innerHTML = '<tr><td class="muted" colspan="' + colspan + '">' + esc(empty) + "</td></tr>";
        } else {
          tbody.innerHTML = items.map(function (row) {
            var cells = columns.map(function (column) {
              return "<td>" + esc(fmt(row[column.key], column.format)) + "</td>";
            }).join("");
            var actionsHtml = "";
            if (actions.length) {
              actionsHtml = '<td class="row-actions">' + actions.map(function (action) {
                var path = action.path.replace(/\{([a-z_]+)\}/g, function (_, key) {
                  return encodeURIComponent(row[key]);
                });
                var confirmAttr = action.confirm ? ' data-confirm="' + esc(action.confirm) + '"' : "";
                return '<button class="btn sm ' + (action.class || "") + '" data-api="' + esc(path) +
                  '" data-method="' + esc(action.method || "POST") + '" data-body="' + esc(action.body || "{}") +
                  '"' + confirmAttr + ">" + esc(action.label) + "</button>";
              }).join(" ") + "</td>";
            }
            return "<tr>" + cells + actionsHtml + "</tr>";
          }).join("");
        }
        if (count) count.textContent = items.length + " voci";
      }).catch(function (error) {
        tbody.innerHTML = '<tr><td class="alert">' + esc(error.message) + "</td></tr>";
      });
    }
    container._refetch = fetchAndRender;
    var refresh = panel.querySelector("[data-ui-refresh]");
    if (refresh) refresh.addEventListener("click", fetchAndRender);
    if (searchInput) searchInput.addEventListener("keydown", function (event) {
      if (event.key === "Enter") { event.preventDefault(); fetchAndRender(); }
    });
    fetchAndRender();
  }
  Array.prototype.forEach.call(document.querySelectorAll("[data-ui-table]"), renderTable);

  // ---- generic actions ----------------------------------------------------
  document.addEventListener("click", function (event) {
    var element = event.target.closest("[data-api]");
    if (!element) return;
    var confirmMessage = element.getAttribute("data-confirm");
    if (confirmMessage && !confirm(confirmMessage)) return;
    var body = {};
    try { body = JSON.parse(element.getAttribute("data-body") || "{}"); } catch (error) { body = {}; }
    element.disabled = true;
    api(element.getAttribute("data-api"), element.getAttribute("data-method") || "POST", body)
      .then(function () {
        var container = element.closest(".panel") &&
          element.closest(".panel").querySelector("[data-ui-table]");
        if (container && container._refetch) { container._refetch(); return; }
        if (partials[view]) { load(); return; }
        location.reload();
      })
      .catch(function (error) { alert("Azione non riuscita: " + error.message); })
      .then(function () { element.disabled = false; });
  });

  // ---- settings forms -----------------------------------------------------
  Array.prototype.forEach.call(document.querySelectorAll("[data-setting-key]"), function (form) {
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      var key = form.getAttribute("data-setting-key");
      var input = form.querySelector("[data-setting-input]");
      var secret = input && input.type === "password";
      var value = input ? input.value : "";
      if (secret && value === "") return;
      var button = form.querySelector("button");
      if (button) button.disabled = true;
      api("/api/config/settings", "POST", { key: key, value: value })
        .then(function () {
          if (!button) return;
          button.textContent = "Salvato";
          setTimeout(function () { button.textContent = "Salva"; button.disabled = false; }, 1500);
        })
        .catch(function (error) {
          alert("Salvataggio non riuscito: " + error.message);
          if (button) button.disabled = false;
        });
    });
  });

  // ---- explore (search) ---------------------------------------------------
  function renderSearch(form) {
    var panel = form.closest(".panel");
    var thead = panel.querySelector("[data-ui-head]");
    var tbody = panel.querySelector("[data-ui-body]");
    var count = panel.querySelector("[data-ui-count]");
    var input = form.querySelector("input");
    var endpoint = form.getAttribute("data-endpoint");
    var resultsKey = form.getAttribute("data-results") || "results";
    var addPath = form.getAttribute("data-add");
    thead.innerHTML = "<tr><th>Titolo</th><th>Seed</th><th>Dimensione</th><th>Azioni</th></tr>";
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      var query = (input.value || "").trim();
      if (!query) return;
      tbody.innerHTML = '<tr><td class="muted">Ricerca in corso…</td></tr>';
      api(endpoint, "POST", { query: query }).then(function (data) {
        var items = data[resultsKey] || [];
        form._items = items;
        if (count) count.textContent = items.length + " risultati";
        if (!items.length) {
          tbody.innerHTML = '<tr><td class="muted">Nessun risultato.</td></tr>';
          return;
        }
        tbody.innerHTML = items.map(function (row, index) {
          var magnet = esc(row.magnet || "");
          var title = row.magnet ? '<a href="' + magnet + '">' + esc(row.title) + "</a>" : esc(row.title);
          return '<tr><td class="truncate">' + title +
            '</td><td class="numeric">' + esc(row.seeders || 0) +
            '</td><td class="numeric">' + humanBytes(row.size_bytes) +
            "</td><td>" + (addPath
              ? '<button class="btn sm primary" data-ui-add data-index="' + index + '">Accoda</button>'
              : "") + "</td></tr>";
        }).join("");
      }).catch(function (error) {
        tbody.innerHTML = '<tr><td class="alert">' + esc(error.message) + "</td></tr>";
      });
    });
    form.addEventListener("click", function (event) {
      var button = event.target.closest("[data-ui-add]");
      if (!button) return;
      var items = form._items || [];
      var row = items[parseInt(button.getAttribute("data-index"), 10)];
      if (!row) return;
      button.disabled = true;
      api(addPath, "POST", { release: row }).then(function () {
        button.textContent = "Accodata";
      }).catch(function (error) {
        alert("Non accodata: " + error.message);
        button.disabled = false;
      });
    });
  }
  Array.prototype.forEach.call(document.querySelectorAll("[data-ui-search-post]"), renderSearch);
})();
