package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	db "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type incidentTestQueries struct {
	mu           sync.Mutex
	items        []db.Incident
	fail         bool
	creates      int
	beforeCreate func()
}

func incidentID(n int) pgtype.UUID {
	var id pgtype.UUID
	if err := id.Scan(fmt.Sprintf("11111111-1111-4111-8111-%012d", n)); err != nil {
		panic(err)
	}
	return id
}

func (q *incidentTestQueries) CreateIncident(_ context.Context, p db.CreateIncidentParams) (db.Incident, error) {
	if q.beforeCreate != nil {
		q.beforeCreate()
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.fail {
		return db.Incident{}, errors.New("sensitive incident SQL error")
	}
	q.creates++
	now := pgtype.Timestamptz{Time: time.Now().UTC().Truncate(time.Microsecond), Valid: true}
	item := db.Incident{ID: incidentID(len(q.items) + 1), ReporterID: p.ReporterID, Title: p.Title, Description: p.Description, AffectedSystem: p.AffectedSystem, Status: "submitted", CreatedAt: now, UpdatedAt: now}
	q.items = append(q.items, item)
	return item, nil
}

func (q *incidentTestQueries) GetVisibleIncident(_ context.Context, p db.GetVisibleIncidentParams) (db.Incident, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.fail {
		return db.Incident{}, errors.New("sensitive incident SQL error")
	}
	for _, row := range q.items {
		if row.ID == p.ID && !row.ArchivedAt.Valid && (row.ReporterID == p.ActorID || p.ReadCompany) {
			return row, nil
		}
	}
	return db.Incident{}, pgx.ErrNoRows
}

func (q *incidentTestQueries) ListVisibleIncidents(_ context.Context, p db.ListVisibleIncidentsParams) ([]db.ListVisibleIncidentsRow, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.fail {
		return nil, errors.New("sensitive incident SQL error")
	}
	items := []db.ListVisibleIncidentsRow{}
	for _, row := range q.items {
		if row.ArchivedAt.Valid || (!p.ReadCompany && row.ReporterID != p.ActorID) ||
			(p.StatusFilter.Valid && row.Status != p.StatusFilter.String) ||
			(p.CategoryFilter.Valid && (!row.Category.Valid || row.Category.String != p.CategoryFilter.String)) {
			continue
		}
		if p.BeforeCreatedAt.Valid && !(row.CreatedAt.Time.Before(p.BeforeCreatedAt.Time) || (row.CreatedAt.Time.Equal(p.BeforeCreatedAt.Time) && row.ID.String() < p.BeforeID.String())) {
			continue
		}
		items = append(items, db.ListVisibleIncidentsRow{ID: row.ID, ReporterID: row.ReporterID, Title: row.Title, Status: row.Status, Category: row.Category, Severity: row.Severity, AssignedAnalystID: row.AssignedAnalystID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Time.Equal(items[j].CreatedAt.Time) {
			return items[i].ID.String() > items[j].ID.String()
		}
		return items[i].CreatedAt.Time.After(items[j].CreatedAt.Time)
	})
	if len(items) > int(p.PageLimit) {
		items = items[:p.PageLimit]
	}
	return items, nil
}

func incidentFixture(t *testing.T) (authHTTPFixture, *incidentTestQueries, *http.Cookie) {
	t.Helper()
	f := newAuthHTTPFixture(t, nil)
	q := &incidentTestQueries{}
	a, err := NewIncidentAPI(q, f.users, f.app)
	if err != nil {
		t.Fatal(err)
	}
	a.Mount(f.router)
	login := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, nil)
	if login.Code != 200 {
		t.Fatal("login failed")
	}
	return f, q, sessionCookie(t, login, f.app.sessions.Cookie.Name)
}

const validIncident = `{"title":"Suspicious email","description":"I opened a link but did not enter a password.","affected_system":"Work email"}`

func incidentJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal("invalid incident response JSON")
	}
	return body
}

