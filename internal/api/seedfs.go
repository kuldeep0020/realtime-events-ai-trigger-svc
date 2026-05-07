package api

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DiskSeedFS reads files from one of a list of root directories (first hit
// wins). Use NewDiskSeedFS for the default lookup order:
//
//   1. /etc/seed/    (in-cluster ConfigMap mount)
//   2. ./seed/       (local dev)
//
// All paths are normalised against each root and rejected if they escape it
// (path traversal protection).
type DiskSeedFS struct {
	roots []string
}

// NewDiskSeedFS returns a SeedFS that searches roots in order. The default
// production wiring passes ["/etc/seed", "./seed"].
func NewDiskSeedFS(roots ...string) *DiskSeedFS {
	if len(roots) == 0 {
		roots = []string{"/etc/seed", "./seed"}
	}
	return &DiskSeedFS{roots: roots}
}

// ReadFile resolves rel against each root in order and returns the first
// successful read. Returns os.ErrNotExist if none match. Path traversal
// (../) is rejected.
func (d *DiskSeedFS) ReadFile(rel string) ([]byte, error) {
	if rel == "" {
		return nil, fs.ErrInvalid
	}
	// Reject absolute paths and parent-traversal segments. We compare on the
	// cleaned, slash-normalised representation so "./a/../b" doesn't pass.
	cleaned := filepath.ToSlash(filepath.Clean(rel))
	if strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "../") {
		return nil, fs.ErrInvalid
	}

	var lastErr error
	for _, root := range d.roots {
		full := filepath.Join(root, cleaned)
		// Defence-in-depth: ensure the resolved absolute path is still
		// under root after symlink resolution.
		absRoot, err := filepath.Abs(root)
		if err != nil {
			lastErr = err
			continue
		}
		absFull, err := filepath.Abs(full)
		if err != nil {
			lastErr = err
			continue
		}
		if !strings.HasPrefix(absFull+string(filepath.Separator), absRoot+string(filepath.Separator)) && absFull != absRoot {
			lastErr = fs.ErrInvalid
			continue
		}
		body, err := os.ReadFile(full)
		if err == nil {
			return body, nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			lastErr = err
			continue
		}
		// Unexpected error — return immediately.
		return nil, err
	}
	if lastErr == nil {
		lastErr = fs.ErrNotExist
	}
	return nil, lastErr
}

// MapSeedFS is an in-memory SeedFS for tests. Construct via NewMapSeedFS.
type MapSeedFS struct {
	files map[string][]byte
}

// NewMapSeedFS returns a SeedFS backed by an in-memory map. Useful in tests
// to avoid disk I/O and root-directory plumbing.
func NewMapSeedFS(files map[string][]byte) *MapSeedFS {
	return &MapSeedFS{files: files}
}

// ReadFile returns the bytes for path; os.ErrNotExist when absent.
func (m *MapSeedFS) ReadFile(path string) ([]byte, error) {
	if b, ok := m.files[path]; ok {
		return b, nil
	}
	return nil, fs.ErrNotExist
}
