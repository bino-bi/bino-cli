package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
	reportgraph "bino.bi/bino/internal/report/graph"
	"bino.bi/bino/internal/report/pipeline"
)

func newGraphCommand() *cobra.Command {
	var (
		workdir string
		view    string
		include []string
		exclude []string
	)

	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Inspect manifest dependencies and hashes",
		Long: strings.TrimSpace(`Load the manifest bundle, build a dependency graph across report artifacts,
components, datasets, and datasources, and print the relationships in either
a tree or flat table view.`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			logger := logx.FromContext(ctx).Channel("graph")

			absDir, err := pipeline.ResolveWorkdir(workdir)
			if err != nil {
				return ConfigError(err)
			}

			docs, err := config.LoadDir(ctx, absDir)
			if err != nil {
				return ConfigError(err)
			}
			if len(docs) == 0 {
				return ConfigErrorf("no YAML documents found in %s", absDir)
			}

			artifacts, err := config.CollectArtefacts(docs)
			if err != nil {
				return ConfigError(err)
			}

			documentArtefacts, err := config.CollectDocumentArtefacts(docs)
			if err != nil {
				return ConfigError(err)
			}

			if len(artifacts) == 0 && len(documentArtefacts) == 0 {
				return ConfigErrorf("no ReportArtefact or DocumentArtefact manifests found in %s", absDir)
			}

			filterOpts := pipeline.FilterOptions{
				Include: include,
				Exclude: exclude,
			}
			selected := pipeline.FilterArtefacts(artifacts, filterOpts)
			selectedDocs := pipeline.FilterDocumentArtefacts(documentArtefacts, filterOpts)

			if len(selected) == 0 && len(selectedDocs) == 0 {
				return ConfigErrorf("no artifacts selected (check --artifact / --exclude-artifact)")
			}

			g, err := reportgraph.Build(ctx, docs)
			if err != nil {
				return RuntimeError(err)
			}

			roots := make([]*reportgraph.Node, 0, len(selected)+len(selectedDocs))
			for _, art := range selected {
				node, ok := g.ReportArtefactByName(art.Document.Name)
				if !ok {
					return RuntimeErrorf("graph: artifact node %s not found", art.Document.Name)
				}
				roots = append(roots, node)
			}
			for _, docArt := range selectedDocs {
				node, ok := g.DocumentArtefactByName(docArt.Document.Name)
				if !ok {
					return RuntimeErrorf("graph: document artifact node %s not found", docArt.Document.Name)
				}
				roots = append(roots, node)
			}

			out := cmd.OutOrStdout()
			switch strings.ToLower(view) {
			case "tree":
				printGraphTree(out, g, roots, absDir)
			case "flat":
				printGraphFlat(out, g, roots, absDir)
			default:
				return ConfigErrorf("unknown view %q (expected tree or flat)", view)
			}

			if logger != nil {
				logger.Successf("Rendered %s view for %d artifact(s)", view, len(roots))
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVarP(&workdir, "work-dir", "w", ".", "Working directory containing report manifests")
	cmd.Flags().StringSliceVar(&include, "artifact", nil, "metadata.name entries to include (default: all)")
	cmd.Flags().StringSliceVar(&exclude, "exclude-artifact", nil, "metadata.name entries to skip")
	cmd.Flags().StringVar(&view, "view", "tree", "Output format: tree or flat")

	return cmd
}

func printGraphTree(out io.Writer, g *reportgraph.Graph, roots []*reportgraph.Node, base string) {
	for idx, root := range roots {
		if root == nil {
			continue
		}
		if idx > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintln(out, formatNodeLine(root, base))
		printTreeChildren(out, g, root, "", base, map[string]bool{})
	}
}

func printTreeChildren(out io.Writer, g *reportgraph.Graph, node *reportgraph.Node, prefix string, base string, stack map[string]bool) {
	if node == nil {
		return
	}
	stack[node.ID] = true
	children := sortedChildren(g, node)
	for idx, child := range children {
		connector := "├──"
		nextPrefix := prefix + "│   "
		if idx == len(children)-1 {
			connector = "└──"
			nextPrefix = prefix + "    "
		}
		line := formatNodeLine(child, base)
		if stack[child.ID] {
			line += " [cycle]"
			fmt.Fprintf(out, "%s%s %s\n", prefix, connector, line)
			continue
		}
		fmt.Fprintf(out, "%s%s %s\n", prefix, connector, line)
		printTreeChildren(out, g, child, nextPrefix, base, stack)
	}
	delete(stack, node.ID)
}

func sortedChildren(g *reportgraph.Graph, node *reportgraph.Node) []*reportgraph.Node {
	if g == nil || node == nil || len(node.DependsOn) == 0 {
		return nil
	}
	children := make([]*reportgraph.Node, 0, len(node.DependsOn))
	for _, dep := range node.DependsOn {
		child, ok := g.NodeByID(dep)
		if !ok {
			continue
		}
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool {
		if children[i].Kind == children[j].Kind {
			return displayName(children[i]) < displayName(children[j])
		}
		return children[i].Kind < children[j].Kind
	})
	return children
}

func printGraphFlat(out io.Writer, g *reportgraph.Graph, roots []*reportgraph.Node, base string) {
	reachable := collectReachableNodes(g, roots)
	nodes := make([]*reportgraph.Node, 0, len(reachable))
	for _, node := range reachable {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Kind == nodes[j].Kind {
			return displayName(nodes[i]) < displayName(nodes[j])
		}
		return nodes[i].Kind < nodes[j].Kind
	})

	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "KIND\tNAME\tHASH\tDEPENDS ON\tDETAILS")
	for _, node := range nodes {
		depLabels := dependencyLabels(g, node)
		details := strings.Join(nodeDetails(node, base), ", ")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			node.Kind,
			displayName(node),
			shortHash(node.Hash),
			strings.Join(depLabels, ", "),
			details,
		)
	}
	tw.Flush()
}

