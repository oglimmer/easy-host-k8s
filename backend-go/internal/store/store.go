package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/oglimmer/easy-host/internal/crypto"
	"github.com/oglimmer/easy-host/internal/model"
)

// contentColumns is the projection every content read shares. The encryption
// columns are NULL for unencrypted entries.
const contentColumns = `id, slug, owner, COALESCE(title,''), COALESCE(source_url,''), creator, allow_external_resources,
	enc_kdf, enc_salt, enc_wrapped_key, created_at, updated_at`

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
	kdf, salt, wrapped := encColumns(c.Encryption)
	res, err := s.db.Exec(
		`INSERT INTO content (slug, owner, title, source_url, creator, allow_external_resources, enc_kdf, enc_salt, enc_wrapped_key, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Slug, c.Owner, c.Title, c.SourceURL, c.Creator, c.AllowExternalResources, kdf, salt, wrapped, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

// UpdateContent persists the editable metadata. Encryption state is changed only
// through ApplyEncryption, which keeps the envelope and the file payloads in
// step.
func (s *Store) UpdateContent(c *model.Content) error {
	c.UpdatedAt = time.Now()
	_, err := s.db.Exec(
		`UPDATE content SET title=?, source_url=?, creator=?, allow_external_resources=?, updated_at=? WHERE id=?`,
		c.Title, c.SourceURL, c.Creator, c.AllowExternalResources, c.UpdatedAt, c.ID,
	)
	return err
}

// ApplyEncryption swaps a content entry's encryption envelope and the payloads
// of the given files in a single transaction: a partial change would either
// orphan ciphertexts (envelope lost) or leave plaintext claiming to be
// encrypted. A nil env marks the entry unencrypted; rewritten maps file ID to
// its new payload and may be empty when only the envelope changes.
func (s *Store) ApplyEncryption(contentID int64, env *crypto.Envelope, rewritten map[int64][]byte) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	kdf, salt, wrapped := encColumns(env)
	if _, err := tx.Exec(
		`UPDATE content SET enc_kdf=?, enc_salt=?, enc_wrapped_key=?, updated_at=? WHERE id=?`,
		kdf, salt, wrapped, time.Now(), contentID,
	); err != nil {
		return err
	}
	for id, data := range rewritten {
		if _, err := tx.Exec(`UPDATE content_file SET file_data=? WHERE id=? AND content_id=?`, data, id, contentID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteContent(id int64) error {
	_, err := s.db.Exec(`DELETE FROM content WHERE id=?`, id)
	return err
}

func (s *Store) GetContentBySlug(slug string) (*model.Content, error) {
	return scanContent(s.db.QueryRow(`SELECT `+contentColumns+` FROM content WHERE slug=?`, slug))
}

func (s *Store) ListContentByOwner(owner string, limit, offset int) ([]model.Content, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM content WHERE owner=?`, owner).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(
		`SELECT `+contentColumns+` FROM content WHERE owner=? ORDER BY updated_at DESC LIMIT ? OFFSET ?`, owner, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var result []model.Content
	for rows.Next() {
		c, err := scanContent(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, *c)
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
		`SELECT cf.id, cf.content_id, cf.file_path, cf.file_data, cf.content_type, c.allow_external_resources, c.enc_kdf IS NOT NULL
		 FROM content_file cf JOIN content c ON cf.content_id = c.id
		 WHERE c.slug=? AND cf.file_path=?`, slug, filePath,
	).Scan(&f.ID, &f.ContentID, &f.FilePath, &f.FileData, &f.ContentType, &f.AllowExternalResources, &f.Encrypted)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// ListContentFiles returns every file of a content entry including its payload.
// Used when re-encrypting or decrypting an entry in place.
func (s *Store) ListContentFiles(contentID int64) ([]model.ContentFile, error) {
	rows, err := s.db.Query(
		`SELECT id, content_id, file_path, file_data, content_type FROM content_file WHERE content_id=?`, contentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []model.ContentFile
	for rows.Next() {
		var f model.ContentFile
		if err := rows.Scan(&f.ID, &f.ContentID, &f.FilePath, &f.FileData, &f.ContentType); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
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

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanContent reads the contentColumns projection, folding the three encryption
// columns back into an Envelope.
func scanContent(row rowScanner) (*model.Content, error) {
	c := &model.Content{}
	var kdf sql.NullString
	var salt, wrapped []byte
	if err := row.Scan(
		&c.ID, &c.Slug, &c.Owner, &c.Title, &c.SourceURL, &c.Creator, &c.AllowExternalResources,
		&kdf, &salt, &wrapped, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if kdf.Valid && kdf.String != "" {
		if len(salt) == 0 || len(wrapped) == 0 {
			return nil, errors.New("content " + c.Slug + " has an incomplete encryption envelope")
		}
		c.Encryption = &crypto.Envelope{KDF: kdf.String, Salt: salt, WrappedKey: wrapped}
	}
	return c, nil
}

// encColumns flattens an Envelope into its three nullable columns.
func encColumns(env *crypto.Envelope) (kdf any, salt, wrapped []byte) {
	if env == nil {
		return nil, nil, nil
	}
	return env.KDF, env.Salt, env.WrappedKey
}
