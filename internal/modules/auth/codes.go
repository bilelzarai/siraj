package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/platform/mailer"
	"siraj/internal/platform/sms"
)

// The V5 way in: one six-digit code, to a phone or to an email, for signing in and
// for creating an account alike. A password is still accepted for anyone who set
// one, but nothing here needs it.
//
// Three rules the design states and this file keeps:
//   - the code lasts ten minutes and only the newest one works;
//   - three wrong tries and it is dead — a new one must be sent;
//   - a new code cannot be asked for more often than every thirty seconds.

const (
	// CodeLength is the number of boxes on the screen.
	CodeLength = 6
	// CodeTTL is how long a code can be used for.
	CodeTTL = 10 * time.Minute
	// CodeResend is how long the screen's timer counts down.
	CodeResend = 30 * time.Second
	// CodeTries is how many wrong guesses a code survives.
	CodeTries = 3
	// CodesPerHour is the brake on one destination being hammered.
	CodesPerHour = 6
	// ShortSession is how long a session lasts when "Keep me signed in" is off.
	ShortSession = 12 * time.Hour
)

// Channel is where a code goes.
type Channel string

const (
	ChannelPhone Channel = "phone"
	ChannelEmail Channel = "email"
)

// StartCodeInput is what the first step of the form collects.
type StartCodeInput struct {
	Channel Channel
	// Country and Phone are the picker and the field; Email is the other tab.
	Country string
	Phone   string
	Email   string
	// Call asks for the code by telephone instead of text.
	Call bool
	// Guest is whoever is playing in this browser right now, so their points
	// follow them into the account (AU-05).
	Guest *authctx.Principal
	Lang  string
	IP    string

	// Creating is the "Create account" tab, which asks for the name and the
	// password before the code goes out ("1 · Your details → 2 · Verify"). The
	// answers wait on the code row until the digits prove the destination.
	Creating    bool
	DisplayName string
	Password    string
	AcceptTerms bool
	// Resend is "Send it again" or "Call me instead": the details were already
	// given and checked, so they are read back off the last code rather than
	// asked for a second time.
	Resend bool
}

// CodeSent is what the screen needs after a code goes out: where it went, whether
// an account is already there, and when the resend button comes back.
type CodeSent struct {
	Channel     Channel
	Destination string
	// Exists is true when the number or address already has an account: the screen
	// then says "Welcome back" rather than asking for a name (AU-02).
	Exists bool
	// ResendIn is the server's own countdown, so a reloaded page cannot shorten it.
	ResendIn  time.Duration
	ExpiresAt time.Time
	// DevCode is the code itself, and is only ever filled in development, where
	// there is no gateway to read it from.
	DevCode string
}

// StartCode sends a code. The same call serves signing in and creating an account:
// which one it turns out to be is decided at verification, by whether the
// destination already belongs to somebody.
func (s *Service) StartCode(ctx context.Context, in StartCodeInput) (CodeSent, error) {
	out := CodeSent{Channel: in.Channel}
	destination, err := s.destination(in)
	if err != nil {
		return out, err
	}
	out.Destination = destination

	// Nothing is sent before the details are good: a code costs money and the
	// screen keeps its button locked until they are (screens 07–11).
	name, hash, err := s.signupDetails(in)
	if err != nil {
		return out, err
	}
	if in.Creating && in.Resend {
		if last, err := s.db.Q.LastAuthCodeFor(ctx, destination); err == nil {
			name, hash = last.DisplayName.String, last.PasswordHash.String
		}
	}

	// The resend timer is the server's: a page reloaded to dodge it gets nowhere.
	if last, err := s.db.Q.LastAuthCodeSentAt(ctx, destination); err == nil && last.Valid {
		if wait := CodeResend - time.Since(last.Time); wait > 0 {
			out.ResendIn = wait.Round(time.Second)
			return out, apperr.Conflict("too_soon", fmt.Sprintf("Ask again in %d seconds", int(wait.Seconds())+1))
		}
	}
	recent, err := s.db.Q.CountCodesSince(ctx, sqlc.CountCodesSinceParams{Destination: destination, Minutes: 60})
	if err != nil {
		return out, err
	}
	if int(recent) >= CodesPerHour {
		return out, apperr.Conflict("too_many_codes", "Too many codes for this number today. Try again later.")
	}

	code, err := newCode()
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256([]byte(code))

	var guest uuid.NullUUID
	if in.Guest != nil && in.Guest.IsGuest() {
		guest = uuid.NullUUID{UUID: in.Guest.ID, Valid: true}
	}
	if err := s.db.Q.ExpireAuthCodesFor(ctx, destination); err != nil {
		return out, err
	}
	row, err := s.db.Q.CreateAuthCode(ctx, sqlc.CreateAuthCodeParams{
		Channel: string(in.Channel), Destination: destination, CodeHash: sum[:],
		GuestID: guest, Lang: langOr(in.Lang), ByCall: in.Call,
		ExpiresAt: ts(time.Now().Add(CodeTTL)), Ip: parseIP(in.IP),
		DisplayName: text(name), PasswordHash: text(hash),
	})
	if err != nil {
		return out, err
	}
	out.ExpiresAt = row.ExpiresAt.Time
	out.ResendIn = CodeResend
	out.Exists, err = s.destinationTaken(ctx, in.Channel, destination)
	if err != nil {
		return out, err
	}

	if err := s.deliver(ctx, in, destination, code); err != nil {
		return out, err
	}
	if s.devCodes {
		out.DevCode = code
	}
	return out, nil
}

