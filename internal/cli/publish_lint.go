package cli

import (
	"context"
	"fmt"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/lint"
)

// runPublishLint runs the project's lint rules and prints what they find,
// without ever blocking the publish.
//
// The registry's validation gate is the authority on whether a package is
// acceptable — it runs its own bino against the uploaded tree — so a local
// finding is advice, not a verdict, and a local failure to even load the
// project is not a reason to refuse to publish. What must not ship is
// enforced by the collector instead (credentials, path grammar, resource
// types), where '[lint] disable' cannot reach it.
func runPublishLint(ctx context.Context, p registryProject) {
	var loadErrors []error
	documents, err := config.LoadDirWithOptions(ctx, p.Root, config.LoadOptions{CollectErrors: &loadErrors})
	if err != nil {
		p.Out.Warning(fmt.Sprintf("could not lint the project before publishing: %v", err))
		return
	}
	runner := lint.NewProjectRunner(p.Root)
	findings := runner.Apply(runner.Run(ctx, lint.DocumentsFromConfig(documents)))
	findings = append(runner.Apply(schemaFindingsFromErrors(loadErrors)), findings...)
	if len(findings) == 0 {
		return
	}
	out := diagnosticsOutput()
	out.Warning(fmt.Sprintf("%d lint finding(s) — the registry's own validation decides whether they block:", len(findings)))
	for _, f := range findings {
		out.List(formatLintFinding(f))
	}
}

func formatLintFinding(f lint.Finding) string {
	out := ""
	if f.File != "" {
		out = f.File
		if f.Line > 0 {
			out = fmt.Sprintf("%s:%d", out, f.Line)
		}
		out += ": "
	}
	out += f.Message
	if f.RuleID != "" {
		out += " (" + f.RuleID + ")"
	}
	return out
}
