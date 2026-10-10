// Package repo 的 finance.go：财务中心只读域的 GORM 实现（12-finance.md §1/§2/§3）。
//
// 行级过滤（红线 7）：
//   - customer_profile.owner_sales_operator_id 是「销售归属」，
//     与 internal/repo/customer.go 的 applyCustomerOwnerScope 同形态（指向 owner_sales_operator_id），
//     做 SELF/DEPT/DEPT_SUB/ALL 四档过滤。
//
// 列扫描纪律（坑位表）：列表/详情用 flat struct + 显式 gorm:"column:..."，
// 绝不内嵌带 TableName 的行模型（否则 embedded 列全丢零值）。
package repo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"model_bss/internal/domain/finance"
	"model_bss/internal/infra/db"
)

// FinanceRepo 实现 finance.ReadStore 与 finance.LockStore。
type FinanceRepo struct {
	base  *gorm.DB
	audit *AuditRepo
}

// NewFinanceRepo 构造。
func NewFinanceRepo(base *gorm.DB) *FinanceRepo {
	return &FinanceRepo{base: base, audit: NewAuditRepo(base)}
}

var (
	_ finance.ReadStore = (*FinanceRepo)(nil)
	_ finance.LockStore = (*FinanceRepo)(nil)
)

func (r *FinanceRepo) txOf(ctx context.Context) *gorm.DB {
	if tx := db.FromContext(ctx); tx != nil {
		return tx
	}
	return r.base
}

// ============================================================
// 行级过滤 Scope（红线 7）
// ============================================================

// applyFinanceOwnerScope 把数据域过滤应用到「JOIN customer_profile cp 之后」的查询上。
// 与 customer.applyCustomerOwnerScope 同形态，指向 owner_sales_operator_id（销售归属）。
func applyFinanceOwnerScope(tx *gorm.DB, scope finance.OwnerScope) *gorm.DB {
	switch scope.DataScope {
	case "ALL":
		return tx
	case "DEPT", "DEPT_SUB":
		tx = tx.Joins("JOIN internal_staff os_ ON os_.id = cp.owner_sales_operator_id")
		if scope.DataScope == "DEPT" {
			return tx.Where("os_.org_unit_id = ?", scope.MyOrgID)
		}
		// DEPT_SUB：负责人可见下属整棵子树。
		if len(scope.ScopePaths) == 0 {
			return tx.Where("os_.org_unit_id = ?", scope.MyOrgID)
		}
		patterns := make([]string, 0, len(scope.ScopePaths))
		for _, p := range scope.ScopePaths {
			patterns = append(patterns, p+"%")
		}
		return tx.Joins("JOIN org_unit ou_ ON ou_.id = os_.org_unit_id").
			Where("ou_.path LIKE ANY(?)", patterns)
	default: // SELF
		return tx.Where("cp.owner_sales_operator_id = ?", scope.StaffID)
	}
}

// accountSelect 是账户行扫描的固定列集（NULL 用 COALESCE 兜底）。
const accountSelect = `cp.id, ls.legal_name, COALESCE(ls.uscc, '') AS uscc,
	cp.level_code, cp.credit_status, cp.credit_limit, cp.credit_used, cp.deposit_amount,
	cp.payment_type, cp.billing_cycle, cp.updated_at`

