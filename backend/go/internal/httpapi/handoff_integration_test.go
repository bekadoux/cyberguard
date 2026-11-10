//go:build integration

package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	incidentdb "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/demoseed"
	"github.com/bekadoux/cyberguard/backend/go/internal/sessionstore"
	"github.com/bekadoux/cyberguard/backend/go/internal/testutil"
)

func TestHandoffWorkflowDatabasePermissions(t *testing.T) {
	identities := testutil.Identity(t, "../../../../db/migrations/identity")
	reports := testutil.Incidents(t, "../../../../db/migrations/incidents")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	accounts := []demoseed.Account{
		{Username: "employee_a", Role: "employee", Password: rand.Text()},
		{Username: "employee_b", Role: "employee", Password: rand.Text()},
		{Username: "analyst", Role: "analyst", Password: rand.Text()},
		{Username: "admin", Role: "admin", Password: rand.Text()},
	}
	if _, err := demoseed.Seed(ctx, identities.Migrator, accounts); err != nil {
		t.Fatal(err)
	}
	users := identity.New(identities.Runtime)
	cfg := config.AuthConfig{Environment: "development", CookieSecure: false, Lifetime: time.Hour, IdleTimeout: 30 * time.Minute,
		TrustedOrigins: []string{"http://127.0.0.1:5173"}}
	authn, router := concurrencyApp(t, users, cfg, sessionstore.NewPostgres(identities.Runtime))
	authn.limiter = auth.NewLoginLimiter(1000, time.Minute, 1000)
	api, err := NewIncidentAPI(incidentdb.New(reports.Runtime), users, authn)
	if err != nil {
		t.Fatal(err)
	}
	api.EnableHandoff(reports.Runtime, identities.Runtime)
	api.Mount(router)
	login := func(account demoseed.Account) *http.Cookie {
		t.Helper()
		data, _ := json.Marshal(map[string]string{"username": account.Username, "password": account.Password})
		result := authRequest(router, http.MethodPost, "/auth/login", string(data), nil)
		if result.Code != 200 {
			t.Fatalf("login %s failed: %d", account.Username, result.Code)
		}
		return sessionCookie(t, result, authn.sessions.Cookie.Name)
	}
	emp1, emp2, analyst, admin := login(accounts[0]), login(accounts[1]), login(accounts[2]), login(accounts[3])
	request := func(method, path, body string, login *http.Cookie, want int) {
		t.Helper()
		result := authRequest(router, method, path, body, login)
		if result.Code != want {
			t.Fatalf("%s %s: got %d want %d; body: %s", method, path, result.Code, want, result.Body.String())
		}
	}
	created := authRequest(router, "POST", "/incidents", validIncident, emp1)
	if created.Code != 201 {
		t.Fatalf("create: %s", created.Body.String())
	}
	id := incidentJSON(t, created)["incident"].(map[string]any)["id"].(string)
	base := "/incidents/" + id
	request("GET", base+"/comments", "", emp2, 404)
	request("POST", base+"/claim", "{}", emp1, 403)
	request("POST", base+"/claim", "{}", analyst, 200)
	request("POST", base+"/claim", "{}", admin, 409)
	request("PATCH", base+"/fields", `{"category":"phishing","severity":"high"}`, analyst, 200)
	request("POST", base+"/transition", `{"status":"investigating"}`, analyst, 200)
	request("POST", base+"/comments", `{"text":"Internal evidence","visibility":"internal"}`, analyst, 201)
	request("GET", base+"/comments", "", emp1, 200)
	employeeComments := authRequest(router, "GET", base+"/comments", "", emp1)
	if len(incidentJSON(t, employeeComments)["comments"].([]any)) != 0 {
		t.Fatal("employee saw internal comments")
	}
	request("POST", base+"/comments", `{"text":"Can you share the email headers?","visibility":"public"}`, analyst, 201)
	request("POST", base+"/transition", `{"status":"awaiting_reporter"}`, analyst, 200)
	request("POST", base+"/comments", `{"text":"Here are the headers","visibility":"public"}`, emp1, 201)
	check := authRequest(router, "GET", base, "", analyst)
	if incidentJSON(t, check)["incident"].(map[string]any)["status"] != "investigating" {
		t.Fatal("reporter public reply did not resume investigation")
	}
	request("PATCH", base+"/fields", `{"resolution_note":"User was informed and sender blocked."}`, analyst, 200)
	request("POST", base+"/transition", `{"status":"resolved"}`, analyst, 200)
	request("POST", base+"/transition", `{"status":"closed"}`, emp2, 404)
	request("POST", base+"/transition", `{"status":"closed"}`, emp1, 200)
	request("POST", base+"/transition", `{"status":"investigating","reason":"Reopen for further review"}`, admin, 200)
	request("GET", base+"/history", "", analyst, 200)
	request("GET", base+"/history", "", emp1, 403)
	request("GET", "/notifications", "", emp1, 200)
	request("GET", "/admin/users", "", emp1, 403)
	request("GET", "/admin/users", "", admin, 200)
	request("POST", "/admin/users", `{"username":"new_employee","password":"a-long-test-password","role":"employee"}`, admin, 201)
	if _, err = reports.Runtime.Exec(ctx, `UPDATE incidents SET title='not allowed' WHERE id=$1`, id); err == nil {
		t.Fatal("runtime role changed immutable original title")
	}
}
