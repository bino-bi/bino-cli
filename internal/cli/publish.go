package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/registry"
	"bino.bi/bino/internal/version"
)

// publishOutcome is the --json shape of a publish. It covers a real publish
// and a dry run: DryRun says which, and Package/Unchanged are only meaningful
// for a real one.
type publishOutcome struct {
	Package   string               `json:"package"`
	Version   string               `json:"version"`
	Digest    string               `json:"digest"`
	Tag       string               `json:"tag,omitempty"`
	Kinds     []string             `json:"kinds,omitempty"`
	Files     []registry.FileEntry `json:"files"`
	Unchanged bool                 `json:"unchanged"`
	DryRun    bool                 `json:"dryRun"`
	Warnings  []string             `json:"warnings,omitempty"`
}

func newPublishCommand() *cobra.Command {
	var (
		bump       string
		dryRun     bool
		visibility string
		jsonOut    bool
	)
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish this project as a registry package",
		Long: `Publishes the current project's [package] file set to the registry as a new
immutable version.

The project must be a predef project: a bino project whose bino.toml carries a
[package] table. The files it ships are the ones [package].include selects —
YAML manifests plus resources — and the version is minted by the registry from
the requested --bump.

The registry validates the package before it accepts it. 'bino lint' runs first
as advice, but the registry's own gate is the authority, so a package that
lints clean can still be rejected. Use --dry-run to see that verdict without
minting anything.

Republishing an unchanged package is not an error: the registry recognizes the
identical content and reports the version that already carries it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !dryRun && bump == "" {
				return ConfigErrorf("--bump is required (patch, minor or major) unless --dry-run is set")
			}
			if bump != "" && bump != "patch" && bump != "minor" && bump != "major" {
				return ConfigErrorf("invalid --bump %q: expected patch, minor or major", bump)
			}
			if visibility != "" && visibility != "public" && visibility != "private" {
				return ConfigErrorf("invalid --visibility %q: expected public or private", visibility)
			}
			return runPublish(cmd, publishOptions{
				bump: bump, dryRun: dryRun, visibility: visibility, jsonOut: jsonOut,
			})
		},
	}
	cmd.Flags().StringVar(&bump, "bump", "", "Version increment: patch, minor or major (required unless --dry-run)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate against the registry without publishing")
	cmd.Flags().StringVar(&visibility, "visibility", "", "Visibility for a first publish: public or private")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the result as JSON")
	return cmd
}

type publishOptions struct {
	bump       string
	dryRun     bool
	visibility string
	jsonOut    bool
}

func runPublish(cmd *cobra.Command, opts publishOptions) error {
	ctx := cmd.Context()
	p, err := publishProjectSetup(opts.jsonOut)
	if err != nil {
		return err
	}
	pkg := p.Cfg.Package

	files, err := collectPackageFiles(p.Root, pkg)
	if err != nil {
		return ConfigError(err)
	}
	entries := publishManifestEntries(files)
	digest, _, err := registry.ManifestDigest(entries)
	if err != nil {
		return ConfigErrorf("compute package digest: %w", err)
	}

	p.Out.Info(fmt.Sprintf("%s — %d file(s), digest %s", pkg.Name, len(files), digest))
	for _, f := range files {
		p.Out.List(fmt.Sprintf("%s (%s)", f.TreePath, f.Type))
	}
	warnPreviewOutsidePackage(p, pkg, files)
	runPublishLint(ctx, p)

	client, err := p.client()
	if err != nil {
		return err
	}
	manifest, err := buildPublishManifest(ctx, client, p, opts, entries)
	if err != nil {
		return err
	}
	uploads, _ := publishFiles(files)

	if opts.dryRun {
		p.Out.Step("Validating with the registry...")
		res, err := client.PublishDryRun(ctx, manifest, uploads)
		if err != nil {
			return publishError(p, err)
		}
		p.Out.StepDone("Validated", 0)
		return emitPublishOutcome(cmd, publishOutcome{
			Package: pkg.Name, Version: res.Version, Digest: res.Digest,
			Files: res.Files, DryRun: true, Warnings: findingLines(res.Warnings),
		}, opts.jsonOut, p)
	}

	p.Out.Step("Publishing...")
	res, err := client.Publish(ctx, manifest, uploads)
	if err != nil {
		return publishError(p, err)
	}
	p.Out.StepDone("Published", 0)
	return emitPublishOutcome(cmd, publishOutcome{
		Package: res.Package, Version: res.Version, Digest: res.Digest, Tag: res.Tag,
		Kinds: res.Kinds, Files: res.Files, Unchanged: res.Unchanged,
		Warnings: findingLines(res.Warnings),
	}, opts.jsonOut, p)
}

// diagnosticsOutput is an Output that writes everything to stderr, for
// multi-line reports that must stay on one stream and must never contaminate
// the stdout that --json owns.
func diagnosticsOutput() *Output { return NewOutput(OutputConfig{Stdout: os.Stderr}) }

// publishProjectSetup resolves the project and insists it is a predef
// project. registryProjectSetup only requires a bino.toml, which every bino
// project has; publishing additionally needs the [package] table that names
// the coordinate to publish to.
func publishProjectSetup(jsonOut bool) (registryProject, error) {
	p, err := registryProjectSetup()
	if err != nil {
		return registryProject{}, err
	}
	if jsonOut {
		// stdout carries only the JSON object; progress goes to stderr.
		p.Out = NewOutput(OutputConfig{Stdout: os.Stderr})
	}
	if p.Cfg.Package == nil {
		return registryProject{}, wrapErrorWithHint(ErrorKindConfig,
			fmt.Errorf("%s has no [package] table, so this project does not publish a package", pathutil.ProjectConfigPath(p.Root)),
			"add a [package] table with at least a name, or run 'bino init predef' to scaffold one")
	}
	// The table is validated lazily elsewhere so an unrelated command never
	// fails on it. Publishing is the command it exists for, so here it is fatal.
	if err := p.Cfg.Package.Validate(); err != nil {
		return registryProject{}, ConfigErrorf("bino.toml: %w", err)
	}
	return p, nil
}

// buildPublishManifest assembles the JSON manifest part.
func buildPublishManifest(ctx context.Context, client *registry.Client, p registryProject, opts publishOptions, entries []registry.FileEntry) (registry.PublishManifest, error) {
	pkg := p.Cfg.Package
	m := registry.PublishManifest{
		Name:         pkg.Name,
		Bump:         opts.bump,
		Description:  pkg.Description,
		Category:     pkg.Category,
		CompatEngine: pkg.CompatEngine,
		CompatCli:    pkg.CompatCLI,
		Preview:      pkg.Preview,
		Tags:         pkg.Tags,
		Dependencies: publishDependencies(p.Cfg),
		Files:        entries,
	}
	vis, err := publishVisibility(ctx, client, p, opts)
	if err != nil {
		return registry.PublishManifest{}, err
	}
	m.Visibility = vis
	return m, nil
}

// publishVisibility decides whether to declare a visibility at all.
//
// The registry only honors it when the package is created, and rejects a
// value that differs from an existing package's — so sending bino.toml's
// visibility on every publish would break the moment someone changed it in the
// web UI. The package is probed first, and an inconclusive answer is fatal
// rather than "send nothing": a first publish that silently lands with the
// server's default visibility cannot be undone.
func publishVisibility(ctx context.Context, client *registry.Client, p registryProject, opts publishOptions) (string, error) {
	declared := opts.visibility
	if declared == "" {
		declared = strings.ToLower(p.Cfg.Package.Visibility)
	}
	if declared == "" {
		return "", nil
	}
	scope, base, err := registry.ParseName(p.Cfg.Package.Name)
	if err != nil {
		return "", ConfigError(err)
	}
	exists, known := client.PackageExists(ctx, scope, base)
	if !known {
		return "", ExternalErrorWithHint(
			fmt.Errorf("could not determine whether %s already exists, and visibility only takes effect when a package is created", p.Cfg.Package.Name),
			"check the registry is reachable and you are logged in, then retry")
	}
	if exists {
		// Already created: the registry owns its visibility from here on.
		return "", nil
	}
	return declared, nil
}

// publishDependencies renders bino.toml's [dependencies] table as the
// registry's "@scope/name@ref" specs, sorted so the manifest is stable.
func publishDependencies(cfg *pathutil.ProjectConfig) []string {
	out := make([]string, 0, len(cfg.Dependencies))
	for name, ref := range cfg.Dependencies {
		if ref == "" {
			out = append(out, name)
			continue
		}
		out = append(out, name+"@"+ref)
	}
	sort.Strings(out)
	return out
}

func publishManifestEntries(files []collectedFile) []registry.FileEntry {
	_, entries := publishFiles(files)
	return entries
}

// warnPreviewOutsidePackage reports a [package].preview that names a file the
// package does not ship — most often one under mocks/, which is excluded from
// every package by design.
func warnPreviewOutsidePackage(p registryProject, pkg *pathutil.PackageConfig, files []collectedFile) {
	if pkg.Preview == "" {
		return
	}
	target, _, _ := strings.Cut(pkg.Preview, "#")
	for _, f := range files {
		if f.TreePath == target {
			return
		}
	}
	p.Out.Warning(fmt.Sprintf("[package].preview points at %q, which this package does not ship — the registry will fall back to its own choice", target))
}

// publishError renders a registry rejection. A validation failure carries the
// gate's own findings and the bino and engine it ran them with, printed next
// to this CLI's, because the commonest cause of a package that lints clean
// locally and fails remotely is exactly that skew.
func publishError(p registryProject, err error) error {
	var apiErr *registry.APIError
	if !errors.As(err, &apiErr) {
		return ExternalError(err)
	}
	details := apiErr.GateDetails()
	if len(details) == 0 {
		return ExternalError(err)
	}
	// One report, one stream: Output splits Error/Warning onto stderr and
	// Info/List onto stdout, which would interleave the findings with
	// whatever the caller is piping stdout into.
	out := diagnosticsOutput()
	out.Blank()
	out.Error("The registry rejected this package:")
	for _, d := range details {
		for _, f := range d.Findings {
			out.List(formatFinding(f))
		}
		out.Blank()
		out.Info(fmt.Sprintf("validated by bino %s / engine %s (registry)", orUnknown(d.Bino), orUnknown(d.Engine)))
		out.Info(fmt.Sprintf("            bino %s / engine %s (here)",
			orUnknown(version.Version), orUnknown(resolveEngineVersionForCompat(p.Cfg.EngineVersion))))
	}
	return ExternalErrorWithHint(
		fmt.Errorf("%s", apiErr.Message),
		"fix the findings above and publish again; 'bino publish --dry-run' re-checks without minting a version")
}

func formatFinding(f registry.Finding) string {
	var b strings.Builder
	if f.File != "" {
		b.WriteString(f.File)
		if f.Line > 0 {
			fmt.Fprintf(&b, ":%d", f.Line)
		}
		b.WriteString(": ")
	}
	if f.Severity != "" {
		fmt.Fprintf(&b, "[%s] ", f.Severity)
	}
	b.WriteString(f.Message)
	if f.Rule != "" {
		fmt.Fprintf(&b, " (%s)", f.Rule)
	}
	return b.String()
}

func findingLines(findings []registry.Finding) []string {
	if len(findings) == 0 {
		return nil
	}
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = formatFinding(f)
	}
	return out
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// emitPublishOutcome prints the result, as JSON on stdout when --json is set
// and as human output otherwise.
func emitPublishOutcome(cmd *cobra.Command, outcome publishOutcome, jsonOut bool, p registryProject) error {
	if outcome.Files == nil {
		outcome.Files = []registry.FileEntry{}
	}
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(outcome)
	}
	for _, w := range outcome.Warnings {
		p.Out.Warning(w)
	}
	switch {
	case outcome.DryRun:
		p.Out.Success(fmt.Sprintf("%s would publish as %s (digest %s)", outcome.Package, outcome.Version, outcome.Digest))
		p.Out.Info("No version was created — drop --dry-run to publish.")
	case outcome.Unchanged:
		p.Out.Success(fmt.Sprintf("%s is unchanged — already published as %s", outcome.Package, outcome.Version))
	default:
		p.Out.Success(fmt.Sprintf("Published %s@%s (digest %s)", outcome.Package, outcome.Version, outcome.Digest))
		if len(outcome.Kinds) > 0 {
			p.Out.Info("Kinds: " + strings.Join(outcome.Kinds, ", "))
		}
	}
	return nil
}
