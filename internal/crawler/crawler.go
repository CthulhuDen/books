package crawler

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/opds-community/libopds2-go/opds1"

	"books/internal/types"
)

const (
	linkTypeCatalog = "application/atom+xml;profile=opds-catalog"
	linkRelImage    = "http://opds-spec.org/image"
	linkRelNext     = "next"

	authorIdTemplate   = "tag:author:%v"
	authorHrefTemplate = "/opds/author/%v"
)

var (
	regLinkTypeImage = regexp.MustCompile("^image/[^/]+$")

	regTagAuthors     = regexp.MustCompile("^tag:authors:[^:]+$")
	regTagAuthor      = regexp.MustCompile("^tag:author:(\\d+)$")
	regTagBio         = regexp.MustCompile("^tag:author:bio:\\d+$")
	regTagAuthorBooks = regexp.MustCompile("^tag:author:\\d+:alphabet$")
	regTagBook        = regexp.MustCompile("^tag:book:[^:]+$")
	regTagSeries      = regexp.MustCompile("^tag:sequences:[^:]+$")
	regTagSequence    = regexp.MustCompile("^tag:sequence:\\d+$")

	regHrefAuthor    = regexp.MustCompile("^/opds/author/\\d+$")
	regHrefAuthorAlt = regexp.MustCompile("^/a/(\\d+)$")
	regHrefSequence  = regexp.MustCompile("^/opds/sequencebooks/\\d+$")

	regTitleAuthorBooks = regexp.MustCompile("^Книги автора\\s+(.+)$")
)

type Crawler interface {
	// Crawl MAY call consumer concurrently
	Crawl(authorsFeed *url.URL, seriesFeed *url.URL, consumer Consumer, handler ErrorHandler) error
	Resume(feed types.ResumableFeed, consumer Consumer, handler ErrorHandler) error
}

func consumeError(err error, feed types.ResumableFeed, handler ErrorHandler, l *slog.Logger) error {
	if er := new(unresumableError); errors.As(err, er) {
		return err
	}

	if err != nil {
		strTyp := "unknown feed type"
		switch feed.Type {
		case types.FeedTypeAuthors:
			strTyp = "authors feed"
		case types.FeedTypeAuthor:
			strTyp = "author"
		case types.FeedTypeBooks:
			strTyp = "books feed"
		case types.FeedTypeSequences:
			strTyp = "sequences feed"
		case types.FeedTypeSeries:
			strTyp = "series"
		}

		hErr := handler.Handle(feed, err)
		if hErr != nil {
			l.Error(fmt.Sprintf("Failed to handle error while parsing %s %s: %v", strTyp, feed.Url, err))
			return &handlerError{hErr}
		} else {
			l.Error(fmt.Sprintf("Ignore error while parsing %s %s: %v", strTyp, feed.Url, err))
		}
	}

	return nil
}

type Flibusta struct {
	Client *http.Client
	Logger *slog.Logger
}

func (f *Flibusta) Resume(feed types.ResumableFeed, consumer Consumer, handler ErrorHandler) error {
	var err error

	switch feed.Type {
	case types.FeedTypeAuthors:
		f.Logger.Debug("Begin resuming authors feed " + feed.Url.Path)

		err = (&flibustaAuthors{
			client:   f.Client,
			logger:   f.Logger,
			feed:     feed.Url,
			consumer: consumer,
			handler:  handler,
		}).crawl()

	case types.FeedTypeAuthor:
		f.Logger.Debug("Begin resuming author " + feed.Url.Path)

		err = (&flibustaAuthors{
			client:   f.Client,
			logger:   f.Logger,
			feed:     feed.Url,
			consumer: consumer,
			handler:  handler,
		}).author(feed.Url, feed.Author)

	case types.FeedTypeBooks:
		f.Logger.Debug("Begin resuming books feed " + feed.Url.Path)

		err = (&flibustaBooks{
			client:   f.Client,
			logger:   f.Logger,
			author:   feed.Author,
			feed:     feed.Url,
			consumer: consumer,
			handler:  handler,
		}).crawl()

	case types.FeedTypeSequences:
		f.Logger.Debug("Begin resuming sequences feed " + feed.Url.Path)

		err = (&flibustaSeries{
			client:   f.Client,
			logger:   f.Logger,
			feed:     feed.Url,
			consumer: consumer,
			handler:  handler,
		}).crawl()

	case types.FeedTypeSeries:
		f.Logger.Debug("Begin resuming series " + feed.Url.Path)

		err = (&flibustaSeries{
			client:   f.Client,
			logger:   f.Logger,
			feed:     feed.Url,
			consumer: consumer,
			handler:  handler,
		}).sequence(feed.Url, feed.Series)

	default:
		return fmt.Errorf("unknown feed type: %v", feed.Type)
	}

	return consumeError(err, feed, handler, f.Logger)
}

