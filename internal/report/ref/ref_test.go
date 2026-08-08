package ref

import (
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func tableDoc(name string, spec string) config.Document {
	return config.Document{
		Kind: "Table",
		Name: name,
		Raw:  json.RawMessage(`{"kind":"Table","metadata":{"name":"` + name + `"},"spec":` + spec + `}`),
	}
}

func indexOf(docs ...config.Document) map[string]config.Document {
	idx := make(map[string]config.Document, len(docs))
	for _, d := range docs {
		idx[d.Kind+":"+d.Name] = d
	}
	return idx
}

func TestResolve(t *testing.T) {
	sales := tableDoc("sales", `{"dataset":"sales_ds","level":"auto"}`)
	idx := indexOf(sales)

	t.Run("inline child passes through", func(t *testing.T) {
		res, err := Resolve(Ref{Kind: "Table", Spec: json.RawMessage(`{"dataset":"x"}`)}, Options{Index: idx})
		if err != nil || res.Skipped {
			t.Fatalf("Resolve = (%+v, %v)", res, err)
		}
		if string(res.Spec) != `{"dataset":"x"}` {
			t.Errorf("inline spec altered: %s", res.Spec)
		}
	})

	t.Run("ref without overrides returns the referenced spec", func(t *testing.T) {
		res, err := Resolve(Ref{Kind: "Table", Name: "sales"}, Options{Index: idx})
		if err != nil || res.Skipped {
			t.Fatalf("Resolve = (%+v, %v)", res, err)
		}
		if !strings.Contains(string(res.Spec), `"sales_ds"`) {
			t.Errorf("referenced spec missing: %s", res.Spec)
		}
		if res.Doc.Name != "sales" {
			t.Errorf("Doc = %+v, want the referenced document", res.Doc)
		}
	})

	t.Run("overrides deep-merge, arrays replace", func(t *testing.T) {
		base := tableDoc("styled", `{"dataset":"d","style":{"width":1,"color":"red"},"columns":["a","b"]}`)
		res, err := Resolve(Ref{
			Kind: "Table", Name: "styled",
			Spec: json.RawMessage(`{"style":{"color":"blue"},"columns":["c"]}`),
		}, Options{Index: indexOf(base)})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		var got struct {
			Style   map[string]any `json:"style"`
			Columns []string       `json:"columns"`
		}
		if err := json.Unmarshal(res.Spec, &got); err != nil {
			t.Fatalf("parse merged: %v", err)
		}
		if got.Style["color"] != "blue" || got.Style["width"] != float64(1) {
			t.Errorf("style merge wrong: %+v", got.Style)
		}
		if len(got.Columns) != 1 || got.Columns[0] != "c" {
			t.Errorf("arrays must replace, got %v", got.Columns)
		}
	})

	t.Run("optional missing ref skips", func(t *testing.T) {
		res, err := Resolve(Ref{Kind: "Table", Name: "ghost", Optional: true}, Options{Index: idx})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !res.Skipped {
			t.Error("optional missing ref must skip")
		}
	})

	t.Run("required missing ref errors with site prefix", func(t *testing.T) {
		_, err := Resolve(Ref{Kind: "Table", Name: "ghost"}, Options{Index: idx, Where: `layout child in "main_page"`})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "required reference") || !strings.Contains(err.Error(), "main_page") {
			t.Errorf("error lacks canonical text or site: %v", err)
		}
	})

	t.Run("constraint-filtered ref skips", func(t *testing.T) {
		filtered := indexOf() // sales was filtered out
		global := idx
		res, err := Resolve(Ref{Kind: "Table", Name: "sales"}, Options{Index: filtered, GlobalIndex: global})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !res.Skipped {
			t.Error("constraint-filtered ref must skip, not error")
		}
	})

	t.Run("LayoutPage misuse is rejected in every context", func(t *testing.T) {
		_, err := Resolve(Ref{Kind: "Table", Name: "some_page"}, Options{
			Index:  idx,
			IsPage: func(name string) bool { return name == "some_page" },
		})
		if err == nil || !strings.Contains(err.Error(), "LayoutPage") {
			t.Errorf("expected the LayoutPage rejection, got: %v", err)
		}
	})

	t.Run("missing index is an error, not a panic", func(t *testing.T) {
		_, err := Resolve(Ref{Kind: "Table", Name: "sales"}, Options{})
		if err == nil || !strings.Contains(err.Error(), "document index") {
			t.Errorf("expected the missing-index error, got: %v", err)
		}
	})

	t.Run("params expand into the referenced document", func(t *testing.T) {
		def := "emea"
		paramDoc := config.Document{
			Kind: "Table",
			Name: "regional",
			Raw:  json.RawMessage(`{"kind":"Table","metadata":{"name":"regional"},"spec":{"dataset":"sales_${REGION}"}}`),
			Params: []config.LayoutPageParamSpec{
				{Name: "REGION", Default: &def},
			},
		}
		res, err := Resolve(Ref{
			Kind: "Table", Name: "regional",
			Params: map[string]string{"REGION": "apac"},
		}, Options{Index: indexOf(paramDoc)})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if !strings.Contains(string(res.Spec), "sales_apac") {
			t.Errorf("params not expanded: %s", res.Spec)
		}
	})
}

func TestMergeSpec(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		override string
		want     string
		wantErr  bool
	}{
		{name: "simple override", base: `{"a": "1", "b": "2"}`, override: `{"b": "3"}`, want: `{"a":"1","b":"3"}`},
		{name: "add new field", base: `{"a": "1"}`, override: `{"b": "2"}`, want: `{"a":"1","b":"2"}`},
		{name: "nested merge", base: `{"outer": {"a": "1", "b": "2"}}`, override: `{"outer": {"b": "3"}}`, want: `{"outer":{"a":"1","b":"3"}}`},
		{name: "array replace", base: `{"arr": [1, 2, 3]}`, override: `{"arr": [4, 5]}`, want: `{"arr":[4,5]}`},
		{name: "non-object base errors", base: `[1]`, override: `{}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergeSpec(json.RawMessage(tt.base), json.RawMessage(tt.override))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var gotMap, wantMap map[string]any
			if err := json.Unmarshal(got, &gotMap); err != nil {
				t.Fatalf("unmarshal result: %v", err)
			}
			if err := json.Unmarshal([]byte(tt.want), &wantMap); err != nil {
				t.Fatalf("unmarshal want: %v", err)
			}
			gotBytes, _ := json.Marshal(gotMap)
			wantBytes, _ := json.Marshal(wantMap)
			if string(gotBytes) != string(wantBytes) {
				t.Fatalf("MergeSpec() = %s, want %s", got, tt.want)
			}
		})
	}
}
