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
