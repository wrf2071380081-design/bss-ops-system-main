package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"model_bss/internal/domain/supply"
	"strings"
	"unicode/utf8"
)

type NoticeTxResolver func(context.Context) (*sql.Tx, error)
type NoticeLogAppender func(context.Context, *sql.Tx, supply.NoticeReadOperation) error
type NoticeUnitOfWork struct {
	resolve   NoticeTxResolver
	appendLog NoticeLogAppender
}

var _ supply.NoticeUnitOfWork = (*NoticeUnitOfWork)(nil)

func NewNoticeUnitOfWork(resolve NoticeTxResolver, appendLog NoticeLogAppender) *NoticeUnitOfWork {
	return &NoticeUnitOfWork{resolve: resolve, appendLog: appendLog}
}

// Within joins a request transaction and never begins, commits or rolls it back.
// The caller must roll back on error and commit before publishing any receipt.
func (w *NoticeUnitOfWork) Within(ctx context.Context, run func(supply.NoticeTransaction) error) error {
	if w == nil || w.resolve == nil || w.appendLog == nil || run == nil {
		return supply.ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := w.resolve(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return supply.ErrDependencyUnavailable
	}
	return run(&noticeWriteTransaction{tx: tx, appendLog: w.appendLog})
}
func validateReadOperation(op supply.NoticeReadOperation) error {
	if strings.TrimSpace(op.RequestID) == "" || !utf8.ValidString(op.RequestID) || utf8.RuneCountInString(op.RequestID) > 64 || strings.ContainsRune(op.RequestID, 0) {
		return &supply.FieldError{Path: "request_id", Reason: "expected nonempty trace within 64 characters"}
	}
	if strings.TrimSpace(op.Reason) == "" || !utf8.ValidString(op.Reason) || utf8.RuneCountInString(op.Reason) > 500 || strings.ContainsRune(op.Reason, 0) {
		return &supply.FieldError{Path: "reason", Reason: "expected nonempty reason within 500 characters"}
	}
	if op.EntityType != "notice" || !noticeID(op.EntityID) || !noticeID(op.ActorID) || op.OccurredAt.IsZero() || op.After == nil || op.After.NoticeID != op.EntityID || op.After.StaffID != op.ActorID || op.After.ReadAt.IsZero() {
		return supply.ErrReadContract
	}
	switch op.Action {
	case "READ":
		if op.Before != nil || !op.After.ReadAt.Equal(op.OccurredAt) {
			return supply.ErrReadContract
		}
	case "READ_NOOP":
		if !sameNoticeRead(op.Before, op.After) {
			return supply.ErrReadContract
		}
	default:
		return supply.ErrReadContract
	}
	return nil
}
func sameNoticeRead(a, b *supply.NoticeRead) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.NoticeID == b.NoticeID && a.StaffID == b.StaffID && a.ReadAt.Equal(b.ReadAt)
}

type noticeWriteTransaction struct {
	tx               *sql.Tx
	appendLog        NoticeLogAppender
	notice, staff    string
	looked, appended bool
	before, after    *supply.NoticeRead
}

var _ supply.NoticeTransaction = (*noticeWriteTransaction)(nil)

func (w *noticeWriteTransaction) NoticeForUpdate(ctx context.Context, id string) (*supply.Notice, error) {
	if !noticeID(id) || w.notice != "" {
		return nil, supply.ErrReadContract
	}
	var row NoticeRow
	var target, resource sql.NullString
	err := w.tx.QueryRowContext(ctx, `SELECT id::text,type,target_type,target_id::text,payload,created_at FROM public.notice n WHERE `+noticeContractScope+` AND n.id=$1::bigint FOR UPDATE`, id).Scan(&row.ID, &row.Kind, &target, &resource, &row.Payload, &row.PublishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, supply.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	row.TargetType = target.String
	row.ResourceID = resource.String
	n, err := row.DomainNotice()
	if err != nil {
		return nil, err
	}
	w.notice = id
	return n, nil
}
func (w *noticeWriteTransaction) InternalStaffExists(ctx context.Context, id string) (bool, error) {
	if w.notice == "" || !noticeID(id) || w.staff != "" {
		return false, supply.ErrReadContract
	}
	var actual string
	err := w.tx.QueryRowContext(ctx, `SELECT id::text FROM public.internal_staff WHERE id=$1::bigint FOR KEY SHARE`, id).Scan(&actual)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if actual != id {
		return false, supply.ErrReadContract
	}
	w.staff = id
	return true, nil
}
func (w *noticeWriteTransaction) GetReadForUpdate(ctx context.Context, notice, staff string) (*supply.NoticeRead, error) {
	if notice != w.notice || staff != w.staff || w.notice == "" || w.staff == "" || w.looked {
		return nil, supply.ErrReadContract
	}
	var read supply.NoticeRead
	err := w.tx.QueryRowContext(ctx, `SELECT notice_id::text,staff_id::text,read_at FROM public.notice_read WHERE notice_id=$1::bigint AND staff_id=$2::bigint FOR UPDATE`, notice, staff).Scan(&read.NoticeID, &read.StaffID, &read.ReadAt)
	if errors.Is(err, sql.ErrNoRows) {
		w.looked = true
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if read.NoticeID != notice || read.StaffID != staff || read.ReadAt.IsZero() {
		return nil, supply.ErrReadContract
	}
	read.ReadAt = read.ReadAt.UTC()
	w.looked = true
	w.before = read.Clone()
	w.after = read.Clone()
	return read.Clone(), nil
}
func (w *noticeWriteTransaction) CreateRead(ctx context.Context, read *supply.NoticeRead) error {
	if !w.looked || w.after != nil || read == nil || read.NoticeID != w.notice || read.StaffID != w.staff || read.ReadAt.IsZero() {
		return supply.ErrReadContract
	}
	result, err := w.tx.ExecContext(ctx, `INSERT INTO public.notice_read(notice_id,staff_id,read_at) VALUES ($1::bigint,$2::bigint,$3)`, read.NoticeID, read.StaffID, read.ReadAt.UTC())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return supply.ErrReadContract
	}
	w.after = read.Clone()
	return nil
}
func (w *noticeWriteTransaction) AppendReadOperation(ctx context.Context, op supply.NoticeReadOperation) error {
	if err := validateReadOperation(op); err != nil {
		return err
	}
	if w.appended || !w.looked || op.EntityID != w.notice || op.ActorID != w.staff || !sameNoticeRead(op.Before, w.before) || !sameNoticeRead(op.After, w.after) {
		return supply.ErrReadContract
	}
	op.Before = op.Before.Clone()
	op.After = op.After.Clone()
	if err := w.appendLog(ctx, w.tx, op); err != nil {
		return err
	}
	w.appended = true
	return nil
}
