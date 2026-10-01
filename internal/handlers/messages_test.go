package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// pair registers two people, makes them friends, and opens the thread between
// them. It answers with the conversation id and the two browsers.
func pair(t *testing.T, a, b *app, left, right string) string {
	t.Helper()
	a.register(left)
	b.register(right)

	if code, _ := a.post("/friends/request", url.Values{"username": {right}}); code != http.StatusSeeOther {
		t.Fatalf("friend request → %d", code)
	}
	if code, _ := b.post("/friends/accept", url.Values{"username": {left}}); code != http.StatusSeeOther {
		t.Fatalf("accept → %d", code)
	}

	res, err := a.client.Get(a.server.URL + "/messages/with/" + right)
	if err != nil {
		t.Fatalf("open thread: %v", err)
	}
	defer res.Body.Close()
	location := res.Header.Get("Location")
	if !strings.HasPrefix(location, "/messages/") {
		t.Fatalf("opening a thread went to %q", location)
	}
	return strings.TrimPrefix(location, "/messages/")
}

// The bug: reading was inferred from the page existing. A thread open in a
// window nobody is looking at — a second browser, a tab restored at boot —
// reported every arriving message as read, because fetching marked.
//
// Fetching is now a read and nothing else.
func TestPollingDoesNotMarkAnythingRead(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "pollsender", "pollreader")

	if code, _ := a.post("/messages/"+conv, url.Values{"body": {"Are you there"}}); code != http.StatusSeeOther {
		t.Fatalf("send → %d", code)
	}

	// The reader's browser fetches the thread, over and over, without anybody
	// ever saying they are looking at it.
	for i := 0; i < 3; i++ {
		if code, _ := b.get("/messages/" + conv + "/poll?after=0"); code != http.StatusOK {
			t.Fatalf("poll → %d", code)
		}
	}

	if got := unreadFor(t, a, conv); got != 1 {
		t.Errorf("after three polls the sender's message reports read=%v; polling must not mark", got == 0)
	}

	// Saying so does mark it.
	if code, _ := b.post("/messages/"+conv+"/read", url.Values{}); code != http.StatusOK {
		t.Fatalf("read → %d", code)
	}
	if got := unreadFor(t, a, conv); got != 0 {
		t.Errorf("%d of the sender's messages are still unread after the reader said they were looking", got)
	}
}

// The bug this is really about: a read that changed nothing was announced
// anyway, the other side refreshed its receipts by refetching the thread, that
// refetch marked and announced a read back, and it never stopped.
//
// Two guards, both here: marking reports what it did, and the receipts
// endpoint cannot mark.
func TestReadingTwiceChangesNothingTheSecondTime(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "loopsender", "loopreader")

	a.post("/messages/"+conv, url.Values{"body": {"first"}})

	user, err := a.repo.UserByUsername(t.Context(), "loopreader")
	if err != nil {
		t.Fatalf("load reader: %v", err)
	}
	n, err := a.repo.MarkRead(t.Context(), mustUUID(t, conv), user.ID)
	if err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if n != 1 {
		t.Fatalf("first read marked %d messages, want 1", n)
	}
	// The second pass has nothing to do, and has to say so — this is the
	// count the publish is gated on.
	if n, err := a.repo.MarkRead(t.Context(), mustUUID(t, conv), user.ID); err != nil || n != 0 {
		t.Errorf("reading an already-read thread marked %d messages (err %v), want 0", n, err)
	}

	// And the receipts endpoint, which the other side calls on every read
	// event, must not itself be able to mark anything.
	a.post("/messages/"+conv, url.Values{"body": {"second"}})
	if code, _ := b.get("/messages/" + conv + "/receipts"); code != http.StatusOK {
		t.Fatalf("receipts → %d", code)
	}
	if got := unreadFor(t, a, conv); got != 1 {
		t.Errorf("fetching receipts marked %d messages read", 1-got)
	}
}

// Looking for something somebody said is not reading the thread. It used to
// consume every unread message and delete the divider showing where you were.
func TestSearchingAThreadDoesNotMarkItRead(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "findsender", "findreader")

	a.post("/messages/"+conv, url.Values{"body": {"the battle of Badr"}})

	if code, _ := b.get("/messages/" + conv + "?find=badr"); code != http.StatusOK {
		t.Fatalf("search → %d", code)
	}
	if got := unreadFor(t, a, conv); got != 1 {
		t.Error("searching a thread marked it read")
	}
}

