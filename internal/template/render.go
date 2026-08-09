package template

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// Render renders a single template body with the engine FuncMap and
// missingkey=error semantics, so a stale or misspelled variable fails loudly
// instead of emitting "<no value>" into a manifest.
func Render(name string, src []byte, data any) ([]byte, error) {
	t, err := template.New(name).Funcs(FuncMap()).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// RenderTree walks srcFS (the render root), renders every file's path name and
// — unless the file matches a verbatim/binary glob — its contents, writing the
// results under destDir. It returns the created relative paths, sorted. The same
// path serves both built-in (embed.FS) and remote (os.DirFS) templates.
func RenderTree(srcFS fs.FS, manifest *ProjectTemplate, destDir string, data any, force bool) ([]string, error) {
	var created []string
	walkErr := fs.WalkDir(srcFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// The manifest lives at the render root only for the "root layout"
		// convention (no template/ subdir); it is config, never scaffolded.
		if p == manifestFile {
			return nil
		}

		renderedPath, err := renderPath(p, data)
		if err != nil {
			return err
		}

		raw, err := fs.ReadFile(srcFS, p)
		if err != nil {
			return fmt.Errorf("read template file %s: %w", p, err)
		}

		var content []byte
		switch {
		case manifest.IsBinary(p), manifest.IsVerbatim(p):
			content = raw
		default:
			content, err = Render(p, raw, data)
			if err != nil {
				return err
			}
		}

		absPath := filepath.Join(destDir, filepath.FromSlash(renderedPath))
		// Guard against a rendered path (e.g. a field value containing "..")
		// escaping the target directory.
		cleanDest := filepath.Clean(destDir)
		if !strings.HasPrefix(absPath, cleanDest) || (absPath != cleanDest && !strings.HasPrefix(absPath, cleanDest+string(os.PathSeparator))) {
			return fmt.Errorf("rendered path %q escapes the target directory", renderedPath)
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", filepath.Dir(absPath), err)
		}
		if !force {
			if _, statErr := os.Stat(absPath); statErr == nil {
				return fmt.Errorf("%s already exists; use --force to overwrite", renderedPath)
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return fmt.Errorf("stat %s: %w", absPath, statErr)
			}
		}
		if err := os.WriteFile(absPath, content, 0o644); err != nil { //nolint:gosec // G306: scaffold files need standard read perms
			return fmt.Errorf("write %s: %w", absPath, err)
		}
		created = append(created, renderedPath)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(created)
	return created, nil
}

// renderPath renders template actions embedded in a file path (e.g.
// pages/{{ .ReportName }}.yaml). Paths without actions are returned untouched.
func renderPath(p string, data any) (string, error) {
	if !strings.Contains(p, "{{") {
		return p, nil
	}
	out, err := Render("path:"+p, []byte(p), data)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// matchGlob reports whether p matches a template glob. It supports a trailing
// "/**" recursive suffix (e.g. resources/assets/**) plus path.Match semantics on
// both the full path and the basename (so "*.md" matches nested files too).
func matchGlob(glob, p string) bool {
	if prefix, ok := strings.CutSuffix(glob, "/**"); ok {
		return p == prefix || strings.HasPrefix(p, prefix+"/")
	}
	if ok, _ := path.Match(glob, p); ok { //nolint:errcheck // an invalid pattern counts as no match
		return true
	}
	if ok, _ := path.Match(glob, path.Base(p)); ok { //nolint:errcheck // an invalid pattern counts as no match
		return true
	}
	return false
}
