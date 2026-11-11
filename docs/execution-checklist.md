# Sirāj — execution checklist

Persistent backlog for the master prompt (v3.1). One row per task; work resumes from
here across sessions.

Status: Pending · In Progress · Blocked · Completed · Validated

## Architecture decisions taken

**AD-1 · Hierarchy strategy: adjacency list + materialized `ltree` path.**
`nodes.parent_id` is the source of truth for structure — one column to write, a real
foreign key, and a trigger that refuses a cycle. `path`, `depth` and `root_id` are
derived from it by the same trigger. Subtree reads dominate this table (navigating a
tree, scoping a challenge to "node + descendants", counting content under a branch),
and `path <@ ancestor` answers them from a GiST index instead of a recursive CTE per
request. `ltree` ships with PostgreSQL, so this costs no application complexity, and
carries 65 535 labels — no depth limit. Trade-off: a move rewrites the paths of the
moved subtree; moves are rare and the rewrite is one statement.

**AD-2 · v1 is migrated beside the old tables, not replaced in one step.**
Migration 20 copies every domain to a root node and every category to a depth-1 node,
and gives `domains`, `categories` and `questions` a `node_id` pointing at the result.
The v1 tables keep working, so the ~28 Go files and 15 query files that read them can
be cut over one at a time and reviewed, and migration 20 can be rolled back without
losing data. The duplication is deliberate and temporary; CUT-1…CUT-4 below remove it.

**AD-3 · Ownership is a column, not a table.** `nodes.owner_id` NULL means the
platform. Sharing and publishing never write it (§7).

**AD-4 · Extensions are a reference on the extension, never a change to the official
row.** `nodes.extends_id` lives on the player's node; the official record is untouched
(§6.2). `CHECK (extends_id IS NULL OR owner_id IS NOT NULL)` keeps the platform from
extending itself.

## Discovery — the gap between the repository and the master prompt

The repository is a complete, working v1-shaped product: fixed `domains → categories →
questions`, solo/live/duel/challenge play, progress, social, messages, an eleven-screen
admin console, and a 132-operation JSON API. What the master prompt adds is a universal
hierarchy, player-created structure, resource-level permissions, and content types
beyond questions. Measured coupling to the fixed two levels at the time of discovery:
`domain_id` in 15 Go files / 7 query files / 3 templates, `category_id` in 13 / 8 / 3,
and 10 foreign keys across 7 migrations.

