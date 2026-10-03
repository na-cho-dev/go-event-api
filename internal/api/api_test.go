package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/na-cho-dev/go-event-api/internal/auth"
	"github.com/na-cho-dev/go-event-api/internal/database"
)

// do performs a request against the server and returns the recorder.
func do(t *testing.T, s *Server, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, want, rec.Body.String())
	}
}

func expectError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) ErrorResponse {
	t.Helper()
	expectStatus(t, rec, status)
	body := decode[ErrorResponse](t, rec)
	if body.Error.Code != code {
		t.Fatalf("error code = %q, want %q; body: %s", body.Error.Code, code, rec.Body.String())
	}
	if body.Error.RequestID == "" {
		t.Fatalf("error response lacks requestId: %s", rec.Body.String())
	}
	return body
}

// registerUser creates an account and returns its token and id.
func registerUser(t *testing.T, s *Server, email string) (string, int64) {
	t.Helper()
	rec := do(t, s, http.MethodPost, "/api/v1/auth/register",
		RegisterRequest{Email: email, Password: "a-strong-password", Name: "Test User"}, "")
	expectStatus(t, rec, http.StatusCreated)
	body := decode[AuthResponse](t, rec)
	return body.Token, body.User.ID
}

func createEvent(t *testing.T, s *Server, token string, name string) database.Event {
	t.Helper()
	rec := do(t, s, http.MethodPost, "/api/v1/events", EventRequest{
		Name:        name,
		Description: "A description long enough to pass validation.",
		Date:        time.Date(2026, 11, 5, 18, 0, 0, 0, time.UTC),
		Location:    "Yaba, Lagos",
	}, token)
	expectStatus(t, rec, http.StatusCreated)
	return decode[database.Event](t, rec)
}

func TestRegister(t *testing.T) {
	s, _ := newTestServer(t, testConfig())

	rec := do(t, s, http.MethodPost, "/api/v1/auth/register",
		RegisterRequest{Email: "Ada@Example.com", Password: "a-strong-password", Name: "Ada"}, "")
	expectStatus(t, rec, http.StatusCreated)
	body := decode[AuthResponse](t, rec)
	if body.Token == "" || body.User.ID == 0 {
		t.Fatalf("expected token and user id, got %+v", body)
	}
	if body.User.Email != "ada@example.com" {
		t.Fatalf("email not normalised: %q", body.User.Email)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("password leaked in response: %s", rec.Body.String())
	}

	// Same email, different case: conflict.
	rec = do(t, s, http.MethodPost, "/api/v1/auth/register",
		RegisterRequest{Email: "ADA@example.com", Password: "a-strong-password", Name: "Ada"}, "")
	expectError(t, rec, http.StatusConflict, CodeConflict)

	// Validation failures name the json fields.
	rec = do(t, s, http.MethodPost, "/api/v1/auth/register", map[string]any{"email": "nope", "password": "short"}, "")
	body2 := expectError(t, rec, http.StatusBadRequest, CodeValidation)
	fields := map[string]string{}
	for _, d := range body2.Error.Details {
		fields[d.Field] = d.Message
	}
	for _, want := range []string{"email", "password", "name"} {
		if _, ok := fields[want]; !ok {
			t.Fatalf("missing validation detail for %q: %+v", want, body2.Error.Details)
		}
	}

	// Garbage body.
	rec = do(t, s, http.MethodPost, "/api/v1/auth/register", "{not json", "")
	expectError(t, rec, http.StatusBadRequest, CodeInvalidJSON)
}

