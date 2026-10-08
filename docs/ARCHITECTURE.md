# Sirāj — Architecture

What must be true about this system. Where this file states a rule, that rule
settles the argument.

## Three files, three roles, nothing said twice

| File | Owns | Never contains |
|---|---|---|
| **ARCHITECTURE.md** | The shape: principles, domain and its invariants, layers, routes, the directory layout, conventions, guarantees, decisions, risks | A version, a command, an environment variable, a code draft, a work item |
| [STACK.md](STACK.md) | The technology: every piece, what it is, what it does here, where it ends — with the versions, commands, variables, containers, release and gates | A domain rule, an invariant, a work item |
| [TODO.md](TODO.md) | The work: one prompt per branch, in order, each with the files to touch, the code to write and the checks that close it | Anything already true of the system |

**A fact lives in exactly one file.** The other two name it and link to it. A
number repeated in two files is a defect: one of them will go stale.

**§6 is the layout to build toward**, not the working tree. §6.5 names every
gap and the prompt that closes it.

**Scale.** 135 routes · 37 tables / 36 migrations · 32 templates / 172
components · 961 translation keys ×3 languages · 337 tests / 63 files. Counted
from the tree, not estimated. Nothing in this line is specified-but-unbuilt:
every table, route and migration named here exists. Code
size, dependencies and client inventory are [STACK.md §2](STACK.md); the work
and its sizing are [TODO.md](TODO.md).

### Where to ask

| Question | Go to |
|---|---|
| Where does this code go? | §3 for the layer, §6 for the directory |
| How do I add a page, a query, a migration? | §7.3 |
| What must be true of the bank, the round, the taxonomy? | §2 |
| What does the interface owe a player? | §8 |
| Is this change finished? | §13 |
| Why is it like this? | §11, then the comments in the file |
| What do I run, with what, and how is it configured? | [STACK.md](STACK.md) |
| **What is left to do, and in what order?** | **[TODO.md](TODO.md)** |

---

## 1. Principles

1. **Nothing fabricated.** No placeholder data, no invented copy, no sample users. A screen with nothing to show says so.
2. **The server owns the truth.** The page is correct before any script runs.
3. **Nothing unreviewed reaches a player.** Machine and imported text is held. Enforced in the query layer, not in a screen.
4. **The narrow case is the real case.** Arabic, RTL, 320 px, slow connection, signed out.
5. **Destroying is harder than creating.** Anything cascading needs a role, a confirmation and a trail.
6. **Say why in the code.** Comments explain the decision and what went wrong before, never the mechanism.
7. **Nothing structural is hardcoded.** A subject area, a category, a locale and a limit are rows and configuration, never constants in Go. An admin extends the product without a deploy.

---

## 2. Domain

Six areas. Each owns its tables; one area reaches another only through the
repository. Every invariant below was verified against the schema, except those
marked *(specified)* — those are what prompt 6a–6d add, and their enforcement
column names the migration or branch that brings them.

### 2.1 Identity — users, sessions, password_resets, request_keys

| Invariant | Enforced by |
|---|---|
| Role is player, moderator or admin | Database enum |
| The last admin cannot be demoted or suspended | Transaction with a row lock |
| A suspended account is locked out twice: sessions revoked, then refused per request | Service + middleware |
| A guest is a real expiring account, not a session flag | Lifetime column + expiry index |
| One guest per device key | Partial unique index |
| The first admin is created outside the web interface | CLI only |
| A repeated submission does its work once | Request-key table |

**Guests have full parity.** Every screen a registered player reaches, a guest
reaches — or is shown account creation rather than a door that refuses them. A
dedicated suite enforces it.

### 2.2 Content — domains and categories, questions, their translations, sets, comments, ratings, imports

| Invariant | Enforced by |
|---|---|
| Unreviewed text is never drawn | Three queries sharing one filter set |
| Arabic is the fallback every language resolves to | Query-level coalesce chain |
| A retired category keeps its questions | Active flag, not deletion |
| A category holding questions cannot be deleted | Count inside the deleting transaction |
| One comment per question per player | Partial unique index |
| An import is idempotent on its own identifiers | Upsert |
| A category belongs to exactly one domain, and a domain has no parent | Two tables and one foreign key, never a self-reference (D10) |
| A retired domain withdraws its categories' questions from play | The filter set, exactly as a retired category does — and the count and the draw are asserted together |
| A domain holding categories cannot be deleted | `RESTRICT` on the key — the database refuses it; the confirmation and the empty-target count arrive with the screens at 6d |
| No domain is special in code — Islamic is a row, not a constant | 0034 inserts it; nothing branches on a slug (principle 7) |

