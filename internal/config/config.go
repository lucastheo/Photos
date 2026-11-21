package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Config holds the runtime configuration for Photo View Tracker.
type Config struct {
	// LibraryPath is the root directory of the read-only photo library.
	LibraryPath string
	// DBPath is the SQLite database file path outside LibraryPath.
	DBPath string
	// CachePath is the preview and thumbnail cache directory outside LibraryPath.
	CachePath string
	// Host is the network interface address to bind (empty binds all interfaces).
	Host string
	// Port is the TCP port for the HTTP server.
	Port int
	// AuthDisabled disables password login enforcement when true.
	AuthDisabled bool
	// AuthPassword is the configured plaintext password for single-user login.
	AuthPassword string
	// AuthPasswordHash is the configured SHA-256 hex digest of the login password.
	AuthPasswordHash string
	// AuthSecret is the optional hex or raw HMAC signing secret for session cookies.
	AuthSecret string
	// CookieSecure sets the Secure attribute on session cookies when true.
	CookieSecure bool
	// SyncWorkers caps parallel SHA-256 file hashing workers during library sync.
	SyncWorkers int
	// PreviewWorkers caps parallel external media decoding workers.
	PreviewWorkers int
	// LogFormat selects structured log output format ("text" or "json").
	LogFormat string
	// LogLevel selects minimum structured log severity ("debug", "info", "warn", "error").
	LogLevel string
}

// DefaultConfig returns a Config populated with documented project defaults.
func DefaultConfig() Config {
	numCPU := max(1, runtime.NumCPU())
	return Config{
		LibraryPath:    "./photos",
		DBPath:         "./data/photos.db",
		CachePath:      "./data/cache",
		Host:           "",
		Port:           8080,
		AuthDisabled:   false,
		CookieSecure:   false,
		SyncWorkers:    numCPU,
		PreviewWorkers: min(4, max(1, numCPU/2)),
		LogFormat:      "text",
		LogLevel:       "info",
	}
}

// Load loads configuration from ".env" (if present) and process environment variables.
func Load() (Config, error) {
	return LoadFromFileAndEnv(".env", os.LookupEnv)
}

// LoadFromFileAndEnv loads defaults, merges key/value pairs from dotEnvPath if
// present, and overlays values returned by lookup.
func LoadFromFileAndEnv(dotEnvPath string, lookup func(string) (string, bool)) (Config, error) {
	cfg := DefaultConfig()
	fileVals, err := readDotEnvFile(dotEnvPath)
	if err != nil {
		return Config{}, err
	}

	resolve := func(key string) (string, bool) {
		if lookup != nil {
			if val, ok := lookup(key); ok {
				return val, true
			}
		}
		val, ok := fileVals[key]
		return val, ok
	}

	if err := applyConfigValues(&cfg, resolve); err != nil {
		return Config{}, err
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ListenAddr returns the host:port address string suitable for net/http.Server.
func (c Config) ListenAddr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// ParseDotEnv parses KEY=VALUE pairs from a .env formatted reader.
func ParseDotEnv(r io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		key, val, ok, err := parseDotEnvLine(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		if ok {
			values[key] = val
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read dotenv: %w", err)
	}
	return values, nil
}

func readDotEnvFile(dotEnvPath string) (map[string]string, error) {
	if strings.TrimSpace(dotEnvPath) == "" {
		return map[string]string{}, nil
	}
	f, err := os.Open(dotEnvPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("open %q: %w", dotEnvPath, err)
	}
	defer func() { _ = f.Close() }()

	vals, err := ParseDotEnv(f)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", dotEnvPath, err)
	}
	return vals, nil
}

func parseDotEnvLine(raw string) (string, string, bool, error) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false, nil
	}
	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	}
	key, val, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false, errors.New("missing '=' separator")
	}
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false, fmt.Errorf("invalid key %q", key)
	}
	return key, parseDotEnvValue(strings.TrimSpace(val)), true, nil
}

