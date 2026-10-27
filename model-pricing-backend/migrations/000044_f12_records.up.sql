-- F12 immutable contract/credit records. Existing columns and legacy rows remain.
-- Nullable metadata intentionally distinguishes legacy records from F12 evidence.
ALTER TABLE public.contract
  ADD COLUMN contract_key text NULL,
  ADD COLUMN document_name text NULL,
  ADD COLUMN document_no text NULL,
  ADD COLUMN file_id text NULL,
  ADD COLUMN source_flow_id text NULL,
  ADD COLUMN source_module_key text NULL,
  ADD COLUMN source_flow_revision bigint NULL,
  ADD COLUMN reason text NULL,
  ADD COLUMN created_by bigint NULL REFERENCES public.internal_staff(id);

ALTER TABLE public.credit_line
  ADD COLUMN document_name text NULL,
  ADD COLUMN document_no text NULL,
  ADD COLUMN file_id text NULL,
  ADD COLUMN source_flow_id text NULL,
  ADD COLUMN source_module_key text NULL,
  ADD COLUMN source_flow_revision bigint NULL,
  ADD COLUMN reason text NULL,
  ADD COLUMN created_by bigint NULL REFERENCES public.internal_staff(id);

-- Abort on conflicting historical versions; never renumber or discard history.
CREATE UNIQUE INDEX uk_contract_f12_version
  ON public.contract(contract_key,version) WHERE contract_key IS NOT NULL;
CREATE UNIQUE INDEX uk_credit_line_f12_version
  ON public.credit_line(subject_type,subject_id,direction,currency,version);

ALTER TABLE public.contract ADD CONSTRAINT ck_contract_f12_record CHECK (
  contract_key IS NULL OR (
    contract_key ~ '^[1-9][0-9]{0,18}$' AND version > 0
    AND subject_type IN ('SUPPLIER','CUSTOMER')
    AND coalesce(btrim(document_name),'') <> ''
    AND coalesce(btrim(document_no),'') <> ''
    AND coalesce(file_id,'') ~ '^f12:v1:[a-f0-9]{32}$'
    AND coalesce(btrim(reason),'') <> '' AND created_by IS NOT NULL
  )
);
ALTER TABLE public.credit_line ADD CONSTRAINT ck_credit_line_f12_record CHECK (
  document_name IS NULL OR (
    coalesce(btrim(document_name),'') <> ''
    AND coalesce(btrim(document_no),'') <> ''
    AND coalesce(file_id,'') ~ '^f12:v1:[a-f0-9]{32}$'
    AND coalesce(btrim(reason),'') <> '' AND created_by IS NOT NULL
    AND version > 0 AND amount > 0 AND currency ~ '^[A-Z]{3}$'
    AND ((subject_type='SUPPLIER' AND direction='SUPPLIER_TO_US')
      OR (subject_type='CUSTOMER' AND direction='US_TO_CUSTOMER'))
  )
);
ALTER TABLE public.contract ADD CONSTRAINT ck_contract_f12_source CHECK (
  (source_flow_id IS NULL AND source_module_key IS NULL AND source_flow_revision IS NULL)
  OR (coalesce(source_flow_id,'') ~ '^[1-9][0-9]{0,18}$'
    AND coalesce(btrim(source_module_key),'') <> '' AND source_flow_revision IS NOT NULL AND source_flow_revision > 0)
);
ALTER TABLE public.credit_line ADD CONSTRAINT ck_credit_line_f12_source CHECK (
  (source_flow_id IS NULL AND source_module_key IS NULL AND source_flow_revision IS NULL)
  OR (coalesce(source_flow_id,'') ~ '^[1-9][0-9]{0,18}$'
    AND coalesce(btrim(source_module_key),'') <> '' AND source_flow_revision IS NOT NULL AND source_flow_revision > 0)
);
COMMENT ON COLUMN public.contract.contract_key IS 'F12 immutable dossier identity; legacy NULL is not flow evidence';
COMMENT ON COLUMN public.contract.file_id IS 'Opaque private F12 attachment identity; never a filesystem path';
COMMENT ON COLUMN public.credit_line.file_id IS 'Opaque private F12 attachment identity; never a filesystem path';
COMMENT ON COLUMN public.contract.source_flow_revision IS 'Trusted flow decision reference captured under the write transaction';
COMMENT ON COLUMN public.credit_line.source_flow_revision IS 'Trusted flow decision reference captured under the write transaction';
