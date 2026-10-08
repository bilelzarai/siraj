# Siraj Admin redesign: implementation brief for the VS Code coding agent

Paste this whole file into your coding agent (Claude Code, Copilot Chat, Cursor…) from the root of the Siraj repo, after copying the `design/` folder into the repo (suggested location: `design/admin/`).

---

## Your task

Restyle every page under `/admin` to match the approved design in `design/admin/`. The design is plain HTML + one CSS file + one JS file, so it ports into any stack (EJS, Blade, Jinja, Razor, React, Vue…).

**Only the UI layer changes.** Keep every existing route, controller, query, form `action`/`method`, CSRF token, permission check and API call exactly as it is.

### Step 1: discover, then plan (do not edit yet)

1. Find how the admin is rendered today: framework, template engine, the shared admin layout (sidebar + header), and where static assets are served from.
2. List the 11 admin views and the template file behind each route (table below).
3. For each page, list which elements of the design already have real data and which do not (see "Data the design shows that may not exist yet").
4. Show me that plan and wait for my OK.

### Step 2: implement

1. **Assets.** Copy `design/admin/assets/admin.css`, `admin.js` and `icons.svg` into the static folder and link them from the admin layout only (do not load them on the public site). Load the fonts with the `<link>` tags from the `<head>` of any design page (Figtree, Noto Naskh Arabic, JetBrains Mono).
2. **Theme bootstrap.** Copy the one-line `<script>` from the design `<head>` (it sets `data-theme` before paint so there is no flash).
3. **Shared layout.** Replace the current admin layout with the shell from any design page: `.app` → `aside.sidebar` + `.main` → `header.topbar` + `main.page`.
   - Sidebar nav groups: Overview, People, Content, Moderation, System. Use the SVG icons (`<svg class="icon"><use href="/path/icons.svg#i-name"/></svg>`); remove all emoji from the navigation.
   - Active item: `class="nav-item is-active" aria-current="page"` based on the current route.
   - Counts on Support, Review queue, Comments, Rated poorly come from real queries. `nav-count is-hot` (gold) only for Support and Review queue when > 0. Hide a count when it is 0.
   - Footer shows the signed-in admin's initials, name and role; the sign-out link uses the existing logout route/form.
   - Breadcrumbs in the top bar: `Admin › Group › Page`.
4. **Pages.** Port each page's markup and swap the mock rows for your real loops. Keep the class names and `data-*` attributes exactly; `admin.js` relies on them.
5. **Existing actions keep working.** Retire / Restore / Delete / Edit / Approve / Reject etc. must still submit to the same endpoints. Wrap design buttons in the existing `<form>`s or move the existing handlers onto them. Remove the demo `data-toast` attributes wherever the real flow already gives feedback (flash message → render it with `sirajToast("…")`).
6. **Drawers and modals.** "New category", "Edit category", "Edit question", "New domain" and "View user" open as side drawers (`data-open="id"`). Retire and Ban use the confirm modal (`data-confirm="modal-id" data-name="…"`). Wire the drawer forms to the existing create/update endpoints. If a page currently uses a separate edit page, keep that page and style it with the same `.form-section`, `.field`, `.input` classes instead of forcing a drawer.
7. **Language direction.** If the admin can run in Arabic, render `<html lang="ar" dir="rtl">`. The CSS uses logical properties only, so the layout mirrors automatically. Any Arabic text inside an English page gets `dir="rtl" lang="ar"` on its element.

### Route → design file

