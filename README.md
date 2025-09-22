# Go Event API

A RESTful API for managing events and attendees, built with [Go](https://golang.org/), [Gin](https://github.com/gin-gonic/gin), and [SQLite](https://www.sqlite.org/).  
Includes JWT authentication, Swagger documentation, and database migrations.

---

## Features

- User registration and authentication (JWT)
- CRUD operations for events
- Attendee management for events
- SQLite database with migrations
- RESTful API with Gin
- Swagger/OpenAPI documentation (`/swagger/index.html`)
- Environment variable configuration

---

## Requirements

- Go 1.18+ (recommended)
- [SQLite3](https://www.sqlite.org/)
- [golang-migrate/migrate](https://github.com/golang-migrate/migrate) (for migrations)
- [swaggo/swag](https://github.com/swaggo/swag) (for Swagger docs)

---

## Getting Started

### 1. Clone the repository

```sh
git clone https://github.com/yourusername/go-event-api.git
cd go-event-api
```

### 2. Install dependencies

```sh
go mod tidy
```

### 3. Set up environment variables

Create a `.env` file or set environment variables as needed:

```env
PORT=3300
JWT_SECRET=your-secret-key
```

### 4. Run database migrations

```sh
go run cmd/migrate/main.go up
```

This will create the necessary tables in `data.db`.

### 5. Generate Swagger docs

```sh
go install github.com/swaggo/swag/cmd/swag@latest
swag init --dir cmd/api --parseDependency --parseInternal --parseDepth 1
```

### 6. Start the server

```sh
go run cmd/api/main.go
```

The API will be available at `http://localhost:3300`.

---

## API Documentation

Swagger UI is available at:  
[http://localhost:3300/swagger/index.html](http://localhost:3300/swagger)

---

## Project Structure

```
go-event-api/
    cmd/
        api/           # Main API server
            main.go
            routes.go
            events.go
            context.go
        migrate/       # Migration runner
            main.go
            migrations/
                000001_create_users_table.up.sql
                000002_create_events_table.up.sql
    internal/
        database/      # Database models and logic
            events.go
        env/           # Environment variable helpers
            env.go
    docs/              # Swagger docs (auto-generated)
        swagger.yaml
    .air.toml          # Air live-reload config
    go.mod
    README.md
```

---

## Main Endpoints

### Auth

- `POST /api/v1/auth/register` — Register a new user
- `POST /api/v1/auth/login` — Login and get JWT token

### Events

- `GET /api/v1/events` — List all events
- `GET /api/v1/events/:id` — Get event by ID
- `POST /api/v1/events` — Create event (auth required)
- `PUT /api/v1/events/:id` — Update event (auth required)
- `DELETE /api/v1/events/:id` — Delete event (auth required)

### Attendees

- `GET /api/v1/events/:id/attendees` — List attendees for an event
- `POST /api/v1/events/:id/attendees/:userId` — Add attendee to event (auth required)
- `DELETE /api/v1/events/:id/attendees/:userId` — Remove attendee from event (auth required)
- `GET /api/v1/attendees/:id/events` — List events for an attendee

---

## Development

### Live Reload

This project uses [Air](https://github.com/cosmtrek/air) for live reloading during development.

```sh
go install github.com/cosmtrek/air@latest
air
```

---

## Migrations

To apply migrations:

```sh
go run cmd/migrate/main.go up
```

To rollback:

```sh
go run cmd/migrate/main.go down
```

---

## License

MIT

---

## Credits

- [Gin](https://github.com/gin-gonic/gin)
- [Swaggo](https://github.com/swaggo/swag)
- [golang-migrate](https://github.com/golang-migrate/migrate)
- [SQLite](https://www.sqlite.org/)

---

## Notes

- Make sure your Go version is up to date (`go version`).
- If you encounter issues with `swag` or `air`, ensure `$HOME/go/bin` is in your `PATH`.

---
