package supply

import (
	"context"
	"testing"
)

func TestPlanReadPermissionsUseOriginalInitiatorAndStatus(t *testing.T) {
	for _, status := range []string{"PENDING", "BLOCKED", "EXECUTING", "EXECUTED", "CANCELED"} {
		for _, actor := range []string{"7", "8"} {
			t.Run(status+"/"+actor, func(t *testing.T) {
				record := planListRecord()
				record.Plan.Status = status
				if status == "EXECUTED" {
					at := modelNow
					record.Plan.ExecutedAt = &at
				}
				summary, err := NewPlanReader(planReadSingleStore{record}).Get(context.Background(), actor, "81")
				if err != nil {
					t.Fatal(err)
				}
				own := actor == "7"
				if summary.Permissions.CanRevise != (own && status == "PENDING") || summary.Permissions.CanCancel != (own && (status == "PENDING" || status == "BLOCKED")) || summary.Permissions.CanExecute != (own && status == "PENDING") {
					t.Fatalf("bad permissions %+v", summary.Permissions)
				}
			})
		}
	}
}
