# Architecture

This document describes the current code structure and the runtime architecture as it exists today.

---

## High-Level Shape

The project follows a lightweight ports-and-adapters style:

- `cmd/server` exposes the HTTP API and serves the static frontend.
- `internal/service` contains booking, event, and user use cases.
- `internal/ports` defines interfaces used by the service layer.
- `internal/infra/postgres` implements repositories and transaction handling.
- `internal/infra/redis` implements reservation lock storage.

The important architectural decision is:

> Inventory is tracked on the `events` row with counters, while Redis only tracks temporary reservation ownership.

---

## Directory Structure

```text
event-ticket/
├── cmd/
│   └── server/
│       ├── container.go
│       ├── frontend/
│       │   ├── app.js
│       │   ├── index.html
│       │   ├── paths.go
│       │   └── styles.css
│       ├── handler/
│       │   ├── booking_handler.go
│       │   ├── event_handler.go
│       │   ├── response.go
│       │   ├── router.go
│       │   └── user_handler.go
│       └── main.go
├── internal/
│   ├── config/
│   ├── entities/
│   ├── infra/
│   │   ├── postgres/
│   │   └── redis/
│   ├── middleware/
│   ├── ports/
│   └── service/
├── migrations/
├── specs/
├── Dockerfile
└── docker-compose.yml
```

---

## Runtime Components

### HTTP layer

Gin exposes these routes:

- `GET /`
- `GET /styles.css`
- `GET /app.js`
- `GET /health`
- `GET /events`
- `GET /users`
- `POST /booking/reserve`
- `POST /booking/confirm`
- `DELETE /booking/:bookingId`
- `GET /booking/mine`

`cmd/server/frontend.ResolveDir()` locates static frontend files either from:

- `FRONTEND_DIR`, or
- the repo tree (`cmd/server/frontend`)

### Service layer

Services are thin for reads and fuller for bookings:

- `eventService` delegates to `EventRepository.List`
- `userService` delegates to `UserRepository.List`
- `bookingService` owns reserve, confirm, cancel, cleanup coordination, and audit writing

### PostgreSQL layer

PostgreSQL stores:

- users
- venues
- events
- bookings
- audit logs

It also enforces inventory changes through atomic updates on `events`.

### Redis layer

Redis stores only the short-lived reservation lock:

- key: `reservation:<eventID>:<userID>`
- value: `<quantity>|<bookingID>`

---

## Data Model

### `users`

Simplified demo identities. The caller passes one of these IDs in `X-User-ID`.

### `venues`

Static venue metadata plus `capacity`. Capacity is authoritative here.

### `events`

`events` is now the main inventory row.

Columns of interest:

- `venue_id`
- `booked_slots`
- `reserved_slots`
- `date_time`

Important constraints / indexes:

- `CHECK (booked_slots >= 0)`
- `CHECK (reserved_slots >= 0)`
- `idx_events_date_time`
- `idx_events_venue_id`
- `idx_events_venue_id_date_time` unique index

### `bookings`

Each booking has:

- `user_id`
- `event_id`
- `quantity`
- `status`
- `expires_at`
- `created_at`
- `updated_at`

Statuses:

- `RESERVED`
- `CONFIRMED`
- `CANCELLED`

### `audit_logs`

Append-only rows with:

- `entity_type`
- `entity_id`
- `action`
- `user_id`
- `outcome`
- `quantity`
- `metadata`
- `created_at`

`metadata` is `JSON`, not `JSONB`.

---

## Inventory Model

The system no longer computes capacity by summing bookings on every request.

Current model:

```text
available = venues.capacity - events.booked_slots - events.reserved_slots
```

Meaning:

- `booked_slots` = confirmed purchased seats
- `reserved_slots` = seats currently held but not yet confirmed

Event listing uses:

```sql
SELECT e.id, e.name, e.description, e.date_time,
       GREATEST(v.capacity - e.booked_slots - e.reserved_slots, 0),
       ...
FROM events e
INNER JOIN venues v ON v.id = e.venue_id
WHERE e.date_time > NOW()
ORDER BY e.date_time ASC
```

---

## Redis Responsibilities

Redis is intentionally small in scope.

### Acquire

Reserve uses:

```text
SET key value NX EX ttl
```

via `SetNX`.

### Release

Confirm and cancel use a guarded Lua delete:

```lua
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0
end
```

This avoids deleting a lock unless the expected value still matches.

### Read owner

Confirm uses `GET` through `GetOwner()` and compares the exact value.

### What Redis does not own