// accountScanRow 是 flat 扫描行（列名全部显式声明）。
type accountScanRow struct {
	ID            int64     `gorm:"column:id"`
	LegalName     string    `gorm:"column:legal_name"`
	Uscc          string    `gorm:"column:uscc"`
	LevelCode     string    `gorm:"column:level_code"`
	CreditStatus  string    `gorm:"column:credit_status"`
	CreditLimit   string    `gorm:"column:credit_limit"`
	CreditUsed    string    `gorm:"column:credit_used"`
	DepositAmount string    `gorm:"column:deposit_amount"`
	PaymentType   string    `gorm:"column:payment_type"`
	BillingCycle  int       `gorm:"column:billing_cycle"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
}

func (row accountScanRow) toDomain() finance.AccountRow {
	return finance.AccountRow{
		ID:            row.ID,
		LegalName:     row.LegalName,
		Uscc:          row.Uscc,
		LevelCode:     row.LevelCode,
		CreditStatus:  row.CreditStatus,
		CreditLimit:   row.CreditLimit,
		CreditUsed:    row.CreditUsed,
		DepositAmount: row.DepositAmount,
		PaymentType:   row.PaymentType,
		BillingCycle:  row.BillingCycle,
		UpdatedAt:     row.UpdatedAt,
	}
}

// ============================================================
// §1 汇率快照
// ============================================================

// ListFxLocks 返回全部汇率锁定行（按月一条，表很小，不分页）。
func (r *FinanceRepo) ListFxLocks(ctx context.Context) ([]finance.FxLockRow, error) {
	var rows []struct {
		ID           int64     `gorm:"column:id"`
		Ym           string    `gorm:"column:ym"`
		FxTier       string    `gorm:"column:fx_tier"`
		LockedByName string    `gorm:"column:locked_by_name"`
		LockedAt     time.Time `gorm:"column:locked_at"`
		AdjustReason string    `gorm:"column:adjust_reason"`
		UpdatedAt    time.Time `gorm:"column:updated_at"`
	}
	err := r.txOf(ctx).Table("fx_rate_lock fl").
		Joins("LEFT JOIN internal_staff st ON st.id = fl.locked_by").
		Select("fl.id, fl.ym, fl.fx_tier, COALESCE(st.name, '') AS locked_by_name, fl.locked_at, COALESCE(fl.adjust_reason, '') AS adjust_reason, fl.updated_at").
		Order("fl.ym DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list fx_rate_lock: %w", err)
	}
	out := make([]finance.FxLockRow, 0, len(rows))
	for _, rw := range rows {
		out = append(out, finance.FxLockRow{
			ID:           rw.ID,
			Ym:           rw.Ym,
			FxTier:       rw.FxTier,
			LockedByName: rw.LockedByName,
			LockedAt:     rw.LockedAt,
			AdjustReason: rw.AdjustReason,
			UpdatedAt:    rw.UpdatedAt,
		})
	}
	return out, nil
}

// ============================================================
// §4/§5 汇率锁定（当月状态 + 锁定/调整）
// ============================================================

// GetFxLock 实现 finance.LockStore.GetFxLock：读指定月的锁定行。
func (r *FinanceRepo) GetFxLock(ctx context.Context, ym string) (*finance.FxLockRow, bool, error) {
	var row struct {
		ID           int64     `gorm:"column:id"`
		Ym           string    `gorm:"column:ym"`
		FxTier       string    `gorm:"column:fx_tier"`
		LockedByName string    `gorm:"column:locked_by_name"`
		LockedAt     time.Time `gorm:"column:locked_at"`
		AdjustReason string    `gorm:"column:adjust_reason"`
		UpdatedAt    time.Time `gorm:"column:updated_at"`
	}
	err := r.txOf(ctx).Table("fx_rate_lock fl").
		Joins("LEFT JOIN internal_staff st ON st.id = fl.locked_by").
		Select("fl.id, fl.ym, fl.fx_tier, COALESCE(st.name, '') AS locked_by_name, fl.locked_at, COALESCE(fl.adjust_reason, '') AS adjust_reason, fl.updated_at").
		Where("fl.ym = ?", ym).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get fx_rate_lock %s: %w", ym, err)
	}
	return &finance.FxLockRow{
		ID:           row.ID,
		Ym:           row.Ym,
		FxTier:       row.FxTier,
		LockedByName: row.LockedByName,
		LockedAt:     row.LockedAt,
		AdjustReason: row.AdjustReason,
		UpdatedAt:    row.UpdatedAt,
	}, true, nil
}

// fxLockInsertRow 是 fx_rate_lock 的插入行模型（只用于 INSERT，字段显式声明）。
type fxLockInsertRow struct {
	ID           int64     `gorm:"primaryKey"`
	Ym           string    `gorm:"column:ym"`
	FxTier       string    `gorm:"column:fx_tier"`
	LockedBy     int64     `gorm:"column:locked_by"`
	LockedAt     time.Time `gorm:"column:locked_at"`
	AdjustReason *string   `gorm:"column:adjust_reason"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
	RequestID    *string   `gorm:"column:request_id"`
	CreatedBy    *int64    `gorm:"column:created_by"`
	UpdatedBy    *int64    `gorm:"column:updated_by"`
}

