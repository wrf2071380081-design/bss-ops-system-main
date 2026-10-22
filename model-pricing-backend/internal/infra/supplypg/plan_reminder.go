package supplypg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"model_bss/internal/domain/supply"
	"strconv"
	"time"
)

// PlanReminderStore publishes one all-staff notice when an active state plan
// reaches its expected time. It only creates a notice; resource state is never
// changed by this worker.
type PlanReminderStore struct {
	db    *sql.DB
	plans *PlanStore
}

func NewPlanReminderStore(db *sql.DB, plans *PlanStore) (*PlanReminderStore, error) {
	if db == nil || plans == nil || plans.state == nil || plans.schema != "public" {
		return nil, supply.ErrDependencyUnavailable
	}
	return &PlanReminderStore{db: db, plans: plans}, nil
}

func (s *PlanReminderStore) PublishDuePlanReminders(ctx context.Context, now time.Time, limit int) (int, error) {
	if s == nil || s.db == nil || s.plans == nil || ctx == nil || limit < 1 || limit > 500 || now.IsZero() {
		return 0, supply.ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT p.id::text
FROM public.plan p
JOIN LATERAL (
  SELECT e.id FROM public.event_outbox e
  WHERE e.event_type='PLAN_REMINDER_SCHEDULED'
    AND e.payload->>'schedule'='true'
    AND e.payload->>'plan_id'=p.id::text
    AND e.payload->>'revision'=p."after"->'plan'->>'revision'
  ORDER BY e.id DESC LIMIT 1
) scheduled ON true
WHERE p.kind IN ('ONLINE','OFFLINE') AND p.status='PENDING' AND p.due_at <= $1
  AND NOT EXISTS (
    SELECT 1 FROM public.notice n
    WHERE n.type='ONLINE_OFFLINE_PLAN' AND n.payload->>'source_event_id'=scheduled.id::text
  )
ORDER BY p.due_at,p.id LIMIT $2`, now.UTC(), limit)
	if err != nil {
		return 0, fmt.Errorf("list due supply plans: %w", err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	created := 0
	for _, id := range ids {
		published, err := s.publishOne(ctx, id, now.UTC().Truncate(time.Microsecond))
		if err != nil {
			return created, fmt.Errorf("publish reminder for plan %s: %w", id, err)
		}
		if published {
			created++
		}
	}
	return created, nil
}

func (s *PlanReminderStore) publishOne(ctx context.Context, id string, now time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var lockedID string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM public.plan WHERE id=$1 FOR UPDATE`, id).Scan(&lockedID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	plan, err := s.plans.current(ctx, tx, lockedID)
	if err != nil {
		return false, err
	}
	if plan.Status != "PENDING" || (plan.Kind != "ONLINE" && plan.Kind != "OFFLINE") || plan.DueAt.After(now) {
		return false, tx.Commit()
	}
	var eventID string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM public.event_outbox
WHERE event_type='PLAN_REMINDER_SCHEDULED' AND payload->>'schedule'='true'
  AND payload->>'plan_id'=$1 AND payload->>'revision'=$2
ORDER BY id DESC LIMIT 1`, plan.ID, strconv.FormatUint(plan.Revision, 10)).Scan(&eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, supply.ErrReadContract
	}
	if err != nil {
		return false, err
	}
	resource, err := s.plans.state.Current(ctx, tx, plan.ResourceID)
	if err != nil {
		return false, err
	}
	var exists bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.notice WHERE type='ONLINE_OFFLINE_PLAN' AND payload->>'source_event_id'=$1)`, eventID).Scan(&exists)
	if err != nil {
		return false, err
	}
	if exists {
		return false, tx.Commit()
	}
	title := "下架计划到点提醒"
	if plan.Kind == "ONLINE" {
		title = "上架计划到点提醒"
	}
	notice := &supply.Notice{
		ResourceID: resource.ID, ResourceName: resource.SupplierModelID, Region: resource.Region,
		Kind: "ONLINE_OFFLINE_PLAN", Title: title,
		Content: "预计时间已到，请在对应平台完成实际操作后登记。", Reason: plan.Reason,
		SourceEventID: eventID, AffectedStatus: "PENDING", PublishedAt: now,
	}
	payload, err := EncodeNoticePayload(notice)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.notice(type,target_type,target_id,payload,created_at) VALUES('ONLINE_OFFLINE_PLAN','RESOURCE',$1,$2::jsonb,$3)`, resource.ID, string(payload), now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
