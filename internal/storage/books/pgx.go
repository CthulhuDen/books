package books

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

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

type pgxBook struct {
	Id       string `db:"id"`
	Title    string `db:"title"`
	Language string `db:"language"`
	Year     uint16 `db:"year"`
	About    string `db:"about"`
	CoverUrl string `db:"cover_url"`
}

func (b *pgxBook) intoCommon(authors []string, genres []string, series []types.InSeries,
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

	return &types.Book{
		Id:       b.Id,
		Title:    b.Title,
		Authors:  authors,
		Series:   series,
		Genres:   genres,
		Language: b.Language,
		Year:     b.Year,
		About:    b.About,
		Cover:    u,
	}
}

func (p *pgxRepo) getAuthorsByBook(ctx context.Context, bookId string) ([]string, error) {
	sql, params, err := p.g.From("book_author").
		Select("author_id").
		Where(goqu.C("book_id").Eq(bookId)).
		Order(goqu.C("author_order").Asc()).
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

func (p *pgxRepo) getGenresByBook(ctx context.Context, bookId string) ([]string, error) {
	sql, params, err := p.g.From("book_genre").
		Join(goqu.T("genre"), goqu.On(goqu.C("genre_id").Eq(goqu.C("id")))).
		Select("title").
		Where(goqu.C("book_id").Eq(bookId)).
		Order(goqu.C("title").Asc()).
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

func (p *pgxRepo) getSeriesByBook(ctx context.Context, bookId string) ([]types.InSeries, error) {
	sql, params, err := p.g.From("book_series").
		Select("series_id", "book_order").
		Where(goqu.C("book_id").Eq(bookId)).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []struct {
		SeriesId  string `json:"series_id"`
		BookOrder uint16 `json:"book_order"`
	}

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make([]types.InSeries, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, types.InSeries{
			Id:       row.SeriesId,
			Position: row.BookOrder,
		})
	}

	return ret, nil
}

func (p *pgxRepo) GetById(ctx context.Context, id string) (*types.Book, error) {
	sql, params, err := p.g.From("book").
		Where(goqu.C("id").Eq(id)).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var row pgxBook

	err = pgxscan.Get(ctx, p.pg, &row, sql, params...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = nil
		}
		return nil, err
	}

	authors, err := p.getAuthorsByBook(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("querying book authors: %w", err)
	}

	genres, err := p.getGenresByBook(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("querying book genres: %w", err)
	}

	series, err := p.getSeriesByBook(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("querying book series: %w", err)
	}

	return row.intoCommon(authors, genres, series, p.l, ctx), nil
}

func (p *pgxRepo) getAuthorsByBooks(ctx context.Context, bookIds ...string) (map[string][]string, error) {
	if len(bookIds) == 0 {
		return map[string][]string{}, nil
	}

	sql, params, err := p.g.From("book_author").
		Select("book_id", "author_id").
		Where(goqu.C("book_id").In(bookIds)).
		Order(goqu.C("author_order").Asc()).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []struct {
		BookId   string `db:"book_id"`
		AuthorId string `db:"author_id"`
	}

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make(map[string][]string, len(bookIds))
	for _, row := range rows {
		ret[row.BookId] = append(ret[row.BookId], row.AuthorId)
	}

	return ret, nil
}

func (p *pgxRepo) getGenresByBooks(ctx context.Context, bookIds ...string) (map[string][]string, error) {
	if len(bookIds) == 0 {
		return map[string][]string{}, nil
	}

	sql, params, err := p.g.From("book_genre").
		Join(goqu.T("genre"), goqu.On(goqu.C("genre_id").Eq(goqu.C("id")))).
		Select("book_id", "title").
		Where(goqu.C("book_id").In(bookIds)).
		Order(goqu.C("title").Asc()).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []struct {
		BookId string `db:"book_id"`
		Title  string `db:"title"`
	}

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make(map[string][]string, len(bookIds))
	for _, row := range rows {
		ret[row.BookId] = append(ret[row.BookId], row.Title)
	}

	return ret, nil
}

func (p *pgxRepo) getSeriesByBooks(ctx context.Context, bookIds ...string) (map[string][]types.InSeries, error) {
	if len(bookIds) == 0 {
		return map[string][]types.InSeries{}, nil
	}

	sql, params, err := p.g.From("book_series").
		Select("book_id", "series_id", "book_order").
		Where(goqu.C("book_id").In(bookIds)).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []struct {
		BookId    string `db:"book_id"`
		SeriesId  string `json:"series_id"`
		BookOrder uint16 `json:"book_order"`
	}

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	ret := make(map[string][]types.InSeries, len(bookIds))
	for _, row := range rows {
		ret[row.BookId] = append(ret[row.BookId], types.InSeries{
			Id:       row.SeriesId,
			Position: row.BookOrder,
		})
	}

	return ret, nil
}

func (p *pgxRepo) GetByIds(ctx context.Context, ids ...string) (map[string]*types.Book, error) {
	if len(ids) == 0 {
		return make(map[string]*types.Book), nil
	}

	sql, params, err := p.g.From("book").
		Where(goqu.C("id").In(ids)).
		ToSQL()
	if err != nil {
		return nil, err
	}

	var rows []pgxBook

	err = pgxscan.Select(ctx, p.pg, &rows, sql, params...)
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return make(map[string]*types.Book), nil
	}

	ids = ids[:0]
	for _, row := range rows {
		ids = append(ids, row.Id)
	}

	authors, err := p.getAuthorsByBooks(ctx, ids...)
	if err != nil {
		return nil, fmt.Errorf("querying books authors: %w", err)
	}

	genres, err := p.getGenresByBooks(ctx, ids...)
	if err != nil {
		return nil, fmt.Errorf("querying books genres: %w", err)
	}

	series, err := p.getSeriesByBooks(ctx, ids...)
	if err != nil {
		return nil, fmt.Errorf("querying books series: %w", err)
	}

	ret := make(map[string]*types.Book, len(ids))
	for _, row := range rows {
		ret[row.Id] = row.intoCommon(authors[row.Id], genres[row.Id], series[row.Id], p.l, ctx)
	}

	return ret, nil
}

func (p *pgxRepo) Save(ctx context.Context, books ...*types.Book) error {
	if len(books) == 0 {
		return nil
	}

	rows := make([]any, 0, len(books))
	for _, book := range books {
		us := ""
		if book.Cover != nil {
			us = book.Cover.String()
		}

		rows = append(rows, pgxBook{
			Id:       book.Id,
			Title:    book.Title,
			Language: book.Language,
			Year:     book.Year,
			About:    book.About,
			CoverUrl: us,
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
