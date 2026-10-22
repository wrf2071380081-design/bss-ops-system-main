package supply

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestExistingWritersRejectMetadataOutsidePublicLogStorage(t *testing.T) {
	checks := map[string]func(WriteMeta) error{
		"model":         func(m WriteMeta) error { return newService(newMemoryDB()).checkWrite(m, "11") },
		"supplier":      func(m WriteMeta) error { return NewSupplierService(newSupplierMemory(), nil).checkWrite(m) },
		"formal config": func(m WriteMeta) error { return formalService(newFormalMemory()).checkWrite(m, "11") },
		"plan maintenance": func(m WriteMeta) error {
			return NewPlanMaintenanceService(maintenanceStore(), nil).validate(m, "11", 1)
		},
		"notice": func(m WriteMeta) error {
			_, err := NewNoticeService(newNoticeMemory(), nil).MarkRead(context.Background(), m, "11")
			return err
		},
		"plan execution": func(m WriteMeta) error {
			_, err := NewPlanExecutionService(planExecutionStore(t), nil).Execute(context.Background(), m, "81", 1, true, "实际完成")
			return err
		},
		"coefficient": func(m WriteMeta) error {
			_, err := NewResourceCoefficientService(coefficientStore(), nil).Change(context.Background(), m, "11", 2, "0.5", coefficientSource())
			return err
		},
		"legacy state": func(m WriteMeta) error {
			_, err := NewResourceStateService(offlineStore(), nil).Change(context.Background(), m, stateCommand())
			return err
		},
	}
	cases := map[string]func(*WriteMeta){
		"trace65":             func(m *WriteMeta) { m.RequestID = strings.Repeat("x", 65) },
		"reason501":           func(m *WriteMeta) { m.Reason = strings.Repeat("中", 501) },
		"nul trace":           func(m *WriteMeta) { m.RequestID = "trace\x00x" },
		"nul reason":          func(m *WriteMeta) { m.Reason = "reason\x00x" },
		"invalid UTF8 trace":  func(m *WriteMeta) { m.RequestID = string([]byte{0xff}) },
		"invalid UTF8 reason": func(m *WriteMeta) { m.Reason = string([]byte{0xff}) },
	}
	for name, check := range checks {
		for label, change := range cases {
			t.Run(name+"/"+label, func(t *testing.T) {
				m := writeMeta("7")
				change(&m)
				err := check(m)
				var field *FieldError
				if !errors.As(err, &field) {
					t.Fatalf("metadata should fail before business work: %v", err)
				}
			})
		}
	}
}

func TestModelLogMetadataAcceptsExactUnicodeLimitsWithoutTruncation(t *testing.T) {
	db := newMemoryDB()
	m := writeMeta("7")
	m.RequestID = strings.Repeat("追", 64)
	m.Reason = strings.Repeat("理", 500)
	_, err := newService(db).Create(context.Background(), m, ModelData{Name: "待补齐模型"})
	if err != nil {
		t.Fatal(err)
	}
	if len(db.state.operations) != 1 || db.state.operations[0].RequestID != m.RequestID || db.state.operations[0].Reason != m.Reason {
		t.Fatal("metadata truncated or not retained")
	}
}