// deliver hands the code to the SMS gateway or the mailer.
func (s *Service) deliver(ctx context.Context, in StartCodeInput, destination, code string) error {
	body := fmt.Sprintf("Your Sirāj code is %s. It expires in %d minutes.", spaced(code), int(CodeTTL.Minutes()))
	if in.Channel == ChannelPhone {
		if s.sms == nil {
			return apperr.Conflict("sms_off", "Codes by text are not available on this server")
		}
		return s.sms.Send(ctx, sms.Message{To: destination, Body: body, Call: in.Call, Lang: in.Lang})
	}
	return s.mailer.Send(ctx, mailer.Mail{
		To: destination, Subject: "Your Sirāj code: " + spaced(code), Body: body,
	})
}

// VerifyCodeInput is the second step.
type VerifyCodeInput struct {
	Channel Channel
	Country string
	Phone   string
	Email   string
	Code    string
	// Remember is "Keep me signed in".
	Remember  bool
	Guest     *authctx.Principal
	Lang      string
	Client    string
	UserAgent string
	IP        string
}

// Verified says what the code proved, and what the screen does next.
type Verified struct {
	// Session is filled when the destination already had an account, or when a
	// guest was promoted: the player is in.
	Session  Session
	SignedIn bool
	// NewAccount is true when nothing is linked to this destination yet and the
	// code carried no details: the screen asks for a display name and then calls
	// Complete (AU-02, edge case 24).
	NewAccount bool
	// Created is true when the digits finished a "Create account": the name and the
	// password were given before the code, so the account exists now and the player
	// is already in it.
	Created bool
	// Destination carries the proven number or address into Complete.
	Destination string
	Channel     Channel
	// MovedPoints is what a guest brought with them into an account they already
	// had, which the screen announces (screen 14).
	MovedPoints int
}

