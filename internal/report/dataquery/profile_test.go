package dataquery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"bino.bi/bino/internal/report/config"
)

func TestProfileMonthlyGap(t *testing.T) {
	_, docs, amb := writeProject(t)
	target := newCatalog(docs).entries["sales_csv"].doc
	p, err := ProfileData(context.Background(), docs, amb, target, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.RowCount != 5 || p.Sampled {
		t.Fatalf("rowCount=%d sampled=%v", p.RowCount, p.Sampled)
	}
	byName := map[string]ColumnProfile{}
	for _, c := range p.Columns {
		byName[c.Name] = c
	}
	date := byName["date"]
	if date.Type != "DATE" || date.Series == nil {
		t.Fatalf("date column = %+v", date)
	}
	s := date.Series
	if s.Step != "month" || s.From != "2024-01-01" || s.To != "2024-05-01" || s.Periods != 4 || !slices.Equal(s.Missing, []string{"2024-03"}) || s.MissingMore != 0 {
		t.Fatalf("series = %+v", s)
	}
	region := byName["region"]
	if region.ApproxDistinct != 2 || len(region.TopValues) != 2 || region.TopValues[0].Value != "North" || region.TopValues[0].Count != 3 {
		t.Fatalf("region = %+v", region)
	}
	if byName["ac1"].Type != "DOUBLE" || byName["ac1"].NullCount != 0 {
		t.Fatalf("ac1 = %+v", byName["ac1"])
	}
}

func TestProfileDataSetWarnsAndSamples(t *testing.T) {
	_, docs, amb := writeProject(t)
	target := newCatalog(docs).entries["monthly"].doc
	p, err := ProfileData(context.Background(), docs, amb, target, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Sampled || p.SampleRows != 3 || p.RowCount <= 3 {
		t.Fatalf("rowCount=%d sampled=%v sampleRows=%d", p.RowCount, p.Sampled, p.SampleRows)
	}
	if !slices.ContainsFunc(p.Warnings, func(w string) bool { return strings.Contains(w, `"code" is VARCHAR`) && strings.Contains(w, "numbers") }) {
		t.Fatalf("warnings = %v", p.Warnings)
	}
}

func TestProfileSampleRowsAboveMaximum(t *testing.T) {
	_, docs, amb := writeProject(t)
	target := newCatalog(docs).entries["sales_csv"].doc
	if _, err := ProfileData(context.Background(), docs, amb, target, MaxSampleRows+1); err == nil {
		t.Fatal("no error")
	}
}

func TestFindGaps(t *testing.T) {
	d := func(s ...string) []time.Time {
		out := make([]time.Time, len(s))
		for i, v := range s {
			out[i], _ = time.Parse(time.DateOnly, v)
		}
		return out
	}
	cases := []struct {
		name    string
		dates   []time.Time
		step    string
		missing []string
	}{
		{"month ends", d("2024-01-31", "2024-02-29", "2024-04-30"), "month", []string{"2024-03"}},
		{"quarters", d("2023-01-01", "2023-04-01", "2023-10-01", "2024-01-01"), "quarter", []string{"2023-Q3"}},
		{"years", d("2020-12-31", "2023-12-31"), "year", []string{"2021", "2022"}},
		{"weeks", d("2024-01-01", "2024-01-08", "2024-01-22"), "week", []string{"2024-01-15"}},
		{"days", d("2024-01-01", "2024-01-02", "2024-01-04"), "day", []string{"2024-01-03"}},
		{"single", d("2024-01-01"), "", []string{}},
		{"weekdays", d("2024-01-01", "2024-01-02", "2024-01-03", "2024-01-04", "2024-01-05", "2024-01-08", "2024-01-09", "2024-01-11", "2024-01-12", "2024-01-15"), "weekday", []string{"2024-01-10"}},
		{"weekdays before 1970", d("1969-12-22", "1969-12-23", "1969-12-24", "1969-12-25", "1969-12-26", "1969-12-29", "1969-12-30", "1970-01-01", "1970-01-02", "1970-01-05"), "weekday", []string{"1969-12-31"}},
	}
	for _, c := range cases {
		s := findGaps(c.dates)
		if s.Step != c.step || !slices.Equal(s.Missing, c.missing) {
			t.Errorf("%s: step=%q missing=%v, want %q %v", c.name, s.Step, s.Missing, c.step, c.missing)
		}
	}

	var many []time.Time
	start, _ := time.Parse(time.DateOnly, "2000-01-01")
	for i := 0; i < 100; i += 3 {
		many = append(many, start.AddDate(0, 0, i))
	}
	s := findGaps(many)
	if len(s.Missing) != maxMissingListed || s.MissingMore != 66-maxMissingListed {
		t.Errorf("cap: listed %d, more %d", len(s.Missing), s.MissingMore)
	}

	// 400 years apart: more than time.Duration can hold.
	far := d("1700-01-02", "2100-01-03")
	span := int(far[1].Unix()-far[0].Unix()) / 86400
	if s := findGaps(far); s.Step != "day" || len(s.Missing)+s.MissingMore != span-1 {
		t.Errorf("far: step %q missing %d+%d, want %d", s.Step, len(s.Missing), s.MissingMore, span-1)
	}
}

func TestProfileExactNullCount(t *testing.T) {
	root, _, _ := writeProject(t)
	var b strings.Builder
	b.WriteString("id,v\n")
	for i := range 50_000 {
		if i == 777 {
			fmt.Fprintf(&b, "%d,\n", i)
			continue
		}
		fmt.Fprintf(&b, "%d,%d\n", i, i%7)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "big.csv"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "apiVersion: bino.bi/v1alpha1\nkind: DataSource\nmetadata:\n  name: big\nspec:\n  type: csv\n  path: ./data/big.csv\n"
	if err := os.WriteFile(filepath.Join(root, "big.yaml"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	docs, err := config.LoadDirWithOptions(context.Background(), root, config.LoadOptions{Lenient: true})
	if err != nil {
		t.Fatal(err)
	}
	docs, amb := config.ResolveForIntrospection(docs)
	p, err := ProfileData(context.Background(), docs, amb, newCatalog(docs).entries["big"].doc, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Columns {
		if c.Name == "v" && c.NullCount != 1 {
			t.Fatalf("v nullCount = %d, want 1", c.NullCount)
		}
	}
}
