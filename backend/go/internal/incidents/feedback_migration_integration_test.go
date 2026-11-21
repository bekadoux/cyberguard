//go:build integration

package incidents

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	db "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/migrations"
	"github.com/bekadoux/cyberguard/backend/go/internal/testutil"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestFeedbackUUIDMigration(t *testing.T) {
	const directory = "../../../../db/migrations/incidents"
	f := testutil.IncidentsAtVersion(t, directory, 6)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	service, err := New(db.New(f.Runtime))
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.Create(ctx, testActor(), Input{Title: "Review", Description: "Test upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	const job = "abcdefab-1234-4321-aaaa-abcdabcdabcd"
	const insert = `INSERT INTO incident_actions (incident_id,action,details)
	    VALUES ($1, 'ai_feedback_submitted', jsonb_build_object('job_id',$2::text,'decision','rejected'))`
	for _, spelling := range []string{job, strings.ToUpper(job)} {
		if _, err := f.Migrator.Exec(ctx, insert, item.ID, spelling); err != nil {
			t.Fatal(err)
		}
	}
	sql := stdlib.OpenDB(*f.Migrator.Config().ConnConfig)
	defer sql.Close()
	provider, err := migrations.NewProvider(sql, os.DirFS(directory))
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.UpTo(ctx, 7)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Message != "duplicate normalized AI feedback job IDs; migration refused" {
		t.Fatalf("expected explicit duplicate preflight failure, got %v", err)
	}
	var count int
	if err := f.Migrator.QueryRow(ctx, `SELECT count(*) FROM incident_actions WHERE action='ai_feedback_submitted'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("failed migration lost audit history: count=%d err=%v", count, err)
	}
	// Resolve the test conflict explicitly, leaving the uppercase legacy spelling.
	if _, err := f.Migrator.Exec(ctx, `DELETE FROM incident_actions WHERE details->>'job_id'=$1`, job); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 7); err != nil {
		t.Fatal(err)
	}
	var saved string
	if err := f.Migrator.QueryRow(ctx, `SELECT details->>'job_id' FROM incident_actions WHERE action='ai_feedback_submitted'`).Scan(&saved); err != nil || saved != strings.ToUpper(job) {
		t.Fatalf("historical job spelling changed: %q err=%v", saved, err)
	}
	_, err = f.Migrator.Exec(ctx, insert, item.ID, job)
	if !errors.As(err, &pgerr) || pgerr.Code != "23505" || pgerr.ConstraintName != "incident_actions_one_ai_feedback_per_job" {
		t.Fatalf("expected UUID uniqueness index violation, got %v", err)
	}
}
