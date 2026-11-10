package httpapi

import "testing"

func TestFeedbackInput(t *testing.T) {
	category := "phishing"
	invalid := "unknown"
	cases := []struct {
		decision string
		category *string
		reason   string
		valid    bool
	}{
		{"accepted", &category, "", true},
		{"corrected", &category, "analyst review", true},
		{"corrected", &category, "", false},
		{"rejected", nil, "wrong assessment", true},
		{"rejected", nil, "", false},
		{"accepted", nil, "", false},
		{"rejected", &category, "reason", false},
		{"accepted", &invalid, "", false},
		{"other", &category, "", false},
	}
	for _, tc := range cases {
		if got := validFeedbackInput(tc.decision, tc.category, tc.reason); got != tc.valid {
			t.Errorf("decision %s, category %v, reason %q: got %v, want %v", tc.decision, tc.category, tc.reason, got, tc.valid)
		}
	}
}