// VerifyCode checks the six digits. A wrong code costs a try; three and it is dead.
func (s *Service) VerifyCode(ctx context.Context, in VerifyCodeInput) (Verified, error) {
	var out Verified
	destination, err := s.destination(StartCodeInput{
		Channel: in.Channel, Country: in.Country, Phone: in.Phone, Email: in.Email,
	})
	if err != nil {
		return out, err
	}
	out.Destination, out.Channel = destination, in.Channel

	digits := digitsOnly.ReplaceAllString(in.Code, "")
	if len(digits) != CodeLength {
		return out, apperr.Invalid(map[string]string{"code": fmt.Sprintf("%d digits", CodeLength)})
	}
	row, err := s.db.Q.LiveAuthCode(ctx, destination)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, apperr.Conflict("code_expired", "That code has expired. Ask for a new one.")
	}
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256([]byte(digits))
	if subtle.ConstantTimeCompare(sum[:], row.CodeHash) != 1 {
		attempts, err := s.db.Q.BumpAuthCodeAttempts(ctx, row.ID)
		if err != nil {
			return out, err
		}
		left := CodeTries - int(attempts)
		if left <= 0 {
			if err := s.db.Q.ConsumeAuthCode(ctx, row.ID); err != nil {
				return out, err
			}
			return out, apperr.Conflict("code_locked", "Too many tries. Ask for a new code.")
		}
		return out, apperr.Invalid(map[string]string{
			"code": fmt.Sprintf("That code doesn't match. %d tries left.", left),
		})
	}
	if err := s.db.Q.ConsumeAuthCode(ctx, row.ID); err != nil {
		return out, err
	}

	// The guest who asked for the code is the one whose points move, even if the
	// browser has since started another session.
	guestID := uuid.Nil
	if row.GuestID.Valid {
		guestID = row.GuestID.UUID
	} else if in.Guest != nil && in.Guest.IsGuest() {
		guestID = in.Guest.ID
	}

	user, err := s.byDestination(ctx, in.Channel, destination)
	switch {
	case err == nil:
		// A returning player: their guest points are added to the account (screen 14).
		if guestID != uuid.Nil && guestID != user.ID {
			if moved, err := s.db.Q.MoveGuestPoints(ctx, sqlc.MoveGuestPointsParams{
				GuestID: guestID, UserID: user.ID,
			}); err == nil {
				out.MovedPoints = int(moved)
			}
		}
		session, err := s.newSessionFor(ctx, user, in)
		if err != nil {
			return out, err
		}
		out.Session, out.SignedIn = session, true
		return out, nil
	case errors.Is(err, pgx.ErrNoRows):
		// A code that came from "Create account" already knows the name and the
		// password: the account is made here and the player lands signed in.
		if row.DisplayName.Valid && row.DisplayName.String != "" {
			created, err := s.createAccount(ctx, newAccount{
				Channel: in.Channel, Destination: destination,
				Name:         row.DisplayName.String,
				Email:        emailOf(in.Channel, destination),
				PasswordHash: row.PasswordHash.String,
				Guest:        in.Guest, Lang: in.Lang,
			})
			if err != nil {
				return out, err
			}
			if guestID != uuid.Nil && guestID != created.ID {
				if moved, err := s.db.Q.MoveGuestPoints(ctx, sqlc.MoveGuestPointsParams{
					GuestID: guestID, UserID: created.ID,
				}); err == nil {
					out.MovedPoints = int(moved)
				}
			}
			session, err := s.newSessionFor(ctx, created, in)
			if err != nil {
				return out, err
			}
			out.Session, out.SignedIn, out.Created = session, true, true
			return out, nil
		}
		// Nothing is linked to it and nothing was asked for: the screen asks for a
		// name (edge case 24).
		out.NewAccount = true
		return out, nil
	default:
		return out, err
	}
}

// CompleteInput finishes a new account: the name other players see, an optional
// address to recover by, and the terms.
type CompleteInput struct {
	Channel     Channel
	Destination string
	DisplayName string
	Email       string
	AcceptTerms bool
	Remember    bool
	Guest       *authctx.Principal
	Lang        string
	Client      string
	UserAgent   string
	IP          string
}

