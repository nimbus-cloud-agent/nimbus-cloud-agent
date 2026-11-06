package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Pure helpers and the chunk reader run without a database.
func TestValidateLibraryName(t *testing.T) {
	for _, bad := range []string{"", "   ", strings.Repeat("a", 201), "a/b", `a\b`, "..", "a..b", "../x"} {
		if err := ValidateLibraryName(bad); err == nil {
			t.Fatalf("name %q was accepted", bad)
		}
	}
	for _, good := range []string{"a", "report.md", "my file (1).html", strings.Repeat("a", 200), "a.b", ".hidden"} {
		if err := ValidateLibraryName(good); err != nil {
			t.Fatalf("name %q was rejected: %v", good, err)
		}
	}
}

func TestInferMimeType(t *testing.T) {
	cases := map[string]string{
		"page.html": "text/html", "doc.MD": "text/markdown", "notes.markdown": "text/markdown",
		"a.txt": "text/plain", "a.png": "image/png", "a.jpg": "image/jpeg", "a.jpeg": "image/jpeg",
		"a.gif": "image/gif", "a.webp": "image/webp", "a.svg": "image/svg+xml",
		"a.json": "application/json", "a.bin": "application/octet-stream", "noext": "application/octet-stream",
	}
	for name, want := range cases {
		if got := inferMimeType(name, ""); got != want {
			t.Fatalf("%s: got %s, want %s", name, got, want)
		}
	}
	if got := inferMimeType("a.txt", "application/custom"); got != "application/custom" {
		t.Fatalf("explicit override lost: %s", got)
	}
	if got := inferMimeType("a.txt", "  "); got != "text/plain" {
		t.Fatalf("blank override replaced inference: %s", got)
	}
}

func TestMimeClassification(t *testing.T) {
	for _, mime := range []string{"text/html", "text/plain", "application/json", "application/vnd.api+json", "TEXT/MARKDOWN"} {
		if !IsTextMime(mime) {
			t.Fatalf("%s is not text", mime)
		}
	}
	for _, mime := range []string{"image/png", "image/svg+xml", "application/octet-stream", "application/pdf"} {
		if IsTextMime(mime) {
			t.Fatalf("%s is text", mime)
		}
	}
	for _, mime := range []string{"image/png", "image/jpeg", "IMAGE/WEBP"} {
		if !IsImageMime(mime) {
			t.Fatalf("%s is not an image", mime)
		}
	}
	if IsImageMime("text/html") {
		t.Fatal("text/html is an image")
	}
}

func TestSha256HexAndSplitChunks(t *testing.T) {
	sum := sha256.Sum256([]byte("abc"))
	if got := sha256Hex([]byte("abc")); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 mismatch: %s", got)
	}
	if got := splitChunks(nil); got != nil {
		t.Fatalf("empty split: %v", got)
	}
	data := bytes.Repeat([]byte("x"), 2*LibraryChunkSize+3)
	chunks := splitChunks(data)
	if len(chunks) != 3 || len(chunks[0]) != LibraryChunkSize || len(chunks[1]) != LibraryChunkSize || len(chunks[2]) != 3 {
		t.Fatalf("split sizes: %v", []int{len(chunks), len(chunks[0]), len(chunks[1]), len(chunks[2])})
	}
	if got := bytes.Join(chunks, nil); !bytes.Equal(got, data) {
		t.Fatal("split lost bytes")
	}
}

func TestChunkRange(t *testing.T) {
	cases := []struct{ offset, length, first, last int64 }{
		{0, 1, 0, 0},
		{0, LibraryChunkSize, 0, 0},
		{0, LibraryChunkSize + 1, 0, 1},
		{LibraryChunkSize - 1, 2, 0, 1},
		{LibraryChunkSize, 1, 1, 1},
		{2 * LibraryChunkSize, 3 * LibraryChunkSize, 2, 4},
	}
	for _, c := range cases {
		if first, last := chunkRange(c.offset, c.length); first != c.first || last != c.last {
			t.Fatalf("chunkRange(%d,%d) = (%d,%d), want (%d,%d)", c.offset, c.length, first, last, c.first, c.last)
		}
	}
}

type fakeChunkSource struct {
	data []byte
}

func (f fakeChunkSource) ReadVersionRange(_ context.Context, _ string, offset, length int64) ([]byte, int64, error) {
	if offset >= int64(len(f.data)) || length <= 0 {
		return []byte{}, int64(len(f.data)), nil
	}
	end := min(offset+length, int64(len(f.data)))
	return f.data[offset:end], int64(len(f.data)), nil
}

