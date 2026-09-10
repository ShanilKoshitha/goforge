// Package asset serves a bounded set of application assets from an fs.FS.
//
// Set validates and reads every file at construction time. Requests therefore
// never touch the source filesystem, and callers can replace Set with any
// ordinary http.Handler and URL resolver when local embedding no longer fits.
package asset

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	defaultURLPrefix = "/assets"
	defaultMaxFiles  = 256
	defaultMaxFile   = 8 << 20
	defaultMaxTotal  = 32 << 20
)

// Config describes one immutable in-memory asset set. Zero limits select
// conservative defaults: 256 files, 8 MiB per file, and 32 MiB total.
type Config struct {
	Root string

	// URLPrefix is an absolute, canonical path without a trailing slash. It
	// defaults to /assets.
	URLPrefix string

	MaxFiles      int
	MaxFileBytes  int64
	MaxTotalBytes int64
}

// Set is a validated collection of application-owned assets. It is safe for
// concurrent use after New returns.
type Set struct {
	prefix string
	files  map[string]file
}

type file struct {
	body        []byte
	contentType string
	etag        string
}

var contentTypes = map[string]string{
	".avif":  "image/avif",
	".css":   "text/css; charset=utf-8",
	".gif":   "image/gif",
	".ico":   "image/x-icon",
	".jpeg":  "image/jpeg",
	".jpg":   "image/jpeg",
	".js":    "text/javascript; charset=utf-8",
	".json":  "application/json",
	".mjs":   "text/javascript; charset=utf-8",
	".otf":   "font/otf",
	".png":   "image/png",
	".ttf":   "font/ttf",
	".wasm":  "application/wasm",
	".webp":  "image/webp",
	".woff":  "font/woff",
	".woff2": "font/woff2",
}

