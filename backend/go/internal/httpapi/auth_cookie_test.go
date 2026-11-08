package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthHTTPCookieExpiryAndIdleRefresh(t *testing.T) {
	f := newAuthHTTPFixture(t, nil)
	login := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, nil)
	if login.Code != 200 {
		t.Fatal("login failed")
	}
	cookie := sessionCookie(t, login, f.app.sessions.Cookie.Name)
	rows, err := f.store.All()
	if err != nil || len(rows) != 1 {
		t.Fatal("could not read session fixture")
	}
	for token, data := range rows {
		deadline, _, err := f.app.sessions.Codec.Decode(data)
		if err != nil {
			t.Fatal("could not decode session fixture")
		}
		// Persistent cookie must not expire at the first idle interval now that
		// passive refreshes no longer rewrite its Max-Age/Expires attributes.
		if cookie.Expires.Before(deadline) || cookie.Expires.After(deadline.Add(time.Second)) ||
			cookie.MaxAge < int(f.app.settings.Lifetime.Seconds())-5 ||
			cookie.MaxAge > int(f.app.settings.Lifetime.Seconds())+1 {
			t.Fatal("login cookie does not use the absolute session deadline")
		}
		if err := f.store.Commit(token, data, time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		w := authRequest(f.router, http.MethodGet, "/auth/me", "", cookie)
		if w.Code != 200 || len(w.Result().Cookies()) != 0 {
			t.Fatal("successful passive refresh mutated the browser cookie")
		}
		// The absolute lifetime must remain unchanged. PostgreSQL integration
		// separately inspects the stored idle expiry (MemStore does not expose it).
		refreshed, found, err := f.store.Find(token)
		if err != nil || !found {
			t.Fatal("refresh lost the session")
		}
		actualDeadline, _, err := f.app.sessions.Codec.Decode(refreshed)
		if err != nil || !actualDeadline.Equal(deadline) {
			t.Fatal("idle refresh extended the absolute lifetime")
		}
		// A retained browser cookie is not authority: server idle expiry wins.
		if err := f.store.Commit(token, refreshed, time.Now().Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
		w = authRequest(f.router, http.MethodGet, "/auth/me", "", cookie)
		if w.Code != 401 || len(w.Result().Cookies()) != 0 {
			t.Fatal("retained cookie bypassed server expiry or triggered cookie deletion")
		}
	}
}

func TestAuthHTTPPassiveDenialsNeverSetCookies(t *testing.T) {
	f := newAuthHTTPFixture(t, nil)
	for _, reason := range []string{"anonymous", "unknown_token", "blocked", "auth_version", "deleted_user", "store_find", "store_refresh", "user_query"} {
		t.Run(reason, func(t *testing.T) {
			login := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, nil)
			if login.Code != 200 {
				t.Fatal("login failed")
			}
			cookie := sessionCookie(t, login, f.app.sessions.Cookie.Name)
			original := f.users.user
			defer func() {
				f.users.user = original
				f.users.err = nil
				f.store.failFind, f.store.failCommit = false, false
			}()
			want := http.StatusUnauthorized
			switch reason {
			case "anonymous":
				cookie = nil
			case "unknown_token":
				cookie.Value = "unknown-session-token"
			case "blocked":
				f.users.user.IsBlocked = true
			case "auth_version":
				f.users.user.AuthVersion++
			case "deleted_user":
				f.users.user.ID.Valid = false
			case "store_find":
				f.store.failFind = true
				want = http.StatusServiceUnavailable
			case "store_refresh":
				f.store.failCommit = true
				want = http.StatusServiceUnavailable
			case "user_query":
				f.users.err = errors.New("sensitive-user-query-error")
				want = http.StatusServiceUnavailable
			}
			w := authRequest(f.router, http.MethodGet, "/auth/me", "", cookie)
			if w.Code != want || len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), `"user"`) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unsafe passive denial: status=%d", w.Code)
			}
		})
	}
}

func TestAuthHTTPRejectedOriginsCannotChangeSession(t *testing.T) {
	f := newAuthHTTPFixture(t, nil)
	login := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, nil)
	if login.Code != 200 {
		t.Fatal("login failed")
	}
	cookie := sessionCookie(t, login, f.app.sessions.Cookie.Name)
	for _, path := range []string{"/auth/login", "/auth/logout"} {
		for _, origin := range []string{"https://hostile.example", "null", "http://127.0.0.1:51730", ""} {
			r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080"+path, strings.NewReader(validLogin))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			if origin != "" {
				r.Header.Set("Origin", origin)
			}
			r.AddCookie(cookie)
			w := httptest.NewRecorder()
			f.router.ServeHTTP(w, r)
			if w.Code != 403 || len(w.Result().Cookies()) != 0 || w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("untrusted origin mutated or exposed authentication")
			}
			if authRequest(f.router, http.MethodGet, "/auth/me", "", cookie).Code != 200 {
				t.Fatal("CSRF-rejected request revoked the live session")
			}
		}
	}
}

func TestAuthHTTPFailedLoginDoesNotChangeExistingCookie(t *testing.T) {
	f := newAuthHTTPFixture(t, nil)
	login := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, nil)
	if login.Code != 200 {
		t.Fatal("login failed")
	}
	cookie := sessionCookie(t, login, f.app.sessions.Cookie.Name)
	for _, body := range []string{"{", "null", "{}", validLogin + "{}", `{"username":"employee_a","password":"incorrect"}`} {
		w := authRequest(f.router, http.MethodPost, "/auth/login", body, cookie)
		if w.Code < 400 || len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), `"user"`) {
			t.Fatal("failed login rewrote an existing browser session")
		}
		if authRequest(f.router, http.MethodGet, "/auth/me", "", cookie).Code != 200 {
			t.Fatal("failed login invalidated the existing session")
		}
	}
}
