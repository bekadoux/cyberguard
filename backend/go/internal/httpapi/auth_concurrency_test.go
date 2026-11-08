package httpapi

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Pause either before lookup or after a successful load. These are different
// orderings: a missing old token and a loaded-but-subsequently-revoked token.
type pausedSessionStore struct {
	scs.CtxStore
	paused       atomic.Bool
	beforeLookup bool
	loaded       chan struct{}
	resume       chan struct{}
}

func (s *pausedSessionStore) FindCtx(ctx context.Context, token string) ([]byte, bool, error) {
	if s.beforeLookup && s.paused.CompareAndSwap(false, true) {
		if err := s.pause(ctx); err != nil {
			return nil, false, err
		}
	}
	data, found, err := s.CtxStore.FindCtx(ctx, token)
	if !s.beforeLookup && err == nil && found && s.paused.CompareAndSwap(false, true) {
		if err := s.pause(ctx); err != nil {
			return nil, false, err
		}
	}
	return data, found, err
}

func (s *pausedSessionStore) pause(ctx context.Context) error {
	close(s.loaded)
	select {
	case <-s.resume:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func concurrencyApp(t *testing.T, users auth.Users, settings config.AuthConfig, store scs.Store) (*Authentication, *chi.Mux) {
	t.Helper()
	app, err := NewAuthentication(users, store, settings, 10*time.Second, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	app.limiter = auth.NewLoginLimiter(1000, time.Minute, 1000)
	router := NewRouter(testLogger(), nil, time.Second)
	app.Mount(router)
	return app, router
}

func concurrentSessionRevocation(t *testing.T, users auth.Users, settings config.AuthConfig, first, second scs.CtxStore, loginBody string, rotate bool) *http.Cookie {
	t.Helper()
	app, revoker := concurrencyApp(t, users, settings, first)
	paused := &pausedSessionStore{CtxStore: second, loaded: make(chan struct{}), resume: make(chan struct{})}
	_, refresher := concurrencyApp(t, users, settings, paused)
	login := authRequest(revoker, http.MethodPost, "/auth/login", loginBody, nil)
	if login.Code != 200 {
		t.Fatal("initial login failed")
	}
	old := sessionCookie(t, login, app.sessions.Cookie.Name)
	// A separate session of the same account must not be affected.
	other := authRequest(revoker, http.MethodPost, "/auth/login", loginBody, nil)
	if other.Code != 200 {
		t.Fatal("independent login failed")
	}
	otherCookie := sessionCookie(t, other, app.sessions.Cookie.Name)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- authRequest(refresher, http.MethodGet, "/auth/me", "", old) }()
	resumed := false
	finished := false
	defer func() {
		if !resumed {
			close(paused.resume)
		}
		if !finished {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("delayed request did not stop")
			}
		}
	}()
	select {
	case <-paused.loaded:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not reach the load barrier")
	}
	var renewed *http.Cookie
	if rotate {
		w := authRequest(revoker, http.MethodPost, "/auth/login", loginBody, old)
		if w.Code != 200 {
			t.Fatal("concurrent rotation failed")
		}
		renewed = sessionCookie(t, w, app.sessions.Cookie.Name)
		if renewed.Value == old.Value {
			t.Fatal("rotation reused the old token")
		}
	} else if w := authRequest(revoker, http.MethodPost, "/auth/logout", "", old); w.Code != 204 {
		t.Fatal("concurrent logout failed")
	}
	close(paused.resume)
	resumed = true
	var delayed *httptest.ResponseRecorder
	select {
	case delayed = <-done:
		finished = true
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not finish")
	}
	if w := authRequest(revoker, http.MethodGet, "/auth/me", "", old); w.Code != 401 {
		t.Fatalf("concurrent refresh restored revoked token: old cookie returned %d", w.Code)
	}
	if delayed.Code != 401 || len(delayed.Result().Cookies()) != 0 || strings.Contains(delayed.Body.String(), `"user"`) {
		t.Fatal("revoked refresh must fail closed without overwriting the rotated cookie")
	}
	if renewed != nil && authRequest(revoker, http.MethodGet, "/auth/me", "", renewed).Code != 200 {
		t.Fatal("refresh invalidated the new rotated token")
	}
	if authRequest(revoker, http.MethodGet, "/auth/me", "", otherCookie).Code != 200 {
		t.Fatal("revocation affected an independent session")
	}
	return old
}

type missingCurrentUser struct{ auth.Users }

func (u missingCurrentUser) GetUserByID(context.Context, pgtype.UUID) (identity.User, error) {
	return identity.User{}, pgx.ErrNoRows
}

// Model an account-version change consistently for both login and current-user
// queries, without mutating the underlying fixture shared with other tests.
type versionedUsers struct {
	auth.Users
	offset atomic.Int64
}

func (u *versionedUsers) GetUserByID(ctx context.Context, id pgtype.UUID) (identity.User, error) {
	user, err := u.Users.GetUserByID(ctx, id)
	user.AuthVersion += u.offset.Load()
	return user, err
}

func (u *versionedUsers) GetUserByUsername(ctx context.Context, username string) (identity.User, error) {
	user, err := u.Users.GetUserByUsername(ctx, username)
	user.AuthVersion += u.offset.Load()
	return user, err
}

// Model browser response delivery explicitly: finish/apply the new login first,
// then apply an older response. Independent stores model separate API instances.
func sessionCookieOrdering(t *testing.T, users auth.Users, settings config.AuthConfig, first, second scs.CtxStore, loginBody, ordering string) {
	t.Helper()
	var account *versionedUsers
	if ordering == "old_account_version" {
		account = &versionedUsers{Users: users}
		users = account
	}
	app, router := concurrencyApp(t, users, settings, first)
	login := authRequest(router, http.MethodPost, "/auth/login", loginBody, nil)
	if login.Code != 200 {
		t.Fatal("initial login failed")
	}
	old := sessionCookie(t, login, app.sessions.Cookie.Name)
	if account != nil {
		account.offset.Store(1)
	}
	var delayed *httptest.ResponseRecorder
	want := http.StatusUnauthorized
	var finish func() *httptest.ResponseRecorder
	if ordering == "before_lookup" {
		paused := &pausedSessionStore{CtxStore: second, beforeLookup: true, loaded: make(chan struct{}), resume: make(chan struct{})}
		_, staleRouter := concurrencyApp(t, users, settings, paused)
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() { done <- authRequest(staleRouter, http.MethodGet, "/auth/me", "", old) }()
		resumed, finished := false, false
		defer func() {
			if !resumed {
				close(paused.resume)
			}
			if !finished {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("paused lookup did not stop")
				}
			}
		}()
		select {
		case <-paused.loaded:
		case <-time.After(5 * time.Second):
			t.Fatal("lookup did not reach the pre-load barrier")
		}
		finish = func() *httptest.ResponseRecorder {
			close(paused.resume)
			resumed = true
			select {
			case w := <-done:
				finished = true
				return w
			case <-time.After(5 * time.Second):
				t.Fatal("paused lookup did not finish")
				return nil
			}
		}
	} else {
		staleUsers := users
		if ordering == "destroyed_identity" {
			staleUsers = missingCurrentUser{Users: users}
		}
		_, staleRouter := concurrencyApp(t, staleUsers, settings, second)
		switch ordering {
		case "failed_login":
			delayed = authRequest(staleRouter, http.MethodPost, "/auth/login", `{"username":"employee_a","password":"incorrect"}`, old)
		case "invalid_body":
			delayed = authRequest(staleRouter, http.MethodPost, "/auth/login", "{", old)
			want = http.StatusBadRequest
		default:
			delayed = authRequest(staleRouter, http.MethodGet, "/auth/me", "", old)
			if ordering == "completed_me" {
				want = http.StatusOK
			}
		}
	}
	rotated := authRequest(router, http.MethodPost, "/auth/login", loginBody, old)
	if rotated.Code != 200 {
		t.Fatal("concurrent login failed")
	}
	fresh := sessionCookie(t, rotated, app.sessions.Cookie.Name)
	if fresh.Value == old.Value {
		t.Fatal("login did not rotate the token")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	scheme := "http"
	if settings.CookieSecure {
		scheme = "https"
	}
	origin, err := url.Parse(scheme + "://127.0.0.1:8080/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(origin, rotated.Result().Cookies())
	if finish != nil {
		delayed = finish()
	}
	if delayed.Code != want {
		t.Fatalf("delayed response: got %d, want %d", delayed.Code, want)
	}
	jar.SetCookies(origin, delayed.Result().Cookies())
	cookies := jar.Cookies(origin)
	if len(cookies) != 1 || cookies[0].Name != fresh.Name || cookies[0].Value != fresh.Value {
		t.Fatal("delayed response deleted or replaced the fresh login cookie")
	}
	if len(delayed.Result().Cookies()) != 0 {
		t.Fatal("passive or failed authentication response emitted Set-Cookie")
	}
	if authRequest(router, http.MethodGet, "/auth/me", "", cookies[0]).Code != 200 {
		t.Fatal("fresh browser session no longer authenticates")
	}
	if authRequest(router, http.MethodGet, "/auth/me", "", old).Code != 401 {
		t.Fatal("old cookie remained valid")
	}
}

func TestAuthHTTPCookieResponseOrdering(t *testing.T) {
	for _, secure := range []bool{false, true} {
		for _, ordering := range []string{"before_lookup", "completed_me", "failed_login", "invalid_body", "old_account_version", "destroyed_identity"} {
			environment := "development"
			if secure {
				environment = "production"
			}
			t.Run(environment+"/"+ordering, func(t *testing.T) {
				f := newAuthHTTPFixture(t, &config.AuthConfig{Environment: environment, CookieSecure: secure, Lifetime: time.Hour, IdleTimeout: 30 * time.Minute})
				store := f.app.sessions.Store.(scs.CtxStore)
				sessionCookieOrdering(t, f.users, f.app.settings, store, store, validLogin, ordering)
			})
		}
	}
}

func TestAuthHTTPConcurrentSessionRevocation(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		name := "logout_refresh"
		if rotate {
			name = "rotation_refresh"
		}
		t.Run(name, func(t *testing.T) {
			f := newAuthHTTPFixture(t, nil)
			store := f.app.sessions.Store.(scs.CtxStore)
			concurrentSessionRevocation(t, f.users, f.app.settings, store, store, validLogin, rotate)
		})
	}
}
