-- Canonical version 79; pre-merge local databases applied this schema at 77.
-- Re-entrant DDL preserves existing catalog rows without rewriting migration history.
-- Official catalog business records. DSH execution state remains outside BSS.
CREATE SCHEMA IF NOT EXISTS model_catalog;
CREATE TABLE IF NOT EXISTS model_catalog.runs (
 id uuid PRIMARY KEY, vendor text NOT NULL CHECK(vendor IN ('deepseek','openai','google','aliyun','zhipu','anthropic')),
 trigger text NOT NULL CHECK(trigger IN ('MANUAL','SCHEDULED')),
 scope text NOT NULL CHECK(scope IN ('manual','personal','platform')), owner_staff_id bigint NOT NULL,
 origin_key text NOT NULL, started_at timestamptz NOT NULL DEFAULT clock_timestamp(), finished_at timestamptz,
 status text NOT NULL CHECK(status IN ('RUNNING','SUCCEEDED','FAILED')), error text, result jsonb,
 revision bigint NOT NULL DEFAULT 1, baseline_id uuid REFERENCES model_catalog.runs(id)
);
CREATE UNIQUE INDEX IF NOT EXISTS catalog_active_scope ON model_catalog.runs(vendor,scope,owner_staff_id) WHERE status='RUNNING';
CREATE INDEX IF NOT EXISTS catalog_vendor_history ON model_catalog.runs(vendor,started_at DESC,id DESC);
CREATE TABLE IF NOT EXISTS model_catalog.comparison_baselines (
 run_id uuid NOT NULL REFERENCES model_catalog.runs(id), staff_id bigint NOT NULL,
 baseline_id uuid REFERENCES model_catalog.runs(id), fixed_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(run_id,staff_id)
);
CREATE TABLE IF NOT EXISTS model_catalog.requests (
 vendor text NOT NULL, request_key text NOT NULL, run_id uuid NOT NULL REFERENCES model_catalog.runs(id),
 PRIMARY KEY(vendor,request_key)
);
CREATE TABLE IF NOT EXISTS model_catalog.decisions (
 run_id uuid NOT NULL REFERENCES model_catalog.runs(id), model_id text NOT NULL, staff_id bigint NOT NULL,
 decision text NOT NULL CHECK(decision IN ('APPROVED','REJECTED')), published_model jsonb,
 approved_at timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(run_id,model_id)
);
CREATE TABLE IF NOT EXISTS model_catalog.review_drafts (
 run_id uuid NOT NULL REFERENCES model_catalog.runs(id), model_id text NOT NULL, staff_id bigint NOT NULL,
 overrides jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(run_id,model_id,staff_id)
);
CREATE TABLE IF NOT EXISTS model_catalog.write_receipts (
 run_id uuid NOT NULL REFERENCES model_catalog.runs(id), model_id text NOT NULL, request_key text NOT NULL,
 staff_id bigint NOT NULL, result jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(run_id,model_id,request_key)
);
CREATE TABLE IF NOT EXISTS model_catalog.evidence (
 evidence_id text PRIMARY KEY, vendor text NOT NULL, source_url text NOT NULL,
 sha256 text NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'), bytes integer NOT NULL CHECK(bytes>=0),
 transport text NOT NULL CHECK(transport IN ('HTTP','MCP','BROWSER')), rendered boolean NOT NULL DEFAULT false,
 observed_at timestamptz NOT NULL, parser_version text NOT NULL, storage_path text NOT NULL,
 model_id text, run_id text, created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS catalog_evidence_run ON model_catalog.evidence(run_id);
CREATE TABLE IF NOT EXISTS model_catalog.plans (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), legacy_id text UNIQUE,
 scope text NOT NULL CHECK(scope IN ('personal','platform')), account_id bigint NOT NULL, staff_id bigint NOT NULL,
 title text NOT NULL DEFAULT '官方模型抓取', vendors jsonb NOT NULL DEFAULT '["deepseek","openai","google","aliyun","zhipu","anthropic"]',
 active boolean NOT NULL DEFAULT false, local_time text NOT NULL DEFAULT '09:00', recurrence text NOT NULL DEFAULT 'daily',
 valid_from date, valid_until date, revision bigint NOT NULL DEFAULT 1,
 created_by bigint NOT NULL, updated_by bigint NOT NULL, updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(valid_until IS NULL OR valid_from IS NULL OR valid_until>=valid_from)
);
CREATE TABLE IF NOT EXISTS model_catalog.plan_occurrences (
 plan_id uuid NOT NULL REFERENCES model_catalog.plans(id), due_at timestamptz NOT NULL,
 request_id uuid NOT NULL DEFAULT gen_random_uuid(), generation bigint NOT NULL DEFAULT 1, state text NOT NULL DEFAULT 'RUNNING', result jsonb,
 lease_until timestamptz NOT NULL DEFAULT now()+interval '5 minutes', created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(plan_id,due_at), UNIQUE(request_id)
);
CREATE TABLE IF NOT EXISTS model_catalog.events (
 id bigserial PRIMARY KEY, kind text NOT NULL, staff_id bigint, run_id uuid, model_id text, detail jsonb,
 at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE OR REPLACE FUNCTION model_catalog.schedule_occurrence(rule text, clock_time time, after_at timestamptz, start_day date, end_day date)
RETURNS timestamptz LANGUAGE plpgsql STABLE AS $$
DECLARE
  local_now timestamp := after_at AT TIME ZONE 'Asia/Shanghai';
  first_day date := greatest(local_now::date, start_day);
  d date := first_day;
  kind text := split_part(rule, ':', 1);
  arg text := split_part(rule, ':', 2);
  candidate timestamp;
  minutes integer;
BEGIN
  IF kind = 'once' THEN
    d := arg::date;
    candidate := d + clock_time;
  ELSIF kind = 'interval' THEN
    minutes := arg::integer;
    IF minutes < 1 THEN RETURN NULL; END IF;
    candidate := coalesce(start_day,date '2000-01-03') + clock_time;
    candidate := candidate + greatest(0,floor(extract(epoch from (local_now-candidate))/(minutes*60))+1) * (minutes * interval '1 minute');
  ELSE
    d := first_day;
    WHILE d <= first_day + 2922 AND (end_day IS NULL OR d <= end_day) LOOP
      IF (CASE kind WHEN 'daily' THEN true WHEN 'weekly' THEN extract(isodow from d)=arg::int
          WHEN 'biweekly' THEN extract(isodow from d)=arg::int AND mod((d-date_trunc('week',coalesce(start_day,date '2000-01-03')::timestamp)::date)/7,2)=0
          WHEN 'monthly' THEN extract(day from d)=least(arg::int,extract(day from date_trunc('month',d)+interval '1 month - 1 day'))
          WHEN 'yearly' THEN to_char(d,'MM-DD')=arg ELSE false END) AND d + clock_time > local_now THEN
        candidate := d + clock_time;
        EXIT;
      END IF;
      d := d+1;
    END LOOP;
  END IF;
  IF candidate > local_now AND candidate::date >= first_day AND (end_day IS NULL OR candidate::date <= end_day) THEN
    RETURN candidate AT TIME ZONE 'Asia/Shanghai';
  END IF;
  RETURN NULL;
END;
$$;

ALTER TABLE model_catalog.plans ADD COLUMN IF NOT EXISTS next_at timestamptz;
