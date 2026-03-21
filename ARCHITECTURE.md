# Architecture

## Table of Contents

1. [High-Level Design](#1-high-level-design)
2. [Directory Structure](#2-directory-structure)
3. [Data Model](#3-data-model)
4. [Entities](#4-entities)
5. [Ports and Services](#5-ports-and-services)
6. [PostgreSQL Responsibilities](#6-postgresql-responsibilities)
7. [Redis Responsibilities](#7-redis-responsibilities)
8. [Booking Flow](#8-booking-flow)
9. [Concurrency and Consistency](#9-concurrency-and-consistency)
10. [HTTP and Frontend](#10-http-and-frontend)
11. [Transactions and Audit Isolation](#11-transactions-and-audit-isolation)
12. [Key Assumptions and Trade-offs](#12-key-assumptions-and-trade-offs)

---

## 1. High-Level Design

The codebase follows a simple ports-and-adapters structure:

- `cmd/server` exposes the HTTP API with Gin.
- `internal/service` contains the business logic.
- `internal/ports` defines the interfaces the service layer depends on.
- `internal/adapters/postgres` and `internal/adapters/redis` implement those interfaces.

The key architectural decision is this:

> Reservations are modeled at the booking level.

That means:

- the system reserves a **quantity** for an event,
- the durable record lives in `bookings`,
- the temporary hold lives in Redis,
- and there is no seat-selection flow in the application layer.

Booking state is handled through `bookings` and Redis reservation locks.

```
┌──────────────────────────────────────────────────────────────┐
│                         HTTP Layer                           │
│                 cmd/server (Gin handlers)                    │
└─────────────────────────────┬────────────────────────────────┘
                              │
┌─────────────────────────────▼────────────────────────────────┐
│                        Service Layer                         │
│        booking_service / event_service / user_service        │
└───────────────┬──────────────────────┬───────────────────────┘
                │                      │
                │                      │
        ┌───────▼────────┐      ┌──────▼─────────┐
        │   PostgreSQL   │      │     Redis      │
        │ repos + tx     │      │ reservation    │
        │                │      │ lock manager   │
        └────────────────┘      └────────────────┘
```

---

## 2. Directory Structure

```
event-ticket/
├── cmd/
│   └── server/
│       ├── main.go
│       ├── router.go
│       ├── middleware.go
│       ├── response.go
│       ├── booking_handler.go
│       ├── event_handler.go
│       └── user_handler.go
│
├── internal/
│   ├── adapters/
│   │   ├── postgres/
│   │   │   ├── db.go
│   │   │   ├── tx.go
│   │   │   ├── event_repo.go
│   │   │   ├── booking_repo.go
│   │   │   ├── audit_repo.go
│   │   │   └── user_repo.go
│   │   └── redis/
│   │       ├── client.go
│   │       └── lock_manager.go
│   │
│   ├── config/
│   │   └── config.go
│   │
│   ├── entities/
│   │   ├── audit.go
│   │   ├── booking.go
│   │   ├── event.go
│   │   ├── errors.go
│   │   └── user.go
│   │
│   ├── ports/
│   │   ├── cache.go
│   │   ├── repository.go
│   │   └── service.go
│   │
│   └── service/
│       ├── booking_service.go
│       ├── event_service.go
│       └── user_service.go
│
├── migrations/
│   ├── 001_init.sql
│   └── 002_seed.sql
│
└── frontend/
    ├── index.html
    ├── styles.css
    └── app.js
```

---

## 3. Data Model

### `users`

Stores the available users for the demo flow. Authentication is simplified; callers pass the user ID in the `X-User-ID` header.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | primary key, DB-generated |
| name | VARCHAR | display name |
| email | VARCHAR | unique |
| created_at | TIMESTAMPTZ | creation timestamp |

### `venues`

Venue metadata plus optional `seat_map`.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | primary key |
| name | VARCHAR | |
| address | TEXT | |
| capacity | INT | venue-level capacity |
| seat_map | JSONB | optional |
| created_at | TIMESTAMPTZ | |

### `events`

Capacity for reservation checks comes from the event's venue.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | primary key |
| name | VARCHAR | |
| description | TEXT | |
| date_time | TIMESTAMPTZ | event time |
| venue_id | UUID | FK → `venues` |
| created_at | TIMESTAMPTZ | |

Index:

- `idx_events_date_time`

### `bookings`

This is now the main business table.

A booking represents:

- one user,
- one event,
- one quantity,
- one lifecycle: `RESERVED` → `CONFIRMED` or `CANCELLED`.

`quantity` stores the number of seats covered by the booking.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | primary key, DB-generated |
| user_id | UUID | FK → `users` |
| event_id | UUID | FK → `events` |
| status | VARCHAR | `RESERVED`, `CONFIRMED`, `CANCELLED` |
| expires_at | TIMESTAMPTZ | reservation expiry time |
| created_at | TIMESTAMPTZ | |
| updated_at | TIMESTAMPTZ | |
| quantity | INT | seat count for the booking |

Indexes:

- `idx_bookings_user_id`
- `idx_bookings_event_id`
- `idx_bookings_status`

### `audit_logs`

Audit rows are append-only and intentionally written outside the booking transaction.

`quantity` stores the number of seats associated with the audited action when applicable.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | primary key, DB-generated |
| entity_type | VARCHAR | always `"booking"` today |
| entity_id | VARCHAR | booking ID, or empty string for pre-create failures |
| action | VARCHAR | `BOOKING_CREATED`, `BOOKING_CONFIRMED`, `BOOKING_CANCELLED` |
| user_id | VARCHAR | acting user |
| outcome | VARCHAR | `SUCCESS` or `FAILURE` |
| quantity | INT | nullable |
| metadata | JSONB | contextual details like `eventId`, `bookingId`, reason |
| created_at | TIMESTAMPTZ | |

Indexes:

- `idx_audit_logs_entity`
- `idx_audit_logs_user_id`
- `idx_audit_logs_created_at`

---

## 4. Entities

Entities live in `internal/entities` and are simple transport types shared across layers.

### `Event`

```go
type Event struct {
    ID              string
    Name            string
    Description     string
    DateTime        time.Time
    Venue           Venue
    AvailableCount  int
    CreatedAt       time.Time
}
```

Capacity comes from `Venue.Capacity`. `AvailableCount` is computed in SQL as:

```sql
v.capacity - COALESCE(SUM(b.quantity), 0)
```

where the sum only includes:

- `CONFIRMED` bookings
- `RESERVED` bookings whose `expires_at > NOW()`

### `Booking`

```go
type Booking struct {
    ID         string
    UserID     string
    EventID    string
    Quantity   int
    Status     BookingStatus
    ExpiresAt  *time.Time
    CreatedAt  time.Time
    UpdatedAt  time.Time
}
```

Important detail:

- booking IDs are generated by PostgreSQL on insert (`RETURNING id`)
- not by the service layer

### `AuditLog`

```go
type AuditLog struct {
    ID         string
    EntityType string
    EntityID   string
    Action     AuditAction
    UserID     string
    Outcome    AuditOutcome
    Quantity   *int
    Metadata   json.RawMessage
    CreatedAt  time.Time
}
```

Although `AuditLog` has an `ID` field, the repository insert does not send it. PostgreSQL generates the primary key.

### `User`, `Venue`

These remain straightforward data holders with no special behavior.

### Sentinel Errors

```go
var (
    ErrNotFound
    ErrTicketUnavailable
    ErrUnauthorized
    ErrConflict
)
```

`ErrTicketUnavailable` is the public error returned when there is not enough capacity or when a reservation lock is missing or expired.

---

## 5. Ports and Services

### Repository ports

#### `EventRepository`

```text
List(ctx)
LockEventCapacity(ctx, eventID)
```

Purpose:

- event reads,
- row-level locking of the event during reserve.

#### `BookingRepository`

```text
Create(ctx, booking)
GetByID(ctx, id)
GetByUserID(ctx, userID)
UpdateStatus(ctx, bookingID, status)
ConfirmReservation(ctx, bookingID)
CancelExpiredReservations(ctx)
SumAllocatedQuantityForEvent(ctx, eventID)
HasActiveReservedBookingForUserEvent(ctx, userID, eventID)
```

These methods support the booking workflow:

- `Create` inserts without sending `id` and scans the DB-generated ID back into the struct.
- `ConfirmReservation` updates only when the row is still `RESERVED`.
- `SumAllocatedQuantityForEvent` is the core availability query.
- `HasActiveReservedBookingForUserEvent` prevents multiple active reservations by the same user for the same event.

#### `AuditRepository`

```text
Log(ctx, entry)
```

The audit repo writes directly with `*sql.DB`, not with transaction-aware `exec(ctx)`.

#### `UserRepository`

```text
List(ctx)
GetByID(ctx, id)
```

### Other ports

#### `LockManager`

```text
Acquire(ctx, key, value, ttl)
Release(ctx, key, value)
GetOwner(ctx, key)
```

#### `Transactor`

```text
WithTransaction(ctx, fn)
```

### Services

#### `BookingService`

This service owns the full reservation lifecycle:

- reserve capacity,
- create booking rows,
- coordinate Redis locks,
- confirm,
- cancel,
- write audit rows.

It depends on:

- `BookingRepository`
- `EventRepository`
- `AuditRepository`
- `LockManager`
- `Transactor`

#### `EventService`

`EventService` is thin:

- `ListEvents` delegates to `eventRepo.List`

#### `UserService`

Used only to populate the frontend user selector.

---

## 6. PostgreSQL Responsibilities

PostgreSQL is the durable system of record for:

- users
- venues
- events
- bookings
- audit logs

Booking semantics in PostgreSQL:

- `bookings` holds the reserved quantity
- `bookings.status` holds the lifecycle state
- `bookings.expires_at` defines whether a reservation is still active
- capacity checks are based on booking quantities

### Available count query

`List` in `event_repo.go` computes availability like this:

```sql
v.capacity - COALESCE((
    SELECT SUM(b.quantity)
    FROM bookings b
    WHERE b.event_id = e.id
      AND (
        b.status = 'CONFIRMED'
        OR (b.status = 'RESERVED' AND b.expires_at > NOW())
      )
), 0)
```

(where `v` is the venue joined to the event)

This is the definition of “available”.

## 7. Redis Responsibilities

Redis stores the temporary reservation lock only.

### Key strategy

The key format is:

```text
reservation:{eventID}:{userID}
```

This preserves the required `event_id:user_id` combination in the key.

### Value strategy

The value format is:

```text
userID|quantity|bookingID
```

This lets the service verify that the lock still belongs to:

- the same user,
- the same quantity,
- the same booking.

### Operations

| Operation | Redis primitive | Used in |
|----------|------------------|---------|
| Acquire | `SET key value NX EX ttl` | reserve |
| Release | Lua guarded delete | confirm / cancel |
| GetOwner | `GET key` | confirm |

The guarded release script ensures a caller only deletes its own lock.

### What Redis does not do

Redis does not store:

- the durable reservation record,
- final booking status,
- venue capacity,
- audit history.

If the lock expires, Redis silently drops the key. The durable cleanup of the matching booking row happens lazily on later requests via `CancelExpiredReservations()`.

---

## 8. Booking Flow

### Reserve

`BookingService.Reserve(ctx, userID, eventID, quantity)` works like this:

1. Normalize `quantity` to at least `1`.
2. Run lazy cleanup of expired reservations.
3. Generate a booking ID and acquire the Redis lock on `reservation:{eventID}:{userID}`.
4. If Redis acquisition fails or the key already exists, return (no DB work).
5. Start a DB transaction. On any failure, release the Redis lock and return.
6. Inside the transaction:
   - clean up expired reservations again,
   - lock the event row with `FOR UPDATE`,
   - sum currently allocated quantity,
   - reject if remaining capacity is insufficient,
   - reject if the same user already has an active `RESERVED` booking for the event,
   - insert a new booking row with the pre-generated ID.
7. Write the audit row.

### Confirm

`BookingService.Confirm(ctx, userID, bookingID)`:

1. Load the booking with `FOR UPDATE`.
2. Validate ownership.
3. Validate status is `RESERVED`.
4. Rebuild the expected Redis key and value.
5. Verify `GetOwner()` exactly matches the expected lock value.
6. Run a transaction calling `ConfirmReservation()`:
   - `UPDATE bookings ... WHERE id = $1 AND status = 'RESERVED'`
   - if `RowsAffected() == 0`, return `ErrConflict`
7. Release the Redis lock.
8. Write the audit row with `quantity`.

### Cancel

`BookingService.Cancel(ctx, userID, bookingID)`:

1. Load the booking.
2. Validate ownership.
3. Reject already-cancelled bookings.
4. Update status to `CANCELLED` in a transaction.
5. If the booking was still `RESERVED`, release the Redis lock.
6. Write the audit row.

### Get user bookings

`BookingService.GetUserBookings(ctx, userID)`:

1. Lazy-clean expired reservations.
2. Load the user's `RESERVED` and `CONFIRMED` bookings.
3. Clear `ExpiresAt` on non-reserved rows before returning them.

---

## 9. Concurrency and Consistency

### Capacity race protection

The critical race during reserve is protected by:

1. **Redis lock first** — Acquiring the lock on `reservation:{eventID}:{userID}` before the DB transaction serializes same-user concurrent reservations for the same event. A second request from the same user fails at Redis and never touches the DB.

2. **Event row lock** — Inside the transaction, locking the event row with `FOR UPDATE` serializes capacity checks and booking inserts across all users:

```sql
SELECT capacity FROM events WHERE id = $1 FOR UPDATE
```

### Confirm race protection

The critical race during confirm is protected by:

- loading the booking row with `FOR UPDATE`,
- and `ConfirmReservation()` updating only rows still in `RESERVED`.

That means two concurrent confirm requests cannot both succeed.

### Redis lock purpose

The Redis lock is not a global event mutex. It protects the user’s active reservation for that event and acts as the expiration mechanism for the temporary hold.

### Lazy cleanup consistency model

Expired `RESERVED` rows are not removed by a worker. They are transitioned to `CANCELLED` lazily when calls like reserve or “my bookings” happen.

This means:

- Redis expiry frees the temporary hold immediately,
- PostgreSQL may temporarily still contain a stale `RESERVED` row,
- but the availability query only subtracts rows whose `expires_at > NOW()`,
- so stale expired rows do not reduce available capacity.

---

## 10. HTTP and Frontend

### Routes

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/` | serve frontend (index.html) |
| GET | `/styles.css` | frontend styles |
| GET | `/app.js` | frontend script |
| GET | `/health` | health check |
| GET | `/events` | list events |
| POST | `/booking/reserve` | reserve quantity for an event |
| POST | `/booking/confirm` | confirm a booking |
| DELETE | `/booking/:bookingId` | cancel a booking |
| GET | `/booking/mine` | list current user bookings |
| GET | `/users` | list demo users |

### Auth model

Authentication is intentionally simplified:

- no JWT
- no session
- `X-User-ID` header is trusted directly

### Error mapping

`response.go` maps service errors to:

```text
ErrNotFound          -> 404
ErrTicketUnavailable -> 409
ErrUnauthorized      -> 403
ErrConflict          -> 409
default              -> 500
```

### Frontend

The server serves the frontend at `GET /`. Static assets:

- `GET /` → `index.html`
- `GET /styles.css` → styles
- `GET /app.js` → script

Source files live in `frontend/`. At startup the server walks up from the process working directory until it finds `frontend/index.html`, so `go run .` works from the repo root or from `cmd/server`. Set `FRONTEND_DIR` to an absolute path to override.

The UI supports:

- selecting a user,
- browsing events,
- reserving a quantity,
- viewing active bookings,
- confirming or cancelling bookings,
- showing countdowns for reserved bookings.

When served from the same origin, the frontend uses `window.location.origin` for API calls.

---

## 11. Transactions and Audit Isolation

### Transaction pattern

`internal/adapters/postgres/tx.go` stores `*sql.Tx` in context so repositories can transparently use either:

- the transaction in progress, or
- the shared DB pool.

This keeps repository methods reusable both inside and outside transactions.

### Audit isolation

`auditRepo` deliberately bypasses the transaction-aware executor and always writes with the root `*sql.DB`.

That means:

- failure audit rows survive booking rollbacks,
- success and failure auditing remain independent of business transaction success,
- audit completeness is favored over strict transactional coupling.

### ID generation

Both primary-key inserts are DB-generated:

- `bookings.id` is generated by PostgreSQL and returned with `RETURNING id`
- `audit_logs.id` is generated by PostgreSQL by default

The service layer does not create those IDs ahead of time.

---

## 12. Key Assumptions and Trade-offs

### 1. Booking is the reservation unit

The system reserves quantities, not explicit seats. This simplifies the flow but means seat-level assignment is not part of the application model.

### 2. One active reservation per user per event

The service explicitly prevents a user from creating multiple simultaneous `RESERVED` bookings for the same event.

### 3. Redis is single-instance locking

The implementation assumes one Redis instance. There is no distributed multi-node lock protocol here.

### 4. No payment processing

`paymentDetails` is accepted by the confirm request shape, but it is not used in the service layer.

### 5. Availability is computed from bookings

The authoritative availability model is:

- venue capacity (from the event's venue)
- minus confirmed quantity
- minus non-expired reserved quantity
