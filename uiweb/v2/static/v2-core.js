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

  document.addEventListener("click", function (event) {
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

  function pinLogTail() {
    if (!logsFollow) return;
    var logView = document.getElementById("v2-logs-view");
    if (logView) logView.scrollTop = logView.scrollHeight;
  }

  document.addEventListener("htmx:afterSwap", function (event) {
    if (!event.target) return;
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

  document.addEventListener("DOMContentLoaded", function () { scanModal(); pinLogTail(); updateTorrentSelection(); updateLogsFollowButton(); });
})();