| ID | Module | Issue | Root cause | Required change | Layers | Priority | Depends on | Changes made | Tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| HIER-1 | db | No universal hierarchy: depth fixed at two by schema | v1 modelled domains and categories as separate tables | `nodes` + `node_translations`, ltree path, cycle prevention, v1 data migrated | migration | P0 | — | Migration 20; triggers `nodes_place`, `nodes_move_subtree` | `internal/db/nodes_test.go` — 5 tests: depth 10, subtree move, cycle refused, sibling slug, extension owner | **Validated** |
| HIER-2 | nodes | No service over the universal tree | AD-2 transition | `internal/modules/nodes`: children (paged), breadcrumb, subtree stats, search, create, update, move, archive, restore — each scoped by viewer | queries, service | P0 | HIER-1 | `internal/db/queries/nodes.sql`, `internal/modules/nodes/service.go` | `service_test.go` — 8 tests incl. §25 #3–#7 | **Validated** |
| HIER-3 | admin | Admin cannot create a node below depth 2 | A04/A05 are domain and category screens | `/admin/tree`: breadcrumb, paged children, create, publish, archive, move | templ, handler, i18n, css | P0 | HIER-2 | `tree.templ`, `web_tree.go`, 29 strings ×3 languages | Ten levels built through the real screen over HTTP | **Validated** |
| CUT-1 | games | Question selection scoped by `category_id` only | v1 shape | `PickQuestions`/`CountAvailableQuestions` take a node and a scope; `games.Scope` carries `NodeID`/`NodeScope`; wired into the web form, the games API and the challenges API | queries, service, api, contract | P0 | HIER-2 | `games.sql`, `games/service.go`, `games/api.go`, `challenges/api.go`, `openapi.yaml` | `nodescope_test.go` — 8 scope cases + the follow trigger; verified over HTTP on a 3-level branch | **Validated** |
| CUT-2 | progress | Mastery keyed on `category_id` | v1 shape | Migration 25: `node_id` on `user_category_stats`, backfilled, kept true by a trigger | migration | P1 | CUT-1 | Migration 25 | `TestMasteryAndSetQuestionsFollowTheirCategory` | **Validated** |
| CUT-3 | sets | Player sets cannot hang on the tree | v1 shape | Migration 25: `node_id` on `question_sets` (new capability) and on `set_questions`, backfilled and triggered | migration | P1 | CUT-1 | Migration 25 | Same test | **Validated** |
| INT-1 | db | `questions.node_id` was filled by migration 20 but no write path set it | A column added without a writer | Trigger keeps `node_id` in step with `category_id` for every caller, plus a category that moves carries its questions | migration | P0 | HIER-1 | Migration 21 | `TestQuestionTakesItsCategoryNode` | **Validated** |
| BRIDGE-1 | db | The bank and the universal model were separate | AD-2 transition | Migration 26 mirrors every question into `content_items` and keeps it mirrored; migration 27 makes the v1 catalog create nodes so the tree never stops being the whole picture | migration | P0 | CONT-1 | Migrations 26, 27 | `TestQuestionsMirrorIntoContent`; rooms suite proved the gap | **Validated** |
| PLAY-1 | content | The engine could not read the universal model | Selection lived on `questions` | `PickContent`/`CountPlayableContent` + `Service.Pick`/`Available`: playable kinds only, §10.2 scope, shares honoured, answers never dealt with the question | queries, service | P0 | BRIDGE-1 | `content/service.go` | 2 tests: scope + playability, private branches never dealt | **Validated** |
| CUT-4a | games | A round could only name a v1 question | `game_questions.question_id` only | Migration 28: `item_id` on `game_questions`, backfilled, paired in both directions by a trigger; the loosened CHECK lets a round name an item alone | migration | P0 | PLAY-1 | Migration 28 | `TestGameRoundsPairBothIDs`; a real game played over the API | **Validated** |
| CUT-4b | games | The engine could not deal content born in the new model | Deal and display both read `questions` | Node-scoped games deal through `PickContent`; `GameQuestionAt` gained a third union branch reading `content_item_translations` with theming off the node. Both moved together — either alone deals unrenderable questions | queries, service | P1 | CUT-4a | `games/service.go` `pickDealt`, `games.sql` | `TestNodeScopedGameDealsContentItems`; played over the API: dealt, rendered, graded, answer never leaked | **Validated** |
| CUT-4c | db | Drop `domains`/`categories` | AD-2 transition ends | Move the remaining readers (admin A04/A05, `user_domains.domain_id`), then the migration | migration, admin, player | P2 | CUT-4b | — | — | Pending |
| OWN-1 | nodes | Players cannot create nodes | No ownership model in v1 | `/library`: the player's own branches, create, archive, paginated; slug derived from the name and de-duplicated in the service | handler, templ, i18n | P0 | HIER-2 | `nodes/web.go`, `pages/library.templ`, 23 strings ×3 | HTTP tests + a branch built as a real guest | **Validated** |
| OWN-2 | nodes | Extensions of official nodes unbuilt | §6.2 | `POST /nodes/:id/extend`; `LibraryRoots` lists extensions beside own roots so they stay reachable | queries, service, handler, templ | P0 | OWN-1 | `LibraryRoots`, `Service.Library`, extend handler | Extension created over HTTP; official node verified untouched | **Validated** |
| PERM-1 | perm | Access rules spread across modules | v1 has platform roles only | `internal/modules/perm`: eight §7 actions, ordered share levels, `Can`/`Ensure`; the hierarchy delegates to it | service | P0 | OWN-1 | `perm/perm.go`, `nodes/service.go` now asks it | `perm_test.go` — 7 tests | **Validated** |
| PERM-2 | nodes | No shares or invitations | §7 | Migration 22: `node_shares` + `node_shared_level()` reaching the whole branch; grant, revoke, list; the four node visibility clauses honour shares and public branches | migration, queries, service | P1 | PERM-1 | Migration 22, 5 share queries | §25 #9 covered: share, raise, revoke, branch reach | **Validated** |
| CONT-1 | content | Only questions exist | v1 scope | Migration 23: `content_types` registry + universal `content_items` with jsonb fields per language; service over `perm`; written and listed in `/library` | migration, queries, service, handler, templ, i18n | P1 | HIER-2 | `content/service.go`, `content.sql`, library content section | `service_test.go` — 6 tests incl. a `recipe` type nobody coded | **Validated** |
| CONT-2 | content | Only single-choice-of-four | v1 scope | Migration 24: `answer_kind` on the type + `answer` jsonb on the item; `Correct()` per kind; multi/true-false/text/number live, matching & ordering are one case each | migration, service | P1 | CONT-1 | `content/answer.go`, `Service.Check` | 8 tests: exact-set multi, Arabic diacritics, Arabic-Indic digits, tolerance, unknown kind never right | **Validated** |
| MP-1 | challenges | Challenge scope is a category list | v1 shape | `node_id` + `node_scope` on the create body and in the contract | api, contract | P1 | CUT-1 | `challenges/api.go`, `openapi.yaml` | Covered by CUT-1's scope cases | **Validated** |
| MP-2 | games | An edit to a question changed a match in progress | Rounds read live content | Migration 29: `content_items.version` bumped on edit, `content_item_versions` frozen on deal, `game_questions.item_version` recording exactly what was dealt; the display and grading paths read the snapshot first | migration, queries | P1 | MP-1 | Migration 29, `games.sql` branch 3 | 2 tests + live: every question rewritten and the right answer moved mid-match, player unaffected, graded on the frozen answer | **Validated** |
| RT-1 | live | Seven screens re-fetched themselves every 2–3 s while an event stream sat unused beside them | The stream existed; nothing listened | Duels now announce pairing and settlement; every 2–3 s poll became `sj:live:<topic> from:body` with a 15 s backstop for a dropped stream or a browser without EventSource | service, templ, js | P1 | — | `duel/service.go`, duel/rooms/game templates | Stream observed delivering `event: rooms` the instant a player readied | **Validated** |
| RT-2 | live | The transport is SSE, not a WebSocket | — | **Deliberately not done.** The hub carries one-directional signals; SSE already gives push with browser-managed reconnect. A socket would add a dependency for no behaviour this product has. Revisit when something needs client→server realtime (presence, typing, live cursors) | — | P3 | — | — | — | **Won't do (documented)** |
| SMS-1 | auth | No SMS gateway: codes are written to the log | `internal/platform/sms` has no provider | Implement `Sender`; sandbox in dev | service, config | P0 | provider credentials | — | — | **Blocked** — no provider account. `.env.example` and CLAUDE.md record that an empty `SMTP_ADDR` and no gateway means nobody can sign in. |
| ADM-1 | admin | CSV import lacks the A/R/E/S shortcuts and per-user domains panel | Unbuilt | Finish A07 tools | templ, js | P2 | — | Review queue shortcuts done (A/E/R/S + Undo) | — | In Progress |
| ADM-2a | admin | Comments used the generic table | A comment is writing, not a row of fields | `comments.templ`: cards with the quoted question, language badge, `dir="auto"` body, per-state actions; flagged tinted. Tabs, search and language filter all page correctly | templ, handler, queries, migration, i18n | P1 | — | Migration 30 (comment language), `comments.templ`, `ModerateComment` extended | Each tab returns its own set; Keep moved a card from Needs action to Resolved | **Validated** |
| ADM-2b | admin | Rated poorly said a question scored 2.1, not why | The generic table has no room for a reason | `rated.templ`: three KPI cards over a 7/30/90-day window, the thumbs split bar, reason chips with counts, Fix / Send to review; the segmented filter is built from the reasons players actually picked | templ, handler, queries, i18n | P1 | — | `RatingSummary`, `RatedPoorlyDetailed`, `RatingReasons` | Filter and range both exercised live; count card and list share one query so they cannot disagree | **Validated** |
| PERM-2b | nodes | Sharing worked in the service and was unreachable | No screen | `/library/:id/share`: find somebody, pick a level, see who has access, revoke. The search excludes people who already have access, and the page itself is the permission check — only somebody who may manage access sees the list | templ, handler, i18n | P1 | PERM-2 | `pages.Share`, `nodes/web.go` | Granted and revoked live; `TestSharedBranchesAreVisibleToTheirGuests` covers the listing queries honouring a share | **Validated** |
| QA-1 | tests | §25 scenarios 10, 13, 14 uncovered | Features were unbuilt when the list was written | `tests/scenarios_test.go` against the real router | tests | P1 | — | Three scenarios added | All pass; two caught real bugs (SNAP-1, PAGE-1) | **Validated** |
| SNAP-1 | games | Editing a v1 bank question changed a game in progress | MP-2 wired the snapshot into the new-model branch only; the bank's branch still read live text | The v1 branch of `GameQuestionAt` reads the frozen version first, for wording, answers, points and what counts as right | queries | P0 | MP-2 | `games.sql` branch 1 | `TestFinishedGamesSurviveContentEdits` — it failed before the fix | **Validated** |
| PAGE-1 | messages | The inbox was capped at 100 with no offset and no total | A cap is not a pager | `MyConversations` gained an offset, `CountInbox` the total, the endpoint a `page` block; contract updated | queries, service, api, contract | P1 | — | `inbox.go`, `messages/api.go`, `openapi.yaml` | Covered by scenario 14 | **Validated** |
| ANS-1 | games | A dealt round came back with one answer instead of four | Answers are frozen in two shapes — jsonb array from the mirrored bank, pipe string from the new model — and the reader assumed one | Migration 31: `content_answer_list()` reads either; both branches call it | migration, queries | P0 | SNAP-1 | Migration 31 | The games suite failed on the regression and passes now | **Validated** |

