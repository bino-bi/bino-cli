package claudeplugin

import (
	"strings"
	"testing"
)

func TestSkillsParse(t *testing.T) {
	skills, err := Skills()
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	want := []string{
		"bino-authoring", "bino-concepts", "bino-data-modeling", "bino-ibcs",
		"bino-orchestration", "bino-requirements", "bino-validation-loop",
	}
	if len(skills) != len(want) {
		t.Fatalf("got %d skills, want %d", len(skills), len(want))
	}
	for i, s := range skills {
		if s.Name != want[i] {
			t.Errorf("skill %d: name = %q, want %q", i, s.Name, want[i])
		}
		if s.Description == "" {
			t.Errorf("%s: empty description", s.Name)
		}
		if !strings.HasPrefix(s.Body, "# ") {
			t.Errorf("%s: body does not start with a heading: %.40q", s.Name, s.Body)
		}
	}
}

func TestIBCSSkillIncludesReferences(t *testing.T) {
	skills, err := Skills()
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	for _, s := range skills {
		if s.Name != "bino-ibcs" {
			continue
		}
		for _, heading := range []string{"## 1. The SUCCESS formula", "## Scenarios"} {
			if !strings.Contains(s.Body, heading) {
				t.Errorf("bino-ibcs body missing reference heading %q", heading)
			}
		}
		return
	}
	t.Fatal("bino-ibcs not found")
}
