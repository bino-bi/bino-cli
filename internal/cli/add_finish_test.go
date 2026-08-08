package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/schema"
)

// stubFinishConfirm replaces the interactive Proceed? prompt for the duration
// of a test. huh cannot run without a terminal, so the confirm step is the one
// seam that must be stubbed; everything around it runs for real.
func stubFinishConfirm(t *testing.T, fn func() (bool, error)) {
	t.Helper()
	orig := finishWizardConfirm
	finishWizardConfirm = fn
	t.Cleanup(func() { finishWizardConfirm = orig })
}

func bufferCmd() (*cobra.Command, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	return cmd, buf
}

func TestFinishWizardWritesAndOrdersOutput(t *testing.T) {
	dir := t.TempDir()
	cmd, buf := bufferCmd()

	stubFinishConfirm(t, func() (bool, error) {
		buf.WriteString("[confirm]\n")
		return true, nil
	})

	doc := buildLayoutPageDocument(LayoutPageManifestData{Name: "finish_page"})
	wrote, err := finishWizard(cmd, doc, dir, "pages/finish_page.yaml", false, false,
		[]string{"\nNote: a test note."}, nil)
	if err != nil {
		t.Fatalf("finishWizard: %v", err)
	}
	if !wrote {
		t.Fatal("wrote = false, want true")
	}

	out := buf.String()
	previewStart := strings.Index(out, "=== Preview ===")
	previewEnd := strings.Index(out, "===============")
	note := strings.Index(out, "Note: a test note.")
	confirm := strings.Index(out, "[confirm]")
	created := strings.Index(out, "Created pages/finish_page.yaml")
	if previewStart < 0 || previewEnd < 0 || note < 0 || confirm < 0 || created < 0 {
		t.Fatalf("output is missing a stage:\n%s", out)
	}
	ordered := previewStart < previewEnd && previewEnd < note && note < confirm && confirm < created
	if !ordered {
		t.Errorf("stages out of order (preview %d, end %d, note %d, confirm %d, created %d):\n%s",
			previewStart, previewEnd, note, confirm, created, out)
	}

	content, err := os.ReadFile(filepath.Join(dir, "pages", "finish_page.yaml"))
	if err != nil {
		t.Fatalf("read written manifest: %v", err)
	}
	if err := schema.Validate(content); err != nil {
		t.Errorf("written manifest does not validate: %v", err)
	}
}

func TestFinishWizardRawDocumentGoesThroughValidatedWrite(t *testing.T) {
	dir := t.TempDir()
	cmd, _ := bufferCmd()
	stubFinishConfirm(t, func() (bool, error) { return true, nil })

	t.Run("valid raw document is written", func(t *testing.T) {
		doc := buildLayoutPageDocumentWithParams(LayoutPageManifestData{
			Name:   "raw_page",
			Params: []LayoutPageParamData{{Name: "region", Type: "string", Required: true}},
		})
		wrote, err := finishWizard(cmd, doc, dir, "raw_page.yaml", false, false, nil, nil)
		if err != nil || !wrote {
			t.Fatalf("finishWizard = (%v, %v), want (true, nil)", wrote, err)
		}
		content, err := os.ReadFile(filepath.Join(dir, "raw_page.yaml"))
		if err != nil {
			t.Fatalf("read written manifest: %v", err)
		}
		if err := schema.Validate(content); err != nil {
			t.Errorf("written manifest does not validate: %v", err)
		}
	})

	t.Run("invalid raw document is rejected before the write", func(t *testing.T) {
		doc := buildLayoutPageDocumentWithParams(LayoutPageManifestData{
			Name:   "_reserved_raw",
			Params: []LayoutPageParamData{{Name: "region", Type: "string"}},
		})
		wrote, err := finishWizard(cmd, doc, dir, "reserved_raw.yaml", false, false, nil, nil)
		if err == nil || wrote {
			t.Fatalf("finishWizard = (%v, %v), want a validation error", wrote, err)
		}
		assertNotWritten(t, filepath.Join(dir, "reserved_raw.yaml"))
	})
}

func TestFinishWizardDecline(t *testing.T) {
	dir := t.TempDir()
	cmd, buf := bufferCmd()
	stubFinishConfirm(t, func() (bool, error) { return false, nil })

	afterWriteRan := false
	doc := buildLayoutPageDocument(LayoutPageManifestData{Name: "declined_page"})
	wrote, err := finishWizard(cmd, doc, dir, "declined_page.yaml", false, false, nil,
		func() error { afterWriteRan = true; return nil })
	if err != nil {
		t.Fatalf("declining must not error, got %v", err)
	}
	if wrote {
		t.Error("wrote = true after declining")
	}
	if afterWriteRan {
		t.Error("afterWrite ran although the user declined")
	}
	if !strings.Contains(buf.String(), "Canceled.") {
		t.Errorf("missing Canceled. message:\n%s", buf.String())
	}
	assertNotWritten(t, filepath.Join(dir, "declined_page.yaml"))
}

