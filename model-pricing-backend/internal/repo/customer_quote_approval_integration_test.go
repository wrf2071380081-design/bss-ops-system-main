package repo

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"model_bss/internal/domain/customer"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
)

// 报价审批「部分批准 + 审批即派发」的落库验证（迁移 000063）。
//
// 只在显式 opt-in 的日常库上跑：BSS_LOCAL_INTEGRATION_DB=model_bss。
// 全部夹具自建自删，不碰别人的数据；断言只覆盖本次事务的产物。
func TestDecideQuoteApprovalPartialDispatchIntegration(t *testing.T) {
	if os.Getenv("BSS_LOCAL_INTEGRATION_DB") != "model_bss" {
		t.Skip("explicit daily database opt-in required")
	}
	cfg, err := config.Load()
	if err != nil || cfg.Database.DBName != "model_bss" {
		t.Fatalf("wrong daily config: %v", err)
	}
	client, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	r := NewCustomerSpecialRepo(client.DB)

	// 取一个真实客户与两个真实员工，避免造主体数据。
	var customerID, salesID, opsID, bizID int64
	if err := client.DB.Raw(`SELECT id FROM customer_profile ORDER BY id LIMIT 1`).Scan(&customerID).Error; err != nil || customerID == 0 {
		t.Skipf("no customer_profile fixture: %v", err)
	}
	if err := client.DB.Raw(`SELECT id FROM internal_staff ORDER BY id LIMIT 1 OFFSET 1`).Scan(&salesID).Error; err != nil || salesID == 0 {
		t.Skipf("no internal_staff fixture: %v", err)
	}
	if err := client.DB.Raw(`SELECT id FROM internal_staff ORDER BY id LIMIT 1 OFFSET 2`).Scan(&opsID).Error; err != nil || opsID == 0 {
		t.Skipf("no second staff fixture: %v", err)
	}
	if err := client.DB.Raw(`SELECT id FROM internal_staff ORDER BY id LIMIT 1 OFFSET 3`).Scan(&bizID).Error; err != nil || bizID == 0 {
		t.Skipf("no third staff fixture: %v", err)
	}

	now := time.Now()
	var quoteID, approvalID int64
	if err := client.DB.Raw(`INSERT INTO customer_quote(customer_id, version_no, status, price_book_version, owner_sales_operator_id, created_by)
		VALUES (?, 999, 'PENDING', 1, ?, ?) RETURNING id`, customerID, salesID, salesID).Scan(&quoteID).Error; err != nil {
		t.Fatalf("insert quote: %v", err)
	}
	defer client.DB.Exec(`DELETE FROM customer_quote WHERE id = ?`, quoteID)

	if err := client.DB.Raw(`INSERT INTO customer_quote_approval(customer_quote_id, status, model_count, note, submitted_by, submitted_at)
		VALUES (?, 'PENDING', 2, '提交审批', ?, ?) RETURNING id`, quoteID, salesID, now).Scan(&approvalID).Error; err != nil {
		t.Fatalf("insert approval: %v", err)
	}
	defer client.DB.Exec(`DELETE FROM customer_quote_approval WHERE id = ?`, approvalID)

	var approvedRowID, rejectedRowID int64
	if err := client.DB.Raw(`INSERT INTO customer_quote_approval_row(approval_id, standard_model_id, model_name, official_id, item_count, result, sort_no, price_detail)
		VALUES (?, 1, '模型A', 'model-a', 2, 'PENDING', 1, '[{"key":"input","price":"1.30","currency":"USD"}]'::jsonb) RETURNING id`, approvalID).Scan(&approvedRowID).Error; err != nil {
		t.Fatalf("insert row A: %v", err)
	}
	if err := client.DB.Raw(`INSERT INTO customer_quote_approval_row(approval_id, standard_model_id, model_name, official_id, item_count, result, sort_no)
		VALUES (?, 2, '模型B', 'model-b', 1, 'PENDING', 2) RETURNING id`, approvalID).Scan(&rejectedRowID).Error; err != nil {
		t.Fatalf("insert row B: %v", err)
	}

	// 审批前先记：这个客户本来有没有进行中的流程。没有的话审批会新建一条，测试结束要清掉。
	var hadFlow bool
	if err := client.DB.Raw(`SELECT EXISTS(SELECT 1 FROM onboarding_flow
		WHERE kind = 'CUSTOMER' AND owner_id = ? AND status = 'PROCESSING')`, customerID).Scan(&hadFlow).Error; err != nil {
		t.Fatal(err)
	}

	out, err := r.DecideQuoteApprovalTx(ctx, approvalID, customer.DecideRecord{
		Decisions: []customer.ApprovalDecision{
			{RowID: approvedRowID, Result: customer.ApprovalRowApproved},
			{RowID: rejectedRowID, Result: customer.ApprovalRowRejected, Reason: "价格过低"},
		},
		Note:               "2 个模型批 1 驳 1",
		OpsAssigneeID:      opsID,
		BusinessAssigneeID: bizID,
	}, salesID+0, "it-req")
	if err != nil {
		t.Fatalf("decide: %v", err)
	}

	// 1) 结论 = PARTIAL，办理人落库
	if out.Status != customer.QuoteApprovalPartial {
		t.Fatalf("approval status=%q want PARTIAL", out.Status)
	}
	if out.OpsAssigneeID == nil || *out.OpsAssigneeID != opsID {
		t.Fatalf("ops assignee not stored: %+v", out.OpsAssigneeID)
	}
	if out.BusinessAssigneeID == nil || *out.BusinessAssigneeID != bizID {
		t.Fatalf("business assignee not stored: %+v", out.BusinessAssigneeID)
	}
	if out.Note != "2 个模型批 1 驳 1" {
		t.Fatalf("note=%q", out.Note)
	}

	// 1b) 审批通过即建客户接入流程：商务点「合同与授信」待办就能直接办，不用先点「发起开通」。
	if !hadFlow {
		var flow struct {
			ID          int64
			CurrentNode string
		}
		if err := client.DB.Raw(`SELECT id, current_node FROM onboarding_flow
			WHERE kind = 'CUSTOMER' AND owner_id = ? AND status = 'PROCESSING'`, customerID).Scan(&flow).Error; err != nil {
			t.Fatal(err)
		}
		if flow.ID == 0 {
			t.Fatal("审批通过后应自动建客户接入流程")
		}
		defer client.DB.Exec(`DELETE FROM onboarding_flow WHERE id = ?`, flow.ID)
		if flow.CurrentNode != "CONTRACT" {
			t.Fatalf("有商务办理人时应先办合同，current_node=%q", flow.CurrentNode)
		}
		var nodes []struct {
			Key        string
			Status     string
			AssigneeID *int64
		}
		if err := client.DB.Raw(`SELECT node_key AS key, status, assignee_id FROM onboarding_node
			WHERE flow_id = ? ORDER BY node_key`, flow.ID).Scan(&nodes).Error; err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 2 || nodes[0].Key != "CONFIG" || nodes[1].Key != "CONTRACT" {
			t.Fatalf("节点应为 CONFIG + CONTRACT，got %+v", nodes)
		}
		if nodes[1].AssigneeID == nil || *nodes[1].AssigneeID != bizID {
			t.Fatalf("合同节点应指派给商务办理人，got %+v", nodes[1])
		}
		if nodes[0].AssigneeID == nil || *nodes[0].AssigneeID != opsID {
			t.Fatalf("配置节点应指派给技术办理人，got %+v", nodes[0])
		}
	}

	// 2) 报价单 = PARTIAL（不能退草稿，也不能当全批准）
	var quoteStatus string
	if err := client.DB.Raw(`SELECT status FROM customer_quote WHERE id = ?`, quoteID).Scan(&quoteStatus).Error; err != nil {
		t.Fatal(err)
	}
	if quoteStatus != customer.QuoteStatusPartial {
		t.Fatalf("quote status=%q want PARTIAL", quoteStatus)
	}

	// 3) 逐模型行：一条批准、一条驳回带原因
	var rows []struct {
		Result string
		Reason *string
	}
	if err := client.DB.Raw(`SELECT result, reason FROM customer_quote_approval_row WHERE approval_id = ? ORDER BY sort_no`, approvalID).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Result != "APPROVED" || rows[1].Result != "REJECTED" || rows[1].Reason == nil {
		t.Fatalf("rows=%+v", rows)
	}

	// 4) 审批即派发：两条待办各落一人
	var tasks []struct {
		BizType    string
		AssigneeID int64
		Title      string
	}
	if err := client.DB.Raw(`SELECT biz_type, assignee_id, title FROM todo_task
		WHERE biz_id = ? AND biz_type IN ('CUSTOMER_MODELS','CUSTOMER_CONTRACT') ORDER BY biz_type`,
		customerID).Scan(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	defer client.DB.Exec(`DELETE FROM todo_task WHERE biz_id = ? AND biz_type IN ('CUSTOMER_MODELS','CUSTOMER_CONTRACT')`, customerID)
	if len(tasks) != 2 {
		t.Fatalf("want 2 dispatched tasks, got %+v", tasks)
	}
	if tasks[0].BizType != "CUSTOMER_CONTRACT" || tasks[0].AssigneeID != bizID {
		t.Fatalf("business task wrong: %+v", tasks[0])
	}
	if tasks[1].BizType != "CUSTOMER_MODELS" || tasks[1].AssigneeID != opsID {
		t.Fatalf("ops task wrong: %+v", tasks[1])
	}
	if tasks[1].Title != "客户模型开通" || tasks[0].Title != "合同与授信" {
		t.Fatalf("task titles wrong: %+v", tasks)
	}

	// 5) 价目快照可直接读出（原型「报价明细」列）
	if len(out.Rows) != 2 || len(out.Rows[0].PriceDetail) != 1 || out.Rows[0].PriceDetail[0].Key != "input" {
		t.Fatalf("price detail not loaded: %+v", out.Rows)
	}

	// 6) 同一客户再批一次：上一条待办还没办完，不应该在工作台堆出第二条一样的
	var quote2ID, approval2ID, row2ID int64
	if err := client.DB.Raw(`INSERT INTO customer_quote(customer_id, version_no, status, price_book_version, owner_sales_operator_id, created_by)
		VALUES (?, 998, 'PENDING', 1, ?, ?) RETURNING id`, customerID, salesID, salesID).Scan(&quote2ID).Error; err != nil {
		t.Fatalf("insert quote2: %v", err)
	}
	defer client.DB.Exec(`DELETE FROM customer_quote WHERE id = ?`, quote2ID)
	if err := client.DB.Raw(`INSERT INTO customer_quote_approval(customer_quote_id, status, model_count, note, submitted_by, submitted_at)
		VALUES (?, 'PENDING', 1, '再次提交', ?, ?) RETURNING id`, quote2ID, salesID, now).Scan(&approval2ID).Error; err != nil {
		t.Fatalf("insert approval2: %v", err)
	}
	defer client.DB.Exec(`DELETE FROM customer_quote_approval WHERE id = ?`, approval2ID)
	if err := client.DB.Raw(`INSERT INTO customer_quote_approval_row(approval_id, standard_model_id, model_name, official_id, item_count, result, sort_no)
		VALUES (?, 3, '模型C', 'model-c', 1, 'PENDING', 1) RETURNING id`, approval2ID).Scan(&row2ID).Error; err != nil {
		t.Fatalf("insert row C: %v", err)
	}
	second, err := r.DecideQuoteApprovalTx(ctx, approval2ID, customer.DecideRecord{
		Decisions:     []customer.ApprovalDecision{{RowID: row2ID, Result: customer.ApprovalRowApproved}},
		Note:          "全部批准",
		OpsAssigneeID: opsID,
	}, salesID, "it-req-2")
	if err != nil {
		t.Fatalf("decide2: %v", err)
	}
	if second.Status != customer.QuoteApprovalApproved {
		t.Fatalf("approval2 status=%q want APPROVED", second.Status)
	}
	var taskCount int64
	if err := client.DB.Raw(`SELECT count(*) FROM todo_task WHERE biz_id = ? AND biz_type IN ('CUSTOMER_MODELS','CUSTOMER_CONTRACT')`, customerID).Scan(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 2 {
		t.Fatalf("want still 2 tasks (不重复派), got %d", taskCount)
	}
}

// 报价审批列表的筛选（对齐原型 3.10：销售 / 状态 / 时间 / 关键字）。
func TestListQuoteApprovalsFiltersIntegration(t *testing.T) {
	if os.Getenv("BSS_LOCAL_INTEGRATION_DB") != "model_bss" {
		t.Skip("explicit daily database opt-in required")
	}
	cfg, err := config.Load()
	if err != nil || cfg.Database.DBName != "model_bss" {
		t.Fatalf("wrong daily config: %v", err)
	}
	client, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	r := NewCustomerSpecialRepo(client.DB)

	var customerID, salesA, salesB int64
	if err := client.DB.Raw(`SELECT id FROM customer_profile ORDER BY id LIMIT 1`).Scan(&customerID).Error; err != nil || customerID == 0 {
		t.Skipf("no customer_profile fixture: %v", err)
	}
	if err := client.DB.Raw(`SELECT id FROM internal_staff ORDER BY id LIMIT 1`).Scan(&salesA).Error; err != nil || salesA == 0 {
		t.Skipf("no staff fixture: %v", err)
	}
	if err := client.DB.Raw(`SELECT id FROM internal_staff ORDER BY id LIMIT 1 OFFSET 1`).Scan(&salesB).Error; err != nil || salesB == 0 {
		t.Skipf("no second staff fixture: %v", err)
	}

	now := time.Now()
	var quoteID int64
	if err := client.DB.Raw(`INSERT INTO customer_quote(customer_id, version_no, status, price_book_version, owner_sales_operator_id, created_by)
		VALUES (?, 997, 'PARTIAL', 1, ?, ?) RETURNING id`, customerID, salesA, salesA).Scan(&quoteID).Error; err != nil {
		t.Fatalf("insert quote: %v", err)
	}
	defer client.DB.Exec(`DELETE FROM customer_quote WHERE id = ?`, quoteID)

	// 三条不同提交人/状态/时间的审批单
	var pendingID, partialID, approvedID int64
	insert := func(status string, by int64, submittedAt time.Time, decidedAt *time.Time) int64 {
		var id int64
		if err := client.DB.Raw(`INSERT INTO customer_quote_approval(customer_quote_id, status, model_count, note, submitted_by, submitted_at, decided_at)
			VALUES (?, ?, 1, 'n', ?, ?, ?) RETURNING id`, quoteID, status, by, submittedAt, decidedAt).Scan(&id).Error; err != nil {
			t.Fatalf("insert approval %s: %v", status, err)
		}
		return id
	}
	pendingID = insert("PENDING", salesA, now.Add(-2*time.Hour), nil)
	partialID = insert("PARTIAL", salesB, now.Add(-26*time.Hour), &[]time.Time{now.Add(-20 * time.Hour)}[0])
	approvedID = insert("APPROVED", salesA, now.Add(-50*time.Hour), &[]time.Time{now.Add(-40 * time.Hour)}[0])
	defer client.DB.Exec(`DELETE FROM customer_quote_approval WHERE customer_quote_id = ?`, quoteID)

	ids := func(q customer.QuoteApprovalQuery) map[int64]bool {
		q.CustomerID = customerID
		list, err := r.ListQuoteApprovals(ctx, q)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		set := map[int64]bool{}
		for _, row := range list {
			set[row.ID] = true
		}
		return set
	}

	if got := ids(customer.QuoteApprovalQuery{Stage: customer.ApprovalStagePending}); len(got) != 1 || !got[pendingID] {
		t.Fatalf("待审批应只返回 PENDING，got %v", got)
	}
	if got := ids(customer.QuoteApprovalQuery{Stage: customer.ApprovalStageHistory}); len(got) != 2 || got[pendingID] {
		t.Fatalf("审批记录应排除 PENDING，got %v", got)
	}
	if got := ids(customer.QuoteApprovalQuery{Stage: customer.ApprovalStageHistory, Status: customer.QuoteApprovalPartial}); len(got) != 1 || !got[partialID] {
		t.Fatalf("按状态筛应只回 PARTIAL，got %v", got)
	}
	if got := ids(customer.QuoteApprovalQuery{SubmittedBy: salesB}); len(got) != 1 || !got[partialID] {
		t.Fatalf("按销售筛应只回销售 B 的单，got %v", got)
	}
	// 待审批阶段按「提交时间」：只取最近 6 小时 → 只剩 pending
	from := now.Add(-6 * time.Hour)
	if got := ids(customer.QuoteApprovalQuery{Stage: customer.ApprovalStagePending, DateFrom: &from}); len(got) != 1 || !got[pendingID] {
		t.Fatalf("按提交时间筛待审批，got %v", got)
	}
	// 审批记录阶段按「处理时间」：同样 6 小时窗口 → 只回处理过的两条里较新的那条
	if got := ids(customer.QuoteApprovalQuery{Stage: customer.ApprovalStageHistory, DateFrom: &from}); len(got) != 0 {
		t.Fatalf("近 6 小时没有已处理的单，got %v", got)
	}
	decidedFrom := now.Add(-22 * time.Hour)
	if got := ids(customer.QuoteApprovalQuery{Stage: customer.ApprovalStageHistory, DateFrom: &decidedFrom}); len(got) != 1 || !got[partialID] {
		t.Fatalf("按处理时间筛审批记录，got %v", got)
	}
	_ = approvedID
}

// 关联驳回模型重报（原型「关联驳回模型新申请」）：新一版报价只对被驳回的模型再走一次审批。
func TestSubmitQuoteApprovalReapplyIntegration(t *testing.T) {
	if os.Getenv("BSS_LOCAL_INTEGRATION_DB") != "model_bss" {
		t.Skip("explicit daily database opt-in required")
	}
	cfg, err := config.Load()
	if err != nil || cfg.Database.DBName != "model_bss" {
		t.Fatalf("wrong daily config: %v", err)
	}
	client, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	r := NewCustomerSpecialRepo(client.DB)

	var customerID, staffID int64
	if err := client.DB.Raw(`SELECT id FROM customer_profile ORDER BY id LIMIT 1`).Scan(&customerID).Error; err != nil || customerID == 0 {
		t.Skipf("no customer_profile fixture: %v", err)
	}
	if err := client.DB.Raw(`SELECT id FROM internal_staff ORDER BY id LIMIT 1`).Scan(&staffID).Error; err != nil || staffID == 0 {
		t.Skipf("no staff fixture: %v", err)
	}
	// 取两条不同标准模型的已准入资源，用来造报价明细。
	rows, err := client.DB.Raw(`SELECT DISTINCT ON (standard_model_id) id, standard_model_id
		FROM resource WHERE state = 'ADMITTED' ORDER BY standard_model_id, id LIMIT 2`).Rows()
	if err != nil {
		t.Fatalf("pick resources: %v", err)
	}
	var resA, resB, modelA, modelB int64
	for rows.Next() {
		var rid, mid int64
		if err := rows.Scan(&rid, &mid); err != nil {
			t.Fatal(err)
		}
		if resA == 0 {
			resA, modelA = rid, mid
		} else {
			resB, modelB = rid, mid
		}
	}
	_ = rows.Close()
	if resA == 0 || resB == 0 || modelA == modelB {
		t.Skip("需要两条不同标准模型的已准入资源")
	}

	makeQuote := func(version int, status string) int64 {
		var id int64
		if err := client.DB.Raw(`INSERT INTO customer_quote(customer_id, version_no, status, price_book_version, owner_sales_operator_id, created_by)
			VALUES (?, ?, ?, 1, ?, ?) RETURNING id`, customerID, version, status, staffID, staffID).Scan(&id).Error; err != nil {
			t.Fatalf("insert quote: %v", err)
		}
		return id
	}
	addItem := func(quoteID, resourceID int64) {
		if err := client.DB.Exec(`INSERT INTO customer_quote_item(customer_quote_id, resource_id, price_item_key, currency, unit_price, floor_price)
			VALUES (?, ?, 'input', 'CNY', 10, 5)`, quoteID, resourceID).Error; err != nil {
			t.Fatalf("insert quote item: %v", err)
		}
	}

	// 统一清理：按创建顺序倒序删（新单的审批引用旧审批，必须先删子再删父），
	// 并且先删价目明细——customer_quote_item 有 FK 指向 customer_quote。
	var cleanupQuoteIDs []int64
	defer func() {
		for i := len(cleanupQuoteIDs) - 1; i >= 0; i-- {
			id := cleanupQuoteIDs[i]
			client.DB.Exec(`DELETE FROM customer_quote_item WHERE customer_quote_id = ?`, id)
			client.DB.Exec(`DELETE FROM customer_quote_approval WHERE customer_quote_id = ?`, id)
			client.DB.Exec(`DELETE FROM customer_quote WHERE id = ?`, id)
		}
	}()

	// 上一张审批：A 批准、B 驳回
	prevQuote := makeQuote(996, "PARTIAL")
	cleanupQuoteIDs = append(cleanupQuoteIDs, prevQuote)
	var prevID int64
	if err := client.DB.Raw(`INSERT INTO customer_quote_approval(customer_quote_id, status, model_count, note, submitted_by, submitted_at, decided_at)
		VALUES (?, 'PARTIAL', 2, '上一轮', ?, now(), now()) RETURNING id`, prevQuote, staffID).Scan(&prevID).Error; err != nil {
		t.Fatalf("insert previous approval: %v", err)
	}

	if err := client.DB.Exec(`INSERT INTO customer_quote_approval_row(approval_id, standard_model_id, model_name, result, sort_no)
		VALUES (?, ?, 'A', 'APPROVED', 1), (?, ?, 'B', 'REJECTED', 2)`, prevID, modelA, prevID, modelB).Error; err != nil {
		t.Fatalf("insert previous rows: %v", err)
	}

	// 新一版报价（改价后的草稿），两个模型都有价目
	newQuote := makeQuote(995, "DRAFT")
	cleanupQuoteIDs = append(cleanupQuoteIDs, newQuote)
	addItem(newQuote, resA)
	addItem(newQuote, resB)

	// 只重报被驳回的 B：不传模型范围，由仓储按来源审批的驳回行自动收窄
	out, err := r.SubmitQuoteApprovalTx(ctx, newQuote, prevID, nil, staffID, "reapply-req")
	if err != nil {
		t.Fatalf("submit reapply: %v", err)
	}
	if out.PreviousApprovalID != prevID {
		t.Fatalf("previous_approval_id=%d want %d", out.PreviousApprovalID, prevID)
	}
	if len(out.Rows) != 1 || out.Rows[0].StandardModelID != modelB {
		t.Fatalf("重报范围应只有被驳回的模型 B，got %+v", out.Rows)
	}

	// 反向：拿已批准的 A 来重报 → 拒绝
	newQuote2 := makeQuote(994, "DRAFT")
	cleanupQuoteIDs = append(cleanupQuoteIDs, newQuote2)
	addItem(newQuote2, resA)
	if _, err := r.SubmitQuoteApprovalTx(ctx, newQuote2, prevID, []int64{modelA}, staffID, "reapply-req-2"); !errors.Is(err, customer.ErrQuoteApprovalReapplyScope) {
		t.Fatalf("拿已批准模型重报应被拒，got %v", err)
	}

	// 反向：来源审批还在待处理 → 拒绝
	pendingQuote := makeQuote(993, "DRAFT")
	cleanupQuoteIDs = append(cleanupQuoteIDs, pendingQuote)
	addItem(pendingQuote, resB)
	var pendingPrev int64
	if err := client.DB.Raw(`INSERT INTO customer_quote_approval(customer_quote_id, status, model_count, note, submitted_by, submitted_at)
		VALUES (?, 'PENDING', 1, '未处理', ?, now()) RETURNING id`, prevQuote, staffID).Scan(&pendingPrev).Error; err != nil {
		t.Fatalf("insert pending approval: %v", err)
	}

	if err := client.DB.Exec(`INSERT INTO customer_quote_approval_row(approval_id, standard_model_id, model_name, result, sort_no)
		VALUES (?, ?, 'B', 'PENDING', 1)`, pendingPrev, modelB).Error; err != nil {
		t.Fatalf("insert pending row: %v", err)
	}
	if _, err := r.SubmitQuoteApprovalTx(ctx, pendingQuote, pendingPrev, []int64{modelB}, staffID, "reapply-req-3"); !errors.Is(err, customer.ErrQuoteApprovalReapplyStatus) {
		t.Fatalf("来源审批未结束时重报应被拒，got %v", err)
	}
}

// 保存进度（草稿）落库与清空：不碰审批状态，提交结论后消失。
func TestSaveQuoteApprovalDraftIntegration(t *testing.T) {
	if os.Getenv("BSS_LOCAL_INTEGRATION_DB") != "model_bss" {
		t.Skip("explicit daily database opt-in required")
	}
	cfg, err := config.Load()
	if err != nil || cfg.Database.DBName != "model_bss" {
		t.Fatalf("wrong daily config: %v", err)
	}
	client, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	r := NewCustomerSpecialRepo(client.DB)

	var customerID, staffID int64
	if err := client.DB.Raw(`SELECT id FROM customer_profile ORDER BY id LIMIT 1`).Scan(&customerID).Error; err != nil || customerID == 0 {
		t.Skipf("no customer_profile fixture: %v", err)
	}
	if err := client.DB.Raw(`SELECT id FROM internal_staff ORDER BY id LIMIT 1`).Scan(&staffID).Error; err != nil || staffID == 0 {
		t.Skipf("no staff fixture: %v", err)
	}

	var quoteID int64
	if err := client.DB.Raw(`INSERT INTO customer_quote(customer_id, version_no, status, price_book_version, owner_sales_operator_id, created_by)
		VALUES (?, 992, 'PENDING', 1, ?, ?) RETURNING id`, customerID, staffID, staffID).Scan(&quoteID).Error; err != nil {
		t.Fatalf("insert quote: %v", err)
	}
	defer client.DB.Exec(`DELETE FROM customer_quote WHERE id = ?`, quoteID)
	var approvalID, rowID int64
	if err := client.DB.Raw(`INSERT INTO customer_quote_approval(customer_quote_id, status, model_count, note, submitted_by, submitted_at)
		VALUES (?, 'PENDING', 1, 'n', ?, now()) RETURNING id`, quoteID, staffID).Scan(&approvalID).Error; err != nil {
		t.Fatalf("insert approval: %v", err)
	}
	defer client.DB.Exec(`DELETE FROM customer_quote_approval WHERE id = ?`, approvalID)
	if err := client.DB.Raw(`INSERT INTO customer_quote_approval_row(approval_id, standard_model_id, model_name, result, sort_no)
		VALUES (?, 13, 'A', 'PENDING', 1) RETURNING id`, approvalID).Scan(&rowID).Error; err != nil {
		t.Fatalf("insert row: %v", err)
	}

	draft := &customer.ApprovalDraft{
		Rows:          []customer.ApprovalDecision{{RowID: rowID, Result: customer.ApprovalRowRejected, Reason: "先写个原因"}},
		Note:          "填到一半",
		OpsAssigneeID: staffID,
	}
	if err := r.SaveQuoteApprovalDraft(ctx, approvalID, draft, staffID, "draft-req"); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	loaded, err := r.LoadQuoteApproval(ctx, approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Draft == nil || len(loaded.Draft.Rows) != 1 || loaded.Draft.Note != "填到一半" {
		t.Fatalf("草稿没读回来: %+v", loaded.Draft)
	}
	if loaded.Status != customer.QuoteApprovalPending {
		t.Fatalf("存草稿不该改审批状态，got %s", loaded.Status)
	}

	// 清空
	if err := r.SaveQuoteApprovalDraft(ctx, approvalID, nil, staffID, "draft-req-2"); err != nil {
		t.Fatalf("clear draft: %v", err)
	}
	cleared, err := r.LoadQuoteApproval(ctx, approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Draft != nil {
		t.Fatalf("草稿应已清空，got %+v", cleared.Draft)
	}
}
