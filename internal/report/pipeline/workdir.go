package pipeline

import (
	"bino.bi/bino/internal/pathutil"
)

// ResolveWorkdir converts a relative or empty directory path to an absolute path
// and validates that it exists and is a directory.
func ResolveWorkdir(dir string) (string, error) {
	return pathutil.ResolveWorkdir(dir)
}

// ResolveProjectRoot finds the project root by searching for bino.toml.
// If workdir is empty, it starts from the current working directory.
// Returns the absolute path to the directory containing bino.toml.
func ResolveProjectRoot(workdir string) (string, error) {
	startDir := workdir
	if startDir == "" {
		startDir = "."
	}
	abs, err := pathutil.ResolveWorkdir(startDir)
	if err != nil {
		return "", err
	}
	return pathutil.FindProjectRoot(abs)
}

// ResolveOutputDir returns an absolute path for the output directory.
// If outDir is relative, it's resolved against workdir.
func ResolveOutputDir(workdir, outDir string) string {
	return pathutil.ResolveOutputDir(workdir, outDir)
}