// A thread is not only its newest page. Without a way back there was no route
// to anything older, and the history was simply unreachable.
func TestTheThreadCanBeReadBackwards(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "pagesender", "pagereader")

	for i := 0; i < 5; i++ {
		a.post("/messages/"+conv, url.Values{"body": {"line " + string(rune('a'+i))}})
	}

	code, body := a.get("/messages/" + conv + "/poll?after=0")
	if code != http.StatusOK {
		t.Fatalf("poll → %d", code)
	}
	var page struct {
		Messages []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
			Day  string `json:"day"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("poll json: %v", err)
	}
	if len(page.Messages) != 5 {
		t.Fatalf("got %d messages, want 5", len(page.Messages))
	}
	if page.Messages[0].Day == "" {
		t.Error("a message arrived without the day it belongs to, so the client cannot draw separators")
	}

	third := page.Messages[2].ID
	code, body = a.get("/messages/" + conv + "/poll?before=" + itoa(third))
	if code != http.StatusOK {
		t.Fatalf("backwards poll → %d", code)
	}
	var older struct {
		Messages []struct {
			ID int64 `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &older); err != nil {
		t.Fatalf("older json: %v", err)
	}
	if len(older.Messages) != 2 {
		t.Fatalf("asking for what is before the third message gave %d, want 2", len(older.Messages))
	}
	if older.Messages[len(older.Messages)-1].ID >= third {
		t.Error("the older page contains the message it was anchored on")
	}
}

// Deleting a conversation is one person's decision. The other side is still in
// the middle of it, and taking their half away would leave them replying to
// nothing.
func TestDeletingAConversationOnlyClearsYourCopy(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "dropsender", "dropreader")

	a.post("/messages/"+conv, url.Values{"body": {"a sentence worth keeping"}})

	if code, _ := b.post("/messages/"+conv+"/delete", url.Values{}); code != http.StatusSeeOther {
		t.Fatalf("delete → %d", code)
	}

	_, mine := b.get("/messages/" + conv)
	if strings.Contains(mine, "a sentence worth keeping") {
		t.Error("the message is still in the thread of the person who deleted it")
	}
	_, theirs := a.get("/messages/" + conv)
	if !strings.Contains(theirs, "a sentence worth keeping") {
		t.Error("one person deleting the conversation took it from the other as well")
	}
}

// Friends you have never written to belong on the screen for writing to
// people. Sending you to the friends list to come back was a round trip
// through a screen about something else.
func TestTheInboxListsFriendsYouHaveNotWrittenTo(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	a.register("inboxme")
	b.register("inboxthem")

	a.post("/friends/request", url.Values{"username": {"inboxthem"}})
	b.post("/friends/accept", url.Values{"username": {"inboxme"}})

	_, body := a.get("/messages")
	if !strings.Contains(body, "/messages/with/inboxthem") {
		t.Error("a friend with no thread yet is not on the messages screen")
	}
}

