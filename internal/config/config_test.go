package config

import (
	"strings"
	"testing"
	"time"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, key := range []string{
		"ENV", "PORT", "DATABASE_URL", "AUTO_MIGRATE", "JWT_SECRET", "BCRYPT_COST", "JWT_TTL",
		"CORS_ORIGINS", "RATE_LIMIT_RPS", "RATE_LIMIT_BURST", "MAX_BODY_BYTES", "LOG_LEVEL",
		"LOG_FORMAT", "SWAGGER_ENABLED", "TRUSTED_PROXIES",
	} {
		t.Setenv(key, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, map[string]string{
		"DATABASE_URL": "postgres://app:app@localhost:5432/db",
		"JWT_SECRET":   "short-is-fine-in-development",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Env != EnvDevelopment || cfg.Port != 8080 || cfg.JWTTTL != 24*time.Hour || cfg.LogFormat != "text" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if len(cfg.CORSOrigins) != 1 || cfg.CORSOrigins[0] != "*" {
		t.Fatalf("unexpected CORS default: %v", cfg.CORSOrigins)
	}
	if cfg.BcryptCost != 12 || cfg.RateLimitRPS != 10 || cfg.RateLimitBurst != 20 {
		t.Fatalf("unexpected numeric defaults: %+v", cfg)
	}
}

func TestLoadProductionRules(t *testing.T) {
	setEnv(t, map[string]string{
		"ENV":          "production",
		"DATABASE_URL": "postgres://app:app@localhost:5432/db",
		"JWT_SECRET":   "too-short",
	})
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET must be at least 32") {
		t.Fatalf("expected a short-secret error, got %v", err)
	}

	setEnv(t, map[string]string{
		"ENV":          "production",
		"DATABASE_URL": "postgres://app:app@localhost:5432/db",
		"JWT_SECRET":   strings.Repeat("x", 40),
		"CORS_ORIGINS": "https://a.example.com, https://b.example.com",
		"JWT_TTL":      "15m",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogFormat != "json" {
		t.Fatalf("production should default to json logs, got %q", cfg.LogFormat)
	}
	if len(cfg.CORSOrigins) != 2 || cfg.CORSOrigins[1] != "https://b.example.com" {
		t.Fatalf("CORS list not parsed: %v", cfg.CORSOrigins)
	}
	if cfg.JWTTTL != 15*time.Minute {
		t.Fatalf("JWT_TTL not parsed: %v", cfg.JWTTTL)
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	setEnv(t, map[string]string{
		"PORT":        "70000",
		"BCRYPT_COST": "2",
	})
	_, err := Load()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"DATABASE_URL is required", "JWT_SECRET is required", "PORT must be", "BCRYPT_COST must be"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should mention %q", err.Error(), want)
		}
	}
}

func TestLoadRejectsBadEnum(t *testing.T) {
	setEnv(t, map[string]string{
		"ENV":          "staging",
		"DATABASE_URL": "x",
		"JWT_SECRET":   "x",
	})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ENV must be") {
		t.Fatalf("expected an ENV error, got %v", err)
	}
}
