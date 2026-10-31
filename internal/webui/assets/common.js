// gx-torrent shared page helpers: the theme switch and toasts. Loaded by every
// page after the theme was applied by the inline snippet in <head> (no flash).
(function () {
  "use strict";
  var KEY = "gx-theme";
  var ORDER = ["auto", "light", "dark"];

  function read() {
    try { var v = localStorage.getItem(KEY); return ORDER.indexOf(v) >= 0 ? v : "auto"; } catch (e) { return "auto"; }
  }
  function apply(mode) {
    var root = document.documentElement;
    if (mode === "light" || mode === "dark") root.setAttribute("data-theme", mode);
    else root.removeAttribute("data-theme");
    document.querySelectorAll("[data-theme-switch]").forEach(function (btn) {
      btn.setAttribute("data-mode", mode);
      btn.querySelectorAll("[data-theme-icon]").forEach(function (icon) {
        // SVG elements have no .hidden property: use the attribute.
        if (icon.getAttribute("data-theme-icon") === mode) icon.removeAttribute("hidden");
        else icon.setAttribute("hidden", "");
      });
      var label = btn.getAttribute("data-label-" + mode);
      if (label) btn.setAttribute("aria-label", label);
    });
  }
  function cycle() {
    var next = ORDER[(ORDER.indexOf(read()) + 1) % ORDER.length];
    try { localStorage.setItem(KEY, next); } catch (e) { /* private mode: session only */ }
    apply(next);
  }

  // Toasts are announced politely to screen readers (role=status region).
  function region() {
    var r = document.getElementById("toasts");
    if (!r) {
      r = document.createElement("div");
      r.id = "toasts";
      r.className = "toasts";
      r.setAttribute("role", "status");
      r.setAttribute("aria-live", "polite");
      document.body.appendChild(r);
    }
    return r;
  }
  function toast(message, isError) {
    var el = document.createElement("div");
    el.className = "toast" + (isError ? " err" : "");
    el.textContent = message;
    region().appendChild(el);
    setTimeout(function () {
      el.classList.add("hide");
      setTimeout(function () { el.remove(); }, 400);
    }, isError ? 6500 : 3500);
  }

  window.gx = window.gx || {};
  window.gx.toast = toast;
  window.gx.theme = { read: read, apply: apply, cycle: cycle };

  // ---------- folder picker ----------
  // A modal <dialog id="browse-overlay" class="picker"> lists the daemon
  // machine's directories (`/ui/browse-dir`, standalone only) so the operator
  // picks a folder instead of typing it. Any <button class="browse"
  // data-target="fieldId"> opens it and writes the chosen path back into that
  // field; with data-append the field is a list (one path per line, ";" also
  // accepted) and the path is added to it instead of replacing it. A button
  // with data-path proposes that fixed path without opening the dialog (the
  // DLNA "Download folder" shortcut). It is a native dialog, so it stacks
  // above an already open modal (the add-torrent form) that a plain overlay
  // could not.
  (function () {
    var overlay = null, listEl = null, pathEl = null;
    var target = null, appendList = false, current = "", parent = "", endpoint = "/ui/browse-dir";

    function ensure() {
      if (overlay && overlay.isConnected) return true;
      overlay = document.getElementById("browse-overlay");
      if (!overlay) return false;
      listEl = document.getElementById("browse-list");
      pathEl = document.getElementById("browse-path");
      var ep = overlay.getAttribute("data-browse-endpoint");
      if (ep) endpoint = ep;
      return true;
    }
    function field(id) { return id ? document.getElementById(id) : null; }

    function listItems(value) {
      return String(value || "").split(/[;\n]/).map(function (p) { return p.trim(); }).filter(Boolean);
    }
    function open(id, list) {
      if (id) target = id;
      appendList = !!list;
      if (!ensure()) return;
      var input = field(target);
      // A list field starts the picker from its last entry.
      var start = input ? input.value : "";
      if (appendList) start = listItems(start).pop() || "";
      if (typeof overlay.showModal === "function") {
        if (!overlay.open) overlay.showModal();
      } else {
        overlay.setAttribute("open", "");
      }
      var close = overlay.querySelector("[data-picker-close]");
      if (close) close.focus();
      go(start);
    }
    function close() {
      if (!overlay) return;
      if (typeof overlay.close === "function" && overlay.open) overlay.close();
      else overlay.removeAttribute("open");
    }
    function go(path) {
      if (!listEl) return;
      listEl.textContent = "…";
      fetch(endpoint + "?path=" + encodeURIComponent(path || ""), { cache: "no-store" })
        .then(function (r) { return r.json(); })
        .then(function (j) {
          current = j.path || "";
          parent = j.parent || "";
          if (pathEl) pathEl.textContent = current;
          listEl.textContent = "";
          if (j.error) {
            var e = document.createElement("p");
            e.className = "err";
            e.textContent = j.error;
            listEl.appendChild(e);
          }
          (j.dirs || []).forEach(function (d) {
            var b = document.createElement("button");
            b.type = "button";
            b.className = "dir";
            b.textContent = d.replace(/[\\/]+$/, "").split(/[\\/]/).pop() || d;
            b.title = d;
            b.addEventListener("click", function () { go(d); });
            listEl.appendChild(b);
          });
        })
        .catch(function () { listEl.textContent = "—"; });
    }
    function up() { if (parent) go(parent); }
    function choose() {
      var input = field(target);
      if (input && current) write(input, current, appendList);
      close();
    }

    // write puts a path straight into the target field: the same rule as
    // choose(), for a button that proposes a known folder (data-path) instead
    // of opening the picker.
    function write(input, path, append) {
      path = String(path || "").trim();
      if (!input || !path) return;
      if (append) {
        var items = listItems(input.value);
        if (items.indexOf(path) < 0) items.push(path);
        input.value = items.join("\n");
      } else {
        input.value = path;
      }
      input.dispatchEvent(new Event("change", { bubbles: true }));
    }

    window.gx.picker = { open: open, close: close, go: go, up: up, choose: choose };

    document.addEventListener("click", function (e) {
      var el = e.target;
      if (!el || !el.closest) return;
      var browse = el.closest(".browse");
      if (browse) {
        e.preventDefault();
        var fixed = browse.getAttribute("data-path");
        if (fixed) {
          // A known folder (the configured Download folder) is added without
          // opening the picker: the path is already a click away.
          write(field(browse.getAttribute("data-target")), fixed, browse.hasAttribute("data-append"));
          return;
        }
        open(browse.getAttribute("data-target"), browse.hasAttribute("data-append"));
        return;
      }
      if (el.closest("[data-picker-close]")) { close(); return; }
      if (el.closest("[data-picker-up]")) { go(parent); return; }
      if (el.closest("[data-picker-choose]")) { choose(); return; }
    });
  })();

  document.addEventListener("click", function (e) {
    var btn = e.target.closest && e.target.closest("[data-theme-switch]");
    if (btn) { e.preventDefault(); cycle(); }
  });
  // ---------- nav menu beside Settings ----------
  // The standalone secondary pages (Wanted, RSS, Archive, Statistics,
  // Settings) link to the main-page actions (Log, Tools, Search indexers)
  // through /?show=…. The dropdown itself is toggled here; the main page
  // uses its own menu system (data-navshow buttons) and is skipped.
  document.addEventListener("click", function (e) {
    var menu = document.getElementById("nav-menu");
    if (!menu || menu.querySelector("[data-navshow]")) return;
    var btn = e.target.closest && e.target.closest(".navmenu > button");
    if (btn) {
      var open = menu.hidden;
      menu.hidden = !open;
      btn.setAttribute("aria-expanded", open ? "true" : "false");
      if (open) {
        var r = btn.getBoundingClientRect();
        menu.style.left = Math.max(8, Math.min(r.left, window.innerWidth - 230)) + "px";
        menu.style.top = (r.bottom + 4) + "px";
        var first = menu.querySelector("[role=menuitem]");
        if (first) first.focus();
      }
      return;
    }
    if (!menu.hidden && (!e.target.closest || !e.target.closest(".navmenu"))) {
      menu.hidden = true;
      var b = document.querySelector(".navmenu > button");
      if (b) b.setAttribute("aria-expanded", "false");
    }
  });
  document.addEventListener("keydown", function (e) {
    if (e.key !== "Escape") return;
    var menu = document.getElementById("nav-menu");
    if (!menu || menu.hidden || menu.querySelector("[data-navshow]")) return;
    menu.hidden = true;
    var b = document.querySelector(".navmenu > button");
    if (b) { b.setAttribute("aria-expanded", "false"); b.focus(); }
  });
  function init() {
    apply(read());
    // The daemon page is installable as an app: the service worker caches only
    // the shared assets, never an authenticated page or an API answer.
    if ("serviceWorker" in navigator) {
      window.addEventListener("load", function () {
        navigator.serviceWorker.register("/sw.js").catch(function () { /* plain HTTP or a proxy: the page still works */ });
      });
    }
    // A server notice (redirect after a classic form post) becomes a toast.
    var n = document.querySelector(".notice[data-toast]");
    if (n) { toast(n.textContent.trim(), n.classList.contains("err")); n.remove(); }
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
  else init();
})();
