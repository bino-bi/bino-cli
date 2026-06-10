package cli

import (
	"os"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/runtimecfg"
)

func TestApplyCommandEnv_ReloadsRuntimeLimits(t *testing.T) {
	// t.Setenv registers cleanup restoring the previous value; Unsetenv then
	// removes the variable so the bino.toml value is not shadowed (Apply
	// skips keys that exist in the environment, even when empty).
	t.Setenv("BNR_MAX_QUERY_ROWS", "")
	os.Unsetenv("BNR_MAX_QUERY_ROWS")
	defer runtimecfg.SetForTests(runtimecfg.Current())()

	cmdCfg := pathutil.CommandConfig{
		Env: pathutil.CommandEnv{Values: map[string]string{"BNR_MAX_QUERY_ROWS": "1234"}},
	}
	applyCommandEnv(cmdCfg, logx.Nop())

	if got := runtimecfg.Current().MaxQueryRows; got != 1234 {
		t.Fatalf("MaxQueryRows = %d, want 1234: bino.toml [cmd.env] BNR_* values must take effect", got)
	}
}
