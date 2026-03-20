-- Sample users (ids: DEFAULT gen_random_uuid())
INSERT INTO users (name, email) VALUES
    ('Alice', 'alice@example.com'),
    ('Bob',   'bob@example.com'),
    ('Carol', 'carol@example.com');

-- Sample performers
INSERT INTO performers (name, description) VALUES
    ('The Rolling Bands', 'Classic rock legends on a world tour'),
    ('DJ Sparks',         'Electronic music producer and live performer');

-- Sample venues
INSERT INTO venues (name, address, capacity) VALUES
    ('Madison Square Garden', '4 Penn Plaza, New York, NY 10001', 20000),
    ('The Jazz Cellar',       '10 W Village St, New York, NY 10014', 50);

-- Sample events (link venues / performers by name)
INSERT INTO events (name, description, date_time, venue_id, performer_id, capacity)
SELECT
    'Rock Night 2026',
    'An epic one-night rock concert with pyrotechnics and special guests.',
    '2026-06-15 20:00:00+00'::timestamptz,
    v.id,
    p.id,
    100
FROM venues v
JOIN performers p ON p.name = 'The Rolling Bands'
WHERE v.name = 'Madison Square Garden';

INSERT INTO events (name, description, date_time, venue_id, performer_id, capacity)
SELECT
    'Intimate Jazz Evening',
    'A small-venue jazz night — only 5 spots available. Book fast.',
    '2026-07-04 19:00:00+00'::timestamptz,
    v.id,
    p.id,
    5
FROM venues v
JOIN performers p ON p.name = 'DJ Sparks'
WHERE v.name = 'The Jazz Cellar';

-- ============================================================
-- Tickets for Rock Night 2026
-- 100 tickets: 5 sections (A-E), 4 rows each, 5 seats per row
-- Price: 150.00
-- ============================================================
INSERT INTO tickets (event_id, seat_number, row, section, price)
SELECT
    e.id,
    s.n::text,
    r.n::text,
    sec,
    150.00
FROM events e
CROSS JOIN (VALUES ('A'), ('B'), ('C'), ('D'), ('E')) AS sections(sec)
CROSS JOIN generate_series(1, 4) AS r(n)
CROSS JOIN generate_series(1, 5) AS s(n)
WHERE e.name = 'Rock Night 2026';

-- ============================================================
-- Tickets for Intimate Jazz Evening
-- 5 tickets: section FLOOR, row 1, seats 1-5
-- Price: 75.00
-- ============================================================
INSERT INTO tickets (event_id, seat_number, row, section, price)
SELECT
    e.id,
    n::text,
    '1',
    'FLOOR',
    75.00
FROM events e
CROSS JOIN generate_series(1, 5) AS n
WHERE e.name = 'Intimate Jazz Evening';