func (f *Flibusta) Crawl(authorsFeed *url.URL, seriesFeed *url.URL, consumer Consumer, handler ErrorHandler) error {
	err := consumeError(
		(&flibustaAuthors{
			client:   f.Client,
			logger:   f.Logger,
			feed:     authorsFeed,
			consumer: consumer,
			handler:  handler,
		}).crawl(),
		types.MakeResumableAuthors(authorsFeed),
		handler, f.Logger,
	)
	if err != nil {
		return err
	}

	return consumeError(
		(&flibustaSeries{
			client:   f.Client,
			logger:   f.Logger,
			feed:     seriesFeed,
			consumer: consumer,
			handler:  handler,
		}).crawl(),
		types.MakeResumableSequences(seriesFeed),
		handler, f.Logger,
	)
}

type flibustaAuthors struct {
	client   *http.Client
	logger   *slog.Logger
	feed     *url.URL
	consumer Consumer
	handler  ErrorHandler
}

func (f *flibustaAuthors) crawl() error {
	f.logger.Debug("Begin processing authors feed " + f.feed.Path)

	var feed opds1.Feed
	if err := fetchAndUnmarshal(f.feed, &feed, "authors feed", f.client, f.logger); err != nil {
		return err
	}

	l := f.logger.With(slog.String("feed", f.feed.Path))

	for _, entry := range feed.Entries {
		entry.ID = strings.TrimSpace(entry.ID)

		if regTagAuthors.MatchString(entry.ID) {
			l.Debug("Found nested feed " + entry.ID)

			link := chooseLink(&entry, func(link *opds1.Link) string {
				if link.TypeLink != linkTypeCatalog {
					return "unknown type: " + link.TypeLink
				}

				return ""
			}, clLogger{logger: l.With(slog.String("id", entry.ID)), levelSkipLink: slog.LevelWarn})

			if link == nil {
				l.Warn("Failed to choose link for nested feed " + entry.ID)
				continue
			}

			linkUrl, err := url.Parse(link.Href)
			if err != nil {
				l.Error("Failed to parse link to nested feed " + entry.ID + ": " + err.Error())
				continue
			}

			linkUrl = f.feed.ResolveReference(linkUrl)

			err = consumeError(
				f.withFeed(linkUrl).crawl(),
				types.MakeResumableAuthors(linkUrl),
				f.handler, l,
			)
			if err != nil {
				return err
			}
		} else if regTagAuthor.MatchString(entry.ID) {
			l.Debug("Found author description " + entry.ID)

			author := &types.Author{
				Id:   entry.ID,
				Name: strings.TrimSpace(entry.Title),
			}

			link := chooseLink(&entry, func(link *opds1.Link) string {
				if link.TypeLink != linkTypeCatalog {
					return "unknown type: " + link.TypeLink
				}

				if !regHrefAuthor.MatchString(link.Href) {
					return "invalid href: " + link.Href
				}

				return ""
			}, clLogger{logger: l.With(slog.String("entry", entry.ID))})

			if link == nil {
				l.Warn("Failed to choose link for author description " + entry.ID)
				continue
			}

			linkUrl, err := url.Parse(link.Href)
			if err != nil {
				l.Error("Failed to parse link to author description " + entry.ID + ": " + err.Error())
				continue
			}

			linkUrl = f.feed.ResolveReference(linkUrl)

			err = consumeError(
				f.author(linkUrl, author),
				types.MakeResumableAuthor(linkUrl, author),
				f.handler, l,
			)
			if err != nil {
				return err
			}
		} else {
			l.Warn("Found unknown entry " + entry.ID)
		}
	}

	urlNextPage, err := getNext(&feed, l)
	if err != nil {
		return err
	}
	if urlNextPage != nil {
		urlNextPage = f.feed.ResolveReference(urlNextPage)
		return consumeError(
			f.withFeed(urlNextPage).crawl(),
			types.MakeResumableAuthors(urlNextPage),
			f.handler, l,
		)
	}

	return nil
}

