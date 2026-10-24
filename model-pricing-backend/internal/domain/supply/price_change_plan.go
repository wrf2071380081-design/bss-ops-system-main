package supply

import (
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

func validSupplierModelID(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && utf8.ValidString(value) && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= 128
}

// NewPriceChangePlan snapshots the resource and planned coefficient. Creating
// the plan never changes the live resource; the assigned executor applies the
// new coefficient only when recording completion.
func NewPriceChangePlan(id, actor, assignee, supplierModelID string, resource *Resource, amount string, due time.Time, reason string, now time.Time) (*StatePlan, error) {
	if !validID(id) || !validID(actor) || !validID(assignee) || resource == nil || !validID(resource.ID) || !validID(resource.BuyerID) || resource.Revision == 0 || resource.Revision > math.MaxInt64 || (resource.Region != "CN" && resource.Region != "OVERSEAS") {
		return nil, invalid("price_change", "valid plan, resource, creator and assignee required")
	}
	if !resource.EverAdmitted || (resource.State != "ADMITTED" && resource.State != "OFFLINE") || !validSupplierModelID(resource.SupplierModelID) || !validSupplierModelID(supplierModelID) || due.IsZero() || now.IsZero() || strings.TrimSpace(reason) == "" {
		return nil, invalid("price_change", "an admitted resource, due date and reason are required")
	}
	before, err := resourceCoefficient(resource.Coefficient)
	if err != nil {
		return nil, invalid("before_coefficient", "resource has an invalid stored coefficient")
	}
	after, err := resourceCoefficient(amount)
	if err != nil || (before.Equal(after) && supplierModelID == resource.SupplierModelID) {
		return nil, invalid("price_change", "supplier model ID or coefficient must change")
	}
	forecast := *resource
	forecast.SupplierModelID = supplierModelID
	forecast.Coefficient = after.StringFixed(4)
	if supplierModelID != resource.SupplierModelID {
		if resource.TargetVersion == 0 || resource.TargetVersion >= math.MaxInt64 {
			return nil, invalid("target_version", "resource target version cannot be advanced")
		}
		forecast.TargetVersion++
	}
	return &StatePlan{
		ResourcePlan: ResourcePlan{
			ID: id, ResourceID: resource.ID, Region: resource.Region, Kind: "PRICE_CHANGE", Status: "PENDING",
			CreatedBy: actor, AssigneeID: assignee, SupplierModelID: supplierModelID, BeforeSupplierModelID: resource.SupplierModelID, AfterSupplierModelID: supplierModelID, Revision: 1, ExpectedResourceRevision: resource.Revision,
			BeforeCoefficient: before.StringFixed(4), AfterCoefficient: after.StringFixed(4), DueAt: due.UTC(), CreatedAt: now.UTC(), Reason: strings.TrimSpace(reason),
		},
		BeforeSnapshot: *resource, AfterSnapshot: forecast,
	}, nil
}
