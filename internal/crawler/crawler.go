package crawler

import (
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
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
	Crawl(authorsFeed *url.URL, consumer Consumer) error
}

type Flibusta struct {
	Client *http.Client
	Logger *slog.Logger
}

func (f *Flibusta) Crawl(authorsFeed *url.URL, seriesFeed *url.URL, consumer Consumer) error {
	err := (&flibustaAuthors{
		client:   f.Client,
		logger:   f.Logger,
		feed:     authorsFeed,
		consumer: consumer,
	}).crawl()
	if err != nil {
		return err
	}

	return (&flibustaSeries{
		client:   f.Client,
		logger:   f.Logger,
		feed:     seriesFeed,
		consumer: consumer,
	}).crawl()
}

type flibustaAuthors struct {
	client   *http.Client
	logger   *slog.Logger
	feed     *url.URL
	consumer Consumer
}

func (f *flibustaAuthors) crawl() error {
	f.logger.Debug("Begin processing authors feed " + f.feed.Path)

	res, err := f.client.Do(&http.Request{
		Method: http.MethodGet,
		URL:    f.feed,
	})

	if err != nil {
		f.logger.Error("Failed to fetch authors feed " + f.feed.Path + ": " + err.Error())
		return fmt.Errorf("fetching authors feed: %w", err)
	}

	var bs []byte
	func() {
		defer res.Body.Close()
		bs, err = io.ReadAll(res.Body)
	}()

	if err != nil {
		f.logger.Error("Failed to read body of authors feed " + f.feed.Path + ": " + err.Error())
		return fmt.Errorf("fetching authors feed (reading response): %w", err)
	}

	l := f.logger.With(slog.String("feed", f.feed.Path))

	var feed opds1.Feed
	err = xml.Unmarshal(removeDisallowedCodepoints(bs, l), &feed)

	if err != nil {
		f.logger.Error("Failed to unmarshal authors feed " + f.feed.Path + ": " + err.Error())
		return fmt.Errorf("unmarshalling authors feed: %w", err)
	}

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

			feedUrl := f.feed.ResolveReference(linkUrl)

			err = f.withFeed(feedUrl).crawl()
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

			err = f.author(f.feed.ResolveReference(linkUrl), author)
			if err != nil {
				return err
			}
		} else {
			l.Warn("Found unknown entry " + entry.ID)
		}
	}

	return nil
}

func (f *flibustaAuthors) withFeed(feed *url.URL) *flibustaAuthors {
	return &flibustaAuthors{
		client:   f.client,
		logger:   f.logger,
		feed:     feed,
		consumer: f.consumer,
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
		l.Error("Failed to find link to books")
		return fmt.Errorf("no link to author books")
	}

	err = f.consumer.ConsumeAuthor(author)
	if err != nil {
		_, ignore := err.(IgnoreError)
		if !ignore {
			return fmt.Errorf("failed to consume author: %w", err)
		}
	}

	return (&flibustaBooks{
		client:   f.client,
		logger:   l,
		author:   author,
		feed:     authorUrl.ResolveReference(booksLink),
		consumer: f.consumer,
	}).crawl()
}

