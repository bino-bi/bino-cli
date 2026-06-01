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
