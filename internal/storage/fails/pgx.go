package fails

import (
	"context"
	"log/slog"
	"net/url"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"

	"books/internal/types"
)

func NewPGXRepository(pg *pgxpool.Pool, l *slog.Logger) Repository {
	return &pgxRepo{pg: pg, g: goqu.Dialect("postgres"), l: l}
}

type pgxRepo struct {
	pg *pgxpool.Pool
	g  goqu.DialectWrapper
	l  *slog.Logger
}

type pgxFeed struct {
	Url    string        `json:"url"`
	Type   uint8         `json:"type"`
	Author *types.Author `json:"author,omitempty"`
	Series *types.Series `json:"series,omitempty"`
}

type pgxRecord struct {
	Id        uint64     `db:"id"`
	StartTime *time.Time `db:"start_time"`
	Feed      pgxFeed    `db:"feed"`
	Error     string     `db:"error"`
}

func (p *pgxRepo) Save(ctx context.Context, startTime *time.Time, feed types.ResumableFeed, err error) error {
	feedRow := pgxFeed{
		Url:    feed.Url.String(),
		Type:   uint8(feed.Type),
		Author: feed.Author,
		Series: feed.Series,
	}

	sql, params, err := p.g.Insert("fail").
		Rows(goqu.Record{
			"start_time": startTime,
			"feed":       feedRow,
			"error":      err.Error(),
		}).
		ToSQL()
	if err != nil {
		return err
	}

	_, err = p.pg.Exec(ctx, sql, params...)
	return err
}

func (p *pgxRepo) GetFails(ctx context.Context, notAfter *time.Time) ([]*Record, error) {
	sql, params, err := p.g.From("fail").
		Where(goqu.C("start_time").Lte(notAfter)).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []pgxRecord

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make([]*Record, 0, len(rows))
	for _, row := range rows {
		u, err := url.Parse(row.Feed.Url)
		if err != nil {
			p.l.ErrorContext(ctx, "Failed to parse fail feed URL stored in DB ("+row.Feed.Url+"): "+err.Error())
			continue
		}

		ret = append(ret, &Record{
			Id:        row.Id,
			StartTime: row.StartTime,
			Feed: types.ResumableFeed{
				Url:    u,
				Type:   types.FeedType(row.Feed.Type),
				Author: row.Feed.Author,
				Series: row.Feed.Series,
			},
			Error: row.Error,
		})
	}

	return ret, nil
}

func (p *pgxRepo) DeleteById(ctx context.Context, id uint64) error {
	sql, params, err := p.g.Delete("fail").
		Where(goqu.C("id").Eq(id)).
		ToSQL()
	if err != nil {
		return err
	}

	//p.l.InfoContext(ctx, sql)
	//return nil

	_, err = p.pg.Exec(ctx, sql, params...)
	return err
}
