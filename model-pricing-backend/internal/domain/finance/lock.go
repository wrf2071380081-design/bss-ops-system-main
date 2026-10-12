// Package finance 的 lock.go：汇率锁定读写（12-finance.md §4/§5，批次 6）。
//
//	GET  /api/internal/finance/fx-lock   当月状态（M10:V）
//	POST /api/internal/finance/fx-lock   锁定/调整（M10:E + 服务层 FINANCE 收敛）
//
// 业务规则：
//   - 档位必须 ∈ 固定 8 档（复用 pkg/fxtier），否则 400。
//   - 当月无记录 → INSERT（首次锁定）；当已有记录 → 视为**调整**，`adjust_reason` 必填，否则 400。
//   - 写操作落 audit_log（同事务），含操作人/时间/档位新旧值/理由（红线 10）。
//   - 月中调整本批为**简化版**：直接写 + 强制理由 + 审计；审批链不在本批（见遗留）。
package finance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"model_bss/pkg/fxtier"
)

// 审计 action（12-finance.md §5）。
const (
	AuditActionFxLock       = "FX_LOCK"        // 首次锁定
	AuditActionFxLockAdjust = "FX_LOCK_ADJUST" // 同月调整
)

// 汇率锁定状态（复用三态命名）。
const (
	LockStatusLocked      = FxStatusLocked      // 当月已锁定
	LockStatusPendingLock = FxStatusPendingLock // 当月未锁定
)

// 领域错误。
var (
	// ErrFxTierInvalid 档位不在固定 8 档内 → 400。
	ErrFxTierInvalid = errors.New("汇率档位不在可选范围内（6.5/6.7/6.75/6.8/6.85/6.9/6.95/7.0）")
	// ErrFxAdjustReasonRequired 同月已有记录属调整，adjust_reason 必填 → 400。
	ErrFxAdjustReasonRequired = errors.New("同月重复锁定属调整，必须填写 adjust_reason")
)

// LockView 是 GET /fx-lock 的响应（12-finance.md §4）。
type LockView struct {
	Month        string     `json:"month"`
	Status       string     `json:"status"`
	FxTier       string     `json:"fxTier"`
	LockedAt     *time.Time `json:"lockedAt"`
	LockedBy     string     `json:"lockedBy"`
	AdjustReason string     `json:"adjustReason"`
	Tiers        []string   `json:"tiers"`
}

// LockInput 是 POST /fx-lock 的输入。
type LockInput struct {
	FxTier       string
	AdjustReason string
}

// LockResult 是 POST /fx-lock 的响应（12-finance.md §5）。
type LockResult struct {
	Month    string    `json:"month"`
	FxTier   string    `json:"fxTier"`
	Status   string    `json:"status"`
	Adjusted bool      `json:"adjusted"`
	LockedAt time.Time `json:"lockedAt"`
	LockedBy string    `json:"lockedBy"`
}

// LockTxInput 是 UpsertFxLockTx 的入参（已校验、已规范化）。
type LockTxInput struct {
	Ym           string
	FxTier       string // 规范形式（如 "7.0"）
	AdjustReason string // 首次锁定为空串
	OperatorID   int64
	OperatorRole string // 仅用于审计落库
	RequestID    string
}

// LockTxResult 是 UpsertFxLockTx 的结果。
type LockTxResult struct {
	ID           int64
	Ym           string
	FxTier       string
	Adjusted     bool
	LockedAt     time.Time
	LockedByName string
}

// LockStore 是汇率锁定写侧的窄接口（GORM 实现见 internal/repo/finance.go）。
type LockStore interface {
	// GetFxLock 读取指定月的锁定行；found=false 表示未锁定。
	GetFxLock(ctx context.Context, ym string) (*FxLockRow, bool, error)
	// UpsertFxLockTx 单事务：SELECT FOR UPDATE → INSERT/UPDATE + audit_log。
	UpsertFxLockTx(ctx context.Context, in LockTxInput) (*LockTxResult, error)
}

// LockService 是汇率锁定服务（无状态，可并发）。
type LockService struct {
	store LockStore
	now   func() time.Time
	loc   *time.Location
}

// NewLockService 构造。now 为 nil 时用 time.Now；时区固定 Asia/Shanghai。
func NewLockService(store LockStore, now func() time.Time) *LockService {
	if now == nil {
		now = time.Now
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return &LockService{store: store, now: now, loc: loc}
}

// currentYM 返回当前月（Asia/Shanghai）。
func (s *LockService) currentYM() string { return s.now().In(s.loc).Format("2006-01") }

// GetCurrent 返回当月锁定状态与 8 档可选值。
func (s *LockService) GetCurrent(ctx context.Context) (*LockView, error) {
	ym := s.currentYM()
	view := &LockView{Month: ym, Status: LockStatusPendingLock, FxTier: "", Tiers: fxtier.Tiers()}
	row, found, err := s.store.GetFxLock(ctx, ym)
	if err != nil {
		return nil, fmt.Errorf("get fx lock %s: %w", ym, err)
	}
	if !found || row == nil {
		return view, nil
	}
	lockedAt := row.LockedAt
	view.Status = LockStatusLocked
	view.FxTier = canonicalTier(row.FxTier)
	view.LockedAt = &lockedAt
	view.LockedBy = row.LockedByName
	view.AdjustReason = row.AdjustReason
	return view, nil
}

// Lock 锁定/调整当月汇率档位（12-finance.md §5）。
//
// 步骤：
//  1. 校验档位 ∈ 8 档（数值等价），非法 → ErrFxTierInvalid。
//  2. 读当月现有记录：存在则为调整，adjust_reason 必填（否则 ErrFxAdjustReasonRequired）。
//  3. 单事务 upsert + audit_log。
func (s *LockService) Lock(ctx context.Context, in LockInput, operatorID int64, operatorRole, requestID string) (*LockResult, error) {
	canonical, ok := fxtier.Normalize(in.FxTier)
	if !ok {
		return nil, ErrFxTierInvalid
	}
	ym := s.currentYM()

	existing, found, err := s.store.GetFxLock(ctx, ym)
	if err != nil {
		return nil, fmt.Errorf("get fx lock %s: %w", ym, err)
	}
	reason := strings.TrimSpace(in.AdjustReason)
	if found && existing != nil && reason == "" {
		return nil, ErrFxAdjustReasonRequired
	}
	if !found {
		reason = "" // 首次锁定不写理由
	}

	res, err := s.store.UpsertFxLockTx(ctx, LockTxInput{
		Ym:           ym,
		FxTier:       canonical,
		AdjustReason: reason,
		OperatorID:   operatorID,
		OperatorRole: operatorRole,
		RequestID:    requestID,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert fx lock %s: %w", ym, err)
	}
	return &LockResult{
		Month:    res.Ym,
		FxTier:   canonicalTier(res.FxTier),
		Status:   LockStatusLocked,
		Adjusted: res.Adjusted,
		LockedAt: res.LockedAt,
		LockedBy: res.LockedByName,
	}, nil
}
