// Package database owns the PostgreSQL connection, the embedded migrations
// and the stores that handlers talk to. Handlers depend on the Store
// interfaces, never on *sql.DB, so they can be tested without a database.
package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var (
	// ErrNotFound is returned when a row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrDuplicate is returned when a unique constraint is violated.
	ErrDuplicate = errors.New("already exists")
	// ErrInvalidReference is returned when a foreign key points at a missing row.
	ErrInvalidReference = errors.New("referenced row does not exist")
)

const queryTimeout = 5 * time.Second

// Open connects to PostgreSQL, configures the pool and verifies the
// connection with a ping.
func Open(ctx context.Context, url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

// Migrate applies the embedded migrations. direction is "up", "down" or
// "status". For "down" every migration is rolled back; for "status" the
// current version is returned without changes.
func Migrate(db *sql.DB, direction string) (version uint, dirty bool, err error) {
	src, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return 0, false, fmt.Errorf("load migrations: %w", err)
	}
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return 0, false, fmt.Errorf("migration driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return 0, false, fmt.Errorf("migrator: %w", err)
	}

	switch direction {
	case "up":
		err = m.Up()
	case "down":
		err = m.Down()
	case "status":
	default:
		return 0, false, fmt.Errorf("unknown migration direction %q (use up, down or status)", direction)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return 0, false, err
	}

	version, dirty, err = m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	return version, dirty, err
}

// Stores groups the repositories handlers depend on.
type Stores struct {
	Users     UserStore
	Events    EventStore
	Attendees AttendeeStore
}

// NewStores wires PostgreSQL-backed stores around one connection pool.
func NewStores(db *sql.DB) Stores {
	return Stores{
		Users:     &userStore{db: db},
		Events:    &eventStore{db: db},
		Attendees: &attendeeStore{db: db},
	}
}

// translate maps driver errors onto the package sentinel errors.
func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrDuplicate
		case "23503": // foreign_key_violation
			return ErrInvalidReference
		}
	}
	return err
}

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, queryTimeout)
}
