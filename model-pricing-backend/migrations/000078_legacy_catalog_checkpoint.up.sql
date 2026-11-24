-- Compatibility checkpoint. The file driver must find the database's current version.
-- Legacy local databases used 77/78 for catalog tables/plan removal; fresh databases
-- use canonical 77 for schema repair. Actual convergent catalog DDL follows in 79/80.
-- No existing version record or business data is rewritten here.
SELECT 1;
