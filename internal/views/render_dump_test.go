package views

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/models"
)

// TestDumpMessagesHTML writes the real messages page to a file so the layout
// can be looked at in a browser. It only runs when asked, by pointing
// DUMP_MESSAGES_HTML at a path.
func TestDumpMessagesHTML(t *testing.T) {
	out := os.Getenv("DUMP_MESSAGES_HTML")
	if out == "" {
		t.Skip("set DUMP_MESSAGES_HTML to a path to write the page")
	}
	bundle, err := i18n.New("ar")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	me := &models.User{ID: uuid.New(), Username: "me", DisplayName: "أنا"}
	them := &models.UserCard{
		ID: uuid.New(), Username: "admin", DisplayName: "Site Admin",
		LastSeenAt: time.Now(),
	}
	c := Ctx{
		Tr: bundle.Printer("ar"), User: me, CSRF: "x",
		Path: "/messages", Locale: "ar", Dir: "rtl", Theme: "light", AssetV: "dev",
	}
	conv := &models.Conversation{
		ID: uuid.New(), Other: them, LastMessage: "khb",
		LastMessageAt: time.Now().Add(-2 * time.Hour), UnreadCount: 2,
	}
	now := time.Now()
	msg := func(body string, mine bool, min int) *models.Message {
		m := &models.Message{ID: int64(min), Body: body, CreatedAt: now.Add(-time.Duration(min) * time.Minute)}
		if mine {
			m.SenderID = me.ID
			read := now
			m.ReadAt = &read
		} else {
			m.SenderID = them.ID
		}
		return m
	}
	from := func(u *models.UserCard, body string, min int) *models.Message {
		return &models.Message{
			ID: int64(1000 + min), Body: body, SenderID: u.ID,
			CreatedAt: now.Add(-time.Duration(min) * time.Minute),
		}
	}
	withdrawn := func(m *models.Message) *models.Message {
		at := now
		m.DeletedAt = &at
		return m
	}
	sami := &models.UserCard{ID: uuid.New(), Username: "sami", DisplayName: "سامي", LastSeenAt: time.Now()}
	amira := &models.UserCard{ID: uuid.New(), Username: "amira", DisplayName: "أميرة"}
	yasin := &models.UserCard{ID: uuid.New(), Username: "yasin", DisplayName: "ياسين"}
	group := &models.Conversation{
		ID: uuid.New(), Kind: models.ConversationGroup, Title: "حلقة الدراسة",
		Members: []*models.UserCard{{ID: me.ID, Username: me.Username, DisplayName: me.DisplayName}, sami, amira, yasin}, MemberCount: 4,
		LastMessage: "أنا مع الاستوديو", LastMessageAt: time.Now().Add(-9 * time.Minute),
		UnreadCount: 1, Joined: true,
	}
	room := &models.Conversation{
		ID: uuid.New(), Kind: models.ConversationRoom, Title: "تدريب التجويد",
		Topic: "نقرأ معاً كل جمعة", MemberCount: 12,
		LastMessageAt: time.Now().Add(-40 * time.Minute), Joined: true,
	}
	d := MessagesData{
		Conversations: []*models.Conversation{conv, group, room},
		Tab:           tabOr(models.ConversationDirect),
		Counts:        map[string]int{models.ConversationDirect: 2, models.ConversationGroup: 1},
		Active:        activeFor(os.Getenv("DUMP_TAB"), conv, group, room),
		AllFriends:    []*models.UserCard{sami, amira, yasin},
		Rooms:         []*models.Conversation{room, {ID: uuid.New(), Kind: models.ConversationRoom, Title: "مجلس السيرة", Topic: "قراءة أسبوعية", MemberCount: 31}},
		Messages: messagesFor(os.Getenv("DUMP_TAB"), []*models.Message{
			msg("test", false, 40), msg("zzz", true, 39),
			msg("lkkhu", false, 20), msg("kjgy", true, 18),
			withdrawn(msg("gone", true, 10)),
			msg("a longer line, to see how a real sentence sits in the column", false, 5),
			msg("and an answer to it that runs on a bit further than the last one did", true, 3),
		}, []*models.Message{
			from(sami, "أين نقيم حفل الإطلاق؟", 22),
			from(sami, "أنا مع الاستوديو", 20),
			from(amira, "السطح أجمل لكنه يحتاج تصريحاً", 16),
			msg("أميل إلى الاستوديو", true, 9),
			from(yasin, "وأنا أوافق سامي", 4),
		}),
		FileLimit: 5,
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	if err := Messages(c, d).Render(context.Background(), f); err != nil {
		t.Fatalf("render: %v", err)
	}
}

// tabOr is which segment the dump draws, so every kind can be looked at.
func tabOr(fallback string) string {
	if t := os.Getenv("DUMP_TAB"); t != "" {
		return t
	}
	return fallback
}

func activeFor(tab string, direct, group, room *models.Conversation) *models.Conversation {
	switch tab {
	case models.ConversationGroup:
		return group
	case models.ConversationRoom:
		return room
	}
	return direct
}

func messagesFor(tab string, direct, group []*models.Message) []*models.Message {
	if tab == models.ConversationGroup {
		return group
	}
	if tab == models.ConversationRoom {
		return nil
	}
	return direct
}