func (f *flibustaAuthors) withFeed(feed *url.URL) *flibustaAuthors {
	return &flibustaAuthors{
		client:   f.client,
		logger:   f.logger,
		feed:     feed,
		consumer: f.consumer,
		handler:  f.handler,
	}
}

func (f *flibustaAuthors) author(authorUrl *url.URL, author *types.Author) error {
	f.logger.Debug("Begin processing author " + author.Id + " (" + author.Name + ", " + authorUrl.Path + ")")

	booksLink, err := f.fillInfo(authorUrl, author)
	if err != nil {
		return err
	}

	l := f.logger.With(slog.String("author", author.Id))

	if booksLink == nil {
		l.Warn("Failed to find link to books")
		return nil
	}

	err = f.consumer.ConsumeAuthor(author)
	if err != nil {
		return &consumerError{fmt.Errorf("failed to consume author: %w", err)}
	}

	booksLink = authorUrl.ResolveReference(booksLink)

	return consumeError(
		(&flibustaBooks{
			client:   f.client,
			logger:   l,
			author:   author,
			feed:     booksLink,
			consumer: f.consumer,
			handler:  f.handler,
		}).crawl(),
		types.MakeResumableBooks(booksLink, author),
		f.handler, l,
	)
}

func (f *flibustaAuthors) fillInfo(authorUrl *url.URL, author *types.Author) (*url.URL, error) {
	var feed opds1.Feed
	if err := fetchAndUnmarshal(authorUrl, &feed, "author description", f.client, f.logger); err != nil {
		return nil, err
	}

	l := f.logger.With(slog.String("author", author.Id))

	if author.Name == "" {
		feed.Title = strings.TrimSpace(feed.Title)

		s := regTitleAuthorBooks.FindStringSubmatch(feed.Title)
		if len(s) == 0 {
			f.logger.Warn("Failed to find author name from feed title " + authorUrl.Path + ": " + feed.Title)
		} else {
			author.Name = s[1]
		}
	}

	foundBio := false
	var booksLink *url.URL

	for _, entry := range feed.Entries {
		entry.ID = strings.TrimSpace(entry.ID)

		if regTagBio.MatchString(entry.ID) {
			l.Debug("Found author description " + entry.ID)
			foundBio = true

			author.Bio = entry.Content.Content

			link := chooseLink(&entry, func(link *opds1.Link) string {
				if link.Rel != linkRelImage {
					return "unknown rel: " + link.Rel
				}

				if !regLinkTypeImage.MatchString(link.TypeLink) {
					return "unknown type: " + link.TypeLink
				}

				return ""
			}, clLogger{logger: l.With(slog.String("entry", entry.ID))})

			if link == nil {
				l.Info("Not found avatar link")
			} else {
				linkUrl, err := url.Parse(link.Href)
				if err != nil {
					l.Error("Failed to parse link to author description " + entry.ID + ": " + err.Error())
					continue
				}

				author.Avatar = authorUrl.ResolveReference(linkUrl).String()
			}
		} else if regTagAuthorBooks.MatchString(entry.ID) {
			if booksLink != nil {
				l.Warn("Found duplicate author books feed " + entry.ID)
				continue
			}

			l.Debug("Found author books feed " + entry.ID)

			link := chooseLink(&entry, func(link *opds1.Link) string {
				if link.TypeLink != linkTypeCatalog {
					return "unknown type: " + link.TypeLink
				}

				return ""
			}, clLogger{logger: l.With(slog.String("entry", entry.ID)), levelSkipLink: slog.LevelWarn})

			if link == nil {
				l.Warn("Failed to choose link for author books " + entry.ID)
				continue
			}

			linkUrl, err := url.Parse(link.Href)
			if err != nil {
				l.Error("Failed to parse link to author books " + entry.ID + ": " + err.Error())
				continue
			}

			booksLink = linkUrl
		} // Number of other entries expected, like books by series and other, so do not report unknown entries
	}

	if !foundBio {
		l.Info("Not found bio")
	}

	return booksLink, nil
}

