package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

// AuthService defines the authentication use-case operations consumed by AuthHandler.
type AuthService interface {
	Login(ctx context.Context, password string) (string, time.Time, error)
	SessionStatus(token string) domain.AuthSession
}

// AuthHandler handles HTTP authentication and session lifecycle endpoints.
type AuthHandler struct {
	auth         AuthService
	limiter      *LoginRateLimiter
	cookieSecure bool
	logger       *slog.Logger
}

// NewAuthHandler constructs a concrete AuthHandler.
func NewAuthHandler(auth AuthService, limiter *LoginRateLimiter, cookieSecure bool) *AuthHandler {
	return &AuthHandler{
		auth:         auth,
		limiter:      limiter,
		cookieSecure: cookieSecure,
	}
}

// WithLogger attaches a structured logger to AuthHandler.
func (h *AuthHandler) WithLogger(logger *slog.Logger) *AuthHandler {
	h.logger = logger
	return h
}

// Session handles GET /api/v1/auth/session.
func (h *AuthHandler) Session(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		WriteProblem(w, r, h.logger, errors.New("auth service unavailable"))
		return
	}
	sess := h.auth.SessionStatus(readSessionCookie(r))
	writeJSON(w, http.StatusOK, toSessionResponse(sess))
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		WriteProblem(w, r, h.logger, errors.New("auth service unavailable"))
		return
	}
	ip := clientIP(r)
	if h.limiter != nil && !h.limiter.Allow(ip) {
		WriteProblem(w, r, h.logger, domain.ErrRateLimited)
		return
	}

	var req LoginRequest
	if err := decodeJSONBody(w, r, &req, false); err != nil {
		WriteProblem(w, r, h.logger, err)
		return
	}
	token, expiresAt, err := h.auth.Login(r.Context(), req.Password)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) && h.limiter != nil {
			h.limiter.RecordFailure(ip)
		}
		WriteProblem(w, r, h.logger, err)
		return
	}
	if h.limiter != nil {
		h.limiter.Reset(ip)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt.UTC(),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.cookieSecure,
	})
	writeJSON(w, http.StatusOK, toSessionResponse(h.auth.SessionStatus(token)))
}

// Logout handles POST /api/v1/auth/logout.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		WriteProblem(w, r, h.logger, errors.New("auth service unavailable"))
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.cookieSecure,
	})
	writeJSON(w, http.StatusOK, toSessionResponse(h.auth.SessionStatus("")))
}
