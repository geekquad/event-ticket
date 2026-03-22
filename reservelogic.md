# Booking Runtime Logic

This document explains the current runtime logic in detail for:

- reserve
- confirm
- cancel / return
- expiry cleanup

It also calls out pain points and how this design would need to evolve for very high scale.

---

## Overview

The current flow has two core ideas:

1. Redis tracks temporary reservation ownership.
2. PostgreSQL tracks durable state and inventory counters.

That means:

- Redis answers "does this user still own the reservation window?"
- PostgreSQL answers "how many seats are reserved/booked right now?"

---

## Data And Lock Model

### Redis key

```text
reservation:<eventID>:<userID>
```

Example:

```text
reservation:1f2d...:8e4c...
```

Why this shape:

- lock is scoped to one user and one event
- same user cannot spam two reserve requests for the same event concurrently

### Redis value

```text
<quantity>|<bookingID>
```

Example:

```text
2|c9c4f0e9-...
```

Why this value exists:

- confirm can verify the same booking ID is being confirmed
- confirm can verify the same quantity is tied to the lock

### Booking row

Each booking row contains:

- `id`
- `user_id`
- `event_id`
- `quantity`
- `status`
- `expires_at`

### Event inventory row

`events` stores:

- `booked_slots`
- `reserved_slots`

Available seats are computed as:

```text
venue.capacity - events.booked_slots - events.reserved_slots
```

---

## Reserve Logic

### Step 1: normalize and validate input

Inside `BookingService.Reserve`:

- `quantity <= 0` becomes `1`
- `quantity > maxSeatsPerReservation` fails with `ErrInvalidQuantity`

This guard is there to stop absurd single-request quantities from reaching Redis and the DB hot path.

### Step 2: generate booking ID

The service generates `bookingID := uuid.New().String()` before doing any external work.

That UUID is reused in:

- Redis value
- `bookings.id`
- later confirm validation

### Step 3: acquire Redis lock

Redis call:

```text
SET reservation:<eventID>:<userID> "<quantity>|<bookingID>" NX EX <ttl>
```

Behavior:

- if key does not exist, reserve may continue
- if key exists, reserve fails with `ErrConflict`

This is only a same-user / same-event guard. It is not a global event mutex.

### Step 4: defer guarded Redis release

Reserve sets:

- `lockAcquired := true`

and installs a defer:

- if the flow later fails, Redis is released
- if the flow succeeds, `lockAcquired` is set to `false` and the lock stays alive

This is important because the lock is supposed to survive after reserve and be checked by confirm.

### Step 5: begin DB transaction

Everything below runs inside `Transactor.WithTransaction`.

### Step 6: cancel expired reservations

First operation in the transaction:

- `CancelExpiredReservations()`

What it does:

1. Marks expired `RESERVED` bookings as `CANCELLED`
2. Aggregates their quantities by event
3. Decrements `events.reserved_slots` with:

```sql
SET reserved_slots = GREATEST(e.reserved_slots - a.sub, 0)
```

Why this matters:

- frees stale holds before new capacity checks
- self-heals if counters drifted

### Step 7: atomically claim reserved capacity

Next operation:

- `eventRepo.TryAddReservedSlots(eventID, quantity)`

SQL shape:

```sql
UPDATE events e
SET reserved_slots = e.reserved_slots + $2
FROM venues v
WHERE e.id = $1::uuid
  AND e.venue_id = v.id
  AND e.booked_slots + e.reserved_slots + $2 <= v.capacity
RETURNING e.id::text
```

Meaning:

- reserve succeeds only if the updated quantity still fits
- capacity is checked and increment is applied in one statement

If no row is returned:

- `SELECT EXISTS(...)` distinguishes:
  - unknown event -> `ErrNotFound`
  - known event but no seats -> `ErrInsufficientCapacity`

### Step 8: enforce one active reservation per user/event

Next query:

```sql
SELECT EXISTS(
  SELECT 1
  FROM bookings
  WHERE user_id = $1
    AND event_id = $2
    AND status = 'RESERVED'
    AND expires_at > NOW()
)
```

If true:

1. release the increment just made on `events.reserved_slots`
2. return `ErrConflict`