// Complete creates the account the verified code belongs to. A guest who was playing
// in this browser becomes that account — same id, so their points, games and friends
// are already there (AU-05).
func (s *Service) Complete(ctx context.Context, in CompleteInput) (Session, error) {
	name := strings.TrimSpace(in.DisplayName)
	fields := map[string]string{}
	if name == "" {
		fields["display_name"] = "Pick the name other players will see"
	}
	if len([]rune(name)) > 40 {
		fields["display_name"] = "40 characters at most"
	}
	if !in.AcceptTerms {
		fields["accept_terms"] = "Please accept the Terms to continue"
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if in.Channel == ChannelEmail {
		email = in.Destination
	} else if email != "" && !strings.Contains(email, "@") {
		fields["email"] = "That is not an email address"
	}
	if in.Destination == "" {
		fields["destination"] = "Verify a number or an address first"
	}
	if len(fields) > 0 {
		return Session{}, apperr.Invalid(fields)
	}

	// Nobody may claim a destination that was taken while they were typing.
	if taken, err := s.destinationTaken(ctx, in.Channel, in.Destination); err != nil {
		return Session{}, err
	} else if taken {
		return Session{}, apperr.Conflict("taken", "That number already has an account. Sign in instead.")
	}
	if email != "" {
		if _, err := s.db.Q.GetUserByEmail(ctx, text(email)); err == nil {
			return Session{}, apperr.Conflict("email_taken", "That email already has an account")
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return Session{}, err
		}
	}

	user, err := s.createAccount(ctx, newAccount{
		Channel: in.Channel, Destination: in.Destination, Name: name, Email: email,
		Guest: in.Guest, Lang: in.Lang,
	})
	if err != nil {
		return Session{}, err
	}
	return s.newSessionFor(ctx, user, VerifyCodeInput{
		Remember: in.Remember, Client: in.Client, UserAgent: in.UserAgent, IP: in.IP,
	})
}

// newAccount is one account being written: the proven destination, the name other
// players see, an optional address to recover by, and the password when the player
// set one on the "Your details" step.
type newAccount struct {
	Channel      Channel
	Destination  string
	Name         string
	Email        string
	PasswordHash string
	Guest        *authctx.Principal
	Lang         string
}

// createAccount is the one place an account is born out of a proven code, whether
// the name came before the digits (Create account) or after them (a number nobody
// uses yet).
func (s *Service) createAccount(ctx context.Context, in newAccount) (sqlc.User, error) {
	var user sqlc.User
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		var phone, mail pgtype.Text
		if in.Channel == ChannelPhone {
			phone = text(in.Destination)
		}
		if in.Email != "" {
			mail = text(in.Email)
		}
		// A guest in this browser keeps their id: the account is the same player.
		if in.Guest != nil && in.Guest.IsGuest() {
			row, err := q.PromoteGuest(ctx, sqlc.PromoteGuestParams{
				ID: in.Guest.ID, DisplayName: in.Name, Phone: phone, Email: mail,
				Username: pgtype.Text{}, Lang: langOr(in.Lang),
			})
			if err != nil {
				return err
			}
			user = row
		} else {
			row, err := q.CreateMember(ctx, sqlc.CreateMemberParams{
				DisplayName: in.Name, Email: mail, Phone: phone, Username: pgtype.Text{},
				Lang: langOr(in.Lang),
			})
			if err != nil {
				return err
			}
			user = row
			if err := q.CreateUserSettings(ctx, user.ID); err != nil {
				return err
			}
			if err := s.giveStarterDomain(ctx, q, user.ID); err != nil {
				return err
			}
		}
		// The proof itself is an identity, so the next sign-in knows how they came in.
		if err := q.UpsertIdentity(ctx, sqlc.UpsertIdentityParams{
			UserID: user.ID, Provider: string(in.Channel), ProviderUid: in.Destination,
		}); err != nil {
			return err
		}
		if in.PasswordHash == "" {
			return nil
		}
		// A password set on the create screen is a second way in, beside the code.
		login := in.Destination
		if in.Email != "" {
			login = in.Email
		}
		return q.UpsertIdentity(ctx, sqlc.UpsertIdentityParams{
			UserID: user.ID, Provider: "password", ProviderUid: login,
			PasswordHash: text(in.PasswordHash),
		})
	})
	return user, err
}

// SignOutEverywhere closes every session of the account, which is the second button
// of the sign-out confirmation (screen 17).
func (s *Service) SignOutEverywhere(ctx context.Context, who *authctx.Principal) (int, error) {
	if who == nil {
		return 0, apperr.Unauthorized("Sign in first")
	}
	n, err := s.db.Q.RevokeAllSessions(ctx, who.ID)
	return int(n), err
}

// Devices lists where this account is signed in ("Linked devices" in the menu).
func (s *Service) Devices(ctx context.Context, who *authctx.Principal, currentToken string) ([]sqlc.ListUserSessionsRow, error) {
	if who == nil {
		return nil, apperr.Unauthorized("Sign in first")
	}
	return s.db.Q.ListUserSessions(ctx, sqlc.ListUserSessionsParams{
		UserID: who.ID, Current: hashToken(currentToken),
	})
}

