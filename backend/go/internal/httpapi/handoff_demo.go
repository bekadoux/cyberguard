package httpapi

import (
	"context"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	db "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/demoseed"
	"github.com/jackc/pgx/v5/pgtype"
)

type demoIncidentStatus struct {
	Enabled bool `json:"enabled"`
	Seeded  bool `json:"seeded"`
}

func (a *HandoffAPI) demoIncidentStatus(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_request", "Unexpected query parameters.")
		return
	}
	status := demoIncidentStatus{Enabled: a.demoIncidents}
	if status.Enabled {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var err error
		status.Seeded, err = db.New(a.incidentDB).DemoIncidentBatchExists(ctx)
		if err != nil {
			handoffStorageError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *HandoffAPI) addDemoIncidents(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	if !a.demoIncidents {
		writeError(w, 404, "not_found", "Demo incident creation is disabled.")
		return
	}
	var body *struct{}
	if !decodeHandoffJSON(w, r, &body) {
		return
	}
	if body == nil {
		writeError(w, 400, "invalid_request", "Expected an empty JSON object.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	// Hold the identity checks through the incident commit, without writing
	// identity data or pretending the two databases share a transaction.
	identityTx, err := a.identityDB.Begin(ctx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer identityTx.Rollback(ctx)
	if !a.lockCurrentAdmin(w, r, ctx, identityTx) {
		return
	}
	actor, _ := actorUUID(a.user(r))

	incidentTx, err := a.incidentDB.Begin(ctx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer incidentTx.Rollback(ctx)
	queries := db.New(incidentTx)
	claimed, err := queries.ClaimDemoIncidentBatch(ctx, actor)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if claimed == 0 {
		writeJSON(w, http.StatusOK, map[string]int{"created": 0})
		return
	}
	reporters, err := identity.New(identityTx).LockDemoReporters(ctx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if len(reporters) != demoseed.ReporterCount {
		writeError(w, 409, "demo_accounts_unavailable", "Provision active demo accounts employee_a, employee_b and analyst with their demo roles.")
		return
	}
	// Give each reporter three cases and a random reporter the extra case.
	order := rand.Perm(len(reporters))
	for i, scenario := range demoseed.SampleIncidents() {
		// Use the same insert and snapshot/job triggers as employee submissions.
		item, err := queries.CreateIncident(ctx, db.CreateIncidentParams{
			ReporterID:     reporters[order[i%len(order)]],
			Title:          scenario.Title,
			Description:    scenario.Description,
			AffectedSystem: pgtype.Text{String: scenario.System, Valid: true},
		})
		if err == nil {
			err = recordIncidentAction(ctx, incidentTx, item.ID, actor, "demo_seeded", "", map[string]any{})
		}
		if err != nil {
			handoffStorageError(w, err)
			return
		}
	}
	if commitOrError(w, incidentTx, ctx) {
		writeJSON(w, http.StatusCreated, map[string]int{"created": demoseed.IncidentCount})
	}
}
