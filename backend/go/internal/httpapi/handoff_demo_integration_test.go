//go:build integration

package httpapi

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestDemoIncidentsPostgres(t *testing.T) {
	ids := testutil.Identity(t, "../../../../db/migrations/identity")
	reports := testutil.Incidents(t, "../../../../db/migrations/incidents")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	accounts := []demoseed.Account{
		{Username: "admin", Role: "admin", Password: rand.Text()},
		{Username: "admin_two", Role: "admin", Password: rand.Text()},
		{Username: "analyst", Role: "analyst", Password: rand.Text()},
		{Username: "employee_a", Role: "employee", Password: rand.Text()},
		{Username: "employee_b", Role: "employee", Password: rand.Text()},
	}
	seed := func(accounts []demoseed.Account) demoseed.Result {
		t.Helper()
		result, err := demoseed.Seed(ctx, ids.Migrator, accounts)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	seed(accounts[:4])
	users := identity.New(ids.Runtime)
	cfg := config.AuthConfig{Environment: "development", Lifetime: time.Hour, IdleTimeout: time.Minute}
	a, router := concurrencyApp(t, users, cfg, sessionstore.NewPostgres(ids.Runtime))
	api, err := NewIncidentAPI(db.New(reports.Runtime), users, a)
	if err != nil {
		t.Fatal(err)
	}
	api.EnableHandoff(reports.Runtime, ids.Runtime, true)
	api.Mount(router)
	const path = "/admin/demo-incidents"
	login := func(index int) *http.Cookie {
		t.Helper()
		acc := accounts[index]
		result := requestAccount(t, router, "POST", "/auth/login", map[string]string{"username": acc.Username, "password": acc.Password}, nil, 200)
		return sessionCookie(t, result, a.sessions.Cookie.Name)
	}
	admin, admin2, analyst, empA := login(0), login(1), login(2), login(3)
	requestAccount(t, router, "POST", path, struct{}{}, nil, 401)
	for _, cookie := range []*http.Cookie{analyst, empA} {
		requestAccount(t, router, "GET", path, nil, cookie, 403)
		requestAccount(t, router, "POST", path, struct{}{}, cookie, 403)
	}
	_, disabled := newSignupApp(t, ids, false)
	off := incidentJSON(t, requestAccount(t, disabled, "GET", path, nil, admin, 200))
	if off["enabled"] != false || off["seeded"] != false {
		t.Fatalf("disabled status: %v", off)
	}
	requestAccount(t, disabled, "POST", path, struct{}{}, admin, 404)
	requestAccount(t, router, "POST", path, map[string]any{"reporter_id": "arbitrary"}, admin, 400)
	requestAccount(t, router, "POST", path, nil, admin, 400)

	req := httptest.NewRequest("POST", "http://localhost"+path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://untrusted.example")
	req.AddCookie(admin)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("foreign-origin mutation returned %d", w.Code)
	}

	expectUnavailable := func() {
		t.Helper()
		w := requestAccount(t, router, "POST", path, struct{}{}, admin, 409)
		if !strings.Contains(w.Body.String(), `"code":"demo_accounts_unavailable"`) {
			t.Fatalf("missing prerequisite error: %s", w.Body.String())
		}
	}
	expectUnavailable() // Employee B does not exist yet.
	hash, err := auth.HashNewPassword(accounts[4].Password)
	if err != nil {
		t.Fatal(err)
	}
	empBRow, err := users.CreateUser(ctx, identity.CreateUserParams{
		Username: accounts[4].Username, Role: "employee", PasswordHash: hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := users.MarkDemoAccount(ctx, empBRow.ID); !isPGCode(err, "42501") {
		t.Fatalf("runtime could write demo account provenance: %v", err)
	}
	if r := seed(accounts[4:]); r.Created != 0 || r.Existing != 1 {
		t.Fatalf("demo seed did not preserve the preexisting account: %+v", r)
	}
	expectUnavailable() // Matching usernames do not qualify ordinary accounts.
	if _, err := ids.Migrator.Exec(ctx, "DELETE FROM users WHERE id=$1", empBRow.ID); err != nil {
		t.Fatal(err)
	}
	seed(accounts[4:])
	empBRow, err = users.GetUserByUsername(ctx, accounts[4].Username)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.SetUserRole(ctx, identity.SetUserRoleParams{ID: empBRow.ID, Role: "analyst"}); err != nil {
		t.Fatal(err)
	}
	expectUnavailable()
	if _, err := users.SetUserRole(ctx, identity.SetUserRoleParams{ID: empBRow.ID, Role: "employee"}); err != nil {
		t.Fatal(err)
	}
	if _, err := users.SetUserBlocked(ctx, identity.SetUserBlockedParams{ID: empBRow.ID, IsBlocked: true}); err != nil {
		t.Fatal(err)
	}
	expectUnavailable()
	if _, err := users.SetUserBlocked(ctx, identity.SetUserBlockedParams{ID: empBRow.ID, IsBlocked: false}); err != nil {
		t.Fatal(err)
	}
	empB := login(4)

	// Fail after an incident, snapshot and job were inserted; none may persist.
	if _, err := reports.Migrator.Exec(ctx, `ALTER TABLE incident_actions ADD CONSTRAINT test_reject_demo CHECK (action <> 'demo_seeded')`); err != nil {
		t.Fatal(err)
	}
	requestAccount(t, router, "POST", path, struct{}{}, admin, 503)
	var count int
	if err := reports.Migrator.QueryRow(ctx, `SELECT (SELECT count(*) FROM incidents)
		+ (SELECT count(*) FROM incident_contexts) + (SELECT count(*) FROM assessment_jobs)
		+ (SELECT count(*) FROM incident_actions) + (SELECT count(*) FROM demo_incident_batch)`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failure left %d records: %v", count, err)
	}
	if _, err := reports.Migrator.Exec(ctx, "ALTER TABLE incident_actions DROP CONSTRAINT test_reject_demo"); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	for _, cookie := range []*http.Cookie{admin, admin2} {
		go func(cookie *http.Cookie) {
			<-start
			responses <- authRequest(router, "POST", path, "{}", cookie)
		}(cookie)
	}
	close(start)
	outcomes := map[int]int{}
	for range 2 {
		w := <-responses
		outcomes[w.Code]++
		created := incidentJSON(t, w)["created"]
		if w.Header().Get("Set-Cookie") != "" ||
			(w.Code == 201 && created != float64(demoseed.IncidentCount)) ||
			(w.Code == 200 && created != float64(0)) {
			t.Fatalf("unexpected seed response: %d %s", w.Code, w.Body)
		}
	}
	if outcomes[201] != 1 || outcomes[200] != 1 {
		t.Fatalf("concurrent outcomes: %v", outcomes)
	}
	ready := incidentJSON(t, requestAccount(t, router, "GET", path, nil, admin, 200))
	if ready["enabled"] != true || ready["seeded"] != true {
		t.Fatalf("seeded status: %v", ready)
	}
	if _, err := reports.Runtime.Exec(ctx, "DELETE FROM demo_incident_batch"); !isPGCode(err, "42501") {
		t.Fatalf("runtime could reset the completed batch: %v", err)
	}

	var actor string
	for _, table := range []string{"incidents", "assessment_jobs", "incident_contexts"} {
		if err := reports.Migrator.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&count); err != nil || count != demoseed.IncidentCount {
			t.Fatalf("%s contains %d rows: %v", table, count, err)
		}
	}
	if err := reports.Runtime.QueryRow(ctx, "SELECT created_by FROM demo_incident_batch").Scan(&actor); err != nil {
		t.Fatal(err)
	}
	loadID := func(index int) string {
		t.Helper()
		user, err := users.GetUserByUsername(ctx, accounts[index].Username)
		if err != nil {
			t.Fatal(err)
		}
		return user.ID.String()
	}
	if actor != loadID(0) && actor != loadID(1) {
		t.Fatalf("batch actor %q was not one of its admins", actor)
	}
	err = reports.Migrator.QueryRow(ctx, `SELECT count(*) FROM incidents i
		JOIN incident_contexts c ON c.incident_id = i.id AND c.revision = i.context_revision
		  AND c.title = i.title AND c.description = i.description AND c.affected_system = i.affected_system
		JOIN assessment_jobs j ON j.context_id = c.id AND j.incident_id = i.id AND j.status = 'queued'
		JOIN incident_actions s ON s.incident_id = i.id AND s.action = 'submitted' AND s.actor_id = i.reporter_id
		JOIN incident_actions a ON a.incident_id = i.id AND a.action = 'demo_seeded' AND a.actor_id = $1
		WHERE i.status = 'submitted' AND i.category IS NULL AND i.severity IS NULL
		  AND i.assigned_analyst_id IS NULL AND i.title LIKE '[Demo] %'`, actor).Scan(&count)
	if err != nil || count != demoseed.IncidentCount {
		t.Fatalf("%d complete reports, snapshot/job pairs, and audit events: %v", count, err)
	}

	items := func(cookie *http.Cookie) []any {
		t.Helper()
		return incidentJSON(t, requestAccount(t, router, "GET", "/incidents", nil, cookie, 200))["incidents"].([]any)
	}
	counts := map[string]int{}
	titles := map[string]bool{}
	for _, row := range items(analyst) {
		incident := row.(map[string]any)
		title := incident["title"].(string)
		if titles[title] {
			t.Errorf("duplicate title %q", title)
		}
		titles[title] = true
		counts[incident["reporter_id"].(string)]++
	}
	if len(titles) != demoseed.IncidentCount || len(counts) != demoseed.ReporterCount ||
		counts[loadID(2)] < 3 || counts[loadID(2)] > 4 {
		t.Fatalf("incorrect reporter distribution: %v", counts)
	}
	for index, cookie := range []*http.Cookie{empA, empB} {
		reporter := loadID(3 + index)
		own := items(cookie)
		if len(own) != counts[reporter] || len(own) < 3 || len(own) > 4 {
			t.Fatalf("employee sees %d reports; expected %d owned reports", len(own), counts[reporter])
		}
		for _, row := range own {
			if row.(map[string]any)["reporter_id"] != reporter {
				t.Fatalf("employee can see another reporter's incident: %v", row)
			}
		}
	}
}