func TestVersionReaderStreamsChunks(t *testing.T) {
	data := bytes.Repeat([]byte("r"), 2*LibraryChunkSize+7)
	reader := NewVersionReader(context.Background(), fakeChunkSource{data: data}, "v", int64(len(data)))
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("reader mismatch: %d bytes, %v", len(got), err)
	}
	// Small reads still reassemble the whole version in order.
	reader = NewVersionReader(context.Background(), fakeChunkSource{data: data}, "v", int64(len(data)))
	buf := make([]byte, 100)
	var out []byte
	for {
		n, err := reader.Read(buf)
		out = append(out, buf[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(out, data) {
		t.Fatal("small reads lost bytes")
	}
	// An empty version reads as EOF immediately.
	reader = NewVersionReader(context.Background(), fakeChunkSource{}, "v", 0)
	if _, err := reader.Read(make([]byte, 10)); err != io.EOF {
		t.Fatalf("empty version read: %v", err)
	}
}

type failingChunkSource struct{ err error }

func (f failingChunkSource) ReadVersionRange(_ context.Context, _ string, _, _ int64) ([]byte, int64, error) {
	return nil, 0, f.err
}

type emptyChunkSource struct{ size int64 }

func (f emptyChunkSource) ReadVersionRange(_ context.Context, _ string, _, _ int64) ([]byte, int64, error) {
	return []byte{}, f.size, nil
}

func TestVersionReaderPropagatesSourceErrors(t *testing.T) {
	want := errors.New("chunk store failed")
	reader := NewVersionReader(context.Background(), failingChunkSource{err: want}, "v", 100)
	if _, err := reader.Read(make([]byte, 10)); !errors.Is(err, want) {
		t.Fatalf("source error lost: %v", err)
	}
	// A source that yields no bytes for a non-empty version ends the stream.
	reader = NewVersionReader(context.Background(), emptyChunkSource{size: 100}, "v", 100)
	if _, err := reader.Read(make([]byte, 10)); err != io.EOF {
		t.Fatalf("empty chunk read: %v", err)
	}
}

// DB-backed tests below need PostgreSQL; they skip locally and run in CI.
func libraryStore(t *testing.T) *Store {
	t.Helper()
	db, _ := migrationDB(t, "999")
	return &Store{db: db}
}

func TestLibraryMigrationCreatesTables(t *testing.T) {
	s := libraryStore(t)
	ctx := context.Background()
	for _, table := range []string{"library_files", "file_versions", "file_chunks"} {
		var count int64
		if err := s.db.WithContext(ctx).Table(table).Count(&count).Error; err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
	var indexCount int64
	if err := s.db.WithContext(ctx).Raw("SELECT count(*) FROM pg_indexes WHERE tablename='file_versions' AND indexname='idx_file_versions_file_id'").Scan(&indexCount).Error; err != nil || indexCount != 1 {
		t.Fatalf("file_versions index missing: %d, %v", indexCount, err)
	}
}

func TestLibraryWriteCreatesVersions(t *testing.T) {
	s := libraryStore(t)
	ctx := context.Background()
	content := strings.Repeat("hello library\n", 100)
	file, v1, err := s.WriteLibraryFile(ctx, "notes.md", "", strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if v1.VersionNumber != 1 || v1.SizeBytes != int64(len(content)) || file.SizeBytes != int64(len(content)) || file.MimeType != "text/markdown" {
		t.Fatalf("first write: %+v %+v", file, v1)
	}
	if file.CurrentVersionID == nil || *file.CurrentVersionID != v1.ID {
		t.Fatal("current version pointer not set")
	}
	if v1.SHA256 != sha256Hex([]byte(content)) {
		t.Fatal("sha256 mismatch")
	}
	v2, err := s.CreateFileVersion(ctx, file.ID, strings.NewReader("second"))
	if err != nil {
		t.Fatal(err)
	}
	if v2.VersionNumber != 2 {
		t.Fatalf("second write version: %d", v2.VersionNumber)
	}
	read, err := s.LibraryFile(ctx, file.ID)
	if err != nil || read.CurrentVersionID == nil || *read.CurrentVersionID != v2.ID || read.SizeBytes != 6 {
		t.Fatalf("current pointer not advanced: %+v, %v", read, err)
	}
	versions, err := s.FileVersions(ctx, file.ID)
	if err != nil || len(versions) != 2 || versions[0].VersionNumber != 1 || versions[1].VersionNumber != 2 {
		t.Fatalf("versions: %+v, %v", versions, err)
	}
	old, err := s.FileVersion(ctx, file.ID, 1)
	if err != nil || old.SHA256 != v1.SHA256 {
		t.Fatalf("old version lost: %+v, %v", old, err)
	}
	if _, err := s.FileVersion(ctx, file.ID, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing version: %v", err)
	}
	if _, err := s.LibraryFileByName(ctx, "missing.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing file: %v", err)
	}
	if _, err := s.CreateFileVersion(ctx, uuid.NewString(), strings.NewReader("x")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("version for missing file: %v", err)
	}
}

func TestLibraryWriteKeepsMimeUnlessOverridden(t *testing.T) {
	s := libraryStore(t)
	ctx := context.Background()
	file, _, err := s.WriteLibraryFile(ctx, "data.txt", "application/custom", strings.NewReader("one"))
	if err != nil {
		t.Fatal(err)
	}
	if file.MimeType != "application/custom" {
		t.Fatalf("override lost: %s", file.MimeType)
	}
	file, _, err = s.WriteLibraryFile(ctx, "data.txt", "", strings.NewReader("two"))
	if err != nil || file.MimeType != "application/custom" {
		t.Fatalf("rewrite without override changed mime: %+v, %v", file, err)
	}
	file, _, err = s.WriteLibraryFile(ctx, "data.txt", "text/plain", strings.NewReader("three"))
	if err != nil || file.MimeType != "text/plain" {
		t.Fatalf("explicit override did not change mime: %+v, %v", file, err)
	}
}

func TestLibraryListOrdersByUpdatedAt(t *testing.T) {
	s := libraryStore(t)
	ctx := context.Background()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, _, err := s.WriteLibraryFile(ctx, name, "", strings.NewReader(name)); err != nil {
			t.Fatal(err)
		}
	}
	// Rewriting a.txt makes it the most recently updated.
	if _, _, err := s.WriteLibraryFile(ctx, "a.txt", "", strings.NewReader("again")); err != nil {
		t.Fatal(err)
	}
	rows, err := s.LibraryFiles(ctx)
	if err != nil || len(rows) != 3 || rows[0].Name != "a.txt" {
		t.Fatalf("list order: %+v, %v", rows, err)
	}
}

func TestReadVersionRangeAcrossChunks(t *testing.T) {
	s := libraryStore(t)
	ctx := context.Background()
	data := make([]byte, 2*LibraryChunkSize+10)
	for i := range data {
		data[i] = byte(i % 251)
	}
	_, version, err := s.WriteLibraryFile(ctx, "big.bin", "", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	// Range straddling the first chunk boundary, plus edge reads.
	got, total, err := s.ReadVersionRange(ctx, version.ID, LibraryChunkSize-5, 12)
	if err != nil || total != int64(len(data)) || !bytes.Equal(got, data[LibraryChunkSize-5:LibraryChunkSize+7]) {
		t.Fatalf("straddling range: %d bytes, total %d, %v", len(got), total, err)
	}
	got, _, err = s.ReadVersionRange(ctx, version.ID, 0, int64(len(data)))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("full range lost bytes", err)
	}
	got, total, err = s.ReadVersionRange(ctx, version.ID, int64(len(data))-3, 100)
	if err != nil || !bytes.Equal(got, data[len(data)-3:]) || total != int64(len(data)) {
		t.Fatalf("clamped tail: %d bytes, %v", len(got), err)
	}
	got, _, err = s.ReadVersionRange(ctx, version.ID, int64(len(data)), 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("past-end read: %d bytes, %v", len(got), err)
	}
	got, _, err = s.ReadVersionRange(ctx, version.ID, 0, 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("zero-length read: %d bytes, %v", len(got), err)
	}
	if _, _, err := s.ReadVersionRange(ctx, version.ID, -1, 1); err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, _, err := s.ReadVersionRange(ctx, uuid.NewString(), 0, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing version: %v", err)
	}
}

// cappedReader yields 1 MiB chunks of zeros forever, so the test proves the
// streaming cap without allocating a 100 MB file.
type cappedReader struct{ chunks int }

func (r *cappedReader) Read(p []byte) (int, error) {
	if r.chunks <= 0 {
		return 0, io.EOF
	}
	r.chunks--
	return len(p), nil
}

func TestLibraryWriteEnforcesSizeCapWhileStreaming(t *testing.T) {
	s := libraryStore(t)
	ctx := context.Background()
	if _, _, err := s.WriteLibraryFile(ctx, "too-big.bin", "", &cappedReader{chunks: LibraryMaxFileSize/LibraryChunkSize + 1}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize write accepted: %v", err)
	}
	// The failed write rolled back: no file row survives.
	if _, err := s.LibraryFileByName(ctx, "too-big.bin"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed write left a file row: %v", err)
	}
	// Exactly at the cap still succeeds.
	file, version, err := s.WriteLibraryFile(ctx, "at-cap.bin", "", &cappedReader{chunks: LibraryMaxFileSize / LibraryChunkSize})
	if err != nil {
		t.Fatal(err)
	}
	if version.SizeBytes != LibraryMaxFileSize || file.SizeBytes != LibraryMaxFileSize {
		t.Fatalf("cap-sized write: %+v %+v", file, version)
	}
}