func (f *flibustaAuthors) fillInfo(authorUrl *url.URL, author *types.Author) (*url.URL, error) {
	res, err := f.client.Do(&http.Request{
		Method: http.MethodGet,
		URL:    authorUrl,
	})

	if err != nil {
		f.logger.Error("Failed to fetch author description " + authorUrl.Path + ": " + err.Error())
		return nil, fmt.Errorf("fetching author description: %w", err)
	}

	var bs []byte
	func() {
		defer res.Body.Close()
		bs, err = io.ReadAll(res.Body)
	}()

	if err != nil {
		f.logger.Error("Failed to read body of author description " + authorUrl.Path + ": " + err.Error())
		return nil, fmt.Errorf("fetching author description (reading response): %w", err)
	}

	l := f.logger.With(slog.String("author", author.Id))

	var feed opds1.Feed
	err = xml.Unmarshal(removeDisallowedCodepoints(bs, l), &feed)

	if err != nil {
		f.logger.Error("Failed to unmarshal author description " + authorUrl.Path + ": " + err.Error())
		return nil, fmt.Errorf("unmarshalling author description: %w", err)
	}

	if author.Name == "" {
		feed.Title = strings.TrimSpace(feed.Title)

		s := regTitleAuthorBooks.FindStringSubmatch(feed.Title)
		if len(s) == 0 {
			f.logger.Error("Failed to find author name from feed title " + authorUrl.Path + ": " + feed.Title)
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
}

func (f *flibustaBooks) crawl() error {
	f.logger.Debug("Begin processing books feed " + f.feed.Path)

	res, err := f.client.Do(&http.Request{
		Method: http.MethodGet,
		URL:    f.feed,
	})

	if err != nil {
		f.logger.Error("Failed to fetch books feed " + f.feed.Path + ": " + err.Error())
		return fmt.Errorf("fetching books feed: %w", err)
	}

	var bs []byte
	func() {
		defer res.Body.Close()
		bs, err = io.ReadAll(res.Body)
	}()

	if err != nil {
		f.logger.Error("Failed to read body of books feed " + f.feed.Path + ": " + err.Error())
		return fmt.Errorf("fetching books feed (reading response): %w", err)
	}

	l := f.logger.With(slog.String("feed", f.feed.Path))

	var feed opds1.Feed
	err = xml.Unmarshal(removeDisallowedCodepoints(bs, l), &feed)

	if err != nil {
		f.logger.Error("Failed to unmarshal books feed " + f.feed.Path + ": " + err.Error())
		return fmt.Errorf("unmarshalling books feed: %w", err)
	}

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

			var year uint16
			entry.Issued = strings.TrimSpace(entry.Issued)
			if entry.Issued != "" {
				y, err := strconv.ParseUint(entry.Issued, 10, 16)
				if err == nil {
					year = uint16(y)
				} else {
					l.Error("Failed to parse book " + entry.ID + " year :" + err.Error())
				}
			}

			var genres []string
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

			var authors []string
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

			coverLink := chooseLink(&entry, func(link *opds1.Link) string {
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
					cover = f.feed.ResolveReference(coverUrl)
				} else {
					l.Error("Failed to parse cover link " + entry.ID + ": " + err.Error())
				}
			}

			cs := ""
			if cover != nil {
				cs = cover.String()
			}

			bks = append(bks, &types.Book{
				Id:       entry.ID,
				Title:    strings.TrimSpace(entry.Title),
				Authors:  authors,
				Genres:   genres,
				Language: strings.TrimSpace(entry.Language),
				Year:     year,
				About:    entry.Content.Content,
				Cover:    cs,
			})
		} else {
			l.Warn("Found unknown entry " + entry.ID)
		}
	}

	if len(bks) == 0 {
		l.Error("No books parsed from feed")
	} else {
		err := f.consumer.ConsumeBooks(bks, func(id string) (*types.Author, error) {
			if id == f.author.Id {
				return f.author, nil
			}

			s := regTagAuthor.FindStringSubmatch(id)
			if len(s) == 0 {
				l.Error("Failed to parse author from id " + id)
				return nil, fmt.Errorf("could not parse author id in %s", id)
			}

			authorUrl, _ := url.Parse(fmt.Sprintf(authorHrefTemplate, s[1]))

			author := &types.Author{Id: id}

			l.Debug("Begin fetching author " + author.Id + " (" + authorUrl.Path + ") by consumer request")

			_, err := (&flibustaAuthors{
				client:   f.client,
				logger:   l,
				feed:     f.feed.ResolveReference(authorUrl),
				consumer: nil,
			}).fillInfo(f.feed.ResolveReference(authorUrl), author)

			if err != nil {
				return nil, fmt.Errorf("fetching author: %w", err)
			}

			return author, nil
		})

		if err != nil {
			_, ignore := err.(IgnoreError)
			if !ignore {
				return fmt.Errorf("failed to consume books: %w", err)
			}
		}
	}

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
		return nil
	}

	l.Debug("Found link to the next page")

	urlNextPage, err := url.Parse(linkNxtPage.Href)
	if err != nil {
		l.Error("Failed to parse next page link " + linkNxtPage.Href + ": " + err.Error())
		return nil
	}

	return f.withFeed(f.feed.ResolveReference(urlNextPage)).crawl()
}

func (f *flibustaBooks) withFeed(feed *url.URL) *flibustaBooks {
	return &flibustaBooks{
		client:   f.client,
		logger:   f.logger,
		author:   f.author,
		feed:     feed,
		consumer: f.consumer,
	}
}

type flibustaSeries struct {
	client   *http.Client
	logger   *slog.Logger
	feed     *url.URL
	consumer Consumer
}

