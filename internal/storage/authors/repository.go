package authors

import (
	"context"

	"books/internal/types"
)

type Repository interface {
	GetById(ctx context.Context, id string) (*types.Author, error)
	// GetByIds shall return map with NON-NULLS!
	GetByIds(ctx context.Context, ids ...string) (map[string]*types.Author, error)

	Save(ctx context.Context, authors ...*types.Author) error

	Search(ctx context.Context, query string, genreIds []uint16, limit int) ([]*types.Author, error)
}
