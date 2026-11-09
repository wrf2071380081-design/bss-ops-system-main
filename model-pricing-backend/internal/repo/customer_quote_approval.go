// Package repo 的 customer_quote_approval.go：客户报价单审批（000057）。
//
// 方法挂在 CustomerSpecialRepo 上：它已经有 txOf / applyQuoteOwnerScope / CheckQuoteOwnerScope
// 与 audit，再开一个 repo 结构只会多一份一模一样的数据域代码。
//
// 红线：
//   - 写路径单事务（红线 8）：审批单 + 逐模型行 + customer_quote.status + audit_log 同一事务。
//   - 并发（红线 6 的乐观锁变体）：审批单与报价单都先 SELECT ... FOR UPDATE，
//     避免两个人同时批准把同一行改两次；逐模型 UPDATE 再带 result='PENDING' 条件兜底。
//   - 幂等（红线 5）：API 层挂 Idempotency，这里只管事务内的状态机。
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"model_bss/internal/domain/customer"
)

// quoteApprovalRowFull 是 customer_quote_approval 的读侧。
type quoteApprovalRowFull struct {
	ID                 int64      `gorm:"column:id"`
	QuoteID            int64      `gorm:"column:customer_quote_id"`
	Status             string     `gorm:"column:status"`
	ModelCount         int        `gorm:"column:model_count"`
	Note               *string    `gorm:"column:note"`
	SubmittedBy        int64      `gorm:"column:submitted_by"`
	SubmittedAt        time.Time  `gorm:"column:submitted_at"`
	DecidedBy          *int64     `gorm:"column:decided_by"`
	DecidedAt          *time.Time `gorm:"column:decided_at"`
	OpsAssigneeID      *int64     `gorm:"column:ops_assignee_id"`
	BusinessAssigneeID *int64     `gorm:"column:business_assignee_id"`
	WithdrawnReason    *string    `gorm:"column:withdrawn_reason"`
}

func (quoteApprovalRowFull) TableName() string { return "customer_quote_approval" }

// quoteApprovalItemRow 是 customer_quote_approval_row 的读侧。
type quoteApprovalItemRow struct {
	ID              int64      `gorm:"column:id"`
	ApprovalID      int64      `gorm:"column:approval_id"`
	StandardModelID int64      `gorm:"column:standard_model_id"`
	ModelName       string     `gorm:"column:model_name"`
	OfficialID      string     `gorm:"column:official_id"`
	ItemCount       int        `gorm:"column:item_count"`
	PriceMin        *string    `gorm:"column:price_min"`
	PriceMax        *string    `gorm:"column:price_max"`
	Result          string     `gorm:"column:result"`
	Reason          *string    `gorm:"column:reason"`
	DecidedBy       *int64     `gorm:"column:decided_by"`
	DecidedAt       *time.Time `gorm:"column:decided_at"`
	PriceDetail     []byte     `gorm:"column:price_detail"`
}

func (quoteApprovalItemRow) TableName() string { return "customer_quote_approval_row" }

// quoteModelGroup 是按标准模型聚合的待审批行。
type quoteModelGroup struct {
	StandardModelID int64   `gorm:"column:standard_model_id"`
	ModelName       string  `gorm:"column:model_name"`
	OfficialID      string  `gorm:"column:official_id"`
	ItemCount       int     `gorm:"column:item_count"`
	PriceMin        *string `gorm:"column:price_min"`
	PriceMax        *string `gorm:"column:price_max"`
	// PriceDetail 是该模型下各价项的单价快照（jsonb 文本），落审批行，
	// 让审批详情能像原型那样直接把「输入 / 输出 / 缓存」的价目摆出来。
	PriceDetail *string `gorm:"column:price_detail"`
}

// quoteModelGroupQuery 是「报价单明细 → 按标准模型聚合」的唯一口径。
//
// price_detail 用相关子查询再扫一遍同模型价项：数量是「一个模型几行价项」，
// 规模极小，换来的好处是审批行自带价目快照，不必在审批详情里回表读可能已被改动的报价单。
//
// 快照里带官方价（amount）与计价单位：审批人要看到「这个价相对官方价打了几折」，
// 只给一串单价（2.266668、11.33334）看不出是贵还是便宜。报价系数由 Go 侧算
// （coefficient = 客户单价 / 官方价），SQL 只负责取数，除零与精度都在 decimal 上处理。
const quoteModelGroupQuery = `
		SELECT COALESCE(r.standard_model_id, 0)                                      AS standard_model_id,
		       COALESCE(NULLIF(sm.name, ''), cqi.price_item_key, '未命名模型')        AS model_name,
		       COALESCE(sm.official_id, '')                                          AS official_id,
		       COUNT(*)                                                              AS item_count,
		       MIN(cqi.unit_price)::text                                             AS price_min,
		       MAX(cqi.unit_price)::text                                             AS price_max,
		       (SELECT jsonb_agg(jsonb_build_object('key', cqi2.price_item_key,
		                                            'price', cqi2.unit_price::text,
		                                            'currency', cqi2.currency,
		                                            'amount', COALESCE(pi.amount, ''),
		                                            'unit_code', COALESCE(pi.unit_code, ''),
		                                            'unit_quantity', COALESCE(pi.unit_quantity, ''))
		                         ORDER BY cqi2.price_item_key)::text
		          FROM customer_quote_item cqi2
		          LEFT JOIN resource r2 ON r2.id = cqi2.resource_id
		          LEFT JOIN standard_model sm2 ON sm2.id = r2.standard_model_id
		          LEFT JOIN LATERAL (
		                SELECT e.elem->>'amount'        AS amount,
		                       e.elem->>'unit_code'     AS unit_code,
		                       e.elem->>'unit_quantity' AS unit_quantity
		                  FROM jsonb_array_elements(COALESCE(sm2.price_items, '[]'::jsonb)) AS e(elem)
		                 WHERE e.elem->>'item_key' = cqi2.price_item_key
		                 LIMIT 1
		          ) pi ON TRUE
		         WHERE cqi2.customer_quote_id = cqi.customer_quote_id
		           AND COALESCE(r2.standard_model_id, 0) = COALESCE(r.standard_model_id, 0)) AS price_detail
		  FROM customer_quote_item cqi
		  LEFT JOIN resource r ON r.id = cqi.resource_id
		  LEFT JOIN standard_model sm ON sm.id = r.standard_model_id
		 WHERE cqi.customer_quote_id = ?
		 GROUP BY 1, 2, 3, r.standard_model_id, cqi.customer_quote_id
		 ORDER BY 3 ASC, 1 ASC`