func TestIncidentHTTPCreateListDetail(t *testing.T) {
	f, q, cookie := incidentFixture(t)
	w := authRequest(f.router, http.MethodPost, "/incidents", validIncident, cookie)
	if w.Code != 201 || len(w.Result().Cookies()) != 0 || q.creates != 1 {
		t.Fatalf("create failed: %d", w.Code)
	}
	item := incidentJSON(t, w)["incident"].(map[string]any)
	id := item["id"].(string)
	if item["reporter_id"] != f.users.user.ID.String() || item["status"] != "submitted" || item["category"] != nil || item["assigned_analyst"] != nil || w.Header().Get("Location") != "/incidents/"+id {
		t.Fatal("incorrect server-managed fields")
	}
	if _, exists := item["severity"]; exists {
		t.Fatal("reporter received internal severity")
	}
	w = authRequest(f.router, http.MethodGet, "/incidents", "", cookie)
	list := incidentJSON(t, w)
	if w.Code != 200 || len(list["incidents"].([]any)) != 1 || list["next_cursor"] != nil || strings.Contains(w.Body.String(), `"description"`) || len(w.Result().Cookies()) != 0 {
		t.Fatal("unsafe list response")
	}
	w = authRequest(f.router, http.MethodGet, "/incidents/"+id, "", cookie)
	if w.Code != 200 || incidentJSON(t, w)["incident"].(map[string]any)["description"] != q.items[0].Description || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("detail failed")
	}
	for _, method := range []string{http.MethodPatch, http.MethodPut, http.MethodDelete} {
		if w := authRequest(f.router, method, "/incidents/"+id, validIncident, cookie); w.Code != 405 || q.creates != 1 {
			t.Fatal("unimplemented mutation endpoint was exposed")
		}
	}
	if w := authRequest(f.router, http.MethodPost, "/incidents?reporter_id=other", validIncident, cookie); w.Code != 400 || q.creates != 1 {
		t.Fatal("create query override was accepted")
	}
}

func TestIncidentAPIRequiresDependencies(t *testing.T) {
	f := newAuthHTTPFixture(t, nil)
	if _, err := NewIncidentAPI(nil, f.users, f.app); err == nil {
		t.Fatal("nil incident queries accepted")
	}
	if _, err := NewIncidentAPI(&incidentTestQueries{}, nil, f.app); err == nil {
		t.Fatal("nil user queries accepted")
	}
	if _, err := NewIncidentAPI(&incidentTestQueries{}, f.users, nil); err == nil {
		t.Fatal("nil authentication accepted")
	}
}

func TestIncidentHTTPUnicodeLimitsAndInvalidPayloads(t *testing.T) {
	f, q, cookie := incidentFixture(t)
	body, _ := json.Marshal(map[string]any{"title": strings.Repeat("🙂", 200), "description": strings.Repeat("界", 10000), "affected_system": strings.Repeat("界", 500)})
	w := authRequest(f.router, http.MethodPost, "/incidents", string(body), cookie)
	if w.Code != 201 || w.Body.Len() <= 16384 || len(w.Result().Cookies()) != 0 {
		t.Fatal("large Unicode detail hit the small auth response buffer")
	}
	for _, body := range []string{
		"{", "null", "[]", "{}", validIncident + "{}", `{"title":null,"description":"text"}`,
		`{"title":"\u2003\u00a0","description":"text"}`, `{"title":"title","description":"\t\n"}`,
		`{"title":"title","description":"bad\u0000"}`, "{\"title\":\"bad\xff\",\"description\":\"text\"}",
		strings.Replace(validIncident, "Suspicious email", strings.Repeat("x", 201), 1),
	} {
		if w := authRequest(f.router, http.MethodPost, "/incidents", body, cookie); w.Code != 400 || q.creates != 1 {
			t.Fatalf("invalid body accepted: %d", w.Code)
		}
	}
	for _, field := range []string{"id", "reporter_id", "status", "category", "severity", "assigned_analyst_id", "created_at", "job_state", "role"} {
		body := strings.TrimSuffix(validIncident, "}") + `,"` + field + `":"injected"}`
		if w := authRequest(f.router, http.MethodPost, "/incidents", body, cookie); w.Code != 400 || q.creates != 1 {
			t.Fatal("server-managed field accepted")
		}
	}
	if w := authRequest(f.router, http.MethodPost, "/incidents", strings.Repeat("x", maxIncidentBodyBytes+1), cookie); w.Code != 413 {
		t.Fatal("body limit not enforced")
	}
	r := httptest.NewRequest(http.MethodPost, "/incidents", strings.NewReader(validIncident))
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal("non-JSON media type accepted")
	}
}

