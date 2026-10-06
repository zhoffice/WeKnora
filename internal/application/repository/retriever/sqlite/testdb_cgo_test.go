//go:build cgo

package sqlite

import (
	"fmt"
	"sync"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var registerSQLiteVec sync.Once

func openSQLiteTestDialector(dbName string) gorm.Dialector {
	registerSQLiteVec.Do(sqlite_vec.Auto)
	return gormsqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName))
}
