package cli

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/httpserver"
)

// TestServeRoute_RejectedQueryParamValue requests a route with values that
// break the declared type or options. The response must be the parameter form:
// the param is listed as missing and no report content is rendered.
func TestServeRoute_RejectedQueryParamValue(t *testing.T) {
	ctx := context.Background()
	workdir := writeServeRaceFixture(t)
	live := `apiVersion: bino.bi/v1alpha1
kind: LiveReportArtefact
metadata:
  name: race-dash
spec:
  title: Race Dashboard
  routes:
    /:
      artefact: race-report
      queryParams:
        - name: REGION
          type: select
          options:
            items:
              - value: DACH
              - value: Nordics
        - name: SALT
          type: number
          default: "0"
          options:
            min: 0
            max: 10
`
	if err := os.WriteFile(filepath.Join(workdir, "live.yaml"), []byte(live), 0o644); err != nil {
		t.Fatalf("write live.yaml: %v", err)
	}
	fn, _ := serveRaceRoute(t, workdir)

	request := func(t *testing.T, query url.Values) (body, contextHTML string) {
		t.Helper()
		raw, _, err := fn(httpserver.WithRequestInfo(ctx, httpserver.RequestInfo{
			Path:     "/",
			RawQuery: query.Encode(),
			Query:    query,
		}))
		if err != nil {
			t.Fatalf("request %v: %v", query, err)
		}
		contextHTML, err = decodeServeContext(string(raw))
		if err != nil {
			t.Fatalf("request %v: %v", query, err)
		}
		return string(raw), contextHTML
	}

	tests := []struct {
		name        string
		query       url.Values
		wantMissing string
	}{
		{"select value outside items", url.Values{"REGION": {"MARS"}}, "REGION"},
		{"number with trailing text", url.Values{"REGION": {"DACH"}, "SALT": {"1 OR 1=1"}}, "SALT"},
		{"number above max", url.Values{"REGION": {"DACH"}, "SALT": {"11"}}, "SALT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, contextHTML := request(t, tt.query)
			if want := `"missingParams":["` + tt.wantMissing + `"]`; !strings.Contains(body, want) {
				t.Errorf("response does not contain %s", want)
			}
			if contextHTML != "" {
				t.Errorf("rejected request rendered report content: %s", contextHTML)
			}
		})
	}

	t.Run("valid values render", func(t *testing.T) {
		body, contextHTML := request(t, url.Values{"REGION": {"DACH"}, "SALT": {"1"}})
		if !strings.Contains(body, `"missingParams":[]`) {
			t.Errorf("valid request lists missing params")
		}
		if !strings.Contains(body+contextHTML, "Region marker: DACH") {
			t.Errorf("valid request did not render the page for its region")
		}
	})
}
