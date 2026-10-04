package schema

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const liveReportArtefactDoc = "../../docs/src/content/docs/reference/live-report-artefact.mdx"

var (
	docYAMLFence   = regexp.MustCompile("(?s)```yaml\n(.*?)```")
	docDataSetKind = regexp.MustCompile(`(?m)^kind: DataSet$`)
	docName        = regexp.MustCompile(`(?m)^  name: (\S+)`)
)

// TestValidate_LiveReportArtefactDocDataSets is a regression gate: no other CI
// step validates the YAML in the docs, so an example can drift from the schema.
func TestValidate_LiveReportArtefactDocDataSets(t *testing.T) {
	raw, err := os.ReadFile(liveReportArtefactDoc)
	if err != nil {
		t.Fatalf("read %s: %v", liveReportArtefactDoc, err)
	}

	seen := 0
	for _, block := range docYAMLFence.FindAllStringSubmatch(string(raw), -1) {
		for _, doc := range strings.Split(block[1], "\n---\n") {
			if !docDataSetKind.MatchString(doc) {
				continue
			}
			seen++
			name := "unnamed"
			if m := docName.FindStringSubmatch(doc); m != nil {
				name = m[1]
			}
			t.Run(name, func(t *testing.T) {
				if err := Validate([]byte(doc)); err != nil {
					t.Errorf("DataSet %s in %s:\n%v", name, liveReportArtefactDoc, err)
				}
			})
		}
	}
	if seen == 0 {
		t.Fatalf("no DataSet example found in %s", liveReportArtefactDoc)
	}
}
