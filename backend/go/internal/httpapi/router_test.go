package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestHealthAndErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		method string
		path   string
		status int
		body   string
	}{
		{http.MethodGet, "/healthz", http.StatusOK, `{"status":"ok"}`},
		{http.MethodGet, "/unknown", http.StatusNotFound, `{"error":{"code":"not_found","message":"Resource not found."}}`},
		{http.MethodPost, "/healthz", http.StatusMethodNotAllowed, `{"error":{"code":"method_not_allowed","message":"Method not allowed."}}`},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			NewRouter(testLogger(), nil, time.Second).ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, nil))
			if recorder.Code != tt.status || strings.TrimSpace(recorder.Body.String()) != tt.body {
				t.Fatalf("got %d %s", recorder.Code, recorder.Body.String())
			}
			if recorder.Header().Get("Content-Type") != "application/json" || recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing JSON or cache control headers")
			}
			if recorder.Header().Get("X-Request-ID") == "" || recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing request ID or security headers")
			}
		})
	}
}

func TestRecoveryDoesNotLeakPanic(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	router := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)), nil, time.Second)
	router.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("sensitive-panic-content")
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String()+logs.String(), "sensitive-panic-content") {
		t.Fatal("panic value leaked")
	}
	if !strings.Contains(logs.String(), `"status":500`) {
		t.Fatal("recovered request was not logged with status 500")
	}
}

func TestLogsExcludeRequestSecrets(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	router := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)), nil, time.Second)
	router.Get("/incidents/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	request := httptest.NewRequest(http.MethodGet, "/incidents/sensitive-id?token=sensitive-query", strings.NewReader("sensitive-body"))
	request.Header.Set("Cookie", "session=sensitive-cookie")
	request.Header.Set("Authorization", "Bearer sensitive-token")
	router.ServeHTTP(httptest.NewRecorder(), request)
	if strings.Contains(logs.String(), "sensitive") {
		t.Fatal("request secrets leaked into logs")
	}
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["route"] != "/incidents/{id}" || entry["request_id"] == "" {
		t.Fatal("expected route template and request ID in logs")
	}
	logs.Reset()
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sensitive-unknown-path", nil))
	if strings.Contains(logs.String(), "sensitive") {
		t.Fatal("unmatched raw path leaked into logs")
	}
}

func TestRequestBodyLimit(t *testing.T) {
	t.Parallel()
	router := NewRouter(testLogger(), nil, time.Second)
	router.Post("/body", func(w http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(io.Discard, r.Body)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large.")
			return
		}
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	for _, size := range []int{maxRequestBodyBytes, maxRequestBodyBytes + 1} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/body", strings.NewReader(strings.Repeat("a", size)))
		router.ServeHTTP(recorder, request)
		want := http.StatusNoContent
		if size > maxRequestBodyBytes {
			want = http.StatusRequestEntityTooLarge
		}
		if recorder.Code != want {
			t.Fatalf("body size %d: got %d, want %d", size, recorder.Code, want)
		}
	}
}

func TestReadiness(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		check func(context.Context) error
		want  int
	}{
		{"healthy", func(context.Context) error { return nil }, http.StatusOK},
		{"unavailable", func(context.Context) error { return errors.New("sensitive-database-error") }, http.StatusServiceUnavailable},
		{"missing check", nil, http.StatusServiceUnavailable},
		{"timeout", func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			router := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)), tt.check, 10*time.Millisecond)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if recorder.Code != tt.want {
				t.Fatalf("got %d, want %d", recorder.Code, tt.want)
			}
			if tt.want == http.StatusOK && strings.TrimSpace(recorder.Body.String()) != `{"status":"ready"}` {
				t.Fatal("unexpected readiness success body")
			}
			if tt.want == http.StatusServiceUnavailable && strings.TrimSpace(recorder.Body.String()) != `{"error":{"code":"not_ready","message":"Service is not ready."}}` {
				t.Fatal("unexpected readiness failure body")
			}
			if strings.Contains(recorder.Body.String()+logs.String(), "sensitive") {
				t.Fatal("database error details leaked")
			}
			// Liveness must neither invoke nor depend on the database check.
			recorder = httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if recorder.Code != http.StatusOK {
				t.Fatal("liveness depends on readiness")
			}
		})
	}
}

func TestLivenessDoesNotCallDatabaseCheck(t *testing.T) {
	t.Parallel()
	called := false
	router := NewRouter(testLogger(), func(context.Context) error {
		called = true
		return nil
	}, time.Second)
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if called {
		t.Fatal("liveness called the database check")
	}
}