func TestLogin(t *testing.T) {
	s, _ := newTestServer(t, testConfig())
	registerUser(t, s, "ada@example.com")

	rec := do(t, s, http.MethodPost, "/api/v1/auth/login", LoginRequest{Email: "ADA@example.com", Password: "a-strong-password"}, "")
	expectStatus(t, rec, http.StatusOK)
	body := decode[AuthResponse](t, rec)
	if body.Token == "" {
		t.Fatal("expected a token")
	}

	rec = do(t, s, http.MethodPost, "/api/v1/auth/login", LoginRequest{Email: "ada@example.com", Password: "wrong-password"}, "")
	expectError(t, rec, http.StatusUnauthorized, CodeInvalidCredentials)

	rec = do(t, s, http.MethodPost, "/api/v1/auth/login", LoginRequest{Email: "nobody@example.com", Password: "a-strong-password"}, "")
	expectError(t, rec, http.StatusUnauthorized, CodeInvalidCredentials)

	// /auth/me works with the issued token.
	rec = do(t, s, http.MethodGet, "/api/v1/auth/me", nil, body.Token)
	expectStatus(t, rec, http.StatusOK)
	me := decode[database.User](t, rec)
	if me.Email != "ada@example.com" {
		t.Fatalf("me returned %+v", me)
	}
}

func TestAuthMiddleware(t *testing.T) {
	cfg := testConfig()
	s, f := newTestServer(t, cfg)
	token, id := registerUser(t, s, "ada@example.com")

	cases := []struct {
		name   string
		header string
		code   string
	}{
		{"missing", "", CodeUnauthorized},
		{"not bearer", "Basic abc", CodeUnauthorized},
		{"empty bearer", "Bearer ", CodeUnauthorized},
		{"garbage", "Bearer not.a.token", CodeUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			expectError(t, rec, http.StatusUnauthorized, tc.code)
		})
	}

	t.Run("expired", func(t *testing.T) {
		expired, _, err := auth.NewTokenIssuer(cfg.JWTSecret, -time.Minute).Issue(id)
		if err != nil {
			t.Fatal(err)
		}
		rec := do(t, s, http.MethodGet, "/api/v1/auth/me", nil, expired)
		expectError(t, rec, http.StatusUnauthorized, CodeTokenExpired)
	})

	t.Run("wrong secret", func(t *testing.T) {
		forged, _, err := auth.NewTokenIssuer("another-secret-that-is-also-long-enough", time.Hour).Issue(id)
		if err != nil {
			t.Fatal(err)
		}
		rec := do(t, s, http.MethodGet, "/api/v1/auth/me", nil, forged)
		expectError(t, rec, http.StatusUnauthorized, CodeUnauthorized)
	})

	t.Run("deleted user", func(t *testing.T) {
		f.users.delete(id)
		rec := do(t, s, http.MethodGet, "/api/v1/auth/me", nil, token)
		expectError(t, rec, http.StatusUnauthorized, CodeUnauthorized)
	})
}

