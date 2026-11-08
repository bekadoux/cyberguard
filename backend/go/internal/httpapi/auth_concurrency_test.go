package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/go-chi/chi/v5"
)

// Pause only the first successful load, after persistence has returned its data.
// The other application/store is independent and can complete revocation first.
type pausedSessionStore struct {
	scs.CtxStore
	paused atomic.Bool
	loaded chan struct{}
	resume chan struct{}
}

func (s *pausedSessionStore) FindCtx(ctx context.Context, token string) ([]byte, bool, error) {
	data, found, err := s.CtxStore.FindCtx(ctx, token)
	if err == nil && found && s.paused.CompareAndSwap(false, true) {
		close(s.loaded)
		select {
		case <-s.resume:
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	return data, found, err
}

func concurrentSessionRevocation(t *testing.T, users auth.Users, settings config.AuthConfig, first, second scs.CtxStore, loginBody string, rotate bool) *http.Cookie {
	t.Helper()
	newApp := func(store scs.Store) (*Authentication, *chi.Mux) {
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
	app, revoker := newApp(first)
	paused := &pausedSessionStore{CtxStore: second, loaded: make(chan struct{}), resume: make(chan struct{})}
	_, refresher := newApp(paused)
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
