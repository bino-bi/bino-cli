package datasource

import (
	"context"
	"errors"
	"fmt"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/pkg/duckdb"
)

// DropAmbiguousViews drops the view of every ambiguous DataSource name on a
// long-lived session, so it cannot keep serving a definition that an earlier
// run registered. A nil session is a no-op.
func DropAmbiguousViews(ctx context.Context, session *duckdb.Session, ambiguous config.Ambiguities) error {
	if session == nil {
		return nil
	}
	var errs []error
	for _, a := range ambiguous {
		if a.Kind != "DataSource" {
			continue
		}
		if _, err := session.DB().ExecContext(ctx, fmt.Sprintf("DROP VIEW IF EXISTS %q", a.Name)); err != nil {
			errs = append(errs, fmt.Errorf("drop view %s: %w", a.Name, err))
		}
	}
	return errors.Join(errs...)
}