**The filter set is the asset:** active question · translated into the asked
language · past review · **active category** · and, from prompt 6b, **active
domain**. It appears in the availability count, the round draw and the daily
draw, and every condition must agree across all three — a count that disagrees
with the draw is worse than no count. **Five conditions since prompt 6b**
(D14); a new read path copies whichever set is current, never a subset.

**The taxonomy is two levels deep in the schema and the draw, and one in the
interface until prompts 6d–6f.** All categories are Siraj and a question belongs to exactly one, so there is
nowhere to say Football and Qur'an are different *kinds* of subject. Above them
goes the domain: an admin creates subject areas — Siraj, Sport, History — and
files categories under exactly one each. **Every domain sits on one level and
every category within a domain sits on one level**; there is no third level, and
no domain has a parent. Deletion is refused at both links while anything hangs
below.

### 2.3 Play — game_sessions, game_answers, badges

| Invariant | Enforced by |
|---|---|
| At most one round in progress per player | Partial unique index |
| One answer per **position** — not per question, since a round may draw one twice | Unique on session + position |
| One round per player per match | Partial unique index |
| The clock is the server's | Timestamps written server-side |
| The daily round is the same for everyone, for one day | Deterministic ordering from the date, stored nowhere |
| A round the bank cannot fill cannot be started | Availability, surfaced before the button |
| A round drawn from a whole domain records which domain | `game_sessions.domain_id`, `challenges.domain_id`, both nullable (D13) |

### 2.4 Social — friendships, challenges, conversations, messages, attachments, notifications

| Invariant | Enforced by |
|---|---|
| A match has a minimum size, and a team side its own | Service, mirrored in the form, agreement tested |
| Everyone in a match answers the same questions | Fixed at start |
| One room per player, one live match per player, one host per match | Three partial unique indexes |
| A withdrawn message leaves a trace, not a gap | Hidden flag |
| Attachments outlive the container | Volume, metadata in the database |

### 2.5 Support — tickets, messages, canned replies

Staff are reached by mail, not only a badge nobody is looking at. A staff note
is never shown to the player. A ticket waiting on us is visibly distinct.

### 2.6 Administration — admin_audit, plus write access to everything above

| Invariant | Enforced by |
|---|---|
| Moderators write content; only admins destroy it | Route-level role gates |
| Every privileged action is recorded with actor, target, address | Audit helper on each mutating path |
| A failed audit write reports but never blocks | Helper logs and continues |
| The area answers "not found", not "forbidden" | Role middleware, deliberately |
| Only an admin reshapes the taxonomy; a moderator writes inside it | All six `/admin/domains` routes behind **admin**, categories stay **moderator** (D11) |

### 2.7 The 36 tables, by area

| Area | Tables |
|---|---|
| Identity | `users` `sessions` `password_resets` `request_keys` `api_keys` |
| Content | `domains` `domain_translations` `categories` `category_translations` `questions` `question_translations` `question_sets` `question_set_items` `question_comments` `question_ratings` `question_duplicate_verdicts` `question_imports` |
| Play | `game_sessions` `game_answers` `badges` `badge_translations` `user_badges` |
| Social | `friendships` `challenges` `challenge_players` `conversations` `conversation_members` `conversation_state` `messages` `message_hidden` `attachments` `notifications` |
| Support | `support_tickets` `support_messages` `support_canned_replies` |
| Administration | `admin_audit` |
| Infrastructure | `schema_migrations` |

`api_keys` joins Identity, and is the one credential here that is not a
browser session: only its hash is stored, and the plaintext exists for the
length of one CLI command.

Plus one database per test process, built and dropped by the harness — §10, and
the leak in §12.

---

## 3. Layers

Dependencies point one way only — downward.

| Layer | Owns | May depend on |
|---|---|---|
| Views | Markup, copy localisation, the client contract | Models, i18n |
| Handlers | HTTP: routing, gates, decoding, status codes | Services, Repository, Views, Models, i18n, Config |
| Services | Rules that are neither storage nor HTTP | Repository, Models, Config |
| Repository | Every SQL statement and transaction boundary | Models |
| Models | Types, invariants, shared constants | **Nothing** |

- **Models imports nothing.** One import into it turns five clean layers into a cycle.
- **No SQL outside Repository** — the filter set is written once, and a second query path is a second place to forget it.
- **No HTTP outside Handlers** — a service that knows about a request cannot be reused by the CLI or the API.

