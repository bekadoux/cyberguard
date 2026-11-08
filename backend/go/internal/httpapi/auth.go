package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/sessionstore"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type Authentication struct {
	service  *auth.Service
	sessions *scs.SessionManager
	limiter  *auth.LoginLimiter
	csrf     *http.CrossOriginProtection
	settings config.AuthConfig
	timeout  time.Duration
}

type userDTO struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type userResponse struct {
	User userDTO `json:"user"`
}

type userContextKey struct{}

func NewAuthentication(users auth.Users, store scs.Store, settings config.AuthConfig, timeout time.Duration, logger *slog.Logger) (*Authentication, error) {
	if store == nil || timeout <= 0 || settings.Lifetime <= 0 || settings.IdleTimeout <= 0 || settings.IdleTimeout > settings.Lifetime {
		return nil, errors.New("invalid authentication dependencies or session timeouts")
	}
	if (settings.Environment != "production" && settings.Environment != "development") || (!settings.CookieSecure && settings.Environment != "development") {
		return nil, errors.New("insecure cookies require an explicit development environment")
	}
	service, err := auth.NewService(users)
	if err != nil {
		return nil, err
	}
	sessions := scs.New()
	sessions.Store = store
	sessions.HashTokenInStore = true
	sessions.Lifetime = settings.Lifetime
	sessions.IdleTimeout = settings.IdleTimeout
	sessions.Cookie.Name = "__Host-cyberguard_session"
	if !settings.CookieSecure {
		sessions.Cookie.Name = "cyberguard_session"
	}
	sessions.Cookie.HttpOnly = true
	sessions.Cookie.Secure = settings.CookieSecure
	sessions.Cookie.SameSite = http.SameSiteLaxMode
	sessions.Cookie.Path = "/"
	sessions.Cookie.Domain = ""
	sessions.Cookie.Persist = true
	sessions.ErrorFunc = func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(err, sessionstore.ErrRevoked) {
			if state, ok := r.Context().Value(sessionStateKey{}).(*sessionState); ok {
				state.revoked = true
				return
			}
			writeError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		// Never log raw store errors: they may contain connection/session details.
		logger.Error("session storage failed", "request_id", middleware.GetReqID(r.Context()))
		if state, ok := r.Context().Value(sessionStateKey{}).(*sessionState); ok {
			state.failed = true
			return
		}
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is temporarily unavailable.")
	}
	csrf := http.NewCrossOriginProtection()
	for _, origin := range settings.TrustedOrigins {
		if err := csrf.AddTrustedOrigin(origin); err != nil {
			return nil, errors.New("invalid trusted authentication origin")
		}
	}
	csrf.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusForbidden, "csrf_rejected", "Request origin is not allowed.")
	}))
	return &Authentication{
		service: service, sessions: sessions, csrf: csrf, settings: settings, timeout: timeout,
		limiter: auth.NewLoginLimiter(5, time.Minute, 10000),
	}, nil
}

func (a *Authentication) Mount(router chi.Router) {
	router.Route("/auth", func(routes chi.Router) {
		// Empty go-chi/cors origins default to '*', so install no CORS middleware
		// at all for the secure, same-origin-only default.
		if len(a.settings.TrustedOrigins) > 0 {
			routes.Use(cors.Handler(cors.Options{
				AllowedOrigins:   a.settings.TrustedOrigins,
				AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodOptions},
				AllowedHeaders:   []string{"Accept", "Content-Type"},
				ExposedHeaders:   []string{"X-Request-ID", "Retry-After"},
				AllowCredentials: true, MaxAge: 300,
			}))
		}
		routes.Use(a.csrf.Handler)
		routes.Use(a.withDeadline)
		routes.With(a.limitLogin, a.withSession).Post("/login", a.login)
		routes.With(a.withSession, a.requireUser).Get("/me", a.me)
		routes.With(a.withSession).Post("/logout", a.logout)
	})
}

func (a *Authentication) withDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *Authentication) limitLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer := loginPeer(r, a.settings.TrustedProxies)
		if allowed, retry := a.limiter.Allow(peer); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(retry.Seconds())))))
			writeError(w, http.StatusTooManyRequests, "login_rate_limited", "Too many login attempts. Try again later.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Authentication) login(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decoder.Decode(&request); err != nil {
		a.invalidLoginBody(w, err)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		a.invalidLoginBody(w, err)
		return
	}
	if len(request.Username) == 0 || len(request.Username) > 128 || len(request.Password) == 0 || len(request.Password) > 128 {
		a.invalidLoginBody(w, nil)
		return
	}
	user, err := a.service.Authenticate(r.Context(), request.Username, request.Password)
	if err != nil {
		a.authError(w, err)
		return
	}
	if err := a.sessions.RenewToken(r.Context()); err != nil {
		a.authError(w, auth.ErrUnavailable)
		return
	}
	_ = a.sessions.Clear(r.Context())
	a.sessions.Put(r.Context(), "user_id", user.ID)
	a.sessions.Put(r.Context(), "auth_version", user.AuthVersion)
	state := r.Context().Value(sessionStateKey{}).(*sessionState)
	state.cookieContext = r.Context()
	// The browser cookie lasts to the absolute deadline. Idle expiry is enforced
	// and refreshed in PostgreSQL without passive responses rewriting cookies.
	state.cookieExpiry = a.sessions.Deadline(r.Context())
	writeJSON(w, http.StatusOK, responseUser(user))
}

func (a *Authentication) invalidLoginBody(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large.")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_request", "Provide one JSON object with username and password strings of 1 to 128 bytes.")
}

func (a *Authentication) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := a.sessions.Get(r.Context(), "user_id").(string)
		version, _ := a.sessions.Get(r.Context(), "auth_version").(int64)
		user, err := a.service.CurrentUser(r.Context(), id, version)
		if err != nil {
			if errors.Is(err, auth.ErrUnauthenticated) {
				if a.sessions.Token(r.Context()) != "" {
					if err := a.sessions.Destroy(r.Context()); err != nil {
						a.authError(w, auth.ErrUnavailable)
						return
					}
				}
			}
			a.authError(w, err)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey{}, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *Authentication) me(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey{}).(auth.User)
	writeJSON(w, http.StatusOK, responseUser(user))
}

func (a *Authentication) logout(w http.ResponseWriter, r *http.Request) {
	if a.sessions.Token(r.Context()) != "" {
		if err := a.sessions.Destroy(r.Context()); err != nil {
			a.authError(w, auth.ErrUnavailable)
			return
		}
	}
	state := r.Context().Value(sessionStateKey{}).(*sessionState)
	state.cookieContext = r.Context()
	// The zero expiry requests a deletion cookie only for explicit logout.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func responseUser(user auth.User) userResponse {
	return userResponse{User: userDTO{ID: user.ID, Username: user.Username, Role: user.Role}}
}

func (a *Authentication) authError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid username or password.")
	case errors.Is(err, auth.ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, auth.ErrBusy):
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "auth_busy", "Authentication is temporarily busy.")
	default:
		writeError(w, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is temporarily unavailable.")
	}
}