func (f *flibustaSeries) crawl() error {
	f.logger.Debug("Begin processing series feed " + f.feed.Path)

	res, err := f.client.Do(&http.Request{
		Method: http.MethodGet,
		URL:    f.feed,
	})

	if err != nil {
		f.logger.Error("Failed to fetch series feed " + f.feed.Path + ": " + err.Error())
		return fmt.Errorf("fetching series feed: %w", err)
	}

	var bs []byte
	func() {
		defer res.Body.Close()
		bs, err = io.ReadAll(res.Body)
	}()

	if err != nil {
		f.logger.Error("Failed to read body of series feed " + f.feed.Path + ": " + err.Error())
		return fmt.Errorf("fetching series feed (reading response): %w", err)
	}

	l := f.logger.With(slog.String("feed", f.feed.Path))

	var feed opds1.Feed
	err = xml.Unmarshal(removeDisallowedCodepoints(bs, l), &feed)

	if err != nil {
		f.logger.Error("Failed to unmarshal series feed " + f.feed.Path + ": " + err.Error())
		return fmt.Errorf("unmarshalling series feed: %w", err)
	}

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

			feedUrl := f.feed.ResolveReference(linkUrl)

			err = f.withFeed(feedUrl).crawl()
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

			err = f.sequence(f.feed.ResolveReference(linkUrl), series)
			if err != nil {
				return err
			}
		} else {
			l.Warn("Found unknown entry " + entry.ID)
		}
	}

	return nil
}

func (f *flibustaSeries) withFeed(feed *url.URL) *flibustaSeries {
	return &flibustaSeries{
		client:   f.client,
		logger:   f.logger,
		feed:     feed,
		consumer: f.consumer,
	}
}

func (f *flibustaSeries) sequence(seriesUrl *url.URL, series *types.Series) error {
	f.logger.Debug("Begin processing series " + series.Id + " (" + series.Title + ", " + seriesUrl.Path + ")")

	res, err := f.client.Do(&http.Request{
		Method: http.MethodGet,
		URL:    seriesUrl,
	})

	if err != nil {
		f.logger.Error("Failed to fetch series description " + seriesUrl.Path + ": " + err.Error())
		return fmt.Errorf("fetching series description: %w", err)
	}

	var bs []byte
	func() {
		defer res.Body.Close()
		bs, err = io.ReadAll(res.Body)
	}()

	if err != nil {
		f.logger.Error("Failed to read body of series description " + seriesUrl.Path + ": " + err.Error())
		return fmt.Errorf("fetching series description (reading response): %w", err)
	}

	l := f.logger.With(slog.String("series", series.Id))

	var feed opds1.Feed
	err = xml.Unmarshal(removeDisallowedCodepoints(bs, l), &feed)

	if err != nil {
		f.logger.Error("Failed to unmarshal series description " + seriesUrl.Path + ": " + err.Error())
		return fmt.Errorf("unmarshalling series description: %w", err)
	}

	var bookIds []string
	seenBookIds := make(map[string]struct{}, len(feed.Entries))

	for _, entry := range feed.Entries {
		entry.ID = strings.TrimSpace(entry.ID)

		if regTagBook.MatchString(entry.ID) {
			if _, ok := seenBookIds[entry.ID]; ok {
				l.Warn("Found duplicate of book " + entry.ID)
				continue
			}

			seenBookIds[entry.ID] = struct{}{}

			bookIds = append(bookIds, entry.ID)
		} else {
			l.Warn("Found unknown entry " + entry.ID)
		}
	}

	if len(bookIds) == 0 {
		l.Error("Empty series " + series.Id + " (" + series.Title + ")")
		return nil
	}

	err = f.consumer.ConsumeSeries(series, bookIds)
	if err != nil {
		_, ignore := err.(IgnoreError)
		if !ignore {
			return fmt.Errorf("failed to consume series: %w", err)
		}
	}

	return nil
}

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

// Inspect each rune for being a disallowed character.
// Fucking litres sometimes include those characters
func removeDisallowedCodepoints(bs []byte, l *slog.Logger) []byte {
	ret := bs[:0]
	buf := bs

	for len(buf) > 0 {
		r, size := utf8.DecodeRune(buf)
		if r == utf8.RuneError && size == 1 {
			l.Warn("Going to fail XML parsing because the bytes do not represent valid UTF8")
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
