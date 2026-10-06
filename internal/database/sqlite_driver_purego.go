//go:build !cgo

package database

import _ "modernc.org/sqlite" // pure-Go SQLite registered as "sqlite"

// sqliteDriverName is modernc.org/sqlite; golang-migrate only needs the *sql.DB.
const sqliteDriverName = "sqlite"

// sqliteMigrationDSN adds the busy timeout mattn applies by default; modernc
// would otherwise fail immediately while gorm holds the database.
func sqliteMigrationDSN(dbPath string) string {
	return dbPath + "?_pragma=busy_timeout(5000)"
}
