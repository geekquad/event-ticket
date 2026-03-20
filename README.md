# Event Ticket Booking System

A backend + frontend for booking individual seats at events, using a **two-phase Reserve → Confirm** flow with Redis-distributed locking — the same model used by BookMyShow.

---

## Assumptions

- **User identity**: no authentication. Pass `X-User-ID: <uuid>` on every booking request. The `users` table contains seeded users whose IDs you can use.
- **Seat selection**: users pick specific ticket IDs (pre-generated seats). One or more per reservation.
- **Two-phase booking**: seats are held for 10 minutes (configurable) via a Redis lock while the user completes payment. The booking expires automatically if not confirmed within the TTL.
- **Performers & venues**: pre-seeded. Events are created via SQL seed; there is no admin API.
- **Payment**: out of scope — `paymentDetails` field is accepted but not processed.

---

## Architecture

```
cmd/server/          HTTP handlers + router + main (package main)
internal/
  entities/          Domain models: Event, Ticket, Booking, AuditLog, User
  ports/             Interfaces (EventRepository, BookingService, LockManager, …)
  application/       Use-case services (booking_service, event_service, user_service)
  adapters/
    postgres/        Repository implementations
    redis/           LockManager (distributed lock via SET NX EX)
  config/            Env-var config
migrations/          SQL files applied in order by migrate.sh
frontend/            Single-file vanilla HTML/JS UI
```

### Two-phase booking flow

```
Client                            Server                       Redis          DB
  │                                  │                           │             │
  │── POST /booking/reserve ────────▶│                           │             │
  │   {ticketIds: [...]}             │── SET NX EX 600 ─────────▶│ (per ticket)│
  │                                  │   (acquire lock)           │             │
  │                                  │── INSERT booking RESERVED ─────────────▶│
  │◀─ 201 {bookingId, status:RESERVED}│                           │             │
  │                                  │                           │             │
  │  [10-minute timer starts]        │                           │             │
  │                                  │                           │             │
  │── POST /booking/confirm ────────▶│                           │             │
  │   {bookingId}                    │── GET lock owner ─────────▶│             │
  │                                  │── UPDATE tickets BOOKED ──────────────▶│
  │                                  │── UPDATE booking CONFIRMED────────────▶│
  │                                  │── DEL lock ───────────────▶│             │
  │◀─ 200 {status: CONFIRMED}        │                           │             │
  │                                  │                           │             │
  │  (or, if TTL expires before confirm)                         │             │
  │── POST /booking/confirm ────────▶│                           │             │
  │                                  │── GET lock owner ─────────▶│ (nil)       │
  │◀─ 409 Conflict (lock expired)    │                           │             │
```

### Concurrency & double-booking prevention

Each ticket is protected by a Redis lock (`ticket:<id>`) acquired with `SET NX EX <ttl>`. All-or-nothing acquisition: if any ticket in a batch is already locked, all acquired locks are rolled back before returning an error.

Ticket rows stay `AVAILABLE` in the database during the reservation window — no DB status change happens until Confirm. This means:
- TTL expiry requires **zero cleanup jobs** — the ticket simply becomes lockable again.
- `availableCount` (from a SQL subquery over `status = 'AVAILABLE'`) naturally excludes confirmed-booked tickets without Redis.

### Audit log

Every booking operation writes to `audit_logs` with:
- `action` — `BOOKING_CREATED`, `BOOKING_CONFIRMED`, `BOOKING_CANCELLED`
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
# 1. Enter the repo
cd event-ticket

# 2. Copy environment config
cp .env.example .env

# 3. Start PostgreSQL + Redis
docker compose up -d

# 4. Run migrations and seed data
chmod +x migrations/migrate.sh
./migrations/migrate.sh

# 5. Start the backend
go run ./cmd/server

# 6. Open the frontend
open frontend/index.html
# No build step needed — open directly in any browser
```

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8080` | HTTP server port |
| `DATABASE_URL` | — | PostgreSQL connection string |
| `REDIS_URL` | `redis://localhost:6379` | Redis connection string |
| `RESERVATION_TTL_MINUTES` | `10` | How long a seat reservation is held before expiry |
| `GIN_MODE` | `debug` | Gin mode (`debug` / `release`) |

