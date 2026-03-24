package plugin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// DiscoverBinary resolves the filesystem path to a plugin binary.
//
// Resolution order:
//  1. Explicit path from bino.toml (if non-empty)
//  2. Project-local: <projectRoot>/.bino/plugins/bino-plugin-<name>
//  3. User-global: ~/.bino/plugins/bino-plugin-<name>
//  4. PATH lookup: bino-plugin-<name>
//
// Returns the absolute path to the binary, or an error if not found.
func DiscoverBinary(name string, explicitPath string, projectRoot string) (string, error) {
	return discoverBinary(name, explicitPath, projectRoot, os.UserHomeDir)
}

// discoverBinary is the internal implementation with injectable home dir for testing.
func discoverBinary(name string, explicitPath string, projectRoot string, homeDir func() (string, error)) (string, error) {
	binaryName := "bino-plugin-" + name
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	// 1. Explicit path
	if explicitPath != "" {
		abs, err := filepath.Abs(explicitPath)
		if err != nil {
			return "", fmt.Errorf("resolving plugin path %q: %w", explicitPath, err)
		}
		// On Windows, append .exe if the explicit path doesn't already have it.
		if runtime.GOOS == "windows" && filepath.Ext(abs) != ".exe" {
			withExe := abs + ".exe"
			if err := checkExecutable(withExe); err == nil {
				return withExe, nil
			}
		}
		if err := checkExecutable(abs); err != nil {
			return "", fmt.Errorf("plugin %q at %s: %w", name, abs, err)
		}
		return abs, nil
	}

	// 2. Project-local
	if projectRoot != "" {
		candidate := filepath.Join(projectRoot, ".bino", "plugins", binaryName)
		if err := checkExecutable(candidate); err == nil {
			return candidate, nil
		}
	}

	// 3. User-global
	if homeDir != nil {
		home, err := homeDir()
		if err == nil {
			candidate := filepath.Join(home, ".bino", "plugins", binaryName)
			if err := checkExecutable(candidate); err == nil {
				return candidate, nil
			}
		}
	}

	// 4. PATH
	path, err := exec.LookPath(binaryName)
	if err == nil {
		return filepath.Abs(path)
	}

	return "", fmt.Errorf("plugin %q not found: searched .bino/plugins/, ~/.bino/plugins/, and $PATH for %s", name, binaryName)
}

// checkExecutable verifies a path exists and is executable.
func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	// On Unix, check execute permission.
	if runtime.GOOS != "windows" {
		if info.Mode()&0o111 == 0 {
			return fmt.Errorf("%s is not executable", path)
		}
	}
	return nil
}