## Done

- **HIER-1** — migration 20, five tests, v1 data migrated and verified on the
  development database: 6 domains → 6 roots, 23 categories → 23 depth-1 nodes,
  87 translations, every domain, category and question linked to its node.
- **HIER-2** — the nodes service. Every read states who is asking: official and
  published is public, a player sees their own, and anything else answers *not found*
  rather than *forbidden*, so a private branch does not announce that it exists.
  `writable()` is the single place that decides a write, which is where "a player
  never edits official content" lives.
- **HIER-3** — `/admin/tree`. Children arrive one level at a time and paged, so
  opening a branch costs the same whether it holds three nodes or thirty thousand.
  Verified by building a domain and ten levels under it through the real screen.
- **INT-1** — migration 21. Migration 20 added `questions.node_id` and backfilled it,
  but every write path still set only `category_id`, so the next question created
  would have been invisible to the engine. A trigger keeps the two in step for every
  caller — console, CSV import, player sets, a hand-written statement — and goes away
  with CUT-4.
- **QA-1** — the three remaining §25 scenarios, written against the real router. Two
  of them failed, which is the point of writing them:
  **SNAP-1** — MP-2's snapshot only covered content born in the new model. Editing a
  question from the v1 bank *still* changed a game in progress, which is most games.
  I had noted the v1 branch as riskier and deferred it; the scenario found it.
  **PAGE-1** — the inbox was capped at a hundred with no offset and no total. A cap is
  not a pager: conversation 101 was unreachable.
  **ANS-1** — fixing SNAP-1 then broke the games suite: answers are frozen in two
  shapes, a jsonb array from the mirrored bank and a pipe string from the new model,
  and my reader assumed one. Both are already in frozen history and neither can be
  rewritten, so one function reads either and both branches call it.
