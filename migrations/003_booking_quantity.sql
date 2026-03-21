-- +goose Up
-- Backfill booking quantity for databases created before bookings.quantity existed.

ALTER TABLE bookings ADD COLUMN IF NOT EXISTS quantity INT NOT NULL DEFAULT 1;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'booking_tickets'
    ) THEN
        UPDATE bookings b
        SET quantity = GREATEST(1, COALESCE(sub.c, 1))
        FROM (
            SELECT booking_id, COUNT(*)::int AS c
            FROM booking_tickets
            GROUP BY booking_id
        ) sub
        WHERE b.id = sub.booking_id;
    END IF;
END $$;
