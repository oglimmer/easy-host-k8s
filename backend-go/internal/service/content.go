package service

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/oglimmer/easy-host/internal/crypto"
	"github.com/oglimmer/easy-host/internal/model"
	"github.com/oglimmer/easy-host/internal/store"
)

var (
	ErrNotFound        = errors.New("content not found")
	ErrSlugExists      = errors.New("slug already exists")
	ErrInvalidSlug     = errors.New("invalid slug format")
	ErrInvalidFilePath = errors.New("invalid file path")
	ErrForbidden       = errors.New("not owner of content")

	// ErrPassphraseRequired means the content is encrypted and the operation
	// cannot proceed without its passphrase: reading a file, or replacing the
	// files of an encrypted entry.
	ErrPassphraseRequired = errors.New("this content is encrypted: a passphrase is required")
	// ErrWrongPassphrase is returned for a passphrase that does not unlock the
	// content.
	ErrWrongPassphrase = crypto.ErrWrongPassphrase
	// ErrNotEncrypted is returned when an encryption-only operation (rotating or
	// removing a passphrase) is requested for unencrypted content.
	ErrNotEncrypted = errors.New("content is not encrypted")

	slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

// CreateParams describes a new content entry.
type CreateParams struct {
	Slug                   string
	Owner                  string
	Title                  string
	SourceURL              string
	Creator                string
	AllowExternalResources bool

	FileData []byte
	FileName string

	// Passphrase, when set, encrypts the files at rest. Visitors must supply it
	// to view the content, and it cannot be recovered if lost.
	Passphrase string
}

// UpdateParams describes a change to an existing entry. Nil metadata pointers
// are left unchanged; empty FileData leaves the files unchanged.
type UpdateParams struct {
	Slug                   string
	Owner                  string
	Title                  *string
	SourceURL              *string
	Creator                *string
	AllowExternalResources *bool

	FileData []byte
	FileName string

	// Passphrase is the entry's current passphrase when it is already encrypted
	// (required to replace its files, rotate, or remove encryption), or the new
	// passphrase to encrypt a previously unencrypted entry with.
	Passphrase string
	// NewPassphrase rotates an encrypted entry to a different passphrase,
	// re-encrypting it under a fresh key so visitors who already unlocked it
	// lose access immediately. Requires Passphrase.
	NewPassphrase string
	// RemoveEncryption decrypts the entry's files back to plaintext at rest.
	RemoveEncryption bool
}

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
	c, err := svc.ownedContent(slug, owner)
	if err != nil {
		return nil, err
	}
	files, _ := svc.store.ListFilePaths(c.ID)
	resp := c.ToResponse(files)
	return &resp, nil
}

func (svc *ContentService) Create(p CreateParams) (*model.ContentResponse, error) {
	if !slugPattern.MatchString(p.Slug) {
		return nil, ErrInvalidSlug
	}
	exists, err := svc.store.SlugExists(p.Slug)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrSlugExists
	}

	title := p.Title
	if strings.TrimSpace(title) == "" {
		title = p.Slug
	}
	creator := p.Creator
	if strings.TrimSpace(creator) == "" {
		creator = p.Owner
	}

	c := &model.Content{
		Slug:                   p.Slug,
		Owner:                  p.Owner,
		Title:                  title,
		SourceURL:              strings.TrimSpace(p.SourceURL),
		Creator:                creator,
		AllowExternalResources: p.AllowExternalResources,
	}

	// The envelope is persisted with the content row, i.e. before any ciphertext
	// exists, so a failure part-way can never leave unreadable files behind.
	var dek []byte
	if p.Passphrase != "" {
		env, key, err := crypto.NewEnvelope(p.Passphrase, p.Slug)
		if err != nil {
			return nil, err
		}
		c.Encryption, dek = env, key
	}

	if err := svc.store.CreateContent(c); err != nil {
		return nil, err
	}

	if err := svc.storeFiles(c, p.FileData, p.FileName, dek); err != nil {
		svc.store.DeleteContent(c.ID)
		return nil, err
	}

	files, _ := svc.store.ListFilePaths(c.ID)
	resp := c.ToResponse(files)
	return &resp, nil
}

func (svc *ContentService) Update(p UpdateParams) (*model.ContentResponse, error) {
	c, err := svc.ownedContent(p.Slug, p.Owner)
	if err != nil {
		return nil, err
	}

	if p.Title != nil {
		c.Title = strings.TrimSpace(*p.Title)
	}
	if p.SourceURL != nil {
		c.SourceURL = strings.TrimSpace(*p.SourceURL)
	}
	if p.Creator != nil {
		c.Creator = strings.TrimSpace(*p.Creator)
	}
	if p.AllowExternalResources != nil {
		c.AllowExternalResources = *p.AllowExternalResources
	}

	// Resolve the encryption change before touching anything, so a bad
	// passphrase is rejected without a partial update.
	dek, err := svc.applyEncryptionChange(c, p)
	if err != nil {
		return nil, err
	}

	if err := svc.store.UpdateContent(c); err != nil {
		return nil, err
	}

	if len(p.FileData) > 0 {
		if err := svc.store.DeleteContentFiles(c.ID); err != nil {
			return nil, err
		}
		if err := svc.storeFiles(c, p.FileData, p.FileName, dek); err != nil {
			return nil, err
		}
	}

	files, _ := svc.store.ListFilePaths(c.ID)
	resp := c.ToResponse(files)
	return &resp, nil
}

