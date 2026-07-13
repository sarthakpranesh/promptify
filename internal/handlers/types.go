package handlers

import "html/template"

type Prompt struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     int      `json:"version"`
	CreatedAt   string   `json:"created_at"`
	Variables   []string `json:"variables"`
}

type Version struct {
	Version             int           `json:"version"`
	Template            string        `json:"template"`
	HighlightedTemplate template.HTML `json:"-"`
	Variables           []string      `json:"variables"`
	IsActive            bool          `json:"is_active"`
	CreatedAt           string        `json:"created_at"`
}

type PromptDetail struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Variables   []string  `json:"variables"`
	Versions    []Version `json:"versions"`
	Active      Version   `json:"active"`
}

type PromptListResponse struct {
	Prompts []Prompt `json:"prompts"`
}

type CurrentUser struct {
	FullName    string
	Email       string
	DisplayName string
	IsAdmin     bool
}
