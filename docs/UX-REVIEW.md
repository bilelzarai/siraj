# Sirāj — UX review, screen by screen

**Reviewed:** 2026-10-07 · **Build:** working tree at the time of review, served from the
`siraj` docker stack on `http://localhost:8080` · **Reviewer role:** senior UX

## Scope and method

Every screen the router exposes as a `GET` was opened and read — 36 in total: 5 public,
15 player, 16 admin. Signed in as `aisha_t` (admin) so the admin surface could be reviewed
from a real session rather than guessed at.

Four passes, because each catches what the others miss:

1. **Structural** — every page parsed, checking headings, labels, table semantics,
   landmarks, `autocomplete`, live regions, viewport and skip links.
2. **Visual** — rendered in headless Chrome at 1440×900 (desktop) and 390×844 (mobile),
   in English LTR and Arabic RTL, and looked at.
3. **Source** — templates, CSS and handlers read where the rendered output raised a
   question, so each point names a cause and not just a symptom.
4. **Data** — the live database queried where a screen's content looked wrong.

Severity: **P1** breaks trust or blocks a task · **P2** real friction, no workaround ·
**P3** polish. Action: **FIX** (defect) · **UPDATE** (change what's there) ·
**ADD** (something missing) · **ENHANCE** (works, could be better) · **REMOVE**.

---

## The ten that matter most

| # | Screen | Severity | Action | Point |
|---|---|---|---|---|
| 1 | Admin · Questions / all play | **P1** | REMOVE | A smoke-test fixture is live in the question bank and players can draw it |
| 2 | Landing | **P1** | UPDATE | Three competing calls to action, and the strongest one points away from signup |
| 3 | Settings | **P1** | UPDATE | Languages are labelled with national flags |
| 4 | Settings | **P2** | FIX | The theme control never shows which theme is active |
| 5 | Landing | **P2** | FIX | Page title reads `Sirāj · Sirāj` |
| 6 | Dashboard | **P2** | ADD | Category cards hide the counts the play screen shows |
| 7 | Admin (3 screens) | **P2** | ADD | Filter bars have no labels on any control |
| 8 | Admin (9 tables) | **P2** | ADD | No table has a caption and no header cell has a scope |
| 9 | Play setup | **P2** | ADD | You can ask for more questions than the difficulty holds |
| 10 | Everywhere | **P2** | UPDATE | Emoji are used as the interface icon set |

---

## Status — fixed 2026-10-07

Everything below was implemented and verified against the running stack except
the four items listed as *not done*, each with its reason. `go build`, `go vet`,
`go test ./...` (6 packages) and `scripts/check-i18n.py` (964 keys) are clean,
and the structural scan that produced this review was re-run over all 36 screens
afterwards.

**Measured before → after, across 36 screens**

| | before | after |
|---|---|---|
| Tables with no `<caption>` | 11 | **0** |
| Header cells with no `scope` | 56 | **0** |
| Form controls with no accessible name | 15 | **0** |
| Heading-level jumps | 1 | **0** |
| Smoke fixtures live in the question bank | 1 | **0** |
| National flags used as language labels | 9 | **0** |

**Fixed:** Q1 (fixture deleted; the suite now cleans up after itself), L1, L2,
L3, L5, S1, S2, AX4, A2, Q2, Q4, C2, D1, D2, D3 (partial — see below), D4, P1,
P2, M1, M2, AD1, AD2, AN1, AN3, AN4, Q3 (partial — see below), AX1, AX2.

**Not done, and why:**

- **C1 · the emoji icon system.** This is a design migration, not a fix: roughly
  26 coherent icons for navigation, tab bar, stat tiles, admin sections and the
  user menu. Doing half of it leaves a mixed interface that reads worse than the
  consistent one there now, so the set should be agreed before it starts. Two
  surfaces were migrated where the icons already existed: the appearance control
  on Settings, and the messages search field (a new `IconSearch`).
- **Q5 · sortable admin columns.** A feature — repository ordering, handler
  parameters, header controls — rather than a defect.
- **AN2 · a pinned first column on mobile tables.** Needs a per-table decision:
  on the questions table the first column is a checkbox, so pinning it anchors
  nothing worth anchoring.
- **P3 · an estimate of how long a round takes.** Needs a real per-question
  duration. Inventing one would be exactly the hardcoded number this project's
  rules forbid.

### Corrections to this review

Five points did not survive implementation. Recording them because a review that
is not corrected is worse than one that was never written:

1. **D5 was wrong.** The daily card already branches on `DailyPlayed` and has
   both states. The screenshot showed *See your result* because that account had
   played, not because the other state was missing.
2. **M3 was wrong.** The `username` field with no `autocomplete` is a
   `type="hidden"` input carrying a username into the block action. The scanner
   excluded hidden inputs from the label check but not from the autocomplete
   check.
3. **Q3 was half wrong.** The confirmation already existed — the form carries
   `data-confirm` and the server refuses a question with recorded answers on the
   first attempt. Only the visual distinction was missing, and that is what was
   changed: Delete now carries the danger colour instead of looking exactly like
   Deactivate beside it.
4. **S2's visual claim was an artifact.** "In the rendered page all three look
   identical" came from a `file://` render where the cross-origin module script
   never executed. With script running, the selected state did show. The actual
   defects — no `aria-checked`, no `role=radio`, nothing rendered by the server —
   were real, and are fixed.
5. **P1's premise was too strong.** The script already wrote the explanation on
   every change; it was simply never rendered by the server, so the first paint
   and any page without script showed a menu with options missing and no reason
   for it. That is what was fixed.

### One fix that depends on the machine

**D3 · the honorific.** `ﷺ` (U+FDFA) is correct in the data; Inter has no glyph
for it, so a Latin-script line fell through to an emoji font and drew a Kaaba.
`"Noto Naskh Arabic"` and `"Amiri"` are now named in `--font-sans`, which
resolves it per glyph on any machine carrying either face. On a machine carrying
neither — the headless browser used for this review is one — it still falls to
the emoji font. A guaranteed fix is either bundling an OFL subset of one of
those faces, or spelling the honorific out in the three locale strings. Both are
decisions for you: one adds a binary asset, the other changes religious text.

---

## Cross-cutting — fix once, every screen improves

### C1 · Emoji are doing the job of an icon system — **P2 · UPDATE**

Navigation (🏠 🎮 ⚔️ 💬 🤝 🏆), stat tiles (🎯 ⚡ 🏆 ⭐ 🪙), all eight category cards,
the admin sidebar, difficulty chips and the language and appearance controls all render
emoji as their icon.

Why it matters: emoji are drawn by the operating system, so the interface looks different
on every device and cannot follow the brand colour or the text weight; they carry no
accessible name of their own; and several read oddly in context — ⚔️ crossed swords for
*Challenges* inside an Islamic knowledge game is a questionable metaphor, and 🎮 for
*Play* says video game rather than learning.

The project already ships a proper SVG icon set in [icons.templ](../internal/views/icons.templ) —
the header theme toggle uses it correctly, with a real `aria-label`. Extend that set and
retire the emoji from chrome. Keep emoji only where they are content, never furniture.

### C2 · Admin tables are not reachable by screen reader structure — **P2 · ADD**

All nine admin tables (dashboard ×2, users, domains, categories, questions, review,
integrity, audit) render with **no `<caption>`**, and **not one `<th>` carries `scope`**
— 56 header cells in total. A sighted user sees the column header; a screen-reader user
hears cell values with nothing to anchor them to.

Add `scope="col"` to every header cell and a `<caption>` naming the table (visually
hidden if it would duplicate the `<h1>`). One shared table partial fixes all nine.

### C3 · What is genuinely solid

Worth stating, because it is unusual: **every** page has exactly one `<h1>`, a viewport
meta, a skip link, a meta description, correct `lang` and `dir`, and at least one
`aria-live` region. **No image anywhere is missing `alt`.** Of every interactive control
across 36 screens, exactly one lacks an accessible name, and that one is a decorative
scrim correctly marked `aria-hidden`. RTL is not an afterthought — the Arabic rendering
mirrors properly, including the CSS logical properties and the RTL-specific overrides in
the stylesheet. This is a well-built front end; the points below are refinements on a
sound base, except where marked P1.

---

## Public screens

### Landing — `/`

**L1 · Three calls to action compete, and the loudest one points away from signup — P1 · UPDATE**

The hero offers *Play without an account* (solid dark green, visually dominant),
*Start playing* (pale green) and *I already have an account* (outline), side by side.
Two problems at once: the labels *Play without an account* and *Start playing* describe
almost the same act, so the choice is not meaningful to a first-time visitor; and the
strongest visual weight sits on the guest path, which the page's own small print says
is deleted after a day.

Recommend one primary — *Start playing* (creates the account) — with *Play without an
account* demoted to a text link beneath it, and *I already have an account* moved to the
header where the *Sign in* button already lives. The guest path stays available; it stops
out-shouting the path that retains people.

**L2 · Page title is the brand twice — P2 · FIX**

`<title>Sirāj · Sirāj</title>` in English, `سِراج · سِراج` in Arabic. The landing page is
passing the brand as its own page name, and the layout appends the brand again. It is the
first thing a browser tab, a bookmark and a search result show. Use the tagline or a plain
noun — `Sirāj — an Islamic game that blends fun with learning`.

**L3 · The hero repeats the logo — P3 · REMOVE**

A 🌙 SIRĀJ eyebrow sits directly under a header that already shows the mark and wordmark.
It spends the most valuable vertical space on the page restating what is 60px above it.
Drop it and let the headline move up.

**L4 · "65 questions" is advertised as a feature — P2 · UPDATE**

The stat row reads 65 questions · 8 categories · 3 languages. 65 is a number that argues
against trying the product, and the admin dashboard's own coverage target is 100 per
difficulty per category. Until the bank is larger, lead with categories and languages, or
replace the count with something that does not shrink under scrutiny ("new questions every
week", "three languages, one bank").

**L5 · Heading level jump — P3 · FIX**

The only page in the application whose headings skip a level. Minor, and the fix is one
tag.

### Sign in — `/login` · Create account — `/register` · Forgot — `/forgot`

**A1 · Good as they stand — no action**

Labels are bound, `autocomplete` is present on identity and password fields, `required`
is set, errors land on the field (`id="identifier-error"`, `id="password-error"`), the
password hint states the 8-character rule before submission rather than after, and the
failed-login copy deliberately avoids revealing whether an account exists. This is the
most correct part of the product.

**A2 · Reset link expiry explains itself well — P3 · ENHANCE**

`/reset` without a valid token returns `410` with a real page rather than an error. The
`<title>` still says *Choose a new password*, which contradicts the body. Give the expired
state its own title.

---

## Player screens

### Dashboard — `/app`

**D1 · Category cards hide the counts the play screen shows — P2 · ADD**

*Choose a category* renders eight cards with name, description and colour — and no
question count, no difficulty spread, no progress. The play setup screen shows exactly
those counts on the same categories (8, 8, 8, 9 …). A player picks blind here and informed
one screen later. Put the count on the card, and — since the data already exists —
the player's own progress through it.

**D2 · A category description is truncated mid-word — P3 · FIX**

*Hadith* reads "Prophetic traditions and their narrat…". The grid gives each card one line.
Either shorten the source strings so all eight fit, or let the card wrap to two lines;
a clipped word reads as a bug, not a design.

**D3 · The honorific renders as an emoji — P2 · FIX**

The *Prophetic Biography* description shows "The life of Prophet Muhammad 🕋" — a Kaaba
emoji where the ﷺ honorific is intended. The Unicode ligature (U+FDFA) is falling back to
an emoji font. In an Islamic product this is not a typographic nicety; it is a correctness
issue. Either load a font that carries the ligature, or spell the honorific out.

**D4 · Five stat tiles leave an orphan on mobile — P3 · ENHANCE**

The two-column mobile grid puts Coins alone on a fifth row. Four tiles, or a 2×2 with the
fifth promoted into the profile header, reads tidier.

**D5 · "Today's round" does not say whether it was played — P2 · ENHANCE**

The card offers *See your result*, which implies it was. If it was not, the same card
should say *Play today's round*. One card, two states — only one is visible here.

### Play setup — `/play`

**P1 · You can ask for more questions than exist — P2 · ADD**

*Hard* holds 9 questions; the default length is 10. The template disables unreachable
lengths ([play.templ](../internal/views/play.templ) via `LengthUnavailable`), which
prevents the error — but the player sees options silently vanish from a select with no
explanation. Say it out loud: "Hard has 9 questions — the longest round is 9."

**P2 · Chip heights go ragged when names wrap — P3 · ENHANCE**

*Prophetic Biography* and *Stories of the Prophets* wrap to two lines while their row-mates
stay on one, so the row has uneven heights. A fixed min-height on the chip settles it.

**P3 · No sense of what a round costs — P3 · ADD**

Nothing tells the player how long 10 questions will take. An estimate next to the length
("about 5 minutes") is the single most common reason people abandon a quiz setup screen.

### Messages — `/messages`

**M1 · The desktop empty state wastes two-thirds of the screen — P2 · ENHANCE**

With no thread selected, the right pane shows a single grey sentence — *Pick a conversation
to start reading* — across roughly 70% of a 1440px viewport. Put the primary action there:
*New message*, recent activity, or unread counts. Right now the only way to start a thread
is a small unlabelled **+** at the top of the list.

**M2 · The search field uses an emoji as its icon — P3 · UPDATE**

🔍 is typed into the placeholder string rather than rendered as an icon in the field. It
inherits the placeholder's grey, cannot be positioned, and is read aloud as "magnifying
glass" before the actual hint.

**M3 · `autocomplete` missing on the people search — P3 · ADD**

The only identity-shaped input in the product without it.

### Settings — `/settings`

**S1 · Languages are labelled with national flags — P1 · UPDATE**

العربية carries 🇸🇦, English carries 🇬🇧, Français carries 🇫🇷. A language is not a country.
Arabic belongs to no single state, and for a product whose audience is the global ummah,
mapping it to one flag is a message nobody intended to send. English-as-UK has the same
problem for every American, Nigerian and Indian user. Drop the flags; the endonym alone
(العربية · English · Français) is the accepted pattern and needs no translation.

**S2 · The theme control never shows which theme is active — P2 · FIX**

[settings.templ:71-80](../internal/views/settings.templ#L71-L80) renders Light / Dark /
System as three `<button class="segment__item">` inside `role="group"`, with **no
`aria-current`, no `aria-pressed`, and no selected styling**. In the rendered page all
three look identical — the user cannot tell what is set, and a screen reader cannot
either. The Language control directly above it does this correctly with `aria-current`.
Two adjacent controls on one screen, two different answers. Use `role="radiogroup"` with
`aria-checked`, and mirror the Language chip's selected style.

**S3 · Appearance options use emoji, Language uses flags, the header uses SVG — P3 · UPDATE**

Three icon idioms within one screen plus its header. See **C1**.

---

## Admin screens

### Admin · Questions — `/admin/questions`

**Q1 · A smoke-test fixture is live in the question bank — P1 · REMOVE**

Row #9201: **"ZZ smoke fixture: which colour is the fourth planet…"**

Verified against the live database — this is not only visible to admins:

| field | value |
|---|---|
| `is_active` | `true` |
| category | 8 — *Manners & Ethics*, also active |
| difficulty | 1 (Easy) |
| translations | `en` only — 1 of 3 |

It is drawable in a real round. A player who picks *Manners & Ethics* on Easy can be asked
about the colour of the fourth planet, in English, inside an Arabic-first Islamic game.
Arabic and French players hit a question with no translation in their language.

This is the single most damaging thing in the product and it is one `DELETE` away. It also
belongs to the wider point that the smoke suite writes into the same database the app
serves — worth separating, so a test run can never again leave content behind.

**Q2 · The filter bar has no labels — P2 · ADD**

Search, category and difficulty carry no `<label>`, `aria-label` or `title`. The same
pattern repeats on **Support** and **Users** — three screens, nine controls. Visually the
placeholder carries the meaning; to a screen reader these are three unnamed inputs in a
row. Add visually-hidden labels.

**Q3 · Destructive action sits adjacent to a routine one — P2 · UPDATE**

Every row ends *Edit · Deactivate · Delete*, three similar buttons at similar weight.
Delete is irreversible and sits one target-width from the action an admin uses constantly.
Move Delete into an overflow menu, or give it a confirmation step and a distinct colour.
Across 65 rows this is 195 buttons, which is also most of the visual noise on the screen.

**Q4 · Truncated question text has no way to be read — P3 · ADD**

Rows clip at roughly 55 characters with an ellipsis and no `title` attribute, so the only
way to read a question is to open its edit form. Add a `title`, or expand the row on hover.

**Q5 · Column headers do not sort — P2 · ADD**

With 65 questions and a target of 2,400, sortable *Difficulty*, *Category* and *Languages*
columns stop being a nicety. The languages badge (1/3, 3/3) is the most useful signal on
the screen and cannot be sorted or filtered on — an admin cannot answer "what still needs
translating?" from here, which is the main reason to visit.

### Admin · Dashboard — `/admin`

**AD1 · The coverage table is a wall of red — P2 · UPDATE**

Every cell in *Question coverage* renders as a red-pink pill because every count (1–5) is
far below the stated target of 100. When everything is an alarm, nothing is. Show
`4 / 100` with a progress bar, and reserve the alert colour for categories that fall
behind the others — relative, not absolute.

**AD2 · The dashboard diagnoses but does not act — P2 · ADD**

It is the best screen in the admin surface for seeing what is missing, and it offers no
way to do anything about it. Link each weak cell to the question list filtered to that
category and difficulty, and put *Import questions* beside the coverage table.

**AD3 · The target is stated without being explained — P3 · ENHANCE**

"Target: 100 per difficulty, per category" sits in small grey text. It implies 2,400
questions against the current 65. Give it a tooltip explaining the reasoning, and show
overall progress toward it.

### Admin · navigation and tables on mobile

**AN1 · The sidebar becomes a scroll strip with no affordance — P2 · ENHANCE**

Under 860px, `.admin__nav` switches to a horizontal `overflow-x: auto` row
([app.css:2424-2432](../web/src/css/app.css)). At 390px only *Dashboard · Users · Support*
and half an icon are visible; the remaining eight destinations — including Questions,
where the work happens — are off-screen with no arrow, fade or scrollbar to say so. Add a
gradient fade at the edge, or collapse it into a select.

**AN2 · Tables scroll horizontally but nothing is pinned — P2 · ENHANCE**

Tables are wrapped in `.scroll-x`, so they do scroll rather than clip — correct, and better
than most. But with seven columns and three action buttons, a mobile admin scrolls a long
way and loses the question text, which is the only column that identifies the row. Pin the
first column, or switch to a card-per-row layout below 600px.

**AN3 · The player tab bar follows admins into the admin surface — P3 · UPDATE**

At mobile width the admin screens still show *Home · Play · Challenges · Messages · Friends*
fixed at the bottom. Two different information architectures occupy one screen. Either swap
the bar for the admin sections, or hide it under `/admin`.

**AN4 · The tab bar is translucent over scrolling content — P3 · ENHANCE**

`backdrop-filter: blur(14px)` over a 92% surface lets text underneath show through behind
the labels. The body correctly reserves space so nothing is permanently hidden
([app.css:2632](../web/src/css/app.css)) — but mid-scroll the labels sit on moving content
and lose contrast. Raise the surface opacity.

### Admin · Users, Support, Domains, Categories, Review, Rated, Integrity, Comments, Audit

**AX1 · Filter bars unlabelled on Users and Support — P2 · ADD** — see **Q2**.

**AX2 · All tables lack captions and header scopes — P2 · ADD** — see **C2**.

**AX3 · `/admin/rated` has the clearest title in the product — no action**

"Questions players marked down" says what it is in plain words, where most systems would
have written "Flagged content". Worth copying as the house style for the other titles.

**AX4 · `/challenges/new` carries the wrong title — P2 · FIX**

It renders `Set up your round · Sirāj`, the same title as `/play`. Two different tasks,
one name — in history, in tabs, and in a screen reader's announcement.

---

## Appendix A · Checked and found clean

So the next review need not repeat it:

- One `<h1>` per screen, no duplicates, 36 of 36.
- Every `<img>` has `alt`, 36 of 36.
- One unnamed control in the whole product, correctly `aria-hidden`.
- `autocomplete` on every identity and password field except people-search.
- `required` and field-level error ids on every form that validates.
- Viewport meta, skip link, meta description, `lang` and `dir`: all present on all screens.
- `aria-live` region present on every screen.
- RTL verified by rendering, not assumed — layout, header and numerals all mirror.
- 961 translation keys, every call site matched (`scripts/check-i18n.py`).

## Appendix B · Reproducing this review

```bash
# the stack
docker compose -f deploy/compose.yaml up -d

# a session (smoke-suite credentials, rotated by the suite itself)
curl -s -c jar "http://localhost:8080/login?lang=en" -o /dev/null
curl -s -b jar -c jar -X POST http://localhost:8080/login \
  -d "csrf_token=$(grep -oP 'csrf_token" value="\K[^"]+' <(curl -s -b jar http://localhost:8080/login))" \
  -d "identifier=aisha_t" -d "password=evenbettersecret2"

# render any screen at a chosen width
google-chrome --headless=new --disable-gpu --screenshot=out.png \
  --window-size=390,844 "http://localhost:8080/?lang=ar"
```

## Appendix C · Suggested order of work

1. **Q1** — delete the smoke fixture, and stop the suite writing into the served database.
2. **S1, L1, L2** — flags, landing CTAs, page title. Hours, not days, and all first-impression.
3. **S2, AX4, D3** — theme state, wrong title, the honorific.
4. **C2, Q2** — captions, scopes and filter labels; one shared partial covers most of it.
5. **D1, P1, AD1, AD2** — the information gaps, in that order.
6. **C1** — the icon migration. Largest, least urgent, most visible when done.
