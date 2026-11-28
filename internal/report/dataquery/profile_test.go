package dataquery

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
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
}
