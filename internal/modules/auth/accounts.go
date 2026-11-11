package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"siraj/internal/db/sqlc"
	"siraj/internal/i18n"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
	"siraj/internal/platform/mailer"
)

// Errors the screens show by name (AU-02).
var (
	ErrBadCredentials = apperr.New(http.StatusUnauthorized, "bad_credentials", "That email or password is not right")
	ErrEmailTaken     = apperr.Conflict("email_taken", "An account already uses this email")
	ErrUsernameTaken  = apperr.Conflict("username_taken", "That username is already taken")
	ErrNotGuest       = apperr.Conflict("not_guest", "This session is already an account")
	ErrGoogleOff      = apperr.New(http.StatusServiceUnavailable, "google_not_configured", "Google sign-in is not configured on this server")
)

// RegisterInput is the sign-up form (AU-02).
type RegisterInput struct {
	DisplayName  string
	Username     string
	Email        string
	Password     string
	Lang         string
	AcceptTerms  bool
	Client       string
	UserAgent    string
	IP           string
	UpgradeGuest *authctx.Principal // set when a guest turns into an account (#5, RL-04)
}

// Register creates an account, or upgrades the guest in UpgradeGuest keeping the same id.
func (s *Service) Register(ctx context.Context, in RegisterInput) (Session, error) {
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.Username = strings.TrimSpace(in.Username)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	fields := map[string]string{}
	if in.DisplayName == "" || len([]rune(in.DisplayName)) > 40 {
		fields["display_name"] = "1 to 40 characters"
	}
	if err := ValidateUsername(in.Username); err != nil {
		fields["username"] = err.Error()
	}
	if !validEmail(in.Email) {
		fields["email"] = "that email does not look right"
	}
	if len(in.Password) < MinPasswordLength {
		fields["password"] = fmt.Sprintf("at least %d characters", MinPasswordLength)
	}
	if !in.AcceptTerms {
		fields["terms"] = "please accept the terms"
	}
	if !i18n.Known(in.Lang) {
		in.Lang = i18n.Fallback
	}
	if in.Client == "" {
		in.Client = "web"
	}
	if len(fields) > 0 {
		return Session{}, apperr.Invalid(fields)
	}

	hash, err := hashPassword(in.Password)
	if err != nil {
		return Session{}, err
	}
	token, tokenHash, err := newToken()
	if err != nil {
		return Session{}, err
	}
	expires := time.Now().Add(s.sessionTTL)

	var user sqlc.User
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if taken, err := q.EmailTaken(ctx, pgtype.Text{String: in.Email, Valid: true}); err != nil {
			return err
		} else if taken {
			return ErrEmailTaken
		}
		if taken, err := q.UsernameTaken(ctx, pgtype.Text{String: in.Username, Valid: true}); err != nil {
			return err
		} else if taken {
			return ErrUsernameTaken
		}

		if in.UpgradeGuest != nil {
			if err := q.LockUser(ctx, in.UpgradeGuest.ID); err != nil {
				return err
			}
			user, err = q.UpgradeGuest(ctx, sqlc.UpgradeGuestParams{
				ID:          in.UpgradeGuest.ID,
				DisplayName: in.DisplayName,
				Username:    pgtype.Text{String: in.Username, Valid: true},
				Email:       pgtype.Text{String: in.Email, Valid: true},
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotGuest
			}
			if err != nil {
				return err
			}
		} else {
			user, err = q.CreateMember(ctx, sqlc.CreateMemberParams{
				DisplayName: in.DisplayName,
				Username:    pgtype.Text{String: in.Username, Valid: true},
				Email:       pgtype.Text{String: in.Email, Valid: true},
				Lang:        in.Lang,
			})
			if err != nil {
				return err
			}
			if err := q.CreateUserSettings(ctx, user.ID); err != nil {
				return err
			}
			if err := s.giveStarterDomain(ctx, q, user.ID); err != nil {
				return err
			}
		}
		if err := q.UpsertIdentity(ctx, sqlc.UpsertIdentityParams{
			UserID: user.ID, Provider: "password", ProviderUid: in.Email,
			PasswordHash: pgtype.Text{String: hash, Valid: true},
		}); err != nil {
			return err
		}
		return q.CreateSession(ctx, sqlc.CreateSessionParams{
			UserID: user.ID, TokenHash: tokenHash, Client: in.Client,
			UserAgent: pgtype.Text{String: in.UserAgent, Valid: in.UserAgent != ""},
			Ip:        parseIP(in.IP), ExpiresAt: ts(expires),
		})
	})
	if err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: expires, User: user}, nil
}

