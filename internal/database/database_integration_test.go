//go:build integration

package database

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// These tests run against a real PostgreSQL given by TEST_DATABASE_URL.
// They reset the schema with the embedded migrations before running.
//
//	TEST_DATABASE_URL=postgres://app:app@localhost:5432/go_event_api_test?sslmode=disable \
//	go test -tags integration ./internal/database/...

func openTestDB(t *testing.T) *Stores {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, _, err := Migrate(db, "down"); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	version, dirty, err := Migrate(db, "up")
	if err != nil || dirty || version != 3 {
		t.Fatalf("migrate up: version=%d dirty=%v err=%v", version, dirty, err)
	}
	stores := NewStores(db)
	return &stores
}

func TestIntegration_UsersEventsAttendees(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	owner := &User{Email: "owner@example.com", Name: "Owner", Password: "hash"}
	if err := s.Users.Insert(ctx, owner); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	if owner.ID == 0 || owner.CreatedAt.IsZero() {
		t.Fatalf("insert did not fill id/createdAt: %+v", owner)
	}
	if err := s.Users.Insert(ctx, &User{Email: "owner@example.com", Name: "Dup", Password: "x"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate email: got %v, want ErrDuplicate", err)
	}
	guest := &User{Email: "guest@example.com", Name: "Guest", Password: "hash"}
	if err := s.Users.Insert(ctx, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Users.Get(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user: got %v", err)
	}
	if u, err := s.Users.GetByEmail(ctx, "guest@example.com"); err != nil || u.ID != guest.ID {
		t.Fatalf("get by email: %+v %v", u, err)
	}

	// Events: insert, list with filters and paging, update, delete.
	when := time.Date(2026, 11, 5, 18, 0, 0, 0, time.UTC)
	first := &Event{OwnerID: owner.ID, Name: "Lagos Backend Meetup", Description: "Talks on APIs.", Date: when, Location: "Yaba, Lagos"}
	second := &Event{OwnerID: guest.ID, Name: "Abuja DevFest", Description: "A festival.", Date: when.Add(48 * time.Hour), Location: "Abuja"}
	for _, e := range []*Event{first, second} {
		if err := s.Events.Insert(ctx, e); err != nil {
			t.Fatalf("insert event: %v", err)
		}
	}
	if err := s.Events.Insert(ctx, &Event{OwnerID: 9999, Name: "Orphan", Description: "x", Date: when, Location: "x"}); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("orphan event: got %v, want ErrInvalidReference", err)
	}

	events, total, err := s.Events.List(ctx, EventFilter{Page: 1, Limit: 1})
	if err != nil || total != 2 || len(events) != 1 || events[0].ID != first.ID {
		t.Fatalf("list page 1: %+v total=%d err=%v", events, total, err)
	}
	events, total, err = s.Events.List(ctx, EventFilter{Page: 1, Limit: 10, Query: "abuja"})
	if err != nil || total != 1 || events[0].ID != second.ID {
		t.Fatalf("list by query: %+v total=%d err=%v", events, total, err)
	}
	from := when.Add(time.Hour)
	events, total, err = s.Events.List(ctx, EventFilter{Page: 1, Limit: 10, From: &from})
	if err != nil || total != 1 || events[0].ID != second.ID {
		t.Fatalf("list from: %+v total=%d err=%v", events, total, err)
	}
	ownerID := owner.ID
	_, total, err = s.Events.List(ctx, EventFilter{Page: 1, Limit: 10, OwnerID: &ownerID})
	if err != nil || total != 1 {
		t.Fatalf("list by owner: total=%d err=%v", total, err)
	}

	first.Name = "Renamed"
	if err := s.Events.Update(ctx, first); err != nil || first.OwnerID != owner.ID {
		t.Fatalf("update: %v owner=%d", err, first.OwnerID)
	}
	if err := s.Events.Update(ctx, &Event{ID: 9999, Name: "x", Description: "x", Date: when, Location: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: got %v", err)
	}

	// Attendance with uniqueness, missing references and cascade on delete.
	if _, err := s.Attendees.Add(ctx, first.ID, guest.ID); err != nil {
		t.Fatalf("add attendee: %v", err)
	}
	if _, err := s.Attendees.Add(ctx, first.ID, guest.ID); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate attendee: got %v", err)
	}
	if _, err := s.Attendees.Add(ctx, first.ID, 9999); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("attendee with missing user: got %v", err)
	}
	users, err := s.Attendees.ListUsersForEvent(ctx, first.ID)
	if err != nil || len(users) != 1 || users[0].ID != guest.ID || users[0].Password != "" {
		t.Fatalf("attendees: %+v err=%v", users, err)
	}
	attending, err := s.Attendees.ListEventsForUser(ctx, guest.ID)
	if err != nil || len(attending) != 1 || attending[0].Name != "Renamed" {
		t.Fatalf("events for user: %+v err=%v", attending, err)
	}
	if err := s.Attendees.Remove(ctx, first.ID, owner.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove non-attendee: got %v", err)
	}

	if err := s.Events.Delete(ctx, first.ID); err != nil {
		t.Fatalf("delete event: %v", err)
	}
	if err := s.Events.Delete(ctx, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: got %v", err)
	}
	attending, err = s.Attendees.ListEventsForUser(ctx, guest.ID)
	if err != nil || len(attending) != 0 {
		t.Fatalf("attendance should cascade on event delete: %+v err=%v", attending, err)
	}
}
