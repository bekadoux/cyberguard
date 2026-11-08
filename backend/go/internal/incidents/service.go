// Package incidents implements the agreed incident create/read rules. Assessment
// enqueueing is intentionally not defined until the shared job contract is agreed.
package incidents

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	db "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrInvalid     = errors.New("invalid incident request")
	ErrNotFound    = errors.New("incident not found")
	ErrForbidden   = errors.New("incident access is forbidden")
	ErrUnavailable = errors.New("incident storage is unavailable")
)

const DefaultLimit = 20
const MaxLimit = 100

type Queries interface {
	CreateIncident(context.Context, db.CreateIncidentParams) (db.Incident, error)
	GetVisibleIncident(context.Context, db.GetVisibleIncidentParams) (db.Incident, error)
	ListVisibleIncidents(context.Context, db.ListVisibleIncidentsParams) ([]db.ListVisibleIncidentsRow, error)
}

type Service struct{ queries Queries }

func New(queries Queries) (*Service, error) {
	if queries == nil {
		return nil, errors.New("incidents require database queries")
	}
	return &Service{queries: queries}, nil
}

type Input struct {
	Title, Description string
	AffectedSystem     *string
}

type ListOptions struct {
	Limit                    int
	Cursor, Status, Category string
}

type Page struct {
	Incidents  []db.ListVisibleIncidentsRow
	NextCursor *string
}

func actorScope(user auth.User) (pgtype.UUID, bool, error) {
	id, err := ParseID(user.ID)
	if err != nil || user.AuthVersion < 1 || (user.Role != "employee" && user.Role != "analyst" && user.Role != "admin") {
		return pgtype.UUID{}, false, ErrForbidden
	}
	return id, user.Role != "employee", nil
}

// ParseID requires a canonical UUID shape. Nonexistent, malformed and inaccessible
// IDs all map to the same public 404; do not expose ownership through errors.
func ParseID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if len(value) != 36 || id.Scan(value) != nil || !id.Valid || id.String() != strings.ToLower(value) {
		return pgtype.UUID{}, ErrNotFound
	}
	return id, nil
}

func validText(value string, max int, required bool) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) &&
		utf8.RuneCountInString(value) <= max && (!required || strings.TrimSpace(value) != "")
}

func (s *Service) Create(ctx context.Context, user auth.User, input Input) (db.Incident, error) {
	id, _, err := actorScope(user)
	if err != nil {
		return db.Incident{}, err
	}
	if !validText(input.Title, 200, true) || !validText(input.Description, 10000, true) ||
		(input.AffectedSystem != nil && !validText(*input.AffectedSystem, 500, false)) {
		return db.Incident{}, fmt.Errorf("%w: title and description must contain text; maximum lengths are 200, 10000 and 500 characters", ErrInvalid)
	}
	var affected pgtype.Text
	if input.AffectedSystem != nil && strings.TrimSpace(*input.AffectedSystem) != "" {
		affected = pgtype.Text{String: *input.AffectedSystem, Valid: true}
	}
	if ctx.Err() != nil {
		return db.Incident{}, ErrUnavailable
	}
	item, err := s.queries.CreateIncident(ctx, db.CreateIncidentParams{
		ReporterID: id, Title: input.Title, Description: input.Description, AffectedSystem: affected,
	})
	if err != nil {
		return db.Incident{}, ErrUnavailable
	}
	return item, nil
}

func (s *Service) Get(ctx context.Context, user auth.User, value string) (db.Incident, error) {
	actor, company, err := actorScope(user)
	if err != nil {
		return db.Incident{}, err
	}
	id, err := ParseID(value)
	if err != nil {
		return db.Incident{}, ErrNotFound
	}
	if ctx.Err() != nil {
		return db.Incident{}, ErrUnavailable
	}
	item, err := s.queries.GetVisibleIncident(ctx, db.GetVisibleIncidentParams{ID: id, ActorID: actor, ReadCompany: company})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Incident{}, ErrNotFound
	}
	if err != nil {
		return db.Incident{}, ErrUnavailable
	}
	return item, nil
}

func ValidStatus(value string) bool {
	switch value {
	case "submitted", "triage", "investigating", "awaiting_reporter", "resolved", "closed", "false_positive":
		return true
	}
	return false
}

func ValidCategory(value string) bool {
	switch value {
	case "phishing", "malware", "account_compromise", "data_exposure", "lost_device", "policy_violation", "service_disruption", "other":
		return true
	}
	return false
}

func (s *Service) List(ctx context.Context, user auth.User, options ListOptions) (Page, error) {
	actor, company, err := actorScope(user)
	if err != nil {
		return Page{}, err
	}
	if options.Limit < 1 || options.Limit > MaxLimit ||
		(options.Status != "" && !ValidStatus(options.Status)) || (options.Category != "" && !ValidCategory(options.Category)) {
		return Page{}, fmt.Errorf("%w: unsupported filters or page size", ErrInvalid)
	}
	args := db.ListVisibleIncidentsParams{
		ActorID: actor, ReadCompany: company, PageLimit: int32(options.Limit + 1),
		StatusFilter:   pgtype.Text{String: options.Status, Valid: options.Status != ""},
		CategoryFilter: pgtype.Text{String: options.Category, Valid: options.Category != ""},
	}
	if options.Cursor != "" {
		created, id, err := decodeCursor(options.Cursor)
		if err != nil {
			return Page{}, fmt.Errorf("%w: malformed cursor", ErrInvalid)
		}
		args.BeforeCreatedAt, args.BeforeID = created, id
	}
	if ctx.Err() != nil {
		return Page{}, ErrUnavailable
	}
	items, err := s.queries.ListVisibleIncidents(ctx, args)
	if err != nil {
		return Page{}, ErrUnavailable
	}
	page := Page{Incidents: items}
	if len(items) > options.Limit {
		page.Incidents = items[:options.Limit]
		last := page.Incidents[len(page.Incidents)-1]
		cursor := encodeCursor(last.CreatedAt.Time, last.ID)
		page.NextCursor = &cursor
	}
	return page, nil
}

func encodeCursor(created time.Time, id pgtype.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(created.UTC().Format(time.RFC3339Nano) + "," + id.String()))
}

func decodeCursor(value string) (pgtype.Timestamptz, pgtype.UUID, error) {
	if len(value) > 256 {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ErrInvalid
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ErrInvalid
	}
	parts := strings.Split(string(data), ",")
	if len(parts) != 2 {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ErrInvalid
	}
	created, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil || created.UTC().Year() < 1 || created.UTC().Year() > 9999 {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ErrInvalid
	}
	id, err := ParseID(parts[1])
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ErrInvalid
	}
	return pgtype.Timestamptz{Time: created.UTC(), Valid: true}, id, nil
}
