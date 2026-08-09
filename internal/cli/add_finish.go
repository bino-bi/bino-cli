package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/schema"
)

// finishWizardConfirm asks the final Proceed? question. It is a package
// variable so tests can stub the interactive prompt — huh cannot run without
// a terminal.
var finishWizardConfirm = func() (bool, error) {
	return addPromptConfirm("Proceed?", true)
}

// finishWizard runs the shared tail of every `bino add` wizard: it prints the
// rendered manifest preview, the advisory notes, asks for confirmation, writes
// the document through the validate-then-write path, runs afterWrite, and
// optionally opens the created file in $EDITOR.
//
// doc is either a *schema.Document (written via WriteSchemaDocument) or a
// map[string]any (written via WriteRawDocument — the shape the parameterized
// LayoutPage/ReportArtefact builders emit).
//
// Confirmation errors are checked, not swallowed: Ctrl-C (errAddCanceled) and
// answering No both print "Canceled." and return (false, nil); any other
// prompt failure is a RuntimeError. The returned bool reports whether the
// manifest was written, so callers can gate post-write steps on it.
func finishWizard(cmd *cobra.Command, doc any, workdir, outputPath string, appendMode, openEditor bool, notes []string, afterWrite func() error) (bool, error) {
	out := cmd.OutOrStdout()

	var manifestBytes []byte
	var err error
	switch d := doc.(type) {
	case *schema.Document:
		manifestBytes, err = RenderSchemaDocument(d)
	case map[string]any:
		manifestBytes, err = yaml.Marshal(d)
	default:
		return false, RuntimeError(fmt.Errorf("finishWizard: unsupported document type %T", doc))
	}
	if err != nil {
		return false, RuntimeError(fmt.Errorf("render preview: %w", err))
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "=== Preview ===")
	fmt.Fprintln(out, string(manifestBytes))
	fmt.Fprintln(out, "===============")

	for _, note := range notes {
		fmt.Fprintln(out, note)
	}

	confirmed, err := finishWizardConfirm()
	if err != nil {
		if errors.Is(err, errAddCanceled) {
			fmt.Fprintln(out, "\nCanceled.")
			return false, nil
		}
		return false, RuntimeError(err)
	}
	if !confirmed {
		fmt.Fprintln(out, "\nCanceled.")
		return false, nil
	}

	switch d := doc.(type) {
	case *schema.Document:
		err = WriteSchemaDocument(d, workdir, outputPath, appendMode, out)
	case map[string]any:
		err = WriteRawDocument(d, workdir, outputPath, appendMode, out)
	}
	if err != nil {
		return false, err
	}

	if afterWrite != nil {
		if err := afterWrite(); err != nil {
			return true, err
		}
	}

	if openEditor {
		if editor := getEditor(); editor != "" {
			args := buildEditorArgs(editor, filepath.Join(workdir, outputPath))
			execCmd := exec.Command(args[0], args[1:]...) //nolint:gosec,noctx // G204: intentionally launching user's editor; interactive editor, no cancellation needed
			execCmd.Stdin = os.Stdin
			execCmd.Stdout = os.Stdout
			execCmd.Stderr = os.Stderr
			_ = execCmd.Run()
		}
	}

	return true, nil
}