- **PERM-2b** — sharing became reachable. The service and the schema could do it
  since migration 22; there was no screen, which made it a feature that did not exist
  from a player's side. The page is its own permission check — reading who a branch is
  open to requires being allowed to manage it — and the search quietly drops anybody
  who already has access, because they belong in the list below with a way to take it
  back rather than in a list of people to invite.
- **RT-1** — the polls are gone. An event stream with backoff and reconnect was
  already running, and seven screens ignored it to re-fetch themselves every two or
  three seconds: the duel search, the duel score bar, the room lobby, the room board,
  the arena and the next-question panel. They listen now, with a fifteen-second poll
  left as a backstop for a dropped stream rather than as the mechanism. Duels had no
  publisher at all — a pairing and a settlement now announce themselves to both
  players.
- **ADM-2b** — Rated poorly rebuilt. The old table said a question scored 2.1 out of
  five; it did not say that a quarter of the people who played it thought the answer
  was wrong, nor what they thought was wrong with it. The screen now leads with the
  split and the reasons. The "below threshold" card and the list read the same query,
  so the number and the rows cannot disagree, and the reason filter is built from what
  players have picked rather than a list in the code — a reason the product does not
  offer shows its raw value rather than a missing key.
- **ADM-2a** — Comments rebuilt as cards. The tabs hid a real bug: **Resolved
  returned every visible comment**, including everything in Needs action, because it
  was defined as a status rather than as "reported and settled". Comments also gained
  a language (migration 30) so a moderator can filter by what was written rather than
  guess from the text, and the body carries `dir="auto"` so Arabic reads correctly
  inside a left-to-right console. `ModerateComment` was extended rather than
  duplicated — Keep, Resolve, Reopen and Suspend join the hide/show it already had.