---

## API Reference

All booking endpoints require the `X-User-ID` header with a valid user UUID.

Seeded user IDs:
| User | ID |
|---|---|
| Alice | `00000000-0000-0000-0000-000000000001` |
| Bob | `00000000-0000-0000-0000-000000000002` |
| Carol | `00000000-0000-0000-0000-000000000003` |

---

### List events
```bash
curl http://localhost:8080/events
curl "http://localhost:8080/events?keyword=jazz&page=1&pageSize=10"
curl "http://localhost:8080/events?start=2026-01-01T00:00:00Z&end=2026-12-31T23:59:59Z"
```

Response includes `availableCount` (live seat count derived from the tickets table).

### Get event + ticket list
```bash
curl http://localhost:8080/events/30000000-0000-0000-0000-000000000001
```

The single-event response includes a `tickets` array with each seat's ID, section, row, seatNumber, price, and status.

---

### Step 1 — Reserve seats (hold for 10 min)
```bash
curl -X POST http://localhost:8080/booking/reserve \
  -H "Content-Type: application/json" \
  -H "X-User-ID: 00000000-0000-0000-0000-000000000001" \
  -d '{"ticketIds": ["<ticket-uuid-1>", "<ticket-uuid-2>"]}'
```

Returns `201 Created`:
```json
{
  "id": "<booking-uuid>",
  "userId": "00000000-0000-0000-0000-000000000001",
  "eventId": "<event-uuid>",
  "ticketIds": ["<ticket-uuid-1>", "<ticket-uuid-2>"],
  "totalPrice": 300.00,
  "status": "RESERVED",
  "createdAt": "2026-03-21T10:00:00Z",
  "updatedAt": "2026-03-21T10:00:00Z"
}
```

Returns `409 Conflict` if any ticket is already reserved or booked.

### Step 2 — Confirm purchase (within 10 min)
```bash
curl -X POST http://localhost:8080/booking/confirm \
  -H "Content-Type: application/json" \
  -H "X-User-ID: 00000000-0000-0000-0000-000000000001" \
  -d '{"bookingId": "<booking-uuid>", "paymentDetails": null}'
```

Returns `200 OK` with the updated booking (`status: "CONFIRMED"`).
Returns `409 Conflict` if the reservation TTL has expired.

### Cancel a booking
Works on both `RESERVED` and `CONFIRMED` bookings.
```bash
curl -X DELETE http://localhost:8080/booking/<booking-uuid> \
  -H "X-User-ID: 00000000-0000-0000-0000-000000000001"
```

Returns `204 No Content`. Confirmed-booking cancellation releases all ticket locks back to AVAILABLE immediately.

### My bookings
```bash
curl http://localhost:8080/booking/mine \
  -H "X-User-ID: 00000000-0000-0000-0000-000000000001"
```

Returns `RESERVED` and `CONFIRMED` bookings for the user.

### List users
```bash
curl http://localhost:8080/users
```

### Health check
```bash
curl http://localhost:8080/health
```

---

## Concurrency demo (seat contention)

The seeded *Intimate Jazz Evening* has only **5 tickets** (`FLOOR-1-1` through `FLOOR-1-5`). First, get the ticket IDs:

```bash
curl http://localhost:8080/events/30000000-0000-0000-0000-000000000002 \
  | python3 -m json.tool | grep '"id"' | head -6
```

Then fire two concurrent reserve requests for the same ticket:

```bash
TICKET="<floor-ticket-uuid>"

for i in 1 2; do
  curl -s -X POST http://localhost:8080/booking/reserve \
    -H "Content-Type: application/json" \
    -H "X-User-ID: 00000000-0000-0000-0000-00000000000${i}" \
    -d "{\"ticketIds\": [\"$TICKET\"]}" &
done
wait
```

One request returns `201`, the other returns `409 Conflict`. Inspect the audit log:

```sql
SELECT action, outcome, metadata, created_at
FROM audit_logs
ORDER BY created_at;
```

You should see one `BOOKING_CREATED / SUCCESS` and one `BOOKING_CREATED / FAILURE` row.

After confirming the first booking and then cancelling it, the ticket becomes available again and a new reservation succeeds.
