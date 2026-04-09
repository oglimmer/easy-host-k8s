package store

import (
	"database/sql"
	"time"

	"github.com/oglimmer/easy-host/internal/model"
)

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateContent(c *model.Content) error {
	now := time.Now()
	c.CreatedAt = now
	c.UpdatedAt = now
	res, err := s.db.Exec(
		`INSERT INTO content (slug, owner, title, source_url, creator, allow_external_resources, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Slug, c.Owner, c.Title, c.SourceURL, c.Creator, c.AllowExternalResources, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdateContent(c *model.Content) error {
	c.UpdatedAt = time.Now()
	_, err := s.db.Exec(
		`UPDATE content SET title=?, source_url=?, creator=?, allow_external_resources=?, updated_at=? WHERE id=?`,
		c.Title, c.SourceURL, c.Creator, c.AllowExternalResources, c.UpdatedAt, c.ID,
	)
	return err
}

func (s *Store) DeleteContent(id int64) error {
	_, err := s.db.Exec(`DELETE FROM content WHERE id=?`, id)
	return err
}

func (s *Store) GetContentBySlug(slug string) (*model.Content, error) {
	c := &model.Content{}
	err := s.db.QueryRow(
		`SELECT id, slug, owner, COALESCE(title,''), COALESCE(source_url,''), creator, allow_external_resources, created_at, updated_at FROM content WHERE slug=?`, slug,
	).Scan(&c.ID, &c.Slug, &c.Owner, &c.Title, &c.SourceURL, &c.Creator, &c.AllowExternalResources, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ListContentByOwner(owner string, limit, offset int) ([]model.Content, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM content WHERE owner=?`, owner).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(
		`SELECT id, slug, owner, COALESCE(title,''), COALESCE(source_url,''), creator, allow_external_resources, created_at, updated_at FROM content WHERE owner=? ORDER BY updated_at DESC LIMIT ? OFFSET ?`, owner, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var result []model.Content
	for rows.Next() {
		var c model.Content
		if err := rows.Scan(&c.ID, &c.Slug, &c.Owner, &c.Title, &c.SourceURL, &c.Creator, &c.AllowExternalResources, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, err
		}
		result = append(result, c)
	}
	return result, total, rows.Err()
}

func (s *Store) SlugExists(slug string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM content WHERE slug=?)`, slug).Scan(&exists)
	return exists, err
}

func (s *Store) CreateContentFile(f *model.ContentFile) error {
	_, err := s.db.Exec(
		`INSERT INTO content_file (content_id, file_path, file_data, content_type) VALUES (?, ?, ?, ?)`,
		f.ContentID, f.FilePath, f.FileData, f.ContentType,
	)
	return err
}

func (s *Store) DeleteContentFiles(contentID int64) error {
	_, err := s.db.Exec(`DELETE FROM content_file WHERE content_id=?`, contentID)
	return err
}

func (s *Store) GetContentFile(slug, filePath string) (*model.ContentFile, error) {
	f := &model.ContentFile{}
	err := s.db.QueryRow(
		`SELECT cf.id, cf.content_id, cf.file_path, cf.file_data, cf.content_type, c.allow_external_resources
		 FROM content_file cf JOIN content c ON cf.content_id = c.id
		 WHERE c.slug=? AND cf.file_path=?`, slug, filePath,
	).Scan(&f.ID, &f.ContentID, &f.FilePath, &f.FileData, &f.ContentType, &f.AllowExternalResources)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Store) ListFilePaths(contentID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT file_path FROM content_file WHERE content_id=?`, contentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}