- **MP-2** — a match no longer changes under the people playing it. Version-on-edit,
  freeze-on-deal: an item nobody plays costs nothing, one played a thousand times is
  stored once per version. Proved live — the author rewrote every question and moved
  the right answer mid-match; the player still saw what they were dealt and was graded
  on the answer that was right when it was dealt. Deleting the source does not erase
  the round it was played in.
  The snapshot being *stored* was not enough: it had to be *read*, and the first run
  showed the engine still serving live text. Both halves now hold.
- **CUT-4b** — the universal model is playable. A game scoped to a node deals from
  `content_items`, and the display path gained a branch that renders an item with no
  v1 question behind it — its text from `content_item_translations`, its theming from
  the node, so content ten levels down still looks like the domain it lives in. The
  two halves moved together on purpose: dealing before rendering would have dealt
  questions nobody could read. Verified on five items that exist **only** in the new
  model: dealt, rendered with their answers, graded right, correct answer never sent.
- **CUT-4a** — a game round now names both the v1 question and the content item it
  stands for, paired in either direction by a trigger. That is what lets the engine be
  moved one query at a time: a half-switched deal cannot leave rows only half the code
  can read. Proved on live traffic — a game played through the API dealt a question,
  graded an answer, **did not send the correct answer with the question**, and every
  dealt row carried both ids.
- **BRIDGE-1 / PLAY-1** — the two models became one picture. Every question is
  mirrored into `content_items` (184 items, 524 texts, nothing lost) and stays
  mirrored whichever path writes it. The engine can now select over the universal
  model with the §10.2 scope, dealing only playable kinds and never another player's
  private branch — and never sending the correct answer with the question.
