package httpapi

import (
	"context"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	db "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/demoseed"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type demoIncidentStatus struct {
	Enabled   bool `json:"enabled"`
	Remaining int  `json:"remaining"`
}

type demoIncidentResult struct {
	Created   int `json:"created"`
	Remaining int `json:"remaining"`
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
		used, err := db.New(a.incidentDB).ListUsedDemoScenarios(ctx)
		if err != nil {
			handoffStorageError(w, err)
			return
		}
		status.Remaining = len(demoseed.AvailableScenarios(used))
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

	incidentTx, err := a.incidentDB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer incidentTx.Rollback(ctx)
	queries := db.New(incidentTx)
	// Serialize selection on the incident database so concurrent requests sample
	// from the latest available set, even if their identity databases differ.
	if err := queries.LockDemoIncidentSelection(ctx); err != nil {
		handoffStorageError(w, err)
		return
	}
	used, err := queries.ListUsedDemoScenarios(ctx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	available := demoseed.AvailableScenarios(used)
	if len(available) == 0 {
		writeJSON(w, http.StatusOK, demoIncidentResult{})
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
	// Spread reports evenly; any extra cases go to random reporters.
	order := rand.Perm(len(reporters))
	batch := demoseed.SampleIncidents(available)
	for i, scenario := range batch {
		// Use the same insert and snapshot/job triggers as employee submissions.
		item, err := queries.CreateIncident(ctx, db.CreateIncidentParams{
			ReporterID:     reporters[order[i%len(order)]],
			Title:          scenario.Title,
			Description:    scenario.Description,
			AffectedSystem: pgtype.Text{String: scenario.System, Valid: true},
		})
		if err == nil {
			err = queries.UseDemoScenario(ctx, db.UseDemoScenarioParams{ScenarioID: scenario.ID, IncidentID: item.ID})
		}
		if err == nil {
			err = recordIncidentAction(ctx, incidentTx, item.ID, actor, "demo_seeded", "", map[string]any{})
		}
		if err != nil {
			handoffStorageError(w, err)
			return
		}
	}
	if commitOrError(w, incidentTx, ctx) {
		writeJSON(w, http.StatusCreated, demoIncidentResult{Created: len(batch), Remaining: len(available) - len(batch)})
	}
}
