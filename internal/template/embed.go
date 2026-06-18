package template

import (
	"embed"
	"fmt"
	"io/fs"
)

// builtinFS holds the templates compiled into the binary. The `all:` prefix is
// required so dotfiles (.bnignore, .gitignore) inside the render roots are
// embedded too.
//
//go:embed all:builtin
var builtinFS embed.FS

// builtinNames is the set of built-in template names.
var builtinNames = map[string]struct{}{
	"minimal":  {},
	"standard": {},
}

// IsBuiltin reports whether name refers to a built-in template.
func IsBuiltin(name string) bool {
	_, ok := builtinNames[name]
	return ok
}

// BuiltinNames returns the built-in template names.
func BuiltinNames() []string {
	return []string{"minimal", "standard"}
}

// BuiltinManifest parses the manifest of a built-in template.
func BuiltinManifest(name string) (*ProjectTemplate, error) {
	if !IsBuiltin(name) {
		return nil, fmt.Errorf("unknown built-in template %q", name)
	}
	data, err := builtinFS.ReadFile("builtin/" + name + "/" + manifestFile)
	if err != nil {
		return nil, fmt.Errorf("read built-in %q manifest: %w", name, err)
	}
	return ParseManifest(data)
}

// BuiltinRoot returns the render-root FS (the template/ subtree) of a built-in.
func BuiltinRoot(name string) (fs.FS, error) {
	if !IsBuiltin(name) {
		return nil, fmt.Errorf("unknown built-in template %q", name)
	}
	sub, err := fs.Sub(builtinFS, "builtin/"+name+"/template")
	if err != nil {
		return nil, fmt.Errorf("open built-in %q render root: %w", name, err)
	}
	return sub, nil
}
