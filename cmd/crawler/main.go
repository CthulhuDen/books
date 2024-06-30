package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
	"strings"
	"time"

	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/joho/godotenv/autoload"

	"books/internal/crawler"
	"books/internal/logger"
	"books/internal/storage/authors"
	"books/internal/storage/books"
	"books/internal/storage/fails"
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
	err := lvl.UnmarshalText([]byte(logLevel))
	if err != nil {
		lvl = slog.LevelDebug
	}
	logger.SetupSLog(lvl, path.Dir(path.Dir(path.Dir(thisFile))), struct{}{})

	if err != nil {
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

	c := crawler.StoringConsumer{
		Logger:  slog.Default(),
		Books:   books.NewPGXRepository(pg, slog.Default()),
		Authors: authors.NewPGXRepository(pg, slog.Default()),
		Genres:  genres.NewPGXRepository(pg, slog.Default()),
		Series:  series.NewPGXRepository(pg, slog.Default()),
	}

	fr := fails.NewPGXRepository(pg, slog.Default())
	n := time.Now()
	h := crawler.StoringHandler{
		StartTime: &n,
		Logger:    slog.Default(),
		Fails:     fr,
	}

	if len(os.Args) > 1 && strings.ToLower(os.Args[1]) == "resume" {
		t := n.Add(-time.Hour)
		if len(os.Args) > 2 {
			t, err = time.Parse(time.DateTime, os.Args[2])
			if err != nil {
				slog.Error("Invalid start time provided: " + err.Error())
				os.Exit(1)
			}
		}

		err = resume(&t, &cr, fr, &c, &h)
		if err != nil {
			slog.Error("Resume failed: " + err.Error())
			os.Exit(1)
		}

		os.Exit(0)
	}

	err = cr.Crawl(urlAuthors, urlSeries, &c, &h)
	if err != nil {
		slog.Error("Crawl failed: " + err.Error())
		os.Exit(1)
	}
}

func resume(startTime *time.Time, cr crawler.Crawler, fr fails.Repository, c crawler.Consumer, h crawler.ErrorHandler) error {
	for {
		fs, err := fr.GetFails(context.Background(), startTime, 100)

		if err != nil {
			return fmt.Errorf("fetching list of fails: %w", err)
		}

		if len(fs) == 0 {
			return nil
		}

		for _, f := range fs {
			err := cr.Resume(f.Feed, c, h)
			if err != nil {
				return fmt.Errorf("while resuming %s: %w", f.Feed.Url, err)
			}

			err = fr.DeleteById(context.Background(), f.Id)
			if err != nil {
				return fmt.Errorf("while deleting %s (#%v): %w", f.Feed.Url, f.Id, err)
			}
		}
	}
}
