package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"model_bss/internal/domain/supply"
	"strings"
)

type NoticeReadStore struct{ db *sql.DB }

var _ supply.NoticeReadStore = (*NoticeReadStore)(nil)

func NewNoticeReadStore(db *sql.DB) *NoticeReadStore { return &NoticeReadStore{db: db} }

const noticeReadFrom = ` FROM public.notice n CROSS JOIN public.internal_staff s
	LEFT JOIN public.notice_read nr ON nr.notice_id=n.id AND nr.staff_id=s.id `
const noticeReadColumns = `n.id::text,n.type,n.target_type,n.target_id::text,n.payload,n.created_at,s.id::text,s.name,nr.read_at`
const noticeContractScope = `n.payload->>'contract'='supply.notice'`

type noticeSelection struct {
	where, staffWhere string
	args, staffArgs   []any
}

// Called with the NoticeReader's normalized query. Validate again here so a
// direct adapter caller cannot overflow pagination or broaden staff selection.
func selectNotices(q supply.NoticeQuery) (noticeSelection, error) {
	selection := noticeSelection{}
	if q.Page < 1 || q.Size < 1 || q.Size > 100 || q.Page > math.MaxInt/q.Size || (q.AllStaff && len(q.StaffIDs) > 0) || (!q.AllStaff && len(q.StaffIDs) == 0) || len(q.StaffIDs) > 100 {
		return selection, supply.ErrReadContract
	}
	if q.ReadStatus != "ALL" && q.ReadStatus != "READ" && q.ReadStatus != "UNREAD" {
		return selection, supply.ErrReadContract
	}
	if q.Region != "" && q.Region != "CN" && q.Region != "OVERSEAS" {
		return selection, supply.ErrReadContract
	}
	if q.ResourceID != "" && !noticeID(q.ResourceID) {
		return selection, supply.ErrReadContract
	}
	where := []string{noticeContractScope}
	bind := func(value any) string {
		selection.args = append(selection.args, value)
		return fmt.Sprintf("$%d", len(selection.args))
	}
	if !q.AllStaff {
		seen := map[string]bool{}
		ids := []string{}
		for _, id := range q.StaffIDs {
			if !noticeID(id) || seen[id] {
				return selection, supply.ErrReadContract
			}
			seen[id] = true
			ids = append(ids, bind(id)+"::bigint")
		}
		selection.staffWhere = "s.id IN (" + strings.Join(ids, ",") + ")"
		selection.staffArgs = append([]any(nil), selection.args...)
		where = append(where, selection.staffWhere)
	}
	if q.Region != "" {
		where = append(where, "n.payload->>'region'="+bind(q.Region))
	}
	if q.ResourceID != "" {
		where = append(where, "n.target_id="+bind(q.ResourceID)+"::bigint")
	}
	if q.Keyword != "" {
		where = append(where, `strpos(lower(concat_ws(' ',n.payload->>'title',n.payload->>'content',n.payload->>'resource_name')),lower(`+bind(q.Keyword)+`::text))>0`)
	}
	if q.ReadStatus == "READ" {
		where = append(where, "nr.notice_id IS NOT NULL")
	}
	if q.ReadStatus == "UNREAD" {
		where = append(where, "nr.notice_id IS NULL")
	}
	selection.where = " WHERE " + strings.Join(where, " AND ")
	return selection, nil
}

func scanNoticeRead(scan interface{ Scan(...any) error }) (supply.NoticeReadRecord, error) {
	var record supply.NoticeReadRecord
	var row NoticeRow
	var target, resource sql.NullString
	var readAt sql.NullTime
	if err := scan.Scan(&row.ID, &row.Kind, &target, &resource, &row.Payload, &row.PublishedAt, &record.StaffID, &record.StaffName, &readAt); err != nil {
		return record, err
	}
	row.TargetType = target.String
	row.ResourceID = resource.String
	notice, err := row.DomainNotice()
	if err != nil {
		return supply.NoticeReadRecord{}, err
	}
	if !noticeID(record.StaffID) || strings.TrimSpace(record.StaffName) == "" {
		return supply.NoticeReadRecord{}, supply.ErrReadContract
	}
	record.Notice = notice
	if readAt.Valid {
		if readAt.Time.IsZero() {
			return supply.NoticeReadRecord{}, supply.ErrReadContract
		}
		record.Read = &supply.NoticeRead{NoticeID: notice.ID, StaffID: record.StaffID, ReadAt: readAt.Time.UTC()}
	}
	return record, nil
}

func (s *NoticeReadStore) ListNoticeReadStates(ctx context.Context, q supply.NoticeQuery) ([]supply.NoticeReadRecord, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, supply.ErrDependencyUnavailable
	}
	selection, err := selectNotices(q)
	if err != nil {
		return nil, 0, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	if !q.AllStaff {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM public.internal_staff s WHERE "+selection.staffWhere, selection.staffArgs...).Scan(&count); err != nil {
			return nil, 0, err
		}
		if count != len(q.StaffIDs) {
			return nil, 0, supply.ErrNotFound
		}
	}
	var total int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*)"+noticeReadFrom+selection.where, selection.args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args := append([]any(nil), selection.args...)
	args = append(args, q.Size, (q.Page-1)*q.Size)
	query := "SELECT " + noticeReadColumns + noticeReadFrom + selection.where + fmt.Sprintf(" ORDER BY n.created_at DESC,n.id DESC,s.id ASC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []supply.NoticeReadRecord{}
	for rows.Next() {
		row, err := scanNoticeRead(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return result, total, nil
}
func (s *NoticeReadStore) GetNoticeReadState(ctx context.Context, id, staff string) (supply.NoticeReadRecord, error) {
	empty := supply.NoticeReadRecord{}
	if s == nil || s.db == nil {
		return empty, supply.ErrDependencyUnavailable
	}
	if !noticeID(id) || !noticeID(staff) {
		return empty, supply.ErrReadContract
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	row, err := scanNoticeRead(tx.QueryRowContext(ctx, "SELECT "+noticeReadColumns+noticeReadFrom+" WHERE "+noticeContractScope+" AND n.id=$1::bigint AND s.id=$2::bigint", id, staff))
	if errors.Is(err, sql.ErrNoRows) {
		return empty, supply.ErrNotFound
	}
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(); err != nil {
		return empty, err
	}
	return row, nil
}
