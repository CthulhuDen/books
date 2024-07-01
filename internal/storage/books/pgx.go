package books

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

var (
	subAuthors = goqu.Select(goqu.L("array_agg(author_id order by author_order)")).
			From("book_author").
			Where(goqu.C("book_id").Eq(goqu.C("id")))
	subGenres = goqu.Select(goqu.L("array_agg(genre.title order by genre.title)")).
			From("book_genre").
			Join(goqu.T("genre"), goqu.On(
			goqu.C("id").Table("genre").
				Eq(goqu.C("genre_id")),
		)).
		Where(goqu.C("book_id").Eq(goqu.C("id").Table("book")))
	subSequences = goqu.Select(goqu.L("jsonb_object_agg(series_id, book_order)")).
			From("book_series").
			Where(goqu.C("book_id").Eq(goqu.C("id")))
)

func NewPGXRepository(pg *pgxpool.Pool, l *slog.Logger) Repository {
	return &pgxRepo{pg: pg, g: goqu.Dialect("postgres"), l: l}
}

type pgxRepo struct {
	pg *pgxpool.Pool
	g  goqu.DialectWrapper
	l  *slog.Logger
}

type pgxBook struct {
	Id       string `db:"id"`
	Title    string `db:"title"`
	Language string `db:"language"`
	Year     uint16 `db:"year"`
	About    string `db:"about"`
	CoverUrl string `db:"cover_url"`
}

type pgxBookRealFull struct {
	Base      pgxBook  `db:""` // follow
	AuthorIds []string `db:"authors"`
	Genres    []string `db:"genres"`
	Sequences any      `db:"sequences"`
	Groupings any      `db:"groupings"`
}

func (b *pgxBook) intoCommon(authors []string, genres []string, sequences map[string]any,
	l *slog.Logger, ctx context.Context) *types.Book {

	var u *url.URL
	if b.CoverUrl != "" {
		var err error
		u, err = url.Parse(b.CoverUrl)
		if err != nil {
			l.ErrorContext(ctx, "Failed to parse cover URL stored in DB ("+b.CoverUrl+"): "+err.Error())
			u = nil
		}
	}

	us := ""
	if u != nil {
		us = u.String()
	}

	series := make([]types.InSeries, 0, len(sequences))
	for id, order := range sequences {
		series = append(series, types.InSeries{Id: id, Order: uint16(order.(float64))})
	}

	return &types.Book{
		Id:       b.Id,
		Title:    b.Title,
		Authors:  authors,
		Series:   series,
		Genres:   genres,
		Language: b.Language,
		Year:     b.Year,
		About:    b.About,
		Cover:    us,
	}
}

func (p *pgxRepo) GetById(ctx context.Context, id string) (*types.Book, error) {
	sql, params, err := p.g.From("book").
		Select("*",
			subAuthors.As("authors"),
			subGenres.As("genres"),
			subSequences.As("sequences")).
		Where(goqu.C("id").Eq(id)).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var row pgxBookRealFull

	err = pgxscan.Get(ctx, p.pg, &row, sql, params...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil
		}
		return nil, err
	}

	seqs, _ := row.Sequences.(map[string]any)

	return row.Base.intoCommon(row.AuthorIds, row.Genres, seqs, p.l, ctx), nil
}

func (p *pgxRepo) GetByIds(ctx context.Context, ids ...string) (map[string]*types.Book, error) {
	if len(ids) == 0 {
		return make(map[string]*types.Book), nil
	}

	sql, params, err := p.g.From("book").
		Select("*",
			subAuthors.As("authors"),
			subGenres.As("genres"),
			subSequences.As("sequences")).
		Where(goqu.C("id").In(ids)).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []pgxBookRealFull

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make(map[string]*types.Book, len(rows))
	for _, row := range rows {
		seqs, _ := row.Sequences.(map[string]any)
		ret[row.Base.Id] = row.Base.intoCommon(row.AuthorIds, row.Genres, seqs, p.l, ctx)
	}

	return ret, nil
}

