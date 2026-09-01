package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"bino.bi/bino/internal/pathutil"
)

// NewProjectRunner creates a Runner with the default rules plus, when the
// project's bino.toml declares a [package] table, the predef package rules.
// A project without [package] — or without a readable bino.toml — gets exactly
// the default rule set, so nothing changes for an ordinary report project.
func NewProjectRunner(projectRoot string) *Runner {
	return NewRunner(append(DefaultRules(), PredefRules(projectRoot)...))
}

// PredefRules returns the lint rules that apply only to predef packages:
// projects whose bino.toml carries a [package] table. It returns nil for every
// other project, so callers can append unconditionally.
func PredefRules(projectRoot string) []Rule {
	cfg, err := pathutil.LoadProjectConfig(projectRoot)
	if err != nil || cfg == nil || cfg.Package == nil {
		return nil
	}
	pkg := cfg.Package
	if verr := pkg.Validate(); verr != nil {
		// A malformed [package] makes every downstream check meaningless (the
		// include set and the name prefix both derive from it), so report the
		// config problem alone and run no content rules.
		return []Rule{packageConfigInvalid(pathutil.ProjectConfigPath(projectRoot), verr)}
	}
	inc := pkg.IncludeSet(projectRoot)
	return []Rule{
		predefNameNamespace(pkg.Name, inc),
		predefForbiddenKind(inc),
		predefAssetAbsolutePath(inc),
		predefExternalRef(pkg.Name, inc, cfg.Dependencies),
	}
}

// packageConfigInvalid reports a [package] table that PackageConfig.Validate
// rejects, anchored on bino.toml itself.
func packageConfigInvalid(configPath string, verr error) Rule {
	return Rule{
		ID:          "package-config-invalid",
		Name:        "Package Config Invalid",
		Description: "The [package] table in bino.toml must satisfy the constraints a registry publish enforces.",
		Check: func(_ context.Context, _ []Document) []Finding {
			return []Finding{{
				RuleID:   "package-config-invalid",
				Message:  verr.Error(),
				File:     configPath,
				Line:     1,
				Column:   1,
				Severity: "error",
			}}
		},
	}
}

// predefNameNamespace requires every document the package publishes to carry
// the package name, so a consumer that installs two packages never sees a name
// collision.
func predefNameNamespace(pkgName string, inc *pathutil.IncludeSet) Rule {
	return Rule{
		ID:   "predef-name-namespace",
		Name: "Predef Name Namespace",
		Description: "Documents inside the package include set must be named after the package: " +
			"exactly the package name for the single main definition, or \"<package>/<definition>\" for every other.",
		Check: func(_ context.Context, docs []Document) []Finding {
			var findings []Finding
			mainSeen := false
			for _, doc := range includedDocs(docs, inc) {
				name := doc.Name
				// The schema owns the empty case, and inline-naming-conflict
				// owns the reserved prefix; a materialized inline definition
				// cannot be renamed by the author anyway.
				if name == "" || strings.HasPrefix(name, "_inline_") {
					continue
				}

				var message string
				switch {
				case name == pkgName:
					if !mainSeen {
						mainSeen = true
						continue
					}
					message = fmt.Sprintf(
						"a package may declare at most one main definition named %q; %s %q also uses it — "+
							"give it a %q prefixed name instead",
						pkgName, doc.Kind, name, pkgName+"/",
					)
				case strings.HasPrefix(name, pkgName+"/") && len(name) > len(pkgName)+1:
					continue
				case doc.Kind == "DataSource":
					// $defs.sqlIdentifier caps a DataSource name at two
					// segments, so namespacing one is impossible by design.
					message = fmt.Sprintf(
						"DataSource %q cannot be namespaced under package %q: a DataSource name becomes a DuckDB "+
							"view name and is limited to two segments (\"@scope/name\"). Move this manifest out of "+
							"the package include set — mocks/ is the conventional place — or name it exactly %q as "+
							"the package's main definition.",
						name, pkgName, pkgName,
					)
				default:
					message = fmt.Sprintf(
						"%s %q is inside the package include set but is not namespaced; rename it to %q or move it "+
							"out of the include set",
						doc.Kind, name, pkgName+"/"+name,
					)
				}

				findings = append(findings, Finding{
					RuleID:   "predef-name-namespace",
					Message:  message,
					File:     doc.File,
					DocIdx:   doc.Position,
					Path:     "metadata.name",
					Severity: "error",
				})
			}
			return findings
		},
	}
}

