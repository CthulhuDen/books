package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path"
	"runtime"
	"strings"

	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/joho/godotenv/autoload"

	"books/internal/logger"
	"books/internal/response"
	"books/internal/server"
	"books/internal/storage/authors"
	"books/internal/storage/books"
	"books/internal/storage/genres"
	"books/internal/storage/series"
)

func getEnvOrDefault(key, default_ string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}

	return default_
}

func getBoolEnv(key string) bool {
	if val := strings.ToLower(os.Getenv(key)); val == "yes" || val == "on" || val == "true" {
		return true
	}

	return false
}

var (
	logLevel  = strings.ToLower(getEnvOrDefault("LOG_LEVEL", "debug"))
	dbConnStr = os.Getenv("DATABASE_URL")
	bindAddr  = getEnvOrDefault("BIND_ADDR", ":8080")
	debugMode = getBoolEnv("DEBUG_MODE")
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)

	var lvl slog.Level
	err := lvl.UnmarshalText([]byte(logLevel))
	if err != nil {
		lvl = slog.LevelDebug
	}
	logger.SetupSLog(lvl, path.Dir(path.Dir(path.Dir(thisFile))), middleware.RequestIDKey)

	if err != nil {
		slog.Error("Invalid log level specified in LOG_LEVEL, one of debug, info, warn or error expected")
		os.Exit(1)
	}

	cfg, err := pgxpool.ParseConfig(dbConnStr)
	if err != nil {
		slog.Error("Failed to parse DATABASE_URL: " + err.Error())
		os.Exit(1)
	}

	cfg.ConnConfig.Tracer = logger.NewPGXTracer()

	pg, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		slog.Error("failed to create postgres pool: " + err.Error())
		os.Exit(1)
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)

	r.Mount("/api", server.Handler(
		authors.NewPGXRepository(pg, slog.Default()),
		books.NewPGXRepository(pg, slog.Default()),
		genres.NewPGXRepository(pg, slog.Default()),
		series.NewPGXRepository(pg, slog.Default()),
		&response.Responder{DebugMode: debugMode},
	))

	slog.Error("aborting: " + http.ListenAndServe(bindAddr, r).Error())
	os.Exit(1)
}
