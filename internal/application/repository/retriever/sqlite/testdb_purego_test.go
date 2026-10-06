//go:build !cgo

package sqlite

import (
	"fmt"

	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// openSQLiteTestDialector runs the suite on modernc.org/sqlite (registered by
// vec_purego.go), exercising the table-scan KNN instead of sqlite-vec.
func openSQLiteTestDialector(dbName string) gorm.Dialector {
	return gormsqlite.New(gormsqlite.Config{
		DriverName: "sqlite",
		DSN:        fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
	})
}
