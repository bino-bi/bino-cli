// Package archive provides a single hardened zip extractor shared by the engine,
// chrome, and template subsystems. Trusted callers (first-party signed release
// archives) use the permissive modes; the template subsystem uses Untrusted
// mode, which rejects symlinks, caps decompression, and normalizes file modes so
// an arbitrary remote ZIP cannot escape the destination or exhaust disk.
package archive

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// StripMode controls how a leading directory is removed from archive entries.
type StripMode int

const (
	// StripNone keeps entry names as-is.
	StripNone StripMode = iota
	// StripFixed strips a caller-provided fixed prefix (e.g. "bn-template-engine/").
	StripFixed
	// StripAuto detects and strips the single common top-level directory
	// (e.g. GitHub's "{repo}-{ref}/" wrapper, or Chrome-for-Testing's dir).
	StripAuto
)

// Limits caps decompression to defeat zip bombs. A zero field disables that cap.
type Limits struct {
	MaxFileBytes  int64 // per-entry decompressed cap
	MaxTotalBytes int64 // total decompressed cap across all entries
	MaxEntries    int   // entry-count cap
}

// DefaultLimits returns the recommended caps for untrusted template archives.
func DefaultLimits() Limits {
	return Limits{
		MaxFileBytes:  32 << 20,  // 32 MiB
		MaxTotalBytes: 128 << 20, // 128 MiB
		MaxEntries:    5000,
	}
}

// Options configures Extract.
type Options struct {
	Strip       StripMode
	FixedPrefix string // required when Strip == StripFixed
	Untrusted   bool   // reject symlinks, normalize modes, enforce Limits
	Limits      Limits // applied only when Untrusted
}

// Sentinel errors for untrusted-mode violations.
var (
	ErrSymlink       = errors.New("symlink entries are not allowed")
	ErrFileTooBig    = errors.New("decompressed file exceeds limit")
	ErrArchiveTooBig = errors.New("decompressed archive exceeds limit")
	ErrTooManyFiles  = errors.New("archive has too many entries")
)

// Extract opens zipPath and extracts it into destDir per opts. The zip-slip
// guard (string-prefix plus, in untrusted mode, a resolved-parent re-check) is
// always enforced.
func Extract(zipPath, destDir string, opts Options) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	prefix, err := resolvePrefix(r.File, opts)
	if err != nil {
		return err
	}

	cleanDest := filepath.Clean(destDir)
	resolvedDest := cleanDest
	if opts.Untrusted {
		if rd, evalErr := filepath.EvalSymlinks(cleanDest); evalErr == nil {
			resolvedDest = rd
		}
	}

	var entries int
	var total int64
	for _, f := range r.File {
		name := strings.TrimPrefix(f.Name, prefix)
		if name == "" {
			continue
		}

		if opts.Untrusted {
			entries++
			if opts.Limits.MaxEntries > 0 && entries > opts.Limits.MaxEntries {
				return fmt.Errorf("%w (max %d)", ErrTooManyFiles, opts.Limits.MaxEntries)
			}
			if !f.FileInfo().IsDir() && !f.Mode().IsRegular() {
				return fmt.Errorf("%w: %s", ErrSymlink, f.Name)
			}
		}

		targetPath := filepath.Join(destDir, filepath.FromSlash(name))

		// Prevent path traversal, including sibling-directory escapes such as
		// "../dest-evil/x" that share destDir as a bare string prefix.
		if targetPath != cleanDest && !strings.HasPrefix(targetPath, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return fmt.Errorf("create directory %s: %w", name, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("create parent directory for %s: %w", name, err)
		}

		if opts.Untrusted {
			if err := verifyResolvedParent(resolvedDest, filepath.Dir(targetPath)); err != nil {
				return fmt.Errorf("invalid file path in zip: %s", f.Name)
			}
		}

		written, err := extractFile(f, targetPath, opts)
		if err != nil {
			return fmt.Errorf("extract %s: %w", name, err)
		}
		if opts.Untrusted {
			total += written
			if opts.Limits.MaxTotalBytes > 0 && total > opts.Limits.MaxTotalBytes {
				return fmt.Errorf("%w (max %d bytes)", ErrArchiveTooBig, opts.Limits.MaxTotalBytes)
			}
		}
	}

	return nil
}

// resolvePrefix returns the directory prefix to strip from every entry.
func resolvePrefix(files []*zip.File, opts Options) (string, error) {
	switch opts.Strip {
	case StripNone:
		return "", nil
	case StripFixed:
		return opts.FixedPrefix, nil
	case StripAuto:
		return detectCommonPrefix(files), nil
	default:
		return "", fmt.Errorf("unknown strip mode %d", opts.Strip)
	}
}

// detectCommonPrefix finds the single shared top-level directory across entries,
// or "" if entries do not share one.
func detectCommonPrefix(files []*zip.File) string {
	var prefix string
	for _, f := range files {
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 {
			continue
		}
		candidate := parts[0] + "/"
		if prefix == "" {
			prefix = candidate
		} else if prefix != candidate {
			return ""
		}
	}
	return prefix
}

// extractFile writes one zip entry to destPath and returns the bytes written.
// Trusted mode preserves the archive's file mode and copies unbounded; untrusted
// mode forces 0o644, opens with O_NOFOLLOW, and enforces the per-file size cap.
func extractFile(f *zip.File, destPath string, opts Options) (int64, error) {
	src, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer src.Close()

	mode := f.Mode()
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if opts.Untrusted {
		mode = 0o644
		flags |= oNoFollow
	}

	dst, err := os.OpenFile(destPath, flags, mode) //nolint:gosec // G304: destPath is validated by the zip-slip guard
	if err != nil {
		return 0, err
	}
	defer dst.Close()

	if !opts.Untrusted {
		n, copyErr := io.Copy(dst, src) //nolint:gosec // G110: decompressing trusted signed release archives
		return n, copyErr
	}

	var reader io.Reader = src
	if opts.Limits.MaxFileBytes > 0 {
		reader = io.LimitReader(src, opts.Limits.MaxFileBytes+1)
	}
	n, err := io.Copy(dst, reader)
	if err != nil {
		return n, err
	}
	if opts.Limits.MaxFileBytes > 0 && n > opts.Limits.MaxFileBytes {
		return n, fmt.Errorf("%w (max %d bytes): %s", ErrFileTooBig, opts.Limits.MaxFileBytes, f.Name)
	}
	return n, nil
}

// verifyResolvedParent re-checks, after MkdirAll, that the entry's parent
// directory still resolves inside the destination — defeating a pre-existing
// symlink that redirects a write outside destDir.
func verifyResolvedParent(resolvedDest, parentDir string) error {
	resolved, err := filepath.EvalSymlinks(parentDir)
	if err != nil {
		// Parent was just created and contains no symlinks we follow; if it
		// cannot be resolved, fall back to the already-applied prefix guard.
		return nil //nolint:nilerr // prefix guard already validated the literal path
	}
	if resolved != resolvedDest && !strings.HasPrefix(resolved, resolvedDest+string(os.PathSeparator)) {
		return fs.ErrPermission
	}
	return nil
}
