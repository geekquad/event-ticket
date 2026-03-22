# Reservation flow (`BookingService.Reserve`)

This document explains **what happens when a client calls reserve**, in order: Redis lock semantics, PostgreSQL statements, and how **Confirm** ties back to the same Redis key/value.

---

## High-level story

1. **Redis** gives you a **short-lived, per-user-per-event mutex** so the same user cannot run two concurrent reserve attempts that both touch inventory (and so **Confirm** can prove the client still “owns” the reservation window).
2. **PostgreSQL** (inside one transaction) cleans up expired holds, **atomically** increases `events.reserved_slots` if capacity allows, ensures the user does not already have an active reservation for that event, then **inserts** the `bookings` row.
3. On **success**, the Redis key is **left in place** until confirm, cancel, or TTL expiry (the defer does **not** release). A **background timer** fires after the same TTL to release DB inventory if the booking was never confirmed.

---

## Step 0: Inputs and defaults

| Input | Role |
|--------|------|
| `userID` | UUID string (demo auth: `X-User-ID`). Stored on the booking and part of the Redis **key** only. |
| `eventID` | Target event UUID. Part of Redis **key** and booking row. |
| `quantity` | Seat count; if `<= 0`, forced to **1**. |

A new **`bookingID`** is generated with `uuid.New()` **before** any I/O. The same ID is used in Redis value, the `bookings.id` insert, and later in **Confirm**’s lock check.

---

## Step 1: Redis — key, value, TTL, and commands

### Key

```text
reservation:<eventID>:<userID>
```

Example: `reservation:550e8400-e29b-41d4-a716-446655440000:6ba7b810-9dad-11d1-80b4-00c04fd430c8`

- **Namespace** `reservation:` avoids colliding with other Redis uses.
- **Scoped to one user and one event**: at most one active reservation workflow per pair in Redis at a time.

### Value

```text
<quantity>|<bookingID>
```

Example: `2|f47ac10b-58cc-4372-a567-0e02b2c3d479`

Pipe (`|`) is a delimiter. **`userID` is not repeated** here — it is already in the Redis key (`reservation:<eventID>:<userID>`). The value carries:

| Segment | Purpose |
|---------|---------|
| `quantity` | Matches the reservation size (Confirm re-builds the expected value with `booking.Quantity`). |
| `bookingID` | Ties the lock to **this** booking row so Confirm can verify the lock matches the booking being paid. |

### TTL

Same duration as **`reservationTTL`** in app config (e.g. 2 minutes). Passed to Redis as the `SET` expiry.

- When TTL elapses, Redis **deletes the key automatically**.
- **Confirm** uses `GetOwner(key)`; if the key is gone or the value differs, confirm fails with “reservation expired or lock mismatch.”

### Acquire: `SET key value NX EX <ttl_seconds>`

Implemented as `SetNX` + TTL in `internal/adapters/redis/lock_manager.go`:

- **NX**: set only if the key does **not** exist.
- If the key already exists → `ok == false` → API returns **conflict** (user already in a reservation flow for that event, or stale key not yet expired).

### Release: Lua script (safe unlock)

```lua
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0
end
```

- **Only deletes** if the stored value equals the **exact** value this client set.
- Prevents deleting another client’s lock if something went wrong (not applicable here with one key per user+event, but correct pattern).

### When Redis is released during `Reserve`

| Outcome | `lockAcquired` / defer |
|---------|-------------------------|
| **Transaction fails** (sold out, duplicate reservation, DB error, etc.) | `lockAcquired` stays **true** → **defer runs** → **Release** so the user can retry. |
| **Transaction succeeds** | `lockAcquired` set to **false** before return → defer **does not** release → key stays until Confirm/Cancel/TTL. |

---

## Step 2: PostgreSQL — one transaction

All of the following run inside **`Transactor.WithTransaction`** (single BEGIN/COMMIT or ROLLBACK). The context carries the `*sql.Tx`, so every repo call uses the same transaction.

### 2.1 `CancelExpiredReservations`

**Purpose:** Free inventory held by **past** `RESERVED` rows and keep `events.reserved_slots` consistent.

**What it runs (conceptually):**

1. **CTE `expired`:** `UPDATE bookings` → `status = 'CANCELLED'`, `updated_at = NOW()`  
   `WHERE status = 'RESERVED' AND expires_at <= NOW()`  
   **RETURNING** `event_id`, `quantity`.

2. **CTE `agg`:** For each `event_id`, **SUM(quantity)** of those cancelled rows.

3. **`UPDATE events`:** For each row in `agg`,  
   `reserved_slots = GREATEST(reserved_slots - sub, 0)`  
   `WHERE id = event_id`  
   so counters stay aligned with cancelled rows even if `reserved_slots` was already lower than `sub` (drift self-heal); the table `CHECK` still enforces non-negative.

If nothing is expired, the CTEs produce no rows; the `UPDATE events` affects **0** rows — still valid.

