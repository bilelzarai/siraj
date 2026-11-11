/* =============================================================================
   Sirāj Admin — the console behaviours the design kit does not ship.

   Three things, each of which the kit draws a control for and leaves inert, or
   which the kit implements against a contract this application cannot use:

     1. Reorder   — drag the category table into the order players see.
     2. Narrowing — the category select follows the chosen domain.
     3. Search    — the top bar's field answers across users, questions and
                    tickets.

   Progressive throughout. Every one of these sits on top of something that
   already works without script: the order is also a field on each category's
   edit page, the category select without narrowing is a longer select, and the
   search field is a plain form that submits to the question bank. That is why
   the save bar and the results panel start hidden in the markup rather than
   being hidden from here — a browser that never runs this file never shows a
   control it cannot honour.
   ========================================================================== */

(function reorder() {
  "use strict";

  const table = document.querySelector("table[data-reorder]");
  if (!table || !table.tBodies.length) return;

  const body = table.tBodies[0];
  const form = document.querySelector("[data-reorder-form]");
  if (!form) return;

  // Sorting and dragging cannot both be true of one table.
  //
  // The order being dragged is the order players see, and it is read off the
  // rows' positions. Sort the table by name and those positions no longer mean
  // the running order — dropping a row would then save the sorted sequence as
  // the order, which is not what anybody dragging it meant. So a sort puts the
  // handles away until the table is back in its own order.
  //
  // The kit's sort writes aria-sort on the header it sorted by, which is the
  // signal to watch: it needs no cooperation from the sort itself.
  const sorted = () => !!table.querySelector("th[aria-sort]");

  function reflectSort() {
    const off = sorted();
    table.classList.toggle("is-sorted", off);
    table.querySelectorAll("[data-reorder-handle]").forEach((h) => {
      h.setAttribute("aria-disabled", String(off));
      h.tabIndex = off ? -1 : 0;
    });
    if (off) form.hidden = true;
  }

  new MutationObserver(reflectSort).observe(table, {
    subtree: true,
    attributes: true,
    attributeFilter: ["aria-sort"],
  });
  reflectSort();

  // The sequence as the server last rendered it. Cancel restores this, and
  // comparing against it is what decides whether the save bar is shown at all
  // — dragging a row and putting it back is not a change.
  const original = rowIDs();

  function rows() {
    return Array.from(body.rows).filter((r) => r.dataset.id);
  }

  function rowIDs() {
    return rows().map((r) => r.dataset.id);
  }

  // The form carries one field per row rather than a delimited string, so the
  // browser does the encoding and a value can never split an id in half.
  function sync() {
    const ids = rowIDs();
    const changed = ids.join() !== original.join();

    form.querySelectorAll('input[name="id"]').forEach((i) => i.remove());
    if (changed) {
      ids.forEach((id) => {
        const field = document.createElement("input");
        field.type = "hidden";
        field.name = "id";
        field.value = id;
        form.appendChild(field);
      });
    }
    form.hidden = !changed;
  }

  /* ---------- pointer ---------- */

  let dragging = null;

  body.addEventListener("pointerdown", (e) => {
    const handle = e.target.closest("[data-reorder-handle]");
    if (!handle || sorted()) return;
    dragging = handle.closest("tr");
    if (!dragging) return;
    // Capture, so a quick drag that leaves the row still delivers its moves
    // here instead of to whatever is under the pointer.
    handle.setPointerCapture(e.pointerId);
    dragging.classList.add("is-dragging");
    e.preventDefault();
  });

  body.addEventListener("pointermove", (e) => {
    if (!dragging) return;
    // Which row is under the pointer now. Compared by midpoint rather than by
    // edge, so a row swaps when the pointer passes its centre and does not
    // flicker back and forth along a boundary.
    const over = document
      .elementsFromPoint(e.clientX, e.clientY)
      .map((el) => el.closest && el.closest("tr"))
      .find((r) => r && r !== dragging && r.parentElement === body);
    if (!over) return;

    const box = over.getBoundingClientRect();
    const after = e.clientY > box.top + box.height / 2;
    body.insertBefore(dragging, after ? over.nextSibling : over);
  });

  function drop() {
    if (!dragging) return;
    dragging.classList.remove("is-dragging");
    dragging = null;
    sync();
  }

  body.addEventListener("pointerup", drop);
  body.addEventListener("pointercancel", drop);

  /* ---------- keyboard ---------- */

  body.addEventListener("keydown", (e) => {
    const handle = e.target.closest("[data-reorder-handle]");
    if (!handle || sorted()) return;
    const row = handle.closest("tr");
    if (!row) return;

    let moved = false;
    if (e.key === "ArrowUp" && row.previousElementSibling) {
      body.insertBefore(row, row.previousElementSibling);
      moved = true;
    }
    if (e.key === "ArrowDown" && row.nextElementSibling) {
      body.insertBefore(row.nextElementSibling, row);
      moved = true;
    }
    if (!moved) return;

    e.preventDefault();
    // The element moved in the document, which takes the focus ring with it
    // only if we put it back.
    handle.focus();
    sync();
  });

  /* ---------- cancel ---------- */

  form.addEventListener("click", (e) => {
    if (!e.target.closest("[data-reorder-cancel]")) return;
    const byID = new Map(rows().map((r) => [r.dataset.id, r]));
    original.forEach((id) => {
      const row = byID.get(id);
      if (row) body.appendChild(row);
    });
    sync();
  });
})();

