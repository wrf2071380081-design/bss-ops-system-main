-- F4 aliases and qualification metadata. Preserve legacy names, files and rows.
CREATE TABLE public.supplier_alias (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 supplier_id bigint NOT NULL REFERENCES public.supplier(id) ON DELETE RESTRICT,
 alias_name varchar(128) NOT NULL CHECK(alias_name=btrim(alias_name) AND alias_name<>''),
 normalized_name varchar(128) NOT NULL CHECK(normalized_name=btrim(normalized_name) AND normalized_name<>''),
 created_at timestamptz NOT NULL DEFAULT now(),
 CONSTRAINT uk_supplier_alias_name UNIQUE(supplier_id,normalized_name)
);
ALTER TABLE public.supplier_qualification
 ADD COLUMN qualification_type text NULL,
 ADD COLUMN valid_from date NULL,
 ADD COLUMN valid_to date NULL,
 ADD COLUMN review_status text NULL,
 ADD COLUMN revision bigint NOT NULL DEFAULT 1,
 ADD COLUMN created_by bigint NULL REFERENCES public.internal_staff(id),
 ADD CONSTRAINT ck_supplier_qualification_revision CHECK(revision>0),
 ADD CONSTRAINT ck_supplier_qualification_dates CHECK(valid_from IS NULL OR valid_to IS NULL OR valid_from<=valid_to),
 ADD CONSTRAINT ck_supplier_qualification_type CHECK(qualification_type IS NULL OR btrim(qualification_type)<>''),
 ADD CONSTRAINT ck_supplier_qualification_review CHECK(review_status IS NULL OR btrim(review_status)<>'');
COMMENT ON COLUMN public.supplier_qualification.review_status IS 'Authoritative review fact only; uploading/maintaining metadata does not approve a document';
-- D07 identity is not yet supplied: retain existing opaque file_ref unchanged.
-- Do not invent a file_id or copy old paths into one.
