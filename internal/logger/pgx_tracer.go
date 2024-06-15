package logger

import (
	"context"
	"log/slog"
	"runtime"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/tracelog"
)

func NewPGXTracer() *tracelog.TraceLog {
	logger := slog.Default()

	return &tracelog.TraceLog{
		Logger: tracelog.LoggerFunc(func(ctx context.Context, l tracelog.LogLevel, msg string, data map[string]any) {
			attrs := make([]slog.Attr, 0, 2)
			for k, v := range data {
				switch k {
				case "args":
				case "pid":
				default:
					attrs = append(attrs, slog.Any(k, v))
				}
			}

			sort.Slice(attrs, func(i, j int) bool {
				return attrs[i].Key < attrs[j].Key
			})

			var lvl slog.Level
			switch l {
			case tracelog.LogLevelTrace:
				lvl = slog.LevelDebug
			case tracelog.LogLevelDebug:
				lvl = slog.LevelDebug
			case tracelog.LogLevelInfo:
				lvl = slog.LevelDebug
			case tracelog.LogLevelWarn:
				lvl = slog.LevelWarn
			case tracelog.LogLevelError:
				lvl = slog.LevelError
			default:
				lvl = slog.LevelError
				attrs = append(attrs, slog.Any("INVALID_PGX_LOG_LEVEL", l))
			}

			if !logger.Enabled(ctx, lvl) {
				return
			}

			var pc uintptr
			var pcs [1]uintptr
			// skip [runtime.Callers, this function, this function's caller * 3]
			runtime.Callers(5, pcs[:])
			pc = pcs[0]

			r := slog.NewRecord(time.Now(), lvl, msg, pc)
			r.AddAttrs(attrs...)
			_ = logger.Handler().Handle(ctx, r)

		}),
		LogLevel: tracelog.LogLevelDebug,
	}
}
