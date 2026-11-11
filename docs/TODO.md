# Sirāj — TODO

The work, as prompts. One prompt per branch, in order; each names the files it
touches, the code it writes and the checks that close it. Nothing here describes
the system as it already is — that is [ARCHITECTURE.md](ARCHITECTURE.md) for the
shape and [STACK.md](STACK.md) for the technology.

**Rules for every prompt.** Ready when its predecessor is merged. Done when the
suite is green, the asset build is green, its boxes are ticked, and the one
document the change made wrong is corrected in the same branch. One branch per
prompt, named below; one idea per commit — a template and the component driving
it land together, a move and a behaviour change do not. The subject says what
changes for a person; the body says why.
**Nothing is staged, committed or pushed on anybody's behalf. Staging,
committing and pushing are the author's — never automated.** **Nor is any
branch: none is created, switched, merged or deleted on anybody's behalf.** The
**Branch** line on each prompt is the name to use, not an instruction to create
one — the author opens it when they start the prompt.

**Where work stops.** Every change lands in the working tree and stays there,
unstaged. The author reads the whole diff, decides what belongs in which commit,
writes the message, and pushes. A prompt that reports itself finished means the
files are edited and the checks were run — never that anything was recorded in
git.

**Order is not advice.** Each prompt assumes its predecessor landed. Prompts 1–5
are infrastructure and independent of the domain. Prompt 6 comes before 7 and 8
because it reshapes the round setup screen — the richest Alpine conversion — and
adds a field the endpoint must carry from its first version. Doing it after
means rewriting two components and versioning a contract that already shipped.

---

## The board

| Prompt | Title | Branch | After | Effort | Boxes |
|---|---|---|---|---|---|
| 1 | One command from a clean clone | `stack/compose-split` | — | 0.5 d | 5 |
| 2 | Toolchain in, sources moved | `stack/vite-toolchain` | 1 | 1 d | 6 |
| 3 | Serving the build | `stack/asset-manifest` | 2 | 1 d | 5 |
| 4 | Assets in the image and CI | `stack/image-assets` | 3 | 0.5 d | 3 |
| 5 | Smoke follows the build | `stack/smoke-assets` | 4 | 0.5 d | 5 |
| 6a | Schema — domains above categories | `taxonomy/migration` | 5 | 0.75 d | 6 |
| 6b | The draw honours the new level | `taxonomy/draw-filter` | 6a | 0.5 d | 4 |
| 6c | Model and repository | `taxonomy/repository` | 6b | 0.5 d | 3 |
| 6d | Admin screens | `taxonomy/admin` | 6c | 0.75 d | 8 |
| 6e | Player surfaces | `taxonomy/player` | 6d | 0.75 d | 3 |
| 6f | Remaining surfaces | `taxonomy/surfaces` | 6e | 0.25 d | 2 |
| 6g | What the review found outside the taxonomy | `taxonomy/aftermath` | 6d | 0.25 d | 3 |
| 7a | The credential | `stack/public-api-keys` | 6f | 0.5 d | 4 |
| 7b | The endpoint and its contract | `stack/public-api` | 7a | 0.75 d | 4 |
| 7c | The two interface endpoints move | `stack/api-prefix` | 7b | 0.25 d | 2 |
| 8a | Teach the class checker | `stack/alpine-prelude` | 7c | 0.5 d | 3 |
| 8b | Twenty conversions, twenty commits | `stack/alpine-<name>` | 8a | 3.5 d | 20 |
| 9a | Spacing and type scales | `stack/design-tokens` | 3 | 0.5 d | 2 |
| 9b | Eleven breakpoints become four | `stack/breakpoints` | 9a | 0.5 d | 2 |
| 9c | Inline styles down | `stack/inline-styles` | 9b | 0.5 d | 2 |
| 10a | Stop the test databases leaking | `stack/test-db-sweep` | — | 0.25 d | 2 |
| 10b | Documents into `docs/` | `stack/docs-layout` | 9c | 0.25 d | 2 |
| 10c | Dockerfile and compose into `deploy/` | `stack/deploy-layout` | 10b | 0.5 d | 3 |
| 11a | README and deployment guide | `stack/docs-readme` | 10c | 0.25 d | 2 |
| 11b | Reconcile the three documents | `stack/docs-reconcile` | 11a | 0.25 d | 9 |

**Eight boxes left of 105.** Everything else in this file has landed and is
green: `make check`, the asset build, the catalogue gates, and a 267-check HTTP
walk against the stack the deployment files describe.

What is left is deliberately left. Two are a person's work that no test in this
repository can do — a visual pass at four widths in two writing directions, and
deciding which of 153 inline styles are genuinely dynamic. One is a conversion
worth doing slowly. One is a note about how to read a smoke run. One is this
file's own tidying, which should happen after somebody has read it.

---

## Blocked on a person, not on code

Principle 1 forbids inventing any of the three. The first two block nothing in
6a–6f and both block calling the taxonomy work finished; the third blocks
writing the prompt 7b contract, not the code around it.

- [ ] **The landing copy, in all three languages.** It promises "Eight categories covering the Qur'an, the Sīrah, fiqh, hadith and Siraj history" — false the moment a second subject area exists. Product copy is written, not generated
- [ ] **Which subject areas to create, with which categories.** Nothing is seeded except moving the eight existing categories under one Siraj domain. The screens from 6d are how the rest are created, so this blocks no branch
- [ ] **Whether the public endpoint filters by `domain` as well as `category`.** The response carries the domain from its first version; the request has `category` and nothing else. Adding a filter parameter after the contract ships is a version decision, so it is taken before 7b writes the contract

**The standing constraint:** the question bank needs reviewed content. Larger
than every prompt here and unaffected by all of them. The authoring path exists;
the shortage is reviewed material, not tooling.

---

## Prompt 1 — One command from a clean clone

**Branch** `stack/compose-split` · **Effort** 0.5 d
**Touches** `docker-compose.yml` · `docker-compose.prod.yml` *(new)* · `.dockerignore`

**Goal.** A clone with no environment file comes up and serves on 8080, and
production is a separate overlay that refuses to start unconfigured.

**Steps.**
1. Rewrite `docker-compose.yml` as the base below — database **and** application, no guards, no profile. Today it starts the database only, which is why a clean clone never serves.
2. Add `docker-compose.prod.yml` as the overlay. `!reset []` on `ports` needs Compose 2.24+.
3. Extend `.dockerignore` — it currently holds `.git`, `.env`, `bin/`, `tmp/`, `*.log` — with `node_modules/`, `static/dist/` and the root documents.
4. Leave the service name `db`, the container names and the credential values alone: `make db-up`, `db-down`, `db-reset`, `db-shell` and `run.sh` (which defaults `DB_CONTAINER=siraj-game-db`) must keep working untouched.

