-- Add resolved_model_id column to track which model actually served
-- a failover group request. For hotel/ requests, model_id stores the
-- group name (e.g. "hotel/mygroup") while resolved_model_id stores
-- the actual provider/model that responded (e.g. "gemma3:12b").
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS resolved_model_id TEXT DEFAULT '';
