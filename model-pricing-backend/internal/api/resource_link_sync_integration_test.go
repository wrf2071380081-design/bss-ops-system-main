package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"model_bss/internal/domain/customer"
	"model_bss/internal/domain/supplier"
	"model_bss/internal/infra/supplypg"
	"model_bss/internal/repo"
	"model_bss/internal/worker"
)

// G3-0 真库验收：供应域「资源下架/上架」事件 → 客户绑定状态迁移 + 审计。
//
// 为什么从 event_outbox 起测而不是真的执行一次 plan：联动只认 outbox 里的事件，
// 计划执行发事件的行为已有既有测试覆盖（plan_execution）。这里要证明的是
// **事件消费侧真的把 link 推过去了**，而不是"代码存在但没人调用"（G3-0 的原始问题）。
func TestResourceLinkSyncThroughEventOutbox(t *testing.T) {
	db := openB3ATestDB(t)
	ctx := context.Background()

	// 未配 webhook 是本次重点之一（A1：仍要消费、推进状态机、做联动，只跳过 HTTP）：
	// 测试前强制把 webhook_url 清空，结束后恢复原值，避免真的往外部发请求。
	restoreWebhook := forceEmptyWebhook(t, db)
	defer restoreWebhook()

	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	eventRepo := repo.NewEventJobRepo(gdb)
	logger := zap.NewNop()

	f := openCustomerWorkspaceTest(t, db)
	registry, err := supplypg.NewCustomerRegistryStore(db, "public")
	if err != nil {
		t.Fatalf("registry store: %v", err)
	}
	linkStore, err := supplypg.NewCustomerResourceLinkStore(db, "public")
	if err != nil {
		t.Fatalf("link store: %v", err)
	}
	linkSvc := customer.NewResourceLinkService(linkStore)

	// 3 个客户绑同一条资源（一条资源被 N 个客户绑定是真实场景）。
	customers := make([]*customer.CustomerProfile, 0, 3)
	linkIDs := make([]int64, 0, 3)
	for i := 0; i < 3; i++ {
		profile := f.createCustomer(t, registry, fmt.Sprintf("sync-%d", i+1))
		customers = append(customers, profile)
		link, err := linkStore.CreateLink(ctx, customer.CreateLinkRecord{
			CustomerProfileID: profile.ID, StandardModelID: f.modelID, ResourceID: f.resActive,
			State: customer.LinkStateActive, ActivationNote: "联动测试", ActivatedAt: ptrTime(time.Now().UTC()),
			CreatedBy: f.ownerID, RequestID: "qa-sync-create",
		})
		if err != nil {
			t.Fatalf("create link %d: %v", i, err)
		}
		linkIDs = append(linkIDs, link.ID)
	}
	t.Cleanup(func() {
		c := context.Background()
		for _, linkID := range linkIDs {
			_, _ = db.ExecContext(c, `DELETE FROM public.operation_log WHERE entity_type='customer_resource_link' AND entity_id=$1`, linkID)
		}
		_, _ = db.ExecContext(c, `DELETE FROM public.event_outbox WHERE request_id LIKE 'qa-link-sync%'`)
	})

	// ---------- 1) 下架事件：3 条全部进入待处理 ----------
	offlineID := insertTestEvent(t, db, "RESOURCE_OFFLINED", f.resActive)
	runEventDeliverJob(t, eventRepo, linkSvc, logger, ctx)

	assertLinkState(t, db, linkIDs[0], customer.LinkStateSupplyOfflinePending, false)
	assertLinkState(t, db, linkIDs[1], customer.LinkStateSupplyOfflinePending, false)
	assertLinkState(t, db, linkIDs[2], customer.LinkStateSupplyOfflinePending, false)
	assertAuditCount(t, db, linkIDs, "SUPPLY_OFFLINE", 3, fmt.Sprintf("event:%d", offlineID))
	assertEventStatus(t, db, offlineID, "DONE") // 未配 webhook 也必须推进到 DONE

	// ---------- 2) 其中 1 条人工确认停用（归档）→ 后续上架不得复活它 ----------
	current := loadLink(t, linkStore, linkIDs[0])
	if _, err := linkSvc.Act(ctx, customer.LinkActionInput{
		ID: linkIDs[0], Action: customer.LinkActionOfflineConfirm, ExpectedRevision: current.Revision,
		Reason: "来源已下架，确认停用",
	}, f.ownerID, "qa-sync-confirm"); err != nil {
		t.Fatalf("confirm offline: %v", err)
	}
	assertLinkState(t, db, linkIDs[0], customer.LinkStateSupplyOffline, true)

	// ---------- 3) 上架事件：只恢复未确认的 2 条，已归档那条原地不动 ----------
	onlineID := insertTestEvent(t, db, "RESOURCE_ONLINED", f.resActive)
	runEventDeliverJob(t, eventRepo, linkSvc, logger, ctx)

	assertLinkState(t, db, linkIDs[0], customer.LinkStateSupplyOffline, true) // 仍归档
	assertLinkState(t, db, linkIDs[1], customer.LinkStateActive, false)
	assertLinkState(t, db, linkIDs[2], customer.LinkStateActive, false)
	assertAuditCount(t, db, linkIDs, "SUPPLY_RESTORED", 2, fmt.Sprintf("event:%d", onlineID))

	// ---------- 4) 同一事件重复投递：不重复迁移、不重复审计 ----------
	if _, err := db.ExecContext(ctx, `UPDATE public.event_outbox SET status='PENDING', next_run_at=now() WHERE id=$1`, onlineID); err != nil {
		t.Fatalf("reset event: %v", err)
	}
	runEventDeliverJob(t, eventRepo, linkSvc, logger, ctx)
	assertAuditCount(t, db, linkIDs, "SUPPLY_RESTORED", 2, fmt.Sprintf("event:%d", onlineID))
	assertAuditCount(t, db, linkIDs, "SUPPLY_OFFLINE", 3, fmt.Sprintf("event:%d", offlineID))

	// ---------- 5) 人工动作（不走事件）也要有审计，且 request_id 可区分 ----------
	manual := loadLink(t, linkStore, linkIDs[1])
	if _, err := linkSvc.Act(ctx, customer.LinkActionInput{
		ID: linkIDs[1], Action: customer.LinkActionDisableRequest, ExpectedRevision: manual.Revision,
		Reason: "客户请求禁用",
	}, f.ownerID, "qa-sync-manual"); err != nil {
		t.Fatalf("manual disable: %v", err)
	}
	var manualRequest, manualOperator string
	if err := db.QueryRowContext(ctx, `
SELECT COALESCE(request_id,''), COALESCE(operator_id::text,'')
  FROM public.operation_log
 WHERE entity_type='customer_resource_link' AND entity_id=$1 AND action='DISABLE_REQUEST'`, linkIDs[1]).
		Scan(&manualRequest, &manualOperator); err != nil {
		t.Fatalf("load manual audit: %v", err)
	}
	if manualRequest != "qa-sync-manual" || strings.HasPrefix(manualRequest, "event:") {
		t.Fatalf("人工动作审计的 request_id 应为请求号而不是事件号：%q", manualRequest)
	}
	if manualOperator == "" {
		t.Fatal("人工动作的审计必须记录 operator_id")
	}
	// 事件驱动的审计没有内部员工操作者（operator_id NULL），这是与人工动作的关键区别。
	var eventOperator sql.NullString
	if err := db.QueryRowContext(ctx, `
SELECT operator_id::text FROM public.operation_log
 WHERE entity_type='customer_resource_link' AND entity_id=$1 AND action='SUPPLY_OFFLINE' LIMIT 1`, linkIDs[1]).
		Scan(&eventOperator); err != nil {
		t.Fatalf("load event audit: %v", err)
	}
	if eventOperator.Valid {
		t.Fatalf("事件驱动的审计不应有 operator_id，got %q", eventOperator.String)
	}
}

