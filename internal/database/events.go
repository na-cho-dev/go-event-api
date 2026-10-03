package database

import (
	"context"
	"database/sql"
	"time"
)

// Event is something a user organises and others attend.
type Event struct {
	ID          int64     `json:"id"`
	OwnerID     int64     `json:"ownerId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Date        time.Time `json:"date"`
	Location    string    `json:"location"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// EventFilter narrows and pages an event listing.
type EventFilter struct {
	// Page is 1-based. Limit is the page size.
	Page  int
	Limit int
	// OwnerID restricts results to one organiser when set.
	OwnerID *int64
	// Query matches name or location, case-insensitively, when non-empty.
	Query string
	// From keeps events on or after this instant when set.
	From *time.Time
}

// EventStore persists events.
type EventStore interface {
	// Insert stores a new event and fills in ID and timestamps.
	Insert(ctx context.Context, event *Event) error
	// Get returns one event or ErrNotFound.
	Get(ctx context.Context, id int64) (*Event, error)
	// List returns a page of events and the total count for the filter.
	List(ctx context.Context, filter EventFilter) ([]Event, int, error)
	// Update rewrites name, description, date and location. Returns
	// ErrNotFound when the event does not exist.
	Update(ctx context.Context, event *Event) error
	// Delete removes an event or returns ErrNotFound.
	Delete(ctx context.Context, id int64) error
}

type eventStore struct {
	db *sql.DB
}

const eventColumns = "id, owner_id, name, description, date, location, created_at, updated_at"

func scanEvent(row interface{ Scan(dest ...any) error }) (*Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.OwnerID, &e.Name, &e.Description, &e.Date, &e.Location, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, translate(err)
	}
	e.Date, e.CreatedAt, e.UpdatedAt = e.Date.UTC(), e.CreatedAt.UTC(), e.UpdatedAt.UTC()
	return &e, nil
}

func (s *eventStore) Insert(ctx context.Context, event *Event) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	const q = `INSERT INTO events (owner_id, name, description, date, location)
	           VALUES ($1, $2, $3, $4, $5)
	           RETURNING id, created_at, updated_at`
	err := s.db.QueryRowContext(ctx, q, event.OwnerID, event.Name, event.Description, event.Date, event.Location).
		Scan(&event.ID, &event.CreatedAt, &event.UpdatedAt)
	event.CreatedAt, event.UpdatedAt = event.CreatedAt.UTC(), event.UpdatedAt.UTC()
	return translate(err)
}

func (s *eventStore) Get(ctx context.Context, id int64) (*Event, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	return scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM events WHERE id = $1`, id))
}

func (s *eventStore) List(ctx context.Context, f EventFilter) ([]Event, int, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	const where = `WHERE ($1::bigint IS NULL OR owner_id = $1)
	                 AND ($2::text = '' OR name ILIKE '%' || $2 || '%' OR location ILIKE '%' || $2 || '%')
	                 AND ($3::timestamptz IS NULL OR date >= $3)`

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM events `+where, f.OwnerID, f.Query, f.From).Scan(&total); err != nil {
		return nil, 0, translate(err)
	}

	offset := (f.Page - 1) * f.Limit
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+eventColumns+` FROM events `+where+` ORDER BY date ASC, id ASC LIMIT $4 OFFSET $5`,
		f.OwnerID, f.Query, f.From, f.Limit, offset)
	if err != nil {
		return nil, 0, translate(err)
	}
	defer rows.Close()

	events := make([]Event, 0, f.Limit)
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		events = append(events, *e)
	}
	return events, total, translate(rows.Err())
}

func (s *eventStore) Update(ctx context.Context, event *Event) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	const q = `UPDATE events
	           SET name = $1, description = $2, date = $3, location = $4, updated_at = now()
	           WHERE id = $5
	           RETURNING owner_id, created_at, updated_at`
	err := s.db.QueryRowContext(ctx, q, event.Name, event.Description, event.Date, event.Location, event.ID).
		Scan(&event.OwnerID, &event.CreatedAt, &event.UpdatedAt)
	event.CreatedAt, event.UpdatedAt = event.CreatedAt.UTC(), event.UpdatedAt.UTC()
	return translate(err)
}

func (s *eventStore) Delete(ctx context.Context, id int64) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE id = $1`, id)
	if err != nil {
		return translate(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