**Where new code goes:** a page → Views + Handlers (route, keys ×3, walk-test
entry) · a play rule → Services (test without HTTP) · a query → Repository (the
filter set) · a shared constant → Models (a test that both sides agree) · an
admin screen → role gate + audit entry + nav entry · client behaviour → a
server-rendered fallback first.

---

## 4. Request path

Request id → peer address (only with declared proxy hops) → logger → recovery →
security headers → compression → **fork**.

The fork: `/static/*` to the asset handler, `/healthz` to the probe,
`/api/v1/*` to the bearer-key group — all three before the session layer. Every
other path continues: session → seat → cross-site token → auth, then role →
handler.

**The order is load-bearing.** The logger precedes recovery, or the panic
handler cannot see the status recorder. The peer address is rewritten only when
the operator declares how many proxies are in front, because the rate limiters
key on it and a header anyone can set must not choose who gets throttled.
Assets are served before the session layer so a stylesheet never touches the
database.

**Two prefixes, two kinds of credential.** `/ui/*` is the interface fetching
for itself — the people picker, the unread counts — inside the session chain,
behind the cookie and the cross-site token. `/api/v1/*` is mounted above that
chain with a bearer key and reads no cookie. They were one prefix once, which
is a way for a surface to be reached with the wrong credential; a test now
asserts neither answers the other's.

---

## 5. Module map

What to open when. 135 routes, 28 handler files, 20 repository files, 14
services, 32 templates.

### 5.1 Routes, by area

| Prefix | Routes | Gate |
|---|---|---|
| `/` `/login` `/register` `/forgot` `/reset` `/guest` | 11 | public |
| `/app` `/profile` `/history` `/settings` `/notifications` `/logout` `/questions/{id}/comments` | 18 | authenticated |
| `/play/*` | 12 | authenticated · the round, the clock, the seat |
| `/challenges/*` | 9 | authenticated · matches, duels, teams |
| `/messages/*` | 17 | authenticated · threads, rooms, attachments, receipts |
| `/friends/*` `/players/*` `/u/{username}` `/leaderboard` | 7 | authenticated |
| `/my/questions/*` | 8 | authenticated · player-authored sets |
| `/support/*` | 6 | authenticated |
| `/admin/*` | 40 | **moderator**, with `/users` `/audit`, the two bulk deletes and all six `/admin/domains` routes behind **admin** (D11) |
| `/static/*` `/healthz` `/api/v1/*` | 3 | outside the session chain — the first two open, the third behind a bearer key |
| `/events` `/files/{id}` `/ui/people` `/ui/counts` | 4 | inside the session chain, authenticated |

**Counted from `router.go`:** every method-and-path registration, the static
handler included. The six `/admin/domains` routes — list, new, edit, save,
delete, action — mirror the category ones exactly, and are registered with
`{id}/{action}` last or it swallows the named routes above it.

### 5.2 Services — rules that are neither storage nor HTTP

| File | Owns |
|---|---|
| `auth` | Sessions, credentials, cross-site tokens |
| `game` | Rounds, scoring, progression, the daily draw |
| `social` | Friendships, matches, notifications |
| `players` | Local players on one device, and their sweep |
| `import` | Spreadsheet and document ingestion, per-row verdicts |
| `importstash` | Holding an import between preview and commit |
| `duplicates` | Near-duplicate detection over prompts |
| `compare` | Field-by-field comparison of two questions |
| `textdiff` | The word-level difference the comparison renders |
| `translate` | Machine translation behind one interface, including `none` |
| `mailer` | Outbound mail; logs instead of sending when unconfigured |
| `reset` | Password-reset tokens and their single use |
| `uploads` | Attachment acceptance, typing, limits |
| `writelimit` | Throttling on the paths worth throttling |

### 5.3 Repository — one file per noun, never per screen

`content` (the filter set lives here) · `categories` · `games` · `matches` ·
`match_questions` · `challenges` · `threads` (conversations and their messages) ·
`social` · `users` · `players` · `question_sets` · `comments` · `support` ·
`stats` · `admin` · `requestkeys` · `domains` (mirroring `categories` one level up) · `repository`
(the pool and the shared sentinels).

### 5.4 Handlers — one file per screen family

`auth` `account` `reset` `game` `hotseat` `players` `social` `threads`
`messages` `comments` `notifications` `question_sets` `support` `people`
`paging` `idempotency` `assets` `middleware` `router` `handlers` (the struct
every screen hangs off), plus admin: `admin` `overview` `questions` `import`
`review` `users` `categories` `domains`.

