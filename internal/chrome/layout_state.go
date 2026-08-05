package chrome

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/web"
)

// captureLayoutState asks the rendered page for a layout-state snapshot.
//
// The script is the same bundle the preview inspector imports, built as an
// IIFE so it can be evaluated here: the component-id rule it implements has to
// match the engine exactly, and a second hand-kept copy would drift.
//
// Returns nil (no error) when the loaded engine predates getLayoutState —
// the CLI supports a far wider engine range than the API does, so an absent
// snapshot is normal, not a failure.
func captureLayoutState(ctx context.Context, logger logx.Logger) ([]byte, error) {
	script, err := web.LayoutCaptureScript()
	if err != nil {
		return nil, fmt.Errorf("read layout capture script: %w", err)
	}

	var raw json.RawMessage
	err = chromedp.Run(ctx,
		chromedp.Evaluate(string(script), nil),
		chromedp.Evaluate(captureExpression, &raw, awaitPromise),
	)
	if err != nil {
		return nil, fmt.Errorf("evaluate layout capture: %w", err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		logger.Debugf("layout state unavailable: engine has no getLayoutState()")
		return nil, nil
	}
	return raw, nil
}

// captureExpression runs the injected bundle and reshapes its result into the
// analyzer's envelope. The live-element map is dropped: it cannot cross the
// CDP boundary and nothing server-side needs it. generatedAt is dropped too,
// so two runs of an unchanged bundle produce byte-identical files.
const captureExpression = `(async () => {
  const capture = await binoLayoutCapture.captureLayoutState({ settleTimeoutMs: 15000 });
  if (!capture) return null;
  const state = capture.state;
  delete state.generatedAt;
  return { state: state, sources: capture.sources, settled: capture.settled };
})()`

// awaitPromise makes Evaluate resolve the promise the expression returns
// instead of handing back a Promise object.
func awaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams {
	return p.WithAwaitPromise(true)
}