func collectReachableNodes(g *reportgraph.Graph, roots []*reportgraph.Node) map[string]*reportgraph.Node {
	visited := make(map[string]*reportgraph.Node)
	var walk func(node *reportgraph.Node)
	walk = func(node *reportgraph.Node) {
		if node == nil {
			return
		}
		if _, ok := visited[node.ID]; ok {
			return
		}
		visited[node.ID] = node
		for _, dep := range node.DependsOn {
			child, ok := g.NodeByID(dep)
			if !ok {
				continue
			}
			walk(child)
		}
	}
	for _, root := range roots {
		walk(root)
	}
	return visited
}

func dependencyLabels(g *reportgraph.Graph, node *reportgraph.Node) []string {
	if g == nil || node == nil {
		return nil
	}
	var labels []string
	for _, dep := range node.DependsOn {
		child, ok := g.NodeByID(dep)
		if !ok {
			continue
		}
		labels = append(labels, fmt.Sprintf("%s:%s", child.Kind, displayName(child)))
	}
	sort.Strings(labels)
	return labels
}

func nodeDetails(node *reportgraph.Node, base string) []string {
	if node == nil {
		return nil
	}
	details := []string{fmt.Sprintf("hash=%s", shortHash(node.Hash))}
	if kind := node.Attributes["componentKind"]; kind != "" && node.Kind == reportgraph.NodeComponent {
		details = append(details, fmt.Sprintf("kind=%s", kind))
	}
	if dataset := node.Attributes["dataset"]; dataset != "" {
		switch {
		case node.Attributes["datasetMissing"] == "true":
			details = append(details, fmt.Sprintf("dataset=%s (missing)", dataset))
		case node.Attributes["datasetKind"] != "":
			target := node.Attributes["datasetKind"]
			details = append(details, fmt.Sprintf("dataset=%s (%s)", dataset, target))
		default:
			details = append(details, fmt.Sprintf("dataset=%s", dataset))
		}
	}
	if typ := node.Attributes["type"]; typ != "" {
		details = append(details, fmt.Sprintf("type=%s", typ))
	}
	if src := node.Attributes["sources"]; src != "" {
		details = append(details, fmt.Sprintf("sources=%s", src))
	}
	if file := formatRelPath(base, node.File); file != "" {
		details = append(details, fmt.Sprintf("file=%s", file))
	}
	return details
}

func formatNodeLine(node *reportgraph.Node, base string) string {
	label := displayName(node)
	meta := strings.Join(nodeDetails(node, base), ", ")
	if meta == "" {
		return fmt.Sprintf("%s [%s]", label, node.Kind)
	}
	return fmt.Sprintf("%s [%s] %s", label, node.Kind, meta)
}

func displayName(node *reportgraph.Node) string {
	if node == nil {
		return ""
	}
	if node.Label != "" {
		return node.Label
	}
	return node.Name
}

func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func formatRelPath(base, target string) string {
	return pathutil.RelPath(base, target)
}
