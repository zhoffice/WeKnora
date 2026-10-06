//go:build !cgo

package sqlite

import (
	"database/sql/driver"
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"gorm.io/gorm"
	msqlite "modernc.org/sqlite"
)

// Pure-Go builds run on modernc.org/sqlite, which cannot load the sqlite-vec
// C extension. Vectors live in an ordinary table instead and KNN is a scan
// ordered by a Go cosine-distance function, which is fine at Lite scale.

func init() {
	if err := msqlite.RegisterDeterministicScalarFunction("vec_distance_cosine", 2, vecDistanceCosine); err != nil {
		panic("register vec_distance_cosine: " + err.Error())
	}
}

// vecDistanceCosine matches sqlite-vec's cosine distance: 1 - cosine similarity.
func vecDistanceCosine(_ *msqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	a, _ := args[0].([]byte)
	b, _ := args[1].([]byte)
	if len(a) != len(b) || len(a)%4 != 0 {
		return nil, fmt.Errorf("vec_distance_cosine: vector sizes differ (%d vs %d bytes)", len(a), len(b))
	}
	var dot, na, nb float64
	for i := 0; i < len(a); i += 4 {
		x := float64(math.Float32frombits(binary.LittleEndian.Uint32(a[i:])))
		y := float64(math.Float32frombits(binary.LittleEndian.Uint32(b[i:])))
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 1.0, nil
	}
	return 1 - dot/(math.Sqrt(na)*math.Sqrt(nb)), nil
}

// vecBackend names the vector implementation in startup logs.
const vecBackend = "pure-Go cosine scan"

// vecTableDDL creates a plain rowid/blob table for dim.
func vecTableDDL(tbl string, dim int) string {
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (rowid INTEGER PRIMARY KEY, embedding BLOB NOT NULL)`,
		tbl,
	)
}

// checkVecTable rejects vec0 tables left by a cgo build: modernc has no vec0
// module, so every query against them would fail.
func checkVecTable(db *gorm.DB, tbl string) error {
	var ddl string
	db.Raw("SELECT sql FROM sqlite_master WHERE name = ?", tbl).Scan(&ddl)
	if strings.Contains(strings.ToLower(ddl), "using vec0") {
		return fmt.Errorf("table %s was created by a cgo (sqlite-vec) build and cannot be read by this pure-Go build; "+
			"use a fresh DB_PATH or the cgo build for this database", tbl)
	}
	return nil
}

// knnQuery returns the table-scan KNN query, split around the row filter.
func knnQuery(tbl string, queryBlob []byte, topK int) (string, []any, string, []any) {
	head := fmt.Sprintf(`
		SELECT v.rowid, vec_distance_cosine(v.embedding, ?) AS distance,%s
		FROM %s v
		JOIN lite_embeddings e ON e.id = v.rowid
		WHERE 1 = 1%s`, knnSelectColumns, tbl, knnFilterHead)
	return head, []any{queryBlob}, ") ORDER BY distance ASC LIMIT ?", []any{topK}
}