// UpsertFxLockTx 实现 finance.LockStore.UpsertFxLockTx。
//
// 单事务：
//  1. SELECT ... FOR UPDATE 锁定当月行；
//  2. 存在 → UPDATE（调整）；不存在 → INSERT（首次锁定）；
//  3. 读锁定人姓名；
//  4. audit_log（action=FX_LOCK / FX_LOCK_ADJUST，含档位新旧值与理由）。
func (r *FinanceRepo) UpsertFxLockTx(ctx context.Context, in finance.LockTxInput) (*finance.LockTxResult, error) {
	now := time.Now().UTC()
	var (
		rowID        int64
		adjusted     bool
		lockedByName string
	)

	txErr := r.txOf(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 锁定当月行。
		var cur struct {
			ID           int64   `gorm:"column:id"`
			FxTier       string  `gorm:"column:fx_tier"`
			AdjustReason *string `gorm:"column:adjust_reason"`
		}
		err := tx.Table("fx_rate_lock").
			Where("ym = ?", in.Ym).
			Select("id, fx_tier, adjust_reason").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Take(&cur).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("lock fx_rate_lock %s: %w", in.Ym, err)
		}

		var before map[string]any
		if err == nil {
			// 2a. UPDATE（调整）。
			adjusted = true
			rowID = cur.ID
			before = map[string]any{"fx_tier": cur.FxTier, "adjust_reason": derefString(cur.AdjustReason)}
			if uerr := tx.Table("fx_rate_lock").Where("id = ?", cur.ID).Updates(map[string]any{
				"fx_tier":       in.FxTier,
				"adjust_reason": in.AdjustReason,
				"locked_by":     in.OperatorID,
				"locked_at":     now,
				"updated_at":    now,
				"updated_by":    in.OperatorID,
				"request_id":    in.RequestID,
			}).Error; uerr != nil {
				return fmt.Errorf("update fx_rate_lock %s: %w", in.Ym, uerr)
			}
		} else {
			// 2b. INSERT（首次锁定；adjust_reason 恒 NULL）。
			ins := fxLockInsertRow{
				Ym:        in.Ym,
				FxTier:    in.FxTier,
				LockedBy:  in.OperatorID,
				LockedAt:  now,
				CreatedAt: now,
				UpdatedAt: now,
				RequestID: strPtr(in.RequestID),
				CreatedBy: int64Ptr(in.OperatorID),
				UpdatedBy: int64Ptr(in.OperatorID),
			}
			if ierr := tx.Table("fx_rate_lock").Create(&ins).Error; ierr != nil {
				return fmt.Errorf("insert fx_rate_lock %s: %w", in.Ym, ierr)
			}
			rowID = ins.ID
		}

		// 3. 锁定人姓名。
		if serr := tx.Table("internal_staff").Where("id = ?", in.OperatorID).
			Select("name").Scan(&lockedByName).Error; serr != nil {
			return fmt.Errorf("load operator name %d: %w", in.OperatorID, serr)
		}

		// 4. audit_log（同事务）。
		action := finance.AuditActionFxLock
		if adjusted {
			action = finance.AuditActionFxLockAdjust
		}
		if aerr := r.audit.Record(db.WithDB(ctx, tx), AuditEntry{
			OperatorID:   in.OperatorID,
			OperatorRole: in.OperatorRole,
			Action:       action,
			TargetType:   "FX_RATE_LOCK",
			TargetID:     rowID,
			BeforeValue:  before,
			AfterValue: map[string]any{
				"ym":            in.Ym,
				"fx_tier":       in.FxTier,
				"adjust_reason": in.AdjustReason,
			},
			Reason:    in.AdjustReason,
			RequestID: in.RequestID,
		}); aerr != nil {
			return fmt.Errorf("write audit_log: %w", aerr)
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return &finance.LockTxResult{
		ID:           rowID,
		Ym:           in.Ym,
		FxTier:       in.FxTier,
		Adjusted:     adjusted,
		LockedAt:     now,
		LockedByName: lockedByName,
	}, nil
}

// derefString 解引用可空字符串（nil → 空串）。
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ============================================================
// §2 客户财务账户列表
// ============================================================

// accountBase 构造带行级过滤 + 关键词/状态过滤的基础查询（每次调用返回新 builder）。
func (r *FinanceRepo) accountBase(ctx context.Context, scope finance.OwnerScope, keyword, status string) *gorm.DB {
	tx := r.txOf(ctx).Table("customer_profile cp").
		Joins("JOIN legal_subject ls ON ls.id = cp.subject_id")
	tx = applyFinanceOwnerScope(tx, scope)
	if keyword != "" {
		tx = tx.Where("(ls.legal_name ILIKE ? OR COALESCE(ls.uscc, '') ILIKE ?)", "%"+keyword+"%", "%"+keyword+"%")
	}
	if status != "" {
		tx = tx.Where("cp.credit_status = ?", status)
	}
	return tx
}

// ListAccounts 实现 finance.ReadStore.ListAccounts。
func (r *FinanceRepo) ListAccounts(ctx context.Context, scope finance.OwnerScope, keyword, status string, page, size int) (*finance.AccountPage, error) {
	var total int64
	if err := r.accountBase(ctx, scope, keyword, status).Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count accounts: %w", err)
	}
	if total == 0 {
		return &finance.AccountPage{Rows: []finance.AccountRow{}, Total: 0}, nil
	}
	var rows []accountScanRow
	if err := r.accountBase(ctx, scope, keyword, status).
		Select(accountSelect).
		Order("cp.updated_at DESC, cp.id DESC").
		Limit(size).Offset((page - 1) * size).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	out := make([]finance.AccountRow, 0, len(rows))
	for _, rw := range rows {
		out = append(out, rw.toDomain())
	}
	return &finance.AccountPage{Rows: out, Total: total}, nil
}

