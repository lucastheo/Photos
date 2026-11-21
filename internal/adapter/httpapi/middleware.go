package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

// SessionCookieName is the HTTP-only cookie storing the signed user session token.
const SessionCookieName = "pt_session"

const maxJSONBodyBytes = 1 << 20

type contextKey int

const requestIDContextKey contextKey = iota

// AuthValidator evaluates session tokens for incoming API requests.
type AuthValidator interface {
	SessionStatus(token string) domain.AuthSession
}

// RequestIDFromContext retrieves the correlation request ID attached to ctx.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDContextKey).(string)
	return id
}

// RequestIDMiddleware attaches a correlation X-Request-ID header and context value.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := sanitizeRequestID(r.Header.Get("X-Request-ID"))
		if reqID == "" {
			reqID = generateRequestID()
		}
		w.Header().Set("X-Request-ID", reqID)
		ctx := context.WithValue(r.Context(), requestIDContextKey, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func sanitizeRequestID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > 128 {
		return ""
	}
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if c < 0x21 || c > 0x7E {
			return ""
		}
	}
	return trimmed
}

func generateRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "req_fallback"
	}
	return "req_" + hex.EncodeToString(buf[:])
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(p []byte) (int, error) {
	if sr.status == 0 {
		sr.status = http.StatusOK
	}
	n, err := sr.ResponseWriter.Write(p)
	sr.bytes += n
	return n, err
}

func (sr *statusRecorder) Unwrap() http.ResponseWriter {
	return sr.ResponseWriter
}

// LoggingMiddleware records structured HTTP request logs via slog.
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if logger == nil {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"bytes", rec.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFromContext(r.Context()),
			}
			if status >= http.StatusInternalServerError {
				logger.ErrorContext(r.Context(), "http request", attrs...)
				return
			}
			logger.InfoContext(r.Context(), "http request", attrs...)
		})
	}
}

// CrossOriginMiddleware protects state-mutating requests against cross-site
// and DNS-rebinding attacks using Go's stdlib http.CrossOriginProtection.
func CrossOriginMiddleware(_ *slog.Logger) func(http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteProblemWithStatus(
			w, r, http.StatusForbidden,
			"CROSS_ORIGIN_FORBIDDEN",
			"Cross-origin state-mutating request rejected",
		)
	}))
	return cop.Handler
}

// AuthMiddleware enforces authenticated sessions for protected /api/ endpoints.
func AuthMiddleware(validator AuthValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/") || isPublicAPIPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			token := readSessionCookie(r)
			if validator == nil || !validator.SessionStatus(token).Authenticated {
				WriteProblem(w, r, nil, domain.ErrUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isPublicAPIPath(p string) bool {
	switch p {
	case "/api/v1/health",
		"/api/v1/auth/session",
		"/api/v1/auth/login",
		"/api/v1/auth/logout":
		return true
	default:
		return false
	}
}

func readSessionCookie(r *http.Request) string {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// LoginRateLimiter bounds consecutive failed login attempts per client IP within
// a rolling time window.
type LoginRateLimiter struct {
	mu          sync.Mutex
	maxAttempts int
	window      time.Duration
	now         func() time.Time
	attempts    map[string][]time.Time
	lastSweep   time.Time
}

// NewLoginRateLimiter constructs a concrete per-IP LoginRateLimiter.
func NewLoginRateLimiter(maxAttempts int, window time.Duration) *LoginRateLimiter {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if window <= 0 {
		window = time.Minute
	}
	return &LoginRateLimiter{
		maxAttempts: maxAttempts,
		window:      window,
		now:         func() time.Time { return time.Now().UTC() },
		attempts:    make(map[string][]time.Time),
	}
}

// WithClock overrides the clock function used for rate-limit window checks.
func (l *LoginRateLimiter) WithClock(nowFunc func() time.Time) *LoginRateLimiter {
	if nowFunc != nil {
		l.now = nowFunc
	}
	return l
}

// Allow reports whether key currently has remaining failed-attempt budget.
func (l *LoginRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now().UTC()
	l.maybeSweepLocked(now)
	recent := l.pruneKeyLocked(key, now)
	return len(recent) < l.maxAttempts
}

// RecordFailure records a failed login attempt for key.
func (l *LoginRateLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now().UTC()
	l.maybeSweepLocked(now)
	recent := l.pruneKeyLocked(key, now)
	l.attempts[key] = append(recent, now)
}

// Reset clears recorded failures for key after a successful login.
func (l *LoginRateLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

func (l *LoginRateLimiter) maybeSweepLocked(now time.Time) {
	if len(l.attempts) == 0 {
		return
	}
	if len(l.attempts) < 64 && now.Sub(l.lastSweep) < l.window {
		return
	}
	for k := range l.attempts {
		l.pruneKeyLocked(k, now)
	}
	l.lastSweep = now
}

func (l *LoginRateLimiter) pruneKeyLocked(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	entries := l.attempts[key]
	kept := entries[:0]
	for _, ts := range entries {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) == 0 {
		delete(l.attempts, key)
		return nil
	}
	l.attempts[key] = kept
	return kept
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return "unknown"
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any, allowEmpty bool) error {
	if r.Body == nil {
		if allowEmpty {
			return nil
		}
		return fmt.Errorf("%w: request body is required", domain.ErrInvalidInput)
	}
	limited := http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	dec := json.NewDecoder(limited)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) && allowEmpty {
			return nil
		}
		return fmt.Errorf("%w: invalid JSON body", domain.ErrInvalidInput)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: unexpected trailing JSON data", domain.ErrInvalidInput)
	}
	return nil
}
