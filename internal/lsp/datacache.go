package lsp

import (
	"context"
	"sync"
	"time"
)

// dataFetchTimeout bounds one background column or sample fetch. Callers wait
// for it only as long as their own budget allows; a fetch that outlives the
// caller still fills the cache for the next request.
const dataFetchTimeout = 3 * time.Second

// sampleRows is how many rows a dataset hover shows.
const sampleRows = 5

// DatasetSample is the head of a DataSet or DataSource: its columns, their
// types (empty when the backend does not report them) and a few rows.
type DatasetSample struct {
	Kind    string
	Columns []string
	Types   []string
	Rows    []map[string]any
}

// dataCache keeps column lists and samples per dataset name until the project
// changes, so completion and hover do not query DuckDB on every keystroke.
// Failures are not cached; concurrent requests for one name share one fetch.
type dataCache struct {
	mu      sync.Mutex
	gen     uint64
	cols    fetchCache[[]string]
	samples fetchCache[DatasetSample]
}

// fetchCache is one kind of cached value; dataCache.mu guards it.
type fetchCache[T any] struct {
	done    map[string]T
	pending map[string]chan struct{}
}

// invalidate drops every entry. Fetches still running finish but do not store.
func (c *dataCache) invalidate() {
	c.mu.Lock()
	c.gen++
	c.cols.done = nil
	c.samples.done = nil
	c.mu.Unlock()
}

// columns returns the cached columns of name, fetching them if needed and
// waiting no longer than ctx allows.
func (c *dataCache) columns(ctx context.Context, b Backend, name string) ([]string, bool) {
	return cachedFetch(ctx, c, &c.cols, name, b.Columns)
}

// sample returns the cached sample of name, fetching it if needed and waiting
// no longer than ctx allows.
func (c *dataCache) sample(ctx context.Context, b Backend, name string) (DatasetSample, bool) {
	return cachedFetch(ctx, c, &c.samples, name, func(ctx context.Context, name string) (DatasetSample, error) {
		return b.Sample(ctx, name, sampleRows)
	})
}

func cachedFetch[T any](ctx context.Context, c *dataCache, fc *fetchCache[T], name string, fetch func(context.Context, string) (T, error)) (T, bool) {
	c.mu.Lock()
	if v, ok := fc.done[name]; ok {
		c.mu.Unlock()
		return v, true
	}
	done, running := fc.pending[name]
	if !running {
		done = make(chan struct{})
		if fc.pending == nil {
			fc.pending = make(map[string]chan struct{})
		}
		fc.pending[name] = done
		gen := c.gen
		// The fetch must not die with the request that started it.
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dataFetchTimeout)
		go func() {
			defer cancel()
			v, err := fetch(fctx, name)
			c.mu.Lock()
			if err == nil && gen == c.gen {
				if fc.done == nil {
					fc.done = make(map[string]T)
				}
				fc.done[name] = v
			}
			delete(fc.pending, name)
			c.mu.Unlock()
			close(done)
		}()
	}
	c.mu.Unlock()

	select {
	case <-done:
	case <-ctx.Done():
		var zero T
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := fc.done[name]
	return v, ok
}
