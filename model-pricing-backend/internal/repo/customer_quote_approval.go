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
	ID          int64      `gorm:"column:id"`
	QuoteID     int64      `gorm:"column:customer_quote_id"`
	Status      string     `gorm:"column:status"`
	ModelCount  int        `gorm:"column:model_count"`
	Note        *string    `gorm:"column:note"`
	SubmittedBy int64      `gorm:"column:submitted_by"`
	SubmittedAt time.Time  `gorm:"column:submitted_at"`
	DecidedBy   *int64     `gorm:"column:decided_by"`
	DecidedAt   *time.Time `gorm:"column:decided_at"`
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
}

// GroupQuoteItemsByModel 把一张报价单的明细按标准模型聚合（提交审批时的行来源）。
//
// 口径：一个模型可能有 text_input / text_output 多个价项，审批看的是「模型能不能卖」，
// 所以行粒度是模型，价项只留数量与价格区间。SKU 口径的老明细 join 不出标准模型，
// standard_model_id 落 0，模型名用 sku 的展示名兜底（不编造标准模型 ID）。
func (r *CustomerSpecialRepo) GroupQuoteItemsByModel(ctx context.Context, quoteID int64) ([]quoteModelGroup, error) {
	const query = `
		SELECT COALESCE(r.standard_model_id, 0)                                      AS standard_model_id,
		       COALESCE(NULLIF(sm.name, ''), cqi.price_item_key, '未命名模型')        AS model_name,
		       COALESCE(sm.official_id, '')                                          AS official_id,
		       COUNT(*)                                                              AS item_count,
		       MIN(cqi.unit_price)::text                                             AS price_min,
		       MAX(cqi.unit_price)::text                                             AS price_max
		  FROM customer_quote_item cqi
		  LEFT JOIN resource r ON r.id = cqi.resource_id
		  LEFT JOIN standard_model sm ON sm.id = r.standard_model_id
		 WHERE cqi.customer_quote_id = ?
		 GROUP BY 1, 2, 3
		 ORDER BY 3 ASC, 1 ASC`
	var rows []quoteModelGroup
	if err := r.txOf(ctx).Raw(query, quoteID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("group quote items: %w", err)
	}
	return rows, nil
}