type flibustaBooks struct {
	client   *http.Client
	logger   *slog.Logger
	author   *types.Author
	feed     *url.URL
	consumer Consumer
	handler  ErrorHandler
}

func (f *flibustaBooks) crawl() error {
	f.logger.Debug("Begin processing books feed " + f.feed.Path)

	var feed opds1.Feed
	if err := fetchAndUnmarshal(f.feed, &feed, "books feed", f.client, f.logger); err != nil {
		return err
	}

	l := f.logger.With(slog.String("feed", f.feed.Path))

	var bks []*types.Book
	seenBooks := make(map[string]struct{}, len(feed.Entries))

	for _, entry := range feed.Entries {
		entry.ID = strings.TrimSpace(entry.ID)

		if regTagBook.MatchString(entry.ID) {
			l.Debug("Found book " + entry.ID)

			if _, ok := seenBooks[entry.ID]; ok {
				l.Warn("Found duplicate of book " + entry.ID)
				continue
			}

			seenBooks[entry.ID] = struct{}{}

			bks = append(bks, parseBook(&entry, f.feed, l))
		} else {
			l.Warn("Found unknown entry " + entry.ID)
		}
	}

	if len(bks) == 0 {
		l.Warn("No books parsed from feed")
	} else {
		ar := authorResolver{
			author: f.author,
			l:      l,
			client: f.client,
			feed:   f.feed,
		}
		err := f.consumer.ConsumeBooks(bks, ar.resolve)
		if err != nil {
			return &consumerError{fmt.Errorf("failed to consume books: %w", err)}
		}
	}

	urlNextPage, err := getNext(&feed, l)
	if err != nil {
		return err
	}
	if urlNextPage != nil {
		urlNextPage = f.feed.ResolveReference(urlNextPage)
		return consumeError(
			f.withFeed(urlNextPage).crawl(),
			types.MakeResumableBooks(urlNextPage, f.author),
			f.handler, l,
		)
	}

	return nil
}

func (f *flibustaBooks) withFeed(feed *url.URL) *flibustaBooks {
	return &flibustaBooks{
		client:   f.client,
		logger:   f.logger,
		author:   f.author,
		feed:     feed,
		consumer: f.consumer,
		handler:  f.handler,
	}
}

type flibustaSeries struct {
	client   *http.Client
	logger   *slog.Logger
	feed     *url.URL
	consumer Consumer
	handler  ErrorHandler
}

