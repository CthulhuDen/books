package crawler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"books/internal/storage/authors"
	"books/internal/storage/books"
	"books/internal/storage/genres"
	"books/internal/storage/series"
	"books/internal/types"
)

type Consumer interface {
	ConsumeAuthor(author *types.Author) error
	ConsumeBooks(books []*types.Book, fetchAuthor func(id string) (*types.Author, error)) error
	ConsumeSeries(series *types.Series, bookIds []string) error
}

type LoggerConsumer struct {
	Logger *slog.Logger
}

func (c *LoggerConsumer) ConsumeAuthor(author *types.Author) error {
	suffixAva := ""
	if author.Avatar != "" {
		suffixAva = " with avatar"
	}

	suffixBio := ""
	if author.Bio != "" {
		suffixBio = " with bio"
	}

	c.Logger.Info("Consumed author " + author.Id + " (" + author.Name + ")" + suffixAva + suffixBio)
	return nil
}

func (c *LoggerConsumer) ConsumeBooks(books []*types.Book, fetchAuthor func(id string) (*types.Author, error)) error {
	for _, b := range books {
		var authors_ string
		if len(b.Authors) > 0 {
			sb := strings.Builder{}
			if len(b.Authors) > 1 {
				sb.WriteString("by authors ")
			} else {
				sb.WriteString("by author ")
			}
			for ix, authId := range b.Authors {
				if ix != 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(authId)

				// Just make sure there are no errors
				_, err := fetchAuthor(authId)
				if err != nil {
					return fmt.Errorf("checking crawler fetchAuthor: %w", err)
				}
			}
			authors_ = sb.String()
		} else {
			authors_ = "without authors"
		}

		c.Logger.Info("Consumed book " + b.Id + " (" + b.Title + ") " + authors_)
	}

	return nil
}

func (c *LoggerConsumer) ConsumeSeries(series *types.Series, bookIds []string) error {
	sb := strings.Builder{}
	sb.WriteString("Consumed series ")
	sb.WriteString(series.Id)
	sb.WriteString(" (")
	sb.WriteString(series.Title)
	sb.WriteString(") with books ")

	for ix, id := range bookIds {
		if ix != 0 {
			sb.WriteString(", ")
		}

		sb.WriteString(id)
	}

	c.Logger.Info(sb.String())

	return nil
}

type StoringConsumer struct {
	Logger  *slog.Logger
	Books   books.Repository
	Authors authors.Repository
	Genres  genres.Repository
	Series  series.Repository
}

func (s *StoringConsumer) ConsumeAuthor(author *types.Author) error {
	return s.Authors.Save(context.Background(), author)
}

func (s *StoringConsumer) ConsumeBooks(books []*types.Book, fetchAuthor func(id string) (*types.Author, error)) error {
	uniqAuthorIds := make(map[string]struct{})
	uniqGenreTitles := make(map[string]struct{})

	for _, b := range books {
		for _, authorId := range b.Authors {
			uniqAuthorIds[authorId] = struct{}{}
		}
		for _, genreTitle := range b.Genres {
			uniqGenreTitles[genreTitle] = struct{}{}
		}
	}

	var authorIds []string
	for authorId := range uniqAuthorIds {
		authorIds = append(authorIds, authorId)
	}

	as, err := s.Authors.GetByIds(context.Background(), authorIds...)
	if err != nil {
		return fmt.Errorf("checking existing authors: %w", err)
	}

	for _, authorId := range authorIds {
		if _, ok := as[authorId]; ok {
			continue
		}

		a, err := fetchAuthor(authorId)
		if err != nil {
			return fmt.Errorf("fetching new author: %w", err)
		}

		if err := s.Authors.Save(context.Background(), a); err != nil {
			return fmt.Errorf("saving new author: %w", err)
		}
	}

	var genreTitles []string
	for genreTitle := range uniqGenreTitles {
		genreTitles = append(genreTitles, genreTitle)
	}

	gs, err := s.Genres.GetIdByTitles(context.Background(), genreTitles...)
	if err != nil {
		return fmt.Errorf("finding existing genres: %w", err)
	}

	numNewGenres := 0
	for _, genreTitle := range genreTitles {
		if _, ok := gs[genreTitle]; !ok {
			genreTitles[numNewGenres] = genreTitle
			numNewGenres += 1
		}
	}
	genreTitles = genreTitles[:numNewGenres]

	newGenres, err := s.Genres.Insert(context.Background(), genreTitles...)
	if err != nil {
		return fmt.Errorf("inserting new genres: %w", err)
	}

	for genreTitle, genreId := range newGenres {
		gs[genreTitle] = genreId
	}

	err = s.Books.Save(context.Background(), books...)
	if err != nil {
		return fmt.Errorf("saving books: %w", err)
	}

	for _, book := range books {
		err := s.Books.LinkBookAndAuthors(context.Background(), book.Id, book.Authors...)
		if err != nil {
			return fmt.Errorf("linking book and authors: %w", err)
		}

		var bookGenres []uint16
		for _, genreTitle := range book.Genres {
			genreId, ok := gs[genreTitle]
			if !ok {
				return fmt.Errorf("impossible lacdkmsgtr " + genreTitle)
			}

			bookGenres = append(bookGenres, genreId)
		}

		err = s.Books.LinkBookAndGenres(context.Background(), book.Id, bookGenres...)
		if err != nil {
			return fmt.Errorf("linking book and genres: %w", err)
		}
	}

	return nil
}

func (s *StoringConsumer) ConsumeSeries(series *types.Series, bookIds []string) error {
	err := s.Series.Save(context.Background(), series)
	if err != nil {
		return fmt.Errorf("saving series: %w", err)
	}

	bs, err := s.Books.GetByIds(context.Background(), bookIds...)
	if err != nil {
		return fmt.Errorf("finding existing books: %w", err)
	}

	for _, bookId := range bookIds {
		if _, ok := bs[bookId]; !ok {
			s.Logger.Warn("Couldn't consume series because one of the book was not found in store: "+bookId,
				slog.String("series", series.Id))
			return nil
		}
	}

	err = s.Books.LinkSeriesWithBooks(context.Background(), series.Id, bookIds...)
	if err != nil {
		return fmt.Errorf("linking series with books: %w", err)
	}

	return nil
}
