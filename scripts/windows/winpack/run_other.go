//go:build !windows

package main

import "os"

func killWithThisProcess(*os.Process) error { return nil }

func processesUnder(string) []string { return nil }
