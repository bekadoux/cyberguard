package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/sessionstore"
)

type sessionStateKey struct{}
type sessionState struct {
	failed, revoked bool
	// Only successful, explicit login/logout handlers request a cookie change.
	// Preserve the SCS context until persistence completes; never emit early.
	cookieContext context.Context
	cookieExpiry  time.Time
}

// The three auth endpoints return small JSON responses, not streams. Buffer them
// until SCS finishes committing: LoadAndSave otherwise continues writing the
// handler's success body even after a store error invokes ErrorFunc.
func (a *Authentication) withSession(next http.Handler) http.Handler {
	loadAndSave := a.sessions.LoadAndSave(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := &sessionState{}
		buffer := &authResponseBuffer{header: make(http.Header)}
		ctx := context.WithValue(sessionstore.WithRequest(r.Context()), sessionStateKey{}, state)
		loadAndSave.ServeHTTP(buffer, r.WithContext(ctx))
		if state.revoked {
			// Do not emit a stale Set-Cookie, including a deletion cookie: a
			// concurrent login may already have installed a fresh token.
			a.authError(w, auth.ErrUnauthenticated)
			return
		}
		if state.failed || buffer.overflow {
			a.authError(w, nil)
			return
		}
		// Even a completed 200 refresh or an automatic SCS deletion can arrive
		// after a newer login. Passive/error responses must never mutate cookies.
		buffer.header.Del("Set-Cookie")
		if state.cookieContext != nil {
			a.sessions.WriteSessionCookie(state.cookieContext, buffer,
				a.sessions.Token(state.cookieContext), state.cookieExpiry)
		}
		for name, values := range buffer.header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		status := buffer.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write(buffer.body.Bytes())
	})
}

type authResponseBuffer struct {
	header   http.Header
	body     bytes.Buffer
	status   int
	overflow bool
}

func (w *authResponseBuffer) Header() http.Header { return w.header }

func (w *authResponseBuffer) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *authResponseBuffer) Write(body []byte) (int, error) {
	if w.body.Len()+len(body) > 16384 {
		w.overflow = true
		return 0, io.ErrShortWrite
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}
