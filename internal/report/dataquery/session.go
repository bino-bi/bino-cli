// Package dataquery runs SQL that an agent writes, and data profiles, over a
// project's DataSources and DataSets. Every call gets its own DuckDB session
// that can read only the project's data and change nothing outside itself.
package dataquery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/pkg/duckdb"
)

const (
	// statementTimeout is DuckDB's max_execution_time for each statement.
	statementTimeout = 30 * time.Second
	// memoryLimit caps the memory of one call's DuckDB session.
	memoryLimit = "1GB"
	// tempLimit caps what one session may spill to disk.
	tempLimit = "1GB"
	// threads is the number of DuckDB threads one session may use.
	threads = 2
	// maxConcurrent is how many calls may hold a session at the same time.
	maxConcurrent = 2
)

// slots limits the sessions open at the same time in this process.
var slots = make(chan struct{}, maxConcurrent)

// acquire waits for a free slot or for ctx to end.
func acquire(ctx context.Context) (release func(), err error) {
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for a free query slot: %w", ctx.Err())
	}
}

// errDirectFile explains a Permission Error: the locked session reads only
// what the DataSources read.
var errDirectFile = errors.New("query_data and profile_data read files and URLs only through DataSources; a DataSet whose own SQL reads a file or URL directly (read_csv, 'file.csv', ...) is not supported by these tools")

// explain adds what DuckDB's message cannot know: the ambiguous names that
// can cause it, and why a file was refused.
func explain(ambiguous config.Ambiguities, err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "file system operations are disabled by configuration") {
		err = errors.Join(err, errDirectFile)
	}
	return ambiguous.Explain(err)
}

// entry is one name the SQL may read.
type entry struct {
	doc *config.Document
}

// catalog maps the lower-cased names of the project's DataSources and
// DataSets to their documents. SQL identifiers are case-insensitive, so a
// DataSet whose name a DataSource already uses is not offered: the name means
// the DataSource.
type catalog struct {
	docs    []config.Document
	entries map[string]entry
}

func newCatalog(docs []config.Document) *catalog {
	c := &catalog{docs: docs, entries: map[string]entry{}}
	for _, kind := range []string{"DataSource", "DataSet"} {
		for i := range docs {
			d := &docs[i]
			if d.Kind != kind || d.Name == "" {
				continue
			}
			key := strings.ToLower(d.Name)
			if _, taken := c.entries[key]; !taken {
				c.entries[key] = entry{doc: d}
			}
		}
	}
	return c
}

func (c *catalog) known(name string) bool {
	_, ok := c.entries[strings.ToLower(name)]
	return ok
}

// names lists the offered names, sorted, for error messages.
func (c *catalog) names() []string {
	out := make([]string, 0, len(c.entries))
	for _, e := range c.entries {
		out = append(out, e.doc.Name)
	}
	slices.Sort(out)
	return out
}

// session is one call's DuckDB session and the connection that runs the
// call's statements.
type session struct {
	sess    *duckdb.Session
	conn    *sql.Conn
	access  datasource.Access // what the views read, set by prepare
	release func()
}

func (s *session) Close() {
	if s.conn != nil {
		_ = s.conn.Close() //nolint:errcheck // teardown of a throwaway session
	}
	_ = s.sess.Close() //nolint:errcheck // teardown of a throwaway session
	s.release()
}

// openSession opens a new in-memory DuckDB session, separate from any shared
// session, with the built-in macros and the extension cache of the CLI.
// It waits for one of the maxConcurrent slots first.
func openSession(ctx context.Context) (*session, error) {
	release, err := acquire(ctx)
	if err != nil {
		return nil, err
	}
	opts, err := duckdb.DefaultOptions()
	if err != nil {
		release()
		return nil, err
	}
	sess, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		release()
		return nil, err
	}
	conn, err := sess.DB().Conn(ctx)
	if err != nil {
		_ = sess.Close() //nolint:errcheck // teardown on the error path
		release()
		return nil, err
	}
	return &session{sess: sess, conn: conn, release: release}, nil
}

