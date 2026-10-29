package supplypg

import (
	"encoding/json"
	"model_bss/internal/domain/supply"
	"strings"
	"testing"
	"time"
)

func noticeDataFixture() *supply.Notice {
	return &supply.Notice{ID: "51", ResourceID: "9007199254740993", Kind: "RESOURCE_OFFLINED", ResourceName: "资源甲", Region: "CN", Title: "资源下架公告", Content: "请核对客户", Reason: "供应商维护", SourceEventID: "52", AffectedStatus: "PENDING", PublishedAt: time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)}
}
func TestNoticePayloadRoundTripPreservesUnknownImpactAndExactCount(t *testing.T) {
	n := noticeDataFixture()
	raw, err := EncodeNoticePayload(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"contract":"supply.notice"`) || !strings.Contains(string(raw), `"version":1`) || strings.Contains(string(raw), `"affected_customer_count"`) {
		t.Fatalf("unexpected pending payload %s", raw)
	}
	row := NoticeRow{ID: n.ID, Kind: n.Kind, TargetType: "RESOURCE", ResourceID: n.ResourceID, Payload: raw, PublishedAt: n.PublishedAt}
	got, err := row.DomainNotice()
	if err != nil || got.ResourceID != n.ResourceID || got.AffectedCustomerCount != nil || got.SourceEventID != n.SourceEventID {
		t.Fatalf("pending roundtrip: %+v %v", got, err)
	}
	for _, count := range []uint64{0, 9007199254740993} {
		n.AffectedStatus = "READY"
		n.AffectedCustomerCount = &count
		row.Payload, err = EncodeNoticePayload(n)
		if err != nil {
			t.Fatal(err)
		}
		got, err = row.DomainNotice()
		if err != nil || got.AffectedCustomerCount == nil || *got.AffectedCustomerCount != count {
			t.Fatalf("lost exact count %+v %v", got, err)
		}
	}
}
func TestNoticePayloadRoundTripKeepsNoticeContext(t *testing.T) {
	n := noticeDataFixture()
	n.Name = "上下架计划 · 资源甲"
	n.PlanName = "来源全部下架【演示】"
	n.SupplierName = "云桥算力"
	n.ExecutorName = "翟龙龙"
	n.AffectedCustomers = []string{"白鹭游戏工作室（演示）", "橙光互动（演示）"}
	at := time.Date(2026, 10, 23, 7, 13, 0, 0, time.FixedZone("CST", 8*3600))
	n.ScheduledAt = &at
	raw, err := EncodeNoticePayload(n)
	if err != nil {
		t.Fatal(err)
	}
	row := NoticeRow{ID: n.ID, Kind: n.Kind, TargetType: "RESOURCE", ResourceID: n.ResourceID, Payload: raw, PublishedAt: n.PublishedAt}
	got, err := row.DomainNotice()
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != n.Name || got.PlanName != n.PlanName || got.SupplierName != n.SupplierName || got.ExecutorName != n.ExecutorName {
		t.Fatalf("lost notice context %+v", got)
	}
	if got.ScheduledAt == nil || !got.ScheduledAt.Equal(at.UTC()) {
		t.Fatalf("scheduled_at lost: %+v", got.ScheduledAt)
	}
	if len(got.AffectedCustomers) != 2 || got.AffectedCustomers[0] != n.AffectedCustomers[0] {
		t.Fatalf("affected customers lost: %+v", got.AffectedCustomers)
	}
	bad := strings.Replace(string(raw), `"scheduled_at":"`+at.UTC().Format(time.RFC3339Nano)+`"`, `"scheduled_at":"not-a-time"`, 1)
	if bad == string(raw) {
		t.Fatal("scheduled_at was not encoded")
	}
	row.Payload = json.RawMessage(bad)
	if _, err := row.DomainNotice(); err == nil {
		t.Fatal("accepted invalid scheduled_at")
	}
}

func TestNoticePayloadRejectsAmbiguousOrIncompatibleRows(t *testing.T) {
	n := noticeDataFixture()
	raw, err := EncodeNoticePayload(n)
	if err != nil {
		t.Fatal(err)
	}
	base := NoticeRow{ID: n.ID, Kind: n.Kind, TargetType: "RESOURCE", ResourceID: n.ResourceID, Payload: raw, PublishedAt: n.PublishedAt}
	for _, replacement := range []string{`{}`, `null`, strings.Replace(string(raw), `"version":1`, `"version":2`, 1), strings.Replace(string(raw), `"region":"CN"`, `"region":"ALL"`, 1), strings.TrimSuffix(string(raw), "}") + `,"api_key":"must-not-escape"}`, strings.TrimSuffix(string(raw), "}") + `,"affected_customer_count":"0"}`, strings.Replace(string(raw), `"affected_status":"PENDING"`, `"affected_status":"READY"`, 1), string(raw) + ` {}`} {
		row := base
		row.Payload = json.RawMessage(replacement)
		if _, err := row.DomainNotice(); err == nil {
			t.Fatalf("accepted incompatible payload %s", replacement)
		}
	}
	for _, mutate := range []func(*NoticeRow){func(r *NoticeRow) { r.ID = "01" }, func(r *NoticeRow) { r.ResourceID = "0" }, func(r *NoticeRow) { r.TargetType = "CUSTOMER" }, func(r *NoticeRow) { r.PublishedAt = time.Time{} }, func(r *NoticeRow) { r.Kind = "" }} {
		row := base
		mutate(&row)
		if _, err := row.DomainNotice(); err == nil {
			t.Fatal("accepted incompatible identity")
		}
	}
	if _, err := EncodeNoticePayload(nil); err == nil {
		t.Fatal("nil notice accepted")
	}
}
