// Package config loads and validates the process configuration from the
// environment. Every value the service needs is declared here, so a missing
// or malformed setting fails at boot rather than on the first request.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
	EnvTest        = "test"
)

// Config is the fully resolved configuration for one process.
type Config struct {
	// Env is one of development, production or test.
	Env string
	// Port is the TCP port the HTTP server listens on. Render sets PORT.
	Port int
	// DatabaseURL is a PostgreSQL connection string.
	DatabaseURL string
	// AutoMigrate applies pending migrations at startup when true.
	AutoMigrate bool
	// JWTSecret signs access tokens. At least 32 bytes in production.
	JWTSecret string
	// BcryptCost is the bcrypt work factor for password hashes (4 to 31).
	BcryptCost int
	// JWTTTL is the access-token lifetime.
	JWTTTL time.Duration
	// CORSOrigins lists allowed browser origins. "*" allows any origin.
	CORSOrigins []string
	// RateLimitRPS and RateLimitBurst configure the per-client limiter.
	// RateLimitRPS <= 0 disables rate limiting.
	RateLimitRPS   float64
	RateLimitBurst int
	// MaxBodyBytes caps request bodies.
	MaxBodyBytes int64
	// LogLevel and LogFormat configure slog. LogFormat is "json" or "text".
	LogLevel  slog.Level
	LogFormat string
	// SwaggerEnabled serves the OpenAPI UI at /swagger/.
	SwaggerEnabled bool
	// TrustedProxies are CIDRs whose X-Forwarded-For headers are trusted.
	TrustedProxies []string
}

// Load reads the environment and returns a validated Config.
func Load() (Config, error) {
	cfg := Config{
		Env:            getString("ENV", EnvDevelopment),
		Port:           getInt("PORT", 8080),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		AutoMigrate:    getBool("AUTO_MIGRATE", false),
		JWTSecret:      os.Getenv("JWT_SECRET"),
		BcryptCost:     getInt("BCRYPT_COST", 12),
		JWTTTL:         getDuration("JWT_TTL", 24*time.Hour),
		CORSOrigins:    getList("CORS_ORIGINS", []string{"*"}),
		RateLimitRPS:   getFloat("RATE_LIMIT_RPS", 10),
		RateLimitBurst: getInt("RATE_LIMIT_BURST", 20),
		MaxBodyBytes:   int64(getInt("MAX_BODY_BYTES", 1<<20)),
		LogFormat:      getString("LOG_FORMAT", ""),
		SwaggerEnabled: getBool("SWAGGER_ENABLED", true),
		TrustedProxies: getList("TRUSTED_PROXIES", nil),
	}

	switch cfg.Env {
	case EnvDevelopment, EnvProduction, EnvTest:
	default:
		return cfg, fmt.Errorf("ENV must be development, production or test, got %q", cfg.Env)
	}

	if cfg.LogFormat == "" {
		cfg.LogFormat = "text"
		if cfg.Env == EnvProduction {
			cfg.LogFormat = "json"
		}
	}
	if cfg.LogFormat != "json" && cfg.LogFormat != "text" {
		return cfg, fmt.Errorf("LOG_FORMAT must be json or text, got %q", cfg.LogFormat)
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(getString("LOG_LEVEL", "info"))); err != nil {
		return cfg, fmt.Errorf("LOG_LEVEL: %w", err)
	}
	cfg.LogLevel = level

	var problems []error
	if cfg.DatabaseURL == "" {
		problems = append(problems, errors.New("DATABASE_URL is required"))
	}
	if cfg.JWTSecret == "" {
		problems = append(problems, errors.New("JWT_SECRET is required"))
	} else if cfg.Env == EnvProduction && len(cfg.JWTSecret) < 32 {
		problems = append(problems, errors.New("JWT_SECRET must be at least 32 characters in production"))
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		problems = append(problems, fmt.Errorf("PORT must be between 1 and 65535, got %d", cfg.Port))
	}
	if cfg.BcryptCost < 4 || cfg.BcryptCost > 31 {
		problems = append(problems, fmt.Errorf("BCRYPT_COST must be between 4 and 31, got %d", cfg.BcryptCost))
	}
	if cfg.JWTTTL <= 0 {
		problems = append(problems, errors.New("JWT_TTL must be a positive duration"))
	}
	if cfg.RateLimitBurst < 1 {
		problems = append(problems, errors.New("RATE_LIMIT_BURST must be at least 1"))
	}
	if cfg.MaxBodyBytes < 1024 {
		problems = append(problems, errors.New("MAX_BODY_BYTES must be at least 1024"))
	}

	return cfg, errors.Join(problems...)
}

// IsProduction reports whether the process runs with ENV=production.
func (c Config) IsProduction() bool { return c.Env == EnvProduction }

func getString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getFloat(key string, fallback float64) float64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getBool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func getList(key string, fallback []string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback
	}
	parts := strings.Split(v, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
