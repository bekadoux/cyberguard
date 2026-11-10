package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	db "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/sessionstore"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxIncidentBodyBytes = 128 << 10

type IncidentAPI struct {
	service *incidents.Service
	users   auth.Users
	auth    *Authentication
	handoff *HandoffAPI
}

func NewIncidentAPI(queries incidents.Queries, users auth.Users, authentication *Authentication) (*IncidentAPI, error) {
	if users == nil || authentication == nil {
		return nil, errors.New("incidents require authentication and user queries")
	}
	service, err := incidents.New(queries)
	if err != nil {
		return nil, err
	}
	return &IncidentAPI{service: service, users: users, auth: authentication}, nil
}

func (a *IncidentAPI) Mount(router chi.Router) {
	router.Route("/incidents", func(routes chi.Router) {
		routes.Use(a.auth.browserPolicy, a.auth.withDeadline, a.auth.protectedSession)
		routes.Post("/", a.create)
		routes.Get("/", a.list)
		routes.Get("/{id}", a.detail)
		if a.handoff != nil {
			a.handoff.mountIncidentRoutes(routes)
		}
	})
	if a.handoff != nil {
		a.handoff.mountAuxiliaryRoutes(router)
	}
}

// EnableHandoff attaches incident workflows without changing the original
// constructor used by the existing unit and integration test fixtures.
func (a *IncidentAPI) EnableHandoff(incidents, identity *pgxpool.Pool) {
	a.handoff = &HandoffAPI{incidentDB: incidents, identityDB: identity, auth: a.auth}
}

// Refresh before running business logic, never after it commits a write on the
// other database. A session failure must not hide an already-created incident.
// Unlike auth LoadAndSave, this path never issues cookies or saves after next.
// In-flight work already authorized before logout may finish (see auth contract).
func (a *Authentication) protectedSession(next http.Handler) http.Handler {
	protected := a.requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := a.sessions.Commit(r.Context()); err != nil {
			a.sessions.ErrorFunc(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string
		if cookie, err := r.Cookie(a.sessions.Cookie.Name); err == nil {
			token = cookie.Value
		}
		ctx, err := a.sessions.Load(sessionstore.WithRequest(r.Context()), token)
		if err != nil {
			a.sessions.ErrorFunc(w, r, err)
			return
		}
		protected.ServeHTTP(w, r.WithContext(ctx))
	})
}

