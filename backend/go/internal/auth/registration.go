package auth

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/alexedwards/argon2id"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidAccount = errors.New("invalid account fields")
	ErrUsernameTaken  = errors.New("username unavailable")
)

type EmployeeRegistrar interface {
	RegisterEmployee(context.Context, identity.RegisterEmployeeParams) (identity.RegisterEmployeeRow, error)
}

// NewAccountUsername applies the same normalization as login. A new account's
// submitted name must also fit the HTTP login limit before normalization.
func NewAccountUsername(value string) (string, bool) {
	username := strings.ToLower(strings.TrimSpace(value))
	return username, len(value) <= 128 && utf8.ValidString(value) && usernamePattern.MatchString(username)
}

// HashNewPassword is the shared policy for password creation and reset. The
// standalone provisioning command may use it without a server-side work budget.
func HashNewPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 128 || !utf8.ValidString(password) ||
		strings.ContainsRune(password, 0) || strings.TrimSpace(password) == "" {
		return "", ErrInvalidAccount
	}
	return argon2id.CreateHash(password, &argon2id.Params{
		Memory: 64 * 1024, Iterations: 1, Parallelism: 2, SaltLength: 16, KeyLength: 32,
	})
}

// HashNewPassword shares login's work budget; public signup cannot introduce an
// unbounded or separate pool of memory-intensive hashes.
func (s *Service) HashNewPassword(ctx context.Context, password string) (string, error) {
	if ctx.Err() != nil {
		return "", ErrUnavailable
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return "", ErrBusy
	}
	hash, err := HashNewPassword(password)
	if ctx.Err() != nil {
		return "", ErrUnavailable
	}
	return hash, err
}

func (s *Service) RegisterEmployee(ctx context.Context, registrar EmployeeRegistrar, username, password string) (User, error) {
	username, ok := NewAccountUsername(username)
	if !ok {
		return User{}, ErrInvalidAccount
	}
	hash, err := s.HashNewPassword(ctx, password)
	if err != nil {
		return User{}, err
	}
	row, err := registrar.RegisterEmployee(ctx, identity.RegisterEmployeeParams{Username: username, PasswordHash: hash})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUsernameTaken
	}
	if err != nil {
		return User{}, ErrUnavailable
	}
	return User{ID: row.ID.String(), Username: row.Username, Role: "employee"}, nil
}
