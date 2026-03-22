# Design Notes

This document answers two questions:

1. Why does the current system work?
2. How would it need to evolve at higher scale?

Simple rule:

- `README.md` explains how to run and use the system.
- `design.md` explains why the design works, its trade-offs, and how it scales.

---

## Core Design Rule

The system is built around one main split of responsibility:

> PostgreSQL owns durable truth. Redis owns temporary reservation ownership.

That means:

- PostgreSQL decides whether inventory is still available.
- PostgreSQL stores the booking lifecycle.
- Redis does not decide final capacity correctness.
- Redis only helps with reservation ownership, TTL, and same-user duplicate coordination for one event.

The model is quantity-based, not seat-based.

---

## High-Level Architecture

### Layers

- `cmd/server` — HTTP bootstrap, router, handlers, frontend serving
- `internal/service` — business logic
- `internal/ports` — interfaces between service and infrastructure
- `internal/infra/postgres` — repositories and transaction helper
- `internal/infra/redis` — temporary reservation locks

### Main runtime pieces

- Gin HTTP API
- PostgreSQL for durable state
- Redis for short-lived reservation locks
- static frontend under `cmd/server/frontend`

---

## Data Model

### `venues`

- Stores static venue data
- `capacity` is the hard limit

### `events`

- Stores event metadata
- Stores live counters:
  - `booked_slots`
  - `reserved_slots`

### `bookings`

Each row represents:

- one user
- one event
- one quantity
- one lifecycle

Lifecycle:

- `RESERVED`
- `CONFIRMED`
- `CANCELLED`

### `audit_logs`

Append-only operational history for booking actions and failures.

---

## Why This Works

### Inventory correctness

Availability is computed as:

```text
available = venue.capacity - events.booked_slots - events.reserved_slots
```

Reserve, confirm, cancel, and expiry cleanup all update the `events` counters in transactions. That is what prevents overselling.

### Reservation ownership

Redis lock:

- key: `reservation:<eventID>:<userID>`
- value: `<quantity>|<bookingID>`

This does not hold capacity. It only proves that the reservation window for that booking is still active and belongs to the caller.

### Booking lifecycle safety

- reserve inserts `RESERVED`
- confirm updates only from `RESERVED` to `CONFIRMED`
- cancel sets `CANCELLED`

This makes repeated or competing transitions fail safely instead of silently overwriting state.

---

## Booking Flow Summary

### Reserve

1. Validate quantity.
2. Generate booking ID.
3. Acquire Redis lock.
4. Start a DB transaction.
5. Cancel expired reservations.
6. Atomically increment `events.reserved_slots` if capacity allows.
7. Reject duplicate active reservation for the same user/event.
8. Insert booking row as `RESERVED`.
9. Commit.
10. Leave Redis lock alive.
11. Schedule background expiry cleanup.

### Confirm

1. Load booking.
2. Validate user and state.
3. Rebuild Redis key/value and compare with `GET`.
4. In one transaction:
   - update booking to `CONFIRMED`
   - move seats from `reserved_slots` to `booked_slots`
5. Release Redis lock.
6. Write audit.

### Cancel / Return

Reserved booking:

- set booking to `CANCELLED`
- decrement `reserved_slots`
- release Redis lock

Confirmed booking:

- set booking to `CANCELLED`
- decrement `booked_slots`

So current "return" behavior is simply confirmed booking cancellation.

---

## Concurrency Model

### What PostgreSQL guarantees

PostgreSQL is the correctness layer.

Reserve uses one conditional update:

```text
booked_slots + reserved_slots + requested_quantity <= venue.capacity
```

If that statement cannot apply, reserve fails.

Confirm and cancel also move counters transactionally, so inventory stays aligned with the booking lifecycle.

### What Redis guarantees

Redis is not the capacity authority.

It gives:

- temporary reservation ownership
- TTL-driven reservation expiry on the lock side
- same-user duplicate prevention for the same event

### What both together guarantee

- capacity cannot exceed venue limit if event counters stay correct
- a user cannot confirm after the reservation lock is gone
- the same user cannot keep opening overlapping reserve flows for one event

---

## Cancel / Return Behavior

The system intentionally uses one `CANCELLED` state for both:

- a reservation cancelled before payment
- a confirmed booking returned later

Reason:

- simple lifecycle
- simple inventory release logic

Impact:

- the system does not yet distinguish business cases like refund, chargeback, or admin void

---

## Audit Logging

Every booking action writes audit rows:

- `BOOKING_CREATED`
- `BOOKING_CONFIRMED`
- `BOOKING_CANCELLED`

