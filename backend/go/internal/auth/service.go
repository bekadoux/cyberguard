// Package auth verifies credentials and current account state. HTTP and session
// transport are separate; no role or account status from a client is trusted.
package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"regexp"
	"strings"

	"github.com/alexedwards/argon2id"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthenticated    = errors.New("authentication required")
	ErrUnavailable        = errors.New("authentication is unavailable")
	ErrBusy               = errors.New("authentication is busy")
	usernamePattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,63}$`)
)

// Users is the small query boundary implemented by the generated sqlc Queries.
type Users interface {
	GetUserByUsername(context.Context, string) (identity.User, error)
	GetUserByID(context.Context, pgtype.UUID) (identity.User, error)
}

type User struct {
	ID          string
	Username    string
	Role        string
	AuthVersion int64
}

type Service struct {
	users     Users
	dummyHash string
	slots     chan struct{}
	verify    func(string, string) bool
}

func NewService(users Users) (*Service, error) {
	if users == nil {
		return nil, errors.New("authentication requires user queries")
	}
	// Fixed lanes keep startup/unknown-user work independent of host CPU count.
	dummy, err := argon2id.CreateHash(rand.Text(), &argon2id.Params{
		Memory: 64 * 1024, Iterations: 1, Parallelism: 2, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		return nil, errors.New("could not initialize password verification")
	}
	return &Service{users: users, dummyHash: dummy, slots: make(chan struct{}, 2), verify: verifyPassword}, nil
}

func (s *Service) Authenticate(ctx context.Context, username, password string) (User, error) {
	if ctx.Err() != nil {
		return User{}, ErrUnavailable
	}
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernamePattern.MatchString(username) || len(password) == 0 || len(password) > 128 {
		return User{}, ErrInvalidCredentials
	}
	select {
	case <-ctx.Done():
		return User{}, ErrUnavailable
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return User{}, ErrBusy
	}
	user, err := s.users.GetUserByUsername(ctx, username)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUnavailable
	}
	exists := err == nil
	hash := s.dummyHash
	if exists {
		hash = user.PasswordHash
	}
	// Unknown and blocked accounts still perform password work. This reduces
	// trivial account enumeration; it is not a promise of identical timings.
	match := s.verify(password, hash)
	if ctx.Err() != nil {
		return User{}, ErrUnavailable
	}
	if !exists || !match || !validUser(user) {
		return User{}, ErrInvalidCredentials
	}
	// Recheck after expensive verification: blocking/password/role changes must
	// not create a new session from an already stale account snapshot.
	current, err := s.users.GetUserByID(ctx, user.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUnavailable
	}
	if err != nil || !validUser(current) || current.AuthVersion != user.AuthVersion || current.PasswordHash != user.PasswordHash {
		return User{}, ErrInvalidCredentials
	}
	return safeUser(current), nil
}

func (s *Service) CurrentUser(ctx context.Context, id string, version int64) (User, error) {
	if ctx.Err() != nil {
		return User{}, ErrUnavailable
	}
	var uuid pgtype.UUID
	if err := uuid.Scan(id); err != nil || !uuid.Valid || version < 1 {
		return User{}, ErrUnauthenticated
	}
	user, err := s.users.GetUserByID(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUnauthenticated
	}
	if err != nil {
		return User{}, ErrUnavailable
	}
	if ctx.Err() != nil {
		return User{}, ErrUnavailable
	}
	if !validUser(user) || user.AuthVersion != version {
		return User{}, ErrUnauthenticated
	}
	return safeUser(user), nil
}

func validUser(user identity.User) bool {
	return user.ID.Valid && !user.IsBlocked && user.AuthVersion > 0 &&
		(user.Role == "employee" || user.Role == "analyst" || user.Role == "admin")
}

func safeUser(user identity.User) User {
	return User{ID: user.ID.String(), Username: user.Username, Role: user.Role, AuthVersion: user.AuthVersion}
}

func verifyPassword(password, hash string) bool {
	if len(hash) > 1024 {
		return false
	}
	params, salt, key, err := argon2id.DecodeHash(hash)
	if err != nil || params.Parallelism == 0 || params.Iterations == 0 || params.Iterations > 10 ||
		params.Memory < 8*uint32(params.Parallelism) || params.Memory > 128*1024 ||
		len(salt) < 8 || len(salt) > 64 || len(key) < 16 || len(key) > 64 {
		return false
	}
	// Support existing library-default hashes (including host-dependent lanes),
	// but reject excessive memory/work before deriving the key.
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	return err == nil && match
}
