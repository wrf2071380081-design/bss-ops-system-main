package api

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/auth"
	"model_bss/internal/domain/customer"
	"model_bss/internal/domain/supply"
	"model_bss/pkg/perm"
	"model_bss/pkg/response"
)

// This projection answers who supplies a model. Test reports and pricing are
// separate facts, and their absence should not obscure a verified directory entry.
const modelSupplierDirectorySQL = `SELECT r.id::text, s.name, m.name, r.region,
       CASE WHEN snap->>'id' = r.id::text AND snap->>'supplier_id' = s.id::text
            AND snap->>'standard_model_id' = m.id::text AND snap->>'region' = r.region
            AND snap->>'state' = r.state AND snap->>'ever_admitted' = 'true'
            AND r.ever_admitted AND r.state = 'ADMITTED' AND s.status = 'ACTIVE'
            THEN 'ADMITTED' ELSE 'UNVERIFIED' END,
       CASE WHEN $5 AND snap->>'id' = r.id::text AND snap->>'supplier_id' = s.id::text
            AND snap->>'standard_model_id' = m.id::text AND snap->>'region' = r.region
            AND snap->>'state' = 'ADMITTED' AND r.state = 'ADMITTED' AND s.status = 'ACTIVE'
            AND snap->>'ever_admitted' = 'true' AND r.ever_admitted
            AND r.coefficient = CASE WHEN snap->>'coefficient' ~ '^[0-9]{1,12}([.][0-9]{1,12})?$'
                                THEN (snap->>'coefficient')::numeric END
            THEN r.coefficient::text END,
       CASE WHEN $5 AND m.status = 'ACTIVE' AND m.price_items =
            (SELECT v.snapshot->'model'->'data'->'price_items' FROM public.standard_model_version v
             WHERE v.standard_model_id = m.id ORDER BY v.version_no DESC LIMIT 1)
            THEN m.price_items END
FROM public.resource r
JOIN public.standard_model m ON m.id = r.standard_model_id
JOIN public.supplier s ON s.id = r.supplier_id
LEFT JOIN LATERAL (
    SELECT l."after"->'resource' AS snap FROM public.operation_log l
    WHERE l.entity_type = 'resource' AND l.entity_id = r.id AND l.action = 'RESOURCE_REVISION'
    ORDER BY l.id DESC LIMIT 1
) h ON true
WHERE (lower(m.name) = lower($1) OR lower(m.official_id) = lower($1))
  AND ($2 = '' OR r.region = $2)
  AND ($3 OR (r.buyer_id = $4 AND $4 = (SELECT a.new_staff_id
       FROM public.assignment_log a WHERE a.entity_type = 'supplier' AND a.entity_id = s.id
       AND a.field = 'buyer_id' ORDER BY a.id DESC LIMIT 1)))
ORDER BY s.name, r.id LIMIT 101`

func registerModelSupplierDirectory(group *gin.RouterGroup, runtime *SupplyChainRuntime) {
	group.GET("/model-suppliers", middleware.RequirePerm(perm.M1View), middleware.RequirePerm(perm.M3View), func(c *gin.Context) {
		op := middleware.OperatorFrom(c)
		if op == nil || op.StaffID <= 0 || (op.DataScope != auth.ScopeALL && op.DataScope != auth.ScopeSELF) {
			supplyHTTPError(c, supply.ErrForbidden)
			return
		}
		for _, key := range op.FieldMask {
			switch key {
			case "id", "name", "supplier_name", "model_name", "official_id", "region", "state", "supplier_id", "standard_model_id", "ever_admitted":
				supplyHTTPError(c, supply.ErrForbidden)
				return
			}
		}
		q, err := supplyQuery(c, "model", "region", "include")
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		include := q.Get("include")
		if include != "" && include != "procurement" {
			supplyHTTPError(c, supply.ErrIncomplete)
			return
		}
		cost := include == "procurement"
		if cost {
			for _, key := range op.FieldMask {
				switch key {
				case "amount", "price_items", "unit_cost", "supplier_cost", "condition", "dimensions", "unit_quantity", "unit_code", "currency", "item_key", "task_type", "label":
					supplyHTTPError(c, supply.ErrForbidden)
					return
				}
			}
		}
		if cost && (!slices.Contains(op.Perms, perm.M5View) || !slices.Contains(op.Perms, perm.M9View) || slices.Contains(op.FieldMask, "coefficient") || slices.Contains(op.FieldMask, "resource_id")) {
			supplyHTTPError(c, supply.ErrForbidden)
			return
		}
		model, region := strings.TrimSpace(q.Get("model")), q.Get("region")
		if model == "" || len(model) > 512 || (region != "" && region != "CN" && region != "OVERSEAS") {
			supplyHTTPError(c, supply.ErrIncomplete)
			return
		}
		if runtime == nil || runtime.raw == nil {
			supplyHTTPError(c, supply.ErrDependencyUnavailable)
			return
		}
		rows, err := runtime.raw.QueryContext(c.Request.Context(), modelSupplierDirectorySQL, model, region, op.DataScope == auth.ScopeALL, op.StaffID, cost)
		if err != nil {
			supplyHTTPError(c, err)
			return
		}
		defer rows.Close()
		type entry struct {
			ID             string                   `json:"id"`
			SupplierName   string                   `json:"supplier_name"`
			ModelName      string                   `json:"model_name"`
			Region         string                   `json:"region"`
			State          string                   `json:"state"`
			Coefficient    *string                  `json:"coefficient,omitempty"`
			ReferenceCosts []directoryReferenceCost `json:"reference_costs,omitempty"`
		}
		entries := []entry{}
		truncated := false
		for rows.Next() {
			if len(entries) == 100 {
				truncated = true
				break
			}
			var row entry
			var officialPrices []byte
			if err := rows.Scan(&row.ID, &row.SupplierName, &row.ModelName, &row.Region, &row.State, &row.Coefficient, &officialPrices); err != nil {
				supplyHTTPError(c, err)
				return
			}
			row.ReferenceCosts = directoryCosts(officialPrices, row.Coefficient)
			entries = append(entries, row)
		}
		if err := rows.Err(); err != nil {
			supplyHTTPError(c, err)
			return
		}
		response.Success(c, gin.H{"list": entries, "truncated": truncated})
	})
}

type directoryReferenceCost struct {
	customer.ResourcePriceItem
	Condition  string            `json:"condition"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

// Reference costs use the same BSS arithmetic as quoting. They do not establish quote eligibility.
func directoryCosts(raw []byte, coefficient *string) []directoryReferenceCost {
	var items []supply.PriceItem
	if coefficient == nil || json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var result []directoryReferenceCost
	for _, item := range items {
		cost, err := customer.ResourceUnitCost(item.Amount, *coefficient)
		if err != nil || item.ItemKey == "" || item.Currency == "" || item.UnitCode == "" || item.UnitQuantity == "" {
			continue
		}
		result = append(result, directoryReferenceCost{ResourcePriceItem: customer.ResourcePriceItem{
			ItemKey: item.ItemKey, Label: item.Label, TaskType: item.TaskType, Amount: item.Amount, Currency: item.Currency,
			UnitCode: item.UnitCode, UnitQuantity: item.UnitQuantity, UnitCost: cost.StringFixed(8),
		}, Condition: item.Condition, Dimensions: item.Dimensions})
	}
	return result
}
