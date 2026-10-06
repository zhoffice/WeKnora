//go:build cgo

package utils

import pg_query "github.com/pganalyze/pg_query_go/v6"

// pgParse / pgDeparse use libpg_query linked through cgo.
func pgParse(sql string) (*pg_query.ParseResult, error) { return pg_query.Parse(sql) }

func pgDeparse(tree *pg_query.ParseResult) (string, error) { return pg_query.Deparse(tree) }