Redis does not store:

- final booking state
- capacity
- durable booking records
- audit history

---

## Repository Ports

### `EventRepository`

- `List`
- `TryAddReservedSlots`
- `TransferReservedToBooked`
- `ReleaseReservedSlots`
- `ReleaseBookedSlots`

### `BookingRepository`

- `Create`
- `GetByID`
- `GetByUserID`
- `UpdateStatus`
- `ConfirmReservation`
- `CancelExpiredReservations`
- `CancelReservationIfExpired`
- `HasActiveReservedBookingForUserEvent`

### Other ports

- `AuditRepository.Log`
- `UserRepository.List`
- `Transactor.WithTransaction`
- `LockManager.Acquire / Release / GetOwner`

---

## Transaction Model

`internal/infra/postgres/tx.go` stores `*sql.Tx` in the request context. Repository methods call `execFromContext()` and transparently use either:

- the transaction, or
- the shared DB pool

This keeps repository methods reusable without creating duplicate `Tx` and non-`Tx` versions.

---

## Booking Workflows

### Reserve

Reserve combines Redis and PostgreSQL:

1. Validate `quantity` (`<= 0` becomes `1`; too large returns `ErrInvalidQuantity`).
2. Generate a booking UUID in the service.
3. Acquire Redis lock for `(eventID, userID)`.
4. Start DB transaction.
5. Run `CancelExpiredReservations()`.
6. Run `TryAddReservedSlots()`.
7. Reject if same user already has an active reservation for the same event.
8. Insert booking row as `RESERVED`.
9. Commit.
10. Keep Redis lock alive.
11. Write success audit.
12. Start in-process timer cleanup.

### Confirm

1. Load booking with `FOR UPDATE`.
2. Validate owner and state.
3. Check Redis owner value.
4. In one transaction:
   - `ConfirmReservation()`
   - `TransferReservedToBooked()`
5. Release Redis lock.
6. Write audit.

### Cancel

1. Load booking.
2. Validate owner and state.
3. In one transaction:
   - mark booking `CANCELLED`
   - release `reserved_slots` if it was reserved
   - release `booked_slots` if it was confirmed
4. If it was reserved, release Redis key.
5. Write audit.

### Expiry cleanup

There are two cleanup paths:

- batch lazy cleanup through `CancelExpiredReservations()`
- single-booking timer cleanup through `scheduleReservationUnlock()`

Both ensure expired reservations release `reserved_slots`.

---

## Consistency Guarantees

### Same user, same event

Redis prevents concurrent reserve attempts for the same `(eventID, userID)` pair.

### Cross-user capacity

The DB protects capacity with conditional atomic updates on `events`.

Reserve succeeds only if:

```sql
booked_slots + reserved_slots + quantity <= venues.capacity
```

### Confirm correctness

Confirm requires:

- the booking row still be `RESERVED`
- the Redis key still exist
- the Redis value still match the expected `quantity|bookingID`

### Counter reconciliation

Expired cleanup uses:

```sql
SET reserved_slots = GREATEST(e.reserved_slots - a.sub, 0)
```

This is intentionally self-healing if counters drift low.

---

## Audit Isolation

`auditRepo` writes with the root DB handle, not the transaction-bound executor.

Why:

- failure audits should survive rollback
- business flow and operational logging are intentionally decoupled

Trade-off:

- audit rows are not part of the same atomic transaction as the business update

---

## Error Mapping

Current HTTP error mapping:

- `ErrNotFound` -> `404`
- `ErrInvalidQuantity` -> `400`
- `ErrInsufficientCapacity` -> `409`
- `ErrTicketUnavailable` -> `409`
- `ErrUnauthorized` -> `403`
- `ErrConflict` -> `409`
- fallback -> `500`

---

## Static Frontend

The frontend lives in `cmd/server/frontend`.

- local dev resolves that directory from the repo tree
- Docker copies only the static assets into `/frontend`

The UI uses the same origin when served from the API server and interacts with:

- `/events`
- `/users`
- `/booking/reserve`
- `/booking/confirm`
- `/booking/mine`
- `/booking/:bookingId`

---

## Key Trade-offs

### What is strong

- simple architecture
- clear ownership split between Postgres and Redis
- efficient event listing
- correct inventory transitions under normal concurrency

### What is weak

- hot events still serialize on the same `events` row
- expiry cleanup partly depends on in-process timers
- no seat-level assignment
- cancellation and return are represented by the same terminal state

For the more narrative explanation of runtime behavior, see `design.md` and `reservelogic.md`.