```yaml
# docker-compose.yml — what a clean clone runs. No guards, no profile.
services:
  db:
    image: postgres:16-alpine
    container_name: siraj-game-db
    environment: { POSTGRES_USER: siraj, POSTGRES_PASSWORD: siraj, POSTGRES_DB: siraj-db }
    ports: ["5434:5432"]
    volumes: [pgdata:/var/lib/postgresql/data]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U siraj -d siraj-db"]
      interval: 5s
      timeout: 3s
      retries: 10

  # SESSION_SECRET is deliberately absent: outside production the application
  # generates one at boot. A literal here would be a fake credential in version
  # control and a footgun if it ever reached a deployment.
  app:
    build: .
    container_name: siraj-game-app
    depends_on: { db: { condition: service_healthy } }
    environment:
      APP_ENV: development
      APP_ADDR: ":8080"
      DATABASE_URL: postgres://siraj:siraj@db:5432/siraj-db?sslmode=disable
      BASE_URL: http://localhost:8080
      SECURE_COOKIES: "false"
      DEFAULT_LOCALE: ar
      UPLOAD_DIR: /app/data/uploads
      # SEED_ON_START stays unset: docker compose exec app /app/sirajctl seed
    volumes: [uploads:/app/data/uploads]
    ports: ["8080:8080"]
    healthcheck:
      test: ["CMD", "/app/server", "-healthcheck"]
      interval: 15s
      timeout: 5s
      retries: 3
      start_period: 20s
    restart: unless-stopped

volumes: { pgdata: , uploads: }
```

```yaml
# docker-compose.prod.yml — overrides the same service, so one container either
# way. Expects a TLS terminator: production refuses to start without it.
#   SESSION_SECRET=$(openssl rand -hex 32) BASE_URL=https://siraj.example \
#     docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
services:
  app:
    environment:
      APP_ENV: production
      SESSION_SECRET: ${SESSION_SECRET:?set SESSION_SECRET — openssl rand -hex 32}
      BASE_URL: ${BASE_URL:?set BASE_URL to the public https:// origin}
      SECURE_COOKIES: "true"
      TRUSTED_PROXY_HOPS: "1"
    ports: !reset []        # the terminator publishes the port. Compose 2.24+
```

**Verify** — all four were run against Compose 2.40.3 and are the acceptance test:

| Command | Expect |
|---|---|
| `docker compose --env-file /dev/null config --services` | `db app`, no interpolation error |
| same, counting the secret variable | `0` — the app generates its own |
| with the overlay, no environment | refuses: *required variable SESSION_SECRET is missing* |
| with the overlay and both set | production, secure cookies, app port unpublished |

**Done when.**
- [x] `docker compose --env-file /dev/null config --services` prints `db` and `app`, no interpolation error
- [x] A clone with no environment file comes up and answers on 8080
- [x] The overlay still demands `SESSION_SECRET` and `BASE_URL`
- [x] The build context excludes `node_modules/`, `static/dist/` and the documents
- [x] [STACK.md §6](STACK.md) matches what the two files now do

> **An existing database is not renamed by changing the variable** — the
> entrypoint only creates one on an empty volume. Once, with nothing connected:
> `ALTER DATABASE islamic_game RENAME TO "siraj-db";` (quoted: the hyphen makes
> it an identifier Postgres will not parse bare). A fresh clone needs none of it.

**Rollback.** Revert two files. No Go changed.

---

## Prompt 2 — Toolchain in, sources moved

**Branch** `stack/vite-toolchain` · **After** 1 · **Effort** 1 d
**Touches** `package.json` · `vite.config.js` *(new)* · `web/src/**` *(new)* · `static/css/`, `static/js/` *(emptied)* · `internal/views/clientsources_test.go` *(new)* · `internal/handlers/clientsources_test.go` *(new)* · the six Go test files that read asset paths · `.github/workflows/ci.yml` · `.gitignore`

**Goal.** Sources leave the served tree and the tests that read them keep
asserting what they were written to assert. **No behaviour change in this
branch.**

**First, before component one:** confirm which package provides Alpine's
policy-safe build. The gate that catches a wrong one only runs at prompt 3,
after twenty components would be written in the wrong dialect
([STACK.md §2.2](STACK.md)).

**Steps.**
1. `npm i` Alpine as the **only** runtime dependency and Vite as a dev dependency, and reclassify both Neon packages as dev dependencies ([STACK.md §2.2](STACK.md)). Add `build` and `dev` scripts. This file is rewritten once, here.
2. Add `vite.config.js` at the root (below).
3. `git mv` the three served sources: `static/css/app.css` → `web/src/css/app.css`, `static/js/app.js` → `web/src/js/app.js`, `static/js/boot.js` → `web/src/js/boot.js`.
4. Split the shared helper block ([STACK.md §2.3](STACK.md) names it) into `web/src/js/helpers.js` and import it from the entries. Import the stylesheet **from `app.js`**, not as its own Vite input. Add `web/src/js/alpine.js` as a third entry: Alpine plus an empty component registry, which prompt 8b fills.
5. Add the `clientSources` helper (below) once per package, `internal/views/` and `internal/handlers/`. `repoRoot` already exists in both.
6. Rewire **in the same commit** every place that reads an asset by path — the nine in [STACK.md §2.4](STACK.md): the four string-searching tests move to `clientSources`, the two stylesheet readers move to `web/src/css/app.css`, CI's `node --check` covers every file under `web/src/js` and `web/src/components`, and the smoke assertions keep their paths until prompt 5.
7. `.gitignore`: add `static/dist/`.

```js
// vite.config.js
import { defineConfig } from 'vite'
import { resolve } from 'node:path'

// Three entries. The stylesheet is imported from app.js, not listed as its own
// input — Vite then records it under the app entry's "css" array, which is how
// the server finds the built filename. boot.js imports nothing and stays its
// own entry: it is render-blocking by design and must never wait on a chunk.
export default defineConfig({
  root: resolve(import.meta.dirname, 'web/src'),
  base: '/static/dist/',            // must match where the static handler serves
  build: {
    outDir: '../../static/dist',    // inside the embedded tree: one binary still
    emptyOutDir: true,              // required, outDir is outside root
    manifest: true,
    rollupOptions: {
      // Absolute — with `root` set, a relative path resolves against root again.
      input: {
        app:    resolve(import.meta.dirname, 'web/src/js/app.js'),
        alpine: resolve(import.meta.dirname, 'web/src/js/alpine.js'),
        boot:   resolve(import.meta.dirname, 'web/src/js/boot.js'),
      },
    },
  },
  server: { port: 5173, strictPort: true, cors: true },
})
```
*Proven here:* `node --check` parses this file.

