-- +goose Up
-- Remove legacy seat-booking coupling.

DROP TABLE IF EXISTS booking_tickets;
DROP TABLE IF EXISTS tickets;
