package genres

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/doug-martin/goqu/v9"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPGXRepository(pg *pgxpool.Pool, l *slog.Logger) Repository {
	return &pgxRepo{pg: pg, g: goqu.Dialect("postgres"), l: l}
}

type pgxRepo struct {
	pg *pgxpool.Pool
	g  goqu.DialectWrapper
	l  *slog.Logger
}

type pgxGenre struct {
	Id    uint16 `db:"id"`
	Title string `db:"title"`
}

func (p *pgxRepo) GetById(ctx context.Context, id uint16) (string, error) {
	sql, params, err := p.g.From("genre").
		Where(goqu.C("id").Eq(id)).
		ToSQL()
	if err != nil {
		return "", err
	}

	var row pgxGenre

	err = pgxscan.Get(ctx, p.pg, &row, sql, params...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil
		}
		return "", err
	}

	return row.Title, nil
}

func (p *pgxRepo) GetByIds(ctx context.Context, ids ...uint16) (map[uint16]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	sql, params, err := p.g.From("genre").
		Where(goqu.C("id").In(ids)).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []pgxGenre

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make(map[uint16]string, len(rows))
	for _, row := range rows {
		ret[row.Id] = row.Title
	}

	return ret, nil
}

func (p *pgxRepo) GetIdByTitle(ctx context.Context, title string) (uint16, error) {
	sql, params, err := p.g.From("genre").
		Where(goqu.L("lower(title)").Eq(strings.ToLower(title))).
		ToSQL()
	if err != nil {
		return 0, err
	}

	var row pgxGenre

	err = pgxscan.Get(ctx, p.pg, &row, sql, params...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil
		}
		return 0, err
	}

	return row.Id, nil
}

func (p *pgxRepo) GetIdByTitles(ctx context.Context, titles ...string) (map[string]uint16, error) {
	if len(titles) == 0 {
		return nil, nil
	}

	lowerTitles := make([]string, 0, len(titles))
	for _, title := range titles {
		lowerTitles = append(lowerTitles, strings.ToLower(title))
	}

	sql, params, err := p.g.From("genre").
		Where(goqu.L("lower(title)").In(lowerTitles)).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []pgxGenre

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make(map[string]uint16, len(rows))
	for _, row := range rows {
		ret[row.Title] = row.Id
	}

	return ret, nil
}

func (p *pgxRepo) Insert(ctx context.Context, titles ...string) (map[string]uint16, error) {
	if len(titles) == 0 {
		return nil, nil
	}

	vals := make([][]any, 0, len(titles))
	for _, title := range titles {
		vals = append(vals, []any{title})
	}

	sql, params, err := p.g.Insert("genre").
		Cols("title").
		Vals(vals...).
		OnConflict(goqu.DoNothing()).
		Returning("id", "title").
		ToSQL()
	if err != nil {
		return nil, err
	}

	rows := make([]pgxGenre, 0, len(titles))

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make(map[string]uint16, len(titles))
	for _, row := range rows {
		ret[row.Title] = row.Id
	}

	var misssingTitles []string
	for _, title := range titles {
		if _, ok := ret[title]; !ok {
			misssingTitles = append(misssingTitles, title)
		}
	}

	if len(misssingTitles) > 0 {
		moreIds, err := p.GetIdByTitles(ctx, misssingTitles...)
		if err != nil {
			return nil, err
		}

		for title, id := range moreIds {
			ret[title] = id
		}
	}

	return ret, nil
}

func (p *pgxRepo) GetAll(ctx context.Context) ([]string, error) {
	sql, params, err := p.g.From("genre").
		Select(goqu.C("title")).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []string

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	return rows, nil
}
