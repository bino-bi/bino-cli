package registry

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// fakeResolver resolves from a static map keyed "scope/name@ref" (ref may be
// empty) and counts calls per package.
type fakeResolver struct {
	responses map[string]ResolveResult
	calls     map[string]int
}

func (f *fakeResolver) Resolve(_ context.Context, scope, name, ref string) (ResolveResult, error) {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[scope+"/"+name]++
	if res, ok := f.responses[scope+"/"+name+"@"+ref]; ok {
		return res, nil
	}
	if res, ok := f.responses[scope+"/"+name+"@"]; ok && !IsPin(ref) {
		return res, nil
	}
	return ResolveResult{}, &APIError{Code: "version_not_found", Status: 404}
}

func res(pkg, tag, version, kind string, deps ...string) ResolveResult {
	return ResolveResult{Package: pkg, Tag: tag, Version: version, Kind: kind, Digest: "sha256:" + version, Dependencies: deps}
}

func TestResolveClosureChain(t *testing.T) {
	f := &fakeResolver{responses: map[string]ResolveResult{
		"acme/tbl@":     res("@acme/tbl", "latest", "2.1.0", "Table", "@bino/style_a"),
		"bino/style_a@": res("@bino/style_a", "latest", "1.4.0", "ComponentStyle", "@bino/tokens"),
		"bino/tokens@":  res("@bino/tokens", "latest", "1.0.0", "ScalingGroup"),
	}}
	got, err := ResolveClosure(context.Background(), f, []Root{{Name: "@acme/tbl"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("closure size = %d, want 3: %+v", len(got), got)
	}
	// Sorted by name; only the root is direct.
	if got[0].Name != "@acme/tbl" || !got[0].Direct || got[1].Direct || got[2].Direct {
		t.Errorf("direct flags wrong: %+v", got)
	}
	if got[0].Dependencies[0] != "@bino/style_a" {
		t.Errorf("dependencies not recorded: %+v", got[0])
	}
}

func TestResolveClosureDiamondDedup(t *testing.T) {
	f := &fakeResolver{responses: map[string]ResolveResult{
		"a/root@":   res("@a/root", "latest", "1.0.0", "LayoutPage", "@a/left", "@a/right"),
		"a/left@":   res("@a/left", "latest", "1.0.0", "Text", "@a/shared"),
		"a/right@":  res("@a/right", "latest", "1.0.0", "Text", "@a/shared"),
		"a/shared@": res("@a/shared", "latest", "3.0.0", "ComponentStyle"),
	}}
	got, err := ResolveClosure(context.Background(), f, []Root{{Name: "@a/root"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("closure size = %d, want 4", len(got))
	}
	if f.calls["a/shared"] != 1 {
		t.Errorf("shared dep resolved %d times, want 1", f.calls["a/shared"])
	}
}

func TestResolveClosurePinConflict(t *testing.T) {
	f := &fakeResolver{responses: map[string]ResolveResult{
		"a/left@":        res("@a/left", "latest", "1.0.0", "Text", "@a/shared@1.0.0"),
		"a/right@":       res("@a/right", "latest", "1.0.0", "Text", "@a/shared@2.0.0"),
		"a/shared@1.0.0": res("@a/shared", "", "1.0.0", "ComponentStyle"),
		"a/shared@2.0.0": res("@a/shared", "", "2.0.0", "ComponentStyle"),
	}}
	_, err := ResolveClosure(context.Background(), f, []Root{{Name: "@a/left"}, {Name: "@a/right"}})
	if !errors.Is(err, ErrDependencyConflict) {
		t.Fatalf("expected ErrDependencyConflict, got %v", err)
	}
}

func TestResolveClosurePinAndTagUnify(t *testing.T) {
	f := &fakeResolver{responses: map[string]ResolveResult{
		"a/left@":        res("@a/left", "latest", "1.0.0", "Text", "@a/shared@2.0.0"),
		"a/right@":       res("@a/right", "latest", "1.0.0", "Text", "@a/shared"),
		"a/shared@2.0.0": res("@a/shared", "", "2.0.0", "ComponentStyle"),
		"a/shared@":      res("@a/shared", "latest", "2.0.0", "ComponentStyle"),
	}}
	got, err := ResolveClosure(context.Background(), f, []Root{{Name: "@a/left"}, {Name: "@a/right"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("closure size = %d, want 3", len(got))
	}
}

func TestResolveClosureCycle(t *testing.T) {
	f := &fakeResolver{responses: map[string]ResolveResult{
		"a/x@": res("@a/x", "latest", "1.0.0", "Text", "@a/y"),
		"a/y@": res("@a/y", "latest", "1.0.0", "Text", "@a/x"),
	}}
	_, err := ResolveClosure(context.Background(), f, []Root{{Name: "@a/x"}})
	if !errors.Is(err, ErrCycle) {
		t.Fatalf("expected ErrCycle, got %v", err)
	}
}

func TestResolveClosureDeterministicOrder(t *testing.T) {
	f := &fakeResolver{responses: map[string]ResolveResult{
		"a/x@": res("@a/x", "latest", "1.0.0", "Text"),
		"a/y@": res("@a/y", "latest", "1.0.0", "Text"),
		"a/z@": res("@a/z", "latest", "1.0.0", "Text"),
	}}
	first, err := ResolveClosure(context.Background(), f, []Root{{Name: "@a/z"}, {Name: "@a/x"}, {Name: "@a/y"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveClosure(context.Background(), f, []Root{{Name: "@a/y"}, {Name: "@a/z"}, {Name: "@a/x"}})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%+v", first) != fmt.Sprintf("%+v", second) {
		t.Errorf("plans differ across root order:\n%+v\n%+v", first, second)
	}
	for i, name := range []string{"@a/x", "@a/y", "@a/z"} {
		if first[i].Name != name {
			t.Errorf("plan[%d] = %s, want %s", i, first[i].Name, name)
		}
	}
}

func TestResolveClosureDirectFlagUpgrade(t *testing.T) {
	// A package that is both a transitive dep and a direct root must be direct.
	f := &fakeResolver{responses: map[string]ResolveResult{
		"a/root@":   res("@a/root", "latest", "1.0.0", "LayoutPage", "@a/shared"),
		"a/shared@": res("@a/shared", "latest", "1.0.0", "ComponentStyle"),
	}}
	got, err := ResolveClosure(context.Background(), f, []Root{{Name: "@a/root"}, {Name: "@a/shared"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got {
		if !r.Direct {
			t.Errorf("%s not marked direct: %+v", r.Name, got)
		}
	}
}
