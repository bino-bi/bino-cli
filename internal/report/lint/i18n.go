package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// i18nDefaultNamespace is the namespace every component reads by default; the
// renderer stores Internationalization content there when spec.namespace is
// empty (mirrors render.collectInternationalizations).
const i18nDefaultNamespace = "_system"

// artefactKindsWithLanguage lists the kinds whose spec.language selects the
// locale the engine looks translations up under.
var artefactKindsWithLanguage = map[string]bool{
	"ReportArtefact":     true,
	"ScreenshotArtefact": true,
}

// i18nCodeUnused warns when an Internationalization document's locale code
// matches no artefact language in the bundle.
//
// The engine looks translations up under the exact string of the artefact's
// spec.language ('de' or 'en'); there is no BCP 47 normalization, so a
// payload registered as 'de-DE' is never consulted when the report renders
// with locale 'de'.
var i18nCodeUnused = Rule{
	ID:   "i18n-code-unused",
	Name: "Internationalization Code Unused",
	Description: "An Internationalization 'spec.code' must exactly match an artefact 'spec.language' " +
		"('de' or 'en'); locale lookup is an exact string match, so codes like 'de-DE' are never consulted.",
	Check: func(_ context.Context, docs []Document) []Finding {
		languages := make(map[string]bool)
		haveArtefact := false
		for _, doc := range docs {
			if !artefactKindsWithLanguage[doc.Kind] {
				continue
			}
			haveArtefact = true
			var payload struct {
				Spec struct {
					Language string `json:"language"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(doc.Raw, &payload); err != nil {
				continue // Schema validation reports malformed documents.
			}
			language := strings.TrimSpace(payload.Spec.Language)
			if language == "" {
				// The pipeline defaults an unset language (DefaultArtefactLanguage).
				language = "de"
			}
			languages[language] = true
		}
		if !haveArtefact {
			return nil // Component-library bundles have nothing to compare against.
		}

		var findings []Finding
		for _, doc := range docs {
			if doc.Kind != "Internationalization" {
				continue
			}
			var payload struct {
				Spec struct {
					Code string `json:"code"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(doc.Raw, &payload); err != nil {
				continue
			}
			code := strings.TrimSpace(payload.Spec.Code)
			if code == "" || languages[code] {
				continue
			}
			findings = append(findings, Finding{
				RuleID: "i18n-code-unused",
				Message: fmt.Sprintf(
					"locale code %q matches no artefact language (%s); the lookup is an exact string match, "+
						"so these translations are never used",
					code, joinSorted(languages),
				),
				File:   doc.File,
				DocIdx: doc.Position,
				Path:   "spec.code",
			})
		}
		return findings
	},
}

// i18nNamespaceUnreferenced warns when an Internationalization document uses a
// named namespace that no component points at.
//
// Components only consult the namespace named by their (possibly inherited)
// i18nNamespace — or titleNamespace for page/card titles — and fall back to
// '_system'. Content stored under any other namespace is never read.
var i18nNamespaceUnreferenced = Rule{
	ID:   "i18n-namespace-unreferenced",
	Name: "Internationalization Namespace Unreferenced",
	Description: "An Internationalization 'spec.namespace' other than '_system' is only consulted by components " +
		"whose i18nNamespace (or a page/card titleNamespace) names it; otherwise its content is never read.",
	Check: func(_ context.Context, docs []Document) []Finding {
		referenced := map[string]bool{i18nDefaultNamespace: true}
		for _, doc := range docs {
			var root any
			if err := json.Unmarshal(doc.Raw, &root); err != nil {
				continue
			}
			walkNodes(root, "", func(node map[string]any, _ string) {
				componentSpec, ok := node["spec"].(map[string]any)
				if !ok {
					return
				}
				for _, field := range []string{"i18nNamespace", "titleNamespace"} {
					if value := strings.TrimSpace(stringField(componentSpec, field)); value != "" {
						referenced[value] = true
					}
				}
			})
		}

		var findings []Finding
		for _, doc := range docs {
			if doc.Kind != "Internationalization" {
				continue
			}
			var payload struct {
				Spec struct {
					Namespace string `json:"namespace"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(doc.Raw, &payload); err != nil {
				continue
			}
			namespace := strings.TrimSpace(payload.Spec.Namespace)
			if namespace == "" || referenced[namespace] {
				continue
			}
			findings = append(findings, Finding{
				RuleID: "i18n-namespace-unreferenced",
				Message: fmt.Sprintf(
					"namespace %q is not referenced by any i18nNamespace; its translations are never read "+
						"(omit the namespace to merge into '_system', the default every component reads)",
					namespace,
				),
				File:   doc.File,
				DocIdx: doc.Position,
				Path:   "spec.namespace",
			})
		}
		return findings
	},
}

// i18nTitleNamespaceDeprecated flags titleNamespace usage.
//
// titleNamespace only ever applied to the page/card title, which is why it is
// superseded by the inheritable i18nNamespace.
var i18nTitleNamespaceDeprecated = Rule{
	ID:   "i18n-title-namespace-deprecated",
	Name: "titleNamespace Deprecated",
	Description: "'titleNamespace' is deprecated and only applies to the page/card title; " +
		"use 'i18nNamespace', which also inherits to all children.",
	Check: func(_ context.Context, docs []Document) []Finding {
		var findings []Finding
		for _, doc := range docs {
			var root any
			if err := json.Unmarshal(doc.Raw, &root); err != nil {
				continue
			}
			docFile, docPos := doc.File, doc.Position
			walkNodes(root, "", func(node map[string]any, path string) {
				componentSpec, ok := node["spec"].(map[string]any)
				if !ok {
					return
				}
				if strings.TrimSpace(stringField(componentSpec, "titleNamespace")) == "" {
					return
				}
				findings = append(findings, Finding{
					RuleID: "i18n-title-namespace-deprecated",
					Message: "titleNamespace is deprecated and only applies to the title; " +
						"use i18nNamespace, which also inherits to all children",
					File:   docFile,
					DocIdx: docPos,
					Path:   joinLintPath(path, "spec.titleNamespace"),
				})
			})
		}
		return findings
	},
}

// joinSorted renders a set of strings as a sorted, quoted list.
func joinSorted(set map[string]bool) string {
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, fmt.Sprintf("%q", value))
	}
	sort.Strings(values)
	return strings.Join(values, ", ")
}
