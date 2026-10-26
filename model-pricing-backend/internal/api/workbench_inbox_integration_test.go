package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"model_bss/internal/api/middleware"
	"model_bss/internal/domain/workbench"
	"model_bss/internal/repo"
)

func TestWorkbenchInboxHTTPProjectsQuoteApprovalsOnlyToEligibleStaffPostgres36(t *testing.T) {
	name := os.Getenv("SUPPLY_SCHEMA_TEST_DB")
	if name == "" {
		t.Skip("explicit dedicated PostgreSQL test required")
	}
	if !strings.HasPrefix(name, "model_bss_a_schema34_") || os.Getenv("PGDATABASE") != name {
		t.Fatal("dedicated A v36 database required")
	}
	options := newCompositionOptions(t)
	raw := options.DB
	ctx := t.Context()
	var actual string
	if err := raw.QueryRowContext(ctx, `SELECT current_database()`).Scan(&actual); err != nil || actual != name {
		t.Fatalf("wrong test database %q: %v", actual, err)
	}

	label := fmt.Sprintf("quote-inbox-%d", time.Now().UnixNano())
	var orgID, buyerID, approverID, observerID, subjectID, supplierID, quoteID, taskID string
	cleanup := func(query string, args ...any) {
		if _, err := raw.ExecContext(ctx, query, args...); err != nil {
			t.Errorf("cleanup failed: %v", err)
		}
	}
	defer func() {
		if taskID != "" {
			cleanup(`DELETE FROM todo_task WHERE id=$1`, taskID)
		}
		if quoteID != "" {
			cleanup(`DELETE FROM quote_sheet WHERE id=$1`, quoteID)
		}
		if supplierID != "" {
			cleanup(`DELETE FROM supplier_profile WHERE id=$1`, supplierID)
		}
		if subjectID != "" {
			cleanup(`DELETE FROM legal_subject WHERE id=$1`, subjectID)
		}
		for _, id := range []string{buyerID, approverID, observerID} {
			if id != "" {
				cleanup(`DELETE FROM internal_staff WHERE id=$1`, id)
			}
		}
		if orgID != "" {
			cleanup(`DELETE FROM org_unit WHERE id=$1`, orgID)
		}
	}()
	if err := raw.QueryRowContext(ctx, `INSERT INTO org_unit(name,path) VALUES($1,$2) RETURNING id::text`, label, "/"+label+"/").Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	newStaff := func(name, mobile string, canApprove bool) string {
		var id string
		if err := raw.QueryRowContext(ctx, `INSERT INTO internal_staff(org_unit_id,name,mobile,can_approve_supplier) VALUES($1,$2,$3,$4) RETURNING id::text`, orgID, name, mobile, canApprove).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	nonce := fmt.Sprint(time.Now().UnixNano())
	buyerID = newStaff(label+" buyer", "b"+nonce, false)
	approverID = newStaff(label+" approver", "a"+nonce, true)
	observerID = newStaff(label+" observer", "o"+nonce, false)
	if err := raw.QueryRowContext(ctx, `INSERT INTO legal_subject(subject_type,legal_name) VALUES('COMPANY',$1) RETURNING id::text`, label+" supplier").Scan(&subjectID); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRowContext(ctx, `INSERT INTO supplier_profile(subject_id,channel_type,owner_procurement_operator_id) VALUES($1,'DIRECT',$2) RETURNING id::text`, subjectID, buyerID).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := raw.QueryRowContext(ctx, `INSERT INTO quote_sheet(supplier_id,version_no,status,valid_from,valid_to,submitted_at,created_by) VALUES($1,1,'APPROVING',$2,$3,$2,$4) RETURNING id::text`, supplierID, now, now.Add(24*time.Hour), buyerID).Scan(&quoteID); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRowContext(ctx, `INSERT INTO todo_task(biz_type,biz_id,assignee_id,title,status,created_by) VALUES('QUOTE',$1,$2,'报价待审批','OPEN',$2) RETURNING id::text`, quoteID, buyerID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	orm, err := gorm.Open(postgres.New(postgres.Config{Conn: raw}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewWorkbenchInboxHandler(workbench.NewInboxService(repo.NewWorkbenchInboxRepo(orm)))
	gin.SetMode(gin.TestMode)
	requestInbox := func(staffID, bizType string) *httptest.ResponseRecorder {
		n, err := strconv.ParseInt(staffID, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		router := gin.New()
		router.GET("/api/internal/workbench/inbox", func(c *gin.Context) {
			c.Set("auth.operator", &middleware.Operator{PortalType: "INTERNAL", OperatorType: "STAFF", StaffID: n, OperatorID: n})
			handler.Inbox(c)
		})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/internal/workbench/inbox?state=TODO&biz_type="+bizType+"&page=1&size=10", nil))
		return recorder
	}
	quoteIDNumber, err := strconv.ParseInt(quoteID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	type response struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				BizType  string `json:"biz_type"`
				BizID    int64  `json:"biz_id"`
				Title    string `json:"title"`
				Deeplink string `json:"deeplink"`
			} `json:"list"`
			Total int `json:"total"`
		} `json:"data"`
	}
	read := func(staffID string) response {
		var got response
		recorder := requestInbox(staffID, "QUOTE_APPROVAL")
		if recorder.Code != 200 {
			t.Fatalf("inbox HTTP status=%d body=%s", recorder.Code, recorder.Body)
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil || got.Code != 0 {
			t.Fatalf("invalid inbox response %s: %v", recorder.Body, err)
		}
		return got
	}
	eligible := read(approverID)
	if eligible.Data.Total != 1 || len(eligible.Data.List) != 1 {
		t.Fatalf("eligible approver inbox = %+v, want one pending approval", eligible.Data)
	}
	item := eligible.Data.List[0]
	if item.BizType != "QUOTE_APPROVAL" || item.BizID != quoteIDNumber || item.Title != "供应商报价审批" || item.Deeplink != "/internal/supplier-quotes?quote_id="+quoteID {
		t.Fatalf("approval work item has wrong contract: %+v", item)
	}
	for _, staffID := range []string{buyerID, observerID} {
		got := read(staffID)
		if got.Data.Total != 0 || len(got.Data.List) != 0 {
			t.Fatalf("non-approver %s received approval work: %+v", staffID, got.Data)
		}
	}
	legacy := requestInbox(buyerID, "TODO_TASK")
	if legacy.Code != 200 {
		t.Fatalf("legacy TODO_TASK query status=%d body=%s", legacy.Code, legacy.Body)
	}
	var legacyEnvelope response
	if err := json.Unmarshal(legacy.Body.Bytes(), &legacyEnvelope); err != nil || legacyEnvelope.Code != 0 || legacyEnvelope.Data.Total != 0 {
		t.Fatalf("pending quote remained assigned to procurement as a generic todo: %s err=%v", legacy.Body, err)
	}
}
