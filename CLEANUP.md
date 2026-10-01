# Cleanup report — unused files & dead code

Generated 2026-09-30 against `master` (working tree, no commits yet).
Nothing in this report has been deleted. Tick a box, tell me, and I'll apply it.

Tools used: `golang.org/x/tools/cmd/deadcode` (reachability from `./cmd/...`),
`staticcheck -checks=U1000`, plus scripted cross-checks of i18n keys, CSS
classes, JS functions, SQL tables/columns, routes and env vars.

---

## 0. Headline

**The project is not slow because of dead code.** Measured on this machine:

| Step | Time |
|---|---|
| `go build ./...` | 1.5 s |
| `templ generate` | 0.35 s |
| `go test ./... -count=1` | 6.2 s |

Disk is 45 MB, of which **43 MB is `bin/`** (two compiled binaries, already
gitignored). Actual source is small and clean:

- 0 unused JS functions (out of 64)
- 0 unused DB tables (out of 31)
- 1 unused DB column (out of 248)
- 0 orphan scripts
- 0 orphan `_templ.go` files (26 `.templ` ↔ 26 generated)
- 0 unregistered HTTP handlers (111 of 112 registered; the 112th is `Routes` itself)
- 0 unused `go.mod` dependencies

The dead code that *does* exist is **~180 lines** — real, but not a performance
problem. The real cause of the slowness is in §8.

---

## 1. Dead Go functions — zero callers anywhere (safe to delete)

`deadcode` proves these are unreachable from `cmd/server` and `cmd/sirajctl`,
and grep confirms no test references either. Deleting them cannot break a build.

