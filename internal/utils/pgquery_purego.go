//go:build !cgo

package utils

import (
	pg_query "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
)

// pgParse / pgDeparse run the same libpg_query compiled to WebAssembly, so
// pure-Go builds keep the identical parser and AST types.
func pgParse(sql string) (*pg_query.ParseResult, error) { return pgquery.Parse(sql) }

func pgDeparse(tree *pg_query.ParseResult) (string, error) { return pgquery.Deparse(tree) }
