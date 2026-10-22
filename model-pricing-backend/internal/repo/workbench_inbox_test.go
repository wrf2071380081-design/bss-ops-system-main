package repo

import (
	"strings"
	"testing"
)

func TestWorkbenchInboxSourceUsesNativePlanID(t *testing.T) {
	source := strings.ToLower(strings.Join(strings.Fields(workbenchInboxSource), " "))
	for _, fragment := range []string{
		"from v_work_item where biz_type <> 'plan'",
		"from plan p",
		"p.resource_id as biz_id",
		"p.id as plan_id",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("workbench inbox source is missing %q", fragment)
		}
	}
	if strings.Contains(source, "p.created_at = v_work_item.occurred_at") || strings.Contains(source, "p.created_at=v_work_item.occurred_at") {
		t.Fatal("plan IDs must not be inferred from a timestamp match")
	}
}