// GroupQuoteItemsByModel 把一张报价单的明细按标准模型聚合（提交审批时的行来源）。
//
// 口径：一个模型可能有 text_input / text_output 多个价项，审批看的是「模型能不能卖」，
// 所以行粒度是模型，价项只留数量与价格区间。SKU 口径的老明细 join 不出标准模型，
// standard_model_id 落 0，模型名用 sku 的展示名兜底（不编造标准模型 ID）。
func (r *CustomerSpecialRepo) GroupQuoteItemsByModel(ctx context.Context, quoteID int64) ([]quoteModelGroup, error) {
	var rows []quoteModelGroup
	if err := r.txOf(ctx).Raw(quoteModelGroupQuery, quoteID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("group quote items: %w", err)
	}
	return rows, nil
}

// SubmitQuoteApprovalTx 提交审批：DRAFT → PENDING，生成逐模型待决行。
func (r *CustomerSpecialRepo) SubmitQuoteApprovalTx(ctx context.Context, quoteID, previousApprovalID int64, modelIDs []int64, operatorID int64, requestID string) (*customer.QuoteApproval, error) {
	var result *customer.QuoteApproval
	err := r.base.Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		var quote quoteRowFull
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", quoteID).Take(&quote).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return customer.ErrQuoteNotFound
			}
			return fmt.Errorf("lock quote %d: %w", quoteID, err)
		}
		if strings.ToUpper(quote.Status) != customer.QuoteStatusDraft {
			return customer.ErrQuoteApprovalQuoteStatus
		}
		var pending int64
		if err := tx.Table("customer_quote_approval").
			Where("customer_quote_id = ? AND status = ?", quoteID, customer.QuoteApprovalPending).
			Count(&pending).Error; err != nil {
			return fmt.Errorf("count pending approval: %w", err)
		}
		if pending > 0 {
			return customer.ErrQuoteAlreadyPending
		}
		groups, err := r.groupWithTx(ctx, tx, quoteID)
		if err != nil {
			return err
		}
		if len(groups) == 0 {
			return customer.ErrQuoteApprovalNoModels
		}
		// 关联重报：只把上一张审批里被驳回的模型纳入本次审批。
		if previousApprovalID > 0 {
			groups, err = r.reapplyGroups(tx, quoteID, previousApprovalID, modelIDs, groups)
			if err != nil {
				return err
			}
		} else if len(modelIDs) > 0 {
			// 非重报却传了模型范围：不静默忽略，避免调用方以为生效了。
			return customer.ErrQuoteApprovalReapplyModel
		}
		now := time.Now()
		note := fmt.Sprintf("报价单 #%d V%d 提交审批", quoteID, quote.VersionNo)
		var previous any
		if previousApprovalID > 0 {
			previous = previousApprovalID
			note = fmt.Sprintf("报价单 #%d V%d 关联驳回模型重报（来源审批 #%d）", quoteID, quote.VersionNo, previousApprovalID)
		}
		if err := tx.Table("customer_quote_approval").Create(map[string]any{
			"customer_quote_id":    quoteID,
			"status":               customer.QuoteApprovalPending,
			"model_count":          len(groups),
			"note":                 note,
			"submitted_by":         operatorID,
			"submitted_at":         now,
			"request_id":           requestID,
			"created_at":           now,
			"updated_at":           now,
			"previous_approval_id": previous,
		}).Error; err != nil {
			return fmt.Errorf("insert approval: %w", err)
		}
		var approvalID int64
		if err := tx.Table("customer_quote_approval").
			Select("id").Where("customer_quote_id = ? AND status = ? AND request_id = ?",
			quoteID, customer.QuoteApprovalPending, requestID).
			Order("id DESC").Limit(1).Scan(&approvalID).Error; err != nil || approvalID == 0 {
			return fmt.Errorf("load inserted approval id: %w", err)
		}
		for i, group := range groups {
			if err := tx.Table("customer_quote_approval_row").Create(map[string]any{
				"approval_id":       approvalID,
				"standard_model_id": group.StandardModelID,
				"model_name":        group.ModelName,
				"official_id":       group.OfficialID,
				"item_count":        group.ItemCount,
				"price_min":         group.PriceMin,
				"price_max":         group.PriceMax,
				"price_detail":      gorm.Expr("?::jsonb", group.PriceDetail),
				"result":            customer.ApprovalRowPending,
				"sort_no":           i + 1,
			}).Error; err != nil {
				return fmt.Errorf("insert approval row %d: %w", i+1, err)
			}
		}
		if err := tx.Table("customer_quote").Where("id = ?", quoteID).Updates(map[string]any{
			"status":     customer.QuoteStatusPending,
			"updated_at": now,
			"updated_by": operatorID,
			"request_id": requestID,
		}).Error; err != nil {
			return fmt.Errorf("update quote status: %w", err)
		}
		if err := r.audit.Record(ctx, AuditEntry{
			OperatorID: operatorID, OperatorRole: "STAFF",
			Action: "QUOTE_SUBMIT_APPROVAL", TargetType: "CUSTOMER_QUOTE", TargetID: quoteID,
			SourceType: "HUMAN", RequestID: requestID,
			AfterValue: map[string]any{
				"approval_id": approvalID, "model_count": len(groups),
				"models": approvalModelNames(groups),
			},
		}); err != nil {
			return fmt.Errorf("audit quote submit: %w", err)
		}
		result = &customer.QuoteApproval{ID: approvalID, QuoteID: quoteID, Status: customer.QuoteApprovalPending}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.LoadQuoteApproval(ctx, result.ID)
}

