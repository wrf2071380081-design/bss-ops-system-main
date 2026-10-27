-- Refuse destructive rollback once F12 business data exists. Export/migrate it first.
DO $$ BEGIN
  IF EXISTS(SELECT 1 FROM public.contract WHERE contract_key IS NOT NULL
       OR document_name IS NOT NULL OR source_flow_id IS NOT NULL)
    OR EXISTS(SELECT 1 FROM public.credit_line WHERE document_name IS NOT NULL
       OR file_id IS NOT NULL OR source_flow_id IS NOT NULL) THEN
    RAISE EXCEPTION 'F12 v44 data exists; migrate/export records and private attachments before rollback';
  END IF;
END $$;
DROP INDEX public.uk_contract_f12_version;
DROP INDEX public.uk_credit_line_f12_version;
ALTER TABLE public.contract DROP CONSTRAINT ck_contract_f12_record, DROP CONSTRAINT ck_contract_f12_source;
ALTER TABLE public.credit_line DROP CONSTRAINT ck_credit_line_f12_record, DROP CONSTRAINT ck_credit_line_f12_source;
ALTER TABLE public.contract
  DROP COLUMN contract_key, DROP COLUMN document_name, DROP COLUMN document_no,
  DROP COLUMN file_id, DROP COLUMN source_flow_id, DROP COLUMN source_module_key,
  DROP COLUMN source_flow_revision, DROP COLUMN reason, DROP COLUMN created_by;
ALTER TABLE public.credit_line
  DROP COLUMN document_name, DROP COLUMN document_no, DROP COLUMN file_id,
  DROP COLUMN source_flow_id, DROP COLUMN source_module_key,
  DROP COLUMN source_flow_revision, DROP COLUMN reason, DROP COLUMN created_by;
