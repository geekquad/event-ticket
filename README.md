# Event Ticket Booking System

This project exposes a small booking API and a bundled frontend for a quantity-based event reservation flow:

1. `Reserve` creates a short-lived hold.
2. `Confirm` converts that hold into a booked purchase.
3. `Cancel` works for both unconfirmed reservations and confirmed bookings.

The current model is event-level inventory, not seat-level assignment.

---

## What A Reviewer Needs To Know

- PostgreSQL is the source of truth for bookings and inventory.
- Redis is not the source of truth for capacity. It is only used for temporary reservation ownership, TTL, and same-user reservation coordination.
- Overbooking is prevented by transactional PostgreSQL updates on the `events` row.
- Available seats are computed from:

```text
venue.capacity - events.booked_slots - events.reserved_slots
```

---

## Run In Under 5 Minutes

### Option 1: everything with Docker

From the repo root:

```bash
docker compose up --build
```

Then open:

- API + frontend: [http://localhost:8085](http://localhost:8085)

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

### Option 2: backend on host, DB + Redis in Docker

```bash
cp .env.example .env
docker compose up -d postgres redis
chmod +x migrations/migrate.sh
./migrations/migrate.sh
go run ./cmd/server
```

Then open:

- API + frontend: [http://localhost:8085](http://localhost:8085)
- OpenAPI spec: `specs/openapi.yaml`

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

For Upstash or any TLS-managed Redis, use a `rediss://...` URL exactly as your provider gives it.

---

## API Quick Use

First fetch a user ID and event ID:

```bash
curl http://localhost:8085/users
curl http://localhost:8085/events
```

If you have `jq`, you can export convenient variables:

```bash
USER_ID=$(curl -s http://localhost:8085/users | jq -r '.users[0].id')
EVENT_ID=$(curl -s http://localhost:8085/events | jq -r '.events[0].id')
```

### 1. List events

```bash
curl http://localhost:8085/events
```

Example response:

```json
{
  "events": [
    {
      "id": "9a2d5f0d-6a2c-4e65-9f73-2d2324d4d5d1",
      "name": "Rock Night 2026",
      "description": "An epic one-night rock concert with pyrotechnics and special guests.",
      "dateTime": "2026-06-15T20:00:00Z",
      "availableCount": 100,
      "venue": {
        "id": "3d8b2d35-2f8c-4f4a-a5e5-4e653bff8db7",
        "name": "Madison Square Garden",
        "address": "4 Penn Plaza, New York, NY 10001",
        "capacity": 100
      },
      "createdAt": "2026-03-22T12:00:00Z"
    }
  ]
}
```

### 2. Reserve

```bash
curl -X POST http://localhost:8085/booking/reserve \
  -H "Content-Type: application/json" \
  -H "X-User-ID: $USER_ID" \
  -d "{\"eventId\":\"$EVENT_ID\",\"quantity\":2}"
```

Example success response:

```json
{
  "id": "c5f9d6f5-6f58-45cb-a7cf-ccdc369c9f0a",
  "userId": "4d8a52d4-6b12-44d1-83fd-d17c15f3bca7",
  "eventId": "9a2d5f0d-6a2c-4e65-9f73-2d2324d4d5d1",
  "quantity": 2,
  "status": "RESERVED",
  "expiresAt": "2026-03-22T12:10:00Z",
  "createdAt": "2026-03-22T12:00:00Z",
  "updatedAt": "2026-03-22T12:00:00Z"
}
```

Typical failures:

- `400 {"error":"invalid quantity"}`
- `404 {"error":"resource not found"}`
- `409 {"error":"insufficient capacity"}`
- `409 {"error":"conflict"}`

### 3. Confirm

```bash
BOOKING_ID=<booking-id-from-reserve>

curl -X POST http://localhost:8085/booking/confirm \
  -H "Content-Type: application/json" \
  -H "X-User-ID: $USER_ID" \
  -d "{\"bookingId\":\"$BOOKING_ID\",\"paymentDetails\":null}"
```

Typical success:

```json
{
  "id": "c5f9d6f5-6f58-45cb-a7cf-ccdc369c9f0a",
  "userId": "4d8a52d4-6b12-44d1-83fd-d17c15f3bca7",
  "eventId": "9a2d5f0d-6a2c-4e65-9f73-2d2324d4d5d1",
  "quantity": 2,
  "status": "CONFIRMED",
  "createdAt": "2026-03-22T12:00:00Z",
  "updatedAt": "2026-03-22T12:01:00Z"
}
```

Typical failure:

- `409 {"error":"ticket unavailable"}` when the reservation lock has expired or no longer matches

### 4. Cancel

```bash
curl -X DELETE http://localhost:8085/booking/$BOOKING_ID \
  -H "X-User-ID: $USER_ID"
```

Success:

```text
204 No Content
```

### 5. List my bookings

```bash
curl http://localhost:8085/booking/mine \
  -H "X-User-ID: $USER_ID"
```

---

## Concurrency Explanation

Overbooking is prevented by PostgreSQL, not by Redis. Reserve, confirm, cancel, and expiry cleanup all update the same `events` inventory counters inside transactions. The reserve path only increments `reserved_slots` when `booked_slots + reserved_slots + requested_quantity <= venue.capacity`. Redis is used only for temporary reservation ownership, TTL, and same-user coordination for one event; it does not decide final capacity correctness.

---

## Testing Concurrency

Pick a small-capacity event from `/events` and try more seats than the venue allows.

Example shell loop:

```bash
EVENT_ID=<small-capacity-event-id>
USER_1=<user-1-id>
USER_2=<user-2-id>
USER_3=<user-3-id>

for USER_ID in "$USER_1" "$USER_2" "$USER_3"; do
  curl -s -X POST http://localhost:8085/booking/reserve \
    -H "Content-Type: application/json" \
    -H "X-User-ID: $USER_ID" \
    -d "{\"eventId\":\"$EVENT_ID\",\"quantity\":5}" &
done
wait
```

Expected outcome:

- only reservations up to real capacity succeed
- the rest fail with `409`
- successful rows move inventory on `events`
- failures are recorded in `audit_logs`

You can inspect audit output with:

```sql
SELECT action, outcome, quantity, metadata, created_at
FROM audit_logs
ORDER BY created_at DESC;
```

---

## Edge Cases

- Concurrent reserve requests for the same hot event: PostgreSQL capacity updates keep inventory correct.
- Same user retries reserve for the same event before expiry: Redis key and DB active-reservation check reject duplicates.
- Reservation expires before confirm: Redis key disappears and confirm returns `409 ticket unavailable`.
- Double confirm on the same booking: only one confirm can move a `RESERVED` row to `CONFIRMED`.
- Cancel after confirm: booking becomes `CANCELLED` and `booked_slots` is released back to inventory.
- Cancel after reserve: booking becomes `CANCELLED`, `reserved_slots` is decremented, and Redis lock is released.

---

## Notes

- `.env.example` is only a template. Seeded user IDs are DB-generated, so query `/users` instead of hard-coding values from docs.
- The frontend is served from `cmd/server/frontend` in local dev and copied into `/frontend` inside the Docker image.
- Full HTTP contract: `specs/openapi.yaml`

---

## Further Reading

- `design.md` — why the current design works, trade-offs, and how it can evolve
- `ARCHITECTURE.md` — repository/module layout and current runtime architecture
- `reservelogic.md` — detailed reserve, confirm, cancel, expiry, and scaling walkthrough
