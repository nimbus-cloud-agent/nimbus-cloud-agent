package server

import (
	"fmt"
	"io"
	"mime"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// The explorer is rooted where the terminal opens, so a path the user sees in
// one tab resolves in the other. Browse intent never widens this: Clean
// collapses ".." before the prefix check, and the check runs on every request.
const sandboxRoot = "/workspace"

// maxPreviewSize bounds a single preview. Above it the panel shows the size
// instead of the bytes, which keeps a build artifact from stalling either side.
const maxPreviewSize = 2 << 20

// sandboxPath normalizes an explorer path and rejects anything outside the
// workspace. The leading slash is implied, so `/workspace` and `workspace`
// both name the root.
func sandboxPath(raw string) (string, error) {
	if strings.ContainsRune(raw, 0) {
		return "", fmt.Errorf("Invalid path.")
	}
	if raw == "" {
		raw = sandboxRoot
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	clean := path.Clean(raw)
	if clean != sandboxRoot && !strings.HasPrefix(clean, sandboxRoot+"/") {
		return "", fmt.Errorf("Only files inside %s can be browsed.", sandboxRoot)
	}
	return clean, nil
}

// sandboxFiles lists one directory level. It never provisions: a paused
// sandbox answers 409 so the panel can offer Resume instead of silently
// resuming a sandbox the user only wanted to look at.
func (s *Server) sandboxFiles(c *gin.Context) {
	sess, ok := s.session(c)
	if !ok {
		return
	}
	target, err := sandboxPath(c.Query("path"))
	if err != nil {
		s.fail(c, 400, err)
		return
	}
	sb, err := s.getSandbox(c.Request.Context(), sess.ID)
	if err != nil || sb.State != "running" {
		c.JSON(409, gin.H{"error": "Start the sandbox first."})
		return
	}
	entries, err := s.Sandboxes.ListDir(c.Request.Context(), sb.E2bSandboxID, target)
	if err != nil {
		s.fail(c, 502, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"path": target, "entries": entries})
}

// sandboxRawFile streams one file for preview. HTML is served as text/html so
// iframes render it, and the sandbox CSP keeps a previewed page from running
// script against this origin even when the URL is opened directly.
func (s *Server) sandboxRawFile(c *gin.Context) {
	sess, ok := s.session(c)
	if !ok {
		return
	}
	target, err := sandboxPath(c.Param("path"))
	if err != nil {
		s.fail(c, 400, err)
		return
	}
	sb, err := s.getSandbox(c.Request.Context(), sess.ID)
	if err != nil || sb.State != "running" {
		c.JSON(409, gin.H{"error": "Start the sandbox first."})
		return
	}
	stream, size, err := s.Sandboxes.ReadFileStream(c.Request.Context(), sb.E2bSandboxID, target)
	if err != nil {
		s.fail(c, 502, err)
		return
	}
	defer stream.Close()
	if size > maxPreviewSize {
		s.fail(c, 413, fmt.Errorf("This file is %s. Previews stop at %s.", humanSize(size), humanSize(maxPreviewSize)))
		return
	}
	c.Header("Content-Type", previewContentType(target))
	c.Header("Content-Security-Policy", "sandbox")
	c.Header("Content-Disposition", "inline")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	c.Status(200)
	// Copy at the cap as well: the size reported by Stat is advisory, and a
	// file can grow between the stat and the read.
	_, _ = io.Copy(c.Writer, io.LimitReader(stream, maxPreviewSize))
}

func humanSize(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(bytes)/(1<<10))
	}
	return fmt.Sprintf("%d B", bytes)
}

// previewContentType picks the response type for the bytes. Known source and
// text types are forced to a charset so the browser never guesses, and
// anything unknown falls back to text/plain rather than an opaque download.
func previewContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs", ".cjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".pdf":
		return "application/pdf"
	}
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		return ct
	}
	return "text/plain; charset=utf-8"
}