// giveStarterDomain keeps the "at least one domain" rule true for a new account (DM-03, D-04).
func (s *Service) giveStarterDomain(ctx context.Context, q *sqlc.Queries, userID uuid.UUID) error {
	domainID, err := q.DefaultDomainID(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // empty catalog (fresh database): onboarding will add one
	}
	if err != nil {
		return err
	}
	_, err = q.AddUserDomain(ctx, sqlc.AddUserDomainParams{
		UserID: userID, DomainID: domainID, Source: sqlc.UserDomainSourceOnboarding,
	})
	return err
}

// LoginInput is the sign-in form: email or username, plus the password (AU-02).
type LoginInput struct {
	Login     string
	Password  string
	Client    string
	UserAgent string
	IP        string
}

func (s *Service) Login(ctx context.Context, in LoginInput) (Session, error) {
	login := strings.ToLower(strings.TrimSpace(in.Login))
	if login == "" || in.Password == "" {
		return Session{}, ErrBadCredentials
	}
	if in.Client == "" {
		in.Client = "web"
	}
	row, err := s.db.Q.GetUserByLogin(ctx, pgtype.Text{String: login, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrBadCredentials
	}
	if err != nil {
		return Session{}, err
	}
	if !row.PasswordHash.Valid || !checkPassword(in.Password, row.PasswordHash.String) {
		return Session{}, ErrBadCredentials
	}
	user := userFromLogin(row)
	if err := s.checkSanctions(ctx, user.ID); err != nil {
		return Session{}, err
	}
	return s.newSession(ctx, user, in.Client, in.UserAgent, in.IP)
}

// checkSanctions refuses sign-in while a suspension or a ban is active.
func (s *Service) checkSanctions(ctx context.Context, id uuid.UUID) error {
	sanction, err := s.db.Q.ActiveSanction(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if sanction.Kind == sqlc.SanctionKindBan {
		return apperr.Forbidden("This account is banned")
	}
	until := "later"
	if sanction.Until.Valid {
		until = sanction.Until.Time.UTC().Format(time.RFC3339)
	}
	return apperr.Forbidden("This account is suspended until " + until)
}

func (s *Service) newSession(ctx context.Context, user sqlc.User, client, userAgent, ip string) (Session, error) {
	token, hash, err := newToken()
	if err != nil {
		return Session{}, err
	}
	expires := time.Now().Add(s.sessionTTL)
	if user.Kind == sqlc.UserKindGuest && user.ExpiresAt.Valid && user.ExpiresAt.Time.Before(expires) {
		expires = user.ExpiresAt.Time
	}
	err = s.db.Q.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID: user.ID, TokenHash: hash, Client: client,
		UserAgent: pgtype.Text{String: userAgent, Valid: userAgent != ""},
		Ip:        parseIP(ip), ExpiresAt: ts(expires),
	})
	if err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: expires, User: user}, nil
}

// Refresh is endpoint 6: slide a valid session's window instead of signing in again.
func (s *Service) Refresh(ctx context.Context, token string) (Session, error) {
	expires := time.Now().Add(s.sessionTTL)
	userID, err := s.db.Q.RenewSession(ctx, sqlc.RenewSessionParams{TokenHash: hashToken(token), ExpiresAt: ts(expires)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, apperr.Unauthorized("That session is over. Sign in again.")
	}
	if err != nil {
		return Session{}, err
	}
	user, err := s.Me(ctx, userID)
	if err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: expires, User: user}, nil
}

// GoogleInput carries the id_token the client got from Google (AU-02, endpoint 3).
type GoogleInput struct {
	IDToken   string
	Lang      string
	Client    string
	UserAgent string
	IP        string
	Upgrade   *authctx.Principal
}