Why this still exists even with Redis:

- Redis blocks concurrent same-user reserve attempts right now
- this DB check protects against stale conditions, retries, or cross-process edge cases

### Step 9: insert booking row

Insert:

```sql
INSERT INTO bookings (
  id, user_id, event_id, quantity, status, expires_at, created_at, updated_at
)
VALUES (...)
```

Inserted values:

- `status = RESERVED`
- `expires_at = now + reservationTTL`

If this insert fails, the transaction rolls back and the deferred Redis release runs.

### Step 10: commit and keep lock alive

After commit:

- `lockAcquired = false`
- success audit is written
- background timer is started

### Step 11: background expiry timer

`go s.scheduleReservationUnlock(booking.ID)`

This timer:

1. waits for `reservationTTL`
2. in a new transaction:
   - cancels the booking if it is still expired + reserved
   - decrements `events.reserved_slots`

It is idempotent:

- if user already confirmed, no change
- if user already cancelled, no change
- if lazy cleanup already ran, no change

---

## Confirm Logic

Confirm is stricter than reserve because it validates both DB state and Redis ownership.

### Step 1: load booking

`bookingRepo.GetByID()` uses:

```sql
SELECT ...
FROM bookings
WHERE id = $1
FOR UPDATE
```

This locks the booking row for the current transaction / request.

### Step 2: validate owner

If `booking.UserID != userID`:

- audit failure
- return `ErrUnauthorized`

### Step 3: validate lifecycle state

If booking is not `RESERVED`:

- audit failure
- return `ErrConflict`

### Step 4: rebuild and verify Redis ownership

Confirm rebuilds:

- key: `reservation:<eventID>:<userID>`
- expected value: `<quantity>|<bookingID>`

Then it calls `GetOwner()` on Redis.

If the returned value does not exactly match:

- audit failure
- return `ErrTicketUnavailable`

This usually means:

- TTL expired
- lock already released
- wrong booking/quantity was presented

### Step 5: confirm in DB

Inside a transaction:

1. `ConfirmReservation(bookingID)` updates booking to `CONFIRMED` only when it is still `RESERVED`
2. `TransferReservedToBooked(eventID, quantity)` moves seats from reserved to booked

Transfer SQL:

```sql
UPDATE events e
SET reserved_slots = e.reserved_slots - $2,
    booked_slots = e.booked_slots + $2
WHERE e.id = $1::uuid
  AND e.reserved_slots >= $2
```

If booking state changed concurrently, confirm fails with `ErrConflict`.

### Step 6: release Redis lock

After DB success, the service releases Redis using the guarded Lua delete.

### Step 7: audit success

Confirm writes `BOOKING_CONFIRMED / SUCCESS`.

---

## Cancel / Return Logic

Cancel handles both reserved cancellations and confirmed ticket returns.

### Step 1: load booking

The service loads the booking and audits `not_found` if missing.

### Step 2: validate owner and state

- wrong user -> `ErrUnauthorized`
- already cancelled -> `ErrConflict`

### Step 3: transactional inventory release

Inside a transaction:

1. update booking status to `CANCELLED`
2. if old state was `RESERVED`, run `ReleaseReservedSlots`
3. if old state was `CONFIRMED`, run `ReleaseBookedSlots`

Reserved release SQL:

```sql
UPDATE events
SET reserved_slots = reserved_slots - $2
WHERE id = $1::uuid
  AND reserved_slots >= $2
```

Booked release SQL:

```sql
UPDATE events
SET booked_slots = booked_slots - $2
WHERE id = $1::uuid
  AND booked_slots >= $2
```

### Step 4: release Redis lock for reserved bookings

If the booking used to be `RESERVED`, the service also releases:

```text
reservation:<eventID>:<userID>
```

using the expected value `<quantity>|<bookingID>`.

Confirmed bookings do not rely on Redis anymore, so no lock release is needed there.

### Step 5: audit success

Cancel writes `BOOKING_CANCELLED / SUCCESS`.

---

## Lazy Cleanup Logic

The service also calls `CancelExpiredReservations()` from:

- reserve
- get my bookings

