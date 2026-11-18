package supplypg

// 手工登记供应资源（2026-11-18，PM 需求：没有货源的模型允许直接录一条资源）。
//
// 定位：这是**运营兜底**，不是准入通道。
//   · 只允许给「当前没有任何资源」的模型录一条，避免绕过既有准入链路去改已有货源；
//   · 落库状态固定 PENDING_TEST（待测试），**不直接变成已准入**；
//     想变成可报价的 ADMITTED，仍要走测试与准入流程——红线「无测试不接入」不变；
//   · 每次登记都写 operation_log（entity_type=resource, action=MANUAL_RESOURCE_REGISTER），
//     谁录的、系数多少、什么时间都能追。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrManualResourceExists 该模型已有资源，不允许手工再加一条。
var ErrManualResourceExists = errors.New("该模型已有供应资源，请到供应商侧维护，不支持手工新增")

// ManualResourceInput 是手工登记一条资源的入参。
type ManualResourceInput struct {
	ModelID         int64
	SupplierID      int64
	SupplierModel   string
	Coefficient     string
	Region          string
	ActorID         int64
	RequestID       string
	Reason          string
}

// ManualResourceStore 手工登记资源。
type ManualResourceStore struct {
	db     *sql.DB
	schema string
}

// NewManualResourceStore 构造。
func NewManualResourceStore(db *sql.DB, schema string) (*ManualResourceStore, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("manual resource store")
	}
	return &ManualResourceStore{db: db, schema: schema}, nil
}

func (s *ManualResourceStore) table(name string) string { return s.schema + "." + name }

// Create 建一条 PENDING_TEST 资源并留痕，返回新资源 id。
func (s *ManualResourceStore) Create(ctx context.Context, in ManualResourceInput) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var existing int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+s.table("resource")+` WHERE standard_model_id = $1`, in.ModelID).Scan(&existing); err != nil {
		return 0, fmt.Errorf("count resources: %w", err)
	}
	if existing > 0 {
		return 0, ErrManualResourceExists
	}

	var resourceID int64
	now := time.Now().UTC()
	err = tx.QueryRowContext(ctx, `INSERT INTO `+s.table("resource")+`
(standard_model_id, supplier_id, supplier_model_name, standard_model_name_snapshot, region, coefficient,
 state, ever_admitted, buyer_id, created_at, updated_at, created_by, updated_by, request_id)
VALUES ($1,$2,$3,(SELECT name FROM `+s.table("standard_model")+` WHERE id=$1),$4,$5::numeric,
 'PENDING_TEST',false,$6,$7,$7,$6,$6,NULLIF($8,''))
RETURNING id`, in.ModelID, in.SupplierID, in.SupplierModel, in.Region, in.Coefficient, in.ActorID, now, in.RequestID).Scan(&resourceID)
	if err != nil {
		return 0, fmt.Errorf("insert manual resource: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO `+s.table("operation_log")+`
(entity_type, entity_id, action, operator_id, operator_name_snapshot, "after", request_id, created_at)
VALUES ('resource', $1, 'MANUAL_RESOURCE_REGISTER', $2,
        (SELECT name FROM `+s.table("internal_staff")+` WHERE id = $2), $3::jsonb, NULLIF($4,''), $5)`,
		resourceID, in.ActorID,
		fmt.Sprintf(`{"standard_model_id":%d,"supplier_id":%d,"coefficient":%q,"region":%q,"state":"PENDING_TEST","reason":%q}`,
			in.ModelID, in.SupplierID, in.Coefficient, in.Region, in.Reason),
		in.RequestID, now); err != nil {
		return 0, fmt.Errorf("write manual resource log: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return resourceID, nil
}
