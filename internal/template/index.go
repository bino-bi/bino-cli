package template

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	indexFile = "index.json"
	// defaultRefToken keys the discovered default-branch SHA in the cache index.
	defaultRefToken = "__default__"
)

// cacheIndex maps a repo's symbolic refs to resolved commit SHAs, so a
// previously-fetched ref resolves offline without re-hitting the network.
type cacheIndex struct {
	Refs map[string]string `json:"refs"`
}

// loadIndex reads a repo's index.json. A missing file yields an empty index.
func loadIndex(repoDir string) *cacheIndex {
	idx := &cacheIndex{Refs: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(repoDir, indexFile))
	if err != nil {
		return idx
	}
	if err := json.Unmarshal(data, idx); err != nil || idx.Refs == nil {
		idx.Refs = map[string]string{}
	}
	return idx
}

func (i *cacheIndex) set(ref, sha string) {
	if ref == "" {
		return
	}
	i.Refs[ref] = sha
}

// saveIndex persists a repo's index.json. Callers must hold the repo flock.
func saveIndex(repoDir string, idx *cacheIndex) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(repoDir, indexFile), data, 0o644) //nolint:gosec // G306: index metadata needs standard read perms
}
