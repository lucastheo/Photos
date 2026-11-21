package httpapi_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/adapter/httpapi"
	"github.com/lucastheo/photos/internal/domain"
)

type fakeAuthValidator struct {
	session domain.AuthSession
}

func (f *fakeAuthValidator) SessionStatus(_ string) domain.AuthSession {
	return f.session
}

func TestRequestIDAndLoggingMiddleware(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := httpapi.RequestIDFromContext(r.Context())
		if id == "" {
			t.Fatal("expected request ID in context")
		}
		if r.URL.Path == "/boom" {
			w.WriteHeader(http.StatusInternalServerError)
		}
		_, _ = w.Write([]byte("ok"))
	})

	chain := httpapi.RequestIDMiddleware(httpapi.LoggingMiddleware(logger)(inner))

	t.Run("generates request id when missing or invalid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ok", nil)
		req.Header.Set("X-Request-ID", "bad\tid")
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("X-Request-ID"), "req_") {
			t.Fatalf("unexpected status=%d X-Request-ID=%q", rec.Code, rec.Header().Get("X-Request-ID"))
		}
	})

	t.Run("preserves valid incoming request id and logs 500 as error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/boom", nil)
		req.Header.Set("X-Request-ID", "client-req-42")
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError || rec.Header().Get("X-Request-ID") != "client-req-42" {
			t.Fatalf("unexpected status=%d X-Request-ID=%q", rec.Code, rec.Header().Get("X-Request-ID"))
		}
		if !strings.Contains(logBuf.String(), `"level":"ERROR"`) {
			t.Fatalf("expected ERROR log entry, got %s", logBuf.String())
		}
	})
}

func TestCrossOriginMiddleware(t *testing.T) {
	t.Parallel()

	okHandler := httpapi.CrossOriginMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("rejects cross-site mutating POST request", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/v1/photos/p1/views", nil)
		req.Host = "localhost:8080"
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		okHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("allows same-origin POST request", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/v1/photos/p1/views", nil)
		req.Host = "localhost:8080"
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Origin", "http://localhost:8080")
		rec := httptest.NewRecorder()
		okHandler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
	})
}

func TestAuthMiddleware(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cases := []struct {
		name       string
		path       string
		validator  httpapi.AuthValidator
		wantStatus int
	}{
		{name: "public health allowed", path: "/api/v1/health", validator: &fakeAuthValidator{}, wantStatus: http.StatusOK},
		{name: "public login allowed", path: "/api/v1/auth/login", validator: &fakeAuthValidator{}, wantStatus: http.StatusOK},
		{name: "non-api SPA route allowed", path: "/library", validator: &fakeAuthValidator{}, wantStatus: http.StatusOK},
		{name: "protected API rejects unauthenticated", path: "/api/v1/photos", validator: &fakeAuthValidator{}, wantStatus: http.StatusUnauthorized},
		{name: "protected API rejects nil validator", path: "/api/v1/photos", validator: nil, wantStatus: http.StatusUnauthorized},
		{name: "protected API allows authenticated", path: "/api/v1/photos", validator: &fakeAuthValidator{session: domain.AuthSession{Authenticated: true}}, wantStatus: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			httpapi.AuthMiddleware(tc.validator)(next).ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

func TestLoginRateLimiter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	limiter := httpapi.NewLoginRateLimiter(2, time.Minute).WithClock(func() time.Time { return now })

	if !limiter.Allow("127.0.0.1") {
		t.Fatal("expected first attempt allowed")
	}
	limiter.RecordFailure("127.0.0.1")
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("expected second attempt allowed")
	}
	limiter.RecordFailure("127.0.0.1")
	if limiter.Allow("127.0.0.1") {
		t.Fatal("expected third attempt blocked after 2 failures")
	}
	if !limiter.Allow("10.0.0.2") {
		t.Fatal("different IP should remain allowed")
	}

	now = now.Add(61 * time.Second)
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("expected budget restored after window expires")
	}

	limiter.RecordFailure("127.0.0.1")
	limiter.RecordFailure("127.0.0.1")
	if limiter.Allow("127.0.0.1") {
		t.Fatal("expected blocked again")
	}
	limiter.Reset("127.0.0.1")
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("expected reset to restore allowance")
	}

	// Distinct IPs trigger stale map sweep after window expires.
	for i := range 70 {
		ip := "198.51.100." + string(rune('a'+(i%26))) + string(rune('0'+(i/26)))
		limiter.RecordFailure(ip)
		limiter.RecordFailure(ip)
		if limiter.Allow(ip) {
			t.Fatalf("expected ip %q blocked", ip)
		}
	}
	now = now.Add(2 * time.Minute)
	if !limiter.Allow("198.51.100.a0") {
		t.Fatal("expected swept ip allowed after window expiry")
	}

	// Default constructor bounds & RequestIDFromContext(nil)
	defLimiter := httpapi.NewLoginRateLimiter(0, 0)
	if !defLimiter.Allow("192.0.2.1") {
		t.Fatal("expected default limiter allow")
	}
	if got := httpapi.RequestIDFromContext(nil); got != "" { //nolint:staticcheck // testing nil context guard
		t.Fatalf("expected empty request ID for nil context, got %q", got)
	}

	// Auth handler without port in RemoteAddr and nil body
	authSvc := &fakeAuthSvc{password: "pw"}
	authH := httpapi.NewAuthHandler(authSvc, defLimiter, false)
	reqNoPort := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	reqNoPort.RemoteAddr = "192.0.2.9"
	rec := httptest.NewRecorder()
	authH.Login(rec, reqNoPort)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("nil login body status = %d, want 400", rec.Code)
	}
}
