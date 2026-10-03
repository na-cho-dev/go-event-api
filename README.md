# Go Event API

A REST API for events and the people who attend them, written in Golang with the Gin framework on PostgreSQL.
Register, log in, create events, and manage who is going.

- JWT authentication with expiring HS256 tokens and bcrypt password hashing
- Events with ownership, paging, search and date filtering
- Attendance that users manage for themselves and owners manage for others
- Embedded, versioned migrations with a `migrate` binary and optional auto-migrate on boot
- Request IDs, structured logging, panic recovery, security headers, CORS, per-client rate limiting and body limits
- Consistent JSON error envelope with machine-readable codes and field-level validation details
- OpenAPI 3 docs at `/swagger/`, a `/health` check that pings the database, graceful shutdown
- Unit tests over HTTP with in-memory stores, integration tests against PostgreSQL, CI on GitHub Actions
- A static, non-root Docker image, `docker compose` for local development, and a Render blueprint

## Quick start

### With Docker Compose

```sh
docker compose up --build
```

The API listens on http://localhost:8080 and migrates the schema on boot. Swagger UI is at http://localhost:8080/swagger/.

### Locally

Requirements: Go 1.25+, a PostgreSQL 14+ database.

```sh
cp .env.example .env          # edit DATABASE_URL and JWT_SECRET
go run ./cmd/migrate up       # apply the schema
go run ./cmd/api              # start the server
```

`make help` lists every task: `make test`, `make test-integration`, `make lint`, `make swag`, `make docker-build`.

## Configuration

Everything is read from the environment (a `.env` file is loaded in development). Missing or invalid values stop the process at boot.

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `DATABASE_URL` | yes | | PostgreSQL connection string |
| `JWT_SECRET` | yes | | Token signing secret, 32+ characters in production |
| `ENV` | | `development` | `development`, `production` or `test` |
| `PORT` | | `8080` | Listen port (Render sets it) |
| `AUTO_MIGRATE` | | `false` | Apply pending migrations at startup |
| `JWT_TTL` | | `24h` | Access-token lifetime |
| `BCRYPT_COST` | | `12` | bcrypt work factor, 4 to 31 |
| `CORS_ORIGINS` | | `*` | Comma-separated allowed origins |
| `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` | | `10` / `20` | Per-client limiter, `0` disables |
| `MAX_BODY_BYTES` | | `1048576` | Request body cap |
| `LOG_LEVEL` / `LOG_FORMAT` | | `info` / `text` | slog level and `json` or `text` (json in production) |
| `SWAGGER_ENABLED` | | `true` | Serve Swagger UI |
| `TRUSTED_PROXIES` | | | CIDRs allowed to set `X-Forwarded-For` |

## Endpoints

All API routes live under `/api/v1`. Protected routes need `Authorization: Bearer <token>`.

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| `GET` | `/` | | Service info |
| `GET` | `/health` | | Liveness plus a database ping (503 when the database is unreachable) |
| `POST` | `/api/v1/auth/register` | | Create an account, returns a token |
| `POST` | `/api/v1/auth/login` | | Exchange credentials for a token |
| `GET` | `/api/v1/auth/me` | token | The current account |
| `GET` | `/api/v1/events` | | List events: `page`, `limit`, `q`, `owner`, `from` |
| `POST` | `/api/v1/events` | token | Create an event |
| `GET` | `/api/v1/events/{id}` | | Get one event |
| `PUT` | `/api/v1/events/{id}` | owner | Replace an event's details |
| `DELETE` | `/api/v1/events/{id}` | owner | Delete an event and its attendance |
| `GET` | `/api/v1/events/{id}/attendees` | | Who attends an event |
| `POST` | `/api/v1/events/{id}/attendees` | token | Join an event |
| `DELETE` | `/api/v1/events/{id}/attendees` | token | Leave an event |
| `POST` | `/api/v1/events/{id}/attendees/{userId}` | owner | Register another user |
| `DELETE` | `/api/v1/events/{id}/attendees/{userId}` | owner | Remove a user |
| `GET` | `/api/v1/attendees/{id}/events` | | Events a user attends |

### Examples

```sh
# Register (also returns a token)
curl -s localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"correct horse battery","name":"Ada"}'

# Create an event
curl -s localhost:8080/api/v1/events \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Lagos Backend Meetup","description":"Talks on APIs, queues and databases.","date":"2026-11-05T18:00:00Z","location":"Yaba, Lagos"}'

# Search upcoming events
curl -s 'localhost:8080/api/v1/events?q=lagos&from=2026-01-01T00:00:00Z&limit=10'
```

### Responses

Lists are paged:

```json
{ "data": [ ... ], "meta": { "page": 1, "limit": 20, "total": 42, "totalPages": 3 } }
```

Errors share one envelope. `code` is stable for clients; `details` appears on validation failures:

```json
{
  "error": {
    "code": "validation_error",
    "message": "The request body failed validation.",
    "details": [{ "field": "email", "message": "must be a valid email address" }],
    "requestId": "3f1c2a9b0d4e"
  }
}
```

Codes: `validation_error`, `invalid_json`, `invalid_id`, `unauthorized`, `token_expired`, `invalid_credentials`, `forbidden`, `not_found`, `conflict`, `rate_limited`, `payload_too_large`, `internal_error`.

## Project layout

```
cmd/api            entry point: config, logger, database, HTTP server, graceful shutdown
cmd/migrate        migrate up | down | status
internal/api       Gin server, middleware, handlers, tests over HTTP with in-memory stores
internal/auth      JWT issue and verify
internal/config    typed configuration from the environment
internal/database  PostgreSQL pool, embedded migrations, store interfaces and implementations
docs               OpenAPI spec generated by swag
```

Handlers depend on the `UserStore`, `EventStore` and `AttendeeStore` interfaces, so they are tested without a database. The PostgreSQL implementations are covered by the integration suite.

## Testing

```sh
make test               # unit tests, race detector
make test-integration   # starts a throwaway Postgres in Docker and runs the store tests
make lint               # gofmt, go vet, staticcheck
```

CI runs the same steps on every push and pull request, plus `govulncheck` and a Docker image build.

## Deploying to Render

`render.yaml` declares a Docker web service and a managed PostgreSQL database. Create a Blueprint instance from the repository, and Render will provision both, inject `DATABASE_URL`, generate `JWT_SECRET`, and run migrations on boot because `AUTO_MIGRATE=true`. The health check path is `/health`.

Two things to know: the free Postgres plan expires 30 days after creation, so upgrade it or point `DATABASE_URL` at a provider with a permanent free tier such as Neon; and free web services sleep after idle, so the first request after a pause takes a few seconds.

## Development notes

- Regenerate the OpenAPI spec after changing handler annotations: `make swag`.
- Add a migration as a pair of files in `internal/database/migrations` named `000004_<name>.up.sql` and `.down.sql`. They are embedded at build time.
- `air` (configured in `.air.toml`) gives live reload: `make dev`.

## License

MIT
