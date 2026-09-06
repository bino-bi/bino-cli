package dataset

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/pkg/duckdb"
)

// monthlyCSV holds two years of month-end actuals and plan for two categories;
// 2019-06 is missing for A so a gap exists in the prior year.
const monthlyCSV = `category,date,ac1,pl1
A,2019-01-31,10,100
A,2019-02-28,20,200
A,2019-03-31,30,300
A,2019-04-30,40,400
A,2019-05-31,50,500
A,2019-07-31,70,700
B,2019-01-31,1,11
B,2019-02-28,2,22
A,2020-01-31,110,1100
A,2020-02-29,120,1200
A,2020-03-31,130,1300
A,2020-04-30,140,1400
A,2020-05-31,150,1500
A,2020-06-30,160,1600
A,2020-07-31,170,1700
B,2020-01-31,11,111
B,2020-02-29,22,222
`

const salesSource = `
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales_csv
spec:
  type: csv
  path: ./sales.csv
`

// writeProject writes a CSV DataSource plus one DataSet manifest and loads it.
func writeProject(t *testing.T, datasetYAML string) (string, []config.Document) {
	t.Helper()
	workdir := t.TempDir()
	files := map[string]string{
		"sales.csv":    monthlyCSV,
		"source.yaml":  salesSource,
		"dataset.yaml": datasetYAML,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workdir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	docs, err := config.LoadDir(context.Background(), workdir)
	if err != nil {
		t.Fatalf("load docs: %v", err)
	}
	return workdir, docs
}

func datasetYAML(spec string) string {
	return "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: sales\nspec:\n" + spec
}

func rowsByKey(t *testing.T, data json.RawMessage) map[string]map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("unmarshal rows: %v", err)
	}
	out := map[string]map[string]any{}
	for _, r := range rows {
		// The CSV sniffer types date as DATE, serialized as an ISO timestamp;
		// key on the calendar day only.
		date := r["date"].(string)
		if len(date) > 10 {
			date = date[:10]
		}
		out[r["category"].(string)+"/"+date] = r
	}
	return out
}

func warnOpts(continueOnError bool) *ExecuteOptions {
	return &ExecuteOptions{DataValidation: DataValidationWarn, ContinueOnQueryError: continueOnError}
}

// requirePrql skips the test when the prql community extension cannot be
// loaded (no network / no cached extension).
func requirePrql(t *testing.T) {
	t.Helper()
	opts, err := duckdb.DefaultOptions()
	if err != nil {
		t.Skip("duckdb options:", err)
	}
	s, err := duckdb.OpenSession(context.Background(), opts)
	if err != nil {
		t.Skip("open session:", err)
	}
	defer s.Close()
	if err := s.InstallAndLoadCommunityExtensions(context.Background(), []string{"prql"}); err != nil {
		t.Skip("prql extension unavailable:", err)
	}
}

func TestExecute_DeriveProducesShiftedSlot(t *testing.T) {
	t.Parallel()
	workdir, docs := writeProject(t, datasetYAML(`
  query: SELECT category, "date", "ac1"::DOUBLE AS ac1 FROM sales_csv
  derive:
    pp2: { from: ac1, shift: 1 year, grain: month }
    pp1: { from: ac1, shift: 1 month, grain: month }
`))
	results, warnings, err := Execute(context.Background(), workdir, docs, warnOpts(false))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w.Message, "null on every row") {
			t.Errorf("unexpected warning %v", w)
		}
	}
	rows := rowsByKey(t, results[0].Data)
	check := func(key, slot string, want any) {
		t.Helper()
		got := rows[key][slot]
		if got != want {
			t.Errorf("%s.%s = %v (%T), want %v", key, slot, got, got, want)
		}
	}
	check("A/2020-02-29", "pp2", 20.0) // month end vs 2019-02-28
	check("A/2020-04-30", "pp2", 40.0)
	check("A/2020-06-30", "pp2", nil) // gap: 2019-06 missing, not the previous row
	check("A/2020-07-31", "pp2", 70.0)
	check("A/2020-01-31", "pp1", nil) // 2019-12 not in the window
	check("A/2020-04-30", "pp1", 130.0)
	check("B/2020-02-29", "pp2", 2.0)
	check("B/2020-02-29", "pp1", 11.0)
	check("A/2019-01-31", "pp2", nil)
}

