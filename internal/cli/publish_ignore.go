package cli

import (
	"os"
	"path/filepath"

	gitignore "github.com/sabhiram/go-gitignore"
	"gopkg.in/yaml.v3"
)

// publishIgnore applies the project's .bnignore to the publish walk. A file
// bino itself never loads must never be published, so publish honors the same
// list the manifest loader does — mirrored here rather than shared, because
// the loader's helpers are unexported and its walk is out of scope.
type publishIgnore struct {
	rules *gitignore.GitIgnore
}

// loadPublishIgnore compiles <projectRoot>/.bnignore. A missing or unparsable
// file ignores nothing, exactly as in the loader — publish must not start
// refusing to run over a file the rest of the CLI tolerates.
func loadPublishIgnore(projectRoot string) *publishIgnore {
	path := filepath.Join(projectRoot, ".bnignore")
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return &publishIgnore{}
	}
	rules, err := gitignore.CompileIgnoreFile(path)
	if err != nil {
		return &publishIgnore{}
	}
	return &publishIgnore{rules: rules}
}

// matches reports whether a project-relative, slash-form path is ignored.
func (p *publishIgnore) matches(slashRel string) bool {
	if p == nil || p.rules == nil {
		return false
	}
	return p.rules.MatchesPath(slashRel) || p.rules.MatchesPath(slashRel+"/")
}

// documentKinds reads the kind of every document in a canonicalized stream.
// The documents have already been through the digest canonicalizer, so a
// failure here would be a bug rather than bad input; an unreadable envelope
// simply contributes no kind and the registry's gate has the final say.
func documentKinds(docs [][]byte) []string {
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		var envelope struct {
			Kind string `yaml:"kind"`
		}
		if err := yaml.Unmarshal(d, &envelope); err != nil {
			continue
		}
		out = append(out, envelope.Kind)
	}
	return out
}
