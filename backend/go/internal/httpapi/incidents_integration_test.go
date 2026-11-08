//go:build integration

package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	db "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/demoseed"
	"github.com/bekadoux/cyberguard/backend/go/internal/sessionstore"
	"github.com/bekadoux/cyberguard/backend/go/internal/testutil"
)

func TestIncidentHTTPPostgresIntegration(t *testing.T) {
	usersDB := testutil.Identity(t, "../../../../db/migrations/identity")
	incidentDB := testutil.Incidents(t, "../../../../db/migrations/incidents")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	accounts := []demoseed.Account{
		{Username: "employee_a", Role: "employee", Password: rand.Text()},
		{Username: "employee_b", Role: "employee", Password: rand.Text()},
		{Username: "analyst", Role: "analyst", Password: rand.Text()},
		{Username: "admin", Role: "admin", Password: rand.Text()},
	}
	if _, err := demoseed.Seed(ctx, usersDB.Migrator, accounts); err != nil {
		t.Fatal("could not seed identity fixture")
	}
	users := identity.New(usersDB.Runtime)
	settings := config.AuthConfig{Environment: "development", CookieSecure: false, Lifetime: time.Hour, IdleTimeout: 30 * time.Minute, TrustedOrigins: []string{"http://127.0.0.1:5173"}}
	app, router := concurrencyApp(t, users, settings, sessionstore.NewPostgres(usersDB.Runtime))
	app.limiter = auth.NewLoginLimiter(1000, time.Minute, 1000)
	api, err := NewIncidentAPI(db.New(incidentDB.Runtime), users, app)
	if err != nil {
		t.Fatal(err)
	}
	api.Mount(router)
	login := func(account demoseed.Account) *http.Cookie {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": account.Username, "password": account.Password})
		w := authRequest(router, http.MethodPost, "/auth/login", string(body), nil)
		if w.Code != 200 {
			t.Fatal("database-backed login failed")
		}
		return sessionCookie(t, w, app.sessions.Cookie.Name)
	}
	a, b, analyst, admin := login(accounts[0]), login(accounts[1]), login(accounts[2]), login(accounts[3])
	w := authRequest(router, http.MethodPost, "/incidents", validIncident, a)
	if w.Code != 201 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("database-backed incident creation failed: %d", w.Code)
	}
	item := incidentJSON(t, w)["incident"].(map[string]any)
	id := item["id"].(string)
	if w := authRequest(router, http.MethodGet, "/incidents/"+id, "", b); w.Code != 404 {
		t.Fatal("employee read another employee's incident")
	}
	w = authRequest(router, http.MethodGet, "/incidents", "", b)
	if w.Code != 200 || len(incidentJSON(t, w)["incidents"].([]any)) != 0 {
		t.Fatal("employee queue leaked other reports")
	}
	analystUser, err := users.GetUserByUsername(ctx, "analyst")
	if err != nil {
		t.Fatal("could not resolve analyst fixture")
	}
	if _, err := incidentDB.Migrator.Exec(ctx, "UPDATE incidents SET category='phishing',severity='critical',assigned_analyst_id=$2,resolution_note='Outcome for the reporter' WHERE id=$1", id, analystUser.ID); err != nil {
		t.Fatal("could not prepare investigation fixture")
	}
	w = authRequest(router, http.MethodGet, "/incidents/"+id, "", a)
	item = incidentJSON(t, w)["incident"].(map[string]any)
	if w.Code != 200 || item["category"] != "phishing" || item["resolution_note"] != "Outcome for the reporter" || item["assigned_analyst"].(map[string]any)["username"] != "analyst" || strings.Contains(w.Body.String(), `"severity"`) || strings.Contains(w.Body.String(), "password_hash") {
		t.Fatal("incorrect reporter projection")
	}
	for _, cookie := range []*http.Cookie{analyst, admin} {
		w = authRequest(router, http.MethodGet, "/incidents/"+id, "", cookie)
		if w.Code != 200 || incidentJSON(t, w)["incident"].(map[string]any)["severity"] != "critical" {
			t.Fatal("professional projection lost severity")
		}
	}
	t.Run("failed session update cannot create incident", func(t *testing.T) {
		if _, err := usersDB.Migrator.Exec(ctx, "REVOKE UPDATE ON sessions FROM go_identity"); err != nil {
			t.Fatal("could not revoke refresh privilege")
		}
		defer func() {
			if _, err := usersDB.Migrator.Exec(ctx, "GRANT UPDATE ON sessions TO go_identity"); err != nil {
				t.Error("could not restore privilege")
			}
		}()
		w := authRequest(router, http.MethodPost, "/incidents", validIncident, a)
		var count int
		if err := incidentDB.Runtime.QueryRow(ctx, "SELECT count(*) FROM incidents").Scan(&count); err != nil || count != 1 {
			t.Fatal("session failure caused a business mutation")
		}
		if w.Code != 503 || len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), `"incident"`) {
			t.Fatal("session refresh failure leaked success")
		}
	})
	t.Run("actual blocked account cannot create incident", func(t *testing.T) {
		user, err := users.GetUserByUsername(ctx, "employee_b")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := users.SetUserBlocked(ctx, identity.SetUserBlockedParams{ID: user.ID, IsBlocked: true}); err != nil {
			t.Fatal("could not block fixture")
		}
		w := authRequest(router, http.MethodPost, "/incidents", validIncident, b)
		if w.Code != 401 || len(w.Result().Cookies()) != 0 {
			t.Fatal("blocked account created incident or changed cookie")
		}
	})
	t.Run("incident database failure is safe", func(t *testing.T) {
		incidentDB.Runtime.Close()
		w := authRequest(router, http.MethodGet, "/incidents", "", a)
		if w.Code != 503 || len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), "postgres") {
			t.Fatal("unavailable incident database did not fail safely")
		}
		if authRequest(router, http.MethodGet, "/healthz", "", nil).Code != 200 {
			t.Fatal("incident outage broke process liveness")
		}
	})
}