// reapplyGroups 校验并收窄「关联驳回模型重报」的范围。
//
// 规则（对齐原型「关联驳回模型新申请」）：
//  1. 来源审批必须已经结束，且结论是部分批准或已驳回；
//  2. 来源审批属于同一个客户；
//  3. 本次要审的模型必须都在来源审批的**驳回**行里（不能用重报去夹带已批准的模型，
//     否则已批准的模型会重复进审批、重复派待办）；
//  4. 这些模型在本次新报价单里必须真的有价目行。
func (r *CustomerSpecialRepo) reapplyGroups(tx *gorm.DB, quoteID, previousApprovalID int64, modelIDs []int64, groups []quoteModelGroup) ([]quoteModelGroup, error) {
	var prev struct {
		ID         int64  `gorm:"column:id"`
		Status     string `gorm:"column:status"`
		CustomerID int64  `gorm:"column:customer_id"`
	}
	if err := tx.Table("customer_quote_approval a").
		Select("a.id, a.status, q.customer_id").
		Joins("JOIN customer_quote q ON q.id = a.customer_quote_id").
		Where("a.id = ?", previousApprovalID).
		Take(&prev).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, customer.ErrQuoteApprovalNotFound
		}
		return nil, fmt.Errorf("load previous approval %d: %w", previousApprovalID, err)
	}
	if prev.Status != customer.QuoteApprovalPartial && prev.Status != customer.QuoteApprovalRejected {
		return nil, customer.ErrQuoteApprovalReapplyStatus
	}
	var currentCustomer int64
	if err := tx.Table("customer_quote").Select("customer_id").Where("id = ?", quoteID).Scan(&currentCustomer).Error; err != nil {
		return nil, fmt.Errorf("load quote customer: %w", err)
	}
	if currentCustomer != prev.CustomerID {
		return nil, customer.ErrQuoteApprovalReapplyOther
	}
	var rejected []int64
	if err := tx.Table("customer_quote_approval_row").
		Where("approval_id = ? AND result = ? AND standard_model_id > 0", previousApprovalID, customer.ApprovalRowRejected).
		Pluck("standard_model_id", &rejected).Error; err != nil {
		return nil, fmt.Errorf("load previous rejected rows: %w", err)
	}
	allowed := make(map[int64]bool, len(rejected))
	for _, id := range rejected {
		allowed[id] = true
	}
	// 没显式指定模型范围时，默认就是「上一轮被驳回的全部模型」——
	// 前端手里没有标准模型 id，让它传只会逼出一个错口径。
	wanted := make(map[int64]bool, len(modelIDs))
	if len(modelIDs) == 0 {
		for _, id := range rejected {
			wanted[id] = true
		}
	} else {
		for _, id := range modelIDs {
			if !allowed[id] {
				return nil, customer.ErrQuoteApprovalReapplyScope
			}
			wanted[id] = true
		}
	}
	if len(wanted) == 0 {
		return nil, customer.ErrQuoteApprovalReapplyModel
	}
	out := make([]quoteModelGroup, 0, len(groups))
	for _, group := range groups {
		if wanted[group.StandardModelID] {
			out = append(out, group)
		}
	}
	if len(out) == 0 {
		// 新报价单里没有任何一个被驳回的模型：要么没配来源，要么模型口径对不上。
		return nil, customer.ErrQuoteApprovalReapplyNoRows
	}
	return out, nil
}

// groupWithTx 是 GroupQuoteItemsByModel 的事务内版本（提交审批时用同一个 tx 读）。
func (r *CustomerSpecialRepo) groupWithTx(ctx context.Context, tx *gorm.DB, quoteID int64) ([]quoteModelGroup, error) {
	var rows []quoteModelGroup
	if err := tx.WithContext(ctx).Raw(quoteModelGroupQuery, quoteID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("group quote items: %w", err)
	}
	return rows, nil
}

func approvalModelNames(groups []quoteModelGroup) []string {
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		names = append(names, group.ModelName)
	}
	return names
}

