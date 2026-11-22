//go:build integration

package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/bekadoux/cyberguard/backend/go/internal/accountowner"
	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	"github.com/bekadoux/cyberguard/backend/go/internal/demoseed"
	"github.com/bekadoux/cyberguard/backend/go/internal/sessionstore"
	"github.com/bekadoux/cyberguard/backend/go/internal/testutil"
	"github.com/jackc/pgx/v5/pgconn"
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
	w := requestAccount(t, enabled, "POST", "/auth/login", body, nil, 200)
	cookie := sessionCookie(t, w, authn.sessions.Cookie.Name)
	requestAccount(t, enabled, "GET", "/admin/ownership", nil, cookie, 403)
	authn.signupLimiter = auth.NewLoginLimiter(1, time.Minute, 100)
	requestAccount(t, enabled, "POST", "/auth/signup", body, nil, 409)
	w = requestAccount(t, enabled, "POST", "/auth/signup", body, nil, 429)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("signup rate limit lacks Retry-After")
	}
	requestAccount(t, enabled, "POST", "/auth/login", body, nil, 200)
}

func TestOwnershipPostgres(t *testing.T) {
	fixture := testutil.Identity(t, "../../../../db/migrations/identity")
	ctx := context.Background()
	authn, router := newSignupApp(t, fixture, true)
	authn.limiter = auth.NewLoginLimiter(100, time.Minute, 100)
	authn.ownerConfirmationLimiter = auth.NewLoginLimiter(100, time.Minute, 100)
	password := rand.Text()
	if _, err := demoseed.Seed(ctx, fixture.Migrator, []demoseed.Account{
		{Username: "admin_one", Role: "admin", Password: password},
		{Username: "admin_two", Role: "admin", Password: password},
		{Username: "employee", Role: "employee", Password: password},
	}); err != nil {
		t.Fatal(err)
	}
	login := func(username, pass string) *http.Cookie {
		t.Helper()
		w := requestAccount(t, router, "POST", "/auth/login", map[string]string{"username": username, "password": pass}, nil, 200)
		return sessionCookie(t, w, authn.sessions.Cookie.Name)
	}
	c1, c2 := login("admin_one", password), login("admin_two", password)
	queries := identity.New(fixture.Runtime)
	id := func(username string) string {
		t.Helper()
		u, err := queries.GetUserByUsername(ctx, username)
		if err != nil {
			t.Fatal(err)
		}
		return u.ID.String()
	}
	first, second, employee := id("admin_one"), id("admin_two"), id("employee")
	admin := "/admin/users/"
	transfer := "/admin/ownership/transfer"
	w := requestAccount(t, router, "GET", "/admin/ownership", nil, c1, 200)
	if !strings.Contains(w.Body.String(), `"owner_id":null`) {
		t.Fatal("migration selected an owner automatically")
	}
	if err := accountowner.Initialize(ctx, fixture.Migrator, "employee", ""); !errors.Is(err, accountowner.ErrIneligible) {
		t.Fatalf("accepted an ineligible first owner: %v", err)
	}
	if err := accountowner.Initialize(ctx, fixture.Runtime, "admin_one", ""); err == nil {
		t.Fatal("runtime account inserted the ownership singleton")
	}
	if err := accountowner.Initialize(ctx, fixture.Migrator, "admin_one", ""); err != nil {
		t.Fatal(err)
	}
	if err := accountowner.Initialize(ctx, fixture.Migrator, "admin_two", ""); !errors.Is(err, accountowner.ErrInitialized) {
		t.Fatalf("could replace existing first owner by bootstrap: %v", err)
	}
	if _, err := fixture.Runtime.Exec(ctx, `UPDATE users SET is_blocked=TRUE WHERE id=$1`, first); !isPGCode(err, "23503") {
		t.Fatalf("database allowed blocking the owner: %v", err)
	}
	if _, err := fixture.Runtime.Exec(ctx, `DELETE FROM account_ownership`); !isPGCode(err, "42501") {
		t.Fatalf("runtime can delete ownership: %v", err)
	}
	requestAccount(t, router, "POST", admin+first+"/role", map[string]string{"role": "employee"}, c2, 403)
	requestAccount(t, router, "POST", admin+first+"/block", map[string]bool{"blocked": true}, c2, 403)
	requestAccount(t, router, "POST", admin+first+"/password", map[string]string{"password": rand.Text()}, c2, 403)
	requestAccount(t, router, "POST", admin+first+"/password", map[string]string{"password": rand.Text()}, c1, 403)
	requestAccount(t, router, "POST", "/admin/users", map[string]string{"username": "new_analyst", "password": password, "role": "analyst"}, c1, 400)
	requestAccount(t, router, "POST", "/admin/users", map[string]string{"username": "new_admin", "password": password, "role": "admin"}, c1, 400)
	requestAccount(t, router, "POST", "/admin/users", map[string]string{"username": "another_user", "password": password}, c1, 201)
	requestAccount(t, router, "POST", admin+id("another_user")+"/role", map[string]string{"role": "analyst"}, c1, 200)
	requestAccount(t, router, "POST", "/auth/signup", map[string]string{"username": "registered", "password": password}, nil, 201)
	registered := login("registered", password)
	requestAccount(t, router, "POST", admin+id("registered")+"/role", map[string]string{"role": "analyst"}, c1, 200)
	requestAccount(t, router, "GET", "/auth/me", nil, registered, 401)
	registered = login("registered", password)
	requestAccount(t, router, "POST", admin+id("registered")+"/block", map[string]bool{"blocked": true}, c1, 200)
	requestAccount(t, router, "GET", "/auth/me", nil, registered, 401)
	requestAccount(t, router, "POST", "/auth/login", map[string]string{"username": "registered", "password": password}, nil, 401)
	requestAccount(t, router, "POST", transfer, map[string]string{"user_id": first, "password": password}, c2, 403)
	requestAccount(t, router, "POST", transfer, map[string]string{"user_id": employee, "password": password}, c1, 409)
	requestAccount(t, router, "POST", admin+second+"/block", map[string]bool{"blocked": true}, c1, 200)
	requestAccount(t, router, "POST", transfer, map[string]string{"user_id": second, "password": password}, c1, 409)
	requestAccount(t, router, "POST", admin+second+"/block", map[string]bool{"blocked": false}, c1, 200)
	c2 = login("admin_two", password)
	requestAccount(t, router, "POST", transfer, map[string]string{"user_id": second, "password": "wrong-password"}, c1, 403)

	// An audit failure must roll back the earlier ownership UPDATE.
	if _, err := fixture.Migrator.Exec(ctx, `ALTER TABLE identity_audit ADD CONSTRAINT test_reject_transfer CHECK (action <> 'ownership_transferred')`); err != nil {
		t.Fatal(err)
	}
	payload := map[string]string{"user_id": second, "password": password}
	requestAccount(t, router, "POST", transfer, payload, c1, 503)
	owner, err := accountowner.Current(ctx, fixture.Runtime)
	if err != nil || owner.String() != first {
		t.Fatal("failed transfer changed owner")
	}
	if _, err := fixture.Migrator.Exec(ctx, `ALTER TABLE identity_audit DROP CONSTRAINT test_reject_transfer`); err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(payload)
	var wg sync.WaitGroup
	outcomes := make(chan int, 2)
	for range 2 {
		wg.Go(func() {
			outcomes <- authRequest(router, "POST", transfer, string(raw), c1).Code
		})
	}
	wg.Wait()
	close(outcomes)
	hits := map[int]int{}
	for code := range outcomes {
		hits[code]++
	}
	if hits[200] != 1 || hits[403] != 1 {
		t.Fatalf("concurrent transfer outcomes: %v", hits)
	}
	w = requestAccount(t, router, "GET", "/admin/ownership", nil, c1, 200)
	if !strings.Contains(w.Body.String(), second) {
		t.Fatal("ownership was not transferred or former owner lost admin access")
	}
	var auditCount int
	err = fixture.Runtime.QueryRow(ctx, `SELECT count(*) FROM identity_audit
        WHERE action='ownership_transferred' AND actor_id=$1 AND subject_id=$2`, first, second).Scan(&auditCount)
	if err != nil || auditCount != 1 {
		t.Fatal("transfer audit must contain one successful change")
	}
	requestAccount(t, router, "POST", admin+second+"/password", map[string]string{"password": rand.Text()}, c1, 403)
	newPassword := rand.Text()
	requestAccount(t, router, "POST", admin+second+"/password", map[string]string{"password": newPassword, "current_password": password}, c2, 200)
	requestAccount(t, router, "GET", "/auth/me", nil, c2, 401)
	c2 = login("admin_two", newPassword)
	requestAccount(t, router, "POST", admin+first+"/role", map[string]string{"role": "employee"}, c2, 200)
	requestAccount(t, router, "GET", "/auth/me", nil, c1, 401)
}

func TestInitializeFreshOwnerPostgres(t *testing.T) {
	fixture := testutil.Identity(t, "../../../../db/migrations/identity")
	authn, router := newSignupApp(t, fixture, false)
	password := rand.Text()
	hash, err := auth.HashNewPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if err := accountowner.Initialize(context.Background(), fixture.Migrator, "first_admin", hash); err != nil {
		t.Fatal(err)
	}
	w := requestAccount(t, router, "POST", "/auth/login", map[string]string{"username": "first_admin", "password": password}, nil, 200)
	requestAccount(t, router, "GET", "/admin/ownership", nil, sessionCookie(t, w, authn.sessions.Cookie.Name), 200)
}

func isPGCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
