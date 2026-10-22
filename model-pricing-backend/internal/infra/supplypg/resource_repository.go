package supplypg

import "model_bss/internal/domain/supply"

type ResourceRepository struct {
	*ResourceReadStore
	*FilterOptionsStore
}

var _ supply.ResourceReadStore = (*ResourceRepository)(nil)
var _ supply.ResourceFilterOptionsStore = (*ResourceRepository)(nil)

func NewResourceRepository(schema string, read ModelReadRunner, facts ResourceFactsLoader) (*ResourceRepository, error) {
	store, err := NewResourceReadStore(schema, read, facts)
	if err != nil {
		return nil, err
	}
	return &ResourceRepository{store, &FilterOptionsStore{schema: schema, read: read}}, nil
}
