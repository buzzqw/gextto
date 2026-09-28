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
    var requestMethod = (method || "GET").toUpperCase();
    var headers = { "Content-Type": "application/json" };
    var value = token();
    if (value) headers["X-Gextto-Token"] = value;
    // Browsers reject a body on GET/HEAD requests. Some generic action
    // buttons intentionally use GET (for example the port check) and carry
    // an empty data-body attribute, so only encode payloads for methods that
    // can transport one.
    var requestBody = requestMethod === "GET" || requestMethod === "HEAD"
      ? undefined
      : (body === undefined || body === null ? undefined : JSON.stringify(body));
    return fetch(path, {
      method: requestMethod,
      headers: headers,
      body: requestBody,
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
    downloads: "/ui/partial/torrents"
  };

  function load() {
    var url = partials[view];
    if (!url) return Promise.resolve();
    return api(url, "GET")
      .then(function (html) {
        if (view === "downloads") {
          var currentSlot = page.querySelector("[data-torrents-slot]");
          if (currentSlot) {
            var holder = document.createElement("div");
            holder.innerHTML = html;
            var incomingSlot = holder.querySelector("[data-torrents-slot]");
            if (incomingSlot) currentSlot.replaceWith(incomingSlot);
            return;
          }
        }
        page.innerHTML = html;
      })
      .catch(function (error) {
        page.innerHTML = '<div class="view"><div class="alert">Impossibile caricare i dati: ' +
          esc(error.message) + "</div></div>";
      });
  }

  // Inline feedback keeps the page context visible; browser alerts were easy
  // to miss on mobile and blocked the rest of the interface.
  var toastHost = null;
  function notify(message, kind) {
    if (!toastHost) {
      toastHost = document.createElement("div");
      toastHost.className = "toast-host";
      toastHost.setAttribute("aria-live", "polite");
      document.body.appendChild(toastHost);
    }
    var toast = document.createElement("div");
    toast.className = "toast " + (kind || "info");
    toast.setAttribute("role", kind === "err" ? "alert" : "status");
    var text = document.createElement("span");
    text.textContent = String(message || "");
    var close = document.createElement("button");
    close.className = "toast-close";
    close.type = "button";
    close.setAttribute("aria-label", "Chiudi messaggio");
    close.textContent = "×";
    close.addEventListener("click", function () { toast.remove(); });
    toast.appendChild(text);
    toast.appendChild(close);
    toastHost.appendChild(toast);
    window.setTimeout(function () { if (toast.parentNode) toast.remove(); }, kind === "err" ? 7000 : 3500);
  }

  var timer = null;
  function schedule() {
    if (timer) clearInterval(timer);
    var interval = view === "downloads" ? 3000 : 0;
    if (interval > 0) {
      timer = setInterval(function () {
        if (document.visibilityState === "visible") load();
      }, interval);
    }
  }

  document.addEventListener("click", function (event) {
    var bulkButton = event.target.closest("[data-download-bulk]");
    if (bulkButton) {
      var selected = Array.prototype.map.call(page.querySelectorAll("[data-download-select]:checked"), function (node) {
        return node.getAttribute("data-hash") || "";
      }).filter(Boolean);
      if (!selected.length) {
        notify("Seleziona almeno un torrent", "err");
        return;
      }
      var bulkAction = bulkButton.getAttribute("data-download-bulk");
      if (bulkAction === "remove" && !confirm("Rimuovere i torrent selezionati dalla sessione? I file restano su disco.")) return;
      var pathFor = function (hash) { return "/api/torrents/" + encodeURIComponent(hash) + "/" + bulkAction; };
      bulkButton.disabled = true;
      Promise.all(selected.map(function (hash) { return api(pathFor(hash), "POST", bulkAction === "remove" ? { delete_files: false } : {}); }))
        .then(function () { load(); notify(selected.length + " torrent aggiornati", "ok"); })
        .catch(function (error) { notify("Azione bulk non riuscita: " + error.message, "err"); })
        .then(function () { bulkButton.disabled = false; });
      return;
    }
    var assignTag = event.target.closest("[data-download-assign-tag]");
    if (assignTag) {
      var tagInput = page.querySelector("[data-download-tag]");
      var tag = tagInput ? tagInput.value.trim() : "";
      var hashes = Array.prototype.map.call(page.querySelectorAll("[data-download-select]:checked"), function (node) {
        return node.getAttribute("data-hash") || "";
      }).filter(Boolean);
      if (!hashes.length) { notify("Seleziona almeno un torrent", "err"); return; }
      if (!tag) { notify("Inserisci un tag", "err"); return; }
      assignTag.disabled = true;
      Promise.all(hashes.map(function (hash) { return api("/api/torrent-tags", "POST", { hash: hash, tag: tag }); }))
        .then(function () { if (tagInput) tagInput.value = ""; load(); notify("Tag assegnato", "ok"); })
        .catch(function (error) { notify("Tag non assegnato: " + error.message, "err"); })
        .then(function () { assignTag.disabled = false; });
      return;
    }
    if (event.target.closest("[data-download-select-all]")) {
      var selectAll = event.target.closest("[data-download-select-all]");
      Array.prototype.forEach.call(page.querySelectorAll("[data-download-select]"), function (node) { node.checked = selectAll.checked; });
      updateDownloadSelection();
      return;
    }
    var detailButton = event.target.closest("[data-torrent-detail]");
    if (detailButton) {
      var detailPanel = page.querySelector("[data-torrent-detail-panel]");
      var detailOutput = detailPanel && detailPanel.querySelector("[data-torrent-detail-output]");
      if (!detailPanel || !detailOutput) return;
      detailButton.disabled = true;
      api("/api/torrents/" + encodeURIComponent(detailButton.getAttribute("data-hash") || ""), "GET")
        .then(function (data) {
          renderReadable(detailOutput, data);
          var detailHash = detailButton.getAttribute("data-hash") || "";
          Array.prototype.forEach.call(detailPanel.querySelectorAll("[data-torrent-export]"), function (link) {
            link.href = "/api/torrents/" + encodeURIComponent(detailHash) + "/" + link.getAttribute("data-torrent-export");
          });
          detailPanel.hidden = false;
          detailPanel.scrollIntoView({ behavior: "smooth", block: "nearest" });
        })
        .catch(function (error) { notify("Dettagli non disponibili: " + error.message, "err"); })
        .then(function () { detailButton.disabled = false; });
      return;
    }
    var subdetailButton = event.target.closest("[data-torrent-subdetail]");
    if (subdetailButton) {
      var subdetailPanel = page.querySelector("[data-torrent-detail-panel]");
      var subdetailOutput = subdetailPanel && subdetailPanel.querySelector("[data-torrent-subdetail-output]");
      var activeDetail = subdetailPanel && subdetailPanel.querySelector("[data-torrent-export]");
      var activeHref = activeDetail && activeDetail.getAttribute("href") || "";
      var match = activeHref.match(/\/api\/torrents\/([^/]+)\//);
      if (!subdetailPanel || !subdetailOutput || !match) return;
      subdetailButton.disabled = true;
      api("/api/torrents/" + match[1] + "/" + subdetailButton.getAttribute("data-torrent-subdetail"), "GET")
        .then(function (data) {
          subdetailOutput.hidden = false;
          renderReadable(subdetailOutput, data);
        })
        .catch(function (error) { notify("Dati non disponibili: " + error.message, "err"); })
        .then(function () { subdetailButton.disabled = false; });
      return;
    }
    if (event.target.closest("[data-torrent-detail-close]")) {
      var panel = page.querySelector("[data-torrent-detail-panel]");
      if (panel) panel.hidden = true;
      return;
    }
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
        .catch(function (error) { notify("Ciclo non avviato: " + error.message, "err"); })
        .then(function () { element.disabled = false; });
      return;
    }
    if (!hash) return;

    if (action === "remove" && !confirm("Rimuovere il torrent dalla sessione? I file restano su disco.")) {
      return;
    }
    var actions = {
      pause: ["/api/torrents/" + encodeURIComponent(hash) + "/pause", "POST", {}],
      resume: ["/api/torrents/" + encodeURIComponent(hash) + "/resume", "POST", {}],
      recheck: ["/api/torrents/" + encodeURIComponent(hash) + "/recheck", "POST", {}],
      remove: ["/api/torrents/" + encodeURIComponent(hash) + "/remove", "POST", { delete_files: false }]
    };
    var entry = actions[action];
    if (!entry) return;
    element.disabled = true;
    api(entry[0], entry[1], entry[2])
      .then(load)
      .catch(function (error) { notify("Azione non riuscita: " + error.message, "err"); })
      .then(function () { element.disabled = false; });
  });

  function updateDownloadSelection() {
    var count = page.querySelectorAll("[data-download-select]:checked").length;
    var label = page.querySelector("[data-download-selected-count]");
    if (label) label.textContent = count + " selezionati";
    var selectAll = page.querySelector("[data-download-select-all]");
    var all = page.querySelectorAll("[data-download-select]");
    if (selectAll) selectAll.checked = all.length > 0 && count === all.length;
  }
  document.addEventListener("change", function (event) {
    if (event.target.closest("[data-download-select]")) updateDownloadSelection();
  });

  document.addEventListener("visibilitychange", schedule);
  schedule();

  // ---- add/upload torrent --------------------------------------------------
  function uploadTorrent(file, form) {
    var headers = { "Content-Type": "application/octet-stream" };
    var value = token();
    if (value) headers["X-Gextto-Token"] = value;
    var uploadURL = "/api/upload-torrent";
    var savePath = form && form.querySelector("[data-torrent-save-path]");
    if (savePath && savePath.value.trim()) uploadURL += "?save_path=" + encodeURIComponent(savePath.value.trim());
    return file.arrayBuffer().then(function (buffer) {
      return fetch(uploadURL, {
        method: "POST", headers: headers, body: buffer, credentials: "same-origin"
      });
    }).then(function (response) {
      if (response.status === 401 && !tokenPrompted) {
        tokenPrompted = true;
        var entered = window.prompt("Token API Gextto (vuoto per annullare):");
        if (entered) {
          try { localStorage.setItem("gextto_api_token", entered); } catch (error) { /* ignore */ }
          return uploadTorrent(file, form);
        }
      }
      return handleResponse(response);
    });
  }
  Array.prototype.forEach.call(document.querySelectorAll("[data-torrent-add]"), function (form) {
    var message = form.querySelector("[data-torrent-message]");
    var upload = form.querySelector("[data-torrent-upload]");
    if (upload) upload.addEventListener("change", function () {
      var file = upload.files && upload.files[0];
      if (!file) return;
      if (message) message.textContent = "Caricamento…";
      upload.disabled = true;
      uploadTorrent(file, form).then(function () {
        if (message) message.textContent = "Torrent caricato";
        if (partials[view]) load();
      }).catch(function (error) {
        if (message) message.textContent = error.message;
        notify("Upload non riuscito: " + error.message, "err");
      }).then(function () { upload.disabled = false; });
    });
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      var magnet = form.querySelector("[data-torrent-magnet]");
      var value = magnet ? magnet.value.trim() : "";
      if (!value) {
        if (message) message.textContent = "Inserisci un magnet o un URL";
        return;
      }
      var option = function (name) {
        var node = form.querySelector('[data-torrent-option="' + name + '"]');
        return !!(node && node.checked);
      };
      var savePath = form.querySelector("[data-torrent-save-path]");
      var body = {
        magnet: value,
        save_path: savePath ? savePath.value.trim() : "",
        start_paused: !option("start"),
        no_rename: option("no_rename"),
        sequential: option("sequential"),
        seed_mode: option("seed_mode"),
        queue_top: option("queue_top"),
        first_last: option("first_last"),
        stop_at_metadata: option("metadata_only"),
        preallocate: option("preallocate")
      };
      var button = form.querySelector("button[type=submit]");
      if (button) button.disabled = true;
      api("/api/send-magnet", "POST", body).then(function () {
        if (message) message.textContent = "Torrent accodato";
        if (magnet) magnet.value = "";
        if (partials[view]) load();
      }).catch(function (error) {
        if (message) message.textContent = error.message;
        notify("Torrent non aggiunto: " + error.message, "err");
      }).then(function () { if (button) button.disabled = false; });
    });
  });

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
    }
    if (typeof value === "object") {
      return value.name || value.title || value.label || value.path || "—";
    }
    return String(value);
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
              // Some APIs return scalar arrays (for example download tags)
              // rather than objects. An empty column key means "the item".
              var rawValue = column.key === "" ? row : row[column.key];
              var value = fmt(rawValue, column.format);
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
        notify("Salvataggio non riuscito: " + error.message, "err");
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
        .catch(function (error) { notify("Cambio lingua non riuscito: " + error.message, "err"); });
    });
    i18nBody.addEventListener("click", function (event) {
      var button = event.target.closest("[data-i18n-save]");
      if (!button) return;
      var key = button.getAttribute("data-i18n-save");
      var input = i18nBody.querySelector('[data-i18n-key="' + key.replace(/(["\\])/g, "\\$1") + '"]');
      button.disabled = true;
      api("/api/i18n", "POST", { lang: langSelect.value, key: key, value: input ? input.value : "" })
        .then(function () { if (i18nMessage) i18nMessage.textContent = "Salvato"; })
        .catch(function (error) { notify("Salvataggio non riuscito: " + error.message, "err"); })
        .then(function () { button.disabled = false; });
    });
    var i18nImport = i18nEditor.querySelector("[data-i18n-import]");
    if (i18nImport) i18nImport.addEventListener("click", function () {
      var area = i18nEditor.querySelector("[data-i18n-yaml]");
      i18nImport.disabled = true;
      api("/api/i18n/import/" + encodeURIComponent(langSelect.value), "POST", { yaml: area ? area.value : "" })
        .then(function () { if (i18nMessage) i18nMessage.textContent = "Importato"; loadI18n(); })
        .catch(function (error) { notify("Import non riuscito: " + error.message, "err"); })
        .then(function () { i18nImport.disabled = false; });
    });
    var i18nDelete = i18nEditor.querySelector("[data-i18n-delete]");
    if (i18nDelete) i18nDelete.addEventListener("click", function () {
      if (!confirm("Eliminare tutte le traduzioni della lingua " + langSelect.value + "?")) return;
      api("/api/i18n/" + encodeURIComponent(langSelect.value), "DELETE", {}).then(function () { loadI18n(); })
        .catch(function (error) { notify("Eliminazione non riuscita: " + error.message, "err"); });
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
      if (!postURL) { notify("Inserisci l'URL del post GetComics", "err"); return; }
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
      .catch(function (error) { notify("Azione non riuscita: " + error.message, "err"); })
      .then(function () { element.disabled = false; });
  });

  // ---- settings forms -----------------------------------------------------
  Array.prototype.forEach.call(document.querySelectorAll("[data-setting-key]"), function (form) {
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      var key = form.getAttribute("data-setting-key");
      var input = form.querySelector("[data-setting-input]");
      if (!input) return; // structured value, edited from its dedicated section
      var button = form.querySelector("button");
      var status = form.querySelector("[data-setting-status]");
      var secret = input.type === "password";
      var value = input.value;
      var tagJSON = input.getAttribute("data-tag-json");
      if (tagJSON !== null) {
        var items = value.split("\n").map(function (line) { return line.trim(); })
          .filter(function (line) { return line !== ""; });
        value = tagJSON === "true" ? JSON.stringify(items) : items.join(", ");
      }
      if (secret && value === "") {
        if (button) {
          button.textContent = "Inserisci un valore";
          setTimeout(function () { button.textContent = "Salva"; }, 1500);
        }
        if (status) status.textContent = "Inserisci un valore";
        return;
      }
      if (button) button.disabled = true;
      api("/api/config/settings", "POST", { key: key, value: value })
        .then(function () {
          if (status) status.textContent = "Salvato";
          if (!button) return;
          button.textContent = "Salvato";
          setTimeout(function () {
            button.textContent = "Salva";
            button.disabled = false;
            if (status) status.textContent = "";
          }, 1500);
        })
        .catch(function (error) {
          notify("Salvataggio non riuscito: " + error.message, "err");
          if (status) status.textContent = "Errore";
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
        notify("Non accodata: " + error.message, "err");
        button.disabled = false;
      });
    });
  }
  Array.prototype.forEach.call(document.querySelectorAll("[data-ui-search-post]"), renderSearch);

  // ---- shell: navigation, theme, font scale, language, live metrics --------
  document.addEventListener("click", function (event) {
    var nav = event.target.closest("[data-nav]");
    if (nav) {
      location.href = "/?view=" + encodeURIComponent(nav.getAttribute("data-nav"));
      return;
    }
    var navMore = event.target.closest("[data-nav-more]");
    if (navMore) {
      var group = navMore.closest(".nav-group");
      if (group) {
        group.classList.toggle("open");
        var icon = navMore.querySelector("[data-nav-more-icon]");
        if (icon) icon.textContent = group.classList.contains("open") ? "▲" : "▾";
      }
      return;
    }
    var navSearch = event.target.closest("[data-nav-search]");
    if (navSearch) {
      location.href = "/?view=settings";
      return;
    }
    var font = event.target.closest("[data-font]");
    if (font) {
      setFontScale(readFontScale() + parseInt(font.getAttribute("data-font"), 10));
      return;
    }
    var theme = event.target.closest("[data-theme-toggle]");
    if (theme) {
      setTheme(document.documentElement.getAttribute("data-theme") === "light" ? "dark" : "light");
      return;
    }
  });

  // Cycle buttons with a domain (Dashboard quick actions).
  document.addEventListener("click", function (event) {
    var button = event.target.closest("[data-cycle]");
    if (!button) return;
    var domain = button.getAttribute("data-cycle") || "full";
    var path = domain === "full" ? "/api/run_now" : "/api/run_now?domain=" + encodeURIComponent(domain);
    button.disabled = true;
    api(path, "POST", {}).then(function () {
      button.textContent = "Avviato";
    }).catch(function (error) {
      notify("Ciclo non avviato: " + error.message, "err");
    }).then(function () { button.disabled = false; });
  });

  function storageGet(key) {
    try { return localStorage.getItem(key); } catch (error) { return null; }
  }
  function storageSet(key, value) {
    try { localStorage.setItem(key, value); } catch (error) { /* ignore */ }
  }
  function readFontScale() {
    var value = parseInt(storageGet("gextto_font_scale") || "100", 10);
    if (!isFinite(value)) value = 100;
    return Math.min(140, Math.max(85, value));
  }
  function setFontScale(percent) {
    percent = Math.min(140, Math.max(85, percent));
    document.documentElement.style.fontSize = (16 * percent / 100) + "px";
    var label = document.querySelector("[data-font-label]");
    if (label) label.textContent = "Testo " + percent + "%";
    storageSet("gextto_font_scale", String(percent));
  }
  function setTheme(mode) {
    document.documentElement.setAttribute("data-theme", mode);
    var button = document.querySelector("[data-theme-toggle]");
    if (button) button.textContent = mode === "light" ? "Tema scuro" : "Tema chiaro";
    storageSet("gextto_theme", mode);
  }
  setFontScale(readFontScale());
  setTheme(storageGet("gextto_theme") === "light" ? "light" : "dark");
  var langSelect = document.querySelector("[data-lang]");
  if (langSelect) langSelect.addEventListener("change", function () {
    api("/api/i18n/active", "POST", { lang: langSelect.value })
      .then(function () { location.reload(); })
      .catch(function (error) { notify("Cambio lingua non riuscito: " + error.message, "err"); });
  });

  function humanRate(value) {
    var n = Number(value);
    if (!isFinite(n) || n <= 0) return "0 B/s";
    return humanBytes(n) + "/s";
  }
  function humanDuration(seconds) {
    var total = Math.max(0, Math.floor(Number(seconds) || 0));
    var hours = Math.floor(total / 3600);
    var minutes = Math.floor((total % 3600) / 60);
    var secs = total % 60;
    if (hours > 0) return hours + "h " + minutes + "m";
    if (minutes > 0) return minutes + "m " + secs + "s";
    return secs + "s";
  }
  function setMetric(name, value) {
    var node = document.querySelector('[data-metric="' + name + '"]');
    if (node) node.textContent = value;
  }
  function refreshShellMetrics() {
    api("/api/process-metrics", "GET").then(function (data) {
      var cpu = data && data.process_cpu_percent;
      setMetric("cpu", cpu == null ? "—" : Number(cpu).toFixed(1) + "%");
      if (data && data.resident_bytes != null) setMetric("ram", humanBytes(data.resident_bytes));
    }).catch(function () { /* keep the previous value */ });
    api("/api/torrents", "GET").then(function (items) {
      var list = Array.isArray(items) ? items : [];
      var dl = 0, ul = 0, peers = 0, seeds = 0;
      list.forEach(function (item) {
        dl += Number(item.download_rate) || 0;
        ul += Number(item.upload_rate) || 0;
        peers += Number(item.num_peers) || 0;
        seeds += Number(item.num_seeds) || 0;
      });
      setMetric("dl", humanRate(dl));
      setMetric("ul", humanRate(ul));
      setMetric("count", String(list.length));
      setMetric("peers", peers + "/" + seeds);
    }).catch(function () { /* keep the previous value */ });
    api("/api/status", "GET").then(function (data) {
      if (!data || !data.next_cycle_at) { setMetric("cycle", "—"); return; }
      var remaining = (Date.parse(data.next_cycle_at) - Date.now()) / 1000;
      setMetric("cycle", humanDuration(remaining));
    }).catch(function () { /* keep the previous value */ });
  }
  refreshShellMetrics();
  setInterval(function () {
    if (document.visibilityState === "visible") refreshShellMetrics();
  }, 4000);

  // ---- sources editor (feeds) ---------------------------------------------
  // Feeds live in the `url` setting and are edited as one URL per line.
  var sourcesEditor = document.querySelector("[data-sources-editor]");
  if (sourcesEditor) {
    var feedsInput = sourcesEditor.querySelector("[data-sources-feeds]");
    var sourcesMessage = sourcesEditor.querySelector("[data-sources-message]");
    api("/api/config", "GET").then(function (config) {
      config = config || {};
      if (feedsInput) feedsInput.value = (config.feed_urls || []).join("\n");
    }).catch(function (error) { if (sourcesMessage) sourcesMessage.textContent = error.message; });
    var saveSources = sourcesEditor.querySelector("[data-sources-save]");
    if (saveSources) saveSources.addEventListener("click", function () {
      var feeds = ((feedsInput && feedsInput.value) || "").split("\n").map(function (line) {
        return line.trim();
      }).filter(function (line) { return line !== ""; });
      saveSources.disabled = true;
      api("/api/config/settings", "POST", { key: "url", value: JSON.stringify(feeds) }).then(function () {
        if (sourcesMessage) sourcesMessage.textContent = "Feed salvati";
      }).catch(function (error) {
        notify("Salvataggio non riuscito: " + error.message, "err");
      }).then(function () { saveSources.disabled = false; });
    });
  }

  // ---- structured list editors --------------------------------------------
  // Real form rows, never raw JSON: indexers, tag-to-folder rules, event hooks,
  // watched folders and source filters.
  function listFieldValue(field) {
    var kind = field.getAttribute("data-kind");
    if (kind === "bool") return field.value === "true";
    if (kind === "number") return Number(field.value || 0);
    if (kind === "tags") {
      return (field.value || "").split(",").map(function (part) { return part.trim(); })
        .filter(function (part) { return part !== ""; });
    }
    return field.value;
  }
  function fillListRow(row, item) {
    Array.prototype.forEach.call(row.querySelectorAll("[data-field]"), function (field) {
      var name = field.getAttribute("data-field");
      var value = item ? item[name] : undefined;
      if (field.getAttribute("data-kind") === "bool") {
        field.value = (value === false || value === "false") ? "false" : "true";
      } else if (field.getAttribute("data-kind") === "tags") {
        field.value = Array.isArray(value) ? value.join(", ") : (value || "");
      } else {
        field.value = (value === undefined || value === null) ? "" : value;
      }
    });
  }
  Array.prototype.forEach.call(document.querySelectorAll("[data-list-editor]"), function (editor) {
    var rowsBox = editor.querySelector("[data-list-rows]");
    var template = editor.querySelector("[data-list-row]");
    var message = editor.querySelector("[data-list-message]");
    var unwrap = editor.getAttribute("data-unwrap") || "";
    var wrap = editor.getAttribute("data-wrap") || "";
    var postKey = editor.getAttribute("data-post-key") || "";
    function addRow(item) {
      var clone = template.content.firstElementChild.cloneNode(true);
      fillListRow(clone, item || {});
      var remove = clone.querySelector("[data-list-remove]");
      if (remove) remove.addEventListener("click", function () { clone.remove(); });
      rowsBox.appendChild(clone);
      return clone;
    }
    api(editor.getAttribute("data-get"), "GET").then(function (data) {
      var payload = (unwrap && data && data[unwrap] !== undefined) ? data[unwrap] : data;
      var items = Array.isArray(payload) ? payload : [];
      if (!items.length) { addRow({}); return; }
      items.forEach(function (item) { addRow(item); });
    }).catch(function (error) {
      if (message) message.textContent = error.message;
      addRow({});
    });
    var addButton = editor.querySelector("[data-list-add]");
    if (addButton) addButton.addEventListener("click", function () { addRow({}); });
    var saveButton = editor.querySelector("[data-list-save]");
    if (saveButton) saveButton.addEventListener("click", function () {
      var items = Array.prototype.map.call(rowsBox.children, function (row) {
        var item = {};
        Array.prototype.forEach.call(row.querySelectorAll("[data-field]"), function (field) {
          item[field.getAttribute("data-field")] = listFieldValue(field);
        });
        return item;
      });
      var encoded = JSON.stringify(items);
      var request;
      if (postKey) {
        request = api(editor.getAttribute("data-post"), "POST", { key: postKey, value: encoded });
      } else if (wrap) {
        var body = {};
        body[wrap] = items;
        request = api(editor.getAttribute("data-post"), "POST", body);
      } else {
        request = api(editor.getAttribute("data-post"), "POST", items);
      }
      saveButton.disabled = true;
      request.then(function () {
        if (message) message.textContent = "Salvato";
      }).catch(function (error) {
        notify("Salvataggio non riuscito: " + error.message, "err");
      }).then(function () { saveButton.disabled = false; });
    });
  });

  // ---- series edit form (structured fields, no JSON) ----------------------
  Array.prototype.forEach.call(document.querySelectorAll("[data-series-edit]"), function (form) {
    var name = form.getAttribute("data-series-name");
    var message = form.querySelector("[data-form-message]");
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      api("/api/config/library", "GET").then(function (library) {
        var series = (library && library.series) || [];
        var found = false;
        series = series.map(function (item) {
          if (item.name !== name) return item;
          found = true;
          item.seasons = form.querySelector("[name=seasons]").value;
          item.quality = form.querySelector("[name=quality]").value;
          item.language = form.querySelector("[name=language]").value;
          item.archive_path = form.querySelector("[name=archive_path]").value;
          item.exclude = form.querySelector("[name=exclude]").value;
          return item;
        });
        if (!found) { notify("Serie non trovata", "err"); return; }
        return api("/api/config/series", "POST", series).then(function () {
          if (message) message.textContent = "Salvata";
        });
      }).catch(function (error) {
        notify("Salvataggio non riuscito: " + error.message, "err");
      });
    });
  });

  // ---- settings search ----------------------------------------------------
  var settingsView = document.querySelector("[data-settings-index]");
  if (settingsView) {
    var settingsIndex = [];
    try { settingsIndex = JSON.parse(settingsView.getAttribute("data-settings-index") || "[]"); }
    catch (error) { settingsIndex = []; }
    var searchInput = settingsView.querySelector("[data-settings-search]");
    var tabSelect = settingsView.querySelector("[data-settings-tab-select]");
    var resultsBox = settingsView.querySelector("[data-settings-results]");
    var settingsBody = settingsView.querySelector("[data-settings-body]");
    if (tabSelect) tabSelect.addEventListener("change", function () {
      location.href = "/?view=settings&tab=" + encodeURIComponent(tabSelect.value);
    });
    if (searchInput) searchInput.addEventListener("input", function () {
      var query = (searchInput.value || "").trim().toLowerCase();
      var cards = settingsBody ? settingsBody.querySelectorAll("[data-setting-key]") : [];
      Array.prototype.forEach.call(cards, function (card) {
        var key = (card.getAttribute("data-setting-key") || "").toLowerCase();
        var labelNode = card.querySelector(".setting-label");
        var label = (labelNode && labelNode.textContent || "").toLowerCase();
        var match = query === "" || key.indexOf(query) >= 0 || label.indexOf(query) >= 0;
        card.style.display = match ? "" : "none";
      });
      if (!resultsBox) return;
      if (query.length < 2) {
        resultsBox.hidden = true;
        resultsBox.innerHTML = "";
        return;
      }
      var matches = settingsIndex.filter(function (entry) {
        return (entry.label || "").toLowerCase().indexOf(query) >= 0 ||
          (entry.key || "").toLowerCase().indexOf(query) >= 0;
      }).slice(0, 30);
      resultsBox.hidden = false;
      if (!matches.length) {
        resultsBox.innerHTML = '<p class="muted">Nessuna impostazione trovata.</p>';
        return;
      }
      resultsBox.innerHTML = matches.map(function (entry) {
        var href = "/?view=settings&amp;tab=" + encodeURIComponent(entry.tab);
        if (entry.key) href += "#setting-" + encodeURIComponent(entry.key);
        return '<a class="settings-result" href="' + href + '">' + esc(entry.label) +
          (entry.key ? " <code>" + esc(entry.key) + "</code>" : "") + "</a>";
      }).join("");
    });
    if (location.hash && location.hash.indexOf("#setting-") === 0) {
      var target = document.getElementById(location.hash.slice(1));
      if (target) {
        var field = target.querySelector("[data-setting-input]");
        if (field) field.focus();
      }
    }
  }

  // ---- readable rendering (never JSON) ------------------------------------
  function renderReadable(container, value) {
    container.innerHTML = "";
    function makeNode(item) {
      if (item === null || item === undefined) return document.createTextNode("—");
      if (typeof item === "boolean") return document.createTextNode(item ? "Sì" : "No");
      if (typeof item === "number") return document.createTextNode(String(item));
      if (typeof item === "string") {
        if (/^https?:\/\//i.test(item)) {
          var link = document.createElement("a");
          link.href = item;
          link.target = "_blank";
          link.rel = "noopener";
          link.textContent = item;
          return link;
        }
        return document.createTextNode(item);
      }
      if (Array.isArray(item)) {
        var list = document.createElement("ul");
        list.className = "kv-list";
        item.forEach(function (entry) {
          var li = document.createElement("li");
          li.appendChild(makeNode(entry));
          list.appendChild(li);
        });
        return list;
      }
      var dl = document.createElement("dl");
      dl.className = "kv";
      Object.keys(item).forEach(function (key) {
        var dt = document.createElement("dt");
        dt.textContent = key;
        var dd = document.createElement("dd");
        dd.appendChild(makeNode(item[key]));
        dl.appendChild(dt);
        dl.appendChild(dd);
      });
      return dl;
    }
    container.appendChild(makeNode(value));
  }

  function renderCalendarList(container, items) {
    container.innerHTML = "";
    if (!items.length) {
      container.innerHTML = '<p class="muted">Nessuna uscita in programma.</p>';
      return;
    }
    items.slice(0, 6).forEach(function (item) {
      var episode = item && item.episode || {};
      var row = document.createElement("div");
      row.className = "list-item";
      var poster = document.createElement("div");
      poster.className = "list-poster placeholder";
      poster.textContent = "N/D";
      var posterURL = item && typeof item.poster === "string" ? item.poster : "";
      if (/^https?:\/\//i.test(posterURL)) {
        var image = document.createElement("img");
        image.className = "list-poster";
        image.src = posterURL;
        image.alt = String(item.series || "Serie");
        image.loading = "lazy";
        poster = image;
      }
      var text = document.createElement("div");
      var title = document.createElement("strong");
      title.textContent = String(item.series || "Serie");
      var detail = document.createElement("small");
      detail.textContent = "S" + String(episode.season_number || "—") +
        "E" + String(episode.episode_number || "—") + " · " + String(episode.air_date || "—");
      text.appendChild(title);
      text.appendChild(detail);
      row.appendChild(poster);
      row.appendChild(text);
      container.appendChild(row);
    });
  }

  var calendarList = document.querySelector("[data-dashboard-calendar-list]");
  if (calendarList) {
    api("/api/calendar", "GET").then(function (data) {
      renderCalendarList(calendarList, data && Array.isArray(data.items) ? data.items : []);
    }).catch(function (error) {
      calendarList.innerHTML = '<p class="muted">' + esc(error.message) + "</p>";
    });
  }

  function renderRecentList(container, items) {
    container.innerHTML = "";
    if (!items.length) {
      container.innerHTML = '<p class="muted">Nessun download recente.</p>';
      return;
    }
    items.slice(0, 6).forEach(function (item) {
      var isSeries = item && item.kind === "series";
      var name = String(item && item.name || "—");
      var label = isSeries
        ? name + " S" + String(item.season || "—") + "E" + String(item.episode || "—")
        : name;
      var detail = isSeries
        ? "S" + String(item.season || "—") + "E" + String(item.episode || "—") + " · "
        : "";
      detail += String(item && item.downloaded_at || "—") + " · " + humanBytes(item && item.size_bytes);
      var row = document.createElement("div");
      row.className = "list-item";
      var poster = document.createElement("div");
      poster.className = "list-poster placeholder";
      poster.textContent = "N/D";
      var posterURL = item && typeof item.poster === "string" ? item.poster : "";
      if (/^https?:\/\//i.test(posterURL)) {
        var image = document.createElement("img");
        image.className = "list-poster";
        image.src = posterURL;
        image.alt = name;
        image.loading = "lazy";
        poster = image;
      }
      var text = document.createElement("div");
      var title = document.createElement("strong");
      title.textContent = label;
      var small = document.createElement("small");
      small.textContent = detail;
      text.appendChild(title);
      text.appendChild(small);
      row.appendChild(poster);
      row.appendChild(text);
      container.appendChild(row);
    });
  }

  var recentList = document.querySelector("[data-dashboard-recent-list]");
  if (recentList) {
    api("/api/recent-downloads", "GET").then(function (data) {
      renderRecentList(recentList, data && Array.isArray(data.items) ? data.items : []);
    }).catch(function (error) {
      recentList.innerHTML = '<p class="muted">' + esc(error.message) + "</p>";
    });
  }

  function renderReleaseResults(container, items) {
    container.innerHTML = "";
    var table = document.createElement("table");
    table.className = "data-table";
    table.innerHTML = "<thead><tr><th>Titolo</th><th>Seed</th><th>Dimensione</th><th></th></tr></thead>";
    var tbody = document.createElement("tbody");
    items.forEach(function (release) {
      var row = document.createElement("tr");
      row.innerHTML = '<td class="truncate">' + esc(release.title || "—") + '</td><td class="numeric">' +
        esc(String(release.seeders || 0)) + '</td><td class="numeric">' + humanBytes(release.size_bytes) + "</td>";
      var cell = document.createElement("td");
      var button = document.createElement("button");
      button.className = "btn sm primary";
      button.textContent = "Accoda";
      button.addEventListener("click", function () {
        button.disabled = true;
        api("/api/search/add", "POST", { release: release }).then(function () {
          button.textContent = "Accodata";
        }).catch(function (error) {
          notify("Non accodata: " + error.message, "err");
          button.disabled = false;
        });
      });
      cell.appendChild(button);
      row.appendChild(cell);
      tbody.appendChild(row);
    });
    table.appendChild(tbody);
    container.appendChild(table);
  }

  function renderTmdbResults(container, items, form) {
    container.innerHTML = "";
    var kindField = form ? form.querySelector("[name=kind]") : null;
    var kind = kindField ? kindField.value : "series";
    var table = document.createElement("table");
    table.className = "data-table";
    table.innerHTML = "<thead><tr><th>Titolo</th><th>Anno</th><th>ID</th><th></th></tr></thead>";
    var tbody = document.createElement("tbody");
    items.forEach(function (item) {
      var title = item.name || item.title || "—";
      var year = String(item.first_air_date || item.release_date || "").slice(0, 4);
      var id = item.id || item.tmdb_id || item.tvdb_id || "";
      var row = document.createElement("tr");
      row.innerHTML = "<td>" + esc(title) + "</td><td>" + esc(year) + "</td><td>" + esc(String(id)) + "</td>";
      var cell = document.createElement("td");
      var button = document.createElement("button");
      button.className = "btn sm primary";
      button.textContent = "Aggiungi";
      button.addEventListener("click", function () {
        button.disabled = true;
        api("/api/tmdb/add", "POST", { kind: kind, name: title, year: year, tmdb_id: String(id) })
          .then(function () { button.textContent = "Aggiunto"; })
          .catch(function (error) { notify("Aggiunta non riuscita: " + error.message, "err"); button.disabled = false; });
      });
      cell.appendChild(button);
      row.appendChild(cell);
      tbody.appendChild(row);
    });
    table.appendChild(tbody);
    container.appendChild(table);
  }

  // ---- generic JSON forms (section forms) ---------------------------------
  Array.prototype.forEach.call(document.querySelectorAll("[data-json-form]"), function (form) {
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      var body = {};
      Array.prototype.forEach.call(form.querySelectorAll("[name]"), function (field) {
        var value = field.value;
        if (field.type === "number") value = Number(value);
        // boolField renders a select for APIs whose JSON contract uses real
        // booleans (weekly/comics and provider settings), not string values.
        if (value === "true") value = true;
        if (value === "false") value = false;
        body[field.name] = value;
      });
      var button = form.querySelector("button[type=submit]");
      var message = form.querySelector("[data-form-message]");
      var output = form.querySelector("[data-form-output]");
      if (button) button.disabled = true;
      var wrap = form.getAttribute("data-wrap");
      var payload = wrap ? (function () { var value = {}; value[wrap] = body; return value; }()) : body;
      api(form.getAttribute("data-endpoint"), form.getAttribute("data-method") || "POST", payload).then(function (data) {
        if (message) message.textContent = "Fatto";
        if (output && data !== undefined && data !== null && data !== "") {
          output.hidden = false;
          if (form.getAttribute("data-render") === "tmdb" && data && Array.isArray(data.items)) {
            renderTmdbResults(output, data.items, form);
          } else if (form.getAttribute("data-render") === "releases" && data && Array.isArray(data.results)) {
            renderReleaseResults(output, data.results);
          } else {
            renderReadable(output, data);
          }
        }
      }).catch(function (error) {
        notify("Operazione non riuscita: " + error.message, "err");
      }).then(function () { if (button) button.disabled = false; });
    });
  });

  // ---- progress polling ---------------------------------------------------
  Array.prototype.forEach.call(document.querySelectorAll("[data-progress]"), function (node) {
    var endpoint = node.getAttribute("data-endpoint");
    var panel = node.closest(".panel");
    var bar = panel.querySelector("[data-progress-bar]");
    var text = panel.querySelector("[data-progress-text]");
    function poll() {
      api(endpoint, "GET").then(function (data) {
        var progress = data && data.progress ? data.progress : data;
        if (!progress) return;
        var total = Number(progress.total) || 0;
        var current = Number(progress.current) || 0;
        var pct = total > 0 ? Math.min(100, Math.round(current / total * 100)) : (progress.running ? 0 : 100);
        if (bar) bar.style.width = pct + "%";
        if (text) {
          text.textContent = (progress.running ? "in corso" : "inattivo") +
            (progress.series ? " · " + progress.series : "") + " · " + current + "/" + total +
            (progress.errors ? " · errori " + progress.errors : "") +
            (progress.message ? " · " + progress.message : "");
        }
      }).catch(function () { /* keep the previous value */ });
    }
    poll();
    setInterval(poll, 3000);
  });

  // ---- OAuth / PIN flows (Trakt, Simkl) -----------------------------------
  document.addEventListener("click", function (event) {
    var start = event.target.closest("[data-oauth-start]");
    if (start) {
      var panel = start.closest(".panel");
      var output = panel.querySelector("[data-oauth-output]");
      start.disabled = true;
      api(start.getAttribute("data-oauth-start"), "POST", {})
        .then(function (data) { if (output) renderReadable(output, data); })
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
      if (!code) { notify("Inserisci il codice di accesso", "err"); return; }
      poll.disabled = true;
      api(poll.getAttribute("data-oauth-poll"), "POST", { code: code })
        .then(function (data) { if (output2) renderReadable(output2, data); })
        .catch(function (error) { if (output2) output2.textContent = error.message; })
        .then(function () { poll.disabled = false; });
      return;
    }
  });
})();
