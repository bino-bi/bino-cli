package lsp

import (
	"strings"

	"bino.bi/bino/internal/report/config"
)

// binoRootKeys are the only top-level keys the document schema allows.
var binoRootKeys = map[string]bool{"apiVersion": true, "kind": true, "metadata": true, "spec": true}

// isBinoBuffer reports whether a buffer should get bino editor features. The
// client also opens docker-compose, CI and k8s YAML with this server, and ':'
// is a completion trigger, so a foreign file must stay silent.
//
// A buffer is bino when any document passes config.IsBinoHeader. Otherwise it
// is foreign when any document shows foreign evidence: a non-bino apiVersion, a
// root sequence, or a top-level key the schema does not allow. A buffer with
// neither (empty, comments only, or a header still being typed such as `kind: `
// or `apiVersion: bin`) stays bino so a new file gets the kind and scaffold
// completion.
//
// It reads column-0 `key: value` lines instead of parsing, so a half-typed,
// syntax-broken buffer is judged the same way as a valid one.
func isBinoBuffer(text string) bool {
	foreign := false
	var apiVersion, kind string
	var otherKeys bool
	// endDoc judges the document collected so far and resets for the next one.
	endDoc := func() bool {
		bino := config.IsBinoHeader(apiVersion, kind, nil)
		// A half-typed apiVersion is a prefix of "bino.bi/", so the order is intended.
		typingAPIVersion := strings.HasPrefix("bino.bi/", apiVersion) //nolint:gocritic
		if !bino && (!typingAPIVersion || otherKeys) {
			foreign = true
		}
		apiVersion, kind, otherKeys = "", "", false
		return bino
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "---") {
			if endDoc() {
				return true
			}
			continue
		}
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line == "..." {
			continue
		}
		if line == "-" || strings.HasPrefix(line, "- ") {
			otherKeys = true // a root sequence (Ansible and the like)
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.Trim(strings.TrimSpace(key), `"'`)
		value = strings.TrimSpace(value)
		if i := strings.Index(value, " #"); i >= 0 {
			value = strings.TrimSpace(value[:i])
		}
		value = strings.Trim(value, `"'`)
		switch {
		case key == "apiVersion":
			apiVersion = value
		case key == "kind":
			kind = value
		case !binoRootKeys[key]:
			otherKeys = true
		}
	}
	return endDoc() || !foreign
}
