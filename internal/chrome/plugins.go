package chrome

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"bino.bi/bino/internal/logx"
)

// pluginRuntime defines window.binoPlugin. The engine's component register is
// private to its bundle, so plugin elements report their drawing here instead.
//
//go:embed plugin-runtime.js
var pluginRuntime string

// injectPluginRuntime adds the runtime to every document the page loads. It
// must run before Navigate.
func injectPluginRuntime() chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: pluginRuntime})
		return err
	})
}

// settlePluginComponents runs after the engine wait. With allowIncomplete a
// plugin problem is logged as a warning and the render goes on.
func settlePluginComponents(taskCtx context.Context, allowIncomplete bool, logger logx.Logger) error {
	if err := taskCtx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("render timed out before plugin components were checked: %w", err)
		}
		return err
	}
	err := waitForPluginComponents(taskCtx)
	if err == nil || !allowIncomplete || taskCtx.Err() != nil {
		return err
	}
	for _, line := range strings.Split(err.Error(), "\n") {
		logger.Warnf("%s", line)
	}
	return nil
}

// waitForPluginComponents waits until no tracked plugin work is pending and
// reports failed and never-defined plugin elements. A page without the
// runtime passes.
func waitForPluginComponents(ctx context.Context) error {
	raw, err := chromedp.Run(ctx,
		chromedp.Evaluate[[]byte](`window.binoPlugin ? window.binoPlugin.settled() : null`, chromedp.EvalAwaitPromise),
	)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("render timed out while plugin components were still drawing: %w", err)
		}
		return fmt.Errorf("check plugin components: %w", err)
	}
	return pluginSettleError(raw)
}

// pluginSettleError turns the result of binoPlugin.settled() into an error,
// or nil when nothing failed.
func pluginSettleError(raw []byte) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var res struct {
		Failures []struct {
			Tag     string `json:"tag"`
			ID      string `json:"id"`
			Message string `json:"message"`
		} `json:"failures"`
		Undefined []string `json:"undefined"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("read plugin component state: %w", err)
	}
	var errs []error
	for _, f := range res.Failures {
		switch {
		case f.Tag == "":
			errs = append(errs, fmt.Errorf("plugin script error: %s", f.Message))
		case f.ID == "":
			errs = append(errs, fmt.Errorf("plugin component %s failed to render: %s", f.Tag, f.Message))
		default:
			errs = append(errs, fmt.Errorf("plugin component %s#%s failed to render: %s", f.Tag, f.ID, f.Message))
		}
	}
	for _, tag := range res.Undefined {
		errs = append(errs, fmt.Errorf("custom element %s was never defined: its plugin script did not load or threw", tag))
	}
	return errors.Join(errs...)
}