func parseDotEnvValue(val string) string {
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		inner := val[1 : len(val)-1]
		replacer := strings.NewReplacer(`\n`, "\n", `\r`, "\r", `\t`, "\t", `\"`, `"`, `\\`, `\`)
		return replacer.Replace(inner)
	}
	if len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'' {
		return val[1 : len(val)-1]
	}
	if idx := strings.Index(val, " #"); idx >= 0 {
		val = strings.TrimSpace(val[:idx])
	}
	return val
}

func applyConfigValues(cfg *Config, resolve func(string) (string, bool)) error {
	applyStringSetting(&cfg.LibraryPath, "LIBRARY_PATH", resolve)
	applyStringSetting(&cfg.DBPath, "DB_PATH", resolve)
	applyStringSetting(&cfg.CachePath, "CACHE_PATH", resolve)
	applyStringSetting(&cfg.Host, "HOST", resolve)
	applyStringSetting(&cfg.AuthPassword, "AUTH_PASSWORD", resolve)
	applyStringSetting(&cfg.AuthPasswordHash, "AUTH_PASSWORD_HASH", resolve)
	applyStringSetting(&cfg.AuthSecret, "AUTH_SECRET", resolve)
	applyStringSetting(&cfg.LogFormat, "LOG_FORMAT", resolve)
	applyStringSetting(&cfg.LogLevel, "LOG_LEVEL", resolve)
	cfg.LogFormat = strings.ToLower(strings.TrimSpace(cfg.LogFormat))
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))

	if err := applyNumericAndBoolSettings(cfg, resolve); err != nil {
		return err
	}
	return nil
}

func applyStringSetting(dst *string, key string, resolve func(string) (string, bool)) {
	if val, ok := resolve(key); ok {
		*dst = strings.TrimSpace(val)
	}
}

func applyNumericAndBoolSettings(cfg *Config, resolve func(string) (string, bool)) error {
	if err := parseIntSetting(&cfg.Port, "PORT", resolve); err != nil {
		return err
	}
	if err := parseIntSetting(&cfg.SyncWorkers, "SYNC_WORKERS", resolve); err != nil {
		return err
	}
	if err := parseIntSetting(&cfg.PreviewWorkers, "PREVIEW_WORKERS", resolve); err != nil {
		return err
	}
	if err := parseBoolSetting(&cfg.AuthDisabled, "AUTH_DISABLED", resolve); err != nil {
		return err
	}
	return parseBoolSetting(&cfg.CookieSecure, "COOKIE_SECURE", resolve)
}

func parseIntSetting(dst *int, key string, resolve func(string) (string, bool)) error {
	val, ok := resolve(key)
	if !ok || strings.TrimSpace(val) == "" {
		return nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil {
		return fmt.Errorf("invalid %s %q: %w", key, val, err)
	}
	*dst = parsed
	return nil
}

func parseBoolSetting(dst *bool, key string, resolve func(string) (string, bool)) error {
	val, ok := resolve(key)
	if !ok || strings.TrimSpace(val) == "" {
		return nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(val))
	if err != nil {
		return fmt.Errorf("invalid %s %q: %w", key, val, err)
	}
	*dst = parsed
	return nil
}

func validateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.LibraryPath) == "" {
		return errors.New("LIBRARY_PATH cannot be empty")
	}
	if strings.TrimSpace(cfg.DBPath) == "" {
		return errors.New("DB_PATH cannot be empty")
	}
	if strings.TrimSpace(cfg.CachePath) == "" {
		return errors.New("CACHE_PATH cannot be empty")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("PORT must be between 1 and 65535, got %d", cfg.Port)
	}
	if cfg.SyncWorkers <= 0 {
		return fmt.Errorf("SYNC_WORKERS must be > 0, got %d", cfg.SyncWorkers)
	}
	if cfg.PreviewWorkers <= 0 {
		return fmt.Errorf("PREVIEW_WORKERS must be > 0, got %d", cfg.PreviewWorkers)
	}
	if cfg.LogFormat != "text" && cfg.LogFormat != "json" {
		return fmt.Errorf("LOG_FORMAT must be 'text' or 'json', got %q", cfg.LogFormat)
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
		return nil
	default:
		return fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error, got %q", cfg.LogLevel)
	}
}
