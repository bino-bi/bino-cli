package explorer

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
)

func TestNewSessionAndClose(t *testing.T) {
	ctx := context.Background()
	sess, err := NewSession(ctx, logx.Nop())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	// Verify the session is functional
	err = sess.WithDB(func(db *sql.DB) error {
		return db.PingContext(ctx)
	})
	if err != nil {
		t.Fatalf("WithDB ping: %v", err)
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestRefreshRegistersViews(t *testing.T) {
	ctx := context.Background()
	sess, err := NewSession(ctx, logx.Nop())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	raw := json.RawMessage(`{
		"apiVersion": "bino.bi/v1beta1",
		"kind": "DataSource",
		"metadata": {"name": "test_source"},
		"spec": {
			"type": "inline",
			"content": [
				{"id": 1, "name": "Alice"},
				{"id": 2, "name": "Bob"}
			]
		}
	}`)
	docs := []config.Document{
		{Kind: "DataSource", Name: "test_source", Raw: raw},
	}

	if err := sess.Refresh(ctx, docs); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Verify we can query the view
	var count int
	err = sess.WithDB(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "test_source"`).Scan(&count)
	})
	if err != nil {
		t.Fatalf("query test_source: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 rows, got %d", count)
	}

	// Verify docs are stored
	storedDocs := sess.Docs()
	if len(storedDocs) != 1 {
		t.Errorf("expected 1 doc, got %d", len(storedDocs))
	}
}

func TestRefreshDropsPreviousViews(t *testing.T) {
	ctx := context.Background()
	sess, err := NewSession(ctx, logx.Nop())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	rawA := json.RawMessage(`{
		"apiVersion": "bino.bi/v1beta1",
		"kind": "DataSource",
		"metadata": {"name": "source_a"},
		"spec": {
			"type": "inline",
			"content": [{"x": 1}]
		}
	}`)
	if err := sess.Refresh(ctx, []config.Document{
		{Kind: "DataSource", Name: "source_a", Raw: rawA},
	}); err != nil {
		t.Fatalf("Refresh 1: %v", err)
	}

	// Second refresh with source_b (should drop source_a)
	rawB := json.RawMessage(`{
		"apiVersion": "bino.bi/v1beta1",
		"kind": "DataSource",
		"metadata": {"name": "source_b"},
		"spec": {
			"type": "inline",
			"content": [{"y": 2}]
		}
	}`)
	if err := sess.Refresh(ctx, []config.Document{
		{Kind: "DataSource", Name: "source_b", Raw: rawB},
	}); err != nil {
		t.Fatalf("Refresh 2: %v", err)
	}

	// source_a should be gone
	err = sess.WithDB(func(db *sql.DB) error {
		_, err := db.QueryContext(ctx, `SELECT * FROM "source_a"`)
		return err
	})
	if err == nil {
		t.Error("expected error querying dropped source_a")
	}

	// source_b should exist
	var count int
	err = sess.WithDB(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "source_b"`).Scan(&count)
	})
	if err != nil {
		t.Fatalf("query source_b: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row in source_b, got %d", count)
	}
}

func TestWithDBConcurrent(t *testing.T) {
	ctx := context.Background()
	sess, err := NewSession(ctx, logx.Nop())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	done := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() {
			done <- sess.WithDB(func(db *sql.DB) error {
				return db.PingContext(ctx)
			})
		}()
	}

	for i := 0; i < 5; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent WithDB failed: %v", err)
		}
	}
}
