package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"bino.bi/bino/internal/report/render"
)

// assetRefPrefix marks a value that explicitly names an Asset document. The
// renderer strips it before looking the name up (see render.CollectAssetRefs).
const assetRefPrefix = "asset:"

// imageFieldByKind maps each kind to the spec field that carries an image
// reference. Every one of these is written to the HTML verbatim, so the value
// has to resolve to an Asset document or to a URL the browser can fetch.
var imageFieldByKind = map[string]string{
	"LayoutPage": "messageImage",
	"LayoutCard": "titleImage",
	"Image":      "source",
}

// markdownFieldsByKind maps each kind to the spec fields rendered as Markdown
// with asset resolution enabled, where an "asset:" image destination resolves
// against the same Asset documents.
var markdownFieldsByKind = map[string][]string{
	"LayoutPage": {"messageText", "titleBusinessUnit"},
	"LayoutCard": {"titleBusinessUnit"},
	"Text":       {"value"},
}

// urlSchemeRe matches an absolute URL like "https://host/x.png".
var urlSchemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*://`)

// assetReferenceUndefined warns when an image reference cannot resolve: it names
// no Asset document, is not a URL the browser can fetch, and — for values that
// look like a file path — points at a file that is not on disk.
//
// Nothing validates these today. The renderer copies the value straight into the
// HTML attribute, and the template engine falls back to using an unresolved name
// as a raw URL, so a typo silently yields a broken image instead of an error.
var assetReferenceUndefined = Rule{
	ID:   "asset-reference-undefined",
	Name: "Asset Reference Undefined",
	Description: "Image references (LayoutPage 'messageImage', LayoutCard 'titleImage', Image 'source', " +
		"and 'asset:' references in Markdown) must name a declared Asset document or a reachable URL.",
	Check: func(_ context.Context, docs []Document) []Finding {
		assets := assetNames(docs)

		var findings []Finding
		for _, doc := range docs {
			var root any
			if err := json.Unmarshal(doc.Raw, &root); err != nil {
				continue // Schema validation reports malformed documents.
			}

			add := func(path, message string) {
				findings = append(findings, Finding{
					RuleID:  "asset-reference-undefined",
					Message: message,
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    path,
				})
			}

			walkNodes(root, "", func(node map[string]any, path string) {
				kind, _ := node["kind"].(string)
				if kind == "" {
					return
				}
				componentSpec, _ := node["spec"].(map[string]any)

				if field, ok := imageFieldByKind[kind]; ok {
					value := strings.TrimSpace(stringField(componentSpec, field))
					if problem := checkImageReference(value, assets, doc.File); problem != "" {
						add(joinLintPath(path, "spec."+field), problem)
					}
				}

				for _, field := range markdownFieldsByKind[kind] {
					for _, name := range render.CollectAssetRefs(stringField(componentSpec, field)) {
						if !assets[name] {
							add(joinLintPath(path, "spec."+field), fmt.Sprintf(
								"Markdown image references asset %q, but no Asset document with that name is defined; "+
									"the image will not render",
								name,
							))
						}
					}
				}
			})
		}
		return findings
	},
}

// checkImageReference classifies an image reference and returns the problem with
// it, or "" when it resolves. An empty value is fine: the renderer omits the
// attribute and the engine renders no image element at all.
func checkImageReference(value string, assets map[string]bool, docFile string) string {
	switch {
	case value == "":
		return ""
	case strings.Contains(value, "${"):
		// Unresolved environment variable; the missing-env-var diagnostic owns it.
		return ""
	case strings.HasPrefix(value, assetRefPrefix):
		name := strings.TrimSpace(strings.TrimPrefix(value, assetRefPrefix))
		if assets[name] {
			return ""
		}
		return fmt.Sprintf(
			"image references asset %q, but no Asset document with that name is defined; the image will not render",
			name,
		)
	case assets[value]:
		return ""
	case isURL(value):
		return ""
	case looksLikePath(value):
		resolved := value
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(docFile), value)
		}
		if info, err := os.Stat(resolved); err != nil {
			return fmt.Sprintf("image file %q does not exist (resolved to %s)", value, resolved)
		} else if info.IsDir() {
			return fmt.Sprintf("image path %q is a directory, not a file", value)
		}
		return ""
	default:
		return fmt.Sprintf(
			"image references asset %q, but no Asset document with that name is defined; "+
				"declare an Asset named %q or use an absolute URL",
			value, value,
		)
	}
}