That means even if the timer path is lost because the app restarts, future traffic still cleans old expired holds.

This is a pragmatic design, but it is not a perfect durable scheduler.

---

## Audit Logic

Audit is written outside the DB transaction for the business operation.

Why:

- failure logs survive business rollback
- operational visibility is favored over strict transactional coupling

What gets logged:

- booking created success/failure
- booking confirmed success/failure
- booking cancelled success/failure

Common metadata:

- `eventId`
- `bookingId`
- `reason`
- `status`
- `max`

Pain point:

- metadata is free-form JSON, so analytics queries are possible but not ideal

---

## Pain Points At Scale

### 1. Hot event row contention

Every reserve, confirm, cancel, and expiry cleanup for one event touches the same `events` row.

Impact:

- hot events serialize on one row
- latency increases under spikes
- throughput drops for blockbuster events

### 2. In-process expiry timer

`scheduleReservationUnlock()` is a goroutine inside the app process.

Impact:

- app restart loses scheduled timers
- cleanup still eventually happens because of lazy cleanup, but not immediately

### 3. Redis is a reservation ownership helper, not a durable workflow engine

If Redis is unavailable:

- reserve and confirm cannot proceed

Impact:

- short Redis issues affect booking flows directly

### 4. No seat-level model

The system reserves quantities only.

Impact:

- cannot implement exact seat selection without redesign
- cannot handle per-seat adjacency rules

### 5. Audit schema is flexible but weakly structured

Impact:

- good for debugging
- less ideal for large-scale compliance analytics or BI

### 6. Capacity lookup still joins `venues`

Reserve uses `FROM venues v` on the hot path.

Impact:

- join cost is usually modest compared to lock contention
- still an extra dependency in the hottest update

### 7. One active reservation per user/event is enforced in app logic, not a partial unique index

Impact:

- logic is correct today, but relies on reserve flow discipline

---

## How To Scale To A Million Users

The current design is good for a small service or demo, but not the final shape for very high concurrency. A path toward much larger scale would look like this:

### 1. Separate "active inventory" from the main `events` row

Instead of updating one hot `events` row, introduce a dedicated inventory model:

- `event_inventory` table
- or sharded inventory buckets per event

Goal:

- spread writes across multiple rows
- reduce hot-row queueing

### 2. Replace in-process expiry timers with durable cleanup

Better options:

- a DB-backed scheduler
- a queue with delayed jobs
- a periodic worker scanning expired reservations

Goal:

- expiry survives pod restarts and rolling deploys

### 3. Treat Redis as optional acceleration, not the only proof source

Today confirm depends on Redis lock ownership.

At larger scale you may want:

- a durable reservation token in DB
- or a signed reservation token
- or both Redis and DB proof

Goal:

- avoid hard dependency on one cache for correctness of confirm ownership

### 4. Add seat-level modeling only if product needs it

If exact seat selection is required:

- create a `seats` table
- create per-seat reservation state
- move from quantity-based inventory to seat-based inventory

Goal:

- support explicit seat maps and precise locking

### 5. Improve audit structure

Add:

- stable failure codes
- request IDs
- actor/device/session info
- typed metadata fields

Goal:

- stronger observability and compliance

### 6. Consider a command/event workflow for blockbuster traffic

For extremely hot events:

- accept reserve requests
- push them into a queue / partitioned stream
- have inventory workers process them deterministically

Goal:

- keep correctness while smoothing bursts

Trade-off:

- more operational complexity
- more asynchronous behavior for clients

### 7. Cache read-heavy endpoints separately

`GET /events` can be cached aggressively because it is read-heavy and its payload is compact.

Goal:

- protect DB from read spikes while the write path remains authoritative

---

## Summary

Today’s design is a good intermediate architecture:

- simpler than seat-level locking
- safer than recomputing counts with `SUM(bookings)` on every write
- expressive enough for reserve/confirm/cancel

Its main future pain points are:

- hot-row contention on `events`
- non-durable in-process expiry timers
- quantity-only inventory

For structural reference (routes, schema, ports), see [ARCHITECTURE.md](ARCHITECTURE.md). For rationale, assumptions, and trade-offs, see [design.md](design.md).
