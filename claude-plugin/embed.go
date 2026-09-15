// Package claudeplugin embeds the Claude Code plugin's skills so the MCP
// server can serve the same bino/IBCS knowledge to any client from the binary.
// The plugin itself keeps reading these files from disk; nothing here changes
// how Claude Code loads them.
package claudeplugin

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed skills/*/SKILL.md skills/*/references/*.md
var skillsFS embed.FS

// Skill is one plugin skill: the frontmatter name and description, and the
// markdown body with every file under the skill's references/ appended so the
// text stands on its own.
type Skill struct {
	Name        string
	Description string
	Body        string
}

// Skills parses every embedded skill, sorted by directory name.
func Skills() ([]Skill, error) {
	files, err := fs.Glob(skillsFS, "skills/*/SKILL.md")
	if err != nil {
		return nil, err
	}
	out := make([]Skill, 0, len(files))
	for _, file := range files {
		s, err := parseSkill(file)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		out = append(out, s)
	}
	return out, nil
}

func parseSkill(file string) (Skill, error) {
	raw, err := skillsFS.ReadFile(file)
	if err != nil {
		return Skill{}, err
	}
	content := string(raw)
	const fence = "---\n"
	if !strings.HasPrefix(content, fence) {
		return Skill{}, fmt.Errorf("missing frontmatter")
	}
	rest := content[len(fence):]
	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		return Skill{}, fmt.Errorf("unterminated frontmatter")
	}
	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), &meta); err != nil {
		return Skill{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	if meta.Name == "" || meta.Description == "" {
		return Skill{}, fmt.Errorf("frontmatter needs name and description")
	}
	body := strings.TrimSpace(rest[end+len("\n"+fence):])

	refs, err := fs.Glob(skillsFS, path.Join(path.Dir(file), "references", "*.md"))
	if err != nil {
		return Skill{}, err
	}
	for _, ref := range refs {
		data, err := skillsFS.ReadFile(ref)
		if err != nil {
			return Skill{}, err
		}
		body += "\n\n---\n\n" + strings.TrimSpace(string(data))
	}
	return Skill{Name: meta.Name, Description: strings.TrimSpace(meta.Description), Body: body}, nil
}
