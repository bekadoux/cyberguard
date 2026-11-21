package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestAIWorkerAuthentication(t *testing.T) {
	const probe = "/internal/ai/auth-check"
	const contextPath = "/internal/ai/jobs/not-a-uuid/context"
	for _, tc := range []struct {
		name, path, token, header string
		want                      int
	}{
		{"disabled", probe, "", "Bearer fixture", http.StatusNotFound},
		{"missing", probe, "fixture", "", http.StatusUnauthorized},
		{"incorrect", probe, "fixture", "Bearer invalid", http.StatusUnauthorized},
		{"authenticated", probe, "fixture", "Bearer fixture", http.StatusNoContent},
		{"context requires same token", contextPath, "fixture", "Bearer invalid", http.StatusUnauthorized},
		{"context authenticates before validating job", contextPath, "fixture", "Bearer fixture", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := chi.NewRouter()
			MountAIWorkerContext(r, nil, tc.token) // A probe must never access storage.
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", tc.header)
			res := httptest.NewRecorder()
			r.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status = %d, want %d", res.Code, tc.want)
			}
			if tc.want == http.StatusNoContent && (res.Body.Len() != 0 ||
				res.Header().Get("Cache-Control") != "no-store" || res.Header().Get("Set-Cookie") != "") {
				t.Fatal("expected an empty, uncached probe response without a session cookie")
			}
		})
	}
}
