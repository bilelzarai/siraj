# TODO — messages

Everything else is closed and lives in [TODO-done.md](TODO-done.md). This list
was one screen: the four bugs I could prove, the refactor they ask for, and the
shape you described.

**All twenty-one are done** (2026-09-29). 1–14 were the messages screen;
15–21 came out of your screenshots afterwards — two of those were regressions
I had introduced, and I have said so where they are. Each item is ticked with
what it took.
`go test ./...` is green, `scripts/smoke.sh` passes 262/262, and the i18n check
passes with three locales.

Three of the fixes are guarded by tests that fail against the old code — I
checked each by putting the bug back:

- `TestPollingDoesNotMarkAnythingRead` — fails when `/poll` marks.
- `TestOnlyOnePlaceMarksAMessageRead` — fails the moment a second caller of
  `MarkRead` appears anywhere in the handlers.
- `TestReadingTwiceChangesNothingTheSecondTime` — pins the row count the
  publish is gated on, and that the receipts endpoint cannot mark.

**How we work through it**

- One item at a time, smallest blast radius first.
- Every fix lands with a test that fails before it and passes after. The
  finding says what should break; if nothing breaks, the finding was wrong.
- No refactor rides along with a behaviour change. Either the diff moves code
  or it changes what the code does, never both.
- A fix that deletes code beats a fix that adds code.
- Severity is about consequence, not effort: **🔴 data or load**,
  **🟠 wrong behaviour**, **🟡 wear and tear**.

---

## A — Bugs

Confirmed against the code and the database, not guessed.

### 1. `[x]` 🔴 Two open threads talk to each other forever

This is the real cause of "it says he read it", and it is worse than a wrong
tick. Follow one message:

