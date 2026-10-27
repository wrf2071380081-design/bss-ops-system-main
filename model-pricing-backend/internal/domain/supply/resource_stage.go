package supply

type ResourceStageItem struct {
	ResourceID             string `json:"resource_id"`
	ExpectedRevision       string `json:"expected_revision"`
	FormalConfigID         string `json:"formal_config_id,omitempty"`
	ExpectedConfigRevision string `json:"expected_config_revision,omitempty"`
}
type ResourceStageReceipt struct {
	ResourceID string `json:"resource_id"`
	Stage      string `json:"stage"`
	Revision   string `json:"revision"`
}