// predefForbiddenKind bans document kinds that must never ship inside a
// package: artefacts render a report rather than compose one, and credentials
// are secret.
func predefForbiddenKind(inc *pathutil.IncludeSet) Rule {
	return Rule{
		ID:   "predef-forbidden-kind",
		Name: "Predef Forbidden Kind",
		Description: "Artefact kinds (ReportArtefact, LiveReportArtefact, ScreenshotArtefact, DocumentArtefact) " +
			"and credential kinds (ConnectionSecret, SigningProfile) must not be inside the package include set.",
		Check: func(_ context.Context, docs []Document) []Finding {
			included := includedDocs(docs, inc)
			findings := make([]Finding, 0, len(included))
			for _, doc := range included {
				var message string
				switch doc.Kind {
				case "ReportArtefact", "LiveReportArtefact", "ScreenshotArtefact", "DocumentArtefact":
					message = fmt.Sprintf(
						"%s %q is inside the package include set; artefacts render a report and are not "+
							"publishable package content — move it to reports/ or mocks/",
						doc.Kind, doc.Name,
					)
				case "ConnectionSecret", "SigningProfile":
					message = fmt.Sprintf(
						"%s %q is inside the package include set; credentials must never be published — move it "+
							"out of the include set (secrets/ and signing/ are included by default, so add an "+
							"explicit [package] include list or relocate the manifest)",
						doc.Kind, doc.Name,
					)
				default:
					continue
				}
				findings = append(findings, Finding{
					RuleID:   "predef-forbidden-kind",
					Message:  message,
					File:     doc.File,
					DocIdx:   doc.Position,
					Path:     "kind",
					Severity: "error",
				})
			}
			return findings
		},
	}
}

// predefAssetAbsolutePath requires a packaged Asset to reference its bytes
// relative to its own manifest: an absolute path names a file on the author's
// machine that the consumer will never have.
func predefAssetAbsolutePath(inc *pathutil.IncludeSet) Rule {
	return Rule{
		ID:          "predef-asset-absolute-path",
		Name:        "Predef Asset Absolute Path",
		Description: "An Asset inside the package include set must declare 'spec.source.localPath' relative to its manifest.",
		Check: func(_ context.Context, docs []Document) []Finding {
			var findings []Finding
			for _, doc := range includedDocs(docs, inc) {
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
				if localPath == "" || strings.Contains(localPath, "${") {
					continue // inlineBase64/remoteURL source, or missing-env-var's business.
				}
				if !isAbsoluteAssetPath(localPath) {
					continue
				}
				findings = append(findings, Finding{
					RuleID: "predef-asset-absolute-path",
					Message: fmt.Sprintf(
						"asset source %q is an absolute path; a published package must use a path relative to its manifest",
						localPath,
					),
					File:     doc.File,
					DocIdx:   doc.Position,
					Path:     "spec.source.localPath",
					Severity: "error",
				})
			}
			return findings
		},
	}
}

// isAbsoluteAssetPath reports whether a path is absolute on either platform: a
// Windows-authored "C:\logo.png" has to be caught on Linux CI and a POSIX
// "/logo.png" on Windows.
func isAbsoluteAssetPath(localPath string) bool {
	return filepath.IsAbs(localPath) ||
		strings.HasPrefix(localPath, "/") ||
		(len(localPath) >= 2 && localPath[1] == ':')
}

