# Architecture

## Table of Contents

1. [High-Level Design](#1-high-level-design)
2. [Directory Structure](#2-directory-structure)
3. [Database Schema](#3-database-schema)
4. [Entities — What Each Struct Holds](#4-entities--what-each-struct-holds)
5. [Ports — The Contract Layer](#5-ports--the-contract-layer)
6. [PostgreSQL — What It Manages](#6-postgresql--what-it-manages)
7. [Redis — What It Manages](#7-redis--what-it-manages)
8. [Application Services — Business Logic](#8-application-services--business-logic)
9. [HTTP Layer](#9-http-layer)
10. [Data Flow: Complete Booking Journey](#10-data-flow-complete-booking-journey)
11. [Concurrency Model](#11-concurrency-model)
12. [Transaction & Audit Isolation Pattern](#12-transaction--audit-isolation-pattern)
13. [Assumptions](#13-assumptions)

---

## 1. High-Level Design

This project follows **Hexagonal Architecture** (also called Ports and Adapters). The core rule is:

> Business logic has zero knowledge of infrastructure. Infrastructure depends on the core — never the other way around.

```
┌─────────────────────────────────────────────────────────────────┐
│                          HTTP Layer                             │
│              (cmd/server — Gin handlers, router)                │
└──────────────────────────┬──────────────────────────────────────┘
                           │ calls interfaces (ports)
┌──────────────────────────▼──────────────────────────────────────┐
│                     Application Layer                           │
│         (internal/application — BookingService, EventService)   │
│                   Pure business logic only                      │
└──────┬──────────────────────────────────┬───────────────────────┘
       │ ports.BookingRepository           │ ports.LockManager
       │ ports.TicketRepository            │ (Redis)
       │ ports.AuditRepository             │
       │ ports.Transactor                  │
┌──────▼──────────────────┐   ┌────────────▼──────────────────────┐
│   PostgreSQL Adapter    │   │         Redis Adapter             │
│  (internal/adapters/    │   │   (internal/adapters/redis)       │
│       postgres)         │   │   LockManager via SET NX EX       │
└─────────────────────────┘   └───────────────────────────────────┘
```

**Why this matters:** You can swap PostgreSQL for MySQL or Redis for Memcached without touching a single line of business logic. The application layer only speaks to interfaces defined in `internal/ports/`.

---

## 2. Directory Structure

```
event-ticket/
├── cmd/
│   └── server/
│       ├── main.go             — wires everything, starts HTTP server
│       ├── router.go           — route declarations, middleware registration
│       ├── booking_handler.go  — HTTP handlers for booking operations
│       ├── event_handler.go    — HTTP handlers for event queries
│       ├── user_handler.go     — HTTP handler for user listing
│       ├── middleware.go       — CORS + request logger middleware
│       └── response.go         — centralised error-to-HTTP-status mapping
│
├── internal/
│   ├── config/
│   │   └── config.go           — reads env vars, provides Config struct
│   │
│   ├── entities/               — pure domain types, no dependencies
│   │   ├── event.go
│   │   ├── ticket.go
│   │   ├── booking.go
│   │   ├── audit.go
│   │   ├── user.go
│   │   └── errors.go           — sentinel errors (ErrNotFound, etc.)
│   │
│   ├── ports/                  — interfaces only, no implementations
│   │   ├── repository.go       — data access contracts
│   │   ├── service.go          — business logic contracts
│   │   └── cache.go            — LockManager contract
│   │
│   ├── application/            — business logic, depends only on ports
│   │   ├── booking_service.go
│   │   ├── event_service.go
│   │   └── user_service.go
│   │
│   └── adapters/
│       ├── postgres/           — PostgreSQL implementations of ports
│       │   ├── db.go           — Connect()
│       │   ├── tx.go           — Transactor + executor context trick
│       │   ├── event_repo.go
│       │   ├── ticket_repo.go
│       │   ├── booking_repo.go
│       │   ├── audit_repo.go
│       │   └── user_repo.go
│       └── redis/
│           ├── client.go       — Connect()
│           └── lock_manager.go — LockManager implementation
│
├── migrations/
│   ├── 001_init.sql            — full schema
│   └── 002_seed.sql            — sample users, venues, performers, events, tickets
│
└── frontend/
    └── index.html              — single-file vanilla JS UI
```

---

## 3. Database Schema

### `users`
Stores the people who can log in and make bookings. In this implementation, auth is simplified — the user ID is passed directly as an `X-User-ID` header (no JWT, no session).

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | Primary key, auto-generated |
| name | VARCHAR | Display name |
| email | VARCHAR | Unique |
| created_at | TIMESTAMPTZ | |

### `venues`
Physical locations where events are held. Holds the building-level capacity and an optional `seat_map` (JSONB) for any future seat-layout data.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | |
| name | VARCHAR | |
| address | TEXT | |
| capacity | INT | max people the venue fits |
| seat_map | JSONB | optional layout data, nullable |
| created_at | TIMESTAMPTZ | |

### `performers`
Bands, artists, DJs. A performer is linked to many events over time.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | |
| name | VARCHAR | |
| description | TEXT | |
| created_at | TIMESTAMPTZ | |

### `events`
A specific show on a specific date. An event belongs to one venue and one performer. The `capacity` column is the total number of tickets that were generated for this event when it was created.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | |
| name | VARCHAR | |
| description | TEXT | |
| date_time | TIMESTAMPTZ | when the show happens |
| venue_id | UUID | FK → venues |
| performer_id | UUID | FK → performers |
| capacity | INT | total tickets pre-generated |
| created_at | TIMESTAMPTZ | |

Index on `date_time` for chronological listing.

### `tickets`
One row per physical seat. Generated in bulk when an event is seeded. Each ticket knows its `section`, `row`, and `seat_number`.

**Critical design decision:** A ticket has only two statuses in the DB — `AVAILABLE` and `BOOKED`. There is **no `RESERVED` status in the database**. The reservation state lives exclusively in Redis (a TTL lock). This means:
- During a reservation window, the ticket row in this table still reads `AVAILABLE`.
- Only when the user completes Confirm does the ticket flip to `BOOKED`.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | |
| event_id | UUID | FK → events |
| seat_number | VARCHAR | e.g. "3" |
| row | VARCHAR | e.g. "B" |
| section | VARCHAR | e.g. "A", "FLOOR" |
| price | NUMERIC(10,2) | price in USD |
| status | VARCHAR | `AVAILABLE` or `BOOKED` |
| booking_id | UUID | nullable; set when BOOKED |
| created_at | TIMESTAMPTZ | |

Indexes on `event_id` and `status` for fast availability queries.

### `bookings`
A booking record ties a user to a set of tickets for an event. It is created at the Reserve step and updated at Confirm or Cancel.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | |
| user_id | UUID | FK → users |
| event_id | UUID | FK → events |
| total_price | NUMERIC(10,2) | sum of ticket prices at reserve time |
| status | VARCHAR | `RESERVED`, `CONFIRMED`, or `CANCELLED` |
| created_at | TIMESTAMPTZ | used as the reservation start time |
| updated_at | TIMESTAMPTZ | updated on status change |

Indexes on `user_id`, `event_id`, and `status`.

### `booking_tickets`
A join table linking a booking to its individual tickets (many-to-many, but in practice one booking maps to a fixed set of tickets).

| Column | Type | Notes |
|--------|------|-------|
| booking_id | UUID | FK → bookings, part of PK |
| ticket_id | UUID | FK → tickets, part of PK |

### `audit_logs`
An append-only record of every significant action — both successes and failures. Never enrolled in a booking transaction (see section 12), so rollbacks never swallow audit entries.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | |
| entity_type | VARCHAR | always `"booking"` currently |
| entity_id | VARCHAR | booking ID, or "" on pre-creation failures |
| action | VARCHAR | `BOOKING_CREATED`, `BOOKING_CONFIRMED`, `BOOKING_CANCELLED` |
| user_id | VARCHAR | who triggered the action |
| outcome | VARCHAR | `SUCCESS` or `FAILURE` |
| metadata | JSONB | reason, ticketIds, eventId, etc. |
| created_at | TIMESTAMPTZ | |

Indexes on `(entity_type, entity_id)`, `user_id`, and `created_at`.

---

## 4. Entities — What Each Struct Holds

Entities live in `internal/entities/`. They are plain Go structs with no methods and no imports from anywhere inside this project. They are the common language between all layers.

### `User`
```go
type User struct {
    ID        string
    Name      string
    Email     string
    CreatedAt time.Time
}
```

### `Venue` / `Performer`
Embedded inside `Event`. Never used standalone in the application layer.

### `Event`
```go
type Event struct {
    ID             string
    Name           string
    Description    string
    DateTime       time.Time
    Venue          Venue
    Performer      Performer
    Capacity       int       // total tickets pre-generated
    AvailableCount int       // computed: AVAILABLE tickets minus actively RESERVED ones
    Tickets        []Ticket  // only populated on GetEvent (single event view)
    CreatedAt      time.Time
}
```
`AvailableCount` is not stored. It is computed in the SQL query at read time:
```sql
(COUNT of AVAILABLE tickets) - (COUNT of tickets in active RESERVED bookings)
```

### `Ticket`
```go
type Ticket struct {
    ID         string
    EventID    string
    SeatNumber string
    Row        string
    Section    string
    Price      float64
    Status     TicketStatus  // "AVAILABLE" or "BOOKED"
    BookingID  *string       // nil when AVAILABLE, set when BOOKED
    CreatedAt  time.Time
}
```

### `Booking`
```go
type Booking struct {
    ID         string
    UserID     string
    EventID    string
    TicketIDs  []string      // loaded from booking_tickets join table
    TotalPrice float64
    Status     BookingStatus // "RESERVED", "CONFIRMED", or "CANCELLED"
    ExpiresAt  *time.Time    // only set for RESERVED bookings; nil otherwise
    CreatedAt  time.Time
    UpdatedAt  time.Time
}
```
`ExpiresAt` is computed as `CreatedAt + ReservationTTL`. It is not persisted — it is computed by the service layer and attached to the response so the frontend can display an accurate countdown.

### `AuditLog`
```go
type AuditLog struct {
    ID         string
    EntityType string           // "booking"
    EntityID   string
    Action     AuditAction      // BOOKING_CREATED / _CONFIRMED / _CANCELLED
    UserID     string
    Outcome    AuditOutcome     // SUCCESS or FAILURE
    Metadata   json.RawMessage  // arbitrary JSON blob
    CreatedAt  time.Time
}
```

### Sentinel Errors (`errors.go`)
```go
var (
    ErrNotFound          // → HTTP 404
    ErrTicketUnavailable // → HTTP 409
    ErrUnauthorized      // → HTTP 403
    ErrConflict          // → HTTP 409
)
```
All errors bubble up through the service layer as these sentinels. The HTTP layer (`response.go`) maps them to status codes in one central `handleError` function. No status code logic exists in handlers.

---

## 5. Ports — The Contract Layer

Ports are Go interfaces in `internal/ports/`. They are the seam between the application layer and infrastructure. The application layer depends only on these — never on concrete types.

### `ports.EventRepository`
```
GetByID(id) → *Event
List(params) → []Event, total int
```

### `ports.TicketRepository`
```
GetByEventID(eventID) → []Ticket
GetAvailableByEventID(eventID, limit) → []Ticket  ← used in Reserve
GetByIDs(ids) → []Ticket
BulkUpdateStatus(ticketIDs, status, bookingID) → error  ← used in Confirm/Cancel
```

### `ports.BookingRepository`
```
Create(booking) → error
GetByID(id) → *Booking          ← uses FOR UPDATE to lock row in Confirm/Cancel
GetByUserID(userID) → []Booking
UpdateStatus(bookingID, status) → error
CancelExpiredReservations(before time.Time) → error  ← lazy cleanup
```

### `ports.AuditRepository`
```
Log(entry) → error
```

### `ports.UserRepository`
```
List() → []User
GetByID(id) → *User
```

### `ports.Transactor`
```
WithTransaction(ctx, fn(txCtx) error) → error
```
Begins a transaction, injects it into the context, runs `fn`, commits or rolls back. Repos automatically detect the transaction by reading the context (see section 12).

### `ports.LockManager`
```
Acquire(key, value, ttl) → (bool, error)   ← SET NX EX in Redis
Release(key, value) → error                 ← atomic Lua: GET+DEL only if owner
GetOwner(key) → (string, error)             ← GET in Redis
```

---

## 6. PostgreSQL — What It Manages

PostgreSQL is the **system of record**. Everything durable lives here.

**What PostgreSQL owns:**
- All domain data (users, venues, performers, events, tickets, bookings)
- Booking status (`RESERVED` → `CONFIRMED` → `CANCELLED`)
- Ticket status (`AVAILABLE` → `BOOKED`)
- Audit trail

**What PostgreSQL does NOT own:**
- The reservation hold itself. A `RESERVED` booking record exists, but the ticket rows stay `AVAILABLE` in Postgres. The actual hold on a seat is the Redis TTL lock.

**Available count computation:**
Every time `GET /events` or `GET /events/:id` is called, the query computes `available_count` live:
```sql
(SELECT COUNT(*) FROM tickets t WHERE t.event_id = e.id AND t.status = 'AVAILABLE')
- (SELECT COUNT(bt.ticket_id)
   FROM booking_tickets bt
   JOIN bookings b ON b.id = bt.booking_id
   WHERE b.event_id = e.id AND b.status = 'RESERVED')
```
This gives the true number of unreserved, unbooked seats visible to the next user.

---

## 7. Redis — What It Manages

Redis is the **reservation lock store**. It holds temporary, volatile state only.

**Key format:** `ticket:<ticket-uuid>`

**Value:** The `userID` (UUID string) of the user who acquired the lock.

**TTL:** Configured via `RESERVATION_TTL_MINUTES` env var (default: 10 minutes). Set atomically with the lock via `SET NX EX`.

**What Redis owns:**
- Whether a specific ticket is currently reserved and by whom.
- Automatic expiry: when the TTL fires, the key vanishes. No code runs. The seat is immediately acquirable by anyone.

**Three operations:**

| Operation | Redis command | When used |
|-----------|--------------|-----------|
| Acquire | `SET key userID NX EX ttl` | Reserve — try to claim a ticket |
| Release | Lua: `if GET key == userID then DEL key` | Confirm, Cancel — release own locks |
| GetOwner | `GET key` | Confirm — verify lock still owned before DB write |

**Why Lua for Release?**
A plain `DEL` without checking ownership is unsafe. Between a `GET` (check owner) and a `DEL` (delete), the TTL could expire and another user could acquire the lock. The Lua script runs as a single atomic Redis operation:
```lua
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0   -- key expired or owned by someone else; do nothing
end
```
This guarantees you never accidentally delete another user's reservation.

**Why not encode userID in the key?**
If the key were `ticket:abc:user-1`, then `ticket:abc:user-2` would be a different key, and both `SetNX` calls would succeed simultaneously — destroying the mutex. The mutex only works because all users compete on the same key `ticket:abc`. Ownership is encoded in the *value*, not the key.

---

## 8. Application Services — Business Logic

### `BookingService`

**`Reserve(userID, eventID, quantity)`**

1. **Lazy cleanup** — cancel booking records whose `created_at` is older than `now - TTL`. This keeps the `bookings` table tidy. Failures are logged as warnings, never fatal.
2. **Overfetch candidates** — fetch `max(quantity * 5, 10)` available tickets from Postgres. More than needed because some may be Redis-locked by other active reservations.
3. **Acquire Redis locks** — iterate candidates in order. For each ticket, attempt `SET NX EX`. If `ok=true`, add to `lockedTickets`. If `ok=false`, that seat is held by someone else — skip it. Stop when `lockedTickets` reaches `quantity`.
4. **Quota check** — if we couldn't lock enough, release all acquired locks and return `ErrTicketUnavailable`.
5. **Create booking record** — insert into `bookings` + `booking_tickets`. Tickets stay `AVAILABLE` in the DB. `ExpiresAt` is computed and returned in the response.
6. **Audit success.**

**`Confirm(userID, bookingID)`**

1. Fetch booking (`FOR UPDATE` to prevent concurrent confirms).
2. Validate ownership — `booking.UserID == userID`.
3. Validate status — must be `RESERVED`.
4. **Validate Redis lock ownership** — for each ticket, `GetOwner(ticket:id)` must return this `userID`. If the TTL already expired, `GetOwner` returns `""`, and Confirm is rejected with `ErrTicketUnavailable`.
5. **DB transaction** — atomically: `BulkUpdateStatus(tickets → BOOKED)` + `UpdateStatus(booking → CONFIRMED)`.
6. Release all Redis locks (they are no longer needed — DB is now authoritative).
7. Audit success.

**`Cancel(userID, bookingID)`**

1. Fetch booking (`FOR UPDATE`).
2. Validate ownership.
3. Validate not already cancelled.
4. **DB transaction:**
   - If `CONFIRMED`: restore tickets to `AVAILABLE` (set `booking_id = NULL`).
   - If `RESERVED`: tickets are already `AVAILABLE` in DB — no ticket update needed.
   - In both cases: `UpdateStatus(booking → CANCELLED)`.
5. If was `RESERVED`: release Redis locks after the DB transaction commits.
6. Audit success.

**`GetUserBookings(userID)`**
1. Lazy cleanup of expired reservations.
2. Fetch all `RESERVED` and `CONFIRMED` bookings for the user.
3. For each `RESERVED` booking, attach `ExpiresAt = createdAt + TTL` so the frontend can show the countdown.

### `EventService`

**`GetEvent(eventID)`** — fetches the event plus all its tickets (used for the detailed single-event view with the seat map).

**`ListEvents(params)`** — paginated list with optional keyword/date filters. `available_count` is computed in SQL.

### `UserService`

**`ListUsers()`** — returns all users. Used by the frontend user-selector dropdown.

---

## 9. HTTP Layer

### Routes

| Method | Path | Handler | Auth |
|--------|------|---------|------|
| GET | `/health` | inline | none |
| GET | `/events` | `EventHandler.ListEvents` | none |
| GET | `/events/:eventId` | `EventHandler.GetEvent` | none |
| POST | `/booking/reserve` | `BookingHandler.Reserve` | X-User-ID header |
| POST | `/booking/confirm` | `BookingHandler.Confirm` | X-User-ID header |
| DELETE | `/booking/:bookingId` | `BookingHandler.Cancel` | X-User-ID header |
| GET | `/booking/mine` | `BookingHandler.GetMyBookings` | X-User-ID header |
| GET | `/users` | `UserHandler.ListUsers` | none |

### Authentication
There is no JWT or session. Identity is passed as `X-User-ID: <uuid>` in every request. The service validates that the UUID matches the booking's `user_id` before any mutation. This is intentionally simple — a real system would use middleware to verify a signed token and inject the user ID into the context.

### Middleware
- **`CORSMiddleware`** — reflects the `Origin` header (required for custom headers like `X-User-ID` — wildcard `*` is invalid when non-standard headers are used). Handles `OPTIONS` preflight.
- **`RequestLogger`** — logs method, path, status code, and latency using `slog`.

### Error mapping (`response.go`)
```
ErrNotFound          → 404
ErrTicketUnavailable → 409
ErrUnauthorized      → 403
ErrConflict          → 409
anything else        → 500 (logged)
```

---

## 10. Data Flow: Complete Booking Journey

```
Browser                    HTTP Layer         BookingService      Postgres    Redis
  │                            │                    │               │           │
  │── POST /booking/reserve ──▶│                    │               │           │
  │   {eventId, quantity:1}    │── Reserve() ──────▶│               │           │
  │                            │                    │─ CancelExpired reservations ─▶│
  │                            │                    │               │           │
  │                            │                    │─ GetAvailable(eventId, 10)▶│
  │                            │                    │◀── [t1,t2,t3,t4,t5] ──────│
  │                            │                    │                           │
  │                            │                    │─ SET ticket:t1 user1 NX ─▶│
  │                            │                    │◀─ OK (acquired) ──────────│
  │                            │                    │                           │
  │                            │                    │─ INSERT booking ──────────▶│
  │                            │                    │─ INSERT booking_tickets ──▶│
  │                            │                    │                           │
  │◀── 201 {booking, expiresAt}│                    │               │           │
  │                            │                    │               │           │
  │  [user sees countdown]     │                    │               │           │
  │                            │                    │               │           │
  │── POST /booking/confirm ──▶│                    │               │           │
  │   {bookingId}              │── Confirm() ───────▶│               │           │
  │                            │                    │─ GetByID(FOR UPDATE) ─────▶│
  │                            │                    │─ GetOwner(ticket:t1) ────────────▶│
  │                            │                    │◀─ "user1" (lock valid) ──────────│
  │                            │                    │                           │
  │                            │                    │─ BEGIN TRANSACTION ───────▶│
  │                            │                    │─ BulkUpdateStatus(BOOKED) ▶│
  │                            │                    │─ UpdateStatus(CONFIRMED) ─▶│
  │                            │                    │─ COMMIT ──────────────────▶│
  │                            │                    │                           │
  │                            │                    │─ Release(ticket:t1, user1) ──────▶│
  │                            │                    │   (Lua: check+del atomic)         │
  │◀── 200 {booking CONFIRMED} │                    │               │           │
```

**Expiry path (no user action):**
```
[TTL fires after N minutes]

Redis                   Browser               BookingService (next Reserve call)
  │                        │                         │
  │  key ticket:t1 expires │                         │
  │  automatically         │                         │
  │                        │                         │
  │                        │── POST /booking/reserve ─▶
  │                        │                         │── CancelExpiredReservations()
  │                        │                         │   UPDATE bookings SET status='CANCELLED'
  │                        │                         │   WHERE status='RESERVED' AND created_at < cutoff
  │                        │                         │
  │                        │                         │── SetNX ticket:t1  ← succeeds (key gone)
  │                        │                         │   new user acquires the seat
```

---

## 11. Concurrency Model

Two users trying to reserve the same ticket at the same moment:

```
User A                              User B
  │                                   │
  │── SetNX ticket:t1 userA ─────────▶Redis
  │                    │── SetNX ticket:t1 userB ──▶Redis
  │◀── OK (wins)       │◀── false (loses)
  │                    │     skip this ticket, try t2
  │
  │  A holds ticket:t1 for TTL duration
```

`SET NX` is an atomic Redis command — Redis processes commands in a single thread. Only one caller can win. The loser simply skips to the next candidate ticket. This is why the service overfetches (`quantity * 5`) — to have spare candidates if some are already locked.

**No deadlocks:** Locks are always acquired on single tickets in DB-natural order (section → row → seat). Since each lock is independent and no lock waits for another, there is no circular dependency possible.

**Postgres `FOR UPDATE`** is used in `GetByID` inside Confirm and Cancel. This row-level lock on the booking row prevents two concurrent Confirm/Cancel calls on the same booking from racing. One will wait; the other will see the updated status and return `ErrConflict`.

---

## 12. Transaction & Audit Isolation Pattern

### Transactor pattern
The `Transactor` starts a `*sql.Tx`, wraps it in the `context.Context` under a private key, and passes the derived `txCtx` to the callback:

```go
txCtx := context.WithValue(ctx, contextKey{}, tx)
fn(txCtx)
```

Each repository's `exec(ctx)` helper checks the context for a transaction:
```go
func execFromContext(ctx context.Context, db *sql.DB) executor {
    if tx := txFromContext(ctx); tx != nil {
        return tx   // use the transaction
    }
    return db       // use the plain pool
}
```

This means the same repository code works both inside and outside a transaction without any changes. The service just calls `WithTransaction(ctx, func(txCtx) { ... repo calls with txCtx ... })`.

### Audit isolation
`auditRepo` deliberately holds a direct `*sql.DB` and always calls `r.db.ExecContext` — never `exec(ctx)`:

```go
type auditRepo struct {
    db *sql.DB  // never enrolled in a booking transaction
}
```

This means: even if the booking transaction rolls back (e.g., DB error during Confirm), the audit log entry for that failure is still written. You always get a complete audit trail regardless of what the business transaction does.

---

## 13. Assumptions

**Authentication is out of scope.** The `X-User-ID` header is trusted without verification. A production system would use JWT middleware that validates a signed token, extracts the user ID, and injects it into the request context.

**Tickets are pre-generated at event creation.** There is no API to create tickets on demand. The seed script generates all tickets for an event up front. This means `capacity` in the `events` table is redundant with `COUNT(*) FROM tickets WHERE event_id = ...` — both will always agree.

**`RESERVED` status is not in the DB tickets table.** Tickets are either `AVAILABLE` or `BOOKED` in Postgres. The reservation state is held only in Redis. This is a deliberate trade-off: it avoids a DB write on Reserve (faster, less lock contention) at the cost of needing Redis to be the source of truth for in-flight reservations.

**Lazy cleanup, not a background worker.** Expired reservations (booking records with status `RESERVED` and `created_at` older than TTL) are cleaned up lazily — on the next `Reserve` or `GetUserBookings` call. This means a stale `RESERVED` record may exist briefly after TTL, but the seat is already free (Redis key expired). The available count query accounts for this by only subtracting currently `RESERVED` bookings, which will be cleaned up on the next request.

**Single-region, single-Redis.** The lock manager uses a single Redis instance. For multi-region deployments, you would need Redlock (distributed consensus across N Redis nodes). This is out of scope.

**No payment processing.** The `confirmRequest` accepts a `paymentDetails` field but does nothing with it. Confirm is a logical step only — no money moves.

**`available_count` is eventually consistent during the reservation window.** Between the Redis TTL expiry and the next lazy cleanup call, a stale `RESERVED` booking may briefly cause `available_count` to read lower than actual. This is the safe direction (under-report availability rather than over-report), and it corrects itself on the next request that triggers cleanup.

**No per-user reservation limit.** A single user can reserve as many tickets as they want across multiple calls. A real system would enforce per-user, per-event limits.

**Seed data is required.** The application has no admin API to create events, venues, or performers. All reference data must be inserted via `002_seed.sql`.
