# Architecture

This document is the **structural reference**: packages, HTTP routes, schema highlights, repository ports, and how transactions and Redis are wired in code.

For **why** the system is shaped this way (assumptions, trade-offs, edge cases, scaling), see [design.md](design.md). For long-form reserve/confirm/cancel flows, see [reservelogic.md](reservelogic.md).

---

## High-Level Shape

The project follows a lightweight ports-and-adapters style:

- `cmd/server` exposes the HTTP API and serves the static frontend.
- `internal/service` contains booking, event, and user use cases.
- `internal/ports` defines interfaces used by the service layer.
- `internal/infra/postgres` implements repositories and transaction handling.
- `internal/infra/redis` implements reservation lock storage.

**Correctness split (Postgres vs Redis):** explained in [design.md — Core design rule](design.md#core-design-rule).

---

## Inventory Model

Availability for listing (and the mental model for counters) is:

```text
available = venues.capacity - events.booked_slots - events.reserved_slots
```

- `booked_slots` — confirmed purchases  
- `reserved_slots` — holds not yet confirmed  

Why counter updates prevent overselling: [design.md — Why this works](design.md#why-this-works).

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

Expired cleanup adjusts `reserved_slots` with `GREATEST(e.reserved_slots - sub, 0)` so reconciliation stays safe if counters drift low. Concurrency guarantees (same user vs cross-user, confirm rules): [design.md — Concurrency model](design.md#concurrency-model).

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

---

## Transaction Model

`internal/infra/postgres/tx.go` stores `*sql.Tx` in the request context. Repository methods call `execFromContext()` and transparently use either:

- the transaction, or
- the shared DB pool

This keeps repository methods reusable without creating duplicate `Tx` and non-`Tx` versions.

---

## Booking Workflows

Narrative version (lifecycle and “why these steps”): [design.md — Booking flow summary](design.md#booking-flow-summary).

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

## Audit Isolation

`auditRepo` writes with the root DB handle, not the transaction-bound executor (so failure audits can survive booking transaction rollback).

Rationale and trade-offs: [design.md — Audit logging](design.md#audit-logging).

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

## Related documentation

| Topic | Document |
|--------|-----------|
| Assumptions, trade-off table, edge cases, future work, scaling | [design.md](design.md) |
| Detailed reserve / confirm / cancel sequences | [reservelogic.md](reservelogic.md) |