func TestExecute_DeriveOnPrql(t *testing.T) {
	requirePrql(t)
	workdir, docs := writeProject(t, datasetYAML(`
  prql: |
    from sales_csv
    select {category, date, ac1};
  derive:
    pp2: { from: ac1, shift: 1 year, grain: month }
`))
	results, _, err := Execute(context.Background(), workdir, docs, warnOpts(false))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	rows := rowsByKey(t, results[0].Data)
	if got := rows["A/2020-03-31"]["pp2"]; got != 30.0 {
		t.Errorf("pp2 = %v, want 30", got)
	}
}

func TestExecute_DeclarationErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		spec string
		want string
	}{
		{
			name: "derived slot already supplied",
			spec: `
  query: SELECT category, "date", "ac1"::DOUBLE AS ac1, 0 AS pp2 FROM sales_csv
  derive:
    pp2: { from: ac1, shift: 1 year, grain: month }
`,
			want: "slot pp2 is already in the query result; use assert: for a supplied slot",
		},
		{
			name: "from slot missing",
			spec: `
  query: SELECT category, "date", "ac1"::DOUBLE AS ac1 FROM sales_csv
  derive:
    pp2: { from: fc1, shift: 1 year, grain: month }
`,
			want: "slot fc1 (from of pp2) is missing from the query result",
		},
		{
			name: "date missing",
			spec: `
  query: SELECT category, "ac1"::DOUBLE AS ac1 FROM sales_csv
  derive:
    pp2: { from: ac1, shift: 1 year, grain: month }
`,
			want: `column "date" is missing from the query result`,
		},
		{
			name: "asserted slot missing",
			spec: `
  query: SELECT category, "date", "ac1"::DOUBLE AS ac1 FROM sales_csv
  assert:
    pp2: { from: ac1, shift: 1 year, grain: month }
`,
			want: "slot pp2 is asserted but missing from the query result",
		},
		{
			name: "duplicate identity in period",
			spec: `
  query: SELECT category, "date", "ac1"::DOUBLE AS ac1 FROM sales_csv
  derive:
    pp2: { from: ac1, shift: 1 year, grain: year }
`,
			want: "duplicate rows for identity {category=A} in period 2019-01-01 (grain year)",
		},
		{
			name: "assert mismatch",
			spec: `
  query: SELECT category, "date", "ac1"::DOUBLE AS ac1, "pl1"::DOUBLE AS pp1 FROM sales_csv
  assert:
    pp1: { from: ac1, shift: 1 year, grain: month }
`,
			want: "assert pp1: 8 row(s) differ from ac1 shifted by 1 year (grain month); first at identity {category=A}, period 2020-01-01",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workdir, docs := writeProject(t, datasetYAML(tc.spec))
			// Default build settings: warn mode, no continue-on-error → hard error.
			_, _, err := Execute(context.Background(), workdir, docs, warnOpts(false))
			if err == nil {
				t.Fatal("Execute: expected error")
			}
			if !strings.Contains(err.Error(), "check: "+tc.want) {
				t.Errorf("error %q\ndoes not contain %q", err, tc.want)
			}

			// With continue-on-error (preview, lint) it is a warning and the dataset is skipped.
			results, warnings, err := Execute(context.Background(), workdir, docs, warnOpts(true))
			if err != nil {
				t.Fatalf("Execute (continue): %v", err)
			}
			if len(results) != 0 {
				t.Errorf("expected no result, got %d", len(results))
			}
			found := false
			for _, w := range warnings {
				if strings.Contains(w.Message, "check: "+tc.want) {
					found = true
				}
			}
			if !found {
				t.Errorf("warnings %v do not carry %q", warnings, tc.want)
			}
		})
	}
}

