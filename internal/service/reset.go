package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/bilelzarai/siraj/internal/repository"
)

// ResetTTL is how long an emailed link stays usable. Long enough to survive a
// slow mail server, short enough that a link left sitting in an inbox is not a
// standing key to the account.
const ResetTTL = 45 * time.Minute

// Reset issues and redeems password-reset tokens.
type Reset struct {
	repo    *repository.Repo
	limiter *attemptLimiter
}

func NewReset(repo *repository.Repo) *Reset {
	return &Reset{
		repo: repo,
		// Per client, not per account: rate-limiting by email address would
		// itself confirm which addresses exist.
		//
		// The cap has to clear a shared NAT — a school or an office is one
		// address to us — while still making it expensive to mail-bomb someone
		// through this form. Enumeration is already handled by answering
		// identically for every address, not by this limit.
		limiter: newAttemptLimiter(10, 15*time.Minute),
	}
}

// HashResetToken is exported so the repository and the tests agree on the
// single representation stored in the database.
func HashResetToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Request creates a token for the account with this email and returns the raw
// value to put in the link, plus the account it belongs to.
//
// Both return values are nil/empty when no such account exists, and the caller
// must still report success to the person: telling them "no account with that
// address" turns this form into a membership oracle for any email address
// anyone cares to try.
func (s *Reset) Request(ctx context.Context, email, clientKey string) (string, *resetTarget, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return "", nil, nil
	}
	if !s.limiter.allow(clientKey) {
		return "", nil, &FieldError{Key: "auth.error.rateLimited"}
	}

	user, err := s.repo.UserByEmail(ctx, email)
	if errors.Is(err, repository.ErrNotFound) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	// A suspended account must not be recoverable by its owner; that is the
	// point of suspending it.
	if user.Status != "active" {
		return "", nil, nil
	}

	raw, err := randomToken(32)
	if err != nil {
		return "", nil, err
	}

	// Older outstanding links stop working the moment a new one is issued, so
	// a forwarded or intercepted email cannot be redeemed later.
	if err := s.repo.CreatePasswordReset(ctx, HashResetToken(raw), user.ID,
		time.Now().Add(ResetTTL), clientKey); err != nil {
		return "", nil, err
	}

	return raw, &resetTarget{
		ID:          user.ID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Locale:      user.Locale,
	}, nil
}

type resetTarget struct {
	ID          string
	Email       string
	DisplayName string
	Locale      string
}

// Check reports whether a raw token is still redeemable, without spending it.
// The reset form calls this before rendering, so an expired link says so
// instead of collecting a password and then refusing it.
func (s *Reset) Check(ctx context.Context, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return &FieldError{Key: "auth.reset.invalid"}
	}
	_, err := s.repo.PasswordResetUser(ctx, HashResetToken(raw), time.Now())
	if errors.Is(err, repository.ErrNotFound) {
		return &FieldError{Key: "auth.reset.invalid"}
	}
	return err
}

// Redeem sets the new password and burns the token.
func (s *Reset) Redeem(ctx context.Context, raw, password, confirm string) error {
	if len(password) < 8 {
		return &FieldError{Field: "password", Key: "auth.error.passwordShort"}
	}
	if password != confirm {
		return &FieldError{Field: "password_confirm", Key: "auth.error.passwordMismatch"}
	}

	userID, err := s.repo.PasswordResetUser(ctx, HashResetToken(raw), time.Now())
	if errors.Is(err, repository.ErrNotFound) {
		return &FieldError{Key: "auth.reset.invalid"}
	}
	if err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	// One statement marks the token used, sets the password and drops every
	// session. Whoever forced the reset — the owner or someone who had taken
	// the account — the other party is signed out by the time this returns.
	return s.repo.RedeemPasswordReset(ctx, HashResetToken(raw), userID, string(hash))
}