// DecideQuoteApprovalTx 逐模型批准/驳回（对齐原型的三分法）：
//
//	全部批准 → 审批单 APPROVED（=原型「已分派」）、报价单 APPROVED
//	部分批准 → 审批单 PARTIAL（原型「部分批准」）、报价单 PARTIAL
//	全部驳回 → 审批单 REJECTED、报价单回 DRAFT（改价后可重提）
//
// 只要有批准模型，就在**同一事务**里给技术/商务办理人各派一条待办——
// 原型是「提交结论的瞬间就派活」，不是批完再让人手工发起。
func (r *CustomerSpecialRepo) DecideQuoteApprovalTx(ctx context.Context, approvalID int64, in customer.DecideRecord, operatorID int64, requestID string) (*customer.QuoteApproval, error) {
	err := r.base.Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		var approval quoteApprovalRowFull
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", approvalID).Take(&approval).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return customer.ErrQuoteApprovalNotFound
			}
			return fmt.Errorf("lock approval %d: %w", approvalID, err)
		}
		if approval.Status != customer.QuoteApprovalPending {
			return customer.ErrQuoteApprovalNotPending
		}
		var quote quoteRowFull
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", approval.QuoteID).Take(&quote).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return customer.ErrQuoteNotFound
			}
			return fmt.Errorf("lock quote %d: %w", approval.QuoteID, err)
		}
		now := time.Now()
		for _, decision := range in.Decisions {
			res := tx.Table("customer_quote_approval_row").
				Where("id = ? AND approval_id = ? AND result = ?", decision.RowID, approvalID, customer.ApprovalRowPending).
				Updates(map[string]any{
					"result":     decision.Result,
					"reason":     nullableString(decision.Reason),
					"decided_by": operatorID,
					"decided_at": now,
				})
			if res.Error != nil {
				return fmt.Errorf("update approval row %d: %w", decision.RowID, res.Error)
			}
			if res.RowsAffected != 1 {
				// 并发下被别人先批了：整单回滚，让前端刷新。
				return customer.ErrQuoteApprovalNotPending
			}
		}
		var undecided int64
		if err := tx.Table("customer_quote_approval_row").
			Where("approval_id = ? AND result = ?", approvalID, customer.ApprovalRowPending).
			Count(&undecided).Error; err != nil {
			return fmt.Errorf("count pending rows: %w", err)
		}
		if undecided > 0 {
			return customer.ErrQuoteApprovalRows
		}
		var approved, rejected int64
		if err := tx.Table("customer_quote_approval_row").
			Where("approval_id = ? AND result = ?", approvalID, customer.ApprovalRowApproved).
			Count(&approved).Error; err != nil {
			return fmt.Errorf("count approved rows: %w", err)
		}
		if err := tx.Table("customer_quote_approval_row").
			Where("approval_id = ? AND result = ?", approvalID, customer.ApprovalRowRejected).
			Count(&rejected).Error; err != nil {
			return fmt.Errorf("count rejected rows: %w", err)
		}
		final := customer.ConclusionOf(int(approved), int(rejected))
		// 部分批准时报价单落 PARTIAL：既不能当「已批准」去全量开通，
		// 也不能退回草稿让销售改掉已经批过的价格。
		quoteStatus := customer.QuoteStatusApproved
		switch final {
		case customer.QuoteApprovalPartial:
			quoteStatus = customer.QuoteStatusPartial
		case customer.QuoteApprovalRejected:
			quoteStatus = customer.QuoteStatusDraft
		}
		var opsAssignee, bizAssignee any
		if approved > 0 && in.OpsAssigneeID > 0 {
			opsAssignee = in.OpsAssigneeID
		}
		if approved > 0 && in.BusinessAssigneeID > 0 {
			bizAssignee = in.BusinessAssigneeID
		}
		if err := tx.Table("customer_quote_approval").Where("id = ?", approvalID).Updates(map[string]any{
			"status":               final,
			"note":                 in.Note,
			"decided_by":           operatorID,
			"decided_at":           now,
			"updated_at":           now,
			"request_id":           requestID,
			"ops_assignee_id":      opsAssignee,
			"business_assignee_id": bizAssignee,
			// 结论一提交，草稿就该消失：留着会让下一次打开弹窗回填旧内容。
			"draft":          nil,
			"draft_saved_by": nil,
			"draft_saved_at": nil,
		}).Error; err != nil {
			return fmt.Errorf("update approval status: %w", err)
		}
		if err := tx.Table("customer_quote").Where("id = ?", approval.QuoteID).Updates(map[string]any{
			"status":     quoteStatus,
			"updated_at": now,
			"updated_by": operatorID,
			"request_id": requestID,
		}).Error; err != nil {
			return fmt.Errorf("update quote status: %w", err)
		}
		// 审批即派发：批准的模型立刻变成两条待办，落到刚指定的两个人头上。
		if approved > 0 {
			// 待办要能真的点进去办：先把客户接入流程建出来（原型里审批通过即存在流程，
			// 商务直接办合同与授信、技术直接配来源）。不建流程的话「合同与授信」待办
			// 点进去只有一句"尚未发起开通"，商务还得自己去点发起开通并选供应来源——那不是他的活。
			if err := ensureCustomerFlow(tx, quote.CustomerID, in.BusinessAssigneeID > 0,
				in.OpsAssigneeID, in.BusinessAssigneeID, operatorID, requestID); err != nil {
				return err
			}
			customerName := customerDisplayName(tx, quote.CustomerID)
			if err := enqueueCustomerTask(tx, taskKindCustomerModels, quote.CustomerID, in.OpsAssigneeID,
				"客户模型开通", customerName+" · "+fmt.Sprintf("%d 个模型", approved), operatorID, requestID); err != nil {
				return err
			}
			if in.BusinessAssigneeID > 0 {
				if err := enqueueCustomerTask(tx, taskKindCustomerContract, quote.CustomerID, in.BusinessAssigneeID,
					"合同与授信", customerName, operatorID, requestID); err != nil {
					return err
				}
			}
		}
		if err := r.audit.Record(ctx, AuditEntry{
			OperatorID: operatorID, OperatorRole: "STAFF",
			Action: "QUOTE_APPROVE", TargetType: "CUSTOMER_QUOTE", TargetID: approval.QuoteID,
			SourceType: "HUMAN", RequestID: requestID,
			AfterValue: map[string]any{
				"approval_id": approvalID, "result": final,
				"approved": approved, "rejected": rejected,
				"note": in.Note, "ops_assignee_id": in.OpsAssigneeID, "business_assignee_id": in.BusinessAssigneeID,
				"decisions": in.Decisions,
			},
		}); err != nil {
			return fmt.Errorf("audit quote approve: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.LoadQuoteApproval(ctx, approvalID)
}

// WithdrawQuoteApprovalTx 撤回：PENDING → WITHDRAWN，报价单回 DRAFT，撤回原因留痕。
func (r *CustomerSpecialRepo) WithdrawQuoteApprovalTx(ctx context.Context, approvalID int64, reason string, operatorID int64, requestID string) (*customer.QuoteApproval, error) {
	err := r.base.Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(ctx)
		var approval quoteApprovalRowFull
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", approvalID).Take(&approval).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return customer.ErrQuoteApprovalNotFound
			}
			return fmt.Errorf("lock approval %d: %w", approvalID, err)
		}
		if approval.Status != customer.QuoteApprovalPending {
			return customer.ErrQuoteApprovalNotPending
		}
		if approval.SubmittedBy != operatorID {
			return customer.ErrQuoteApprovalNotOwner
		}
		now := time.Now()
		if err := tx.Table("customer_quote_approval").Where("id = ?", approvalID).Updates(map[string]any{
			"status":           customer.QuoteApprovalWithdrawn,
			"withdrawn_reason": reason,
			"updated_at":       now,
			"request_id":       requestID,
		}).Error; err != nil {
			return fmt.Errorf("withdraw approval: %w", err)
		}
		if err := tx.Table("customer_quote").Where("id = ?", approval.QuoteID).Updates(map[string]any{
			"status":     customer.QuoteStatusDraft,
			"updated_at": now,
			"updated_by": operatorID,
			"request_id": requestID,
		}).Error; err != nil {
			return fmt.Errorf("restore quote status: %w", err)
		}
		if err := r.audit.Record(ctx, AuditEntry{
			OperatorID: operatorID, OperatorRole: "STAFF",
			Action: "QUOTE_WITHDRAW_APPROVAL", TargetType: "CUSTOMER_QUOTE", TargetID: approval.QuoteID,
			SourceType: "HUMAN", RequestID: requestID,
			AfterValue: map[string]any{"approval_id": approvalID, "status": customer.QuoteApprovalWithdrawn, "reason": reason},
		}); err != nil {
			return fmt.Errorf("audit quote withdraw: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.LoadQuoteApproval(ctx, approvalID)
}

// quoteApprovalHead 是「审批单 + 报价单当前状态」的联表读侧。
type quoteApprovalHead struct {
	ID                 int64      `gorm:"column:id"`
	QuoteID            int64      `gorm:"column:customer_quote_id"`
	Status             string     `gorm:"column:status"`
	ModelCount         int        `gorm:"column:model_count"`
	Note               *string    `gorm:"column:note"`
	SubmittedBy        int64      `gorm:"column:submitted_by"`
	SubmittedAt        time.Time  `gorm:"column:submitted_at"`
	DecidedBy          *int64     `gorm:"column:decided_by"`
	DecidedAt          *time.Time `gorm:"column:decided_at"`
	OpsAssigneeID      *int64     `gorm:"column:ops_assignee_id"`
	BusinessAssigneeID *int64     `gorm:"column:business_assignee_id"`
	WithdrawnReason    *string    `gorm:"column:withdrawn_reason"`
	PreviousApprovalID *int64     `gorm:"column:previous_approval_id"`
	DraftJSON          *string    `gorm:"column:draft_json"`
	DraftSavedBy       *int64     `gorm:"column:draft_saved_by"`
	DraftSavedAt       *time.Time `gorm:"column:draft_saved_at"`
	CustomerID         int64      `gorm:"column:customer_id"`
	CustomerName       string     `gorm:"column:customer_name"`
	QuoteVersion       int        `gorm:"column:quote_version"`
	QuoteStatus        string     `gorm:"column:quote_status"`
}

// LoadQuoteApproval 读审批单 + 逐模型行 + 报价单当前状态。
func (r *CustomerSpecialRepo) LoadQuoteApproval(ctx context.Context, approvalID int64) (*customer.QuoteApproval, error) {
	var head quoteApprovalHead
	err := r.txOf(ctx).Table("customer_quote_approval a").
		Select(`a.id, a.customer_quote_id, a.status, a.model_count, a.note, a.submitted_by,
		        a.submitted_at, a.decided_by, a.decided_at,
		        a.ops_assignee_id, a.business_assignee_id, a.withdrawn_reason, a.previous_approval_id,
		        a.draft::text AS draft_json, a.draft_saved_by, a.draft_saved_at,
		        q.customer_id, q.version_no AS quote_version, q.status AS quote_status,
		        COALESCE(ls.legal_name, '') AS customer_name`).
		Joins("JOIN customer_quote q ON q.id = a.customer_quote_id").
		Joins("LEFT JOIN customer_profile cp ON cp.id = q.customer_id").
		Joins("LEFT JOIN legal_subject ls ON ls.id = cp.subject_id").
		Where("a.id = ?", approvalID).
		Take(&head).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("load quote approval %d: %w", approvalID, err)
	}
	var rows []quoteApprovalItemRow
	if err := r.txOf(ctx).Table("customer_quote_approval_row").
		Where("approval_id = ?", approvalID).
		Order("sort_no ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load approval rows: %w", err)
	}
	out := &customer.QuoteApproval{
		ID: head.ID, QuoteID: head.QuoteID, Status: head.Status, ModelCount: head.ModelCount,
		SubmittedBy: head.SubmittedBy, SubmittedAt: head.SubmittedAt,
		DecidedBy: head.DecidedBy, DecidedAt: head.DecidedAt,
		CustomerID: head.CustomerID, CustomerName: head.CustomerName,
		QuoteVersion:       head.QuoteVersion,
		QuoteStatus:        head.QuoteStatus,
		OpsAssigneeID:      head.OpsAssigneeID,
		BusinessAssigneeID: head.BusinessAssigneeID,
		Rows:               make([]customer.QuoteApprovalRow, 0, len(rows)),
	}
	if head.Note != nil {
		out.Note = *head.Note
	}
	if head.PreviousApprovalID != nil {
		out.PreviousApprovalID = *head.PreviousApprovalID
	}
	if head.DraftJSON != nil && strings.TrimSpace(*head.DraftJSON) != "" {
		var draft customer.ApprovalDraft
		if err := json.Unmarshal([]byte(*head.DraftJSON), &draft); err == nil {
			draft.SavedBy = 0
			if head.DraftSavedBy != nil {
				draft.SavedBy = *head.DraftSavedBy
			}
			draft.SavedAt = head.DraftSavedAt
			out.Draft = &draft
		}
	}
	if head.WithdrawnReason != nil {
		out.WithdrawReason = *head.WithdrawnReason
	}
	// 历史单（price_detail 是后加的列）没有价目快照：用当前报价单回算一份。
	// 不回算的话审批详情只能显示「2.266668 ~ 11.33334」这种单价区间——
	// 审批人看不出这是贵还是便宜，也看不到报价系数。
	var fallback map[int64][]customer.ApprovalPriceItem
	for _, row := range rows {
		if len(row.PriceDetail) > 0 {
			continue
		}
		if fallback != nil {
			continue
		}
		fallback = map[int64][]customer.ApprovalPriceItem{}
		groups, gerr := r.GroupQuoteItemsByModel(ctx, out.QuoteID)
		if gerr != nil {
			return nil, gerr
		}
		for _, group := range groups {
			if group.PriceDetail == nil {
				continue
			}
			items := parseApprovalPriceItems([]byte(*group.PriceDetail))
			if len(items) > 0 {
				fallback[group.StandardModelID] = items
			}
		}
	}

	for _, row := range rows {
		item := customer.QuoteApprovalRow{
			ID: row.ID, StandardModelID: row.StandardModelID, ModelName: row.ModelName,
			OfficialID: row.OfficialID, ItemCount: row.ItemCount, Result: row.Result,
			DecidedBy: row.DecidedBy, DecidedAt: row.DecidedAt,
		}
		if row.PriceMin != nil {
			item.PriceMin = trimDecimal(*row.PriceMin)
		}
		if row.PriceMax != nil {
			item.PriceMax = trimDecimal(*row.PriceMax)
		}
		if row.Reason != nil {
			item.Reason = *row.Reason
		}
		if len(row.PriceDetail) > 0 {
			item.PriceDetail = parseApprovalPriceItems(row.PriceDetail)
		}
		if len(item.PriceDetail) == 0 && fallback != nil {
			item.PriceDetail = fallback[row.StandardModelID]
		}
		out.Rows = append(out.Rows, item)
	}
	return out, nil
}