// Google signs in or creates an account from a verified Google id_token.
func (s *Service) Google(ctx context.Context, in GoogleInput) (Session, error) {
	if s.googleClientID == "" {
		return Session{}, ErrGoogleOff
	}
	claims, err := s.verifyGoogleToken(ctx, in.IDToken)
	if err != nil {
		return Session{}, err
	}
	if in.Client == "" {
		in.Client = "web"
	}
	if !i18n.Known(in.Lang) {
		in.Lang = i18n.Fallback
	}

	// Already linked?
	if user, err := s.db.Q.GetUserByProvider(ctx, sqlc.GetUserByProviderParams{Provider: "google", ProviderUid: claims.Sub}); err == nil {
		if err := s.checkSanctions(ctx, user.ID); err != nil {
			return Session{}, err
		}
		return s.newSession(ctx, user, in.Client, in.UserAgent, in.IP)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Session{}, err
	}

	// Same email as an existing account? Link Google to it.
	if user, err := s.db.Q.GetUserByEmail(ctx, pgtype.Text{String: claims.Email, Valid: true}); err == nil {
		if err := s.db.Q.UpsertIdentity(ctx, sqlc.UpsertIdentityParams{
			UserID: user.ID, Provider: "google", ProviderUid: claims.Sub,
		}); err != nil {
			return Session{}, err
		}
		if err := s.checkSanctions(ctx, user.ID); err != nil {
			return Session{}, err
		}
		return s.newSession(ctx, user, in.Client, in.UserAgent, in.IP)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Session{}, err
	}

	// New account (or a guest turning into one, keeping the same id).
	username, err := s.freeUsername(ctx, claims.Email)
	if err != nil {
		return Session{}, err
	}
	name := claims.Name
	if name == "" {
		name = username
	}
	token, tokenHash, err := newToken()
	if err != nil {
		return Session{}, err
	}
	expires := time.Now().Add(s.sessionTTL)
	var user sqlc.User
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if in.Upgrade != nil {
			if err := q.LockUser(ctx, in.Upgrade.ID); err != nil {
				return err
			}
			user, err = q.UpgradeGuest(ctx, sqlc.UpgradeGuestParams{
				ID: in.Upgrade.ID, DisplayName: name,
				Username: pgtype.Text{String: username, Valid: true},
				Email:    pgtype.Text{String: claims.Email, Valid: true},
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotGuest
			}
			if err != nil {
				return err
			}
		} else {
			user, err = q.CreateMember(ctx, sqlc.CreateMemberParams{
				DisplayName: name,
				Username:    pgtype.Text{String: username, Valid: true},
				Email:       pgtype.Text{String: claims.Email, Valid: true},
				Lang:        in.Lang,
			})
			if err != nil {
				return err
			}
			if err := q.CreateUserSettings(ctx, user.ID); err != nil {
				return err
			}
			if err := s.giveStarterDomain(ctx, q, user.ID); err != nil {
				return err
			}
		}
		if err := q.UpsertIdentity(ctx, sqlc.UpsertIdentityParams{
			UserID: user.ID, Provider: "google", ProviderUid: claims.Sub,
		}); err != nil {
			return err
		}
		return q.CreateSession(ctx, sqlc.CreateSessionParams{
			UserID: user.ID, TokenHash: tokenHash, Client: in.Client,
			UserAgent: pgtype.Text{String: in.UserAgent, Valid: in.UserAgent != ""},
			Ip:        parseIP(in.IP), ExpiresAt: ts(expires),
		})
	})
	if err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: expires, User: user}, nil
}

type googleClaims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Aud   string `json:"aud"`
	// Google returns these as strings in the tokeninfo endpoint.
	EmailVerified string `json:"email_verified"`
	Exp           string `json:"exp"`
}

// verifyGoogleToken asks Google whether the id_token is real and meant for this app.
func (s *Service) verifyGoogleToken(ctx context.Context, idToken string) (googleClaims, error) {
	var claims googleClaims
	if strings.TrimSpace(idToken) == "" {
		return claims, apperr.Invalid(map[string]string{"id_token": "missing"})
	}
	endpoint := "https://oauth2.googleapis.com/tokeninfo?id_token=" + url.QueryEscape(idToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return claims, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return claims, apperr.New(http.StatusBadGateway, "google_unreachable", "Could not reach Google")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return claims, apperr.Unauthorized("That Google sign-in is not valid")
	}
	if err := json.Unmarshal(body, &claims); err != nil {
		return claims, err
	}
	if claims.Aud != s.googleClientID {
		return claims, apperr.Unauthorized("That Google sign-in is for another app")
	}
	if claims.Email == "" || claims.EmailVerified != "true" {
		return claims, apperr.Unauthorized("That Google account has no verified email")
	}
	return claims, nil
}

