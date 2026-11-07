package demoseed

import (
	"strings"
	"testing"
)

func testPasswords() map[string]string {
	return map[string]string{
		"DEV_EMPLOYEE_A_PASSWORD": "test-employee-a-password",
		"DEV_EMPLOYEE_B_PASSWORD": "test-employee-b-password",
		"DEV_ANALYST_PASSWORD":    "test-analyst-password",
		"DEV_ADMIN_PASSWORD":      "test-admin-password",
	}
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestLoadAccounts(t *testing.T) {
	t.Parallel()
	accounts, err := loadAccounts(mapLookup(testPasswords()))
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 4 {
		t.Fatalf("got %d accounts", len(accounts))
	}
	for index, role := range []string{"employee", "employee", "analyst", "admin"} {
		if accounts[index].Role != role || accounts[index].Username == "" || accounts[index].Password == "" {
			t.Fatal("incorrect demo account specification")
		}
	}
}

func TestRejectUnsafePasswords(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]string{
		"missing": "", "short": "short", "long": strings.Repeat("x", 129),
		"placeholder": "REPLACE_WITH_A_PASSWORD", "duplicate": "test-employee-b-password",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := testPasswords()
			env["DEV_EMPLOYEE_A_PASSWORD"] = value
			_, err := loadAccounts(mapLookup(env))
			if err == nil {
				t.Fatal("expected password validation failure")
			}
			if value != "" && strings.Contains(err.Error(), value) {
				t.Fatal("validation error leaked a password")
			}
		})
	}
}

func TestLoadAccountsFromEnvironment(t *testing.T) {
	for key, value := range testPasswords() {
		t.Setenv(key, value)
	}
	accounts, err := LoadAccounts()
	if err != nil || len(accounts) != 4 {
		t.Fatal("could not load demo configuration from environment")
	}
}
