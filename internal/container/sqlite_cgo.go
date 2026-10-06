//go:build cgo

package container

import (
	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// openSQLiteDialector opens Lite's database with mattn/go-sqlite3 and the
// sqlite-vec extension linked in through cgo.
func openSQLiteDialector(dbPath string) gorm.Dialector {
	sqlite_vec.Auto()
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"
	return sqlite.Open(dsn)
}
