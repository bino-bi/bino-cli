package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/bino-bi/bino-plugin-sdk/registrydigest"
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
			fmt.Fprintln(w, "PACKAGE\tVERSION\tTAG\tKIND\tORIGIN\tPATH")
			for _, e := range lock.Packages {
				tag := e.Tag
				if tag == "" {
					tag = "-"
				}
				origin := "transitive"
				if e.Direct {
					origin = "direct"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", e.Name, e.Version, tag, e.Kind, origin, e.Path)
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
		Args:  cobra.ExactArgs(1),
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
			res, err := client.Resolve(ctx, spec.Scope, spec.Base, spec.Ref)
			if err != nil {
				return ExternalError(err)
			}
			fmt.Printf("Package:      %s\n", res.Package)
			fmt.Printf("Version:      %s\n", res.Version)
			if res.Tag != "" {
				fmt.Printf("Tag:          %s\n", res.Tag)
			}
			fmt.Printf("Kind:         %s\n", res.Kind)
			fmt.Printf("Digest:       %s\n", res.Digest)
			if len(res.Dependencies) > 0 {
				fmt.Printf("Dependencies: %s\n", strings.Join(res.Dependencies, ", "))
			}
			fmt.Printf("Download URL: %s\n", res.DownloadURL)
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
				status := "OK"
				abs, _, err := registry.StorePath(p.Root, e.Name)
				if err != nil {
					status = "INVALID NAME"
				} else if body, readErr := os.ReadFile(abs); readErr != nil {
					status = "MISSING"
				} else if digest, digErr := registrydigest.Digest(body); digErr != nil || digest != e.Digest {
					status = "MODIFIED"
				}
				if status == "OK" {
					for _, r := range e.Resources {
						resAbs, _, err := registry.ResourcePath(p.Root, e.Name, r.Name)
						if err != nil {
							status = "MODIFIED"
							break
						}
						data, readErr := os.ReadFile(resAbs)
						if readErr != nil {
							status = "MISSING"
							break
						}
						sum := sha256.Sum256(data)
						if "sha256:"+hex.EncodeToString(sum[:]) != r.ContentHash {
							status = "MODIFIED"
							break
						}
					}
				}
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
