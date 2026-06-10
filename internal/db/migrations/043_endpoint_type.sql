-- Add endpoint_type column to request_logs.
-- The proxy now serves multimodal endpoints (embeddings, image generation,
-- text-to-speech, speech-to-text) alongside chat completions. This column
-- distinguishes the endpoint family per request for routing/metering
-- analytics. Values: 'chat', 'embeddings', 'image', 'tts', 'stt'.
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS endpoint_type TEXT NOT NULL DEFAULT 'chat';