**The four largest are worth knowing:** `social` (1 200 lines), `messages`
(1 038), `game` (674), `support` (565). They are large because each is one
screen family with many routes, not because they mix concerns.

### 5.5 Views

31 template files, one per screen family, plus the shared set: `layout` (the
document shell and both chrome variants), `components` (field, empty state,
error, avatar primitives), `icons`, `paging`, `errors`.

---

## 6. Repository layout — the target

**This is the layout to build toward, not a description of today.** Where the
tree currently differs, §6.5 says so. Every move here is already a decision
(D5, and the tidy-up in [TODO.md](TODO.md) prompts 10b and 10c); nothing below
is new opinion.

### 6.1 The tree

```
siraj/
│
├── api/                          ← NEW · the published contract, not code
│   └── openapi.yaml                  the v1 REST surface, versioned with it
│
├── cmd/                          ← one directory per binary, wiring only
│   ├── server/main.go                config → database → services → router
│   └── sirajctl/main.go              seed · promote · apikey · stats
│
├── deploy/                       ← NEW · everything that describes a deployment
│   ├── Dockerfile                    three stages: assets → compile → runtime
│   ├── compose.yaml                  the clean-clone stack
│   └── compose.prod.yaml             the production overlay
│
├── docs/                         ← NEW · everything long-form except the README
│   ├── ARCHITECTURE.md               this file
│   ├── STACK.md                      the technology
│   ├── TODO.md                       the work, as prompts
│   ├── DEPLOY.md                     operator runbook
│   └── api.md                        the endpoint contract, prose
│
├── internal/                     ← the application. Nothing here is importable
│   │                                 from outside the module, by design
│   ├── assets/                   ← resolves a source entry to its built
│   │   └── manifest.go               filename; misses cleanly when unbuilt
│   │
│   ├── config/config.go              every environment variable — STACK.md §5
│   │
│   ├── database/
│   │   ├── database.go               connect, migrate on boot, seed loader
│   │   ├── migrations/*.sql          forward-only, numbered, embedded
│   │   └── seed/questions.json       the reviewed bundled bank
│   │
│   ├── handlers/                 ← HTTP only. One file per screen family
│   │   ├── router.go                 the whole request tree, §4
│   │   ├── middleware.go             session · seat · CSRF · roles · headers
│   │   ├── api/                  ← NEW · the public surface, mounted above
│   │   │   ├── v1.go                 the session layer so no cookie is read
│   │   │   └── keys.go               bearer credential, hashed at rest
│   │   └── admin_*.go                questions · categories · domains · users …
│   │
│   ├── i18n/
│   │   ├── i18n.go                   negotiation, fallback chain
│   │   └── locales/{ar,en,fr}.json   904 keys, complete in all three
│   │
│   ├── models/                   ← imports nothing from this project
│   ├── repository/               ← every SQL statement. One file per noun
│   ├── service/                  ← rules that are neither storage nor HTTP
│   └── views/                    ← *.templ + committed *_templ.go
│
├── scripts/
│   ├── smoke.sh                      end-to-end HTTP walk
│   ├── check-i18n.py                 catalogue completeness
│   └── reset-test-data.sh
│
├── static/                       ← what is actually served at /static/*
│   ├── dist/                     ← BUILD OUTPUT · gitignored, embedded
│   ├── img/favicon.svg
│   └── emoji.json                    fetched by the picker, not built
│
├── web/                          ← frontend SOURCES. Never served
│   ├── public/boot.js            ← copied verbatim, never bundled: a built
│   │                                 entry is a module, and a module script is
│   │                                 deferred — this one must block
│   └── src/
│       ├── css/app.css               the one stylesheet
│       ├── js/                       app · alpine entries, and helpers.js
│       ├── components/           ← one Alpine component per file
│       └── modules/              ← the ones that stay vanilla
│
├── bin/                              build output · gitignored
├── data/uploads/                     development uploads · gitignored
│
├── assets.go                     ← the embed. Must be at the root: go:embed
│                                     cannot reach above its own directory
├── go.mod · go.sum
├── package.json · package-lock.json · vite.config.js
├── Makefile · run.sh
├── README.md · CLAUDE.md
└── .gitignore · .dockerignore · .env.example
```

The client file inventory — which component goes in which directory, and why —
is [STACK.md §2.3](STACK.md).

### 6.2 Every directory, and the rule that governs it

