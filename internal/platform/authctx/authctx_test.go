package authctx

import "testing"

func TestRoles(t *testing.T) {
	mk := func(kind, role string) *Principal { return &Principal{Kind: kind, Role: role} }
	cases := []struct {
		name  string
		p     *Principal
		staff bool
		modOK bool // HasRole("moderator")
	}{
		{"anonymous", nil, false, false},
		{"guest is never staff", mk("guest", "admin"), false, false},
		{"player", mk("member", "player"), false, false},
		{"translator is staff but not a moderator", mk("member", "translator"), true, false},
		{"moderator", mk("member", "moderator"), true, true},
		{"support is staff but not a moderator", mk("member", "support"), true, false},
		{"admin passes every role check", mk("member", "admin"), true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.IsStaff(); got != tc.staff {
				t.Fatalf("IsStaff = %v, want %v", got, tc.staff)
			}
			if got := tc.p.HasRole("moderator"); got != tc.modOK {
				t.Fatalf("HasRole(moderator) = %v, want %v", got, tc.modOK)
			}
		})
	}
}
