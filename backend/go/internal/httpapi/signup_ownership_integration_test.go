//go:build integration

package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	"github.com/bekadoux/cyberguard/backend/go/internal/sessionstore"
	"github.com/bekadoux/cyberguard/backend/go/internal/testutil"
)

func newSignupApp(t *testing.T, fixture testutil.DatabaseFixture, enabled bool) (*Authentication, http.Handler) {
	t.Helper()
	authn, router := concurrencyApp(t, identity.New(fixture.Runtime), config.AuthConfig{
		Environment: "development", Lifetime: time.Hour, IdleTimeout: time.Minute, SignupEnabled: enabled,
	}, sessionstore.NewPostgres(fixture.Runtime))
	(&HandoffAPI{identityDB: fixture.Runtime, auth: authn}).mountAuxiliaryRoutes(router)
	return authn, router
}

func requestAccount(t *testing.T, router http.Handler, method, path string, body any, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w := authRequest(router, method, path, string(raw), cookie)
	if w.Code != want {
		t.Fatalf("%s %s: status %d, want %d; response: %s", method, path, w.Code, want, w.Body.String())
	}
	return w
}

func TestSignupPostgres(t *testing.T) {
	fixture := testutil.Identity(t, "../../../../db/migrations/identity")
	ctx := context.Background()
	_, disabled := newSignupApp(t, fixture, false)
	password := "  " + rand.Text() + "  "
	body := map[string]string{"username": " New_Employee ", "password": password}
	requestAccount(t, disabled, "POST", "/auth/signup", body, nil, 404)

	authn, enabled := newSignupApp(t, fixture, true)
	authn.signupLimiter = auth.NewLoginLimiter(100, time.Minute, 100)
	for _, role := range []string{"employee", "analyst", "admin"} {
		requestAccount(t, enabled, "POST", "/auth/signup", map[string]string{
			"username": "not_created", "password": password, "role": role,
		}, nil, 400)
	}
	for _, invalid := range []string{"short", strings.Repeat("é", 65), strings.Repeat(" ", 16)} {
		requestAccount(t, enabled, "POST", "/auth/signup", map[string]string{"username": "not_created", "password": invalid}, nil, 400)
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "http://localhost/auth/signup", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://untrusted.example")
	res := httptest.NewRecorder()
	enabled.ServeHTTP(res, req)
	if res.Code != 403 {
		t.Fatalf("foreign-origin signup returned %d", res.Code)
	}
	requestAccount(t, enabled, "POST", "/auth/signup", map[string]string{
		"username": "not_created", "password": strings.Repeat("x", 5000),
	}, nil, 413)

	// Competing registrations of the same normalized name must commit once.
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for _, spelling := range []string{" New_Employee ", "new_employee"} {
		go func(spelling string) {
			data, _ := json.Marshal(map[string]string{"username": spelling, "password": password})
			<-start
			results <- authRequest(enabled, "POST", "/auth/signup", string(data), nil)
		}(spelling)
	}
	close(start)
	counts := map[int]int{}
	for range 2 {
		w := <-results
		counts[w.Code]++
		if w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), password) || strings.Contains(w.Body.String(), "$argon2id$") {
			t.Fatal("signup leaked credentials or issued a cookie")
		}
		switch w.Code {
		case 201:
			if !strings.Contains(w.Body.String(), `"role":"employee"`) {
				t.Fatal("signup returned a non-employee identity")
			}
		case 409:
			if !strings.Contains(w.Body.String(), `"code":"username_taken"`) || strings.Contains(w.Body.String(), `"user"`) {
				t.Fatal("conflict leaked existing identity or used the wrong code")
			}
		}
	}
	if counts[201] != 1 || counts[409] != 1 {
		t.Fatalf("duplicate registration outcomes: %v", counts)
	}
	user, err := identity.New(fixture.Runtime).GetUserByUsername(ctx, "new_employee")
	if err != nil || user.IsBlocked || user.Role != "employee" || user.PasswordHash == password {
		t.Fatal("incorrect persisted signup identity")
	}
	valid, err := argon2id.ComparePasswordAndHash(password, user.PasswordHash)
	if err != nil || !valid {
		t.Fatal("password did not verify against the stored hash")
	}
	requestAccount(t, enabled, "POST", "/auth/login", body, nil, 200)
	authn.signupLimiter = auth.NewLoginLimiter(1, time.Minute, 100)
	requestAccount(t, enabled, "POST", "/auth/signup", body, nil, 409)
	w := requestAccount(t, enabled, "POST", "/auth/signup", body, nil, 429)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("signup rate limit lacks Retry-After")
	}
	requestAccount(t, enabled, "POST", "/auth/login", body, nil, 200)
}