// parseApprovalPriceItems 解析价目快照，并补齐展示所需的两件事：
// 去掉 numeric 尾零、按「客户单价 ÷ 官方价」算出报价系数。
//
// 系数不放 SQL 算：单价与官方价都是 decimal，除法要固定 4 位、要处理除零，
// 放在 Go 里能直接复用 decimal 且有单测兜底。
func parseApprovalPriceItems(raw []byte) []customer.ApprovalPriceItem {
	var items []customer.ApprovalPriceItem
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	for i := range items {
		items[i].Price = trimDecimal(items[i].Price)
		items[i].Amount = trimDecimal(items[i].Amount)
		items[i].Coefficient = customer.QuoteCoefficient(items[i].Price, items[i].Amount)
	}
	return items
}

// ListQuoteApprovals 列审批单。customerID=0 表示跨客户（客户报价单页签的全局审批队列）。
//
// 带出客户名与销售名：原型列表直接显示「F2 虚构客户 a6 / 林舟」，
// 只回 ID 会让前端退化成「员工 #12」，审批人得靠记忆认人。
//
// 筛选对齐原型 3.10：销售 / 状态 / 时间 / 关键字。时间字段随阶段变——
// 待审批看「提交时间」，审批记录看「处理时间」，否则按提交时间筛历史会筛出一堆空行。
func (r *CustomerSpecialRepo) ListQuoteApprovals(ctx context.Context, q customer.QuoteApprovalQuery) ([]customer.QuoteApprovalSummary, error) {
	// 仓储也兜一次默认值：直接调仓储的调用方（测试/内部复用）不该因为漏填 limit 拿到空列表。
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 200
	}
	tx := r.txOf(ctx).Table("customer_quote_approval a").
		Select(`a.id, a.customer_quote_id AS quote_id, q.customer_id, a.status, a.model_count,
		        a.submitted_by, a.submitted_at, a.decided_by, a.decided_at,
		        COALESCE(ls.legal_name, '') AS customer_name,
		        COALESCE(sub.name, '') AS submitted_name,
		        COALESCE(dec.name, '') AS decided_name`).
		Joins("JOIN customer_quote q ON q.id = a.customer_quote_id").
		Joins("LEFT JOIN customer_profile cp ON cp.id = q.customer_id").
		Joins("LEFT JOIN legal_subject ls ON ls.id = cp.subject_id").
		Joins("LEFT JOIN internal_staff sub ON sub.id = a.submitted_by").
		Joins("LEFT JOIN internal_staff dec ON dec.id = a.decided_by")
	if q.CustomerID > 0 {
		tx = tx.Where("q.customer_id = ?", q.CustomerID)
	}
	// 阶段即状态：待审批 = PENDING，审批记录 = 其余全部（已分派/部分批准/已驳回/已撤回）。
	switch q.Stage {
	case customer.ApprovalStagePending:
		tx = tx.Where("a.status = ?", customer.QuoteApprovalPending)
	case customer.ApprovalStageHistory:
		tx = tx.Where("a.status <> ?", customer.QuoteApprovalPending)
	}
	if q.Status != "" {
		tx = tx.Where("a.status = ?", q.Status)
	}
	if q.SubmittedBy > 0 {
		tx = tx.Where("a.submitted_by = ?", q.SubmittedBy)
	}
	if keyword := strings.TrimSpace(q.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		tx = tx.Where("(ls.legal_name ILIKE ? OR CAST(a.customer_quote_id AS text) = ?)", like, keyword)
	}
	timeField := "a.submitted_at"
	if q.Stage == customer.ApprovalStageHistory {
		timeField = "a.decided_at"
	}
	if q.DateFrom != nil {
		tx = tx.Where(timeField+" >= ?", *q.DateFrom)
	}
	if q.DateTo != nil {
		tx = tx.Where(timeField+" <= ?", *q.DateTo)
	}
	var rows []struct {
		ID            int64      `gorm:"column:id"`
		QuoteID       int64      `gorm:"column:quote_id"`
		CustomerID    int64      `gorm:"column:customer_id"`
		CustomerName  string     `gorm:"column:customer_name"`
		Status        string     `gorm:"column:status"`
		ModelCount    int        `gorm:"column:model_count"`
		SubmittedBy   int64      `gorm:"column:submitted_by"`
		SubmittedName string     `gorm:"column:submitted_name"`
		SubmittedAt   time.Time  `gorm:"column:submitted_at"`
		DecidedBy     *int64     `gorm:"column:decided_by"`
		DecidedName   string     `gorm:"column:decided_name"`
		DecidedAt     *time.Time `gorm:"column:decided_at"`
	}
	if err := tx.Order("a.submitted_at DESC, a.id DESC").Limit(q.Limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list quote approvals: %w", err)
	}
	out := make([]customer.QuoteApprovalSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, customer.QuoteApprovalSummary{
			ID: row.ID, QuoteID: row.QuoteID, CustomerID: row.CustomerID, CustomerName: row.CustomerName,
			Status: row.Status, ModelCount: row.ModelCount,
			SubmittedBy: row.SubmittedBy, SubmittedName: row.SubmittedName, SubmittedAt: row.SubmittedAt,
			DecidedBy: row.DecidedBy, DecidedName: row.DecidedName, DecidedAt: row.DecidedAt,
		})
	}
	return out, nil
}

