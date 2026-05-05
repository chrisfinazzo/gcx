package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultTTL      = 60 * time.Second
	defaultCacheDir = "query"
	parentDir       = "gcx"
)

// QueryCache is a disk-backed cache for query results with TTL-based expiry.
// A nil *QueryCache is safe to use — all methods are no-ops.
type QueryCache struct {
	dir string
	ttl time.Duration
}

// NewQueryCache returns a QueryCache configured from environment variables.
// Returns nil when caching is disabled (GCX_QUERY_CACHE=false or GCX_QUERY_CACHE=0).
func NewQueryCache() *QueryCache {
	if v := os.Getenv("GCX_QUERY_CACHE"); v == "false" || v == "0" {
		return nil
	}

	ttl := defaultTTL
	if v := os.Getenv("GCX_QUERY_CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			ttl = d
		}
	}

	dir := os.Getenv("GCX_QUERY_CACHE_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		dir = filepath.Join(home, ".cache", parentDir, defaultCacheDir)
	}

	return &QueryCache{dir: dir, ttl: ttl}
}

// Key builds a deterministic cache key from the given parts.
func (c *QueryCache) Key(parts ...string) string {
	if c == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

// Get returns the cached bytes for key if the entry exists and has not expired.
func (c *QueryCache) Get(key string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	path := filepath.Join(c.dir, key+".bin")
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if time.Since(info.ModTime()) > c.ttl {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// Set writes data to the cache under key. Errors are silently ignored
// because caching is best-effort.
func (c *QueryCache) Set(key string, data []byte) {
	if c == nil || len(data) == 0 {
		return
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(c.dir, ".tmp-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	tmp.Close()
	if err := os.Rename(tmpName, filepath.Join(c.dir, key+".bin")); err != nil {
		os.Remove(tmpName)
	}
}
