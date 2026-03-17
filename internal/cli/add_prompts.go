package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
	"golang.org/x/term"
)

// Theme colors for the add wizard
var (
	successColor = lipgloss.Color("42") // Green

	successStyle = lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true)
)

// SelectOption represents an option in a select prompt.
type SelectOption struct {
	Label       string
	Description string
	Value       any
}

// FuzzyItem represents an item that can be fuzzy searched.
type FuzzyItem struct {
	Name     string
	Kind     string
	File     string
	Position int
}

// String returns the searchable string for fuzzy matching.
func (f FuzzyItem) String() string {
	return f.Name
}

// Display returns the formatted display string.
func (f FuzzyItem) Display() string {
	rel := f.File
	if wd, err := os.Getwd(); err == nil {
		if r, err := filepath.Rel(wd, f.File); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	return fmt.Sprintf("%s (%s, %s:%d)", f.Name, f.Kind, rel, f.Position)
}

// FuzzyItems implements fuzzy.Source interface for FuzzyItem slices.
type FuzzyItems []FuzzyItem

func (f FuzzyItems) Len() int            { return len(f) }
func (f FuzzyItems) String(i int) string { return f[i].Name }

// getHuhTheme returns a customized huh theme.
func getHuhTheme() *huh.Theme {
	t := huh.ThemeCharm()
	return t
}

// huhSelect displays an interactive select menu with arrow key navigation.
func huhSelect(title string, options []SelectOption, def int) (int, error) {
	if len(options) == 0 {
		return -1, fmt.Errorf("no options provided")
	}

	// Build huh options
	huhOptions := make([]huh.Option[int], len(options))
	for i, opt := range options {
		label := opt.Label
		if opt.Description != "" {
			label = fmt.Sprintf("%s - %s", opt.Label, opt.Description)
		}
		huhOptions[i] = huh.NewOption(label, i)
	}

	var selected int
	if def >= 0 && def < len(options) {
		selected = def
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title(title).
				Options(huhOptions...).
				Value(&selected),
		),
	).WithTheme(getHuhTheme())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return -1, errAddCanceled
		}
		return -1, err
	}

	return selected, nil
}

// huhInput displays an interactive text input with optional validation.
func huhInput(title, placeholder string, def string, validate func(string) error) (string, error) {
	var value string
	if def != "" {
		value = def
	}

	input := huh.NewInput().
		Title(title).
		Value(&value)

	if placeholder != "" {
		input = input.Placeholder(placeholder)
	}

	if validate != nil {
		input = input.Validate(validate)
	}

	form := huh.NewForm(
		huh.NewGroup(input),
	).WithTheme(getHuhTheme())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", errAddCanceled
		}
		return "", err
	}

	return value, nil
}

// huhConfirm displays an interactive yes/no confirmation.
func huhConfirm(title string) (bool, error) {
	var confirmed bool

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Affirmative("Yes").
				Negative("No").
				Value(&confirmed),
		),
	).WithTheme(getHuhTheme())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, errAddCanceled
		}
		return false, err
	}

	return confirmed, nil
}

// huhFuzzySelect displays a filterable select for fuzzy searching.
func huhFuzzySelect(title string, items []FuzzyItem, allowEmpty bool) (*FuzzyItem, error) {
	if len(items) == 0 {
		if allowEmpty {
			return nil, nil
		}
		return nil, fmt.Errorf("no items to search")
	}

	// Build huh options with filtering support
	huhOptions := make([]huh.Option[int], len(items))
	for i, item := range items {
		huhOptions[i] = huh.NewOption(item.Display(), i)
	}

	// Add skip option if allowed
	skipIdx := -1
	if allowEmpty {
		skipIdx = len(items)
		huhOptions = append(huhOptions, huh.NewOption("(Skip)", skipIdx))
	}

	var selected int
	if allowEmpty {
		selected = skipIdx
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title(title).
				Options(huhOptions...).
				Value(&selected).
				Filtering(true).
				Height(15),
		),
	).WithTheme(getHuhTheme())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, errAddCanceled
		}
		return nil, err
	}

	if selected == skipIdx {
		return nil, nil
	}

	item := items[selected]
	return &item, nil
}

