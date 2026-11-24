package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucastheo/photos/internal/config"
)

func TestDefaultConfigAndListenAddr(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	if cfg.LibraryPath != "./photos" || cfg.DBPath != "./data/photos.db" || cfg.CachePath != "./data/cache" {
		t.Fatalf("unexpected default paths: %+v", cfg)
	}
	if cfg.Port != 8080 || cfg.LogFormat != "text" || cfg.LogLevel != "info" {
		t.Fatalf("unexpected default server/log config: %+v", cfg)
	}
	if cfg.SyncWorkers < 1 || cfg.PreviewWorkers < 1 {
		t.Fatalf("expected positive worker counts, got sync=%d preview=%d", cfg.SyncWorkers, cfg.PreviewWorkers)
	}
	if got := cfg.ListenAddr(); got != ":8080" {
		t.Fatalf("ListenAddr() = %q, want :8080", got)
	}
	cfg.Host = "127.0.0.1"
	cfg.Port = 9090
	if got := cfg.ListenAddr(); got != "127.0.0.1:9090" {
		t.Fatalf("ListenAddr() = %q, want 127.0.0.1:9090", got)
	}
}

func TestParseDotEnv(t *testing.T) {
	t.Parallel()

	t.Run("valid entries comments and quotes", func(t *testing.T) {
		t.Parallel()
		input := `
# Comment line
LIBRARY_PATH=/mnt/photos # inline comment
export PORT="9001"
AUTH_PASSWORD='s#cr!t'
QUOTED_ESC="line1\nline2\\end"
`
		got, err := config.ParseDotEnv(strings.NewReader(input))
		if err != nil {
			t.Fatalf("ParseDotEnv failed: %v", err)
		}
		if got["LIBRARY_PATH"] != "/mnt/photos" || got["PORT"] != "9001" ||
			got["AUTH_PASSWORD"] != "s#cr!t" || got["QUOTED_ESC"] != "line1\nline2\\end" {
			t.Fatalf("unexpected parsed map: %+v", got)
		}
	})

	t.Run("invalid syntax returns line error", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name  string
			input string
		}{
			{name: "missing equals", input: "PORT\n"},
			{name: "empty key", input: "=value\n"},
			{name: "whitespace in key", input: "MY KEY=value\n"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				if _, err := config.ParseDotEnv(strings.NewReader(tc.input)); err == nil {
					t.Fatal("expected ParseDotEnv error")
				}
			})
		}
	})
}

