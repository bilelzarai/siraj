/* The admin console's entry point: its own stylesheet (the Sirāj Admin design) and the
   data-attribute behaviours from the design kit (theme, nav, drawers, modals, menus,
   tabs, segmented controls, table selection, filters, sorting, toasts, counters).

   htmx is here for one thing: the drawers. A row's edit button asks the server for
   the drawer with that row in it and drops it into #drawer-slot; without JavaScript
   the same href opens the same form as a page (NF-02). */
import htmx from "htmx.org";
import "../css/admin.css";
import "./admin-ui.js";

declare global {
  interface Window {
    htmx: typeof htmx;
    sirajAdminInit?: (root?: Element | Document) => void;
    sirajAdminClose?: () => void;
  }
}

window.htmx = htmx;

/* Every request carries the page's CSRF token: the console writes with cookies. */
document.body.addEventListener("htmx:configRequest", (e: Event) => {
  const detail = (e as CustomEvent<{ headers: Record<string, string> }>).detail;
  const token = document.querySelector<HTMLMetaElement>('meta[name="csrf-token"]')?.content;
  if (token) detail.headers["X-CSRF-Token"] = token;
});

/* A drawer that has just arrived needs its tabs, counters and pickers bound, and
   the overlay opened over the page. */
document.body.addEventListener("htmx:afterSwap", (e: Event) => {
  const target = (e as CustomEvent<{ target: Element }>).detail.target;
  if (target && target.id === "drawer-slot") window.sirajAdminInit?.(target);
});

/* The server closes a drawer by answering with an empty slot plus this header. */
document.body.addEventListener("htmx:afterRequest", (e: Event) => {
  const xhr = (e as CustomEvent<{ xhr: XMLHttpRequest }>).detail.xhr;
  if (xhr && xhr.getResponseHeader("X-Siraj-Close") === "1") window.sirajAdminClose?.();
});
