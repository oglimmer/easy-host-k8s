package service

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/oglimmer/easy-host/internal/model"
	"github.com/oglimmer/easy-host/internal/store"
)

var (
	ErrNotFound         = errors.New("content not found")
	ErrSlugExists       = errors.New("slug already exists")
	ErrInvalidSlug      = errors.New("invalid slug format")
	ErrInvalidFilePath  = errors.New("invalid file path")
	ErrForbidden        = errors.New("not owner of content")

	slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

type ContentService struct {
	store *store.Store
}

func NewContentService(s *store.Store) *ContentService {
	return &ContentService{store: s}
}

func (svc *ContentService) List(owner string, limit, offset int) ([]model.ContentResponse, int, error) {
	contents, total, err := svc.store.ListContentByOwner(owner, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	var result []model.ContentResponse
	for _, c := range contents {
		files, _ := svc.store.ListFilePaths(c.ID)
		result = append(result, c.ToResponse(files))
	}
	return result, total, nil
}

func (svc *ContentService) Get(slug, owner string) (*model.ContentResponse, error) {
	c, err := svc.store.GetContentBySlug(slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if c.Owner != owner {
		return nil, ErrNotFound
	}
	files, _ := svc.store.ListFilePaths(c.ID)
	resp := c.ToResponse(files)
	return &resp, nil
}

func (svc *ContentService) Create(slug string, fileData []byte, fileName string, owner, title, sourceURL, creator string) (*model.ContentResponse, error) {
	if !slugPattern.MatchString(slug) {
		return nil, ErrInvalidSlug
	}
	exists, err := svc.store.SlugExists(slug)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrSlugExists
	}

	if strings.TrimSpace(title) == "" {
		title = slug
	}
	if strings.TrimSpace(creator) == "" {
		creator = owner
	}
	sourceURL = strings.TrimSpace(sourceURL)

	c := &model.Content{
		Slug:      slug,
		Owner:     owner,
		Title:     title,
		SourceURL: sourceURL,
		Creator:   creator,
	}
	if err := svc.store.CreateContent(c); err != nil {
		return nil, err
	}

	if err := svc.storeFiles(c.ID, fileData, fileName); err != nil {
		svc.store.DeleteContent(c.ID)
		return nil, err
	}

	files, _ := svc.store.ListFilePaths(c.ID)
	resp := c.ToResponse(files)
	return &resp, nil
}

func (svc *ContentService) Update(slug, owner string, fileData []byte, fileName string, title, sourceURL, creator *string) (*model.ContentResponse, error) {
	c, err := svc.store.GetContentBySlug(slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if c.Owner != owner {
		return nil, ErrNotFound
	}

	if title != nil {
		c.Title = strings.TrimSpace(*title)
	}
	if sourceURL != nil {
		c.SourceURL = strings.TrimSpace(*sourceURL)
	}
	if creator != nil {
		c.Creator = strings.TrimSpace(*creator)
	}

	if err := svc.store.UpdateContent(c); err != nil {
		return nil, err
	}

	if fileData != nil && len(fileData) > 0 {
		svc.store.DeleteContentFiles(c.ID)
		if err := svc.storeFiles(c.ID, fileData, fileName); err != nil {
			return nil, err
		}
	}

	files, _ := svc.store.ListFilePaths(c.ID)
	resp := c.ToResponse(files)
	return &resp, nil
}

func (svc *ContentService) Delete(slug, owner string) error {
	c, err := svc.store.GetContentBySlug(slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if c.Owner != owner {
		return ErrNotFound
	}
	return svc.store.DeleteContent(c.ID)
}

func (svc *ContentService) GetFile(slug, filePath string) (*model.ContentFile, error) {
	if strings.Contains(filePath, "..") {
		return nil, ErrInvalidFilePath
	}
	f, err := svc.store.GetContentFile(slug, filePath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

func (svc *ContentService) storeFiles(contentID int64, data []byte, fileName string) error {
	if isZip(fileName) {
		return svc.extractZip(contentID, data)
	}
	return svc.store.CreateContentFile(&model.ContentFile{
		ContentID:   contentID,
		FilePath:    "index.html",
		FileData:    data,
		ContentType: "text/html",
	})
}

func (svc *ContentService) extractZip(contentID int64, data []byte) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)
		if strings.HasPrefix(name, "__MACOSX") || isHidden(name) {
			continue
		}
		name = normalizeFilePath(name)
		if name == "" {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		buf, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}

		if err := svc.store.CreateContentFile(&model.ContentFile{
			ContentID:   contentID,
			FilePath:    name,
			FileData:    buf,
			ContentType: detectContentType(name),
		}); err != nil {
			return err
		}
	}
	return nil
}

func isZip(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".zip")
}

func isHidden(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func normalizeFilePath(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	if strings.HasPrefix(path, "..") || strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) {
		return ""
	}
	return path
}

func detectContentType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	types := map[string]string{
		".html":  "text/html",
		".htm":   "text/html",
		".css":   "text/css",
		".js":    "application/javascript",
		".json":  "application/json",
		".png":   "image/png",
		".jpg":   "image/jpeg",
		".jpeg":  "image/jpeg",
		".gif":   "image/gif",
		".svg":   "image/svg+xml",
		".ico":   "image/x-icon",
		".woff":  "font/woff",
		".woff2": "font/woff2",
		".ttf":   "font/ttf",
	}
	if ct, ok := types[ext]; ok {
		return ct
	}
	return "application/octet-stream"
}
