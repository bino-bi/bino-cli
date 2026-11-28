package datasource

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"bino.bi/bino/internal/report/config"
)

// CSVOptions are the CSV reader options a DataSource declares. They mirror
// the csv fields of a DataSource spec.
type CSVOptions struct {
	Delimiter        string
	Header           *bool
	SkipRows         int
	Thousands        string
	DecimalSeparator string
	ColumnNames      []string
	DateFormat       string
	Columns          map[string]string
}

// CSVIssue is one problem InspectCSV found in a CSV file. Option is the
// DataSource spec field it concerns ("delimiter", "header", "dateFormat"),
// or empty when it concerns the file as a whole.
type CSVIssue struct {
	Option  string `json:"option,omitempty"`
	Message string `json:"message"`
}

// CSVReport is what InspectCSV detected in a CSV file.
type CSVReport struct {
	// Delimiter, HasHeader and DateFormat are what DuckDB's sniffer detects
	// without any declared option. DateFormat is empty when no column is a date.
	Delimiter  string `json:"delimiter"`
	HasHeader  bool   `json:"hasHeader"`
	DateFormat string `json:"dateFormat,omitempty"`
	// Columns are the columns as the declared options read them; nil when
	// the file cannot be read with them.
	Columns []ProbeColumn `json:"columns"`
	Issues  []CSVIssue    `json:"issues,omitempty"`
}

// csvSampleRows bounds the rows the header checks read.
const csvSampleRows = 1000

var paddedColumnName = regexp.MustCompile(`^column\d+$`)

// LocalCSV returns the local file and the declared reader options of a csv
// DataSource. ok is false for other source types, URLs, globs, directories
// and files that do not exist.
func LocalCSV(doc config.Document) (path string, declared CSVOptions, ok bool) {
	if doc.Kind != "DataSource" {
		return "", CSVOptions{}, false
	}
	spec, err := parseSpec(doc.Raw)
	if err != nil || spec.Type != sourceTypeCSV {
		return "", CSVOptions{}, false
	}
	spec.BaseDir = filepath.Dir(doc.File)
	path, ok = localFilePath(spec)
	if !ok {
		return "", CSVOptions{}, false
	}
	return path, CSVOptions{
		Delimiter:        spec.Delimiter,
		Header:           spec.Header,
		SkipRows:         spec.SkipRows,
		Thousands:        spec.Thousands,
		DecimalSeparator: spec.DecimalSeparator,
		ColumnNames:      spec.ColumnNames,
		DateFormat:       spec.DateFormat,
		Columns:          spec.Columns,
	}, true
}

// InspectCSV sniffs a local CSV file with DuckDB and compares what it finds
// with the declared reader options: a missing header row, a header row read
// as data, a delimiter other than the declared one, and dates the declared
// dateFormat does not parse. The error is only for a file DuckDB cannot sniff
// at all; problems with the declared options are issues.
func InspectCSV(ctx context.Context, db *sql.DB, path string, declared CSVOptions) (CSVReport, error) {
	var rep CSVReport
	quoted := escapeSQLString(path)

	var dateFormat sql.NullString
	row := db.QueryRowContext(ctx, fmt.Sprintf("SELECT Delimiter, HasHeader, DateFormat FROM sniff_csv('%s')", quoted)) //nolint:gosec // G201: path is SQL-string-escaped
	if err := row.Scan(&rep.Delimiter, &rep.HasHeader, &dateFormat); err != nil {
		return rep, fmt.Errorf("sniff %s: %w", path, err)
	}
	rep.DateFormat = dateFormat.String
	sniffed, err := queryColumns(ctx, db, fmt.Sprintf("SELECT * FROM read_csv('%s') LIMIT 0", quoted)) //nolint:gosec // G201: path is SQL-string-escaped
	if err != nil {
		return rep, fmt.Errorf("sniff %s: %w", path, err)
	}

	delimiterWrong := false
	if d := unescapeDelimiter(declared.Delimiter); d != "" && d != rep.Delimiter {
		delimiterWrong = true
		rep.Issues = append(rep.Issues, CSVIssue{Option: "delimiter", Message: fmt.Sprintf(
			"the file is delimited by %s, but delimiter is %s", showDelimiter(rep.Delimiter), showDelimiter(d))})
	}

	source := declaredCSVSource(quoted, declared)
	cols, err := queryColumns(ctx, db, source+" LIMIT 0")
	if err != nil {
		if ctx.Err() != nil {
			return rep, ctx.Err()
		}
		if !delimiterWrong {
			rep.Issues = append(rep.Issues, CSVIssue{Message: "the declared options cannot read the file: " + firstLine(err.Error())})
		}
	} else {
		rep.Columns = cols
		if len(declared.ColumnNames) == 0 {
			issues, err := headerIssues(ctx, db, quoted, declared, source, cols)
			if err != nil {
				return rep, err
			}
			rep.Issues = append(rep.Issues, issues...)
		}
	}

	if declared.DateFormat != "" && declared.DateFormat != rep.DateFormat {
		issue, err := dateFormatIssue(ctx, db, quoted, declared.DateFormat, rep.DateFormat, sniffed)
		if err != nil {
			return rep, err
		}
		if issue != nil {
			rep.Issues = append(rep.Issues, *issue)
		}
	}
	return rep, nil
}