func (svc *ContentService) Delete(slug, owner string) error {
	c, err := svc.ownedContent(slug, owner)
	if err != nil {
		return err
	}
	return svc.store.DeleteContent(c.ID)
}

// IsEncrypted reports whether the content at slug is passphrase protected.
func (svc *ContentService) IsEncrypted(slug string) (bool, error) {
	c, err := svc.contentBySlug(slug)
	if err != nil {
		return false, err
	}
	return c.IsEncrypted(), nil
}

// Unlock verifies a visitor's passphrase and returns the content's data key,
// which callers hold only for as long as they serve that visitor. It returns
// ErrNotEncrypted for public content and ErrWrongPassphrase for a bad guess.
func (svc *ContentService) Unlock(slug, passphrase string) ([]byte, error) {
	c, err := svc.contentBySlug(slug)
	if err != nil {
		return nil, err
	}
	if !c.IsEncrypted() {
		return nil, ErrNotEncrypted
	}
	if passphrase == "" {
		return nil, ErrPassphraseRequired
	}
	return c.Encryption.Unwrap(passphrase, slug)
}

// GetFile returns a file ready to serve. For encrypted content, dek must be the
// data key from Unlock; without it the call fails with ErrPassphraseRequired,
// and that takes precedence over reporting a missing path so an encrypted site's
// layout is not enumerable without the passphrase.
func (svc *ContentService) GetFile(slug, filePath string, dek []byte) (*model.ContentFile, error) {
	if strings.Contains(filePath, "..") {
		return nil, ErrInvalidFilePath
	}
	f, err := svc.store.GetContentFile(slug, filePath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, svc.notFoundOrLocked(slug, dek)
		}
		return nil, err
	}
	if !f.Encrypted {
		return f, nil
	}
	if len(dek) == 0 {
		return nil, ErrPassphraseRequired
	}
	plaintext, err := crypto.DecryptFile(dek, slug, filePath, f.FileData)
	if err != nil {
		return nil, err
	}
	f.FileData = plaintext
	return f, nil
}

// notFoundOrLocked decides what a missing file means to the caller: for locked
// content, "unlock first" rather than "no such file".
func (svc *ContentService) notFoundOrLocked(slug string, dek []byte) error {
	if len(dek) > 0 {
		return ErrNotFound
	}
	encrypted, err := svc.IsEncrypted(slug)
	if err != nil {
		return err
	}
	if encrypted {
		return ErrPassphraseRequired
	}
	return ErrNotFound
}

// applyEncryptionChange resolves the requested encryption transition and
// persists it, returning the data key that new files must be encrypted with
// (nil when the entry ends up unencrypted).
func (svc *ContentService) applyEncryptionChange(c *model.Content, p UpdateParams) ([]byte, error) {
	switch {
	case !c.IsEncrypted() && (p.RemoveEncryption || p.NewPassphrase != ""):
		return nil, ErrNotEncrypted

	case !c.IsEncrypted() && p.Passphrase != "":
		return svc.encrypt(c, p.Passphrase, len(p.FileData) > 0)

	case !c.IsEncrypted():
		return nil, nil

	case p.RemoveEncryption:
		return nil, svc.decrypt(c, p.Passphrase, len(p.FileData) > 0)

	case p.NewPassphrase != "":
		return svc.rotatePassphrase(c, p.Passphrase, p.NewPassphrase, len(p.FileData) > 0)

	case len(p.FileData) > 0:
		// Replacement files have to be sealed with the existing data key, so the
		// current passphrase is required.
		return svc.unwrap(c, p.Passphrase)

	default:
		// Metadata-only edit of encrypted content: no key needed.
		return nil, nil
	}
}

// encrypt turns on encryption for a previously public entry, sealing the files
// it already has. When filesReplaced is set those files are about to be dropped
// for a fresh upload, so only the envelope is stored.
func (svc *ContentService) encrypt(c *model.Content, passphrase string, filesReplaced bool) ([]byte, error) {
	env, dek, err := crypto.NewEnvelope(passphrase, c.Slug)
	if err != nil {
		return nil, err
	}
	var rewritten map[int64][]byte
	if !filesReplaced {
		files, err := svc.store.ListContentFiles(c.ID)
		if err != nil {
			return nil, err
		}
		rewritten = make(map[int64][]byte, len(files))
		for _, f := range files {
			sealed, err := crypto.EncryptFile(dek, c.Slug, f.FilePath, f.FileData)
			if err != nil {
				return nil, fmt.Errorf("encrypting %s: %w", f.FilePath, err)
			}
			rewritten[f.ID] = sealed
		}
	}
	if err := svc.store.ApplyEncryption(c.ID, env, rewritten); err != nil {
		return nil, err
	}
	c.Encryption = env
	return dek, nil
}

