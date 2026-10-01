# Done

Finished work, kept for the reasoning rather than the tick: why the third
import button was cut, why the smoke script stopped matching on id, what the
comparator was built instead of. The live list is in [TODO.md](TODO.md).

---

## 1. `[x]` Reference ids become links

Where a question id is printed, make it useful:

- **Hover** a reference id → show that question's prompt.
- **Click** it → open that question.

Appears in at least two places:
- the import report (`skip — looks like question 1010100023`)
- Content health → **Possible duplicates**, where both sides print an id

Built opening the **admin editor**, since every screen that prints a
reference is an admin screen. Say if you wanted the player view instead.

## 2. `[x]` Loading state on every button that waits

Any button whose action goes to the server should say so while it works:

- a spinner in the button
- the button disabled until it finishes, so a second press cannot fire

Covers: answer submission, bulk actions, import preview/apply, review
approve/reject, support replies, friend actions, challenge accept/decline.

Built freezing only the **pressed button**, not the whole form — the rest of
the page stays readable while it works. The button is disabled one tick after
submitting, never before: a disabled submit button is left out of the
submission, which would have turned "delete the selection" into no action.

## 3. `[x]` Star rating, 1–5, per question

Alongside the note a player can already leave after answering:

- a 1–5 star rating on the question
- questions averaging **2 or less** raise a **red alert** for the admin

Built with **3 ratings minimum** before an average counts as evidence, and the
alert on its own admin screen (**Rated poorly**) with a red badge on the nav
from every admin page. Both numbers are constants in one place —
`PoorRatingThreshold` and `MinRatingVotes` — so say the word and they change.

## 4. `[x]` CSV upload: stop skipping near-duplicates silently

Today the importer drops rows it thinks are near-duplicates. 300 rows in
`quran.csv` became 297 written, and the three it dropped were only reported in
small text. Some of what it calls duplicates are not:

- `#1020100063 In which month did the Battle of Badr take place?`
- `#1020300039 In which month did the Battle of Uhud take place?` — 84% "similar"

Wanted instead: **ask, don't decide.** For each suspected duplicate, offer:

- **Duplicate** — it is one, leave the row out
- **Upload anyway** — write the row despite the warning

Built **per row**, two buttons. A third — "Nothing" — was built first and
then cut: it left the row out exactly as Duplicate does, so it was a choice
whose difference the reader had to hunt for and never find.

A row nobody has answered is tagged **pending** and is not written, so
applying a file without reading the report can only ever import less than it
would have, never more.

## 5. `[x]` Compare the two questions instead of trusting the percentage

Both screens that raise a duplicate said only how alike two prompts are and
left the check to the reader: memorise one, open the other in a second tab,
read them against each other. With sixty pairs that is not a check anyone
performs.

Built as one panel used by both — **Import preview** and **Content health** —
every field of both questions, in **every language**, with the differing words
highlighted. Opened by anchor, closed by the back button, no script.

Content health now also lists **one language at a time** (🇸🇦 🇬🇧 🇫🇷 chips,
`?loc=`, which does not switch the interface). The same pair was appearing
once per language it is written in. The panel behind it still carries all
three, which is the point: a pair raised by French has to be decided with
Arabic and English in view.

The sweep was taking **10.9 s**. `%` was picking candidates at pg_trgm's
default 0.3 while the query kept only ≥ 0.55, so everything in between was
fetched, scored and thrown away. Setting the operator's threshold to the
query's own number: **0.9 s (ar) / 3.5 s (en) / 3.6 s (fr)**.

---

# Archived from TODO.md on 2026-09-29

Items 6–44: the audit, the speed review, the line-by-line pass, the
password-reset review, the mail review, the smoke run and your screenshots.
All closed. Kept for the reasoning.

## A — Access, abuse and data that grows forever

### 1. `[x]` 🔴 Blocking does not exist