// huhMultiFuzzySelect allows selecting multiple items with fuzzy filtering.
func huhMultiFuzzySelect(title string, items []FuzzyItem) ([]FuzzyItem, error) {
	if len(items) == 0 {
		return nil, nil
	}

	// Build huh options
	huhOptions := make([]huh.Option[int], len(items))
	for i, item := range items {
		huhOptions[i] = huh.NewOption(item.Display(), i)
	}

	var selectedIndices []int

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title(title).
				Description("Use space to select, enter to confirm").
				Options(huhOptions...).
				Value(&selectedIndices).
				Filterable(true).
				Height(15),
		),
	).WithTheme(getHuhTheme())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, errAddCanceled
		}
		return nil, err
	}

	selected := make([]FuzzyItem, len(selectedIndices))
	for i, idx := range selectedIndices {
		selected[i] = items[idx]
	}

	return selected, nil
}

// huhConstraintBuilder helps build constraint expressions interactively.
func huhConstraintBuilder() ([]string, error) {
	var constraints []string

	commonOptions := []huh.Option[string]{
		huh.NewOption("Preview-only (mode == preview)", "mode == preview"),
		huh.NewOption("Build-only (mode == build)", "mode == build"),
		huh.NewOption("Serve-only (mode == serve)", "mode == serve"),
		huh.NewOption("PDF format only (spec.format == pdf)", "spec.format == pdf"),
		huh.NewOption("XGA format only (spec.format == xga)", "spec.format == xga"),
		huh.NewOption("Custom expression...", "custom"),
		huh.NewOption("[Done - finish adding constraints]", "done"),
	}

	for {
		var selected string

		title := "Select constraint to add"
		if len(constraints) > 0 {
			title = fmt.Sprintf("Add another constraint (current: %s)", strings.Join(constraints, "; "))
		}

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(title).
					Options(commonOptions...).
					Value(&selected),
			),
		).WithTheme(getHuhTheme())

		if err := form.Run(); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				return constraints, errAddCanceled
			}
			return constraints, err
		}

		switch selected {
		case "done":
			return constraints, nil
		case "custom":
			expr, err := huhCustomConstraint()
			if err != nil {
				return constraints, err
			}
			if expr != "" {
				constraints = append(constraints, expr)
				fmt.Println(successStyle.Render(fmt.Sprintf("  Added: %s", expr)))
			}
		default:
			constraints = append(constraints, selected)
			fmt.Println(successStyle.Render(fmt.Sprintf("  Added: %s", selected)))
		}
	}
}

// huhCustomConstraint prompts for a custom constraint expression.
func huhCustomConstraint() (string, error) {
	// Field selection
	var field string
	fieldOptions := []huh.Option[string]{
		huh.NewOption("mode - Execution mode (build/preview/serve)", "mode"),
		huh.NewOption("spec.format - Output format (pdf/xga)", "spec.format"),
		huh.NewOption("spec.language - Report language", "spec.language"),
		huh.NewOption("labels.<key> - Custom label value", "labels."),
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select field").
				Options(fieldOptions...).
				Value(&field),
		),
	).WithTheme(getHuhTheme())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", errAddCanceled
		}
		return "", err
	}

	// If labels, prompt for key
	if field == "labels." {
		var labelKey string
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Label key (e.g., env)").
					Value(&labelKey).
					Placeholder("env"),
			),
		).WithTheme(getHuhTheme())

		if err := form.Run(); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				return "", errAddCanceled
			}
			return "", err
		}
		field = "labels." + labelKey
	}

	// Operator selection
	var op string
	opOptions := []huh.Option[string]{
		huh.NewOption("== (Equals)", "=="),
		huh.NewOption("!= (Not equals)", "!="),
		huh.NewOption("in (In list)", "in"),
		huh.NewOption("not-in (Not in list)", "not-in"),
	}

	form = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select operator").
				Options(opOptions...).
				Value(&op),
		),
	).WithTheme(getHuhTheme())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", errAddCanceled
		}
		return "", err
	}

	// Value input
	var value string
	var valueTitle string
	var valuePlaceholder string

	if op == "in" || op == "not-in" {
		valueTitle = "Values (comma-separated)"
		valuePlaceholder = "dev, staging, prod"
	} else {
		valueTitle = "Value"
		valuePlaceholder = ""
	}

	form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(valueTitle).
				Value(&value).
				Placeholder(valuePlaceholder),
		),
	).WithTheme(getHuhTheme())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", errAddCanceled
		}
		return "", err
	}

	// Format value for in/not-in operators
	if op == "in" || op == "not-in" {
		parts := strings.Split(value, ",")
		trimmed := make([]string, len(parts))
		for i, p := range parts {
			trimmed[i] = strings.TrimSpace(p)
		}
		value = "[" + strings.Join(trimmed, ", ") + "]"
	}

	return fmt.Sprintf("%s %s %s", field, op, value), nil
}

