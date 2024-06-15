package logger

import (
	"context"
	"go/build"
	"log/slog"
	"os"
	"runtime"
	"strings"
)

func getEnvOrDefault(key, default_ string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}

	return default_
}

var (
	logFormat = getEnvOrDefault("LOG_FORMAT", "text")
)

// SetupSLog configures logging handler with format depending on environment var LOG_FORMAT
// and which strips common prefix from file paths (rootPath param)
func SetupSLog(lvl slog.Level, rootPath string, requestIdKey any) {
	ho := slog.HandlerOptions{
		Level: lvl,
	}

	var h slog.Handler
	switch logFormat {
	case "json":
		h = slog.NewJSONHandler(os.Stderr, &ho)
		break
	case "text":
		h = slog.NewTextHandler(os.Stderr, &ho)
		break
	default:
		slog.Error("LOG_FORMAT must be json or text")
		os.Exit(1)
	}

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = build.Default.GOPATH
	}

	slog.SetDefault(slog.New(&handler{
		baseHandler:  h,
		rootPath:     strings.TrimSuffix(rootPath, "/") + "/",
		goPath:       strings.TrimSuffix(gopath, "/") + "/",
		requestIdKey: requestIdKey,
	}))
}

type handler struct {
	baseHandler  slog.Handler
	rootPath     string
	goPath       string
	requestIdKey any
}

func (e *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return e.baseHandler.Enabled(ctx, level)
}

func (e *handler) Handle(ctx context.Context, record slog.Record) error {
	record = record.Clone()

	fs := runtime.CallersFrames([]uintptr{record.PC})
	f, _ := fs.Next()
	file := f.File
	if strings.HasPrefix(file, e.rootPath) {
		file = file[len(e.rootPath):]
	} else if strings.HasPrefix(file, e.goPath) {
		file = file[len(e.goPath):]
	}
	record.AddAttrs(slog.Any(slog.SourceKey, &slog.Source{
		Function: f.Function,
		File:     file,
		Line:     f.Line,
	}))

	if requestId := ctx.Value(e.requestIdKey); requestId != nil {
		record.AddAttrs(slog.String("request_id", requestId.(string)))
	}

	return e.baseHandler.Handle(ctx, record)
}

func (e *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{
		baseHandler: e.baseHandler.WithAttrs(attrs),
		rootPath:    e.rootPath,
	}
}

func (e *handler) WithGroup(name string) slog.Handler {
	return &handler{
		baseHandler: e.baseHandler.WithGroup(name),
		rootPath:    e.rootPath,
	}
}
