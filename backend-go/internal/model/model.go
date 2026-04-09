package model

import "time"

type Content struct {
	ID                     int64     `json:"id"`
	Slug                   string    `json:"slug"`
	Owner                  string    `json:"owner"`
	Title                  string    `json:"title"`
	SourceURL              string    `json:"sourceUrl"`
	Creator                string    `json:"creator"`
	AllowExternalResources bool      `json:"allowExternalResources"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

type ContentFile struct {
	ID                     int64  `json:"id"`
	ContentID              int64  `json:"-"`
	FilePath               string `json:"filePath"`
	FileData               []byte `json:"-"`
	ContentType            string `json:"contentType"`
	AllowExternalResources bool   `json:"-"`
}

type ContentResponse struct {
	ID                     int64     `json:"id"`
	Slug                   string    `json:"slug"`
	Title                  string    `json:"title"`
	SourceURL              string    `json:"sourceUrl,omitempty"`
	Owner                  string    `json:"owner"`
	Creator                string    `json:"creator"`
	AllowExternalResources bool      `json:"allowExternalResources"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
	Files                  []string  `json:"files"`
}

func (c *Content) ToResponse(files []string) ContentResponse {
	return ContentResponse{
		ID:                     c.ID,
		Slug:                   c.Slug,
		Title:                  c.Title,
		SourceURL:              c.SourceURL,
		Owner:                  c.Owner,
		Creator:                c.Creator,
		AllowExternalResources: c.AllowExternalResources,
		CreatedAt:              c.CreatedAt,
		UpdatedAt:              c.UpdatedAt,
		Files:                  files,
	}
}