// SaveQuoteApprovalDraft 存/清审批草稿（原型「保存进度」）。
//
// draft 传 nil = 清空（提交结论时调用）。只更新草稿三列，不碰 status 与逐模型行，
// 这样「存了草稿」不会影响待审批队列的任何统计。
func (r *CustomerSpecialRepo) SaveQuoteApprovalDraft(ctx context.Context, approvalID int64, draft *customer.ApprovalDraft, operatorID int64, requestID string) error {
	updates := map[string]any{
		"draft":          nil,
		"draft_saved_by": nil,
		"draft_saved_at": nil,
		"updated_at":     time.Now(),
		"request_id":     requestID,
	}
	if draft != nil {
		payload, err := json.Marshal(draft)
		if err != nil {
			return fmt.Errorf("marshal approval draft: %w", err)
		}
		updates["draft"] = gorm.Expr("?::jsonb", string(payload))
		updates["draft_saved_by"] = operatorID
		updates["draft_saved_at"] = time.Now()
	}
	res := r.txOf(ctx).Table("customer_quote_approval").
		Where("id = ? AND status = ?", approvalID, customer.QuoteApprovalPending).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("save approval draft %d: %w", approvalID, res.Error)
	}
	if res.RowsAffected == 0 {
		return customer.ErrQuoteApprovalNotPending
	}
	return nil
}

