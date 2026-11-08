package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alexedwards/argon2id"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type fakeUsers struct {
	user       identity.User
	current    *identity.User
	lookupErr  error
	currentErr error
}

func (f *fakeUsers) GetUserByUsername(_ context.Context, username string) (identity.User, error) {
	if f.lookupErr != nil {
		return identity.User{}, f.lookupErr
	}
	if username != f.user.Username {
		return identity.User{}, pgx.ErrNoRows
	}
	return f.user, nil
}

func (f *fakeUsers) GetUserByID(_ context.Context, id pgtype.UUID) (identity.User, error) {
	if f.currentErr != nil {
		return identity.User{}, f.currentErr
	}
	if id != f.user.ID {
		return identity.User{}, pgx.ErrNoRows
	}
	if f.current != nil {
		return *f.current, nil
	}
	return f.user, nil
}

func serviceFixture(t *testing.T) (*Service, *fakeUsers) {
	t.Helper()
	hash, err := argon2id.CreateHash("correct-test-password", &argon2id.Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	var id pgtype.UUID
	if err := id.Scan("12345678-1234-4234-8234-123456789abc"); err != nil {
		t.Fatal(err)
	}
	users := &fakeUsers{user: identity.User{ID: id, Username: "employee_a", PasswordHash: hash, Role: "employee", AuthVersion: 1}}
	return &Service{users: users, dummyHash: hash, slots: make(chan struct{}, 2), verify: verifyPassword}, users
}

func TestAuthenticate(t *testing.T) {
	t.Parallel()
	service, users := serviceFixture(t)
	for _, role := range []string{"employee", "analyst", "admin"} {
		users.user.Role = role
		user, err := service.Authenticate(context.Background(), " Employee_A ", "correct-test-password")
		if err != nil || user.ID != users.user.ID.String() || user.Role != role || user.AuthVersion != 1 {
			t.Fatal("valid account did not authenticate")
		}
	}
	for _, credentials := range [][2]string{
		{"employee_a", "wrong-password"}, {"unknown_user", "correct-test-password"},
		{"ab", "correct-test-password"}, {"employee_a", ""}, {"employee_a", strings.Repeat("x", 129)},
	} {
		if _, err := service.Authenticate(context.Background(), credentials[0], credentials[1]); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal("invalid credentials did not produce the common error")
		}
	}
	users.user.IsBlocked = true
	if _, err := service.Authenticate(context.Background(), "employee_a", "correct-test-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("blocked account did not produce the common error")
	}
}

func TestAuthenticateRechecksAccount(t *testing.T) {
	t.Parallel()
	for _, change := range []func(*identity.User){
		func(user *identity.User) { user.IsBlocked = true },
		func(user *identity.User) { user.AuthVersion++ },
		func(user *identity.User) { user.PasswordHash = "changed" },
		func(user *identity.User) { user.Role = "unknown" },
	} {
		service, users := serviceFixture(t)
		current := users.user
		change(&current)
		users.current = &current
		if _, err := service.Authenticate(context.Background(), "employee_a", "correct-test-password"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal("stale account snapshot was accepted")
		}
	}
}

func TestCurrentUserChecksFreshState(t *testing.T) {
	t.Parallel()
	service, users := serviceFixture(t)
	user, err := service.CurrentUser(context.Background(), users.user.ID.String(), 1)
	if err != nil || user.Username != "employee_a" {
		t.Fatal("valid session identity was not loaded")
	}
	for _, session := range []struct {
		id      string
		version int64
	}{{"invalid", 1}, {user.ID, 0}, {user.ID, 2}} {
		if _, err := service.CurrentUser(context.Background(), session.id, session.version); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal("invalid session identity was accepted")
		}
	}
	users.user.IsBlocked = true
	if _, err := service.CurrentUser(context.Background(), user.ID, 1); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("blocked user retained access")
	}
	users.user.IsBlocked = false
	users.user.AuthVersion++
	if _, err := service.CurrentUser(context.Background(), user.ID, 1); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("unblocking restored an old session version")
	}
}

func TestDependenciesAndCapacityFailClosed(t *testing.T) {
	t.Parallel()
	service, users := serviceFixture(t)
	users.lookupErr = errors.New("sensitive-database-details")
	if _, err := service.Authenticate(context.Background(), "employee_a", "correct-test-password"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("database failure must not authenticate")
	}
	users.currentErr = errors.New("sensitive-database-details")
	if _, err := service.CurrentUser(context.Background(), users.user.ID.String(), 1); !errors.Is(err, ErrUnavailable) {
		t.Fatal("current-user dependency failure must deny access")
	}
	service.slots <- struct{}{}
	service.slots <- struct{}{}
	if _, err := service.Authenticate(context.Background(), "employee_a", "correct-test-password"); !errors.Is(err, ErrBusy) {
		t.Fatal("verification concurrency is not bounded")
	}
	<-service.slots
	<-service.slots
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Authenticate(ctx, "employee_a", "correct-test-password"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("canceled authentication must fail closed")
	}
}

func TestUnsafeHashesAreRejectedBeforeDerivation(t *testing.T) {
	t.Parallel()
	service, users := serviceFixture(t)
	for _, hash := range []string{
		"not-a-hash", strings.Repeat("x", 1025),
		strings.Replace(users.user.PasswordHash, "m=8192", "m=4294967295", 1),
		strings.Replace(users.user.PasswordHash, "t=1", "t=0", 1),
		strings.Replace(users.user.PasswordHash, "p=1", "p=0", 1),
	} {
		users.user.PasswordHash = hash
		if _, err := service.Authenticate(context.Background(), "employee_a", "correct-test-password"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal("unsafe stored hash was accepted")
		}
	}
}

func TestNewService(t *testing.T) {
	_, users := serviceFixture(t)
	if _, err := NewService(nil); err == nil {
		t.Fatal("nil queries must be rejected")
	}
	service, err := NewService(users)
	if err != nil || cap(service.slots) != 2 || service.dummyHash == "" {
		t.Fatal("could not initialize bounded password verification")
	}
}
