package render

import (
	"encoding/json"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/datasource"
)

func makeDoc(kind, name string, raw json.RawMessage) config.Document {
	return config.Document{Kind: kind, Name: name, Raw: raw}
}

func TestCollectReferencedDatasources(t *testing.T) {
	tests := []struct {
		name       string
		docs       []config.Document
		globalDocs []config.Document
		want       map[string]bool
	}{
		{
			name: "component with $ datasource ref",
			docs: []config.Document{
				makeDoc("Table", "t1", json.RawMessage(`{"spec":{"dataset":"$sales"}}`)),
			},
			want: map[string]bool{"sales": true},
		},
		{
			name: "component with dataset ref (no $) is not included",
			docs: []config.Document{
				makeDoc("Table", "t1", json.RawMessage(`{"spec":{"dataset":"myDataset"}}`)),
			},
			want: map[string]bool{},
		},
		{
			name: "mixed refs only includes $ prefixed",
			docs: []config.Document{
				makeDoc("ChartStructure", "c1", json.RawMessage(`{"spec":{"dataset":["myDataset","$revenue"]}}`)),
			},
			want: map[string]bool{"revenue": true},
		},
		{
			name: "multiple components",
			docs: []config.Document{
				makeDoc("Table", "t1", json.RawMessage(`{"spec":{"dataset":"$sales"}}`)),
				makeDoc("Text", "txt1", json.RawMessage(`{"spec":{"dataset":"$labels"}}`)),
				makeDoc("ChartTime", "ct1", json.RawMessage(`{"spec":{"dataset":"regularDS"}}`)),
			},
			want: map[string]bool{"sales": true, "labels": true},
		},
		{
			name: "LayoutPage with inline child having $ ref",
			docs: []config.Document{
				makeDoc("LayoutPage", "page1", json.RawMessage(`{
					"spec":{
						"children":[
							{"kind":"Table","spec":{"dataset":"$inventory"}}
						]
					}
				}`)),
			},
			want: map[string]bool{"inventory": true},
		},
		{
			name: "Tree node with $ ref",
			docs: []config.Document{
				makeDoc("Tree", "tree1", json.RawMessage(`{
					"spec":{
						"nodes":[
							{"id":"n1","kind":"Table","spec":{"dataset":"$treeData"}}
						]
					}
				}`)),
			},
			want: map[string]bool{"treeData": true},
		},
		{
			name: "Grid child with $ ref",
			docs: []config.Document{
				makeDoc("Grid", "grid1", json.RawMessage(`{
					"spec":{
						"children":[
							{"row":"r1","column":"c1","kind":"ChartStructure","spec":{"dataset":"$gridData"}}
						]
					}
				}`)),
			},
			want: map[string]bool{"gridData": true},
		},
		{
			name: "LayoutPage with inline Tree child containing node refs",
			docs: []config.Document{
				makeDoc("LayoutPage", "page1", json.RawMessage(`{
					"spec":{
						"children":[
							{"kind":"Tree","spec":{
								"nodes":[
									{"id":"n1","kind":"Table","spec":{"dataset":"$nestedSrc"}}
								]
							}}
						]
					}
				}`)),
			},
			want: map[string]bool{"nestedSrc": true},
		},
		{
			name: "LayoutPage with inline Grid child containing child refs",
			docs: []config.Document{
				makeDoc("LayoutPage", "page1", json.RawMessage(`{
					"spec":{
						"children":[
							{"kind":"Grid","spec":{
								"children":[
									{"row":"r1","column":"c1","kind":"Table","spec":{"dataset":"$gridSrc"}}
								]
							}}
						]
					}
				}`)),
			},
			want: map[string]bool{"gridSrc": true},
		},
		{
			name: "LayoutPage with inline LayoutCard with nested child",
			docs: []config.Document{
				makeDoc("LayoutPage", "page1", json.RawMessage(`{
					"spec":{
						"children":[
							{"kind":"LayoutCard","spec":{
								"children":[
									{"kind":"Table","spec":{"dataset":"$cardSrc"}}
								]
							}}
						]
					}
				}`)),
			},
			want: map[string]bool{"cardSrc": true},
		},
		{
			name: "globalDocs also scanned",
			docs: []config.Document{
				makeDoc("Table", "t1", json.RawMessage(`{"spec":{"dataset":"$fromDocs"}}`)),
			},
			globalDocs: []config.Document{
				makeDoc("Table", "t2", json.RawMessage(`{"spec":{"dataset":"$fromGlobal"}}`)),
			},
			want: map[string]bool{"fromDocs": true, "fromGlobal": true},
		},
		{
			name: "no dataset field",
			docs: []config.Document{
				makeDoc("Image", "img1", json.RawMessage(`{"spec":{"source":"test.png"}}`)),
			},
			want: map[string]bool{},
		},
		{
			name: "empty docs",
			docs: nil,
			want: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectReferencedDatasources(tt.docs, tt.globalDocs)
			if len(got) != len(tt.want) {
				t.Errorf("got %d refs, want %d: got=%v want=%v", len(got), len(tt.want), got, tt.want)
				return
			}
			for k := range tt.want {
				if !got[k] {
					t.Errorf("missing expected ref %q", k)
				}
			}
		})
	}
}

func TestFilterDatasourcesByRefs(t *testing.T) {
	results := []datasource.Result{
		{Name: "sales", Data: json.RawMessage(`[]`)},
		{Name: "inventory", Data: json.RawMessage(`[]`)},
		{Name: "unused", Data: json.RawMessage(`[]`)},
	}

	tests := []struct {
		name       string
		referenced map[string]bool
		wantNames  []string
	}{
		{
			name:       "filters to referenced subset",
			referenced: map[string]bool{"sales": true, "inventory": true},
			wantNames:  []string{"sales", "inventory"},
		},
		{
			name:       "single match",
			referenced: map[string]bool{"sales": true},
			wantNames:  []string{"sales"},
		},
		{
			name:       "empty referenced returns nil",
			referenced: map[string]bool{},
			wantNames:  nil,
		},
		{
			name:       "no matches",
			referenced: map[string]bool{"nonexistent": true},
			wantNames:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterDatasourcesByRefs(results, tt.referenced)
			if tt.wantNames == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != len(tt.wantNames) {
				t.Errorf("got %d results, want %d", len(got), len(tt.wantNames))
				return
			}
			gotNames := make(map[string]bool, len(got))
			for _, r := range got {
				gotNames[r.Name] = true
			}
			for _, name := range tt.wantNames {
				if !gotNames[name] {
					t.Errorf("missing expected result %q", name)
				}
			}
		})
	}
}