/* ---------------------------------------------------------------------------
   The category select follows the chosen domain.

   Not the kit's data-depends, which cannot express this: it is written for a
   client-side filter, so it treats "all" as the unfiltered value, resets a
   hidden selection to the string "all", and only ever runs on a change event.
   The console's filters are server-side — the unfiltered value is 0, because
   that is what the handler reads — and the narrowing has to be correct on
   first paint as well, since the page arrives with a domain already chosen.
   --------------------------------------------------------------------------- */

(function narrow() {
  "use strict";

  const target = document.querySelector("select[data-narrow-by]");
  if (!target) return;
  const source = document.getElementById(target.dataset.narrowBy);
  if (!source) return;

  function apply() {
    const domain = source.value;
    const all = domain === "0" || domain === "";

    target.querySelectorAll("option[data-domain]").forEach((o) => {
      o.hidden = !all && o.dataset.domain !== domain;
    });
    // A group whose every option is hidden is a heading with nothing under it.
    target.querySelectorAll("optgroup").forEach((g) => {
      g.hidden = Array.from(g.querySelectorAll("option")).every((o) => o.hidden);
    });
    // A selection that has just been hidden would still submit. Fall back to
    // "every category", which is the honest reading of "the category I had
    // chosen is not in this domain".
    const chosen = target.selectedOptions[0];
    if (chosen && chosen.hidden) target.value = "0";
  }

  source.addEventListener("change", apply);
  apply();
})();

/* ---------------------------------------------------------------------------
   The top bar's search.

   The field is a real form that submits to the question bank, so it answers
   with no script at all. With script it fetches the server's own rendered
   fragment and drops it under the field, because what it searches — every
   account, the whole bank, every ticket — is not on the page being typed into
   and cannot be filtered client-side.
   --------------------------------------------------------------------------- */

(function search() {
  "use strict";

  const input = document.querySelector("input[data-admin-search]");
  if (!input) return;
  const panel = document.getElementById("admin-search-panel");
  if (!panel) return;

  const endpoint = input.dataset.adminSearch;
  let timer = null;
  let token = 0;
  let lastQuery = null;

  function close() {
    panel.hidden = true;
    input.setAttribute("aria-expanded", "false");
  }

  async function run() {
    const query = input.value.trim();
    if (query === lastQuery) return;
    lastQuery = query;

    if (query.length < 2) {
      close();
      return;
    }
    // Each request carries a sequence number and a late answer is dropped.
    // Without it a slow reply to "sal" can land after a fast reply to
    // "salah" and leave the panel showing results for what was typed before.
    const mine = ++token;
    try {
      const res = await fetch(endpoint + "?q=" + encodeURIComponent(query), {
        headers: { "X-Requested-With": "fetch" },
      });
      if (!res.ok || mine !== token) return;
      panel.innerHTML = await res.text();
      panel.hidden = false;
      input.setAttribute("aria-expanded", "true");
    } catch (e) {
      // Offline, or the request was superseded. The form underneath still
      // submits, so there is nothing to report and nothing to recover.
      close();
    }
  }

  input.setAttribute("role", "combobox");
  input.setAttribute("aria-controls", "admin-search-panel");
  input.setAttribute("aria-expanded", "false");
  input.setAttribute("aria-autocomplete", "list");

  input.addEventListener("input", () => {
    clearTimeout(timer);
    timer = setTimeout(run, 180);
  });

  // Reopening on focus uses what is already in the panel rather than asking
  // again: the answer to the query still in the field has not changed.
  input.addEventListener("focus", () => {
    if (panel.children.length && input.value.trim().length >= 2) {
      panel.hidden = false;
      input.setAttribute("aria-expanded", "true");
    }
  });

  document.addEventListener("click", (e) => {
    if (!e.target.closest(".top-search")) close();
  });

  // Escape closes the panel and nothing else. The kit also listens for it to
  // shut its drawers, which is why this does not stop the event.
  input.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      close();
      return;
    }
    // Down from the field moves into the list, which is what makes the
    // results reachable without a pointer.
    if (e.key === "ArrowDown" && !panel.hidden) {
      const first = panel.querySelector(".search-hit");
      if (first) {
        e.preventDefault();
        first.focus();
      }
    }
  });

  // Arrow keys walk the results, and anything else hands the typing back.
  panel.addEventListener("keydown", (e) => {
    const hits = Array.from(panel.querySelectorAll(".search-hit"));
    const at = hits.indexOf(document.activeElement);
    if (at < 0) return;
    if (e.key === "ArrowDown" && hits[at + 1]) {
      e.preventDefault();
      hits[at + 1].focus();
    }
    if (e.key === "ArrowUp") {
      e.preventDefault();
      (hits[at - 1] || input).focus();
    }
    if (e.key === "Escape") {
      close();
      input.focus();
    }
  });
})();