// freeUsername turns an email into a username nobody uses yet.
func (s *Service) freeUsername(ctx context.Context, email string) (string, error) {
	base := strings.ToLower(email)
	if at := strings.IndexByte(base, '@'); at > 0 {
		base = base[:at]
	}
	clean := make([]rune, 0, 20)
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			clean = append(clean, r)
		}
		if len(clean) == 16 {
			break
		}
	}
	if len(clean) < 3 {
		clean = append(clean, 'p', 'l', 'a', 'y', 'e', 'r')
	}
	candidate := string(clean)
	for i := 0; i < 50; i++ {
		try := candidate
		if i > 0 {
			try = fmt.Sprintf("%s%d", candidate, i+1)
		}
		taken, err := s.db.Q.UsernameTaken(ctx, pgtype.Text{String: try, Valid: true})
		if err != nil {
			return "", err
		}
		if !taken {
			return try, nil
		}
	}
	return "", apperr.Conflict("username_taken", "Could not find a free username")
}

// UsernameAvailable is endpoint 10, called while the player types (AU-02).
func (s *Service) UsernameAvailable(ctx context.Context, username string) (bool, error) {
	username = strings.TrimSpace(username)
	if err := ValidateUsername(username); err != nil {
		return false, apperr.Invalid(map[string]string{"username": err.Error()})
	}
	taken, err := s.db.Q.UsernameTaken(ctx, pgtype.Text{String: username, Valid: true})
	return !taken, err
}

// ValidateUsername is the rule the sign-up screen shows: letters, numbers and _, 3 to 20.
func ValidateUsername(username string) error {
	runes := []rune(username)
	if len(runes) < 3 || len(runes) > 20 {
		return errors.New("3 to 20 characters")
	}
	for _, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return errors.New("letters, numbers and _ only")
		}
	}
	return nil
}

func validEmail(address string) bool {
	if len(address) < 5 || len(address) > 254 {
		return false
	}
	parsed, err := mail.ParseAddress(address)
	return err == nil && parsed.Address == address
}

// ForgotPassword is endpoint 8. It always answers the same way, so the screen can never
// be used to find out which emails have an account.
func (s *Service) ForgotPassword(ctx context.Context, email, lang, baseURL string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if !validEmail(email) {
		return apperr.Invalid(map[string]string{"email": "that email does not look right"})
	}
	user, err := s.db.Q.GetUserByEmail(ctx, pgtype.Text{String: email, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	token, hash, err := newToken()
	if err != nil {
		return err
	}
	if err := s.db.Q.DeleteUserTokens(ctx, sqlc.DeleteUserTokensParams{UserID: user.ID, Purpose: "reset_password"}); err != nil {
		return err
	}
	if err := s.db.Q.CreateAuthToken(ctx, sqlc.CreateAuthTokenParams{
		UserID: user.ID, Purpose: "reset_password", TokenHash: hash,
		ExpiresAt: ts(time.Now().Add(2 * time.Hour)),
	}); err != nil {
		return err
	}
	link := strings.TrimRight(baseURL, "/") + "/reset-password?token=" + url.QueryEscape(token)
	return s.mailer.Send(ctx, mailer.Mail{
		To:      email,
		Subject: "Sirāj · reset your password",
		Body:    "Open this link within 2 hours to choose a new password:\n\n" + link + "\n\nIf you did not ask for this, ignore this message.",
	})
}

// ResetPassword is endpoint 9: the link is one-time and signs every other session out.
func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	if len(password) < MinPasswordLength {
		return apperr.Invalid(map[string]string{"password": fmt.Sprintf("at least %d characters", MinPasswordLength)})
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		userID, err := q.UseAuthToken(ctx, sqlc.UseAuthTokenParams{TokenHash: hashToken(token), Purpose: "reset_password"})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.BadRequest("bad_token", "This link is no longer valid")
		}
		if err != nil {
			return err
		}
		user, err := q.GetUser(ctx, userID)
		if err != nil {
			return err
		}
		email := ""
		if user.Email.Valid {
			email = user.Email.String
		}
		if err := q.UpsertIdentity(ctx, sqlc.UpsertIdentityParams{
			UserID: userID, Provider: "password", ProviderUid: email,
			PasswordHash: pgtype.Text{String: hash, Valid: true},
		}); err != nil {
			return err
		}
		return q.RevokeUserSessions(ctx, userID)
	})
}

