package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"model_bss/internal/api"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplypg"
	"model_bss/internal/infra/supplysecurity"
)

// newSupplyRuntimeDeps wires the A routes whose facts and persistence already
// live in schema v36. B-owned submission/report transitions remain behind their
// existing trusted providers; no HTTP input is promoted to test evidence.
func newSupplyRuntimeDeps(db *gorm.DB, idem middleware.IdempotencyStore) (api.SupplyDeps, error) {
	raw, err := db.DB()
	if err != nil {
		return api.SupplyDeps{}, err
	}
	policy, err := supplypg.NewOfficialSourcePolicy(map[string][]string{
		"OpenAI":        {"https://openai.com", "https://platform.openai.com", "https://help.openai.com"},
		"Anthropic":     {"https://anthropic.com", "https://docs.anthropic.com"},
		"DeepSeek":      {"https://deepseek.com", "https://api-docs.deepseek.com"},
		"阿里云":           {"https://aliyun.com", "https://help.aliyun.com", "https://dashscope.aliyuncs.com"},
		"通义千问":          {"https://aliyun.com", "https://help.aliyun.com", "https://dashscope.aliyuncs.com"},
		"智谱AI":          {"https://bigmodel.cn", "https://open.bigmodel.cn"},
		"百度智能云":         {"https://cloud.baidu.com"},
		"Google":        {"https://ai.google.dev", "https://cloud.google.com"},
		"Google Gemini": {"https://ai.google.dev", "https://cloud.google.com"},
	})
	if err != nil {
		return api.SupplyDeps{}, err
	}
	deps, err := api.NewStandardModelSupplyDeps(api.StandardModelSupplyOptions{
		DB: raw, Schema: "public", IdemStore: idem, SourcePolicy: policy, Clock: time.Now,
	})
	if err != nil {
		return api.SupplyDeps{}, err
	}
	supplierStore, err := supplypg.NewSupplierStore("public", raw, supplypg.GORMModelTxFromContext, appendSupplyResourceLog)
	if err != nil {
		return api.SupplyDeps{}, err
	}
	deps.SupplierReader = supply.NewSupplierReader(supplierStore)
	deps.SupplierWriter = supply.NewSupplierService(supplierStore, time.Now)

	read := supplypg.ModelReadTransactions(raw)
	f12Store, err := supplypg.NewF12ReadStore("public", read)
	if err != nil {
		return api.SupplyDeps{}, err
	}
	deps.F12Reader = supply.NewF12Reader(f12Store)
	state, err := supplypg.NewResourceStateStore("public", appendSupplyResourceLog)
	if err != nil {
		return api.SupplyDeps{}, err
	}
	configs, err := supplypg.NewFormalConfigStore("public", appendSupplyFormalConfigLog)
	if err != nil {
		return api.SupplyDeps{}, err
	}
	report := func(ctx context.Context, tx *sql.Tx, resource *supply.Resource) (*supplypg.ReportEvidenceRef, error) {
		var jobID string
		err := tx.QueryRowContext(ctx, `SELECT test_job_id::text FROM public.test_report WHERE id=$1 AND resource_id=$2`, resource.LatestReportID, resource.ID).Scan(&jobID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, supply.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		valid, err := state.SelectedReportValid(ctx, tx, resource)
		if err != nil {
			return nil, err
		}
		return &supplypg.ReportEvidenceRef{
			ReportID: resource.LatestReportID, JobID: jobID, ResourceID: resource.ID,
			SupplierID: resource.SupplierID, StandardModelID: resource.StandardModelID,
			Region: resource.Region, OriginRowID: resource.OriginRowID,
			OriginRowVersion: resource.OriginRowVersion, TargetVersion: resource.TargetVersion,
			Sequence: resource.LatestReportSequence, Valid: valid,
		}, nil
	}
	resourceStore, err := supplypg.NewResourceReadStore("public", read, state.FactsLoader(report, configs.ResourceView))
	if err != nil {
		return api.SupplyDeps{}, err
	}
	deps.Resources = supply.NewResourceReader(resourceStore)
	formalRead, err := supplypg.NewFormalConfigReadStore("public", read, configs)
	if err != nil {
		return api.SupplyDeps{}, err
	}
	deps.FormalConfigReader = supply.NewFormalConfigReader(formalRead)
	sealer, err := supplysecurity.AESGCMSealerFromEnv()
	if err != nil {
		return api.SupplyDeps{}, err
	}
	if sealer != nil {
		formalWork := supplypg.NewFormalConfigWork(configs, supplypg.GORMModelTxFromContext)
		deps.FormalConfigs = supply.NewFormalConfigService(formalWork, sealer, supplysecurity.NewFormalURLPolicy(nil), time.Now)
	}
	deps.Notices = supply.NewNoticeReader(supplypg.NewNoticeReadStore(raw))
	noticeWork := supplypg.NewNoticeUnitOfWork(supplypg.GORMModelTxFromContext, appendSupplyNoticeLog)
	deps.NoticeWriter = supply.NewNoticeService(noticeWork, time.Now)
	planStore, err := supplypg.NewPlanStore("public", read, state, &supplypg.PlanHooks{
		Resolve: supplypg.GORMModelTxFromContext,
		Log:     appendSupplyResourceLog,
		Outbox:  appendSupplyOutbox,
		PendingConfiguration: func(ctx context.Context, tx *sql.Tx, resourceID string) (bool, error) {
			// Binding and revision live in the A control snapshot, not resource columns.
			resource, err := state.Current(ctx, tx, resourceID)
			if err != nil {
				return false, err
			}
			if resource.FormalConfigID == "" {
				return false, nil
			}
			config, err := configs.Current(ctx, tx, resource.SupplierID, resource.FormalConfigID)
			if errors.Is(err, supply.ErrNotFound) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			return config.Status != "ACTIVE" || config.Revision != resource.FormalConfigRevision, nil
		},
	})
	if err != nil {
		return api.SupplyDeps{}, err
	}
	// T-13：登记接入前必须存在有效测试证据（通过 + 已选为证据 + 指纹未变）。
	gate, gateErr := supplypg.NewAdmissionReportGate(raw, "public")
	if gateErr != nil {
		return api.SupplyDeps{}, gateErr
	}
	deps.ResourceAdmitter = api.WithAdmissionGate(deps.ResourceAdmitter, gate)
	deps.Plans = supply.NewPlanReader(planStore)
	deps.PlanWriter = supply.NewPlanMaintenanceService(planStore, time.Now)
	deps.PlanExecutor = supply.NewPlanExecutionService(supplypg.NewPlanExecutionWork(planStore, configs, report), time.Now)
	return deps, nil
}

func newSupplyPlanReminderStore(db *gorm.DB) (*supplypg.PlanReminderStore, error) {
	if db == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	raw, err := db.DB()
	if err != nil {
		return nil, err
	}
	state, err := supplypg.NewResourceStateStore("public", nil)
	if err != nil {
		return nil, err
	}
	plans, err := supplypg.NewPlanStore("public", supplypg.ModelReadTransactions(raw), state, nil)
	if err != nil {
		return nil, err
	}
	return supplypg.NewPlanReminderStore(raw, plans)
}

func appendSupplyResourceLog(ctx context.Context, tx *sql.Tx, entry supplypg.ResourceStateLog) error {
	if tx == nil || !json.Valid(entry.After) || (len(entry.Before) > 0 && !json.Valid(entry.Before)) {
		return supply.ErrReadContract
	}
	var before any
	if len(entry.Before) > 0 {
		before = string(entry.Before)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,operator_name_snapshot,"before","after",reason,request_id,created_at)
VALUES($1,$2,$3,$4,(SELECT name FROM public.internal_staff WHERE id=$4),$5::jsonb,$6::jsonb,$7,$8,$9)`, entry.EntityType, entry.EntityID, entry.Action, entry.ActorID, before, string(entry.After), entry.Reason, entry.RequestID, entry.OccurredAt.UTC())
	return err
}

func appendSupplyFormalConfigLog(ctx context.Context, tx *sql.Tx, entry supplypg.FormalConfigLog) error {
	return appendSupplyResourceLog(ctx, tx, supplypg.ResourceStateLog(entry))
}

func appendSupplyOutbox(ctx context.Context, tx *sql.Tx, entry supplypg.AdmissionOutboxEntry) (string, error) {
	if tx == nil || len(entry.Payload) == 0 || !json.Valid(entry.Payload) {
		return "", supply.ErrReadContract
	}
	var id string
	err := tx.QueryRowContext(ctx, `INSERT INTO public.event_outbox(event_type,payload,request_id,created_by,updated_by,created_at,updated_at)
VALUES($1,$2::jsonb,$3,$4,$4,$5,$5) RETURNING id::text`, entry.EventType, string(entry.Payload), entry.RequestID, entry.ActorID, entry.OccurredAt.UTC()).Scan(&id)
	return id, err
}

func appendSupplyNoticeLog(ctx context.Context, tx *sql.Tx, op supply.NoticeReadOperation) error {
	if tx == nil || op.After == nil {
		return supply.ErrReadContract
	}
	var before any
	if op.Before != nil {
		encoded, err := json.Marshal(op.Before)
		if err != nil {
			return err
		}
		before = string(encoded)
	}
	after, err := json.Marshal(op.After)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.operation_log(entity_type,entity_id,action,operator_id,operator_name_snapshot,"before","after",reason,request_id,created_at)
VALUES($1,$2,$3,$4,(SELECT name FROM public.internal_staff WHERE id=$4),$5::jsonb,$6::jsonb,$7,$8,$9)`, op.EntityType, op.EntityID, op.Action, op.ActorID, before, string(after), op.Reason, op.RequestID, op.OccurredAt.UTC())
	return err
}
