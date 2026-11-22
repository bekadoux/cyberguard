package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
)

func (a *Authentication) signup(w http.ResponseWriter, r *http.Request) {
	peer := loginPeer(r, a.settings.TrustedProxies)
	if allowed, retry := a.signupLimiter.Allow(peer); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(retry.Seconds())))))
		writeError(w, http.StatusTooManyRequests, "signup_rate_limited", "Too many signup attempts. Try again later.")
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Unexpected query parameters.")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large.")
		return
	}
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err != nil || !utf8.Valid(data) || decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_request", "Provide one JSON object with only username and password.")
		return
	}
	user, err := a.service.RegisterEmployee(r.Context(), a.registrar, request.Username, request.Password)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, responseUser(user))
	case errors.Is(err, auth.ErrInvalidAccount):
		writeError(w, http.StatusBadRequest, "invalid_request", "Provide a valid username and a nonblank password of 12 to 128 UTF-8 bytes.")
	case errors.Is(err, auth.ErrUsernameTaken):
		writeError(w, http.StatusConflict, "username_taken", "This username is already taken.")
	default:
		a.authError(w, err)
	}
}
