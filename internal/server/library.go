package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/model"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

func libraryLink(id string) string { return "nimbus://library/" + id }

func (s *Server) libraryRoutes(r *gin.Engine) {
	r.GET("/api/library", func(c *gin.Context) {
		files, err := s.librarySummaries(c.Request.Context())
		if err != nil {
			s.fail(c, 500, err)
			return
		}
		c.JSON(200, gin.H{"files": files})
	})
	r.GET("/api/library/:id", func(c *gin.Context) {
		includeVersions := false
		if raw, ok := c.GetQuery("include_versions"); ok {
			var err error
			includeVersions, err = strconv.ParseBool(raw)
			if err != nil {
				c.JSON(400, gin.H{"error": "include_versions must be a boolean"})
				return
			}
		}
		detail, err := s.libraryDetail(c.Request.Context(), c.Param("id"), includeVersions)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				c.Status(404)
				return
			}
			s.fail(c, 500, err)
			return
		}
		c.JSON(200, detail)
	})
	r.GET("/api/library/:id/content", func(c *gin.Context) {
		if _, err := uuid.Parse(c.Param("id")); err != nil {
			c.Status(404)
			return
		}
		file, err := s.S.LibraryFile(c.Request.Context(), c.Param("id"))
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				c.Status(404)
				return
			}
			s.fail(c, 500, err)
			return
		}
		version, err := s.libraryContentVersion(c.Request.Context(), file, c.Query("version"))
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				c.Status(404)
				return
			}
			s.fail(c, 500, err)
			return
		}
		headers := map[string]string{"Content-Disposition": "inline", "X-Content-Type-Options": "nosniff"}
		if file.MimeType == "text/html" {
			// Scripts may run, but the sandbox keeps an opaque origin and
			// forbids access to the app's DOM, cookies, and local storage.
			headers["Content-Security-Policy"] = "sandbox allow-scripts"
		}
		reader := store.NewVersionReader(c.Request.Context(), s.S, version.ID, version.SizeBytes)
		c.DataFromReader(200, version.SizeBytes, file.MimeType, reader, headers)
	})
}

// libraryContentVersion resolves the ?version=N query for the content
// endpoint: empty means the current version; an unknown version is
// ErrNotFound, as is a file with no versions at all.
func (s *Server) libraryContentVersion(ctx context.Context, file store.LibraryFile, raw string) (store.FileVersion, error) {
	if raw == "" {
		if file.CurrentVersionID == nil {
			return store.FileVersion{}, store.ErrNotFound
		}
		return s.S.FileVersionByID(ctx, *file.CurrentVersionID)
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return store.FileVersion{}, store.ErrNotFound
	}
	return s.S.FileVersion(ctx, file.ID, n)
}

type libraryDetailView struct {
	ID        string                        `json:"id"`
	Name      string                        `json:"name"`
	MimeType  string                        `json:"mime_type"`
	SizeBytes int64                         `json:"size_bytes"`
	Version   int64                         `json:"version"`
	CreatedAt time.Time                     `json:"created_at"`
	UpdatedAt time.Time                     `json:"updated_at"`
	Versions  []model.LibraryVersionSummary `json:"versions"`
}

func (s *Server) libraryDetail(ctx context.Context, id string, includeVersions bool) (libraryDetailView, error) {
	if _, err := uuid.Parse(id); err != nil {
		return libraryDetailView{}, store.ErrNotFound
	}
	file, err := s.S.LibraryFile(ctx, id)
	if err != nil {
		return libraryDetailView{}, err
	}
	detail := libraryDetailView{ID: file.ID, Name: file.Name, MimeType: file.MimeType, SizeBytes: file.SizeBytes, CreatedAt: file.CreatedAt, UpdatedAt: file.UpdatedAt, Versions: []model.LibraryVersionSummary{}}
	if includeVersions {
		detail.Versions, err = s.libraryVersionSummaries(ctx, file.ID)
		if err != nil {
			return libraryDetailView{}, err
		}
	}
	if file.CurrentVersionID != nil {
		if current, err := s.S.FileVersionByID(ctx, *file.CurrentVersionID); err == nil {
			detail.Version = int64(current.VersionNumber)
		}
	}
	return detail, nil
}

