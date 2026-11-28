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
	// maxResultBytes caps the JSON size of all cells of a query_data result.
	maxResultBytes = 2 << 20
	cutMark        = "...[truncated]"
)

// capCell returns v, or a string holding its first maxCellBytes bytes ending
// in cutMark when v is larger, with its encoded size and whether it was cut.
// A cut list, map or struct becomes the start of its JSON text.
func capCell(v any) (out any, size int, cut bool) {
	if str, ok := v.(string); ok {
		if len(str) <= maxCellBytes {
			return str, len(str) + 2, false
		}
		return cutString(str), maxCellBytes + len(cutMark) + 2, true
	}
	b, err := json.Marshal(v)
	if err != nil || len(b) <= maxCellBytes {
		return v, len(b), false
	}
	return cutString(string(b)), maxCellBytes + len(cutMark) + 2, true
}

// cutString keeps the first maxCellBytes bytes of s, not splitting a rune.
func cutString(s string) string {
	n := maxCellBytes
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + cutMark
}
