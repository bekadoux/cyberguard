//go:build integration

package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	incidentdb "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/demoseed"
	"github.com/bekadoux/cyberguard/backend/go/internal/sessionstore"
	"github.com/bekadoux/cyberguard/backend/go/internal/testutil"
	"github.com/jackc/pgx/v5/pgtype"
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
	api.EnableHandoff(reports.Runtime, identities.Runtime, false)
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
	// Synthetic stored results exercise the real runtime grants and transaction,
	// not model inference. A fixture source must be rejected before any review.
	var job pgtype.UUID
	err = reports.Migrator.QueryRow(ctx, `WITH completed AS (
        UPDATE assessment_jobs SET status='completed',finished_at=now()
        WHERE incident_id=$1 RETURNING id,incident_id
    ) INSERT INTO assessment_results(job_id,incident_id,assessment,source_kind)
      SELECT id,incident_id,'{"triage":{"suggested_category":"phishing"}}'::jsonb,'fake'
      FROM completed RETURNING job_id`, id).Scan(&job)
	if err != nil {
		t.Fatal(err)
	}
	feedbackURL := base + "/ai-feedback/" + job.String()
	request("GET", feedbackURL, "", emp1, 403)
	request("GET", feedbackURL, "", nil, 401)
	request("GET", base+"/ai-feedback/not-a-uuid", "", analyst, 404)
	unreviewed := authRequest(router, "GET", feedbackURL, "", analyst)
	if review, ok := incidentJSON(t, unreviewed)["review"]; unreviewed.Code != 200 || !ok || review != nil {
		t.Fatalf("unexpected unreviewed response: %d %s", unreviewed.Code, unreviewed.Body.String())
	}
	other := authRequest(router, "POST", "/incidents", validIncident, emp2)
	otherID := incidentJSON(t, other)["incident"].(map[string]any)["id"].(string)
	request("GET", "/incidents/"+otherID+"/ai-feedback/"+job.String(), "", analyst, 404)
	// A legacy review may retain its original mixed-case JSON spelling. Its
	// persisted decision is visible by UUID to an unassigned professional.
	var legacy pgtype.UUID
	err = reports.Migrator.QueryRow(ctx, `WITH stored AS (
	    INSERT INTO assessment_results(job_id,incident_id,assessment,source_kind)
	    SELECT id,incident_id,'{}','kev_llm' FROM assessment_jobs WHERE incident_id=$1 RETURNING job_id
	) INSERT INTO incident_actions(incident_id,action,reason,details)
	    SELECT $1,'ai_feedback_submitted','Insufficient evidence',
	        jsonb_build_object('job_id',upper(job_id::text),'decision','rejected','confirmed_category',null)
	    FROM stored RETURNING (details->>'job_id')::uuid`, otherID).Scan(&legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyRead := authRequest(router, "GET", "/incidents/"+otherID+"/ai-feedback/"+legacy.String(), "", analyst)
	legacyReview, ok := incidentJSON(t, legacyRead)["review"].(map[string]any)
	if legacyRead.Code != 200 || !ok || legacyReview["job_id"] != legacy.String() || legacyReview["decision"] != "rejected" ||
		legacyReview["category"] != nil || legacyReview["reason"] != "Insufficient evidence" {
		t.Fatalf("unexpected legacy review: %d %s", legacyRead.Code, legacyRead.Body.String())
	}
	feedback := fmt.Sprintf(`{"job_id":%q,"decision":"accepted","category":"phishing","reason":""}`, strings.ToUpper(job.String()))
	request("POST", base+"/ai-feedback", feedback, emp1, 403)
	request("POST", base+"/ai-feedback", feedback, analyst, 409)
	if _, err = reports.Migrator.Exec(ctx, `UPDATE assessment_results SET source_kind='kev_llm' WHERE job_id=$1`, job); err != nil {
		t.Fatal(err)
	}
	mismatch := fmt.Sprintf(`{"job_id":%q,"decision":"accepted","category":"malware","reason":""}`, job.String())
	request("POST", base+"/ai-feedback", mismatch, analyst, 400)
	request("POST", base+"/ai-feedback", feedback, analyst, 201)
	duplicate := fmt.Sprintf(`{"job_id":%q,"decision":"corrected","category":"malware","reason":"Duplicate must roll back"}`, job.String())
	request("POST", base+"/ai-feedback", duplicate, admin, 409)
	reviewed := authRequest(router, "GET", feedbackURL, "", admin)
	review, ok := incidentJSON(t, reviewed)["review"].(map[string]any)
	if reviewed.Code != 200 || !ok || review["job_id"] != job.String() || review["decision"] != "accepted" || review["category"] != "phishing" || review["reason"] != "" {
		t.Fatalf("unexpected persisted review: %d %s", reviewed.Code, reviewed.Body.String())
	}
	var savedJob string
	if err := reports.Runtime.QueryRow(ctx, `SELECT details->>'job_id' FROM incident_actions WHERE incident_id=$1 AND action='ai_feedback_submitted'`, id).Scan(&savedJob); err != nil || savedJob != job.String() {
		t.Fatalf("posted job ID not canonical: %q err=%v", savedJob, err)
	}
	var category string
	var reviews int
	err = reports.Runtime.QueryRow(ctx, `SELECT category,
        (SELECT count(*) FROM incident_actions WHERE incident_id=$1 AND action='ai_feedback_submitted')
        FROM incidents WHERE id=$1`, id).Scan(&category, &reviews)
	if err != nil || category != "phishing" || reviews != 1 {
		t.Fatalf("feedback category=%q reviews=%d err=%v", category, reviews, err)
	}
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
	request("GET", feedbackURL, "", analyst, 200)
	request("POST", base+"/transition", `{"status":"investigating","reason":"Reopen for further review"}`, admin, 200)
	request("GET", base+"/history", "", analyst, 200)
	request("GET", base+"/history", "", emp1, 403)
	request("GET", "/notifications", "", emp1, 200)
	request("GET", "/admin/users", "", emp1, 403)
	request("GET", "/admin/users", "", admin, 200)
	request("POST", "/admin/users", `{"username":"new_employee","password":"a-long-test-password","role":"employee"}`, admin, 201)
	request("POST", base+"/archive", `{"reason":"Test archived review visibility"}`, admin, 200)
	request("GET", feedbackURL, "", admin, 404)
	if _, err = reports.Runtime.Exec(ctx, `UPDATE incidents SET title='not allowed' WHERE id=$1`, id); err == nil {
		t.Fatal("runtime role changed immutable original title")
	}
}