func TestIncidentHTTPVisibilityAndPagination(t *testing.T) {
	f, q, cookie := incidentFixture(t)
	for range 3 {
		if authRequest(f.router, http.MethodPost, "/incidents", validIncident, cookie).Code != 201 {
			t.Fatal("create failed")
		}
	}
	// Another employee owns this classified incident. It is never in our page.
	other := q.items[0]
	other.ID = incidentID(100)
	other.ReporterID = incidentID(200)
	other.Category = pgtype.Text{String: "phishing", Valid: true}
	other.Severity = pgtype.Text{String: "critical", Valid: true}
	other.AssignedAnalystID = f.users.user.ID
	other.ResolutionNote = pgtype.Text{String: "Public resolution", Valid: true}
	q.items = append(q.items, other)
	for _, path := range []string{"/incidents/" + other.ID.String(), "/incidents/not-a-uuid", "/incidents/" + incidentID(999).String()} {
		if authRequest(f.router, http.MethodGet, path, "", cookie).Code != 404 {
			t.Fatal("cross-user existence leaked")
		}
	}
	w := authRequest(f.router, http.MethodGet, "/incidents?limit=2", "", cookie)
	page := incidentJSON(t, w)
	if w.Code != 200 || len(page["incidents"].([]any)) != 2 || page["next_cursor"] == nil {
		t.Fatal("first scoped page failed")
	}
	w = authRequest(f.router, http.MethodGet, "/incidents?limit=2&cursor="+page["next_cursor"].(string), "", cookie)
	if w.Code != 200 || len(incidentJSON(t, w)["incidents"].([]any)) != 1 {
		t.Fatal("second scoped page failed")
	}
	for _, role := range []string{"analyst", "admin"} {
		f.users.user.Role = role
		f.users.user.AuthVersion++
		login := authRequest(f.router, http.MethodPost, "/auth/login", validLogin, nil)
		cookie = sessionCookie(t, login, f.app.sessions.Cookie.Name)
		w = authRequest(f.router, http.MethodGet, "/incidents/"+other.ID.String(), "", cookie)
		item := incidentJSON(t, w)["incident"].(map[string]any)
		if w.Code != 200 || item["severity"] != "critical" || item["resolution_note"] != "Public resolution" || item["assigned_analyst"].(map[string]any)["username"] != "employee_a" || strings.Contains(w.Body.String(), "password_hash") {
			t.Fatal("incorrect professional DTO")
		}
		w = authRequest(f.router, http.MethodGet, "/incidents/"+q.items[0].ID.String(), "", cookie)
		if strings.Contains(w.Body.String(), `"severity"`) {
			t.Fatal("own-report projection exposed internal data")
		}
		w = authRequest(f.router, http.MethodGet, "/incidents", "", cookie)
		if len(incidentJSON(t, w)["incidents"].([]any)) != 4 {
			t.Fatal("company queue missing incidents")
		}
	}
	q.items[3].ArchivedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	if authRequest(f.router, http.MethodGet, "/incidents/"+other.ID.String(), "", cookie).Code != 404 {
		t.Fatal("archived incident exposed")
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=abc", "limit=1&limit=2", "cursor=broken", "cursor=", "status=queued", "category=unknown", "severity=critical", "reporter_id=other", "read_company=true", "sort=severity", "limit=%zz"} {
		if w := authRequest(f.router, http.MethodGet, "/incidents?"+query, "", cookie); w.Code != 400 {
			t.Fatalf("invalid query accepted: %s -> %d", query, w.Code)
		}
	}
}

func TestIncidentHTTPAuthenticationAndFailures(t *testing.T) {
	f, q, cookie := incidentFixture(t)
	for _, endpoint := range [][2]string{{"GET", "/incidents"}, {"GET", "/incidents/" + incidentID(1).String()}, {"POST", "/incidents"}} {
		if w := authRequest(f.router, endpoint[0], endpoint[1], validIncident, nil); w.Code != 401 || len(w.Result().Cookies()) != 0 {
			t.Fatal("anonymous incident request succeeded or set cookies")
		}
	}
	f.store.failCommit = true
	w := authRequest(f.router, http.MethodPost, "/incidents", validIncident, cookie)
	if w.Code != 503 || q.creates != 0 || len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), `"incident"`) {
		t.Fatal("session refresh failure happened after business mutation")
	}
	f.store.failCommit = false
	q.fail = true
	w = authRequest(f.router, http.MethodPost, "/incidents", validIncident, cookie)
	if w.Code != 503 || strings.Contains(w.Body.String(), "sensitive") || len(w.Result().Cookies()) != 0 {
		t.Fatal("SQL error leaked")
	}
	q.fail = false
	f.users.user.AuthVersion++
	w = authRequest(f.router, http.MethodPost, "/incidents", validIncident, cookie)
	if w.Code != 401 || q.creates != 0 || len(w.Result().Cookies()) != 0 {
		t.Fatal("stale auth version created incident")
	}
}

