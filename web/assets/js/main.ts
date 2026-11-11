/* Sirāj v2 front-end boot: htmx for server-rendered swaps, Alpine for local UI state only
   (NF-03). Nothing here owns game rules — the server does (NF-05). */
import "../css/app.css";
import htmx from "htmx.org";
import Alpine from "alpinejs";
import focus from "@alpinejs/focus";

import { timerStore, answerKeys } from "./alpine/game";
import { pager } from "./alpine/pager";
import { stars } from "./alpine/stars";
import { overlay } from "./alpine/overlay";
import { theme } from "./alpine/theme";
import { today, hijri, ramadan, nextQuestion } from "./alpine/dates";
import { confetti } from "./alpine/confetti";
import { questionEditor } from "./alpine/question-editor";
import { roomCode } from "./alpine/room-code";
import { composer, voice } from "./alpine/composer";
import { invite } from "./alpine/invite";
import { sjPhone, sjCode, sjPassword } from "./alpine/entry";
import { picks } from "./alpine/picks";
import { upload } from "./alpine/upload";
import { openLiveStream } from "./live";
// This-device rounds (T-39) register their own store when that screen ships.

declare global {
  interface Window {
    htmx: typeof htmx;
    Alpine: typeof Alpine;
    sjToast: (message: string) => void;
  }
}

window.htmx = htmx;
window.Alpine = Alpine;

/* x-trap keeps the keyboard inside an open modal or drawer (NF-02). */
Alpine.plugin(focus);

/* The question timer is a store so it survives the htmx swap of the card. */
Alpine.store("timer", timerStore());

Alpine.data("sjAnswerKeys", answerKeys);
Alpine.data("sjPager", pager);
Alpine.data("sjStars", stars);
Alpine.data("sjOverlay", overlay);
Alpine.data("sjTheme", theme);
Alpine.data("sjToday", today);
Alpine.data("sjHijri", hijri);
Alpine.data("sjRamadan", ramadan);
Alpine.data("sjNextQuestion", nextQuestion);
Alpine.data("sjConfetti", confetti);
Alpine.data("sjQuestionEditor", questionEditor);
Alpine.data("sjRoomCode", roomCode);
Alpine.data("sjComposer", composer);
Alpine.data("sjVoice", voice);
Alpine.data("sjInvite", invite);
Alpine.data("sjPhone", sjPhone);
Alpine.data("sjCode", sjCode);
Alpine.data("sjPassword", sjPassword);
Alpine.data("sjPicks", picks);
Alpine.data("sjUpload", upload);

/* The server pushes; the screens pull what changed (NF-02). */
if (document.getElementById("live")) openLiveStream();

/* Toasts: the server can ask for one with the HX-Trigger header {"sj:toast":"…"}. */
function toast(message: string) {
  let box = document.querySelector(".toasts");
  if (!box) {
    box = document.createElement("div");
    box.className = "toasts";
    box.setAttribute("aria-live", "polite");
    document.body.appendChild(box);
  }
  const el = document.createElement("div");
  el.className = "toast";
  el.innerHTML = '<svg class="i" aria-hidden="true"><use href="/static/icons.svg#i-check"></use></svg><span></span>';
  el.querySelector("span")!.textContent = message;
  box.appendChild(el);
  setTimeout(() => el.remove(), 3800);
}
window.sjToast = toast;
document.body.addEventListener("sj:toast", (e: Event) => {
  const detail = (e as CustomEvent<string | { value: string }>).detail;
  toast(typeof detail === "string" ? detail : detail?.value ?? "");
});

/* Every htmx request carries the CSRF token of the page (middleware.CSRF), and an
   answer carries how long the player took. The server measures the same thing from
   the moment it served the question and uses its own number for the score (NF-05);
   this one only helps when there is no stamp (very old games). */
document.body.addEventListener("htmx:configRequest", (e: Event) => {
  const detail = (e as CustomEvent<{ headers: Record<string, string>; parameters: Record<string, unknown> }>).detail;
  const token = document.querySelector<HTMLMetaElement>('meta[name="csrf-token"]')?.content;
  if (token) detail.headers["X-CSRF-Token"] = token;
  if ("time_ms" in detail.parameters) {
    detail.parameters.time_ms = (window.Alpine?.store("timer") as { elapsedMs?: number } | undefined)?.elapsedMs ?? 0;
  }
});

/* ---------- what happens when a request does not go well ----------

   Every htmx request in the app lands here, so a failure says something instead of
   leaving a button that looks like it did nothing. The server's own message is
   preferred — it is already translated — and the generic line is the fallback for a
   failure that never reached a handler. */

/* The one translated fallback, read off the page rather than written in English
   here: this file cannot see the locale files (§14). */
const failureMessage = () =>
  document.querySelector<HTMLMetaElement>('meta[name="error-message"]')?.content ||
  "Something went wrong. Please try again.";

const announceFailure = (xhr?: XMLHttpRequest) => {
  let message = failureMessage();
  try {
    /* An error from the API carries {"error":{message}}; one from a page does not. */
    const body = xhr?.responseText ? JSON.parse(xhr.responseText) : null;
    if (body?.error?.message) message = body.error.message;
  } catch {
    /* Not JSON: the generic line is the honest thing to show. */
  }
  document.body.dispatchEvent(new CustomEvent("sj:toast", { detail: message, bubbles: true }));
};

document.body.addEventListener("htmx:responseError", (e: Event) => {
  const xhr = (e as CustomEvent<{ xhr: XMLHttpRequest }>).detail.xhr;
  /* A 401 means the session is gone: go to the welcome screen rather than explain. */
  if (xhr.status === 401) {
    window.location.assign("/welcome");
    return;
  }
  announceFailure(xhr);
});

/* No response at all — offline, a dropped connection, a server being deployed. */
document.body.addEventListener("htmx:sendError", () => announceFailure());
document.body.addEventListener("htmx:timeout", () => announceFailure());

/* ---------- a request in flight ----------

   Two things every request needs and no template should have to remember: the
   element that started it says so, and it cannot be started twice. htmx disables a
   submitter for the length of its own request, but a link or a div with hx-get has
   no such thing — and a double tap on a phone is one tap too many. */
document.body.addEventListener("htmx:beforeRequest", (e: Event) => {
  const el = (e as CustomEvent<{ elt: Element }>).detail.elt as HTMLElement;
  el.setAttribute("aria-busy", "true");
  el.classList.add("is-loading");
  if (el instanceof HTMLButtonElement || el instanceof HTMLInputElement) el.disabled = true;
});

const settle = (e: Event) => {
  const el = (e as CustomEvent<{ elt: Element }>).detail.elt as HTMLElement;
  if (!el) return;
  el.removeAttribute("aria-busy");
  el.classList.remove("is-loading");
  if (el instanceof HTMLButtonElement || el instanceof HTMLInputElement) el.disabled = false;
};
document.body.addEventListener("htmx:afterRequest", settle);
document.body.addEventListener("htmx:responseError", settle);
document.body.addEventListener("htmx:sendError", settle);

Alpine.start();
