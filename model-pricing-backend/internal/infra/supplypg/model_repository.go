package supplypg

import "model_bss/internal/domain/supply"

type ModelRepository struct {
	*ModelReadStore
	*FilterOptionsStore
}

var _ supply.ModelReadStore = (*ModelRepository)(nil)
var _ supply.ModelFilterOptionsStore = (*ModelRepository)(nil)

func NewModelRepository(schema string, read ModelReadRunner) (*ModelRepository, error) {
	store, err := NewModelReadStore(schema, read)
	if err != nil {
		return nil, err
	}
	return &ModelRepository{store, &FilterOptionsStore{schema: schema, read: read}}, nil
}