func (s *Server) librarySummaries(ctx context.Context) ([]model.LibraryFileSummary, error) {
	rows, err := s.S.LibraryFiles(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.LibraryFileSummary, 0, len(rows))
	for _, row := range rows {
		summary := model.LibraryFileSummary{ID: row.ID, Name: row.Name, MimeType: row.MimeType, SizeBytes: row.SizeBytes, UpdatedAt: row.UpdatedAt}
		if row.CurrentVersionID != nil {
			if current, err := s.S.FileVersionByID(ctx, *row.CurrentVersionID); err == nil {
				summary.Version = int64(current.VersionNumber)
			}
		}
		out = append(out, summary)
	}
	return out, nil
}

func (s *Server) libraryVersionSummaries(ctx context.Context, fileID string) ([]model.LibraryVersionSummary, error) {
	rows, err := s.S.FileVersions(ctx, fileID)
	if err != nil {
		return nil, err
	}
	out := make([]model.LibraryVersionSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.LibraryVersionSummary{Version: int64(row.VersionNumber), SizeBytes: row.SizeBytes, SHA256: row.SHA256, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

// resolveLibraryFile finds a file by id when given, otherwise by name.
// Neither is an error; an unknown id or name is store.ErrNotFound.
func (s *Server) resolveLibraryFile(ctx context.Context, id, name string) (store.LibraryFile, error) {
	if id != "" {
		return s.S.LibraryFile(ctx, id)
	}
	if name != "" {
		return s.S.LibraryFileByName(ctx, name)
	}
	return store.LibraryFile{}, fmt.Errorf("a library file id or name is required")
}

// resolveLibraryVersion selects the version a tool call addresses: 0 means
// current, any other number must exist for the file.
func (s *Server) resolveLibraryVersion(ctx context.Context, file store.LibraryFile, version int64) (store.FileVersion, error) {
	if version == 0 {
		if file.CurrentVersionID == nil {
			return store.FileVersion{}, fmt.Errorf("library file %q has no versions", file.Name)
		}
		return s.S.FileVersionByID(ctx, *file.CurrentVersionID)
	}
	if version < 0 {
		return store.FileVersion{}, fmt.Errorf("library version must be positive")
	}
	return s.S.FileVersion(ctx, file.ID, int(version))
}

func (s *Server) libraryList(ctx context.Context) (model.LibraryResult, error) {
	files, err := s.librarySummaries(ctx)
	if err != nil {
		return model.LibraryResult{}, err
	}
	return model.LibraryResult{Files: files}, nil
}

func (s *Server) libraryRead(ctx context.Context, args model.LibraryArgs) (model.LibraryResult, error) {
	file, err := s.resolveLibraryFile(ctx, args.ID, args.Name)
	if err != nil {
		return model.LibraryResult{}, err
	}
	version, err := s.resolveLibraryVersion(ctx, file, args.Version)
	if err != nil {
		return model.LibraryResult{}, err
	}
	result := model.LibraryResult{ID: file.ID, Name: file.Name, Version: int64(version.VersionNumber), MimeType: file.MimeType, SizeBytes: version.SizeBytes, Link: libraryLink(file.ID)}
	switch {
	case store.IsImageMime(file.MimeType):
		if version.SizeBytes > store.LibraryMaxImageRead {
			return model.LibraryResult{}, fmt.Errorf("library image %q is %d bytes, over the %d byte read limit; use library_copy_to_sandbox to work with it in the sandbox", file.Name, version.SizeBytes, store.LibraryMaxImageRead)
		}
		// Images ignore the range and return the whole file as base64.
		data, _, err := s.S.ReadVersionRange(ctx, version.ID, 0, version.SizeBytes)
		if err != nil {
			return model.LibraryResult{}, err
		}
		result.Image = &model.LibraryImage{MediaType: file.MimeType, Data: base64.StdEncoding.EncodeToString(data)}
	case store.IsTextMime(file.MimeType):
		if args.Offset < 0 || args.Length < 0 {
			return model.LibraryResult{}, fmt.Errorf("library read offset and length must not be negative")
		}
		length := args.Length
		if length == 0 || length > store.LibraryMaxTextRead {
			length = store.LibraryMaxTextRead
		}
		data, total, err := s.S.ReadVersionRange(ctx, version.ID, args.Offset, length)
		if err != nil {
			return model.LibraryResult{}, err
		}
		result.Content = strings.ToValidUTF8(string(data), "�")
		result.Truncated = args.Offset+int64(len(data)) < total
	default:
		return model.LibraryResult{}, fmt.Errorf("library file %q is %s, which cannot be read as text or an image; use library_copy_to_sandbox to work with it in the sandbox", file.Name, file.MimeType)
	}
	return result, nil
}

func (s *Server) libraryWrite(ctx context.Context, args model.LibraryArgs) (model.LibraryResult, error) {
	if err := store.ValidateLibraryName(args.Name); err != nil {
		return model.LibraryResult{}, err
	}
	if !utf8.ValidString(args.Content) {
		return model.LibraryResult{}, fmt.Errorf("library_write content must be valid UTF-8 text")
	}
	if int64(len(args.Content)) > store.LibraryMaxFileSize {
		return model.LibraryResult{}, fmt.Errorf("library file exceeds %d bytes", store.LibraryMaxFileSize)
	}
	file, version, err := s.S.WriteLibraryFile(ctx, args.Name, args.MimeType, strings.NewReader(args.Content))
	if err != nil {
		return model.LibraryResult{}, err
	}
	return model.LibraryResult{ID: file.ID, Name: file.Name, Version: int64(version.VersionNumber), SizeBytes: version.SizeBytes, MimeType: file.MimeType, Link: libraryLink(file.ID), SHA256: version.SHA256}, nil
}

func (s *Server) libraryVersions(ctx context.Context, args model.LibraryArgs) (model.LibraryResult, error) {
	file, err := s.resolveLibraryFile(ctx, args.ID, args.Name)
	if err != nil {
		return model.LibraryResult{}, err
	}
	versions, err := s.libraryVersionSummaries(ctx, file.ID)
	if err != nil {
		return model.LibraryResult{}, err
	}
	return model.LibraryResult{ID: file.ID, Name: file.Name, Versions: versions}, nil
}

func (s *Server) libraryCopyToSandbox(ctx context.Context, sb model.Sandbox, args model.LibraryArgs) (model.LibraryResult, error) {
	file, err := s.resolveLibraryFile(ctx, args.ID, args.Name)
	if err != nil {
		return model.LibraryResult{}, err
	}
	version, err := s.resolveLibraryVersion(ctx, file, args.Version)
	if err != nil {
		return model.LibraryResult{}, err
	}
	if args.Path == "" {
		return model.LibraryResult{}, fmt.Errorf("library_copy_to_sandbox requires a sandbox path")
	}
	// The reader yields 1 MiB chunks from PostgreSQL and the sandbox
	// filesystem API streams them in one upload; the whole file is never
	// held in memory on either side.
	reader := store.NewVersionReader(ctx, s.S, version.ID, version.SizeBytes)
	written, err := s.Sandboxes.WriteFileStream(ctx, sb.E2bSandboxID, args.Path, reader)
	if err != nil {
		return model.LibraryResult{}, err
	}
	return model.LibraryResult{Path: written.Path, Bytes: version.SizeBytes, Version: int64(version.VersionNumber)}, nil
}

func (s *Server) libraryCopyFromSandbox(ctx context.Context, sb model.Sandbox, args model.LibraryArgs) (model.LibraryResult, error) {
	if args.Path == "" {
		return model.LibraryResult{}, fmt.Errorf("library_copy_from_sandbox requires a sandbox path")
	}
	name := args.Name
	if name == "" {
		name = filepath.Base(strings.ReplaceAll(args.Path, "\\", "/"))
	}
	if err := store.ValidateLibraryName(name); err != nil {
		return model.LibraryResult{}, err
	}
	stream, size, err := s.Sandboxes.ReadFileStream(ctx, sb.E2bSandboxID, args.Path)
	if err != nil {
		return model.LibraryResult{}, err
	}
	defer stream.Close()
	if size > store.LibraryMaxFileSize {
		return model.LibraryResult{}, fmt.Errorf("library file exceeds %d bytes", store.LibraryMaxFileSize)
	}
	// The store consumes the stream in 1 MiB chunks inside one transaction
	// and enforces the cap again while reading, in case the file grows
	// between Stat and the end of the stream.
	file, version, err := s.S.WriteLibraryFile(ctx, name, args.MimeType, stream)
	if err != nil {
		return model.LibraryResult{}, err
	}
	return model.LibraryResult{ID: file.ID, Name: file.Name, Version: int64(version.VersionNumber), SizeBytes: version.SizeBytes, Link: libraryLink(file.ID)}, nil
}
