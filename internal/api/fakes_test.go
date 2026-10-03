package api

import (
	"context"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/na-cho-dev/go-event-api/internal/auth"
	"github.com/na-cho-dev/go-event-api/internal/config"
	"github.com/na-cho-dev/go-event-api/internal/database"
)

// In-memory stores that honour the same contracts as the PostgreSQL ones,
// so handlers can be exercised over HTTP without a database.

type fakeUsers struct {
	mu   sync.Mutex
	byID map[int64]*database.User
	next int64
}

func newFakeUsers() *fakeUsers { return &fakeUsers{byID: map[int64]*database.User{}} }

func (f *fakeUsers) Insert(_ context.Context, u *database.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.byID {
		if strings.EqualFold(existing.Email, u.Email) {
			return database.ErrDuplicate
		}
	}
	f.next++
	u.ID = f.next
	u.CreatedAt = time.Now().UTC()
	cp := *u
	f.byID[u.ID] = &cp
	return nil
}

func (f *fakeUsers) Get(_ context.Context, id int64) (*database.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byID[id]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeUsers) GetByEmail(_ context.Context, email string) (*database.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.byID {
		if u.Email == email {
			cp := *u
			return &cp, nil
		}
	}
	return nil, database.ErrNotFound
}

func (f *fakeUsers) delete(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byID, id)
}

type fakeEvents struct {
	mu   sync.Mutex
	byID map[int64]*database.Event
	next int64
}

func newFakeEvents() *fakeEvents { return &fakeEvents{byID: map[int64]*database.Event{}} }

func (f *fakeEvents) Insert(_ context.Context, e *database.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	e.ID = f.next
	e.CreatedAt = time.Now().UTC()
	e.UpdatedAt = e.CreatedAt
	cp := *e
	f.byID[e.ID] = &cp
	return nil
}

func (f *fakeEvents) Get(_ context.Context, id int64) (*database.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.byID[id]; ok {
		cp := *e
		return &cp, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeEvents) List(_ context.Context, filter database.EventFilter) ([]database.Event, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	matches := []database.Event{}
	for _, e := range f.byID {
		if filter.OwnerID != nil && e.OwnerID != *filter.OwnerID {
			continue
		}
		if filter.Query != "" {
			q := strings.ToLower(filter.Query)
			if !strings.Contains(strings.ToLower(e.Name), q) && !strings.Contains(strings.ToLower(e.Location), q) {
				continue
			}
		}
		if filter.From != nil && e.Date.Before(*filter.From) {
			continue
		}
		matches = append(matches, *e)
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Date.Equal(matches[j].Date) {
			return matches[i].ID < matches[j].ID
		}
		return matches[i].Date.Before(matches[j].Date)
	})
	total := len(matches)
	start := (filter.Page - 1) * filter.Limit
	if start > total {
		start = total
	}
	end := start + filter.Limit
	if end > total {
		end = total
	}
	return matches[start:end], total, nil
}

func (f *fakeEvents) Update(_ context.Context, e *database.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.byID[e.ID]
	if !ok {
		return database.ErrNotFound
	}
	existing.Name, existing.Description, existing.Date, existing.Location = e.Name, e.Description, e.Date, e.Location
	existing.UpdatedAt = time.Now().UTC()
	e.OwnerID, e.CreatedAt, e.UpdatedAt = existing.OwnerID, existing.CreatedAt, existing.UpdatedAt
	return nil
}

func (f *fakeEvents) Delete(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byID[id]; !ok {
		return database.ErrNotFound
	}
	delete(f.byID, id)
	return nil
}

type fakeAttendees struct {
	mu     sync.Mutex
	users  *fakeUsers
	events *fakeEvents
	rows   map[[2]int64]*database.Attendee
	next   int64
}

func newFakeAttendees(users *fakeUsers, events *fakeEvents) *fakeAttendees {
	return &fakeAttendees{users: users, events: events, rows: map[[2]int64]*database.Attendee{}}
}

func (f *fakeAttendees) Add(ctx context.Context, eventID, userID int64) (*database.Attendee, error) {
	if _, err := f.events.Get(ctx, eventID); err != nil {
		return nil, database.ErrInvalidReference
	}
	if _, err := f.users.Get(ctx, userID); err != nil {
		return nil, database.ErrInvalidReference
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]int64{eventID, userID}
	if _, ok := f.rows[key]; ok {
		return nil, database.ErrDuplicate
	}
	f.next++
	a := &database.Attendee{ID: f.next, EventID: eventID, UserID: userID, CreatedAt: time.Now().UTC()}
	f.rows[key] = a
	cp := *a
	return &cp, nil
}

func (f *fakeAttendees) Remove(_ context.Context, eventID, userID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]int64{eventID, userID}
	if _, ok := f.rows[key]; !ok {
		return database.ErrNotFound
	}
	delete(f.rows, key)
	return nil
}

func (f *fakeAttendees) ListUsersForEvent(ctx context.Context, eventID int64) ([]database.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	users := []database.User{}
	for key := range f.rows {
		if key[0] != eventID {
			continue
		}
		if u, err := f.users.Get(ctx, key[1]); err == nil {
			u.Password = ""
			users = append(users, *u)
		}
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID < users[j].ID })
	return users, nil
}

func (f *fakeAttendees) ListEventsForUser(ctx context.Context, userID int64) ([]database.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	events := []database.Event{}
	for key := range f.rows {
		if key[1] != userID {
			continue
		}
		if e, err := f.events.Get(ctx, key[0]); err == nil {
			events = append(events, *e)
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].ID < events[j].ID })
	return events, nil
}

type fakes struct {
	users     *fakeUsers
	events    *fakeEvents
	attendees *fakeAttendees
}

func testConfig() config.Config {
	return config.Config{
		Env:            config.EnvTest,
		Port:           8080,
		DatabaseURL:    "postgres://unused",
		JWTSecret:      "test-secret-that-is-long-enough-for-hs256!!",
		BcryptCost:     4,
		JWTTTL:         time.Hour,
		CORSOrigins:    []string{"https://app.example.com"},
		RateLimitRPS:   0,
		RateLimitBurst: 1,
		MaxBodyBytes:   1 << 20,
		LogLevel:       slog.LevelError,
		LogFormat:      "text",
		SwaggerEnabled: false,
	}
}

// newTestServer builds a Server on in-memory stores.
func newTestServer(t *testing.T, cfg config.Config) (*Server, *fakes) {
	t.Helper()
	users := newFakeUsers()
	events := newFakeEvents()
	f := &fakes{users: users, events: events, attendees: newFakeAttendees(users, events)}
	stores := database.Stores{Users: users, Events: events, Attendees: f.attendees}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := auth.NewTokenIssuer(cfg.JWTSecret, cfg.JWTTTL)
	return New(cfg, stores, tokens, log, nil, "test"), f
}