// New validates root and reads its complete file inventory into a Set.
// Directories and filenames must use portable, canonical paths. HTML, SVG,
// XML, and other unlisted types are rejected instead of being served as
// same-origin active content or relying on platform MIME databases.
func New(files fs.FS, config Config) (*Set, error) {
	if files == nil {
		return nil, errors.New("asset: filesystem is required")
	}
	root := config.Root
	if root == "" {
		root = "."
	}
	if root != "." && (!fs.ValidPath(root) || !validLogicalPath(root)) {
		return nil, fmt.Errorf("asset: root %q is not a canonical filesystem path", root)
	}
	prefix, err := validatedPrefix(config.URLPrefix)
	if err != nil {
		return nil, err
	}
	maxFiles, maxFile, maxTotal, err := validatedLimits(config)
	if err != nil {
		return nil, err
	}
	rootInfo, err := fs.Stat(files, root)
	if err != nil {
		return nil, fmt.Errorf("asset: inspect root %q: %w", root, err)
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("asset: root %q is not a directory", root)
	}

	set := &Set{prefix: prefix, files: make(map[string]file)}
	casePaths := make(map[string]string)
	var total int64
	err = fs.WalkDir(files, root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == root {
			return nil
		}
		logical, err := pathRelative(root, name)
		if err != nil {
			return err
		}
		if !validLogicalPath(logical) {
			return fmt.Errorf("asset: path %q is not portable and canonical", logical)
		}
		folded := strings.ToLower(logical)
		if other, exists := casePaths[folded]; exists {
			return fmt.Errorf("asset: paths %q and %q collide when case-folded", other, logical)
		}
		casePaths[folded] = logical
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("asset: inspect %q: %w", logical, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("asset: %q is not a regular file", logical)
		}
		if len(set.files) >= maxFiles {
			return fmt.Errorf("asset: file count exceeds limit %d", maxFiles)
		}
		mediaType, ok := contentTypes[strings.ToLower(path.Ext(logical))]
		if !ok {
			return fmt.Errorf("asset: %q has an unsupported file type", logical)
		}
		if info.Size() < 0 || info.Size() > maxFile {
			return fmt.Errorf("asset: %q exceeds per-file limit %d bytes", logical, maxFile)
		}
		body, err := readBounded(files, name, maxFile)
		if err != nil {
			return fmt.Errorf("asset: read %q: %w", logical, err)
		}
		if int64(len(body)) != info.Size() {
			return fmt.Errorf("asset: %q changed size while being read", logical)
		}
		if int64(len(body)) > maxTotal-total {
			return fmt.Errorf("asset: total size exceeds limit %d bytes", maxTotal)
		}
		total += int64(len(body))
		digest := sha256.Sum256(body)
		set.files[logical] = file{
			body:        body,
			contentType: mediaType,
			etag:        `"` + hex.EncodeToString(digest[:]) + `"`,
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("asset: load root %q: %w", root, err)
	}
	return set, nil
}

// URL resolves a logical asset name to its stable same-origin URL. It returns
// an error for invalid or unknown names so template rendering fails before a
// response is committed.
func (set *Set) URL(logical string) (string, error) {
	if set == nil {
		return "", errors.New("asset: set is nil")
	}
	if !validLogicalPath(logical) {
		return "", fmt.Errorf("asset: logical path %q is not portable and canonical", logical)
	}
	if _, ok := set.files[logical]; !ok {
		return "", fmt.Errorf("asset: unknown asset %q", logical)
	}
	segments := strings.Split(logical, "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	return set.prefix + strings.Join(segments, "/"), nil
}

// ServeHTTP serves GET and HEAD requests below the configured URL prefix.
// Successful representations always use strong SHA-256 validators and require
// cache revalidation. http.ServeContent supplies standard conditional and byte
// range semantics over the immutable in-memory bytes.
func (set *Set) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if set == nil || request == nil || request.URL == nil {
		writeError(response, http.StatusNotFound, "asset not found\n", request != nil && request.Method == http.MethodHead)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		writeError(response, http.StatusMethodNotAllowed, "method not allowed\n", false)
		return
	}
	if request.URL.RawPath != "" || request.URL.RawQuery != "" || !strings.HasPrefix(request.URL.Path, set.prefix) {
		writeError(response, http.StatusNotFound, "asset not found\n", request.Method == http.MethodHead)
		return
	}
	logical := strings.TrimPrefix(request.URL.Path, set.prefix)
	if !validLogicalPath(logical) {
		writeError(response, http.StatusNotFound, "asset not found\n", request.Method == http.MethodHead)
		return
	}
	item, ok := set.files[logical]
	if !ok {
		writeError(response, http.StatusNotFound, "asset not found\n", request.Method == http.MethodHead)
		return
	}

	response.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	response.Header().Set("Content-Type", item.contentType)
	response.Header().Set("Content-Length", fmt.Sprintf("%d", len(item.body)))
	response.Header().Set("ETag", item.etag)
	response.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(response, request, logical, time.Time{}, bytes.NewReader(item.body))
}

func validatedPrefix(prefix string) (string, error) {
	if prefix == "" {
		prefix = defaultURLPrefix
	}
	if prefix == "/" || !strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/") ||
		strings.ContainsAny(prefix, "\\%?#") || strings.Contains(prefix, "//") ||
		path.Clean(prefix) != prefix {
		return "", fmt.Errorf("asset: URL prefix %q must be a canonical absolute path without a trailing /", prefix)
	}
	for _, segment := range strings.Split(strings.Trim(prefix, "/"), "/") {
		if segment != "" && !validSegment(segment) {
			return "", fmt.Errorf("asset: URL prefix %q must be a canonical absolute path without a trailing /", prefix)
		}
	}
	return prefix + "/", nil
}

func validatedLimits(config Config) (int, int64, int64, error) {
	maxFiles := config.MaxFiles
	maxFile := config.MaxFileBytes
	maxTotal := config.MaxTotalBytes
	if maxFiles == 0 {
		maxFiles = defaultMaxFiles
	}
	if maxFile == 0 {
		maxFile = defaultMaxFile
	}
	if maxTotal == 0 {
		maxTotal = defaultMaxTotal
	}
	if maxFiles < 0 || maxFile < 0 || maxTotal < 0 {
		return 0, 0, 0, errors.New("asset: limits must be positive")
	}
	if maxFile > maxTotal {
		return 0, 0, 0, errors.New("asset: per-file limit must not exceed total limit")
	}
	return maxFiles, maxFile, maxTotal, nil
}

func pathRelative(root, name string) (string, error) {
	if root == "." {
		return name, nil
	}
	prefix := root + "/"
	if !strings.HasPrefix(name, prefix) {
		return "", fmt.Errorf("asset: path %q escaped root %q", name, root)
	}
	return strings.TrimPrefix(name, prefix), nil
}

func validLogicalPath(name string) bool {
	if name == "" || !fs.ValidPath(name) || strings.ContainsAny(name, "\\%") || path.Clean(name) != name {
		return false
	}
	for _, segment := range strings.Split(name, "/") {
		if !validSegment(segment) {
			return false
		}
	}
	return true
}

func validSegment(segment string) bool {
	if segment == "" || strings.HasPrefix(segment, ".") {
		return false
	}
	for _, character := range segment {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func readBounded(files fs.FS, name string, limit int64) ([]byte, error) {
	file, err := files.Open(name)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(file, limit))
	var extra [1]byte
	extraBytes := 0
	if readErr == nil {
		extraBytes, readErr = io.ReadFull(file, extra[:])
		if errors.Is(readErr, io.EOF) {
			readErr = nil
		}
	}
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	if extraBytes != 0 {
		return nil, fmt.Errorf("content exceeds limit %d bytes", limit)
	}
	return body, nil
}

func writeError(response http.ResponseWriter, status int, message string, head bool) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.Header().Set("Content-Length", fmt.Sprintf("%d", len(message)))
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(status)
	if !head {
		_, _ = io.WriteString(response, message)
	}
}
