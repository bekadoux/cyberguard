package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	"github.com/bekadoux/cyberguard/backend/go/internal/sessionstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type authTestUsers struct {
	user identity.User
	err  error
}

func (u *authTestUsers) GetUserByUsername(ctx context.Context, name string) (identity.User, error) {
	if u.err != nil {
		return identity.User{}, u.err
	}
	if name != u.user.Username {
		return identity.User{}, pgx.ErrNoRows
	}
	return u.user, nil
}

func (u *authTestUsers) GetUserByID(ctx context.Context, id pgtype.UUID) (identity.User, error) {
	if u.err != nil {
		return identity.User{}, u.err
	}
	if id != u.user.ID {
		return identity.User{}, pgx.ErrNoRows
	}
	return u.user, nil
}

type authTestStore struct {
	*memstore.MemStore
	mu                               sync.Mutex
	failFind, failCommit, failDelete bool
}

func (s *authTestStore) FindCtx(ctx context.Context, token string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failFind {
		return nil, false, errors.New("sensitive-session-error")
	}
	return s.Find(token)
}

func (s *authTestStore) CreateCtx(ctx context.Context, token string, data []byte, expiry time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCommit {
		return errors.New("sensitive-session-error")
	}
	if _, found, err := s.Find(token); err != nil || found {
		return errors.New("session token collision")
	}
	return s.Commit(token, data, expiry)
}

func (s *authTestStore) RefreshCtx(ctx context.Context, token string, data []byte, expiry time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCommit {
		return errors.New("sensitive-session-error")
	}
	if _, found, err := s.Find(token); err != nil || !found {
		return sessionstore.ErrRevoked
	}
	return s.Commit(token, data, expiry)
}

func (s *authTestStore) DeleteCtx(ctx context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failDelete {
		return errors.New("sensitive-session-error")
	}
	return s.Delete(token)
}

type authHTTPFixture struct {
	app    *Authentication
	router *chi.Mux
	users  *authTestUsers
	store  *authTestStore
}

func newAuthHTTPFixture(t *testing.T, settings *config.AuthConfig) authHTTPFixture {
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
	users := &authTestUsers{user: identity.User{ID: id, Username: "employee_a", PasswordHash: hash, Role: "employee", AuthVersion: 1}}
	store := &authTestStore{MemStore: memstore.NewWithCleanupInterval(0)}
	if settings == nil {
		settings = &config.AuthConfig{
			Environment: "development", CookieSecure: false, TrustedOrigins: []string{"http://127.0.0.1:5173"},
			Lifetime: time.Hour, IdleTimeout: 30 * time.Minute,
		}
	}
	app, err := NewAuthentication(users, sessionstore.New(store), *settings, time.Second, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	app.limiter = auth.NewLoginLimiter(1000, time.Minute, 1000)
	router := NewRouter(testLogger(), nil, time.Second)
	app.Mount(router)
	return authHTTPFixture{app: app, router: router, users: users, store: store}
}

func authRequest(router http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("missing %s cookie", name)
	return nil
}

const validLogin = `{"username":"employee_a","password":"correct-test-password"}`

func TestAuthHTTPFlowAndRotation(t *testing.T) {
	f := newAuthHTTPFixture(t, nil)
	if w := authRequest(f.router, http.MethodGet, "/auth/me", "", nil); w.Code != 401 {
		t.Fatal("anonymous /me must fail")
	}
	login := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, nil)
	if login.Code != 200 {
		t.Fatal("login failed")
	}
	var response map[string]map[string]any
	if err := json.Unmarshal(login.Body.Bytes(), &response); err != nil || len(response["user"]) != 3 || response["user"]["username"] != "employee_a" {
		t.Fatal("login must return only the safe user DTO")
	}
	cookie := sessionCookie(t, login, f.app.sessions.Cookie.Name)
	if !cookie.HttpOnly || cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Domain != "" || cookie.Path != "/" || cookie.MaxAge <= 0 {
		t.Fatal("unexpected development cookie policy")
	}
	rows, err := f.store.All()
	if err != nil || len(rows) != 1 || rows[cookie.Value] != nil {
		t.Fatal("session store must use a hashed token, not the browser cookie")
	}
	if w := authRequest(f.router, http.MethodGet, "/auth/me", "", cookie); w.Code != 200 {
		t.Fatal("session did not authenticate /me")
	}
	second := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, cookie)
	rotated := sessionCookie(t, second, f.app.sessions.Cookie.Name)
	if second.Code != 200 || rotated.Value == cookie.Value {
		t.Fatal("login did not renew the session token")
	}
	if w := authRequest(f.router, http.MethodGet, "/auth/me", "", cookie); w.Code != 401 {
		t.Fatal("old token remained valid after renewal")
	}
	logout := authRequest(f.router, http.MethodPost, "/auth/logout", "", rotated)
	if logout.Code != 204 || logout.Body.Len() != 0 || sessionCookie(t, logout, f.app.sessions.Cookie.Name).MaxAge >= 0 {
		t.Fatal("logout did not destroy the session/cookie")
	}
	if w := authRequest(f.router, http.MethodGet, "/auth/me", "", rotated); w.Code != 401 {
		t.Fatal("logout left the token valid")
	}
	if w := authRequest(f.router, http.MethodPost, "/auth/logout", "", nil); w.Code != 204 {
		t.Fatal("logout must be idempotent")
	}
}

