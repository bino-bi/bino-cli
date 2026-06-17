package duckdb

import (
	"context"
	"database/sql/driver"
	"fmt"

	duckdbdriver "github.com/duckdb/duckdb-go/v2"
)

// signFunc is a scalar UDF that reduces a numeric value to its sign as a string.
//
//	op:  v > 0 -> "+",  v < 0 -> "-",  v == 0 -> ""
//	iop: the inverse (v > 0 -> "-", v < 0 -> "+"), for "inverted" measures where
//	     an increase is unfavorable (costs, headcount, error rates).
//
// Pair either with the built-in abs() to format a signed magnitude, e.g.
//
//	SELECT op(variance) || abs(variance)::VARCHAR;  -- "+500" / "-500"
//
// NULL handling is the DuckDB default: NULL in, NULL out.
type signFunc struct {
	cfg    duckdbdriver.ScalarFuncConfig
	invert bool
}

func (f *signFunc) Config() duckdbdriver.ScalarFuncConfig { return f.cfg }

func (f *signFunc) Executor() duckdbdriver.ScalarFuncExecutor {
	pos, neg := "+", "-"
	if f.invert {
		pos, neg = "-", "+"
	}
	return duckdbdriver.ScalarFuncExecutor{
		RowExecutor: func(values []driver.Value) (any, error) {
			if values[0] == nil {
				return nil, nil
			}
			v, ok := values[0].(float64)
			if !ok {
				return nil, fmt.Errorf("expected a numeric (DOUBLE) input, got %T", values[0])
			}
			switch {
			case v > 0:
				return pos, nil
			case v < 0:
				return neg, nil
			default:
				return "", nil
			}
		},
	}
}

// registerBuiltinUDFs installs bino's built-in scalar UDFs (op, iop) into the
// session's shared in-memory database. The database/sql pool shares a single
// DuckDB instance (one Connector), so registering on one pooled connection makes
// the functions available to every query run through the session.
func (s *Session) registerBuiltinUDFs(ctx context.Context) error {
	doubleInfo, err := duckdbdriver.NewTypeInfo(duckdbdriver.TYPE_DOUBLE)
	if err != nil {
		return fmt.Errorf("build DOUBLE type info: %w", err)
	}
	varcharInfo, err := duckdbdriver.NewTypeInfo(duckdbdriver.TYPE_VARCHAR)
	if err != nil {
		return fmt.Errorf("build VARCHAR type info: %w", err)
	}
	cfg := duckdbdriver.ScalarFuncConfig{
		InputTypeInfos: []duckdbdriver.TypeInfo{doubleInfo},
		ResultTypeInfo: varcharInfo,
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for UDF registration: %w", err)
	}
	defer conn.Close()

	udfs := []struct {
		name string
		fn   duckdbdriver.ScalarFunc
	}{
		{"op", &signFunc{cfg: cfg}},
		{"iop", &signFunc{cfg: cfg, invert: true}},
	}
	for _, u := range udfs {
		if err := duckdbdriver.RegisterScalarUDF(conn, u.name, u.fn); err != nil {
			return fmt.Errorf("register scalar UDF %q: %w", u.name, err)
		}
	}
	return nil
}