// RevokeDevice signs one of them out.
func (s *Service) RevokeDevice(ctx context.Context, who *authctx.Principal, id uuid.UUID) error {
	if who == nil {
		return apperr.Unauthorized("Sign in first")
	}
	n, err := s.db.Q.RevokeSessionByID(ctx, sqlc.RevokeSessionByIDParams{ID: id, UserID: who.ID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("device")
	}
	return nil
}

// ---------------------------------------------------------------- small parts

// destination turns the form into the one string everything else keys on.
func (s *Service) destination(in StartCodeInput) (string, error) {
	switch in.Channel {
	case ChannelPhone:
		number, ok := NormalisePhone(in.Country, in.Phone)
		if !ok {
			return "", apperr.Invalid(map[string]string{"phone": "That number does not look right for this country"})
		}
		return number, nil
	case ChannelEmail:
		email := strings.ToLower(strings.TrimSpace(in.Email))
		if email == "" || !strings.Contains(email, "@") || strings.HasSuffix(email, "@") {
			return "", apperr.Invalid(map[string]string{"email": "That is not an email address"})
		}
		return email, nil
	}
	return "", apperr.BadRequest("bad_channel", "phone or email")
}

// signupDetails validates the "Your details" step and returns what has to wait on
// the code row: the name other players will see, and the password already hashed.
// Signing in asks for none of it, and gets a pair of empty strings.
func (s *Service) signupDetails(in StartCodeInput) (name, hash string, err error) {
	if !in.Creating || in.Resend {
		return "", "", nil
	}
	name = strings.TrimSpace(in.DisplayName)
	fields := map[string]string{}
	if name == "" {
		fields["display_name"] = "Pick the name other players will see"
	} else if len([]rune(name)) > 40 {
		fields["display_name"] = "40 characters at most"
	}
	if missing := ValidateNewPassword(in.Password); len(missing) > 0 {
		fields["password"] = missingPassword(missing)
	}
	if !in.AcceptTerms {
		fields["accept_terms"] = "Please accept the Terms to continue"
	}
	if len(fields) > 0 {
		return "", "", apperr.Invalid(fields)
	}
	hash, err = hashPassword(in.Password)
	if err != nil {
		return "", "", err
	}
	return name, hash, nil
}

// missingPassword names the checks the screen still has in red.
func missingPassword(missing []PasswordRule) string {
	words := map[PasswordRule]string{
		RuleLength: fmt.Sprintf("%d characters", MinPasswordLength),
		RuleUpper:  "a capital letter",
		RuleLower:  "a small letter",
		RuleDigit:  "a number",
		RuleSymbol: "a symbol",
	}
	parts := make([]string, 0, len(missing))
	for _, rule := range missing {
		parts = append(parts, words[rule])
	}
	return "Your password still needs " + strings.Join(parts, ", ") + "."
}

// emailOf is the address an account keeps when the code went to one.
func emailOf(channel Channel, destination string) string {
	if channel == ChannelEmail {
		return destination
	}
	return ""
}

func (s *Service) destinationTaken(ctx context.Context, channel Channel, destination string) (bool, error) {
	_, err := s.byDestination(ctx, channel, destination)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	default:
		return false, err
	}
}

func (s *Service) byDestination(ctx context.Context, channel Channel, destination string) (sqlc.User, error) {
	if channel == ChannelPhone {
		return s.db.Q.GetUserByPhone(ctx, text(destination))
	}
	return s.db.Q.GetUserByEmail(ctx, text(destination))
}

// newSessionFor opens the session, with the length "Keep me signed in" asks for.
func (s *Service) newSessionFor(ctx context.Context, user sqlc.User, in VerifyCodeInput) (Session, error) {
	if err := s.checkSanctions(ctx, user.ID); err != nil {
		return Session{}, err
	}
	ttl := s.sessionTTL
	if !in.Remember {
		ttl = min(ShortSession, s.sessionTTL)
	}
	token, hash, err := newToken()
	if err != nil {
		return Session{}, err
	}
	expires := time.Now().Add(ttl)
	client := in.Client
	if client == "" {
		client = "web"
	}
	if err := s.db.Q.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID: user.ID, TokenHash: hash, Client: client,
		UserAgent: text(in.UserAgent), Ip: parseIP(in.IP), ExpiresAt: ts(expires),
		Remember: in.Remember,
	}); err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: expires, User: user}, nil
}

// newCode is six digits, read from the same source as the session tokens.
func newCode() (string, error) {
	out := make([]byte, CodeLength)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		out[i] = byte('0' + n.Int64())
	}
	return string(out), nil
}

// spaced is how a code reads in a message: 482 173.
func spaced(code string) string {
	if len(code) != 6 {
		return code
	}
	return code[:3] + " " + code[3:]
}

// text is a nullable string the way pgx wants it.
func text(v string) pgtype.Text { return pgtype.Text{String: v, Valid: v != ""} }

func langOr(lang string) string {
	switch lang {
	case "ar", "en", "fr":
		return lang
	}
	return "en"
}
