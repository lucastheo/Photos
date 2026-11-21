package usecase

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucastheo/photos/internal/domain"
)

const (
	defaultSessionTTL = 30 * 24 * time.Hour
	sessionSecretKey  = "session_secret"
	secretByteLength  = 32
	tokenNonceLength  = 16
)

// PasswordVerifier checks whether a plaintext login password matches configured credentials.
type PasswordVerifier interface {
	VerifyPassword(password string) bool
}

// SessionSecretStore loads and persists generated session signing secrets.
type SessionSecretStore interface {
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string, now time.Time) error
}

// StaticPasswordVerifier validates passwords in constant time against a
// configured SHA-256 hex digest or plaintext password.
type StaticPasswordVerifier struct {
	expectedDigest [sha256.Size]byte
	configured     bool
}

// NewStaticPasswordVerifier constructs a constant-time PasswordVerifier from
// either passwordHash (SHA-256 hex, optionally prefixed with "sha256:") or plainPassword.
func NewStaticPasswordVerifier(plainPassword, passwordHash string) *StaticPasswordVerifier {
	trimmedHash := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(passwordHash)), "sha256:")
	if trimmedHash != "" {
		raw, err := hex.DecodeString(trimmedHash)
		if err != nil || len(raw) != sha256.Size {
			return &StaticPasswordVerifier{configured: false}
		}
		var digest [sha256.Size]byte
		copy(digest[:], raw)
		return &StaticPasswordVerifier{expectedDigest: digest, configured: true}
	}
	if plainPassword != "" {
		return &StaticPasswordVerifier{
			expectedDigest: sha256.Sum256([]byte(plainPassword)),
			configured:     true,
		}
	}
	return &StaticPasswordVerifier{configured: false}
}

// Configured reports whether a valid non-empty password or SHA-256 digest is set.
func (v *StaticPasswordVerifier) Configured() bool {
	return v != nil && v.configured
}

// VerifyPassword reports whether password matches the configured credential in constant time.
func (v *StaticPasswordVerifier) VerifyPassword(password string) bool {
	if v == nil || !v.configured || password == "" {
		return false
	}
	candidate := sha256.Sum256([]byte(password))
	return subtle.ConstantTimeCompare(candidate[:], v.expectedDigest[:]) == 1
}

// AuthUseCase manages single-user password authentication, HMAC session tokens,
// and persistent signing secret initialization.
type AuthUseCase struct {
	authDisabled   bool
	verifier       PasswordVerifier
	explicitSecret bool
	mu             sync.RWMutex
	secret         []byte
	ttl            time.Duration
	now            func() time.Time
}

// NewAuthUseCase constructs a concrete AuthUseCase. When secret is empty, a
// random signing key is generated immediately and may be replaced via EnsureSecret.
func NewAuthUseCase(authDisabled bool, verifier PasswordVerifier, secret []byte) *AuthUseCase {
	explicit := len(secret) > 0
	var key []byte
	if explicit {
		key = slices.Clone(secret)
	} else {
		key = mustRandomBytes(secretByteLength)
	}
	return &AuthUseCase{
		authDisabled:   authDisabled,
		verifier:       verifier,
		explicitSecret: explicit,
		secret:         key,
		ttl:            defaultSessionTTL,
		now:            func() time.Time { return time.Now().UTC() },
	}
}

// WithClock overrides the clock function used for token issuance and expiry checks.
func (u *AuthUseCase) WithClock(nowFunc func() time.Time) *AuthUseCase {
	if nowFunc != nil {
		u.now = nowFunc
	}
	return u
}

// WithTTL overrides the session token lifetime when ttl is positive.
func (u *AuthUseCase) WithTTL(ttl time.Duration) *AuthUseCase {
	if ttl > 0 {
		u.ttl = ttl
	}
	return u
}

// AuthDisabled reports whether password enforcement is disabled by configuration.
func (u *AuthUseCase) AuthDisabled() bool {
	return u.authDisabled
}

// EnsureSecret loads an existing persisted signing secret from store or generates
// and saves a new 32-byte random secret if no explicit secret was configured.
func (u *AuthUseCase) EnsureSecret(ctx context.Context, store SessionSecretStore) error {
	if u.explicitSecret || store == nil {
		return nil
	}
	stored, err := store.GetSetting(ctx, sessionSecretKey)
	if err == nil {
		decoded, decErr := hex.DecodeString(strings.TrimSpace(stored))
		if decErr == nil && len(decoded) >= secretByteLength {
			u.mu.Lock()
			u.secret = decoded
			u.mu.Unlock()
			return nil
		}
	} else if !errors.Is(err, domain.ErrSettingNotFound) {
		return fmt.Errorf("load persisted session secret: %w", err)
	}

	generated := mustRandomBytes(secretByteLength)
	if err := store.SetSetting(ctx, sessionSecretKey, hex.EncodeToString(generated), u.now().UTC()); err != nil {
		return fmt.Errorf("persist generated session secret: %w", err)
	}
	u.mu.Lock()
	u.secret = generated
	u.mu.Unlock()
	return nil
}

// Login verifies password and returns a signed session token and expiration time.
func (u *AuthUseCase) Login(_ context.Context, password string) (string, time.Time, error) {
	if !u.authDisabled {
		if u.verifier == nil || !u.verifier.VerifyPassword(password) {
			return "", time.Time{}, domain.ErrInvalidCredentials
		}
	}
	now := u.now().UTC()
	exp := now.Add(u.ttl)
	nonce := mustRandomBytes(tokenNonceLength)
	payload := strconv.FormatInt(exp.Unix(), 16) + "." + hex.EncodeToString(nonce)
	sig := u.signPayload(payload)
	return payload + "." + hex.EncodeToString(sig), exp, nil
}

// VerifyToken validates the HMAC signature and expiration timestamp of token.
func (u *AuthUseCase) VerifyToken(token string) bool {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return false
	}
	gotSig, err := hex.DecodeString(parts[2])
	if err != nil || len(gotSig) != sha256.Size {
		return false
	}
	payload := parts[0] + "." + parts[1]
	wantSig := u.signPayload(payload)
	if !hmac.Equal(gotSig, wantSig) {
		return false
	}
	expUnix, err := strconv.ParseInt(parts[0], 16, 64)
	if err != nil {
		return false
	}
	return u.now().UTC().Unix() < expUnix
}

// SessionStatus returns the effective AuthSession for the provided token.
func (u *AuthUseCase) SessionStatus(token string) domain.AuthSession {
	if u.authDisabled {
		return domain.AuthSession{Authenticated: true, AuthDisabled: true}
	}
	return domain.AuthSession{
		Authenticated: u.VerifyToken(token),
		AuthDisabled:  false,
	}
}

func (u *AuthUseCase) signPayload(payload string) []byte {
	u.mu.RLock()
	key := slices.Clone(u.secret)
	u.mu.RUnlock()
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func mustRandomBytes(n int) []byte {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("crypto/rand read failed: %v", err))
	}
	return buf
}
