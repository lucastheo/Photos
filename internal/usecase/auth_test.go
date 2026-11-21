package usecase_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lucastheo/photos/internal/domain"
	"github.com/lucastheo/photos/internal/usecase"
)

type fakeSecretStore struct {
	settings map[string]string
	getErr   error
	setErr   error
}

func (f *fakeSecretStore) GetSetting(_ context.Context, key string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	v, ok := f.settings[key]
	if !ok {
		return "", domain.ErrSettingNotFound
	}
	return v, nil
}

func (f *fakeSecretStore) SetSetting(_ context.Context, key, value string, _ time.Time) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.settings == nil {
		f.settings = make(map[string]string)
	}
	f.settings[key] = value
	return nil
}

func TestStaticPasswordVerifier(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte("secret-pass"))
	hexHash := hex.EncodeToString(sum[:])

	cases := []struct {
		name           string
		plain          string
		hash           string
		attempt        string
		wantConfigured bool
		wantPass       bool
	}{
		{name: "plaintext match", plain: "my-pw", attempt: "my-pw", wantConfigured: true, wantPass: true},
		{name: "plaintext mismatch", plain: "my-pw", attempt: "other", wantConfigured: true, wantPass: false},
		{name: "plaintext empty attempt", plain: "my-pw", attempt: "", wantConfigured: true, wantPass: false},
		{name: "sha256 hash match", hash: hexHash, attempt: "secret-pass", wantConfigured: true, wantPass: true},
		{name: "sha256 prefixed upper match", hash: "sha256:" + strings.ToUpper(hexHash), attempt: "secret-pass", wantConfigured: true, wantPass: true},
		{name: "sha256 hash mismatch", hash: hexHash, attempt: "wrong", wantConfigured: true, wantPass: false},
		{name: "malformed hash rejects", hash: "not-hex", attempt: "secret-pass", wantConfigured: false, wantPass: false},
		{name: "unconfigured verifier rejects", attempt: "anything", wantConfigured: false, wantPass: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := usecase.NewStaticPasswordVerifier(tc.plain, tc.hash)
			if got := v.Configured(); got != tc.wantConfigured {
				t.Fatalf("Configured() = %v, want %v", got, tc.wantConfigured)
			}
			if got := v.VerifyPassword(tc.attempt); got != tc.wantPass {
				t.Fatalf("VerifyPassword(%q) = %v, want %v", tc.attempt, got, tc.wantPass)
			}
		})
	}
	var nilVerifier *usecase.StaticPasswordVerifier
	if nilVerifier.Configured() || nilVerifier.VerifyPassword("pw") {
		t.Fatal("nil verifier should report unconfigured and reject")
	}
}

func TestAuthUseCase(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

	t.Run("login issues verifiable token that expires after TTL", func(t *testing.T) {
		t.Parallel()
		cur := now
		verifier := usecase.NewStaticPasswordVerifier("hunter2", "")
		uc := usecase.NewAuthUseCase(false, verifier, []byte("0123456789abcdef0123456789abcdef")).
			WithClock(func() time.Time { return cur }).
			WithTTL(2 * time.Hour)

		if uc.AuthDisabled() {
			t.Fatal("expected AuthDisabled() == false")
		}
		if _, _, err := uc.Login(context.Background(), "wrong"); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("expected ErrInvalidCredentials, got %v", err)
		}

		tok, exp, err := uc.Login(context.Background(), "hunter2")
		if err != nil {
			t.Fatalf("Login failed: %v", err)
		}
		if !exp.Equal(now.Add(2 * time.Hour)) {
			t.Fatalf("exp = %v, want %v", exp, now.Add(2*time.Hour))
		}
		if sess := uc.SessionStatus(tok); !sess.Authenticated || sess.AuthDisabled {
			t.Fatalf("expected authenticated session, got %+v", sess)
		}
		if uc.VerifyToken(tok + "tampered") {
			t.Fatal("expected tampered token to fail")
		}
		if uc.VerifyToken("bad.token") || uc.VerifyToken("nothex.nonce.nothex") {
			t.Fatal("expected malformed tokens to fail")
		}

		cur = now.Add(3 * time.Hour)
		if sess := uc.SessionStatus(tok); sess.Authenticated {
			t.Fatalf("expected expired token to be rejected, got %+v", sess)
		}
	})

	t.Run("auth disabled bypasses credentials and reports authenticated", func(t *testing.T) {
		t.Parallel()
		uc := usecase.NewAuthUseCase(true, nil, nil)
		if !uc.AuthDisabled() {
			t.Fatal("expected AuthDisabled() == true")
		}
		if sess := uc.SessionStatus(""); !sess.Authenticated || !sess.AuthDisabled {
			t.Fatalf("unexpected disabled session status: %+v", sess)
		}
		tok, _, err := uc.Login(context.Background(), "")
		if err != nil || !uc.VerifyToken(tok) {
			t.Fatalf("disabled Login/VerifyToken failed: tok=%q err=%v", tok, err)
		}
	})

	t.Run("EnsureSecret persists and reloads secret across restarts", func(t *testing.T) {
		t.Parallel()
		store := &fakeSecretStore{}
		verifier := usecase.NewStaticPasswordVerifier("pw", "")
		uc1 := usecase.NewAuthUseCase(false, verifier, nil).WithClock(func() time.Time { return now })
		if err := uc1.EnsureSecret(context.Background(), store); err != nil {
			t.Fatalf("EnsureSecret initial failed: %v", err)
		}
		if store.settings["session_secret"] == "" {
			t.Fatal("expected persisted session_secret in store")
		}
		tok, _, err := uc1.Login(context.Background(), "pw")
		if err != nil {
			t.Fatalf("Login failed: %v", err)
		}

		uc2 := usecase.NewAuthUseCase(false, verifier, nil).WithClock(func() time.Time { return now })
		if err := uc2.EnsureSecret(context.Background(), store); err != nil {
			t.Fatalf("EnsureSecret reload failed: %v", err)
		}
		if !uc2.VerifyToken(tok) {
			t.Fatal("expected reloaded AuthUseCase to verify token issued before restart")
		}
	})

	t.Run("EnsureSecret handles explicit secret and store errors", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("sqlite error")
		explicit := usecase.NewAuthUseCase(false, nil, []byte("explicit-secret"))
		if err := explicit.EnsureSecret(context.Background(), &fakeSecretStore{getErr: boom}); err != nil {
			t.Fatalf("explicit secret should skip store, got %v", err)
		}

		ucGetErr := usecase.NewAuthUseCase(false, nil, nil)
		if err := ucGetErr.EnsureSecret(context.Background(), &fakeSecretStore{getErr: boom}); !errors.Is(err, boom) {
			t.Fatalf("expected get error, got %v", err)
		}

		ucSetErr := usecase.NewAuthUseCase(false, nil, nil)
		if err := ucSetErr.EnsureSecret(context.Background(), &fakeSecretStore{setErr: boom}); !errors.Is(err, boom) {
			t.Fatalf("expected set error, got %v", err)
		}
	})
}
