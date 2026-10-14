package config

import (
	"context"
	"errors"
	"testing"

	"bino.bi/bino/internal/schema"
)

// validatorProvider is a KindProvider that also supplies a document validator.
type validatorProvider struct {
	mockKindProvider
	v   *schema.Validator
	err error
}

func (p *validatorProvider) DocumentValidator(context.Context) (*schema.Validator, error) {
	return p.v, p.err
}

const pageWithPluginChild = `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Gauge
      spec:
        title: x
`

func TestValidateManifest(t *testing.T) {
	ctx := context.Background()
	permissive, err := schema.CompileValidator([]byte(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}

	if err := ValidateManifest(ctx, nil, []byte(pageWithPluginChild)); err == nil {
		t.Error("embedded schema must reject a plugin child")
	}
	if err := ValidateManifest(ctx, &validatorProvider{v: permissive}, []byte(pageWithPluginChild)); err != nil {
		t.Errorf("plugin validator must be used: %v", err)
	}
	if err := ValidateManifest(ctx, &validatorProvider{}, []byte(pageWithPluginChild)); err == nil {
		t.Error("a nil plugin validator must fall back to the embedded schema")
	}
	broken := errors.New("plugin gauges: get schemas: rpc broke")
	if err := ValidateManifest(ctx, &validatorProvider{err: broken}, []byte(pageWithPluginChild)); !errors.Is(err, broken) {
		t.Errorf("want the validator error, got %v", err)
	}
}
