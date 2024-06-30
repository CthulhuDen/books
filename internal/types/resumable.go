package types

import "net/url"

type FeedType uint8

const (
	FeedTypeAuthors   FeedType = 1
	FeedTypeAuthor    FeedType = 2
	FeedTypeBooks     FeedType = 3
	FeedTypeSequences FeedType = 4
	FeedTypeSeries    FeedType = 5
)

// ResumableFeed represents a feed that can be resumed from a specific point.
// It contains information about the feed URL, type, and associated entities.
//
// To create a ResumableFeed, use one of the following functions:
//   - MakeResumableAuthors
//   - MakeResumableAuthor
//   - MakeResumableBooks
//   - MakeResumableSequences
//   - MakeResumableSeries
//
// Direct construction of ResumableFeed is discouraged.
type ResumableFeed struct {
	Url    *url.URL
	Type   FeedType
	Author *Author // required for Type == FeedTypeAuthor or Type == FeedTypeBooks
	Series *Series // required for Type == FeedTypeSeries
}

func MakeResumableAuthors(u *url.URL) ResumableFeed {
	return ResumableFeed{Url: u, Type: FeedTypeAuthors}
}

func MakeResumableAuthor(u *url.URL, author *Author) ResumableFeed {
	return ResumableFeed{Url: u, Type: FeedTypeAuthor, Author: author}
}

func MakeResumableBooks(u *url.URL, author *Author) ResumableFeed {
	return ResumableFeed{Url: u, Type: FeedTypeBooks, Author: author}
}

func MakeResumableSequences(u *url.URL) ResumableFeed {
	return ResumableFeed{Url: u, Type: FeedTypeSequences}
}

func MakeResumableSeries(u *url.URL, series *Series) ResumableFeed {
	return ResumableFeed{Url: u, Type: FeedTypeSeries, Series: series}
}
