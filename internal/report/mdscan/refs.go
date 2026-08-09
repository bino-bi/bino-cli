// Package mdscan provides filesystem and content scanning for markdown
// document sources without pulling in the rendering stack. It is shared by
// the markdown renderer and the dependency-graph builder, which must stay
// lean (it is linked into the LSP daemon).
package mdscan

import "regexp"

// RefPatternSrc is the shared :ref[Kind:name]{caption="..."} pattern source.
// markdown's refext.go compiles it with a leading ^ for goldmark's inline
// parser (anchored at the trigger position); ScanRefs compiles it unanchored
// for whole-file scanning. Group order: 1=kind, 2=name, 3=caption.
const RefPatternSrc = `:ref\[([A-Za-z]+):([A-Za-z0-9_-]+)\](?:\{caption="([^"]*)"\})?`

var scanPattern = regexp.MustCompile(RefPatternSrc)

// Ref is one component reference found in markdown source.
type Ref struct {
	Kind string
	Name string
}

// ScanRefs returns the deduplicated component references found anywhere in
// content, in first-occurrence order. It is a plain text scan: refs inside
// fenced code blocks match too. Callers use the result to over-approximate
// dependencies, where a false positive only widens the affected set.
func ScanRefs(content []byte) []Ref {
	matches := scanPattern.FindAllSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[Ref]struct{}, len(matches))
	refs := make([]Ref, 0, len(matches))
	for _, m := range matches {
		r := Ref{Kind: string(m[1]), Name: string(m[2])}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		refs = append(refs, r)
	}
	return refs
}
