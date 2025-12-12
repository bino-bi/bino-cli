package pipeline

import (
	"bino.bi/bino/internal/pathutil"
)

// ResolveWorkdir converts a relative or empty directory path to an absolute path
// and validates that it exists and is a directory.
func ResolveWorkdir(dir string) (string, error) {
	return pathutil.ResolveWorkdir(dir)
}

// ResolveOutputDir returns an absolute path for the output directory.
// If outDir is relative, it's resolved against workdir.
func ResolveOutputDir(workdir, outDir string) string {
	return pathutil.ResolveOutputDir(workdir, outDir)
}
