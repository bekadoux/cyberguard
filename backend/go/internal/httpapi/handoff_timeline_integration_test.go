//go:build integration

package httpapi

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	db "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/demoseed"
	"github.com/bekadoux/cyberguard/backend/go/internal/sessionstore"
	"github.com/bekadoux/cyberguard/backend/go/internal/testutil"
)

func TestReporterTimelinePostgres(t *testing.T) {
	ids := testutil.Identity(t, "../../../../db/migrations/identity")
	reports := testutil.Incidents(t, "../../../../db/migrations/incidents")
	ctx := t.Context()
	accounts := []demoseed.Account{
		{Username: "reporter", Role: "employee", Password: rand.Text()},
		{Username: "handler", Role: "analyst", Password: rand.Text()},
		{Username: "manager", Role: "admin", Password: rand.Text()},
	}
	if _, err := demoseed.Seed(ctx, ids.Migrator, accounts); err != nil {
		t.Fatal(err)
	}
	users := identity.New(ids.Runtime)
	session, router := concurrencyApp(t, users, config.AuthConfig{
		Environment: "development", Lifetime: time.Hour, IdleTimeout: time.Hour / 2,
	}, sessionstore.NewPostgres(ids.Runtime))
	api, err := NewIncidentAPI(db.New(reports.Runtime), users, session)
	if err != nil {
		t.Fatal(err)
	}
	api.EnableHandoff(reports.Runtime, ids.Runtime, false)
	api.Mount(router)

	login := func(account demoseed.Account) *http.Cookie {
		t.Helper()
		body := fmt.Sprintf(`{"username":%q,"password":%q}`, account.Username, account.Password)
		w := authRequest(router, http.MethodPost, "/auth/login", body, nil)
		if w.Code != 200 {
			t.Fatalf("login failed: %d", w.Code)
		}
		return sessionCookie(t, w, session.sessions.Cookie.Name)
	}
	employee, analyst, admin := login(accounts[0]), login(accounts[1]), login(accounts[2])
	request := func(who *http.Cookie, method, path, body string, want int) map[string]any {
		t.Helper()
		w := authRequest(router, method, path, body, who)
		if w.Code != want || w.Header().Get("Set-Cookie") != "" {
			t.Fatalf("%s %s: %d, wanted %d without cookie: %s", method, path, w.Code, want, w.Body)
		}
		return incidentJSON(t, w)
	}
	create := func(who *http.Cookie) string {
		t.Helper()
		return request(who, "POST", "/incidents", validIncident, 201)["incident"].(map[string]any)["id"].(string)
	}
	base := "/incidents/" + create(employee)
	staff := "/incidents/" + create(admin)
	missing := request(employee, "GET", "/incidents/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/timeline", "", 404)
	for _, path := range []string{staff + "/timeline", "/incidents/bad-id/timeline"} {
		if got := request(employee, "GET", path, "", 404); !reflect.DeepEqual(got, missing) {
			t.Fatal("inaccessible timeline is distinguishable from a missing report")
		}
	}
	request(analyst, "GET", base+"/timeline", "", 404)
	request(admin, "GET", base+"/timeline", "", 404)
	request(admin, "GET", staff+"/timeline", "", 200)
	request(nil, "GET", base+"/timeline", "", 401)

	assignee, err := users.GetUserByUsername(ctx, "handler")
	if err != nil {
		t.Fatal(err)
	}
	assignment := fmt.Sprintf(`{"analyst_id":%q}`, assignee.ID.String())
	request(admin, "POST", base+"/assign", assignment, 200)
	request(analyst, "PATCH", base+"/fields", `{"category":"other","severity":"low","resolution_note":"Done."}`, 200)
	request(analyst, "POST", base+"/transition", `{"status":"investigating"}`, 200)
	request(admin, "POST", base+"/assign", assignment, 200)
	request(analyst, "POST", base+"/comments", `{"visibility":"internal","text":"sensitive note"}`, 201)
	request(analyst, "POST", base+"/transition", `{"status":"resolved"}`, 200)
	request(employee, "POST", base+"/transition", `{"status":"investigating","reason":"Please review again"}`, 200)

	path := base + "/timeline"
	var statuses []string
	var cursor string
	for range 6 {
		query := "?limit=1"
		if cursor != "" {
			query += "&cursor=" + url.QueryEscape(cursor)
		}
		page := request(employee, "GET", path+query, "", 200)
		events := page["events"].([]any)
		if len(events) != 1 {
			t.Fatalf("unexpected timeline page: %v", events)
		}
		event := events[0].(map[string]any)
		if len(event) != 2 {
			t.Fatalf("timeline exposed event metadata: %v", event)
		}
		if _, err := time.Parse(time.RFC3339Nano, event["occurred_at"].(string)); err != nil {
			t.Fatal("invalid timeline timestamp")
		}
		statuses = append(statuses, event["status"].(string))
		if page["next_cursor"] == nil {
			break
		}
		cursor = page["next_cursor"].(string)
		if len(statuses) == 1 {
			request(admin, "GET", staff+"/timeline?cursor="+url.QueryEscape(cursor), "", 400)
			request(admin, "GET", path+"?cursor="+url.QueryEscape(cursor), "", 404)
		}
	}
	if want := []string{"investigating", "resolved", "investigating", "triage", "submitted"}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("timeline = %v, want %v", statuses, want)
	}
	for _, query := range []string{"?limit=0", "?limit=1&limit=2", "?cursor=bad", "?internal=true"} {
		request(employee, "GET", path+query, "", 400)
	}
	request(admin, "POST", base+"/archive", `{"reason":"Internal decision"}`, 200)
	if got := request(employee, "GET", path+"?cursor="+url.QueryEscape(cursor), "", 404); !reflect.DeepEqual(got, missing) {
		t.Fatal("archived timeline is distinguishable from a missing report")
	}

	// Old assignment/reassignment audit rows store only the assignee. Even
	// though the current status is triage, do not invent a historical transition.
	staffID := staff[len("/incidents/"):]
	if _, err := reports.Migrator.Exec(ctx, `UPDATE incidents SET status='triage' WHERE id=$1`, staffID); err != nil {
		t.Fatal(err)
	}
	if _, err := reports.Migrator.Exec(ctx, `DELETE FROM incident_actions WHERE incident_id=$1`, staffID); err != nil {
		t.Fatal(err)
	}
	if _, err := reports.Migrator.Exec(ctx, `INSERT INTO incident_actions(incident_id,action,details)
		VALUES ($1,'assigned',jsonb_build_object('analyst_id',$2::text))`, staffID, assignee.ID); err != nil {
		t.Fatal(err)
	}
	page := request(admin, "GET", staff+"/timeline", "", 200)
	if events := page["events"].([]any); len(events) != 1 || events[0].(map[string]any)["status"] != "submitted" {
		t.Fatalf("legacy timeline invented a transition: %v", page)
	}

	// Multiple events can have the same timestamp. Paging in the middle of
	// that tie must continue in the same order as a single page.
	if _, err := reports.Migrator.Exec(ctx, `INSERT INTO incident_actions(incident_id, action, details, created_at)
		SELECT id, 'status_changed', jsonb_build_object('from','triage','to',s), created_at
		FROM incidents CROSS JOIN (VALUES ('investigating'),('false_positive')) AS states(s) WHERE id=$1`, staffID); err != nil {
		t.Fatal(err)
	}
	all := request(admin, "GET", staff+"/timeline", "", 200)["events"].([]any)
	if len(all) != 3 {
		t.Fatalf("fixture should return two tied transitions plus the submission: %v", all)
	}
	one := request(admin, "GET", staff+"/timeline?limit=1", "", 200)
	two := request(admin, "GET", staff+"/timeline?cursor="+url.QueryEscape(one["next_cursor"].(string)), "", 200)
	if got := append(one["events"].([]any), two["events"].([]any)...); !reflect.DeepEqual(got, all) || two["next_cursor"] != nil {
		t.Fatalf("timeline skipped or repeated an event at a timestamp tie: %v", got)
	}
}