// decrypt removes encryption, writing the files back as plaintext. As in
// encrypt, files that are about to be replaced are left alone.
func (svc *ContentService) decrypt(c *model.Content, passphrase string, filesReplaced bool) error {
	dek, err := svc.unwrap(c, passphrase)
	if err != nil {
		return err
	}
	var rewritten map[int64][]byte
	if !filesReplaced {
		files, err := svc.store.ListContentFiles(c.ID)
		if err != nil {
			return err
		}
		rewritten = make(map[int64][]byte, len(files))
		for _, f := range files {
			plaintext, err := crypto.DecryptFile(dek, c.Slug, f.FilePath, f.FileData)
			if err != nil {
				return fmt.Errorf("decrypting %s: %w", f.FilePath, err)
			}
			rewritten[f.ID] = plaintext
		}
	}
	if err := svc.store.ApplyEncryption(c.ID, nil, rewritten); err != nil {
		return err
	}
	c.Encryption = nil
	return nil
}

// rotatePassphrase re-encrypts an entry under a brand-new data key derived from
// a new passphrase.
//
// Re-wrapping the existing key would be cheaper, but visitors who already
// unlocked the content hold that key in their unlock cookie: rotating to a fresh
// key is what makes "the old passphrase no longer works" true immediately rather
// than one cookie lifetime later. Content is capped at 10 MB, so the rewrite is
// cheap in practice.
func (svc *ContentService) rotatePassphrase(c *model.Content, current, next string, filesReplaced bool) ([]byte, error) {
	oldDEK, err := svc.unwrap(c, current)
	if err != nil {
		return nil, err
	}
	env, newDEK, err := crypto.NewEnvelope(next, c.Slug)
	if err != nil {
		return nil, err
	}

	var rewritten map[int64][]byte
	if !filesReplaced {
		files, err := svc.store.ListContentFiles(c.ID)
		if err != nil {
			return nil, err
		}
		rewritten = make(map[int64][]byte, len(files))
		for _, f := range files {
			plaintext, err := crypto.DecryptFile(oldDEK, c.Slug, f.FilePath, f.FileData)
			if err != nil {
				return nil, fmt.Errorf("decrypting %s: %w", f.FilePath, err)
			}
			sealed, err := crypto.EncryptFile(newDEK, c.Slug, f.FilePath, plaintext)
			if err != nil {
				return nil, fmt.Errorf("re-encrypting %s: %w", f.FilePath, err)
			}
			rewritten[f.ID] = sealed
		}
	}

	if err := svc.store.ApplyEncryption(c.ID, env, rewritten); err != nil {
		return nil, err
	}
	c.Encryption = env
	return newDEK, nil
}

// unwrap recovers an encrypted entry's data key from the supplied passphrase.
func (svc *ContentService) unwrap(c *model.Content, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, ErrPassphraseRequired
	}
	return c.Encryption.Unwrap(passphrase, c.Slug)
}

func (svc *ContentService) contentBySlug(slug string) (*model.Content, error) {
	c, err := svc.store.GetContentBySlug(slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

// ownedContent loads a content entry and hides entries belonging to other
// owners behind ErrNotFound.
func (svc *ContentService) ownedContent(slug, owner string) (*model.Content, error) {
	c, err := svc.contentBySlug(slug)
	if err != nil {
		return nil, err
	}
	if c.Owner != owner {
		return nil, ErrNotFound
	}
	return c, nil
}

// storeFiles writes an upload's files, sealing each payload when dek is set.
func (svc *ContentService) storeFiles(c *model.Content, data []byte, fileName string, dek []byte) error {
	if isZip(fileName) {
		return svc.extractZip(c, data, dek)
	}
	return svc.createFile(c, "index.html", "text/html", data, dek)
}

func (svc *ContentService) extractZip(c *model.Content, data []byte, dek []byte) error {
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

		if err := svc.createFile(c, name, detectContentType(name), buf, dek); err != nil {
			return err
		}
	}
	return nil
}

// createFile persists one file, encrypting it when the content entry has a data
// key.
func (svc *ContentService) createFile(c *model.Content, path, contentType string, data []byte, dek []byte) error {
	if len(dek) > 0 {
		sealed, err := crypto.EncryptFile(dek, c.Slug, path, data)
		if err != nil {
			return fmt.Errorf("encrypting %s: %w", path, err)
		}
		data = sealed
	}
	return svc.store.CreateContentFile(&model.ContentFile{
		ContentID:   c.ID,
		FilePath:    path,
		FileData:    data,
		ContentType: contentType,
	})
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