func TestExecute_AssertTolerance(t *testing.T) {
	t.Parallel()
	// pp1 is ac1 of the same identity one year earlier, computed in SQL and
	// disturbed by a relative epsilon.
	spec := func(eps string) string {
		return datasetYAML(`
  query: |
    SELECT category, "date", "ac1"::DOUBLE AS ac1,
           (SELECT p."ac1"::DOUBLE FROM sales_csv p
             WHERE p.category = s.category
               AND date_trunc('month', p."date"::DATE) = date_trunc('month', s."date"::DATE) - INTERVAL 1 YEAR) * (1 + ` + eps + `) AS pp1
    FROM sales_csv s
  assert:
    pp1: { from: ac1, shift: 1 year, grain: month }
`)
	}
	workdir, docs := writeProject(t, spec("1e-12"))
	if _, _, err := Execute(context.Background(), workdir, docs, warnOpts(false)); err != nil {
		t.Errorf("1e-12 noise should pass, got %v", err)
	}
	workdir, docs = writeProject(t, spec("1e-6"))
	if _, _, err := Execute(context.Background(), workdir, docs, warnOpts(false)); err == nil || !strings.Contains(err.Error(), "assert pp1:") {
		t.Errorf("1e-6 drift should fail, got %v", err)
	}
}

func TestExecute_AssertPasses(t *testing.T) {
	t.Parallel()
	workdir, docs := writeProject(t, datasetYAML(`
  query: |
    SELECT category, "date", "ac1"::DOUBLE AS ac1,
           (SELECT p."ac1"::DOUBLE FROM sales_csv p
             WHERE p.category = s.category
               AND date_trunc('month', p."date"::DATE) = date_trunc('month', s."date"::DATE) - INTERVAL 1 YEAR) AS pp2
    FROM sales_csv s
  assert:
    pp2: { from: ac1, shift: 1 year, grain: month }
`))
	results, _, err := Execute(context.Background(), workdir, docs, warnOpts(false))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
}

func TestExecute_DerivedAllNullWarnsFreshAndCached(t *testing.T) {
	t.Parallel()
	workdir, docs := writeProject(t, datasetYAML(`
  query: SELECT category, "date", "ac1"::DOUBLE AS ac1 FROM sales_csv WHERE "date" >= '2020-01-01'
  derive:
    pp2: { from: ac1, shift: 1 year, grain: month }
`))
	want := "pp2 derived from ac1 is null on every row — the query window has no prior period"
	for _, run := range []string{"fresh", "cached"} {
		results, warnings, err := Execute(context.Background(), workdir, docs, warnOpts(false))
		if err != nil {
			t.Fatalf("%s: Execute: %v", run, err)
		}
		if len(results) != 1 {
			t.Fatalf("%s: results = %d", run, len(results))
		}
		found := false
		for _, w := range warnings {
			if w.DataSet == "sales" && w.Message == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: warnings %v lack %q", run, warnings, want)
		}
	}
}

func cacheFiles(t *testing.T, workdir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(workdir, ".bino", "cache", "datasets"))
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestExecute_CacheKeyFollowsDeriveAndMacroRevision(t *testing.T) {
	// Not parallel: bumps the package-level macro revision.
	workdir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(workdir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("sales.csv", monthlyCSV)
	write("source.yaml", salesSource)
	write("plain.yaml", `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: plain
spec:
  query: SELECT 1 AS ac1
`)
	derived := func(shift string) string {
		return datasetYAML(`
  query: SELECT category, "date", "ac1"::DOUBLE AS ac1 FROM sales_csv
  derive:
    pp2: { from: ac1, shift: ` + shift + `, grain: month }
`)
	}
	run := func() []string {
		t.Helper()
		docs, err := config.LoadDir(context.Background(), workdir)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := Execute(context.Background(), workdir, docs, nil); err != nil {
			t.Fatal(err)
		}
		return cacheFiles(t, workdir)
	}

	write("dataset.yaml", derived("1 year"))
	first := run()
	if len(first) != 2 {
		t.Fatalf("cache files after first run = %v", first)
	}

	// Editing derive: changes the declaring dataset's key.
	write("dataset.yaml", derived("1 month"))
	second := run()
	if len(second) != 3 {
		t.Fatalf("expected a new cache file after editing derive, got %v", second)
	}

	// Bumping the macro revision changes it again, but leaves "plain" alone.
	old := shiftMacroRevision
	shiftMacroRevision = old + 1
	defer func() { shiftMacroRevision = old }()
	third := run()
	if len(third) != 4 {
		t.Fatalf("expected a new cache file after bumping the revision, got %v", third)
	}
	plain := 0
	for _, f := range third {
		if strings.HasPrefix(f, "plain-") {
			plain++
		}
	}
	if plain != 1 {
		t.Errorf("plain dataset should keep one cache file, got %d in %v", plain, third)
	}
}
