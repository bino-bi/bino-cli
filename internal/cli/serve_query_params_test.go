package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/httpserver"
)

// TestServeRoutes_RejectedQueryParamValue requests a route with values that
// break the declared type or options. The response must be the parameter form:
// the param is listed as missing and no report content is rendered. Artefact
// and layoutPages routes validate in different handlers, so both are covered.
func TestServeRoutes_RejectedQueryParamValue(t *testing.T) {
	const live = `apiVersion: bino.bi/v1alpha1
kind: LiveReportArtefact
metadata:
  name: race-dash
spec:
  title: Race Dashboard
  routes:
    /:
%s
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
	routes := []struct {
		name   string
		target string
	}{
		{"artefact route", "      artefact: race-report"},
		{"layoutPages route", "      layoutPages:\n        - race-page"},
	}
	tests := []struct {
		name        string
		query       url.Values
		wantMissing string // empty if the request is valid
	}{
		{"select value outside items", url.Values{"REGION": {"MARS"}}, "REGION"},
		{"number with trailing text", url.Values{"REGION": {"DACH"}, "SALT": {"1 OR 1=1"}}, "SALT"},
		{"number above max", url.Values{"REGION": {"DACH"}, "SALT": {"11"}}, "SALT"},
		{"valid values", url.Values{"REGION": {"DACH"}, "SALT": {"1"}}, ""},
	}

	ctx := context.Background()
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			workdir := writeServeRaceFixture(t)
			manifest := fmt.Sprintf(live, route.target)
			if err := os.WriteFile(filepath.Join(workdir, "live.yaml"), []byte(manifest), 0o644); err != nil {
				t.Fatalf("write live.yaml: %v", err)
			}
			fn, _ := serveRaceRoute(t, workdir)

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					raw, _, err := fn(httpserver.WithRequestInfo(ctx, httpserver.RequestInfo{
						Path:     "/",
						RawQuery: tt.query.Encode(),
						Query:    tt.query,
					}))
					if err != nil {
						t.Fatalf("request: %v", err)
					}
					body := string(raw)
					contextHTML, err := decodeServeContext(body)
					if err != nil {
						t.Fatal(err)
					}

					if tt.wantMissing == "" {
						if !strings.Contains(body, `"missingParams":[]`) {
							t.Errorf("valid request lists missing params")
						}
						if !strings.Contains(body+contextHTML, "Region marker: DACH") {
							t.Errorf("valid request did not render the page for its region")
						}
						return
					}
					if want := `"missingParams":["` + tt.wantMissing + `"]`; !strings.Contains(body, want) {
						t.Errorf("response does not contain %s", want)
					}
					if contextHTML != "" {
						t.Errorf("rejected request rendered report content: %s", contextHTML)
					}
				})
			}
		})
	}
}
