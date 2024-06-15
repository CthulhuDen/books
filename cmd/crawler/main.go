package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
	"strings"

	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/joho/godotenv/autoload"

	"books/internal/crawler"
	"books/internal/logger"
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

var (
	feedAuthors = getEnvOrDefault("FEED_AUTHORS", "https://flibusta.is/opds/authorsindex")
	feedSeries  = getEnvOrDefault("FEED_SERIES", "https://flibusta.is/opds/sequencesindex")
	logLevel    = strings.ToLower(getEnvOrDefault("LOG_LEVEL", "debug"))
	dbConnStr   = os.Getenv("DATABASE_URL")
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)

	var lvl slog.Level
	invalidLvl := false
	switch logLevel {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelDebug
		invalidLvl = true
	}
	logger.SetupSLog(lvl, path.Dir(path.Dir(path.Dir(thisFile))), struct{}{})

	if invalidLvl {
		slog.Error("Invalid log level specified in LOG_LEVEL, one of debug, info, warn or error expected")
		os.Exit(1)
	}

	if feedAuthors == "" {
		slog.Error("You need to specify FEED_AUTHORS env var")
		os.Exit(1)
	}

	urlAuthors, err := url.Parse(feedAuthors)
	if err != nil {
		slog.Error("Invalid URL in FEED_AUTHORS: " + err.Error())
		os.Exit(1)
	}

	if feedSeries == "" {
		slog.Error("You need to specify FEED_SERIES env var")
		os.Exit(1)
	}

	urlSeries, err := url.Parse(feedSeries)
	if err != nil {
		slog.Error("Invalid URL in FEED_SERIES: " + err.Error())
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

	cr := crawler.Flibusta{Client: http.DefaultClient, Logger: slog.Default()}

	consumer := crawler.StoringConsumer{
		Logger:  slog.Default(),
		Books:   books.NewPGXRepository(pg, slog.Default()),
		Authors: authors.NewPGXRepository(pg, slog.Default()),
		Genres:  genres.NewPGXRepository(pg, slog.Default()),
		Series:  series.NewPGXRepository(pg, slog.Default()),
	}

	err = cr.Crawl(urlAuthors, urlSeries, &consumer)
	if err != nil {
		slog.Error("Crawl failed: " + err.Error())
		os.Exit(1)
	}
}