// Two different acts that both read as "delete". Withdrawing takes a sentence
// out of the conversation and is the sender's alone; hiding takes it out of
// your own copy and is anybody's. Neither may do the other's job.
func TestHidingAMessageOnlyTakesItFromYourCopy(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "hidesender", "hidereader")

	a.post("/messages/"+conv, url.Values{"body": {"one for the record"}})
	a.post("/messages/"+conv, url.Values{"body": {"and one to bury"}})

	code, body := b.get("/messages/" + conv + "/poll?after=0")
	if code != http.StatusOK {
		t.Fatalf("poll → %d", code)
	}
	var page struct {
		Messages []struct {
			ID     int64  `json:"id"`
			Body   string `json:"body"`
			SentAt string `json:"sentAt"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("poll json: %v", err)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(page.Messages))
	}
	if page.Messages[0].SentAt == "" {
		t.Error("a message arrived with no timestamp, so the details panel has nothing to show")
	}

	buried := page.Messages[1].ID
	if code, _ := b.post("/messages/"+conv+"/m/"+itoa(buried)+"/hide", url.Values{}); code != http.StatusOK {
		t.Fatalf("hide → %d", code)
	}

	// It stops being unread news, the same way a withdrawn one does. Checked
	// before the thread is rendered, which is what marks it read.
	if n, err := b.repo.TotalUnread(t.Context(), userID(t, b, "hidereader")); err != nil || n != 1 {
		t.Errorf("unread = %d (err %v) after hiding one of two, want 1", n, err)
	}

	_, mine := b.get("/messages/" + conv)
	if strings.Contains(mine, "and one to bury") {
		t.Error("the message is still in the thread of the person who hid it")
	}
	if !strings.Contains(mine, "one for the record") {
		t.Error("hiding one message took the others with it")
	}
	_, theirs := a.get("/messages/" + conv)
	if !strings.Contains(theirs, "and one to bury") {
		t.Error("hiding a message took it from the other person as well")
	}

}

func userID(t *testing.T, a *app, username string) uuid.UUID {
	t.Helper()
	user, err := a.repo.UserByUsername(t.Context(), username)
	if err != nil {
		t.Fatalf("load %s: %v", username, err)
	}
	return user.ID
}

// A reply carries the line it answers, and may only quote one from its own
// thread — a crafted form must not be able to pull a sentence out of somebody
// else's conversation and show it here.
func TestAReplyQuotesOnlyItsOwnThread(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "quoteone", "quotetwo")

	a.post("/messages/"+conv, url.Values{"body": {"what time is iftar"}})

	_, body := a.get("/messages/" + conv + "/poll?after=0")
	var page struct {
		Messages []struct {
			ID int64 `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("poll json: %v", err)
	}
	asked := page.Messages[0].ID

	if code, _ := b.post("/messages/"+conv, url.Values{
		"body": {"just after sunset"}, "reply_to": {itoa(asked)},
	}); code != http.StatusSeeOther {
		t.Fatalf("reply → %d", code)
	}

	_, thread := a.get("/messages/" + conv)
	if !strings.Contains(thread, "bubble__quote") {
		t.Error("the reply arrived with no quote of what it answers")
	}

	// A message from a conversation this pair is not in cannot be quoted.
	c := newAppSharing(t, a)
	d := newAppSharing(t, a)
	other := pair(t, c, d, "quotethree", "quotefour")
	c.post("/messages/"+other, url.Values{"body": {"a private sentence"}})

	_, elsewhere := c.get("/messages/" + other + "/poll?after=0")
	var theirs struct {
		Messages []struct {
			ID int64 `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(elsewhere), &theirs); err != nil {
		t.Fatalf("poll json: %v", err)
	}

	b.post("/messages/"+conv, url.Values{
		"body": {"nice try"}, "reply_to": {itoa(theirs.Messages[0].ID)},
	})
	_, after := a.get("/messages/" + conv)
	if strings.Contains(after, "a private sentence") {
		t.Error("a reply quoted a message from a conversation it is not part of")
	}
}

func unreadFor(t *testing.T, sender *app, conv string) int {
	t.Helper()
	code, body := sender.get("/messages/" + conv + "/receipts")
	if code != http.StatusOK {
		t.Fatalf("receipts → %d", code)
	}
	var payload struct {
		Receipts []struct {
			ID   int64 `json:"id"`
			Read bool  `json:"read"`
		} `json:"receipts"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("receipts json: %v", err)
	}
	n := 0
	for _, r := range payload.Receipts {
		if !r.Read {
			n++
		}
	}
	return n
}

func mustUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("conversation id %q: %v", s, err)
	}
	return id
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// The invariant behind the loop: exactly one function marks a message read.
//
// The bug was not that marking was wrong, it was that marking was everywhere —
// in the page render, in the poll, and therefore in the receipt refresh that
// the poll's own announcement triggered. A source check is crude, but it is
// the only thing that fails when somebody adds a third caller.
func TestOnlyOnePlaceMarksAMessageRead(t *testing.T) {
	src, err := os.ReadFile("messages.go")
	if err != nil {
		t.Fatalf("read handler: %v", err)
	}
	callers := regexp.MustCompile(`(?m)^func \(h \*Handlers\) (\w+)|repo\.MarkRead\(`).
		FindAllStringSubmatch(string(src), -1)

	current := ""
	var marking []string
	for _, m := range callers {
		if m[1] != "" {
			current = m[1]
			continue
		}
		if current == "markThreadRead" {
			continue
		}
		marking = append(marking, current)
	}
	if len(marking) != 0 {
		t.Errorf("MarkRead is called from %v; it belongs in markThreadRead alone", marking)
	}
	if !strings.Contains(string(src), "func (h *Handlers) markThreadRead") {
		t.Error("markThreadRead is gone; the guard above no longer guards anything")
	}
}

