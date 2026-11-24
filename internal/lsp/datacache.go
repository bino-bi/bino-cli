package lsp

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"bino.bi/bino/internal/logx"
)

// dataFetchTimeout bounds one background column or sample fetch, like the
// in-process introspection cap: it holds the shared DuckDB session meanwhile.
// Callers wait only as long as their own budget allows; a fetch that outlives
// the caller still fills the cache for the next request.
const dataFetchTimeout = 800 * time.Millisecond

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
// Errors are cached too (a broken query stays broken until the project
// changes); timeouts are not. Concurrent requests for one name share one fetch.
type dataCache struct {
	log     logx.Logger
	mu      sync.Mutex
	gen     uint64
	cols    fetchCache[[]string]
	samples fetchCache[DatasetSample]
}

// fetchCache is one kind of cached value; dataCache.mu guards it.
type fetchCache[T any] struct {
	done    map[string]fetchResult[T]
	pending map[string]chan struct{}
}

type fetchResult[T any] struct {
	v   T
	err error
}

// invalidate drops every entry. Fetches still running finish but do not store.
func (c *dataCache) invalidate() {
	c.mu.Lock()
	c.gen++
	c.cols = fetchCache[[]string]{}
	c.samples = fetchCache[DatasetSample]{}
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
	if r, ok := fc.done[name]; ok {
		c.mu.Unlock()
		return r.v, r.err == nil
	}
	done, running := fc.pending[name]
	if !running {
		done = make(chan struct{})
		if fc.pending == nil {
			fc.pending = make(map[string]chan struct{})
		}
		fc.pending[name] = done
		go runFetch(ctx, c, fc, c.gen, name, done, fetch)
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
	r, ok := fc.done[name]
	return r.v, ok && r.err == nil
}

// runFetch runs one fetch for cachedFetch. invalidate may have replaced fc's
// maps meanwhile: the generation check keeps a stale result out, and only this
// fetch's own pending entry is removed.
func runFetch[T any](ctx context.Context, c *dataCache, fc *fetchCache[T], gen uint64, name string, done chan struct{}, fetch func(context.Context, string) (T, error)) {
	// The fetch must not die with the request that started it.
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dataFetchTimeout)
	var r fetchResult[T]
	defer func() {
		if p := recover(); p != nil {
			c.log.Errorf("dataset fetch for %q panicked: %v\n%s", name, p, debug.Stack())
			r = fetchResult[T]{err: fmt.Errorf("panic: %v", p)}
		}
		timedOut := errors.Is(r.err, context.DeadlineExceeded) || fctx.Err() != nil
		cancel()
		c.mu.Lock()
		if gen == c.gen && !timedOut {
			if fc.done == nil {
				fc.done = make(map[string]fetchResult[T])
			}
			fc.done[name] = r
		}
		if fc.pending[name] == done {
			delete(fc.pending, name)
		}
		c.mu.Unlock()
		close(done)
	}()
	r.v, r.err = fetch(fctx, name)
}
