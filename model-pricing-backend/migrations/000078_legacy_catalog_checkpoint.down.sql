-- Version 78 can represent legacy catalog DDL. Do not cross this boundary on rollback.
DO $$ BEGIN RAISE EXCEPTION 'Legacy catalog checkpoint is forward-only; restore a verified backup for rollback'; END $$;