// Reading a thread is what its "somebody wrote to you" notifications were
// pointing at. They used to outlive the read: the messages were stamped and
// the notification rows were not, so the bell went on counting news that was
// open on the screen in front of the reader, and only a visit to the
// notifications page — marking everything, from everywhere, read — cleared it.
func TestReadingAThreadSpendsItsNotifications(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "bellsender", "bellreader")

	reader, err := b.repo.UserByUsername(t.Context(), "bellreader")
	if err != nil {
		t.Fatalf("load reader: %v", err)
	}

	// Becoming friends is itself news, and it is news about somewhere else.
	before, err := b.repo.UnreadNotificationCount(t.Context(), reader.ID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	a.post("/messages/"+conv, url.Values{"body": {"salam"}})
	a.post("/messages/"+conv, url.Values{"body": {"are you there"}})

	during, err := b.repo.UnreadNotificationCount(t.Context(), reader.ID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if during != before+2 {
		t.Fatalf("two messages produced %d notifications, want 2", during-before)
	}

	code, body := b.postBody("/messages/"+conv+"/read", nil)
	if code != http.StatusOK {
		t.Fatalf("read → %d", code)
	}

	var said struct {
		Unread        int `json:"unread"`
		Notifications int `json:"notifications"`
	}
	if err := json.Unmarshal([]byte(body), &said); err != nil {
		t.Fatalf("read answered %q", body)
	}
	if said.Unread != 0 {
		t.Errorf("the read answered %d unread messages, want 0", said.Unread)
	}
	// The friendship news is about somewhere else, and survives.
	if said.Notifications != before {
		t.Errorf("the read answered %d notifications, want %d", said.Notifications, before)
	}

	after, err := b.repo.UnreadNotificationCount(t.Context(), reader.ID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if after != before {
		t.Errorf("%d of the thread's notifications outlived the read", after-before)
	}
}

// The panel beside the thread has to be able to redraw itself without the page
// being reloaded out from under a half-written message.
func TestTheSummaryCarriesThePanelsNumbers(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "sumsender", "sumreader")

	a.post("/messages/"+conv, url.Values{"body": {"the last thing said"}})

	code, body := b.get("/messages/summary")
	if code != http.StatusOK {
		t.Fatalf("summary → %d", code)
	}

	var said struct {
		Conversations []struct {
			ID      string `json:"id"`
			Unread  int    `json:"unread"`
			Preview string `json:"preview"`
			At      string `json:"at"`
		} `json:"conversations"`
		Unread int `json:"unread"`
	}
	if err := json.Unmarshal([]byte(body), &said); err != nil {
		t.Fatalf("summary answered %q", body)
	}
	if said.Unread != 1 {
		t.Errorf("the summary says %d unread, want 1", said.Unread)
	}

	var found bool
	for _, row := range said.Conversations {
		if row.ID != conv {
			continue
		}
		found = true
		if row.Unread != 1 {
			t.Errorf("the thread shows %d unread, want 1", row.Unread)
		}
		if row.Preview != "the last thing said" {
			t.Errorf("the preview is %q", row.Preview)
		}
		if row.At == "" {
			t.Error("the row carries no time")
		}
	}
	if !found {
		t.Error("the summary left out the thread that just changed")
	}
}

// Nobody else's panel. The summary is drawn as whoever asked for it.
func TestTheSummaryIsOnlyYourOwnThreads(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	c := newAppSharing(t, a)
	conv := pair(t, a, b, "privsender", "privreader")
	c.register("onlooker")

	a.post("/messages/"+conv, url.Values{"body": {"between the two of us"}})

	code, body := c.get("/messages/summary")
	if code != http.StatusOK {
		t.Fatalf("summary → %d", code)
	}
	if strings.Contains(body, "between the two of us") || strings.Contains(body, conv) {
		t.Error("the summary handed somebody else's thread to a stranger")
	}
}
