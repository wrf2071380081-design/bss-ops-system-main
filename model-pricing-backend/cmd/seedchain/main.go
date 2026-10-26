// Command seedchain 驱动 A 的供应侧服务，跑通一条完整链路：
// 供应商申报行 → 资源创建 → mock 测试（失败/通过）→ 正式配置绑定 → 登记接入。
//
// 只用于本地/测试库；它直接写 submission / onboarding / test_report，
// 再调用 A 的 ResourceCreation / TestEvidence / ResourceAdmission 服务生成
// 带控制快照的资源，供模型广场读取。
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/config"
	"model_bss/internal/infra/supplypg"
)

type seedLog struct{ db *sql.DB }

func (l seedLog) append(ctx context.Context, tx *sql.Tx, e supplypg.ResourceStateLog) error {
	var before any
	if len(e.Before) > 0 {
		before = string(e.Before)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO public.operation_log
(entity_type,entity_id,action,operator_id,operator_name_snapshot,"before","after",reason,request_id,created_at)
VALUES($1,$2,$3,$4,(SELECT name FROM public.internal_staff WHERE id=$4),$5::jsonb,$6::jsonb,$7,$8,$9)`,
		e.EntityType, e.EntityID, e.Action, e.ActorID, before, string(e.After), e.Reason, e.RequestID, e.OccurredAt.UTC())
	return err
}

type seedURLPolicy struct{}

func (seedURLPolicy) ValidateFormalURL(context.Context, string) error { return nil }

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.Database.User, cfg.Database.Password, cfg.Database.Host, cfg.Database.Port, cfg.Database.DBName, cfg.Database.SSLMode)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()

	supplierID := flag.Int64("supplier", 1001, "supplier id")
	modelID := flag.Int64("model", 1, "standard model id")
	region := flag.String("region", "CN", "CN or OVERSEAS")
	coefficient := flag.String("coefficient", "0.6000", "resource coefficient")
	supplierModel := flag.String("supplier-model", "deepseek-chat", "supplier model name")
	flag.Parse()
	const (
		actorID    = 1
		rowVersion = 2
		targetVer  = 1
		flowVer    = 1
	)

	var supplierExists, modelExists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM public.supplier WHERE id=$1`, *supplierID).Scan(&supplierExists); err != nil {
		log.Fatal(err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM public.standard_model WHERE id=$1`, *modelID).Scan(&modelExists); err != nil {
		log.Fatal(err)
	}
	if supplierExists == 0 || modelExists == 0 {
		log.Fatalf("seed requires supplier %d and standard_model %d", supplierID, modelID)
	}

	// B-side buyer assignment (formal config requires an assigned buyer).
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.assignment_log(entity_type,entity_id,field,old_staff_id,new_staff_id,changed_by)
SELECT 'supplier',$1,'buyer_id',NULL,$2,$2
WHERE NOT EXISTS (SELECT 1 FROM public.assignment_log WHERE entity_type='supplier' AND entity_id=$1 AND field='buyer_id')`, *supplierID, actorID); err != nil {
		log.Fatalf("assignment log: %v", err)
	}

	var submissionID, rowID, flowID, nodeID int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO public.submission(supplier_id,kind,region,status,submitted_by,reason)
VALUES($1,'MODEL_NEW',$2,'PROCESSING',$3,'seedchain 演示链路') RETURNING id`, *supplierID, *region, actorID).Scan(&submissionID); err != nil {
		log.Fatalf("submission: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO public.submission_row(submission_id,standard_model_id,supplier_model_name,coefficient,row_status)
VALUES($1,$2,$3,$4,'PENDING') RETURNING id`, submissionID, *modelID, *supplierModel, *coefficient).Scan(&rowID); err != nil {
		log.Fatalf("submission row: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO public.onboarding_flow(kind,owner_id,status,current_node)
VALUES('SUPPLIER',$1,'PROCESSING','CONFIG') RETURNING id`, *supplierID).Scan(&flowID); err != nil {
		log.Fatalf("onboarding flow: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO public.onboarding_node(flow_id,node_key,status,assignee_id)
VALUES($1,'CONFIG','IN_PROGRESS',$2) RETURNING id`, flowID, actorID).Scan(&nodeID); err != nil {
		log.Fatalf("onboarding node: %v", err)
	}
	log.Printf("submission=%d row=%d flow=%d node=%d", submissionID, rowID, flowID, nodeID)

	rowIDStr := fmt.Sprint(rowID)
	flowIDStr := fmt.Sprint(flowID)
	nodeIDStr := fmt.Sprint(nodeID)
	actorStr := fmt.Sprint(actorID)
	supplierStr := fmt.Sprint(*supplierID)

	logger := seedLog{db: db}
	state, err := supplypg.NewResourceStateStore("public", logger.append)
	if err != nil {
		log.Fatal(err)
	}
	configs, err := supplypg.NewFormalConfigStore("public", func(ctx context.Context, tx *sql.Tx, e supplypg.FormalConfigLog) error {
		return logger.append(ctx, tx, supplypg.ResourceStateLog(e))
	})
	if err != nil {
		log.Fatal(err)
	}
	resolve := func(ctx context.Context) (*sql.Tx, error) { return tx, nil }

	creationWork := supplypg.NewResourceCreationWork(state, resolve, supplypg.ResourceCreationHooks{
		LockSource: func(ctx context.Context, tx *sql.Tx, row string, version uint64) (*supply.ResourceCreationContext, error) {
			var mid, vendor, coefficient string
			if err := tx.QueryRowContext(ctx, `SELECT standard_model_id::text,supplier_model_name,coefficient::text
FROM public.submission_row WHERE id=$1 FOR UPDATE`, row).Scan(&mid, &vendor, &coefficient); err != nil {
				return nil, err
			}
			return &supply.ResourceCreationContext{Proof: &supply.SubmissionResourceProof{
				RowID: row, RowVersion: rowVersion, TargetVersion: targetVer, SupplierID: supplierStr,
				StandardModelID: mid, Region: *region, SupplierModelID: vendor, Coefficient: coefficient,
				BuyerID: actorStr, FlowID: flowIDStr, FlowRevision: flowVer, AuthorizedActorID: actorStr,
				ApprovedAndAssigned: true, EnteredHandling: true,
			}}, nil
		},
		AttachRow: func(ctx context.Context, tx *sql.Tx, e supply.ResourceCreationEffect) error {
			res, err := tx.ExecContext(ctx, `UPDATE public.submission_row SET resource_id=$1 WHERE id=$2`, e.Resource.ID, e.RowID)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return supply.ErrRevisionConflict
			}
			return nil
		},
	})

	creation := supply.NewResourceCreationService(creationWork, func() time.Time { return time.Now().UTC() })
	created, err := creation.CreateFromSubmission(ctx, supply.WriteMeta{ActorID: actorStr, Reason: "seedchain 创建资源", RequestID: "seedchain-create"}, supply.ResourceCreationCommand{RowID: rowIDStr, RowVersion: rowVersion})
	if err != nil {
		log.Fatalf("create resource: %v", err)
	}
	resource := created.Resource
	log.Printf("resource=%s state=%s revision=%d origin_row=%s", resource.ID, resource.State, resource.Revision, resource.OriginRowID)

	refs := map[string]*supplypg.ReportEvidenceRef{}
	addReport := func(r *supply.Resource, sequence uint64, connection, pressure bool) string {
		var job, report string
		if err := tx.QueryRowContext(ctx, `INSERT INTO public.test_job(resource_id,category,status,verdict,created_by,finished_at)
VALUES($1,'stress','SUCCEEDED','PASS',$2,now()) RETURNING id::text`, r.ID, actorID).Scan(&job); err != nil {
			log.Fatalf("test job: %v", err)
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO public.test_report
(test_job_id,resource_id,connection_ms,connection_passed,stress_passed,rps,tps,availability,error_rate,ttft_ms,tpot_ms,sample_count,verdict,report_url,report_format,scene,tested_by,tested_at,cache_hit_tokens,cache_input_tokens)
VALUES($1,$2,86,$3,$4,24.5,19750,99.9,0.1,320,28,100,'PASS','https://example.com/report/seed','JSON','seedchain',$5,now(),300,1000)
RETURNING id::text`, job, r.ID, connection, pressure, actorID).Scan(&report); err != nil {
			log.Fatalf("test report: %v", err)
		}
		refs[report] = &supplypg.ReportEvidenceRef{
			ReportID: report, JobID: job, ResourceID: r.ID, SupplierID: r.SupplierID,
			StandardModelID: r.StandardModelID, Region: r.Region, OriginRowID: r.OriginRowID,
			OriginRowVersion: r.OriginRowVersion, TargetVersion: r.TargetVersion, Sequence: sequence, Valid: true,
		}
		return report
	}
	authority := func(ctx context.Context, tx *sql.Tx, r *supply.Resource, report string) (*supplypg.ReportEvidenceRef, bool, error) {
		ref := refs[report]
		if ref == nil {
			return nil, true, supply.ErrNotFound
		}
		copy := *ref
		return &copy, true, nil
	}
	evidence := supply.NewTestEvidenceService(supplypg.NewTestEvidenceWork(state, resolve, authority), func() time.Time { return time.Now().UTC() })

	failed := addReport(resource, 1, true, false)
	if _, err := evidence.AttachTestEvidence(ctx, supply.WriteMeta{ActorID: actorStr, Reason: "seedchain 失败报告", RequestID: "seedchain-evidence-failed"}, supply.TestEvidenceCommand{ResourceID: resource.ID, ReportID: failed}); err != nil {
		log.Fatalf("attach failed report: %v", err)
	}
	passed := addReport(resource, 2, true, true)
	selected, err := evidence.AttachTestEvidence(ctx, supply.WriteMeta{ActorID: actorStr, Reason: "seedchain 通过报告", RequestID: "seedchain-evidence-passed"}, supply.TestEvidenceCommand{ResourceID: resource.ID, ReportID: passed})
	if err != nil {
		log.Fatalf("attach passed report: %v", err)
	}
	log.Printf("after evidence state=%s revision=%d report=%s", selected.Resource.State, selected.Resource.Revision, selected.Resource.LatestReportID)

	configService := supply.NewFormalConfigService(supplypg.NewFormalConfigWork(configs, resolve), seedURLPolicy{}, func() time.Time { return time.Now().UTC() })
	secret := "seedchain-formal-secret"
	formal, err := configService.Create(ctx, supply.WriteMeta{ActorID: actorStr, Reason: "seedchain 正式配置", RequestID: "seedchain-config"}, supplierStr, supply.FormalConfigInput{
		Scope: "全部模型", BaseURL: "https://formal.example/v1", APIKey: &secret, ExternalConfirmed: true,
	})
	if err != nil {
		log.Fatalf("create formal config: %v", err)
	}
	log.Printf("formal config=%s revision=%d", formal.ID, formal.Revision)

	proof := func(ctx context.Context, tx *sql.Tx, r *supply.Resource) (*supply.AdmissionProof, error) {
		var assignee, nodeStatus, flowStatus string
		if err := tx.QueryRowContext(ctx, `SELECT n.assignee_id::text,n.status,f.status
FROM public.onboarding_node n JOIN public.onboarding_flow f ON f.id=n.flow_id
WHERE n.id=$1 AND f.id=$2 FOR UPDATE OF n,f`, nodeIDStr, flowIDStr).Scan(&assignee, &nodeStatus, &flowStatus); err != nil {
			return nil, err
		}
		return &supply.AdmissionProof{
			FlowID: flowIDStr, NodeID: nodeIDStr, FlowRevision: flowVer, ResourceID: r.ID,
			SupplierID: r.SupplierID, Region: r.Region, OriginRowID: r.OriginRowID,
			OriginRowVersion: r.OriginRowVersion, TargetVersion: r.TargetVersion, AssigneeID: assignee,
			FlowInProgress: flowStatus == "PROCESSING", NodeInProgress: nodeStatus == "IN_PROGRESS", PrerequisitesReady: true,
		}, nil
	}
	readRef := func(ctx context.Context, tx *sql.Tx, r *supply.Resource) (*supplypg.ReportEvidenceRef, error) {
		ref := refs[r.LatestReportID]
		if ref == nil {
			return nil, supply.ErrNotFound
		}
		copy := *ref
		return &copy, nil
	}
	binding := supply.NewResourceConfigBindingService(supplypg.NewResourceBindingWork(state, configs, resolve, proof), func() time.Time { return time.Now().UTC() })
	bound, err := binding.BindFormalConfig(ctx, supply.WriteMeta{ActorID: actorStr, Reason: "seedchain 绑定正式配置", RequestID: "seedchain-bind"}, supply.ResourceConfigBindingCommand{
		ResourceID: resource.ID, ConfigID: formal.ID, ExpectedRevision: selected.Resource.Revision, ExpectedConfigRevision: formal.Revision,
	})
	if err != nil {
		log.Fatalf("bind config: %v", err)
	}
	log.Printf("after binding revision=%d config=%s", bound.Resource.Revision, bound.Resource.FormalConfigID)

	admission := supply.NewResourceAdmissionService(supplypg.NewResourceAdmissionWork(state, configs, resolve, supplypg.ResourceAdmissionHooks{
		Proof:  proof,
		Report: readRef,
		CompleteRow: func(ctx context.Context, tx *sql.Tx, e supply.ResourceAdmissionEffect) error {
			res, err := tx.ExecContext(ctx, `UPDATE public.submission_row SET row_status='ADMITTED' WHERE id=$1 AND resource_id=$2 AND row_status='PENDING'`, e.After.OriginRowID, e.After.ID)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return supply.ErrRevisionConflict
			}
			return nil
		},
		Outbox: func(ctx context.Context, tx *sql.Tx, e supplypg.AdmissionOutboxEntry) (string, error) {
			var id string
			err := tx.QueryRowContext(ctx, `INSERT INTO public.event_outbox(event_type,payload,request_id,created_by,updated_by)
VALUES($1,$2::jsonb,$3,$4,$4) RETURNING id::text`, e.EventType, string(e.Payload), e.RequestID, e.ActorID).Scan(&id)
			return id, err
		},
	}), func() time.Time { return time.Now().UTC() })
	admitted, err := admission.Admit(ctx, supply.WriteMeta{ActorID: actorStr, Reason: "seedchain 登记接入", RequestID: "seedchain-admit"}, supply.ResourceAdmissionCommand{
		ResourceID: resource.ID, ExpectedRevision: bound.Resource.Revision, ExternalConfirmed: true,
		CompletionNote: "seedchain 已完成外部平台配置并登记接入",
	})
	if err != nil {
		log.Fatalf("admit: %v", err)
	}
	log.Printf("ADMITTED resource=%s state=%s revision=%d ever_admitted=%v", admitted.ID, admitted.State, admitted.Revision, admitted.EverAdmitted)
	if err := tx.Commit(); err != nil {
		log.Fatalf("commit: %v", err)
	}
	log.Printf("committed")
}