type incidentSummaryDTO struct {
	ID                string    `json:"id"`
	ReporterID        string    `json:"reporter_id"`
	Title             string    `json:"title"`
	Status            string    `json:"status"`
	Category          *string   `json:"category"`
	AssignedAnalystID *string   `json:"assigned_analyst_id"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type incidentDetailDTO struct {
	incidentSummaryDTO
	Description     string      `json:"description"`
	AffectedSystem  *string     `json:"affected_system"`
	ResolutionNote  *string     `json:"resolution_note"`
	AssignedAnalyst *analystDTO `json:"assigned_analyst"`
}

type analystDTO struct {
	ID       string  `json:"id"`
	Username *string `json:"username"`
}

type internalIncidentSummaryDTO struct {
	incidentSummaryDTO
	Severity *string `json:"severity"`
}

type internalIncidentDetailDTO struct {
	incidentDetailDTO
	Severity *string `json:"severity"`
}

func nullableText(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableUUID(value pgtype.UUID) *string {
	if !value.Valid {
		return nil
	}
	id := value.String()
	return &id
}

// A professional keeps professional visibility even when they are also the reporter.
// Ordinary employees never receive severity or internal details.
func internalIncidentView(user auth.User, _ pgtype.UUID) bool {
	return user.Role == "analyst" || user.Role == "admin"
}

func summaryDTO(row db.ListVisibleIncidentsRow) incidentSummaryDTO {
	return incidentSummaryDTO{
		ID: row.ID.String(), ReporterID: row.ReporterID.String(), Title: row.Title,
		Status: row.Status, Category: nullableText(row.Category), AssignedAnalystID: nullableUUID(row.AssignedAnalystID),
		CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(),
	}
}

func (a *IncidentAPI) detailDTO(r *http.Request, user auth.User, row db.Incident) (any, error) {
	dto := incidentDetailDTO{
		incidentSummaryDTO: summaryDTO(db.ListVisibleIncidentsRow{
			ID: row.ID, ReporterID: row.ReporterID, Title: row.Title, Status: row.Status,
			Category: row.Category, AssignedAnalystID: row.AssignedAnalystID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}),
		Description: row.Description, AffectedSystem: nullableText(row.AffectedSystem), ResolutionNote: nullableText(row.ResolutionNote),
	}
	if row.AssignedAnalystID.Valid {
		dto.AssignedAnalyst = &analystDTO{ID: row.AssignedAnalystID.String()}
		assigned, err := a.users.GetUserByID(r.Context(), row.AssignedAnalystID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, incidents.ErrUnavailable
		}
		if err == nil {
			dto.AssignedAnalyst.Username = &assigned.Username
		}
	}
	if internalIncidentView(user, row.ReporterID) {
		return internalIncidentDetailDTO{incidentDetailDTO: dto, Severity: nullableText(row.Severity)}, nil
	}
	return dto, nil
}

func (a *IncidentAPI) create(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		incidentError(w, incidents.ErrInvalid)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxIncidentBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Incident request body exceeds 128 KiB.")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_request", "Could not read the incident request.")
		}
		return
	}
	var request struct {
		Title          *string `json:"title"`
		Description    *string `json:"description"`
		AffectedSystem *string `json:"affected_system"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if !utf8.Valid(data) || decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_request", "Provide one UTF-8 JSON object with title, description and optional affected_system only.")
		return
	}
	input := incidents.Input{AffectedSystem: request.AffectedSystem}
	if request.Title != nil {
		input.Title = *request.Title
	}
	if request.Description != nil {
		input.Description = *request.Description
	}
	user := r.Context().Value(userContextKey{}).(auth.User)
	row, err := a.service.Create(r.Context(), user, input)
	if err != nil {
		incidentError(w, err)
		return
	}
	dto, err := a.detailDTO(r, user, row)
	if err != nil {
		incidentError(w, err)
		return
	}
	w.Header().Set("Location", "/incidents/"+row.ID.String())
	writeJSON(w, http.StatusCreated, map[string]any{"incident": dto})
}

func (a *IncidentAPI) list(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		incidentError(w, incidents.ErrInvalid)
		return
	}
	options := incidents.ListOptions{Limit: incidents.DefaultLimit}
	for key, values := range query {
		if len(values) != 1 || values[0] == "" {
			incidentError(w, incidents.ErrInvalid)
			return
		}
		switch key {
		case "limit":
			options.Limit, err = strconv.Atoi(values[0])
		case "cursor":
			options.Cursor = values[0]
		case "status":
			options.Status = values[0]
		case "category":
			options.Category = values[0]
		default:
			incidentError(w, incidents.ErrInvalid)
			return
		}
		if err != nil {
			incidentError(w, incidents.ErrInvalid)
			return
		}
	}
	user := r.Context().Value(userContextKey{}).(auth.User)
	page, err := a.service.List(r.Context(), user, options)
	if err != nil {
		incidentError(w, err)
		return
	}
	items := make([]any, 0, len(page.Incidents))
	for _, row := range page.Incidents {
		dto := summaryDTO(row)
		if internalIncidentView(user, row.ReporterID) {
			items = append(items, internalIncidentSummaryDTO{incidentSummaryDTO: dto, Severity: nullableText(row.Severity)})
		} else {
			items = append(items, dto)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": items, "next_cursor": page.NextCursor})
}

func (a *IncidentAPI) detail(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		incidentError(w, incidents.ErrInvalid)
		return
	}
	user := r.Context().Value(userContextKey{}).(auth.User)
	row, err := a.service.Get(r.Context(), user, chi.URLParam(r, "id"))
	if err != nil {
		incidentError(w, err)
		return
	}
	dto, err := a.detailDTO(r, user, row)
	if err != nil {
		incidentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incident": dto})
}

func incidentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, incidents.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid incident fields, filters, cursor or page size.")
	case errors.Is(err, incidents.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Incident not found.")
	case errors.Is(err, incidents.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "This action is not allowed.")
	default:
		writeError(w, http.StatusServiceUnavailable, "incidents_unavailable", "Incident service is temporarily unavailable.")
	}
}
