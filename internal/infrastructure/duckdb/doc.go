// Package duckdb registers the "duckdb" database/sql driver used by the data
// analysis tool, picking an implementation per build:
//
//   - cgo builds: github.com/duckdb/duckdb-go, statically linked (driver_cgo.go)
//   - Windows pure-Go builds: duckdb.dll loaded at runtime through syscall,
//     so no C toolchain is needed (driver_windows.go)
//   - other pure-Go builds: a stub whose connections report DuckDB as
//     unavailable, so the server still starts (driver_stub.go)
//
// Import it for its side effect:
//
//	import _ "github.com/Tencent/WeKnora/internal/infrastructure/duckdb"
package duckdb
