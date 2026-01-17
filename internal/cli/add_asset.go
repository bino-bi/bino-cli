package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/pathutil"
)

// AssetType represents the type of asset.
type AssetType int

const (
	AssetTypeNone AssetType = iota
	AssetTypeImage
	AssetTypeFont
	AssetTypeFile
)

// String returns a human-readable name for the asset type.
func (a AssetType) String() string {
	switch a {
	case AssetTypeImage:
		return "Image"
	case AssetTypeFont:
		return "Font"
	case AssetTypeFile:
		return "File"
	default:
		return "None"
	}
}

// TypeString returns the YAML type string for the asset.
func (a AssetType) TypeString() string {
	switch a {
	case AssetTypeImage:
		return "image"
	case AssetTypeFont:
		return "font"
	case AssetTypeFile:
		return "file"
	default:
		return ""
	}
}

// AssetManifestData holds data for rendering an Asset manifest.
type AssetManifestData struct {
	Name        string
	Description string
	Constraints []string
	Type        AssetType
	MediaType   string // MIME type (e.g., image/png)
	LocalPath   string // Local file path
	RemoteURL   string // Remote URL
	InlineData  string // Base64 inline data
}

func newAddAssetCommand() *cobra.Command {
	var (
		flagType       string
		flagMediaType  string
		flagPath       string
		flagURL        string
		flagConstraint []string
		flagOutput     string
		flagAppendTo   string
		flagDesc       string
		flagNoPrompt   bool
		flagOpenEditor bool
	)

	cmd := &cobra.Command{
		Use:   "asset [name]",
		Short: "Create an Asset manifest",
		Long: strings.TrimSpace(`
Create a new Asset manifest for images, fonts, or other files.

Assets can be sourced from:
  - Local file path
  - Remote URL
  - Inline base64 data

Common asset types:
  - image: PNG, JPEG, SVG images for reports
  - font: Custom TTF/OTF fonts
  - file: Other files (PDFs, etc.)
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add asset

  # Image from local file
  bino add asset company_logo \
    --type image \
    --path assets/logo.png \
    --output assets/logo.yaml \
    --no-prompt

  # Font from URL
  bino add asset custom_font \
    --type font \
    --url https://fonts.example.com/roboto.ttf \
    --media-type font/ttf \
    --output assets/fonts.yaml \
    --no-prompt
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

			// Parse type from flag
			var assetType AssetType
			switch strings.ToLower(flagType) {
			case "image":
				assetType = AssetTypeImage
			case "font":
				assetType = AssetTypeFont
			case "file":
				assetType = AssetTypeFile
			}

			// Validate source flags
			sourceCount := 0
			if flagPath != "" {
				sourceCount++
			}
			if flagURL != "" {
				sourceCount++
			}
			if sourceCount > 1 {
				return ConfigError(fmt.Errorf("only one of --path or --url can be specified"))
			}

			// In non-interactive mode, validate required flags
			if nonInteractive {
				var missing []string
				if name == "" {
					missing = append(missing, "name (as argument)")
				}
				if assetType == AssetTypeNone {
					missing = append(missing, "--type")
				}
				if sourceCount == 0 {
					missing = append(missing, "--path or --url")
				}
				if flagOutput == "" && flagAppendTo == "" {
					missing = append(missing, "--output or --append-to")
				}
				if len(missing) > 0 {
					return ConfigError(fmt.Errorf("missing required values in non-interactive mode:\n  %s\n\nRun without --no-prompt for interactive mode", strings.Join(missing, "\n  ")))
				}
			}

			manifests, err := ScanManifests(ctx, workdir)
			if err != nil {
				return RuntimeError(fmt.Errorf("scan manifests: %w", err))
			}

			data := AssetManifestData{
				Name:        name,
				Description: flagDesc,
				Constraints: flagConstraint,
				Type:        assetType,
				MediaType:   flagMediaType,
				LocalPath:   flagPath,
				RemoteURL:   flagURL,
			}

			var outputPath string
			var appendMode bool
			if flagAppendTo != "" {
				outputPath = flagAppendTo
				appendMode = true
			} else if flagOutput != "" {
				outputPath = flagOutput
				appendMode = false
			}

			if nonInteractive {
				return writeAssetManifest(cmd, workdir, data, outputPath, appendMode)
			}

			// Interactive wizard
			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new Asset manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Step 1: Name
			if data.Name == "" {
				var err error
				data.Name, err = promptAssetName(reader, out, manifests)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			} else {
				if err := ValidateName(data.Name); err != nil {
					return ConfigError(err)
				}
				if !IsNameUnique(manifests, "Asset", data.Name) {
					existing := FindByName(manifests, "Asset", data.Name)
					return ConfigError(fmt.Errorf("an Asset named %q already exists in %s:%d", data.Name, existing.File, existing.Position))
				}
			}

			if data.Description == "" {
				desc, err := addPromptString(reader, out, "Description (optional)", "")
				if err != nil {
					return RuntimeError(err)
				}
				data.Description = desc
			}

			// Step 2: Type
			if data.Type == AssetTypeNone {
				assetType, err := promptAssetType(reader, out)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
				data.Type = assetType
			}

			// Step 3: Source
			if data.LocalPath == "" && data.RemoteURL == "" {
				if err := promptAssetSource(reader, out, workdir, &data); err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Step 4: Media Type (auto-detect or prompt)
			if data.MediaType == "" {
				data.MediaType = detectMediaType(data)
				if data.MediaType == "" {
					mediaType, err := addPromptString(reader, out, "Media type (e.g., image/png)", "")
					if err != nil {
						return RuntimeError(err)
					}
					data.MediaType = mediaType
				}
			}

			// Step 5: Constraints
			if len(data.Constraints) == 0 {
				addConstraints, err := addPromptConfirm(reader, out, "Add constraints to conditionally include this Asset?", false)
				if err != nil {
					return RuntimeError(err)
				}
				if addConstraints {
					constraints, err := addPromptConstraintBuilder(reader, out)
					if err != nil {
						return RuntimeError(err)
					}
					data.Constraints = constraints
				}
			}

			// Step 6: Output location
			if outputPath == "" {
				var err error
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "Asset", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Step 7: Preview & Confirmation
			manifest := RenderAssetManifest(data)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, manifest)
			fmt.Fprintln(out, "===============")
			fmt.Fprintln(out)

			if appendMode {
				fmt.Fprintf(out, "Will append to: %s\n", outputPath)
			} else {
				fmt.Fprintf(out, "Will create: %s\n", outputPath)
			}

			confirmed, err := addPromptConfirm(reader, out, "Proceed?", true)
			if err != nil {
				return RuntimeError(err)
			}
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			if err := writeAssetManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
				return err
			}

			if flagOpenEditor {
				editor := getEditor()
				if editor != "" {
					args := buildEditorArgs(editor, filepath.Join(workdir, outputPath))
					execCmd := exec.Command(args[0], args[1:]...)
					execCmd.Stdin = os.Stdin
					execCmd.Stdout = os.Stdout
					execCmd.Stderr = os.Stderr
					_ = execCmd.Run()
				}
			}

			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagType, "type", "", "Asset type (image, font, file)")
	cmd.Flags().StringVar(&flagMediaType, "media-type", "", "MIME type (e.g., image/png)")
	cmd.Flags().StringVar(&flagPath, "path", "", "Local file path")
	cmd.Flags().StringVar(&flagURL, "url", "", "Remote URL")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing multi-doc YAML file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	// Shell completion
	_ = cmd.RegisterFlagCompletionFunc("type", completeAssetTypes)

	return cmd
}

func completeAssetTypes(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"image\tImage file (PNG, JPEG, SVG)",
		"font\tFont file (TTF, OTF, WOFF)",
		"file\tGeneric file",
	}, cobra.ShellCompDirectiveNoFileComp
}

func promptAssetName(reader *bufio.Reader, out io.Writer, manifests []ManifestInfo) (string, error) {
	return addPromptAddString(reader, out, "Name for this Asset", "", func(name string) error {
		if err := ValidateName(name); err != nil {
			return err
		}
		if !IsNameUnique(manifests, "Asset", name) {
			existing := FindByName(manifests, "Asset", name)
			return fmt.Errorf("an Asset named %q already exists in %s:%d", name, existing.File, existing.Position)
		}
		return nil
	})
}

func promptAssetType(reader *bufio.Reader, out io.Writer) (AssetType, error) {
	options := []SelectOption{
		{Label: "Image", Description: "PNG, JPEG, SVG image file"},
		{Label: "Font", Description: "TTF, OTF, WOFF font file"},
		{Label: "File", Description: "Generic file (PDF, etc.)"},
	}

	idx, err := addPromptSelect(reader, out, "What type of asset?", options, 0)
	if err != nil {
		return AssetTypeNone, err
	}

	types := []AssetType{AssetTypeImage, AssetTypeFont, AssetTypeFile}
	return types[idx], nil
}

func promptAssetSource(reader *bufio.Reader, out io.Writer, workdir string, data *AssetManifestData) error {
	options := []SelectOption{
		{Label: "Local file", Description: "Reference a file in the project"},
		{Label: "Remote URL", Description: "Reference a file from a URL"},
	}

	idx, err := addPromptSelect(reader, out, "Asset source", options, 0)
	if err != nil {
		return err
	}

	switch idx {
	case 0: // Local file
		ext := getAssetExtension(data.Type)
		files, _ := searchAssetFiles(workdir, ext)

		if len(files) > 0 {
			subOptions := []SelectOption{
				{Label: "Select existing file", Description: fmt.Sprintf("Choose from %d found files", len(files))},
				{Label: "Enter path manually", Description: "Type a file path"},
			}

			subIdx, err := addPromptSelect(reader, out, "File source", subOptions, 0)
			if err != nil {
				return err
			}

			if subIdx == 0 {
				items := FilesToFuzzyItems(files, data.Type.String())
				item, err := addPromptFuzzySearch(reader, out, "Select file", items, false)
				if err != nil {
					return err
				}
				if item == nil {
					return errAddCanceled
				}
				data.LocalPath = item.Name
				return nil
			}
		}

		path, err := addPromptString(reader, out, "File path", "")
		if err != nil {
			return err
		}
		data.LocalPath = path

	case 1: // Remote URL
		url, err := addPromptString(reader, out, "Remote URL", "")
		if err != nil {
			return err
		}
		data.RemoteURL = url
	}

	return nil
}

func getAssetExtension(t AssetType) []string {
	switch t {
	case AssetTypeImage:
		return []string{".png", ".jpg", ".jpeg", ".svg", ".gif", ".webp"}
	case AssetTypeFont:
		return []string{".ttf", ".otf", ".woff", ".woff2"}
	case AssetTypeFile:
		return []string{".pdf", ".doc", ".docx"}
	default:
		return nil
	}
}

func searchAssetFiles(dir string, exts []string) ([]string, error) {
	var files []string

	searchDirs := []string{".", "assets", "images", "fonts", "files"}

	for _, searchDir := range searchDirs {
		searchPath := filepath.Join(dir, searchDir)
		if _, err := os.Stat(searchPath); os.IsNotExist(err) {
			continue
		}

		err := filepath.WalkDir(searchPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			for _, e := range exts {
				if ext == e {
					rel := path
					if r, err := filepath.Rel(dir, path); err == nil {
						rel = r
					}
					files = append(files, rel)
					break
				}
			}
			return nil
		})
		if err != nil {
			continue
		}
	}

	return files, nil
}

func detectMediaType(data AssetManifestData) string {
	path := data.LocalPath
	if path == "" {
		path = data.RemoteURL
	}
	if path == "" {
		return ""
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".svg":
		return "image/svg+xml"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".pdf":
		return "application/pdf"
	default:
		return ""
	}
}

func writeAssetManifest(cmd *cobra.Command, workdir string, data AssetManifestData, outputPath string, appendMode bool) error {
	out := cmd.OutOrStdout()

	if err := ValidateName(data.Name); err != nil {
		return ConfigError(err)
	}

	manifest := RenderAssetManifest(data)

	absPath := outputPath
	if !filepath.IsAbs(outputPath) {
		absPath = filepath.Join(workdir, outputPath)
	}

	if appendMode {
		if err := AppendToManifest(absPath, manifest); err != nil {
			return RuntimeError(err)
		}
		fmt.Fprintf(out, "Appended to %s\n", outputPath)
	} else {
		if err := WriteManifest(absPath, manifest); err != nil {
			return RuntimeError(err)
		}
		fmt.Fprintf(out, "Created %s\n", outputPath)
	}

	return nil
}

// RenderAssetManifest renders an Asset manifest from the given data.
func RenderAssetManifest(data AssetManifestData) string {
	var b strings.Builder

	b.WriteString("apiVersion: bino.bi/v1alpha1\n")
	b.WriteString("kind: Asset\n")
	b.WriteString("metadata:\n")
	b.WriteString(fmt.Sprintf("  name: %s\n", data.Name))

	if data.Description != "" {
		b.WriteString(fmt.Sprintf("  description: %s\n", quoteYAMLIfNeeded(data.Description)))
	}

	if len(data.Constraints) > 0 {
		b.WriteString("  constraints:\n")
		for _, c := range data.Constraints {
			b.WriteString(fmt.Sprintf("    - %s\n", quoteYAMLIfNeeded(c)))
		}
	}

	b.WriteString("spec:\n")
	b.WriteString(fmt.Sprintf("  type: %s\n", data.Type.TypeString()))

	if data.MediaType != "" {
		b.WriteString(fmt.Sprintf("  mediaType: %s\n", data.MediaType))
	}

	switch {
	case data.LocalPath != "":
		b.WriteString(fmt.Sprintf("  source:\n    localPath: %s\n", data.LocalPath))
	case data.RemoteURL != "":
		b.WriteString(fmt.Sprintf("  source:\n    remoteURL: %s\n", data.RemoteURL))
	case data.InlineData != "":
		b.WriteString(fmt.Sprintf("  source:\n    inlineBase64: %s\n", data.InlineData))
	}

	return b.String()
}
