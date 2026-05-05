package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewQueryCache_Disabled(t *testing.T) {
	t.Setenv("GCX_QUERY_CACHE", "false")
	if c := NewQueryCache(); c != nil {
		t.Fatal("expected nil cache when disabled")
	}

	t.Setenv("GCX_QUERY_CACHE", "0")
	if c := NewQueryCache(); c != nil {
		t.Fatal("expected nil cache when disabled with 0")
	}
}

func TestNewQueryCache_CustomTTL(t *testing.T) {
	t.Setenv("GCX_QUERY_CACHE_TTL", "30s")
	t.Setenv("GCX_QUERY_CACHE_DIR", t.TempDir())
	c := NewQueryCache()
	if c == nil {
		t.Fatal("expected non-nil cache")
	}
	if c.ttl != 30*time.Second {
		t.Fatalf("expected 30s TTL, got %v", c.ttl)
	}
}

func TestNewQueryCache_InvalidTTL(t *testing.T) {
	t.Setenv("GCX_QUERY_CACHE_TTL", "not-a-duration")
	t.Setenv("GCX_QUERY_CACHE_DIR", t.TempDir())
	c := NewQueryCache()
	if c == nil {
		t.Fatal("expected non-nil cache")
	}
	if c.ttl != defaultTTL {
		t.Fatalf("expected default TTL %v, got %v", defaultTTL, c.ttl)
	}
}

func TestKey_Deterministic(t *testing.T) {
	c := &QueryCache{dir: t.TempDir(), ttl: time.Minute}
	k1 := c.Key("ctx", "ds-001", "up", "now-1h", "now")
	k2 := c.Key("ctx", "ds-001", "up", "now-1h", "now")
	if k1 != k2 {
		t.Fatalf("keys should be equal: %q vs %q", k1, k2)
	}
}

func TestKey_DifferentParts(t *testing.T) {
	c := &QueryCache{dir: t.TempDir(), ttl: time.Minute}
	k1 := c.Key("ctx", "ds-001", "up")
	k2 := c.Key("ctx", "ds-002", "up")
	if k1 == k2 {
		t.Fatal("keys should differ for different datasource UIDs")
	}
}

func TestKey_NilCache(t *testing.T) {
	var c *QueryCache
	if k := c.Key("a", "b"); k != "" {
		t.Fatalf("expected empty key from nil cache, got %q", k)
	}
}

func TestGetSet_RoundTrip(t *testing.T) {
	c := &QueryCache{dir: t.TempDir(), ttl: time.Minute}
	key := c.Key("test")
	want := []byte(`{"status":"success"}`)

	c.Set(key, want)

	got, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(got) != string(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGet_Miss(t *testing.T) {
	c := &QueryCache{dir: t.TempDir(), ttl: time.Minute}
	if _, ok := c.Get("nonexistent"); ok {
		t.Fatal("expected cache miss for nonexistent key")
	}
}

func TestGet_Expired(t *testing.T) {
	c := &QueryCache{dir: t.TempDir(), ttl: 1 * time.Millisecond}
	key := c.Key("test")
	c.Set(key, []byte("data"))

	time.Sleep(5 * time.Millisecond)

	if _, ok := c.Get(key); ok {
		t.Fatal("expected cache miss for expired entry")
	}
}

func TestSet_NilCache(t *testing.T) {
	var c *QueryCache
	c.Set("key", []byte("data")) // should not panic
}

func TestSet_EmptyData(t *testing.T) {
	c := &QueryCache{dir: t.TempDir(), ttl: time.Minute}
	c.Set("key", nil) // should not write
	if _, ok := c.Get("key"); ok {
		t.Fatal("expected no cache entry for empty data")
	}
}

func TestGet_NilCache(t *testing.T) {
	var c *QueryCache
	if _, ok := c.Get("key"); ok {
		t.Fatal("expected miss from nil cache")
	}
}

func TestSet_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "cache")
	c := &QueryCache{dir: dir, ttl: time.Minute}
	c.Set("key", []byte("data"))

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("expected cache directory to be created")
	}
}
