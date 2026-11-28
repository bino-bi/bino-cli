package dataquery

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

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