func TestEventsLifecycle(t *testing.T) {
	s, _ := newTestServer(t, testConfig())
	ownerToken, ownerID := registerUser(t, s, "owner@example.com")
	otherToken, _ := registerUser(t, s, "other@example.com")

	// Unauthenticated create is rejected.
	rec := do(t, s, http.MethodPost, "/api/v1/events", EventRequest{}, "")
	expectError(t, rec, http.StatusUnauthorized, CodeUnauthorized)

	// Bad date gives a field-level error.
	rec = do(t, s, http.MethodPost, "/api/v1/events",
		map[string]any{"name": "Meetup", "description": "A description long enough.", "date": "2026-11-05", "location": "Lagos"}, ownerToken)
	body := expectError(t, rec, http.StatusBadRequest, CodeValidation)
	if len(body.Error.Details) == 0 || body.Error.Details[0].Field != "date" {
		t.Fatalf("expected a date detail, got %+v", body.Error.Details)
	}

	event := createEvent(t, s, ownerToken, "Lagos Backend Meetup")
	if event.OwnerID != ownerID || event.ID == 0 {
		t.Fatalf("unexpected event %+v", event)
	}
	createEvent(t, s, otherToken, "Abuja DevFest")

	// Listing is paged and filterable.
	rec = do(t, s, http.MethodGet, "/api/v1/events?limit=1&page=2", nil, "")
	expectStatus(t, rec, http.StatusOK)
	list := decode[ListResponse[database.Event]](t, rec)
	if list.Meta.Total != 2 || list.Meta.TotalPages != 2 || len(list.Data) != 1 || list.Meta.Page != 2 {
		t.Fatalf("unexpected paging %+v", list.Meta)
	}
	rec = do(t, s, http.MethodGet, "/api/v1/events?q=abuja", nil, "")
	list = decode[ListResponse[database.Event]](t, rec)
	if list.Meta.Total != 1 || list.Data[0].Name != "Abuja DevFest" {
		t.Fatalf("query filter failed: %+v", list)
	}
	rec = do(t, s, http.MethodGet, "/api/v1/events?limit=1000", nil, "")
	expectError(t, rec, http.StatusBadRequest, CodeValidation)

	// Get, including bad ids.
	rec = do(t, s, http.MethodGet, "/api/v1/events/abc", nil, "")
	expectError(t, rec, http.StatusBadRequest, CodeInvalidID)
	rec = do(t, s, http.MethodGet, "/api/v1/events/999", nil, "")
	expectError(t, rec, http.StatusNotFound, CodeNotFound)

	// Only the owner may update or delete.
	update := EventRequest{Name: "Renamed Meetup", Description: "A description long enough to pass.", Date: event.Date, Location: "Ikeja, Lagos"}
	rec = do(t, s, http.MethodPut, "/api/v1/events/1", update, otherToken)
	expectError(t, rec, http.StatusForbidden, CodeForbidden)
	rec = do(t, s, http.MethodPut, "/api/v1/events/1", update, ownerToken)
	expectStatus(t, rec, http.StatusOK)
	updated := decode[database.Event](t, rec)
	if updated.Name != "Renamed Meetup" || updated.OwnerID != ownerID {
		t.Fatalf("update lost fields: %+v", updated)
	}

	rec = do(t, s, http.MethodDelete, "/api/v1/events/1", nil, otherToken)
	expectError(t, rec, http.StatusForbidden, CodeForbidden)
	rec = do(t, s, http.MethodDelete, "/api/v1/events/1", nil, ownerToken)
	expectStatus(t, rec, http.StatusNoContent)
	rec = do(t, s, http.MethodGet, "/api/v1/events/1", nil, "")
	expectError(t, rec, http.StatusNotFound, CodeNotFound)
}

func TestAttendees(t *testing.T) {
	s, _ := newTestServer(t, testConfig())
	ownerToken, ownerID := registerUser(t, s, "owner@example.com")
	guestToken, guestID := registerUser(t, s, "guest@example.com")
	event := createEvent(t, s, ownerToken, "Lagos Backend Meetup")
	base := "/api/v1/events/1"

	// A guest joins, cannot join twice, shows up in the list, then leaves.
	rec := do(t, s, http.MethodPost, base+"/attendees", nil, guestToken)
	expectStatus(t, rec, http.StatusCreated)
	rec = do(t, s, http.MethodPost, base+"/attendees", nil, guestToken)
	expectError(t, rec, http.StatusConflict, CodeConflict)

	rec = do(t, s, http.MethodGet, base+"/attendees", nil, "")
	expectStatus(t, rec, http.StatusOK)
	attendees := decode[CollectionResponse[database.User]](t, rec)
	if len(attendees.Data) != 1 || attendees.Data[0].ID != guestID {
		t.Fatalf("unexpected attendees %+v", attendees.Data)
	}

	rec = do(t, s, http.MethodGet, "/api/v1/attendees/2/events", nil, "")
	expectStatus(t, rec, http.StatusOK)
	events := decode[CollectionResponse[database.Event]](t, rec)
	if len(events.Data) != 1 || events.Data[0].ID != event.ID {
		t.Fatalf("unexpected events for attendee %+v", events.Data)
	}

	rec = do(t, s, http.MethodDelete, base+"/attendees", nil, guestToken)
	expectStatus(t, rec, http.StatusNoContent)
	rec = do(t, s, http.MethodDelete, base+"/attendees", nil, guestToken)
	expectError(t, rec, http.StatusNotFound, CodeNotFound)

	// Owner-managed attendance.
	rec = do(t, s, http.MethodPost, base+"/attendees/2", nil, guestToken)
	expectError(t, rec, http.StatusForbidden, CodeForbidden)
	rec = do(t, s, http.MethodPost, base+"/attendees/999", nil, ownerToken)
	expectError(t, rec, http.StatusNotFound, CodeNotFound)
	rec = do(t, s, http.MethodPost, base+"/attendees/2", nil, ownerToken)
	expectStatus(t, rec, http.StatusCreated)
	rec = do(t, s, http.MethodDelete, base+"/attendees/2", nil, ownerToken)
	expectStatus(t, rec, http.StatusNoContent)

	// Empty lists serialise as [] rather than null.
	rec = do(t, s, http.MethodGet, base+"/attendees", nil, "")
	if !strings.Contains(rec.Body.String(), `"data":[]`) {
		t.Fatalf("expected an empty array, got %s", rec.Body.String())
	}
	_ = ownerID

	rec = do(t, s, http.MethodGet, "/api/v1/attendees/999/events", nil, "")
	expectError(t, rec, http.StatusNotFound, CodeNotFound)
}

