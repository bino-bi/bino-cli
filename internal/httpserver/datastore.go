package httpserver

import "sync"

// DataKindDatasource and DataKindDataset are the kind values accepted by
// dataStore.Put / Get. They match the URL path segments under
// /__bino/data/{kind}/{name}.
const (
	DataKindDatasource = "datasource"
	DataKindDataset    = "dataset"
)

// defaultDataKeep is the default number of hashes retained per (kind,name).
// Keeping more than one absorbs the in-flight refresh case where a new
// rendered HTML has registered a fresh hash while the previous page is still
// fetching the old one (preview SSE refresh, two concurrent serve requests).
const defaultDataKeep = 3

// dataKey identifies a registered payload by component kind and metadata name.
type dataKey struct {
	Kind string
	Name string
}

// dataStore holds JSON payloads for bn-datasource / bn-dataset URL mode and
// serves them by (kind, name, hash). Bodies are immutable after Put; only the
// most recent `keep` hashes per (kind,name) are retained.
type dataStore struct {
	mu      sync.RWMutex
	entries map[dataKey]*dataVersions
	keep    int
}

// dataVersions retains the last N hash→body entries for one (kind,name) in
// insertion order. Insertion order is enough for "most recent N" because Put
// appends and trims from the front.
type dataVersions struct {
	order []string          // hashes, oldest first
	body  map[string][]byte // hash -> body
}

func newDataStore(keep int) *dataStore {
	if keep <= 0 {
		keep = defaultDataKeep
	}
	return &dataStore{
		entries: make(map[dataKey]*dataVersions),
		keep:    keep,
	}
}

// Put registers body under (kind,name,hash). If the same hash is already
// registered, the body is left unchanged (hashes are content-addressed). When
// the number of retained hashes for a (kind,name) exceeds keep, the oldest is
// evicted.
func (s *dataStore) Put(kind, name, hash string, body []byte) {
	if kind == "" || name == "" || hash == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	key := dataKey{Kind: kind, Name: name}
	versions, ok := s.entries[key]
	if !ok {
		versions = &dataVersions{body: make(map[string][]byte)}
		s.entries[key] = versions
	}
	if _, exists := versions.body[hash]; exists {
		return
	}
	versions.body[hash] = body
	versions.order = append(versions.order, hash)
	for len(versions.order) > s.keep {
		oldest := versions.order[0]
		versions.order = versions.order[1:]
		delete(versions.body, oldest)
	}
}

// Get returns the body registered for (kind,name,hash) and whether it was found.
func (s *dataStore) Get(kind, name, hash string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	versions, ok := s.entries[dataKey{Kind: kind, Name: name}]
	if !ok {
		return nil, false
	}
	body, ok := versions.body[hash]
	return body, ok
}
