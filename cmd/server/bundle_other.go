//go:build !windows

package main

import "os"

// Bundle builds are only produced for Windows; these keep the package building
// elsewhere.

func ownsConsoleWindow() bool { return false }

type processJob struct{}

func newProcessJob() (*processJob, error) { return &processJob{}, nil }

func (j *processJob) assign(*os.Process) error { return nil }
