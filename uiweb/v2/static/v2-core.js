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
    // The toggle exists twice: in the top bar and, on phones, in "Altro".
    var buttons = document.querySelectorAll("[data-theme-toggle]");
    for (var i = 0; i < buttons.length; i++) buttons[i].textContent = themeLabel();
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

  // ------------------------------------------------------ mobile navigation --
  // The system group is a real control on phones, not just a visual label.
  function setSystemMenu(open) {
    var group = document.getElementById("app-system-menu");
    var toggle = document.querySelector("[data-mobile-system-toggle]");
    var backdrop = document.getElementById("app-system-backdrop");
    if (!group || !toggle) return;
    group.classList.toggle("open", open);
    toggle.setAttribute("aria-expanded", open ? "true" : "false");
    var indicator = toggle.querySelector(".nav-more-indicator");
    if (indicator) indicator.textContent = open ? "−" : "＋";
    if (backdrop) backdrop.hidden = !open;
  }
  // On a phone the menu is a panel over the page: never open it by itself
  // (the server marks it open on the pages it contains, for the sidebar).
  function isPhone() { return window.matchMedia && window.matchMedia("(max-width: 560px)").matches; }
  var systemGroup = document.getElementById("app-system-menu");
  if (systemGroup) setSystemMenu(systemGroup.classList.contains("open") && !isPhone());

  document.addEventListener("click", function (event) {
    if (event.target && event.target.id === "app-system-backdrop") {
      setSystemMenu(false);
      return;
    }
    var toggle = event.target.closest && event.target.closest("[data-mobile-system-toggle]");
    if (toggle) {
      var group = document.getElementById("app-system-menu");
      setSystemMenu(!(group && group.classList.contains("open")));
      return;
    }
    var group = document.getElementById("app-system-menu");
    if (group && group.classList.contains("open") && !event.target.closest("#app-system-menu")) {
      setSystemMenu(false);
    }
  });

  // ------------------------------------------------ torrent detail tabs --
  // HTMX replaces only the panel content. Keep the persistent tab bar in sync
  // with the panel requested by the user.
  document.addEventListener("click", function (event) {
    var tab = event.target.closest && event.target.closest("[data-v2-detail-tab]");
    if (!tab) return;
    var tablist = tab.closest('[role="tablist"]');
    if (!tablist) return;
    var tabs = tablist.querySelectorAll("[data-v2-detail-tab]");
    for (var i = 0; i < tabs.length; i++) {
      var active = tabs[i] === tab;
      tabs[i].classList.toggle("primary", active);
      tabs[i].setAttribute("aria-selected", active ? "true" : "false");
    }
  });

  document.addEventListener("keydown", function (event) {
    if (event.key !== "Escape") return;
    var group = document.getElementById("app-system-menu");
    if (group && group.classList.contains("open")) {
      setSystemMenu(false);
      var toggle = document.querySelector("[data-mobile-system-toggle]");
      if (toggle) toggle.focus();
    }
  });

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
    // On a phone the bulk actions appear only while something is selected.
    var downloadsView = document.querySelector(".downloads-view");
    if (downloadsView) downloadsView.classList.toggle("has-selection", selected.length > 0);
    var label = document.querySelector("[data-v2-selected-count]");
    if (label) label.textContent = selected.length + " selezionati · Azioni:";
    var all = document.querySelector("[data-v2-select-all]");
    var rows = document.querySelectorAll("[data-v2-select]");
    if (all) all.checked = rows.length > 0 && selected.length === rows.length;
  }

  var logsFollow = true;
  var pausedLogScrollTop = null;
  var isLogSelecting = false;

  function hasLogSelection() {
    var sel = window.getSelection();
    if (!sel || sel.isCollapsed || sel.rangeCount === 0) return false;
    var logView = document.getElementById("v2-logs-view");
    if (!logView) return false;
    try {
      for (var i = 0; i < sel.rangeCount; i++) {
        var range = sel.getRangeAt(i);
        var ancestor = range.commonAncestorContainer;
        if (ancestor && (ancestor === logView || logView.contains(ancestor))) {
          return true;
        }
      }
    } catch (e) {
      return false;
    }
    return false;
  }

  function canPollLogs() {
    return logsFollow && !isLogSelecting && !hasLogSelection();
  }
  window.gexttoCanPollLogs = canPollLogs;

  document.addEventListener("mousedown", function (event) {
    var logView = document.getElementById("v2-logs-view");
    if (logView && (event.target === logView || logView.contains(event.target))) {
      isLogSelecting = true;
    }
  }, true);
  document.addEventListener("mouseup", function () {
    isLogSelecting = false;
  }, true);
  document.addEventListener("touchstart", function (event) {
    var logView = document.getElementById("v2-logs-view");
    if (logView && (event.target === logView || logView.contains(event.target))) {
      isLogSelecting = true;
    }
  }, { passive: true, capture: true });
  document.addEventListener("touchend", function () {
    isLogSelecting = false;
  }, { passive: true, capture: true });

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
        form.set("redirect", "/?view=dashboard");
        fetch("/search/add", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/x-www-form-urlencoded" }, body: form.toString() })
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
      .then(function () { overlay._target.value = target; overlay._target.dispatchEvent(new Event("input", { bubbles: true })); closeFolderBrowser(); })
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
      else if (event.target.closest("[data-v2-browse-select]")) { input.value = overlay._current; input.dispatchEvent(new Event("input", { bubbles: true })); closeFolderBrowser(); }
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

  // ------------------------------------------------------ settings editing --
  // Every setting row is its own small form. The browser keeps each control's
  // initial value (defaultValue/defaultChecked/defaultSelected), so a row is
  // "dirty" when any control differs from it. Dirty rows are marked, counted
  // in the save bar, saved together by «Salva tutto», and protected from
  // being lost by a section change or by leaving the page.
  var TRACKED = "form.setting-row[data-setting-key], form[data-v2-track]";
  var savingAll = false;
  var pendingDirty = "";
  var focusHeading = false;

  function controlDirty(control) {
    if (control.disabled || control.type === "hidden" || control.type === "submit" || control.type === "button") return false;
    if (control.type === "checkbox" || control.type === "radio") return control.checked !== control.defaultChecked;
    if (control.tagName === "SELECT") {
      for (var i = 0; i < control.options.length; i++) {
        if (control.options[i].selected !== control.options[i].defaultSelected) return true;
      }
      return false;
    }
    return typeof control.value === "string" && control.value !== control.defaultValue;
  }
  function formDirty(form) {
    if (form.getAttribute("data-v2-saving")) return false;
    if (form.getAttribute("data-v2-force-dirty")) return true;
    for (var i = 0; i < form.elements.length; i++) {
      if (controlDirty(form.elements[i])) return true;
    }
    return false;
  }
  function dirtyForms(scope) {
    return Array.prototype.filter.call((scope || document).querySelectorAll(TRACKED), formDirty);
  }
  function markForm(form) {
    var dirty = formDirty(form);
    if (dirty) form.setAttribute("data-dirty", "");
    else form.removeAttribute("data-dirty");
    var badges = form.querySelectorAll("[data-v2-dirty-badge]");
    for (var i = 0; i < badges.length; i++) badges[i].hidden = !dirty;
  }
  function refreshSavebar() {
    var bar = document.querySelector("[data-v2-savebar]");
    if (!bar) return;
    var rows = dirtyForms().filter(function (form) { return form.matches("form.setting-row[data-setting-key]"); });
    var count = bar.querySelector("[data-v2-dirty-count]");
    if (count) count.textContent = String(rows.length);
    bar.hidden = rows.length === 0;
  }
  function refreshAll() {
    Array.prototype.forEach.call(document.querySelectorAll(TRACKED), markForm);
    refreshSavebar();
  }
  function announce(text) {
    var live = document.getElementById("v2-settings-live");
    if (!live || !text) return;
    live.textContent = "";
    window.setTimeout(function () { live.textContent = text; }, 30);
  }
  function messageOf(selector) {
    var node = document.querySelector(selector);
    return node ? node.textContent.trim() : "";
  }

  // A switch shows or hides the rows that depend on it, before it is saved,
  // so the user immediately sees what turning it on means.
  function applyDependents(row) {
    if (!row || !row.matches || !row.matches("form.setting-row[data-setting-key]")) return;
    var toggle = row.querySelector('input[role="switch"]');
    if (!toggle) return;
    var key = row.getAttribute("data-setting-key");
    var children = document.querySelectorAll('[data-v2-depends-on="' + key + '"]');
    for (var i = 0; i < children.length; i++) children[i].hidden = !toggle.checked;
  }

  function setRowValue(row, value) {
    var toggle = row.querySelector('input[role="switch"]');
    if (toggle) { toggle.checked = value === toggle.value; return; }
    var days = row.querySelectorAll('.setting-days input[type="checkbox"]');
    if (days.length) {
      var wanted = {};
      String(value).split(",").forEach(function (day) { wanted[day.trim()] = true; });
      for (var i = 0; i < days.length; i++) days[i].checked = !!wanted[days[i].value];
      return;
    }
    var control = row.querySelector('select[name="value"], textarea[name="value"], input[name="value"]:not([type="hidden"])');
    if (control) control.value = value;
  }

  document.addEventListener("input", function (event) {
    var form = event.target.closest && event.target.closest(TRACKED);
    if (!form) return;
    markForm(form);
    refreshSavebar();
  });
  document.addEventListener("change", function (event) {
    var form = event.target.closest && event.target.closest(TRACKED);
    if (form) {
      markForm(form);
      refreshSavebar();
      if (event.target.matches('input[role="switch"]')) applyDependents(form);
    }
    if (event.target.matches && event.target.matches("[data-v2-show-keys]")) {
      showKeys(event.target.checked);
      storageSet("gextto_settings_keys", event.target.checked ? "1" : "0");
    }
  });

  function showKeys(on) {
    var view = document.querySelector("[data-v2-settings]");
    if (view) view.classList.toggle("show-keys", !!on);
  }
  function initSettingsView() {
    var toggle = document.querySelector("[data-v2-show-keys]");
    if (toggle) {
      toggle.checked = storageGet("gextto_settings_keys") === "1";
      showKeys(toggle.checked);
    }
    refreshAll();
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", initSettingsView);
  else initSettingsView();

  // Keys of the rows «Salva tutto» is saving: their own result is not
  // announced one by one, a single summary is announced when the last row is
  // back (htmx inserts each row after its request promise has resolved).
  var batch = null;

  function finishBatch() {
    if (!batch || batch.pending > 0 || !batch.sent) return;
    var keys = batch.keys;
    var buttons = batch.buttons;
    batch = null;
    savingAll = false;
    for (var j = 0; j < buttons.length; j++) buttons[j].disabled = false;
    var failed = keys.map(function (key) { return document.getElementById("v2-setting-" + key); })
      .filter(function (row) { return row && row.querySelector(".htx-status.err"); });
    refreshAll();
    if (failed.length) {
      announce(messageOf("[data-v2-errors-message]") + " " + failed.length);
      var control = failed[0].querySelector("input:not([type=hidden]), select, textarea");
      failed[0].hidden = false;
      if (control) control.focus();
    } else {
      announce(messageOf("[data-v2-saved-all-message]") + " " + keys.length);
    }
  }

  function saveAll() {
    var rows = dirtyForms().filter(function (form) { return form.matches("form.setting-row[data-setting-key]"); });
    if (!rows.length || !window.htmx || batch) return;
    var bar = document.querySelector("[data-v2-savebar]");
    var buttons = bar ? Array.prototype.slice.call(bar.querySelectorAll("button")) : [];
    for (var i = 0; i < buttons.length; i++) buttons[i].disabled = true;
    savingAll = true;
    var keys = rows.map(function (form) { return form.getAttribute("data-setting-key"); });
    batch = { keys: keys, waiting: {}, pending: keys.length, sent: false, buttons: buttons };
    keys.forEach(function (key) { batch.waiting[key] = true; });
    // One request at a time: every save opens the configuration database.
    var chain = Promise.resolve();
    rows.forEach(function (form) {
      chain = chain.then(function () {
        if (!form.isConnected) return null;
        return window.htmx.ajax("POST", form.getAttribute("hx-post") || "/settings/save", { source: form, target: form, swap: "outerHTML" });
      }).catch(function () {
        // A failed request leaves the row in place: stop waiting for it.
        var key = form.getAttribute("data-setting-key");
        if (batch && batch.waiting[key]) { delete batch.waiting[key]; batch.pending--; }
      });
    });
    chain.then(function () {
      if (!batch) return;
      batch.sent = true;
      // Rows that never came back (network error, removed) are not waited for.
      var current = batch;
      window.setTimeout(function () {
        if (batch === current) { batch.pending = 0; finishBatch(); }
      }, 3000);
      finishBatch();
    });
  }
  function discardAll() {
    dirtyForms().forEach(function (form) {
      form.reset();
      form.removeAttribute("data-v2-force-dirty");
      applyDependents(form);
    });
    refreshAll();
  }

  document.addEventListener("click", function (event) {
    var target = event.target;
    if (!target.closest) return;
    var reset = target.closest("[data-v2-reset]");
    if (reset) {
      var row = reset.closest("form.setting-row");
      if (row) {
        setRowValue(row, reset.getAttribute("data-v2-reset"));
        markForm(row);
        refreshSavebar();
        applyDependents(row);
      }
      return;
    }
    if (target.closest("[data-v2-save-all]")) { saveAll(); return; }
    if (target.closest("[data-v2-discard]")) { discardAll(); }
  });

  document.body.addEventListener("htmx:beforeRequest", function (event) {
    var detail = event.detail || {};
    var elt = detail.elt;
    var target = detail.target;
    if (!elt || !elt.closest) return;
    // Changing section (or swapping the whole page) drops unsaved edits.
    var body = document.getElementById("v2-settings-body");
    if (body && target && (target === body || (target.contains && target.contains(body)))) {
      if (dirtyForms(body).length && !window.confirm(messageOf("[data-v2-leave-message]") || "?")) {
        event.preventDefault();
        var select = body.querySelector(".settings-nav-select select");
        if (select) select.value = body.getAttribute("data-active-tab") || select.value;
        return;
      }
      focusHeading = !!(elt.closest(".settings-nav") || elt.closest("#v2-settings-search"));
    }
    if (elt.matches && elt.matches(TRACKED)) {
      // The form is being saved: its edits are no longer pending.
      elt.setAttribute("data-v2-saving", "1");
      elt.removeAttribute("data-v2-force-dirty");
      markForm(elt);
      refreshSavebar();
      return;
    }
    if (elt.hasAttribute && elt.hasAttribute("data-v2-marks-dirty")) {
      var selector = elt.getAttribute("data-v2-marks-dirty");
      var owner = selector ? null : elt.closest("form[data-v2-track]");
      if (owner) { owner.setAttribute("data-v2-force-dirty", "1"); markForm(owner); }
      else pendingDirty = selector;
    }
  });
  document.body.addEventListener("htmx:afterRequest", function (event) {
    var detail = event.detail || {};
    var elt = detail.elt;
    if (!elt || !elt.matches || !elt.matches(TRACKED) || !elt.isConnected) return;
    elt.removeAttribute("data-v2-saving");
    if (detail.successful) {
      // Saved without a swap (hx-swap="none"): the current values become the
      // new baseline.
      for (var i = 0; i < elt.elements.length; i++) {
        var control = elt.elements[i];
        if (control.type === "checkbox" || control.type === "radio") control.defaultChecked = control.checked;
        else if (control.tagName === "SELECT") {
          for (var j = 0; j < control.options.length; j++) control.options[j].defaultSelected = control.options[j].selected;
        } else if (typeof control.value === "string" && control.type !== "hidden") control.defaultValue = control.value;
      }
    }
    markForm(elt);
    refreshSavebar();
  });
  document.body.addEventListener("htmx:load", function (event) {
    var elt = event.detail && event.detail.elt;
    if (!elt || !elt.matches) return;
    if (pendingDirty) {
      var marked = document.querySelector(pendingDirty);
      if (marked) marked.setAttribute("data-v2-force-dirty", "1");
      pendingDirty = "";
    }
    if (elt.id === "v2-settings-body") {
      if (focusHeading && !elt.hasAttribute("data-highlight")) {
        var heading = document.getElementById("v2-settings-heading");
        if (heading) heading.focus();
      }
      focusHeading = false;
    }
    if (elt.matches("form.setting-row[data-setting-key]") && batch && batch.waiting[elt.getAttribute("data-setting-key")]) {
      delete batch.waiting[elt.getAttribute("data-setting-key")];
      batch.pending--;
      refreshAll();
      finishBatch();
      return;
    }
    if (elt.matches("form.setting-row[data-setting-key]") && !savingAll) {
      var status = elt.querySelector(".setting-status");
      var label = elt.querySelector(".setting-label");
      if (status && status.textContent.trim()) announce((label ? label.textContent.trim() + ": " : "") + status.textContent.trim());
    }
    refreshAll();
  });
  window.addEventListener("beforeunload", function (event) {
    if (!dirtyForms().length) return;
    event.preventDefault();
    event.returnValue = "";
  });

  var discoverKind = "series";

  function setDiscoverKind(kind) {
    discoverKind = kind === "movie" ? "movie" : "series";
    document.querySelectorAll("[data-v2-discover-kind-input]").forEach(function (input) {
      input.value = discoverKind;
    });
    document.querySelectorAll("[data-v2-discover-kind]").forEach(function (button) {
      button.classList.toggle("primary", button.getAttribute("data-v2-discover-kind") === discoverKind);
    });
  }

  function setDiscoverChoice(choice) {
    document.querySelectorAll("[data-v2-discover-choice]").forEach(function (form) {
      var active = form.getAttribute("data-v2-discover-choice") === choice;
      var button = form.querySelector("button");
      if (button) {
        button.classList.toggle("primary", active);
        button.classList.toggle("active", active);
        button.setAttribute("aria-pressed", active ? "true" : "false");
      }
    });
  }

  function discoverChoiceTarget(event) {
    var target = event && event.target;
    return target && target.closest && target.closest("[data-v2-discover-choice]");
  }

  setDiscoverChoice("trending:week");

  document.addEventListener("click", function (event) {
    var discoverKind = event.target.closest && event.target.closest("[data-v2-discover-kind]");
    if (discoverKind) {
      var kind = discoverKind.getAttribute("data-v2-discover-kind");
      setDiscoverKind(kind);
    }
    var discoverChoice = discoverChoiceTarget(event);
    if (discoverChoice) {
      setDiscoverChoice(discoverChoice.getAttribute("data-v2-discover-choice"));
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
    var copyLogs = event.target.closest && event.target.closest("[data-v2-logs-copy]");
    if (copyLogs) {
      event.preventDefault();
      var textToCopy = "";
      var sel = window.getSelection();
      var logView = document.getElementById("v2-logs-view");
      if (sel && !sel.isCollapsed && logView && logView.contains(sel.anchorNode)) {
        textToCopy = sel.toString();
      } else if (logView) {
        textToCopy = logView.innerText || logView.textContent || "";
      }
      if (textToCopy) copyText(textToCopy, copyLogs);
      return;
    }
    var follow = event.target.closest && event.target.closest("[data-v2-logs-follow]");
    if (follow) {
      event.preventDefault();
      logsFollow = !logsFollow;
      if (!logsFollow) {
        var logView = document.getElementById("v2-logs-view");
        pausedLogScrollTop = logView ? logView.scrollTop : 0;
      }
      updateLogsFollowButton();
      if (logsFollow) {
        pausedLogScrollTop = null;
        var form = document.querySelector(".logs-toolbar");
        if (form && window.htmx) {
          window.htmx.trigger(form, "submit");
        } else {
          pinLogTail();
        }
      }
      return;
    }
  });
  // Apply the visual state on pointer-down too. This happens before HTMX can
  // process the submit and also covers touch input where the click event can
  // be delayed or suppressed.
  document.addEventListener("pointerdown", function (event) {
    var discoverChoice = discoverChoiceTarget(event);
    if (discoverChoice) setDiscoverChoice(discoverChoice.getAttribute("data-v2-discover-choice"));
  }, true);
  // Capture submit as well as click: HTMX handles the form submission before
  // the bubbling click handler in some browsers, so this keeps the selected
  // list visible even when the response is very fast.
  document.addEventListener("submit", function (event) {
    var form = event.target && event.target.closest && event.target.closest("[data-v2-discover-choice]");
    var submitter = event.submitter;
    var kindButton = submitter && submitter.closest && submitter.closest("[data-v2-discover-kind]");
    if (kindButton) setDiscoverKind(kindButton.getAttribute("data-v2-discover-kind"));
    if (form) {
      var kindInput = form.querySelector('input[name="kind"]');
      if (kindInput) kindInput.value = discoverKind;
      setDiscoverChoice(form.getAttribute("data-v2-discover-choice"));
    }
  }, true);
  document.addEventListener("change", function (event) {
    var preset = event.target.closest && event.target.closest("[data-preset-for]");
    if (preset && preset.value) {
      var scope = preset.closest("form") || preset.closest(".form-grid") || preset.closest(".panel");
      var input = scope && scope.querySelector('[name="' + preset.getAttribute("data-preset-for") + '"]');
      if (input) input.value = preset.value;
    }
    var select = event.target.matches && event.target.matches("[data-v2-discover-select]") ? event.target : null;
    if (!select) return;
    setDiscoverKind(select.value);
  });

  function pinLogTail() {
    if (!logsFollow) return;
    var logView = document.getElementById("v2-logs-view");
    if (logView) logView.scrollTop = logView.scrollHeight;
  }

  document.addEventListener("htmx:configRequest", function (event) {
    var path = event.detail && event.detail.path;
    if (!path || path.indexOf("/partial/logs") === -1) return;
    var trig = event.detail && event.detail.triggeringEvent;
    var isPolling = !trig || trig.type === "hx:poll:trigger";
    if (isPolling && !canPollLogs()) {
      event.preventDefault();
    }
  });

  document.addEventListener("htmx:beforeSwap", function (event) {
    var target = event.detail && event.detail.target;
    if (!target || target.id !== "v2-logs-view") return;
    var trig = event.detail && event.detail.triggeringEvent;
    var isPolling = !trig || trig.type === "hx:poll:trigger";
    if (isPolling && !canPollLogs()) {
      if (event.detail) event.detail.shouldSwap = false;
      return;
    }
    if (!logsFollow) {
      pausedLogScrollTop = target.scrollTop;
    }
  });

  document.addEventListener("htmx:afterSwap", function (event) {
    if (!event.target) return;
    updateProblemsChip();
    updateDownloadsBadge();
    ensureTooltips(event.target);
    if (event.target.id === "v2-modal") { scanModal(); return; }
    updateTorrentSelection();
    if (event.target.id === "v2-logs-view") {
      updateLogsFollowButton();
      if (!logsFollow && pausedLogScrollTop !== null) {
        event.target.scrollTop = pausedLogScrollTop;
      }
    }
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

  // --------------------------------------------------------------- toasts --
  // Transient feedback for actions whose server response arrives later (e.g.
  // the duplicate scan). The shell renders the toast for redirecting actions;
  // this one fires the moment the button is clicked, so the user knows the
  // work started before the panel updates with the results.
  function autoDismissToast(el) {
    if (!el || el._v2DismissTimer) return;
    el._v2DismissTimer = window.setTimeout(function () {
      if (el.parentNode) el.parentNode.removeChild(el);
    }, 6000);
  }

  function initToastRegion() {
    var region = document.getElementById("v2-toast-region");
    if (!region) return;
    var existing = region.querySelectorAll(".v2-toast");
    for (var i = 0; i < existing.length; i++) autoDismissToast(existing[i]);
    if (window.MutationObserver) {
      new MutationObserver(function (mutations) {
        mutations.forEach(function (m) {
          for (var j = 0; j < m.addedNodes.length; j++) {
            var node = m.addedNodes[j];
            if (node.nodeType === 1) {
              if (node.classList && node.classList.contains("v2-toast")) {
                autoDismissToast(node);
              } else if (node.querySelectorAll) {
                var toasts = node.querySelectorAll(".v2-toast");
                for (var k = 0; k < toasts.length; k++) autoDismissToast(toasts[k]);
              }
            }
          }
        });
      }).observe(region, { childList: true, subtree: true });
    }
  }

  function showToast(title, message, isError) {
    var region = document.getElementById("v2-toast-region");
    if (!region) return;
    var toast = document.createElement("div");
    toast.className = "v2-toast" + (isError ? " error" : "");
    toast.setAttribute("role", "status");
    var strong = document.createElement("strong");
    strong.textContent = title || "Operazione";
    var span = document.createElement("span");
    span.textContent = message || "";
    toast.appendChild(strong);
    toast.appendChild(span);
    region.appendChild(toast);
    autoDismissToast(toast);
  }

  document.addEventListener("htmx:beforeRequest", function (event) {
    var detail = event.detail || {};
    var config = detail.requestConfig || {};
    var verb = String(config.verb || "").toLowerCase();
    var source = detail.elt;
    var userEvent = detail.triggeringEvent;
    if (!source || !userEvent) return; // Ignore polling and automatic page loads.

    var clicked = userEvent.submitter || userEvent.target;
    var trigger = clicked && clicked.closest ? clicked.closest("[data-v2-toast-message]") : null;
    if (!trigger && source.closest) trigger = source.closest("[data-v2-toast-message]");
    if (verb !== "post" && !trigger) return;

    var title = trigger && trigger.getAttribute("data-v2-toast-title");
    var message = trigger && trigger.getAttribute("data-v2-toast-message");
    if (!message) message = "Richiesta avviata.";
    if (config._v2ToastShown) return;
    config._v2ToastShown = true;
    config._v2ToastTitle = title || "Operazione";
    showToast(config._v2ToastTitle, message, false);
  });

  document.addEventListener("htmx:afterRequest", function (event) {
    var detail = event.detail || {};
    var config = detail.requestConfig || {};
    if (!config._v2ToastShown) return;
    var xhr = detail.xhr;
    var response = xhr && xhr.responseText || "";
    // Some actions provide a more useful server-rendered completion toast.
    if (response.indexOf('id="v2-toast-region"') !== -1 || response.indexOf("id='v2-toast-region'") !== -1) return;
    if (detail.successful) showToast(config._v2ToastTitle, "Richiesta completata.", false);
    else showToast(config._v2ToastTitle, "Operazione non riuscita.", true);
  });

  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape" && fontOverlay && !fontOverlay.hidden) { event.preventDefault(); closeFontPicker(); }
  });

  // ------------------------------------------------------ phone helpers --
  // New warnings: the status strip carries the daemon's WARN/ERROR count;
  // the page remembers the count seen on the Log page and shows the rest.
  function onLogsPage() { return /[?&]view=logs(&|$)/.test(window.location.search); }
  function updateProblemsChip() {
    var strip = document.getElementById("v2-live-mobile-metrics");
    if (!strip) return;
    var current = parseInt(strip.getAttribute("data-problems") || "0", 10) || 0;
    var stored = storageGet("gextto_seen_problems");
    var seen = stored === null ? current : (parseInt(stored, 10) || 0);
    if (stored === null || onLogsPage()) { seen = current; storageSet("gextto_seen_problems", String(current)); }
    // The counter restarts with the daemon: then everything counted is new.
    var unseen = current >= seen ? current - seen : current;
    var chip = strip.querySelector("[data-ms-problems]");
    if (chip) {
      chip.hidden = unseen === 0;
      var count = chip.querySelector("[data-ms-problems-count]");
      if (count) count.textContent = String(unseen);
    }
    var logNav = document.querySelector('#app-sidebar [data-nav="logs"]');
    if (logNav) logNav.classList.toggle("has-news", unseen > 0);
  }

  // "Scarico" badge: downloads in progress, refreshed with the live metrics.
  function updateDownloadsBadge() {
    var source = document.getElementById("v2-live-mobile-metrics") || document.getElementById("v2-live-top-metrics");
    if (!source || !source.hasAttribute("data-active-downloads")) return;
    var badgeStr = source.getAttribute("data-active-downloads") || "";
    var badges = document.querySelectorAll('#app-sidebar [data-nav="downloads"] .nav-count');
    for (var i = 0; i < badges.length; i++) {
      badges[i].textContent = badgeStr;
      badges[i].hidden = (badgeStr === "0/0" || badgeStr === "0" || badgeStr === "");
    }
  }

  // Panels marked data-mobile-collapse start closed on a phone, unless they
  // already hold something to act on (a shared link in the add form).
  function collapseOnPhone() {
    if (!isPhone()) return;
    var panels = document.querySelectorAll("details[data-mobile-collapse]");
    for (var i = 0; i < panels.length; i++) {
      var filled = panels[i].querySelector('input[name="magnet"]');
      if (!(filled && filled.value)) panels[i].removeAttribute("open");
    }
  }

  // Scarico: bandwidth limits and clean-up tools sit behind "Opzioni" on a
  // phone. The class lives outside the auto-refreshed table, so it survives
  // the 5-second swaps.
  document.addEventListener("click", function (event) {
    var toggle = event.target.closest && event.target.closest("[data-downloads-tools]");
    if (!toggle) return;
    var view = toggle.closest(".downloads-view");
    if (!view) return;
    var open = !view.classList.contains("show-download-tools");
    view.classList.toggle("show-download-tools", open);
    toggle.setAttribute("aria-expanded", open ? "true" : "false");
  });

  // Pull to refresh for the installed app, where the browser's own gesture
  // is not available. In a normal browser tab the native gesture is used.
  function initPullToRefresh() {
    var standalone = (window.matchMedia && window.matchMedia("(display-mode: standalone)").matches) || window.navigator.standalone;
    if (!standalone || !("ontouchstart" in window)) return;
    var startY = null, pulled = 0;
    var hint = document.createElement("div");
    hint.className = "ptr-hint";
    hint.textContent = "↓ Rilascia per aggiornare";
    hint.hidden = true;
    document.body.appendChild(hint);
    window.addEventListener("touchstart", function (event) {
      startY = window.scrollY <= 0 && !activeDialog ? event.touches[0].clientY : null;
      pulled = 0;
    }, { passive: true });
    window.addEventListener("touchmove", function (event) {
      if (startY === null) return;
      pulled = event.touches[0].clientY - startY;
      hint.hidden = pulled < 30;
      hint.classList.toggle("ready", pulled > 90);
    }, { passive: true });
    window.addEventListener("touchend", function () {
      if (startY !== null && pulled > 90) window.location.reload();
      startY = null;
      hint.hidden = true;
    });
  }
  function initLiveMetrics() {
    if (!window.EventSource) return;
    var es = new EventSource("/partial/chrome/sse");
    es.addEventListener("chrome-top", function(e) {
      var el = document.getElementById("v2-live-top-metrics");
      if (el) el.outerHTML = e.data;
      updateDownloadsBadge();
    });
    es.addEventListener("chrome-mobile", function(e) {
      var el = document.getElementById("v2-live-mobile-metrics");
      if (el) el.outerHTML = e.data;
      updateDownloadsBadge();
      updateProblemsChip();
    });
    es.addEventListener("chrome-status", function(e) {
      var el = document.getElementById("v2-live-status");
      if (el) el.outerHTML = e.data;
    });
  }

  if ("serviceWorker" in navigator && window.isSecureContext) {
    window.addEventListener("load", function () { navigator.serviceWorker.register("/sw.js").catch(function () { /* optional */ }); });
  }

  document.addEventListener("DOMContentLoaded", function () { ensureTooltips(document); scanModal(); pinLogTail(); updateTorrentSelection(); updateLogsFollowButton(); initToastRegion(); updateProblemsChip(); updateDownloadsBadge(); collapseOnPhone(); initPullToRefresh(); initLiveMetrics(); });
})();