---

### 2.2 `TryAddReservedSlots(eventID, quantity)`

**Purpose:** Atomically claim inventory: increase **`events.reserved_slots`** only if **venue capacity** is not exceeded.

**SQL shape:**

```sql
UPDATE events e
SET reserved_slots = e.reserved_slots + $2
FROM venues v
WHERE e.id = $1::uuid AND e.venue_id = v.id
  AND e.booked_slots + e.reserved_slots + $2 <= v.capacity
RETURNING e.id
```

- **Single row update** on `events` (and read `venues.capacity` via join).
- Uses **`RETURNING`** so one statement both applies the change and proves a row matched (no separate `RowsAffected` round trip on success).
- If **no row** is returned: `SELECT EXISTS(SELECT 1 FROM events WHERE id = $1)` distinguishes **missing event** (`ErrNotFound`) from **event exists but not enough seats** (`ErrInsufficientCapacity`). The API returns **409** with `{"error":"insufficient capacity"}` for the latter; other reserve failures use **409** with `{"error":"conflict"}` or **404** for unknown event.

**Inventory model:** `booked_slots` = confirmed tickets; `reserved_slots` = held-but-not-paid; **available** = `capacity - booked - reserved` (computed in list queries).

---

### 2.3 `HasActiveReservedBookingForUserEvent(userID, eventID)`

**Purpose:** Enforce **one active reservation per user per event** at the DB layer (in addition to Redis).

```sql
SELECT EXISTS(
  SELECT 1 FROM bookings
  WHERE user_id = $1 AND event_id = $2
  AND status = 'RESERVED' AND expires_at > NOW()
)
```

- If **true**, the user already has a non-expired `RESERVED` row. That should be rare right after `TryAddReservedSlots` in a correct single-threaded flow, but it covers races or weird retries.

**If `has == true`:**

1. **`ReleaseReservedSlots(eventID, quantity)`** — reverses the increment from `TryAddReservedSlots` in the **same transaction** so inventory is not leaked.
2. Returns **`ErrConflict`** to the client.

---

### 2.4 `Create(booking)`

**Purpose:** Persist the reservation.

```sql
INSERT INTO bookings (id, user_id, event_id, quantity, status, expires_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
```

- `id` = pre-generated `bookingID` (matches Redis value).
- `status` = `'RESERVED'`.
- `expires_at` = `now + reservationTTL` (aligned with Redis TTL conceptually).

Failure (e.g. FK violation) rolls back the whole transaction, including `TryAddReservedSlots` — so **`reserved_slots` is rolled back** too.

---

## Step 3: After commit — audit and background unlock

### Success audit

`writeAudit` with **SUCCESS**, `entityId` = booking id, metadata includes `bookingId`, `eventId`.

### `scheduleReservationUnlock(bookingID)` (goroutine)

- Waits **`reservationTTL`** (same idea as Redis key lifetime).
- In a **new** transaction:
  1. **`CancelReservationIfExpired`** — `UPDATE bookings` to `CANCELLED` **only if** still `RESERVED` and `expires_at <= NOW()`, returning `event_id` and `quantity`.
  2. If a row was updated, **`ReleaseReservedSlots`** for that quantity.

**Idempotent:** If the user **Confirmed**, the row is no longer `RESERVED` → step 1 updates **0** rows → no inventory change. If **Cancel** or **batch** `CancelExpiredReservations` already ran, same.

---

## How Confirm reuses the same Redis key/value

Confirm rebuilds:

- **Key:** `reservation:<eventID>:<userID>` (from booking).
- **Expected value:** `reservationLockValue(booking.Quantity, bookingID)` — must **exactly match** `GET reservation:...`.

So the lock proves:

1. The reservation window is still active (key exists), and  
2. The client is confirming the **same** booking id and quantity that created the lock.

---

## End-to-end ordering (reference)

```text
1. Generate bookingID
2. Redis SETNX reservation:<eventId>:<userId> = "<qty>|<bookingId>" TTL=reservationTTL
3. BEGIN
4. CancelExpiredReservations (bookings + events counters)
5. TryAddReservedSlots
6. HasActiveReservedBookingForUserEvent → if true, ReleaseReservedSlots + abort
7. INSERT booking
8. COMMIT
9. Clear defer so Redis key is NOT deleted
10. Audit success; start timer goroutine for DB-side expiry cleanup
```

On any failure before step 9, transaction rolls back and defer **releases** Redis so the user can try again.

---

## Design notes

- **Redis** = fast per-user mutex + **proof of possession** for confirm; it does **not** store inventory counts.
- **Postgres** = source of truth for **capacity** (`events` counters + `venues.capacity`) and **booking rows**.
- **TTL alignment:** Redis expiry, `bookings.expires_at`, and `scheduleReservationUnlock` are all driven by the same configured **`reservationTTL`** so behaviour stays coherent.
