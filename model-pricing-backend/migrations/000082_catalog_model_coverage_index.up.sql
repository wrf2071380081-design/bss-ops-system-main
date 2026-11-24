-- JSONB payloads remain immutable. This index serves exact model + coverage
-- lookup; a vendor run succeeding never implies every model was complete.
CREATE INDEX IF NOT EXISTS catalog_runs_model_coverage_idx
ON model_catalog.runs USING gin ((result->'models') jsonb_path_ops)
WHERE status='SUCCEEDED';
