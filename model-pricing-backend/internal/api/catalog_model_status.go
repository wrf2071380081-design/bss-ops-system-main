package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/pkg/apperr"
	"model_bss/pkg/perm"
	"model_bss/pkg/response"
)

// Model state is distinct from the vendor job state. Missing values with a
// resolved official disclosure can be complete; a pending extraction cannot.
func catalogModelState(runStatus string, model map[string]any) string {
	if runStatus == "RUNNING" {
		return "RUNNING"
	}
	if runStatus != "SUCCEEDED" {
		return "UNAVAILABLE"
	}
	if model == nil || model["status"] == "NOT_OBSERVED" {
		return "NOT_OBSERVED"
	}
	for _, group := range []string{"fieldEvidence", "specEvidence"} {
		if fields, ok := model[group].(map[string]any); ok {
			for _, raw := range fields {
				if field, ok := raw.(map[string]any); ok {
					switch field["state"] {
					case "CONFLICT", "PENDING_FOLLOWUP", "NOT_ATTEMPTED", "DEFERRED":
						return "PARTIAL"
					}
				}
			}
		}
	}
	if model["coverage"] == "COMPLETE" {
		return "COMPLETE"
	}
	if model["coverage"] == "UNAVAILABLE" {
		return "UNAVAILABLE"
	}
	return "PARTIAL"
}

type catalogModelStatus struct {
	State          string     `json:"state"`
	AttemptedAt    *time.Time `json:"attemptedAt,omitempty"`
	LastCompleteAt *time.Time `json:"lastCompleteAt,omitempty"`
}

// ids identify existing BSS records. Caller-supplied vendor or source URLs can
// never redirect this query to another model or another employee's private run.
func registerCatalogModelStatus(group *gin.RouterGroup, store *CatalogBusinessStore) {
	if store == nil {
		return
	}
	group.GET("/agent-catalog/model-status", middleware.RequirePerm(perm.M1View), func(c *gin.Context) {
		actor := middleware.OperatorFrom(c)
		if actor == nil || actor.StaffID <= 0 || actor.PortalType != "INTERNAL" || actor.OperatorType != "STAFF" {
			response.Error(c, apperr.ErrForbidden)
			return
		}
		ids := strings.Split(c.Query("ids"), ",")
		if len(ids) > 100 || len(ids) == 0 {
			c.Status(400)
			return
		}
		for _, id := range ids {
			if len(id) > 19 || id == "" || strings.Trim(id, "0123456789") != "" || id[0] == '0' {
				c.Status(400)
				return
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
		defer cancel()
		result, err := store.modelStatuses(ctx, ids, actor.StaffID)
		if err != nil {
			c.Status(503)
			return
		}
		c.Header("Cache-Control", "no-store")
		response.Success(c, result)
	})
}

func (s *CatalogBusinessStore) modelStatuses(ctx context.Context, ids []string, staff int64) (map[string]catalogModelStatus, error) {
	raw, _ := json.Marshal(ids)
	// The latest run and last complete run share the same visibility predicate.
	// A new partial run must not erase the prior complete timestamp.
	rows, err := s.db.QueryContext(ctx, `
 WITH refs AS (
 SELECT m.id::text AS id,m.official_id,
 CASE lower(m.company) WHEN 'deepseek' THEN 'deepseek' WHEN 'openai' THEN 'openai'
 WHEN 'google' THEN 'google' WHEN 'anthropic' THEN 'anthropic' WHEN '阿里云' THEN 'aliyun'
 WHEN 'aliyun' THEN 'aliyun' WHEN 'alibaba' THEN 'aliyun' WHEN '智谱' THEN 'zhipu' WHEN 'zhipu' THEN 'zhipu' END AS vendor
 FROM public.standard_model m WHERE m.id::text IN (SELECT jsonb_array_elements_text($1::jsonb))
 )
 SELECT refs.id,latest.status,latest.started_at,
 (SELECT value FROM jsonb_array_elements(COALESCE(latest.result->'models','[]'::jsonb)) WHERE value->>'id'=refs.official_id LIMIT 1),
 complete.finished_at
 FROM refs
 LEFT JOIN LATERAL (
 SELECT status,started_at,result FROM model_catalog.runs r WHERE r.vendor=refs.vendor
 AND (r.scope='platform' OR r.owner_staff_id=$2) ORDER BY started_at DESC,id DESC LIMIT 1
 ) latest ON true
 LEFT JOIN LATERAL (
 SELECT finished_at FROM model_catalog.runs r WHERE r.vendor=refs.vendor AND r.status='SUCCEEDED'
 AND (r.scope='platform' OR r.owner_staff_id=$2)
 AND (r.result->'models') @> jsonb_build_array(jsonb_build_object('id',refs.official_id,'coverage','COMPLETE'))
 AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(r.result->'models') v WHERE v->>'id'=refs.official_id AND v->>'status'='NOT_OBSERVED')
 AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(r.result->'models') v,
 jsonb_each(COALESCE(v->'fieldEvidence','{}'::jsonb)||COALESCE(v->'specEvidence','{}'::jsonb)) f
 WHERE v->>'id'=refs.official_id AND f.value->>'state' IN ('CONFLICT','PENDING_FOLLOWUP','NOT_ATTEMPTED','DEFERRED'))
 ORDER BY r.started_at DESC,r.id DESC LIMIT 1
 ) complete ON true`, string(raw), staff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]catalogModelStatus{}
	for rows.Next() {
		var id string
		var state sql.NullString
		var at, complete sql.NullTime
		var payload []byte
		if err = rows.Scan(&id, &state, &at, &payload, &complete); err != nil {
			return nil, err
		}
		value := catalogModelStatus{State: "UNKNOWN"}
		if state.Valid {
			var model map[string]any
			_ = json.Unmarshal(payload, &model)
			value.State = catalogModelState(state.String, model)
		}
		if at.Valid {
			value.AttemptedAt = &at.Time
		}
		if complete.Valid {
			value.LastCompleteAt = &complete.Time
		}
		result[id] = value
	}
	return result, rows.Err()
}
