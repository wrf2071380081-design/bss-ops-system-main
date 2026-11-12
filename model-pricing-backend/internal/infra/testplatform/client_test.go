package testplatform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := New(Config{BaseURL: srv.URL + "/open-api", Token: "cons-test", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client, srv
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	if _, err := New(Config{Token: "cons-x"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("empty base url should be ErrNotConfigured, got %v", err)
	}
	if _, err := New(Config{BaseURL: "http://127.0.0.1:1"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("empty token should be ErrNotConfigured, got %v", err)
	}
	if _, err := New(Config{BaseURL: "ftp://x", Token: "t"}); err == nil {
		t.Fatal("non-http scheme should fail")
	}
}

func TestCreateJobSendsAuthIdempotencyAndPrefix(t *testing.T) {
	var gotPath, gotAuth, gotIdem, gotBody string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotIdem = r.Header.Get("Idempotency-Key")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"job_id":"j_abc","status":"queued","position":0,"estimated_seconds":30}}`))
	})

	req := CreateJobRequest{
		Category: CategoryBaseline,
		Target:   Target{Model: "demo/deepseek-v4", BaseURL: "demo", Protocol: "openai", APIKey: "k"},
		Owner:    "云桥算力",
		Labels:   map[string]string{"bss_batch": "42"},
	}
	out, err := client.CreateJob(context.Background(), req, "bss-42-row-1-v1")
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if out.JobID != "j_abc" || out.Status != "queued" {
		t.Fatalf("unexpected result %+v", out)
	}
	if gotPath != "/open-api/api/v1/test-jobs" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer cons-test" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotIdem != "bss-42-row-1-v1" {
		t.Fatalf("idempotency = %q", gotIdem)
	}
	var echoed CreateJobRequest
	if err := json.Unmarshal([]byte(gotBody), &echoed); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if echoed.Category != CategoryBaseline || echoed.Target.Model != "demo/deepseek-v4" || echoed.Labels["bss_batch"] != "42" {
		t.Fatalf("body = %s", gotBody)
	}
}

func TestCreateJobMapsBusinessError(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":40902,"message":"idempotency key reused with different body","data":null}`))
	})
	_, err := client.CreateJob(context.Background(), CreateJobRequest{Category: CategoryBaseline, Owner: "x"}, "dup")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want APIError, got %v", err)
	}
	if apiErr.Code != 40902 || apiErr.Retryable() {
		t.Fatalf("unexpected error %+v retryable=%v", apiErr, apiErr.Retryable())
	}
}

func TestGetJobParsesSummaryAndTerminal(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-api/api/v1/test-jobs/j_1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{
			"job_id":"j_1","status":"succeeded","verdict":"pass","category":"baseline",
			"summary":{"availability":1,"ttft_ms":88.5},
			"report_dir":"/data/reports/j_1","finished_at":"2026-10-23T11:00:00+08:00"}}`))
	})
	job, err := client.GetJob(context.Background(), "j_1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if !IsTerminal(job.Status) || job.Verdict != "pass" {
		t.Fatalf("unexpected job %+v", job)
	}
	var summary map[string]any
	if err := json.Unmarshal(job.Summary, &summary); err != nil {
		t.Fatalf("summary not json: %v", err)
	}
	if summary["ttft_ms"].(float64) != 88.5 {
		t.Fatalf("summary = %v", summary)
	}
	if job.FinishedAt == nil || job.FinishedAt.Year() != 2026 {
		t.Fatalf("finished_at = %v", job.FinishedAt)
	}
}

func TestGetJobRejectsUnsafeID(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach server")
	})
	for _, id := range []string{"", "../etc/passwd", "j 1", "j/1"} {
		if _, err := client.GetJob(context.Background(), id); err == nil {
			t.Fatalf("id %q should be rejected", id)
		}
	}
}

func TestReportReturnsRawBytes(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("format = %q", r.URL.Query().Get("format"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"layers":[]}`))
	})
	raw, status, err := client.Report(context.Background(), "j_1", "json")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if status != http.StatusOK || string(raw) != `{"layers":[]}` {
		t.Fatalf("status=%d raw=%s", status, raw)
	}
}

func TestReportMapsNotFound(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":40404,"message":"报告已归档","data":null}`))
	})
	_, _, err := client.Report(context.Background(), "j_1", "json")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 40404 {
		t.Fatalf("want 40404, got %v", err)
	}
}

func TestIsTerminal(t *testing.T) {
	for _, s := range []string{StatusSucceeded, StatusFailed, StatusAborted, StatusCanceled} {
		if !IsTerminal(s) {
			t.Fatalf("%s should be terminal", s)
		}
	}
	for _, s := range []string{StatusQueued, StatusScheduled, StatusRunning, StatusPaused, ""} {
		if IsTerminal(s) {
			t.Fatalf("%s should not be terminal", s)
		}
	}
}

// TestPingUsesLightweightProjection 守住探测请求的形态。
//
// 探测必须带 view=lite：平台任务列表默认是全量投影（与详情同构），
// 2026-11-12 实测一页 3.96MB / 1.74s，加 view=lite 后 37KB / 0.37s。
// 探针按最重形态去拉，就会撞上 probeTimeout，把「平台慢」误判成「连不上内网」
// ——当天供应商 6 的连通性测试就是这么被判失败的。
func TestPingUsesLightweightProjection(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotAuth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[],"total":0}}`))
	})

	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if gotPath != "/open-api/api/v1/test-jobs" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if !strings.Contains(gotQuery, "view=lite") {
		t.Fatalf("探测必须用轻量投影，实际 query=%q", gotQuery)
	}
	if gotAuth != "Bearer cons-test" {
		t.Fatalf("unexpected auth %q", gotAuth)
	}
}

// TestPingMapsUnauthorized：探测失败要保留平台给的原因。
// 只报「连不上内网」会把 token 失效说成网络问题，排查方向直接跑偏。
func TestPingMapsUnauthorized(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":40101,"message":"token 无效","data":null}`))
	})

	err := client.Ping(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("want 401 APIError, got %v", err)
	}
}

