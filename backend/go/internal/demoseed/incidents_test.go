package demoseed

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSampleIncidents(t *testing.T) {
	t.Parallel()
	titles := map[string]bool{}
	ids := map[string]bool{}
	for _, item := range scenarios {
		if item.ID == "" || ids[item.ID] || titles[item.Title] || !strings.HasPrefix(item.Title, "[Demo] ") ||
			!validFixtureText(item.Title, 200) || !validFixtureText(item.Description, 10000) ||
			!validFixtureText(item.System, 500) {
			t.Fatalf("invalid or duplicate scenario %q", item.Title)
		}
		titles[item.Title] = true
		ids[item.ID] = true
	}
	used := []string{"retired-scenario"}
	for _, want := range []int{BatchLimit, len(scenarios) - BatchLimit, 0} {
		sample := SampleIncidents(AvailableScenarios(used))
		if len(sample) != want {
			t.Fatalf("sample contains %d, expected %d", len(sample), want)
		}
		for _, item := range sample {
			if !ids[item.ID] {
				t.Fatalf("sample contains unknown or previously used fixture %q", item.ID)
			}
			delete(ids, item.ID)
			used = append(used, item.ID)
		}
	}
}

func validFixtureText(s string, limit int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= limit &&
		!strings.ContainsRune(s, 0) && strings.TrimSpace(s) != ""
}