Audit rows include:

- `action`
- `outcome`
- `user_id`
- `entity_id`
- `quantity`
- `metadata`

Important design decision:

- audit is written outside the booking transaction

Why:

- failures should still be visible even if the business transaction rolls back

Impact:

- audit durability is improved
- audit rows are not strictly atomic with business writes

---

## Key Assumptions

### Product assumptions

- quantity-based reservation is enough for now
- one active reservation per user/event is enough
- payment integration is out of scope
- confirmed booking cancellation should return capacity immediately

### Technical assumptions

- one PostgreSQL database is enough for current scale
- one Redis deployment is enough for the current lock model
- `venues.capacity` is authoritative
- `bookingID` can be generated in the service before insert
- timer-based cleanup plus lazy cleanup is acceptable

### Operational assumptions

- app restarts can happen even though in-process timers are lost
- a fresh Docker environment is a normal dev workflow
- users are demo rows returned by `/users`

---

## Trade-offs

| Decision | Reason | Impact |
|---|---|---|
| Event-level counters on `events` | Avoid repeated `SUM(bookings)` in the hot path | Faster inventory checks, but app logic must keep counters aligned |
| Conditional inventory updates in PostgreSQL | Make capacity enforcement atomic and transactional | Correctness is strong, but hot events contend on one row |
| Redis only for reservation ownership and TTL | Keep capacity correctness in one durable system | Simpler correctness model, but confirm depends on Redis availability |
| In-process expiry timer after reserve | Easy way to reconcile DB state after TTL | Timers are lost on restart and need lazy cleanup backup |
| No idempotency keys | Keep API simple for the assignment/demo | Client retries can still hit conflict-style errors instead of replay semantics |
| Quantity-based booking instead of seat-based booking | Smaller model and easier flows | No exact seat selection, adjacency, or seat-map constraints |
| Single `CANCELLED` state for reserve-cancel and post-confirm return | Simple lifecycle and simple counter release | Cannot distinguish refund/return business states yet |
| Audit written outside transaction | Preserve failure logs on rollback | Audit and business updates are not one atomic unit |

---

## Edge Cases

- Concurrent reserve requests for the same event: PostgreSQL conditional counter update prevents oversell.
- Same user retries reserve before TTL expiry: Redis key and active-reservation check reject the duplicate flow.
- Double confirm: only one request can transition a booking from `RESERVED` to `CONFIRMED`.
- Confirm after TTL expiry: Redis lock mismatch returns `ticket unavailable`.
- Cancel after confirm: booking becomes `CANCELLED` and `booked_slots` is released.
- Cancel after reserve: booking becomes `CANCELLED`, `reserved_slots` is released, and Redis lock is deleted.
- App restart after reserve: in-process timer is lost, but later lazy cleanup can still cancel expired reservations.

---

## Future Improvements

Short list of likely next evolutions:

- add idempotency keys for reserve and confirm
- add explicit refund / return states
- add request IDs and structured failure codes to audit metadata
- move expiry cleanup to a durable worker or scheduled job
- add rate limiting on reserve endpoints
- denormalize or isolate hot inventory paths further if contention grows
- consider queue-based booking or sharded inventory for very hot events
- add seat-level inventory only if the product needs explicit seat selection

---

## How This Would Evolve Under High Scale

### 1. Hot-event contention

Today, one hot event means one hot `events` row.

At larger scale:

- split inventory into dedicated rows or shards
- or move booking admission through a queue/worker model

### 2. Durable expiry handling

Today, expiry is partly in-process.

At larger scale:

- move to a background worker, cron-like scanner, or delayed job queue

### 3. Stronger client semantics

Today, retries are handled by conflict checks.

At larger scale:

- add idempotency keys so duplicate client calls are safe and predictable

### 4. Better observability

Today, audit logs are good for debugging.

At larger scale:

- add stable failure codes
- add correlation IDs
- add metrics and dashboards for reserve/confirm/cancel outcomes

### 5. Optional seat-level design

Today, the system sells quantities.

If explicit seats are required later:

- add seat inventory rows
- add per-seat reservation states
- move from event counters to seat allocation logic

---

## Summary

The current design is a solid intermediate system:

- simple enough to run and review quickly
- strong enough to avoid overselling
- explicit enough to reason about reserve, confirm, cancel, and expiry

Its main limitations are hot-row contention, in-process expiry timers, and the lack of idempotency and seat-level modeling.

For code structure, see `ARCHITECTURE.md`. For the detailed runtime sequence, see `reservelogic.md`.
