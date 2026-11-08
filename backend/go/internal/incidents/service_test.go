package incidents

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	db "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type testQueries struct {
	created db.CreateIncidentParams
	get     db.GetVisibleIncidentParams
	list    db.ListVisibleIncidentsParams
	rows    []db.ListVisibleIncidentsRow
	err     error
	calls   int
}

func (q *testQueries) CreateIncident(_ context.Context, p db.CreateIncidentParams) (db.Incident, error) {
	q.calls++
	q.created = p
	return db.Incident{ReporterID: p.ReporterID, Title: p.Title, Description: p.Description}, q.err
}
func (q *testQueries) GetVisibleIncident(_ context.Context, p db.GetVisibleIncidentParams) (db.Incident, error) {
	q.calls++
	q.get = p
	return db.Incident{}, q.err
}
func (q *testQueries) ListVisibleIncidents(_ context.Context, p db.ListVisibleIncidentsParams) ([]db.ListVisibleIncidentsRow, error) {
	q.calls++
	q.list = p
	return q.rows, q.err
}

func testID(value string) pgtype.UUID {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		panic(err)
	}
	return id
}

func testActor() auth.User {
	return auth.User{ID: "12345678-1234-4234-8234-123456789abc", Role: "employee", AuthVersion: 1}
}

func TestCreateValidationAndOwnership(t *testing.T) {
	q := &testQueries{}
	s, err := New(q)
	if err != nil {
		t.Fatal(err)
	}
	actor := testActor()
	longTitle, longDescription := strings.Repeat("🙂", 200), strings.Repeat("界", 10000)
	affected := strings.Repeat("界", 500)
	if _, err := s.Create(context.Background(), actor, Input{Title: longTitle, Description: longDescription, AffectedSystem: &affected}); err != nil {
		t.Fatal("Unicode character limits were counted as bytes")
	}
	if q.created.ReporterID.String() != actor.ID || q.created.Title != longTitle || q.created.Description != longDescription || !q.created.AffectedSystem.Valid {
		t.Fatal("creation changed original text or trusted the wrong owner")
	}
	for _, input := range []Input{
		{Title: "", Description: "text"}, {Title: "\u2003\u00a0\t", Description: "text"},
		{Title: "title", Description: "\n\r\t"}, {Title: longTitle + "x", Description: "text"},
		{Title: "title", Description: longDescription + "x"}, {Title: "title", Description: "text", AffectedSystem: &longDescription},
		{Title: "title\x00", Description: "text"}, {Title: "title", Description: "bad\xff"},
	} {
		before := q.calls
		if _, err := s.Create(context.Background(), actor, input); !errors.Is(err, ErrInvalid) || q.calls != before {
			t.Fatal("invalid input reached persistence")
		}
	}
	blank := "  \u2003 "
	if _, err := s.Create(context.Background(), actor, Input{Title: "title", Description: " original\ntext ", AffectedSystem: &blank}); err != nil || q.created.AffectedSystem.Valid || q.created.Description != " original\ntext " {
		t.Fatal("optional blank value or original description was not handled correctly")
	}
}

func TestScopeErrorsAndCancellation(t *testing.T) {
	q := &testQueries{}
	s, _ := New(q)
	actor := testActor()
	for _, role := range []string{"employee", "analyst", "admin"} {
		actor.Role = role
		if _, err := s.Get(context.Background(), actor, actor.ID); err != nil || q.get.ReadCompany != (role != "employee") || q.get.ActorID.String() != actor.ID {
			t.Fatal("incorrect query scope")
		}
	}
	actor.Role = "invented"
	before := q.calls
	if _, err := s.Create(context.Background(), actor, Input{Title: "title", Description: "text"}); !errors.Is(err, ErrForbidden) || q.calls != before {
		t.Fatal("unknown role reached persistence")
	}
	actor = testActor()
	if _, err := s.Get(context.Background(), actor, "invalid"); !errors.Is(err, ErrNotFound) || q.calls != before {
		t.Fatal("malformed ID was not concealed")
	}
	q.err = pgx.ErrNoRows
	if _, err := s.Get(context.Background(), actor, actor.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing row was not concealed")
	}
	q.err = errors.New("sensitive SQL credentials")
	if _, err := s.Get(context.Background(), actor, actor.ID); !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "sensitive") {
		t.Fatal("dependency error leaked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before = q.calls
	if _, err := s.Create(ctx, actor, Input{Title: "title", Description: "text"}); !errors.Is(err, ErrUnavailable) || q.calls != before {
		t.Fatal("canceled create reached persistence")
	}
	if _, err := New(nil); err == nil {
		t.Fatal("nil queries accepted")
	}
}

func TestListPaginationAndValidation(t *testing.T) {
	actor := testActor()
	created := time.Date(2026, 11, 8, 20, 0, 0, 123000, time.UTC)
	id := testID(actor.ID)
	q := &testQueries{rows: []db.ListVisibleIncidentsRow{
		{ID: id, CreatedAt: pgtype.Timestamptz{Time: created, Valid: true}},
		{ID: id, CreatedAt: pgtype.Timestamptz{Time: created.Add(-time.Second), Valid: true}},
	}}
	s, _ := New(q)
	page, err := s.List(context.Background(), actor, ListOptions{Limit: 1, Status: "submitted", Category: "phishing"})
	if err != nil || len(page.Incidents) != 1 || page.NextCursor == nil || q.list.PageLimit != 2 || q.list.ReadCompany {
		t.Fatal("list did not apply bounded scope/pagination")
	}
	if _, err := s.List(context.Background(), actor, ListOptions{Limit: 1, Cursor: *page.NextCursor}); err != nil || !q.list.BeforeCreatedAt.Time.Equal(created) || q.list.BeforeID != id {
		t.Fatal("cursor did not round trip")
	}
	for _, options := range []ListOptions{
		{Limit: 0}, {Limit: 101}, {Limit: 1, Status: "queued"}, {Limit: 1, Category: "injected"},
		{Limit: 1, Cursor: "not-base64"}, {Limit: 1, Cursor: strings.Repeat("x", 257)},
	} {
		before := q.calls
		if _, err := s.List(context.Background(), actor, options); !errors.Is(err, ErrInvalid) || q.calls != before {
			t.Fatal("invalid list options reached SQL")
		}
	}
}

func FuzzIncidentCursor(f *testing.F) {
	id := testID("12345678-1234-4234-8234-123456789abc")
	f.Add(encodeCursor(time.Date(2026, 11, 8, 20, 0, 0, 123000, time.UTC), id))
	f.Add("")
	f.Add("not-base64")
	f.Add(strings.Repeat("x", 257))
	f.Fuzz(func(t *testing.T, value string) {
		created, id, err := decodeCursor(value)
		if err != nil {
			return
		}
		if !created.Valid || !id.Valid || created.Time.Year() < 1 || created.Time.Year() > 9999 {
			t.Fatal("invalid decoded cursor state")
		}
		again, againID, err := decodeCursor(encodeCursor(created.Time, id))
		if err != nil || !created.Time.Equal(again.Time) || id != againID {
			t.Fatal("accepted cursor cannot round trip")
		}
	})
}