func (p *pgxRepo) Save(ctx context.Context, books ...*types.Book) error {
	if len(books) == 0 {
		return nil
	}

	rows := make([]any, 0, len(books))
	for _, book := range books {
		rows = append(rows, pgxBook{
			Id:       book.Id,
			Title:    book.Title,
			Language: book.Language,
			Year:     book.Year,
			About:    book.About,
			CoverUrl: book.Cover,
		})
	}

	sql, params, err := p.g.Insert("book").
		Rows(rows...).
		OnConflict(goqu.DoUpdate("id", map[string]any{
			"title":     goqu.L("excluded.title"),
			"language":  goqu.L("excluded.language"),
			"year":      goqu.L("excluded.year"),
			"about":     goqu.L("excluded.about"),
			"cover_url": goqu.L("excluded.cover_url"),
		})).
		ToSQL()
	if err != nil {
		return err
	}

	_, err = p.pg.Exec(ctx, sql, params...)
	return err
}

func (p *pgxRepo) LinkBookAndAuthors(ctx context.Context, bookId string, authorIds ...string) error {
	sql, params, err := p.g.Delete("book_author").
		Where(goqu.C("book_id").Eq(bookId)).
		ToSQL()
	if err != nil {
		return err
	}

	_, err = p.pg.Exec(ctx, sql, params...)
	if err != nil {
		return err
	}

	if len(authorIds) == 0 {
		return nil
	}

	type row struct {
		BookId      string `db:"book_id"`
		AuthorId    string `db:"author_id"`
		AuthorOrder uint16 `db:"author_order"`
	}

	rows := make([]any, 0, len(authorIds))

	for ix, authorId := range authorIds {
		rows = append(rows, row{
			BookId:      bookId,
			AuthorId:    authorId,
			AuthorOrder: uint16(ix + 1),
		})
	}

	sql, params, err = p.g.Insert("book_author").
		Rows(rows...).
		ToSQL()
	if err != nil {
		return err
	}

	_, err = p.pg.Exec(ctx, sql, params...)
	return err
}

func (p *pgxRepo) LinkBookAndGenres(ctx context.Context, bookId string, genreIds ...uint16) error {
	sql, params, err := p.g.Delete("book_genre").
		Where(goqu.C("book_id").Eq(bookId)).
		ToSQL()
	if err != nil {
		return err
	}

	_, err = p.pg.Exec(ctx, sql, params...)
	if err != nil {
		return err
	}

	if len(genreIds) == 0 {
		return nil
	}

	type row struct {
		BookId  string `db:"book_id"`
		GenreId uint16 `db:"genre_id"`
	}

	rows := make([]any, 0, len(genreIds))

	for _, genreId := range genreIds {
		rows = append(rows, row{
			BookId:  bookId,
			GenreId: genreId,
		})
	}

	sql, params, err = p.g.Insert("book_genre").
		Rows(rows...).
		ToSQL()
	if err != nil {
		return err
	}

	_, err = p.pg.Exec(ctx, sql, params...)
	return err
}

func (p *pgxRepo) LinkSeriesWithBooks(ctx context.Context, seriesId string, bookIds ...string) error {
	sql, params, err := p.g.Delete("book_series").
		Where(goqu.C("series_id").Eq(seriesId)).
		ToSQL()
	if err != nil {
		return err
	}

	_, err = p.pg.Exec(ctx, sql, params...)
	if err != nil {
		return err
	}

	if len(bookIds) == 0 {
		return nil
	}

	type row struct {
		BookId    string `db:"book_id"`
		SeriesId  string `db:"series_id"`
		BookOrder uint16 `db:"book_order"`
	}

	rows := make([]any, 0, len(bookIds))

	for ix, bookId := range bookIds {
		rows = append(rows, row{
			BookId:    bookId,
			SeriesId:  seriesId,
			BookOrder: uint16(ix + 1),
		})
	}

	sql, params, err = p.g.Insert("book_series").
		Rows(rows...).
		ToSQL()
	if err != nil {
		return err
	}

	_, err = p.pg.Exec(ctx, sql, params...)
	return err
}

