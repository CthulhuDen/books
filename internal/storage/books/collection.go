package books

import (
	"encoding/json"

	"books/internal/types"
)

type Grouping struct {
	// Enum: one of the next three fields will be non-empty (if all empty, then the grouping is by empty genres)
	ByAuthor string
	BySeries string
	ByGenres []string
}

func (g Grouping) MarshalJSON() ([]byte, error) {
	if g.ByAuthor != "" {
		return json.Marshal(struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		}{
			Type:  "author",
			Value: g.ByAuthor,
		})
	} else if g.BySeries != "" {
		return json.Marshal(struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		}{
			Type:  "series",
			Value: g.BySeries,
		})
	} else {
		return json.Marshal(struct {
			Type  string   `json:"type"`
			Value []string `json:"value"`
		}{
			Type:  "genres",
			Value: g.ByGenres,
		})
	}
}

type BookInGroup struct {
	Groups []Grouping  `json:"groups"`
	Book   *types.Book `json:"book"`
}
