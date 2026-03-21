-- Sample users (ids: DEFAULT gen_random_uuid())
INSERT INTO users (name, email) VALUES
    ('Alice', 'alice@example.com'),
    ('Bob',   'bob@example.com'),
    ('Carol', 'carol@example.com');

-- Sample venues
INSERT INTO venues (name, address, capacity) VALUES
    ('Madison Square Garden', '4 Penn Plaza, New York, NY 10001', 100),
    ('The Jazz Cellar',       '10 W Village St, New York, NY 10014', 5);

-- Sample events (capacity comes from venue)
INSERT INTO events (name, description, date_time, venue_id)
SELECT
    'Rock Night 2026',
    'An epic one-night rock concert with pyrotechnics and special guests.',
    '2026-06-15 20:00:00+00'::timestamptz,
    v.id
FROM venues v
WHERE v.name = 'Madison Square Garden';

INSERT INTO events (name, description, date_time, venue_id)
SELECT
    'Intimate Jazz Evening',
    'A small-venue jazz night with limited capacity. Book fast.',
    '2026-07-04 19:00:00+00'::timestamptz,
    v.id
FROM venues v
WHERE v.name = 'The Jazz Cellar';
