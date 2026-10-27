package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"model_bss/internal/api"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/supplyfiles"
	"model_bss/internal/infra/supplypg"
	"model_bss/internal/infra/supplysecurity"
)

// newSupplyRuntimeDeps wires the A routes whose facts and persistence already
// live in schema v36. B-owned submission/report transitions remain behind their
// existing trusted providers; no HTTP input is promoted to test evidence.
func newSupplyRuntimeDeps(db *gorm.DB, idem middleware.IdempotencyStore, workflow ...api.SupplyResourceWorkflowProviders) (api.SupplyDeps, error) {
	if len(workflow) > 1 {
		return api.SupplyDeps{}, supply.ErrReadContract
	}
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
	if qualificationRoot := strings.TrimSpace(os.Getenv("MODEL_BSS_SUPPLIER_QUALIFICATION_STORE_DIR")); qualificationRoot != "" {
		qualificationFiles, err := supplyfiles.NewQualificationFileStore(qualificationRoot)
		if err != nil {
			return api.SupplyDeps{}, err
		}
		deps.SupplierQualificationFiles = supply.NewSupplierQualificationService(supplierStore, qualificationFiles, time.Now)
		cleaner, err := supplypg.NewSupplierQualificationAttachmentCleaner(raw, "public", qualificationFiles)
		if err != nil {
			log.Printf("supplier qualification orphan cleanup unavailable: %v", err)
		} else {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if _, cleanupErr := cleaner.CleanupUnreferenced(cleanupCtx); cleanupErr != nil {
				log.Printf("supplier qualification orphan cleanup skipped: %v", cleanupErr)
			}
			cancel()
		}
	}

	read := supplypg.ModelReadTransactions(raw)
	mergePreviewStore, err := supplypg.NewSupplierMergePreviewStore("public", supplierStore, read)
	if err != nil {
		return api.SupplyDeps{}, err
	}
	deps.SupplierMergePreviewReader = supply.NewSupplierMergePreviewReader(mergePreviewStore)
	f12Store, err := supplypg.NewF12ReadStore("public", supplypg.F12ReadTransactions(raw))
	if err != nil {
		return api.SupplyDeps{}, err
	}
	deps.F12Reader = supply.NewF12Reader(f12Store)
	f12Flow, err := supplypg.NewNativeSupplierF12(raw, "public")
	if err != nil {
		return api.SupplyDeps{}, err
	}
	deps.F12Readiness = supply.NewF12ReadinessService(f12Flow, f12Store)
	var f12Files supply.F12AttachmentBlobStore
	var f12AttachmentVerifier supplypg.F12AttachmentVerifier
	f12AttachmentRoot := strings.TrimSpace(os.Getenv("MODEL_BSS_F12_ATTACHMENT_STORE_DIR"))
	if f12AttachmentRoot != "" {
		fileStore, err := supplyfiles.NewF12AttachmentFileStore(f12AttachmentRoot)
		if err != nil {
			return api.SupplyDeps{}, err
		}
		f12Files = fileStore
		f12AttachmentVerifier = supplypg.F12AttachmentBlobVerifier{Files: fileStore}
	}
	f12RecordStore, err := supplypg.NewF12RecordStore("public", supplypg.GORMModelTxFromContext, f12Flow, f12AttachmentVerifier)
	if err != nil {
		return api.SupplyDeps{}, err
	}
	deps.F12Writer = supply.NewF12RecordService(f12RecordStore, time.Now)
	if f12Files != nil {
		f12Attachments, err := supplypg.NewF12AttachmentService(raw, "public", f12RecordStore, f12Files)
		if err != nil {
			return api.SupplyDeps{}, err
		}
		deps.F12Attachments = f12Attachments
		if _, cleanupErr := f12Attachments.CleanupUnreferenced(context.Background()); cleanupErr != nil {
			log.Printf("F12 attachment orphan cleanup skipped: %v", cleanupErr)
		}
	}
	state, err := supplypg.NewResourceStateStore("public", appendSupplyResourceLog)
	if err != nil {
		return api.SupplyDeps{}, err
	}
	configs, err := supplypg.NewFormalConfigStore("public", appendSupplyFormalConfigLog)
	if err != nil {
		return api.SupplyDeps{}, err
	}
	report := func(ctx context.Context, tx *sql.Tx, resource *supply.Resource) (*supplypg.ReportEvidenceRef, error) {
		ref, err := (&supplypg.NativeHandling{DB: raw, State: state}).SelectedReport(ctx, tx, resource)
		if err != nil {
			return nil, err
		}
		if ref != nil {
			return ref, nil
		}
		var jobID string
		err = tx.QueryRowContext(ctx, `SELECT test_job_id::text FROM public.test_report WHERE id=$1 AND resource_id=$2`, resource.LatestReportID, resource.ID).Scan(&jobID)
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
	resourceStore, err := supplypg.NewResourceRepository("public", read, state.FactsLoader(report, configs.ResourceView))
	if err != nil {
		return api.SupplyDeps{}, err
	}
	deps.Resources = supply.NewResourceReader(resourceStore)
	reportReader, err := supplypg.NewResourceReportReader("public", read)
	if err != nil {
		return api.SupplyDeps{}, err
	}
	deps.ResourceReports = reportReader
	formalRead, err := supplypg.NewFormalConfigReadStore("public", read, configs)
	if err != nil {
		return api.SupplyDeps{}, err
	}
	deps.FormalConfigReader = supply.NewFormalConfigReader(formalRead)
	formalWork := supplypg.NewFormalConfigWork(configs, supplypg.GORMModelTxFromContext)
	deps.FormalConfigs = supply.NewFormalConfigService(formalWork, supplysecurity.NewFormalURLPolicy(nil), time.Now)
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
	providers := api.NewNativeSupplyWorkflowProviders(raw, state)
	if len(workflow) == 1 {
		providers = workflow[0]
	}
	// The transaction work validates selected, version-matched report evidence;
	// no standalone read outside that transaction can authorize admission.
	api.WireSupplyResourceWorkflow(&deps, state, configs, supplypg.GORMModelTxFromContext, report, appendSupplyOutbox, providers)
	deps.ResourceStages = supplypg.NewResourceStageWriter(state, supplypg.GORMModelTxFromContext, deps.ResourceConfigBinder, deps.ResourceAdmitter)
	deps.Plans = supply.NewPlanReader(planStore)
	planWriter := supply.NewPlanMaintenanceService(planStore, time.Now)
	deps.PlanWriter = planWriter
	deps.PricePlanWriter = planWriter
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
