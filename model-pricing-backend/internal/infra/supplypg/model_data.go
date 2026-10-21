package supplypg

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"model_bss/internal/domain/supply"
	"slices"
	"time"
	"unicode/utf8"
)

// ModelDataRow maps only existing 000033 columns. It cannot construct an
// authoritative StandardModel: maintainer/revision/verification need new schema.
type ModelDataRow struct {
	Company                                                        string `gorm:"column:company"`
	Name                                                           string `gorm:"column:name"`
	OfficialID                                                     string `gorm:"column:official_id"`
	ModelType                                                      string `gorm:"column:model_type"`
	InputModalities, OutputModalities                              []string
	Capacity, Abilities, PriceItems, APIAccess, Lifecycle, Sources json.RawMessage
	CheckedAt                                                      sql.NullTime
}

func mappingError(field string) error {
	return fmt.Errorf("supply target schema mapping: incompatible %s", field)
}
func decodeModelList[T any](raw json.RawMessage, field string, allowEmptyObject bool) ([]T, error) {
	raw = bytes.TrimSpace(raw)
	if allowEmptyObject && len(raw) > 0 && raw[0] == '{' {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) == nil && len(object) == 0 {
			return []T{}, nil
		}
	}
	if len(raw) == 0 || raw[0] != '[' {
		return nil, mappingError(field)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var list []T
	if err := decoder.Decode(&list); err != nil || list == nil {
		return nil, mappingError(field)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, mappingError(field)
	}
	return list, nil
}
func (row ModelDataRow) DomainData() (supply.ModelData, error) {
	data := supply.ModelData{Company: row.Company, Name: row.Name, OfficialID: row.OfficialID, ModelType: row.ModelType, InputModalities: slices.Clone(row.InputModalities), OutputModalities: slices.Clone(row.OutputModalities)}
	var err error
	if data.Capacity, err = decodeModelList[supply.OfficialFact](row.Capacity, "capacity", true); err != nil {
		return data, err
	}
	if data.Abilities, err = decodeModelList[supply.Ability](row.Abilities, "abilities", true); err != nil {
		return data, err
	}
	if data.PriceItems, err = decodeModelList[supply.PriceItem](row.PriceItems, "price_items", false); err != nil {
		return data, err
	}
	if data.APIAccess, err = decodeModelList[supply.OfficialFact](row.APIAccess, "api_access", true); err != nil {
		return data, err
	}
	if data.Lifecycle, err = decodeModelList[supply.OfficialFact](row.Lifecycle, "lifecycle", true); err != nil {
		return data, err
	}
	if data.Sources, err = decodeModelList[supply.Source](row.Sources, "sources", false); err != nil {
		return data, err
	}
	if row.CheckedAt.Valid {
		data.CheckedAt = row.CheckedAt.Time.Format("2006-01-02")
	}
	return data, nil
}
func encodeModelList[T any](list []T) json.RawMessage {
	if list == nil {
		list = []T{}
	}
	raw, _ := json.Marshal(list)
	return raw
}

// EncodeModelData checks storage compatibility, not business readiness. The
// domain service remains responsible for price/fact/source validation.
func EncodeModelData(data supply.ModelData) (ModelDataRow, error) {
	row := ModelDataRow{}
	for _, item := range []struct {
		field, value string
		limit        int
	}{{"company", data.Company, 64}, {"name", data.Name, 128}, {"official_id", data.OfficialID, 128}, {"model_type", data.ModelType, 16}} {
		if !utf8.ValidString(item.value) || utf8.RuneCountInString(item.value) > item.limit || bytes.IndexByte([]byte(item.value), 0) >= 0 {
			return row, mappingError(item.field)
		}
	}
	row.Company = data.Company
	row.Name = data.Name
	row.OfficialID = data.OfficialID
	row.ModelType = data.ModelType
	row.InputModalities = slices.Clone(data.InputModalities)
	row.OutputModalities = slices.Clone(data.OutputModalities)
	if row.InputModalities == nil {
		row.InputModalities = []string{}
	}
	if row.OutputModalities == nil {
		row.OutputModalities = []string{}
	}
	row.Capacity = encodeModelList(data.Capacity)
	row.Abilities = encodeModelList(data.Abilities)
	row.PriceItems = encodeModelList(data.PriceItems)
	row.APIAccess = encodeModelList(data.APIAccess)
	row.Lifecycle = encodeModelList(data.Lifecycle)
	row.Sources = encodeModelList(data.Sources)
	if data.CheckedAt != "" {
		at, err := time.Parse("2006-01-02", data.CheckedAt)
		if err != nil || at.Format("2006-01-02") != data.CheckedAt {
			return ModelDataRow{}, mappingError("checked_at")
		}
		row.CheckedAt = sql.NullTime{Time: at, Valid: true}
	}
	return row, nil
}
