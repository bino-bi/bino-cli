package watchers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	gitignore "github.com/sabhiram/go-gitignore"

	"bino.bi/bino/internal/logx"
)

const ignoreFileName = ".bnignore"

// Event represents a file update notification once filters have been applied.
type Event struct {
	Path         string
	RelativePath string
	Op           fsnotify.Op
}

// Handler reacts to watcher events.
type Handler func(Event)

// Config configures the Watcher.
type Config struct {
	Root    string
	Logger  logx.Logger
	Handler Handler

	// Dirs, when non-empty, provides a pre-collected list of directories to watch.
	// This avoids a redundant filesystem walk when the caller already walked the tree
	// (e.g., config.LoadDirWithOptions with CollectedDirs). When empty, the watcher
	// walks the Root directory tree itself.
	Dirs []string
}

// Watcher monitors a directory tree for file changes and emits events for relevant files.
type Watcher struct {
	watcher *fsnotify.Watcher
	cfg     Config

	ignoreMu sync.RWMutex
	ignore   *gitignore.GitIgnore
}

// NewWatcher constructs a watcher and registers the directory tree immediately.
func NewWatcher(cfg Config) (*Watcher, error) {
	if cfg.Root == "" {
		return nil, fmt.Errorf("watcher: root directory is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = logx.Nop()
	}
	if cfg.Handler == nil {
		cfg.Handler = func(Event) {}
	}

	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watcher: init file watcher: %w", err)
	}

	absRoot, err := filepath.Abs(filepath.Clean(cfg.Root))
	if err != nil {
		fw.Close()
		return nil, fmt.Errorf("watcher: resolve root: %w", err)
	}
	cfg.Root = absRoot
	yw := &Watcher{watcher: fw, cfg: cfg}
	if err := yw.refreshIgnorePatterns(); err != nil {
		fw.Close()
		return nil, err
	}
	if len(cfg.Dirs) > 0 {
		// Use pre-collected directories instead of walking the tree again.
		if err := yw.registerDirs(cfg.Dirs); err != nil {
			fw.Close()
			return nil, err
		}
	} else {
		if err := yw.addTree(cfg.Root); err != nil {
			fw.Close()
			return nil, err
		}
	}

	cfg.Logger.Infof("Watching %s for changes", cfg.Root)
	return yw, nil
}

// Close releases watcher resources.
func (y *Watcher) Close() error {
	if y.watcher == nil {
		return nil
	}
	return y.watcher.Close()
}

// Run begins processing events until the context is canceled.
func (y *Watcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-y.watcher.Events:
			if !ok {
				return
			}
			y.process(event)
		case err, ok := <-y.watcher.Errors:
			if !ok {
				return
			}
			y.cfg.Logger.Errorf("watcher: %v", err)
		}
	}
}

// registerDirs registers a pre-collected list of directories with the fsnotify watcher,
// skipping any that match ignore patterns. This is O(n) in the number of directories.
func (y *Watcher) registerDirs(dirs []string) error {
	for _, d := range dirs {
		if y.shouldIgnorePath(d, true) && d != y.cfg.Root {
			continue
		}
		if err := y.watcher.Add(d); err != nil {
			return fmt.Errorf("watcher: add %s: %w", d, err)
		}
	}
	return nil
}

func (y *Watcher) addTree(root string) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			if y.shouldIgnorePath(p, false) {
				return nil
			}
			return nil
		}
		if y.shouldIgnorePath(p, true) && root != p {
			return filepath.SkipDir
		}
		if err := y.watcher.Add(p); err != nil {
			return fmt.Errorf("watcher: add %s: %w", p, err)
		}
		return nil
	})
}

func (y *Watcher) process(event fsnotify.Event) {
	if event.Op&fsnotify.Create == fsnotify.Create {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			if y.shouldIgnorePath(event.Name, true) {
				return
			}
			_ = y.addTree(event.Name)
			return
		}
	}

	if y.handleIgnoreUpdate(event) {
		return
	}

	if y.shouldIgnorePath(event.Name, false) {
		return
	}

	if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
		return
	}

	rel, _ := y.relativePath(event.Name)

	y.cfg.Handler(Event{
		Path:         event.Name,
		RelativePath: rel,
		Op:           event.Op,
	})
}

func (y *Watcher) handleIgnoreUpdate(event fsnotify.Event) bool {
	if y.cfg.Root == "" {
		return false
	}
	rel, ok := y.relativePath(event.Name)
	if !ok || rel != ignoreFileName {
		return false
	}
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
		return true
	}
	if err := y.refreshIgnorePatterns(); err != nil {
		y.cfg.Logger.Warnf("watcher: reload %s failed: %v", ignoreFileName, err)
	} else if y.cfg.Logger != nil {
		y.cfg.Logger.Infof("watcher: updated ignore patterns from %s", ignoreFileName)
	}
	return true
}

func (y *Watcher) refreshIgnorePatterns() error {
	path := filepath.Join(y.cfg.Root, ignoreFileName)
	ignore, err := compileIgnoreFile(path)
	if err != nil {
		return err
	}
	y.ignoreMu.Lock()
	y.ignore = ignore
	y.ignoreMu.Unlock()
	return nil
}

func compileIgnoreFile(path string) (*gitignore.GitIgnore, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	ignore, err := gitignore.CompileIgnoreFile(path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return ignore, nil
}

func (y *Watcher) shouldIgnorePath(path string, isDir bool) bool {
	rel, ok := y.relativePath(path)
	if !ok || rel == "" {
		return false
	}
	rel = filepath.ToSlash(rel)

	// Always ignore .bncache directory (built-in, not configurable)
	if rel == ".bncache" || strings.HasPrefix(rel, ".bncache/") {
		return true
	}

	// Always ignore daemon port file (built-in, not configurable)
	if rel == ".bino-daemon.json" {
		return true
	}

	y.ignoreMu.RLock()
	ignore := y.ignore
	y.ignoreMu.RUnlock()
	if ignore == nil {
		return false
	}
	if ignore.MatchesPath(rel) {
		return true
	}
	if isDir {
		return ignore.MatchesPath(rel + "/")
	}
	return false
}

func (y *Watcher) relativePath(path string) (string, bool) {
	if y.cfg.Root == "" {
		return "", false
	}
	rel, err := filepath.Rel(y.cfg.Root, path)
	if err != nil {
		return "", false
	}
	if rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return rel, true
}