type incidentAssignedUsers struct {
	auth.Users
	id  pgtype.UUID
	err error
}

func (u incidentAssignedUsers) GetUserByID(ctx context.Context, id pgtype.UUID) (identity.User, error) {
	if id == u.id {
		return identity.User{}, u.err
	}
	return u.Users.GetUserByID(ctx, id)
}

func TestIncidentHTTPAssignedAnalystLookupIsSafe(t *testing.T) {
	f, q, cookie := incidentFixture(t)
	if authRequest(f.router, http.MethodPost, "/incidents", validIncident, cookie).Code != 201 {
		t.Fatal("create failed")
	}
	assigned := incidentID(123)
	q.items[0].AssignedAnalystID = assigned
	for _, failure := range []error{pgx.ErrNoRows, errors.New("sensitive identity failure")} {
		router := NewRouter(testLogger(), nil, time.Second)
		api, err := NewIncidentAPI(q, incidentAssignedUsers{Users: f.users, id: assigned, err: failure}, f.app)
		if err != nil {
			t.Fatal(err)
		}
		api.Mount(router)
		w := authRequest(router, http.MethodGet, "/incidents/"+q.items[0].ID.String(), "", cookie)
		if errors.Is(failure, pgx.ErrNoRows) {
			if w.Code != 200 || incidentJSON(t, w)["incident"].(map[string]any)["assigned_analyst"].(map[string]any)["username"] != nil {
				t.Fatal("missing analyst identity was mishandled")
			}
		} else if w.Code != 503 || strings.Contains(w.Body.String(), `"incident"`) || strings.Contains(w.Body.String(), "sensitive") {
			t.Fatal("identity lookup failure leaked partial incident data")
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("lookup response emitted cookie")
		}
	}
}

func TestIncidentHTTPOriginPolicy(t *testing.T) {
	f, q, cookie := incidentFixture(t)
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/incidents", strings.NewReader(validIncident))
	r.AddCookie(cookie)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://hostile.example")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != 403 || q.creates != 0 || len(w.Result().Cookies()) != 0 {
		t.Fatal("incident CSRF protection failed")
	}
	r = httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:8080/incidents", nil)
	r.Header.Set("Origin", "http://127.0.0.1:5173")
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "Content-Type")
	w = httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:5173" || w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("incident credentialed preflight failed")
	}
}

func TestIncidentHTTPRevocationBeforeRefreshPreventsCreate(t *testing.T) {
	f, q, cookie := incidentFixture(t)
	paused := &pausedSessionStore{CtxStore: f.app.sessions.Store.(scs.CtxStore), loaded: make(chan struct{}), resume: make(chan struct{})}
	app, router := concurrencyApp(t, f.users, f.app.settings, paused)
	api, err := NewIncidentAPI(q, f.users, app)
	if err != nil {
		t.Fatal(err)
	}
	api.Mount(router)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- authRequest(router, http.MethodPost, "/incidents", validIncident, cookie) }()
	resumed, finished := false, false
	defer func() {
		if !resumed {
			close(paused.resume)
		}
		if !finished {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("incident request did not stop")
			}
		}
	}()
	select {
	case <-paused.loaded:
	case <-time.After(5 * time.Second):
		t.Fatal("session did not load")
	}
	if authRequest(f.router, http.MethodPost, "/auth/logout", "", cookie).Code != 204 {
		t.Fatal("logout failed")
	}
	close(paused.resume)
	resumed = true
	select {
	case w := <-done:
		finished = true
		if w.Code != 401 || len(w.Result().Cookies()) != 0 || q.creates != 0 {
			t.Fatal("revoked request created an incident or changed cookies")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not finish")
	}
}

func TestIncidentHTTPAuthorizedInFlightCreateCanFinish(t *testing.T) {
	f, q, cookie := incidentFixture(t)
	q.beforeCreate = func() {
		if authRequest(f.router, http.MethodPost, "/auth/logout", "", cookie).Code != 204 {
			t.Error("logout failed")
		}
	}
	w := authRequest(f.router, http.MethodPost, "/incidents", validIncident, cookie)
	if w.Code != 201 || q.creates != 1 || len(w.Result().Cookies()) != 0 {
		t.Fatal("post-write session refresh obscured a committed authorized operation")
	}
	if authRequest(f.router, http.MethodGet, "/auth/me", "", cookie).Code != 401 {
		t.Fatal("business response resurrected the session")
	}
}