// ============================================================
// §3 客户财务账户详情
// ============================================================

// GetAccountInScope 实现 finance.ReadStore.GetAccountInScope。
//
// 先带行级过滤取单行：取到 → found=true, inScope=true；
// 取不到时再不带过滤判存在性：存在 → found=true, inScope=false（403）；不存在 → 404。
func (r *FinanceRepo) GetAccountInScope(ctx context.Context, customerID int64, scope finance.OwnerScope) (*finance.AccountRow, bool, bool, error) {
	var row accountScanRow
	tx := r.txOf(ctx).Table("customer_profile cp").
		Joins("JOIN legal_subject ls ON ls.id = cp.subject_id").
		Where("cp.id = ?", customerID)
	tx = applyFinanceOwnerScope(tx, scope)
	err := tx.Select(accountSelect).Take(&row).Error
	if err == nil {
		d := row.toDomain()
		return &d, true, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, false, fmt.Errorf("load account %d: %w", customerID, err)
	}
	// 存在性判定（不带过滤）。
	var cnt int64
	if cerr := r.txOf(ctx).Table("customer_profile cp").
		Where("cp.id = ?", customerID).Count(&cnt).Error; cerr != nil {
		return nil, false, false, fmt.Errorf("check account exists %d: %w", customerID, cerr)
	}
	if cnt == 0 {
		return nil, false, false, nil
	}
	return nil, true, false, nil
}

// ListRecentTransactions 实现 finance.ReadStore.ListRecentTransactions。
//
// 合并 credit_txn 与 deposit_txn：
//   - credit_txn 按 customer_id 过滤；
//   - deposit_txn 按 subject_side='CUSTOMER' AND profile_id 过滤（该表无 customer_id 列）。
//
// 各自按 created_at DESC 取前 limit 条，合并后再按时间倒序取前 limit 条。
func (r *FinanceRepo) ListRecentTransactions(ctx context.Context, customerID int64, limit int) ([]finance.TxnRow, error) {
	var credit []struct {
		ID        int64     `gorm:"column:id"`
		CreatedAt time.Time `gorm:"column:created_at"`
		Direction string    `gorm:"column:direction"`
		Amount    string    `gorm:"column:amount"`
		RefType   string    `gorm:"column:ref_type"`
		RefID     string    `gorm:"column:ref_id"`
	}
	if err := r.txOf(ctx).Table("credit_txn").
		Where("customer_id = ?", customerID).
		Select("id, created_at, direction, amount, COALESCE(ref_type, '') AS ref_type, COALESCE(ref_id::text, '') AS ref_id").
		Order("created_at DESC, id DESC").Limit(limit).
		Scan(&credit).Error; err != nil {
		return nil, fmt.Errorf("list credit_txn customer=%d: %w", customerID, err)
	}
	var deposit []struct {
		ID        int64     `gorm:"column:id"`
		CreatedAt time.Time `gorm:"column:created_at"`
		Direction string    `gorm:"column:direction"`
		Amount    string    `gorm:"column:amount"`
		Reason    string    `gorm:"column:reason"`
	}
	if err := r.txOf(ctx).Table("deposit_txn").
		Where("subject_side = 'CUSTOMER' AND profile_id = ?", customerID).
		Select("id, created_at, direction, amount, COALESCE(reason, '') AS reason").
		Order("created_at DESC, id DESC").Limit(limit).
		Scan(&deposit).Error; err != nil {
		return nil, fmt.Errorf("list deposit_txn customer=%d: %w", customerID, err)
	}

	out := make([]finance.TxnRow, 0, len(credit)+len(deposit))
	for _, c := range credit {
		out = append(out, finance.TxnRow{
			ID: c.ID, Source: "CREDIT", OccurredAt: c.CreatedAt,
			Direction: c.Direction, Amount: c.Amount, RefType: c.RefType, RefID: c.RefID,
		})
	}
	for _, d := range deposit {
		out = append(out, finance.TxnRow{
			ID: d.ID, Source: "DEPOSIT", OccurredAt: d.CreatedAt,
			Direction: d.Direction, Amount: d.Amount, Reason: d.Reason,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OccurredAt.After(out[j].OccurredAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
