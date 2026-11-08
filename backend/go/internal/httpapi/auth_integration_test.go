//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	"github.com/bekadoux/cyberguard/backend/go/internal/demoseed"
	"github.com/bekadoux/cyberguard/backend/go/internal/sessionstore"
	"github.com/bekadoux/cyberguard/backend/go/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuthenticationPostgresIntegration(t *testing.T) {
	fixture := testutil.Identity(t, "../../../../db/migrations/identity")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	accounts := []demoseed.Account{
		{Username: "employee_a", Role: "employee", Password: rand.Text()},
		{Username: "employee_b", Role: "employee", Password: rand.Text()},
		{Username: "analyst", Role: "analyst", Password: rand.Text()},
		{Username: "admin", Role: "admin", Password: rand.Text()},
	}
	if _, err := demoseed.Seed(ctx, fixture.Migrator, accounts); err != nil {
		t.Fatal("could not seed authentication fixtures")
	}
	queries := identity.New(fixture.Runtime)
	settings := config.AuthConfig{
		Environment: "development", CookieSecure: false, TrustedOrigins: []string{"http://127.0.0.1:5173"},
		Lifetime: 8 * time.Hour, IdleTimeout: 30 * time.Minute,
	}
	newApp := func() (*Authentication, *chi.Mux) {
		t.Helper()
		app, err := NewAuthentication(queries, sessionstore.NewPostgres(fixture.Runtime), settings, 2*time.Second, testLogger())
		if err != nil {
			t.Fatal("could not initialize authentication")
		}
		app.limiter = auth.NewLoginLimiter(1000, time.Minute, 1000)
		router := NewRouter(testLogger(), nil, time.Second)
		app.Mount(router)
		return app, router
	}
	app, router := newApp()
	// A separate connection pool and SCS manager model a second API process;
	// correctness must not depend on a shared Go mutex or session-store object.
	secondPool, err := pgxpool.NewWithConfig(ctx, fixture.Runtime.Config())
	if err != nil {
		t.Fatal("could not create independent session-store pool")
	}
	t.Cleanup(secondPool.Close)
	for _, rotate := range []bool{false, true} {
		name := "concurrent_logout_refresh"
		if rotate {
			name = "concurrent_rotation_refresh"
		}
		t.Run(name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"username": accounts[0].Username, "password": accounts[0].Password})
			old := concurrentSessionRevocation(t, queries, settings,
				sessionstore.NewPostgres(fixture.Runtime),
				sessionstore.NewPostgres(secondPool), string(body), rotate)
			hash := sha256.Sum256([]byte(old.Value))
			var rows int
			if err := fixture.Runtime.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE token = $1", base64.RawURLEncoding.EncodeToString(hash[:])).Scan(&rows); err != nil || rows != 0 {
				t.Fatal("revoked session row was recreated")
			}
		})
	}
	login := func(account demoseed.Account) *http.Cookie {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": account.Username, "password": account.Password})
		w := authRequest(router, http.MethodPost, "/auth/login", string(body), nil)
		if w.Code != 200 || strings.Contains(w.Body.String(), "password_hash") || strings.Contains(w.Body.String(), "auth_version") {
			t.Fatal("real database login failed or leaked private fields")
		}
		return sessionCookie(t, w, app.sessions.Cookie.Name)
	}
	// Test-only knowledge of the pinned SCS token-hashing format; production
	// delegates serialization/hashing to SCS and persistence to sessionstore.
	storedToken := func(cookie *http.Cookie) string {
		hash := sha256.Sum256([]byte(cookie.Value))
		return base64.RawURLEncoding.EncodeToString(hash[:])
	}

	t.Run("all roles login me logout", func(t *testing.T) {
		for _, account := range accounts {
			cookie := login(account)
			w := authRequest(router, http.MethodGet, "/auth/me", "", cookie)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"role":"`+account.Role+`"`) {
				t.Fatal("current-user response has incorrect role")
			}
			if w := authRequest(router, http.MethodPost, "/auth/logout", "", cookie); w.Code != 204 {
				t.Fatal("real database logout failed")
			}
			if w := authRequest(router, http.MethodGet, "/auth/me", "", cookie); w.Code != 401 {
				t.Fatal("logout did not revoke stored session")
			}
		}
	})

	t.Run("stored token safe payload and expiry bounds", func(t *testing.T) {
		cookie := login(accounts[0])
		var token string
		var data []byte
		var expiry time.Time
		if err := fixture.Runtime.QueryRow(ctx, "SELECT token, data, expiry FROM sessions WHERE token = $1", storedToken(cookie)).Scan(&token, &data, &expiry); err != nil {
			t.Fatal("could not read hashed session fixture")
		}
		deadline, values, err := app.sessions.Codec.Decode(data)
		if err != nil || token == cookie.Value || len(values) != 2 || values["user_id"] == nil || values["auth_version"] == nil {
			t.Fatal("session contains unexpected identity data")
		}
		if bytes.Contains(data, []byte(accounts[0].Password)) || deadline.Before(time.Now().Add(7*time.Hour)) || expiry.After(time.Now().Add(31*time.Minute)) {
			t.Fatal("unsafe session payload or expiry policy")
		}
		authRequest(router, http.MethodGet, "/auth/me", "", cookie)
		if err := fixture.Runtime.QueryRow(ctx, "SELECT data FROM sessions WHERE token = $1", token).Scan(&data); err != nil {
			t.Fatal("could not read refreshed session")
		}
		refreshed, _, err := app.sessions.Codec.Decode(data)
		if err != nil || !deadline.Equal(refreshed) {
			t.Fatal("idle refresh changed the absolute lifetime")
		}
		body, _ := json.Marshal(map[string]string{"username": accounts[0].Username, "password": accounts[0].Password})
		w := authRequest(router, http.MethodPost, "/auth/login", string(body), cookie)
		rotated := sessionCookie(t, w, app.sessions.Cookie.Name)
		if rotated.Value == cookie.Value || authRequest(router, http.MethodGet, "/auth/me", "", cookie).Code != 401 {
			t.Fatal("login did not invalidate the old PostgreSQL-backed token")
		}
	})

	t.Run("new application instance restores session", func(t *testing.T) {
		cookie := login(accounts[1])
		_, restarted := newApp()
		if w := authRequest(restarted, http.MethodGet, "/auth/me", "", cookie); w.Code != 200 {
			t.Fatal("session did not survive session-manager recreation")
		}
	})

	t.Run("logout affects only its current session", func(t *testing.T) {
		first, second := login(accounts[1]), login(accounts[1])
		if first.Value == second.Value {
			t.Fatal("independent logins reused one token")
		}
		if w := authRequest(router, http.MethodPost, "/auth/logout", "", first); w.Code != 204 {
			t.Fatal("could not log out the first session")
		}
		if authRequest(router, http.MethodGet, "/auth/me", "", first).Code != 401 || authRequest(router, http.MethodGet, "/auth/me", "", second).Code != 200 {
			t.Fatal("logout did not isolate the current session")
		}
	})

	t.Run("blocking unblocking password reset and role change", func(t *testing.T) {
		cookie := login(accounts[0])
		otherCookie := login(accounts[0])
		user, err := queries.GetUserByUsername(ctx, accounts[0].Username)
		if err != nil {
			t.Fatal("could not load account")
		}
		if _, err := queries.SetUserBlocked(ctx, identity.SetUserBlockedParams{ID: user.ID, IsBlocked: true}); err != nil {
			t.Fatal("could not block account")
		}
		if authRequest(router, http.MethodGet, "/auth/me", "", cookie).Code != 401 || authRequest(router, http.MethodGet, "/auth/me", "", otherCookie).Code != 401 {
			t.Fatal("blocked account retained access")
		}
		if _, err := queries.SetUserBlocked(ctx, identity.SetUserBlockedParams{ID: user.ID, IsBlocked: false}); err != nil {
			t.Fatal("could not unblock account")
		}
		if w := authRequest(router, http.MethodGet, "/auth/me", "", cookie); w.Code != 401 {
			t.Fatal("unblocking restored an old session")
		}
		cookie = login(accounts[0])
		otherCookie = login(accounts[0])
		oldPassword := accounts[0].Password
		accounts[0].Password = rand.Text()
		hash, err := argon2id.CreateHash(accounts[0].Password, argon2id.DefaultParams)
		if err != nil {
			t.Fatal("could not prepare password-reset fixture")
		}
		if _, err := queries.SetUserPassword(ctx, identity.SetUserPasswordParams{ID: user.ID, PasswordHash: hash}); err != nil {
			t.Fatal("could not reset password")
		}
		if authRequest(router, http.MethodGet, "/auth/me", "", cookie).Code != 401 || authRequest(router, http.MethodGet, "/auth/me", "", otherCookie).Code != 401 {
			t.Fatal("password reset did not revoke session")
		}
		body, _ := json.Marshal(map[string]string{"username": accounts[0].Username, "password": oldPassword})
		if w := authRequest(router, http.MethodPost, "/auth/login", string(body), nil); w.Code != 401 {
			t.Fatal("old password remained valid")
		}
		cookie = login(accounts[0])
		otherCookie = login(accounts[0])
		if _, err := queries.SetUserRole(ctx, identity.SetUserRoleParams{ID: user.ID, Role: "analyst"}); err != nil {
			t.Fatal("could not change role")
		}
		if authRequest(router, http.MethodGet, "/auth/me", "", cookie).Code != 401 || authRequest(router, http.MethodGet, "/auth/me", "", otherCookie).Code != 401 {
			t.Fatal("role change did not revoke session")
		}
		cookie = login(accounts[0])
		if w := authRequest(router, http.MethodGet, "/auth/me", "", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"role":"analyst"`) {
			t.Fatal("fresh login did not obtain the current role")
		}
	})

	t.Run("expired sessions and bounded housekeeping", func(t *testing.T) {
		cookie := login(accounts[1])
		if _, err := fixture.Runtime.Exec(ctx, "UPDATE sessions SET expiry = current_timestamp - interval '1 second' WHERE token = $1", storedToken(cookie)); err != nil {
			t.Fatal("could not expire test session")
		}
		if w := authRequest(router, http.MethodGet, "/auth/me", "", cookie); w.Code != 401 {
			t.Fatal("expired session retained access")
		}
		liveCookie := login(accounts[2])
		if err := auth.CleanupExpiredSessions(ctx, fixture.Runtime, time.Second); err != nil {
			t.Fatal("session housekeeping failed")
		}
		var expired int
		if err := fixture.Runtime.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE expiry < current_timestamp").Scan(&expired); err != nil || expired != 0 {
			t.Fatal("expired session rows were not cleaned")
		}
		if w := authRequest(router, http.MethodGet, "/auth/me", "", liveCookie); w.Code != 200 {
			t.Fatal("housekeeping deleted a live session")
		}
	})

	t.Run("failed database writes cannot return login success", func(t *testing.T) {
		cookie := login(accounts[3])
		if _, err := fixture.Migrator.Exec(ctx, "REVOKE INSERT, UPDATE ON sessions FROM go_identity"); err != nil {
			t.Fatal("could not prepare session-store failure")
		}
		defer func() {
			if _, err := fixture.Migrator.Exec(ctx, "GRANT INSERT, UPDATE ON sessions TO go_identity"); err != nil {
				t.Error("could not restore session grants")
			}
		}()
		body, _ := json.Marshal(map[string]string{"username": accounts[3].Username, "password": accounts[3].Password})
		w := authRequest(router, http.MethodPost, "/auth/login", string(body), nil)
		if w.Code != 503 || strings.Contains(w.Body.String(), `"user"`) || len(w.Result().Cookies()) != 0 {
			t.Fatal("a real failed session commit returned success data/cookie")
		}
		w = authRequest(router, http.MethodGet, "/auth/me", "", cookie)
		if w.Code != 503 || strings.Contains(w.Body.String(), `"user"`) || len(w.Result().Cookies()) != 0 {
			t.Fatal("a real failed session refresh returned identity data/cookie")
		}
	})

	t.Run("database loss denies access but preserves liveness", func(t *testing.T) {
		cookie := login(accounts[3])
		fixture.Runtime.Close()
		if w := authRequest(router, http.MethodGet, "/auth/me", "", cookie); w.Code != 503 {
			t.Fatal("unavailable session dependency did not deny access")
		}
		if w := authRequest(router, http.MethodGet, "/healthz", "", cookie); w.Code != 200 {
			t.Fatal("session failure broke process liveness")
		}
	})
}
