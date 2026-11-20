-- 000073 down
ALTER TABLE test_report
    DROP COLUMN IF EXISTS upload_test_type,
    DROP COLUMN IF EXISTS upload_test_name,
    DROP COLUMN IF EXISTS upload_tested_at,
    DROP COLUMN IF EXISTS upload_tester,
    DROP COLUMN IF EXISTS upload_note;
