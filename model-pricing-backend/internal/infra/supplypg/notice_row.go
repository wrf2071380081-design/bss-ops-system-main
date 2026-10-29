package supplypg

import (
	"bytes"
	"encoding/json"
	"io"
	"model_bss/internal/domain/supply"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type NoticeRow struct {
	ID, Kind, TargetType, ResourceID string
	Payload                          json.RawMessage
	PublishedAt                      time.Time
}

type noticePayload struct {
	Contract              string   `json:"contract"`
	Version               int      `json:"version"`
	ResourceName          string   `json:"resource_name"`
	Region                string   `json:"region"`
	Title                 string   `json:"title"`
	Content               string   `json:"content"`
	Reason                string   `json:"reason"`
	SourceEventID         string   `json:"source_event_id,omitempty"`
	Name                  string   `json:"name,omitempty"`
	AffectedStatus        string   `json:"affected_status"`
	AffectedCustomerCount *string  `json:"affected_customer_count,omitempty"`
	PlanName              string   `json:"plan_name,omitempty"`
	SupplierName          string   `json:"supplier_name,omitempty"`
	ScheduledAt           string   `json:"scheduled_at,omitempty"`
	ExecutorName          string   `json:"executor_name,omitempty"`
	AffectedCustomers     []string `json:"affected_customers,omitempty"`
}

func noticeID(id string) bool {
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == id
}
func validNoticeData(n *supply.Notice) bool {
	return n != nil && noticeID(n.ResourceID) && (n.Region == "CN" || n.Region == "OVERSEAS") && strings.TrimSpace(n.Kind) != "" && utf8.RuneCountInString(n.Kind) <= 32 && strings.TrimSpace(n.Title) != "" && (n.SourceEventID == "" || noticeID(n.SourceEventID)) && ((n.AffectedStatus == "PENDING" && n.AffectedCustomerCount == nil) || (n.AffectedStatus == "READY" && n.AffectedCustomerCount != nil))
}
func EncodeNoticePayload(n *supply.Notice) (json.RawMessage, error) {
	if !validNoticeData(n) {
		return nil, supply.ErrReadContract
	}
	p := noticePayload{Contract: "supply.notice", Version: 1, Name: n.Name, ResourceName: n.ResourceName, Region: n.Region, Title: n.Title, Content: n.Content, Reason: n.Reason, SourceEventID: n.SourceEventID, AffectedStatus: n.AffectedStatus, PlanName: n.PlanName, SupplierName: n.SupplierName, ExecutorName: n.ExecutorName}
	if n.AffectedCustomerCount != nil {
		count := strconv.FormatUint(*n.AffectedCustomerCount, 10)
		p.AffectedCustomerCount = &count
	}
	if n.ScheduledAt != nil {
		p.ScheduledAt = n.ScheduledAt.UTC().Format(time.RFC3339Nano)
	}
	if len(n.AffectedCustomers) > 0 {
		p.AffectedCustomers = append([]string{}, n.AffectedCustomers...)
	}
	return json.Marshal(p)
}
func (row NoticeRow) DomainNotice() (*supply.Notice, error) {
	if !noticeID(row.ID) || row.TargetType != "RESOURCE" || row.PublishedAt.IsZero() {
		return nil, supply.ErrReadContract
	}
	var p noticePayload
	d := json.NewDecoder(bytes.NewReader(row.Payload))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || p.Contract != "supply.notice" || p.Version != 1 {
		return nil, supply.ErrReadContract
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, supply.ErrReadContract
	}
	n := &supply.Notice{ID: row.ID, ResourceID: row.ResourceID, Kind: row.Kind, PublishedAt: row.PublishedAt.UTC(), Name: p.Name, ResourceName: p.ResourceName, Region: p.Region, Title: p.Title, Content: p.Content, Reason: p.Reason, SourceEventID: p.SourceEventID, AffectedStatus: p.AffectedStatus, PlanName: p.PlanName, SupplierName: p.SupplierName, ExecutorName: p.ExecutorName}
	if p.ScheduledAt != "" {
		at, err := time.Parse(time.RFC3339Nano, p.ScheduledAt)
		if err != nil {
			return nil, supply.ErrReadContract
		}
		n.ScheduledAt = &at
	}
	if len(p.AffectedCustomers) > 0 {
		n.AffectedCustomers = append([]string{}, p.AffectedCustomers...)
	}
	if p.AffectedCustomerCount != nil {
		count, err := strconv.ParseUint(*p.AffectedCustomerCount, 10, 64)
		if err != nil || strconv.FormatUint(count, 10) != *p.AffectedCustomerCount {
			return nil, supply.ErrReadContract
		}
		n.AffectedCustomerCount = &count
	}
	if !validNoticeData(n) {
		return nil, supply.ErrReadContract
	}
	return n, nil
}
