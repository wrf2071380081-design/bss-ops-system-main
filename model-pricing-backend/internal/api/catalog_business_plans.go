package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
)

type catalogPlan struct {
	ID         string   `json:"id"`
	Scope      string   `json:"scope"`
	Title      string   `json:"title"`
	Active     bool     `json:"active"`
	LocalTime  string   `json:"localTime"`
	Recurrence string   `json:"recurrence"`
	ValidFrom  *string  `json:"validFrom"`
	ValidUntil *string  `json:"validUntil"`
	Vendors    []string `json:"vendors"`
	Revision   int64    `json:"revision"`
}

const catalogPlanJSON = `jsonb_build_object('id',p.id,'scope',p.scope,'title',p.title,'active',p.active,'localTime',p.local_time,'recurrence',p.recurrence,'validFrom',p.valid_from,'validUntil',p.valid_until,'nextAt',p.next_at,'revision',p.revision,'vendors',p.vendors,'accountId',p.account_id,'staffId',p.staff_id,'singleton',false,'history',coalesce((SELECT jsonb_agg(jsonb_build_object('vendor',x->>'vendor','at',h.due_at,'run',x->>'runId') ORDER BY h.due_at DESC) FROM model_catalog.plan_occurrences h CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(h.result)='array' THEN h.result ELSE '[]'::jsonb END) x WHERE h.plan_id=p.id),'[]'::jsonb),'recent',coalesce((SELECT jsonb_agg(jsonb_build_object('requestId',h.request_id,'dueAt',h.due_at,'status',h.state,'result',h.result) ORDER BY h.due_at DESC) FROM (SELECT * FROM model_catalog.plan_occurrences WHERE plan_id=p.id ORDER BY due_at DESC LIMIT 5) h),'[]'::jsonb),'action','official-crawl')`