// SubmitQuoteApprovalTx 提交审批：DRAFT → PENDING，生成逐模型待决行。
func (r *CustomerSpecialRepo) SubmitQuoteApprovalTx(ctx context.Context, quoteID int64, operatorID int64, requestID string) (*customer.QuoteApproval, error) {
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
		now := time.Now()
		note := fmt.Sprintf("报价单 #%d V%d 提交审批", quoteID, quote.VersionNo)
		if err := tx.Table("customer_quote_approval").Create(map[string]any{
			"customer_quote_id": quoteID,
			"status":            customer.QuoteApprovalPending,
			"model_count":       len(groups),
			"note":              note,
			"submitted_by":      operatorID,
			"submitted_at":      now,
			"request_id":        requestID,
			"created_at":        now,
			"updated_at":        now,
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
				"models":      approvalModelNames(groups),
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

// groupWithTx 是 GroupQuoteItemsByModel 的事务内版本（提交审批时用同一个 tx 读）。
func (r *CustomerSpecialRepo) groupWithTx(ctx context.Context, tx *gorm.DB, quoteID int64) ([]quoteModelGroup, error) {
	const query = `
		SELECT COALESCE(r.standard_model_id, 0)                                      AS standard_model_id,
		       COALESCE(NULLIF(sm.name, ''), cqi.price_item_key, '未命名模型')        AS model_name,
		       COALESCE(sm.official_id, '')                                          AS official_id,
		       COUNT(*)                                                              AS item_count,
		       MIN(cqi.unit_price)::text                                             AS price_min,
		       MAX(cqi.unit_price)::text                                             AS price_max
		  FROM customer_quote_item cqi
		  LEFT JOIN resource r ON r.id = cqi.resource_id
		  LEFT JOIN standard_model sm ON sm.id = r.standard_model_id
		 WHERE cqi.customer_quote_id = ?
		 GROUP BY 1, 2, 3
		 ORDER BY 3 ASC, 1 ASC`
	var rows []quoteModelGroup
	if err := tx.WithContext(ctx).Raw(query, quoteID).Scan(&rows).Error; err != nil {
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

// DecideQuoteApprovalTx 逐模型批准/驳回：全部批准 → APPROVED；任一驳回 → REJECTED 且报价单回 DRAFT。
func (r *CustomerSpecialRepo) DecideQuoteApprovalTx(ctx context.Context, approvalID int64, decisions []customer.ApprovalDecision, operatorID int64, requestID string) (*customer.QuoteApproval, error) {
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
		now := time.Now()
		for _, decision := range decisions {
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
		var undecided, rejected int64
		if err := tx.Table("customer_quote_approval_row").
			Where("approval_id = ? AND result = ?", approvalID, customer.ApprovalRowPending).
			Count(&undecided).Error; err != nil {
			return fmt.Errorf("count pending rows: %w", err)
		}
		if undecided > 0 {
			return customer.ErrQuoteApprovalRows
		}
		if err := tx.Table("customer_quote_approval_row").
			Where("approval_id = ? AND result = ?", approvalID, customer.ApprovalRowRejected).
			Count(&rejected).Error; err != nil {
			return fmt.Errorf("count rejected rows: %w", err)
		}
		final := customer.QuoteApprovalApproved
		quoteStatus := customer.QuoteStatusApproved
		if rejected > 0 {
			final = customer.QuoteApprovalRejected
			// 驳回后报价单回到草稿：销售改价/换来源再提，而不是留一张卡住的单。
			quoteStatus = customer.QuoteStatusDraft
		}
		if err := tx.Table("customer_quote_approval").Where("id = ?", approvalID).Updates(map[string]any{
			"status":     final,
			"decided_by": operatorID,
			"decided_at": now,
			"updated_at": now,
			"request_id": requestID,
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
		if err := r.audit.Record(ctx, AuditEntry{
			OperatorID: operatorID, OperatorRole: "STAFF",
			Action: "QUOTE_APPROVE", TargetType: "CUSTOMER_QUOTE", TargetID: approval.QuoteID,
			SourceType: "HUMAN", RequestID: requestID,
			AfterValue: map[string]any{
				"approval_id": approvalID, "result": final,
				"rejected": rejected, "decisions": decisions,
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

// WithdrawQuoteApprovalTx 撤回：PENDING → WITHDRAWN，报价单回 DRAFT。
func (r *CustomerSpecialRepo) WithdrawQuoteApprovalTx(ctx context.Context, approvalID int64, operatorID int64, requestID string) (*customer.QuoteApproval, error) {
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
			"status":     customer.QuoteApprovalWithdrawn,
			"updated_at": now,
			"request_id": requestID,
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
			AfterValue: map[string]any{"approval_id": approvalID, "status": customer.QuoteApprovalWithdrawn},
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
	ID           int64      `gorm:"column:id"`
	QuoteID      int64      `gorm:"column:customer_quote_id"`
	Status       string     `gorm:"column:status"`
	ModelCount   int        `gorm:"column:model_count"`
	Note         *string    `gorm:"column:note"`
	SubmittedBy  int64      `gorm:"column:submitted_by"`
	SubmittedAt  time.Time  `gorm:"column:submitted_at"`
	DecidedBy    *int64     `gorm:"column:decided_by"`
	DecidedAt    *time.Time `gorm:"column:decided_at"`
	CustomerID   int64      `gorm:"column:customer_id"`
	QuoteVersion int        `gorm:"column:quote_version"`
	QuoteStatus  string     `gorm:"column:quote_status"`
}

// LoadQuoteApproval 读审批单 + 逐模型行 + 报价单当前状态。
func (r *CustomerSpecialRepo) LoadQuoteApproval(ctx context.Context, approvalID int64) (*customer.QuoteApproval, error) {
	var head quoteApprovalHead
	err := r.txOf(ctx).Table("customer_quote_approval a").
		Select(`a.id, a.customer_quote_id, a.status, a.model_count, a.note, a.submitted_by,
		        a.submitted_at, a.decided_by, a.decided_at,
		        q.customer_id, q.version_no AS quote_version, q.status AS quote_status`).
		Joins("JOIN customer_quote q ON q.id = a.customer_quote_id").
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
		CustomerID: head.CustomerID, QuoteVersion: head.QuoteVersion, QuoteStatus: head.QuoteStatus,
		Rows: make([]customer.QuoteApprovalRow, 0, len(rows)),
	}
	if head.Note != nil {
		out.Note = *head.Note
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
		out.Rows = append(out.Rows, item)
	}
	return out, nil
}

// ListQuoteApprovals 列审批单。customerID=0 表示跨客户（客户报价单页签的全局审批队列）。
func (r *CustomerSpecialRepo) ListQuoteApprovals(ctx context.Context, customerID int64, status string, limit int) ([]customer.QuoteApprovalSummary, error) {
	tx := r.txOf(ctx).Table("customer_quote_approval a").
		Select("a.id, a.customer_quote_id AS quote_id, q.customer_id, a.status, a.model_count, a.submitted_by, a.submitted_at, a.decided_by, a.decided_at").
		Joins("JOIN customer_quote q ON q.id = a.customer_quote_id")
	if customerID > 0 {
		tx = tx.Where("q.customer_id = ?", customerID)
	}
	if status != "" {
		tx = tx.Where("a.status = ?", status)
	}
	var rows []struct {
		ID          int64      `gorm:"column:id"`
		QuoteID     int64      `gorm:"column:quote_id"`
		CustomerID  int64      `gorm:"column:customer_id"`
		Status      string     `gorm:"column:status"`
		ModelCount  int        `gorm:"column:model_count"`
		SubmittedBy int64      `gorm:"column:submitted_by"`
		SubmittedAt time.Time  `gorm:"column:submitted_at"`
		DecidedBy   *int64     `gorm:"column:decided_by"`
		DecidedAt   *time.Time `gorm:"column:decided_at"`
	}
	if err := tx.Order("a.submitted_at DESC, a.id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list quote approvals: %w", err)
	}
	out := make([]customer.QuoteApprovalSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, customer.QuoteApprovalSummary{
			ID: row.ID, QuoteID: row.QuoteID, CustomerID: row.CustomerID, Status: row.Status,
			ModelCount: row.ModelCount, SubmittedBy: row.SubmittedBy, SubmittedAt: row.SubmittedAt,
			DecidedBy: row.DecidedBy, DecidedAt: row.DecidedAt,
		})
	}
	return out, nil
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
