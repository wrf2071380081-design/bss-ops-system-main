-- Canonical version 80; the same column existed at legacy local version 78.
ALTER TABLE model_catalog.plans ADD COLUMN IF NOT EXISTS deleted_at timestamptz;
COMMENT ON COLUMN model_catalog.plans.deleted_at IS 'Removed from plan lists and scheduling; execution history remains available';
