package supplypg

// 供应商「生产平台服务商」关联（000069）。
//
// 与 supplier.region 的区别：region 是历史遗留的主体归属，新口径下不再用于判定
// "能不能在国内/海外平台上架"。真正的事实是这张表：某个供应商在某个生产平台上
// 有一个固定标识的服务商账号，人工登记、人工确认。
//
// 三条纪律：
//  1. 同平台同标识唯一（部分唯一索引 uk_sps_platform_identifier，软删不占位）；
//  2. 登记/确认都不改变模型或资源的任何状态——它只是"账号归属"的事实记录；
//  3. 越权防护落在 SQL：所有方法都必须带 supplier_id 条件，防止跨主体探测。

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrPlatformServiceTaken 同平台同标识已被其他供应商关联。
	ErrPlatformServiceTaken = errors.New("该平台服务商标识已被关联，请核对后再登记")
	// ErrPlatformServiceNotFound 记录不存在，或不属于该主体。
	ErrPlatformServiceNotFound = errors.New("平台服务商记录不存在")
)

// PlatformService 一行平台服务商关联。
type PlatformService struct {
	ID          int64
	SupplierID  int64
	Platform    string // CN / OVERSEAS
	ServiceName string
	Identifier  string
	Status      string // PENDING（待核对）/ CONFIRMED（已关联）
	CreatedAt   time.Time
	ConfirmedAt *time.Time
}

// PlatformServiceStore 平台服务商关联仓储。
type PlatformServiceStore struct {
	db     *sql.DB
	schema string
}

// NewPlatformServiceStore 构造。
func NewPlatformServiceStore(db *sql.DB, schema string) (*PlatformServiceStore, error) {
	if db == nil || !modelSchemaName.MatchString(schema) {
		return nil, mappingError("supplier platform service store")
	}
	return &PlatformServiceStore{db: db, schema: schema}, nil
}

func (s *PlatformServiceStore) table(name string) string { return s.schema + "." + name }

const platformServiceColumns = `id, supplier_id, platform, service_name, identifier, status, created_at, confirmed_at`

func scanPlatformService(row interface{ Scan(...any) error }) (*PlatformService, error) {
	var item PlatformService
	var confirmedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.SupplierID, &item.Platform, &item.ServiceName, &item.Identifier, &item.Status, &item.CreatedAt, &confirmedAt); err != nil {
		return nil, err
	}
	if confirmedAt.Valid {
		at := confirmedAt.Time
		item.ConfirmedAt = &at
	}
	return &item, nil
}

// List 列出某主体的全部平台服务商记录（含待核对）。
func (s *PlatformServiceStore) List(ctx context.Context, supplierID int64) ([]PlatformService, error) {
	if supplierID <= 0 {
		return nil, ErrPlatformServiceNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+platformServiceColumns+` FROM `+s.table("supplier_platform_service")+`
WHERE supplier_id = $1 AND deleted_at IS NULL ORDER BY platform, id`, supplierID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]PlatformService, 0, 2)
	for rows.Next() {
		item, scanErr := scanPlatformService(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// Create 登记一条平台服务商记录（默认待核对）。
func (s *PlatformServiceStore) Create(ctx context.Context, supplierID int64, platform, serviceName, identifier string, staffID int64) (*PlatformService, error) {
	row := s.db.QueryRowContext(ctx, `INSERT INTO `+s.table("supplier_platform_service")+`
(supplier_id, platform, service_name, identifier, status, linked_by)
VALUES ($1,$2,$3,$4,'PENDING',$5)
RETURNING `+platformServiceColumns, supplierID, platform, serviceName, identifier, staffID)
	item, err := scanPlatformService(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrPlatformServiceTaken
		}
		return nil, err
	}
	return item, nil
}

// Confirm 确认关联（待核对 → 已关联）。
func (s *PlatformServiceStore) Confirm(ctx context.Context, supplierID, id, staffID int64) (*PlatformService, error) {
	row := s.db.QueryRowContext(ctx, `UPDATE `+s.table("supplier_platform_service")+`
SET status = 'CONFIRMED', confirmed_by = $3, confirmed_at = now(), updated_at = now()
WHERE id = $1 AND supplier_id = $2 AND deleted_at IS NULL
RETURNING `+platformServiceColumns, id, supplierID, staffID)
	item, err := scanPlatformService(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPlatformServiceNotFound
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

// Remove 软删一条记录（历史留给操作记录回溯）。
func (s *PlatformServiceStore) Remove(ctx context.Context, supplierID, id int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE `+s.table("supplier_platform_service")+`
SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND supplier_id = $2 AND deleted_at IS NULL`, id, supplierID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrPlatformServiceNotFound
	}
	return nil
}