1. B's browser receives `message.read` over the stream.
2. `siraj:read` runs `syncReceipts()`
   ([app.js:1056](static/js/app.js#L1056)), which fetches
   `/messages/{id}/poll?after=0` — the whole thread.
3. `PollMessages` sees `len(messages) > 0`, calls `MarkRead`, and publishes
   `message.read` to the other side ([messages.go:270-277](internal/handlers/messages.go#L270-L277)).
4. A's browser receives `message.read`. Go to 1.

It never stops while two people have the same thread open. Every hop is two
`Conversation` lookups, an 80-row `SELECT`, an `UPDATE` and an SSE frame — for
nothing, because after the first pass there is never a row left to mark. It
also means anything either side sends is stamped read within the same second,
which is exactly what the table shows: id 113 sent 12:57, read 12:57.

**Fix:** reading and announcing are two different things and neither belongs
in a fetch. See items 5 and 6 — this bug is what they are for. A receipt
refresh must not be able to produce a receipt event.

Test: two sessions, both with the thread open, no traffic. Count the
`message.read` events over ten seconds — it must be zero.

**Done —** `syncReceipts()` now calls `GET /{id}/receipts`, which returns `{id, read}` for your own messages and cannot write. `/poll` no longer marks or publishes. The publish is gated on `MarkRead`'s row count, so an announcement that changes nothing is never sent — the loop has no edge left to travel along.

### 2. `[x]` 🟠 Reading is inferred from the page existing

Even with item 1 fixed, nobody has to look at anything for a message to count
as read. `MarkRead` runs on every render of the thread
([messages.go:74](internal/handlers/messages.go#L74)) — a reload, a
back-button, a tab restored at boot — and on any poll that returns rows
([messages.go:271](internal/handlers/messages.go#L271)).

So a thread open in a second window, or on a phone in a pocket, reports back
as read. Which is what you have when you test with two browsers side by side.

**Fix:** reading is an act.

- Mark read only while the page is **visible and focused**, and re-mark on
  focus — not on load, not on poll.
- The client says "I am looking" explicitly (item 5). Fetching stays a read.

Test: open a thread, blur the window, have the other side send — `read_at`
stays null until focus comes back.

**Done —** Marking moved to `POST /messages/{id}/read`, sent by the client only when the page is visible *and* focused, and again on focus. A browser with no script still gets the old behaviour — the server falls back to marking on render when the `js=1` cookie is absent, so curl, readers and scripting-off browsers are not left with an unread count that never clears.

### 3. `[x]` 🟠 Searching a thread marks it read and loses your place

[messages.go:63-77](internal/handlers/messages.go#L63-L77): a search sets
`firstUnread = 0` and then falls straight into `MarkRead`. So looking for
something somebody said last week silently consumes everything you had not
read yet, and drops the divider that would have shown you where you were.

**Fix:** searching is reading a *list*, not reading the *messages*. No
`MarkRead` on a search request, and the divider survives.

**Done —** `?find=` is the thread search now, and the render marks nothing when it is set. The divider survives it.

### 4. `[x]` 🟠 A new message has no visible sign

The notification is written — eight unread `message.new` rows sitting there
now — and the count already reaches the page: `notifications` is in the counts
payload at [messages.go:346](internal/handlers/messages.go#L346). The problem
is the one place it renders: [layout.templ:249](internal/views/layout.templ#L249),
*inside the account dropdown*, a menu that is closed. You have to go looking
for the thing whose job is to stop you having to look.

Three small gaps:

- Nothing on the avatar button. A dot when `Notifications > 0`.
- [app.js:1245](static/js/app.js#L1245) repaints /messages, /challenges,
  /friends, /support and /admin/support live — `/notifications` is not in the
  list, so even the hidden badge is stale until reload.
- No toast on `message.new` when you are elsewhere in the app. The event is
  published and already subscribed; it just never surfaces.

**Done —** A red dot on the avatar button, server-rendered from `c.Notifications` and repainted live by `paintAvatarDot`; `/notifications` added to the badge repaint; and a toast on `message.new` when the thread it belongs to is not the one on screen.

---

## B — Refactor

The bugs above are all the same mistake in different clothes: one endpoint
that fetches, writes and broadcasts, so no caller can do one without the
others.
all composent 

### 5. `[x]` 🟡 `/poll` does three jobs; split it

`PollMessages` fetches messages, marks them read, and announces the read. That
is why a receipt refresh writes to the database (item 1) and why a background
tab reports as read (item 2).

- `GET /messages/{id}/poll` — read-only. No `UPDATE`, no publish. Ever.
- `POST /messages/{id}/read` — the act of reading, sent by the client when the
  page is visible and focused and there is something unread. This is the only
  place that marks and the only place that publishes.

It also loads the conversation twice per call
([messages.go:250](internal/handlers/messages.go#L250) and
[:272](internal/handlers/messages.go#L272)); `WithdrawMessage` does the same
([:214](internal/handlers/messages.go#L214), [:232](internal/handlers/messages.go#L232)).
Load it once.

**Done —** `GET /{id}/poll` reads (and now pages backwards). `POST /{id}/read` marks and announces, and is the only thing that does. `WithdrawMessage` and `PollMessages` load the conversation once each instead of twice.

### 6. `[x]` 🟡 `MarkRead` cannot say whether it did anything

[social.go:502](internal/repository/social.go#L502) throws the command tag
away and returns `error`. So the caller cannot tell "I marked four messages"
from "there was nothing to mark" — which is precisely why the publish in item
1 fires unconditionally.

Return the row count. Publish only when it is greater than zero. That one
change breaks the loop even before the endpoints are split.

**Done —** `MarkRead` returns `(int64, error)`. Every caller goes through `markThreadRead`, which publishes only when the count is above zero.

### 7. `[x]` 🟡 A receipt costs a whole thread

`syncReceipts()` refetches up to 80 messages and re-renders every bubble to
change one tick. Once item 5 lands, let the `message.read` event carry the
conversation and let the client flip the ticks it already has on screen — no
request at all — or add a receipts-only endpoint returning `{id, read}`.

**Done —** `GET /{id}/receipts` returns the ticks alone. A read event costs one small query instead of eighty rows and a full re-render.

### 8. `[x]` 🟡 Only the last eighty messages exist

[social.go:405](internal/repository/social.go#L405) with `afterID = 0` returns
the newest 80 — correct, but there is no way to ask for the eighty before
those. A long thread's history is simply unreachable, and no route exists for
it ([router.go:99-106](internal/handlers/router.go#L99-L106)).

Add `?before=` and load older on scroll-to-top.

**Done —** `MessagesBefore` plus `?before=` on the poll. The button appears when the first page is full, and scrolling to the top loads the next page while holding the reader's position.

---

## C — The screen

What you described, with the shape I think it should take. Strike anything you
disagree with before I build it.

### 9. `[x]` 🟠 You can only write to people you have already written to

The list is conversations, so a friend you have never messaged is not on the
screen. The ✚ ([messages.templ:30](internal/views/messages.templ#L30)) sends
you to `/friends?tab=all` — away from messages, to a screen about friendship,
to come back.

**Fix:** friends with no thread yet belong in the same list, below the
conversations, dimmed, with no preview line. Clicking one opens an empty
thread. No round trip, no second screen.

**Done —** `friendsWithoutThread` puts them under a **Friends** heading below the conversations, dimmed, linking straight to `/messages/with/{username}`. The ✚ that left the screen is gone.

### 10. `[x]` 🟡 A search field at the top of the side panel

One field over both: friends and conversations. Typing filters the list under
it; a friend with no thread shows the same as in item 9. Each result carries
its actions — message, challenge — so the panel is where you start things, not
only where you resume them.

**Done —** One field at the top of the panel over three things: display name, username, and message bodies (`SearchConversations`). The matching line becomes the preview.

### 11. `[x]` 🟡 The thread header repeats what the panel already says

[messages.templ:87](internal/views/messages.templ#L87) names the person you
are talking to, directly beside a highlighted row naming the person you are
talking to.

**Fix:** delete the header. Identity stays in the side-panel row, and a ⋮ on
that row carries what the header held plus what you asked for — challenge,
delete conversation, block, mute. None of the last three exist yet: there is
no route for deleting or muting a conversation, so this item builds them.

Keep the back arrow on mobile, where the list is hidden
([app.css:1875](static/css/app.css#L1875)) and the header is the only way out.

**Done —** The header is gone on desktop. `conversationRow` carries a ⋮ with challenge, profile, search, mute and delete. Mute and delete are new: `conversation_state` (migration 0019) holds `cleared_at` and `muted_at` per participant, so deleting clears your copy and leaves theirs, and muting silences the notification without touching the live event. On mobile the row collapses to a back link with the name, because there the panel is not on screen.

### 12. `[x]` 🟡 The in-thread search is a permanent bar in the wrong place

[messages.templ:110](internal/views/messages.templ#L110) sits between the
header and the log, always, taking a row of height from the conversation to
offer something almost nobody is doing right now.

**Fix:** move it into the ⋮ from item 11. Pressing it opens a compact field
over the log with a match count and next/previous; Escape closes it. It costs
nothing when unused.

While it is being rebuilt: results come back newest-first
([social.go:473](internal/repository/social.go#L473)) into a thread that reads
oldest-first, so the order flips under you. Pick one.

**Done —** Into the ⋮, opening as a `:target` so it still works with no script, closing on Escape. `SearchMessages` now returns results in the order the thread reads instead of reversing it.

### 13. `[x]` 🟡 Pin the compose box; scroll only the log

Desktop already does this — `.chat` is a fixed `calc(100dvh - var(--nav-h))`
and `.chat__log` is the only thing that scrolls
([app.css:1377](static/css/app.css#L1377)). What eats it is everything stacked
above the log in that fixed column: the header (11) and the search bar (12)
both take permanent rows, and `100dvh` is a lie on a phone, where the keyboard
and the URL bar move it.

So most of this falls out of 11 and 12. What is left is testing it on a phone
with the keyboard open and switching the unit to `svh`/`dvh` with a real
fallback.

**Done —** `svh` before `dvh` on both breakpoints, and the two permanent rows above the log are gone with items 11 and 12.

### 14. `[x]` 🟡 The small things that are genuinely missing

Checked one by one against the code — this is what is actually absent:

- **No day separators.** `.chat__date` exists in the stylesheet
  ([app.css:1495](static/css/app.css#L1495)) and is rendered nowhere. Either
  print "today"/"yesterday"/a date between days, or delete the dead rule.
- **Sending has no pending state.** The bubble only appears once the POST
  returns ([app.js:1014-1017](static/js/app.js#L1014-L1017)); on a slow
  connection nothing happens at all. Show it immediately, greyed, and settle
  it on the response.
- **No global search.** You can search inside a thread but not across
  conversations — the panel search in item 10 should cover message bodies too.

**Done —** Day separators are rendered by `threadRows`/`dayLabel` and mirrored client-side by `ensureDay`, so `.chat__date` is no longer dead. A pending bubble appears the moment you press send and settles — or turns red — on the answer. Global search came with item 10.

---

## D — From your screenshots (2026-09-29, second pass)

Four of these are mine to answer for: 15 and 16 are regressions I introduced in
the work above, and 17 and 18 are things I built badly rather than things that
were already wrong.

### 15. `[x]` 🟠 The unread dot is on the language switcher

`paintAvatarDot` looks up `$("[data-dropdown] [data-dropdown-trigger]")`, which
is the *first* dropdown trigger in the document — and that is the globe, not
the avatar. So a new message marks the language button.

The server-rendered dot is in the right place; only the live repaint is wrong,
which is why it moves after the first refresh.

**Done —** The button is found by name now — `[data-account-button]` — instead of by being first. The server-rendered dot was always right; only the repaint moved it.

### 16. `[x]` 🟠 The send button drops below the writing box

Mine, from item 14: `.chat__compose` got `flex-wrap: wrap` so the reply bar
could take a row of its own, and the textarea has no `flex` of its own — so it
claims its default width and pushes the button onto a second line.

**Done —** `.chat__compose .textarea` gets `flex: 1 1 auto; min-width: 0` and the buttons `flex: 0 0 auto`, so the wrap that gives the reply bar its own row no longer costs the send button its place.

### 17. `[x]` 🟡 The ⋮ floats away from the message

It is centred against the bubble and sits a gap away from it. It should hang
off the end of the message it belongs to, aligned with the last line, and it
should be on the same side in both writing directions.

**Done —** `align-self: flex-end` with a 4px bottom margin, a fixed 22px width and a 2px row gap: it sits on the message's last line, against the bubble, on the same side in both directions.

### 18. `[x]` 🟡 The reply quote says "Them"

A quote naming the person you are talking to "Them" is worse than naming
nobody. It should carry their display name — the panel is already showing it.

The block itself is a grey slab with two lines of equal weight. It wants to
read as a quotation: a rule down the side, the name small and coloured, the
line itself quiet and clipped to one row.

**Done —** It carries the other person's display name — the panel is already showing it, so a quote that said "Them" was refusing to name the one person in the conversation. The block lost its grey slab for a 2px accent rule, a small coloured name and one clipped line of the quote.

## E — What the messages screen still cannot do

### 19. `[x]` 🟡 No emoji

There is no way to put one in a message except the system picker, which on a
desktop browser most people do not know exists.

A picker of its own, opening from the composer, grouped and searchable, and
carrying no third-party script — the whole point of the CSP is that there is
nothing to trust.

**Done —** A picker of its own: 364 emoji in eight groups, fetched from `/static/emoji.json` the first time it opens rather than shipped in every page, searchable across all groups at once, inserting at the cursor rather than at the end. The tabs are emoji, so there are no group names to translate. No third-party script — the CSP has nothing new to allow.

### 20. `[x]` 🟠 Nothing can be sent but text

No image, no file, no voice note. This is the largest of these by a distance:
it needs somewhere to put bytes, a table to describe them, a route that serves
them only to the two people in the conversation, limits on size and type, and
a composer that can hold an attachment before it is sent.

Voice notes additionally need the microphone, which the current
`Permissions-Policy` header switches off outright.

**Done —** Images, audio, PDFs and text files, up to `MAX_UPLOAD_MB` (8 by default), plus voice notes recorded in the browser through `MediaRecorder` and handed to the same file input a chosen file uses. Migration 0022 adds `attachments` and `messages.attachment_id`; bytes live under `UPLOAD_DIR`, sharded by the first two characters of the id. The type is decided by sniffing the file's own first bytes, never by the Content-Type the client sent — a script named `picture.png` is refused. `/files/{id}` serves only to the sender, the two people in the conversation, or anyone if it is a profile photo. Multipart bodies are now bounded by `MaxBytesReader`, which `ParseMultipartForm` alone never did, and `Permissions-Policy` allows `microphone=(self)` because nothing else here needed it and voice notes do.

### 21. `[x]` 🟡 A profile is a coloured square with a letter in it

The generated gradient is a good default and a poor ceiling. A photo, the same
upload path as item 20, shown everywhere an avatar is — the panel, the bubbles,
the leaderboard, the duel scoreboard.

**Done —** A photo, stored as `photo:<attachment-id>` in `avatar_seed`. That column is already selected by twenty-one queries and read by every avatar on the site, so tagging its value is what lets a photo appear in the panel, the bubbles, the leaderboard, the duel scoreboard and the admin table without threading a new column through all of them — `views.AvatarStyle` documents the tagging. The gradient stays underneath as what shows while the picture loads. Three screens that were drawing a gradient from the username rather than the seed were drawing the wrong avatar even before this, and now use the seed.

---

## Already right — leave alone

Things I listed yesterday as missing that are in fact built. Struck so we do
not rebuild them:

- **✓ / ✓✓ sent-versus-read is there**, server-side
  ([messages.templ:190-198](internal/views/messages.templ#L190-L198)) and in
  the live renderer ([app.js:899-906](static/js/app.js#L899-L906)). The ticks
  are right; what feeds them is not (items 1–2).
- **The unread divider is there** — `FirstUnread` and `.chat__since`
  ([messages.templ:139-143](internal/views/messages.templ#L139-L143)).
- **Enter sends, Shift+Enter breaks the line**
  ([app.js:985-991](static/js/app.js#L985-L991)).
- **Empty states are there**, for no conversations and for an empty thread.
- **A failed send is not silent** — the text is restored and a toast shown
  ([app.js:1019-1022](static/js/app.js#L1019-L1022)).
- **Withdraw works end to end**, tombstone and all, and the other side is told.
- **The poller already backs off** to 60 s while the stream is alive and stops
  entirely on a hidden tab.

---

## Standing rules (not tasks)

- **No fake or seeded data.** Content comes from the app, never from a seed
  file. `SEED_ON_START` stays `false`.
- **Content must survive** a refresh, a new session, a different machine, and
  a test run.
  - 2026-09-25: it did not. `scripts/reset-test-data.sh` deleted every
    question with `id >= 9000`, and an imported bank numbered with ten-digit
    ids is entirely above 9000 — so every smoke run wiped it. The id rule is
    gone; only the `ZZ `/`Smoke test` prompt markers are matched now, and the
    script prints how many questions remain.
- **Unreviewed machine text never reaches players.** Machine translations are
  written with `needs_review` and every player-facing query filters on it.
