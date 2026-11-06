package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

// Library storage limits. ChunkSize is the bytea row size; MaxFileSize is
// the per-file cap enforced while streaming writes.
const (
	LibraryChunkSize   = 1 << 20
	LibraryMaxFileSize = 100 << 20
	LibraryMaxNameLen  = 200
)

// LibraryMaxTextRead is the most bytes a single library_read text call
// returns; longer reads are served through offset/length paging.
const LibraryMaxTextRead = 1 << 20

// LibraryMaxImageRead is the most bytes a single library_read image call
// returns whole as base64. Larger images must go through
// library_copy_to_sandbox: the runtime attachment store caps images at
// this size, and the whole base64 payload would otherwise cross the
// stdio JSON-RPC link.
const LibraryMaxImageRead = 5 << 20

// ValidateLibraryName rejects names the flat Library namespace cannot hold:
// empty names, names over 200 characters, and names containing a path
// separator or parent-directory traversal.
func ValidateLibraryName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("library file name cannot be empty")
	}
	if len([]rune(name)) > LibraryMaxNameLen {
		return fmt.Errorf("library file name exceeds %d characters", LibraryMaxNameLen)
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("library file name must not contain / or \\ or ..")
	}
	return nil
}

var libraryMimeByExt = map[string]string{
	".html":     "text/html",
	".md":       "text/markdown",
	".markdown": "text/markdown",
	".txt":      "text/plain",
	".png":      "image/png",
	".jpg":      "image/jpeg",
	".jpeg":     "image/jpeg",
	".gif":      "image/gif",
	".webp":     "image/webp",
	".svg":      "image/svg+xml",
	".json":     "application/json",
}

// inferMimeType returns the explicit override when one is given, otherwise
// the MIME type for the name's extension, defaulting to
// application/octet-stream.
func inferMimeType(name, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	if mime, ok := libraryMimeByExt[strings.ToLower(filepath.Ext(name))]; ok {
		return mime
	}
	return "application/octet-stream"
}

// IsTextMime reports whether a stored MIME type can be read as text:
// text/*, application/json, or any +json suffix.
func IsTextMime(mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	return strings.HasPrefix(mime, "text/") || mime == "application/json" || strings.HasSuffix(mime, "+json")
}

// IsImageMime reports whether a stored MIME type is an image.
func IsImageMime(mime string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(mime)), "image/")
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// splitChunks cuts data into 1 MiB pieces. The final piece may be smaller;
// empty data yields no pieces.
func splitChunks(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}
	chunks := make([][]byte, 0, (len(data)+LibraryChunkSize-1)/LibraryChunkSize)
	for len(data) > 0 {
		n := min(len(data), LibraryChunkSize)
		chunks = append(chunks, data[:n])
		data = data[n:]
	}
	return chunks
}

// chunkRange returns the indexes of the chunks covering
// [offset, offset+length). Callers must ensure offset >= 0 and length > 0.
func chunkRange(offset, length int64) (first, last int64) {
	first = offset / LibraryChunkSize
	last = (offset + length - 1) / LibraryChunkSize
	return first, last
}

func (s *Store) CreateLibraryFile(ctx context.Context, row LibraryFile) error {
	return dbError(s.db.WithContext(ctx).Create(&row).Error)
}
func (s *Store) LibraryFile(ctx context.Context, id string) (LibraryFile, error) {
	var row LibraryFile
	err := s.db.WithContext(ctx).First(&row, "id=?", id).Error
	return row, dbError(err)
}
func (s *Store) LibraryFileByName(ctx context.Context, name string) (LibraryFile, error) {
	var row LibraryFile
	err := s.db.WithContext(ctx).First(&row, "name=?", name).Error
	return row, dbError(err)
}
func (s *Store) LibraryFiles(ctx context.Context) ([]LibraryFile, error) {
	rows := []LibraryFile{}
	err := s.db.WithContext(ctx).Order("updated_at DESC, id").Find(&rows).Error
	return rows, dbError(err)
}
func (s *Store) FileVersions(ctx context.Context, fileID string) ([]FileVersion, error) {
	rows := []FileVersion{}
	err := s.db.WithContext(ctx).Where("file_id=?", fileID).Order("version_number").Find(&rows).Error
	return rows, dbError(err)
}
func (s *Store) FileVersion(ctx context.Context, fileID string, version int) (FileVersion, error) {
	var row FileVersion
	err := s.db.WithContext(ctx).First(&row, "file_id=? AND version_number=?", fileID, version).Error
	return row, dbError(err)
}
func (s *Store) FileVersionByID(ctx context.Context, id string) (FileVersion, error) {
	var row FileVersion
	err := s.db.WithContext(ctx).First(&row, "id=?", id).Error
	return row, dbError(err)
}

