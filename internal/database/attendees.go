package database

import (
	"context"
	"database/sql"
	"time"
)

// Attendee links a user to an event they are attending.
type Attendee struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"userId"`
	EventID   int64     `json:"eventId"`
	CreatedAt time.Time `json:"createdAt"`
}

// AttendeeStore persists event attendance.
type AttendeeStore interface {
	// Add registers a user for an event. Returns ErrDuplicate when they
	// already attend and ErrInvalidReference when the user or event is missing.
	Add(ctx context.Context, eventID, userID int64) (*Attendee, error)
	// Remove unregisters a user or returns ErrNotFound.
	Remove(ctx context.Context, eventID, userID int64) error
	// ListUsersForEvent returns everyone attending an event.
	ListUsersForEvent(ctx context.Context, eventID int64) ([]User, error)
	// ListEventsForUser returns every event a user attends.
	ListEventsForUser(ctx context.Context, userID int64) ([]Event, error)
}

type attendeeStore struct {
	db *sql.DB
}

func (s *attendeeStore) Add(ctx context.Context, eventID, userID int64) (*Attendee, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	a := Attendee{EventID: eventID, UserID: userID}
	const q = `INSERT INTO attendees (event_id, user_id) VALUES ($1, $2) RETURNING id, created_at`
	if err := s.db.QueryRowContext(ctx, q, eventID, userID).Scan(&a.ID, &a.CreatedAt); err != nil {
		return nil, translate(err)
	}
	a.CreatedAt = a.CreatedAt.UTC()
	return &a, nil
}

func (s *attendeeStore) Remove(ctx context.Context, eventID, userID int64) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	res, err := s.db.ExecContext(ctx, `DELETE FROM attendees WHERE event_id = $1 AND user_id = $2`, eventID, userID)
	if err != nil {
		return translate(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *attendeeStore) ListUsersForEvent(ctx context.Context, eventID int64) ([]User, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	const q = `SELECT u.id, u.email, u.name, u.created_at
	           FROM users u
	           JOIN attendees a ON a.user_id = u.id
	           WHERE a.event_id = $1
	           ORDER BY a.created_at ASC, u.id ASC`
	rows, err := s.db.QueryContext(ctx, q, eventID)
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt); err != nil {
			return nil, translate(err)
		}
		u.CreatedAt = u.CreatedAt.UTC()
		users = append(users, u)
	}
	return users, translate(rows.Err())
}

func (s *attendeeStore) ListEventsForUser(ctx context.Context, userID int64) ([]Event, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	const q = `SELECT e.id, e.owner_id, e.name, e.description, e.date, e.location, e.created_at, e.updated_at
	           FROM events e
	           JOIN attendees a ON a.event_id = e.id
	           WHERE a.user_id = $1
	           ORDER BY e.date ASC, e.id ASC`
	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, translate(err)
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, *e)
	}
	return events, translate(rows.Err())
}
