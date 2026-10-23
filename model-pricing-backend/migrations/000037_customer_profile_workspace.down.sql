ALTER TABLE customer_profile
    DROP CONSTRAINT IF EXISTS ck_customer_region,
    DROP CONSTRAINT IF EXISTS ck_customer_contract_status;

ALTER TABLE customer_profile
    DROP COLUMN IF EXISTS region,
    DROP COLUMN IF EXISTS settlement_currency,
    DROP COLUMN IF EXISTS tax_rate,
    DROP COLUMN IF EXISTS contract_status;
