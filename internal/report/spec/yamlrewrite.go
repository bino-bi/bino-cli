package spec

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// rewriteDocument applies mutate to the root mapping of the 1-based document
// position and writes back only that document's byte range. Every other
// document, every "---" line, and the blank lines around the edited document
// stay byte-identical; the edited document keeps its indentation width and
// line endings. Shapes the range splitter does not handle fall back to
// re-encoding the whole file.
func rewriteDocument(content string, position int, mutate func(root *yaml.Node) error) (full, edited string, err error) {
	docs, err := decodeDocuments(content)
	if err != nil {
		return "", "", err
	}
	root, err := documentRoot(docs, position)
	if err != nil {
		return "", "", err
	}

	if r, ok := documentRange(content, position, len(docs)); ok {
		body := content[r.start:r.end]
		a, b := coreBounds(body)
		core := body[a:b]
		lf := strings.ReplaceAll(core, "\r\n", "\n")
		// Decoding the range alone keeps comments in their own document: on a
		// whole stream yaml.v3 attaches comments across "---" lines.
		segDocs, decErr := decodeDocuments(lf)
		if decErr == nil {
			if segRoot, rootErr := documentRoot(segDocs, 1); rootErr == nil && len(segDocs) == 1 {
				if err := mutate(segRoot); err != nil {
					return "", "", err
				}
				edited, err = encodeDocuments(segDocs, detectIndent(lf))
				if err != nil {
					return "", "", err
				}
				out := edited
				if !strings.HasSuffix(core, "\n") {
					out = strings.TrimSuffix(out, "\n")
				}
				if strings.Contains(core, "\r\n") {
					out = strings.ReplaceAll(out, "\n", "\r\n")
				}
				full = content[:r.start] + body[:a] + out + body[b:] + content[r.end:]
				return full, edited, nil
			}
		}
	}

	if err := mutate(root); err != nil {
		return "", "", err
	}
	full, err = encodeDocuments(docs, 2)
	if err != nil {
		return "", "", err
	}
	edited, err = encodeDocuments(docs[position-1:position], 2)
	if err != nil {
		return "", "", err
	}
	return full, edited, nil
}

// byteRange is a [start, end) byte range of content.
type byteRange struct{ start, end int }

// documentRange returns the byte range of the 1-based document position,
// excluding its "---" lines. ok is false when the file uses a shape the line
// splitter does not handle (content after "---", "..." end markers,
// directives) or when its document count disagrees with yaml.v3's (count).
func documentRange(content string, position, count int) (byteRange, bool) {
	var ranges []byteRange
	start := 0
	for off := 0; off < len(content); {
		next := len(content)
		if nl := strings.IndexByte(content[off:], '\n'); nl >= 0 {
			next = off + nl + 1
		}
		line := strings.TrimRight(content[off:next], "\r\n")
		switch {
		case isMarker(line, "---"):
			if rest := strings.TrimSpace(line[3:]); rest != "" && !strings.HasPrefix(rest, "#") {
				return byteRange{}, false
			}
			ranges = append(ranges, byteRange{start, off})
			start = next
		case isMarker(line, "..."), strings.HasPrefix(line, "%"):
			return byteRange{}, false
		}
		off = next
	}
	ranges = append(ranges, byteRange{start, len(content)})

	// Text before the first "---" is a document only if it has content; every
	// "---" starts one, even an empty one.
	first, err := decodeDocuments(content[ranges[0].start:ranges[0].end])
	if err != nil {
		return byteRange{}, false
	}
	if len(first) == 0 {
		ranges = ranges[1:]
	}
	if len(ranges) != count || position < 1 || position > count {
		return byteRange{}, false
	}
	return ranges[position-1], true
}

// isMarker reports whether line starts with the document marker m at column 0.
func isMarker(line, m string) bool {
	if !strings.HasPrefix(line, m) {
		return false
	}
	return len(line) == len(m) || line[len(m)] == ' ' || line[len(m)] == '\t'
}

// coreBounds returns the offsets of body without its leading and trailing
// whitespace-only lines. The trailing bound includes the line break of the
// last non-blank line.
func coreBounds(body string) (a, b int) {
	for a < len(body) {
		nl := strings.IndexByte(body[a:], '\n')
		if nl < 0 || strings.TrimSpace(body[a:a+nl]) != "" {
			break
		}
		a += nl + 1
	}
	b = len(strings.TrimRight(body, " \t\r\n"))
	if b < a {
		return a, a
	}
	if nl := strings.IndexByte(body[b:], '\n'); nl >= 0 {
		return a, b + nl + 1
	}
	return a, len(body)
}

// detectIndent returns the smallest indentation of a content line, which is
// the nesting width the document was written with. yaml.v3 accepts 2 to 9;
// anything else falls back to 2.
func detectIndent(doc string) int {
	indent := 0
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		n := len(line) - len(trimmed)
		if n == 0 || strings.TrimSpace(trimmed) == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if indent == 0 || n < indent {
			indent = n
		}
	}
	if indent < 2 || indent > 9 {
		return 2
	}
	return indent
}