type failingPinger struct{}

func (failingPinger) PingContext(context.Context) error { return errors.New("connection refused") }

func TestHealthAndInfo(t *testing.T) {
	s, _ := newTestServer(t, testConfig())
	rec := do(t, s, http.MethodGet, "/health", nil, "")
	expectStatus(t, rec, http.StatusOK)
	health := decode[HealthResponse](t, rec)
	if health.Status != "ok" || health.Version != "test" {
		t.Fatalf("unexpected health %+v", health)
	}

	rec = do(t, s, http.MethodGet, "/", nil, "")
	expectStatus(t, rec, http.StatusOK)

	s.db = failingPinger{}
	rec = do(t, s, http.MethodGet, "/health", nil, "")
	expectStatus(t, rec, http.StatusServiceUnavailable)
	health = decode[HealthResponse](t, rec)
	if health.Database != "unreachable" {
		t.Fatalf("expected unreachable database, got %+v", health)
	}
}

func TestRateLimit(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 2
	s, _ := newTestServer(t, cfg)

	for i := 0; i < 2; i++ {
		rec := do(t, s, http.MethodGet, "/", nil, "")
		expectStatus(t, rec, http.StatusOK)
	}
	rec := do(t, s, http.MethodGet, "/", nil, "")
	expectError(t, rec, http.StatusTooManyRequests, CodeRateLimited)
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected a Retry-After header")
	}
}

func TestCORSAndHeaders(t *testing.T) {
	s, _ := newTestServer(t, testConfig())

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/events", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	expectStatus(t, rec, http.StatusNoContent)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("Allow-Origin = %q", got)
	}

	req = httptest.NewRequest(http.MethodOptions, "/api/v1/events", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	expectStatus(t, rec, http.StatusForbidden)

	rec = do(t, s, http.MethodGet, "/api/v1/events", nil, "")
	expectStatus(t, rec, http.StatusOK)
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Cache-Control":          "no-store",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected X-Request-ID on every response")
	}
}

func TestUnknownRoutes(t *testing.T) {
	s, _ := newTestServer(t, testConfig())
	rec := do(t, s, http.MethodGet, "/api/v1/nothing", nil, "")
	expectError(t, rec, http.StatusNotFound, CodeNotFound)
	rec = do(t, s, http.MethodPatch, "/api/v1/events", nil, "")
	expectStatus(t, rec, http.StatusMethodNotAllowed)
}

func TestBodyLimit(t *testing.T) {
	cfg := testConfig()
	cfg.MaxBodyBytes = 1024
	s, _ := newTestServer(t, cfg)
	huge := strings.Repeat("x", 2048)
	rec := do(t, s, http.MethodPost, "/api/v1/auth/register", map[string]string{"email": "a@b.co", "password": huge, "name": "x"}, "")
	expectError(t, rec, http.StatusRequestEntityTooLarge, CodePayloadTooLarge)
}