// ReadVersionRange returns the bytes of one version covering
// [offset, offset+length), plus the version's total size. Only the chunks
// covering the requested range are fetched. An offset at or past the end,
// or a zero length, returns an empty slice with the total size.
func (s *Store) ReadVersionRange(ctx context.Context, versionID string, offset, length int64) ([]byte, int64, error) {
	if offset < 0 || length < 0 {
		return nil, 0, fmt.Errorf("invalid read range")
	}
	version, err := s.FileVersionByID(ctx, versionID)
	if err != nil {
		return nil, 0, err
	}
	if length == 0 || offset >= version.SizeBytes {
		return []byte{}, version.SizeBytes, nil
	}
	end := min(offset+length, version.SizeBytes)
	first, last := chunkRange(offset, end-offset)
	rows := []FileChunk{}
	if err := s.db.WithContext(ctx).Where("version_id=? AND chunk_index>=? AND chunk_index<=?", versionID, first, last).Order("chunk_index").Find(&rows).Error; err != nil {
		return nil, 0, dbError(err)
	}
	if int64(len(rows)) != last-first+1 {
		return nil, 0, fmt.Errorf("library version %s is missing chunks", versionID)
	}
	out := make([]byte, 0, end-offset)
	for i, row := range rows {
		chunkStart := (first + int64(i)) * LibraryChunkSize
		start := int64(0)
		if i == 0 {
			start = offset - chunkStart
		}
		take := min(int64(len(row.Data)), end-chunkStart) - start
		out = append(out, row.Data[start:start+take]...)
	}
	return out, version.SizeBytes, nil
}

// CreateFileVersion appends a new version to an existing file, streaming
// content in 1 MiB chunks inside one transaction: the version row, its
// chunks, the file's current-version pointer, size, and updated_at all
// commit or roll back together.
func (s *Store) CreateFileVersion(ctx context.Context, fileID string, r io.Reader) (FileVersion, error) {
	var version FileVersion
	err := s.Transaction(ctx, func(tx *Store) error {
		v, err := tx.createFileVersion(ctx, fileID, r)
		if err != nil {
			return err
		}
		version = v
		return nil
	})
	return version, err
}

// VersionConflict reports a write based on a version that is no longer
// current, so the caller can re-read and merge instead of overwriting someone
// else's version.
type VersionConflict struct {
	Name    string
	Base    int
	Current int
}

func (e *VersionConflict) Error() string {
	if e.Current == 0 {
		return fmt.Sprintf("library file %q no longer exists", e.Name)
	}
	return fmt.Sprintf("library file %q is at version %d, not version %d", e.Name, e.Current, e.Base)
}

// libraryCurrentVersion reads a file's current version number inside the
// caller's transaction; 0 means the file has no versions yet.
func (s *Store) libraryCurrentVersion(ctx context.Context, fileID string) (int, error) {
	var current int
	row := s.db.WithContext(ctx).Table("file_versions").Where("file_id=?", fileID).Select("COALESCE(MAX(version_number),0)").Scan(&current)
	return current, dbError(row.Error)
}

// WriteLibraryFile writes content under name, creating the file when the
// name is new and otherwise appending a new version, all in one
// transaction. baseVersion is the version the caller built its content on:
// 0 creates a new file and must not match an existing name, any other value
// must equal the file's current version or a *VersionConflict is returned.
// An explicit mime type overrides the inferred one; without an override a
// new file infers from its name and an existing file keeps its stored type.
func (s *Store) WriteLibraryFile(ctx context.Context, name, mimeType string, r io.Reader, baseVersion int64) (LibraryFile, FileVersion, error) {
	var file LibraryFile
	var version FileVersion
	err := s.Transaction(ctx, func(tx *Store) error {
		row, err := tx.LibraryFileByName(ctx, name)
		if err != nil && err != ErrNotFound {
			return err
		}
		if err == nil {
			file = row
			locked, err := tx.lockLibraryFile(ctx, name)
			if err != nil {
				return err
			}
			file = locked
			current, err := tx.libraryCurrentVersion(ctx, file.ID)
			if err != nil {
				return err
			}
			if baseVersion != int64(current) {
				return &VersionConflict{Name: file.Name, Base: int(baseVersion), Current: current}
			}
			if strings.TrimSpace(mimeType) != "" && mimeType != file.MimeType {
				if err := tx.db.WithContext(ctx).Model(&LibraryFile{ID: file.ID}).Select("mime_type").Updates(&LibraryFile{MimeType: mimeType}).Error; err != nil {
					return dbError(err)
				}
				file.MimeType = mimeType
			}
		} else {
			if baseVersion != 0 {
				return &VersionConflict{Name: name, Base: int(baseVersion), Current: 0}
			}
			now := time.Now()
			file = LibraryFile{ID: uuid.NewString(), Name: name, MimeType: inferMimeType(name, mimeType), SizeBytes: 0, CreatedAt: now, UpdatedAt: now}
			// ON CONFLICT DO NOTHING lets a concurrent writer for the same
			// new name win; the row is re-read locked below either way.
			if err := tx.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&file).Error; err != nil {
				return dbError(err)
			}
			locked, err := tx.lockLibraryFile(ctx, name)
			if err != nil {
				return err
			}
			file = locked
		}
		v, err := tx.createFileVersion(ctx, file.ID, r)
		if err != nil {
			return err
		}
		version = v
		file.CurrentVersionID = &version.ID
		file.SizeBytes = version.SizeBytes
		file.UpdatedAt = version.CreatedAt
		return nil
	})
	return file, version, err
}

