//go:build cgo

package sqlite

import (
	"fmt"

	"gorm.io/gorm"
)

// vecBackend names the vector implementation in startup logs.
const vecBackend = "sqlite-vec"

// vecTableDDL creates a sqlite-vec vec0 virtual table for dim.
func vecTableDDL(tbl string, dim int) string {
	return fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(embedding float[%d] distance_metric=cosine)`,
		tbl, dim,
	)
}

// checkVecTable has nothing to check: vec0 tables are native here.
func checkVecTable(*gorm.DB, string) error { return nil }

// knnQuery returns the sqlite-vec KNN query, split around the row filter.
func knnQuery(tbl string, queryBlob []byte, topK int) (string, []any, string, []any) {
	// ⚠️ sqlite-vec 要求必须有 k = ?
	head := fmt.Sprintf(`
		SELECT v.rowid, v.distance,%s
		FROM %s v
		JOIN lite_embeddings e ON e.id = v.rowid
		WHERE v.embedding MATCH ?
		AND k = ?%s`, knnSelectColumns, tbl, knnFilterHead)
	// ⚠️ 这里仍然建议加 ORDER BY，虽然 vec0 已经按距离返回
	return head, []any{queryBlob, topK}, ") ORDER BY v.distance ASC", nil
}