func validCatalogPlan(p catalogPlan) bool {
	if p.Scope != "platform" && p.Scope != "personal" {
		return false
	}
	if len(p.Title) > 200 || len(p.Vendors) == 0 || len(p.Vendors) > 6 {
		return false
	}
	if _, err := time.Parse("15:04", p.LocalTime); err != nil {
		return false
	}
	seen := map[string]bool{}
	for _, v := range p.Vendors {
		if seen[v] || !hasCatalogPerm([]string{"deepseek", "openai", "google", "aliyun", "zhipu", "anthropic"}, v) {
			return false
		}
		seen[v] = true
	}
	return len(p.Recurrence) > 0 && len(p.Recurrence) <= 64
}
func (s *CatalogBusinessStore) plans(ctx context.Context, q catalogCommand, a catalogActor) (any, error) {
	switch q.Operation {
	case "plans-bulk":
		if a.staff == 0 {
			return nil, errCatalogForbidden
		}
		var input struct {
			Items []struct {
				ID       string `json:"id"`
				Revision int64  `json:"revision"`
			} `json:"items"`
			Active bool   `json:"active"`
			Action string `json:"action"`
		}
		if json.Unmarshal(q.Plan, &input) != nil || len(input.Items) == 0 || len(input.Items) > 200 || (input.Action != "" && input.Action != "delete") {
			return nil, errCatalogForbidden
		}
		sort.Slice(input.Items, func(i, j int) bool { return input.Items[i].ID < input.Items[j].ID })
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		for i, item := range input.Items {
			if i > 0 && input.Items[i-1].ID == item.ID {
				return nil, errCatalogStale
			}
			var scope string
			var owner, revision int64
			if err = tx.QueryRowContext(ctx, "SELECT scope,staff_id,revision FROM model_catalog.plans WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", item.ID).Scan(&scope, &owner, &revision); err != nil {
				return nil, err
			}
			if !catalogVisible(scope, owner, a.staff) || (scope == "platform" && !a.manage) {
				return nil, errCatalogForbidden
			}
			if revision != item.Revision {
				return nil, errCatalogStale
			}
			_, err = tx.ExecContext(ctx, `UPDATE model_catalog.plans SET active=$2,revision=revision+1,updated_by=$3,updated_at=now(),deleted_at=CASE WHEN $4 THEN now() ELSE NULL END,next_at=CASE WHEN $4 THEN NULL WHEN $2 THEN model_catalog.schedule_occurrence(recurrence,local_time::time,now(),valid_from,valid_until) ELSE next_at END WHERE id=$1`, item.ID, input.Active && input.Action != "delete", a.staff, input.Action == "delete")
			if err != nil {
				return nil, err
			}
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO model_catalog.events(kind,staff_id,detail) VALUES('plans-bulk',$1,$2)", a.staff, string(q.Plan))
		if err != nil {
			return nil, err
		}
		return gin.H{"rowCount": len(input.Items)}, tx.Commit()
	case "plans-list":
		var raw json.RawMessage
		err := s.db.QueryRowContext(ctx, "SELECT coalesce(jsonb_agg(v ORDER BY v->>'localTime',v->>'id'),'[]') FROM (SELECT "+catalogPlanJSON+` AS v FROM model_catalog.plans p WHERE p.deleted_at IS NULL AND ($1::bigint=0 OR p.scope='platform' OR p.staff_id=$1)) x`, a.staff).Scan(&raw)
		return raw, err
	case "plan-save", "plan-pause":
		if a.staff == 0 {
			return nil, errCatalogForbidden
		}
		var p catalogPlan
		if json.Unmarshal(q.Plan, &p) != nil {
			return nil, errCatalogForbidden
		}
		if q.Operation == "plan-save" && !validCatalogPlan(p) {
			return nil, errCatalogForbidden
		}
		tx, e := s.db.BeginTx(ctx, nil)
		if e != nil {
			return nil, e
		}
		defer tx.Rollback()
		if p.ID != "" {
			var owner, revision int64
			var scope string
			e = tx.QueryRowContext(ctx, "SELECT staff_id,scope,revision FROM model_catalog.plans WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", p.ID).Scan(&owner, &scope, &revision)
			if errors.Is(e, sql.ErrNoRows) {
				return nil, errCatalogStale
			}
			if e != nil {
				return nil, e
			}
			if !catalogVisible(scope, owner, a.staff) || (scope == "platform" && !a.manage) {
				return nil, errCatalogForbidden
			}
			if revision != p.Revision {
				return nil, errCatalogStale
			}
			p.Scope = scope
		} else if p.Scope == "platform" && !a.manage {
			return nil, errCatalogForbidden
		}
		if q.Operation == "plan-pause" {
			_, e = tx.ExecContext(ctx, "UPDATE model_catalog.plans SET active=$2,next_at=CASE WHEN $2 THEN model_catalog.schedule_occurrence(recurrence,local_time::time,now(),valid_from,valid_until) ELSE next_at END,revision=revision+1,updated_by=$3,updated_at=now() WHERE id=$1", p.ID, p.Active, a.staff)
		} else {
			vendors, _ := json.Marshal(p.Vendors)
			if p.ID == "" {
				e = tx.QueryRowContext(ctx, `INSERT INTO model_catalog.plans(scope,account_id,staff_id,title,vendors,active,local_time,recurrence,valid_from,valid_until,created_by,updated_by,next_at,legacy_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$3,$3,model_catalog.schedule_occurrence($8,$7::text::time,now(),$9::date,$10::date),nullif($11,'')) ON CONFLICT(legacy_id) DO UPDATE SET legacy_id=excluded.legacy_id RETURNING id`, p.Scope, a.account, a.staff, p.Title, string(vendors), p.Active, p.LocalTime, p.Recurrence, p.ValidFrom, p.ValidUntil, q.Key).Scan(&p.ID)
			} else {
				_, e = tx.ExecContext(ctx, `UPDATE model_catalog.plans SET title=$2,vendors=$3,active=$4,local_time=$5,recurrence=$6,valid_from=$7,valid_until=$8,next_at=model_catalog.schedule_occurrence($6,$5::text::time,now(),$7::date,$8::date),revision=revision+1,updated_by=$9,updated_at=now() WHERE id=$1`, p.ID, p.Title, string(vendors), p.Active, p.LocalTime, p.Recurrence, p.ValidFrom, p.ValidUntil, a.staff)
			}
		}
		if e != nil {
			return nil, e
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO model_catalog.events(kind,staff_id,detail) VALUES($1,$2,jsonb_build_object('planId',$3::text))", q.Operation, a.staff, p.ID)
		if e != nil {
			return nil, e
		}
		return gin.H{"id": p.ID, "rowCount": 1}, tx.Commit()
	case "plans-claim":
		if a.staff != 0 || (q.Key != "platform" && q.Key != "personal") {
			return nil, errCatalogForbidden
		}
		rows, e := s.db.QueryContext(ctx, `WITH due AS (
 SELECT id,next_at FROM model_catalog.plans WHERE active AND deleted_at IS NULL AND scope=$1 AND next_at<=now() ORDER BY next_at LIMIT 10 FOR UPDATE SKIP LOCKED
 ), advanced AS (
 UPDATE model_catalog.plans p SET next_at=model_catalog.schedule_occurrence(p.recurrence,p.local_time::time,now(),p.valid_from,p.valid_until) FROM due WHERE p.id=due.id RETURNING p.id,due.next_at
 ), added AS (
 INSERT INTO model_catalog.plan_occurrences(plan_id,due_at) SELECT id,next_at FROM advanced ON CONFLICT DO NOTHING RETURNING request_id,plan_id,generation
 ), retry AS (
 UPDATE model_catalog.plan_occurrences o SET generation=o.generation+1,lease_until=now()+interval '5 minutes' FROM model_catalog.plans p
 WHERE p.id=o.plan_id AND p.scope=$1 AND p.active AND p.deleted_at IS NULL AND o.state='RUNNING' AND o.lease_until<now() RETURNING o.request_id,o.plan_id,o.generation
 ), ready AS(SELECT * FROM added UNION SELECT * FROM retry) SELECT o.request_id,p.id,p.account_id,p.staff_id,p.scope,p.vendors,o.generation FROM model_catalog.plans p JOIN ready o ON o.plan_id=p.id`, q.Key)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var request, id, scope string
			var account, staff, generation int64
			var vendors json.RawMessage
			if e = rows.Scan(&request, &id, &account, &staff, &scope, &vendors, &generation); e != nil {
				return nil, e
			}
			out = append(out, gin.H{"scheduleId": id, "requestId": request, "accountId": account, "staffId": staff, "scope": scope, "vendors": vendors, "action": "official-crawl", "attempt": generation})
		}
		return out, rows.Err()
	case "plans-settle":
		if a.staff != 0 {
			return nil, errCatalogForbidden
		}
		if !hasCatalogPerm([]string{"SUCCEEDED", "FORBIDDEN", "UNKNOWN", "SKIPPED"}, q.Error) {
			return nil, errCatalogForbidden
		}
		res, e := s.db.ExecContext(ctx, "UPDATE model_catalog.plan_occurrences SET state=$2,result=$3 WHERE request_id=$1 AND state='RUNNING' AND generation=$4 AND lease_until>now()", q.Key, q.Error, string(q.Result), q.Attempt)
		if e != nil {
			return nil, e
		}
		n, _ := res.RowsAffected()
		return gin.H{"rowCount": n}, nil
	}
	return nil, fmt.Errorf("%w: plan operation", errCatalogForbidden)
}
