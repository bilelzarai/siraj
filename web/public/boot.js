/* =============================================================================
   Sirāj — theme boot

   Loaded synchronously in <head> so the applied theme is settled before first
   paint. It lives in its own file rather than inline in the document because
   the Content-Security-Policy allows scripts from 'self' only: an inline
   <script> would need a nonce or a hash, and a separate file needs neither.

   The server has already written data-theme from the account preference (or
   the cookie). This only overrides it when *this browser* holds a choice:
   clearing the attribute unconditionally meant a signed-in user on a fresh
   browser got the light theme while the appearance button announced "Dark".
   ========================================================================== */

(function () {
  "use strict";

  var root = document.documentElement;
  try {
    var t = localStorage.getItem("theme");
    if (t === "light" || t === "dark") {
      root.setAttribute("data-theme", t);
      return;
    }
    if (t === "system") {
      root.removeAttribute("data-theme");
      return;
    }
    // Nothing stored yet: adopt whatever the server rendered, so the choice
    // survives into this browser instead of being thrown away on first load.
    var s = root.getAttribute("data-theme");
    localStorage.setItem("theme", s === "light" || s === "dark" ? s : "system");
  } catch (e) {
    /* private mode, or storage disabled — the server-rendered theme stands */
  }
})();