func (f *flibustaSeries) crawl() error {
	f.logger.Debug("Begin processing series feed " + f.feed.Path)

	var feed opds1.Feed
	if err := fetchAndUnmarshal(f.feed, &feed, "series feed", f.client, f.logger); err != nil {
		return err
	}

	l := f.logger.With(slog.String("feed", f.feed.Path))

	for _, entry := range feed.Entries {
		entry.ID = strings.TrimSpace(entry.ID)

		if regTagSeries.MatchString(entry.ID) {
			l.Debug("Found nested feed " + entry.ID)

			link := chooseLink(&entry, func(link *opds1.Link) string {
				if link.TypeLink != linkTypeCatalog {
					return "unknown type: " + link.TypeLink
				}

				return ""
			}, clLogger{logger: l.With(slog.String("id", entry.ID)), levelSkipLink: slog.LevelWarn})

			if link == nil {
				l.Warn("Failed to choose link for nested feed " + entry.ID)
				continue
			}

			linkUrl, err := url.Parse(link.Href)
			if err != nil {
				l.Error("Failed to parse link to nested feed " + entry.ID + ": " + err.Error())
				continue
			}

			linkUrl = f.feed.ResolveReference(linkUrl)

			err = consumeError(
				f.withFeed(linkUrl).crawl(),
				types.MakeResumableSequences(linkUrl),
				f.handler, l,
			)
			if err != nil {
				return err
			}
		} else if regTagSequence.MatchString(entry.ID) {
			l.Debug("Found series description " + entry.ID)

			series := &types.Series{
				Id:    entry.ID,
				Title: strings.TrimSpace(entry.Title),
			}

			link := chooseLink(&entry, func(link *opds1.Link) string {
				if link.TypeLink != linkTypeCatalog {
					return "unknown type: " + link.TypeLink
				}

				if !regHrefSequence.MatchString(link.Href) {
					return "invalid href: " + link.Href
				}

				return ""
			}, clLogger{logger: l.With(slog.String("entry", entry.ID))})

			if link == nil {
				l.Warn("Failed to choose link for series description " + entry.ID)
				continue
			}

			linkUrl, err := url.Parse(link.Href)
			if err != nil {
				l.Error("Failed to parse link to series description " + entry.ID + ": " + err.Error())
				continue
			}

			linkUrl = f.feed.ResolveReference(linkUrl)

			err = consumeError(
				f.sequence(linkUrl, series),
				types.MakeResumableSeries(linkUrl, series),
				f.handler, l,
			)
			if err != nil {
				return err
			}
		} else {
			l.Warn("Found unknown entry " + entry.ID)
		}
	}

	urlNextPage, err := getNext(&feed, l)
	if err != nil {
		return err
	}
	if urlNextPage != nil {
		urlNextPage = f.feed.ResolveReference(urlNextPage)
		return consumeError(
			f.withFeed(urlNextPage).crawl(),
			types.MakeResumableSequences(urlNextPage),
			f.handler, l,
		)
	}

	return nil
}

func (f *flibustaSeries) withFeed(feed *url.URL) *flibustaSeries {
	return &flibustaSeries{
		client:   f.client,
		logger:   f.logger,
		feed:     feed,
		consumer: f.consumer,
		handler:  f.handler,
	}
}

func (f *flibustaSeries) sequence(seriesUrl *url.URL, series *types.Series) error {
	f.logger.Debug("Begin processing series " + series.Id + " (" + series.Title + ", " + seriesUrl.Path + ")")

	var feed opds1.Feed
	if err := fetchAndUnmarshal(seriesUrl, &feed, "series description", f.client, f.logger); err != nil {
		return err
	}

	l := f.logger.With(slog.String("series", series.Id))

	var bks []*types.Book
	seenBookIds := make(map[string]struct{}, len(feed.Entries))

	for _, entry := range feed.Entries {
		entry.ID = strings.TrimSpace(entry.ID)

		if regTagBook.MatchString(entry.ID) {
			if _, ok := seenBookIds[entry.ID]; ok {
				l.Warn("Found duplicate of book " + entry.ID)
				continue
			}

			seenBookIds[entry.ID] = struct{}{}

			bks = append(bks, parseBook(&entry, seriesUrl, l))
		} else {
			l.Warn("Found unknown entry " + entry.ID)
		}
	}

	if len(bks) == 0 {
		l.Warn("Empty series " + series.Id + " (" + series.Title + ")")
		return nil
	}

	ar := authorResolver{
		l:      l,
		client: f.client,
		feed:   seriesUrl,
	}

	err := f.consumer.ConsumeSeries(series, bks, ar.resolve)
	if err != nil {
		return &consumerError{fmt.Errorf("failed to consume series: %w", err)}
	}

	return nil
}

type unresumableError interface {
	isUnresumable()
}

type consumerError struct {
	error
}

func (e *consumerError) Error() string {
	return e.error.Error()
}

func (e *consumerError) Format(f fmt.State, verb rune) {
	if er, ok := e.error.(fmt.Formatter); ok {
		er.Format(f, verb)
		return
	}

	_, _ = fmt.Fprintf(f, fmt.FormatString(f, verb), e.error)
}

func (e *consumerError) isUnresumable() {}

type handlerError struct {
	error
}

func (e *handlerError) Error() string {
	return e.error.Error()
}

