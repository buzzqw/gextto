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

  var tokenPrompted = false;

  function request(path, method, body) {
    var headers = { "Content-Type": "application/json" };
    var value = token();
    if (value) headers["X-Gextto-Token"] = value;
    return fetch(path, {
      method: method || "GET",
      headers: headers,
      body: body ? JSON.stringify(body) : undefined,
      credentials: "same-origin"
    });
  }

  function handleResponse(response) {
    var type = response.headers.get("content-type") || "";
    var isJSON = type.indexOf("application/json") >= 0;
    if (!response.ok) {
      return (isJSON ? response.json().catch(function () { return null; }) : Promise.resolve(null))
        .then(function (data) {
          var message = data && data.error ? String(data.error) : "HTTP " + response.status;
          if (response.status === 401) message = "token API mancante o non valido";
          throw new Error(message);
        });
    }
    return isJSON ? response.json() : response.text();
  }

  function api(path, method, body) {
    return request(path, method, body).then(function (response) {
      // The API can be protected by an optional token; the new UI has no form
      // for it, so ask once and retry (the token is shared with the classic UI).
      if (response.status === 401 && !tokenPrompted) {
        tokenPrompted = true;
        var entered = window.prompt("Token API Gextto (vuoto per annullare):");
        if (entered) {
          try { localStorage.setItem("gextto_api_token", entered); } catch (error) { /* ignore */ }
        }
        return request(path, method, body).then(handleResponse);
      }
      return handleResponse(response);
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
          esc(error.message) + "</div></div>";
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
  // safeHref returns an escaped href only for http/https/magnet links, so a
  // malicious release or comic field cannot inject javascript: URLs.
  function safeHref(value) {
    var href = String(value === null || value === undefined ? "" : value).trim();
    if (!/^(https?:|magnet:)/i.test(href)) return "";
    return esc(href);
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
        var items = (itemsKey && data[itemsKey]) || (Array.isArray(data) ? data : []);
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
              var value = fmt(row[column.key], column.format);
              if (column.format === "series_link") {
                return '<td><a href="/?view=series&amp;series=' + encodeURIComponent(row[column.key]) + '" title="Apri il dettaglio della serie">' + esc(value) + "</a></td>";
              }
              if (column.format === "movie_link") {
                return '<td><a href="/?view=movies&amp;movie=' + encodeURIComponent(row.id) + '" title="Apri il dettaglio del film">' + esc(value) + "</a></td>";
              }
              if (column.format === "url" || column.format === "getcomics") {
                if (!value) return "<td></td>";
                var href = String(value);
                if (column.format === "getcomics" && href.charAt(0) === "/") {
                  href = "https://getcomics.org" + href;
                }
                href = safeHref(href);
                if (!href) return "<td></td>";
                return '<td><a href="' + href + '" target="_blank" rel="noopener">apri</a></td>';
              }
              return "<td>" + esc(value) + "</td>";
            }).join("");
            var actionsHtml = "";
            if (actions.length) {
              actionsHtml = '<td class="row-actions">' + actions.map(function (action) {
                var path = action.path.replace(/\{([a-z_]+)\}/g, function (_, key) {
                  return encodeURIComponent(row[key]);
                });
                var body = (action.body || "{}").replace(/\{([a-z_]+)\}/g, function (_, key) {
                  var encoded = JSON.stringify(row[key] == null ? "" : row[key]);
                  return encoded.slice(1, -1);
                });
                var confirmAttr = action.confirm ? ' data-confirm="' + esc(action.confirm) + '"' : "";
                return '<button class="btn sm ' + (action.class || "") + '" data-api="' + esc(path) +
                  '" data-method="' + esc(action.method || "POST") + '" data-body="' + esc(body) +
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

  // ---- movie detail edit form ---------------------------------------------
  Array.prototype.forEach.call(document.querySelectorAll("[data-movie-id]"), function (form) {
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      var id = form.getAttribute("data-movie-id");
      var body = {};
      Array.prototype.forEach.call(form.querySelectorAll("input[name]"), function (input) {
        body[input.name] = input.value;
      });
      var message = form.querySelector("small");
      api("/api/movies/" + encodeURIComponent(id), "POST", body).then(function () {
        if (message) message.textContent = "Salvato";
      }).catch(function (error) {
        alert("Salvataggio non riuscito: " + error.message);
      });
    });
  });

  // ---- i18n editor --------------------------------------------------------
  var i18nEditor = document.querySelector("[data-i18n-editor]");
  if (i18nEditor) {
    var langSelect = i18nEditor.querySelector("[data-i18n-language]");
    var i18nBody = i18nEditor.querySelector("[data-i18n-body]");
    var i18nExport = i18nEditor.querySelector("[data-i18n-export]");
    var i18nMessage = i18nEditor.querySelector("[data-i18n-message]");
    function loadI18n() {
      if (i18nExport) i18nExport.href = "/api/i18n/export/" + encodeURIComponent(langSelect.value);
      api("/api/i18n?lang=" + encodeURIComponent(langSelect.value), "GET").then(function (data) {
        var items = (data && data.items) || [];
        if (!items.length) {
          i18nBody.innerHTML = '<tr><td class="muted" colspan="3">Nessuna traduzione.</td></tr>';
          return;
        }
        i18nBody.innerHTML = items.map(function (item) {
          return '<tr><td class="truncate">' + esc(item.key) + "</td>" +
            '<td><input data-i18n-key="' + esc(item.key) + '" value="' + esc(item.value) + '" /></td>' +
            '<td><button class="btn sm" data-i18n-save="' + esc(item.key) + '">Salva</button></td></tr>';
        }).join("");
      }).catch(function (error) { if (i18nMessage) i18nMessage.textContent = error.message; });
    }
    api("/api/i18n/active", "GET").then(function (data) {
      if (data && data.lang) langSelect.value = data.lang;
      loadI18n();
    }).catch(function () { loadI18n(); });
    langSelect.addEventListener("change", function () {
      api("/api/i18n/language", "POST", { lang: langSelect.value }).then(function () { loadI18n(); })
        .catch(function (error) { alert("Cambio lingua non riuscito: " + error.message); });
    });
    i18nBody.addEventListener("click", function (event) {
      var button = event.target.closest("[data-i18n-save]");
      if (!button) return;
      var key = button.getAttribute("data-i18n-save");
      var input = i18nBody.querySelector('[data-i18n-key="' + key.replace(/(["\\])/g, "\\$1") + '"]');
      button.disabled = true;
      api("/api/i18n", "POST", { lang: langSelect.value, key: key, value: input ? input.value : "" })
        .then(function () { if (i18nMessage) i18nMessage.textContent = "Salvato"; })
        .catch(function (error) { alert("Salvataggio non riuscito: " + error.message); })
        .then(function () { button.disabled = false; });
    });
    var i18nImport = i18nEditor.querySelector("[data-i18n-import]");
    if (i18nImport) i18nImport.addEventListener("click", function () {
      var area = i18nEditor.querySelector("[data-i18n-yaml]");
      i18nImport.disabled = true;
      api("/api/i18n/import/" + encodeURIComponent(langSelect.value), "POST", { yaml: area ? area.value : "" })
        .then(function () { if (i18nMessage) i18nMessage.textContent = "Importato"; loadI18n(); })
        .catch(function (error) { alert("Import non riuscito: " + error.message); })
        .then(function () { i18nImport.disabled = false; });
    });
    var i18nDelete = i18nEditor.querySelector("[data-i18n-delete]");
    if (i18nDelete) i18nDelete.addEventListener("click", function () {
      if (!confirm("Eliminare tutte le traduzioni della lingua " + langSelect.value + "?")) return;
      api("/api/i18n/" + encodeURIComponent(langSelect.value), "DELETE", {}).then(function () { loadI18n(); })
        .catch(function (error) { alert("Eliminazione non riuscita: " + error.message); });
    });
  }

  // ---- comics: GetComics link finder --------------------------------------
  var comicsDownload = document.querySelector("[data-comics-download]");
  if (comicsDownload) {
    var comicsInput = comicsDownload.querySelector("[data-comics-post]");
    var comicsMessage = comicsDownload.querySelector("[data-comics-message]");
    var comicsResults = comicsDownload.querySelector("[data-comics-results]");
    var comicsLinks = comicsDownload.querySelector("[data-comics-links]");
    var comicGroups = [
      { key: "download_now", label: "Download diretto", method: "download_now" },
      { key: "direct", label: "File diretti", method: "direct" },
      { key: "torrents", label: "Torrent", method: "torrent" },
      { key: "magnets", label: "Magnet", method: "magnet" },
      { key: "mega", label: "Mega", method: "mega" }
    ];
    comicsLinks.addEventListener("click", function () {
      var postURL = comicsInput.value.trim();
      if (!postURL) { alert("Inserisci l'URL del post GetComics"); return; }
      comicsLinks.disabled = true;
      if (comicsMessage) comicsMessage.textContent = "Ricerca in corso…";
      api("/api/comics/links", "POST", { url: postURL }).then(function (data) {
        var links = (data && data.links) || {};
        var html = comicGroups.map(function (group) {
          var values = links[group.key] || [];
          if (!values.length) return "";
          return '<div class="field span-full" style="margin-top:10px"><span>' + esc(group.label) + "</span></div>" +
            '<div class="toolbar">' + values.map(function (value) {
              var body = JSON.stringify({ url: value, method: group.method, title: "", post_url: postURL });
              return '<button class="btn sm" data-api="/api/comics/download" data-method="POST" data-body="' + esc(body) + '" title="' + esc(value) + '">Scarica</button>';
            }).join(" ") + "</div>";
        }).join("");
        comicsResults.innerHTML = html || '<p class="muted">Nessun link trovato.</p>';
        if (comicsMessage) comicsMessage.textContent = "";
      }).catch(function (error) {
        if (comicsMessage) comicsMessage.textContent = error.message;
      }).then(function () { comicsLinks.disabled = false; });
    });
  }

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
      var button = form.querySelector("button");
      var secret = input && input.type === "password";
      var value = input ? input.value : "";
      if (secret && value === "") {
        if (button) {
          button.textContent = "Inserisci un valore";
          setTimeout(function () { button.textContent = "Salva"; }, 1500);
        }
        return;
      }
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
          var magnet = safeHref(row.magnet || "");
          var title = magnet ? '<a href="' + magnet + '">' + esc(row.title) + "</a>" : esc(row.title);
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

  // ---- library editor (feeds + indexers + series/movies) ------------------
  // Feeds live in the `url` setting, indexers in the `indexers` setting (the
  // classic UI saves them the same way via /api/config/settings); series and
  // movies go through /api/config/library.
  var libraryEditor = document.querySelector("[data-library-editor]");
  if (libraryEditor) {
    var library = null;
    var libraryConfig = null;
    var feedsInput = libraryEditor.querySelector("[data-library-feeds]");
    var indexersInput = libraryEditor.querySelector("[data-library-indexers]");
    var seriesInput = libraryEditor.querySelector("[data-library-series]");
    var moviesInput = libraryEditor.querySelector("[data-library-movies]");
    var libraryMessage = libraryEditor.querySelector("[data-library-message]");
    Promise.all([
      api("/api/config", "GET"),
      api("/api/config/library", "GET")
    ]).then(function (results) {
      libraryConfig = results[0] || {};
      library = results[1] || {};
      if (feedsInput) feedsInput.value = (libraryConfig.feed_urls || []).join("\n");
      if (indexersInput) indexersInput.value = JSON.stringify(libraryConfig.indexers || [], null, 2);
      if (seriesInput) seriesInput.value = JSON.stringify(library.series || [], null, 2);
      if (moviesInput) moviesInput.value = JSON.stringify(library.movies || [], null, 2);
    }).catch(function (error) {
      if (libraryMessage) libraryMessage.textContent = error.message;
    });
    var saveLibrary = libraryEditor.querySelector("[data-library-save]");
    if (saveLibrary) saveLibrary.addEventListener("click", function () {
      if (!library || !libraryConfig) return;
      var feeds = [];
      if (feedsInput) {
        feeds = (feedsInput.value || "").split("\n").map(function (line) {
          return line.trim();
        }).filter(function (line) { return line !== ""; });
      }
      var indexers, series, movies;
      try { indexers = JSON.parse(indexersInput.value || "[]"); }
      catch (error) { alert("Indexer JSON non valido: " + error.message); return; }
      try { series = JSON.parse(seriesInput.value || "[]"); }
      catch (error) { alert("Serie JSON non valido: " + error.message); return; }
      try { movies = JSON.parse(moviesInput.value || "[]"); }
      catch (error) { alert("Film JSON non valido: " + error.message); return; }
      saveLibrary.disabled = true;
      Promise.all([
        api("/api/config/settings", "POST", { key: "url", value: JSON.stringify(feeds) }),
        api("/api/config/settings", "POST", { key: "indexers", value: JSON.stringify(indexers) }),
        api("/api/config/library", "POST", { series: series, movies: movies })
      ]).then(function () {
        if (libraryMessage) libraryMessage.textContent = "Libreria e sorgenti salvate";
      }).catch(function (error) {
        alert("Salvataggio non riuscito: " + error.message);
      }).then(function () { saveLibrary.disabled = false; });
    });
  }

  // ---- generic JSON editors ----------------------------------------------
  Array.prototype.forEach.call(document.querySelectorAll("[data-json-editor]"), function (editor) {
    var textarea = editor.querySelector("textarea");
    var message = editor.querySelector("small");
    var button = editor.querySelector("button");
    var unwrap = editor.getAttribute("data-unwrap") || "";
    var wrap = editor.getAttribute("data-wrap") || "";
    api(editor.getAttribute("data-get"), "GET").then(function (data) {
      var payload = (unwrap && data && data[unwrap] !== undefined) ? data[unwrap] : data;
      if (textarea) textarea.value = JSON.stringify(payload, null, 2);
    }).catch(function (error) { if (message) message.textContent = error.message; });
    if (!button) return;
    button.addEventListener("click", function () {
      var parsed;
      try { parsed = JSON.parse((textarea && textarea.value) || "null"); }
      catch (error) { alert("JSON non valido: " + error.message); return; }
      var body = parsed;
      if (wrap) { body = {}; body[wrap] = parsed; }
      button.disabled = true;
      api(editor.getAttribute("data-post"), "POST", body).then(function () {
        if (message) message.textContent = "Salvato";
      }).catch(function (error) {
        alert("Salvataggio non riuscito: " + error.message);
      }).then(function () { button.disabled = false; });
    });
  });

  // ---- OAuth / PIN flows (Trakt, Simkl) -----------------------------------
  document.addEventListener("click", function (event) {
    var start = event.target.closest("[data-oauth-start]");
    if (start) {
      var panel = start.closest(".panel");
      var output = panel.querySelector("[data-oauth-output]");
      start.disabled = true;
      api(start.getAttribute("data-oauth-start"), "POST", {})
        .then(function (data) { if (output) output.textContent = JSON.stringify(data, null, 2); })
        .catch(function (error) { if (output) output.textContent = error.message; })
        .then(function () { start.disabled = false; });
      return;
    }
    var poll = event.target.closest("[data-oauth-poll]");
    if (poll) {
      var panel2 = poll.closest(".panel");
      var output2 = panel2.querySelector("[data-oauth-output]");
      var input = panel2.querySelector("[data-oauth-code]");
      var code = input ? input.value.trim() : "";
      if (!code) { alert("Inserisci il codice di accesso"); return; }
      poll.disabled = true;
      api(poll.getAttribute("data-oauth-poll"), "POST", { code: code })
        .then(function (data) { if (output2) output2.textContent = JSON.stringify(data, null, 2); })
        .catch(function (error) { if (output2) output2.textContent = error.message; })
        .then(function () { poll.disabled = false; });
      return;
    }
  });
})();
