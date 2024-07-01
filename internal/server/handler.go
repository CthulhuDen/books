package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"books/internal/response"
	"books/internal/storage/authors"
	"books/internal/storage/books"
	"books/internal/storage/genres"
	"books/internal/storage/series"
	"books/internal/types"
)

func Handler(ar authors.Repository, br books.Repository, gr genres.Repository, sr series.Repository,
	rr *response.Responder) http.Handler {

	r := chi.NewRouter()

	r.Get("/genres", func(w http.ResponseWriter, r *http.Request) {
		rows, err := gr.GetAll(r.Context())
		if err != nil {
			rr.RespondAndLogError(w, r.Context(), err)
			return
		}

		if rows == nil {
			rows = make([]string, 0)
		}

		rr.SendJson(w, r.Context(), struct {
			Titles []string `json:"titles"`
		}{Titles: rows})
	})

	r.Get("/authors", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		rows, err := ar.Search(r.Context(), q.Get("search"),
			getGenreIds(r.Context(), q, gr),
			getIntOrDefault("limit", q, 10),
		)

		if err != nil {
			rr.RespondAndLogError(w, r.Context(), err)
			return
		}

		rr.SendJson(w, r.Context(), struct {
			Authors []*types.Author `json:"authors"`
		}{Authors: rows})
	})

	r.Get("/series", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		rows, err := sr.Search(r.Context(), q.Get("search"),
			q.Get("author"), getGenreIds(r.Context(), q, gr),
			getIntOrDefault("limit", q, 10),
		)

		if err != nil {
			rr.RespondAndLogError(w, r.Context(), err)
			return
		}

		if rows == nil {
			rows = make([]*types.Series, 0)
		}

		rr.SendJson(w, r.Context(), struct {
			Sequences []*types.Series `json:"sequences"`
		}{Sequences: rows})
	})

	r.Get("/books", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		var groupings []books.GroupingType
		for _, t := range getMulti("group", q) {
			groupings = append(groupings, books.GroupingType(t))
		}

		rows, err := br.Search(r.Context(), q.Get("search"),
			q.Get("author"), getGenreIds(r.Context(), q, gr), q.Get("series"),
			uint16(getIntOrDefault("year_min", q, 0)),
			uint16(getIntOrDefault("year_max", q, 0)),
			getIntOrDefault("limit", q, 20), getIntOrDefault("offset", q, 0),
			groupings...)

		if err != nil {
			rr.RespondAndLogError(w, r.Context(), err)
			return
		}

		var authorIds []string
		seenAuthor := make(map[string]struct{})
		var seriesIds []string
		seenSeries := make(map[string]struct{})

		for _, row := range rows {
			for _, authorId := range row.Book.Authors {
				if _, ok := seenAuthor[authorId]; !ok {
					seenAuthor[authorId] = struct{}{}
					authorIds = append(authorIds, authorId)
				}
			}
			for _, s := range row.Book.Series {
				if _, ok := seenSeries[s.Id]; !ok {
					seenSeries[s.Id] = struct{}{}
					seriesIds = append(seriesIds, s.Id)
				}
			}
		}

		as, err := ar.GetByIds(r.Context(), authorIds...)
		if err != nil {
			rr.RespondAndLogError(w, r.Context(), err)
			return
		}

		ss, err := sr.GetByIds(r.Context(), seriesIds...)
		if err != nil {
			rr.RespondAndLogError(w, r.Context(), err)
			return
		}

		if rows == nil {
			rows = make([]books.BookInGroup, 0)
		}

		rr.SendJson(w, r.Context(), struct {
			Books   []books.BookInGroup      `json:"books"`
			Authors map[string]*types.Author `json:"authors"`
			Series  map[string]*types.Series `json:"series"`
		}{
			Books:   rows,
			Authors: as,
			Series:  ss,
		})
	})

	return r
}

func Static(r chi.Router, openApiYaml, webDir string) {
	r.Get("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, openApiYaml)
	})

	for _, filename := range []string{"/rapidoc.html", "/redocly.html", "/swagger-ui.html", "/scalar.html"} {
		r.Get(filename, func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, webDir+filename)
		})
	}
}

func getGenreIds(ctx context.Context, q url.Values, gr genres.Repository) []uint16 {
	var genreIds []uint16

	genres_ := getMulti("genre", q)
	if len(genres_) > 0 {
		gs, err := gr.GetIdByTitles(ctx, genres_...)
		if err == nil && len(gs) > 0 {
			for _, genreId := range gs {
				genreIds = append(genreIds, genreId)
			}
		}
	}

	return genreIds
}

func getIntOrDefault(key string, q url.Values, default_ int) int {
	if ls := q.Get(key); ls != "" {
		limit, err := strconv.Atoi(ls)
		if err == nil {
			return limit
		}
	}

	return default_
}

func getMulti(key string, q url.Values) []string {
	raw, ok := q[key]
	if !ok {
		return nil
	}

	vals := make([]string, 0, len(raw))
	for _, val := range raw {
		val = strings.TrimSpace(val)
		if val != "" {
			vals = append(vals, val)
		}
	}

	return vals
}