```go
// clientSources is every line of client script, concatenated.
//
// Four tests assert the server and the client agree — waveform constants,
// event names, team minimums, class names. They were written when the client
// was one file and they read that file. It is about to be twenty-six. Reading
// one of them would leave those tests passing while asserting nothing, which
// is worse than failing: the drift they exist to catch would go through
// silently.
func clientSources(t *testing.T) string {
	t.Helper()
	root := filepath.Join(repoRoot(t), "web", "src")
	var out strings.Builder

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".js" { return err }
		body, err := os.ReadFile(path)
		if err != nil { return err }
		out.WriteString("\n// ---- " + path + "\n")
		out.Write(body)
		return nil
	})
	if err != nil { t.Fatalf("reading the client sources: %v", err) }

	// A refactor that moved the sources would otherwise make every caller pass
	// against an empty string.
	if out.Len() < 50_000 {
		t.Fatalf("only %d bytes of client source under %s; the checks that "+
			"read this would pass vacuously", out.Len(), root)
	}
	return out.String()
}
```
Imports: `os`, `path/filepath`, `strings`, `testing` — verified, nothing else.

**Verify.** `npm run build` · `make check` · `node --check` over every entry.

**Done when.**
- [x] Build output exists under `static/dist/` and is excluded from version control
- [x] The four cross-cutting tests read concatenated sources, and the size floor is exercised once against an empty directory
- [x] No file under `static/` is a source file any more, and nothing under `web/` is reachable over HTTP
- [x] CI syntax-checks every client source, not one file
- [x] Which package provides the policy-safe Alpine build is recorded in the branch — `@alpinejs/csp`, [STACK.md §2.2](STACK.md)
- [x] Suite green — *one behaviour change, reported: the bundler found a CSS block whose selector had been lost, which every browser was silently dropping. Fixed, and the build is now the test that catches the next one*

**Risk this retires.** The cross-cutting tests going silent when the script
splits: the helper and its size floor land in the *same* commit as the move.

**Rollback.** A move; reverting restores the nine paths that reference them.

---

## Prompt 3 — Serving the build

**Branch** `stack/asset-manifest` · **After** 2 · **Effort** 1 d
**Touches** `internal/assets/manifest.go` *(new)* · `internal/views/layout.templ` (the five asset tags) · `internal/handlers/assets.go` · `cmd/server/main.go` · `internal/config/config.go` · the content-policy constant · a new policy test

**Goal.** The page links hashed, built filenames when a build exists, and the
source paths when it does not — with no configuration in either case.

**Steps.**
1. Add `internal/assets/manifest.go` (below). Both manifest locations are tried: Vite 5 writes under a dot-directory inside the output, Vite 4 at the output root. Log which candidate matched, at boot.
2. Replace the whole-tree `?v=` helper behind the five tags in `layout.templ` with manifest resolution: `Resolve("js/app.js")`, `Resolve("js/boot.js")`, `Resolve("js/alpine.js")`, and `Stylesheets("js/app.js")` for the link tags. A miss falls back to the source path — that fallback *is* the no-toolchain story.
3. `Load(staticFS, "/static/dist/")` in `cmd/server`, passed into the view layer the same way `assetV` is today.
4. Add `VITE_DEV_SERVER` to `internal/config` — empty default, **ignored when `APP_ENV=production`**.
5. Turn the content-policy constant into a function of config, as [STACK.md §3](STACK.md) requires: the dev origin and its websocket are admitted when configured and `APP_ENV` is not production, and the production string is compared byte-for-byte against today's constant.

```go
// Package assets resolves a source entry to the file the bundler built from it.
//
// A missing manifest is not an error — it is the dev server, or an unbuilt
// tree — so every lookup misses and the caller links the source path. That is
// what keeps the application working with no toolchain installed.
package assets

import ("encoding/json"; "io/fs"; "path")

type Entry struct {
	File    string   `json:"file"`
	Src     string   `json:"src"`
	IsEntry bool     `json:"isEntry"`
	CSS     []string `json:"css"`
	Imports []string `json:"imports"`
}

type Manifest struct {
	entries map[string]Entry
	base    string
}

// Both locations are tried rather than one pinned: Vite 5 writes it under a
// dot-directory inside the output, Vite 4 at the output root. An upgrade must
// not silently start serving unhashed URLs.
var candidates = []string{"dist/.vite/manifest.json", "dist/manifest.json"}

func Load(staticFS fs.FS, base string) *Manifest {
	m := &Manifest{entries: map[string]Entry{}, base: base}
	for _, c := range candidates {
		raw, err := fs.ReadFile(staticFS, c)
		if err != nil { continue }
		if err := json.Unmarshal(raw, &m.entries); err != nil { continue }
		break
	}
	return m
}

func (m *Manifest) Loaded() bool { return len(m.entries) > 0 }

// Resolve returns the public URL of a built entry, and whether it was found.
func (m *Manifest) Resolve(entry string) (string, bool) {
	e, ok := m.entries[entry]
	if !ok || e.File == "" { return "", false }
	return path.Join(m.base, e.File), true
}

// Stylesheets is how the link tag finds its hashed filename: the stylesheet is
// imported from js/app.js, so Vite records it under that entry.
func (m *Manifest) Stylesheets(entry string) []string {
	e, ok := m.entries[entry]
	if !ok { return nil }
	out := make([]string, 0, len(e.CSS))
	for _, href := range e.CSS { out = append(out, path.Join(m.base, href)) }
	return out
}
```
*Proven here:* `go vet` clean in a scratch module.

**Done when.**
- [x] Which manifest candidate matched is logged at boot
- [x] With the dev server configured, the page and the client module load from it — *the replacement itself still wants a browser*
- [x] With no dev server and no build, the page still **answers** — unstyled, and the log says why. *Changed from "loads from sources": sources are no longer reachable over HTTP, which is the point of prompt 2, so `run.sh`, `make run` and `make build` build the assets first*
- [x] The production policy string is byte-identical with the dev origin set and ignored
- [x] A test builds the production string *with* a dev origin configured and asserts the origin is absent and no evaluation exception is granted

**Risk retired.** The policy relaxed for the dev server and never tightened.

**Rollback.** The manifest misses and the page links source paths — the
fallback path is the rollback.

---

## Prompt 4 — Assets in the image and CI

**Branch** `stack/image-assets` · **After** 3 · **Effort** 0.5 d
**Touches** `Dockerfile` · `.github/workflows/ci.yml`

**Goal.** The image carries the assets it was built with, and CI proves a clean
clone still comes up.

**Steps.**
1. Write the three stages exactly as [STACK.md §6](STACK.md) forces them — assets, then compile, then runtime. The failure to avoid: an asset stage *beside* the compile stage produces an image with an empty asset directory and a page that links nothing.
2. CI: a Node step and `npm run build` **before** the Go steps, so the embedded tree exists when `go build` runs.
3. A separate job: clone, `docker compose up -d`, wait for the health probe, request `/healthz` and the landing page, assert both answer.

**Done when.**
- [x] `docker compose build` green from a clean clone — and the stack comes up healthy, serving the hashed assets it links
- [x] The clean-clone job exists and its sequence was run by hand here: one command → `/healthz` 200 → every linked `/static/dist/` URL 200. *The job itself runs on the next push*
- [x] The runtime stage contains no Node and no Go toolchain. *Corrected from "no shell": the `alpine:3.20` base carries busybox. The probe needs none — a distroless base would, and is a decision nobody has taken ([STACK.md §6](STACK.md))*

