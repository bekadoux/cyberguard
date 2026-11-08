package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const maxRequestBodyBytes = 1 << 20

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewRouter(logger *slog.Logger, check func(context.Context) error, databaseTimeout time.Duration) *chi.Mux {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(requestLogger(logger))
	router.Use(recoverPanic(logger))
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-ID", middleware.GetReqID(r.Context()))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
			next.ServeHTTP(w, r)
		})
	})
	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if check == nil {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "Service is not ready.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), databaseTimeout)
		defer cancel()
		if err := check(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "Service is not ready.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Resource not found.")
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	})
	return router
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: apiError{Code: code, Message: message}})
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			response := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				status := response.Status()
				if status == 0 {
					status = http.StatusOK
				}
				// Log the route template, never raw URLs, query strings, headers or bodies.
				logger.Info("HTTP request",
					"request_id", middleware.GetReqID(r.Context()),
					"method", r.Method,
					"route", chi.RouteContext(r.Context()).RoutePattern(),
					"status", status,
					"duration_ms", time.Since(started).Milliseconds(),
				)
			}()
			next.ServeHTTP(response, r)
		})
	}
}

func recoverPanic(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if value := recover(); value != nil {
					if value == http.ErrAbortHandler {
						panic(value)
					}
					// Panic values can contain incident content or credentials.
					logger.Error("HTTP handler panicked", "request_id", middleware.GetReqID(r.Context()))
					if response, ok := w.(middleware.WrapResponseWriter); ok && response.Status() != 0 {
						// A response already on the wire cannot be replaced with JSON.
						// Ask net/http to abort it instead of appending a misleading error.
						panic(http.ErrAbortHandler)
					}
					writeError(w, http.StatusInternalServerError, "internal_error", "An internal error occurred.")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
