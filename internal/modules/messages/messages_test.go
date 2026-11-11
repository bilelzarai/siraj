package messages

import (
	"strings"
	"testing"

	"siraj/internal/db/sqlc"
	"siraj/internal/view"
)

// The three tabs read the kinds the design puts on them: the Sirāj team thread sits
// with people, not on a tab of its own (MS-01).
func TestTabsHoldTheRightKinds(t *testing.T) {
	if got := strings.Join(TabPeople.kinds(), ","); got != "direct,system" {
		t.Errorf("People holds %s", got)
	}
	if got := strings.Join(TabGroups.kinds(), ","); got != "group" {
		t.Errorf("Groups holds %s", got)
	}
	if got := strings.Join(TabRooms.kinds(), ","); got != "room" {
		t.Errorf("Rooms holds %s", got)
	}
	// A tab typed into the address bar never reaches the query.
	if ParseTab("' OR 1=1") != TabPeople {
		t.Error("an unknown tab should fall back to People")
	}
}

// The line under a name says who spoke and what it was: a message that is not words
// says what it is instead (MS-01).
func TestPreviewNamesTheSenderAndTheKind(t *testing.T) {
	s := &Service{}
	me := sqlc.MyConversationsRow{}
	me.LastBody.String, me.LastBody.Valid = "I got 7", true
	me.LastSender.UUID, me.LastSender.Valid = [16]byte{1}, true
	if got := s.preview(me, [16]byte{1}); got != "{you}I got 7" {
		t.Errorf("my own line should be prefixed, got %q", got)
	}
	if got := s.preview(me, [16]byte{2}); got != "I got 7" {
		t.Errorf("their line stands alone, got %q", got)
	}
	voice := sqlc.MyConversationsRow{LastKind: "voice"}
	if got := s.preview(voice, [16]byte{2}); got != "{voice}" {
		t.Errorf("a voice note says so, got %q", got)
	}
	group := sqlc.MyConversationsRow{Kind: sqlc.ConversationKindGroup, LastSenderName: "Omar K."}
	group.LastBody.String, group.LastBody.Valid = "Send me the questions", true
	if got := s.preview(group, [16]byte{2}); got != "Omar K.: Send me the questions" {
		t.Errorf("a group names the speaker, got %q", got)
	}
}

// Search looks at both the name and what was said (MS-01).
func TestSearchLooksAtNameAndLine(t *testing.T) {
	row := Row{Name: "Khadija M.", Preview: "Ramadan category tonight after Isha?"}
	for _, q := range []string{"", "khadija", "RAMADAN", "isha"} {
		if !matches(row, q) {
			t.Errorf("%q should have matched", q)
		}
	}
	if matches(row, "zzz") {
		t.Error("zzz matched a row it has nothing to do with")
	}
}

// A file reads the way the design prints it.
func TestSizeAndClockReadLikeTheDesign(t *testing.T) {
	for in, want := range map[int64]string{248 * 1024: "248 KB", 2048: "2 KB", 900: "900 B"} {
		if got := size(in); got != want {
			t.Errorf("%d bytes is %q, want %q", in, got, want)
		}
	}
	if got := clock(6000); got != "0:06" {
		t.Errorf("six seconds is %q", got)
	}
	if got := clock(125000); got != "2:05" {
		t.Errorf("two minutes five is %q", got)
	}
}

// The placeholders the service writes become the player's own words (LG-05).
func TestPlaceholdersAreTranslated(t *testing.T) {
	v := &view.Ctx{}
	out := preview(v, "{you}{voice}")
	if strings.Contains(out, "{") {
		t.Errorf("a placeholder reached the screen: %q", out)
	}
}
