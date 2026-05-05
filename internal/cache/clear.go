package cache

import (
	"fmt"
	"os"
	"path/filepath"
)

// CacheDir returns the root gcx cache directory (~/.cache/gcx/).
func CacheDir() (string, error) {
	if dir := os.Getenv("GCX_QUERY_CACHE_DIR"); dir != "" {
		return filepath.Dir(dir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", parentDir), nil
}

// Clear removes all files from known cache subdirectories under ~/.cache/gcx/.
// Returns the number of files removed and any error.
func Clear() (int, error) {
	root, err := CacheDir()
	if err != nil {
		return 0, fmt.Errorf("could not determine cache directory: %w", err)
	}

	subdirs := []string{"query", "discovery", "openapi"}
	total := 0
	for _, sub := range subdirs {
		dir := filepath.Join(root, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				if err := os.RemoveAll(filepath.Join(dir, e.Name())); err == nil {
					total++
				}
			} else {
				if err := os.Remove(filepath.Join(dir, e.Name())); err == nil {
					total++
				}
			}
		}
	}
	return total, nil
}
