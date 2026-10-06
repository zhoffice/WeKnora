//go:build !cgo

package duckdb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"unsafe"
)

// A minimal database/sql driver over the DuckDB C API in duckdb.dll, called
// through syscall so pure-Go Windows builds need no C toolchain. It covers what
// WeKnora uses: plain SQL text (no bind parameters), row reads and simple
// transactions. All pooled connections of one sql.DB share a single DuckDB
// database, so tables created on one connection are visible on the others.
//
// DLL lookup order: $DUCKDB_DLL_PATH, duckdb.dll next to the executable, then
// duckdb.dll in the working directory.

// duckdb_result from duckdb.h (48 bytes); only DuckDB reads its fields.
type cResult struct {
	columnCount, rowCount, rowsChanged uint64
	columns, errorMessage, internal    uintptr
}

// duckdb_type values from duckdb.h that get a native Go type; the rest are
// read as VARCHAR.
const (
	typeBoolean   = 1
	typeTinyint   = 2
	typeSmallint  = 3
	typeInteger   = 4
	typeBigint    = 5
	typeUtinyint  = 6
	typeUsmallint = 7
	typeUinteger  = 8
	typeFloat     = 10
	typeDouble    = 11
)

var (
	loadOnce sync.Once
	loadErr  error
	api      struct {
		open, close, connect, disconnect, interrupt *syscall.Proc
		query, destroyResult, resultError           *syscall.Proc
		columnCount, rowCount, rowsChanged          *syscall.Proc
		columnName, columnType                      *syscall.Proc
		valueIsNull, valueBoolean, valueInt64       *syscall.Proc
		valueVarchar, free                          *syscall.Proc
	}
)

func dllCandidates() []string {
	if p := os.Getenv("DUCKDB_DLL_PATH"); p != "" {
		return []string{p}
	}
	var paths []string
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), "duckdb.dll"))
	}
	if wd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(wd, "duckdb.dll"))
	}
	return paths
}

func loadDLL() error {
	loadOnce.Do(func() {
		var dll *syscall.DLL
		candidates := dllCandidates()
		for _, p := range candidates {
			if dll, loadErr = syscall.LoadDLL(p); loadErr == nil {
				break
			}
		}
		if dll == nil {
			loadErr = fmt.Errorf("duckdb: cannot load duckdb.dll (tried %v; set DUCKDB_DLL_PATH): %w", candidates, loadErr)
			return
		}
		procs := map[string]**syscall.Proc{
			"duckdb_open":           &api.open,
			"duckdb_close":          &api.close,
			"duckdb_connect":        &api.connect,
			"duckdb_disconnect":     &api.disconnect,
			"duckdb_interrupt":      &api.interrupt,
			"duckdb_query":          &api.query,
			"duckdb_destroy_result": &api.destroyResult,
			"duckdb_result_error":   &api.resultError,
			"duckdb_column_count":   &api.columnCount,
			"duckdb_row_count":      &api.rowCount,
			"duckdb_rows_changed":   &api.rowsChanged,
			"duckdb_column_name":    &api.columnName,
			"duckdb_column_type":    &api.columnType,
			"duckdb_value_is_null":  &api.valueIsNull,
			"duckdb_value_boolean":  &api.valueBoolean,
			"duckdb_value_int64":    &api.valueInt64,
			"duckdb_value_varchar":  &api.valueVarchar,
			"duckdb_free":           &api.free,
		}
		for name, dst := range procs {
			p, err := dll.FindProc(name)
			if err != nil {
				loadErr = fmt.Errorf("duckdb: %s does not export %s (needs DuckDB 1.x): %w", dll.Name, name, err)
				return
			}
			*dst = p
		}
	})
	return loadErr
}

func cString(s string) (*byte, error) {
	return syscall.BytePtrFromString(s)
}

// goString copies a NUL-terminated C string owned by DuckDB (C heap, never
// moved by the Go GC) into a Go string.
func goString(p uintptr) string {
	if p == 0 {
		return ""
	}
	start := unsafe.Add(nil, p)
	n := 0
	for *(*byte)(unsafe.Add(start, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(start), n))
}

// --- driver / connector ---

type dllDriver struct{}

func (dllDriver) Open(name string) (driver.Conn, error) {
	c, err := dllDriver{}.OpenConnector(name)
	if err != nil {
		return nil, err
	}
	return c.Connect(context.Background())
}

func (dllDriver) OpenConnector(name string) (driver.Connector, error) {
	return &connector{path: name}, nil
}

// connector owns the DuckDB database shared by every connection of a sql.DB.
type connector struct {
	path string
	mu   sync.Mutex
	db   uintptr
}

func (c *connector) Driver() driver.Driver { return dllDriver{} }

func (c *connector) database() (uintptr, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db != 0 {
		return c.db, nil
	}
	if err := loadDLL(); err != nil {
		return 0, err
	}
	path, err := cString(c.path)
	if err != nil {
		return 0, err
	}
	var db uintptr
	if rc, _, _ := api.open.Call(uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&db))); rc != 0 {
		return 0, fmt.Errorf("duckdb: failed to open database %q", c.path)
	}
	c.db = db
	return db, nil
}

func (c *connector) Connect(context.Context) (driver.Conn, error) {
	db, err := c.database()
	if err != nil {
		return nil, err
	}
	var handle uintptr
	if rc, _, _ := api.connect.Call(db, uintptr(unsafe.Pointer(&handle))); rc != 0 {
		return nil, errors.New("duckdb: failed to open connection")
	}
	return &conn{handle: handle}, nil
}