// Legacy function wrappers for backward compatibility with add commands
// These ignore the reader/out parameters and use huh instead

// addPromptSelect wraps huhSelect for backward compatibility.
func addPromptSelect(_ interface{}, _ interface{}, label string, options []SelectOption) (int, error) {
	return huhSelect(label, options, 0)
}

// addPromptString wraps huhInput for backward compatibility.
func addPromptString(_ interface{}, _ interface{}, label, def string) (string, error) {
	return huhInput(label, "", def, nil)
}

// addPromptConfirm wraps huhConfirm for backward compatibility.
func addPromptConfirm(_ interface{}, _ interface{}, label string, _ bool) (bool, error) {
	return huhConfirm(label)
}

// addPromptAddString wraps huhInput with validation for backward compatibility.
func addPromptAddString(_ interface{}, _ interface{}, label string, validate func(string) error) (string, error) {
	return huhInput(label, "", "", validate)
}

// addPromptFuzzySearch wraps huhFuzzySelect for backward compatibility.
func addPromptFuzzySearch(_ interface{}, _ interface{}, label string, items []FuzzyItem) (*FuzzyItem, error) {
	return huhFuzzySelect(label, items, false)
}

// addPromptMultiFuzzySearch wraps huhMultiFuzzySelect for backward compatibility.
func addPromptMultiFuzzySearch(_ interface{}, _ interface{}, label string, items []FuzzyItem) ([]FuzzyItem, error) {
	return huhMultiFuzzySelect(label, items)
}

// addPromptConstraintBuilder wraps huhConstraintBuilder for backward compatibility.
func addPromptConstraintBuilder(_ interface{}, _ interface{}) ([]string, error) {
	return huhConstraintBuilder()
}

// promptWithEditor opens the user's preferred editor to edit content.
// Returns the edited content or an error.
func promptWithEditor(tempPrefix, ext, template string) (string, error) {
	// Create temp file
	tmpDir := os.TempDir()
	tmpFile, err := os.CreateTemp(tmpDir, tempPrefix+"*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Write template
	if _, err := tmpFile.WriteString(template); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("write template: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}

	// Find editor
	editor := getEditor()
	if editor == "" {
		return "", fmt.Errorf("no editor found (set $EDITOR or $VISUAL)")
	}

	// Build command with appropriate flags
	args := buildEditorArgs(editor, tmpPath)

	// Run editor
	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec,noctx // G204: intentionally launching user's editor; interactive editor, no cancellation needed
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor failed: %w", err)
	}

	// Read result
	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("read edited file: %w", err)
	}

	return string(content), nil
}

// getEditor returns the user's preferred editor.
func getEditor() string {
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}

	// Check for common editors in PATH
	for _, editor := range []string{"vim", "nano", "vi"} {
		if path, err := exec.LookPath(editor); err == nil {
			return path
		}
	}

	return ""
}

// buildEditorArgs constructs the command arguments for the editor.
// Adds --wait flag for GUI editors like VS Code.
func buildEditorArgs(editor, filePath string) []string {
	// Normalize editor path to basename for comparison
	base := filepath.Base(editor)

	// GUI editors that need --wait flag
	needsWait := map[string]bool{
		"code":          true, // VS Code
		"code-insiders": true,
		"codium":        true,
		"subl":          true, // Sublime Text
		"sublime_text":  true,
		"atom":          true,
		"zed":           true,
	}

	if needsWait[base] {
		return []string{editor, "--wait", filePath}
	}

	return []string{editor, filePath}
}

// isTerminal checks if the given file descriptor is a terminal.
func isTerminal(fd int) bool {
	return term.IsTerminal(fd)
}

// isInteractive checks if stdin is a terminal (interactive session).
func isInteractive() bool {
	return isTerminal(int(os.Stdin.Fd())) //nolint:gosec // G115: fd value fits in int on all supported platforms
}

// previewLines returns the first n lines of content for preview.
func previewLines(content string, n int) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= n {
		return content
	}
	preview := strings.Join(lines[:n], "\n")
	return fmt.Sprintf("%s\n... (%d more lines)", preview, len(lines)-n)
}

// Unused but kept for interface compatibility with fuzzy package
var _ fuzzy.Source = FuzzyItems{}