func (s *Store) lockLibraryFile(ctx context.Context, name string) (LibraryFile, error) {
	var row LibraryFile
	err := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "name=?", name).Error
	return row, dbError(err)
}

// createFileVersion is the transactional core shared by CreateFileVersion
// and WriteLibraryFile. The caller must already hold the transaction; the
// file row is locked so two writers for one file cannot take the same
// version number. The reader is consumed in 1 MiB chunks and the per-file
// cap is enforced while streaming, so an arbitrarily large stream cannot
// grow memory without bound. The version's final size and SHA-256 are only
// known once the stream ends, so the row is inserted with placeholders and
// finalized before commit; the placeholder is never visible outside this
// transaction.
func (s *Store) createFileVersion(ctx context.Context, fileID string, r io.Reader) (FileVersion, error) {
	if r == nil {
		return FileVersion{}, fmt.Errorf("library content reader is required")
	}
	var file LibraryFile
	if err := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&file, "id=?", fileID).Error; err != nil {
		return FileVersion{}, dbError(err)
	}
	var maxVersion int
	row := s.db.WithContext(ctx).Table("file_versions").Where("file_id=?", fileID).Select("COALESCE(MAX(version_number),0)").Scan(&maxVersion)
	if row.Error != nil {
		return FileVersion{}, dbError(row.Error)
	}
	now := time.Now()
	version := FileVersion{ID: uuid.NewString(), FileID: fileID, VersionNumber: maxVersion + 1, SizeBytes: 0, SHA256: "", CreatedAt: now}
	if err := s.db.WithContext(ctx).Create(&version).Error; err != nil {
		return FileVersion{}, dbError(err)
	}
	hash := sha256.New()
	buf := make([]byte, LibraryChunkSize)
	var total int64
	index := 0
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if _, werr := hash.Write(chunk); werr != nil {
				return FileVersion{}, werr
			}
			total += int64(n)
			if total > LibraryMaxFileSize {
				return FileVersion{}, fmt.Errorf("library file exceeds %d bytes", LibraryMaxFileSize)
			}
			if err := s.db.WithContext(ctx).Create(&FileChunk{VersionID: version.ID, ChunkIndex: index, Data: chunk}).Error; err != nil {
				return FileVersion{}, dbError(err)
			}
			index++
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return FileVersion{}, err
		}
	}
	version.SizeBytes = total
	version.SHA256 = hex.EncodeToString(hash.Sum(nil)[:])
	if err := s.db.WithContext(ctx).Model(&FileVersion{ID: version.ID}).Select("size_bytes", "sha256").Updates(&version).Error; err != nil {
		return FileVersion{}, dbError(err)
	}
	changes := LibraryFile{CurrentVersionID: &version.ID, SizeBytes: total, UpdatedAt: now}
	if err := s.db.WithContext(ctx).Model(&LibraryFile{ID: fileID}).Select("current_version_id", "size_bytes", "updated_at").Updates(&changes).Error; err != nil {
		return FileVersion{}, dbError(err)
	}
	return version, nil
}

// ChunkSource fetches one version's bytes for a range. *Store satisfies it;
// tests substitute a fake.
type ChunkSource interface {
	ReadVersionRange(ctx context.Context, versionID string, offset, length int64) ([]byte, int64, error)
}

// VersionReader streams one file version's bytes in chunk order without
// holding the whole file in memory. It is the reader handed to the sandbox
// filesystem upload and to the HTTP content endpoint.
type VersionReader struct {
	src       ChunkSource
	ctx       context.Context
	versionID string
	size      int64
	offset    int64
	buf       []byte
	pos       int
}

func NewVersionReader(ctx context.Context, src ChunkSource, versionID string, size int64) *VersionReader {
	return &VersionReader{src: src, ctx: ctx, versionID: versionID, size: size}
}

func (r *VersionReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.buf) {
		if r.offset >= r.size {
			return 0, io.EOF
		}
		chunk, _, err := r.src.ReadVersionRange(r.ctx, r.versionID, r.offset, LibraryChunkSize)
		if err != nil {
			return 0, err
		}
		if len(chunk) == 0 {
			return 0, io.EOF
		}
		r.buf, r.pos = chunk, 0
		r.offset += int64(len(chunk))
	}
	n := copy(p, r.buf[r.pos:])
	r.pos += n
	return n, nil
}
