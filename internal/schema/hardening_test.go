package schema

import "testing"

// TestValidate_PartialRefOverride verifies that a referenced component child may
// override individual spec fields without re-supplying the referenced spec's
// required fields (Issue 1: base/full split).
func TestValidate_PartialRefOverride(t *testing.T) {
	yaml := `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      ref: sales_table
      spec:
        filter: "amount > 0"
`
	if err := Validate([]byte(yaml)); err != nil {
		t.Errorf("partial ref override should pass, got: %v", err)
	}
}

// TestValidate_InlineChildRequiresDataset verifies that an inline child (no ref)
// still requires the spec's mandatory fields.
func TestValidate_InlineChildRequiresDataset(t *testing.T) {
	yaml := `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      spec:
        filter: "amount > 0"
`
	err := Validate([]byte(yaml))
	if err == nil {
		t.Fatal("inline Table child without dataset should fail")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
}

// TestValidate_RefOverrideTypoFails verifies that a typo inside an override spec
// is rejected because the referenced base spec is closed (additionalProperties:false).
func TestValidate_RefOverrideTypoFails(t *testing.T) {
	yaml := `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      ref: sales_table
      spec:
        filterr: "amount > 0"
`
	if err := Validate([]byte(yaml)); err == nil {
		t.Fatal("typo in ref override spec should fail")
	}
}

// TestValidate_Strictness covers the additionalProperties:false cases (Issue 2):
// misspelled spec fields, unknown top-level keys, and unknown keys inside a
// ConnectionSecret are all rejected, while valid documents still pass.
func TestValidate_Strictness(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "ChartStructure spec typo rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartStructure
metadata:
  name: chart
spec:
  dataset: revenue
  scenrios: [ac1]
`,
			wantErr: true,
		},
		{
			name: "unknown top-level key rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: ds
spec:
  query: SELECT 1
foo: bar
`,
			wantErr: true,
		},
		{
			name: "ConnectionSecret s3 valid",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata:
  name: my_s3
spec:
  type: s3
  s3:
    keyId: AKIA
    secret: shhh
`,
			wantErr: false,
		},
		{
			name: "ConnectionSecret s3 with unknown key rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata:
  name: my_s3
spec:
  type: s3
  s3:
    keyId: AKIA
  foo: bar
`,
			wantErr: true,
		},
		{
			name: "Table selectedStyle accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: sales
spec:
  dataset: revenue
  selectedStyle: corporate-style
`,
			wantErr: false,
		},
		{
			name: "Table selectedStyle typo rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: sales
spec:
  dataset: revenue
  selectedStyl: corporate-style
`,
			wantErr: true,
		},
		{
			name: "LayoutPage selectedStyle accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  selectedStyle: corporate-style
  children:
    - kind: Table
      ref: sales_table
`,
			wantErr: false,
		},
		{
			name: "inline layout child selectedStyle accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Text
      spec:
        value: hello
        selectedStyle: corporate-style
`,
			wantErr: false,
		},
		{
			name: "Asset with localPath source valid",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Asset
metadata:
  name: logo
spec:
  type: image
  mediaType: image/png
  source:
    localPath: ./logo.png
`,
			wantErr: false,
		},
		{
			name: "Asset source with two variants rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Asset
metadata:
  name: logo
spec:
  type: image
  mediaType: image/png
  source:
    localPath: ./logo.png
    remoteURL: https://example.com/logo.png
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

// TestValidate_Scale verifies the tightened scale field (Issue 8): the keywords
// none/auto, positive numbers and positive numeric strings are accepted, while a
// misspelled keyword is rejected.
func TestValidate_Scale(t *testing.T) {
	cases := []struct {
		scale   string
		wantErr bool
	}{
		{"none", false},
		{"auto", false},
		{"0.8", false},   // numeric string (documented form)
		{`"0.8"`, false}, // explicitly quoted numeric string
		{"1.2", false},
		{"atuo", true}, // typo of "auto"
		{"abc", true},
	}
	for _, c := range cases {
		t.Run(c.scale, func(t *testing.T) {
			yaml := "apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: t\nspec:\n  value: hi\n  scale: " + c.scale + "\n"
			err := Validate([]byte(yaml))
			if c.wantErr && err == nil {
				t.Errorf("scale %q: expected error, got nil", c.scale)
			}
			if !c.wantErr && err != nil {
				t.Errorf("scale %q: expected valid, got: %v", c.scale, err)
			}
		})
	}
}
