package httpapi

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	db "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/incidents"
	"github.com/jackc/pgx/v5/pgtype"
)

// The API projection intentionally excludes even the internal pagination key.
type timelineEvent struct {
	Status     string    `json:"status"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (a *HandoffAPI) listReporterTimeline(w http.ResponseWriter, r *http.Request) {
	id, ok := incidentPath(w, r)
	if !ok {
		return
	}
	reporter, err := actorUUID(a.user(r))
	if err != nil {
		incidentError(w, incidents.ErrForbidden)
		return
	}
	params, ok := timelineParams(r.URL.RawQuery, id)
	if !ok {
		incidentError(w, incidents.ErrInvalid)
		return
	}
	params.ReporterID = reporter
	data, err := db.New(a.incidentDB).ListReporterTimeline(r.Context(), params)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	var stored []struct {
		timelineEvent
		Key string `json:"key"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		incidentError(w, incidents.ErrUnavailable)
		return
	}
	var next *string
	if len(stored) == int(params.PageLimit) {
		stored = stored[:len(stored)-1]
		last := stored[len(stored)-1]
		cursor := encodeTimelineCursor(id, last.OccurredAt, last.Key)
		next = &cursor
	}
	events := make([]timelineEvent, 0, len(stored))
	for _, record := range stored {
		events = append(events, timelineEvent{Status: record.Status, OccurredAt: record.OccurredAt.UTC()})
	}
	writeJSON(w, http.StatusOK, struct {
		Events     []timelineEvent `json:"events"`
		NextCursor *string         `json:"next_cursor"`
	}{events, next})
}

func timelineParams(raw string, id pgtype.UUID) (db.ListReporterTimelineParams, bool) {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return db.ListReporterTimelineParams{}, false
	}
	params := db.ListReporterTimelineParams{IncidentID: id, PageLimit: incidents.DefaultLimit + 1}
	for key, values := range query {
		if len(values) != 1 || values[0] == "" {
			return params, false
		}
		switch key {
		case "limit":
			n, err := strconv.Atoi(values[0])
			if err != nil || n < 1 || n > incidents.MaxLimit {
				return params, false
			}
			params.PageLimit = int32(n + 1)
		case "cursor":
			at, key, ok := decodeTimelineCursor(id, values[0])
			if !ok {
				return params, false
			}
			params.BeforeTime, params.BeforeKey = pgtype.Timestamptz{Time: at, Valid: true}, key
		default:
			return params, false
		}
	}
	return params, true
}

func encodeTimelineCursor(id pgtype.UUID, at time.Time, key string) string {
	// Key is a digest of the internal event ID, never an audit UUID.
	value := id.String() + "," + at.UTC().Format(time.RFC3339Nano) + "," + key
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeTimelineCursor(id pgtype.UUID, cursor string) (time.Time, string, bool) {
	if len(cursor) > 256 {
		return time.Time{}, "", false
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", false
	}
	fields := strings.Split(string(data), ",")
	if len(fields) != 3 || fields[0] != id.String() || len(fields[2]) != 64 || fields[2] != strings.ToLower(fields[2]) {
		return time.Time{}, "", false
	}
	at, err := time.Parse(time.RFC3339Nano, fields[1])
	if err != nil || at.UTC().Year() < 1 || at.UTC().Year() > 9999 {
		return time.Time{}, "", false
	}
	if _, err = hex.DecodeString(fields[2]); err != nil {
		return time.Time{}, "", false
	}
	return at.UTC(), fields[2], true
}
