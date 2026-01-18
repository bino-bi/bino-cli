package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/schema"
)

// ManifestInfo holds metadata about a manifest document.
type ManifestInfo struct {
	File     string
	Position int
	Kind     string
	Name     string
}

// FilePattern describes the file organization pattern detected in the project.
type FilePattern struct {
	Mode      string // "separate-files" or "multi-document"
	Directory string // For separate-files: common directory (e.g., "datasets/")
	File      string // For multi-document: common file (e.g., "data.yaml")
	DocCount  int    // Number of documents in the file (for multi-document)
}

// ScanManifests scans the working directory for existing manifest documents.
// Uses lenient mode to handle partially valid manifests.
func ScanManifests(ctx context.Context, dir string) ([]ManifestInfo, error) {
	dir = filepath.Clean(dir)

	docs, err := config.LoadDirWithOptions(ctx, dir, config.LoadOptions{
		Lenient: true,
	})
	if err != nil {
		// If we can't load at all, return empty list rather than error
		// This allows the add command to work in empty projects
		return nil, nil
	}

	manifests := make([]ManifestInfo, 0, len(docs))
	for _, doc := range docs {
		manifests = append(manifests, ManifestInfo{
			File:     doc.File,
			Position: doc.Position,
			Kind:     doc.Kind,
			Name:     doc.Name,
		})
	}

	return manifests, nil
}

// FilterByKind returns manifests matching any of the specified kinds.
func FilterByKind(manifests []ManifestInfo, kinds ...string) []ManifestInfo {
	if len(kinds) == 0 {
		return manifests
	}

	kindSet := make(map[string]bool)
	for _, k := range kinds {
		kindSet[k] = true
	}

	filtered := make([]ManifestInfo, 0)
	for _, m := range manifests {
		if kindSet[m.Kind] {
			filtered = append(filtered, m)
		}
	}

	return filtered
}

// IsNameUnique checks if a name is unique within a kind.
func IsNameUnique(manifests []ManifestInfo, kind, name string) bool {
	for _, m := range manifests {
		if m.Kind == kind && m.Name == name {
			return false
		}
	}
	return true
}

// FindByName finds a manifest by kind and name.
func FindByName(manifests []ManifestInfo, kind, name string) *ManifestInfo {
	for _, m := range manifests {
		if m.Kind == kind && m.Name == name {
			return &m
		}
	}
	return nil
}

// DetectFilePattern analyzes existing manifests to determine file organization.
func DetectFilePattern(manifests []ManifestInfo, kind string) FilePattern {
	kindManifests := FilterByKind(manifests, kind)

	if len(kindManifests) == 0 {
		// No existing manifests of this kind - use separate-files as default
		dir := defaultDirectoryForKind(kind)
		return FilePattern{
			Mode:      "separate-files",
			Directory: dir,
		}
	}

	// Count documents per file
	fileCounts := make(map[string]int)
	for _, m := range kindManifests {
		fileCounts[m.File]++
	}

	// Calculate average documents per file
	totalFiles := len(fileCounts)
	totalDocs := len(kindManifests)
	avgDocsPerFile := float64(totalDocs) / float64(totalFiles)

	// If average is less than 2, use separate-files mode
	if avgDocsPerFile < 2.0 {
		// Find the common directory
		dir := findCommonDirectory(kindManifests)
		return FilePattern{
			Mode:      "separate-files",
			Directory: dir,
		}
	}

	// Multi-document mode: find the file with most documents
	var maxFile string
	var maxCount int
	for file, count := range fileCounts {
		if count > maxCount {
			maxFile = file
			maxCount = count
		}
	}

	return FilePattern{
		Mode:     "multi-document",
		File:     maxFile,
		DocCount: maxCount,
	}
}

// defaultDirectoryForKind returns the default directory for a kind.
func defaultDirectoryForKind(kind string) string {
	switch kind {
	case "DataSet":
		return "datasets"
	case "DataSource":
		return "datasources"
	case "ConnectionSecret":
		return "secrets"
	case "Asset":
		return "assets"
	case "LayoutPage", "LayoutCard":
		return "layouts"
	case "ChartStructure", "ChartTime", "Table", "Text":
		return "components"
	case "ComponentStyle":
		return "styles"
	case "Internationalization":
		return "i18n"
	case "ReportArtefact", "LiveReportArtefact":
		return "reports"
	case "SigningProfile":
		return "signing"
	default:
		return "manifests"
	}
}

// findCommonDirectory finds the most common directory among manifests.
func findCommonDirectory(manifests []ManifestInfo) string {
	if len(manifests) == 0 {
		return ""
	}

	dirCounts := make(map[string]int)
	for _, m := range manifests {
		dir := filepath.Dir(m.File)
		dirCounts[dir]++
	}

	var maxDir string
	var maxCount int
	for dir, count := range dirCounts {
		if count > maxCount {
			maxDir = dir
			maxCount = count
		}
	}

	// Return relative path from cwd
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, maxDir); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}

	return maxDir
}

// SuggestOutputPath suggests an output path based on the file pattern.
func SuggestOutputPath(pattern FilePattern, name, kind string) string {
	switch pattern.Mode {
	case "multi-document":
		return pattern.File
	case "separate-files":
		dir := pattern.Directory
		if dir == "" {
			dir = defaultDirectoryForKind(kind)
		}
		return filepath.Join(dir, name+".yaml")
	default:
		return filepath.Join(defaultDirectoryForKind(kind), name+".yaml")
	}
}

// WriteManifest writes a manifest to a new file.
func WriteManifest(path, content string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := pathutil.EnsureDir(dir); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	// Check if file exists
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("file %s already exists", path)
	}

	// Write file
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

