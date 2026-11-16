// Package api 的 staff_position.go：内部人员业务岗位（000070）。
//
// 与权限的关系：**只用于展示与职责沟通**。多岗位不授予超管、不改变审批链与办理人规则；
// 权限仍由 role_permission、data_scope 与指派关系决定。
package api

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/pkg/apperr"
	"model_bss/pkg/response"
)

// staffPositions 是允许的业务岗位枚举。
var staffPositions = map[string]string{
	"OPS":   "运营",
	"BIZ":   "商务",
	"SALES": "销售",
}

// StaffPositionStore 业务岗位读写。
type StaffPositionStore struct {
	db     *sql.DB
	schema string
}

// NewStaffPositionStore 构造。
func NewStaffPositionStore(db *sql.DB, schema string) (*StaffPositionStore, error) {
	if db == nil || schema == "" {
		return nil, errors.New("staff position store: invalid args")
	}
	return &StaffPositionStore{db: db, schema: schema}, nil
}

func (s *StaffPositionStore) table() string { return s.schema + ".internal_staff_position" }

// ListAll 返回 staff_id → 岗位列表（一次查全表，页面一次渲染就要全部）。
func (s *StaffPositionStore) ListAll(ctx context.Context) (map[int64][]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT staff_id, position FROM `+s.table()+` ORDER BY staff_id, position`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[int64][]string)
	for rows.Next() {
		var staffID int64
		var position string
		if err := rows.Scan(&staffID, &position); err != nil {
			return nil, err
		}
		out[staffID] = append(out[staffID], position)
	}
	return out, rows.Err()
}

// ListByStaff 返回某个人的岗位。
func (s *StaffPositionStore) ListByStaff(ctx context.Context, staffID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT position FROM `+s.table()+` WHERE staff_id = $1 ORDER BY position`, staffID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]string, 0, 3)
	for rows.Next() {
		var position string
		if err := rows.Scan(&position); err != nil {
			return nil, err
		}
		out = append(out, position)
	}
	return out, rows.Err()
}

// Replace 全量替换某人的岗位（空切片 = 清空）。事务内先删后插，幂等。
func (s *StaffPositionStore) Replace(ctx context.Context, staffID int64, positions []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+s.table()+` WHERE staff_id = $1`, staffID); err != nil {
		return err
	}
	for _, position := range positions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+s.table()+` (staff_id, position, created_at) VALUES ($1,$2,$3)`,
			staffID, position, time.Now().UTC()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// StaffExists 校验人员是否存在（防把岗位挂到不存在的 id 上）。
func (s *StaffPositionStore) StaffExists(ctx context.Context, staffID int64) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.schema+`.internal_staff WHERE id = $1)`, staffID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// StaffPositionHandler 业务岗位接口。
type StaffPositionHandler struct {
	store *StaffPositionStore
}

// NewStaffPositionHandler 构造。
func NewStaffPositionHandler(store *StaffPositionStore) *StaffPositionHandler {
	if store == nil {
		return nil
	}
	return &StaffPositionHandler{store: store}
}

// list 返回全部人员的岗位（前端一次拿全，渲染账号列表的「业务岗位」列）。
func (h *StaffPositionHandler) list(c *gin.Context) {
	data, err := h.store.ListAll(c.Request.Context())
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	items := make([]gin.H, 0, len(data))
	for staffID, positions := range data {
		items = append(items, gin.H{"staff_id": strconv.FormatInt(staffID, 10), "positions": positions})
	}
	response.Success(c, gin.H{"list": items})
}

type staffPositionWrite struct {
	Positions []string `json:"positions"`
}

// save 全量替换某人的岗位（不改变任何权限）。
func (h *StaffPositionHandler) save(c *gin.Context) {
	op := middleware.OperatorFrom(c)
	if op == nil {
		response.Error(c, apperr.ErrUnauthorized)
		return
	}
	staffID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || staffID <= 0 {
		response.Error(c, apperr.ErrInvalidParams)
		return
	}
	var body staffPositionWrite
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "请求体格式不正确", 400))
		return
	}
	seen := make(map[string]struct{}, len(body.Positions))
	cleaned := make([]string, 0, len(body.Positions))
	for _, raw := range body.Positions {
		position := strings.ToUpper(strings.TrimSpace(raw))
		if position == "" {
			continue
		}
		if _, ok := staffPositions[position]; !ok {
			response.Error(c, apperr.New(apperr.ErrInvalidParams.Code, "岗位只支持 OPS / BIZ / SALES", 400))
			return
		}
		if _, dup := seen[position]; dup {
			continue
		}
		seen[position] = struct{}{}
		cleaned = append(cleaned, position)
	}
	exists, err := h.store.StaffExists(c.Request.Context(), staffID)
	if err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	if !exists {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	if err := h.store.Replace(c.Request.Context(), staffID, cleaned); err != nil {
		response.Error(c, apperr.ErrSystem)
		return
	}
	middleware.SetIdempotencyResult(c, gin.H{"staff_id": strconv.FormatInt(staffID, 10), "positions": cleaned})
	response.Success(c, gin.H{"staff_id": strconv.FormatInt(staffID, 10), "positions": cleaned})
}

// registerStaffPositionRoutes 挂载业务岗位路由（M11 查看 / M11:E 维护）。
func registerStaffPositionRoutes(protected *gin.RouterGroup, deps Deps, idempotent gin.HandlerFunc) {
	if deps.StaffPositions == nil {
		return
	}
	handler := NewStaffPositionHandler(deps.StaffPositions)
	protected.GET("/staff-positions", handler.list)
	protected.PUT("/staff/:id/positions", idempotent, handler.save)
}
