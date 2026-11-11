/* Siraj Admin — UI behaviour (no dependencies)
   Everything is wired with data-attributes so it survives being ported
   into any template engine:
     data-theme-toggle            → light/dark switch (persisted)
     data-nav-toggle              → open/close the sidebar on mobile
     data-open="id" / data-close  → drawers + modals (with scrim, Esc, focus)
     data-menu                    → dropdown menu trigger (next .menu)
     data-tabs                    → tab list; buttons carry aria-controls
     data-segmented               → single-choice segmented control
     data-select-all / data-row   → table row selection + bulk bar
     data-filter="tableId"        → live text filter on a table
     data-sort                    → click-to-sort table header
     data-toast="message"         → show a toast on click
     data-confirm="modalId"       → open a confirm modal; fills [data-confirm-name]
     data-count="inputId"         → live character counter
     data-pressed-group           → aria-pressed single choice (icon/colour pickers)
     data-copy="text"             → copy to the clipboard (pairs with data-toast)
     data-shortcuts               → one-key decisions inside it: [data-key="a"] …
     data-day / data-day-toggle   → fold one day of the audit log away
*/
(function () {
  "use strict";
  const $ = (s, r = document) => r.querySelector(s);
  const $$ = (s, r = document) => Array.from(r.querySelectorAll(s));
  const root = document.documentElement;

  /* ---------- theme ---------- */
  const THEME_KEY = "siraj-admin-theme";
  function storedTheme() { try { return localStorage.getItem(THEME_KEY); } catch (e) { return null; } }
  function applyTheme(t) {
    root.setAttribute("data-theme", t);
    $$("[data-theme-toggle]").forEach((b) => {
      b.setAttribute("aria-pressed", t === "dark");
      b.setAttribute("aria-label", t === "dark" ? "Switch to light theme" : "Switch to dark theme");
      const use = b.querySelector("use");
      if (use) use.setAttribute("href", t === "dark" ? "#i-sun" : "#i-moon");
    });
  }
  applyTheme(storedTheme() || (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"));
  document.addEventListener("click", (e) => {
    const b = e.target.closest("[data-theme-toggle]");
    if (!b) return;
    const next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
    applyTheme(next);
    try { localStorage.setItem(THEME_KEY, next); } catch (err) {}
  });

  /* ---------- mobile nav ---------- */
  document.addEventListener("click", (e) => {
    if (e.target.closest("[data-nav-toggle]")) {
      document.body.classList.toggle("nav-open");
      scrim(document.body.classList.contains("nav-open"));
    }
  });

  /* ---------- scrim / overlays ---------- */
  let scrimEl = $(".scrim");
  if (!scrimEl) { scrimEl = document.createElement("div"); scrimEl.className = "scrim"; document.body.appendChild(scrimEl); }
  function scrim(on) { scrimEl.classList.toggle("is-open", !!on); }
  let lastFocus = null;
  function openOverlay(el) {
    if (!el) return;
    lastFocus = document.activeElement;
    el.classList.add("is-open");
    el.setAttribute("aria-hidden", "false");
    scrim(true);
    const f = el.querySelector("[autofocus], input, select, textarea, button:not([data-close])");
    if (f) setTimeout(() => f.focus(), 60);
  }
  function closeOverlays() {
    $$(".drawer.is-open, .modal.is-open").forEach((el) => { el.classList.remove("is-open"); el.setAttribute("aria-hidden", "true"); });
    document.body.classList.remove("nav-open");
    scrim(false);
    if (lastFocus) lastFocus.focus();
  }
  document.addEventListener("click", (e) => {
    const opener = e.target.closest("[data-open]");
    if (opener) { e.preventDefault(); openOverlay(document.getElementById(opener.dataset.open)); return; }
    if (e.target.closest("[data-close]") || e.target === scrimEl) closeOverlays();
  });

  /* confirm modal: copies the row name into the modal */
  document.addEventListener("click", (e) => {
    const b = e.target.closest("[data-confirm]");
    if (!b) return;
    const m = document.getElementById(b.dataset.confirm);
    if (!m) return;
    $$("[data-confirm-name]", m).forEach((n) => (n.textContent = b.dataset.name || ""));
    openOverlay(m);
  });

  /* ---------- menus ---------- */
  document.addEventListener("click", (e) => {
    const t = e.target.closest("[data-menu]");
    $$(".menu.is-open").forEach((m) => { if (!t || m !== t.nextElementSibling) m.classList.remove("is-open"); });
    if (t) {
      const m = t.nextElementSibling;
      const open = m.classList.toggle("is-open");
      t.setAttribute("aria-expanded", open);
    }
  });

  /* ---------- tabs ---------- */
  function bindTabs(root) {
  $$("[data-tabs]", root).forEach((list) => {
    if (list.dataset.bound) return;
    list.dataset.bound = "1";
    const tabs = $$("[role=tab]", list);
    function select(tab) {
      tabs.forEach((t) => {
        const on = t === tab;
        t.setAttribute("aria-selected", on);
        t.tabIndex = on ? 0 : -1;
        const p = document.getElementById(t.getAttribute("aria-controls"));
        if (p) p.hidden = !on;
      });
    }
    list.addEventListener("click", (e) => { const t = e.target.closest("[role=tab]"); if (t) select(t); });
    list.addEventListener("keydown", (e) => {
      const i = tabs.indexOf(document.activeElement);
      if (i < 0) return;
      const dir = getComputedStyle(list).direction === "rtl" ? -1 : 1;
      if (e.key === "ArrowRight") { const n = tabs[(i + dir + tabs.length) % tabs.length]; n.focus(); select(n); }
      if (e.key === "ArrowLeft") { const n = tabs[(i - dir + tabs.length) % tabs.length]; n.focus(); select(n); }
    });
  });
  }
  bindTabs(document);

  /* ---------- segmented + pressed groups ---------- */
  document.addEventListener("click", (e) => {
    const b = e.target.closest("[data-segmented] button, [data-pressed-group] button");
    if (!b) return;
    const g = b.parentElement;
    $$("button", g).forEach((x) => x.setAttribute("aria-pressed", x === b));
    if (g.dataset.filterTable) filterBySegment(g.dataset.filterTable, b.dataset.value);
    /* A picker in a form writes what was chosen into the field that gets posted:
       the level, the family, the icon, the motif and the colour of a drawer. */
    if (g.dataset.sets) {
      const form = g.closest("form") || document;
      const field = form.querySelector('[data-field="' + g.dataset.sets + '"]');
      if (field) field.value = b.dataset.value || "";
    }
  });
  function filterBySegment(tableId, val) {
    const t = document.getElementById(tableId);
    if (!t) return;
    $$("tbody tr[data-state]", t).forEach((r) => { r.hidden = !(val === "all" || r.dataset.state === val); });
  }

  /* ---------- table: selection ---------- */
  $$("table[data-selectable]").forEach((table) => {
    const all = $("[data-select-all]", table);
    const bar = document.getElementById(table.dataset.selectable);
    const rows = () => $$("[data-row]", table);
    function sync() {
      const r = rows(), n = r.filter((c) => c.checked).length;
      r.forEach((c) => c.closest("tr").classList.toggle("is-selected", c.checked));
      if (all) { all.checked = n && n === r.length; all.indeterminate = n > 0 && n < r.length; }
      if (bar) { bar.classList.toggle("is-on", n > 0); const c = $("[data-selected-count]", bar); if (c) c.textContent = n; }
    }
    table.addEventListener("change", (e) => {
      if (e.target.matches("[data-select-all]")) rows().forEach((c) => (c.checked = e.target.checked));
      sync();
    });
    if (bar) $$("[data-clear-selection]", bar).forEach((b) => b.addEventListener("click", () => { rows().forEach((c) => (c.checked = false)); sync(); }));
  });

  /* ---------- table: text filter ---------- */
  $$("[data-filter]").forEach((input) => {
    const t = document.getElementById(input.dataset.filter);
    if (!t) return;
    const out = document.querySelector(`[data-filter-count="${input.dataset.filter}"]`);
    input.addEventListener("input", () => {
      const q = input.value.trim().toLowerCase();
      let n = 0;
      $$("tbody tr", t).forEach((r) => {
        if (r.classList.contains("day-sep")) return;
        const hit = !q || r.textContent.toLowerCase().includes(q);
        r.hidden = !hit; if (hit) n++;
      });
      if (out) out.textContent = n;
    });
  });

  /* ---------- table: sort ---------- */
  $$("th[data-sort]").forEach((th) => {
    th.classList.add("sortable");
    th.tabIndex = 0;
    const go = () => {
      const table = th.closest("table"), body = table.tBodies[0];
      const idx = Array.from(th.parentElement.children).indexOf(th);
      const asc = th.getAttribute("aria-sort") !== "ascending";
      $$("th[aria-sort]", table).forEach((x) => x.removeAttribute("aria-sort"));
      th.setAttribute("aria-sort", asc ? "ascending" : "descending");
      const num = th.dataset.sort === "num";
      const val = (r) => { const c = r.children[idx]; const v = (c && (c.dataset.value ?? c.textContent)).trim(); return num ? parseFloat(v.replace(/[^\d.-]/g, "")) || 0 : v.toLowerCase(); };
      Array.from(body.rows).sort((a, b) => (val(a) > val(b) ? 1 : val(a) < val(b) ? -1 : 0) * (asc ? 1 : -1)).forEach((r) => body.appendChild(r));
    };
    th.addEventListener("click", go);
    th.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); go(); } });
  });

  /* ---------- combined filters ----------
     Any control with data-filter-for="targetId" data-key="x" filters the
     target's rows (tbody tr) or items ([data-item]) by their data-x value.
       select / input[type=search] → value ("all" or "" = no filter; key "q" = text search)
       .segmented / [data-chips]   → the pressed button's data-value
     Row values may be space-separated lists (data-status="open mine").
     data-depends="selectId" on a category select hides options whose
     data-domain does not match the chosen domain.
     [data-filter-count="targetId"] shows the visible count; [data-empty] shows when 0. */
  function controlValue(c) {
    if (c.matches("select, input")) return c.value.trim().toLowerCase();
    const b = c.querySelector('[aria-pressed="true"]');
    return b ? (b.dataset.value || "all") : "all";
  }
  function applyFilters(id) {
    const target = document.getElementById(id);
    if (!target) return;
    const items = $$("tbody tr:not(.day-sep):not([data-empty]), [data-item]", target);
    const crit = $$(`[data-filter-for="${id}"]`).map((c) => ({ key: c.dataset.key, v: controlValue(c) }));
    let n = 0;
    items.forEach((r) => {
      const ok = crit.every(({ key, v }) => {
        if (!v || v === "all") return true;
        if (key === "q") return r.textContent.toLowerCase().includes(v);
        return (r.dataset[key] || "").toLowerCase().split(" ").includes(v);
      });
      r.hidden = !ok;
      if (ok) n++;
    });
    $$(`[data-filter-count="${id}"]`).forEach((o) => (o.textContent = n));
    const empty = $("[data-empty]", target);
    if (empty) empty.hidden = n > 0;
  }
  function syncDepends(sel) {
    const src = document.getElementById(sel.dataset.depends);
    if (!src) return;
    const d = src.value;
    $$("option[data-domain]", sel).forEach((o) => { o.hidden = d !== "all" && o.dataset.domain !== d; });
    $$("optgroup", sel).forEach((g) => { g.hidden = $$("option", g).every((o) => o.hidden); });
    if (sel.selectedOptions[0] && sel.selectedOptions[0].hidden) sel.value = "all";
  }
  document.addEventListener("input", (e) => { const c = e.target.closest("[data-filter-for]"); if (c && c.matches("input")) applyFilters(c.dataset.filterFor); });
  document.addEventListener("change", (e) => {
    const c = e.target.closest("select[data-filter-for]");
    if (!c) return;
    $$(`select[data-depends="${c.id}"]`).forEach(syncDepends);
    applyFilters(c.dataset.filterFor);
  });
  document.addEventListener("click", (e) => {
    const b = e.target.closest("[data-filter-for] button");
    if (b) {
      const g = b.closest("[data-filter-for]");
      if (g.hasAttribute("data-chips")) {
        const was = b.getAttribute("aria-pressed") === "true";
        $$("button", g).forEach((x) => x.setAttribute("aria-pressed", "false"));
        b.setAttribute("aria-pressed", String(!was));
      }
      applyFilters(g.dataset.filterFor);
    }
    const clr = e.target.closest("[data-clear-filters]");
    if (clr) {
      const id = clr.dataset.clearFilters;
      $$(`[data-filter-for="${id}"]`).forEach((c) => {
        if (c.matches("select")) c.selectedIndex = 0;
        else if (c.matches("input")) c.value = "";
        else $$("button", c).forEach((x, i) => x.setAttribute("aria-pressed", String(!c.hasAttribute("data-chips") && i === 0)));
      });
      $$(`select[data-depends]`).forEach(syncDepends);
      applyFilters(id);
    }
  });

  /* KPI tiles that jump to a filter: data-set-filter="targetId:key:value" */
  document.addEventListener("click", (e) => {
    const b = e.target.closest("[data-set-filter]");
    if (!b) return;
    const [id, key, val] = b.dataset.setFilter.split(":");
    $$(`[data-filter-for="${id}"]`).forEach((c) => {
      if (c.matches("input")) return;
      if (c.matches("select")) { c.value = c.dataset.key === key ? val : "all"; return; }
      $$("button", c).forEach((x) => x.setAttribute("aria-pressed", String(c.dataset.key === key ? x.dataset.value === val : (!c.hasAttribute("data-chips") && x.dataset.value === "all"))));
    });
    applyFilters(id);
  });
  new Set($$("[data-filter-for]").map((c) => c.dataset.filterFor)).forEach(applyFilters);

  /* ---------- show/hide panels (compare views etc.) ---------- */
  document.addEventListener("click", (e) => {
    const b = e.target.closest("[data-toggle]");
    if (!b) return;
    const p = document.getElementById(b.dataset.toggle);
    if (!p) return;
    p.hidden = !p.hidden;
    b.setAttribute("aria-expanded", String(!p.hidden));
    if (b.dataset.labelOpen) b.querySelector("span").textContent = p.hidden ? b.dataset.labelClosed : b.dataset.labelOpen;
  });

  /* ---------- dismiss a card (e.g. "Different questions") ---------- */
  document.addEventListener("click", (e) => {
    const b = e.target.closest("[data-dismiss]");
    if (!b) return;
    const card = b.closest("[data-dismissable]");
    if (card) card.remove();
    $$("[data-dismiss-count]").forEach((c) => (c.textContent = Math.max(0, parseInt(c.textContent, 10) - 1)));
  });

  /* ---------- toast ---------- */
  let toasts = $(".toasts");
  if (!toasts) { toasts = document.createElement("div"); toasts.className = "toasts"; toasts.setAttribute("aria-live", "polite"); document.body.appendChild(toasts); }
  window.sirajToast = function (msg, undo) {
    const t = document.createElement("div");
    t.className = "toast";
    t.innerHTML = '<svg class="icon" aria-hidden="true"><use href="#i-check-circle"/></svg><span></span>' + (undo ? "<button type=button>Undo</button>" : "");
    t.querySelector("span").textContent = msg;
    toasts.appendChild(t);
    setTimeout(() => t.remove(), 4200);
  };
  document.addEventListener("click", (e) => {
    const b = e.target.closest("[data-toast]");
    if (!b) return;
    if (b.closest(".modal, .drawer")) closeOverlays();
    sirajToast(b.dataset.toast, b.hasAttribute("data-undo"));
  });

  /* ---------- char counters ---------- */
  function bindCounters(root) {
    $$("[data-count]", root).forEach((out) => {
      if (out.dataset.bound) return;
      out.dataset.bound = "1";
      const inp = document.getElementById(out.dataset.count);
      if (!inp) return;
      const max = inp.getAttribute("maxlength");
      const upd = () => (out.textContent = inp.value.length + (max ? " / " + max : ""));
      inp.addEventListener("input", upd); upd();
    });
  }
  bindCounters(document);

  /* ---------- slug auto-fill ---------- */
  function bindSlugs(root) {
  $$("[data-slug-from]", root).forEach((slug) => {
    if (slug.dataset.bound) return;
    slug.dataset.bound = "1";
    const src = document.getElementById(slug.dataset.slugFrom);
    if (!src) return;
    let touched = false;
    slug.addEventListener("input", () => (touched = true));
    src.addEventListener("input", () => {
      if (touched) return;
      slug.value = src.value.toLowerCase().normalize("NFKD").replace(/[̀-ͯ]/g, "").replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
    });
  });
  }
  bindSlugs(document);

  /* ---------- live preview for category drawer ---------- */
  function bindPreviews(root) {
  $$("[data-preview]", root).forEach((pv) => {
    if (pv.dataset.bound) return;
    pv.dataset.bound = "1";
    const scope = pv.closest(".drawer") || document;
    const name = $("[data-preview-name]", scope), desc = $("[data-preview-desc]", scope);
    const tile = $(".cat-tile", pv);
    if (name) name.addEventListener("input", () => ($("[data-pv-name]", pv).textContent = name.value || "Category name"));
    if (desc) desc.addEventListener("input", () => ($("[data-pv-desc]", pv).textContent = desc.value || "Short description"));
    scope.addEventListener("click", (e) => {
      const ic = e.target.closest("[data-icon]"); if (ic && tile) tile.textContent = ic.dataset.icon;
      const sw = e.target.closest("[data-color]"); if (sw && tile) tile.style.setProperty("--c", sw.dataset.color);
    });
  });
  }
  bindPreviews(document);


  /* ---------- domain drawer preview (colour, icon, motif) ---------- */
  function bindDomainPreview(root) {
  const dp = $("#dom-preview", root);
  if (dp && !dp.dataset.bound) {
    dp.dataset.bound = "1";
    const scope = dp.closest(".drawer") || root;
    const nm = $("[data-preview-name]", scope), ds = $("[data-preview-desc]", scope);
    if (nm) nm.addEventListener("input", () => ($("[data-pv-name]", dp).textContent = nm.value || "Domain name"));
    if (ds) ds.addEventListener("input", () => ($("[data-pv-desc]", dp).textContent = ds.value || "Short description"));
    scope.addEventListener("click", (e) => {
      const sw = e.target.closest("[data-color]"); if (sw) dp.style.setProperty("--c", sw.dataset.color);
      const ic = e.target.closest("[data-icon]"); if (ic) $(".dp-ic", dp).textContent = ic.dataset.icon;
      const mo = e.target.closest("[data-motif]"); if (mo) $(".dp-motif", dp).textContent = mo.dataset.motif;
    });
  }
  }
  bindDomainPreview(document);

  /* ---------- the correct answer, across the language tabs ----------
     Each tab has its own radio group so the choice is visible in all of them;
     clicking one moves the others, because a question has one right answer. */
  document.addEventListener("change", (e) => {
    const r = e.target.closest("input[type=radio][data-correct]");
    if (!r) return;
    const form = r.closest("form") || document;
    $$("input[type=radio][data-correct]", form).forEach((other) => {
      if (other !== r && other.value === r.value) other.checked = true;
    });
  });

  /* ---------- a day of the audit log ----------
     Each day is its own <tbody>, so folding one away is hiding its rows and leaving
     its heading. Without JavaScript every day is simply open, which is the state the
     markup ships in. */
  document.addEventListener("click", (e) => {
    const b = e.target.closest("[data-day-toggle]");
    if (!b) return;
    const body = b.closest("[data-day]");
    if (!body) return;
    const open = b.getAttribute("aria-expanded") !== "false";
    b.setAttribute("aria-expanded", String(!open));
    body.classList.toggle("is-closed", open);
  });

  /* ---------- copy to clipboard ----------
     The "Copy link" item of A07's menu. data-toast says so, through the handler
     that attribute already has. */
  document.addEventListener("click", (e) => {
    const b = e.target.closest("[data-copy]");
    if (!b) return;
    const text = b.getAttribute("data-copy") || "";
    const absolute = text.startsWith("http") ? text : location.origin + text;
    if (navigator.clipboard) navigator.clipboard.writeText(absolute).catch(() => {});
  });

  /* ---------- one-key decisions (A07) ----------
     A moderator works down the queue with the keyboard: A approves, E asks for
     changes, R rejects, S skips. The keys are shown on the buttons themselves, and
     they only fire while nothing is being typed into — a note to the author must be
     able to contain the letter "a". */
  document.addEventListener("keydown", (e) => {
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    const el = document.activeElement;
    if (el && (el.matches("input, textarea, select") || el.isContentEditable)) return;
    const scope = $("[data-shortcuts]");
    if (!scope) return;
    const target = $(`[data-key="${e.key.toLowerCase()}"]`, scope);
    if (!target) return;
    e.preventDefault();
    target.click();
  });

  /* ---------- drawers the server sends ----------
     A drawer fetched with htmx arrives after everything above has run, so the
     behaviours inside it are bound here, and the overlay is opened for it. */
  window.sirajAdminInit = function (root) {
    root = root || document;
    bindTabs(root); bindCounters(root); bindSlugs(root); bindPreviews(root); bindDomainPreview(root);
    const panel = root.matches && root.matches(".drawer, .modal") ? root : $(".drawer, .modal", root);
    if (panel) openOverlay(panel);
  };
  window.sirajAdminClose = closeOverlays;

  /* ---------- inbox (mobile thread toggle) ---------- */
  document.addEventListener("click", (e) => {
    const t = e.target.closest(".ticket");
    if (t) {
      $$(".ticket").forEach((x) => x.classList.toggle("is-active", x === t));
      t.classList.remove("is-unread");
      const box = t.closest(".inbox"); if (box) box.classList.add("show-thread");
    }
    if (e.target.closest("[data-back-to-list]")) { const box = e.target.closest(".inbox"); if (box) box.classList.remove("show-thread"); }
  });

  /* ---------- keyboard ---------- */
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") { closeOverlays(); $$(".menu.is-open").forEach((m) => m.classList.remove("is-open")); }
    const typing = /input|textarea|select/i.test(document.activeElement.tagName);
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") { e.preventDefault(); const s = $(".top-search input"); if (s) s.focus(); }
    if (!typing && e.key === "/") { e.preventDefault(); const s = $("[data-filter]") || $(".top-search input"); if (s) s.focus(); }
    /* review queue shortcuts */
    if (!typing && document.body.dataset.page === "review") {
      const k = e.key.toLowerCase();
      const map = { a: "[data-key-approve]", r: "[data-key-reject]", e: "[data-key-edit]", s: "[data-key-skip]" };
      if (map[k]) { const b = $(map[k]); if (b) { b.click(); b.focus(); } }
    }
  });
})();
