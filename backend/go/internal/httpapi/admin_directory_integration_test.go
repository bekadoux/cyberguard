//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/testutil"
)

func TestAdminDirectoryEligibleAnalystsAcrossPages(t *testing.T) {
	fixture := testutil.Identity(t, "../../../../db/migrations/identity")
	ctx := context.Background()
	_, err := fixture.Migrator.Exec(ctx, `
		INSERT INTO users(username,role,password_hash)
		SELECT 'employee_' || n, 'employee', '$argon2id$fixture' FROM generate_series(1,501) n;
		INSERT INTO users(username,role,password_hash,is_blocked) VALUES
		('z_staff1','analyst','$argon2id$fixture',false),
		('z_staff2','analyst','$argon2id$fixture',true),
		('z_staff3','analyst','$argon2id$fixture',false),
		('zxstaff4','analyst','$argon2id$fixture',false)`)
	if err != nil {
		t.Fatal(err)
	}
	api := &HandoffAPI{identityDB: fixture.Runtime}
	type page struct {
		Users []struct{ Username string }
		Next  *string `json:"next_cursor"`
	}
	read := func(query string, code int) page {
		t.Helper()
		r := httptest.NewRequest("GET", "/admin/users?"+query, nil)
		r = r.WithContext(context.WithValue(r.Context(), userContextKey{}, auth.User{Role: "admin"}))
		w := httptest.NewRecorder()
		api.listUsers(w, r)
		if w.Code != code {
			t.Fatalf("directory %q: got %d, want %d", query, w.Code, code)
		}
		var p page
		if code == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	first := read("eligible=analyst&q=Z_&limit=1", 200)
	if len(first.Users) != 1 || first.Users[0].Username != "z_staff1" || first.Next == nil {
		t.Fatalf("unexpected first analyst page: %+v", first)
	}
	last := read("eligible=analyst&q=z_&limit=1&cursor="+*first.Next, 200)
	if len(last.Users) != 1 || last.Users[0].Username != "z_staff3" || last.Next != nil {
		t.Fatalf("unexpected final analyst page: %+v", last)
	}
	for _, query := range []string{"eligible=admin", "limit=501", "q=z&q=e", "cursor=", "unknown=x"} {
		read(query, 400)
	}
}
