-- Sample users
INSERT INTO users (id, name, email) VALUES
    ('00000000-0000-0000-0000-000000000001', 'Alice', 'alice@example.com'),
    ('00000000-0000-0000-0000-000000000002', 'Bob',   'bob@example.com'),
    ('00000000-0000-0000-0000-000000000003', 'Carol', 'carol@example.com');

-- Sample performers
INSERT INTO performers (id, name, description) VALUES
    ('10000000-0000-0000-0000-000000000001', 'The Rolling Bands', 'Classic rock legends on a world tour'),
    ('10000000-0000-0000-0000-000000000002', 'DJ Sparks',         'Electronic music producer and live performer');

-- Sample venues
INSERT INTO venues (id, name, address, capacity) VALUES
    ('20000000-0000-0000-0000-000000000001', 'Madison Square Garden', '4 Penn Plaza, New York, NY 10001', 20000),
    ('20000000-0000-0000-0000-000000000002', 'The Jazz Cellar',       '10 W Village St, New York, NY 10014', 50);

-- Sample events
INSERT INTO events (id, name, description, date_time, venue_id, performer_id, capacity) VALUES
    (
        '30000000-0000-0000-0000-000000000001',
        'Rock Night 2026',
        'An epic one-night rock concert with pyrotechnics and special guests.',
        '2026-06-15 20:00:00+00',
        '20000000-0000-0000-0000-000000000001',
        '10000000-0000-0000-0000-000000000001',
        100
    ),
    (
        '30000000-0000-0000-0000-000000000002',
        'Intimate Jazz Evening',
        'A small-venue jazz night — only 5 spots available. Book fast.',
        '2026-07-04 19:00:00+00',
        '20000000-0000-0000-0000-000000000002',
        '10000000-0000-0000-0000-000000000002',
        5
    );

-- ============================================================
-- Tickets for Rock Night 2026 (event 30...001)
-- 100 tickets: 5 sections (A-E), 4 rows each, 5 seats per row
-- Price: 150.00
-- ============================================================

-- Section A, Row 1
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '1', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '1', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '1', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '1', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '1', 'A', 150.00);
-- Section A, Row 2
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '2', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '2', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '2', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '2', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '2', 'A', 150.00);
-- Section A, Row 3
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '3', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '3', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '3', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '3', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '3', 'A', 150.00);
-- Section A, Row 4
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '4', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '4', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '4', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '4', 'A', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '4', 'A', 150.00);

-- Section B, Row 1
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '1', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '1', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '1', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '1', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '1', 'B', 150.00);
-- Section B, Row 2
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '2', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '2', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '2', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '2', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '2', 'B', 150.00);
-- Section B, Row 3
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '3', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '3', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '3', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '3', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '3', 'B', 150.00);
-- Section B, Row 4
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '4', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '4', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '4', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '4', 'B', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '4', 'B', 150.00);

-- Section C, Row 1
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '1', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '1', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '1', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '1', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '1', 'C', 150.00);
-- Section C, Row 2
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '2', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '2', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '2', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '2', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '2', 'C', 150.00);
-- Section C, Row 3
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '3', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '3', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '3', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '3', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '3', 'C', 150.00);
-- Section C, Row 4
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '4', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '4', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '4', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '4', 'C', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '4', 'C', 150.00);

-- Section D, Row 1
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '1', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '1', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '1', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '1', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '1', 'D', 150.00);
-- Section D, Row 2
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '2', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '2', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '2', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '2', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '2', 'D', 150.00);
-- Section D, Row 3
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '3', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '3', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '3', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '3', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '3', 'D', 150.00);
-- Section D, Row 4
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '4', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '4', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '4', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '4', 'D', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '4', 'D', 150.00);

-- Section E, Row 1
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '1', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '1', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '1', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '1', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '1', 'E', 150.00);
-- Section E, Row 2
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '2', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '2', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '2', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '2', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '2', 'E', 150.00);
-- Section E, Row 3
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '3', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '3', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '3', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '3', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '3', 'E', 150.00);
-- Section E, Row 4
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '1', '4', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '2', '4', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '3', '4', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '4', '4', 'E', 150.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000001', '5', '4', 'E', 150.00);

-- ============================================================
-- Tickets for Intimate Jazz Evening (event 30...002)
-- 5 tickets: section FLOOR, row 1, seats 1-5
-- Price: 75.00
-- ============================================================
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000002', '1', '1', 'FLOOR', 75.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000002', '2', '1', 'FLOOR', 75.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000002', '3', '1', 'FLOOR', 75.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000002', '4', '1', 'FLOOR', 75.00);
INSERT INTO tickets (event_id, seat_number, row, section, price) VALUES ('30000000-0000-0000-0000-000000000002', '5', '1', 'FLOOR', 75.00);
