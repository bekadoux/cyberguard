//go:build integration

package httpapi

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	incidentdb "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/demoseed"
	"github.com/bekadoux/cyberguard/backend/go/internal/sessionstore"
	"github.com/bekadoux/cyberguard/backend/go/internal/testutil"
)

func TestHandoffWorkflowRegressions(t *testing.T) {
	ctx := t.Context()
	ids := testutil.Identity(t, "../../../../db/migrations/identity")
	reports := testutil.Incidents(t, "../../../../db/migrations/incidents")
	users := identity.New(ids.Runtime)
	accounts := []demoseed.Account{
		{Username: "reporter", Role: "employee", Password: rand.Text()},
		{Username: "handler", Role: "analyst", Password: rand.Text()},
		{Username: "manager", Role: "admin", Password: rand.Text()},
	}
	if _, err := demoseed.Seed(ctx, ids.Migrator, accounts); err != nil {
		t.Fatal(err)
	}
	session, router := concurrencyApp(t, users, config.AuthConfig{
		Environment: "development", Lifetime: time.Hour, IdleTimeout: time.Hour / 2,
		TrustedOrigins: []string{"http://127.0.0.1:5173"},
	}, sessionstore.NewPostgres(ids.Runtime))
	api, err := NewIncidentAPI(incidentdb.New(reports.Runtime), users, session)
	if err != nil {
		t.Fatal(err)
	}
	api.EnableHandoff(reports.Runtime, ids.Runtime, false)
	api.Mount(router)
	login := func(account demoseed.Account) *http.Cookie {
		t.Helper()
		body := fmt.Sprintf(`{"username":%q,"password":%q}`, account.Username, account.Password)
		w := authRequest(router, "POST", "/auth/login", body, nil)
		if w.Code != 200 {
			t.Fatalf("login: %d", w.Code)
		}
		return sessionCookie(t, w, session.sessions.Cookie.Name)
	}
	employee, analyst, admin := login(accounts[0]), login(accounts[1]), login(accounts[2])
	analystUser, err := users.GetUserByUsername(ctx, accounts[1].Username)
	if err != nil {
		t.Fatal(err)
	}
	request := func(t *testing.T, who *http.Cookie, method, path, body string, want int) map[string]any {
		t.Helper()
		w := authRequest(router, method, path, body, who)
		if w.Code != want {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, w.Code, want, w.Body)
		}
		return incidentJSON(t, w)
	}
	create := func(t *testing.T, who *http.Cookie) string {
		t.Helper()
		return request(t, who, "POST", "/incidents", validIncident, 201)["incident"].(map[string]any)["id"].(string)
	}
	t.Run("unanswered questions and resolution review", func(t *testing.T) {
		// A staff reporter can also write internal notes. Those must never
		// answer a question or request a resolution review.
		id := create(t, admin)
		path := "/incidents/" + id
		post := func(who *http.Cookie, endpoint, body string, want int) {
			t.Helper()
			request(t, who, "POST", path+endpoint, body, want)
		}
		pub := `{"text":"Public follow-up","visibility":"public"}`
		internal := `{"text":"Internal note","visibility":"internal"}`
		wait := `{"status":"awaiting_reporter"}`
		resolve := `{"status":"resolved"}`
		post(analyst, "/claim", "{}", 200)
		request(t, analyst, "PATCH", path+"/fields", `{"category":"other","severity":"low","resolution_note":"Complete."}`, 200)
		post(analyst, "/comments", pub, 201)
		post(analyst, "/transition", `{"status":"investigating"}`, 200)
		post(analyst, "/transition", wait, 409) // prior triage comment
		post(analyst, "/comments", pub, 201)
		post(admin, "/comments", pub, 201)
		post(analyst, "/transition", wait, 409) // already answered before wait
		for range 2 {
			post(analyst, "/comments", pub, 201)
			post(admin, "/comments", internal, 201)
			post(analyst, "/transition", wait, 200)
			post(admin, "/comments", internal, 201)
			post(admin, "/comments", pub, 201)
			post(analyst, "/transition", wait, 409) // no reuse across cycles
		}
		assertReview := func(wantStatus string, wantCount int) {
			t.Helper()
			var status string
			var cleared bool
			var audits, notifications int
			err := reports.Runtime.QueryRow(ctx, `SELECT i.status,r.resolved_at IS NULL,
				(SELECT count(*) FROM incident_actions WHERE incident_id=i.id AND actor_id=i.reporter_id
				  AND action='status_changed' AND details @> '{"from":"resolved","to":"investigating"}'),
				(SELECT count(*) FROM incident_notifications WHERE incident_id=i.id
				  AND recipient_id=i.assigned_analyst_id AND event_type='review_requested')
				FROM incidents i JOIN incident_resolutions r ON r.incident_id=i.id WHERE i.id=$1`, id).
				Scan(&status, &cleared, &audits, &notifications)
			if err != nil || status != wantStatus || cleared != (status == "investigating") || audits != wantCount || notifications != wantCount {
				t.Fatalf("status=%q cleared=%t audits=%d notifications=%d err=%v", status, cleared, audits, notifications, err)
			}
		}
		post(analyst, "/transition", resolve, 200)
		post(admin, "/comments", internal, 201)
		post(analyst, "/comments", pub, 201)
		assertReview("resolved", 0)
		post(admin, "/comments", pub, 201)
		assertReview("investigating", 1)
		post(analyst, "/transition", resolve, 200)
		post(admin, "/transition", `{"status":"investigating","reason":"Please review."}`, 200)
		assertReview("investigating", 2)
	})

	t.Run("self-assigned formal review sends one notification", func(t *testing.T) {
		id := create(t, analyst)
		path := "/incidents/" + id
		request(t, analyst, "POST", path+"/claim", "{}", 200)
		request(t, analyst, "PATCH", path+"/fields", `{"category":"other","severity":"low","resolution_note":"Done."}`, 200)
		request(t, analyst, "POST", path+"/transition", `{"status":"investigating"}`, 200)
		request(t, analyst, "POST", path+"/transition", `{"status":"resolved"}`, 200)
		notifications := func() (total, review int) {
			t.Helper()
			err := reports.Runtime.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE event_type='review_requested')
				FROM incident_notifications WHERE incident_id=$1 AND recipient_id=$2`, id, analystUser.ID).Scan(&total, &review)
			if err != nil {
				t.Fatal(err)
			}
			return total, review
		}
		before, _ := notifications()
		request(t, analyst, "POST", path+"/transition", `{"status":"investigating","reason":"Needs review."}`, 200)
		if after, reviews := notifications(); after != before+1 || reviews != 1 {
			t.Fatalf("wanted only the review notification, got %d new notifications (%d reviews)", after-before, reviews)
		}
	})

	t.Run("recent visible windows in chronological order", func(t *testing.T) {
		id := create(t, employee)
		path := "/incidents/" + id
		// Groups share timestamps, including across the 500-row cutoff.
		for _, stmt := range []string{
			`INSERT INTO incident_comments(id,incident_id,author_id,visibility,body,created_at)
			 SELECT lpad(to_hex(n),32,'0')::uuid,$1,$2,CASE WHEN n>510 THEN 'internal' ELSE 'public' END,
			 n::text,'2100-01-01'::timestamptz + (n/20)*interval '1 second' FROM generate_series(1,520) n`,
			`INSERT INTO incident_actions(id,incident_id,actor_id,action,created_at)
			 SELECT lpad(to_hex(n),32,'0')::uuid,$1,$2,n::text,
			 '2100-01-01'::timestamptz + (n/20)*interval '1 second' FROM generate_series(1,520) n`,
		} {
			if _, err := reports.Migrator.Exec(ctx, stmt, id, analystUser.ID); err != nil {
				t.Fatal(err)
			}
		}
		for _, tc := range []struct {
			who   *http.Cookie
			list  string
			field string
			first int
		}{
			{employee, "comments", "text", 11},
			{analyst, "comments", "text", 21},
			{analyst, "history", "action", 21},
		} {
			items := request(t, tc.who, "GET", path+"/"+tc.list, "", 200)[tc.list].([]any)
			if len(items) != 500 {
				t.Fatalf("%s returned %d rows", tc.list, len(items))
			}
			for i, item := range items {
				if value := item.(map[string]any)[tc.field]; value != fmt.Sprint(tc.first+i) {
					t.Fatalf("%s[%d] = %v, want %d", tc.list, i, value, tc.first+i)
				}
			}
		}
	})

	t.Run("notification scope after demotion and archive", func(t *testing.T) {
		other, own := create(t, employee), create(t, analyst)
		seed := func(id any) string {
			t.Helper()
			var nid string
			if err := reports.Migrator.QueryRow(ctx, `INSERT INTO incident_notifications(recipient_id,incident_id,event_type,message)
				VALUES($1,$2,'scope_test','Fixture') RETURNING id::text`, analystUser.ID, id).Scan(&nid); err != nil {
				t.Fatal(err)
			}
			return nid
		}
		otherNotice, ownNotice, globalNotice := seed(other), seed(own), seed(nil)
		assertScope := func(who *http.Cookie, id string, visible bool) {
			t.Helper()
			rows := request(t, who, "GET", "/notifications", "", 200)["notifications"].([]any)
			found := false
			for _, row := range rows {
				found = found || row.(map[string]any)["id"] == id
			}
			if found != visible {
				t.Fatalf("notification %s listed=%t, want %t", id, found, visible)
			}
			want := 404
			if visible {
				want = 200
			}
			for _, action := range []string{"read", "unread"} {
				request(t, who, "POST", "/notifications/"+id+"/"+action, "{}", want)
			}
		}
		assertScope(analyst, otherNotice, true)
		assertScope(admin, otherNotice, false)
		request(t, admin, "POST", "/admin/users/"+analystUser.ID.String()+"/role", `{"role":"employee","reason":"Scope test"}`, 200)
		demoted := login(accounts[1])
		assertScope(demoted, otherNotice, false)
		assertScope(demoted, ownNotice, true)
		assertScope(demoted, globalNotice, true)
		request(t, admin, "POST", "/incidents/"+own+"/archive", `{"reason":"Archive test"}`, 200)
		assertScope(demoted, ownNotice, false)
	})
}
