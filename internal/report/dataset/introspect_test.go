package dataset

import (
	"context"
	"strings"
	"testing"
)

func TestIntrospectColumns_ShowsDerivedSlot(t *testing.T) {
	t.Parallel()
	_, docs := writeProject(t, datasetYAML(`
  query: SELECT category, "date", "ac1"::DOUBLE AS ac1 FROM sales_csv
  derive:
    pp2: { from: ac1, shift: 1 year, grain: month }
`))
	cols, err := IntrospectColumns(context.Background(), docs, "sales")
	if err != nil {
		t.Fatalf("IntrospectColumns: %v", err)
	}
	if got := strings.Join(cols, ","); got != "category,date,ac1,pp2" {
		t.Errorf("columns = %q, want category,date,ac1,pp2", got)
	}

	// DataSource lookup with the $ prefix still works.
	cols, err = IntrospectColumns(context.Background(), docs, "$sales_csv")
	if err != nil {
		t.Fatalf("IntrospectColumns($sales_csv): %v", err)
	}
	if len(cols) != 4 {
		t.Errorf("datasource columns = %v", cols)
	}
}

func TestIntrospectColumns_PrqlDataset(t *testing.T) {
	requirePrql(t)
	_, docs := writeProject(t, datasetYAML(`
  prql: |
    from sales_csv
    select {category, date, ac1}
  derive:
    pp1: { from: ac1, shift: 1 month, grain: month }
`))
	cols, err := IntrospectColumns(context.Background(), docs, "sales")
	if err != nil {
		t.Fatalf("IntrospectColumns: %v", err)
	}
	if got := strings.Join(cols, ","); got != "category,date,ac1,pp1" {
		t.Errorf("columns = %q", got)
	}
}