// declaredCSVSource is the SELECT the build runs for the file.
func declaredCSVSource(quotedPath string, declared CSVOptions) string {
	params := buildCSVParams(sourceSpec{
		Delimiter:        declared.Delimiter,
		Header:           declared.Header,
		SkipRows:         declared.SkipRows,
		Thousands:        declared.Thousands,
		DecimalSeparator: declared.DecimalSeparator,
		ColumnNames:      declared.ColumnNames,
		DateFormat:       declared.DateFormat,
		Columns:          declared.Columns,
	})
	if params == "" {
		return fmt.Sprintf("SELECT * FROM read_csv_auto('%s')", quotedPath)
	}
	return fmt.Sprintf("SELECT * FROM read_csv('%s', %s)", quotedPath, params)
}

// headerIssues looks for a header row that is missing, read as data, or
// really a data row.
func headerIssues(ctx context.Context, db *sql.DB, quotedPath string, declared CSVOptions, source string, cols []ProbeColumn) ([]CSVIssue, error) {
	if len(cols) == 0 {
		return nil, nil
	}
	allVarchar := true
	for _, c := range cols {
		if c.Type != "VARCHAR" {
			allVarchar = false
			break
		}
	}
	if allVarchar {
		read, err := firstRowIsHeader(ctx, db, quotedPath, declared)
		if err != nil {
			return nil, err
		}
		if read {
			return []CSVIssue{{Option: "header", Message: "every column is VARCHAR, but the values below the first row are numbers or dates: " +
				"the first row looks like a header that is read as data (set header: true)"}}, nil
		}
	}

	if paddedColumnName.MatchString(cols[0].Name) {
		if declared.Header != nil && !*declared.Header {
			return nil, nil
		}
		return []CSVIssue{{Option: "header", Message: fmt.Sprintf(
			"the file has no header row, so DuckDB names the columns %s; add a header row or set columnNames", columnList(cols))}}, nil
	}

	checks := make([]string, len(cols))
	for i, c := range cols {
		checks[i] = fmt.Sprintf("coalesce(bool_or(CAST(%s AS VARCHAR) = '%s'), false)", quoteIdent(c.Name), escapeSQLString(c.Name))
	}
	query := fmt.Sprintf("SELECT %s FROM (%s LIMIT %d)", strings.Join(checks, ", "), source, csvSampleRows) //nolint:gosec // G201: identifiers are quoted, values escaped
	found := make([]bool, len(cols))
	dest := make([]any, len(cols))
	for i := range found {
		dest[i] = &found[i]
	}
	if err := db.QueryRowContext(ctx, query).Scan(dest...); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil //nolint:nilerr // a column that does not cast is no header evidence
	}
	var dup []string
	for i, f := range found {
		if f {
			dup = append(dup, fmt.Sprintf("%q", cols[i].Name))
		}
	}
	if len(dup) == 0 {
		return nil, nil
	}
	return []CSVIssue{{Option: "header", Message: fmt.Sprintf(
		"header value(s) %s also occur as data in their column: the first row may be data, not a header (set header: false and columnNames)",
		strings.Join(dup, ", "))}}, nil
}

