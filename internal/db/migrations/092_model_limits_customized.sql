-- An operator edit to a model's context_length or max_output_tokens was
-- overwritten by the next discovery pass on every provider whose scan marks
-- those limits as live (OpenRouter, Ollama, LM Studio, KoboldCpp, Kimi,
-- NanoGPT, MiniMax): the upsert let the provider's value win with no pin,
-- while prices had price_customized. Set by any limit edit, honoured by the
-- upsert the way price_customized is, cleared (with the limits nulled so the
-- next scan refills them) by an explicit limits_customized=false.
ALTER TABLE models ADD COLUMN IF NOT EXISTS limits_customized BOOLEAN NOT NULL DEFAULT false;