| Directory | Holds | Rule |
|---|---|---|
| `api/` | The machine-readable contract | Changes here are a version decision. The file is published, not internal |
| `cmd/` | One `main.go` per binary | Wiring only. A `cmd` file that contains a rule has put it in the wrong place |
| `deploy/` | Dockerfile, both compose files | Describes a deployment, never carries a credential value |
| `docs/` | Everything long-form | The README stays at the root and answers *what is this, how do I run it*. Everything else is one click away |
| `internal/` | The application | Not importable from outside the module. The compiler enforces the boundary |
| `internal/assets/` | The build-manifest resolver | A missing manifest is not an error — it is the dev server or an unbuilt tree |
| `internal/handlers/api/` | The public REST surface | Mounted **above** the session layer: no cookie read, no cross-site token |
| `internal/repository/` | Every SQL statement | One file per **noun**, never per screen. The filter set lives in `content.go` and is copied, never re-derived |
| `scripts/` | Operator and CI helpers | Anything a person runs by hand that is longer than one line |
| `static/` | What is served | Only built output and unbuilt assets. **No source file is ever reachable over HTTP** |
| `web/src/` | Frontend sources | Never served. One component per file, named for the attribute the markup uses to summon it |
| `bin/`, `data/` | Build output, development uploads | Both gitignored. `bin/` is the **only** home for a binary |

### 6.3 What moves, and why

| From | To | Why |
|---|---|---|
| `static/css/app.css`, `static/js/*.js` | `web/src/`, and `boot.js` to `web/public/` | ✅ **Done.** Served and source stopped being one directory; an unbundled source file is no longer a public URL |
| *(new)* | `static/dist/` | ✅ **Done.** Build output, content-hashed, embedded, gitignored |
| `ARCHITECTURE.md` `STACK.md` `TODO.md` `DEPLOY.md` | `docs/` | The root should answer *what is this and how do I run it*, in one screen |
| `Dockerfile` `docker-compose.yml` `docker-compose.prod.yml` | `deploy/Dockerfile` `deploy/compose.yaml` `deploy/compose.prod.yaml` | Three root entries become one directory. `.dockerignore` stays at the root, where the build context is |
| *(new)* | `api/openapi.yaml` | A contract nobody can read is one nobody can consume |
| *(new)* | `internal/assets/` ✅, `internal/handlers/api/` | Two capabilities that do not belong in an existing package |
| `./sirajctl` | `bin/` | ✅ **Done.** A stray binary at the root made the root read as a build directory |
| `internal/middleware/`, `locales/` | deleted | ✅ **Done.** Both empty, both naming a concept that lives elsewhere |

**One client file becomes twenty-six.** The split, the inventory and the
reasoning are [STACK.md §2.3](STACK.md); the order is [TODO.md](TODO.md)
prompts 2 and 8b.

### 6.4 What deliberately does not move

- **`cmd/`, `internal/` and every package under it.** The five-layer split is the architecture; renaming it would cost every import path and buy nothing.
- **`assets.go` at the root.** `go:embed` cannot reach above its own directory. People keep trying to "fix" this; the file says so in a comment.
- **`internal/database/migrations` and `seed`.** Beside the code that applies them, and embedded with it. A top-level `migrations/` would separate them from their only reader.
- **`internal/i18n/locales`.** The catalogues belong to the package that negotiates them. A root `locales/` existed, was empty, and is now ignored precisely so it cannot come back.
- **`Makefile`, `run.sh`, `README.md`.** The three things a newcomer looks for first stay where they look.

### 6.5 Where today differs

Four gaps remain between this tree and the working one. Each is a
[TODO.md](TODO.md) prompt, not an aspiration.

| Gap | Prompt |
|---|---|
| ✅ **Closed.** Frontend sources moved to `web/src`, build output to `static/dist/`, `internal/assets/` added | 2–3 |
| ✅ **Closed.** `api/openapi.yaml`, `docs/api.md` and `internal/handlers/api/` all exist | 7a–7b |
| Documents at the root, with the Dockerfile and both compose files beside them — **seventeen entries** | 10b, 10c |

**Already closed:** the two empty directories, the stray binary, and two
template components with no caller. Verified at the same time: no unused Go
dependency, no uncalled function, no unused stylesheet rule — seven classes
look unreferenced to a plain search and are all composed at runtime.

---

## 7. Conventions

### 7.1 Files

Template files are named for the screen; handler files mirror them; repository
files are named for the **noun they query**, never the screen that calls them.
Migrations are a zero-padded sequence then what they do in words. Translation
keys are area → screen → element, never prose.

One concept per file. Over ~600 lines needs a reason. Generated files are
committed, never edited — CI regenerates and fails on a difference. No file is
named "utils", "helpers", "common" or "misc".

