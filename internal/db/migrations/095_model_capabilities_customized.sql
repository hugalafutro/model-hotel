-- A custom provider's models carry only the capabilities its own listing
-- reports (for a plain OpenAI-compatible /models that is streaming alone), and
-- the operator fills in the rest by hand. capabilities_customized pins that
-- edit: set by any capability edit, honoured by the discovery upsert the way
-- price_customized and limits_customized are, cleared by an explicit
-- capabilities_customized=false so the next scan writes the listing's reading
-- again. The API accepts the edit for custom providers only.
ALTER TABLE models ADD COLUMN IF NOT EXISTS capabilities_customized BOOLEAN NOT NULL DEFAULT false;