// isURL reports whether a value is something the browser can fetch on its own:
// an absolute URL, a data URI, or a root-relative path.
func isURL(value string) bool {
	return urlSchemeRe.MatchString(value) ||
		strings.HasPrefix(value, "data:") ||
		strings.HasPrefix(value, "/")
}

// looksLikePath reports whether a value reads as a file path rather than an
// Asset name: it has a directory separator or a file extension.
func looksLikePath(value string) bool {
	return strings.ContainsAny(value, "/\\") || filepath.Ext(value) != ""
}

// assetSourceMissing warns when an Asset's local source file is not on disk.
//
// The renderer stats the file and fails the build (render.resolveLocalAssetPath),
// but lint, the daemon and the editor never render, so today a deleted image
// stays invisible until the build breaks.
var assetSourceMissing = Rule{
	ID:          "asset-source-missing",
	Name:        "Asset Source Missing",
	Description: "An Asset's 'spec.source.localPath' must point at an existing file, resolved relative to the manifest.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		for _, doc := range docs {
			if doc.Kind != "Asset" {
				continue
			}
			var payload struct {
				Spec struct {
					Source struct {
						LocalPath string `json:"localPath"`
					} `json:"source"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(doc.Raw, &payload); err != nil {
				continue // Schema validation reports malformed documents.
			}

			localPath := strings.TrimSpace(payload.Spec.Source.LocalPath)
			if localPath == "" {
				continue // inlineBase64 or remoteURL source.
			}

			// Mirrors render.resolveLocalAssetPath: relative to the manifest file.
			resolved := localPath
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(filepath.Dir(doc.File), localPath)
			}

			var message string
			if info, err := os.Stat(resolved); err != nil {
				message = fmt.Sprintf("asset source file %q does not exist (resolved to %s)", localPath, resolved)
			} else if info.IsDir() {
				message = fmt.Sprintf("asset source %q is a directory, not a file", localPath)
			}
			if message != "" {
				findings = append(findings, Finding{
					RuleID:  "asset-source-missing",
					Message: message,
					File:    doc.File,
					DocIdx:  doc.Position,
					Path:    "spec.source.localPath",
				})
			}
		}
		return findings
	},
}

// assetNames indexes the names of every Asset document that can back an image
// reference. Fonts are excluded because render.ResolveAssetURLs skips them, so
// naming one from an image field resolves to nothing.
func assetNames(docs []Document) map[string]bool {
	names := make(map[string]bool)
	for _, doc := range docs {
		if doc.Kind != "Asset" || doc.Name == "" {
			continue
		}
		var payload struct {
			Spec struct {
				Type string `json:"type"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			continue
		}
		if payload.Spec.Type == "font" {
			continue
		}
		names[doc.Name] = true
	}
	return names
}

// walkNodes visits every object in a decoded document, passing the path that
// locates it. Path segments are dot-separated with bare numeric indices
// ("spec.children.1"), the only form spec.ResolvePathPosition can resolve back
// to a line and column — that is what anchors the editor diagnostic on the
// offending key. Map keys are visited in sorted order so findings come out
// deterministically.
func walkNodes(node any, path string, visit func(obj map[string]any, path string)) {
	switch value := node.(type) {
	case map[string]any:
		visit(value, path)
		for _, key := range slices.Sorted(maps.Keys(value)) {
			walkNodes(value[key], joinLintPath(path, key), visit)
		}
	case []any:
		for i, item := range value {
			walkNodes(item, joinLintPath(path, strconv.Itoa(i)), visit)
		}
	}
}
