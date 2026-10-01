/* Gextto UI v2 — the only retained client script.
 *
 * HTMX does the requests and swaps; this file only covers the few things that
 * genuinely need the browser: theme and font preferences (localStorage + CSS
 * variables) and an accessible dialog contract for the server-rendered modals
 * (focus trap, Escape, click on the backdrop, focus restore).
 *
 * Keep it dependency-free and small. Everything else is server-side.
 */
(function () {
  "use strict";

  var doc = document.documentElement;

  function storageGet(key) {
    try { return window.localStorage.getItem(key); } catch (error) { return null; }
  }
  function storageSet(key, value) {
    try { window.localStorage.setItem(key, value); } catch (error) { /* storage may be unavailable */ }
  }

  // ---------------------------------------------------------------- theme --
  function themeLabel() {
    return doc.getAttribute("data-theme") === "light" ? "Tema scuro" : "Tema chiaro";
  }
  function setTheme(mode) {
    doc.setAttribute("data-theme", mode);
    storageSet("gextto_theme", mode);
    var button = document.querySelector("[data-theme-toggle]");
    if (button) button.textContent = themeLabel();
  }
  function toggleTheme() {
    setTheme(doc.getAttribute("data-theme") === "light" ? "dark" : "light");
  }
  setTheme(storageGet("gextto_theme") === "light" ? "light" : "dark");

  // ----------------------------------------------------------- font scale --
  function readScale() {
    var value = parseInt(storageGet("gextto_font_scale") || "100", 10);
    if (!isFinite(value)) value = 100;
    return Math.min(140, Math.max(85, value));
  }
  function setScale(percent) {
    percent = Math.min(140, Math.max(85, percent));
    doc.style.fontSize = (17 * percent / 100) + "px";
    storageSet("gextto_font_scale", String(percent));
    var labels = document.querySelectorAll("[data-font-label]");
    for (var i = 0; i < labels.length; i++) labels[i].textContent = "Testo " + percent + "%";
  }
  setScale(readScale());

  // ---------------------------------------------------------- font family --
  var presets = {
    system: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, system-ui, sans-serif',
    sans: "sans-serif",
    serif: "serif",
    mono: "monospace"
  };
  function readFamily() {
    var value = storageGet("gextto_font_family") || "system";
    if (presets[value] || value.indexOf("local:") === 0) return value;
    return "system";
  }
  function fontFamilyCSS(value) {
    if (presets[value]) return presets[value];
    if (value.indexOf("local:") === 0 && value.length > 6) {
      var name = value.slice(6).slice(0, 160).replace(/[\\"]/g, "").replace(/[\r\n]/g, " ");
      return '"' + name + '", system-ui, sans-serif';
    }
    return presets.system;
  }
  function setFamily(value, persist) {
    if (!presets[value] && value.indexOf("local:") !== 0) value = "system";
    doc.style.setProperty("--ui-font-family", fontFamilyCSS(value));
    if (persist !== false) storageSet("gextto_font_family", value);
  }
  setFamily(readFamily(), false);

  // -------------------------------------------------------------- dialogs --
  var activeDialog = null;
  var activeOpener = null;

  function focusables(root) {
    var nodes = root.querySelectorAll(
      'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])'
    );
    return Array.prototype.filter.call(nodes, function (node) {
      return !node.hidden && (node.offsetWidth || node.offsetHeight || node.getClientRects().length);
    });
  }
  function openDialog(overlay, opener) {
    activeDialog = overlay;
    activeOpener = opener || document.activeElement;
    var list = focusables(overlay);
    var target = list[0] || overlay;
    if (!target.matches("input,select,textarea,button,a") && !target.hasAttribute("tabindex")) target.tabIndex = -1;
    window.setTimeout(function () { try { target.focus(); } catch (error) { /* ignore */ } }, 0);
  }
  function restoreFocus() {
    if (activeOpener && document.contains(activeOpener) && !activeOpener.disabled) {
      try { activeOpener.focus(); } catch (error) { /* ignore */ }
    }
    activeOpener = null;
  }
  function closeDialog() {
    var modal = document.getElementById("v2-modal");
    if (modal) modal.innerHTML = "";
    activeDialog = null;
    restoreFocus();
  }
  function scanModal() {
    var modal = document.getElementById("v2-modal");
    if (!modal) return;
    var overlay = modal.querySelector(".overlay");
    if (overlay) {
      if (overlay !== activeDialog) openDialog(overlay, document.activeElement);
      return;
    }
    if (activeDialog) {
      activeDialog = null;
      restoreFocus();
    }
  }

  document.addEventListener("keydown", function (event) {
    if (!activeDialog || !document.contains(activeDialog)) return;
    if (event.key === "Escape") {
      event.preventDefault();
      if (activeDialog.id === "v2-browse-overlay") { closeFolderBrowser(); return; }
      if (activeDialog.id === "v2-font-overlay") { closeFontPicker(); return; }
      closeDialog();
      return;
    }
    if (event.key !== "Tab") return;
    var list = focusables(activeDialog);
    if (!list.length) { event.preventDefault(); return; }
    var first = list[0];
    var last = list[list.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  }, true);

  document.addEventListener("click", function (event) {
    if (!activeDialog) return;
    if (event.target === activeDialog && activeDialog.id === "v2-browse-overlay") { closeFolderBrowser(); return; }
    if (event.target === activeDialog && activeDialog.id !== "v2-font-overlay") { closeDialog(); return; }
    if (event.target === activeDialog && activeDialog.id === "v2-font-overlay") closeFontPicker();
  }, true);

  function updateTorrentSelection() {
    var selected = document.querySelectorAll("[data-v2-select]:checked");
    var label = document.querySelector("[data-v2-selected-count]");
    if (label) label.textContent = selected.length + " selezionati · Azioni:";
    var all = document.querySelector("[data-v2-select-all]");
    var rows = document.querySelectorAll("[data-v2-select]");
    if (all) all.checked = rows.length > 0 && selected.length === rows.length;
  }

  var logsFollow = true;
  function updateLogsFollowButton() {
    var button = document.querySelector("[data-v2-logs-follow]");
    if (button) button.textContent = logsFollow ? "⏸ Ferma scorrimento" : "▶ Segui ultime righe";
  }

  // Every interactive field gets at least a localized native tooltip. Pages
  // rendered by HTMX can add controls after the initial load, so this is
  // deliberately idempotent and runs after every swap. Explicit, explanatory
  // titles remain untouched.
  function ensureTooltips(root) {
    root = root || document;
    var nodes = root.querySelectorAll ? root.querySelectorAll('button,a.btn,input:not([type="hidden"]),select,textarea') : [];
    for (var i = 0; i < nodes.length; i++) {
      var node = nodes[i];
      if (node.disabled) continue;
      // A translated server-side title is more useful than the visible label.
      // If the active language is not Italian and the title was not translated
      // by the catalog, replace it with a label/aria description below rather
      // than exposing an Italian tooltip in another locale.
      if (document.documentElement.lang && document.documentElement.lang !== "it" && !node.hasAttribute("data-v2-title-translated")) {
        node.removeAttribute("title");
      }
      if (String(node.getAttribute("title") || "").trim()) continue;
      var text = node.getAttribute("aria-label") || node.getAttribute("placeholder") || "";
      if (!text) {
        var label = node.closest && node.closest("label");
        var labelText = label && label.querySelector("span:not(.sr-only)");
        text = labelText ? labelText.textContent : (label ? label.textContent : node.textContent);
      }
      text = String(text || "").replace(/\s+/g, " ").trim();
      if (!text) {
        if (node.matches("select")) text = "Seleziona un valore";
        else if (node.matches("textarea,input")) text = "Inserisci un valore";
        else text = "Esegui azione";
      }
      node.setAttribute("title", text);
    }
  }

  // ------------------------------------------------------- dashboard search --
  // Match the classic/rextto layout: results stay below the form, the local
  // archive is rendered first, and a second request fills the same table.
  var dashboardSearchToken = 0;
  function searchBytes(value) {
    var number = Number(value || 0);
    if (!isFinite(number) || number <= 0) return "0 B";
    var units = ["B", "KB", "MB", "GB", "TB"];
    var index = 0;
    while (number >= 1024 && index < units.length - 1) { number /= 1024; index++; }
    return (index ? number.toFixed(1) : Math.round(number)) + " " + units[index];
  }
  function dashboardSearchRequest(path, query) {
    return fetch(path, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: query })
    }).then(function (response) {
      return response.json().then(function (data) {
        if (!response.ok) throw new Error(data.error || "Ricerca non riuscita");
        return data;
      });
    });
  }
  function renderDashboardSearchResults(panel, releases) {
    panel._searchItems = releases || [];
    var root = panel.querySelector("[data-v2-search-body]");
    var filter = panel.querySelector("[data-v2-dashboard-search-filter]");
    var count = panel.querySelector("[data-v2-search-count]");
    var terms = String(filter.value || "").trim().toLowerCase().split(/\s+/).filter(Boolean);
    var visible = terms.length ? panel._searchItems.filter(function (release) {
      var quality = release.quality || {};
      var text = (String(release.title || "") + " " + String(release.source || "") + " " + String(quality.resolution || "") + " " + String(quality.codec || "")).toLowerCase();
      return terms.every(function (term) { return term.charAt(0) === "-" ? text.indexOf(term.slice(1)) < 0 : text.indexOf(term) >= 0; });
    }) : panel._searchItems;
    count.textContent = terms.length ? visible.length + "/" + panel._searchItems.length + " risultati" : panel._searchItems.length + " risultati";
    root.textContent = "";
    if (!releases.length) {
      var emptyRow = document.createElement("tr");
      var emptyCell = document.createElement("td");
      emptyCell.className = "muted";
      emptyCell.colSpan = 4;
      emptyCell.textContent = terms.length ? "Nessun risultato con questo filtro." : "Nessun risultato compatibile.";
      emptyRow.appendChild(emptyCell);
      root.appendChild(emptyRow);
      return;
    }
    if (!visible.length) {
      var noMatch = document.createElement("tr");
      var noMatchCell = document.createElement("td");
      noMatchCell.className = "muted";
      noMatchCell.colSpan = 4;
      noMatchCell.textContent = "Nessun risultato con questo filtro.";
      noMatch.appendChild(noMatchCell);
      root.appendChild(noMatch);
      return;
    }
    visible.forEach(function (release) {
      var row = document.createElement("tr");
      var title = document.createElement("td");
      title.className = "release-title truncate";
      title.title = String(release.title || "");
      title.textContent = String(release.title || "—");
      row.appendChild(title);
      [release.source || "—", release.score || 0].forEach(function (value) {
        var cell = document.createElement("td");
        cell.textContent = String(value);
        row.appendChild(cell);
      });
      var actions = document.createElement("td");
      actions.className = "row-actions";
      var add = document.createElement("button");
      add.className = "btn sm primary";
      add.type = "button";
      add.textContent = "Aggiungi";
      add.title = "Accoda questa release";
      add.addEventListener("click", function () {
        add.disabled = true;
        var form = new URLSearchParams();
        form.set("release", JSON.stringify(release));
        form.set("redirect", "/v2?view=dashboard");
        fetch("/v2/search/add", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/x-www-form-urlencoded" }, body: form.toString() })
          .then(function (response) { if (!response.ok) throw new Error("Impossibile accodare la release"); add.textContent = "Accodata"; })
          .catch(function (error) { add.disabled = false; add.textContent = error.message; });
      });
      actions.appendChild(add);
      var copy = document.createElement("button");
      copy.className = "btn sm";
      copy.type = "button";
      copy.textContent = "Copia";
      copy.title = "Copia il magnet negli appunti";
      copy.addEventListener("click", function () { if (navigator.clipboard && release.magnet) navigator.clipboard.writeText(String(release.magnet)).then(function () { copy.textContent = "Copiato"; }); });
      actions.appendChild(copy);
      row.appendChild(actions);
      root.appendChild(row);
    });
  }
  document.addEventListener("submit", function (event) {
    var form = event.target.closest && event.target.closest("[data-v2-dashboard-search]");
    if (!form) return;
    event.preventDefault();
    var input = form.querySelector("input[name=q]");
    var query = String(input && input.value || "").trim();
    if (!query) return;
    var token = ++dashboardSearchToken;
    var panel = form.closest(".dashboard-search-panel");
    var filter = panel.querySelector("[data-v2-dashboard-search-filter]");
    var status = panel.querySelector("[data-v2-search-status]");
    filter.disabled = false;
    filter.value = "";
    status.textContent = "Ricerca nell’archivio…";
    panel.querySelector("[data-v2-search-body]").innerHTML = '<tr><td class="muted" colspan="4">Ricerca nell’archivio…</td></tr>';
    var fullDone = false;
    dashboardSearchRequest("/api/search/archive", query).then(function (data) {
      if (token !== dashboardSearchToken || fullDone) return;
      var results = data.results || [];
      renderDashboardSearchResults(panel, results);
      status.textContent = "Archivio: " + results.length + " · ricerca RSS, indexer e web in corso…";
    }).catch(function () {});
    dashboardSearchRequest("/api/search/dashboard", query).then(function (data) {
      if (token !== dashboardSearchToken) return;
      fullDone = true;
      var results = data.results || [];
      renderDashboardSearchResults(panel, results);
      status.textContent = results.length + " risultati trovati";
    }).catch(function (error) {
      if (token !== dashboardSearchToken) return;
      fullDone = true;
      status.textContent = "Ricerca web non riuscita: " + error.message;
    });
  });
  document.addEventListener("input", function (event) {
    var filter = event.target.matches && event.target.matches("[data-v2-dashboard-search-filter]") ? event.target : null;
    if (!filter) return;
    var panel = filter.closest(".dashboard-search-panel");
    if (panel && panel._searchItems) renderDashboardSearchResults(panel, panel._searchItems);
  });

  // Server-side folder picker: unlike a native browser picker this browses
  // the machine/container where Gextto is actually running.
  var folderBrowser = null;
  function closeFolderBrowser() {
    if (!folderBrowser) return;
    var overlay = folderBrowser;
    folderBrowser = null;
    if (activeDialog === overlay) activeDialog = null;
    overlay.remove();
    restoreFocus();
  }
  function loadFolderBrowser(overlay) {
    var list = overlay.querySelector("[data-v2-browse-list]");
    var path = overlay.querySelector("[data-v2-browse-path]");
    list.textContent = "Caricamento…";
    fetch("/api/browse_dir?path=" + encodeURIComponent(overlay._current || ""), { credentials: "same-origin" })
      .then(function (response) { return response.json().then(function (data) { if (!response.ok) throw new Error(data.error || "Impossibile leggere le cartelle"); return data; }); })
      .then(function (data) {
        overlay._current = String(data.path || overlay._current || "");
        overlay._parent = data.parent || "";
        path.value = overlay._current;
        list.textContent = "";
        var dirs = data.dirs || [];
        if (!dirs.length) { list.textContent = "Nessuna sottocartella."; return; }
        dirs.forEach(function (dir) {
          var button = document.createElement("button");
          button.className = "path-item";
          button.type = "button";
          button.title = dir;
          button.textContent = dir;
          button.addEventListener("click", function () { overlay._current = dir; loadFolderBrowser(overlay); });
          list.appendChild(button);
        });
        ensureTooltips(overlay);
      })
      .catch(function (error) { list.textContent = error.message; });
  }
  function createFolder(overlay) {
    var name = String(overlay.querySelector("[data-v2-browse-new]").value || "").trim();
    if (!name) return;
    var base = (overlay._current || "/").replace(/\/+$/, "");
    var target = (base || "/") + "/" + name;
    fetch("/api/mkdir", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ path: target }) })
      .then(function (response) { return response.json().then(function (data) { if (!response.ok) throw new Error(data.error || "Impossibile creare la cartella"); return data; }); })
      .then(function () { overlay._target.value = target; closeFolderBrowser(); })
      .catch(function (error) { overlay.querySelector("[data-v2-browse-message]").textContent = error.message; });
  }
  function openFolderBrowser(input) {
    closeFolderBrowser();
    var overlay = document.createElement("div");
    overlay.id = "v2-browse-overlay";
    overlay.className = "overlay";
    overlay.innerHTML = '<div class="modal path-modal" role="dialog" aria-modal="true" aria-labelledby="v2-browse-title"><div class="modal-head"><h3 id="v2-browse-title">Sfoglia cartelle</h3><button class="btn sm" type="button" data-v2-browse-close>Chiudi</button></div><div class="modal-body"><div class="toolbar"><button class="btn sm" type="button" data-v2-browse-up title="Vai alla cartella superiore">↑ Su</button><input class="input mono" type="text" data-v2-browse-path aria-label="Percorso corrente" title="Modifica il percorso e premi Invio per navigare" /><button class="btn sm primary" type="button" data-v2-browse-select title="Usa questa cartella">Seleziona</button><button class="btn sm" type="button" data-v2-browse-create-prompt title="Crea una nuova cartella dentro quella corrente">Crea cartella</button></div><div class="path-list" data-v2-browse-list></div><div class="toolbar"><input class="input" data-v2-browse-new aria-label="Nome nuova cartella" placeholder="Nuova cartella" title="Nome della nuova cartella" /><button class="btn sm primary" type="button" data-v2-browse-create title="Crea la cartella e selezionala">Crea e usa</button></div><small class="muted" data-v2-browse-message aria-live="polite"></small></div></div>';
    document.body.appendChild(overlay);
    overlay._target = input;
    overlay._current = String(input.value || "").trim();
    folderBrowser = overlay;
    overlay.addEventListener("click", function (event) {
      if (event.target === overlay || event.target.closest("[data-v2-browse-close]")) closeFolderBrowser();
      else if (event.target.closest("[data-v2-browse-up]")) { overlay._current = overlay._parent || overlay._current; loadFolderBrowser(overlay); }
      else if (event.target.closest("[data-v2-browse-select]")) { input.value = overlay._current; closeFolderBrowser(); }
      else if (event.target.closest("[data-v2-browse-create-prompt]")) { var entered = window.prompt("Nome nuova cartella:", ""); if (entered) { overlay.querySelector("[data-v2-browse-new]").value = entered; createFolder(overlay); } }
      else if (event.target.closest("[data-v2-browse-create]")) createFolder(overlay);
    });
    overlay.addEventListener("keydown", function (event) { if (event.key === "Enter" && event.target.matches("[data-v2-browse-path]")) { event.preventDefault(); overlay._current = event.target.value.trim(); loadFolderBrowser(overlay); } });
    openDialog(overlay, input);
    loadFolderBrowser(overlay);
  }

  function copyText(value, button) {
    if (!value) return;
    var done = function () {
      var previous = button.textContent;
      button.textContent = "Copiato";
      window.setTimeout(function () { button.textContent = previous; }, 1200);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(value).then(done).catch(function () {});
      return;
    }
    var area = document.createElement("textarea");
    area.value = value;
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    try { document.execCommand("copy"); done(); } catch (error) { /* clipboard unavailable */ }
    area.remove();
  }

  document.addEventListener("change", function (event) {
    if (event.target.matches && event.target.matches("[data-v2-select-all]")) {
      var rows = document.querySelectorAll("[data-v2-select]");
      for (var i = 0; i < rows.length; i++) rows[i].checked = event.target.checked;
      updateTorrentSelection();
    } else if (event.target.matches && event.target.matches("[data-v2-select]")) {
      updateTorrentSelection();
    }
  });

  // Settings tabs swap only the body with HTMX. Keep the chip highlight in
  // sync with the tab that completed the request instead of leaving the
  // initially rendered "Daemon" chip active.
  document.body.addEventListener("htmx:afterRequest", function (event) {
    var detail = event.detail || {};
    if (!detail.successful) return;
    var chip = detail.elt && detail.elt.closest && detail.elt.closest(".settings-view .chip");
    if (!chip) return;
    Array.prototype.forEach.call(document.querySelectorAll(".settings-view .chip-row .chip"), function (item) {
      item.classList.toggle("active", item === chip);
    });
  });

  document.addEventListener("click", function (event) {
    var discoverKind = event.target.closest && event.target.closest("[data-v2-discover-kind]");
    if (discoverKind) {
      var kind = discoverKind.getAttribute("data-v2-discover-kind");
      document.querySelectorAll("[data-v2-discover-kind]").forEach(function (button) {
        button.classList.toggle("primary", button.getAttribute("data-v2-discover-kind") === kind);
      });
    }
    var browse = event.target.closest && event.target.closest("[data-v2-browse-for]");
    if (browse) {
      var scope = browse.closest("form") || browse.closest(".setting-row");
      var input = scope && (scope.querySelector("[data-v2-browse-input]") || scope.querySelector('[name="value"]'));
      if (input) openFolderBrowser(input);
      return;
    }
    var copy = event.target.closest && event.target.closest("[data-v2-copy]");
    if (copy) {
      event.preventDefault();
      copyText(copy.getAttribute("data-v2-copy") || "", copy);
      return;
    }
    var follow = event.target.closest && event.target.closest("[data-v2-logs-follow]");
    if (follow) {
      logsFollow = !logsFollow;
      updateLogsFollowButton();
      if (logsFollow) pinLogTail();
    }
  });
  document.addEventListener("change", function (event) {
    var preset = event.target.closest && event.target.closest("[data-preset-for]");
    if (preset && preset.value) {
      var scope = preset.closest("form") || preset.closest(".form-grid") || preset.closest(".panel");
      var input = scope && scope.querySelector('[name="' + preset.getAttribute("data-preset-for") + '"]');
      if (input) input.value = preset.value;
    }
    var select = event.target.matches && event.target.matches("[data-v2-discover-select]") ? event.target : null;
    if (!select) return;
    var kind = select.value;
    document.querySelectorAll("[data-v2-discover-kind]").forEach(function (button) {
      button.classList.toggle("primary", button.getAttribute("data-v2-discover-kind") === kind);
    });
  });

  function pinLogTail() {
    if (!logsFollow) return;
    var logView = document.getElementById("v2-logs-view");
    if (logView) logView.scrollTop = logView.scrollHeight;
  }

  document.addEventListener("htmx:afterSwap", function (event) {
    if (!event.target) return;
    ensureTooltips(event.target);
    if (event.target.id === "v2-modal") { scanModal(); return; }
    updateTorrentSelection();
    // Keep the log tail pinned to the newest line after the periodic refresh.
    pinLogTail();
  });

  // ------------------------------------------------------------ font picker --
  var fontOverlay = null;
  function buildFontOverlay() {
    fontOverlay = document.createElement("div");
    fontOverlay.className = "overlay";
    fontOverlay.id = "v2-font-overlay";
    fontOverlay.hidden = true;
    fontOverlay.innerHTML =
      '<div class="modal font-modal" role="dialog" aria-modal="true" aria-labelledby="v2-font-title">' +
      '<div class="modal-head"><h3 id="v2-font-title">Tipo di carattere</h3>' +
      '<button class="btn sm" type="button" data-font-close>Chiudi</button></div>' +
      '<div class="modal-body">' +
      '<p class="setting-hint">Scegli un font predisposto o rileva quelli installati. La scelta vale solo in questo browser.</p>' +
      '<label class="field"><span>Font dell\u2019interfaccia</span><select class="input" data-font-family></select></label>' +
      '<div class="toolbar" style="margin-top:10px"><button class="btn" type="button" data-font-detect>Rileva font installati</button>' +
      '<span class="font-detect-status muted" data-font-status></span></div>' +
      '<div class="toolbar" style="margin-top:10px"><input class="input" type="text" data-font-custom maxlength="160" placeholder="es. Noto Sans" />' +
      '<button class="btn" type="button" data-font-apply-custom>Applica</button></div>' +
      '</div></div>';
    document.body.appendChild(fontOverlay);

    var select = fontOverlay.querySelector("[data-font-family]");
    function option(value, label) {
      var node = document.createElement("option");
      node.value = value;
      node.textContent = label;
      select.appendChild(node);
    }
    option("system", "Sistema");
    option("sans", "Sans-serif");
    option("serif", "Serif");
    option("mono", "Monospace");

    function sync() {
      select.value = readFamily();
      var custom = fontOverlay.querySelector("[data-font-custom]");
      if (custom && readFamily().indexOf("local:") === 0) custom.value = readFamily().slice(6);
    }
    function addFamily(family) {
      family = String(family || "").trim().slice(0, 160);
      if (!family) return;
      var value = "local:" + family;
      var exists = Array.prototype.some.call(select.options, function (o) { return o.value === value; });
      if (!exists) option(value, family);
    }
    select.addEventListener("change", function () { setFamily(select.value); });
    fontOverlay.querySelector("[data-font-apply-custom]").addEventListener("click", function () {
      var input = fontOverlay.querySelector("[data-font-custom]");
      var name = String(input.value || "").trim();
      if (!name) return;
      addFamily(name);
      setFamily("local:" + name);
    });
    fontOverlay.querySelector("[data-font-custom]").addEventListener("keydown", function (event) {
      if (event.key === "Enter") { event.preventDefault(); fontOverlay.querySelector("[data-font-apply-custom]").click(); }
    });
    fontOverlay.querySelector("[data-font-detect]").addEventListener("click", function (event) {
      var status = fontOverlay.querySelector("[data-font-status]");
      if (!window.queryLocalFonts) { if (status) status.textContent = "Rilevamento non supportato dal browser."; return; }
      event.currentTarget.disabled = true;
      if (status) status.textContent = "Richiesta autorizzazione\u2026";
      window.queryLocalFonts().then(function (fonts) {
        var families = {};
        (fonts || []).forEach(function (font) {
          var family = String(font.family || "").trim();
          if (family) families[family.toLocaleLowerCase()] = family;
        });
        Object.keys(families).sort(function (a, b) { return families[a].localeCompare(families[b]); })
          .forEach(function (key) { addFamily(families[key]); });
        if (status) status.textContent = Object.keys(families).length + " font rilevati.";
      }).catch(function (error) {
        if (status) status.textContent = error && error.name === "NotAllowedError" ? "Accesso ai font non autorizzato." : "Rilevamento non riuscito.";
      }).then(function () { event.currentTarget.disabled = false; });
    });
    fontOverlay.querySelector("[data-font-close]").addEventListener("click", closeFontPicker);
    fontOverlay._sync = sync;
  }
  function openFontPicker(opener) {
    if (!fontOverlay) buildFontOverlay();
    fontOverlay.hidden = false;
    fontOverlay._sync();
    openDialog(fontOverlay, opener);
  }
  function closeFontPicker() {
    if (!fontOverlay) return;
    fontOverlay.hidden = true;
    activeDialog = null;
    restoreFocus();
  }

  // -------------------------------------------------------------- toolbar --
  document.addEventListener("click", function (event) {
    var target = event.target;
    var fontButton = target.closest("[data-font]");
    if (fontButton) {
      event.preventDefault();
      var raw = String(fontButton.getAttribute("data-font"));
      var current = readScale();
      setScale(raw.charAt(0) === "+" ? current + parseInt(raw.slice(1), 10) : raw.charAt(0) === "-" ? current - parseInt(raw.slice(1), 10) : parseInt(raw, 10));
      return;
    }
    if (target.closest("[data-font-open]")) { event.preventDefault(); openFontPicker(target.closest("[data-font-open]")); return; }
    if (target.closest("[data-theme-toggle]")) { event.preventDefault(); toggleTheme(); }
  });

  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape" && fontOverlay && !fontOverlay.hidden) { event.preventDefault(); closeFontPicker(); }
  });

  document.addEventListener("DOMContentLoaded", function () { ensureTooltips(document); scanModal(); pinLogTail(); updateTorrentSelection(); updateLogsFollowButton(); });
})();
