package sqlite

import (
	"encoding/binary"
	"math"
)

// serializeFloat32 encodes a vector as little-endian float32s, the blob format
// both sqlite-vec and the pure-Go cosine function read.
func serializeFloat32(vector []float32) []byte {
	buf := make([]byte, 4*len(vector))
	for i, v := range vector {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// knnSelectColumns is the projection shared by both KNN implementations;
// every query must also expose the distance as "distance".
const knnSelectColumns = `
			e.source_id, e.source_type, e.chunk_id,
			e.knowledge_id, e.knowledge_base_id,
			e.tag_id, e.content`

// knnFilterHead opens the row filter that buildFilterWhere appends to; the
// implementation's tail closes it.
const knnFilterHead = `
		AND v.rowid IN (
			SELECT filtered.id
			FROM lite_embeddings filtered
			WHERE (filtered.is_enabled IS NULL OR filtered.is_enabled = 1)
	`
