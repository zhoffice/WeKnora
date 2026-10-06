//go:build !cgo

package container

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	_ "modernc.org/sqlite" // pure-Go SQLite registered as "sqlite"
)

// openSQLiteDialector opens Lite's database with modernc.org/sqlite. The gorm
// dialector is the same one cgo builds use (its Name() stays "sqlite"); only
// the database/sql driver underneath changes. Vector search is provided by
// the retriever's vec_purego.go instead of sqlite-vec.
func openSQLiteDialector(dbPath string) gorm.Dialector {
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	return sqlite.New(sqlite.Config{DriverName: "sqlite", DSN: dsn})
}