**One thing, one place.** A constant, a rule, a query shape or a documented fact
exists once; the second copy is deleted, not kept in step. This governs the
three documents as much as the code.

### 7.2 Code

- **Errors** wrap on the way up, handle once at the top. Sentinels for what the caller must distinguish, compared by identity. A handler turns an error into a field message, a flash plus redirect, or a logged 500 — never a blank page. Nothing is swallowed silently.
- **Context** first on anything touching the database or network. Request-scoped values through typed accessors. No background work inherits a request context.
- **Naming** uses the domain word: a round is a round, not a session. Booleans read as assertions.
- **Configuration** comes from the environment with a documented default. A missing value that would produce a silently broken deployment fails at boot, naming the variable. Production is stricter than development and never relaxable by a variable. The variables themselves are [STACK.md §5](STACK.md).
- **Concurrency**: shared state is guarded or owned by one goroutine. Long-lived work is cancellable. A sweep logs what it swept — silence looks like not running.

### 7.3 Recipes

**A page** — route, handler, template from the shared primitives, keys in all
three catalogues, walk-test entry; then check it signed out, as a guest, signed
in, and in Arabic at 320 px.

**A query** — repository file named for the noun; if it reads the bank, apply
the filter set by copying a neighbour; a test asserting the constraint.

**A migration** — next number, constraint in the schema not only in Go,
deletion behaviour stated on every key, never edit one that has run, and expand
before contract for anything renaming.

**An admin screen** — role gate, audit entry, nav entry, confirmation, and an
empty-target check inside the transaction where it cascades.

**A taxonomy change** — reshaping it is admin-only, writing inside it is
moderator work (D11): a domain route goes behind `RequireAdmin`, a category
route stays behind `RequireModerator`. Both carry the audit entry, and both
refuse a non-empty target inside the transaction.

---

## 8. The interface

### 8.1 Design system

Ninety custom properties: brand ramp, semantic colours with soft and ink
variants, surfaces, borders, text at three emphases, radii, shadows, easings,
three font stacks including a dedicated Arabic face. Light and dark both
defined; dark resolves three ways so a toggle wins in both directions.

**Missing, measured:** no spacing scale (80 hardcoded gap values in markup), no
type scale, **214 inline style attributes**, and nine `max-width` breakpoints
plus two `min-width`, each added on its own.

A token is the only way to express a repeating value — a literal is allowed
once. An inline style must be dynamic; a fixed gap is not. **The 214 inline
styles are why the style policy must stay permissive** — retiring them is a
security improvement wearing a design-system costume.

### 8.2 Responsive

Eleven ad-hoc widths collapse to four: **Compact** ≤360 (one column) ·
**Phone** 361–560 (two tracks, actions stack) · **Tablet** 561–860 (bottom bar,
grids open) · **Desktop** >860 (top nav with labels). A fifth needs a reason
written beside it.

The page never scrolls sideways — wide content scrolls inside its own
container. The action cluster is pinned to the inline end at every width. A
label gives way by **wrapping, not clipping**. An action beside a sentence moves
below it before the sentence becomes a column. Targets ≥44 px where the pointer
is coarse. Every layout is verified in both writing directions, measured by a
headless pass, not eyeballed.

### 8.3 Accessibility — WCAG 2.2 AA

Already enforced as tests on every player screen in both directions: the
document declares language and direction, images carry alternative text,
buttons have accessible names, fields are labelled.

Colour is never the only signal. Focus is always visible. The keyboard reaches
everything the pointer does. Dynamic regions announce themselves. Motion
respects the reduced-motion preference. **A component conversion may not change
a name, a role or a relationship.**

### 8.4 Languages

Three, Arabic first and RTL. 904 keys complete in all three, enforced both
directions so a missing key and an orphan both fail.

Arabic is the fallback everything resolves to — the one value never optional.
Logical properties only. Translation is a human act; machine output is marked
and held. No English in the client script. A key with a placeholder is always
called with its argument. A new language is a catalogue, a locale row in every
translated table, and one line in `i18n.Supported` — which is also the order the
switcher renders.

---

## 9. Guarantees

### 9.1 Content integrity

Review before players. **Retiring is not deleting** — withdrawal keeps history;
deletion requires the admin role, an empty target and a confirmation. Nothing
is seeded by accident: loading the bank is a command somebody runs, never a
boot-time side effect.

### 9.2 Data

Forward-only numbered migrations, never edited, applied on boot. The schema is
the enforcement — every foreign key states its deletion behaviour and a cascade
is a recorded decision. Transactions are drawn at the business operation.

