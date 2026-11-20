-- 000073: 外部上传的专项报告补元数据（对齐原型「上传专项报告」表单）
ALTER TABLE test_report
    ADD COLUMN IF NOT EXISTS upload_test_type varchar(32),
    ADD COLUMN IF NOT EXISTS upload_test_name varchar(64),
    ADD COLUMN IF NOT EXISTS upload_tested_at timestamptz,
    ADD COLUMN IF NOT EXISTS upload_tester    varchar(64),
    ADD COLUMN IF NOT EXISTS upload_note      text;
