package types

type Author struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	Bio    string `json:"bio,omitempty"`
	Avatar string `json:"avatar_url,omitempty"`
}

type Series struct {
	Id    string `json:"id"`
	Title string `json:"title"`
}

type InSeries struct {
	Id    string `json:"id"`
	Order uint16 `json:"order"`
}

type Book struct {
	Id    string `json:"id"`
	Title string `json:"title"`
	// Must be unique and sorted by (unspecified priority in the source)
	Authors []string   `json:"author_ids"`
	Series  []InSeries `json:"series"`
	// Must be unique and sorted by alphabet
	Genres   []string `json:"genres"`
	Language string   `json:"language"`
	Year     uint16   `json:"year"`
	About    string   `json:"about,omitempty"`
	Cover    string   `json:"cover_url,omitempty"`
}
