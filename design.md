# Design Notes

This document answers two questions:

1. Why does the current system work?
2. How would it need to evolve at higher scale?

**Doc split:** [README.md](README.md) — run and use. **[ARCHITECTURE.md](ARCHITECTURE.md)** — packages, routes, schema columns, repository methods, SQL/Lua snippets, HTTP error mapping. **This file** — rationale, assumptions, trade-offs, edge cases, and scaling (no duplicate route or port inventories).

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

## Where the code lives

Layer layout, HTTP routes, and **full table/column/index reference**: [ARCHITECTURE.md](ARCHITECTURE.md) (see *High-level shape*, *Runtime components*, *Data model*).

At a glance: Gin HTTP API, PostgreSQL for durable state, Redis for short-lived reservation locks, static frontend under `cmd/server/frontend`.

---

## Data model (conceptual)

- **`venues`** — static metadata; **`capacity`** is the hard limit.
- **`events`** — event metadata plus live counters **`booked_slots`** and **`reserved_slots`** (inventory row for quantity-based sales).
- **`bookings`** — one row per user/event/quantity with lifecycle **`RESERVED` → `CONFIRMED` / `CANCELLED`**.
- **`audit_logs`** — append-only operational history.

**Schema details** (constraints, indexes, `audit_logs` fields, `metadata` type): [ARCHITECTURE.md — Data model](ARCHITECTURE.md#data-model).

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

**Implementation steps** (validation quirks, repo calls, timer cleanup): [ARCHITECTURE.md — Booking workflows](ARCHITECTURE.md#booking-workflows).

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

Recent rows are exposed read-only at **`GET /audit/logs`** (newest first; optional `limit` and **`eventId`** to filter by event via the booking join; see [ARCHITECTURE.md](ARCHITECTURE.md) and OpenAPI). This demo endpoint is not authenticated.

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

## Edge Cases

- Concurrent reserve requests for the same event: PostgreSQL conditional counter update prevents oversell.
- Same user retries reserve before TTL expiry: Redis key and active-reservation check reject the duplicate flow.
- Double confirm: only one request can transition a booking from `RESERVED` to `CONFIRMED`.
- Confirm after TTL expiry: Redis lock mismatch returns `ticket unavailable`.
- Cancel after confirm: booking becomes `CANCELLED` and `booked_slots` is released.
- Cancel after reserve: booking becomes `CANCELLED`, `reserved_slots` is released, and Redis lock is deleted.
- App restart after reserve: in-process timer is lost, but later lazy cleanup can still cancel expired reservations.
---

## How This Would Evolve Under High Scale

### 1. Hot-event contention

Today, one hot event means one hot `events` row.

At larger scale:

- split inventory into dedicated rows
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
- add metrics and dashboards for reserve/confirm/cancel outcomes

---

## Summary

The current design is a solid intermediate system:

- simple enough to run and review quickly
- strong enough to avoid overselling
- explicit enough to reason about reserve, confirm, cancel, and expiry

For code structure and ports, see [ARCHITECTURE.md](ARCHITECTURE.md). For the detailed runtime sequence, see [reservelogic.md](reservelogic.md).
