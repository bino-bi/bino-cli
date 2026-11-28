package dataquery

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	duckdbdrv "github.com/duckdb/duckdb-go/v2"
)

// normalizeTyped is normalize for a value of a column of DuckDB type typ:
// a DATE becomes "2006-01-02" and a TIME "15:04:05.999999".
func normalizeTyped(v any, typ string) any {
	if t, ok := v.(time.Time); ok {
		switch {
		case typ == "DATE":
			return t.Format(time.DateOnly)
		case strings.HasPrefix(typ, "TIME") && !strings.HasPrefix(typ, "TIMESTAMP"):
			return t.Format("15:04:05.999999")
		}
	}
	return normalize(v)
}

// normalize turns a value scanned from DuckDB into one encoding/json can
// write without losing digits: DECIMAL and HUGEINT become JSON numbers from
// their exact text, NaN and infinity become strings, MAP keys become strings.
// A time becomes RFC 3339 text, which also covers years encoding/json
// refuses (DuckDB's infinity is a time far beyond year 9999).
func normalize(v any) any {
	switch val := v.(type) {
	case nil:
		return nil
	case time.Time:
		return val.Format(time.RFC3339Nano)
	case []byte:
		return string(val)
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return fmt.Sprint(val)
		}
		return val
	case float32:
		return normalize(float64(val))
	case *big.Int:
		return json.Number(val.String())
	case duckdbdrv.Decimal:
		return json.Number(val.String())
	case duckdbdrv.UUID:
		return val.String()
	case *duckdbdrv.UUID:
		return val.String()
	case duckdbdrv.Bit:
		return val.String()
	case duckdbdrv.Union:
		return normalize(val.Value)
	case duckdbdrv.Map:
		out := make(map[string]any, len(val))
		for k, x := range val {
			out[fmt.Sprint(normalize(k))] = normalize(x)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, x := range val {
			out[k] = normalize(x)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, x := range val {
			out[i] = normalize(x)
		}
		return out
	default:
		return val
	}
}

const (
	// maxCellBytes caps the JSON size of one cell.
	maxCellBytes = 4 << 10
	// maxColumns caps the columns of a query_data result.
	maxColumns = 200
	cutMark    = "...[truncated]"
)

// maxResultBytes caps the JSON size of all cells of a query_data result. A
// var so tests can lower it.
var maxResultBytes = 2 << 20

// capCell returns v, or a string holding its start ending in cutMark when
// its JSON encoding is longer than maxCellBytes, with the encoded size of
// what it returns and whether it was cut. A cut list, map or struct becomes
// the start of its JSON text.
func capCell(v any) (out any, size int, cut bool) {
	str, isString := v.(string)
	if !isString {
		b, err := json.Marshal(v)
		if err != nil || len(b) <= maxCellBytes {
			return v, len(b), false
		}
		str = string(b)
	}
	head, n, cut := cutJSON(str, maxCellBytes)
	if !cut && isString {
		return str, n, false
	}
	head += cutMark
	return head, n + len(cutMark), true
}

// cutJSON returns the longest start of s whose JSON string encoding (quotes
// included) fits in limit bytes, that encoded length, and whether s was
// cut. It never cuts inside a rune or an escape.
func cutJSON(s string, limit int) (head string, size int, cut bool) {
	size = 2
	for i := 0; i < len(s); {
		r, w := utf8.DecodeRuneInString(s[i:])
		n := jsonRuneLen(r, w)
		if size+n > limit {
			return s[:i], size, true
		}
		size += n
		i += w
	}
	return s, size, false
}

// jsonRuneLen is how many bytes encoding/json writes for the rune r that
// took w bytes in the input: it escapes quotes, backslashes, control
// characters, <, >, &, U+2028 and U+2029, and writes each invalid byte as
// U+FFFD.
func jsonRuneLen(r rune, w int) int {
	switch {
	case r == utf8.RuneError && w == 1:
		return utf8.RuneLen(utf8.RuneError)
	case r == '"' || r == '\\' || r == '\n' || r == '\r' || r == '\t' || r == '\b' || r == '\f':
		return 2
	case r < 0x20 || r == '<' || r == '>' || r == '&' || r == '\u2028' || r == '\u2029':
		return 6
	default:
		return w
	}
}