**Closed, prompt 6a:** `questions.category_id` was `ON DELETE CASCADE`, so
application code was the only thing between a category delete and every answer
players gave. Migration 0034 made it `RESTRICT`, and the database now refuses
that delete — and the delete of a domain still holding categories.

Retention is explicit for the audit trail, notifications, sessions and spent
tokens.

### 9.3 Security

| Control | State |
|---|---|
| Content policy | Scripts restricted to this origin; **no inline script anywhere** |
| Cross-site forgery | Paired token on every mutation, JSON path for fetch callers |
| Framing / sniffing / referrer | Denied, blocked, trimmed |
| Permissions | Microphone only; location and camera denied |
| Transport | Production refuses to start without secure cookies and an https origin |
| Peer address | Forwarded headers trusted only for declared proxy hops |
| Secrets | Required in production, generated in development, never literal in a file |
| Credentials | Hashed, never recoverable, never logged |
| Upload bodies | Ceiling applied before anything reads the body |

**The answer sheet is the asset** — anything exposing correct answers at scale
is a product decision, which is why the public endpoint is credentialed.

### 9.4 Performance budgets

Stylesheet <50 KB compressed · first-load script <120 KB · median server render
<100 ms · <10 database round trips per page · LCP <2.5 s on a phone · INP
<200 ms. Protected by content-hashed immutable assets, compression, and
server-capped pagination. **These numbers are the tripwire** for every piece of
infrastructure [STACK.md §8](STACK.md) refuses until it is earned.

---

## 10. Testing

304 tests across 53 files, against a **real** PostgreSQL — most of what matters
here is a constraint or a transaction, and neither can be checked against a
substitute.

| Package | Files | Tests | Owns |
|---|---|---|---|
| `handlers` | 19 | 126 | Status codes, role gates, redirects, flashes, the walk across every screen, guest parity, accessibility, the hotseat and device-match flows |
| `service` | 10 | 75 | Scoring, matching, import verdicts, duplicate detection, text diffing, mail, reset tokens |
| `repository` | 13 | 46 | Constraints and transactions — cascades, partial unique indexes, the filter set |
| `views` | 6 | 28 | Markup contracts: class coverage, the composer, live events, rendered dumps |
| `models` | 3 | 18 | Invariants expressible as a method |
| `i18n` | 2 | 11 | Catalogue completeness both directions, placeholder agreement, call-site verbs |

**Five layers, each catching what the others cannot see.** Model and service
for rules in isolation; repository for constraints and transactions; handler
for codes, gates and redirects; **cross-cutting for agreement between sides**;
walk and smoke for every screen answering at all.

**Each test process builds and drops its own database**, named from the process
id — never the development database — so a suite run cannot touch development
data.

**Protect the cross-cutting layer.** It catches the server and client
disagreeing on a minimum, a published event with no subscriber, a class in
markup with no rule behind it. Those tests read the client source — when that
source stops being one file they must read *all* of it, or they pass while
asserting nothing.

A bug fixed gets a test that fails without the fix. A test name says what must
be true. Nothing is skipped silently. Which rules are enforced as build-failing
tests is [STACK.md §7](STACK.md).

---

## 11. Decisions

| # | Decision | Over | Reversal |
|---|---|---|---|
| D1 | Alpine's policy-safe build; scripts stay `'self'` | Relaxing the policy | Relax it; components still work |
| D2 | One read-only public endpoint, bearer-authenticated | A public endpoint with no credential | Drop two fields; route stays |
| D3 | Production is a compose **overlay**, not a profile | A profiled service | None — a required-variable guard fails the whole file before profile filtering |
| D4 | Alpine takes the 20 local-state components only | Converting all client code | None; the layers coexist |
| D5 | Frontend sources leave the served tree | Building in place | A move |
| D6 | The binary stays self-contained, assets embedded | Assets from another origin | Changes deployment, not code |
| D7 | Server-rendered with progressive enhancement | A single-page application | Deep — §8.3 and §9.4 depend on it |
| D8 | PostgreSQL only — no cache, queue or search engine | Infrastructure per feature | Each is a deliberate addition against §9.4 |
| D9 | The level above a category is a **domain** | Subject / field / section / pack | Expensive after the first migration. *Section* is Category's Arabic word; *subject* collides with tickets; *pack* implies purchase |
| D10 | A separate `domains` table | `categories.parent_id` | A self-reference shares one slug namespace and lets a question point at a parent |
| D11 | Domains are admin-only; categories stay moderator-editable | One rule for both | Reshaping the taxonomy is structural; writing a category is content work |
| D12 | Category slugs stay globally unique | Unique per domain | Per-domain breaks the bundled bank and the importer |
| D13 | The two round columns — `game_sessions.domain_id`, `challenges.domain_id` — ship in migration 0034 with the domains themselves | Adding them later | Otherwise the only choices are one category or everything, and a Sport player is asked about the Qur'an |
| D14 | The domain join enters the filter set in its own branch (6b), after the schema and before any screen | Shipping it with the migration, or with the screens | None — it is a sequencing rule. The branch that follows it would otherwise put screens over an unfiltered draw |
| D15 | One fact, one file: architecture, stack and work are three documents with no overlap | One long document, or repeating shared facts for convenience | A merge, and the staleness that follows |

