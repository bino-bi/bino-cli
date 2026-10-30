package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/schema"
)

// annotationTargetKinds are the kinds an Annotation can point at.
var annotationTargetKinds = []string{"ChartTime", "ChartStructure", "ChartScatter", "ChartBubble", "ChartBullet", "Table", "Text", "Tree", "Grid"}

// AnnotationManifestData holds data for rendering an Annotation manifest.
type AnnotationManifestData struct {
	Name        string
	Description string
	Constraints []string
	Mark        string
	Color       string
	Target      string   // Kind:name
	Point       []string // field=value
	To          string   // Kind:name
	ToPoint     []string // field=value
	Title       string
	Text        string
}

func newAddAnnotationCommand() *cobra.Command { //nolint:gocognit // mirrors the other wizards
	var (
		flagMark       string
		flagColor      string
		flagTarget     string
		flagPoint      []string
		flagTo         string
		flagToPoint    []string
		flagTitle      string
		flagText       string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
	)

	cmd := &cobra.Command{
		Use:   "annotation [name]",
		Short: "Create an Annotation manifest",
		Long: strings.TrimSpace(`
Create a new Annotation manifest: a numbered marker, a frame or an arrow on a
component or on one data point of it, with an optional text for an AnnotationList.

A point holds raw dataset values. Fields: rowGroup, category, subCategory, date,
scenario, row, part.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add annotation

  # A numbered marker on one bar
  bino add annotation plan_2024 \
    --target ChartTime:net_sales \
    --point date=2024-01-01 --point scenario=pl1 \
    --title "2024 Plan" --text "Software increases by 9.1 kEUR." \
    --output components/annotations.yaml --no-prompt

  # An arrow between two stack totals
  bino add annotation decline --mark arrow --color negative \
    --target ChartTime:net_sales --point date=2024-01-01 --point part=total \
    --to ChartTime:net_sales --to-point date=2028-01-01 --to-point part=total \
    --output components/annotations.yaml --no-prompt
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			workdir, err := pathutil.ResolveWorkdir(".")
			if err != nil {
				return ConfigError(err)
			}

			nonInteractive := flagNoPrompt || !isInteractive()

			data := AnnotationManifestData{
				Description: flagDesc,
				Constraints: flagConstraint,
				Mark:        flagMark,
				Color:       flagColor,
				Target:      flagTarget,
				Point:       flagPoint,
				To:          flagTo,
				ToPoint:     flagToPoint,
				Title:       flagTitle,
				Text:        flagText,
			}
			if len(args) > 0 {
				data.Name = args[0]
			}

			var outputPath string
			var appendMode bool
			if flagAppendTo != "" {
				outputPath = flagAppendTo
				appendMode = true
			} else if flagOutput != "" {
				outputPath = flagOutput
			}

			if nonInteractive {
				var missing []string
				if data.Name == "" {
					missing = append(missing, "name (as argument)")
				}
				if data.Target == "" {
					missing = append(missing, "--target")
				}
				if outputPath == "" {
					missing = append(missing, "--output or --append-to")
				}
				if len(missing) > 0 {
					return ConfigError(fmt.Errorf("missing required values in non-interactive mode:\n  %s", strings.Join(missing, "\n  ")))
				}
				doc, err := buildAnnotationDocument(data)
				if err != nil {
					return ConfigError(err)
				}
				return WriteSchemaDocument(doc, workdir, outputPath, appendMode, cmd.OutOrStdout())
			}

			manifests, err := ScanManifests(ctx, workdir)
			if err != nil {
				return RuntimeError(fmt.Errorf("scan manifests: %w", err))
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Create a new Annotation manifest.")
			fmt.Fprintln(out)

			canceled := func(err error) error {
				if errors.Is(err, errAddCanceled) {
					fmt.Fprintln(out, "\nCanceled.")
					return nil
				}
				return RuntimeError(err)
			}

			if data.Name == "" {
				if data.Name, err = promptGenericName(manifests, "Annotation"); err != nil {
					return canceled(err)
				}
			}
			if data.Mark == "" {
				marks := []string{"marker", "frame", "arrow"}
				idx, err := addPromptSelect("Mark", []SelectOption{
					{Label: "Numbered marker"},
					{Label: "Frame around the value"},
					{Label: "Arrow between two data points"},
				})
				if err != nil {
					return canceled(err)
				}
				data.Mark = marks[idx]
			}
			if data.Target == "" {
				if data.Target, err = promptAnnotationEnd(manifests, "Target component"); err != nil {
					return canceled(err)
				}
			}
			if len(data.Point) == 0 {
				if data.Point, err = promptAnnotationPoint("Point of the target"); err != nil {
					return canceled(err)
				}
			}
			if data.Mark == "arrow" {
				if data.To == "" {
					if data.To, err = promptAnnotationEnd(manifests, "End of the arrow"); err != nil {
						return canceled(err)
					}
				}
				if len(data.ToPoint) == 0 {
					if data.ToPoint, err = promptAnnotationPoint("Point of the end"); err != nil {
						return canceled(err)
					}
				}
			}
			if data.Title == "" {
				data.Title, _ = addPromptString("Title (optional)", "")
			}
			if data.Text == "" {
				data.Text, _ = addPromptString("Text (optional, inline Markdown)", "")
			}

			doc, err := buildAnnotationDocument(data)
			if err != nil {
				return ConfigError(err)
			}

			if outputPath == "" {
				if outputPath, appendMode, err = promptOutputLocation(workdir, manifests, "Annotation", data.Name); err != nil {
					return canceled(err)
				}
			}
			_, err = finishWizard(cmd, doc, workdir, outputPath, appendMode, false, nil, nil)
			return err
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagMark, "mark", "", "Mark: marker (default), frame or arrow")
	cmd.Flags().StringVar(&flagColor, "color", "", "Color name from the bn-annotation style, e.g. negative")
	cmd.Flags().StringVar(&flagTarget, "target", "", "Target component as Kind:name")
	cmd.Flags().StringArrayVar(&flagPoint, "point", nil, "Point of the target as field=value (repeatable)")
	cmd.Flags().StringVar(&flagTo, "to", "", "End of an arrow as Kind:name")
	cmd.Flags().StringArrayVar(&flagToPoint, "to-point", nil, "Point of the end as field=value (repeatable)")
	cmd.Flags().StringVar(&flagTitle, "title", "", "Title in the AnnotationList")
	cmd.Flags().StringVar(&flagText, "text", "", "Text in the AnnotationList (inline Markdown)")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")

	return cmd
}

func promptAnnotationEnd(manifests []ManifestInfo, label string) (string, error) {
	components := FilterByKind(manifests, annotationTargetKinds...)
	if len(components) == 0 {
		return addPromptRequiredString(label + " as Kind:name")
	}
	item, err := addPromptFuzzySearch(label, ManifestsToFuzzyItems(components))
	if err != nil {
		return "", err
	}
	if item == nil {
		return "", errAddCanceled
	}
	return item.Kind + ":" + item.Name, nil
}

func promptAnnotationPoint(label string) ([]string, error) {
	value, err := addPromptString(label+" as field=value, comma separated (empty: whole component)", "")
	if err != nil {
		return nil, err
	}
	return splitPointEntries(value), nil
}

// splitPointEntries splits field=value entries at commas. A part without "=" is
// the rest of the value before it: a raw value can hold a comma.
func splitPointEntries(value string) []string {
	var point []string
	for _, part := range strings.Split(value, ",") {
		switch {
		case strings.TrimSpace(part) == "":
		case strings.Contains(part, "=") || len(point) == 0:
			point = append(point, strings.TrimSpace(part))
		default:
			point[len(point)-1] += "," + strings.TrimRight(part, " ")
		}
	}
	return point
}

func buildAnnotationDocument(data AnnotationManifestData) (*schema.Document, error) {
	if len(data.ToPoint) > 0 && data.To == "" {
		return nil, errors.New("--to-point needs --to")
	}
	switch data.Mark {
	case "", "marker", "frame":
		if data.To != "" {
			return nil, errors.New("--to needs --mark arrow")
		}
	case "arrow":
		if data.To == "" {
			return nil, errors.New("--mark arrow needs --to")
		}
	default:
		return nil, fmt.Errorf("unknown mark %q: use marker, frame or arrow", data.Mark)
	}

	target, err := parseAnnotationTarget(data.Target, data.Point)
	if err != nil {
		return nil, err
	}
	doc := schema.NewDocument(schema.KindAnnotation, data.Name)
	doc.Metadata.Description = data.Description
	doc.Metadata.Constraints = schema.ConstraintListFromStrings(data.Constraints)
	spec := &schema.AnnotationSpec{
		Color:  data.Color,
		Target: target,
		Title:  data.Title,
		Text:   data.Text,
	}
	if data.Mark != "marker" {
		spec.Mark = data.Mark
	}
	if data.To != "" {
		to, err := parseAnnotationTarget(data.To, data.ToPoint)
		if err != nil {
			return nil, err
		}
		spec.To = &to
	}
	doc.Spec = spec
	return doc, nil
}

func parseAnnotationTarget(value string, point []string) (schema.AnnotationTarget, error) {
	kind, name, ok := strings.Cut(value, ":")
	if !ok || kind == "" || name == "" {
		return schema.AnnotationTarget{}, fmt.Errorf("target %q: use Kind:name, e.g. ChartTime:net_sales", value)
	}
	target := schema.AnnotationTarget{Kind: kind, Name: name}
	if len(point) == 0 {
		return target, nil
	}
	target.Point = &schema.AnnotationPoint{}
	fields := map[string]*string{
		"rowGroup":    &target.Point.RowGroup,
		"category":    &target.Point.Category,
		"subCategory": &target.Point.SubCategory,
		"date":        &target.Point.Date,
		"scenario":    &target.Point.Scenario,
		"row":         &target.Point.Row,
		"part":        &target.Point.Part,
	}
	for _, entry := range point {
		field, val, ok := strings.Cut(entry, "=")
		dest, known := fields[field]
		if !ok || !known {
			return schema.AnnotationTarget{}, fmt.Errorf("point %q: use field=value, with a field of rowGroup, category, subCategory, date, scenario, row, part", entry)
		}
		*dest = val
	}
	return target, nil
}

func newAddAnnotationListCommand() *cobra.Command {
	var (
		flagAnnotations []string
		flagStyle       string
		flagConstraint  []string
		flagOutput      string
		flagAppendTo    string
		flagDesc        string
		flagNoPrompt    bool
	)

	cmd := &cobra.Command{
		Use:   "annotationlist [name]",
		Short: "Create an AnnotationList manifest",
		Long: strings.TrimSpace(`
Create a new AnnotationList manifest: a layout child that prints the numbered
texts of the annotations on its page. Without --annotation it shows all of them.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add annotationlist

  # Two annotations in this order
  bino add annotationlist comments \
    --annotation plan_2024 --annotation software_cell \
    --output components/annotations.yaml --no-prompt
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			workdir, err := pathutil.ResolveWorkdir(".")
			if err != nil {
				return ConfigError(err)
			}

			nonInteractive := flagNoPrompt || !isInteractive()

			var name string
			if len(args) > 0 {
				name = args[0]
			}

			var outputPath string
			var appendMode bool
			if flagAppendTo != "" {
				outputPath = flagAppendTo
				appendMode = true
			} else if flagOutput != "" {
				outputPath = flagOutput
			}

			build := func() *schema.Document {
				doc := schema.NewDocument(schema.KindAnnotationList, name)
				doc.Metadata.Description = flagDesc
				doc.Metadata.Constraints = schema.ConstraintListFromStrings(flagConstraint)
				doc.Spec = &schema.AnnotationListSpec{Annotations: flagAnnotations, SelectedStyle: flagStyle}
				return doc
			}

			if nonInteractive {
				var missing []string
				if name == "" {
					missing = append(missing, "name (as argument)")
				}
				if outputPath == "" {
					missing = append(missing, "--output or --append-to")
				}
				if len(missing) > 0 {
					return ConfigError(fmt.Errorf("missing required values in non-interactive mode:\n  %s", strings.Join(missing, "\n  ")))
				}
				return WriteSchemaDocument(build(), workdir, outputPath, appendMode, cmd.OutOrStdout())
			}

			manifests, err := ScanManifests(ctx, workdir)
			if err != nil {
				return RuntimeError(fmt.Errorf("scan manifests: %w", err))
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Create a new AnnotationList manifest.")
			fmt.Fprintln(out)

			canceled := func(err error) error {
				if errors.Is(err, errAddCanceled) {
					fmt.Fprintln(out, "\nCanceled.")
					return nil
				}
				return RuntimeError(err)
			}

			if name == "" {
				if name, err = promptGenericName(manifests, "AnnotationList"); err != nil {
					return canceled(err)
				}
			}
			if len(flagAnnotations) == 0 {
				if annotations := FilterByKind(manifests, schema.KindAnnotation); len(annotations) > 0 {
					fmt.Fprintln(out, "Select the annotations in their order. Select none to show all of the page.")
					items, err := addPromptMultiFuzzySearch("Annotations", ManifestsToFuzzyItems(annotations))
					if err != nil {
						return canceled(err)
					}
					for _, item := range items {
						flagAnnotations = append(flagAnnotations, item.Name)
					}
				}
			}
			if outputPath == "" {
				if outputPath, appendMode, err = promptOutputLocation(workdir, manifests, "AnnotationList", name); err != nil {
					return canceled(err)
				}
			}
			_, err = finishWizard(cmd, build(), workdir, outputPath, appendMode, false, nil, nil)
			return err
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringArrayVar(&flagAnnotations, "annotation", nil, "Annotation to show, in this order (repeatable). Default: all of the page")
	cmd.Flags().StringVar(&flagStyle, "selected-style", "", "ComponentStyle that colors the circles")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")

	return cmd
}
