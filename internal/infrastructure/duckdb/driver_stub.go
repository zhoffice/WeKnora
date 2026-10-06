//go:build !cgo && !windows

package duckdb

import (
	"database/sql"
	"database/sql/driver"
	"errors"
)

var errUnavailable = errors.New("duckdb: not available in this pure-Go build; build with CGO_ENABLED=1 to enable data analysis")

type unavailableDriver struct{}

func (unavailableDriver) Open(string) (driver.Conn, error) { return nil, errUnavailable }

func init() {
	sql.Register("duckdb", unavailableDriver{})
}