func TestFinishWizardCtrlCCancelsQuietly(t *testing.T) {
	dir := t.TempDir()
	cmd, buf := bufferCmd()
	stubFinishConfirm(t, func() (bool, error) { return false, errAddCanceled })

	doc := buildLayoutPageDocument(LayoutPageManifestData{Name: "aborted_page"})
	wrote, err := finishWizard(cmd, doc, dir, "aborted_page.yaml", false, false, nil, nil)
	if err != nil {
		t.Fatalf("Ctrl-C must cancel quietly, got %v", err)
	}
	if wrote {
		t.Error("wrote = true after Ctrl-C")
	}
	if !strings.Contains(buf.String(), "Canceled.") {
		t.Errorf("missing Canceled. message:\n%s", buf.String())
	}
	assertNotWritten(t, filepath.Join(dir, "aborted_page.yaml"))
}

func TestFinishWizardConfirmFailureIsRuntimeError(t *testing.T) {
	dir := t.TempDir()
	cmd, _ := bufferCmd()
	promptErr := errors.New("tty exploded")
	stubFinishConfirm(t, func() (bool, error) { return false, promptErr })

	doc := buildLayoutPageDocument(LayoutPageManifestData{Name: "broken_prompt_page"})
	wrote, err := finishWizard(cmd, doc, dir, "broken_prompt.yaml", false, false, nil, nil)
	if wrote {
		t.Error("wrote = true after a prompt failure")
	}
	if !errors.Is(err, promptErr) {
		t.Fatalf("err = %v, want it to wrap the prompt error", err)
	}
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.kind != ErrorKindRuntime {
		t.Errorf("err = %#v, want a runtime exitError", err)
	}
	assertNotWritten(t, filepath.Join(dir, "broken_prompt.yaml"))
}

func TestFinishWizardAfterWrite(t *testing.T) {
	t.Run("runs after a successful write", func(t *testing.T) {
		dir := t.TempDir()
		cmd, _ := bufferCmd()
		stubFinishConfirm(t, func() (bool, error) { return true, nil })

		sawFile := false
		doc := buildLayoutPageDocument(LayoutPageManifestData{Name: "hooked_page"})
		wrote, err := finishWizard(cmd, doc, dir, "hooked_page.yaml", false, false, nil, func() error {
			_, statErr := os.Stat(filepath.Join(dir, "hooked_page.yaml"))
			sawFile = statErr == nil
			return nil
		})
		if err != nil || !wrote {
			t.Fatalf("finishWizard = (%v, %v), want (true, nil)", wrote, err)
		}
		if !sawFile {
			t.Error("afterWrite did not observe the written file")
		}
	})

	t.Run("skipped when the write fails", func(t *testing.T) {
		dir := t.TempDir()
		cmd, _ := bufferCmd()
		stubFinishConfirm(t, func() (bool, error) { return true, nil })

		afterWriteRan := false
		// Reserved-prefix name fails ValidateName inside the write path.
		doc := buildLayoutPageDocument(LayoutPageManifestData{Name: "_reserved_page"})
		wrote, err := finishWizard(cmd, doc, dir, "reserved_page.yaml", false, false, nil,
			func() error { afterWriteRan = true; return nil })
		if err == nil {
			t.Fatal("expected a validation error")
		}
		if wrote {
			t.Error("wrote = true although the write failed")
		}
		if afterWriteRan {
			t.Error("afterWrite ran although the write failed")
		}
		assertNotWritten(t, filepath.Join(dir, "reserved_page.yaml"))
	})

	t.Run("afterWrite error is propagated with wrote=true", func(t *testing.T) {
		dir := t.TempDir()
		cmd, _ := bufferCmd()
		stubFinishConfirm(t, func() (bool, error) { return true, nil })

		hookErr := errors.New("hook failed")
		doc := buildLayoutPageDocument(LayoutPageManifestData{Name: "hook_err_page"})
		wrote, err := finishWizard(cmd, doc, dir, "hook_err_page.yaml", false, false, nil,
			func() error { return hookErr })
		if !errors.Is(err, hookErr) {
			t.Fatalf("err = %v, want the hook error", err)
		}
		if !wrote {
			t.Error("wrote = false although the manifest was written")
		}
	})
}

func TestFinishWizardOpenEditor(t *testing.T) {
	// `true` exits 0 and ignores its arguments, so the editor launch is
	// exercised without anything interactive.
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "true")

	dir := t.TempDir()
	cmd, _ := bufferCmd()
	stubFinishConfirm(t, func() (bool, error) { return true, nil })

	doc := buildLayoutPageDocument(LayoutPageManifestData{Name: "edited_page"})
	wrote, err := finishWizard(cmd, doc, dir, "edited_page.yaml", false, true, nil, nil)
	if err != nil || !wrote {
		t.Fatalf("finishWizard = (%v, %v), want (true, nil)", wrote, err)
	}
}

func TestFinishWizardUnsupportedDocType(t *testing.T) {
	cmd, _ := bufferCmd()
	stubFinishConfirm(t, func() (bool, error) {
		t.Error("confirm must not run for an unsupported document type")
		return false, nil
	})

	wrote, err := finishWizard(cmd, 42, t.TempDir(), "x.yaml", false, false, nil, nil)
	if err == nil || wrote {
		t.Fatalf("finishWizard = (%v, %v), want an error", wrote, err)
	}
}