// trimDecimal 去掉 numeric::text 尾部的零（12.00000000 → 12）。
func trimDecimal(raw string) string {
	text := strings.TrimSpace(raw)
	if !strings.Contains(text, ".") {
		return text
	}
	text = strings.TrimRight(text, "0")
	return strings.TrimSuffix(text, ".")
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

// 审批通过后派发的两类待办（对齐原型工作台里的「客户模型开通」「合同与授信」）。
//
// biz_id 存 customer_profile.id 而不是待办自己的 id：工作台点「详情」要直接落到客户详情，
// 收件箱据此拼 /internal/customers/{id}?tab=...。
const (
	taskKindCustomerModels   = "CUSTOMER_MODELS"
	taskKindCustomerContract = "CUSTOMER_CONTRACT"
)

// StaffCanApproveCustomerQuote 读审批标记位（迁移 000066）。
//
// 为什么不用权限点 M9:A：种子数据把 M9:A 也给了销售，于是销售能看见审批队列、
// 甚至能批自己的价（只被"不能自审"挡一半）。报价审批只由管理岗处理，用标记位表达。
func (r *CustomerSpecialRepo) StaffCanApproveCustomerQuote(ctx context.Context, staffID int64) (bool, error) {
	if staffID <= 0 {
		return false, nil
	}
	var allowed bool
	if err := r.txOf(ctx).Raw(
		`SELECT COALESCE(can_approve_customer_quote, FALSE) AND status = 'ACTIVE'
		   FROM internal_staff WHERE id = ?`, staffID).Scan(&allowed).Error; err != nil {
		return false, fmt.Errorf("load staff approve flag: %w", err)
	}
	return allowed, nil
}

// CustomerNeedsBusiness 判断这次批准是否必须同时派商务。
//
// 口径（对齐原型 cuNeedsBusiness）：客户合同还没归档就必须有人去办合同与授信；
// 已归档则复用既有商务条件，不再重复派。
//
// 事实源用**开通流程的 CONTRACT 节点**，不用 customer_profile.contract_status：
// 后者全库没有更新路径（只在建客户时写默认值，见 customer_workspace.go 的口径注释），
// 读它等于永远「待记录」→ 永远强制选商务办理人，和客户列表显示的合同状态也对不上。
func (r *CustomerSpecialRepo) CustomerNeedsBusiness(ctx context.Context, quoteID int64) (bool, error) {
	const query = `
		SELECT CASE
		         WHEN EXISTS (
		           SELECT 1 FROM onboarding_flow f
		             JOIN onboarding_node n ON n.flow_id = f.id AND n.node_key = 'CONTRACT' AND n.enabled
		            WHERE f.kind = 'CUSTOMER' AND f.owner_id = q.customer_id AND n.status = 'COMPLETED'
		         ) AND NOT EXISTS (
		           SELECT 1 FROM onboarding_flow f2
		             JOIN onboarding_node n2 ON n2.flow_id = f2.id AND n2.node_key = 'CONTRACT' AND n2.enabled
		            WHERE f2.kind = 'CUSTOMER' AND f2.owner_id = q.customer_id
		              AND n2.status IN ('IN_PROGRESS', 'WAITING_PREDECESSOR')
		         ) THEN FALSE
		         ELSE TRUE
		       END
		  FROM customer_quote q
		 WHERE q.id = ?`
	var needs bool
	if err := r.txOf(ctx).Raw(query, quoteID).Scan(&needs).Error; err != nil {
		return false, fmt.Errorf("load customer contract status: %w", err)
	}
	return needs, nil
}

// ensureCustomerFlow 在审批事务内建客户接入流程（对齐原型：审批通过即派活）。
//
// 为什么不写模型明细：审批时只知道「批了哪几个模型」，供应来源要技术后续在
// 「接入模型 → 配置资源」里挑（POST /customers/:id/onboarding/models 追加）；
// customer_onboarding_model.resource_id 是 NOT NULL，这会儿没有来源可填。
//
// 节点顺序沿用既有开通语义：
//   - 需要商务（合同未归档）→ CONTRACT 立刻办理，CONFIG 挂等待；
//   - 不需要商务 → 只有 CONFIG，直接办理。
//
// 幂等：已有进行中的流程就跳过（并发由部分唯一索引 uk_flow_customer_processing 兜底）。
func ensureCustomerFlow(tx *gorm.DB, customerID int64, needsBusiness bool, opsAssigneeID, bizAssigneeID, operatorID int64, requestID string) error {
	if customerID <= 0 {
		return nil
	}
	currentNode := "CONFIG"
	if needsBusiness {
		currentNode = "CONTRACT"
	}
	res := tx.Exec(`INSERT INTO onboarding_flow (kind, owner_id, status, current_node)
	                VALUES ('CUSTOMER', ?, 'PROCESSING', ?)
	                ON CONFLICT DO NOTHING`, customerID, currentNode)
	if res.Error != nil {
		return fmt.Errorf("insert customer flow: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		// 已经有一条进行中的流程：审批重复提交或上一轮还没办完，保持原样。
		return nil
	}
	var flowID int64
	if err := tx.Raw(`SELECT id FROM onboarding_flow
	                   WHERE kind = 'CUSTOMER' AND owner_id = ? AND status = 'PROCESSING'`,
		customerID).Scan(&flowID).Error; err != nil {
		return fmt.Errorf("load customer flow id: %w", err)
	}
	if flowID == 0 {
		return nil
	}
	if needsBusiness {
		if err := insertFlowNode(tx, flowID, "CONTRACT", "IN_PROGRESS", bizAssigneeID); err != nil {
			return err
		}
		if err := insertFlowNode(tx, flowID, "CONFIG", "WAITING_PREDECESSOR", opsAssigneeID); err != nil {
			return err
		}
	} else if err := insertFlowNode(tx, flowID, "CONFIG", "IN_PROGRESS", opsAssigneeID); err != nil {
		return err
	}
	// 与开通模块的审计同一张表、同一 entity_type，审批详情里串起来看得到。
	if err := tx.Exec(`INSERT INTO operation_log (entity_type, entity_id, action, operator_id, operator_name_snapshot,
	                                           "after", request_id, created_at)
	                   VALUES ('onboarding_flow', ?, 'START', ?,
	                           (SELECT name FROM internal_staff WHERE id = ?), ?::jsonb, ?, now())`,
		flowID, operatorID, operatorID,
		fmt.Sprintf(`{"from":"quote-approval","customer_id":%d,"first_order":%t,"current_node":%q}`,
			customerID, needsBusiness, currentNode),
		nullableString(requestID)).Error; err != nil {
		return fmt.Errorf("audit customer flow start: %w", err)
	}
	return nil
}

// insertFlowNode 建一个开通节点；assignee 为 0 时不指派（老数据兜底）。
func insertFlowNode(tx *gorm.DB, flowID int64, key, status string, assigneeID int64) error {
	var assignee any
	if assigneeID > 0 {
		assignee = assigneeID
	}
	if err := tx.Exec(`INSERT INTO onboarding_node (flow_id, node_key, status, assignee_id)
	                   VALUES (?, ?, ?, ?)
	                   ON CONFLICT (flow_id, node_key) DO NOTHING`, flowID, key, status, assignee).Error; err != nil {
		return fmt.Errorf("insert onboarding node %s: %w", key, err)
	}
	return nil
}

// enqueueCustomerTask 在审批事务内派一条待办。
//
// 去重口径与收件箱的显示口径必须一致：判定用的是「这条待办还看不看得见（TODO）」，
// 不是表里的 status——待办的状态是按办理节点算出来的，表里永远是 OPEN。
// 只按 status 判存在的话，这个客户第一次审批之后再也派不出新待办；
// 反过来无脑新建，工作台又会堆出两条一模一样的「客户模型开通」。所以：
//
//	还存在一条「对应节点未完成」的同类待办 → 不重复派；
//	上一条已经因为节点完成而在工作台转成 DONE → 派新的。
func enqueueCustomerTask(tx *gorm.DB, bizType string, customerID, assigneeID int64, title, bizObject string, operatorID int64, requestID string) error {
	if assigneeID <= 0 || customerID <= 0 {
		return nil
	}
	nodeKey := "CONFIG"
	if bizType == taskKindCustomerContract {
		nodeKey = "CONTRACT"
	}
	var active int64
	const activeQuery = `
		SELECT count(*) FROM todo_task t
		 WHERE t.biz_type = ? AND t.biz_id = ? AND t.status = 'OPEN'
		   AND NOT EXISTS (
		       SELECT 1 FROM onboarding_node n
		         JOIN onboarding_flow f ON f.id = n.flow_id
		        WHERE f.kind = 'CUSTOMER' AND f.owner_id = t.biz_id
		          AND n.node_key = ? AND n.status = 'COMPLETED' AND n.completed_at >= t.created_at
		   )`
	if err := tx.Raw(activeQuery, bizType, customerID, nodeKey).Scan(&active).Error; err != nil {
		return fmt.Errorf("check active todo %s/%d: %w", bizType, customerID, err)
	}
	if active > 0 {
		return nil
	}
	now := time.Now()
	if err := tx.Table("todo_task").Create(map[string]any{
		"biz_type":    bizType,
		"biz_id":      customerID,
		"assignee_id": assigneeID,
		"title":       title,
		"priority":    "MID",
		"status":      "OPEN",
		"created_at":  now,
		"updated_at":  now,
		"created_by":  operatorID,
		"request_id":  requestID,
	}).Error; err != nil {
		return fmt.Errorf("insert todo %s/%d: %w", bizType, customerID, err)
	}
	return nil
}

// customerDisplayName 取客户展示名（legal_subject.legal_name），取不到时退化成 ID。
func customerDisplayName(tx *gorm.DB, customerID int64) string {
	var name string
	if err := tx.Table("customer_profile cp").
		Select("COALESCE(ls.legal_name, '')").
		Joins("LEFT JOIN legal_subject ls ON ls.id = cp.subject_id").
		Where("cp.id = ?", customerID).
		Scan(&name).Error; err != nil || strings.TrimSpace(name) == "" {
		return fmt.Sprintf("客户 #%d", customerID)
	}
	return name
}