func (p *pgxRepo) Search(ctx context.Context, query string,
	authorId string, genreIds []uint16, seriesId string,
	yearMin, yearMax uint16,
	limit, offset int,
	groupings ...GroupingType) ([]BookInGroup, error) {

	qb := p.g.From("book").
		Select("book.*",
			subAuthors.As("authors"),
			subGenres.As("genres"),
			subSequences.As("sequences")).
		Limit(uint(limit))

	if offset != 0 {
		qb = qb.Offset(uint(offset))
	}

	groupingExprs := make([]string, 0, len(groupings))
	groupingPostProcess := make([]func(row *pgxBookRealFull) Grouping, 0, len(groupings))

	seenGrouping := make(map[GroupingType]struct{}, len(groupings))
	for _, grouping := range groupings {
		if _, ok := seenGrouping[grouping]; ok {
			continue
		}

		seenGrouping[grouping] = struct{}{}

		switch grouping {
		case GroupByAuthor:
			groupIx := len(groupingExprs)
			groupingPostProcess = append(groupingPostProcess, func(row *pgxBookRealFull) Grouping {
				return Grouping{ByAuthor: row.Groupings.([]any)[groupIx].(string)}
			})
			groupingExprs = append(groupingExprs, "book_author.author_id")
			qb = qb.
				Join(goqu.T("book_author"), goqu.On(
					goqu.C("id").Eq(goqu.C("book_id").Table("book_author")),
				)).
				OrderAppend(goqu.C("author_id").Asc())
		case GroupBySeries:
			groupIx := len(groupingExprs)
			groupingPostProcess = append(groupingPostProcess, func(row *pgxBookRealFull) Grouping {
				return Grouping{BySeries: row.Groupings.([]any)[groupIx].(string)}
			})
			groupingExprs = append(groupingExprs, "book_series.series_id")

			if seriesId == "" {
				qb = qb.
					Join(goqu.T("book_series"), goqu.On(
						goqu.C("id").Eq(goqu.C("book_id").Table("book_series")),
					)).
					OrderAppend(goqu.C("series_id").Asc())
			}
		case GroupByGenres:
			groupingPostProcess = append(groupingPostProcess, func(row *pgxBookRealFull) Grouping {
				return Grouping{ByGenres: row.Genres}
			})

			qb = qb.OrderAppend(goqu.C("genres").Asc())
		}
	}

	if len(groupingExprs) != 0 {
		qb = qb.SelectAppend(
			goqu.L("jsonb_build_array(" + strings.Join(groupingExprs, ", ") + ")").
				As("groupings"),
		)
	}

	query = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(query),
		"\\", "\\\\"),
		"_", "\\_"),
		"%", "\\%")
	if query != "" {
		qb = qb.Where(goqu.C("title").ILike("%" + query + "%"))
	}

	authorId = strings.TrimSpace(authorId)
	if authorId != "" {
		qb = qb.Where(goqu.C("id").In(
			goqu.Select("book_id").
				From("book_author").
				Where(goqu.C("author_id").Eq(authorId)),
		))
	}

	if len(genreIds) > 0 {
		qb = qb.Where(goqu.C("id").In(
			goqu.Select("book_id").
				From("book_genre").
				Where(goqu.C("genre_id").In(genreIds)),
		))
	}

	seriesId = strings.TrimSpace(seriesId)
	if seriesId != "" {
		qb = qb.
			Join(goqu.T("book_series"), goqu.On(
				goqu.C("id").
					Eq(goqu.C("book_id").Table("book_series")),
			)).
			Where(goqu.C("series_id").Eq(seriesId)).
			OrderAppend(goqu.C("book_order").Asc())
	}

	if yearMin > 0 {
		qb = qb.Where(goqu.C("year").Gte(yearMin))
	}

	if yearMax > 0 {
		qb = qb.Where(goqu.C("year").Lte(yearMax))
	}

	sql, params, err := qb.
		OrderAppend(goqu.C("title").Asc()).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []pgxBookRealFull

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make([]BookInGroup, 0, len(rows))
	for _, row := range rows {
		groupings := make([]Grouping, 0, len(groupingPostProcess))
		for _, pp := range groupingPostProcess {
			groupings = append(groupings, pp(&row))
		}

		seqs, _ := row.Sequences.(map[string]any)

		ret = append(ret, BookInGroup{
			Groups: groupings,
			Book:   row.Base.intoCommon(row.AuthorIds, row.Genres, seqs, p.l, ctx),
		})
	}

	return ret, nil
}