// forceEmptyWebhook 清空 sys_config 的 webhook_url（保留原值，测试结束恢复）。
func forceEmptyWebhook(t *testing.T, db *sql.DB) func() {
	t.Helper()
	ctx := context.Background()
	var original string
	var existed bool
	if err := db.QueryRowContext(ctx,
		`SELECT config_value FROM public.sys_config WHERE config_key='open_api.webhook_url'`).Scan(&original); err == nil {
		existed = true
	} else if err != sql.ErrNoRows {
		t.Fatalf("load webhook config: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO public.sys_config (config_key, config_value) VALUES ('open_api.webhook_url','')
ON CONFLICT (config_key) DO UPDATE SET config_value=''`); err != nil {
		t.Fatalf("clear webhook config: %v", err)
	}
	return func() {
		c := context.Background()
		if existed {
			_, _ = db.ExecContext(c, `UPDATE public.sys_config SET config_value=$1 WHERE config_key='open_api.webhook_url'`, original)
			return
		}
		_, _ = db.ExecContext(c, `DELETE FROM public.sys_config WHERE config_key='open_api.webhook_url'`)
	}
}

func insertTestEvent(t *testing.T, db *sql.DB, eventType string, resourceID int64) int64 {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"contract": "supply.resource_state_changed", "schema_version": 1,
		"resource_id": resourceID, "region": "CN",
		"state":           map[string]string{"RESOURCE_OFFLINED": "OFFLINE", "RESOURCE_ONLINED": "ADMITTED"}[eventType],
		"affected_status": "PENDING", "occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("marshal event payload: %v", err)
	}
	var id int64
	if err := db.QueryRowContext(context.Background(), `
INSERT INTO public.event_outbox (event_type, payload, status, next_run_at, created_at, updated_at, request_id)
VALUES ($1, $2::jsonb, 'PENDING', now(), now(), now(), $3) RETURNING id`,
		eventType, string(payload), "qa-link-sync-"+eventType).Scan(&id); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	return id
}

func runEventDeliverJob(t *testing.T, eventRepo *repo.EventJobRepo, links worker.LinkStateTransitioner, logger *zap.Logger, ctx context.Context) {
	t.Helper()
	job := worker.EventDeliverJob(eventRepo, links, logger)
	if err := job(ctx, supplier.JobIdentity{OperatorID: 0, OperatorRole: "SYSTEM", SourceType: "SYSTEM", RequestID: "qa-link-sync"}); err != nil {
		t.Fatalf("run event deliver job: %v", err)
	}
}

func loadLink(t *testing.T, store *supplypg.CustomerResourceLinkStore, id int64) *customer.ResourceLink {
	t.Helper()
	link, err := store.GetLink(context.Background(), id)
	if err != nil {
		t.Fatalf("load link %d: %v", id, err)
	}
	return link
}

func assertLinkState(t *testing.T, db *sql.DB, id int64, state string, archived bool) {
	t.Helper()
	var gotState string
	var gotArchived bool
	if err := db.QueryRowContext(context.Background(),
		`SELECT state, archived FROM public.customer_resource_link WHERE id=$1`, id).Scan(&gotState, &gotArchived); err != nil {
		t.Fatalf("load link %d: %v", id, err)
	}
	if gotState != state || gotArchived != archived {
		t.Fatalf("link %d 期望 %s/archived=%v，got %s/archived=%v", id, state, archived, gotState, gotArchived)
	}
}

func assertAuditCount(t *testing.T, db *sql.DB, linkIDs []int64, action string, want int, requestID string) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(context.Background(), `
SELECT count(*) FROM public.operation_log
 WHERE entity_type='customer_resource_link' AND action=$1 AND request_id=$2 AND entity_id = ANY($3)`,
		action, requestID, linkIDs).Scan(&got); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if got != want {
		t.Fatalf("审计 %s 期望 %d 条（request_id=%s），got %d", action, want, requestID, got)
	}
}

func assertEventStatus(t *testing.T, db *sql.DB, eventID int64, want string) {
	t.Helper()
	var got string
	if err := db.QueryRowContext(context.Background(),
		`SELECT status FROM public.event_outbox WHERE id=$1`, eventID).Scan(&got); err != nil {
		t.Fatalf("load event: %v", err)
	}
	if got != want {
		t.Fatalf("事件 %d 状态期望 %s，got %s", eventID, want, got)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
