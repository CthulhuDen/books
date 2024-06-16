package authors

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
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

type pgxAuthor struct {
	Id        string `db:"id"`
	Name      string `db:"name"`
	Bio       string `db:"bio"`
	AvatarUrl string `db:"avatar_url"`
}

func (a *pgxAuthor) intoCommon(l *slog.Logger, ctx context.Context) *types.Author {
	var u *url.URL
	if a.AvatarUrl != "" {
		var err error
		u, err = url.Parse(a.AvatarUrl)
		if err != nil {
			l.ErrorContext(ctx, "Failed to parse avatar URL stored in DB ("+a.AvatarUrl+"): "+err.Error())
			u = nil
		}
	}

	us := ""
	if u != nil {
		us = u.String()
	}

	return &types.Author{
		Id:     a.Id,
		Name:   a.Name,
		Bio:    a.Bio,
		Avatar: us,
	}
}

func (p *pgxRepo) GetById(ctx context.Context, id string) (*types.Author, error) {
	sql, params, err := p.g.From("author").
		Where(goqu.C("id").Eq(id)).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var row pgxAuthor

	err = pgxscan.Get(ctx, p.pg, &row, sql, params...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil
		}
		return nil, err
	}

	return row.intoCommon(p.l, ctx), nil
}

func (p *pgxRepo) GetByIds(ctx context.Context, ids ...string) (map[string]*types.Author, error) {
	if len(ids) == 0 {
		return make(map[string]*types.Author), nil
	}

	sql, params, err := p.g.From("author").
		Where(goqu.C("id").In(ids)).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []pgxAuthor

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make(map[string]*types.Author, len(rows))
	for _, row := range rows {
		ret[row.Id] = row.intoCommon(p.l, ctx)
	}

	return ret, nil
}

func (p *pgxRepo) Save(ctx context.Context, authors ...*types.Author) error {
	if len(authors) == 0 {
		return nil
	}

	rows := make([]any, 0, len(authors))
	for _, author := range authors {
		rows = append(rows, pgxAuthor{
			Id:        author.Id,
			Name:      author.Name,
			Bio:       author.Bio,
			AvatarUrl: author.Avatar,
		})
	}

	sql, params, err := p.g.Insert("author").
		Rows(rows...).
		OnConflict(goqu.DoUpdate("id", map[string]any{
			"name":       goqu.L("excluded.name"),
			"bio":        goqu.L("excluded.bio"),
			"avatar_url": goqu.L("excluded.avatar_url"),
		})).
		ToSQL()
	if err != nil {
		return err
	}

	_, err = p.pg.Exec(ctx, sql, params...)
	return err
}

func (p *pgxRepo) Search(ctx context.Context, query string, limit int, genreIds []uint16) ([]*types.Author, error) {
	qb := p.g.From("author").
		Order(goqu.C("name").Asc()).
		Limit(uint(limit))

	for _, word := range strings.Split(query, " ") {
		word = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(word),
			"\\", "\\\\"),
			"_", "\\_"),
			"%", "\\%")
		if word != "" {
			qb = qb.Where(goqu.C("name").ILike("%" + word + "%"))
		}
	}

	if len(genreIds) > 0 {
		qb = qb.Where(goqu.C("id").In(
			goqu.Select("author_id").
				From("book_author").
				Where(goqu.C("book_id").In(
					goqu.Select("book_id").
						From("book_genre").
						Where(goqu.C("genre_id").In(genreIds)),
				)),
		))
	}

	sql, params, err := qb.ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []pgxAuthor

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make([]*types.Author, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.intoCommon(p.l, ctx))
	}

	return ret, nil
}
