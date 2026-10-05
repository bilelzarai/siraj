/* =============================================================================
   Sirāj — client runtime
   No framework, no external dependencies. Each block is an independent
   enhancement: if one fails, the rest of the page still works.
   ========================================================================== */

(function () {
  "use strict";

  const $  = (sel, root) => (root || document).querySelector(sel);
  const $$ = (sel, root) => Array.from((root || document).querySelectorAll(sel));

  /* ------------------------------------------------------------- helpers */

  function csrfToken() {
    const match = document.cookie.match(/(?:^|;\s*)siraj_csrf=([^;]+)/);
    return match ? decodeURIComponent(match[1]) : "";
  }

  async function postJSON(url, body) {
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

  async function postForm(url, data) {
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

  function toast(text, kind) {
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
  function tmplText(name) {
    const el = $("[data-i18n-" + name + "]");
    if (!el) return "";
    const source = el.content || el;
    return (source.textContent || "").trim();
  }

  /* --------------------------------------------------------------- theme */

  (function theme() {
    const STORAGE = "theme";

    function current() {
      try { return localStorage.getItem(STORAGE) || "system"; } catch (e) { return "system"; }
    }

    function apply(value) {
      if (value === "light" || value === "dark") {
        document.documentElement.setAttribute("data-theme", value);
      } else {
        document.documentElement.removeAttribute("data-theme");
      }
      try { localStorage.setItem(STORAGE, value); } catch (e) {}
      // Mirror into a cookie so the server renders the same theme, avoiding
      // a flash on the next full page load.
      document.cookie = "theme=" + encodeURIComponent(value) +
        ";path=/;max-age=31536000;samesite=lax";
      syncIcon(value);
      syncSegments(value);
    }

    const ORDER = ["system", "light", "dark"];

    // The button shows the theme that is *applied* and says which one the next
    // press will give you, so the tooltip answers "what theme is on?" instead
    // of the old generic "Appearance".
    function syncIcon(value) {
      const toggle = $("[data-theme-toggle]");
      if (!toggle) return;
      $$("[data-theme-slot]", toggle).forEach((slot) => {
        slot.hidden = slot.dataset.themeSlot !== value;
      });
      const next = ORDER[(ORDER.indexOf(value) + 1) % ORDER.length];
      // The labels are rendered server-side, already translated.
      const label = toggle.dataset["label" + next[0].toUpperCase() + next.slice(1)];
      if (label) {
        toggle.setAttribute("aria-label", label);
        toggle.title = label;
      }
    }

    function syncSegments(value) {
      $$("[data-theme-set]").forEach((btn) => {
        btn.classList.toggle("is-active", btn.dataset.themeSet === value);
      });
    }

    syncIcon(current());
    syncSegments(current());

    const toggle = $("[data-theme-toggle]");
    if (toggle) {
      toggle.addEventListener("click", () => {
        apply(ORDER[(ORDER.indexOf(current()) + 1) % ORDER.length]);
      });
    }

    $$("[data-theme-set]").forEach((btn) => {
      btn.addEventListener("click", () => apply(btn.dataset.themeSet));
    });
  })();

  /* -------------------------------------------------------- script here */

  // Tells the server this browser can speak for itself.
  //
  // It decides who marks a message read. A page with script says so when it is
  // focused, which is the only moment somebody is actually looking; anything
  // else has no way to say so, and for those the server falls back to marking
  // on render. Without the split, either a background tab reports messages as
  // read or nobody ever does.
  document.cookie = "js=1; path=/; max-age=31536000; samesite=lax";

  /* ----------------------------------------------------------- dropdowns */

  (function dropdowns() {
    function closeAll(except) {
      $$("[data-dropdown]").forEach((dd) => {
        if (dd === except) return;
        const menu = $("[data-dropdown-menu]", dd);
        const trigger = $("[data-dropdown-trigger]", dd);
        if (menu) {
          menu.dataset.open = "false";
          if ("dropdownFloat" in dd.dataset) {
            menu.style.position = "";
            menu.style.left = "";
            menu.style.top = "";
            menu.style.insetInlineEnd = "";
            menu.style.insetBlockStart = "";
          }
        }
        if (trigger) trigger.setAttribute("aria-expanded", "false");
      });
    }

    // A menu inside a scrolling panel is clipped by it, however high its
    // z-index. These are lifted out and positioned against the button instead.
    function float(trigger, menu) {
      const box = trigger.getBoundingClientRect();
      menu.style.position = "fixed";
      menu.style.insetInlineEnd = "auto";
      menu.style.insetBlockStart = "auto";
      const width = menu.offsetWidth || 190;
      const rtl = document.documentElement.dir === "rtl";
      const left = rtl ? box.left : box.right - width;
      menu.style.left = Math.max(8, Math.min(left, window.innerWidth - width - 8)) + "px";
      // Below the button, unless that would run off the bottom.
      const height = menu.offsetHeight || 0;
      const below = box.bottom + 6;
      menu.style.top = (below + height > window.innerHeight - 8
        ? Math.max(8, box.top - height - 6)
        : below) + "px";
    }

    function unfloat(menu) {
      menu.style.position = "";
      menu.style.left = "";
      menu.style.top = "";
      menu.style.insetInlineEnd = "";
      menu.style.insetBlockStart = "";
    }

    $$("[data-dropdown]").forEach((dd) => {
      const trigger = $("[data-dropdown-trigger]", dd);
      const menu = $("[data-dropdown-menu]", dd);
      if (!trigger || !menu) return;

      trigger.addEventListener("click", (e) => {
        e.stopPropagation();
        const open = menu.dataset.open === "true";
        closeAll(dd);
        menu.dataset.open = open ? "false" : "true";
        trigger.setAttribute("aria-expanded", open ? "false" : "true");
        if (!("dropdownFloat" in dd.dataset)) return;
        if (open) unfloat(menu);
        else float(trigger, menu);
      });
    });

    document.addEventListener("click", () => closeAll(null));
    // A lifted menu is positioned against where the button was. Scroll the
    // panel under it and it would hang in the air pointing at nothing.
    window.addEventListener("scroll", () => closeAll(null), true);
    // Same reason, for a phone turning on its side: the menu is pinned to a
    // coordinate measured before the rotation, so it would point at nothing.
    window.addEventListener("resize", () => closeAll(null));
    window.addEventListener("orientationchange", () => closeAll(null));
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape") closeAll(null);
    });
  })();

  /* ---------------------------------------------------- password reveal */

  (function passwordReveal() {
    $$("[data-password-toggle]").forEach((btn) => {
      const field = btn.closest(".field__control");
      const input = field && $("[data-password-input]", field);
      if (!input) return;

      function paint(revealed) {
        $$("[data-reveal-slot]", btn).forEach((slot) => {
          slot.hidden = slot.dataset.revealSlot !== (revealed ? "hide" : "show");
        });
        const label = revealed ? btn.dataset.labelHide : btn.dataset.labelShow;
        btn.setAttribute("aria-label", label);
        btn.title = label;
        btn.setAttribute("aria-pressed", String(revealed));
      }

      btn.addEventListener("click", () => {
        const reveal = input.type === "password";
        input.type = reveal ? "text" : "password";
        paint(reveal);

        // Keep the caret where the user left it rather than jumping to the end.
        const pos = input.selectionStart;
        input.focus();
        if (pos !== null) {
          try { input.setSelectionRange(pos, pos); } catch (e) {}
        }
      });

      // Never leave a password on screen after the form is submitted.
      const form = input.form;
      if (form) {
        form.addEventListener("submit", () => {
          input.type = "password";
          paint(false);
        });
      }
    });
  })();

  /* ------------------------------------------------------ question notes */

  (function questionNote() {
    const note = $("[data-note]");
    if (!note) return;

    const root   = $("[data-round]");
    const body   = $("[data-note-body]", note);
    const save   = $("[data-note-save]", note);
    const status = $("[data-note-status]", note);
    if (!root || !body || !save) return;

    save.addEventListener("click", async () => {
      const text = body.value.trim();
      if (text.length < 2) return;

      window.sirajBusy.mark(save);
      try {
        // The server resolves the position to a question and refuses unless
        // that position has already been answered, so this cannot be used to
        // ask about a question before seeing it.
        await postJSON("/play/comment", {
          position: Number(root.dataset.position),
          body: text,
        });
        if (status) status.textContent = tmplText("note-saved");
        note.open = false;
      } catch (err) {
        if (status) status.textContent = tmplText("error");
      } finally {
        window.sirajBusy.clear(save);
      }
    });
  })();

  /* ---------------------------------------------------- question rating */

  (function questionRating() {
    const rate = $("[data-rate]");
    if (!rate) return;

    const root   = $("[data-round]");
    const status = $("[data-rate-status]", rate);
    const stars  = $$("[data-star]", rate);
    if (!root || !stars.length) return;

    let chosen = 0;
    let sending = false;

    // Paint up to n, so hovering the third star lights the first three — the
    // gesture people already expect from a star row.
    function paint(n) {
      stars.forEach((s) => {
        const on = Number(s.dataset.star) <= n;
        s.classList.toggle("is-on", on);
        s.setAttribute("aria-checked", String(Number(s.dataset.star) === chosen));
      });
    }

    stars.forEach((star) => {
      star.addEventListener("mouseenter", () => !sending && paint(Number(star.dataset.star)));
      star.addEventListener("focus", () => !sending && paint(Number(star.dataset.star)));
      star.addEventListener("click", async () => {
        if (sending) return;
        sending = true;
        chosen = Number(star.dataset.star);
        paint(chosen);
        rate.classList.add("is-sending");
        try {
          const res = await postJSON("/play/rate", {
            position: Number(root.dataset.position),
            stars: chosen,
          });
          if (status) {
            status.textContent = res.votes > 1
              ? tmplText("rate-thanks") + " " + res.average.toFixed(1) + "/5"
              : tmplText("rate-thanks");
          }
        } catch (err) {
          chosen = 0;
          paint(0);
          if (status) status.textContent = tmplText("error");
        } finally {
          sending = false;
          rate.classList.remove("is-sending");
        }
      });
    });

    rate.addEventListener("mouseleave", () => paint(chosen));
  })();

  /* ------------------------------------------------- support canned replies */

  (function cannedReplies() {
    const box = $("[data-reply-box]");
    if (!box) return;

    $$("[data-canned]").forEach((btn) => {
      btn.addEventListener("click", () => {
        const text = btn.dataset.canned || "";
        // Insert rather than replace: staff often personalise a saved reply.
        box.value = box.value.trim() ? box.value.trimEnd() + "\n\n" + text : text;
        box.focus();
        box.setSelectionRange(box.value.length, box.value.length);
      });
    });
  })();

  /* --------------------------------------------------------- round setup */

  // The bank is thin in places: most category-and-difficulty pairs hold fewer
  // questions than a round needs. The server renders the counts and disables
  // the impossible chips; this keeps them in step when the category changes,
  // so a player is never invited to pick a combination that cannot be played.
  (function roundSetup() {
    const form = $("[data-setup]");
    if (!form) return;

    let table;
    try {
      table = JSON.parse(form.dataset.availability || "null");
    } catch (e) {
      return;
    }
    if (!table || !Array.isArray(table.entries)) return;

    // (category, difficulty) -> count
    const counts = new Map();
    table.entries.forEach((e) => counts.set(e.category + ":" + e.difficulty, e.count));
    const countFor = (category, difficulty) =>
      counts.get(category + ":" + difficulty) || 0;

    const note = $("[data-setup-note]", form);
    const lengths = $("[data-setup-count]", form);
    const tooFew = tmplText("setup-too-few");
    const capped = tmplText("setup-capped");

    function selectedCategory() {
      const on = $("[data-setup-category]:checked", form);
      return on ? parseInt(on.value, 10) || 0 : 0;
    }

    function sync() {
      const category = selectedCategory();
      let checkedDifficulty = null;

      $$("[data-setup-difficulty]", form).forEach((input) => {
        const difficulty = parseInt(input.value, 10) || 0;
        const available = countFor(category, difficulty);
        const playable = available >= table.min;

        input.disabled = !playable;
        input.closest(".chip-radio").classList.toggle("is-empty", !playable);

        const badge = $('[data-setup-count-for="' + difficulty + '"]', input.closest(".chip-radio"));
        if (badge) badge.textContent = String(available);

        // A disabled chip cannot stay selected, or the form submits a
        // combination the page has just said is unplayable.
        if (!playable && input.checked) input.checked = false;
        if (input.checked) checkedDifficulty = difficulty;
      });

      if (checkedDifficulty === null) {
        const any = $('[data-setup-difficulty][value="0"]', form);
        if (any && !any.disabled) {
          any.checked = true;
          checkedDifficulty = 0;
        }
      }

      const available = countFor(category, checkedDifficulty === null ? 0 : checkedDifficulty);
      syncLengths(available);
      syncNote(available);
    }

    function syncLengths(available) {
      if (!lengths) return;
      let fallback = null;

      Array.from(lengths.options).forEach((option) => {
        const wanted = parseInt(option.value, 10) || 0;
        option.disabled = wanted > available;
        if (!option.disabled) fallback = option;
      });

      if (lengths.selectedOptions[0] && lengths.selectedOptions[0].disabled && fallback) {
        fallback.selected = true;
      }
    }

    function syncNote(available) {
      if (!note) return;
      if (available < table.min) {
        note.textContent = tooFew;
        note.hidden = !tooFew;
        return;
      }
      const largest = Math.max.apply(null, Array.from(lengths ? lengths.options : [])
        .map((o) => parseInt(o.value, 10) || 0));
      if (lengths && available < largest && capped) {
        note.textContent = capped;
        note.hidden = false;
        return;
      }
      note.textContent = "";
      note.hidden = true;
    }

    form.addEventListener("change", (e) => {
      if (e.target.matches("[data-setup-category], [data-setup-difficulty]")) sync();
    });
    sync();
  })();

  /* ------------------------------------------------------- confirm forms */

  (function confirmForms() {
    $$("form[data-confirm]").forEach((form) => {
      form.addEventListener("submit", (e) => {
        if (!window.confirm(form.dataset.confirm)) e.preventDefault();
      });
    });
    // A form with several submit buttons cannot put the question on the form:
    // only one of the buttons is destructive. Those carry it themselves.
    $$("button[data-confirm]").forEach((btn) => {
      btn.addEventListener("click", (e) => {
        if (!window.confirm(btn.dataset.confirm)) e.preventDefault();
      });
    });
  })();

  /* ------------------------------------------------- busy state on buttons */

  // Every button that goes to the server should look like it is working, and
  // refuse a second press while it is. Without this a slow import or a bulk
  // delete looks like a button that did nothing, and the honest reaction to a
  // button that did nothing is to press it again.
  function markBusy(btn) {
    if (!btn || btn.dataset.busy) return;
    btn.dataset.busy = "1";
    btn.classList.add("is-busy");
    btn.setAttribute("aria-busy", "true");
    // Disabling happens on the next tick, never now: a disabled submit button
    // is not successful, so its name and value are left out of the submission
    // — which would quietly turn "delete the selection" into no action at all.
    setTimeout(() => { btn.disabled = true; }, 0);
  }

  function clearBusy(btn) {
    if (!btn) return;
    delete btn.dataset.busy;
    btn.classList.remove("is-busy");
    btn.removeAttribute("aria-busy");
    btn.disabled = false;
  }

  window.sirajBusy = { mark: markBusy, clear: clearBusy };

  (function busyButtons() {
    // One listener on the document rather than per form, so anything rendered
    // later is covered too.
    document.addEventListener("submit", (e) => {
      // A confirm dialog that was answered "no" already stopped this.
      if (e.defaultPrevented) return;
      const form = e.target;
      if (!(form instanceof HTMLFormElement) || form.dataset.noBusy) return;
      markBusy(e.submitter || $("button[type=submit], button:not([type])", form));
    });

    // Coming back with the browser's Back button restores the page from the
    // bfcache exactly as it was left — including a button frozen mid-submit.
    window.addEventListener("pageshow", (e) => {
      if (e.persisted) $$("[data-busy]").forEach(clearBusy);
    });
  })();

  /* --------------------------------------------- bulk selection in tables */

  (function bulkSelect() {
    const form = $("[data-bulk]");
    if (!form) return;

    const bar        = $("[data-bulk-bar]", form);
    const count      = $("[data-bulk-count]", form);
    const scope      = $("[data-bulk-scope]", form);
    const actions    = $("[data-bulk-actions]", form);
    const activate   = $("[data-bulk-activate]", form);
    const deactivate = $("[data-bulk-deactivate]", form);
    const all        = $("[data-bulk-all]");
    // The rows live outside the form and join it with form=, so they are
    // looked up in the document rather than inside it.
    const items = $$("[data-bulk-item]");
    if (!items.length) return;

    function sync() {
      const chosen = items.filter((i) => i.checked);
      const n = chosen.length;
      // Ticking "everything the filter matches" is a selection in its own
      // right — the point of it is that it covers rows this page never
      // showed, so the bar has to stay up even with nothing ticked here.
      const whole = !!(scope && scope.checked);

      // The bar carries the way to select everything the filter matches, so
      // it has to be reachable before anything is selected. It is the actions
      // that wait for a selection, not the bar around them.
      if (bar) bar.hidden = false;
      if (actions) actions.hidden = n === 0 && !whole;
      if (count) count.textContent = String(n);
      if (all) {
        all.checked = n > 0 && n === items.length;
        all.indeterminate = n > 0 && n < items.length;
      }

      // Offer only the toggle that would change something. A selection that
      // is entirely active has nothing to activate, so that button would do
      // nothing at all; showing it anyway leaves the admin to work out which
      // of the pair applies. Both stay up for a mixed selection, and for the
      // whole filter, whose rows this page has not seen.
      const some = (state) => chosen.some((i) => i.dataset.active === state);
      if (activate) activate.hidden = !whole && !some("false");
      if (deactivate) deactivate.hidden = !whole && !some("true");
    }

    if (all) {
      all.addEventListener("change", () => {
        items.forEach((i) => (i.checked = all.checked));
        sync();
      });
    }
    items.forEach((i) => i.addEventListener("change", sync));
    if (scope) scope.addEventListener("change", sync);

    sync();
  })();

  /* Directional glyphs are mirrored in CSS, under [dir="rtl"], the same way
     .backlink__arrow always was — so they point the right way on first paint
     rather than after this script runs. See "directional glyphs" in app.css. */

  /* ========================================================== the round  */

  (function round() {
    const root = $("[data-round]");
    if (!root) return;

    const limitMS   = parseInt(root.dataset.limit, 10) || 25000;
    const position  = parseInt(root.dataset.position, 10) || 0;
    const answersEl = $("[data-answers]", root);
    const verdictEl = $("[data-verdict]", root);
    const noteEl    = $("[data-note]", root);
    const rateEl    = $("[data-rate]", root);
    const confirmBtn= $("[data-confirm-answer]", root);
    const nextBtn   = $("[data-next]", root);
    const nextLabel = $("[data-next-label]", root);
    const scoreEl   = $("[data-score]");
    const streakEl  = $("[data-streak]");
    const streakWrap= $("[data-streak-wrap]");

    const timerEl   = $("[data-timer]");
    const ringEl    = $("[data-timer-ring]");
    const timerText = $("[data-timer-text]");
    const CIRC      = 119.4;

    let selected = -1;      // what the player has picked but not yet committed
    let answered = false;   // set once the answer is actually submitted
    let nextURL  = "/play/round";

    // The server's clock on this question started before this document
    // loaded — on a refresh, a language switch from the round bar, or a round
    // picked back up from the dashboard. Back-date the start by what it says
    // has already gone, instead of handing out a fresh countdown the server
    // will not honour when the answer is scored.
    const spentMS   = parseInt(root.dataset.elapsed, 10) || 0;
    const startedAt = performance.now() - spentMS;

    // ...and tell the server to stop that clock as this page goes away, so the
    // time picks up here rather than having drained while the round sat
    // waiting to be resumed. pagehide covers closing and navigating away;
    // visibilitychange is the one that fires when a phone browser is put in
    // the background and never gets a pagehide at all.
    //
    // Skipped once the answer is in: that navigation is the round advancing,
    // and the next question starts its own clock.
    let banked = false;
    function bankClock() {
      if (banked || answered) return;
      banked = true;
      if (!navigator.sendBeacon) return;
      navigator.sendBeacon("/play/pause", new URLSearchParams({
        csrf_token: csrfToken(),
        position: String(position),
      }));
    }
    window.addEventListener("pagehide", bankClock);
    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "hidden") bankClock();
    });

    /* ---- timer: keeps running while the player deliberates, and only stops
       when they commit an answer or the clock reaches zero. */
    function tick() {
      if (answered) return;
      const elapsed = performance.now() - startedAt;
      const left = Math.max(0, limitMS - elapsed);
      const secs = Math.ceil(left / 1000);

      if (timerText) timerText.textContent = String(secs);
      if (ringEl) ringEl.style.strokeDashoffset = String(CIRC * (1 - left / limitMS));
      if (timerEl) timerEl.classList.toggle("is-low", left <= 6000);

      if (left <= 0) {
        // Out of time: commit whatever is selected, or nothing at all.
        submit(selected);
        return;
      }
      requestAnimationFrame(tick);
    }
    requestAnimationFrame(tick);

    function elapsedMS() {
      return Math.min(limitMS, Math.round(performance.now() - startedAt));
    }

    /* ---- selecting is free and reversible */
    function select(index) {
      if (answered) return;
      selected = index;

      $$(".answer", answersEl).forEach((btn, i) => {
        const on = i === index;
        btn.classList.toggle("answer--selected", on);
        btn.setAttribute("aria-checked", String(on));
      });

      if (confirmBtn) {
        confirmBtn.disabled = false;
        confirmBtn.classList.add("is-ready");
      }
    }

    /* ---- committing stops the clock and reveals the result */
    async function submit(choice) {
      if (answered) return;
      answered = true;

      const buttons = $$(".answer", answersEl);
      buttons.forEach((b) => (b.disabled = true));
      // Spinning rather than vanishing: the verdict arrives over the network,
      // and hiding the button first leaves a blank gap that reads as a dropped
      // press on a slow connection.
      window.sirajBusy.mark(confirmBtn);
      if (timerEl) timerEl.classList.add("is-done");

      let data;
      try {
        data = await postJSON("/play/answer", {
          position: position,
          choice: choice,
          timeMs: elapsedMS(),
        });
      } catch (err) {
        if (err.status === 409) {
          // The server already moved on — follow it rather than arguing.
          window.location.href = "/play/round";
          return;
        }
        toast(tmplText("error"), "error");
        answered = false;
        window.sirajBusy.clear(confirmBtn);
        buttons.forEach((b) => (b.disabled = false));
        return;
      }
      window.sirajBusy.clear(confirmBtn);
      if (confirmBtn) confirmBtn.hidden = true;

      // A hot seat holds the verdict back: the next player is standing there
      // looking at this screen, and the correct answer is exactly what they
      // must not be shown. The choice is marked as locked in and the phone
      // goes to the handover, which is where the reveal happens once
      // everybody here has committed.
      if (data.held) {
        buttons.forEach((btn, i) => {
          btn.classList.remove("answer--selected");
          btn.classList.add(i === choice ? "answer--locked" : "answer--dim");
        });
        if (verdictEl) {
          verdictEl.className = "verdict verdict--held";
          verdictEl.hidden = false;
          verdictEl.textContent = tmplText("locked") || "";
        }
        window.setTimeout(() => {
          window.location.href = data.nextUrl || "/play/round";
        }, 700);
        return;
      }

      reveal(buttons, choice, data);
    }

    function reveal(buttons, choice, data) {
      nextURL = data.nextUrl || "/play/round";

      // The note box only exists once the answer is revealed — writing one
      // beforehand would be a place to stash the answer.
      if (noteEl) noteEl.hidden = false;
      if (rateEl) rateEl.hidden = false;

      buttons.forEach((btn, i) => {
        btn.classList.remove("answer--selected");
        if (i === data.correctIndex) {
          btn.classList.add("answer--correct");
        } else if (i === choice) {
          btn.classList.add("answer--wrong");
        } else {
          btn.classList.add("answer--dim");
        }
      });

      if (scoreEl) scoreEl.textContent = String(data.score);
      if (streakEl) streakEl.textContent = String(data.streak);
      if (streakWrap) streakWrap.classList.toggle("sr-only", data.streak < 2);

      if (verdictEl) {
        const timedOut = choice < 0;
        const headline = data.correct
          ? tmplText("correct")
          : timedOut ? tmplText("timeup") : tmplText("wrong");

        verdictEl.className = "verdict " + (data.correct ? "verdict--correct" : "verdict--wrong");
        verdictEl.hidden = false;
        verdictEl.innerHTML = "";

        const head = document.createElement("div");
        head.className = "verdict__head";
        head.append(iconSpan(data.correct ? "\u2713" : "\u2715"), textSpan(headline));
        if (data.points > 0) {
          head.append(pointsSpan("+" + data.points));
        }
        verdictEl.appendChild(head);

        // Name the right answer when they did not pick it. Reading it off the
        // highlighted button works for a sighted user; this is what makes the
        // reveal say it, which is also what the live region announces.
        if (!data.correct) {
          const answer = buttons[data.correctIndex];
          const label = tmplText("answer-was");
          if (answer && label) {
            const line = document.createElement("p");
            line.className = "verdict__answer small";
            line.textContent = label + " " + answer.textContent.trim();
            verdictEl.appendChild(line);
          }
        }

        if (data.explanation) {
          const body = document.createElement("p");
          body.className = "verdict__body";
          body.textContent = data.explanation;
          verdictEl.appendChild(body);
        }
      }

      if (nextBtn) {
        if (nextLabel) {
          nextLabel.textContent = data.finished ? tmplText("finish") : tmplText("next");
        }
        nextBtn.hidden = false;
        nextBtn.focus();
      }
    }

    function iconSpan(text) {
      const s = document.createElement("span");
      s.setAttribute("aria-hidden", "true");
      s.textContent = text;
      return s;
    }
    function textSpan(text) {
      const s = document.createElement("span");
      s.textContent = text;
      return s;
    }
    function pointsSpan(text) {
      const s = document.createElement("span");
      s.className = "verdict__points bold";
      s.textContent = text;
      return s;
    }

    answersEl.addEventListener("click", (e) => {
      const btn = e.target.closest(".answer");
      if (!btn || btn.disabled) return;
      select(parseInt(btn.dataset.choice, 10));
    });

    if (confirmBtn) {
      confirmBtn.addEventListener("click", () => {
        if (selected < 0) {
          toast(tmplText("pick"), "error");
          return;
        }
        submit(selected);
      });
    }

    if (nextBtn) {
      nextBtn.addEventListener("click", () => {
        if (nextURL === "/play/finish") {
          postAndGo("/play/finish");
        } else {
          window.location.href = nextURL;
        }
      });
    }

    // Finishing is a POST so it is not replayed by a refresh or a prefetch.
    function postAndGo(url) {
      const form = document.createElement("form");
      form.method = "post";
      form.action = url;
      const input = document.createElement("input");
      input.type = "hidden";
      input.name = "csrf_token";
      input.value = csrfToken();
      form.appendChild(input);
      document.body.appendChild(form);
      form.submit();
    }

    // Keyboard: 1–4 select, Enter confirms, Enter again advances.
    document.addEventListener("keydown", (e) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;

      if (!answered && e.key >= "1" && e.key <= "4") {
        const idx = parseInt(e.key, 10) - 1;
        if ($$(".answer", answersEl)[idx]) { e.preventDefault(); select(idx); }
        return;
      }
      if (e.key !== "Enter" && e.key !== " ") return;

      if (!answered) {
        if (selected >= 0) { e.preventDefault(); submit(selected); }
      } else if (nextBtn && !nextBtn.hidden) {
        e.preventDefault();
        nextBtn.click();
      }
    });
  })();

  /* ============================================================== chat  */

  (function chat() {
    const thread = $("[data-thread]");
    if (!thread) return;

    const convID  = thread.dataset.conversation;
    const log     = $("[data-log]", thread);
    const form    = $("[data-compose]", thread);
    const input   = $("[data-input]", thread);
    let lastID    = parseInt(thread.dataset.last, 10) || 0;
    let firstID   = parseInt(thread.dataset.first, 10) || 0;
    let fetching  = false;
    let tempSeq   = 0;

    function scrollToEnd(smooth) {
      if (!log) return;
      log.scrollTo({ top: log.scrollHeight, behavior: smooth ? "smooth" : "auto" });
    }
    scrollToEnd(false);

    /* ---- reading -------------------------------------------------------
       Reading is an act, not a state of the DOM. A thread open in a window
       nobody is looking at — a second browser, a tab restored at boot, a
       phone in a pocket — used to report every arriving message as read,
       because marking was a side effect of the page existing and of every
       poll. Now the client says so, and only while somebody is here. */
    function looking() {
      return !document.hidden && document.hasFocus();
    }

    let marking = false;
    async function markRead() {
      if (marking || !looking()) return;
      if (!$(".chat__item.is-unread") && !$(".chat__since")) {
        // Nothing unread that this page knows about. The server is the
        // authority, but there is no reason to ask it on every focus.
        if (!thread.dataset.unreadHint) return;
      }
      marking = true;
      try {
        const res = await postForm("/messages/" + convID + "/read", new FormData());
        thread.dataset.unreadHint = "";
        // The thread was read, so nothing about it is still news. The row in
        // the panel and every badge in the chrome used to say otherwise until
        // the next full page load — seven unread, on a conversation open on
        // the screen in front of you.
        const row = $('[data-conv="' + convID + '"]');
        if (row) {
          row.classList.remove("is-unread");
          const dot = $(".count-dot", row);
          if (dot) dot.remove();
        }
        if (res) {
          paintBadge("/messages", res.unread);
          paintBadge("/notifications", res.notifications);
          paintAvatarDot(res.notifications);
        }
      } catch (err) {
        /* the next focus retries */
      } finally {
        marking = false;
      }
    }
    thread.dataset.unreadHint = "1";
    markRead();
    window.addEventListener("focus", markRead);
    document.addEventListener("visibilitychange", () => {
      if (looking()) { poll(); markRead(); }
    });

    /* ---- drawing -------------------------------------------------------- */

    function dayLabel(key) {
      const today = new Date();
      const pad = (n) => String(n).padStart(2, "0");
      const stamp = (d) => d.getFullYear() + "-" + pad(d.getMonth() + 1) + "-" + pad(d.getDate());
      if (key === stamp(today)) return tmplText("today");
      const yesterday = new Date(today.getTime() - 86400000);
      if (key === stamp(yesterday)) return tmplText("yesterday");
      return key;
    }

    // The separator above a message, when it is the first of its day. The
    // server draws these for the page it renders; this is the same rule for
    // the ones that arrive afterwards.
    function ensureDay(key, before) {
      if (!key || $('[data-day="' + key + '"]', log)) return;
      const div = document.createElement("div");
      div.className = "chat__date";
      div.dataset.day = key;
      div.textContent = dayLabel(key);
      if (before) log.insertBefore(div, before);
      else log.appendChild(div);
    }

    function buildRow(m) {
      const row = document.createElement("div");
      row.className = "bubble-row " + (m.mine ? "bubble-row--out" : "bubble-row--in");
      row.dataset.message = String(m.id);

      const bubble = document.createElement("div");
      bubble.className = "bubble " + (m.mine ? "bubble--out" : "bubble--in");

      if (m.reply) {
        const quote = document.createElement("a");
        quote.className = "bubble__quote";
        quote.href = "#m" + m.reply.id;
        const who = document.createElement("span");
        who.className = "bubble__quote-who";
        who.textContent = m.reply.mine ? tmplText("you") : tmplText("them");
        const line = document.createElement("span");
        line.className = "bubble__quote-body truncate";
        line.textContent = m.reply.withdrawn ? tmplText("withdrawn") : m.reply.body;
        quote.append(who, line);
        bubble.appendChild(quote);
      }

      if (m.file) bubble.appendChild(buildFile(m.file));

      const body = document.createElement("span");
      body.textContent = m.body || "";
      if (!m.body) body.hidden = true;

      const meta = document.createElement("span");
      meta.className = "bubble__meta";

      const time = document.createElement("span");
      time.className = "bubble__time num";
      time.textContent = m.time;
      meta.appendChild(time);

      // A receipt on your own messages only: whether they have read it is news
      // to you, not to them.
      if (m.mine) {
        const seen = document.createElement("span");
        seen.className = "bubble__seen";
        seen.setAttribute("aria-hidden", "true");
        seen.textContent = m.read ? "✓✓" : "✓";
        meta.appendChild(seen);
      }

      bubble.appendChild(body);
      if (m.mine && m.read) meta.classList.add("is-read");

      // The bubble and what is said about it, stacked: the time sits under the
      // message rather than floated into its last line.
      const col = document.createElement("div");
      col.className = "bubble__col";

      // In a group or a room the message has to say who wrote it. In a pair
      // the side of the screen already has, so the server sends no author and
      // nothing is drawn.
      if (m.author) {
        const face = document.createElement("span");
        face.className = "bubble__who";
        face.title = m.author.name;
        const av = document.createElement("span");
        av.className = "avatar avatar--xs" + (m.author.photo ? " avatar--photo" : "");
        av.setAttribute("style", m.author.style);
        if (!m.author.photo) av.textContent = m.author.initial;
        face.appendChild(av);
        row.appendChild(face);

        const name = document.createElement("span");
        name.className = "bubble__name";
        name.setAttribute("style", m.author.style);
        name.textContent = m.author.name;
        col.appendChild(name);
      }

      col.append(bubble, meta);
      row.id = "m" + m.id;
      row.appendChild(col);

      row.dataset.mine = m.mine ? "true" : "false";
      row.dataset.withdrawn = m.withdrawn ? "true" : "false";
      row.dataset.sent = m.sentAt || "";
      row.dataset.read = m.readAt || "";

      if (m.id > 0) {
        const more = document.createElement("button");
        more.className = "bubble__menu";
        more.type = "button";
        more.dataset.messageMenu = String(m.id);
        more.setAttribute("aria-haspopup", "true");
        more.setAttribute("aria-expanded", "false");
        more.textContent = "⋮";
        row.appendChild(more);
      }
      return row;
    }

    // The same player the server draws, for a recording that arrives while the
    // thread is open. The markup has to match: one module plays both.
    function buildVoice(f) {
      const holder = document.createElement("span");
      holder.className = "voice";
      holder.dataset.voice = "";

      const play = document.createElement("button");
      play.className = "voice__play";
      play.type = "button";
      play.dataset.voicePlay = "";
      play.setAttribute("aria-label", tmplText("play") || "Play");
      play.innerHTML = PLAY_ICON;

      const wave = document.createElement("span");
      wave.className = "voice__wave";
      wave.dataset.voiceWave = "";
      wave.setAttribute("aria-hidden", "true");
      waveform(f.id).forEach((h) => {
        const bar = document.createElement("i");
        bar.style.setProperty("--h", h + "%");
        wave.appendChild(bar);
      });

      const time = document.createElement("span");
      time.className = "voice__time num";
      time.dataset.voiceTime = "";
      time.textContent = f.length || "–:––";

      const audio = document.createElement("audio");
      audio.dataset.voiceAudio = "";
      audio.preload = "none";
      audio.src = "/files/" + f.id;

      holder.append(play, wave, time, audio);
      return holder;
    }

    // The same shape the server draws for the same id: FNV-1a over the whole
    // id, then one step per bar.
    function waveform(id) {
      const out = [];
      let h = 2166136261;
      for (let i = 0; i < id.length; i++) {
        h = (h ^ id.charCodeAt(i)) >>> 0;
        h = Math.imul(h, 16777619) >>> 0;
      }
      for (let i = 0; i < 26; i++) {
        h = (h ^ i) >>> 0;
        h = Math.imul(h, 16777619) >>> 0;
        out.push(30 + ((h >>> 17) % 70));
      }
      return out;
    }

    // What a message carries besides words: shown if it can be shown, played
    // if it can be played, offered as a file otherwise.
    function buildFile(f) {
      const href = "/files/" + f.id;
      if (f.kind === "image") {
        const link = document.createElement("a");
        link.className = "bubble__image";
        link.href = href;
        link.target = "_blank";
        link.rel = "noopener";
        const img = document.createElement("img");
        img.src = href;
        img.alt = f.name;
        img.loading = "lazy";
        link.appendChild(img);
        return link;
      }
      if (f.kind === "audio") return buildVoice(f);
      const link = document.createElement("a");
      link.className = "bubble__file";
      link.href = href;
      const icon = document.createElement("span");
      icon.className = "bubble__file-icon";
      icon.setAttribute("aria-hidden", "true");
      icon.textContent = "📎";
      const holder = document.createElement("span");
      holder.className = "bubble__file-body";
      const name = document.createElement("span");
      name.className = "bubble__file-name truncate";
      name.textContent = f.name;
      const size = document.createElement("span");
      size.className = "bubble__file-size tiny";
      size.textContent = f.size;
      holder.append(name, size);
      link.append(icon, holder);
      return link;
    }

    function renderMessage(m) {
      // Already on screen: update what can change — whether it has been read,
      // and whether its sender took it back.
      const existing = $('[data-message="' + m.id + '"]', log);
      if (existing) {
        updateMessage(existing, m);
        return;
      }

      // Drop the "no messages yet" placeholder on first arrival.
      const placeholder = $(".chat__empty", log);
      if (placeholder) placeholder.remove();

      ensureDay(m.day, null);
      log.appendChild(buildRow(m));
      if (m.withdrawn) updateMessage($('[data-message="' + m.id + '"]', log), m);
    }

    // What changes about a message after it is sent: it gets read, or it gets
    // taken back. Both arrive on the stream and both are applied in place, so
    // the thread never has to be rebuilt.
    function updateMessage(row, m) {
      if (!row) return;
      const bubble = $(".bubble", row);
      if (!bubble) return;

      if (m.withdrawn) {
        bubble.classList.add("bubble--gone");
        const body = bubble.firstElementChild;
        if (body) {
          body.className = "bubble__gone";
          body.textContent = tmplText("withdrawn") || "…";
        }
        row.dataset.withdrawn = "true";
        // The receipt lives beside the bubble now, not inside it.
        const gone = $(".bubble__seen", row);
        if (gone) gone.remove();
        const meta = $(".bubble__meta", row);
        if (meta) meta.classList.remove("is-read");
        return;
      }
      const seen = $(".bubble__seen", row);
      if (seen && typeof m.read === "boolean") {
        seen.textContent = m.read ? "✓✓" : "✓";
        const meta = $(".bubble__meta", row);
        if (meta) meta.classList.toggle("is-read", m.read);
      }
    }

    /* ---- what you can do with one message -------------------------------
       One menu for the whole thread, moved to whichever bubble asked for it.
       Eighty copies of the same twelve elements is the alternative, and the
       live renderer would have to build them all again by hand. */
    const menu = $("[data-message-menu-panel]");
    let menuFor = null;

    function closeMenu() {
      if (!menu) return;
      menu.hidden = true;
      if (menuFor) {
        const button = $('[data-message-menu="' + menuFor + '"]', log);
        if (button) button.setAttribute("aria-expanded", "false");
      }
      menuFor = null;
    }

    // A timestamp the browser formats in its own locale, which is the one
    // place a time is already known to be right.
    function stamp(iso) {
      if (!iso) return "";
      const at = new Date(iso);
      if (isNaN(at)) return "";
      try {
        return at.toLocaleString(document.documentElement.lang || undefined, {
          dateStyle: "medium", timeStyle: "short",
        });
      } catch (err) {
        return at.toLocaleString();
      }
    }

    function openMenu(button, row) {
      if (!menu) return;
      menuFor = row.dataset.message;
      const mine = row.dataset.mine === "true";
      const gone = row.dataset.withdrawn === "true";

      $("[data-menu-sent]", menu).textContent = stamp(row.dataset.sent);
      const readRow = $("[data-menu-read-row]", menu);
      // Whether they read it is news for the sender and nobody else.
      readRow.hidden = !mine || gone;
      $("[data-menu-read]", menu).textContent =
        stamp(row.dataset.read) || tmplText("notread");

      $("[data-menu-copy]", menu).hidden = gone;
      $("[data-menu-reply]", menu).hidden = gone;
      // Taking it back is the sender's alone, and only while it is still there.
      $("[data-menu-withdraw]", menu).hidden = !mine || gone;

      menu.hidden = false;
      button.setAttribute("aria-expanded", "true");

      const box = button.getBoundingClientRect();
      const width = menu.offsetWidth;
      const height = menu.offsetHeight;
      // Anchored to the handle, opening away from the bubble: your own
      // messages carry their handle on the inner side, so the panel unfolds
      // outward into the empty column beside them rather than over what was
      // said.
      const left = mine ? box.left - width + box.width : box.left;
      menu.style.left = Math.max(8, Math.min(left, window.innerWidth - width - 8)) + "px";
      menu.style.top = (box.bottom + height > window.innerHeight - 8
        ? Math.max(8, box.top - height - 4)
        : box.bottom + 4) + "px";
    }

    log.addEventListener("click", (e) => {
      const button = e.target.closest("[data-message-menu]");
      if (!button) return;
      e.stopPropagation();
      const row = button.closest("[data-message]");
      if (!row) return;
      if (menuFor === row.dataset.message && menu && !menu.hidden) {
        closeMenu();
        return;
      }
      closeMenu();
      openMenu(button, row);
    });

    document.addEventListener("click", closeMenu);
    window.addEventListener("scroll", closeMenu, true);
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape") closeMenu();
    });

    if (menu) {
      menu.addEventListener("click", (e) => e.stopPropagation());

      $("[data-menu-reply]", menu).addEventListener("click", () => {
        const row = $('[data-message="' + menuFor + '"]', log);
        closeMenu();
        startReply(row);
      });

      $("[data-menu-copy]", menu).addEventListener("click", async () => {
        const row = $('[data-message="' + menuFor + '"]', log);
        const body = row && $(".bubble > span", row);
        if (!body) return;
        try {
          await navigator.clipboard.writeText(body.textContent);
          toast(tmplText("copied") || "…", "success");
        } catch (err) {
          toast(tmplText("error") || "…", "error");
        }
        closeMenu();
      });

      // Yours to drop from your own copy, whoever wrote it. The other side
      // never sees that it happened.
      $("[data-menu-hide]", menu).addEventListener("click", async () => {
        const id = menuFor;
        const row = $('[data-message="' + id + '"]', log);
        closeMenu();
        try {
          await postForm("/messages/" + convID + "/m/" + id + "/hide", new FormData());
          if (row) row.remove();
        } catch (err) {
          toast(tmplText("error") || "…", "error");
        }
      });

      // Taking it out of the conversation for both of you.
      $("[data-menu-withdraw]", menu).addEventListener("click", async (e) => {
        const id = menuFor;
        const row = $('[data-message="' + id + '"]', log);
        const ask = e.currentTarget.dataset.confirm;
        closeMenu();
        if (ask && !window.confirm(ask)) return;
        try {
          await postForm("/messages/" + convID + "/m/" + id + "/withdraw", new FormData());
          if (row) updateMessage(row, { withdrawn: true });
        } catch (err) {
          toast(tmplText("error") || "…", "error");
        }
      });
    }

    /* ---- the tail ------------------------------------------------------
       A read, and nothing more. It used to mark messages read and announce
       the read as a side effect of being asked for them, which is how two
       open threads ended up answering each other forever. */
    async function poll() {
      if (fetching || document.hidden) return;
      fetching = true;
      try {
        const res = await fetch("/messages/" + convID + "/poll?after=" + lastID, {
          headers: { "Accept": "application/json", "X-Requested-With": "fetch" },
        });
        if (!res.ok) return;
        const data = await res.json();
        if (data.messages && data.messages.length) {
          const atBottom = log.scrollHeight - log.scrollTop - log.clientHeight < 120;
          data.messages.forEach(renderMessage);
          lastID = data.lastId || lastID;
          if (data.messages.some((m) => !m.mine)) {
            thread.dataset.unreadHint = "1";
            markRead();
          }
          if (atBottom) scrollToEnd(true);
        }
      } catch (e) {
        /* transient network failure — the next tick retries */
      } finally {
        fetching = false;
      }
    }

    /* ---- the head ------------------------------------------------------
       Everything older than the page the server rendered. Without this a
       thread was only ever its newest eighty messages. */
    const olderButton = $("[data-older]", log);
    let loadingOlder = false;

    async function loadOlder() {
      if (loadingOlder || !firstID) return;
      loadingOlder = true;
      if (olderButton) olderButton.disabled = true;
      const anchorHeight = log.scrollHeight;
      try {
        const res = await fetch("/messages/" + convID + "/poll?before=" + firstID, {
          headers: { "Accept": "application/json", "X-Requested-With": "fetch" },
        });
        if (!res.ok) return;
        const data = await res.json();
        const older = data.messages || [];
        if (!older.length) {
          if (olderButton) olderButton.remove();
          return;
        }
        // Prepended in reverse so each lands above the last, and the first
        // message already on screen keeps its day separator honest.
        const top = $(".bubble-row", log);
        let anchor = top;
        for (let i = older.length - 1; i >= 0; i--) {
          const m = older[i];
          if ($('[data-message="' + m.id + '"]', log)) continue;
          const row = buildRow(m);
          log.insertBefore(row, anchor);
          anchor = row;
        }
        let previous = "";
        older.forEach((m) => {
          if (m.day !== previous) {
            previous = m.day;
            const row = $('[data-message="' + m.id + '"]', log);
            ensureDay(m.day, row);
          }
        });
        firstID = data.firstId || firstID;
        // Keep the reader where they were rather than at the new top.
        log.scrollTop += log.scrollHeight - anchorHeight;
        if (!data.hasOlder && olderButton) olderButton.remove();
      } catch (err) {
        toast(tmplText("error") || "…", "error");
      } finally {
        loadingOlder = false;
        if (olderButton) olderButton.disabled = false;
      }
    }

    if (olderButton) olderButton.addEventListener("click", loadOlder);
    log.addEventListener("scroll", () => {
      if (log.scrollTop < 40 && olderButton && !loadingOlder) loadOlder();
    });

    /* ---- sending -------------------------------------------------------- */

    /* ---- replying --------------------------------------------------------
       The quote is not decoration: a reply arriving an hour after the thing it
       answers answers nothing you can still see. */
    const replying = $("[data-replying]", thread);
    const replyID = $("[data-reply-id]", thread);

    function startReply(row) {
      if (!replying || !replyID || !row) return;
      const body = $(".bubble > span:not(.bubble__quote):not(.bubble__meta)", row);
      replyID.value = row.dataset.message;
      $("[data-replying-who]", replying).textContent =
        row.dataset.mine === "true" ? tmplText("you") : tmplText("them");
      $("[data-replying-text]", replying).textContent = body ? body.textContent : "";
      replying.hidden = false;
      if (input) input.focus();
    }

    function cancelReply() {
      if (!replying || !replyID) return;
      replying.hidden = true;
      replyID.value = "";
    }

    if (replying) {
      $("[data-replying-cancel]", replying).addEventListener("click", cancelReply);
    }

    // Clicking a quote goes back to what it quotes, and says which one.
    log.addEventListener("click", (e) => {
      const quote = e.target.closest(".bubble__quote");
      if (!quote) return;
      e.preventDefault();
      const target = document.getElementById(quote.getAttribute("href").slice(1));
      if (!target) return;
      target.scrollIntoView({ behavior: "smooth", block: "center" });
      target.classList.add("is-found");
      setTimeout(() => target.classList.remove("is-found"), 1400);
    });

    if (form && input) {
      // Enter sends, Shift+Enter inserts a newline.
      input.addEventListener("keydown", (e) => {
        if (e.key === "Enter" && !e.shiftKey) {
          e.preventDefault();
          form.requestSubmit();
        }
      });

      // Grow the composer with its content, up to the CSS max-height.
      input.addEventListener("input", () => {
        input.style.height = "auto";
        input.style.height = Math.min(input.scrollHeight, 140) + "px";
        syncSend();
      });

      form.addEventListener("submit", async (e) => {
        e.preventDefault();
        const body = input.value.trim();
        // A photograph with nothing said about it is still a message.
        if (!body && !picked.length) return;
        const sending = picked.slice();
        const sendingLengths = lengths.slice();

        const data = new FormData();
        data.set("csrf_token", csrfToken());
        data.set("body", body);
        // Each file with its length beside it, in the same order, so a voice
        // note sent alongside two photographs is still matched with its own.
        sending.forEach((file, i) => {
          data.append("file", file, file.name);
          data.append("duration_ms", String(sendingLengths[i] || 0));
        });
        if (replyID && replyID.value) data.set("reply_to", replyID.value);
        cancelReply();

        input.value = "";
        input.style.height = "auto";
        clearAttached();
        syncSend();

        // On screen before the server has it. Waiting for the round trip
        // meant a slow connection looked like a composer that had eaten the
        // message.
        const now = new Date();
        const pad = (n) => String(n).padStart(2, "0");
        const placeholder = $(".chat__empty", log);
        if (placeholder) placeholder.remove();
        ensureDay(now.getFullYear() + "-" + pad(now.getMonth() + 1) + "-" + pad(now.getDate()), null);

        // One bubble per file, the way they will arrive. Four photographs
        // showing as one bubble that then becomes four is the composer
        // telling the sender something that is not true for as long as the
        // round trip takes.
        const waiting = (sending.length ? sending : [null]).map((file, i) => {
          const tempID = "pending-" + (++tempSeq);
          const row = buildRow({
            id: tempID, body: i === 0 ? body : "", mine: true, read: false,
            time: pad(now.getHours()) + ":" + pad(now.getMinutes()),
            file: file ? {
              id: "", kind: kindOf(file) === "🖼️" ? "image" : "file",
              name: file.name, size: readableSize(file.size),
            } : null,
          });
          row.dataset.message = tempID;
          $(".bubble", row).classList.add("bubble--pending");
          log.appendChild(row);
          return row;
        });
        scrollToEnd(true);

        markBusy(sendBtn);
        try {
          const res = await postForm("/messages/" + convID, data);
          waiting.forEach((row) => row.remove());
          (res.messages || []).forEach(renderMessage);
          lastID = res.lastId || lastID;
          if (!firstID) firstID = lastID;
          scrollToEnd(true);
          // The panel says what was last said in each thread, and this was it.
          window.dispatchEvent(new CustomEvent("siraj:panel"));
        } catch (err) {
          waiting.forEach((row) => {
            const bubble = $(".bubble", row);
            if (bubble) {
              bubble.classList.remove("bubble--pending");
              bubble.classList.add("bubble--failed");
            }
          });
          input.value = body;
          // What was not sent is still waiting to be.
          picked = sending;
          lengths = sendingLengths;
          drawShelf();
          toast((err && err.said && err.message) || tmplText("error") || "…", "error");
        } finally {
          clearBusy(sendBtn);
          syncSend();
        }
      });
    }

    /* ---- is there anything to send ---------------------------------------
       A photograph, a recording or a document is a message. The box used to
       be `required`, so the browser refused the submit before the script got
       to say so and a voice note could not be sent without a sentence typed
       beside it. The rule lives here now, where it can see the file too. */
    const sendBtn = $("[data-send]", thread);

    function hasPayload() {
      return Boolean(input && (input.value || "").trim()) || picked.length > 0;
    }

    // Disabled from here rather than from the template: without the script
    // the form still posts, and a button the server drew as disabled would be
    // a composer nobody could ever send from.
    function syncSend() {
      if (sendBtn) sendBtn.disabled = !hasPayload();
    }

    /* ---- what else can be sent ------------------------------------------
       A file rides with the message rather than being uploaded ahead of it,
       so a send that fails leaves nothing orphaned on disk. */
    const fileInput   = $("[data-file]", thread);
    const fileOpen    = $("[data-file-open]", thread);
    const shelf       = $("[data-shelf]", thread);
    const chipTmpl    = $("[data-chip]", thread);
    const recordBtn   = $("[data-record]", thread);
    const recordClock = $("[data-record-clock]", thread);

    // What is waiting to be sent. The file input cannot be added to — picking
    // again replaces everything it holds — so the list is kept here and the
    // input is only ever read from.
    let picked = [];
    // A length in milliseconds per file, by the same index. Only a recording
    // made here has one; nothing measures the length of a file off the disk.
    let lengths = [];

    // Bytes as a person reads them. The server says the same thing about a
    // message already sent; this is the same sentence about one not sent yet.
    function readableSize(bytes) {
      if (!bytes && bytes !== 0) return "";
      const units = ["B", "KB", "MB"];
      let n = bytes;
      let unit = 0;
      while (n >= 1024 && unit < units.length - 1) {
        n /= 1024;
        unit++;
      }
      return (unit === 0 ? n : n.toFixed(1)) + " " + units[unit];
    }

    // A recording's own length, as a clock reads it.
    function readableLength(ms) {
      const total = Math.round((ms || 0) / 1000);
      return Math.floor(total / 60) + ":" + String(total % 60).padStart(2, "0");
    }

    function kindOf(file) {
      if (/^audio\//.test(file.type || "")) return "🎙️";
      if (/^image\//.test(file.type || "")) return "🖼️";
      return "📎";
    }

    // The shelf is drawn from the list, never added to piecemeal: one place
    // decides what is waiting to be sent, so removing the second of four
    // cannot leave the chips and the files disagreeing about which is which.
    function drawShelf() {
      if (!shelf || !chipTmpl) return;
      shelf.textContent = "";
      picked.forEach((file, i) => {
        const chip = chipTmpl.content.firstElementChild.cloneNode(true);
        $("[data-attached-kind]", chip).textContent = kindOf(file);
        $("[data-attached-name]", chip).textContent = file.name;
        // A recording is its length; a file is its size. Saying "84.2 KB"
        // about a voice note answers a question nobody asked.
        $("[data-attached-size]", chip).textContent =
          lengths[i] > 0 ? readableLength(lengths[i]) : readableSize(file.size);
        $("[data-attached-clear]", chip).addEventListener("click", () => {
          picked.splice(i, 1);
          lengths.splice(i, 1);
          drawShelf();
        });
        shelf.appendChild(chip);
      });
      shelf.hidden = picked.length === 0;
      syncSend();
    }

    // How many the server will take. Told here rather than discovered by
    // being refused, and it is the server's own number.
    const fileLimit = parseInt(thread.dataset.fileLimit, 10) || 1;

    function addFiles(files, ms) {
      const room = fileLimit - picked.length;
      if (room <= 0) {
        toast(tmplText("toomany") || "…", "error");
        return;
      }
      const taking = Array.prototype.slice.call(files, 0, room);
      if (taking.length < files.length) toast(tmplText("toomany") || "…", "error");
      taking.forEach((file) => {
        picked.push(file);
        lengths.push(ms || 0);
      });
      drawShelf();
    }

    function clearAttached() {
      picked = [];
      lengths = [];
      if (fileInput) fileInput.value = "";
      drawShelf();
    }

    if (fileOpen && fileInput) {
      fileOpen.addEventListener("click", () => fileInput.click());
      fileInput.addEventListener("change", () => {
        addFiles(fileInput.files || [], 0);
        // Read out of the input straight away: keeping them there would send
        // the same photograph twice the next time something is picked.
        fileInput.value = "";
      });
    }

    /* ---- voice notes -----------------------------------------------------
       The recording lands in the same file input as a chosen file, so there
       is one path from here to the server rather than two. */
    if (recordBtn && fileInput) {
      let recorder = null;
      let chunks = [];
      let startedAt = 0;
      let ticking = null;

      // How long it has been listening, on the button. Recording into a
      // pulsing icon with no clock is recording blind — you find out how long
      // it ran once it is already a message.
      function tick() {
        if (!recordClock) return;
        recordClock.textContent = readableLength(Date.now() - startedAt);
      }

      function stopTicking() {
        if (ticking) clearInterval(ticking);
        ticking = null;
        if (recordClock) {
          recordClock.hidden = true;
          recordClock.textContent = "";
        }
      }

      recordBtn.addEventListener("click", async () => {
        if (recorder && recorder.state === "recording") {
          recorder.stop();
          return;
        }
        if (!navigator.mediaDevices || !window.MediaRecorder) {
          toast(tmplText("nomic") || "…", "error");
          return;
        }
        let stream;
        try {
          stream = await navigator.mediaDevices.getUserMedia({ audio: true });
        } catch (err) {
          toast(tmplText("nomic") || "…", "error");
          return;
        }
        chunks = [];
        recorder = new MediaRecorder(stream);
        recorder.addEventListener("dataavailable", (e) => {
          if (e.data && e.data.size) chunks.push(e.data);
        });
        recorder.addEventListener("stop", () => {
          const ran = Date.now() - startedAt;
          stream.getTracks().forEach((t) => t.stop());
          recordBtn.classList.remove("is-recording");
          recordBtn.title = recordBtn.dataset.idleTitle || recordBtn.title;
          stopTicking();
          if (!chunks.length) return;
          const blob = new Blob(chunks, { type: recorder.mimeType || "audio/webm" });
          const name = (tmplText("voice") || "voice") + ".webm";
          // The length measured here is the only one there will ever be: the
          // WebM this produces carries no duration of its own.
          addFiles([new File([blob], name, { type: "audio/webm" })], ran);
        });
        recordBtn.dataset.idleTitle = recordBtn.title;
        recordBtn.title = tmplText("recording") || recordBtn.title;
        recordBtn.classList.add("is-recording");
        startedAt = Date.now();
        if (recordClock) {
          recordClock.hidden = false;
          tick();
        }
        ticking = setInterval(tick, 250);
        recorder.start();
      });
    }

    /* ---- emoji -----------------------------------------------------------
       The catalogue is fetched the first time the picker opens. Three hundred
       and sixty characters is a real weight to put on every page in the app
       for the sake of one, and nothing here comes from anywhere but /static —
       the whole point of the CSP is that there is no third party to trust. */
    const emojiOpen   = $("[data-emoji-open]", thread);
    const emojiPanel  = $("[data-emoji-panel]", thread);

    if (emojiOpen && emojiPanel && input) {
      const tabs   = $("[data-emoji-tabs]", emojiPanel);
      const grid   = $("[data-emoji-grid]", emojiPanel);
      const search = $("[data-emoji-search]", emojiPanel);
      const empty  = $("[data-emoji-empty]", emojiPanel);
      let catalogue = null;
      let group = 0;

      function paint(items) {
        grid.textContent = "";
        items.forEach(([char]) => {
          const button = document.createElement("button");
          button.type = "button";
          button.className = "emoji__one";
          button.textContent = char;
          button.tabIndex = -1;
          grid.appendChild(button);
        });
        empty.hidden = items.length > 0;
      }

      function show() {
        if (!catalogue) return;
        const needle = search.value.trim().toLowerCase();
        if (!needle) {
          paint(catalogue[group].items);
          return;
        }
        // Across every group: when you are searching you are not in one.
        const hits = [];
        catalogue.forEach((g) => g.items.forEach((item) => {
          if (item[0] === needle || item[1].includes(needle)) hits.push(item);
        }));
        paint(hits);
      }

      async function load() {
        if (catalogue) return;
        const res = await fetch("/static/emoji.json", { headers: { "Accept": "application/json" } });
        catalogue = await res.json();
        catalogue.forEach((g, i) => {
          const tab = document.createElement("button");
          tab.type = "button";
          tab.className = "emoji__tab" + (i === 0 ? " is-active" : "");
          tab.textContent = g.tab;
          tab.setAttribute("role", "tab");
          tab.addEventListener("click", () => {
            group = i;
            search.value = "";
            $$(".emoji__tab", tabs).forEach((t, j) => t.classList.toggle("is-active", i === j));
            show();
          });
          tabs.appendChild(tab);
        });
      }

      // Into the box at the cursor, not at the end: half a sentence in and a
      // smile belongs where you were, not after the full stop.
      function insert(char) {
        const at = input.selectionStart === null ? input.value.length : input.selectionStart;
        const to = input.selectionEnd === null ? at : input.selectionEnd;
        input.value = input.value.slice(0, at) + char + input.value.slice(to);
        const after = at + char.length;
        input.setSelectionRange(after, after);
        input.focus();
        input.dispatchEvent(new Event("input"));
      }

      function closeEmoji() {
        emojiPanel.hidden = true;
        emojiOpen.setAttribute("aria-expanded", "false");
      }

      emojiOpen.addEventListener("click", async (e) => {
        e.stopPropagation();
        if (!emojiPanel.hidden) { closeEmoji(); return; }
        try {
          await load();
        } catch (err) {
          toast(tmplText("error") || "…", "error");
          return;
        }
        show();
        emojiPanel.hidden = false;
        emojiOpen.setAttribute("aria-expanded", "true");
        const box = emojiOpen.getBoundingClientRect();
        emojiPanel.style.left = Math.max(8, Math.min(box.left,
          window.innerWidth - emojiPanel.offsetWidth - 8)) + "px";
        emojiPanel.style.top = Math.max(8, box.top - emojiPanel.offsetHeight - 8) + "px";
      });

      grid.addEventListener("click", (e) => {
        const one = e.target.closest(".emoji__one");
        if (!one) return;
        insert(one.textContent);
      });

      search.addEventListener("input", show);
      emojiPanel.addEventListener("click", (e) => e.stopPropagation());
      document.addEventListener("click", closeEmoji);
      document.addEventListener("keydown", (e) => {
        if (e.key === "Escape") closeEmoji();
      });
    }

    /* ---- searching inside the thread ------------------------------------
       The panel is a :target so it works with no script at all; this only
       adds the close-on-escape a keyboard expects. */
    const find = $(".chat__find");
    if (find) {
      document.addEventListener("keydown", (e) => {
        if (e.key !== "Escape") return;
        if (!find.matches(":target") && !find.classList.contains("is-open")) return;
        find.classList.remove("is-open");
        window.location.hash = "";
      });
      $$('a[href$="#find"]').forEach((link) =>
        link.addEventListener("click", () => find.classList.add("is-open")));
    }

    /* ---- the stream ----------------------------------------------------
       The primary channel: the hub publishes "message.new" to the recipient,
       so a reply appears without asking. The timer is the fallback for when
       the stream is not there — a browser without EventSource, a proxy that
       buffers it, a dropped connection — and it backs off to a minute while
       the stream is alive. */
    const LIVE_MS = 60000;
    const FALLBACK_MS = 4000;
    let timer = null;
    let interval = null;

    function schedule(ms) {
      if (interval === ms) return;
      interval = ms;
      if (timer) clearInterval(timer);
      timer = setInterval(poll, ms);
    }
    schedule(FALLBACK_MS);

    window.addEventListener("siraj:stream", (e) => {
      schedule(e.detail && e.detail.live ? LIVE_MS : FALLBACK_MS);
    });
    window.addEventListener("siraj:message", () => poll());

    // Somebody read what you sent. Only the ticks change, so only the ticks
    // are asked for — this used to refetch the whole conversation, which
    // marked it read, which announced a read back, which never ended.
    window.addEventListener("siraj:read", () => syncReceipts());

    async function syncReceipts() {
      try {
        const res = await fetch("/messages/" + convID + "/receipts", {
          headers: { "Accept": "application/json", "X-Requested-With": "fetch" },
        });
        if (!res.ok) return;
        const data = await res.json();
        (data.receipts || []).forEach((r) => {
          const row = $('[data-message="' + r.id + '"]', log);
          if (row) updateMessage(row, { read: r.read });
        });
      } catch (err) {
        /* the next event will carry it */
      }
    }

    // An empty composer has nothing to send. Said once here, after everything
    // the answer depends on exists.
    syncSend();
  })();

  /* --------------------------------------------------- the profile photo */

  // What you picked, before you save it. Choosing a picture and seeing the old
  // one still there reads as the choice not having registered.
  (function photoPreview() {
    const input = $("[data-photo-input]");
    const preview = $("[data-photo-preview]");
    if (!input || !preview) return;

    input.addEventListener("change", () => {
      const picked = input.files && input.files[0];
      if (!picked) return;
      const url = URL.createObjectURL(picked);
      preview.style.setProperty("--photo", "url('" + url + "')");
      preview.classList.add("avatar--photo");
      preview.textContent = "";
    });
  })();

  /* ==================================================== pages that go stale */

  // A page is a photograph of the moment it was rendered. Somebody accepts your
  // friend request, answers your duel, replies to your ticket — and the screen
  // in front of you still shows what was true a minute ago. Until now the only
  // thing the stream moved was the badge numbers, so "Request sent" sat there
  // beside a person who had already said yes.
  //
  // A page declares which events change what it is showing; when one arrives,
  // it refreshes.
  //
  // Not while somebody is typing. Reloading out from under a half-written
  // message to say "your friend accepted" trades one person's work for another
  // person's news, so a form in progress gets a nudge it can act on instead.
  (function liveRefresh() {
    const zone = $("[data-live]");
    if (!zone) return;

    const listen = zone.dataset.live.split(/\s+/).filter(Boolean);
    if (!listen.length) return;

    let pending = false;

    function typing() {
      const el = document.activeElement;
      if (el && /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName)) return true;
      // Or has typed: a half-finished message the field no longer has focus on
      // is still work somebody did.
      return $$("input, textarea").some((field) => {
        if (field.type === "hidden" || field.type === "checkbox" || field.type === "radio") return false;
        return field.value && field.value !== field.defaultValue;
      });
    }

    function refresh() {
      if (pending) return;
      if (document.hidden || typing()) {
        pending = true;
        liveNudge();
        return;
      }
      window.location.reload();
    }


    window.addEventListener("siraj:live", (e) => {
      if (listen.includes(e.detail && e.detail.type)) refresh();
    });
    // Coming back to a tab that missed something shows it straight away.
    document.addEventListener("visibilitychange", () => {
      if (!document.hidden && pending) window.location.reload();
    });
  })();

  /* ================================================= where questions come from */

  // The three sources are exclusive, so the form shows one at a time. Without
  // JavaScript every pane stays visible and the radio still decides on the
  // server — the panes are a tidier way to ask, not the rule itself.
  (function questionSource() {
    const pick = $("[data-source-pick]");
    if (!pick) return;

    const panes = $$("[data-source-pane]");

    function show(source) {
      panes.forEach((pane) => {
        // A pane can belong to more than one source: the round length applies
        // to anything drawn, but not to questions written here and now.
        const owners = pane.dataset.sourcePane.split(" ");
        pane.hidden = !owners.includes(source);
      });
    }

    $$('input[name="source"]', pick).forEach((radio) => {
      radio.addEventListener("change", () => radio.checked && show(radio.value));
    });

    const chosen = $('input[name="source"]:checked', pick);
    show(chosen ? chosen.value : "bank");
  })();

  /* ============================================= writing your own questions */

  // "Add another" clones the row that is already there and clears it. The first
  // row is rendered by the server, so the markup lives in one place and this
  // does not have to know what a question looks like.
  (function authoredQuestions() {
    const add = $("[data-authored-add]");
    const rows = $("[data-authored]");
    if (!add || !rows) return;

    const MAX = 10;

    add.addEventListener("click", () => {
      const all = $$(".authored__row", rows);
      if (all.length >= MAX) {
        add.disabled = true;
        return;
      }
      const index = all.length;
      const copy = all[all.length - 1].cloneNode(true);
      copy.classList.remove("authored__row--bad");
      copy.dataset.row = String(index);

      // The radio group is per row. Cloning without renaming would put the new
      // row in the previous row's group, so marking an answer here would unmark
      // the one above — which is exactly what one shared name did to the whole
      // form before this.
      $$("input", copy).forEach((input) => {
        if (input.type === "radio") {
          input.name = "q_correct_" + index;
          input.checked = input.value === "0";
        } else {
          input.value = "";
        }
      });
      // Any error left from the row it was cloned from belongs to that row.
      $$("p", copy).forEach((p) => p.remove());

      const legend = $("legend", copy);
      if (legend) {
        legend.textContent = legend.textContent.replace(/\d+/, String(index + 1));
      }
      rows.appendChild(copy);
      if (all.length + 1 >= MAX) add.disabled = true;
      const first = $("input[type=text]", copy);
      if (first) first.focus();
    });
  })();

  /* ========================================== the panel of people, live */

  // Who wrote, what they said, and how long ago — repainted where the page
  // stands rather than by reloading it. A reload here is never free: the
  // panel sits beside an open thread, and taking the screen out from under a
  // half-written message to say a different message arrived trades one
  // person's work for another person's news.
  //
  // The stream carries no words (an event large enough to draw a preview with
  // is an event carrying what somebody said past whoever may read it), so the
  // page asks the server, as the reader, for the lines it is allowed to show.
  (function messagePanel() {
    const panel = $(".chat__scroll");
    if (!panel) return;

    const openThread = $("[data-thread]");
    const openID = openThread ? openThread.dataset.conversation : "";
    let asking = false;

    function paintRow(row, conv) {
      const preview = $(".chat__item-preview", row);
      if (preview) preview.textContent = conv.preview;
      const when = $(".chat__item-time", row);
      if (when) when.textContent = conv.at;

      // The thread you are looking at is not unread, whatever a count that
      // crossed the read on its way here happens to say.
      const unread = row.dataset.conv === openID ? 0 : conv.unread;
      row.classList.toggle("is-unread", unread > 0);

      const aside = $(".chat__item-aside", row);
      let dot = $(".count-dot", row);
      if (unread > 0 && aside) {
        if (!dot) {
          dot = document.createElement("span");
          dot.className = "count-dot";
          aside.appendChild(dot);
        }
        dot.textContent = String(unread);
      } else if (dot) {
        dot.remove();
      }
    }

    // Newest first, the order the server sorts them in. Moving a row is not
    // rebuilding it: the menu on it keeps its state and its listeners.
    function reorder(ids) {
      const anchor = $(".chat__group", panel);
      let before = null;
      for (let i = ids.length - 1; i >= 0; i--) {
        const row = $('[data-conv="' + ids[i] + '"]', panel);
        if (!row) continue;
        panel.insertBefore(row, before || anchor);
        before = row;
      }
    }

    async function refresh() {
      if (asking || document.hidden) return;
      asking = true;
      try {
        const res = await fetch("/messages/summary", {
          headers: { "Accept": "application/json", "X-Requested-With": "fetch" },
        });
        if (!res.ok) return;
        const data = await res.json();
        const order = [];
        let unseen = false;
        // The panel shows one kind at a time. A group thread is not missing
        // from the people tab, it is simply not that tab's business — and
        // treating it as missing is what made every single send end in
        // "something changed, tap to refresh".
        const tab = panel.dataset.panelTab || "direct";
        (data.conversations || []).forEach((conv) => {
          if ((conv.kind || "direct") !== tab) return;
          const row = $('[data-conv="' + conv.id + '"]', panel);
          // Somebody the panel has never drawn, in the tab it is drawing. The
          // row carries an avatar, a name and a menu, none of which this knows
          // how to invent — so the page offers to fetch it rather than
          // guessing at it.
          if (!row) {
            unseen = true;
            return;
          }
          order.push(conv.id);
          paintRow(row, conv);
        });
        reorder(order);
        paintBadge("/messages", data.unread);
        paintBadge("/notifications", data.notifications);
        paintAvatarDot(data.notifications);
        if (unseen) liveNudge();
      } catch (err) {
        /* the next event asks again */
      } finally {
        asking = false;
      }
    }

    const WATCHED = ["message.new", "message.read", "message.withdrawn"];
    window.addEventListener("siraj:live", (e) => {
      if (WATCHED.includes(e.detail && e.detail.type)) refresh();
    });
    // A thread read here changes this panel's own numbers.
    window.addEventListener("siraj:panel", refresh);
    document.addEventListener("visibilitychange", () => {
      if (!document.hidden) refresh();
    });
  })();

  /* ===================================================== live nudges (SSE) */

  (function events() {
    if (!("EventSource" in window)) return;
    if (!document.querySelector(".tabbar, .nav__links")) return;

    let source;
    try {
      source = new EventSource("/events");
    } catch (e) {
      return;
    }

    // Tell the thread poller whether it still has to do the work itself.
    const announce = (live) =>
      window.dispatchEvent(new CustomEvent("siraj:stream", { detail: { live } }));
    source.addEventListener("open", () => announce(true));
    source.addEventListener("error", () => announce(false));
    source.addEventListener("message.new", (e) => {
      window.dispatchEvent(new CustomEvent("siraj:message"));
      // A badge answers "is there anything?"; it does not answer "somebody is
      // talking to you right now". If the thread is not the one on screen,
      // say so where it can be seen.
      let convID = "";
      try {
        convID = (JSON.parse(e.data) || {}).conversationId || "";
      } catch (err) {}
      const open = $("[data-thread]");
      if (open && open.dataset.conversation === convID) return;
      const said = tmplText("newmessage");
      if (said) toast(said, "info");
    });
    source.addEventListener("message.read", () =>
      window.dispatchEvent(new CustomEvent("siraj:read")));

    const refreshCounts = debounce(async () => {
      try {
        const res = await fetch("/api/counts", {
          headers: { "Accept": "application/json", "X-Requested-With": "fetch" },
        });
        if (!res.ok) return;
        const counts = await res.json();
        paintBadge("/messages", counts.messages);
        paintBadge("/challenges", counts.challenges);
        paintBadge("/friends", counts.friends);
        paintBadge("/support", counts.support);
        paintBadge("/admin/support", counts.supportQueue);
        paintBadge("/notifications", counts.notifications);
        paintAvatarDot(counts.notifications);
      } catch (e) {}
    }, 400);

    // Every event type the server publishes has to be listed here. The support
    // events were being pushed to a client that had never subscribed to them,
    // so a staff reply sat unseen until the player reloaded by hand.
    // Every event the server can publish. One missing from this list is an
    // event that arrives and is heard by nobody, which looks exactly like the
    // page being right.
    ["message.new", "message.withdrawn", "message.read", "challenge.received",
     "challenge.completed", "challenge.played", "challenge.declined",
     "challenge.cancelled", "challenge.accepted", "challenge.started",
     "friend.request", "friend.accepted", "support.reply",
     "support.new"].forEach((type) => {
      source.addEventListener(type, (e) => {
        refreshCounts();
        // And tell the page, which may be showing the very thing that changed.
        window.dispatchEvent(new CustomEvent("siraj:live", { detail: { type, data: e.data } }));
      });
    });

    // A badge is the right answer when the news is about somewhere else. When
    // it is about the screen in front of you, the screen should say so: a duel
    // settling while you are looking at its scoreboard has to fill in the dash
    // beside your opponent's name without being asked.
    ["challenge.completed", "challenge.played", "challenge.cancelled",
     "challenge.accepted", "challenge.started"].forEach((type) => {
      source.addEventListener(type, (e) => {
        let id = "";
        try {
          id = (JSON.parse(e.data) || {}).challengeId || "";
        } catch (err) {
          return;
        }
        const panel = $('[data-duel="' + id + '"]');
        if (panel && panel.dataset.duelWaiting === "1") {
          window.location.reload();
        }
      });
    });

    window.addEventListener("beforeunload", () => source.close());
  })();

  // The notification badge lives inside the account menu, which is closed. A
  // dot on the button that opens it is the only part anybody sees.
  function paintAvatarDot(count) {
    // The account button by name. Taking "the first dropdown trigger" put the
    // dot on the language switcher, which is simply the one that comes first
    // in the document.
    const trigger = $("[data-account-button]");
    if (!trigger) return;
    let dot = $(".has-news", trigger);
    if (typeof count === "number" && count > 0) {
      if (!dot) {
        dot = document.createElement("span");
        dot.className = "has-news";
        dot.setAttribute("aria-hidden", "true");
        trigger.appendChild(dot);
      }
    } else if (dot) {
      dot.remove();
    }
  }

  // One number on one link. Out here rather than inside the stream, because
  // reading a thread clears the very badges the stream draws, and a badge
  // cleared one way and drawn another is a badge that disagrees with itself.
  function paintBadge(href, count) {
    if (typeof count !== "number") return;
    $$('a[href="' + href + '"]').forEach((link) => {
      let dot = $(".count-dot", link);
      if (count > 0) {
        if (!dot) {
          dot = document.createElement("span");
          dot.className = "count-dot";
          link.appendChild(dot);
        }
        dot.textContent = String(count);
      } else if (dot) {
        dot.remove();
      }
    });
  }

  /* ================================================================ sheets */

  // A panel that opens on :target needs one thing the URL cannot give it: the
  // escape key. Everything else — opening, closing, working without a script —
  // is the anchor and the stylesheet.
  (function sheets() {
    if (!$("[data-sheet]")) return;

    function openSheet() {
      return $$("[data-sheet]").find((el) => el.id && location.hash === "#" + el.id);
    }

    document.addEventListener("keydown", (e) => {
      if (e.key !== "Escape" || !openSheet()) return;
      // Back to the page with no fragment, and without leaving a history
      // entry for having closed a panel.
      history.replaceState(null, "", location.pathname + location.search);
    });

    // Focus what the panel is for, rather than leaving the caret where the
    // page had it.
    window.addEventListener("hashchange", () => {
      const open = openSheet();
      if (!open) return;
      const first = $("input, a, button", $(".sheet__body", open) || open);
      if (first) first.focus({ preventScroll: true });
    });
  })();

  /* ================================================== voice notes */

  // One player for every recording on the page.
  //
  // The browser's own control is the wrong shape for a voice note and reads
  // its length out of a file that does not carry one. This draws the button,
  // the bars and the clock; the <audio> element underneath still does the
  // playing, because nothing else should.
  const PLAY_ICON = "<svg class=\"icon\" viewBox=\"0 0 24 24\" fill=\"currentColor\" aria-hidden=\"true\"><path d=\"M8 5.2v13.6a.8.8 0 0 0 1.2.7l11-6.8a.8.8 0 0 0 0-1.4l-11-6.8a.8.8 0 0 0-1.2.7Z\"></path></svg>";
  const PAUSE_ICON = "<svg class=\"icon\" viewBox=\"0 0 24 24\" fill=\"currentColor\" aria-hidden=\"true\"><rect x=\"6.5\" y=\"4.5\" width=\"4\" height=\"15\" rx=\"1.3\"></rect> <rect x=\"13.5\" y=\"4.5\" width=\"4\" height=\"15\" rx=\"1.3\"></rect></svg>";

  (function voiceNotes() {
    let playing = null;

    function face(holder, isPlaying) {
      const button = $("[data-voice-play]", holder);
      if (!button) return;
      button.innerHTML = isPlaying ? PAUSE_ICON : PLAY_ICON;
      button.setAttribute("aria-label",
        (isPlaying ? tmplText("pause") : tmplText("play")) || "");
      holder.classList.toggle("is-playing", isPlaying);
    }

    function clock(seconds) {
      const total = Math.round(seconds || 0);
      return Math.floor(total / 60) + ":" + String(total % 60).padStart(2, "0");
    }

    // How far through it is, as a count of bars. Lighting whole bars rather
    // than cutting one in half: at three pixels wide, half a bar is nothing
    // anybody can see anyway.
    function progress(holder, audio) {
      const wave = $("[data-voice-wave]", holder);
      if (!wave || !audio.duration || !isFinite(audio.duration)) return;
      const bars = Array.prototype.slice.call(wave.children);
      const upto = Math.round(audio.currentTime / audio.duration * bars.length);
      bars.forEach((bar, i) => bar.classList.toggle("is-played", i < upto));
    }

    function rewind(holder) {
      const wave = $("[data-voice-wave]", holder);
      if (!wave) return;
      Array.prototype.forEach.call(wave.children, (bar) => bar.classList.remove("is-played"));
    }

    function stop(holder) {
      const audio = $("[data-voice-audio]", holder);
      if (audio) audio.pause();
    }

    // One at a time: two recordings talking over each other is nobody being
    // heard.
    document.addEventListener("click", (e) => {
      const button = e.target.closest("[data-voice-play]");
      if (!button) return;
      const holder = button.closest("[data-voice]");
      const audio = holder && $("[data-voice-audio]", holder);
      if (!audio) return;

      if (!audio.paused) {
        audio.pause();
        return;
      }
      if (playing && playing !== holder) stop(playing);
      playing = holder;

      // Wired once. Pressing play, pause and play again used to add another
      // copy of every listener, so by the third press the clock was being set
      // three times a frame.
      if (holder.dataset.voiceWired) {
        const again = audio.play();
        if (again && again.catch) again.catch(() => face(holder, false));
        return;
      }
      holder.dataset.voiceWired = "1";

      audio.addEventListener("timeupdate", () => progress(holder, audio));
      audio.addEventListener("ended", () => {
        face(holder, false);
        rewind(holder);
        // Back to the length it runs, which is what the row said before it
        // was ever pressed.
        const time = $("[data-voice-time]", holder);
        if (time && holder.dataset.voiceLength) time.textContent = holder.dataset.voiceLength;
      });
      audio.addEventListener("pause", () => face(holder, false));
      audio.addEventListener("play", () => face(holder, true));
      // Once it is loaded the file can say how long it is after all, and a
      // measured length is better than a claimed one.
      audio.addEventListener("loadedmetadata", () => {
        const time = $("[data-voice-time]", holder);
        if (!holder.dataset.voiceLength && time) holder.dataset.voiceLength = time.textContent;
        if (time && isFinite(audio.duration) && audio.duration > 0) {
          time.textContent = clock(audio.duration);
          holder.dataset.voiceLength = time.textContent;
        }
      });
      // Counting up while it plays, not down: the clock says where you are.
      audio.addEventListener("timeupdate", () => {
        const time = $("[data-voice-time]", holder);
        if (time) time.textContent = clock(audio.currentTime);
      });

      const started = audio.play();
      if (started && started.catch) {
        started.catch(() => {
          face(holder, false);
          toast(tmplText("error") || "…", "error");
        });
      }
    });
  })();


  /* ============================================== picking somebody, in place */

  // Rule 5 is explicit: choosing a player happens in a dropdown or dialog you
  // pick from, and never by going to another page. The list is already on the
  // page, so this is a filter over it rather than a search — no request, no
  // spinner, nothing to lose if it fails.
  (function picker() {
    $$("[data-picker]").forEach((root) => {
      const input = $("[data-picker-input]", root);
      const empty = $("[data-picker-empty]", root);
      if (!input) return;

      const rows = $$("[data-picker-item]", root);
      // A heading with nothing under it is worse than no heading: it says a
      // group exists and then shows an empty space where it should be.
      const labels = $$(".sheet__label", root);

      function apply() {
        const q = input.value.trim().toLowerCase();
        let shown = 0;
        rows.forEach((row) => {
          const hit = !q || (row.dataset.pickerItem || "").toLowerCase().includes(q);
          row.hidden = !hit;
          if (hit) shown++;
        });
        labels.forEach((label) => {
          let any = false;
          for (let el = label.nextElementSibling; el; el = el.nextElementSibling) {
            if (el.classList.contains("sheet__label")) break;
            if (el.dataset && el.dataset.pickerItem && !el.hidden) { any = true; break; }
          }
          label.hidden = !any;
        });
        if (empty) empty.hidden = shown > 0;
      }

      // Past what the page rendered there may be more — a room of three
      // hundred is not a list. The filter above answers instantly from what is
      // already here; this asks the server only once the local answer is
      // empty, so the common case costs nothing.
      const remote = $("[data-picker-remote]", root);
      const scope = root.dataset.scope || "message";
      let lastAsked = "";

      async function search() {
        const q = input.value.trim();
        if (!remote) return;
        if (q.length < 2 || q === lastAsked) return;
        lastAsked = q;
        try {
          const res = await fetch(
            "/api/people?scope=" + encodeURIComponent(scope) + "&q=" + encodeURIComponent(q),
            { headers: { "Accept": "application/json", "X-Requested-With": "fetch" } });
          if (!res.ok) return;
          const found = ((await res.json()) || {}).people || [];
          // Anything the page already shows is not news.
          const known = new Set(rows.map((r) => (r.dataset.pickerUser || "")));
          remote.innerHTML = "";
          found.filter((p) => !known.has(p.username)).forEach((p) => {
            const a = document.createElement("a");
            a.className = "chat__item picker__row";
            a.href = "/messages/with/" + encodeURIComponent(p.username);
            const name = document.createElement("span");
            name.className = "chat__item-body";
            const line = document.createElement("span");
            line.className = "chat__item-name truncate";
            line.textContent = p.displayName;
            const at = document.createElement("span");
            at.className = "chat__item-preview truncate";
            at.textContent = "@" + p.username;
            name.append(line, at);
            a.append(name);
            remote.append(a);
          });
          if (empty) empty.hidden = remote.childElementCount > 0 || shownCount() > 0;
        } catch (e) {
          // The local filter already answered. A failed search adds nothing
          // and should take nothing away.
        }
      }

      function shownCount() {
        return rows.filter((r) => !r.hidden).length;
      }

      input.addEventListener("input", apply);
      input.addEventListener("input", debounce(search, 250));
      apply();
    });
  })();

  /* ================================================= who is here, without a trip */

  // The member list is rendered with the page and opened by :target, so the
  // link already works with no script at all. Two things are added here.
  //
  // The first is not leaving a history entry: opening a panel is not a place,
  // and pressing Back after reading it should leave the thread rather than
  // close a box.
  //
  // The second is keeping it current. Somebody joining or leaving a room is
  // exactly the news this panel exists to carry, and a list that was true when
  // the page loaded is the wrong answer to "who is here".
  (function whoIsHere() {
    const panel = $("[data-who]");
    if (!panel) return;

    const list = $("[data-who-list]", panel);
    const convID = panel.dataset.conversation;

    $$("[data-who-open]").forEach((link) => {
      link.addEventListener("click", (e) => {
        e.preventDefault();
        const open = panel.classList.contains("is-open") ||
          location.hash === "#who";
        if (open) {
          panel.classList.remove("is-open");
          history.replaceState(null, "", location.pathname + location.search);
          return;
        }
        panel.classList.add("is-open");
        refresh();
      });
    });

    document.addEventListener("keydown", (e) => {
      if (e.key !== "Escape") return;
      if (!panel.classList.contains("is-open") && location.hash !== "#who") return;
      panel.classList.remove("is-open");
      history.replaceState(null, "", location.pathname + location.search);
    });

    async function refresh() {
      if (!list || !convID) return;
      try {
        const res = await fetch("/messages/" + convID + "/members", {
          headers: { "Accept": "application/json", "X-Requested-With": "fetch" },
        });
        if (!res.ok) return;
        const html = await res.text();
        const holder = document.createElement("div");
        holder.innerHTML = html;
        const fresh = $("[data-who-list]", holder);
        if (fresh) list.replaceWith(fresh);
      } catch (e) {
        // The list already on screen is the fallback, and it is a reasonable
        // one: it was true a moment ago.
      }
    }

    // Somebody joining or leaving a room publishes a message event to
    // everyone in it, which is the cheapest signal that the membership has
    // moved. Only acted on while the panel is actually open: a list nobody is
    // looking at does not need to be right.
    window.addEventListener("siraj:live", (e) => {
      const type = e.detail && e.detail.type;
      if (type !== "message.new") return;
      if (!panel.classList.contains("is-open") && location.hash !== "#who") return;
      refresh();
    });
  })();

  /* ================================================== duel or teams, on the form */

  // The team panel is only a question if the answer is "teams". Shown and
  // hidden here rather than left on screen greyed out, because a set of
  // side-pickers that do nothing is a set of controls that lie.
  (function matchFormat() {
    const pick = $("[data-format-pick]");
    const pane = $("[data-team-pane]");
    if (!pick || !pane) return;

    function sync() {
      const chosen = $("input[name=format]:checked", pick);
      pane.hidden = !chosen || chosen.value !== "team";
    }
    pick.addEventListener("change", sync);
    sync();
  })();



  /* ------------------------------------------- one group per match (setup) */

  // A match is with one group. The form shows that group's list and hides the
  // others — and empties them, because a checkbox that is hidden is still
  // submitted, and a hidden tick is how a mixture reaches the server.
  (function sourceGroup() {
    const picker = $("[data-source-group]");
    if (!picker) return;
    const pools = $$("[data-source-pool]");

    // Which sides to ask about: the people actually ticked, and nobody else.
    // A select beside the name of somebody who is not playing is a question
    // about a non-player, and it was being asked for every person in every
    // group whatever the form had been told.
    // The team minimum, kept beside the server's own number. A test reads this
    // line and fails if the two ever disagree, so the form can never offer a
    // shape the server refuses.
    const MIN_TEAM_PLAYERS = 4;
    // And how many of them each side needs. A side of one is not a side.
    const MIN_PER_SIDE = 2;
    // And the smallest match there is. One person is a solo round, which is
    // the other screen.
    const MIN_PLAYERS = 2;
    // The group whose people are at this phone rather than on a list, spelled
    // the way the server spells it in models.SourceDevice.
    const DEVICE_SOURCE = "device";

    // Who is actually ticked, read from the boxes themselves.
    //
    // This used to look each one up by building a selector around its value,
    // which for a uuid means interpolating a string into an attribute selector
    // and hoping the escaping is right — and then probing offsetParent to
    // guess whether the box was on screen. Two mechanisms that can each fail
    // silently and leave the count at zero, which is what kept Teams shut with
    // two players at the device ticked in front of you.
    //
    // Reading the boxes needs neither. A pool we have hidden is a pool whose
    // boxes sync() has already unchecked, so "visible and checked" is just
    // "checked".
    function pickedKeys() {
      const keys = new Set();
      $$("[data-source-pool]").forEach((pool) => {
        if (pool.hidden) return;
        $$("input[name=opponent], input[name=local]", pool).forEach((box) => {
          if (box.checked && !box.disabled) keys.add(box.value);
        });
      });
      return keys;
    }

    function syncTeams() {
      const picked = pickedKeys();
      const enoughPlayers = syncStart(picked.size);

      // A side-picker beside the name of somebody who is not playing is a
      // question about a non-player.
      $$("[data-team-row]").forEach((row) => {
        row.hidden = !picked.has(row.dataset.teamRow);
      });
      const empty = $("[data-team-empty]");
      if (empty) empty.hidden = picked.size > 0;

      // Sides need somebody to share one with. Two players is one against one
      // however the sides are labelled, so the option is shut until a third
      // person is in — and if it was already chosen, it falls back rather than
      // letting the form carry a shape the server will refuse.
      const teamChip = $("[data-format-team]");
      if (!teamChip) return;
      const teamRadio = $("input", teamChip);
      const enough = picked.size + 1 >= MIN_TEAM_PLAYERS;

      teamRadio.disabled = !enough;
      teamChip.classList.toggle("is-empty", !enough);
      const locked = $("[data-format-locked]");
      if (locked) locked.hidden = enough;

      if (!enough && teamRadio.checked) {
        const duel = $('[data-format-pick] input[value="duel"]');
        if (duel) {
          duel.checked = true;
          duel.dispatchEvent(new Event("change", { bubbles: true }));
        }
      }

      syncSplit(teamRadio.checked && enough, enoughPlayers);
    }

    // What the start button says, and whether it can be pressed.
    //
    // The count is the people ticked plus the host, who is always in it. The
    // wording is rebuilt from the two sentences the server rendered — one
    // singular, one plural with a specimen number in it — by swapping the
    // digits for the real count. Building it from a format string instead is
    // how "%d" ends up on a button, which has happened here before.
    function syncStart(picked) {
      const button = $("[data-start-button]");
      if (!button) return;

      const total = picked + 1;
      const template = total === 1 ? button.dataset.labelOne : button.dataset.labelMany;
      if (template) button.textContent = template.replace(/\d+/, String(total));

      const enough = total >= MIN_PLAYERS;
      button.disabled = !enough;
      const hint = $("[data-start-hint]");
      if (hint) hint.hidden = enough;
      return enough;
    }

    // Whether the sides, as they currently stand, are sides.
    //
    // Enough players is not the same question as a workable split: four
    // people arranged three against one is a duel in which one of the
    // duellists is a crowd, and the server refuses it. It used to refuse it
    // after the form was sent, which is the worst moment to learn that one
    // dropdown needed changing. So the split is counted here as it is edited,
    // and the button is held until it adds up.
    function syncSplit(isTeams, enoughHere) {
      const warning = $("[data-team-split]");
      const submit = $("[data-start-button]");

      if (!isTeams) {
        if (warning) warning.hidden = true;
        // Only the split's own objection is lifted here. Whether there are
        // enough people at all is syncStart's answer, and it stands.
        if (submit && enoughHere) submit.disabled = false;
        return;
      }

      // The host counts, and so does everybody whose row is on screen —
      // a hidden row belongs to somebody who is not playing.
      const sides = new Map();
      const add = (side) => sides.set(side, (sides.get(side) || 0) + 1);

      const host = $("select[name=host_team]");
      add(host ? host.value : "1");
      $$("[data-team-row]").forEach((row) => {
        if (row.hidden) return;
        const pick = $("select", row);
        if (pick) add(pick.value);
      });

      let ok = sides.size >= 2;
      sides.forEach((count) => { if (count < MIN_PER_SIDE) ok = false; });

      if (warning) warning.hidden = ok;
      if (submit) submit.disabled = !ok || !enoughHere;
    }


    function sync() {
      const chosen = $("input[name=player_source]:checked", picker);
      const value = chosen ? chosen.value : "";
      pools.forEach((pool) => {
        const on = pool.dataset.sourcePool === value;
        pool.hidden = !on;
        if (!on) $$("input[type=checkbox]", pool).forEach((box) => { box.checked = false; });
      });
      $$(".segment__item", picker).forEach((item) => {
        const input = $("input", item);
        item.classList.toggle("is-on", !!input && input.checked);
      });
      // The roster panel above the card is about this phone, so it belongs to
      // the device group and goes with it. Left on screen for the other two it
      // headed a challenge to a friend three cities away with "Playing on this
      // device", over a box for adding somebody who could not be in that match.
      const roster = $("[data-device-roster]");
      if (roster) roster.hidden = value !== DEVICE_SOURCE;
      syncTeams();
    }

    picker.addEventListener("change", sync);
    // Ticking a person is what decides whether they get a side.
    document.addEventListener("change", (e) => {
      if (e.target.name === "opponent" || e.target.name === "local") syncTeams();
      if (e.target.name === "host_team" || e.target.name.startsWith("team_") ||
          e.target.name === "format") syncTeams();
    });
    sync();
  })();

  /* ----------------------------------------- one question per kind (support) */

  // The new-message form asks what the chosen kind needs and nothing else.
  // Without a script every pane is visible and the server still reads only the
  // field that belongs to the kind — the panes are a tidier way to ask, not the
  // rule itself.
  (function ticketKind() {
    const picker = $("[data-ticket-kind]");
    if (!picker) return;
    const panes = $$("[data-ticket-pane]");

    function sync() {
      const chosen = $("input[name=kind]:checked", picker);
      const kind = chosen ? chosen.value : "";
      panes.forEach((pane) => {
        pane.hidden = !pane.dataset.ticketPane.split(" ").includes(kind);
      });
    }
    picker.addEventListener("change", sync);
    sync();
  })();

  // Something changed that the page cannot draw where it stands. A bar the
  // reader can act on, rather than a reload that decides for them.
  function liveNudge() {
    if ($("#live-nudge")) return;
    const bar = document.createElement("button");
    bar.id = "live-nudge";
    bar.className = "live-nudge";
    bar.type = "button";
    bar.textContent = tmplText("live-changed") || "…";
    bar.addEventListener("click", () => window.location.reload());
    document.body.appendChild(bar);
  }

  function debounce(fn, ms) {
    let timer;
    return function () {
      clearTimeout(timer);
      timer = setTimeout(fn, ms);
    };
  }
})();