Corrected 2026-09-28 by the line-by-line pass. The earlier note here said a
block failed to stop an existing conversation. It is worse than that: **nothing
in the codebase ever writes `status = 'blocked'`**. `/friends/{action}` accepts
request, accept, decline and remove — and nothing else
([social.go:77-89](internal/handlers/social.go#L77-L89)).

Everything downstream of it is already built and unreachable: the enum value
([0001_init.sql:167](internal/database/migrations/0001_init.sql#L167)),
`FriendBlocked` and `RelationBlocked`, the `blocked` branch in `SearchUsers`
([users.go:138](internal/repository/users.go#L138)), `Relation` returning it
([social.go:169](internal/repository/social.go#L169)), the red tag in
[social.templ:336](internal/views/social.templ#L336) and the
`messages.blockedNotice` string.

Decide which way to go, then do it in one direction:

- **Build it** — a `block` action, and then `Send` must re-check, because it
  only asks whether you are a member of the conversation
  ([social.go:84](internal/service/social.go#L84)); `OpenConversation` requires
  an accepted friendship so a block would stop *new* threads only.
- **Delete it** — drop the enum value, the constants, the branch and the tag.
  Dead UI that looks like a safety feature is worse than no feature.

**Done 2026-09-28.** Built rather than deleted, since the interface already promised it. `BlockUser` / `UnblockUser` / `IsBlockedBetween`, a block button wherever a relationship is shown, `Send` re-checking so an existing thread closes too, and friend requests refused in both directions. The enum value, the constants, the search branch, the relation switch and the red tag are all reachable now.

### 2. `[x]` 🔴 Only two endpoints are rate limited

`login` (8 per 10 min, [auth.go:89](internal/service/auth.go#L89)) and
password reset (10 per 15 min, [reset.go:38](internal/service/reset.go#L38)).
Nothing else is. Unlimited, from one session: **registration**, **question
comments**, **direct messages**, **support tickets and replies**, **ratings**,
**friend requests**.

Support ticket creation is the worst of them — it writes a row and emails an
admin. Fix: reuse `attemptLimiter` behind one middleware, per user id rather
than per IP for authenticated routes.

**Done 2026-09-28.** Per-kind budgets in `service.WriteLimits`, keyed on the account where there is one and on the address before sign-in. Registration 5/h, tickets 4/h, comments 20 per 10 min, messages 60/min, ratings 60 per 10 min, friend requests 30 per 10 min.

### 3. `[x]` 🟠 Notifications are never deleted

The janitor sweeps sessions, stale duels and the audit trail
([main.go:194-215](cmd/server/main.go#L194-L215)). `notifications` is not in
it, and seven places write into it. One active player is a few hundred rows a
month, forever.

Fix: a fourth sweep, read rows older than the retention window. Same shape as
`PurgeAuditOlderThan`.

**Done 2026-09-28.** `PurgeNotificationsOlderThan`, 90 days, in the janitor beside the audit sweep.

### 4. `[x]` 🟡 A player cannot close their own support ticket

Status is admin-only ([support.go:305](internal/handlers/support.go#L305)).
A player who solved their own problem can only reply "never mind", which
leaves the ticket in the queue and the badge lit.

---

**Done 2026-09-28.** `CloseTicketAsOwner` and a close button on the player's own thread. Scoped to the owner and to closing; triage stays with staff.

## B — Duplicate detection: three doors, three rules

### 5. `[x]` 🟠 One threshold, not three

| Door | Threshold | Behaviour |
|---|---|---|
| Import | **0.85** ([import.go:27](internal/service/import.go#L27)) | blocks the row |
| New question form | **0.70** ([admin.go:707](internal/handlers/admin.go#L707)) | warns once, second save goes through |
| Content health | **0.55** ([admin.go:1104](internal/handlers/admin.go#L1104)) | lists only |

Nobody chose these three numbers together. A 70% pair is a hard stop on one
screen, a shrug on another and invisible on the third. Fix: one constant per
*intent* (`BlockAbove`, `WarnAbove`, `ListAbove`) in one file, named so the
gap between them is a decision rather than an accident.

**Done 2026-09-28.** `BlockAbove` / `WarnAbove` / `ListAbove` in one block, named by what they cause. Every caller passes one.

### 6. `[x]` 🟠 The New question form has no comparator

It shows up to three near-matches as a list of prompts in a yellow box
([admin.templ, `AdminQuestionForm`](internal/views/admin.templ)) — a
percentage and no way to check it. It is the weakest door: one extra click
and the duplicate is in. The panel built for the import preview and Content
health already takes a `QuestionCompare`; this is wiring, not new work.

**Done 2026-09-28.** The form shows the same panel as the other two doors, with the unsaved draft as the left side.

### 7. `[x]` 🟠 The importer never compares a file against itself

Every row is checked against the **bank**
([import.go:370-380](internal/service/import.go#L370-L380)), never against
the other rows of the same upload. Two identical rows in one file both read
as `add` in the preview. On apply, the first is written and the second then
finds it and is reported `pending` — a row the admin was never asked about,
skipped with a reason that appears only after the fact.

**Done 2026-09-28.** `sameFileClashes` compares the upload against itself on the normalised prompt, and the report says which row of the file it repeats.

### 8. `[x]` 🟡 The import check stops at the first language

`SimilarPrompts(..., 1)` and `break` on the first clashing locale
([import.go:371-380](internal/service/import.go#L371-L380)). A row that
resembles one question in Arabic and a different one in English is reported
against whichever language came first — and map iteration order in Go is
random, so the same file can report different questions on two runs.

---

**Done 2026-09-28.** `findClashes` asks every language and keeps the strongest match, so the answer no longer depends on Go's map order.

## C — Pagination

### 9. `[x]` 🟠 No page-size control anywhere

Four constants, four files, nobody can change any of them from the screen:
history **20** ([account.go:14](internal/handlers/account.go#L14)), messages
**80** ([messages.go:20](internal/handlers/messages.go#L20)), notifications
**60** ([notifications.go:11](internal/handlers/notifications.go#L11)), every
admin list **40** ([admin.go:25](internal/handlers/admin.go#L25)).

Wanted: a **5 / 10 / 20 / 50** selector on every list, `?size=` with an
allowlist, remembered per user so it survives the next visit. One helper
reading page and size together, so no screen invents its own.

**Done 2026-09-28.** `handlers.Paging` and `views.Pager`: a 5/10/20/50 selector on every list, `?size=`, remembered in a cookie for a year.

### 10. `[x]` 🟠 Six lists have a hard cap and no pager

They do not paginate — they stop, and nothing on screen says so:

| Screen | Cap | |
|---|---|---|
| Rated poorly | 40 | [admin.go:1234](internal/handlers/admin.go#L1234) |
| Audit log | 200 | [admin.go:1192](internal/handlers/admin.go#L1192) |
| Comments | 200 | [admin.go:1142](internal/handlers/admin.go#L1142) |
| Notifications | 60 | [notifications.go:23](internal/handlers/notifications.go#L23) |
| Content health → duplicates | 60 | [admin.go:1104](internal/handlers/admin.go#L1104) |
| Messages in a thread | 80 | [messages.go:20](internal/handlers/messages.go#L20) |

The audit log is the one that matters: it is the record you consult *after*
something went wrong, and it silently ends at 200.

**Done 2026-09-28.** All six page now — rated, audit, comments, notifications, support, history — each saying "showing 21-40 of 96", so a list can no longer just stop.

### 11. `[x]` 🟡 Two pagers, one job

`adminPager` for admin screens, a hand-rolled prev/next in
[history.templ:55-65](internal/views/history.templ#L55-L65). Different
markup, different behaviour, same purpose. One component, and it takes the
size selector from item 9 with it.

---

**Done 2026-09-28.** One `PagerBar`. `adminPager` and the hand-rolled history pager are gone.

## D — Screens that list but cannot act

### 12. `[x]` 🟠 Rated poorly never empties

The only action on a flagged question is **Edit**
([`AdminRated`](internal/views/admin.templ)). There is no "reviewed, it is
fine" — so a question three players disliked and a moderator has read stays
on the list and keeps the nav badge red forever. The badge stops meaning
anything about a week in.

Fix: dismiss, recorded with who and when, and the list filters on it.

**Done 2026-09-28.** `ratings_reviewed_at` (migration 0011) and a **Reviewed** button. Dismissing settles the ratings that exist; a new rating brings the question back.

### 13. `[x]` 🟡 Content health issues are not actionable

`missing_locale`, `duplicate_choice`, `choice_count` are printed as text with
the question id. Every one of them is a link to the editor with the field to
fix, and none of them is a link.

---

**Done 2026-09-28.** Every issue's question id is a link into the editor.

## E — Code health

### 14. `[x]` 🟡 Two files are carrying whole screens

`internal/views/admin.templ` is **2055 lines**; `internal/handlers/admin.go`
is **1249**. Both are ten screens in a trench coat. Split by screen —
`admin_users`, `admin_questions`, `admin_import`, `admin_review`,
`admin_integrity`, `admin_support` — with nothing else changing in the same
diff.

**Done 2026-09-28.** `handlers/admin.go` 1249 lines is now six files of 147-441; `views/admin.templ` 2055 is seven of 134-512, with the comparison panel in its own file since three screens share it. Nothing but the split in the diff — same tests, same i18n count.

### 15. `[x]` 🟡 `internal/repository` has no tests

Not one file. Every SQL string in the project is verified only by a human
loading a page — including `NearDuplicates`, which was quietly doing ten
seconds of work per call until it was measured by hand this week. A
`testcontainers` or throwaway-schema harness would pay for itself on the
first query.

**Done 2026-09-28.** Harness built: every run creates its own database from the migrations and drops it again, and skips when no server is reachable. Fourteen tests on it so far.

### 16. `[x]` 🟡 Dead code

- `Auth.Secure()` ([auth.go:301](internal/service/auth.go#L301)) — no caller.
- Sweep for others once the splits in item 14 make them visible.

Not dead, checked: the nine `notifications.kind.*` catalog keys are built by
prefix, and `CreateUser` / `CreateUserWithRole` are used as function values.

---

**Done 2026-09-28.** `Auth.Secure`, `AwardXP` (folded into the finish transaction) and `adminPager` (replaced by `PagerBar`) are gone. A sweep after the split found nothing else unreferenced.

## F — Speed

Measured 2026-09-28 against the dev database (2 098 questions, 6 294
translations) and against the server's own request log, which has real
browsing in it. Numbers below are `EXPLAIN (ANALYZE)` execution times and
logged `ms=`, not estimates.

**What the log says.** Excluding `/events`, which is a stream and is supposed
to be long:

| Path | requests | avg | worst |
|---|---|---|---|
| `/admin/integrity` | 4 | 8 524 ms | **27 720 ms** |
| `/admin/questions/import` | 19 | 1 555 ms | **9 986 ms** |
| everything else (GET) | ~200 | 2–30 ms | 175 ms |

Two screens account for all of it. The rest of the app renders in single-digit
milliseconds and does not need work.

**Already fast, do not chase:** assets are 13.6 KB of CSS and 10 KB of JS over
the wire, served `immutable` with a content hash; compression is on for HTML,
CSS, JS and JSON; static files are served before the session middleware, so
they never touch the database; the pool is 16 connections with sane lifetimes;
presence writes are throttled in memory to one per window, not one per
request.

### 17. `[x]` 🟡 The duplicate sweep is still seconds, not milliseconds

3.5 s for English after this week's fix, and it runs on every visit to
Content health. It is 2 098 questions. At ten thousand it is unusable.

Fix: compute it on a schedule into a `question_duplicates` table and read
that, or cache the result for a few minutes. The sweep is a report, not a
live query — it does not need to be accurate to the second.

**Done 2026-09-28.** `service.Duplicates` holds each language's sweep for ten minutes, a second visitor arriving mid-sweep waits for the first rather than starting another, saving a question drops it, and the card says when it was swept — a list that is not live should say so.

### 18. `[x]` 🟠 `SimilarPrompts` has the bug that made the sweep take ten seconds

`%` picks its candidates at pg_trgm's default **0.3** while the caller keeps
only ≥ 0.85 (import) or ≥ 0.70 (form)
([admin.go:870](internal/repository/admin.go#L870)). Measured on one call:

```
default 0.3   index returns 566 rows, 550 thrown away on recheck   11.6 ms
matched 0.85  index returns 1 row                                   1.3 ms
```

Nine times cheaper, and it is called once per row per language of an import.
Same one-line shape as the fix already in `NearDuplicates`: set the threshold
to the number the caller filters by.

**Done 2026-09-28.** The threshold is the caller's, applied at the index and in the WHERE. Measured 11.6 ms to 1.3 ms.

### 19. `[x]` 🔴 The import preview is one trigram query per row per language

[import.go:372](internal/service/import.go#L372), inside the row loop, inside
a loop over languages. A 300-row trilingual file is **900** probes at 11.6 ms
— about 10 s, which is exactly the 9 986 ms in the log. Then the *same work
runs again* on apply, because preview and commit share one code path and
neither remembers the other's verdicts.

Three fixes, in order of how much they buy:

1. Match the threshold (item 18) — 900 × 11.6 ms becomes 900 × 1.3 ms.
2. Ask once per language instead of once per row: one query joining the
   file's prompts against the bank, 3 queries for the whole upload.
3. Carry the preview's verdicts in the stash so apply does not re-derive
   them. It already holds the file; this is the same idea one step further.

**Done 2026-09-28.** `BestMatches` answers a whole file in one query per language. Measured: 300 prompts in 229 ms, against 3.0 ms each one at a time.

### 20. `[x]` 🟠 The import asks "does this id exist?" one row at a time

[import.go:349](internal/service/import.go#L349) calls `QuestionDraftByID`
per record — two queries each, and it loads every translation of the question
to answer a yes/no. `SELECT id FROM questions WHERE id = ANY($1)` answers it
for the whole file in one.

**Done 2026-09-28.** `ExistingQuestionIDs` — one query for the file, replacing two per row.

### 21. `[x]` 🟡 Comparison panels on the import path still load one draft at a time

[compare.go:70](internal/service/compare.go#L70). The Content-health path was
batched into `QuestionDraftsByIDs` this week; the import path was not, so up
to 50 flagged rows are 100 queries. Same call, already written.

**Done 2026-09-28.** `QuestionDraftsByIDs` on the import path too.

### 22. `[x]` 🟡 Five count queries before every authenticated page, eight before an admin page

`viewCtx` runs unread messages, pending duels, friend requests, support and
notifications one after another
([handlers.go:106-141](internal/handlers/handlers.go#L106-L141)), and
`adminCtx` adds review, support queue and poorly-rated on top
([admin.go:52-66](internal/handlers/admin.go#L52-L66)).

Each query is genuinely cheap — 0.13 ms and 0.52 ms measured — and a local
admin page still renders in 2–13 ms, so **this is not a problem today**. It
becomes one on a managed database, where nine round trips is nine network
hops before the handler starts. Fix when the database stops being a unix
socket: one `SELECT` with the counts as subqueries. `UnreadCounts`
([handlers.go](internal/handlers/handlers.go)) has the same shape and takes
the same fix.

**Done 2026-09-28.** `BadgeCounts` — one query for all five, and `/api/counts` uses it too.

### 23. `[x]` 🟡 An open message thread polls every 4 s while SSE is already connected

[app.js:954](static/js/app.js#L954) plus the `/events` stream. Two live
channels for one feature: the SSE hub already publishes `message.new` to the
recipient. Either drop the poll and render from the event, or keep the poll
as the fallback for when the stream is down and stop it while the stream is
up.

---

**Done 2026-09-28.** The poll backs off to 60 s while the stream is connected and returns to 4 s if it drops; `message.new` on the stream triggers an immediate fetch.

## G — Found by the line-by-line pass (2026-09-28)

Every Go file in `internal/` read in full, plus the migrations, the templates
and `app.js`. Each item below was traced to the code that causes it, not
inferred from a smell.

### 24. `[x]` ~~You cannot log in with the email you registered with~~ — WRONG, withdrawn

Written from reading `UserByIdentifier` (`WHERE username = $1 OR email = $1`)
and comparing it with `UserByEmail` (`lower(email) = lower($1)`). The inference
was wrong: **both columns are `citext`**
([0001_init.sql:12-13](internal/database/migrations/0001_init.sql#L12-L13)), so
every comparison on them is already case-insensitive — including the two UNIQUE
constraints, so two accounts cannot differ only by case either. The `lower()`
in `UserByEmail` is redundant, not a mismatch.

Caught by writing the test first: `TestUserByIdentifierMatchesEmailWhateverTheCase`
passed before there was a fix to make. The test is kept — it pins the behaviour
to something a future `citext` → `text` migration would break loudly.

### 25. `[x]` 🟠 A declined or expired duel can be accepted

`AcceptChallenge` checks that the viewer is part of the duel and that their
side has not been played, and never looks at `ch.Status`
([social.go:302-337](internal/handlers/social.go#L302-L337)).
`AttachChallengeSession` then sets `status = 'accepted'` with no guard of its
own ([challenges.go](internal/repository/challenges.go)). A back button, a
stale tab, or a re-POST revives a duel the other player declined — or one the
janitor expired.

**Done 2026-09-28.** `AttachChallengeSession` refuses anything not pending or accepted and unexpired; the handler checks first, so no round is created only to be thrown away.

### 26. `[x]` 🟠 The history review 500s on a question with no Arabic translation

`GameAnswers` falls back exactly twice: the viewer's locale, then `'ar'`
([games.go:247-249](internal/repository/games.go#L247-L249)). Both can be
missing — a question approved only in English, reviewed by a player reading
French — and `COALESCE` then yields NULL into a `string`, which is a scan
error, which is a 500 on the whole page.

`Question()` does not have this bug: it carries a third fallback for exactly
this reason ([content.go](internal/repository/content.go)). Content health
already lists the questions that are missing a language, so the rows that
trigger it are known to exist. Fix: the same three-way fallback, or
`COALESCE(..., '')` and a visible "not available in this language".

**Done 2026-09-28.** A third fallback — whatever language the question does have — plus empty defaults. The test reproduced the exact scan error before the fix.

### 27. `[x]` 🟠 Deleting your account asks for your username, not your password

`DeleteAccount` compares what is typed against `c.User.Username`
([account.go](internal/handlers/account.go)) — a string that is on the screen,
in the URL bar and in the page title. A borrowed session is enough to destroy
the account and everything cascading from it. The admin-side delete has the
same shape but is at least performed by someone other than the victim.

Fix: require the current password, as `ChangePassword` already does.

**Done 2026-09-28.** The current password is required. The username stays as the deliberate step that stops a mis-click.

### 28. `[x]` 🟠 A moderator can delete the whole question bank in two presses

`/admin/questions/bulk` sits under `RequireModerator`
([router.go](internal/handlers/router.go)). With `scope=filter` and an empty
search, `AdminQuestionIDs` returns **every** id, and delete cascades into
`game_answers` — every player's history with it
([admin.go:352-430](internal/handlers/admin.go#L352-L430)). The only brake is
a second press carrying `confirm=delete`.

Managing *users* requires `RequireAdmin`. Destroying all content and all
history does not. Fix: put bulk delete behind `RequireAdmin`, or cap the
filter scope, or both.

**Done 2026-09-28.** Bulk question actions are behind `RequireAdmin`.

### 29. `[x]` 🟡 Suspension is enforced in exactly one place

The suspend action does the right thing — it revokes the account's sessions on
the spot ([admin.go:239-243](internal/handlers/admin.go#L239-L243)) — so
suspension does take effect immediately today. But `IsSuspended` is checked
only inside `Login` ([auth.go:212](internal/service/auth.go#L212)); neither
`Session` nor `RequireAuth` looks at it. Any other route to a suspended
account — a direct `UPDATE`, a bulk tool, a future admin screen that forgets
the second call — leaves it fully working until the cookie expires. One check
in `RequireAuth` makes the property true by construction.

**Done 2026-09-28.** `RequireAuth` turns away a suspended account and ends its session, so the property holds for the request rather than for whichever path did the suspending.

### 30. `[x]` 🟡 Finishing a round is two writes with no transaction around them

`FinishGame` then `AwardXP` ([game.go:236-243](internal/service/game.go#L236-L243)).
A failure between them leaves the round closed and the XP unpaid, and `Finish`
is idempotent by returning the stored outcome — so the second attempt reports
success and never retries the award. The player has no way to ask for it
again.

**Done 2026-09-28.** Closing the round and crediting it are one transaction, and a second finish pays nothing.

### 31. `[x]` 🟡 Starting the daily silently abandons the round in progress

`Start` abandons any active round ([game.go:60-66](internal/service/game.go#L60-L66)),
and `PlayedDaily` counts abandoned rounds as played
([content.go](internal/repository/content.go)). Press the daily with a solo
round open and quit: the solo round is gone *and* the daily is spent. Ask
first, or let the daily coexist.

**Done 2026-09-28.** The daily refuses while a round is open instead of abandoning it and spending the attempt.

### 32. `[x]` 🟡 The poorly-rated list shows unreviewed machine translations

Its `question_translations` join omits `AND NOT t.needs_review`
([comments.go:215-220](internal/repository/comments.go#L215-L220)), unlike
every player-facing query. Nothing reaches a player, so the standing rule
holds — but the admin judging a question is reading text the review gate has
not passed, with nothing on screen saying so.

---

**Done 2026-09-28.** `NOT needs_review` on both joins.

## H — Password reset

The flow itself is correct and was verified end to end short of redeeming a
token: 256-bit token, only its SHA-256 stored, single use enforced in the
write, 45-minute expiry, reissue kills outstanding links, redeem sets the
password and drops every session in one transaction, and an unknown address is
answered identically — confirmed live against the running server, which
created no row. These two are what it is missing, not what it gets wrong.

### 33. `[x]` 🟠 The password-reset flow has no tests

`internal/service` has tests for auth, game, import, mailer, translate and
compare. It has none for `reset.go` — the one flow where a mistake hands over
an account. Single use, expiry, supersede-on-reissue and "an unknown address
is answered identically" are four cheap tests against behaviour that is
currently protected only by having been read carefully.

**Done 2026-09-28.** Four tests: single use, expiry, supersede-on-reissue, every session ended.

### 34. `[x]` 🟡 `password_resets` is never swept

The janitor sweeps sessions, duels and audit
([main.go:196-220](cmd/server/main.go#L196-L220)) — not this table. Migration
0005 ships `password_resets_expires_idx` with the comment "sweeping expired
rows is cheap with this index", and nothing sweeps. Used and expired tokens
are precisely what should not be kept. Same fix as item 3, same loop.

---

**Done 2026-09-28.** `PurgeSpentPasswordResets` in the same sweep.