// prepare makes the session ready to read targets: it registers the views of
// the DataSources they need, runs each DataSet's setup and returns the SELECT
// that produces each target's rows, keyed by lower-cased name. It must run
// before lock: loading extensions and ATTACH are no longer possible after.
func (s *session) prepare(ctx context.Context, docs []config.Document, ambiguous config.Ambiguities, targets []*config.Document) (map[string]string, error) {
	compiled := map[string]dataset.Compiled{}
	needed := map[string]bool{}
	var texts []string
	for _, t := range targets {
		switch t.Kind {
		case "DataSource":
			needed[t.Name] = true
		case "DataSet":
			c, err := dataset.Compile(*t)
			if err != nil {
				return nil, explain(ambiguous, fmt.Errorf("DataSet %s: %w", t.Name, err))
			}
			compiled[t.Name] = c
			texts = append(texts, c.Query)
			texts = append(texts, c.Setup...)
		}
	}
	// A DataSet names the DataSources it reads only in its SQL; a name that
	// merely appears in it registers one view too many, which is harmless.
	for _, d := range docs {
		if d.Kind != "DataSource" || needed[d.Name] {
			continue
		}
		re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(d.Name) + `($|[^A-Za-z0-9_])`)
		for _, text := range texts {
			if re.MatchString(text) {
				needed[d.Name] = true
				break
			}
		}
	}

	var viewDocs, sources []config.Document
	for _, d := range docs {
		switch {
		case d.Kind != "DataSource":
			viewDocs = append(viewDocs, d)
		case needed[d.Name]:
			viewDocs = append(viewDocs, d)
			sources = append(sources, d)
		}
	}
	scratch, err := s.sess.ScratchDir()
	if err != nil {
		return nil, err
	}
	if len(sources) > 0 {
		if datasource.AccessFor(sources).Remote {
			if err := s.sess.InstallAndLoadExtensions(ctx, []string{"httpfs"}); err != nil {
				return nil, err
			}
		}
		diags, err := datasource.RegisterViews(ctx, s.sess, viewDocs, &datasource.ViewsOptions{TempDir: scratch})
		if err != nil {
			return nil, ambiguous.Explain(err)
		}
		for _, d := range diags {
			if needed[d.Datasource] {
				return nil, ambiguous.Explain(d)
			}
		}
	}

	out := map[string]string{}
	for _, t := range targets {
		key := strings.ToLower(t.Name)
		if t.Kind == "DataSource" {
			out[key] = "SELECT * FROM " + quoteIdent(t.Name)
			continue
		}
		c := compiled[t.Name]
		if c.Prql {
			if err := s.sess.LoadPRQL(ctx); err != nil {
				return nil, err
			}
		}
		if err := dataset.RunSetup(ctx, s.sess, c); err != nil {
			return nil, explain(ambiguous, fmt.Errorf("DataSet %s: %w", t.Name, err))
		}
		out[key] = strings.TrimRight(strings.TrimSpace(c.Query), "; \t\r\n")
	}
	s.access = datasource.AccessFor(sources)
	return out, nil
}

// lock confines the session: it may read only the files and prefixes
// prepare found and its own scratch dir, load no extension, run each
// statement for at most statementTimeout, and use bounded memory, disk and
// threads. The allow-list is set before external access is
// disabled, because after that it can no longer change; lock_configuration
// comes last so nothing can undo the rest.
func (s *session) lock(ctx context.Context) error {
	scratch, err := s.sess.ScratchDir()
	if err != nil {
		return err
	}
	spill := filepath.Join(scratch, "spill")
	dirs := append([]string{filepath.ToSlash(scratch) + "/"}, s.access.Prefixes...)
	if resolved, err := filepath.EvalSymlinks(scratch); err == nil && resolved != scratch {
		dirs = append(dirs, filepath.ToSlash(resolved)+"/")
	}
	stmts := []string{
		"SET GLOBAL temp_directory = " + quoteString(spill),
		"SET GLOBAL max_temp_directory_size = " + quoteString(tempLimit),
		fmt.Sprintf("SET GLOBAL threads = %d", threads),
		"SET GLOBAL allowed_paths = " + quoteList(s.access.Files),
		"SET GLOBAL allowed_directories = " + quoteList(dirs),
		"SET GLOBAL enable_external_access = false",
		"SET GLOBAL autoload_known_extensions = false",
		"SET GLOBAL autoinstall_known_extensions = false",
		fmt.Sprintf("SET GLOBAL max_execution_time = %d", statementTimeout.Milliseconds()),
		"SET GLOBAL memory_limit = " + quoteString(memoryLimit),
		"SET GLOBAL lock_configuration = true",
	}
	for _, stmt := range stmts {
		if _, err := s.conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("lock session (%s): %w", stmt, err)
		}
	}
	return nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteList(items []string) string {
	quoted := make([]string, len(items))
	for i, it := range items {
		quoted[i] = quoteString(it)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func quoteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