// AppendToManifest appends a manifest to an existing multi-document file.
func AppendToManifest(path, content string) error {
	// Read existing content
	existing, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	// Ensure content has document separator
	newContent := string(existing)
	if !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += "---\n"
	newContent += content

	// Write back
	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

// WriteSchemaDocument validates and writes a schema.Document to a file.
// It handles both creating new files and appending to existing multi-document files.
// The out writer is used to print success messages.
func WriteSchemaDocument(doc *schema.Document, workdir, outputPath string, appendMode bool, out io.Writer) error {
	// Validate the document name
	if err := ValidateName(doc.Metadata.Name); err != nil {
		return ConfigError(err)
	}

	// Marshal to YAML
	manifestBytes, err := yaml.Marshal(doc)
	if err != nil {
		return RuntimeError(fmt.Errorf("render manifest: %w", err))
	}

	// Validate against JSON schema
	if err := schema.Validate(manifestBytes); err != nil {
		return ConfigError(fmt.Errorf("generated manifest is invalid: %w", err))
	}

	manifest := string(manifestBytes)

	// Resolve absolute path
	absPath := outputPath
	if !filepath.IsAbs(outputPath) {
		absPath = filepath.Join(workdir, outputPath)
	}

	// Write to file
	if appendMode {
		if err := AppendToManifest(absPath, manifest); err != nil {
			return RuntimeError(err)
		}
		fmt.Fprintf(out, "Appended to %s\n", outputPath)
	} else {
		if err := WriteManifest(absPath, manifest); err != nil {
			return RuntimeError(err)
		}
		fmt.Fprintf(out, "Created %s\n", outputPath)
	}

	return nil
}

// RenderSchemaDocument renders a schema.Document to YAML bytes.
// Useful for previewing before writing.
func RenderSchemaDocument(doc *schema.Document) ([]byte, error) {
	return yaml.Marshal(doc)
}

// DataSetManifestData holds data for rendering a DataSet manifest.
type DataSetManifestData struct {
	Name         string
	Description  string
	Constraints  []string
	Dependencies []string
	Query        string   // SQL query content (inline)
	QueryFile    string   // SQL file path (external)
	PRQL         string   // PRQL query content (inline)
	PRQLFile     string   // PRQL file path (external)
	Source       string   // DataSource name (pass-through)
}

// DataSourceManifestData holds data for rendering a DataSource manifest.
type DataSourceManifestData struct {
	Name        string
	Description string
	Constraints []string
	Type        DataSourceType

	// For file-based sources (csv, parquet, excel, json)
	Path string

	// For database sources (postgres_query, mysql_query)
	DBHost     string
	DBPort     int
	DBDatabase string
	DBSchema   string
	DBUser     string
	DBSecret   string // ConnectionSecret name
	DBQuery    string

	// CSV-specific options
	CSVDelimiter string
	CSVHeader    *bool
	CSVSkipRows  int
}

// quoteYAMLIfNeeded adds quotes to a string if it contains special characters.
func quoteYAMLIfNeeded(s string) string {
	// Check if quoting is needed
	needsQuotes := false
	for _, r := range s {
		switch r {
		case ':', '#', '[', ']', '{', '}', '&', '*', '!', '|', '>', '\'', '"', '%', '@', '`', ',', '?':
			needsQuotes = true
		}
	}
	if strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		needsQuotes = true
	}

	if needsQuotes {
		escaped := strings.ReplaceAll(s, "\"", "\\\"")
		return fmt.Sprintf("\"%s\"", escaped)
	}

	return s
}

// SearchQueryFiles searches for SQL and PRQL files in the project.
func SearchQueryFiles(dir string, ext string) ([]string, error) {
	var files []string

	// Search in common locations
	searchDirs := []string{
		".",
		"queries",
		"sql",
		"prql",
	}

	for _, searchDir := range searchDirs {
		searchPath := filepath.Join(dir, searchDir)
		if _, err := os.Stat(searchPath); os.IsNotExist(err) {
			continue
		}

		err := filepath.WalkDir(searchPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // Skip errors
			}
			if d.IsDir() {
				// Skip hidden directories and common ignores
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ext) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			continue
		}
	}

	// Sort by path
	sort.Strings(files)

	return files, nil
}

// ManifestsToFuzzyItems converts manifest infos to fuzzy search items.
func ManifestsToFuzzyItems(manifests []ManifestInfo) []FuzzyItem {
	items := make([]FuzzyItem, len(manifests))
	for i, m := range manifests {
		items[i] = FuzzyItem{
			Name:     m.Name,
			Kind:     m.Kind,
			File:     m.File,
			Position: m.Position,
		}
	}
	return items
}

// FilesToFuzzyItems converts file paths to fuzzy search items.
func FilesToFuzzyItems(files []string, kind string) []FuzzyItem {
	items := make([]FuzzyItem, len(files))
	for i, f := range files {
		// Get relative path for display
		rel := f
		if wd, err := os.Getwd(); err == nil {
			if r, err := filepath.Rel(wd, f); err == nil && !strings.HasPrefix(r, "..") {
				rel = r
			}
		}
		items[i] = FuzzyItem{
			Name:     rel,
			Kind:     kind,
			File:     f,
			Position: 0,
		}
	}
	return items
}

// GetMultiDocFiles returns a list of multi-document YAML files.
func GetMultiDocFiles(manifests []ManifestInfo) []string {
	fileCounts := make(map[string]int)
	for _, m := range manifests {
		fileCounts[m.File]++
	}

	var files []string
	for file, count := range fileCounts {
		if count > 1 {
			files = append(files, file)
		}
	}

	sort.Strings(files)
	return files
}
