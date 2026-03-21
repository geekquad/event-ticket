-- +goose Up
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS quantity INT;