func (e *handlerError) Format(f fmt.State, verb rune) {
	if er, ok := e.error.(fmt.Formatter); ok {
		er.Format(f, verb)
		return
	}

	_, _ = fmt.Fprintf(f, fmt.FormatString(f, verb), e.error)
}

func (e *handlerError) isUnresumable() {}

type clLogger struct {
	logger        *slog.Logger
	levelSkipLink slog.Leveler
}

func chooseLink(e *opds1.Entry, matcher func(link *opds1.Link) string, l clLogger) *opds1.Link {
	var ret *opds1.Link

	for _, link := range e.Links {
		link.Rel = strings.TrimSpace(link.Rel)
		link.TypeLink = strings.TrimSpace(link.TypeLink)

		if matcher != nil {
			mismatch := matcher(&link)
			if mismatch != "" {
				if l.levelSkipLink != nil {
					l.logger.LogAttrs(nil, l.levelSkipLink.Level(), "Skip non-matching link: "+mismatch)
				}

				continue
			}
		}

		if ret != nil {
			l.logger.Warn("Skip duplicate matching link: " + link.Href)
			continue
		}

		ret = &link
	}

	return ret
}

type authorResolver struct {
	author *types.Author
	l      *slog.Logger
	client *http.Client
	feed   *url.URL
}

func (ar *authorResolver) resolve(id string) (*types.Author, error) {
	if ar.author != nil && id == ar.author.Id {
		return ar.author, nil
	}

	s := regTagAuthor.FindStringSubmatch(id)
	if len(s) == 0 {
		ar.l.Error("Failed to parse author from id " + id)
		return nil, fmt.Errorf("could not parse author id in %s", id)
	}

	authorUrl, _ := url.Parse(fmt.Sprintf(authorHrefTemplate, s[1]))

	author := &types.Author{Id: id}

	ar.l.Debug("Begin fetching author " + author.Id + " (" + authorUrl.Path + ") by consumer request")

	_, err := (&flibustaAuthors{
		client:   ar.client,
		logger:   ar.l,
		feed:     ar.feed.ResolveReference(authorUrl),
		consumer: nil,
	}).fillInfo(ar.feed.ResolveReference(authorUrl), author)

	if err != nil {
		return nil, fmt.Errorf("fetching author: %w", err)
	}

	return author, nil
}

func getNext(feed *opds1.Feed, l *slog.Logger) (*url.URL, error) {
	linkNxtPage := chooseLink(&opds1.Entry{Links: feed.Links}, func(link *opds1.Link) string {
		if link.Rel != linkRelNext {
			return "unknown rel " + link.Rel
		}

		if link.TypeLink != linkTypeCatalog {
			return "unknown type: " + link.TypeLink
		}

		return ""
	}, clLogger{logger: l})

	if linkNxtPage == nil {
		return nil, nil
	}

	l.Debug("Found link to the next page")

	urlNextPage, err := url.Parse(linkNxtPage.Href)
	if err != nil {
		l.Error("Failed to parse next page link " + linkNxtPage.Href + ": " + err.Error())
		return nil, fmt.Errorf("parsing next page link: %w", err)
	}

	return urlNextPage, nil
}

