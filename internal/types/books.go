package types

import (
	"net/url"
)

type Author struct {
	Id     string
	Name   string
	Bio    string
	Avatar *url.URL
}

type Series struct {
	Id    string
	Title string
}

type InSeries struct {
	Id       string
	Position uint16
}

type Book struct {
	Id       string
	Title    string
	Authors  []string
	Series   []InSeries
	Genres   []string
	Language string
	Year     uint16
	About    string
	Cover    *url.URL
}