// Close is called by sql.DB.Close once every connection is closed.
func (c *connector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db != 0 {
		api.close.Call(uintptr(unsafe.Pointer(&c.db)))
		c.db = 0
	}
	return nil
}

// --- connection ---

type conn struct {
	handle uintptr
}

var errArgs = errors.New("duckdb: bind parameters are not supported by the duckdb.dll driver")

// run executes query and returns its result, interrupting DuckDB if ctx ends.
func (c *conn) run(ctx context.Context, query string) (*cResult, error) {
	q, err := cString(query)
	if err != nil {
		return nil, err
	}
	res := new(cResult)

	done := make(chan struct{})
	defer close(done)
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				api.interrupt.Call(c.handle)
			case <-done:
			}
		}()
	}

	rc, _, _ := api.query.Call(c.handle, uintptr(unsafe.Pointer(q)), uintptr(unsafe.Pointer(res)))
	runtime.KeepAlive(q)
	if rc != 0 {
		msg, _, _ := api.resultError.Call(uintptr(unsafe.Pointer(res)))
		err := errors.New(goString(msg))
		api.destroyResult.Call(uintptr(unsafe.Pointer(res)))
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	return res, nil
}

func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if len(args) > 0 {
		return nil, errArgs
	}
	res, err := c.run(ctx, query)
	if err != nil {
		return nil, err
	}
	defer api.destroyResult.Call(uintptr(unsafe.Pointer(res)))
	n, _, _ := api.rowsChanged.Call(uintptr(unsafe.Pointer(res)))
	return driver.RowsAffected(int64(n)), nil
}

func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) > 0 {
		return nil, errArgs
	}
	res, err := c.run(ctx, query)
	if err != nil {
		return nil, err
	}
	return newRows(res), nil
}

func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return &stmt{conn: c, query: query}, nil
}

func (c *conn) Begin() (driver.Tx, error) {
	if _, err := c.ExecContext(context.Background(), "BEGIN TRANSACTION", nil); err != nil {
		return nil, err
	}
	return &tx{conn: c}, nil
}

func (c *conn) Close() error {
	if c.handle != 0 {
		api.disconnect.Call(uintptr(unsafe.Pointer(&c.handle)))
		c.handle = 0
	}
	return nil
}

type tx struct{ conn *conn }

func (t *tx) Commit() error {
	_, err := t.conn.ExecContext(context.Background(), "COMMIT", nil)
	return err
}

func (t *tx) Rollback() error {
	_, err := t.conn.ExecContext(context.Background(), "ROLLBACK", nil)
	return err
}

// stmt only replays the SQL text; database/sql uses it for Prepare callers.
type stmt struct {
	conn  *conn
	query string
}

func (s *stmt) Close() error  { return nil }
func (s *stmt) NumInput() int { return -1 }

func (s *stmt) Exec(args []driver.Value) (driver.Result, error) {
	if len(args) > 0 {
		return nil, errArgs
	}
	return s.conn.ExecContext(context.Background(), s.query, nil)
}

func (s *stmt) Query(args []driver.Value) (driver.Rows, error) {
	if len(args) > 0 {
		return nil, errArgs
	}
	return s.conn.QueryContext(context.Background(), s.query, nil)
}

// --- rows ---

type rows struct {
	res     *cResult
	names   []string
	types   []uintptr
	rowNum  uintptr
	numRows uintptr
}

func newRows(res *cResult) *rows {
	r := &rows{res: res}
	p := uintptr(unsafe.Pointer(res))
	cols, _, _ := api.columnCount.Call(p)
	r.numRows, _, _ = api.rowCount.Call(p)
	r.names = make([]string, cols)
	r.types = make([]uintptr, cols)
	for i := uintptr(0); i < cols; i++ {
		name, _, _ := api.columnName.Call(p, i)
		r.names[i] = goString(name)
		r.types[i], _, _ = api.columnType.Call(p, i)
	}
	return r
}

func (r *rows) Columns() []string { return r.names }

func (r *rows) Close() error {
	if r.res != nil {
		api.destroyResult.Call(uintptr(unsafe.Pointer(r.res)))
		r.res = nil
	}
	return nil
}

func (r *rows) Next(dest []driver.Value) error {
	if r.res == nil || r.rowNum >= r.numRows {
		return io.EOF
	}
	p := uintptr(unsafe.Pointer(r.res))
	row := r.rowNum
	for col := range dest {
		c := uintptr(col)
		if isNull, _, _ := api.valueIsNull.Call(p, c, row); byte(isNull) != 0 {
			dest[col] = nil
			continue
		}
		switch r.types[col] {
		case typeBoolean:
			v, _, _ := api.valueBoolean.Call(p, c, row)
			dest[col] = byte(v) != 0
		case typeTinyint, typeSmallint, typeInteger, typeBigint,
			typeUtinyint, typeUsmallint, typeUinteger:
			v, _, _ := api.valueInt64.Call(p, c, row)
			dest[col] = int64(v)
		case typeFloat, typeDouble:
			// syscall only returns RAX, so doubles go through their text form.
			s := r.varchar(c, row)
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				dest[col] = f
			} else {
				dest[col] = s
			}
		default:
			dest[col] = r.varchar(c, row)
		}
	}
	r.rowNum++
	return nil
}

func (r *rows) varchar(col, row uintptr) string {
	v, _, _ := api.valueVarchar.Call(uintptr(unsafe.Pointer(r.res)), col, row)
	if v == 0 {
		return ""
	}
	s := goString(v)
	api.free.Call(v)
	return s
}

func init() {
	sql.Register("duckdb", dllDriver{})
}
