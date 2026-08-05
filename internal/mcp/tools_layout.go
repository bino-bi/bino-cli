package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/report/layoutstate"
)

type layoutStateInput struct {
	Artefact  string `json:"artefact,omitempty" jsonschema:"metadata.name of the ReportArtefact to inspect (default: the only one, or an error when the project has several)"`
	Component string `json:"component,omitempty" jsonschema:"component id or manifest name to describe in full — omit for the whole-report summary"`
}

// layoutComponent is the compact per-component view returned to an agent.
// The raw snapshot is deliberately not returned: it repeats geometry the agent
// cannot act on, and a full-detail table runs to megabytes.
type layoutComponent struct {
	ID       string   `json:"id"`
	Tag      string   `json:"tag"`
	Kind     string   `json:"kind,omitempty"`
	Name     string   `json:"name,omitempty"`
	Width    float64  `json:"width"`
	Height   float64  `json:"height"`
	Counts   string   `json:"counts,omitempty"`
	Scaling  string   `json:"scaling,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type layoutStateOutput struct {
	Artefact   string                `json:"artefact"`
	Findings   []layoutstate.Finding `json:"findings"`
	Components []layoutComponent     `json:"components,omitempty"`
	// Detail is the engine's full per-element geometry, present only when the
	// request named a component.
	Detail json.RawMessage `json:"detail,omitempty"`
	// SnapshotPath is where the raw capture was written, for an agent that
	// genuinely needs the untruncated data.
	SnapshotPath string `json:"snapshotPath,omitempty"`
	Note         string `json:"note,omitempty"`
}

func (h *handlers) registerLayoutTool(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "get_layout_state",
		Description: "Inspect what a report ACTUALLY rendered, by building it with a layout capture (runs headless Chrome — slow, writes files). " +
			"Reports per component the box, row/bar counts, resolved scaling and diagnostics, plus findings: components that rendered empty, " +
			"content that overflows, fonts auto-fitted down, and charts of the same measure left on diverging scales. " +
			"Use it to verify a report after build — validate_project only checks the manifests, not the rendered result. " +
			"Requires template engine v1.0.0-next.24 or newer.",
	}, h.runLayoutState)
}

func (h *handlers) runLayoutState(ctx context.Context, _ *mcpsdk.CallToolRequest, in layoutStateInput) (*mcpsdk.CallToolResult, layoutStateOutput, error) {
	// Serialized with `build`: both shell out to `bino build` writing the same
	// output directory.
	buildMu.Lock()
	defer buildMu.Unlock()

	root := h.deps.State.ProjectRoot()
	outDir := filepath.Join(root, "dist")

	artefact, err := h.resolveArtefact(in.Artefact)
	if err != nil {
		return nil, layoutStateOutput{}, err
	}

	if err := runLayoutCapture(ctx, root, artefact); err != nil {
		return nil, layoutStateOutput{}, err
	}

	snapshotPath, snap, err := readLayoutSnapshot(outDir, artefact)
	if err != nil {
		return nil, layoutStateOutput{}, err
	}

	out := layoutStateOutput{
		Artefact:     artefact,
		Findings:     layoutstate.Analyze(snap),
		SnapshotPath: snapshotPath,
	}
	if out.Findings == nil {
		out.Findings = []layoutstate.Finding{}
	}

	if in.Component == "" {
		out.Components = compactComponents(snap)
		return nil, out, nil
	}

	detail, ok := findComponent(snap, in.Component)
	if !ok {
		out.Components = compactComponents(snap)
		out.Note = fmt.Sprintf("no component %q in this report; listing all instead", in.Component)
		return nil, out, nil
	}
	// The capture is taken at summary detail, so this is the component's
	// metadata and regions rather than per-element geometry.
	raw, err := json.Marshal(detail)
	if err != nil {
		return nil, layoutStateOutput{}, fmt.Errorf("encode component detail: %w", err)
	}
	out.Detail = raw
	return nil, out, nil
}

// resolveArtefact picks the artefact to inspect, defaulting to the only
// ReportArtefact in the project.
func (h *handlers) resolveArtefact(requested string) (string, error) {
	if requested != "" {
		return requested, nil
	}
	var names []string
	for _, doc := range h.deps.State.Documents() {
		if doc.Kind == "ReportArtefact" {
			names = append(names, doc.Name)
		}
	}
	switch len(names) {
	case 0:
		return "", errors.New("no ReportArtefact in this project")
	case 1:
		return names[0], nil
	default:
		return "", fmt.Errorf("project has %d report artefacts (%s); pass one as `artefact`",
			len(names), strings.Join(names, ", "))
	}
}

func runLayoutCapture(ctx context.Context, root, artefact string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	args := []string{
		"build", "--work-dir", root, "--out-dir", "dist",
		"--layout-state", "--artefact", artefact, "--log-format", "json",
	}
	cmd := exec.CommandContext(ctx, exe, args...) //nolint:gosec // G204: exe is our own binary, args are controlled
	cmd.Env = append(os.Environ(), "BINO_DISABLE_UPDATE_CHECK=1", "NO_COLOR=1")

	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("build with layout capture failed: %w\n%s", runErr, tail(string(output), maxBuildOutput))
	}
	return nil
}

// readLayoutSnapshot loads the capture the build wrote. The build derives the
// filename from the artefact's PDF, which the artefact spec may rename, so
// fall back to the only capture present.
func readLayoutSnapshot(outDir, artefact string) (string, layoutstate.Snapshot, error) {
	candidate := filepath.Join(outDir, artefact+".layout.json")
	data, err := os.ReadFile(candidate) //nolint:gosec // G304: path built from our own out-dir
	if err != nil {
		matches, _ := filepath.Glob(filepath.Join(outDir, "*.layout.json"))
		if len(matches) != 1 {
			return "", layoutstate.Snapshot{}, fmt.Errorf(
				"no layout capture for %q — the template engine must be v1.0.0-next.24 or newer", artefact)
		}
		candidate = matches[0]
		data, err = os.ReadFile(candidate) //nolint:gosec // G304: path from our own out-dir glob
		if err != nil {
			return "", layoutstate.Snapshot{}, fmt.Errorf("read layout capture: %w", err)
		}
	}

	var snap layoutstate.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return "", layoutstate.Snapshot{}, fmt.Errorf("parse layout capture: %w", err)
	}
	if !layoutstate.SupportedVersion(snap.State.Version) {
		return "", layoutstate.Snapshot{}, fmt.Errorf(
			"layout capture version %d is not supported by this CLI", snap.State.Version)
	}
	return candidate, snap, nil
}

func compactComponents(snap layoutstate.Snapshot) []layoutComponent {
	out := make([]layoutComponent, 0, len(snap.State.Components))
	for _, c := range snap.State.Components {
		src := snap.Sources[c.ID]
		entry := layoutComponent{
			ID:      c.ID,
			Tag:     c.Tag,
			Kind:    src.Kind,
			Name:    src.Name,
			Width:   c.Rect.Component.Width,
			Height:  c.Rect.Component.Height,
			Counts:  formatCounts(c.Metadata),
			Scaling: formatScaling(c.Scaling),
		}
		for _, d := range c.Diagnostics {
			if d.ID != "" {
				entry.Warnings = append(entry.Warnings, d.ID)
			}
		}
		out = append(out, entry)
	}
	return out
}

// findComponent matches on the engine id first, then the manifest name, so an
// agent can use whichever identifier it has.
func findComponent(snap layoutstate.Snapshot, want string) (layoutstate.Component, bool) {
	for _, c := range snap.State.Components {
		if c.ID == want {
			return c, true
		}
	}
	for _, c := range snap.State.Components {
		src := snap.Sources[c.ID]
		if src.Name == want || src.Ref == want {
			return c, true
		}
	}
	return layoutstate.Component{}, false
}

func formatCounts(m layoutstate.Metadata) string {
	var parts []string
	add := func(label string, v *int) {
		if v != nil {
			parts = append(parts, fmt.Sprintf("%d %s", *v, label))
		}
	}
	add("bars", m.BarCount)
	add("points", m.PointCount)
	add("rows", m.RowCount)
	add("columns", m.ColumnCount)
	add("nodes", m.NodeCount)
	return strings.Join(parts, ", ")
}

func formatScaling(s *layoutstate.Scaling) string {
	if s == nil || s.UnitsPerEm == nil {
		return ""
	}
	mode := s.UnitMode
	if mode == "" {
		mode = "auto"
	}
	return fmt.Sprintf("%s %g units/em", mode, *s.UnitsPerEm)
}
