package supply

import (
	"io"
	"time"
)

// ResourceReportView is the safe read projection consumed by the A model-plaza
// report panel. The provider owns report provenance and target matching.
type ResourceReportView struct {
	ID string `json:"id"`
	// B history is job-based; the persisted evidence report has its own ID.
	TestJobID          string                `json:"test_job_id,omitempty"`
	TestReportID       string                `json:"test_report_id,omitempty"`
	ResourceID         string                `json:"resource_id"`
	Status             string                `json:"status"`
	CreatedAt          time.Time             `json:"created_at"`
	TestedAt           *time.Time            `json:"tested_at"`
	TestItem           string                `json:"test_item"`
	OperatorName       string                `json:"operator_name"`
	Source             string                `json:"source"`
	Scene              string                `json:"scene"`
	FinishedAt         *time.Time            `json:"finished_at"`
	TargetMatches      *bool                 `json:"target_matches"`
	ConnectivityPassed *bool                 `json:"connectivity_passed"`
	PressurePassed     *bool                 `json:"pressure_passed"`
	Metrics            ResourceReportMetrics `json:"metrics"`
	CacheStats         ResourceCacheStats    `json:"cache_stats"`
}

type ResourceReportPage struct {
	List  []ResourceReportView `json:"list"`
	Total int64                `json:"total"`
	Page  int                  `json:"page"`
	Size  int                  `json:"size"`
}

type ResourceReportFile struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	CanDownload       bool   `json:"can_download"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

type ResourceReportDetail struct {
	ResourceReportView
	Files []ResourceReportFile `json:"files"`
}

// ResourceReportFileContent is returned by the trusted attachment provider;
// storage paths and object-store URLs never enter the HTTP response.
type ResourceReportFileContent struct {
	Name        string
	ContentType string
	Size        int64
	Body        io.ReadCloser
}
