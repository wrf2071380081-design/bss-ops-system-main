-- Forward-only: the catalog can predate this migration on a legacy version-78 database.
-- Refuse automatic rollback rather than delete pre-existing business records.
DO $$ BEGIN RAISE EXCEPTION 'Catalog business migration is forward-only; restore a verified backup for rollback'; END $$;
