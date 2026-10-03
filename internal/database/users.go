package database

import (
	"context"
	"database/sql"
	"time"
)

// User is an account that can own events and attend them.
type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Password  string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
}

// UserStore persists users.
type UserStore interface {
	// Insert stores a new user and fills in ID and CreatedAt.
	// Returns ErrDuplicate when the email is taken.
	Insert(ctx context.Context, user *User) error
	// Get returns the user with the given id or ErrNotFound.
	Get(ctx context.Context, id int64) (*User, error)
	// GetByEmail returns the user with the given email or ErrNotFound.
	GetByEmail(ctx context.Context, email string) (*User, error)
}

type userStore struct {
	db *sql.DB
}

const userColumns = "id, email, name, password, created_at"

func scanUser(row interface{ Scan(dest ...any) error }) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &u.Password, &u.CreatedAt); err != nil {
		return nil, translate(err)
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return &u, nil
}

func (s *userStore) Insert(ctx context.Context, user *User) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	const q = `INSERT INTO users (email, name, password)
	           VALUES ($1, $2, $3)
	           RETURNING id, created_at`
	err := s.db.QueryRowContext(ctx, q, user.Email, user.Name, user.Password).
		Scan(&user.ID, &user.CreatedAt)
	user.CreatedAt = user.CreatedAt.UTC()
	return translate(err)
}

func (s *userStore) Get(ctx context.Context, id int64) (*User, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}

func (s *userStore) GetByEmail(ctx context.Context, email string) (*User, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, email))
}