// firstRowIsHeader reports whether some column has a text first row and only
// numbers or dates below it, read with the declared delimiter and skip.
func firstRowIsHeader(ctx context.Context, db *sql.DB, quotedPath string, declared CSVOptions) (bool, error) {
	params := []string{"header = false", "all_varchar = true"}
	if declared.Delimiter != "" {
		params = append(params, fmt.Sprintf("delim = '%s'", escapeSQLString(declared.Delimiter)))
	}
	if declared.SkipRows > 0 {
		params = append(params, fmt.Sprintf("skip = %d", declared.SkipRows))
	}
	source := fmt.Sprintf("SELECT * FROM read_csv('%s', %s) LIMIT %d", quotedPath, strings.Join(params, ", "), csvSampleRows) //nolint:gosec // G201: path and options are SQL-string-escaped
	rows, err := db.QueryContext(ctx, source)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, nil //nolint:nilerr // an unreadable file is reported by the declared read
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return false, err
	}
	values := make([]sql.NullString, len(names))
	dest := make([]any, len(names))
	for i := range values {
		dest[i] = &values[i]
	}
	first := make([]string, len(names))
	typed := make([]int, len(names)) // rows below the first that look typed; -1 once one does not
	rowIdx := 0
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return false, err
		}
		for i, v := range values {
			switch {
			case rowIdx == 0:
				first[i] = v.String
			case !v.Valid || typed[i] < 0:
			case looksTyped(v.String):
				typed[i]++
			default:
				typed[i] = -1
			}
		}
		rowIdx++
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	for i := range names {
		if typed[i] > 0 && first[i] != "" && !looksTyped(first[i]) {
			return true, nil
		}
	}
	return false, nil
}

var (
	numberLike = regexp.MustCompile(`^[-+]?(?:\d+[.,]?\d*|[.,]\d+)(?:[eE][-+]?\d+)?$`)
	dateLike   = regexp.MustCompile(`^\d{1,4}[-./]\d{1,2}[-./]\d{1,4}(?:[ T]\d{1,2}:\d{2}(?::\d{2})?)?$`)
)

func looksTyped(v string) bool {
	v = strings.TrimSpace(v)
	return numberLike.MatchString(v) || dateLike.MatchString(v)
}

// dateFormatIssue finds a value in a column the sniffer reads as a date that
// the declared dateFormat does not parse.
func dateFormatIssue(ctx context.Context, db *sql.DB, quotedPath, declared, detected string, sniffed []ProbeColumn) (*CSVIssue, error) {
	var dateCols []string
	for _, c := range sniffed {
		if c.Type == "DATE" {
			dateCols = append(dateCols, c.Name)
		}
	}
	for _, col := range dateCols {
		q := quoteIdent(col)
		query := fmt.Sprintf("SELECT %s FROM read_csv('%s', all_varchar = true) WHERE %s IS NOT NULL AND try_strptime(%s, '%s') IS NULL LIMIT 1", //nolint:gosec // G201: identifiers are quoted, values escaped
			q, quotedPath, q, q, escapeSQLString(declared))
		var value string
		err := db.QueryRowContext(ctx, query).Scan(&value)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		msg := fmt.Sprintf("dateFormat %q does not parse %q in column %s", declared, value, col)
		if detected != "" {
			msg += fmt.Sprintf(" (the dates look like %s)", detected)
		}
		return &CSVIssue{Option: "dateFormat", Message: msg}, nil
	}
	return nil, nil
}

func queryColumns(ctx context.Context, db *sql.DB, query string) ([]ProbeColumn, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	cols := make([]ProbeColumn, len(types))
	for i, ct := range types {
		cols[i] = ProbeColumn{Name: ct.Name(), Type: ct.DatabaseTypeName()}
	}
	return cols, nil
}

// unescapeDelimiter accepts the two-character "\t" spelling of a tab.
func unescapeDelimiter(d string) string {
	if d == `\t` {
		return "\t"
	}
	return d
}

func showDelimiter(d string) string {
	if d == "\t" {
		return `"\t"`
	}
	return fmt.Sprintf("%q", d)
}

func columnList(cols []ProbeColumn) string {
	names := make([]string, 0, 3)
	for i, c := range cols {
		if i == 2 {
			names = append(names, "...")
			break
		}
		names = append(names, c.Name)
	}
	return strings.Join(names, ", ")
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