- **CONT-2** — a question is not one shape. `answer_kind` on the content type decides
  how an answer is compared, so a new shape is a row plus one case in the checker and
  never a change to the engine. Typed answers forgive case, spacing, Arabic diacritics
  and the tatweel; numbers read Arabic-Indic digits and accept a tolerance. Multiple
  choice demands exactly the right set — three of four when three are right is not the
  same as knowing which three. An unimplemented kind is never silently correct.
- **CUT-2 / CUT-3** — mastery and a player's sets reach the tree. Same pattern as
  migration 21: add, backfill, and hold it true with a trigger so no write path can
  leave the two columns disagreeing while both exist.
- **CONT-1** — content of any kind, hanging on a node at any depth. What every kind
  shares is a column; what one kind has is a jsonb field described by a row in
  `content_types`. Proved by inventing a `recipe` type in the test: it is offered,
  validated against the fields the registry declares, stored and read back — with no
  branch for it anywhere in the code. A player wrote a flashcard onto a branch through
  the real screen.
- **PERM-1 / PERM-2** — one authority on who may do what. The hierarchy's `writable()`
  now asks it instead of deciding for itself, which immediately tightened two real
  rules: an unpublished official node is invisible rather than merely uneditable, and
  a refusal on something invisible answers not-found rather than forbidden. Shares
  reach the whole branch below the node they name, and the most generous share wins
  when a branch is shared twice.
- **OWN-1 / OWN-2** — `/library`. A player builds their own branches and extends an
  official node without touching it; the official row was checked untouched after an
  extension. Extensions are listed beside the player's own roots, because an extension
  lives deep in the official tree and would otherwise be unreachable once created.
- **CUT-1 / MP-1** — the engine plays from any node, with the scope §10.2 requires
  stated rather than assumed: `direct`, `subtree`, `both`. Verified over the real API
  on a three-level branch with five labelled questions per level: `direct` on the root
  drew only ROOT, `subtree` drew MID and LEAF, `both` drew all three.
- **FE-1 (§16, §18, §19)** — a sweep of every component and page against the frontend
  rules, which turned up four classes of defect rather than one:
  - *Failures were silent.* htmx only handled 401; a 500, a dropped connection or a
    timeout changed nothing on screen. `main.ts` now reports all three, preferring the
    server's own `error.message` and falling back to a translated line carried in a
    `meta` tag. Every control that starts a request is marked `aria-busy`, dimmed and
    disabled until it settles, so a slow button cannot be pressed twice.
  - *Three toasts never appeared.* "Copied", "file too big" and "no microphone" were
    dispatched on `window` while the listener sits on `document.body`; an event on
    `window` does not reach it. They all go through `window.sjToast` now.
  - *The avatar rule existed eleven times.* The palette was copied into eleven files
    and the initials rule into nine, in two variants that disagreed — the duel and
    question screens drew an empty circle where every other screen drew "?".
    `internal/platform/avatar` is now the one source, with a test; the page reads it
    too, which is allowed because platform is not a module.
  - *Numbers and colours were duplicated into the view.* The composer carried its own
    10 MB cap beside the upload handler's, and seven templates froze the light-mode
    brand hex, so those tiles stayed light-gold in dark mode. Both now read the one
    source: `MaxUpload` through the page data, `var(--brand)` through the token.
  Also: `dir="auto"` on every place a player's own words are drawn (messages and their
  quotes, comments, set and question titles, the question of the day, support), three
  unlabelled controls named, the admin console given the live region its toasts need,
  and `recorder.ts` deleted — no template mounted it.
- Pagination audited across all eleven admin screens; four were broken (domains and
  categories had no `LIMIT` at all, content health truncated at 20 silently, the
  support inbox could not reach page 2). All fixed and verified.
- Audit log and Review queue rebuilt to the approved designs.

## Blockers

| ID | Blocker | Why | Workaround in place |
|---|---|---|---|
| SMS-1 | No SMS provider account | Phone sign-in cannot deliver a code | Codes are logged; the address tab is the way in. Documented in `.env.example` and CLAUDE.md. |
