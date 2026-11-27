ALTER TABLE supplier
  DROP COLUMN IF EXISTS contact_title,
  DROP COLUMN IF EXISTS contact_email,
  DROP COLUMN IF EXISTS default_settlement,
  DROP COLUMN IF EXISTS default_payment,
  DROP COLUMN IF EXISTS invoice_preference,
  DROP COLUMN IF EXISTS min_consumption,
  DROP COLUMN IF EXISTS data_training_policy,
  DROP COLUMN IF EXISTS log_retention_policy,
  DROP COLUMN IF EXISTS default_sla;
