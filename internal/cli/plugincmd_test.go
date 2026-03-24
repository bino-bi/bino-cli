package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestPluginExecCommand_RequiresArg(t *testing.T) {
	cmd := newPluginExecCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error with no args")
	}
}

func TestPluginExecCommand_BadFormat(t *testing.T) {
	// Override RunE to test arg parsing without needing a real plugin.
	root := &cobra.Command{Use: "bino"}
	root.AddCommand(newPluginExecCommand())
	root.SetArgs([]string{"exec", "nocolon"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for arg without colon")
	}
}
