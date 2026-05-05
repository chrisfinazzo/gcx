package query

import (
	"bytes"
	"context"
	"io"

	"github.com/grafana/gcx/internal/cache"
	"github.com/grafana/grafana-app-sdk/logging"
)

// CachedWriter wraps the cache check/store pattern used by query commands.
// A nil *CachedWriter is safe — Writer() returns the original writer and Store() is a no-op.
type CachedWriter struct {
	qcache   *cache.QueryCache
	cacheKey string
	buf      bytes.Buffer
}

// NewCachedWriter builds a cache key from parts and checks for a hit.
// If there is a hit, data contains the cached bytes and hit is true.
// If caching is disabled or noCache is true, cw is nil.
func NewCachedWriter(ctx context.Context, noCache bool, parts ...string) (cw *CachedWriter, data []byte, hit bool) {
	if noCache {
		return nil, nil, false
	}
	qcache := cache.NewQueryCache()
	if qcache == nil {
		return nil, nil, false
	}
	key := qcache.Key(parts...)
	logger := logging.FromContext(ctx)
	if cached, ok := qcache.Get(key); ok {
		logger.Debug("query cache hit", "key", key)
		return nil, cached, true
	}
	logger.Debug("query cache miss", "key", key)
	return &CachedWriter{qcache: qcache, cacheKey: key}, nil, false
}

// Writer returns an io.Writer that tees to both w and an internal buffer.
// If cw is nil, returns w unchanged.
func (cw *CachedWriter) Writer(w io.Writer) io.Writer {
	if cw == nil {
		return w
	}
	return io.MultiWriter(w, &cw.buf)
}

// Store writes the buffered output to the cache. Call only after successful encoding.
func (cw *CachedWriter) Store() {
	if cw == nil {
		return
	}
	cw.qcache.Set(cw.cacheKey, cw.buf.Bytes())
}
