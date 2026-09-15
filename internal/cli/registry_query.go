package cli

import (
	"fmt"
	"os"
	"path"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/registry"
)

func newRegistryListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed registry packages",
		Long:  "Lists the packages pinned in bino.lock. Works offline.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := registryProjectSetup()
			if err != nil {
				return err
			}
			lock, err := registry.LoadLockfile(p.Root)
			if err != nil {
				return ConfigError(err)
			}
			if len(lock.Packages) == 0 {
				fmt.Println("No registry packages installed.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "PACKAGE\tVERSION\tTAG\tKIND\tFILES\tORIGIN\tPATH")
			for _, e := range lock.Packages {
				tag := e.Tag
				if tag == "" {
					tag = "-"
				}
				origin := "transitive"
				if e.Direct {
					origin = "direct"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
					e.Name, e.Version, tag, entryKinds(e), len(e.TreeFiles()), origin, e.Path)
			}
			w.Flush()
			return nil
		},
	}
}

func newRegistrySearchCommand() *cobra.Command {
	var (
		kinds   []string
		scopes  []string
		tags    []string
		page    int
		perPage int
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the registry for packages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			p, err := registryProjectSetup()
			if err != nil {
				return err
			}
			client, err := p.client()
			if err != nil {
				return err
			}
			res, err := client.Search(ctx, registry.SearchParams{
				Query:   args[0],
				Kinds:   kinds,
				Scopes:  scopes,
				Tags:    tags,
				Page:    page,
				PerPage: perPage,
			})
			if err != nil {
				return ExternalError(err)
			}
			if res.TotalItems == 0 {
				fmt.Println("No packages found.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "PACKAGE\tKIND\tLATEST\tDESCRIPTION\tPULLS")
			for _, item := range res.Items {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n", item.Package, item.Kind, item.LatestVersion, oneLine(item.Description), item.PullsTotal)
			}
			w.Flush()
			fmt.Printf("page %d/%d (%d packages)\n", res.Page, res.TotalPages, res.TotalItems)
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&kinds, "kind", nil, "Filter by document kind (repeatable)")
	cmd.Flags().StringArrayVar(&scopes, "scope", nil, "Filter by scope (repeatable)")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "Filter by tag (repeatable)")
	cmd.Flags().IntVar(&page, "page", 1, "Result page (1-based)")
	cmd.Flags().IntVar(&perPage, "per-page", 20, "Results per page")
	return cmd
}

func newRegistryInfoCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "info <package[@version|@tag]>",
		Short: "Show a package's resolved metadata",
		Long: `Resolves a package and prints what the registry knows about it, including
the files the version ships. A single-document package lists exactly one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			p, err := registryProjectSetup()
			if err != nil {
				return err
			}
			spec, err := registry.ParseSpec(args[0])
			if err != nil {
				return ConfigError(err)
			}
			client, err := p.client()
			if err != nil {
				return err
			}
			res, err := client.ResolveTree(ctx, spec.Scope, spec.Base, spec.Ref)
			if err != nil {
				return ExternalError(err)
			}
			fmt.Printf("Package:      %s\n", res.Package)
			fmt.Printf("Version:      %s\n", res.Version)
			if res.Tag != "" {
				fmt.Printf("Tag:          %s\n", res.Tag)
			}
			fmt.Printf("Kind:         %s\n", strings.Join(res.Kinds, ", "))
			fmt.Printf("Digest:       %s\n", res.Digest)
			if res.CompatEngine != "" {
				fmt.Printf("Engine:       %s\n", res.CompatEngine)
			}
			if res.CompatCli != "" {
				fmt.Printf("CLI:          %s\n", res.CompatCli)
			}
			if len(res.Dependencies) > 0 {
				fmt.Printf("Dependencies: %s\n", strings.Join(res.Dependencies, ", "))
			}
			fmt.Printf("Files:        %d\n", len(res.Files))
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, f := range res.Files {
				fmt.Fprintf(w, "  %s\t%s\t%s\n", f.Path, f.Type, f.Digest)
			}
			w.Flush() //nolint:errcheck // stdout write failures surface on the next print
			return nil
		},
	}
}

func newRegistryVerifyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Verify installed packages against bino.lock",
		Long: `Re-hashes every package file under .bino/registry/ and compares it with the
digest pinned in bino.lock. Works offline; intended as a CI gate. The digest
is computed over the canonical document form, so cosmetic reformatting of a
file does not fail verification.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := registryProjectSetup()
			if err != nil {
				return err
			}
			lock, err := registry.LoadLockfile(p.Root)
			if err != nil {
				return ConfigError(err)
			}
			if len(lock.Packages) == 0 {
				p.Out.Info("Nothing to verify")
				return nil
			}
			bad := 0
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "PACKAGE\tVERSION\tSTATUS")
			for _, e := range lock.Packages {
				status := verifyPackage(p.Root, e)
				if status != "OK" {
					bad++
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", e.Name, e.Version, status)
			}
			w.Flush()
			if bad > 0 {
				return RuntimeErrorf("%d of %d package(s) failed verification — run 'bino registry install' to re-materialize from %s", bad, len(lock.Packages), registry.LockfileName)
			}
			p.Out.Success(fmt.Sprintf("Verified %d package(s)", len(lock.Packages)))
			return nil
		},
	}
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

// entryKinds renders a lock entry's kind set for the list table: every kind a
// file-tree package ships, or the single kind of a document package.
func entryKinds(e registry.Entry) string {
	if len(e.Kinds) > 0 {
		return strings.Join(e.Kinds, ",")
	}
	return e.Kind
}

// verifyPackage re-checks one locked package against the store, reporting the
// first problem it finds. The digest rule is chosen by the entry's format, so
// a single-document package keeps verifying under the rule it was published
// with while a tree verifies file by file. Files the lock does not record are
// reported too: without that, an injected document — which the build's second
// pass would happily load — verifies clean.
func verifyPackage(projectRoot string, e registry.Entry) string {
	files := e.TreeFiles()
	if len(files) == 0 {
		return "INVALID LOCK"
	}
	known := make(map[string]bool, len(files))
	for _, f := range files {
		known[f.Path] = true
		abs, _, err := registry.TreeFilePath(projectRoot, e.Name, f.Path)
		if err != nil {
			return "INVALID NAME"
		}
		body, readErr := os.ReadFile(abs)
		if readErr != nil {
			return "MISSING"
		}
		if registry.VerifyFile(e.Format, f.Type, body, f.Digest) != nil {
			return "MODIFIED"
		}
	}
	onDisk, err := registry.ListPackageFiles(projectRoot, e.Name)
	if err != nil {
		return "MISSING"
	}
	for _, p := range onDisk {
		if !known[p] && !isWriteResidue(p) {
			return "EXTRA"
		}
	}
	return "OK"
}

// isWriteResidue reports whether a store file is a leftover of an interrupted
// atomic write rather than content. writeFileAtomic creates ".<name>.tmp-*"
// beside its target and removes it on the way out, so a killed install can
// leave one behind; reporting that as tampering would turn ordinary crash
// residue into a red CI run.
func isWriteResidue(p string) bool {
	base := path.Base(p)
	return strings.HasPrefix(base, ".") && strings.Contains(base, ".tmp-")
}
