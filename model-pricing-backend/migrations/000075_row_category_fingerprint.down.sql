-- 000075 down
ALTER TABLE submission_row
    DROP COLUMN IF EXISTS connectivity_fingerprint,
    DROP COLUMN IF EXISTS stress_fingerprint;
