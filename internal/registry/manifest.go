package registry

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"bino.bi/bino/internal/pathutil"
)

// dependenciesHeaderRe matches the [dependencies] table header line.
var dependenciesHeaderRe = regexp.MustCompile(`(?m)^[ \t]*\[dependencies\][ \t]*(?:#.*)?$`)

// SetDependency records name = ref in bino.toml's [dependencies] table,
// editing textually so the user's formatting and comments survive. The table
// is created at EOF when absent. Before writing, the new content is re-parsed
// and the change asserted, so a bino.toml this editor cannot handle safely
// (e.g. an inline dependencies table) fails cleanly instead of corrupting.
func SetDependency(projectRoot, name, ref string) error {
	return editDependencies(projectRoot, name, func(content string) string {
		line := fmt.Sprintf("%q = %q", name, ref)
		header := dependenciesHeaderRe.FindStringIndex(content)
		if header == nil {
			if content != "" && !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			return content + "\n[dependencies]\n" + line + "\n"
		}
		start, end := tableExtent(content, header[1])
		if key := keyLineRe(name).FindStringIndex(content[start:end]); key != nil {
			return content[:start+key[0]] + line + content[start+key[1]:]
		}
		insert := lastContentEnd(content, start, end)
		return content[:insert] + "\n" + line + content[insert:]
	}, func(deps map[string]string) error {
		if deps[name] != ref {
			return fmt.Errorf("edit did not take effect")
		}
		return nil
	})
}

// RemoveDependency deletes name's entry from bino.toml's [dependencies]
// table. A missing entry is a no-op.
func RemoveDependency(projectRoot, name string) error {
	return editDependencies(projectRoot, name, func(content string) string {
		header := dependenciesHeaderRe.FindStringIndex(content)
		if header == nil {
			return content
		}
		start, end := tableExtent(content, header[1])
		key := keyLineRe(name).FindStringIndex(content[start:end])
		if key == nil {
			return content
		}
		lineEnd := start + key[1]
		if lineEnd < len(content) && content[lineEnd] == '\n' {
			lineEnd++
		}
		return content[:start+key[0]] + content[lineEnd:]
	}, func(deps map[string]string) error {
		if _, ok := deps[name]; ok {
			return fmt.Errorf("edit did not take effect")
		}
		return nil
	})
}

func editDependencies(projectRoot, name string, edit func(string) string, check func(map[string]string) error) error {
	path := pathutil.ProjectConfigPath(projectRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	next := edit(string(data))
	if next == string(data) {
		return nil
	}
	var parsed struct {
		Dependencies map[string]string `toml:"dependencies"`
	}
	if err := toml.Unmarshal([]byte(next), &parsed); err != nil {
		return fmt.Errorf("cannot safely edit %s (dependency %q): %w — edit the [dependencies] table manually", path, name, err)
	}
	if err := check(parsed.Dependencies); err != nil {
		return fmt.Errorf("cannot safely edit %s (dependency %q): %v — edit the [dependencies] table manually", path, name, err)
	}
	if err := writeFileAtomic(path, []byte(next)); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// keyLineRe matches the full line assigning the (always-quoted) key.
func keyLineRe(name string) *regexp.Regexp {
	q := regexp.QuoteMeta(name)
	return regexp.MustCompile(`(?m)^[ \t]*(?:"` + q + `"|'` + q + `')[ \t]*=.*$`)
}

// tableExtent returns the [start, end) byte range of a table's body, from
// just past the header line to the next table header or EOF.
func tableExtent(content string, headerEnd int) (int, int) {
	start := headerEnd
	if start < len(content) && content[start] == '\n' {
		start++
	}
	if next := regexp.MustCompile(`(?m)^[ \t]*\[`).FindStringIndex(content[start:]); next != nil {
		return start, start + next[0]
	}
	return start, len(content)
}

// lastContentEnd returns the offset just past the last non-blank line in
// [start, end), or just past the header when the table body is empty, so an
// inserted line lands inside the table rather than after trailing blanks.
func lastContentEnd(content string, start, end int) int {
	insert := start
	if start > 0 {
		insert = start - 1 // sit on the header's newline
	}
	for _, loc := range regexp.MustCompile(`(?m)^.*\S.*$`).FindAllStringIndex(content[start:end], -1) {
		insert = start + loc[1]
	}
	return insert
}
