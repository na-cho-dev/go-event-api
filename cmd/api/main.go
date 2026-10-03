// Command api runs the HTTP server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/joho/godotenv/autoload" // loads .env in local development

	_ "github.com/na-cho-dev/go-event-api/docs" // generated OpenAPI spec
	"github.com/na-cho-dev/go-event-api/internal/api"
	"github.com/na-cho-dev/go-event-api/internal/auth"
	"github.com/na-cho-dev/go-event-api/internal/config"
	"github.com/na-cho-dev/go-event-api/internal/database"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// @title			Go Event API
// @version		1.2
// @description	Events and attendees with JWT authentication. Register, log in, create events, and manage who attends them.
// @contact.name	Fortune Iheanacho
// @contact.url	https://nachodev.me
// @license.name	MIT
// @BasePath		/
// @securityDefinitions.apikey	BearerAuth
// @in							header
// @name						Authorization
// @description				Prefix the token with "Bearer ", for example: Bearer eyJhbGciOi...
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := newLogger(cfg)
	log.Info("starting", slog.String("version", version), slog.String("env", cfg.Env), slog.Int("port", cfg.Port))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	log.Info("database connected")

	if cfg.AutoMigrate {
		v, dirty, err := database.Migrate(db, "up")
		if err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		log.Info("migrations applied", slog.Uint64("version", uint64(v)), slog.Bool("dirty", dirty))
	}

	server := api.New(cfg, database.NewStores(db), auth.NewTokenIssuer(cfg.JWTSecret, cfg.JWTTTL), log, db, version)

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", httpServer.Addr))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		log.Info("stopped")
	}
	return nil
}

func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var handler slog.Handler
	if cfg.LogFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}
