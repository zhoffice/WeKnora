//go:build !cgo

package duckdb

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// openTestDB opens an in-memory database through duckdb.dll, skipping when the
// DLL is not available (set DUCKDB_DLL_PATH to run these tests).
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	if err := loadDLL(); err != nil {
		t.Skipf("duckdb.dll not available: %v", err)
	}
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestDLLDriverValueTypes(t *testing.T) {
	db := openTestDB(t)

	var (
		i    int64
		f    float64
		s    string
		b    bool
		null any
		big  string
	)
	err := db.QueryRow(`SELECT 42::INTEGER, 2.5::DOUBLE, '中文', true, NULL, 18446744073709551615::UBIGINT`).
		Scan(&i, &f, &s, &b, &null, &big)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if i != 42 || f != 2.5 || s != "中文" || !b || null != nil || big != "18446744073709551615" {
		t.Fatalf("got %v %v %q %v %v %q", i, f, s, b, null, big)
	}
}

func TestDLLDriverSharesDatabaseAcrossConnections(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	c1, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	c2, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()

	if _, err := c1.ExecContext(ctx, `CREATE TABLE t AS SELECT range AS n FROM range(5)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	var count int64
	if err := c2.QueryRowContext(ctx, `SELECT count(*) FROM t`).Scan(&count); err != nil {
		t.Fatalf("query from second connection: %v", err)
	}
	if count != 5 {
		t.Fatalf("count = %d, want 5", count)
	}
}

func TestDLLDriverRowsAffectedAndErrors(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.Exec(`CREATE TABLE t (n INTEGER)`); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO t VALUES (1), (2), (3)`)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 3 {
		t.Fatalf("rows affected = %d, want 3", n)
	}

	_, err = db.Query(`SELECT * FROM missing_table`)
	if err == nil || !strings.Contains(err.Error(), "missing_table") {
		t.Fatalf("expected catalog error naming the table, got %v", err)
	}

	if _, err := db.Exec(`SELECT ?`, 1); err != errArgs {
		t.Fatalf("expected errArgs for bind parameters, got %v", err)
	}
}

func TestDLLDriverContextCancelInterruptsQuery(t *testing.T) {
	db := openTestDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := db.ExecContext(ctx, `SELECT count(*) FROM range(10000000000) a, range(1000) b`)
	if err == nil {
		t.Fatal("expected the long query to be interrupted")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("interrupt took %v", elapsed)
	}
}