---

## 12. Weaknesses and risks

| Weakness | Cost | Why tolerated |
|---|---|---|
| One stylesheet, one client script | Finding a rule means searching; 26 behaviours in one closure | Being fixed — [TODO.md](TODO.md) prompts 2, 8b and 9a–9c. Sizes in [STACK.md §2.3](STACK.md) |
| 214 inline styles | A permissive style policy | Retired opportunistically — prompt 9c |
| No read replica or cache | Every page hits the primary | Correct at this size; §9.4 is the tripwire |
| Attachments on a local volume | Blocks horizontal scale | The metadata split means only the adapter moves |
| Polling beside the event stream | Two paths to one freshness | The stream optimises, the poll guarantees |
| Admin screens untested for accessibility | The suite walks player screens only | Extend it |
| ~~Test databases leak~~ | — | ✅ **Closed.** Each run now sweeps what an interrupted one left — at the start, because a run that is killed cannot clean up after itself. 46 survivors found and dropped on the first sweep |
| The player's screens still show one level | A player cannot choose a subject area, only a category | The admin side is built; the player side is prompt 6e |
| ~~Questions cascade from categories~~ | — | ✅ **Closed** by migration 0034: the schema refuses it, not only the handler |

| Risk | Mitigation |
|---|---|
| Cross-cutting tests go quiet during the client split | They read every source file, with a size floor |
| The content policy is relaxed and never tightened | A test asserts production grants no evaluation exception |
| Unreviewed content reaches players by a new query path | One filter set, one layer, a test per read path |
| A category delete reaches the cascade | Role gate, empty-target check, confirmation, audit — and `RESTRICT` in the schema since 0034 |
| A new subject area leaks into Siraj rounds | The domain join enters the filter set in branch 6b — after the schema, before any screen exists (D14) |
| The bank stalls for want of reviewed content | Tracked in [TODO.md](TODO.md); the constraint is supply, not tooling |
| A clean clone stops working unnoticed | A CI job clones, runs one command and probes |
| The three documents drift back into overlap | D15, §7.1, and the last step of every prompt: correct the one file the change made wrong |

---

## 13. Done, and reviewed

**A change is done when all of these are true.** It does what was asked and
names what it did not · no fabricated value reached the repository · nothing
structural was hardcoded · it works signed out, as a guest and signed in · it
works in Arabic at 320 px · every string is in all three catalogues · it is
keyboard-reachable and named for assistive technology · the page is correct
before any script runs · a test fails without the fix · privileged actions are
gated and recorded · comments say why · every merge gate is green · the one
document this made wrong is corrected in the same branch.

**A reviewer checks, in this order.** Right layer, imports pointing downward,
no SQL outside the repository, no copied constant → destructive paths gated and
recorded, new bank reads carrying the filter set, no swallowed errors → signed
out / guest / signed in, Arabic at 320 px, keyboard, works without script →
three catalogues with matching placeholders → a test that fails without the
change → documentation corrected in the file that owns it. *A review that
checks only the first group is a style review.*

---

## 14. Glossary

**Domain** the subject area a category belongs to, admin-created and one level
deep — in the schema and the draw since prompts 6a and 6b, with the screens
still to come; retiring one withdraws its categories'
questions from the draw and from the availability count alike ·
**Taxonomy** domain and category together, two levels and no more ·
**Guest** a real expiring account with full parity · **Seat** whose turn it is
on a shared device · **Hotseat** a match played by passing one device ·
**Room** an open conversation anyone may join · **Group** a closed one ·
**Match** a challenge with more than two players · **Daily** the round that is
the same for everyone for one day · **Availability** how many questions a
category-and-difficulty pair can actually draw · **Retire** withdraw from play
while keeping history · **Set** questions a player wrote · **The filter set**
active, translated, past review, active category, active domain — five, since
prompt 6b.