func TestLoadFromFileAndEnv(t *testing.T) {
	t.Parallel()

	t.Run("missing dotenv file falls back to defaults and env", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{
			"PORT":          "8181",
			"AUTH_DISABLED": "true",
			"LOG_FORMAT":    "JSON",
			"LOG_LEVEL":     "WARN",
		}
		cfg, err := config.LoadFromFileAndEnv(filepath.Join(t.TempDir(), "missing.env"), mapLookup(env))
		if err != nil {
			t.Fatalf("LoadFromFileAndEnv failed: %v", err)
		}
		if cfg.Port != 8181 || !cfg.AuthDisabled || cfg.LogFormat != "json" || cfg.LogLevel != "warn" {
			t.Fatalf("unexpected config: %+v", cfg)
		}
	})

	t.Run("env overrides dotenv values", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		envFile := filepath.Join(dir, ".env")
		content := "LIBRARY_PATH=/from/file\nDB_PATH=/from/file.db\nCACHE_PATH=/from/cache\nPORT=7000\nSYNC_WORKERS=2\nPREVIEW_WORKERS=1\nCOOKIE_SECURE=true\nAUTH_SECRET=abc\nAUTH_PASSWORD=pw\nAUTH_PASSWORD_HASH=hash\n"
		if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
			t.Fatalf("write .env: %v", err)
		}
		env := map[string]string{
			"PORT":      "8088",
			"LOG_LEVEL": "debug",
		}
		cfg, err := config.LoadFromFileAndEnv(envFile, mapLookup(env))
		if err != nil {
			t.Fatalf("LoadFromFileAndEnv failed: %v", err)
		}
		if cfg.LibraryPath != "/from/file" || cfg.Port != 8088 || cfg.SyncWorkers != 2 ||
			cfg.PreviewWorkers != 1 || !cfg.CookieSecure || cfg.AuthSecret != "abc" ||
			cfg.AuthPassword != "pw" || cfg.AuthPasswordHash != "hash" || cfg.LogLevel != "debug" {
			t.Fatalf("unexpected merged config: %+v", cfg)
		}
	})

	t.Run("validation and parse errors", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name string
			env  map[string]string
		}{
			{name: "empty library path", env: map[string]string{"LIBRARY_PATH": " "}},
			{name: "empty db path", env: map[string]string{"DB_PATH": ""}},
			{name: "empty cache path", env: map[string]string{"CACHE_PATH": ""}},
			{name: "non numeric port", env: map[string]string{"PORT": "abc"}},
			{name: "port out of range low", env: map[string]string{"PORT": "0"}},
			{name: "port out of range high", env: map[string]string{"PORT": "70000"}},
			{name: "invalid sync workers", env: map[string]string{"SYNC_WORKERS": "bad"}},
			{name: "non positive sync workers", env: map[string]string{"SYNC_WORKERS": "0"}},
			{name: "invalid preview workers", env: map[string]string{"PREVIEW_WORKERS": "bad"}},
			{name: "non positive preview workers", env: map[string]string{"PREVIEW_WORKERS": "0"}},
			{name: "invalid auth disabled", env: map[string]string{"AUTH_DISABLED": "maybe"}},
			{name: "invalid cookie secure", env: map[string]string{"COOKIE_SECURE": "nope"}},
			{name: "invalid log format", env: map[string]string{"LOG_FORMAT": "xml"}},
			{name: "invalid log level", env: map[string]string{"LOG_LEVEL": "trace"}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				if _, err := config.LoadFromFileAndEnv("", mapLookup(tc.env)); err == nil {
					t.Fatalf("expected error for %s", tc.name)
				}
			})
		}
	})

	t.Run("dotenv read and parse errors", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		badFile := filepath.Join(dir, "bad.env")
		if err := os.WriteFile(badFile, []byte("INVALID_LINE\n"), 0o600); err != nil {
			t.Fatalf("write bad.env: %v", err)
		}
		if _, err := config.LoadFromFileAndEnv(badFile, nil); err == nil {
			t.Fatal("expected parse error")
		}
		if _, err := config.LoadFromFileAndEnv(dir, nil); err == nil {
			t.Fatal("expected directory read error")
		}
	})

	t.Run("repository .env.example parses and validates", func(t *testing.T) {
		t.Parallel()
		examplePath := filepath.Join("..", "..", ".env.example")
		cfg, err := config.LoadFromFileAndEnv(examplePath, nil)
		if err != nil {
			t.Fatalf("LoadFromFileAndEnv(%q) failed: %v", examplePath, err)
		}
		if cfg.LibraryPath != "./examples/photos" || cfg.DBPath != "./data/photos.db" ||
			cfg.CachePath != "./data/cache" || cfg.Host != "127.0.0.1" ||
			cfg.Port != 8080 || cfg.AuthDisabled || cfg.AuthPassword != "change-me" ||
			cfg.CookieSecure || cfg.LogFormat != "text" || cfg.LogLevel != "info" {
			t.Fatalf("unexpected .env.example config: %+v", cfg)
		}
	})

	t.Run("Load wrapper uses defaults when no env set", func(t *testing.T) {
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("Load() failed: %v", err)
		}
		if cfg.Port <= 0 {
			t.Fatalf("expected valid port, got %d", cfg.Port)
		}
	})
}

func mapLookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}
