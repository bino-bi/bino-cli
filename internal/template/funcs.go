package template

import (
	"fmt"
	"strings"
	"text/template"
)

// FuncMap returns the function map every bino template is rendered with.
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"yaml":      yamlQuote,
		"sqlString": sqlString,
		"title":     titleCase,
	}
}

// yamlQuote renders a value as a double-quoted YAML scalar. It is byte-for-byte
// identical to the historical init.quoteYAML helper (fmt.Sprintf("%q", v)) so
// scaffolds keep escaping user values correctly under text/template.
func yamlQuote(v string) string {
	return fmt.Sprintf("%q", v)
}

// sqlString escapes a value for embedding inside a single-quoted SQL literal.
// The template supplies the surrounding quotes, e.g. '{{ .Region | sqlString }}'.
func sqlString(v string) string {
	return strings.ReplaceAll(v, "'", "''")
}

// titleCase upper-cases the first ASCII letter of each word, used for derived
// defaults such as {{ .ReportName | title }}.
func titleCase(v string) string {
	var b strings.Builder
	atWordStart := true
	for _, r := range v {
		if atWordStart && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		b.WriteRune(r)
		atWordStart = r == ' ' || r == '\t' || r == '-' || r == '_'
	}
	return b.String()
}
