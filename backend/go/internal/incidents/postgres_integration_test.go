//go:build integration

package incidents

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	db "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/migrations"
	"github.com/bekadoux/cyberguard/backend/go/internal/testutil"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestIncidentPostgresIntegration(t *testing.T) {
	fixture := testutil.Incidents(t, "../../../../db/migrations/incidents")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := New(db.New(fixture.Runtime))
	if err != nil {
		t.Fatal(err)
	}
	a := testActor()
	b := auth.User{ID: "22222222-2222-4222-8222-222222222222", Role: "employee", AuthVersion: 1}
	analyst := auth.User{ID: "33333333-3333-4333-8333-333333333333", Role: "analyst", AuthVersion: 1}
	create := func(user auth.User, title string) db.Incident {
		t.Helper()
		row, err := s.Create(ctx, user, Input{Title: title, Description: "Original incident description"})
		if err != nil {
			t.Fatal("real incident creation failed")
		}
		if row.ReporterID.String() != user.ID || row.Status != "submitted" || row.Category.Valid || row.Severity.Valid || row.AssignedAnalystID.Valid || !row.CreatedAt.Valid {
			t.Fatal("unsafe SQL defaults")
		}
		return row
	}
	a1, a2, b1 := create(a, "A first"), create(a, "A second"), create(b, "B report")
	t.Run("ownership and missing IDs use the same policy", func(t *testing.T) {
		if _, err := s.Get(ctx, a, b1.ID.String()); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross-user SQL read permitted")
		}
		if _, err := s.Get(ctx, b, a1.ID.String()); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross-user SQL read permitted")
		}
		if _, err := s.Get(ctx, analyst, b1.ID.String()); err != nil {
			t.Fatal("analyst cannot read company incident")
		}
	})
	t.Run("stable tied timestamp pagination is scoped in SQL", func(t *testing.T) {
		tied := time.Date(2026, 11, 8, 20, 0, 0, 0, time.UTC)
		if _, err := fixture.Migrator.Exec(ctx, "UPDATE incidents SET created_at = $1", tied); err != nil {
			t.Fatal("could not prepare tied timestamps")
		}
		seen := map[string]bool{}
		cursor := ""
		for pages := 0; pages < 4; pages++ {
			page, err := s.List(ctx, a, ListOptions{Limit: 1, Cursor: cursor})
			if err != nil {
				t.Fatal("SQL pagination failed")
			}
			for _, row := range page.Incidents {
				if row.ReporterID.String() != a.ID || seen[row.ID.String()] {
					t.Fatal("pagination leaked/repeated an incident")
				}
				seen[row.ID.String()] = true
			}
			if page.NextCursor == nil {
				break
			}
			cursor = *page.NextCursor
		}
		if len(seen) != 2 || !seen[a1.ID.String()] || !seen[a2.ID.String()] {
			t.Fatal("tied timestamp pagination lost a row")
		}
		page, err := s.List(ctx, analyst, ListOptions{Limit: 100})
		if err != nil || len(page.Incidents) != 3 {
			t.Fatal("company queue did not include all permitted rows")
		}
	})
	t.Run("filters and archived records", func(t *testing.T) {
		if _, err := fixture.Migrator.Exec(ctx, "UPDATE incidents SET status = 'investigating', category = 'phishing', severity = 'high' WHERE id = $1", a1.ID); err != nil {
			t.Fatal("could not set classification fixture")
		}
		page, err := s.List(ctx, a, ListOptions{Limit: 20, Status: "investigating", Category: "phishing"})
		if err != nil || len(page.Incidents) != 1 || page.Incidents[0].ID != a1.ID {
			t.Fatal("SQL filters failed")
		}
		if _, err := fixture.Migrator.Exec(ctx, "UPDATE incidents SET archived_at = now(), archive_reason = 'Test archive' WHERE id = $1", a2.ID); err != nil {
			t.Fatal("could not archive fixture")
		}
		if _, err := s.Get(ctx, analyst, a2.ID.String()); !errors.Is(err, ErrNotFound) {
			t.Fatal("archived record exposed")
		}
	})
	t.Run("runtime grants preserve immutable and server-managed fields", func(t *testing.T) {
		for _, stmt := range []string{
			"UPDATE incidents SET title = 'Changed'",
			"UPDATE incidents SET reporter_id = gen_random_uuid()",
			"DELETE FROM incidents",
			"INSERT INTO incidents (reporter_id,title,description,status) VALUES (gen_random_uuid(),'x','x','resolved')",
		} {
			if _, err := fixture.Runtime.Exec(ctx, stmt); err == nil {
				t.Fatal("forbidden SQL operation permitted")
			}
		}
	})
	t.Run("database rejects invalid input independently of HTTP", func(t *testing.T) {
		for _, title := range []string{"", "\u2003\u00a0\t", strings.Repeat("x", 201)} {
			if _, err := fixture.Runtime.Exec(ctx, "INSERT INTO incidents (reporter_id,title,description) VALUES ($1,$2,'Description')", a1.ReporterID, title); err == nil {
				t.Fatal("SQL accepted invalid title")
			}
		}
		if _, err := s.Create(ctx, a, Input{Title: strings.Repeat("🙂", 200), Description: strings.Repeat("界", 10000)}); err != nil {
			t.Fatal("PostgreSQL counted Unicode limits as bytes")
		}
		for _, fields := range [][3]string{
			{"Title", strings.Repeat("x", 10001), "System"},
			{"Title", "\u2003\u00a0", "System"},
			{"Title", "Description", strings.Repeat("x", 501)},
		} {
			if _, err := fixture.Runtime.Exec(ctx, "INSERT INTO incidents (reporter_id,title,description,affected_system) VALUES ($1,$2,$3,$4)", a1.ReporterID, fields[0], fields[1], fields[2]); err == nil {
				t.Fatal("PostgreSQL accepted invalid text constraints")
			}
		}
	})
	t.Run("repeat migration is a no-op", func(t *testing.T) {
		connection := stdlib.OpenDB(*fixture.Migrator.Config().ConnConfig)
		defer connection.Close()
		provider, err := migrations.NewProvider(connection, os.DirFS("../../../../db/migrations/incidents"))
		if err != nil {
			t.Fatal("could not load migrations")
		}
		changes, err := provider.Up(ctx)
		if err != nil || len(changes) != 0 {
			t.Fatal("incident migration is not repeatable")
		}
	})
}
