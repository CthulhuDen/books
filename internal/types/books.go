package types

type Author struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	Bio    string `json:"bio"`
	Avatar string `json:"avatar_url"`
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
	Id       string     `json:"id"`
	Title    string     `json:"title"`
	Authors  []string   `json:"author_ids"`
	Series   []InSeries `json:"series"`
	Genres   []string   `json:"genres"`
	Language string     `json:"language"`
	Year     uint16     `json:"year"`
	About    string     `json:"about"`
	Cover    string     `json:"cover_url"`
}
