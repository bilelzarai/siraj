/* =============================================================================
   Shared client helpers.

   Both halves of the client use these — the Alpine components and the plain
   modules alike — which is why they are their own file rather than a closure
   inside one of them. Every caller imports from here; nothing here touches
   application state.
   ========================================================================== */

export const $  = (sel, root) => (root || document).querySelector(sel);
export const $$ = (sel, root) => Array.from((root || document).querySelectorAll(sel));

/* ------------------------------------------------------------- helpers */

export function csrfToken() {
  const match = document.cookie.match(/(?:^|;\s*)siraj_csrf=([^;]+)/);
  return match ? decodeURIComponent(match[1]) : "";
}

export async function postJSON(url, body) {
  const res = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Accept": "application/json",
      "X-Requested-With": "fetch",
      "X-CSRF-Token": csrfToken(),
    },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    // The server says why a refusal happened — too large, wrong kind — in
    // the reader's own language. Throwing a generic failure over the top of
    // it turns an answerable problem into a shrug.
    let said = "";
    try {
      said = ((await res.json()) || {}).error || "";
    } catch (e) {}
    const err = new Error(said || "request failed");
    err.status = res.status;
    err.said = Boolean(said);
    throw err;
  }
  return res.json();
}

export async function postForm(url, data) {
  const res = await fetch(url, {
    method: "POST",
    headers: {
      "Accept": "application/json",
      "X-Requested-With": "fetch",
      "X-CSRF-Token": csrfToken(),
    },
    body: data,
  });
  if (!res.ok) {
    // The server says why a refusal happened — too large, wrong kind — in
    // the reader's own language. Throwing a generic failure over the top of
    // it turns an answerable problem into a shrug.
    let said = "";
    try {
      said = ((await res.json()) || {}).error || "";
    } catch (e) {}
    const err = new Error(said || "request failed");
    err.status = res.status;
    err.said = Boolean(said);
    throw err;
  }
  return res.json();
}

export function toast(text, kind) {
  const host = $("#toasts");
  if (!host) return;
  const el = document.createElement("div");
  el.className = "toast" + (kind ? " toast--" + kind : "");
  el.textContent = text;
  host.appendChild(el);
  setTimeout(() => {
    el.style.transition = "opacity .3s, transform .3s";
    el.style.opacity = "0";
    el.style.transform = "translateY(8px)";
    setTimeout(() => el.remove(), 320);
  }, 3200);
}

// The server hands the browser its strings in <template> elements, so no
// English is written in this file. A template's children live in .content,
// not as child nodes — reading .textContent off the element itself returns
// the empty string, which is what every one of these was doing: the verdict
// after an answer, "message withdrawn", the saved and error notices. All
// silently blank, because an empty string is a plausible-looking label.
export function tmplText(name) {
  const el = $("[data-i18n-" + name + "]");
  if (!el) return "";
  const source = el.content || el;
  return (source.textContent || "").trim();
}
