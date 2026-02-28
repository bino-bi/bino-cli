package web

import (
	"io/fs"
)

// ImportMapScript returns an HTML <script type="importmap"> block
// containing the embedded import map JSON. The result is safe to
// inject before any <script type="module"> tags.
func ImportMapScript() string {
	data, err := fs.ReadFile(assets, "shared/import-map.json")
	if err != nil {
		return ""
	}
	return `<script type="importmap">` + string(data) + `</script>`
}
