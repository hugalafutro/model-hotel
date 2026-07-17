-- Modality classification cleanup: input_modalities/output_modalities become
-- the source of truth and models.modality becomes a *derived endpoint class*
-- with a closed vocabulary (chat, embedding, rerank, image, video, tts, stt).
-- The legacy column mixed input-derived words for chat models ("vision",
-- "audio", "multimodal"), output/endpoint words for non-chat models ("image",
-- "embedding") and raw arrow strings ("text+image->text") from OpenRouter.
--
-- This backfill seeds the arrays from the legacy vocabulary so nothing waits
-- on a rediscovery, then rewrites every row's modality to the derived class.
-- internal/provider/model_class.go is the Go equivalent that governs all
-- newly discovered models; live discovery data overwrites this heuristic
-- backfill on the next scan (live values always win).

-- 1) Arrow-notation modality strings describe both sides; parse them into
--    whichever arrays are still empty.
UPDATE models
SET input_modalities = to_jsonb(string_to_array(replace(replace(split_part(lower(modality), '->', 1), ' ', ''), ',', '+'), '+'))
WHERE modality LIKE '%->%'
  AND (input_modalities IS NULL OR input_modalities = '[]'::jsonb);

UPDATE models
SET output_modalities = to_jsonb(string_to_array(replace(replace(split_part(lower(modality), '->', 2), ' ', ''), ',', '+'), '+'))
WHERE modality LIKE '%->%'
  AND (output_modalities IS NULL OR output_modalities = '[]'::jsonb);

-- 2) Legacy single-word modalities seed empty arrays. Input words described
--    what a chat model accepts ("vision" = image input); endpoint words imply
--    their class-default arrays (tts speaks audio, stt hears audio).
UPDATE models
SET input_modalities = CASE lower(COALESCE(modality, ''))
		WHEN 'vision' THEN '["text","image"]'::jsonb
		WHEN 'audio' THEN '["text","audio"]'::jsonb
		WHEN 'video' THEN '["text","video"]'::jsonb
		WHEN 'multimodal' THEN '["text","image","audio"]'::jsonb
		WHEN 'stt' THEN '["audio"]'::jsonb
		ELSE '["text"]'::jsonb
	END
WHERE (input_modalities IS NULL OR input_modalities = '[]'::jsonb);

UPDATE models
SET output_modalities = CASE lower(COALESCE(modality, ''))
		WHEN 'embedding' THEN '["embedding"]'::jsonb
		WHEN 'rerank' THEN '["rerank"]'::jsonb
		WHEN 'image' THEN '["image"]'::jsonb
		WHEN 'tts' THEN '["audio"]'::jsonb
		ELSE '["text"]'::jsonb
	END
WHERE (output_modalities IS NULL OR output_modalities = '[]'::jsonb);

-- 3) Rewrite modality to the derived endpoint class. Explicit endpoint
--    classes are kept; everything else derives from output_modalities in the
--    same precedence order as DeriveModelClass (rerank > embedding > text →
--    chat > video > image > audio-only → tts, unknown → chat).
UPDATE models
SET modality = CASE
		WHEN lower(COALESCE(modality, '')) IN ('embedding', 'rerank', 'image', 'tts', 'stt') THEN lower(modality)
		WHEN output_modalities ? 'rerank' THEN 'rerank'
		WHEN output_modalities ? 'embedding' THEN 'embedding'
		WHEN output_modalities ? 'text' THEN 'chat'
		WHEN output_modalities ? 'video' THEN 'video'
		WHEN output_modalities ? 'image' THEN 'image'
		WHEN output_modalities ? 'audio' THEN 'tts'
		ELSE 'chat'
	END;