**Risk retired.** The image embedding a stale bundle: the asset stage runs
before compilation and the build context excludes local output, so the stage
output is the only copy.

---

## Prompt 5 — Smoke follows the build

**Branch** `stack/smoke-assets` · **After** 4 · **Effort** 0.5 d
**Touches** `scripts/smoke.sh`

**Goal.** The asset assertions check what is actually served, not two paths
that used to exist.

**Steps.**
1. The `css` and `js` checks currently request `/static/css/app.css` and `/static/js/app.js` directly. Read the hrefs out of the rendered markup instead — the script already captures a page into `assets.html` for the versioned-URL check — and assert *those* answer 200.
2. Keep the fingerprint assertion: the stylesheet and script URLs must carry a hash, and the pair must agree, which is what the `?v=` check always meant.
3. Keep the boot-script pair: it is served as a file and is not inline.
4. Run the whole script against the compose stack, not only a local `make run`.

**Done when.**
- [x] Every asset assertion reads its URL from the rendered page — and one more: the boot script must **not** be a module, which is the whole reason it is copied rather than bundled
- [x] Smoke green against the compose stack: **267 passed, 0 failed**, reset flow included. The stack gained a mail catcher, kept out of production by a profile nobody enables
- [x] The run no longer blames the database when the cause is the register limiter — ten per address per hour ([writelimit.go:47](internal/service/writelimit.go#L47)), and one full run spends most of that budget, so a second run inside the hour needs the app restarted first. The message now says so
- [x] `scripts/mail-token.py` replaces `mailhog-token.py`: it reads the catcher's API directly and names the failure — an empty mailbox, an unreachable catcher, a message with no link — instead of raising `IndexError`
- [ ] **Smoke is only meaningful from a known state.** The same revision scored 267/0 on a freshly created database and 259/8 on a dirty one — admin gating, the friends flow and a thread check all read leftovers from the run before, and `DROP DATABASE` had silently refused while a connection was open. `reset-test-data.sh` clears fixture accounts but not everything they touched, so the script should assert the state it needs rather than assume it

---

## Prompt 6a — Schema: domains above categories

**Branch** `taxonomy/migration` · **After** 5 · **Effort** 0.75 d
**Touches** `internal/database/migrations/0034_domains.sql` *(new)*

**Goal.** The level above a category exists in the schema, the eight categories
sit under one Siraj domain, a whole-domain round has somewhere to record what
it drew from, and a category delete can no longer cascade into player history.

**First:** confirm the foreign-key name on the target database. It is
`questions_category_id_fkey` here, verified, but one built by another path may
differ.

```sql
CREATE TABLE domains (
    id serial PRIMARY KEY,
    slug text NOT NULL UNIQUE,
    icon text NOT NULL DEFAULT '🗂️',
    color text NOT NULL DEFAULT '#0ea5a4',
    sort_order integer NOT NULL DEFAULT 0,
    is_active boolean NOT NULL DEFAULT true,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- The same provenance discipline category_translations carries: an unreviewed
-- machine name must not reach a player.
CREATE TABLE domain_translations (
    domain_id integer NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    locale text NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    source text NOT NULL DEFAULT 'seed'
           CHECK (source IN ('seed','human','machine','import')),
    needs_review boolean NOT NULL DEFAULT false,
    PRIMARY KEY (domain_id, locale)
);

-- The eight categories are all Siraj, so they move under one domain rather
-- than being re-entered. A backfill of what exists, not new content.
INSERT INTO domains (id, slug, icon, color, sort_order)
VALUES (1, 'siraj', '🕌', '#0ea5a4', 1) ON CONFLICT (id) DO NOTHING;
SELECT setval('domains_id_seq', (SELECT max(id) FROM domains));

INSERT INTO domain_translations (domain_id, locale, name, description, source) VALUES
  (1,'ar','العلوم الإسلامية','القرآن والسنة والفقه والسيرة والتاريخ','seed'),
  (1,'en','Siraj Knowledge','Qur''an, Sunnah, fiqh, sīrah and history','seed'),
  (1,'fr','Savoir Islamique','Coran, Sunna, fiqh, sîra et histoire','seed')
ON CONFLICT (domain_id, locale) DO NOTHING;

-- Nullable, backfilled, then required — expand-then-contract inside one
-- migration, because the table is small and the window is the deploy itself.
ALTER TABLE categories
  ADD COLUMN domain_id integer REFERENCES domains(id) ON DELETE RESTRICT;
UPDATE categories SET domain_id = 1 WHERE domain_id IS NULL;
ALTER TABLE categories ALTER COLUMN domain_id SET NOT NULL;
CREATE INDEX categories_domain_idx ON categories (domain_id, sort_order);

-- questions.category_id was ON DELETE CASCADE, safe only while nothing but a
-- migration could delete a category. The admin screens changed that: a Delete
-- button over a cascade destroys every question and, through game_answers, the
-- answers players gave. The handler has refused a non-empty delete since those
-- screens shipped, so nothing violating can exist — this makes the schema say
-- the same thing. The count runs first so it fails loudly, not halfway.
DO $$
DECLARE orphans integer;
BEGIN
  SELECT count(*) INTO orphans FROM questions q
    LEFT JOIN categories c ON c.id = q.category_id WHERE c.id IS NULL;
  IF orphans > 0 THEN
    RAISE EXCEPTION 'questions reference % missing categories', orphans;
  END IF;
END $$;

ALTER TABLE questions DROP CONSTRAINT questions_category_id_fkey;
ALTER TABLE questions ADD CONSTRAINT questions_category_id_fkey
  FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE RESTRICT;

-- A round may be "any category in Sport". Without somewhere to record it the
-- only choices are one category or the whole bank, and a Sport player is asked
-- about the Qur'an. (D13)
ALTER TABLE game_sessions ADD COLUMN domain_id integer
  REFERENCES domains(id) ON DELETE SET NULL;
ALTER TABLE challenges ADD COLUMN domain_id integer
  REFERENCES domains(id) ON DELETE SET NULL;
CREATE INDEX game_sessions_domain_idx ON game_sessions (domain_id)
  WHERE domain_id IS NOT NULL;
```
*Proven here:* run against a schema copy carrying the eight real categories —
applies clean, and both deletes are refused by the schema afterwards.

> `source` defaults to `'seed'` above to match `category_translations`, which
> has defaulted to `'seed'` since migration 0008. Every row this migration
> inserts names its source explicitly, so the three backfilled names are `seed`
> either way; the default matters for the next writer, not this one.

**Done when.**
- [x] All eight categories carry `domain_id = 1`
- [x] `questions_category_id_fkey` reports `RESTRICT`, not `CASCADE` — and the database refuses the delete of a category holding questions, or a domain holding categories
- [x] An existing finished round still shows its category name — the backfill orphaned nothing
- [x] `game_sessions.domain_id` and `challenges.domain_id` exist — nullable, `ON DELETE SET NULL`, with the partial index on sessions (D13)
- [x] `domain_translations` matches `category_translations` on provenance: the same four `source` values, the same `'seed'` default, `needs_review` defaulting false, and three backfilled names saying `seed`
- [x] **`categories.domain_id` carries `DEFAULT 1`** — the suite caught this: a `NOT NULL` column with no default turns every category write into a 500 between this prompt and the screens that name a domain. 6d drops it once the form carries the field

**Risks retired.** The backfill orphaning a finished round — nullable,
backfilled, then required inside one migration, and the third box checks an old
round still renders. Tightening the key failing on a violating row — the count
runs first and raises rather than erroring halfway.

**Rollback.** Additive apart from the key swap: drop the two round columns, the
category column, both tables, restore the key to `CASCADE`, delete the
migration row. By hand, after a dump — there is no down-runner. Past the point
where an admin has created a real second domain, roll forward instead.

**Document.** [ARCHITECTURE.md §2.7](ARCHITECTURE.md) — the two tables stop
being *specified*.

---

## Prompt 6b — The draw honours the new level

**Branch** `taxonomy/draw-filter` · **After** 6a · **Effort** 0.5 d
**Touches** `internal/repository/content.go` · `internal/repository/*_test.go`

**Goal.** A domain is part of the filter set before any screen can create one.
**Four conditions become five** (D14).

**Steps.**
1. The three draw queries in `content.go` — `AvailableCounts`, `PickQuestionIDs`, `PickDailyQuestionIDs` — each already join `categories c ON c.id = q.category_id AND c.is_active` and require an active question and a translation past review. Add `JOIN domains d ON d.id = c.domain_id AND d.is_active` to each, in the same position, so the three stay textually identical to one another.
2. `Categories(ctx, locale)` — the list every setup and form screen reads — gains the same join. Without it a screen offers a category whose availability is permanently zero.
3. A repository test per read path: a retired domain's questions are absent from the draw **and** from the count, and the two still agree.

**Done when.**
- [x] Retiring a *domain* withdraws its categories' questions from the draw **and** from the availability count — one rule, the same one a retired category already follows
- [x] Availability and the draw still agree — the count is the contract. Asserted together in one test, and the test fails on both lines when the join is removed
- [x] A category under a retired domain is not offered on any screen — `Categories` carries the condition too, so the list and the draw cannot disagree
- [x] **Before any screen exists.** This is what stops a half-built subject area reaching players

**Risk retired.** A new subject area leaking into Siraj rounds.

**Document.** [ARCHITECTURE.md §2.2](ARCHITECTURE.md) and its glossary — the
filter set is five conditions from here.

---

## Prompt 6c — Model and repository

**Branch** `taxonomy/repository` · **After** 6b · **Effort** 0.5 d
**Touches** `internal/models/models.go` · `internal/repository/domains.go` *(new)* · `internal/repository/content.go` · the eleven `Categories(` call sites

**Goal.** A domain is a type and a repository, with no screen yet and no
behaviour change.

**Steps.**
1. `models`: a `Domain` type and `DomainDraft`, mirroring `Category` and `CategoryDraft`; domain fields on `Category` and `AdminCategory` so a list can show where a category sits.
2. `repository/domains.go` mirroring `categories.go` function for function: `AdminDomains`, `DomainDraft`, `UpsertDomain`, `SetDomainActive`, `DeleteDomain`. `DeleteDomain` counts categories inside its own transaction and refuses a non-empty target, exactly as `DeleteCategory` counts questions.
3. `Categories(ctx, locale)` gains a domain filter. **Pass zero at all eleven call sites first** — `service/import.go`, `service/compare.go`, `handlers/game.go` ×2, `handlers/auth.go`, `handlers/support.go` ×2, `handlers/admin_questions.go` ×3 and one test — so nothing is reviewed twice and this branch changes no behaviour.

**Done when.**
- [x] Repository tests against a real database: write and read back, the Arabic fallback, a shared slug refused, a domain holding categories refused and the same call going through once empty, retire/restore, and the counts the delete button is built from
- [x] All twelve call sites compile with zero — eleven, plus the one 6b's own test added. No behaviour change: zero means every active domain
- [x] A new call site is a new read path — it carries the filter set or it does not ship. `Categories` now returns each row's domain and that domain's name, so a screen can group without a second read

**Document.** [ARCHITECTURE.md §5.3](ARCHITECTURE.md) — `domains` joins the
repository list.

---

## Prompt 6d — Admin screens

**Branch** `taxonomy/admin` · **After** 6c · **Effort** 0.75 d
**Touches** `internal/handlers/admin_domains.go` *(new)* · `internal/views/admin_domains.templ` *(new)* · `internal/views/admin.templ` (nav) · `internal/handlers/router.go` · `internal/i18n/locales/{ar,en,fr}.json`

**Goal.** ⇒ **Usable here.** An admin creates a subject area and files
categories under it, without a deploy.

**Steps.**
0. **Migration `0035_categories_require_a_domain.sql`** — `ALTER TABLE categories ALTER COLUMN domain_id DROP DEFAULT;`, landing *in the same branch as the form that names one*. The default existed only to keep writers working between 0034 and this screen; past it, a default would hide a form that forgot the field, and filing a new category under Islamic silently is worse than refusing it. The planned API-keys migration becomes **0036**.
1. `admin_domains.go` mirroring `admin_categories.go`: list, form (new and edit), save, action (retire/restore), delete. Every mutating path writes an audit entry.
2. Six routes in `router.go`, registered beside the category block and in the same order — list, new, `{id}/edit`, save, `{id}/delete`, `{id}/{action}` — with `{id}/{action}` **last**, or it swallows the others. All six behind `RequireAdmin` (D11); the category routes keep `RequireModerator`.
3. A domain select on the category form, listing active domains only, required.
4. About twenty keys in all three catalogues — the i18n gate fails both directions, so a missing or orphan key fails the build.

**Done when.**
- [x] A moderator can still edit a category and is refused a domain — 404, not 403, on all eight domain paths, and nothing they post changes a row
- [x] Audit entries for create, update, retire, restore, delete — asserted by reading the trail back, not by trusting the call
- [x] The keys exist in all three catalogues — 53 of them, composed from the reviewed category wording; both catalogue gates green
- [x] A domain holding categories cannot be deleted; nor a category holding questions — refused in the screen, in the repository and by the key
- [x] ⇒ An admin can create a subject area and add categories under it, with nothing hardcoded — walked live against the stack: created, listed in all three languages, a category filed under it, the delete refused while it held one, then both removed so no demo content was left behind
- [x] **The nav offers a moderator only what they can reach.** `/admin/domains`, `/admin/users` and `/admin/audit` all answer 404 for a moderator, and a link that 404s on click hands back exactly what the 404-not-403 rule withholds. The first of those three was new; the other two were not, and all three are gated now
- [x] **With no active subject area the category form says so** rather than refusing every submission on a field with nothing in it — a moderator cannot create a domain, so the dead end had no exit
- [x] **Editing a category whose domain has been retired does not move it.** The form offers active domains; a category already filed under a retired one keeps that option, marked. Without it the select held no option for the category's own domain, the browser preselected the first entry, and saving a name correction relocated the category silently. Found by review, fixed, and the test fails without the fix

**Document.** [ARCHITECTURE.md §5.1](ARCHITECTURE.md) and §5.4 — the routes
stop being a projection, `admin_domains` joins the handler list.

---

## Prompt 6e — Player surfaces

**Branch** `taxonomy/player` · **After** 6d · **Effort** 0.75 d
**Touches** `internal/views/play.templ` · `internal/views/home.templ` · `internal/handlers/game.go` · `internal/repository/content.go` (availability shape)

**Goal.** A player chooses a subject area, then a category inside it — or the
whole area. The largest piece, and why this prompt precedes Alpine.

**Steps.**
1. Setup gains a domain row above the category row. One domain means the row is a single choice and must not read as a decision.
2. The home grid groups by domain, in `sort_order`.
3. Availability gains the domain axis: a whole-domain round needs a count per domain-and-difficulty, not only per category.
4. A whole-domain round writes `game_sessions.domain_id`; the result and history screens read it back and show the domain name where there is no category.

**Done when.**
- [x] Setup works in Arabic with more than one subject area — the accessibility walk covers `/play` in both writing directions, and the list is one radio group so it reflows as a single column. *The 320 px half needs a browser and a person; the markup half is what a test can hold*
- [x] A whole-domain round records its domain and reads back correctly — and records **no** category, so the two can never disagree; every question it drew is checked to be inside the area it named
- [x] A round with no category shows the subject area's name and icon, not "all categories" — which would have claimed it could have asked about anything in the bank

---

## Prompt 6f — Remaining surfaces

**Branch** `taxonomy/surfaces` · **After** 6e · **Effort** 0.25 d
**Touches** `internal/handlers/admin_questions.go` · `internal/service/import.go` · `internal/handlers/support.go` · `internal/handlers/admin_overview.go` · `cmd/sirajctl`

**Goal.** Every screen that names a category can name its domain.

**Steps.** Question filter and author form · coverage table · accuracy
breakdown · an optional `domain` column in the importer, absent meaning "as
before" · ticket grouping · the integrity sweep · the CLI count.

**Done when.**
- [x] The import rejects a row whose category is not in the named domain, and behaves as before when the column is absent — `domain` is an *optional* column, so a file written before the taxonomy had two levels parses unchanged, and the refusal names which subject area the category is actually in
- [x] The integrity sweep reports an inactive domain beside an inactive category — its own check, not folded into the one below it: a live question in a live category inside a retired subject area is reachable by nobody, and the sweep used to call that bank healthy

---

## Prompt 6g — What the review found outside the taxonomy ✅

**Branch** `taxonomy/aftermath` · **After** 6d · **Effort** 0.25 d

Five reviewers read the domain work. Everything they confirmed is fixed; these
three needed a decision rather than a patch, and the decision is recorded here.

- [x] **The English catalogue said "An Siraj game".** A find-and-replace had put the product name where the word *Islamic* belonged, in `app.tagline` and `landing.feature.learn.body`, while Arabic says «لعبة إسلامية» and French «Un jeu islamique». Restored to what `HEAD` and the other two languages say — the rename was to Sirāj, and "An Siraj game" is not that rename, it is a broken sentence on the first screen a visitor reads
- [x] **Counts no longer have to inflect.** `%d domains` read "1 domains" on a fresh install, and Arabic needs six plural forms to be right. The four count strings became a label and a number — "Subject areas: 3", «المجالات: ٣» — which agrees in every language without the catalogue learning plural rules. Both levels changed, because fixing one and not the other is worse than fixing neither
- [x] **The landing counter now counts what can actually be drawn.** `TotalQuestions` carried neither taxonomy condition, so a bank with a retired subject area in it advertised questions no visitor could ever be asked. The same sweep found and fixed `PlatformStats`; the remaining `FROM questions` reads are match, set and comment queries working from ids that are already chosen, which is a different question

---

## Prompt 7a — The credential

**Branch** `stack/public-api-keys` · **After** 6f · **Effort** 0.5 d
**Touches** `internal/database/migrations/0036_api_keys.sql` *(new)* · `internal/handlers/api/keys.go` *(new)* · `internal/repository/apikeys.go` *(new)* · `cmd/sirajctl`

**Goal.** A credential that is not a browser session, mintable and revocable
from the CLI.

```sql
-- An external consumer cannot present a session cookie, and every mutating
-- route pairs the cookie with a token it has no way to obtain. This is the one
-- credential in the project that is not a browser session.
CREATE TABLE api_keys (
    id serial PRIMARY KEY,
    label text NOT NULL,
    -- Only the hash. Shown once at mint, never recoverable — the rule the
    -- password column already follows.
    key_hash bytea NOT NULL UNIQUE,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at timestamptz
);
CREATE INDEX api_keys_live_idx ON api_keys (key_hash) WHERE revoked_at IS NULL;
```
*Proven here:* applies clean against a schema copy, partial index present.

**Steps.**
1. The migration above.
2. `handlers/api/keys.go`: read the bearer header, hash, look up a live key, compare in constant time, record `last_used_at` without blocking the response. Missing header ⇒ 401; revoked ⇒ 403.
3. `sirajctl apikey new|list|revoke`. `new` prints the key once and stores only the hash.

**Done when.**
- [x] A minted key authenticates; no header 401; a revoked key 403 — and an *unknown* key answers exactly as no key does, so the endpoint cannot be used to learn which keys exist
- [x] The plaintext key appears once, at mint, and is never logged or recoverable — only its SHA-256 is stored, and nothing reads it back
- [x] Last-used moves on a successful call, and a failed write of it is logged rather than turned into a refusal
- [x] [ARCHITECTURE.md §2.7](ARCHITECTURE.md) — `api_keys` stops being *specified*

---

## Prompt 7b — The endpoint and its contract

**Branch** `stack/public-api` · **After** 7a · **Effort** 0.75 d
**Touches** `internal/handlers/api/v1.go` *(new)* · `internal/handlers/router.go` · `internal/repository/content.go` · `docs/api.md` *(new)* · `api/openapi.yaml` *(new)* · `internal/service/writelimit.go`

**Goal.** One read-only, credentialed, versioned JSON endpoint that inherits
the filter set rather than reinventing it (D2).

```
GET /api/v1/questions?locale=en&category=quran&difficulty=2&limit=50&cursor=…
Authorization: Bearer …

200 { "data": [ { id, category{slug,name,icon}, domain{…}, difficulty, points,
                  prompt, choices, correct_index, explanation, locale } ],
      "next_cursor": "…", "total": 2163 }
401 unauthorized · 403 revoked · 429 rate limited
```

**Steps.**
1. Mount `/api/v1/*` above the session layer, beside the health probe — no cookie read, no cross-site token.
2. The read goes through the repository and carries the **five** filter conditions. A held translation is absent by construction, not by a flag on this path.
3. Cursor pagination with a server-side cap on `limit`; the rate limiter keyed on the key, not the peer address.
4. `docs/api.md` in prose and `api/openapi.yaml` as the machine-readable contract. The directory `docs/` is born here; prompt 10b moves the rest in.
5. **Settle the open decision first:** whether the filter gains a `domain` parameter beside `category`. The response carries the domain either way.

**Done when.**
- [x] A minted key returns questions — walked live: 401 without a header, a page with one, filters on category, subject area and difficulty, 400 on a locale nobody ships
- [x] A seeded held translation is absent from every response — the same question is served in the language it was reviewed in and withheld in the one awaiting review
- [x] `docs/api.md` and `api/openapi.yaml` exist and match the handler
- [x] The question shape carries its domain as well as its category, from its first version — and a `domain` filter beside `category`, because a consumer that can see a field but not filter on it asks immediately, and adding it later is a version decision

**Risk retired.** The endpoint serving unreviewed content: it reuses the filter
set, and a test seeds a held translation then asserts its absence.

---

## Prompt 7c — The two interface endpoints move

**Branch** `stack/api-prefix` · **After** 7b · **Effort** 0.25 d
**Touches** `internal/handlers/router.go` · the two client call sites · `internal/handlers/threads_test.go` · `internal/handlers/rules_test.go` · `internal/handlers/qa_test.go` · `scripts/smoke.sh`

**Goal.** One prefix stops meaning two opposite things
([ARCHITECTURE.md §4](ARCHITECTURE.md) states the clash).

**Steps.**
1. **Decide the new prefix in this branch** — it is recorded nowhere yet. The one constraint: both must stay inside the session chain, so they cannot live under the prefix the public group mounts above it.
2. Move both route registrations in `router.go`.
3. Follow all callers: the panel's counts fetch and the picker's people fetch in the client sources, the three test call sites, and the `counts api` assertion in `scripts/smoke.sh`.

**Done when.**
- [x] They are `/ui/people` and `/ui/counts` now: the interface fetching for itself, inside the session chain. A test asserts neither surface answers the other's credential
- [x] Every caller follows — client, three tests, smoke, and one comment — and a grep for the old paths finds nothing

**Document.** [ARCHITECTURE.md §4](ARCHITECTURE.md) and §5.1 — the paragraph
describing the clash goes, the route row takes the new names.

---

## Prompt 8a — Teach the class checker

**Branch** `stack/alpine-prelude` · **After** 7c · **Effort** 0.5 d
**Touches** `internal/views/styles_test.go`

**Goal.** The class checker keeps working when a class moves into a binding
attribute. It matches `class="…"` only ([STACK.md §2.4](STACK.md)), so a class a
conversion moves into a binding attribute stops being checked — silent, not red.

**Done when.**
- [x] The checker reads the binding attributes too — `:class` and `x-bind:class`, pulling the quoted literals out of an expression and leaving what it cannot read alone, because a checker that reports what it cannot read is one people learn to ignore
- [x] Re-run before any conversion: **2 385 plain class names checked, 0 binding attributes so far** — the count can only rise as components convert, and the checker has its own test with five cases so a conversion cannot quietly take a class out of scope
- [x] Nothing to prune: the stale-allowlist check runs as its own test and passes after every conversion so far

---

## Prompt 8b — Twenty conversions, twenty commits

**Branch** `stack/alpine-<name>`, one per component · **After** 8a · **Effort** 3.5 d

**The recipe, identical each time.** Find the closure **by name** — line numbers
drift from prompt 6 onward → write it as a policy-safe registration, state in
the returned object → register it by name in `web/src/js/alpine.js` → rewrite
the template, keeping every class, id, label and relationship exactly → delete
the closure, leave the shared helpers → gate on the full suite, the asset build,
then the accessibility suite specifically.

**Not optional:** one component per commit, template and component together.
And the two team-minimum constants stay greppable wherever they land — they are
carried into the component file verbatim and the test greps the concatenated
sources.

Smallest first, so the recipe is proven on the smallest widget before the
largest. The sizes that order this list are [STACK.md §2.3](STACK.md).

**Twenty was an estimate made before anybody read how each one attaches.**
Eight of them listen on `document` or `window` rather than owning an element:
converting those would mean an `x-data` on every form and every table in 31
templates, and behaviour that stops working for anything rendered outside a
scope. That is the boundary [STACK.md §3](STACK.md) already draws — Alpine owns
local state, and a delegated listener is not local state — so they stay
modules, which is a decision rather than an omission.

**Converted — one element's state each**

- [x] matchFormat · [x] ticketKind (+ `ticketPane`) · [x] questionSource (as `sourcePick` + `sourcePane`) · [x] cannedReplies (as `cannedReply`) · [x] passwordReveal
- [x] questionNote · [x] colourField · [x] authoredQuestions · [x] questionRating (+ `ratingStar`, because a star has to be able to ask whether it is lit and the policy-safe build binds to a name, not a call)
- [ ] **roundSetup** — the last one, and the richest: it reads the availability table and disables ten controls. Under the policy-safe build each of those becomes a named getter, which is a conversion worth doing carefully rather than quickly

**Staying plain modules — they attach to the document, not to an element**

- [x] busyButtons · confirmForms · dropdowns · sheets · bulkSelect · photoPreview · whoIsHere · messagePanel · sourceGroup — each with its reason beside it in [STACK.md §2.3](STACK.md). `picker` joins them: its work is fetching and injecting rows, which is network code

**Two gates landed with the first conversion**, so the rest are mechanical:
every directive must *name* something rather than evaluate it (the policy-safe
build evaluates nothing, and an expression it cannot read simply does nothing —
silently), and every component a template summons must be registered, with no
registration nothing summons.

Smoke after every third.

**Risks retired.** A conversion dropping a name, role or relationship — the
accessibility suite runs after every one of the twenty. The two team-minimum
constants drifting — the concatenated-sources test still sees them.

**Rollback.** One widget. That is the whole reason for one component per commit.

---

## Prompt 9a — Spacing and type scales

**Branch** `stack/design-tokens` · **After** 3 · **Effort** 0.5 d
**Touches** `web/src/css/app.css` · the templates holding hardcoded gaps

**Goal.** A repeating value is expressible only as a token. The hardcoded gap
values and the missing type scale are counted in
[ARCHITECTURE.md §8.1](ARCHITECTURE.md).

**Done when.**
- [x] A spacing scale and a type scale exist as custom properties — nine spacing steps and six type steps, **measured rather than invented**: they are the values the markup was already using, counted
- [x] The hardcoded gap count is measured before and after: **89 `gap:` literals → 2**, and those two are a 1px hairline, which is a literal used once

---

## Prompt 9b — Eleven breakpoints become four

**Branch** `stack/breakpoints` · **After** 9a · **Effort** 0.5 d
**Touches** `web/src/css/app.css`

**Goal.** The four named ranges in [ARCHITECTURE.md §8.2](ARCHITECTURE.md)
replace nine `max-width` and two `min-width` widths added one at a time.

**Done when.**
- [x] Every query uses one of the four ranges: **ten distinct widths → 360 / 560 / 860 (and its 861 companion)**. The widths are also tokens now, to be read beside the queries — a custom property cannot be used inside a media query, so the numbers are spelled twice and that is said where they are defined
- [ ] **A visual pass at each of the four ranges, in both writing directions.** Merging 480px into 560px and 720px into 860px changes when those rules apply, and no test in this repository can see a layout — the markup checks pass, the pixels need a person

---

## Prompt 9c — Inline styles down

**Branch** `stack/inline-styles` · **After** 9b · **Effort** 0.5 d
**Touches** the templates holding the inline style attributes counted in [ARCHITECTURE.md §8.1](ARCHITECTURE.md)

**Goal.** An inline style must be dynamic; a fixed gap is not. Retiring them is
what would eventually let the style policy tighten.

**Done when.**
- [x] The inline-style count is measured before and after: **224 → 153**. The 71 that went were the repeats — ten shapes appearing five times or more — folded into named classes beside the scales they use
- [ ] **The 153 survivors.** Many are one-offs that are genuinely layout rather than data; the ones that are dynamic (a colour per row, a width from a score) are the only ones that may stay. Until the count is zero or every survivor is dynamic, the style policy keeps `'unsafe-inline'`

---

## Prompt 10a — Stop the test databases leaking

**Branch** `stack/test-db-sweep` · **After** nothing · **Effort** 0.25 d
**Touches** the test harness's setup helper

**Goal.** End the leak counted in [ARCHITECTURE.md §12](ARCHITECTURE.md): the
harness drops its database in a deferred call, and a kill signal skips it.

**Done when.**
- [x] A sweep at suite start drops anything carrying the harness's prefix — and skips any database something is still connected to, because that is another run in progress rather than a leak
- [x] The leak count is measured before and after: **46 before, 0 after**. The sweep says how many it took, because a sweep that reports nothing looks identical to one that never ran

---

## Prompt 10b — Documents into `docs/`

**Branch** `stack/docs-layout` · **After** 9c · **Effort** 0.25 d
**Touches** `ARCHITECTURE.md` `STACK.md` `TODO.md` `DEPLOY.md` → `docs/` · `README.md` · `.dockerignore`

**Done when.**
- [x] The four long-form documents sit in `docs/`, `README.md` stays at the root, and every link between them resolves
- [x] The build-context exclusions collapse to one `docs/` entry, with `*.md` kept for the two that stay at the root

Last but for 10c, because every earlier prompt is still rewriting these files.

---

## Prompt 10c — Dockerfile and compose into `deploy/`

**Branch** `stack/deploy-layout` · **After** 10b · **Effort** 0.5 d
**Touches** `Dockerfile` `docker-compose.yml` `docker-compose.prod.yml` → `deploy/` · `Makefile` · `run.sh` · `README.md` · `DEPLOY.md`

**Goal.** Three root entries become one directory
([ARCHITECTURE.md §6.3](ARCHITECTURE.md)).

**Steps.** `git mv` to `deploy/Dockerfile`, `deploy/compose.yaml`,
`deploy/compose.prod.yaml`; repoint the four files that name them — the
`Makefile` database targets, `run.sh`, the README and the deployment guide;
leave `.dockerignore` at the root, where the build context is. No service,
container or credential value changes.

**Done when.**
- [x] `docker compose -f deploy/compose.yaml up -d` brings up the stack and **smoke passes 267/0 against it**; the overlay still refuses to start unconfigured
- [x] `make db-up`, `db-down`, `db-reset`, `db-shell`, `./run.sh` and both CI jobs carry the path
- [x] [STACK.md §6](STACK.md) carries the new names
- [x] **The project name is pinned.** Compose derives it from the directory the file sits in, so the move silently renamed the project from `islamic-game` to `deploy` — new volumes, new network, and a container-name clash with the stack already running. `name: siraj` in the base file: a deployment's identity must not depend on where its description is filed

---

## Prompt 11a — README and deployment guide

**Branch** `stack/docs-readme` · **After** 10c · **Effort** 0.25 d

**Done when.**
- [x] The README's stack, deployment and policy paragraphs match reality, and it still answers *what is this, how do I run it*: every command in it was run, the deployment lines carry `deploy/`, and the public endpoint has its own short section
- [x] **The deployment guide** carries `deploy/`, the overlay instead of the profile, a section on the assets (built in a stage *before* the compiler, because the tree is embedded at compile time) and one on the endpoint's credential — plus three new lines on the pre-deploy checklist

---

## Prompt 11b — Reconcile the three documents

**Branch** `stack/docs-reconcile` · **After** 11a · **Effort** 0.25 d

Every prompt corrects the one document it made wrong. This is the sweep that
checks they agree with each other and with the tree.

**Done when.**
- [x] [ARCHITECTURE.md §12](ARCHITECTURE.md) weaknesses updated — the leaked test databases and the cascading category delete are both closed, and the taxonomy row now says what is left rather than what was never built
- [x] [STACK.md §1](STACK.md) moves every row it can from *target* to *today*: the client, the stylesheet, the assets, external access, the taxonomy, first run and production have all crossed over
- [x] Every count recounted from the tree: **135 routes · 37 tables / 36 migrations · 32 templates / 172 components · 961 keys ×3 · 337 tests / 63 files**, and each still appears in exactly one file
- [ ] **This file still carries what landed.** Every prompt keeps its ticked boxes, because the reasoning in them is the record of why the system is the way it is — collapsing them to one line each is the last thing to do, and it should be done by whoever reads them once more

---

## Verification

After every prompt: build, vet, the full suite against a real database, the
asset build. The standing gates are [STACK.md §7](STACK.md).

Four new tests, each guarding something this work can silently break:

1. **The production policy grants no evaluation exception** — built with a dev origin configured and production set, asserting the origin is absent. Prompt 3.
2. **The endpoint serves no held translation** — seed one awaiting review, request that language with a valid key, assert absence. Prompt 7b.
3. **A clean clone comes up** — scripted in CI; the requirement most likely to rot and the only one no unit test covers. Prompt 4.
4. **A retired domain is neither drawn nor counted** — the assertion that already exists one level down for categories, which is why the draw and the availability count are asserted together. Prompt 6b.

Plus the net already in place: the accessibility suite after every conversion in
8b.

---

## Already landed

A summary, not a tracked list.

- [x] Category admin screens: routes, repository writes, screens, audit, translations, seven tests
- [x] The three draw queries require an active category — a retired one no longer feeds every round
- [x] Two empty directories and a stray 14 MB binary removed; the root one ignored so it cannot return
- [x] Two template components with no caller removed, output regenerated
- [x] The database renamed to `siraj-db` across all eleven files that name it
