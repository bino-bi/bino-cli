package engine

import (
	"os"
	"regexp"
	"testing"
)

// The template engine baked into the container image is pinned in two files no
// Go code ever reads, so nothing stopped them drifting below
// SupportedEngineRanges — which is what happened when the alpha.19 floor landed
// in v0.91.0 and neither pin moved. Every image published from then on shipped
// an engine its own CLI rejects at render time. This test is the guard.
var (
	dockerfilePin = regexp.MustCompile(`(?m)^ARG ENGINE_VERSION=(\S+)`)
	workflowPin   = regexp.MustCompile(`(?m)^\s+ENGINE_VERSION:\s*"([^"]+)"`)
)

func TestDockerEngineVersionPin(t *testing.T) {
	pins := []struct{ where, version string }{
		{"Dockerfile", readPin(t, "../../Dockerfile", dockerfilePin)},
		{".github/workflows/release.yml", readPin(t, "../../.github/workflows/release.yml", workflowPin)},
	}

	// The workflow passes its value as a build-arg, so the Dockerfile default is
	// only the local-build fallback — the two diverge silently.
	if pins[0].version != pins[1].version {
		t.Errorf("ENGINE_VERSION differs: %s has %q, %s has %q",
			pins[0].where, pins[0].version, pins[1].where, pins[1].version)
	}
	for _, pin := range pins {
		if err := CheckCompatibility(pin.version); err != nil {
			t.Errorf("%s pins engine %s, which this CLI does not support: %v", pin.where, pin.version, err)
		}
	}
}

func readPin(t *testing.T, path string, re *regexp.Regexp) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := re.FindSubmatch(b)
	if m == nil {
		t.Fatalf("no ENGINE_VERSION pin matching %s in %s", re, path)
	}
	return string(m[1])
}