// UpdateProfile is endpoint 12.
type ProfileInput struct {
	DisplayName *string
	Username    *string
	AvatarURL   *string
	Lang        *string
}

func (s *Service) UpdateProfile(ctx context.Context, who *authctx.Principal, in ProfileInput) (sqlc.User, error) {
	p := sqlc.UpdateProfileParams{ID: who.ID}
	if in.DisplayName != nil {
		name := strings.TrimSpace(*in.DisplayName)
		if name == "" || len([]rune(name)) > 40 {
			return sqlc.User{}, apperr.Invalid(map[string]string{"display_name": "1 to 40 characters"})
		}
		p.DisplayName = pgtype.Text{String: name, Valid: true}
	}
	if in.Username != nil {
		name := strings.ToLower(strings.TrimSpace(*in.Username))
		if err := ValidateUsername(name); err != nil {
			return sqlc.User{}, apperr.Invalid(map[string]string{"username": err.Error()})
		}
		taken, err := s.db.Q.UsernameTaken(ctx, pgtype.Text{String: name, Valid: true})
		if err != nil {
			return sqlc.User{}, err
		}
		current, err := s.db.Q.GetUser(ctx, who.ID)
		if err != nil {
			return sqlc.User{}, err
		}
		if taken && !strings.EqualFold(current.Username.String, name) {
			return sqlc.User{}, apperr.Conflict("username_taken", "That username is taken")
		}
		p.Username = pgtype.Text{String: name, Valid: true}
	}
	if in.AvatarURL != nil {
		p.AvatarUrl = pgtype.Text{String: *in.AvatarURL, Valid: true}
	}
	if in.Lang != nil {
		if !i18n.Known(*in.Lang) {
			return sqlc.User{}, apperr.Invalid(map[string]string{"lang": "ar, en or fr"})
		}
		p.Lang = pgtype.Text{String: *in.Lang, Valid: true}
		who.Lang = *in.Lang
	}
	return s.db.Q.UpdateProfile(ctx, p)
}

// DeleteAccount is endpoint 16. Everything personal cascades from users.
func (s *Service) DeleteAccount(ctx context.Context, who *authctx.Principal, confirm string) error {
	if confirm != "DELETE" {
		return apperr.Invalid(map[string]string{"confirm": `type DELETE to confirm`})
	}
	return s.db.Q.DeleteUser(ctx, who.ID)
}

// Providers lists the sign-in methods the account has (password, google).
func (s *Service) Providers(ctx context.Context, userID uuid.UUID) (map[string]bool, error) {
	rows, err := s.db.Q.UserProviders(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, p := range rows {
		out[p] = true
	}
	return out, nil
}

// RegisterDevice is endpoint 15 (push notifications).
func (s *Service) RegisterDevice(ctx context.Context, who *authctx.Principal, platform, pushToken string) error {
	switch platform {
	case "ios", "android", "web":
	default:
		return apperr.Invalid(map[string]string{"platform": "ios, android or web"})
	}
	if strings.TrimSpace(pushToken) == "" {
		return apperr.Invalid(map[string]string{"push_token": "missing"})
	}
	return s.db.Q.RegisterDevice(ctx, sqlc.RegisterDeviceParams{UserID: who.ID, Platform: platform, PushToken: pushToken})
}

// userFromLogin drops the password hash from the joined row.
func userFromLogin(r sqlc.GetUserByLoginRow) sqlc.User {
	return sqlc.User{
		ID: r.ID, Kind: r.Kind, Role: r.Role, Username: r.Username, DisplayName: r.DisplayName,
		Email: r.Email, AvatarUrl: r.AvatarUrl, Lang: r.Lang, Level: r.Level, Xp: r.Xp,
		ExpiresAt: r.ExpiresAt, LastSeenAt: r.LastSeenAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}
