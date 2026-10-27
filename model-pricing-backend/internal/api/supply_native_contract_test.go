package api

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"model_bss/internal/domain/supply"
	"model_bss/internal/infra/config"
	infradb "model_bss/internal/infra/db"
	"os"
	"strconv"
	"strings"
	"testing"
)

type nativeContractReadinessPort struct {
	ready    bool
	revision uint64
}

func (p nativeContractReadinessPort) Get(_ context.Context, q supply.F12ReadinessRequest) (*supply.F12ReadinessSnapshot, error) {
	return &supply.F12ReadinessSnapshot{SubjectType: q.SubjectType, SubjectID: q.SubjectID, FlowID: q.FlowID, ModuleKey: q.ModuleKey, FlowRevision: p.revision, Ready: p.ready, Contracts: []supply.F12ContractVersionRef{{RecordID: "test-contract-reference", ContractKey: "test-port", Version: 1}}, Credits: []supply.F12CreditVersionRef{{RecordID: "test-credit-reference", Version: 1, Currency: "CNY", Amount: "100.00"}}}, nil
}

// Only verifies the transaction consumer with a declared readiness port double.
// Does not claim actual F12 documents/schema or business completion is available.
func TestNativeContractCompletionPortDaily42(t *testing.T) {
	if os.Getenv("BSS_LOCAL_INTEGRATION_DB") != "model_bss" || os.Getenv("BSS_NATIVE_MANIFEST") == "" {
		t.Skip("explicit current QA fixture required")
	}
	raw, err := os.ReadFile(os.Getenv("BSS_NATIVE_MANIFEST"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Run, Supplier string }
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(f.Run, "QA-NATIVE-") {
		t.Fatal("QA fixture required")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	client, err := infradb.New(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	db, err := client.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	var node, decision uint64
	var actor string
	err = db.QueryRow(`SELECT n.id,l.id,n.assignee_id::text FROM onboarding_node n JOIN onboarding_flow f ON f.id=n.flow_id JOIN operation_log l ON l.entity_type='submission' AND l.action='APPROVE' AND l."after"->>'contract'='supply.handling_approval' AND l."after"->>'flow_id'=f.id::text WHERE f.owner_id=$1 AND f.kind='SUPPLIER' AND n.node_key='CONTRACT' AND n.status='IN_PROGRESS' ORDER BY l.id DESC LIMIT 1`, f.Supplier).Scan(&node, &decision, &actor)
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("verify successful candidate then rollback")
	for _, ready := range []bool{false, true} {
		runtime := NewSupplyChainRuntime(db, nil, nativeContractReadinessPort{ready: ready, revision: decision})
		err = client.DB.Transaction(func(tx *gorm.DB) error {
			ctx := infradb.WithDB(context.Background(), tx)
			err := runtime.completeNativeContract(ctx, supply.WriteMeta{ActorID: actor, Reason: f.Run, RequestID: f.Run + "-port-" + strconv.FormatBool(ready)}, node, decision)
			if !ready {
				if !errors.Is(err, supply.ErrRevisionConflict) {
					t.Fatalf("missing readiness got %v", err)
				}
				return rollback
			}
			if err != nil {
				return err
			}
			var status string
			var logged int
			if err := tx.Raw(`SELECT status FROM onboarding_node WHERE id=?`, node).Scan(&status).Error; err != nil || status != "COMPLETED" {
				t.Fatalf("node candidate=%s %v", status, err)
			}
			if err := tx.Raw(`SELECT count(*) FROM operation_log WHERE entity_type='onboarding_node' AND entity_id=? AND action='COMPLETE_CONTRACT' AND operator_id=?`, node, actor).Scan(&logged).Error; err != nil || logged != 1 {
				t.Fatalf("audit candidate=%d %v", logged, err)
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal(err)
		}
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM onboarding_node WHERE id=$1`, node).Scan(&status); err != nil || status != "IN_PROGRESS" {
		t.Fatalf("outer transaction did not roll back %s %v", status, err)
	}
}
