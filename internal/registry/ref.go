// Package registry implements the native client side of the bino package
// registry: parsing package specs, resolving dependency closures, verifying
// content digests, and materializing packages under .bino/registry.
package registry

import (
	"fmt"
	"regexp"
	"strings"
)

// Grammars shared with the registry server (internal/domain): a package name
// is "@scope/name" where each segment matches tokenRe. pinRe mirrors the
// server's resolve-route disambiguation exactly: a semver-shaped ref names an
// exact version, anything else names a tag.
var (
	tokenRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	pinRe   = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
)

// Spec is a parsed package spec "@scope/name[@ref]".
type Spec struct {
	Name  string // full identity "@scope/name"
	Scope string // scope segment, no "@"
	Base  string // name segment
	Ref   string // exact version, tag name, or "" (follow the default tag)
}

// IsPin reports whether ref names an exact version rather than a tag, using
// the same classification as the server's resolve route.
func IsPin(ref string) bool { return pinRe.MatchString(ref) }

// ParseName splits a package name "@scope/name" into its segments.
func ParseName(name string) (scope, base string, err error) {
	rest, ok := strings.CutPrefix(name, "@")
	if !ok {
		return "", "", fmt.Errorf("invalid package name %q: must start with \"@\"", name)
	}
	scope, base, ok = strings.Cut(rest, "/")
	if !ok || !tokenRe.MatchString(scope) || !tokenRe.MatchString(base) {
		return "", "", fmt.Errorf("invalid package name %q: expected @scope/name with lowercase a-z0-9_- segments", name)
	}
	return scope, base, nil
}

// ParseSpec parses "@scope/name[@ref]" as used by command arguments and
// dependency references. The ref, when present, must be a semver version or a
// tag token.
func ParseSpec(s string) (Spec, error) {
	name, ref := s, ""
	// The identity always starts with "@"; a later "@" separates the ref.
	if len(s) > 1 && s[0] == '@' {
		if i := strings.IndexByte(s[1:], '@'); i >= 0 {
			name, ref = s[:i+1], s[i+2:]
			if ref == "" {
				return Spec{}, fmt.Errorf("invalid package spec %q: empty ref after \"@\"", s)
			}
		}
	}
	scope, base, err := ParseName(name)
	if err != nil {
		return Spec{}, err
	}
	if ref != "" && !IsPin(ref) && !tokenRe.MatchString(ref) {
		return Spec{}, fmt.Errorf("invalid package spec %q: ref must be an exact version (1.2.3) or a tag name", s)
	}
	return Spec{Name: name, Scope: scope, Base: base, Ref: ref}, nil
}
