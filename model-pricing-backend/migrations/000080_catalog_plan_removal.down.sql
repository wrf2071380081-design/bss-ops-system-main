-- Deletion markers and plan history must survive deployment rollback.
DO $$ BEGIN RAISE EXCEPTION 'Plan removal migration is forward-only; restore a verified backup for rollback'; END $$;
