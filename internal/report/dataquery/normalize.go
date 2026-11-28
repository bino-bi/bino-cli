package dataquery

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"

	duckdbdrv "github.com/duckdb/duckdb-go/v2"
)

// normalize turns a value scanned from DuckDB into one encoding/json can
// write without losing digits: DECIMAL and HUGEINT become JSON numbers from
// their exact text, NaN and infinity become strings, MAP keys become strings.
func normalize(v any) any {
	switch val := v.(type) {
	case nil:
		return nil
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
