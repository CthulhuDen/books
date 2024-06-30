package fails

import (
	"context"
	"time"

	"books/internal/types"
)

type Record struct {
	Id        uint64
	StartTime *time.Time
	Feed      types.ResumableFeed
	Error     string
}

type Repository interface {
	Save(ctx context.Context, startTime *time.Time, feed types.ResumableFeed, err error) error

	GetFails(ctx context.Context, notAfter *time.Time, limit uint) ([]*Record, error)
	DeleteById(ctx context.Context, id uint64) error
}