func TestAuthHTTPUniformFailuresAndRoles(t *testing.T) {
	f := newAuthHTTPFixture(t, nil)
	wrong := authRequest(f.router, http.MethodPost, "/auth/login", `{"username":"employee_a","password":"incorrect"}`, nil)
	unknown := authRequest(f.router, http.MethodPost, "/auth/login", `{"username":"unknown_user","password":"incorrect"}`, nil)
	f.users.user.IsBlocked = true
	blocked := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, nil)
	for _, w := range []*httptest.ResponseRecorder{wrong, unknown, blocked} {
		if w.Code != 401 || !bytes.Equal(w.Body.Bytes(), wrong.Body.Bytes()) || len(w.Result().Cookies()) != 0 {
			t.Fatal("credential failures disclose account state or create a session")
		}
	}
	f.users.user.IsBlocked = false
	for _, role := range []string{"employee", "analyst", "admin"} {
		f.users.user.Role = role
		w := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"role":"`+role+`"`) {
			t.Fatal("a supported role could not log in")
		}
	}
}

func TestAuthHTTPRevocation(t *testing.T) {
	f := newAuthHTTPFixture(t, nil)
	for _, mutate := range []func(){
		func() { f.users.user.IsBlocked = true; f.users.user.AuthVersion++ },
		func() { f.users.user.AuthVersion++ },
		func() { f.users.user.Role = "analyst"; f.users.user.AuthVersion++ },
	} {
		f.users.user.IsBlocked = false
		login := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, nil)
		cookie := sessionCookie(t, login, f.app.sessions.Cookie.Name)
		mutate()
		if w := authRequest(f.router, http.MethodGet, "/auth/me", "", cookie); w.Code != 401 {
			t.Fatal("stale or blocked session retained access")
		}
		f.users.user.IsBlocked = false
		if w := authRequest(f.router, http.MethodGet, "/auth/me", "", cookie); w.Code != 401 {
			t.Fatal("old session revived after account became active")
		}
	}
}

func TestAuthHTTPInvalidBodiesAndMethods(t *testing.T) {
	f := newAuthHTTPFixture(t, nil)
	for _, test := range []struct {
		body string
		want int
	}{
		{"{", 400}, {"null", 400}, {"[]", 400}, {"{}", 400},
		{validLogin + "{}", 400},
		{`{"username":"employee_a","password":"p","role":"admin"}`, 400},
		{`{"username":"employee_a","password":123}`, 400},
		{`{"username":"employee_a","password":"` + strings.Repeat("x", 129) + `"}`, 400},
		{`{"username":"` + strings.Repeat("x", 5000) + `","password":"p"}`, 413},
	} {
		if w := authRequest(f.router, http.MethodPost, "/auth/login", test.body, nil); w.Code != test.want {
			t.Fatalf("invalid body: got %d, want %d", w.Code, test.want)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(validLogin))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal("non-JSON login was accepted")
	}
	for _, test := range [][2]string{{"GET", "/auth/login"}, {"POST", "/auth/me"}, {"GET", "/auth/logout"}} {
		if w := authRequest(f.router, test[0], test[1], "", nil); w.Code != 405 {
			t.Fatal("unsupported auth method did not return 405")
		}
	}
}

func TestAuthHTTPBrowserPolicy(t *testing.T) {
	f := newAuthHTTPFixture(t, nil)
	var trustedCookie *http.Cookie
	for _, origin := range []string{"http://127.0.0.1:5173", "https://hostile.example"} {
		r := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:8080/auth/login", nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Access-Control-Request-Method", "POST")
		r.Header.Set("Access-Control-Request-Headers", "Content-Type")
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		if origin == "http://127.0.0.1:5173" {
			if w.Header().Get("Access-Control-Allow-Origin") != origin || w.Header().Get("Access-Control-Allow-Credentials") != "true" {
				t.Fatal("trusted credentialed preflight failed")
			}
		} else if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("untrusted origin received CORS permission")
		}
	}
	for _, origin := range []string{"http://127.0.0.1:5173", "https://hostile.example"} {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/auth/login", strings.NewReader(validLogin))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		want := 200
		if origin == "https://hostile.example" {
			want = 403
		}
		if w.Code != want {
			t.Fatalf("origin %s: got %d, want %d", origin, w.Code, want)
		}
		if want == 200 {
			trustedCookie = sessionCookie(t, w, f.app.sessions.Cookie.Name)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/auth/logout", nil)
	r.Header.Set("Origin", "https://hostile.example")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.AddCookie(trustedCookie)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != 403 || authRequest(f.router, http.MethodGet, "/auth/me", "", trustedCookie).Code != 200 {
		t.Fatal("cross-site logout was not rejected before session mutation")
	}
}

func TestAuthHTTPSecureDefaults(t *testing.T) {
	f := newAuthHTTPFixture(t, &config.AuthConfig{
		Environment: "production", CookieSecure: true, Lifetime: 8 * time.Hour, IdleTimeout: 30 * time.Minute,
	})
	w := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, nil)
	cookie := sessionCookie(t, w, "__Host-cyberguard_session")
	if !cookie.Secure || !cookie.HttpOnly || cookie.Domain != "" || cookie.Path != "/" {
		t.Fatal("production cookie does not meet __Host- requirements")
	}
	r := httptest.NewRequest(http.MethodGet, "https://api.example/auth/me", nil)
	r.Header.Set("Origin", "https://untrusted.example")
	w = httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("empty trusted origins enabled wildcard CORS")
	}
}

func TestAuthHTTPRateLimitDoesNotTrustForwardedHeaders(t *testing.T) {
	f := newAuthHTTPFixture(t, nil)
	f.app.limiter = auth.NewLoginLimiter(5, time.Minute, 10)
	for attempt := range 6 {
		r := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"employee_a","password":"wrong"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", "203.0.113."+string(rune('1'+attempt)))
		r.Header.Set("X-Real-IP", "203.0.113.99")
		r.RemoteAddr = "192.0.2.1:1000"
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		if attempt < 5 && w.Code != 401 {
			t.Fatal("early attempt was incorrectly limited")
		}
		if attempt == 5 && (w.Code != 429 || w.Header().Get("Retry-After") == "") {
			t.Fatal("spoofed forwarded headers bypassed rate limiting")
		}
	}
}

func TestAuthHTTPSessionFailuresNeverReturnSuccessData(t *testing.T) {
	f := newAuthHTTPFixture(t, nil)
	f.store.failCommit = true
	w := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, nil)
	if w.Code != 503 || strings.Contains(w.Body.String(), `"user"`) || len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), "sensitive") {
		t.Fatal("session commit failure leaked login success/cookie or store details")
	}
	f.store.failCommit = false
	login := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, nil)
	cookie := sessionCookie(t, login, f.app.sessions.Cookie.Name)
	f.store.failFind = true
	if w := authRequest(f.router, http.MethodGet, "/auth/me", "", cookie); w.Code != 503 || strings.Contains(w.Body.String(), "sensitive") {
		t.Fatal("session lookup failure did not fail closed")
	}
	if w := authRequest(f.router, http.MethodGet, "/healthz", "", cookie); w.Code != 200 {
		t.Fatal("healthz depends on session storage")
	}
	f.store.failFind = false
	f.store.failDelete = true
	if w := authRequest(f.router, http.MethodPost, "/auth/logout", "", cookie); w.Code != 503 {
		t.Fatal("failed logout reported success")
	}
	f.store.failDelete = false
	f.users.err = errors.New("sensitive-user-query-error")
	if w := authRequest(f.router, http.MethodGet, "/auth/me", "", cookie); w.Code != 503 || strings.Contains(w.Body.String(), "sensitive") {
		t.Fatal("identity lookup failure did not fail closed")
	}
}

func TestAuthHTTPLogsExcludeCredentialsAndStoreDetails(t *testing.T) {
	f := newAuthHTTPFixture(t, nil)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	app, err := NewAuthentication(f.users, sessionstore.New(f.store), f.app.settings, time.Second, logger)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(logger, nil, time.Second)
	app.Mount(router)
	f.store.failCommit = true
	authRequest(router, http.MethodPost, "/auth/login", validLogin, nil)
	f.store.failCommit = false
	login := authRequest(router, http.MethodPost, "/auth/login", validLogin, nil)
	cookie := sessionCookie(t, login, app.sessions.Cookie.Name)
	f.store.failFind = true
	authRequest(router, http.MethodGet, "/auth/me", "", cookie)
	for _, secret := range []string{"correct-test-password", f.users.user.PasswordHash, cookie.Value, "sensitive-session-error"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("authentication logs leaked credentials or diagnostics")
		}
	}
	if !strings.Contains(logs.String(), "session storage failed") {
		t.Fatal("safe session failure event was not logged")
	}
}
