-- 000071 down
ALTER TABLE submission_row
    DROP COLUMN IF EXISTS connectivity_status,
    DROP COLUMN IF EXISTS connectivity_note;
