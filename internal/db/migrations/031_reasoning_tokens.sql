-- Add reasoning_tokens column to request_logs.
-- Reasoning/thinking models (e.g. Claude extended thinking) report reasoning
-- tokens separately from visible text tokens in completion_tokens_details.
-- Without this column the proxy undercounts output tokens and TPS is wrong.
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS tokens_completion_reasoning INTEGER NOT NULL DEFAULT 0;
