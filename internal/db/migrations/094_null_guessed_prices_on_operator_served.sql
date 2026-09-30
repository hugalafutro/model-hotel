-- A custom endpoint and the self-hosted server types (ollama, lmstudio,
-- koboldcpp) are no longer enriched from models.dev or the embedded catalogs:
-- they serve whatever their operator loaded under whatever name, so a price
-- looked up by that name was a guess. Discovery stops writing such prices, but
-- a scan that brings no price keeps the stored one, so the guesses already on
-- these rows would stay forever, still labelled as models.dev's. They become
-- NULL (unknown), their source labels with them. Prices the provider itself
-- reported or the operator set are kept, and so is every price on a row the
-- operator pinned. Idempotent.
UPDATE models m SET input_price_per_million = NULL, price_sources = m.price_sources - 'input'
    FROM providers p
    WHERE p.id = m.provider_id AND p.provider_type IN ('custom', 'ollama', 'lmstudio', 'koboldcpp')
      AND NOT m.price_customized AND m.price_sources->>'input' IN ('modelsdev', 'catalog');
UPDATE models m SET input_price_per_million_cache_hit = NULL, price_sources = m.price_sources - 'cache_hit'
    FROM providers p
    WHERE p.id = m.provider_id AND p.provider_type IN ('custom', 'ollama', 'lmstudio', 'koboldcpp')
      AND NOT m.price_customized AND m.price_sources->>'cache_hit' IN ('modelsdev', 'catalog');
UPDATE models m SET output_price_per_million = NULL, price_sources = m.price_sources - 'output'
    FROM providers p
    WHERE p.id = m.provider_id AND p.provider_type IN ('custom', 'ollama', 'lmstudio', 'koboldcpp')
      AND NOT m.price_customized AND m.price_sources->>'output' IN ('modelsdev', 'catalog');
UPDATE models m SET search_price_per_thousand = NULL, price_sources = m.price_sources - 'search'
    FROM providers p
    WHERE p.id = m.provider_id AND p.provider_type IN ('custom', 'ollama', 'lmstudio', 'koboldcpp')
      AND NOT m.price_customized AND m.price_sources->>'search' IN ('modelsdev', 'catalog');
