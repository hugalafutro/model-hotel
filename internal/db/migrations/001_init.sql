-- Providers
CREATE TABLE IF NOT EXISTS providers (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    base_url      TEXT NOT NULL,
    encrypted_key BYTEA NOT NULL,
    key_nonce     BYTEA NOT NULL,
    enabled       BOOLEAN DEFAULT true,
    last_discovered_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now()
);

-- Discovered Models
CREATE TABLE IF NOT EXISTS models (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id   UUID REFERENCES providers(id) ON DELETE CASCADE,
    model_id      TEXT NOT NULL,
    display_name  TEXT,
    capabilities  JSONB,
    params        JSONB,
    enabled       BOOLEAN DEFAULT true,
    created_at    TIMESTAMPTZ DEFAULT now(),
    -- The UNIQUE constraint below implicitly creates an index on (provider_id, model_id),
    -- which also covers queries filtering by provider_id alone. No separate index needed.
    UNIQUE(provider_id, model_id)
);

-- Failover Groups: same model from multiple providers
CREATE TABLE IF NOT EXISTS model_failover_groups (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    display_model TEXT NOT NULL UNIQUE,
    priority_order JSONB,
    created_at    TIMESTAMPTZ DEFAULT now()
);

-- Usage Logs (NO prompts, NO responses)
CREATE TABLE IF NOT EXISTS request_logs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id   UUID REFERENCES providers(id),
    model_id      TEXT,
    request_id    TEXT,
    status_code   INT,
    latency_ms    INT,
    tokens_prompt INT,
    tokens_completion INT,
    streaming     BOOLEAN,
    error_message TEXT,
    created_at    TIMESTAMPTZ DEFAULT now()
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_request_logs_created_at ON request_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_request_logs_model_id ON request_logs(model_id);
CREATE INDEX IF NOT EXISTS idx_request_logs_provider_id ON request_logs(provider_id);

-- Proxy API Keys (for clients connecting to this proxy)
CREATE TABLE IF NOT EXISTS proxy_keys (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash      TEXT NOT NULL UNIQUE,
    name          TEXT,
    created_at    TIMESTAMPTZ DEFAULT now()
);