/* ---------------------------------------------------------------------------
   The two behaviours a native <details> does not have.

   The console uses <details class="disclosure"> for the menus that hold a
   field — a suspension's reason, a review note — because the kit's own menu is
   closed by its global click handler the moment the field is focused. Native
   details is click-safe and needs no script to open, but it does not close on
   Escape or when the reader clicks somewhere else, and a dropdown that stays
   open behind the rest of the page is worse than one that never opened.
   --------------------------------------------------------------------------- */

(function disclosures() {
  "use strict";

  const all = () => Array.from(document.querySelectorAll("details.disclosure[open]"));

  document.addEventListener("click", (e) => {
    all().forEach((d) => {
      if (!d.contains(e.target)) d.open = false;
    });
  });

  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    const open = all();
    if (!open.length) return;
    open.forEach((d) => {
      d.open = false;
      // Focus goes back to what opened it, or it is left nowhere after the
      // panel holding it disappears.
      const summary = d.querySelector("summary");
      if (summary && d.contains(document.activeElement)) summary.focus();
    });
  });
})();

/* ---------------------------------------------------------------------------
   Panels: a drawer whose contents come from the server.

   The design opens a side panel on a row — a user's stats, role and strongest
   categories. Rendering one per row would be fifty copies in every page of the
   directory, most never opened, and each carrying an aggregate computed per
   player. So the page holds one empty drawer and the row says where to fill it
   from.

   The control is a real link to a real page, so with no script it still goes
   somewhere useful; the panel is what happens instead when there is one.
   --------------------------------------------------------------------------- */

(function panels() {
  "use strict";

  const host = document.querySelector("[data-panel-host]");
  if (!host) return;

  let token = 0;

  function open() {
    host.classList.add("is-open");
    host.setAttribute("aria-hidden", "false");
    document.querySelector(".scrim")?.classList.add("is-open");
  }

  document.addEventListener("click", async (e) => {
    const trigger = e.target.closest("[data-panel]");
    if (!trigger) return;
    e.preventDefault();

    const mine = ++token;
    host.innerHTML = '<div class="drawer-body"><p class="help">' +
      (host.dataset.loading || "…") + "</p></div>";
    open();

    try {
      const res = await fetch(trigger.dataset.panel, {
        headers: { "X-Requested-With": "fetch" },
      });
      // A reply to a panel the reader has already moved past is dropped: two
      // quick presses would otherwise leave the first answer in the drawer.
      if (mine !== token) return;
      if (!res.ok) {
        // Nothing to show and nothing to pretend: fall back to the link the
        // trigger already is.
        window.location.href = trigger.getAttribute("href");
        return;
      }
      host.innerHTML = await res.text();
      const focusable = host.querySelector("select, input, button, a");
      if (focusable) focusable.focus();
    } catch (err) {
      window.location.href = trigger.getAttribute("href");
    }
  });
})();

/* ---------------------------------------------------------------------------
   The icon picker writes the field.

   The kit's preview repaints the tile when an icon is pressed, which is the
   half a reader sees; the half the server reads is the text field underneath,
   and the kit leaves that alone. Without this the picker looks like it works
   and saves the icon that was already there.
   --------------------------------------------------------------------------- */

(function iconPicker() {
  "use strict";

  const field = document.querySelector("[data-icon-field]");
  if (!field) return;

  document.addEventListener("click", (e) => {
    const choice = e.target.closest("[data-icon]");
    if (!choice) return;
    field.value = choice.dataset.icon;
    // The preview listens to the field as well as to the picker, so a value
    // typed by hand and one chosen here arrive the same way.
    field.dispatchEvent(new Event("input", { bubbles: true }));
  });

  // And the colour swatches, for the same reason: the kit paints the tile and
  // the text box is what submits.
  const colour = document.querySelector("[data-color-text]");
  if (!colour) return;
  document.addEventListener("click", (e) => {
    const swatch = e.target.closest("[data-color]");
    if (!swatch) return;
    colour.value = swatch.dataset.color;
    colour.dispatchEvent(new Event("input", { bubbles: true }));
  });
})();
