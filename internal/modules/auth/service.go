// Package auth: guest sessions, tokens and the current user.
// Register / login / Google / password reset (endpoints 1–3, 5–10) come next in build step 2.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db"
	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/platform/mailer"
	"siraj/internal/platform/sms"
)

type Service struct {
	db             *db.DB
	sessionTTL     time.Duration
	guestTTL       time.Duration
	mailer         mailer.Mailer
	sms            sms.Sender
	googleClientID string
	appleClientID  string
	baseURL        string
	http           *http.Client
	// devCodes hands the one-time code back to the screen in development, where
	// there is no gateway to read it from. It is never on in production.
	devCodes bool
}

type Deps struct {
	SessionTTL     time.Duration
	GuestTTL       time.Duration
	Mailer         mailer.Mailer
	SMS            sms.Sender
	GoogleClientID string
	AppleClientID  string
	BaseURL        string
	// DevCodes shows the code on screen instead of hiding it in a log; development
	// only, and the server refuses to set it when APP_ENV is production.
	DevCodes bool
}

func NewService(d *db.DB, deps Deps) *Service {
	m := deps.Mailer
	if m == nil {
		m = mailer.Log{}
	}
	texter := deps.SMS
	if texter == nil {
		texter = sms.Logger{}
	}
	return &Service{
		db: d, sessionTTL: deps.SessionTTL, guestTTL: deps.GuestTTL, mailer: m, sms: texter,
		googleClientID: deps.GoogleClientID, appleClientID: deps.AppleClientID,
		baseURL: deps.BaseURL, devCodes: deps.DevCodes,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// BaseURL is the public URL used in the links the service emails.
func (s *Service) BaseURL() string { return s.baseURL }

// GoogleEnabled tells the sign-in screens whether to show the Google button.
func (s *Service) GoogleEnabled() bool { return s.googleClientID != "" }

// AppleEnabled is the same for the Apple button: the design draws it, and it is
// shown once a client id is configured.
func (s *Service) AppleEnabled() bool { return s.appleClientID != "" }

// DevCodes says whether the screens may show the one-time code themselves.
func (s *Service) DevCodes() bool { return s.devCodes }

type GuestInput struct {
	Name      string
	Lang      string
	Client    string // web | ios | android
	UserAgent string
	IP        string
}

type Session struct {
	Token     string
	ExpiresAt time.Time
	User      sqlc.User
}

// StartGuest creates a guest user, their settings, a starter domain and a session, in one transaction.
func (s *Service) StartGuest(ctx context.Context, in GuestInput) (Session, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "Guest"
	}
	if len([]rune(name)) > 40 {
		return Session{}, apperr.Invalid(map[string]string{"name": "40 characters at most"})
	}
	if in.Client == "" {
		in.Client = "web"
	}
	token, hash, err := newToken()
	if err != nil {
		return Session{}, err
	}
	now := time.Now()
	sessionExp := now.Add(min(s.sessionTTL, s.guestTTL))

	var user sqlc.User
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		user, err = q.CreateGuest(ctx, sqlc.CreateGuestParams{
			DisplayName: name,
			Lang:        in.Lang,
			ExpiresAt:   ts(now.Add(s.guestTTL)),
		})
		if err != nil {
			return err
		}
		if err := q.CreateUserSettings(ctx, user.ID); err != nil {
			return err
		}
		// Every player needs at least one domain; start with the flagship until onboarding says otherwise.
		if domainID, err := q.DefaultDomainID(ctx); err == nil {
			if _, err := q.AddUserDomain(ctx, sqlc.AddUserDomainParams{UserID: user.ID, DomainID: domainID, Source: sqlc.UserDomainSourceOnboarding}); err != nil {
				return err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return q.CreateSession(ctx, sqlc.CreateSessionParams{
			UserID:    user.ID,
			TokenHash: hash,
			Client:    in.Client,
			UserAgent: pgtype.Text{String: in.UserAgent, Valid: in.UserAgent != ""},
			Ip:        parseIP(in.IP),
			ExpiresAt: ts(sessionExp),
		})
	})
	if err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: sessionExp, User: user}, nil
}

// Resolve implements middleware.Resolver.
func (s *Service) Resolve(ctx context.Context, token string) (*authctx.Principal, error) {
	row, err := s.db.Q.GetSessionUser(ctx, hashToken(token))
	if err != nil {
		return nil, err
	}
	return &authctx.Principal{
		ID:          row.ID,
		Kind:        string(row.Kind),
		Role:        string(row.Role),
		Lang:        row.Lang,
		IslamicOnly: row.IslamicOnly,
	}, nil
}

func (s *Service) Me(ctx context.Context, id uuid.UUID) (sqlc.User, error) {
	u, err := s.db.Q.GetUser(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, apperr.NotFound("user")
	}
	return u, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.db.Q.RevokeSession(ctx, hashToken(token))
}

// Tokens are 32 random bytes; only their sha256 is stored.
func newToken() (string, []byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
	return tok, hashToken(tok), nil
}

func hashToken(tok string) []byte {
	h := sha256.Sum256([]byte(tok))
	return h[:]
}

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func parseIP(s string) *netip.Addr {
	if a, err := netip.ParseAddr(s); err == nil {
		return &a
	}
	return nil
}

// Settings returns the player's settings row (theme, daily goal, Islamic-only, …).
func (s *Service) Settings(ctx context.Context, id uuid.UUID) (sqlc.UserSetting, error) {
	st, err := s.db.Q.GetUserSettings(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, apperr.NotFound("settings")
	}
	return st, err
}

// SuggestGuestName is the name the guest screen prefills ("Guest 4821", RL-05). It is a
// suggestion the player types over; a guest created without any name stays "Guest".
func SuggestGuestName() string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "Guest"
	}
	return fmt.Sprintf("Guest %04d", (int(b[0])<<8|int(b[1]))%9000+1000)
}

// Seen marks a player as here right now. The write is cheap: the query only touches
// the row when the last stamp is more than a minute old, so a screen that polls
// itself every few seconds does not write every time (FR-02).
func (s *Service) Seen(ctx context.Context, userID uuid.UUID) {
	if userID == uuid.Nil {
		return
	}
	_ = s.db.Q.TouchUser(ctx, userID)
}
