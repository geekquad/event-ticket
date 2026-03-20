CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE venues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    address TEXT NOT NULL,
    capacity INT NOT NULL,
    seat_map JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE performers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    date_time TIMESTAMPTZ NOT NULL,
    venue_id UUID NOT NULL REFERENCES venues(id),
    performer_id UUID NOT NULL REFERENCES performers(id),
    capacity INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_events_date_time ON events(date_time);

-- Tickets are pre-generated when an event is created (one per seat)
-- Status is AVAILABLE or BOOKED only in DB.
-- RESERVED state is held purely in Redis (TTL lock).
CREATE TABLE tickets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES events(id),
    seat_number VARCHAR(50),
    row VARCHAR(50),
    section VARCHAR(50),
    price NUMERIC(10,2) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'AVAILABLE',
    booking_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tickets_event_id ON tickets(event_id);
CREATE INDEX idx_tickets_status ON tickets(status);

CREATE TABLE bookings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    event_id UUID NOT NULL REFERENCES events(id),
    total_price NUMERIC(10,2) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'RESERVED',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_bookings_user_id ON bookings(user_id);
CREATE INDEX idx_bookings_event_id ON bookings(event_id);
CREATE INDEX idx_bookings_status ON bookings(status);

CREATE TABLE booking_tickets (
    booking_id UUID NOT NULL REFERENCES bookings(id),
    ticket_id UUID NOT NULL REFERENCES tickets(id),
    PRIMARY KEY (booking_id, ticket_id)
);

-- outcome field captures SUCCESS or FAILURE (including booking attempts that fail)
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type VARCHAR(50) NOT NULL,
    entity_id VARCHAR(255) NOT NULL,
    action VARCHAR(50) NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    outcome VARCHAR(10) NOT NULL DEFAULT 'SUCCESS',
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_logs_entity ON audit_logs(entity_type, entity_id);
CREATE INDEX idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);
