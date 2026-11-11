package views

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// composer returns the message composer's markup, from the opening form tag to
// the closing one.
func composer(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/views/messages.templ"))
	if err != nil {
		t.Fatalf("read messages.templ: %v", err)
	}
	form := regexp.MustCompile(`(?s)<form\s+class="chat__compose".*?</form>`).
		FindString(string(body))
	if form == "" {
		t.Fatal("no composer in messages.templ")
	}
	return form
}

// A photograph, a document or a voice note is a message. The box used to carry
// `required`, so the browser refused the submit before the script could say a
// file was enough — and a recording could not be sent without a sentence typed
// beside it. The server has always accepted one; the composer has to as well.
func TestTheBodyIsNotRequiredWhenSomethingElseRides(t *testing.T) {
	form := composer(t)

	field := regexp.MustCompile(`(?s)<textarea.*?</textarea>`).FindString(form)
	if field == "" {
		t.Fatal("no message field in the composer")
	}
	if regexp.MustCompile(`(?m)^\s*required\s*$`).MatchString(field) {
		t.Error("the message field is required, so a file on its own can never be sent")
	}
	// And the button the script enables and disables has to be findable.
	if !strings.Contains(form, "data-send") {
		t.Error("the send button has no handle, so nothing can tell it there is something to send")
	}
}

// The tools, the box and send are one line. They were direct children of a
// wrapping flex container, and because every field in the app is `width: 100%`
// the box claimed the whole row and pushed send onto a line of its own.
func TestTheComposerControlsShareOneRow(t *testing.T) {
	form := composer(t)

	// The row is the composer's last child, so everything from its opening
	// tag to the end of the form is inside it.
	start := strings.Index(form, `<div class="chat__row">`)
	if start < 0 {
		t.Fatal("the composer has no control row")
	}
	row := form[start:]
	for _, handle := range []string{"data-file-open", "data-record", "data-emoji-open", "data-input", "data-send"} {
		if !strings.Contains(row, handle) {
			t.Errorf("%s is outside the control row, so it sits on a line of its own", handle)
		}
	}

	// The bars that do belong on their own line are siblings of the row, not
	// items in it — that was what the wrap existed for.
	for _, own := range []string{"data-replying", "data-attached"} {
		if strings.Contains(row, own) {
			t.Errorf("%s is inside the control row", own)
		}
	}
}

// Several files in one send, and a shelf with a chip for each. The chip is
// cloned from a template the server drew, so the words on it are translated
// rather than assembled in the browser.
func TestTheComposerTakesSeveralFiles(t *testing.T) {
	form := composer(t)

	field := regexp.MustCompile(`(?s)<input\s+class="chat__file-input".*?/>`).FindString(form)
	if field == "" {
		t.Fatal("no file input in the composer")
	}
	if !regexp.MustCompile(`(?m)^\s*multiple\s*$`).MatchString(field) {
		t.Error("the file input takes one file, so several can never be sent at once")
	}
	for _, part := range []string{"data-shelf", "data-chip", "data-attached-clear"} {
		if !strings.Contains(form, part) {
			t.Errorf("the composer has no %s, so what is waiting to be sent cannot be shown", part)
		}
	}
	// And the recorder has somewhere to say how long it has been listening.
	if !strings.Contains(form, "data-record-clock") {
		t.Error("recording has no clock, so its length is only known once it is already sent")
	}
}

// A recording is played by the page's own control, not the browser's. The
// browser's reads its length out of the file, and a WebM from MediaRecorder
// does not carry one — which is how a seven-second note announced itself as
// 0:00 / 0:00.
func TestARecordingIsDrawnRatherThanHandedToTheBrowser(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/views/messages.templ"))
	if err != nil {
		t.Fatalf("read messages.templ: %v", err)
	}
	src := string(body)

	if regexp.MustCompile(`<audio[^>]*\bcontrols\b`).MatchString(src) {
		t.Error("a voice note is handed to the browser's own player, which cannot say how long it runs")
	}
	for _, part := range []string{"data-voice-play", "data-voice-wave", "data-voice-time", "data-voice-audio"} {
		if !strings.Contains(src, part) {
			t.Errorf("the player has no %s", part)
		}
	}
}

// The same id has to give the same shape, or a recording looks like a
// different recording on the next page load.
func TestTheWaveformIsStableForAnID(t *testing.T) {
	first := waveform("6f1a2b3c-0000-4000-8000-000000000001")
	again := waveform("6f1a2b3c-0000-4000-8000-000000000001")
	other := waveform("6f1a2b3c-0000-4000-8000-000000000002")

	if len(first) != 26 {
		t.Fatalf("the waveform has %d bars", len(first))
	}
	for i := range first {
		if first[i] != again[i] {
			t.Fatalf("bar %d came back as %d and then %d for the same id", i, first[i], again[i])
		}
		if first[i] < 30 || first[i] > 100 {
			t.Errorf("bar %d is %d%% high, which is off the track", i, first[i])
		}
	}
	same := true
	for i := range first {
		if first[i] != other[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("two different recordings are drawn with the same shape")
	}
}

// The server draws the shape for a message already in the thread, and the
// script draws it for one that arrives while the thread is open. They have to
// be the same arithmetic, or a recording changes appearance the moment the
// page is reloaded.
func TestBothSidesDrawTheSameWaveform(t *testing.T) {
	body := clientSources(t)

	// The numbers the shape is made of. A change to either side that does not
	// change the other lands here.
	for _, part := range []string{"2166136261", "16777619", "30 + ((h >>> 17) % 70)", "i < 26"} {
		if !strings.Contains(body, part) {
			t.Errorf("the script's waveform no longer uses %q, so it has drifted from the server's", part)
		}
	}
}

// A row the panel repaint cannot name is a row it cannot repaint.
func TestConversationRowsCarryTheirID(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/views/messages.templ"))
	if err != nil {
		t.Fatalf("read messages.templ: %v", err)
	}
	if !strings.Contains(string(body), `data-conv={ conv.ID.String() }`) {
		t.Error("conversation rows carry no id, so the live panel cannot find one to update")
	}
}
