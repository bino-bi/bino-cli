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

// SkillFile is one file of a skill directory, by its path relative to the
// skill root (SKILL.md, references/ibcs-standard.md).
type SkillFile struct {
	Path string
	Data []byte
}

// SkillDir is a skill as a directory, for the MCP Skills extension: its
// frontmatter verbatim and every embedded file, SKILL.md first.
type SkillDir struct {
	Name        string
	Frontmatter map[string]any
	Files       []SkillFile
}

// SkillDirs returns every embedded skill as a directory, sorted by name.
func SkillDirs() ([]SkillDir, error) {
	files, err := fs.Glob(skillsFS, "skills/*/SKILL.md")
	if err != nil {
		return nil, err
	}
	out := make([]SkillDir, 0, len(files))
	for _, file := range files {
		root := path.Dir(file)
		raw, err := skillsFS.ReadFile(file)
		if err != nil {
			return nil, err
		}
		fm, err := frontmatter(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		d := SkillDir{Name: path.Base(root), Frontmatter: fm, Files: []SkillFile{{Path: "SKILL.md", Data: raw}}}
		if name, _ := fm["name"].(string); name != d.Name {
			return nil, fmt.Errorf("%s: frontmatter name %q differs from the directory", file, name)
		}
		err = fs.WalkDir(skillsFS, root, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || p == file {
				return err
			}
			data, err := skillsFS.ReadFile(p)
			if err != nil {
				return err
			}
			d.Files = append(d.Files, SkillFile{Path: strings.TrimPrefix(p, root+"/"), Data: data})
			return nil
		})
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// frontmatter parses the YAML frontmatter of a SKILL.md into a map, every
// field as written.
func frontmatter(content string) (map[string]any, error) {
	const fence = "---\n"
	if !strings.HasPrefix(content, fence) {
		return nil, fmt.Errorf("missing frontmatter")
	}
	rest := content[len(fence):]
	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter")
	}
	fm := map[string]any{}
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}
	if fm["name"] == nil || fm["description"] == nil {
		return nil, fmt.Errorf("frontmatter needs name and description")
	}
	return fm, nil
}
