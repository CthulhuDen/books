package series

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/doug-martin/goqu/v9"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
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

type pgxSeries struct {
	Id    string `db:"id"`
	Title string `db:"title"`
}

func (p *pgxRepo) GetById(ctx context.Context, id string) (*types.Series, error) {
	sql, params, err := p.g.From("series").
		Where(goqu.C("id").Eq(id)).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var row pgxSeries

	err = pgxscan.Get(ctx, p.pg, &row, sql, params...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil
		}
		return nil, err
	}

	return &types.Series{
		Id:    row.Id,
		Title: row.Title,
	}, nil
}

func (p *pgxRepo) GetByIds(ctx context.Context, ids ...string) (map[string]*types.Series, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	sql, params, err := p.g.From("series").
		Where(goqu.C("id").In(ids)).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []pgxSeries

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make(map[string]*types.Series, len(rows))
	for _, row := range rows {
		ret[row.Id] = &types.Series{
			Id:    row.Id,
			Title: row.Title,
		}
	}

	return ret, nil
}

func (p *pgxRepo) Save(ctx context.Context, sequences ...*types.Series) error {
	if len(sequences) == 0 {
		return nil
	}

	rows := make([]any, 0, len(sequences))
	for _, series := range sequences {
		rows = append(rows, pgxSeries{
			Id:    series.Id,
			Title: series.Title,
		})
	}

	sql, params, err := p.g.Insert("series").
		Rows(rows...).
		OnConflict(goqu.DoUpdate("id", map[string]any{
			"title": goqu.L("excluded.title"),
		})).
		ToSQL()
	if err != nil {
		return err
	}

	_, err = p.pg.Exec(ctx, sql, params...)
	return err
}

func (p *pgxRepo) Search(ctx context.Context, query string,
	authorId string, genreIds []uint16,
	limit int) ([]*types.Series, error) {

	qb := p.g.From("series").
		Order(goqu.C("title").Asc()).
		Limit(uint(limit))

	query = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(query),
		"\\", "\\\\"),
		"_", "\\_"),
		"%", "\\%")
	if query != "" {
		qb = qb.Where(goqu.C("title").ILike("%" + query + "%"))
	}

	authorId = strings.ToLower(authorId)
	if authorId != "" || len(genreIds) > 0 {
		sub := goqu.Select("series_id").
			From("book_series")

		if authorId != "" {
			sub = sub.Where(goqu.C("book_id").In(
				goqu.Select("book_id").
					From("book_author").
					Where(goqu.C("author_id").Eq(authorId)),
			))
		}

		if len(genreIds) > 0 {
			sub = sub.Where(goqu.C("book_id").In(
				goqu.Select("book_id").
					From("book_genre").
					Where(goqu.C("genre_id").In(genreIds)),
			))
		}

		qb = qb.Where(goqu.C("id").In(sub))
	}

	sql, params, err := qb.ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []pgxSeries

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make([]*types.Series, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, &types.Series{Id: row.Id, Title: row.Title})
	}

	return ret, nil
}
