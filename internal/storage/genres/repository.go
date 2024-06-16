package genres

import (
	"context"
)

type Repository interface {
	GetById(ctx context.Context, id uint16) (string, error)
	// GetByIds shall return map with NON-NULLS!
	GetByIds(ctx context.Context, ids ...uint16) (map[uint16]string, error)

	GetIdByTitle(ctx context.Context, title string) (uint16, error)
	GetIdByTitles(ctx context.Context, titles ...string) (map[string]uint16, error)

	Insert(ctx context.Context, titles ...string) (map[string]uint16, error)

	GetAll(ctx context.Context) ([]string, error)
}