| Route | Design file | Key components |
|---|---|---|
| `/admin` | `dashboard.html` | KPI tiles, bar chart, needs-attention list, category performance table, language split, activity timeline |
| `/admin/users` | `users.html` | toolbar + segmented filter, selectable table + bulk bar, user drawer, ban modal |
| `/admin/support` | `support.html` | 4 stat tiles (Open, Awaiting reply, High priority, Assigned to me) that jump to a filter; status tabs All/Open/Awaiting/Mine/Closed; search by subject or username; priority + assignee selects; topic chips with counts (Suggestion, About a question, Something is broken, My account, Report a player, Something else); rows-per-page 5/10/20/50; ticket thread with Status/Priority/Assignee/Topic selects, internal notes, reply composer |
| `/admin/domains` | `domains.html` | domain cards with stats, new-domain card + drawer |
| `/admin/categories` | `categories.html` | info callout, filter toolbar, sortable table, language chips, live/retired badges, category drawer with live preview, retire modal |
| `/admin/questions` | `questions.html` | **Domain filter** + category select grouped by domain (category list narrows to the chosen domain), difficulty, translation coverage, status tabs incl. Retired, live result count, "Clear filters", empty state; `#ID` links; points shown per question; selectable table; question drawer with Domain, Category, Difficulty, **Points**, AR/EN/FR tabs |
| `/admin/review` | `review.html` | queue list, 3-language side-by-side card, automatic checks, approve/request changes/reject with keyboard shortcuts A/E/R/S |
| `/admin/comments` | `comments.html` | tabs, comment cards with question reference, flagged state |
| `/admin/rated` | `rated.html` | up/down split bars, reason tags |
| `/admin/integrity` | `integrity.html` | score ring, check rows with severity, **Possible duplicates** (language tabs العربية/English/Français with pair counts, similarity %, Compare → side-by-side table of Category, Difficulty, Points, Correct answer and the question + A–D in that language with differing words highlighted and ✓ on the correct answer, "Different questions" dismiss, "Retire this one" per question), translation coverage matrix |
| `/admin/audit` | `audit.html` | day-grouped table, before/after diff |

In the design files, links between pages point to `*.html`; replace them with the real routes.

### Already in the current admin (wire these, they exist)

Seen on the live site; map them straight onto the new design:

- Support: Open / Awaiting reply / High priority / Assigned to me counts, status tabs, topic chips with counts, priority and assignee filters, subject-or-username search, rows per page.
- Content health: the duplicate sweep ("swept just now"), per-language results, similarity %, compare view (fields that differ, highlighted words, correct-answer ✓), "Different questions" and "Retire this one" actions, and the "No problems found" empty states.
- Questions: numeric IDs (`#401`), difficulty 1–3, points, domain → category.

The admin currently sits inside the player app's top navigation (Home, Play, Challenges…). The new design gives the admin its own shell; keep a way back with the "Open Sirāj" link in the top bar.

### Filters (no page reload needed for the demo, server-side in your app)

The design filters in the browser through `data-filter-for="targetId" data-key="x"` on selects, search inputs, segmented controls and chip rows; rows carry `data-x` values. In your app, keep the filters **server-side** (query string → query) if lists are paginated, and render the same controls with the current values selected. Use the client-side engine only for lists that are fully loaded.

### Data the design shows that may not exist yet

Do **not** invent backend features to fill these. For each, either wire it to existing data or leave it out, then list what you skipped in your final message:

- Dashboard: sparklines, week-over-week deltas, 14-day games chart, players by language.
- Categories: drag-to-reorder (if there is no reorder endpoint, keep the Order column and hide the drag handle).
- Questions: accuracy and plays per question; "Needs update" (stale translation) state.
- Review queue: automatic checks, note to author, "Request changes" status.
- Support: internal notes, assignee, canned replies, device/app info.
- Comments: flag/report counts, Hidden tab.
- Rated poorly: reason tags.
- Users: streak, strongest categories.

### Rules

- Colours, radii, shadows and spacing come from the CSS tokens at the top of `admin.css`. Do not hard-code hex values in templates (the only exception is a category's own colour, passed as `style="--c:#hex"` on `.cat-tile`).
- Category icons stay as the emoji stored on each category, inside `.cat-tile`.
- Status badges: `badge-live`, `badge-retired`, `badge-draft`, `badge-pending`, `badge-danger`, `badge-info`, `badge-lamp`.
- Language coverage: `<span class="lang">AR</span>`, add `is-missing` or `is-stale`.
- Tables that must collapse into cards on phones get `class="table stack-sm"` and every `<td>` a `data-label`. The first cell gets `cell-first`; low-value cells get `hide-sm`.
- Every icon-only button needs an `aria-label`.
- Do not change the public player-facing site.

### Done when

- All 11 pages match the design at 1440px, 1024px and 390px widths, in light and dark.
- Every action that worked before still works (create, edit, retire, restore, delete, approve, reject, reply, ban…).
- No console errors; no horizontal page scroll on mobile.
- Keyboard: Tab reaches every control with a visible focus ring; Esc closes drawers/modals; Ctrl/⌘K focuses search; `/` focuses the page filter.
- You list any design element you left out because the data does not exist.
