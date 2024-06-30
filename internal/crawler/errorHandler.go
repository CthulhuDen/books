package crawler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"books/internal/storage/fails"
	"books/internal/types"
)

type ErrorHandler interface {
	Handle(feed types.ResumableFeed, err error) error
}

type StoringHandler struct {
	StartTime *time.Time
	Logger    *slog.Logger
	Fails     fails.Repository
}

func (s *StoringHandler) Handle(feed types.ResumableFeed, err error) error {
	err = s.Fails.Save(context.Background(), s.StartTime, feed, err)
	if err != nil {
		err = fmt.Errorf("saving fail: %w", err)
	}

	return err
}
