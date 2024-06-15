package series

import (
	"context"

	"books/internal/types"
)

type Repository interface {
	GetById(ctx context.Context, id string) (*types.Series, error)
	// GetByIds shall return map with NON-NULLS!
	GetByIds(ctx context.Context, ids ...string) (map[string]*types.Series, error)

	Save(ctx context.Context, sequences ...*types.Series) error
}
