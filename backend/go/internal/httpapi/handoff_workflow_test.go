package httpapi

import (
	"testing"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/jackc/pgx/v5/pgtype"
)

func handoffTestID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestHandoffRolePrecedenceAndTransitions(t *testing.T) {
	analyst := handoffTestID(t, "11111111-1111-4111-8111-111111111111")
	reporter := handoffTestID(t, "22222222-2222-4222-8222-222222222222")
	outsider := handoffTestID(t, "33333333-3333-4333-8333-333333333333")
	if !internalIncidentView(auth.User{Role: "analyst", ID: analyst.String()}, analyst) ||
		!internalIncidentView(auth.User{Role: "admin", ID: analyst.String()}, analyst) {
		t.Fatal("professional reporters must keep professional view")
	}
	if internalIncidentView(auth.User{Role: "employee", ID: analyst.String()}, analyst) {
		t.Fatal("employee reporter must not receive severity")
	}
	for _, tc := range []struct {
		from, to, role  string
		assigned, actor pgtype.UUID
		allow           bool
	}{
		{"triage", "investigating", "analyst", analyst, analyst, true},
		{"triage", "false_positive", "analyst", outsider, analyst, false},
		{"triage", "investigating", "admin", outsider, analyst, true},
		{"submitted", "triage", "analyst", analyst, analyst, false},
		{"investigating", "awaiting_reporter", "analyst", analyst, analyst, true},
		{"awaiting_reporter", "investigating", "analyst", analyst, analyst, false},
		{"resolved", "closed", "employee", outsider, reporter, true},
		{"resolved", "investigating", "analyst", outsider, reporter, true},
		{"resolved", "closed", "admin", outsider, analyst, false},
		{"closed", "investigating", "admin", outsider, outsider, true},
		{"false_positive", "investigating", "analyst", analyst, analyst, false},
		{"closed", "submitted", "admin", outsider, outsider, false},
	} {
		got := allowedTransition(tc.from, tc.to, tc.role, reporter, tc.assigned, tc.actor)
		if got != tc.allow {
			t.Errorf("transition %s -> %s (%s): got %v want %v", tc.from, tc.to, tc.role, got, tc.allow)
		}
	}
}

func TestHandoffTextValidation(t *testing.T) {
	if !validHandoffText("🙂", 1, true) || validHandoffText("🙂🙂", 1, true) ||
		validHandoffText("\u0000", 20, true) || validHandoffText("   ", 20, true) {
		t.Fatal("Unicode limits or blank validation failed")
	}
}
