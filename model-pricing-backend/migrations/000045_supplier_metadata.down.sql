DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM public.supplier_alias)
 OR EXISTS(SELECT 1 FROM public.supplier_qualification WHERE qualification_type IS NOT NULL OR valid_from IS NOT NULL OR valid_to IS NOT NULL OR review_status IS NOT NULL OR created_by IS NOT NULL OR revision<>1) THEN
  RAISE EXCEPTION 'F4 v45 data exists; export/migrate aliases and qualification metadata before rollback';
 END IF;
END $$;
DROP TABLE public.supplier_alias;
ALTER TABLE public.supplier_qualification
 DROP CONSTRAINT ck_supplier_qualification_revision,
 DROP CONSTRAINT ck_supplier_qualification_dates,
 DROP CONSTRAINT ck_supplier_qualification_type,
 DROP CONSTRAINT ck_supplier_qualification_review,
 DROP COLUMN qualification_type,DROP COLUMN valid_from,DROP COLUMN valid_to,
 DROP COLUMN review_status,DROP COLUMN revision,DROP COLUMN created_by;
