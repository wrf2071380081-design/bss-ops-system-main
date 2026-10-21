package supplypg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"model_bss/internal/domain/supply"
	"strconv"
)

// ReadModelData reads only the existing official-data projection inside the
// caller's transaction. It is not a full StandardModel repository.
func ReadModelData(ctx context.Context, tx *sql.Tx, id string) (supply.ModelData, error) {
	var empty supply.ModelData
	parsed, err := strconv.ParseInt(id, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != id {
		return empty, mappingError("id")
	}
	if tx == nil {
		return empty, supply.ErrDependencyUnavailable
	}
	var row ModelDataRow
	var inputs, outputs []byte
	// SQL array-to-JSON conversion avoids driver-specific text[] scanning and
	// manual array escaping. Keep NULL elements visible for explicit rejection.
	err = tx.QueryRowContext(ctx, `SELECT company, name, official_id, model_type,
		to_json(input_modalities), to_json(output_modalities),
		capacity, abilities, price_items, api_access, lifecycle, sources, checked_at
		FROM public.standard_model WHERE id=$1`, id).Scan(
		&row.Company, &row.Name, &row.OfficialID, &row.ModelType,
		&inputs, &outputs, &row.Capacity, &row.Abilities, &row.PriceItems,
		&row.APIAccess, &row.Lifecycle, &row.Sources, &row.CheckedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, supply.ErrNotFound
	}
	if err != nil {
		return empty, fmt.Errorf("read standard model official data: %w", err)
	}
	if row.InputModalities, err = modelModalities(inputs, "input_modalities"); err != nil {
		return empty, err
	}
	if row.OutputModalities, err = modelModalities(outputs, "output_modalities"); err != nil {
		return empty, err
	}
	data, err := row.DomainData()
	if err != nil {
		return empty, err
	}
	return data, nil
}

func modelModalities(raw []byte, field string) ([]string, error) {
	var values []*string
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return nil, mappingError(field)
	}
	result := make([]string, len(values))
	for i, value := range values {
		if value == nil {
			return nil, mappingError(field)
		}
		result[i] = *value
	}
	return result, nil
}
