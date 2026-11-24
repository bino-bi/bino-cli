package lsp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sync"

	"bino.bi/bino/internal/schema"
)

// namePatterns are the metadata.name rules of the embedded document schema:
// the general pattern, and $defs.sqlIdentifier, which a DataSource name must
// also match because it becomes a DuckDB view name.
var namePatterns = sync.OnceValues(func() (general, dataSource *regexp.Regexp) {
	var doc struct {
		Defs struct {
			Metadata struct {
				Properties struct {
					Name struct {
						Pattern string `json:"pattern"`
					} `json:"name"`
				} `json:"properties"`
			} `json:"metadata"`
			SQLIdentifier struct {
				Pattern string `json:"pattern"`
			} `json:"sqlIdentifier"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schema.DocumentSchemaBytes(), &doc); err != nil {
		return nil, nil // the embedded schema is tested to parse
	}
	compile := func(p string) *regexp.Regexp {
		if p == "" {
			return nil
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return nil
		}
		return re
	}
	return compile(doc.Defs.Metadata.Properties.Name.Pattern), compile(doc.Defs.SQLIdentifier.Pattern)
})

// invalidNameReason returns why newName is not a valid metadata.name for kind,
// or "" when it is valid.
func invalidNameReason(kind, newName string) string {
	general, dataSource := namePatterns()
	if general != nil && !general.MatchString(newName) {
		return fmt.Sprintf("%q is not a valid name: use letters, digits, '-' and '_' (pattern %s)", newName, general)
	}
	if kind == "DataSource" && dataSource != nil && !dataSource.MatchString(newName) {
		return fmt.Sprintf("%q is not a valid DataSource name: it becomes a DuckDB view name, so use a lowercase SQL identifier or '@scope/name' (pattern %s)", newName, dataSource)
	}
	return ""
}
