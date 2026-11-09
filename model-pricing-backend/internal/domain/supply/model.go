package supply

import (
	"errors"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/shopspring/decimal"
)

var (
	ErrForbidden             = errors.New("only the bound maintainer may modify this model")
	ErrRevisionConflict      = errors.New("model revision has changed")
	ErrIncomplete            = errors.New("official model information is incomplete")
	ErrInactive              = errors.New("model is not active")
	ErrNotFound              = errors.New("standard model not found")
	ErrIdentityConflict      = errors.New("official model identity already exists")
	ErrDependencyUnavailable = errors.New("required supply dependency is unavailable")
	ErrSourceRejected        = errors.New("official sources could not be verified")
)

type OfficialFact struct {
	Key        string `json:"key"`
	Disclosure string `json:"disclosure"`
	Value      string `json:"value"`
}
type Ability struct {
	Key         string `json:"key"`
	State       string `json:"state"`
	Description string `json:"description"`
}
type Source struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Kind  string `json:"source_kind"`
}

// ModelData is a typed domain shape, independent of pending JSONB column mappings.
type ModelData struct {
	Company          string         `json:"company"`
	Name             string         `json:"name"`
	OfficialID       string         `json:"official_id"`
	ModelType        string         `json:"model_type"`
	InputModalities  []string       `json:"input_modalities"`
	OutputModalities []string       `json:"output_modalities"`
	Capacity         []OfficialFact `json:"capacity"`
	Abilities        []Ability      `json:"abilities"`
	PriceItems       []PriceItem    `json:"price_items"`
	APIAccess        []OfficialFact `json:"api_access"`
	Lifecycle        []OfficialFact `json:"lifecycle"`
	Sources          []Source       `json:"sources"`
	CheckedAt        string         `json:"checked_at"`
}
type StandardModel struct {
	ID           string    `json:"id"`
	MaintainerID string    `json:"maintainer_id"`
	Data         ModelData `json:"data"`
	Status       string    `json:"status"`
	VerifyStatus string    `json:"verify_status"`
	CheckedBy    string    `json:"checked_by"`
	VersionNo    uint64    `json:"version_no,string"`
	Revision     uint64    `json:"revision,string"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
type ModelReadiness struct {
	Ready                  bool     `json:"ready"`
	Reasons                []string `json:"reasons"`
	StandardModelVersionNo uint64   `json:"standard_model_version_no,string"`
}

func validID(value string) bool {
	n, err := strconv.ParseInt(value, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == value
}

func NewStandardModel(id, actorID string, data ModelData, now time.Time) (*StandardModel, error) {
	if !validID(id) {
		return nil, invalid("id", "expected a positive bigint string")
	}
	if !validID(actorID) {
		return nil, invalid("actor_id", "expected an authenticated staff bigint string")
	}
	if err := validateModelData(data, now); err != nil {
		return nil, err
	}
	m := &StandardModel{ID: id, MaintainerID: actorID, Data: canonicalData(data), Status: "ACTIVE", VersionNo: 1, Revision: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	m.VerifyStatus = m.unverifiedStatus()
	return m, nil
}

func cloneData(data ModelData) ModelData {
	data.InputModalities = slices.Clone(data.InputModalities)
	data.OutputModalities = slices.Clone(data.OutputModalities)
	data.Capacity = slices.Clone(data.Capacity)
	data.Abilities = slices.Clone(data.Abilities)
	data.APIAccess = slices.Clone(data.APIAccess)
	data.Lifecycle = slices.Clone(data.Lifecycle)
	data.Sources = slices.Clone(data.Sources)
	data.PriceItems = slices.Clone(data.PriceItems)
	for i := range data.PriceItems {
		data.PriceItems[i].Dimensions = maps.Clone(data.PriceItems[i].Dimensions)
	}
	return data
}
func canonicalData(data ModelData) ModelData {
	data = cloneData(data)
	data.Company = strings.TrimSpace(data.Company)
	data.Name = strings.TrimSpace(data.Name)
	data.OfficialID = strings.TrimSpace(data.OfficialID)
	if len(data.InputModalities) == 0 {
		data.InputModalities = nil
	}
	if len(data.OutputModalities) == 0 {
		data.OutputModalities = nil
	}
	if len(data.Capacity) == 0 {
		data.Capacity = nil
	}
	if len(data.Abilities) == 0 {
		data.Abilities = nil
	}
	if len(data.APIAccess) == 0 {
		data.APIAccess = nil
	}
	if len(data.Lifecycle) == 0 {
		data.Lifecycle = nil
	}
	if len(data.Sources) == 0 {
		data.Sources = nil
	}
	if len(data.PriceItems) == 0 {
		data.PriceItems = nil
	}
	for i := range data.PriceItems {
		amount, _ := decimal.NewFromString(data.PriceItems[i].Amount)
		data.PriceItems[i].Amount = amount.StringFixed(8)
		qty, _ := decimal.NewFromString(data.PriceItems[i].UnitQuantity)
		data.PriceItems[i].UnitQuantity = qty.String()
		if len(data.PriceItems[i].Dimensions) == 0 {
			data.PriceItems[i].Dimensions = nil
		}
	}
	return data
}
func (m *StandardModel) Clone() *StandardModel {
	if m == nil {
		return nil
	}
	copied := *m
	copied.Data = cloneData(m.Data)
	return &copied
}

func (m *StandardModel) unverifiedStatus() string {
	if len(m.MissingFields()) > 0 {
		return "INCOMPLETE"
	}
	return "UNVERIFIED"
}
// authorize 校验编辑权：维护人本人，或上层已判定其具备维护授权（WriteMeta.CanMaintain）。
func (m *StandardModel) authorize(meta WriteMeta, revision uint64) error {
	if meta.ActorID != m.MaintainerID && !meta.CanMaintain {
		return ErrForbidden
	}
	if revision != m.Revision {
		return ErrRevisionConflict
	}
	return nil
}

// authorizeMaintainer 校验核验权：只认绑定维护人本人。
// 核验（生成可信资料）刻意不随编辑权放开，否则「谁改谁核验」，双人复核名存实亡。
func (m *StandardModel) authorizeMaintainer(meta WriteMeta, revision uint64) error {
	if meta.ActorID != m.MaintainerID {
		return ErrForbidden
	}
	if revision != m.Revision {
		return ErrRevisionConflict
	}
	return nil
}

// Updated returns a new snapshot. Caller data, original state and the result never share mutable slices/maps.
func (m *StandardModel) Updated(meta WriteMeta, expectedRevision uint64, data ModelData, now time.Time) (*StandardModel, bool, error) {
	if err := m.authorize(meta, expectedRevision); err != nil {
		return nil, false, err
	}
	if err := validateModelData(data, now); err != nil {
		return nil, false, err
	}
	next := m.Clone()
	next.Data = canonicalData(data)
	if reflect.DeepEqual(m.Data, next.Data) {
		return next, false, nil
	}
	next.Revision++
	next.VersionNo++
	next.UpdatedAt = now.UTC()
	next.CheckedBy = ""
	next.VerifyStatus = next.unverifiedStatus()
	return next, true, nil
}

// ManuallySaved records one maintainer action. Complete official data becomes the
// attested version in this same transition; incomplete data is saved unverified.
//
// 只有维护人本人的「齐备保存」才会直接变成 VERIFIED。持有维护授权的其他人可以保存，
// 但结果停在 UNVERIFIED，等维护人核验——保证「改」与「核验」不是同一个人。
func (m *StandardModel) ManuallySaved(meta WriteMeta, expectedRevision uint64, data ModelData, now time.Time) (*StandardModel, bool, bool, error) {
	if err := m.authorize(meta, expectedRevision); err != nil {
		return nil, false, false, err
	}
	if err := validateModelData(data, now); err != nil {
		return nil, false, false, err
	}
	next := m.Clone()
	next.Data = canonicalData(data)
	complete := len(next.MissingFields()) == 0
	if complete && m.Status != "ACTIVE" {
		return nil, false, false, ErrInactive
	}
	if complete && meta.ActorID == m.MaintainerID {
		next.VerifyStatus = "VERIFIED"
		next.CheckedBy = meta.ActorID
	} else {
		next.VerifyStatus = next.unverifiedStatus()
		next.CheckedBy = ""
	}
	changed := !reflect.DeepEqual(m.Data, next.Data) || m.VerifyStatus != next.VerifyStatus || m.CheckedBy != next.CheckedBy
	if changed {
		next.Revision++
		next.VersionNo++
		next.UpdatedAt = now.UTC()
	}
	return next, changed, complete, nil
}

// attestInitialVersion marks a complete creation as checked by its authenticated
// maintainer without creating a redundant second version.
func (m *StandardModel) attestInitialVersion(actorID string) error {
	if m == nil || actorID != m.MaintainerID {
		return ErrForbidden
	}
	if m.Status != "ACTIVE" {
		return ErrInactive
	}
	if len(m.MissingFields()) > 0 {
		return ErrIncomplete
	}
	m.VerifyStatus = "VERIFIED"
	m.CheckedBy = actorID
	return nil
}

// verified is used only after ModelService has checked the injected official-source policy.
func (m *StandardModel) verified(meta WriteMeta, expectedRevision uint64, date string, now time.Time) (*StandardModel, bool, error) {
	if err := m.authorizeMaintainer(meta, expectedRevision); err != nil {
		return nil, false, err
	}
	if m.Status != "ACTIVE" {
		return nil, false, ErrInactive
	}
	if date == "" {
		return nil, false, invalid("checked_at", "verification date is required")
	}
	next := m.Clone()
	next.Data.CheckedAt = date
	if err := validateModelData(next.Data, now); err != nil {
		return nil, false, err
	}
	if len(next.MissingFields()) > 0 {
		return nil, false, ErrIncomplete
	}
	if m.VerifyStatus == "VERIFIED" && m.Data.CheckedAt == date {
		return next, false, nil
	}
	next.VerifyStatus = "VERIFIED"
	next.CheckedBy = meta.ActorID
	next.Revision++
	next.VersionNo++
	next.UpdatedAt = now.UTC()
	return next, true, nil
}

func (m *StandardModel) Readiness() ModelReadiness {
	result := ModelReadiness{Reasons: []string{}, StandardModelVersionNo: m.VersionNo}
	if m.Status != "ACTIVE" {
		result.Reasons = append(result.Reasons, "MODEL_INACTIVE")
	}
	if len(m.MissingFields()) > 0 {
		result.Reasons = append(result.Reasons, "MODEL_INCOMPLETE")
	}
	if m.VerifyStatus != "VERIFIED" {
		result.Reasons = append(result.Reasons, "MODEL_UNVERIFIED")
	}
	result.Ready = len(result.Reasons) == 0
	return result
}
