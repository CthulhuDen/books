package books

import (
	"context"

	"books/internal/types"
)

type GroupingType string

const (
	GroupByAuthor GroupingType = "author"
	GroupByGenres GroupingType = "genres"
	GroupBySeries GroupingType = "series"
)

type Repository interface {
	GetById(ctx context.Context, id string) (*types.Book, error)
	// GetByIds shall return map with NON-NULLS!
	GetByIds(ctx context.Context, ids ...string) (map[string]*types.Book, error)

	Save(ctx context.Context, books ...*types.Book) error

	LinkBookAndAuthors(ctx context.Context, bookId string, authorIds ...string) error
	LinkBookAndGenres(ctx context.Context, bookId string, genreIds ...uint16) error
	LinkSeriesWithBooks(ctx context.Context, seriesId string, bookIds ...string) error

	Search(ctx context.Context, query string, limit, offset int,
		authorId string, genreIds []uint16, seriesId string,
		yearMin, yearMax uint16,
		groupings ...GroupingType) ([]BookInGroup, error)
}