func parseBook(entry *opds1.Entry, feedUrl *url.URL, l *slog.Logger) *types.Book {
	var year uint16
	entry.Issued = strings.TrimSpace(entry.Issued)
	if entry.Issued != "" {
		y, err := strconv.ParseUint(entry.Issued, 10, 16)
		if err == nil {
			year = uint16(y)
		} else {
			l.Warn("Failed to parse book " + entry.ID + " year :" + err.Error())
		}
	}

	genres := make([]string, 0, len(entry.Category))
	seenGenres := make(map[string]struct{}, len(entry.Category))
	for _, cat := range entry.Category {
		cat.Term = strings.TrimSpace(cat.Term)

		if _, ok := seenGenres[strings.ToLower(cat.Term)]; ok {
			l.Warn("In the same book found duplicate of genre " + cat.Term)
			continue
		}

		seenGenres[strings.ToLower(cat.Term)] = struct{}{}

		genres = append(genres, cat.Term)
	}
	sort.Strings(genres)

	authors := make([]string, 0, len(entry.Author))
	seenAuthors := make(map[string]struct{}, len(entry.Author))
	for _, auth := range entry.Author {
		s := regHrefAuthorAlt.FindStringSubmatch(auth.URI)
		if len(s) == 0 {
			l.Error("Failed to parse author " + entry.ID + " from URI: " + auth.URI)
			continue
		}

		authorId := fmt.Sprintf(authorIdTemplate, s[1])

		if _, ok := seenAuthors[authorId]; ok {
			l.Warn("In the same book found duplicate of author " + authorId)
			continue
		}

		seenAuthors[authorId] = struct{}{}

		authors = append(authors, authorId)
	}

	coverLink := chooseLink(entry, func(link *opds1.Link) string {
		if link.Rel != linkRelImage {
			return "unknown rel: " + link.Rel
		}

		if !regLinkTypeImage.MatchString(link.TypeLink) {
			return "unknown type: " + link.TypeLink
		}

		return ""
	}, clLogger{logger: l.With(slog.String("entry", entry.ID))})

	var cover *url.URL

	if coverLink == nil {
		l.Info("Not found book cover link " + entry.ID)
	} else {
		coverUrl, err := url.Parse(coverLink.Href)
		if err == nil {
			cover = feedUrl.ResolveReference(coverUrl)
		} else {
			l.Error("Failed to parse cover link " + entry.ID + ": " + err.Error())
		}
	}

	cs := ""
	if cover != nil {
		cs = cover.String()
	}

	return &types.Book{
		Id:       entry.ID,
		Title:    strings.TrimSpace(entry.Title),
		Authors:  authors,
		Genres:   genres,
		Language: strings.TrimSpace(entry.Language),
		Year:     year,
		About:    entry.Content.Content,
		Cover:    cs,
	}
}

// Inspect each rune for being a disallowed character.
// Fucking litres sometimes include those characters
func removeDisallowedCodepoints(bs []byte, l *slog.Logger) []byte {
	ret := make([]byte, 0, len(bs))
	buf := bs

	for len(buf) > 0 {
		r, size := utf8.DecodeRune(buf)
		if r == utf8.RuneError && size == 1 {
			l.Error("Going to fail XML parsing because the bytes do not represent valid UTF8")
			// invalid UTF-8, hope it doesn't come to this
			return bs
		}

		if isInCharacterRange(r) {
			ret = append(ret, buf[:size]...)
		} else {
			l.Warn("Removed invalid rune from XML")
		}

		buf = buf[size:]
	}

	return ret
}

// Decide whether the given rune is in the XML Character Range, per
// the Char production of https://www.xml.com/axml/testaxml.htm,
// Section 2.2 Characters.
//
// Stolen from /usr/local/go/src/encoding/xml/xml.go
func isInCharacterRange(r rune) (inrange bool) {
	return r == 0x09 ||
		r == 0x0A ||
		r == 0x0D ||
		r >= 0x20 && r <= 0xD7FF ||
		r >= 0xE000 && r <= 0xFFFD ||
		r >= 0x10000 && r <= 0x10FFFF
}

func fetchAndUnmarshal(url *url.URL, v any, resourceType string, h *http.Client, l *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := h.Do((&http.Request{
		Method: http.MethodGet,
		URL:    url,
	}).WithContext(ctx))

	if err != nil {
		l.Error("Failed to fetch " + resourceType + " " + url.Path + ": " + err.Error())
		return fmt.Errorf("fetching "+resourceType+": %w", err)
	}

	var bs []byte
	func() {
		defer res.Body.Close()
		bs, err = io.ReadAll(res.Body)
	}()

	if err != nil {
		l.Error("Failed to read body of " + resourceType + " " + url.Path + ": " + err.Error())
		return fmt.Errorf("fetching "+resourceType+" (reading response): %w", err)
	}

	err = xml.Unmarshal(removeDisallowedCodepoints(bs, l.With(slog.String("feed", url.Path))), v)

	if err != nil {
		l.Error("Failed to unmarshal " + resourceType + " " + url.Path + ": " + err.Error())
		return fmt.Errorf("unmarshalling "+resourceType+": %w", err)
	}

	return nil
}
