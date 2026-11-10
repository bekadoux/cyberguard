//go:build integration

package incidents

import (
	"context"
	"testing"
	"time"

	db "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/testutil"
)

func TestAtomicJobAndImmutableContextIntegration(t *testing.T) {
	fixture := testutil.Incidents(t, "../../../../db/migrations/incidents")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	service, err := New(db.New(fixture.Runtime))
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.Create(ctx, testActor(), Input{Title: "First submitted title", Description: "Original immutable description"})
	if err != nil {
		t.Fatal(err)
	}
	var snapshotID string
	var title, description string
	var rev int64
	err = fixture.Migrator.QueryRow(ctx, `SELECT c.id::text,c.title,c.description,c.revision
      FROM assessment_jobs j JOIN incident_contexts c ON c.id=j.context_id
      WHERE j.incident_id=$1 AND j.kind='initial_assessment'`, item.ID).Scan(&snapshotID, &title, &description, &rev)
	if err != nil || title != "First submitted title" || description != "Original immutable description" || rev != 1 {
		t.Fatal("initial job did not reference persisted immutable snapshot", err)
	}
	// An affected-system correction creates a NEW snapshot revision, leaving the
	// original initial job tied to the original immutable values.
	if _, err = fixture.Runtime.Exec(ctx, `UPDATE incidents SET affected_system='new system' WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	var snapshots int
	if err = fixture.Runtime.QueryRow(ctx, `SELECT count(*) FROM incident_contexts WHERE incident_id=$1`, item.ID).Scan(&snapshots); err != nil || snapshots != 2 {
		t.Fatal("revised context was not snapshotted", err)
	}
	if _, err = fixture.Runtime.Exec(ctx, `UPDATE incident_contexts SET title='tamper' WHERE id=$1`, snapshotID); err == nil {
		t.Fatal("runtime could mutate immutable snapshot")
	}
	if _, err = fixture.Runtime.Exec(ctx, `INSERT INTO assessment_jobs(incident_id,incident_revision,kind)
      VALUES($1,1,'initial_assessment')`, item.ID); err == nil {
		t.Fatal("duplicate initial job accepted")
	}
	if _, err = fixture.Runtime.Exec(ctx, `UPDATE incidents SET title='tamper' WHERE id=$1`, item.ID); err == nil {
		t.Fatal("original title can be changed by runtime")
	}
	// A failing enqueue must roll back the incident row and its snapshot.
	_, err = fixture.Migrator.Exec(ctx, `CREATE FUNCTION handoff_reject_job() RETURNS TRIGGER
       LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test job rejection'; END $$`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.Migrator.Exec(ctx, `CREATE TRIGGER handoff_reject_job BEFORE INSERT ON assessment_jobs
       FOR EACH ROW EXECUTE FUNCTION handoff_reject_job()`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = fixture.Migrator.Exec(context.Background(), `DROP TRIGGER IF EXISTS handoff_reject_job ON assessment_jobs`)
		_, _ = fixture.Migrator.Exec(context.Background(), `DROP FUNCTION IF EXISTS handoff_reject_job()`)
	}()
	_, err = service.Create(ctx, testActor(), Input{Title: "Must roll back", Description: "Not persisted"})
	if err == nil {
		t.Fatal("failed job enqueue still returned a successful incident")
	}
	var count int
	if err = fixture.Runtime.QueryRow(ctx, `SELECT count(*) FROM incidents WHERE title='Must roll back'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed enqueue leaked incident", err)
	}
}
