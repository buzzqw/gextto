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

  document.addEventListener("click", function (e) {
    var btn = e.target.closest && e.target.closest("[data-theme-switch]");
    if (btn) { e.preventDefault(); cycle(); }
  });
  function init() {
    apply(read());
    // A server notice (redirect after a classic form post) becomes a toast.
    var n = document.querySelector(".notice[data-toast]");
    if (n) { toast(n.textContent.trim(), n.classList.contains("err")); n.remove(); }
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
  else init();
})();
