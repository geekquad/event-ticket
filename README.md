# Event Ticket Booking System

This project exposes a small booking API and a bundled frontend for a quantity-based event reservation flow:

1. `Reserve` creates a short-lived hold.
2. `Confirm` converts that hold into a booked purchase.
3. `Cancel` works for both unconfirmed reservations and confirmed bookings.

The current model is event-level inventory, not seat-level assignment.

---

## How to run locally?

From the repo root:

```bash
docker compose up --build
```

Then open:

- API + frontend: [http://localhost:8085](http://localhost:8085)
- OpenAPI: [specs/openapi.yaml](specs/openapi.yaml)

This starts:

- app on `8085`
- Postgres on `5433`
- Redis on `6379`

Important notes:

- `001_init.sql` and `002_seed.sql` run automatically on the first boot of a fresh Postgres volume.
- If you change schema and want a clean DB, reset the volume:

```bash
docker compose down -v
docker compose up --build
```

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8085` | HTTP server port |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5433/ticketbooking?sslmode=disable` | PostgreSQL DSN |
| `REDIS_URL` | `redis://localhost:6379` | Redis URL; use `rediss://` when the server requires TLS |
| `RESERVATION_TTL_MINUTES` | `10` | Reservation lock / hold duration |
| `RESERVATION_MAX_SEATS` | `100` | Upper bound per reserve request |
| `GIN_MODE` | `debug` | Gin mode |
| `FRONTEND_DIR` | auto | Optional override for the static frontend directory |

---

## Notes

- `.env.example` is only a template. Seeded user IDs are DB-generated, so query `/users` instead of hard-coding values from docs.
- The frontend is served from `cmd/server/frontend` in local dev and copied into `/frontend` inside the Docker image.
- Full HTTP contract: [specs/openapi.yaml](specs/openapi.yaml)

---

## Further Reading

- [design.md](design.md) — assumptions, trade-offs, edge cases, scaling, concurrency, cancel/return, and audit logging
- [ARCHITECTURE.md](ARCHITECTURE.md) — packages, routes, schema, ports, transactions, Redis/Lua snippets, HTTP errors
- [reservelogic.md](reservelogic.md) — detailed reserve, confirm, cancel, expiry, and scaling walkthrough