- [ ] [internal/handlers/paging.go:136-138](internal/handlers/paging.go#L136-L138) — `Paging.String` (3 lines)
- [ ] [internal/models/models.go:705-716](internal/models/models.go#L705-L716) — `AdminUser.Initials` (12 lines). `User`, `UserCard` and `Ticket` each have their own live `Initials()`; only the `AdminUser` one is dead.
- [ ] [internal/models/models.go:747](internal/models/models.go#L747) — `AdminQuestion.Complete` (1 line)
- [ ] [internal/models/models.go:1121-1123](internal/models/models.go#L1121-L1123) — `Ticket.IsOpen` (3 lines). `Stage()`/`NeedsYou()` replaced it.
- [ ] [internal/repository/comments.go:114-116](internal/repository/comments.go#L114-L116) — `Repo.RecentQuestionComments` (3 lines). `QuestionCommentsPage` replaced it.
- [ ] [internal/repository/question_sets.go:88-104](internal/repository/question_sets.go#L88-L104) — `Repo.RenameQuestionSet` (17 lines). No route ever calls it — renaming a set is not wired to the UI.
- [ ] [internal/service/uploads.go:60](internal/service/uploads.go#L60) — `Uploads.Limit` (1 line)

**Subtotal: 7 functions, ~40 lines.**

---

## 2. The legacy 1-v-1 duel layer (the one real cluster)

Migration `0013_multiplayer_challenges.sql` turned duels into N-player matches
and moved the data into `challenge_players`. The new implementation lives in
[internal/repository/matches.go](internal/repository/matches.go). The old
duel-shaped repo methods were left behind and are now unreachable:

| Dead (old duel) | Live replacement |
|---|---|
| [`Repo.CreateChallenge`](internal/repository/challenges.go#L54) :54-62 | [matches.go:35](internal/repository/matches.go#L35) INSERT |
| [`Repo.AttachChallengeSession`](internal/repository/challenges.go#L174) :168-190 | [matches.go:150](internal/repository/matches.go#L150) |
| [`Repo.SettleChallenge`](internal/repository/challenges.go#L194) :192-247 | [`RecordMatchScore`](internal/repository/matches.go#L172) matches.go:172 |
| [`Repo.PendingChallengeCount`](internal/repository/challenges.go#L144) :142-154 | superseded by the batched counts query |

- [ ] Delete all four (~105 lines). Three of them are kept compiling only by
      [internal/repository/challenges_test.go](internal/repository/challenges_test.go)
      and [internal/repository/admin_batch_test.go](internal/repository/admin_batch_test.go),
      so those tests come out with them.

⚠️ **Not included above, flagged for a decision:** the `challenges` table still
carries the old duel columns `challenger_id`, `opponent_id`,
`challenger_session_id`, `opponent_session_id`, `challenger_score`,
`opponent_score`, plus index `challenges_opponent_idx` and the
`CHECK (challenger_id <> opponent_id)` constraint
([0001_init.sql:141-158](internal/database/migrations/0001_init.sql#L141-L158)).
They are still **written** for compatibility — see the comment at
[matches.go:30](internal/repository/matches.go#L30) — and
[`ActiveChallengeBetween`](internal/repository/challenges.go#L258) still reads
them, so they are legacy-but-live, not dead. Retiring them is a real migration
(rewrite `ActiveChallengeBetween` against `challenge_players`, drop 6 columns +
1 index + 1 constraint, shrink `challengeColumns`/`scanChallenge` by 8 fields
and drop 2 JOINs from every challenge query). Say the word and I'll scope it.

---

## 3. Dead in production, kept alive only by tests

These have no production caller. The test is testing code nothing uses.
Either delete both, or leave them as documented API surface — your call.

- [ ] [internal/i18n/i18n.go:126](internal/i18n/i18n.go#L126) — `Printer.Locale` (test: [i18n_test.go:130](internal/i18n/i18n_test.go#L130))
- [ ] [internal/i18n/i18n.go:127](internal/i18n/i18n.go#L127) — `Printer.Dir` (test: [i18n_test.go:121](internal/i18n/i18n_test.go#L121))
- [ ] [internal/i18n/i18n.go:128](internal/i18n/i18n.go#L128) — `Printer.IsRTL` (test: [i18n_test.go:121](internal/i18n/i18n_test.go#L121), :125)
- [ ] [internal/views/context.go:44](internal/views/context.go#L44) — `Ctx.IsRTL`
- [ ] [internal/repository/admin.go:782-792](internal/repository/admin.go#L782-L792) — `Repo.ForgetPairVerdict` — "undo a duplicate verdict" was never wired to a route
- [ ] [internal/repository/social.go:210-216](internal/repository/social.go#L210-L216) — `Repo.IncomingRequestCount` — superseded by the batched counts query; the test only asserts it agrees with the batch
- [ ] [internal/service/mailer.go:77](internal/service/mailer.go#L77) — `Mailer.Enabled` — every caller uses `m.cfg.Enabled()` directly

**Recommendation:** keep the three i18n/`Ctx` RTL helpers (cheap, obviously
useful, RTL is a shipped feature). Delete `ForgetPairVerdict`,
`IncomingRequestCount` and `Mailer.Enabled` with their tests.

---

## 4. Dead translation keys

4 keys are genuinely unreferenced — × 3 locales = **12 entries to remove** from
`ar.json`, `en.json`, `fr.json`:

- [ ] `admin.import.tooLarge` — "That file is too large."
- [ ] `challenge.notYet` — "Not yet"
- [ ] `challenge.writeOwnHint` — "Only the people in this match ever see them."
- [ ] `sets.pickOne` — "Pick a set, or make one."

The other 28 keys a naïve scan flags are **false positives** — built at runtime
by key-builder functions, so never appear as literals. Do **not** delete these:

| Key family | Built by |
|---|---|
| `admin.role.*` | [`RoleLabelKey`](internal/models/models.go#L60) models.go:60 |
| `support.kind.*` / `status.*` / `priority.*` | models.go:1039-1041 |
| `support.stage.*` / `next.*` | models.go:1078-1079 |
| `notifications.kind.*` | [notifications.templ:72](internal/views/notifications.templ#L72) |
| `challenge.state.*` | [components.templ:458-466](internal/views/components.templ#L458-L466) |

(`challenge.sent` is also a false positive — used in
[internal/handlers/social.go](internal/handlers/social.go).)

---

## 5. Dead CSS

4 rules in [static/css/app.css](static/css/app.css) with no markup or JS
referencing them:

- [ ] `.row--top` — [line 286](static/css/app.css#L286)
- [ ] `.nav__toggle` — [line 418](static/css/app.css#L418)
- [ ] `.card--link` (+ `:hover`) — [lines 468-473](static/css/app.css#L468-L473)
- [ ] `.streak-flame` — [line 1293](static/css/app.css#L1293)

**False positives — keep:** `.avatar--md` (used at
[play.templ:114](internal/views/play.templ#L114)), `.avatar--sm/xs/lg/xl`
(built as `"avatar--" + size`), `.lb-rank--2/3` (built as `lb-rank--%d` at
[context.go:283](internal/views/context.go#L283)), `.ticket-stage--*` (built
from `Ticket.Stage()`), `.toast--error/success` (built as `"toast--" + kind` at
[app.js:77](static/js/app.js#L77)), and `.w3` (not a class at all — it's inside
the `www.w3.org` data-URI on line 587).

---

## 6. Small stuff

- [ ] **Unused DB column** — `message_hidden.hidden_at`
      ([0020_message_hidden.sql](internal/database/migrations/0020_message_hidden.sql)),
      never named in Go.
- [ ] **Stale doc comments** left behind by renames — they document a function
      that no longer follows them:
  - [internal/repository/comments.go:99](internal/repository/comments.go#L99) — documents `RecentQuestionComments`, sits above `CountQuestionComments`
  - [internal/service/social.go:160](internal/service/social.go#L160) — documents `CreateChallenge`, sits above `MatchOptions`/`CreateMatch`
  - [internal/repository/support.go:421](internal/repository/support.go#L421) — documents `StaffMembers`, sits above `StaffContacts`
  - [internal/repository/support.go:291](internal/repository/support.go#L291) — dangling sentence above `CloseTicketAsOwner`
- [x] **`go.mod` was untidy** — `github.com/anthropics/anthropic-sdk-go` was
      marked `// indirect` but is a direct import of
      [internal/service/translate.go:14](internal/service/translate.go#L14).
      **Already fixed** by `go mod tidy`; build verified. (This also added 4
      lines to `go.sum`.) It's the only change I made to your tree.
- [ ] **10 env vars are undocumented** in `.env.example` — not dead code, but a
      setup trap: `ANTHROPIC_API_KEY`, `ANTHROPIC_MODEL`, `BASE_URL`,
      `LIBRETRANSLATE_API_KEY`, `LIBRETRANSLATE_URL`, `SMTP_HOST`,
      `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM_EMAIL`, `SMTP_FROM_NAME`.

---

## 7. About "the project is very slow"

Build, codegen and tests are all fast (§0), so whatever is slow is **not** the
Go toolchain and **not** caused by the dead code above — removing every line in
this report will not make anything measurably faster.

Two candidates worth separating:

**a) Editor / gopls slowness.** 53.6k lines of Go, but **~30k of it is
generated** `*_templ.go` — and all 26 generated files are committed to git.
[play_templ.go](internal/views/play_templ.go) alone is 2,733 lines / 124 KB.
gopls indexes all of it, and every `templ generate` rewrites all 26 files.
Since [Makefile](Makefile) `build`/`run`/`test` and [run.sh](run.sh) all run
`templ generate` first, these are reproducible artifacts:

- [ ] Add `internal/views/*_templ.go` to [.gitignore](.gitignore) and
      `git rm --cached` them. Cuts ~1 MB and ~30k indexed lines from the repo,
      and stops every template edit from producing a 3,000-line diff.
      *Trade-off:* `go build` then requires the `templ` CLI, and plain
      `go install github.com/bilelzarai/siraj/cmd/server` stops working.

**b) Runtime slowness of the running app.** This is the real one — see §8.

- [ ] **Free 43 MB instantly, unrelated to any of the above:** `make clean`
      (removes `bin/server` 29 MB + `bin/sirajctl` 14 MB). They're regenerated
      by `make build` and are already gitignored.

---

## Suggested order

1. `make clean` — 43 MB back, zero risk.
2. §1 + §2 deletions — ~145 lines, provably unreachable.
3. §4 + §5 + §6 — translations, CSS, stale comments.
4. §3 — decide per item (I'd keep the RTL helpers).
5. §7a/§7b — separate decisions; §7b is where actual speed lives.

---

## 8. Why the running app is slow (verified)

A full pass over the repository, migrations, handlers and JS. Everything below
was re-checked against the source by hand. Ranked by what a user actually feels.

### Tier 1 — makes pages visibly crawl

**8.1 — Every avatar image triggers two unindexed full-table scans.**
[internal/repository/social.go:991-1003](internal/repository/social.go#L991-L1003) (`CanSeeAttachment`):

```sql
OR EXISTS (SELECT 1 FROM users    WHERE avatar_seed = 'photo:' || $1::text)
OR EXISTS (SELECT 1 FROM messages m JOIN conversations c … WHERE m.attachment_id = $1 …)
```

Neither `users.avatar_seed` nor `messages.attachment_id` is indexed — confirmed
against every `CREATE INDEX` in the migrations (`users` has only `xp`, two
trigram indexes, `role`, `status`, `country`, `last_seen_at`). Every
`/files/{id}` request ([messages.go:327-367](internal/handlers/messages.go#L327-L367))
runs this. A friends list of 25 photo avatars = 25 seq scans of `users`, each
plus a `SessionUser` lookup and a `Presence.Touch`. **This is the worst item in
the report.**

- [ ] `CREATE INDEX users_avatar_photo_idx ON users (avatar_seed) WHERE avatar_seed LIKE 'photo:%';`
- [ ] `CREATE INDEX messages_attachment_idx ON messages (attachment_id) WHERE attachment_id IS NOT NULL;`

**8.2 — N+1 on the challenges list, under a comment claiming the opposite.**
[internal/repository/challenges.go:123-128](internal/repository/challenges.go#L123-L128):

```go
// Players for the whole page in one pass rather than one query per match.
for _, c := range out {
    if c.Players, err = r.MatchPlayers(ctx, c.ID); err != nil {
```

That is exactly one query per match. `/challenges` requests 40 rows
([social.go:281-287](internal/handlers/social.go#L281-L287)) → **41 round
trips**; the dashboard requests 4 ([game.go:51](internal/handlers/game.go#L51))
→ 5.

- [ ] Rewrite [`MatchPlayers`](internal/repository/matches.go#L69) to take
      `challenge_id = ANY($1)` and group in Go. One query for the page.

**8.3 — The leaderboard evaluates a per-user aggregate for every registered user.**
[internal/repository/stats.go:142-156](internal/repository/stats.go#L142-L156):
a `LEFT JOIN LATERAL (SELECT … FROM game_sessions WHERE user_id = u.id …)` under
`ORDER BY u.xp DESC, u.games_won DESC, u.created_at ASC LIMIT 10`. The sort key
doesn't match `users_xp_idx` (`xp DESC` only,
[0001_init.sql:32](internal/database/migrations/0001_init.sql#L32)), so Postgres
materialises and sorts **all** users — running the lateral once each — before
applying `LIMIT 10`. Degrades linearly with signups no matter the page size.
[`MyRank`](internal/repository/stats.go#L186) then adds a second full count.

- [ ] Pre-limit in a CTE (pick the top N by the sort key first, join the
      accuracy aggregate to those N only), and add
      `CREATE INDEX users_rank_idx ON users (xp DESC, games_won DESC, created_at ASC);`

**8.4 — Starting a round sorts the entire question bank.**
[content.go:105-118](internal/repository/content.go#L105-L118) uses
`ORDER BY random() LIMIT $3` over `questions ⋈ question_translations`;
[content.go:137-149](internal/repository/content.go#L137-L149) uses
`ORDER BY md5($1 || ':' || q.id::text) LIMIT $2` for the daily. Both are a full
scan + full sort on the "Start playing" click
([game.go:73](internal/service/game.go#L73), `:387`). No index can help either.

- [ ] The daily set is identical for every player all day and is recomputed per
      request — cache it in memory keyed by date+locale. Cheapest real win here.
- [ ] For the random draw, sample ids by `TABLESAMPLE`/random-offset instead of
      sorting the bank.

**8.5 — `UserByEmail` defeats its own unique index.**
[internal/repository/users.go:70-72](internal/repository/users.go#L70-L72) —
`WHERE lower(email) = lower($1)`. But `email` is
[`citext NOT NULL UNIQUE`](internal/database/migrations/0001_init.sql#L13),
already case-insensitive. Wrapping it in `lower()` makes the unique index
unusable → seq scan of `users` on every login and forgot-password.

- [ ] One-character fix: `WHERE email = $1`.

### Tier 2 — per-request overhead on every page

**8.6 — `BadgeCounts` runs on every authenticated render, uncached.**
[handlers.go:136-146](internal/handlers/handlers.go#L136-L146) →
[social.go:869-889](internal/repository/social.go#L869-L889). Five correlated
`count(*)` subqueries in one round trip (good), but unconditional — it runs on
`NotFound`, `forbidden`, `serverError` ([handlers.go:225,231,239](internal/handlers/handlers.go#L225)),
on every `refuseWrite`, and on POST-redirect paths that render nothing. It is
recomputed again by `/api/counts`
([messages.go:726-748](internal/handlers/messages.go#L726-L748)) on every SSE event.

- [ ] Skip it on error/redirect paths; add a short per-user TTL cache.

**8.7 — Admin pages add three more uncached counts.**
[admin.go:45-57](internal/handlers/admin.go#L45-L57) (`adminCtx`) runs
`PendingReviewCount` + `TicketCounts` + `CountPoorlyRated` **on top of**
`BadgeCounts` — 4 count queries before any admin page starts.
[`CountPoorlyRated`](internal/repository/comments.go#L291) is a full
`GROUP BY question_id … HAVING` over `question_ratings ⋈ questions`; no index
can satisfy it.

**8.8 — The client polls every 4 seconds until SSE confirms itself.**
[app.js:1704-1715](static/js/app.js#L1704-L1715) — `FALLBACK_MS = 4000`, and
`schedule(FALLBACK_MS)` fires immediately on load, backing off to 60 s only
after a `siraj:stream {live:true}` event. Each poll of `/messages/{id}/poll`
([messages.go:559-618](internal/handlers/messages.go#L559-L618)) costs 3
queries. If a proxy buffers SSE or `EventSource` never opens, **it stays at 4 s
forever, per open tab.**

- [ ] Start at the slow interval and speed up only on a confirmed miss, or add
      exponential backoff when the stream never reports live.

### Tier 3 — other missing indexes

| Column | Queried at | Existing index |
|---|---|---|
| `users.avatar_seed` | [social.go:995](internal/repository/social.go#L995) | none |
| `messages.attachment_id` | [social.go:999](internal/repository/social.go#L999) | none |
| `game_answers.question_id` | [admin.go:477](internal/repository/admin.go#L477), [comments.go:182](internal/repository/comments.go#L182) (`HasAnsweredQuestion`, on the comment-thread path) | only `(session_id, position)` |
| `challenges.winner_id` | [challenges.go:249](internal/repository/challenges.go#L249) (`ChallengeWins` — every profile view **and** after every finished round) | only opponent/challenger |
| `messages.body` (`ILIKE '%…%'`) | [social.go:626](internal/repository/social.go#L626) | none — no trigram index on `body` |
| `notifications.read_at` | [social.go:884](internal/repository/social.go#L884) | `(user_id, created_at DESC)` only |

### Tier 4 — counts, pagination, unbounded queries

- [ ] [admin.go:697-713](internal/repository/admin.go#L697-L713) — the admin
      dashboard is **fifteen** `count(*)` subqueries, including
      `count(*) FROM game_answers` and `count(*) FROM messages`, the two
      fastest-growing tables. No cache.
- [ ] Every paginated screen runs its filter twice (count + page) with
      `LIMIT/OFFSET`: [games.go:239/263](internal/repository/games.go#L239),
      [admin.go:114/125](internal/repository/admin.go#L114),
      [admin.go:373/398](internal/repository/admin.go#L373),
      [comments.go:256/294](internal/repository/comments.go#L256). Page size is
      capped at 50 ([paging.go:14](internal/handlers/paging.go#L14)), which
      limits the damage.
- [ ] `AdminQuestionIDs` ([admin.go](internal/repository/admin.go)) has **no
      LIMIT at all** — materialises every matching id for bulk actions.
- [ ] [`ListSessions`](internal/repository/users.go#L224),
      `Conversations` and `Friends` ([social.go](internal/repository/social.go))
      are unbounded. `Conversations` additionally runs two `LATERAL`
      subqueries *per conversation*, each with a correlated `NOT EXISTS`
      against `message_hidden`.
- [ ] Uploads do **no image processing** — [internal/service/uploads.go](internal/service/uploads.go)
      has no resize, re-encode or thumbnail step. An 8 MB phone photo
      (`MAX_UPLOAD_MB` default 8, [config.go:87](internal/config/config.go#L87))
      is stored as-is and served as a 40 px avatar. A page-weight problem that
      compounds 8.1.

### Checked and clean — not findings

Templates are compiled in (no per-request parsing) · i18n catalogs load once at
`New()` ([i18n.go:52-70](internal/i18n/i18n.go#L52-L70)) · exactly one regexp,
package-level `MustCompile` ([auth.go:36](internal/service/auth.go#L36)) ·
bcrypt at `DefaultCost`, with a correct dummy-hash timing defence
([auth.go:195-197](internal/service/auth.go#L195-L197)) · asset hashing runs
once in `Routes()` · mail is sent via `SendAsync`, off the request path · the
connection pool **is** configured (`MaxConns=16`, `MinConns=2`, lifetime, idle
time — [database.go:29-32](internal/database/database.go#L29-L32)).

**The SSE hub is the best-engineered part of the codebase**
([social.go:474-513](internal/service/social.go#L474-L513)): buffered channels
(cap 8), non-blocking `Publish` with a `default:` arm, `Subscribe` returns an
unsubscribe closure that is correctly `defer`red
([messages.go:691](internal/handlers/messages.go#L691)), 25 s heartbeat with the
ticker stopped, and `Presence` throttles writes to one per user per minute and
sweeps its map. No leaks, no blocking.

One caveat on the pool: with only 16 connections, the Tier-1 sequential scans
will saturate it and queue requests long before Postgres itself struggles —
which is very likely what "very slow" feels like from the browser.

---

## If you only do three things

1. **Index `users.avatar_seed` and `messages.attachment_id`** (8.1) — two
   `CREATE INDEX` lines in a new migration. Biggest win by a wide margin.
2. **Batch `MatchPlayers`** (8.2) — turns 41 queries into 2 on `/challenges`.
3. **`WHERE email = $1`** (8.5) — one character deleted, removes a seq scan
   from every login.

All three are low-risk and independent. Want me to do them?
