package registry

import (
	"strings"
	"testing"
)

func compatBody(labels string) []byte {
	return []byte(`{"apiVersion":"bino.bi/v1alpha1","kind":"Text","metadata":{"name":"@acme/x","labels":{` + labels + `}},"spec":{}}`)
}

func TestCompatWarnings(t *testing.T) {
	t.Run("in range is silent", func(t *testing.T) {
		body := compatBody(`"registry.compat.cli":">=1.0.0 <3.0.0","registry.compat.engine":">=1.0.0"`)
		if got := CompatWarnings("@acme/x", "1.0.0", body, "1.4.0", "v1.2.0"); len(got) != 0 {
			t.Errorf("unexpected warnings: %v", got)
		}
	})

	t.Run("out of range warns", func(t *testing.T) {
		body := compatBody(`"registry.compat.cli":">=2.0.0"`)
		got := CompatWarnings("@acme/x", "1.0.0", body, "1.4.0", "")
		if len(got) != 1 || !strings.Contains(got[0], "registry.compat.cli") || !strings.Contains(got[0], "1.4.0") {
			t.Errorf("warnings = %v", got)
		}
	})

	t.Run("both out of range", func(t *testing.T) {
		body := compatBody(`"registry.compat.cli":">=2.0.0","registry.compat.engine":">=9.0.0"`)
		if got := CompatWarnings("@acme/x", "1.0.0", body, "1.0.0", "v1.0.0"); len(got) != 2 {
			t.Errorf("warnings = %v", got)
		}
	})

	t.Run("no labels is silent", func(t *testing.T) {
		if got := CompatWarnings("@acme/x", "1.0.0", compatBody(``), "1.0.0", "v1.0.0"); len(got) != 0 {
			t.Errorf("warnings = %v", got)
		}
	})

	t.Run("unknown engine version skips engine check", func(t *testing.T) {
		body := compatBody(`"registry.compat.engine":">=9.0.0"`)
		if got := CompatWarnings("@acme/x", "1.0.0", body, "1.0.0", ""); len(got) != 0 {
			t.Errorf("warnings = %v", got)
		}
	})

	t.Run("unparsable range skips silently", func(t *testing.T) {
		body := compatBody(`"registry.compat.cli":"not a range"`)
		if got := CompatWarnings("@acme/x", "1.0.0", body, "1.0.0", ""); len(got) != 0 {
			t.Errorf("warnings = %v", got)
		}
	})
}