// predefExternalRef reports structural and presentational references that a
// published package cannot reach.
//
// Deliberately out of scope, and not to be "fixed" later: 'spec.dataset' (the
// binding seam — a packaged Table exists precisely so the consumer supplies
// their own dataset), a DataSet's 'spec.dependencies'/'spec.source',
// 'spec.i18nNamespace' (names a namespace, not a metadata.name), Markdown
// ':ref[Kind:name]' and 'asset:' destinations (which cannot express a
// namespaced name at all) and raw SQL in 'spec.query'. Artefact-level refs and
// 'sqlConnection.secret' are unreachable here because predef-forbidden-kind
// bans every kind that carries them.
func predefExternalRef(pkgName string, inc *pathutil.IncludeSet, deps map[string]string) Rule {
	return Rule{
		ID:   "predef-external-ref",
		Name: "Predef External Reference",
		Description: "Structural and presentational references inside the package include set — {kind, ref} " +
			"children, 'spec.selectedStyle' and 'spec.ruleset' — must resolve inside the package or to a declared " +
			"dependency. Data bindings are exempt: 'spec.dataset', a DataSet's 'spec.dependencies'/'spec.source' " +
			"and 'spec.i18nNamespace' are the seam the consumer rebinds, and raw SQL is not parsed.",
		Check: func(_ context.Context, docs []Document) []Finding {
			included := includedDocs(docs, inc)
			inSet := docFiles(included)
			project := docFiles(docs)

			var findings []Finding
			for _, doc := range included {
				var root any
				if err := json.Unmarshal(doc.Raw, &root); err != nil {
					continue // Schema validation reports malformed documents.
				}

				check := func(kind, name, path string) {
					message := externalRefProblem(kind, name, pkgName, inSet, project, deps)
					if message == "" {
						return
					}
					findings = append(findings, Finding{
						RuleID:   "predef-external-ref",
						Message:  message,
						File:     doc.File,
						DocIdx:   doc.Position,
						Path:     path,
						Severity: "error",
					})
				}

				walkNodes(root, "", func(node map[string]any, path string) {
					kind, _ := node["kind"].(string)
					if kind == "" {
						return
					}
					if ref, _ := node["ref"].(string); ref != "" {
						check(kind, ref, joinLintPath(path, "ref"))
					}
					componentSpec, _ := node["spec"].(map[string]any)
					check("ComponentStyle", stringField(componentSpec, "selectedStyle"), joinLintPath(path, "spec.selectedStyle"))
					check("RuleSet", stringField(componentSpec, "ruleset"), joinLintPath(path, "spec.ruleset"))
				})
			}
			return findings
		},
	}
}

// externalRefProblem resolves a single reference candidate against the package
// and returns the problem with it, or "" when it is fine. An entirely unknown
// name is fine here: missing-required-reference already reports a dangling
// reference, with better wording.
func externalRefProblem(kind, name, pkgName string, inSet, project map[string]string, deps map[string]string) string {
	name = strings.TrimSpace(name)
	if name == "" ||
		strings.Contains(name, "${") || // param or env placeholder
		strings.ContainsAny(name, "*?[") || // glob
		strings.HasPrefix(name, "_inline_") {
		return ""
	}
	name = strings.TrimPrefix(name, "$")

	if lookupDoc(inSet, kind, name) != "" {
		return ""
	}
	if strings.HasPrefix(name, "@") {
		if identity := packageIdentity(name); identity != "" && identity != pkgName {
			if _, ok := deps[identity]; ok {
				return ""
			}
			return fmt.Sprintf(
				"references %q, which belongs to package %q; declare it under [dependencies] in bino.toml",
				name, identity,
			)
		}
	}
	if file := lookupDoc(project, kind, name); file != "" {
		return fmt.Sprintf(
			"references %q, which is outside the package include set (%s); a published package cannot reach it",
			name, file,
		)
	}
	return ""
}

// packageIdentity returns the "@scope/name" package a namespaced document name
// belongs to, or "" when the name carries no scope/package pair.
func packageIdentity(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// includedDocs returns the documents that belong to the package. Registry-
// installed documents under .bino/registry, mocks/ and reports/ are excluded by
// IncludeSet.Contains.
func includedDocs(docs []Document, inc *pathutil.IncludeSet) []Document {
	var included []Document
	for _, doc := range docs {
		if inc.Contains(doc.File) {
			included = append(included, doc)
		}
	}
	return included
}

// docFiles indexes documents by "Kind:name", mapping each to the file that
// declares it. Every reference shape this rule inspects carries the target's
// kind, so a bare-name key would only let an unrelated document of another kind
// mask the real target.
func docFiles(docs []Document) map[string]string {
	index := make(map[string]string, len(docs))
	for _, doc := range docs {
		if doc.Name == "" {
			continue
		}
		index[doc.Kind+":"+doc.Name] = doc.File
	}
	return index
}

// lookupDoc finds the file declaring a referenced name. It returns "" when
// nothing of that kind declares it.
func lookupDoc(index map[string]string, kind, name string) string {
	return index[kind+":"+name]
}
