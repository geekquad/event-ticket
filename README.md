# Event Ticket Booking System

A backend + frontend for booking spots at events with strong concurrency guarantees.

---

## Assumptions

- **User identity**: no authentication. Pass `X-User-ID: <uuid>` on every booking request. The users table contains seeded users whose IDs you can use.
- **One booking = one spot**: a user books one spot per request. To book multiple spots, call the endpoint multiple times.
- **Performers & venues**: pre-seeded. Events are created via SQL seed; there is no admin API to create events.
- **Payment**: out of scope.

---

## Architecture

```
cmd/server/          HTTP handlers + router + main (package main)
internal/
  entities/          Domain models: Event, Booking, AuditLog, User
  ports/             Interfaces (EventRepository, BookingService, …)
  application/       Use-case services (booking_service, event_service, user_service)
  adapters/
    postgres/        Repository implementations
  config/            Env-var config
migrations/          SQL files applied in order by migrate.sh
frontend/            Single-file vanilla HTML/JS UI
```

### Concurrency & double-booking prevention

Bookings are prevented from exceeding capacity using a **PostgreSQL conditional UPDATE** inside a transaction:

```sql
UPDATE events
SET booked_count = booked_count + 1
WHERE id = $1 AND booked_count < capacity
```

If `0` rows are affected the event is at capacity and the booking is rejected. PostgreSQL's row-level locking on `UPDATE` serializes concurrent requests on the same event row — no application-level locks or Redis required.

Two `CHECK` constraints act as a hard database-level safety net:
- `booked_count >= 0`
- `booked_count <= capacity`

**Cancel** atomically decrements `booked_count` in the same transaction as the status update, so cancelled spots are immediately available again.

### Audit log

Every booking-changing operation writes an entry to `audit_logs` with:
- `action` — `BOOKING_CREATED` or `BOOKING_CANCELLED`
- `outcome` — `SUCCESS` or `FAILURE`
- `user_id`, `entity_id`, `metadata` (reason on failure), `created_at`

Failure entries are written **outside** any rolled-back transaction so they always persist.

---

## Prerequisites

- Go 1.24+
- Docker & Docker Compose
- `psql` CLI (for running migrations)

---

## Quick Start

```bash
# 1. Clone and enter the repo
cd event-ticket

# 2. Copy environment config
cp .env.example .env

# 3. Start PostgreSQL
docker compose up -d

# 4. Run migrations and seed data
chmod +x migrations/migrate.sh
./migrations/migrate.sh

# 5. Start the backend
go run ./cmd/server

# 6. Open the frontend
open frontend/index.html
# or just open the file in any browser — no build step needed
```

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8080` | HTTP server port |
| `DATABASE_URL` | — | PostgreSQL connection string |
| `GIN_MODE` | `debug` | Gin mode (`debug` / `release`) |

---

## API Reference

All booking endpoints require the `X-User-ID` header with a valid user UUID from the `users` table.

Seeded user IDs:
| User | ID |
|---|---|
| Alice | `00000000-0000-0000-0000-000000000001` |
| Bob | `00000000-0000-0000-0000-000000000002` |
| Carol | `00000000-0000-0000-0000-000000000003` |

### List events
```bash
curl http://localhost:8080/events
curl "http://localhost:8080/events?keyword=jazz&page=1&pageSize=10"
```

### Get event details
```bash
curl http://localhost:8080/events/30000000-0000-0000-0000-000000000002
```

### Book a spot
```bash
curl -X POST http://localhost:8080/bookings \
  -H "Content-Type: application/json" \
  -H "X-User-ID: 00000000-0000-0000-0000-000000000001" \
  -d '{"eventId": "30000000-0000-0000-0000-000000000002"}'
```
Returns `201 Created` with the booking, or `409 Conflict` when sold out.

### My bookings
```bash
curl http://localhost:8080/bookings/mine \
  -H "X-User-ID: 00000000-0000-0000-0000-000000000001"
```

### Cancel a booking
```bash
curl -X DELETE http://localhost:8080/bookings/<bookingId> \
  -H "X-User-ID: 00000000-0000-0000-0000-000000000001"
```
Returns `204 No Content`. The spot is immediately returned to the event.

### List users
```bash
curl http://localhost:8080/users
```

### Health check
```bash
curl http://localhost:8080/health
```

---

## Testing concurrency (sold-out scenario)

The seeded *Intimate Jazz Evening* event has **capacity 3**. Run four concurrent bookings to verify the fourth is rejected:

```bash
EVENT="30000000-0000-0000-0000-000000000002"

for i in 1 2 3 4; do
  curl -s -X POST http://localhost:8080/bookings \
    -H "Content-Type: application/json" \
    -H "X-User-ID: 00000000-0000-0000-0000-00000000000${i}" \
    -d "{\"eventId\": \"$EVENT\"}" &
done
wait
```

Then inspect the audit log:
```sql
SELECT action, outcome, metadata, created_at
FROM audit_logs
ORDER BY created_at;
```

You should see 3 `SUCCESS` rows and 1 `FAILURE` row with `"reason":"event is at capacity"`.
