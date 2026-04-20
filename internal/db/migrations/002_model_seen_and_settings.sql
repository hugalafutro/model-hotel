ALTER TABLE models ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ DEFAULT now();
ALTER TABLE models ADD COLUMN IF NOT EXISTS owned_by TEXT;
ALTER TABLE models ADD COLUMN IF NOT EXISTS context_length INTEGER;
ALTER TABLE models ADD COLUMN IF NOT EXISTS input_price_per_million REAL;
ALTER TABLE models ADD COLUMN IF NOT EXISTS output_price_per_million REAL;

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now()
);

INSERT INTO settings (key, value) VALUES
    ('discovery_interval', '6h'),
    ('discovery_on_startup', 'true'),
    ('discovery_on_provider_create', 'true'),
    ('theme', 'dark')
ON CONFLICT (key) DO NOTHING;