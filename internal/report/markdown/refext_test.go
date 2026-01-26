package markdown

import (
	"bytes"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

func TestRefExtension(t *testing.T) {
	md := goldmark.New(
		goldmark.WithExtensions(Ref()),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic ref",
			input:    "See :ref[DataSet:sales] for details.",
			expected: `<p>See <bn-ref kind="DataSet" name="sales"></bn-ref> for details.</p>` + "\n",
		},
		{
			name:     "ref with hyphen in name",
			input:    "Check :ref[DataSource:my-source].",
			expected: `<p>Check <bn-ref kind="DataSource" name="my-source"></bn-ref>.</p>` + "\n",
		},
		{
			name:     "ref with underscore in name",
			input:    "Use :ref[Component:chart_widget] here.",
			expected: `<p>Use <bn-ref kind="Component" name="chart_widget"></bn-ref> here.</p>` + "\n",
		},
		{
			name:     "multiple refs",
			input:    "From :ref[DataSource:db] to :ref[DataSet:results].",
			expected: `<p>From <bn-ref kind="DataSource" name="db"></bn-ref> to <bn-ref kind="DataSet" name="results"></bn-ref>.</p>` + "\n",
		},
		{
			name:     "ref in heading",
			input:    "# About :ref[ReportArtefact:quarterly]\n\nContent here.",
			expected: `<h1 id="about-refreportartefactquarterly">About <bn-ref kind="ReportArtefact" name="quarterly"></bn-ref></h1>` + "\n<p>Content here.</p>\n",
		},
		{
			name:     "non-ref colon preserved",
			input:    "Time is 10:30 and :ref[DataSet:time] works.",
			expected: `<p>Time is 10:30 and <bn-ref kind="DataSet" name="time"></bn-ref> works.</p>` + "\n",
		},
		{
			name:     "ref with numbers in name",
			input:    "Check :ref[DataSet:data2024] for details.",
			expected: `<p>Check <bn-ref kind="DataSet" name="data2024"></bn-ref> for details.</p>` + "\n",
		},
		{
			name:     "invalid ref syntax not parsed",
			input:    "This :ref[invalid] is not a ref.",
			expected: `<p>This :ref[invalid] is not a ref.</p>` + "\n",
		},
		{
			name:     "empty kind not parsed",
			input:    "This :ref[:name] is invalid.",
			expected: `<p>This :ref[:name] is invalid.</p>` + "\n",
		},
		{
			name:     "empty name not parsed",
			input:    "This :ref[Kind:] is invalid.",
			expected: `<p>This :ref[Kind:] is invalid.</p>` + "\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := md.Convert([]byte(tc.input), &buf); err != nil {
				t.Fatalf("Convert failed: %v", err)
			}

			got := buf.String()
			if got != tc.expected {
				t.Errorf("mismatch:\n  input:    %q\n  got:      %q\n  expected: %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestRefExtensionWithCaption(t *testing.T) {
	md := goldmark.New(
		goldmark.WithExtensions(Ref()),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ref with caption",
			input:    `:ref[Table:sales]{caption="Table 1: Quarterly Sales"}`,
			expected: `<p><figure class="bn-figure"><bn-ref kind="Table" name="sales"></bn-ref><figcaption>Table 1: Quarterly Sales</figcaption></figure></p>` + "\n",
		},
		{
			name:     "ref with empty caption",
			input:    `:ref[Table:data]{caption=""}`,
			expected: `<p><bn-ref kind="Table" name="data"></bn-ref></p>` + "\n",
		},
		{
			name:     "ref without caption unchanged",
			input:    `:ref[Table:plain]`,
			expected: `<p><bn-ref kind="Table" name="plain"></bn-ref></p>` + "\n",
		},
		{
			name:     "ref with caption containing special chars",
			input:    `:ref[Image:logo]{caption="Company & Logo <2024>"}`,
			expected: `<p><figure class="bn-figure"><bn-ref kind="Image" name="logo"></bn-ref><figcaption>Company &amp; Logo &lt;2024&gt;</figcaption></figure></p>` + "\n",
		},
		{
			name:     "multiple refs with mixed captions",
			input:    `:ref[Table:a]{caption="First"} and :ref[Table:b]`,
			expected: `<p><figure class="bn-figure"><bn-ref kind="Table" name="a"></bn-ref><figcaption>First</figcaption></figure> and <bn-ref kind="Table" name="b"></bn-ref></p>` + "\n",
		},
		{
			name:     "ref with caption in text",
			input:    `See :ref[ChartTime:revenue]{caption="Figure 1"} for details.`,
			expected: `<p>See <figure class="bn-figure"><bn-ref kind="ChartTime" name="revenue"></bn-ref><figcaption>Figure 1</figcaption></figure> for details.</p>` + "\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := md.Convert([]byte(tc.input), &buf); err != nil {
				t.Fatalf("Convert failed: %v", err)
			}

			got := buf.String()
			if got != tc.expected {
				t.Errorf("mismatch:\n  input:    %q\n  got:      %q\n  expected: %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestRefNodeKind(t *testing.T) {
	node := &RefNode{
		RefKind: "DataSet",
		RefName: "test",
	}

	if node.Kind() != KindRefNode {
		t.Errorf("expected KindRefNode, got %v", node.Kind())
	}
}

func TestRefNodeWithCaption(t *testing.T) {
	node := &RefNode{
		RefKind: "Table",
		RefName: "sales",
		Caption: "Table 1",
	}

	if node.Kind() != KindRefNode {
		t.Errorf("expected KindRefNode, got %v", node.Kind())
	}
	if node.Caption != "Table 1" {
		t.Errorf("expected Caption 'Table 1', got %q", node.Caption)
	}
}
