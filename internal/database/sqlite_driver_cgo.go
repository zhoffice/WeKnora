//go:build cgo

package database

// sqliteDriverName is mattn/go-sqlite3, registered by golang-migrate's sqlite3 driver.
const sqliteDriverName = "sqlite3"

// sqliteMigrationDSN keeps mattn's defaults (it already waits 5s on a busy database).
func sqliteMigrationDSN(dbPath string) string { return dbPath }
