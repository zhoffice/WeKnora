package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
)

// bundledService is a service shipped under services/ that a bundle build starts
// as a child process when .env points the matching address at this machine.
type bundledService struct {
	name string
	exe  string // relative to the bundle directory
	args []string
	dir  string // working directory, relative to the bundle directory
	env  []string
	// readyAddr is polled before the container is built when waitReady is set;
	// otherwise readiness is only logged.
	readyAddr    string
	waitReady    bool
	readyTimeout time.Duration
}

// startBundledServices launches the bundled services this configuration uses.
// WEKNORA_BUNDLED_SERVICES=off leaves them all to the operator.
func startBundledServices(dir string) {
	ctx := context.Background()
	if strings.EqualFold(strings.TrimSpace(os.Getenv("WEKNORA_BUNDLED_SERVICES")), "off") {
		logger.Infof(ctx, "[bundle] WEKNORA_BUNDLED_SERVICES=off, not starting bundled services")
		return
	}
	services := bundledServices(ctx, dir)
	if len(services) == 0 {
		return
	}
	job, err := newProcessJob()
	if err != nil {
		logger.Fatalf(ctx, "[bundle] cannot tie bundled services to this process: %v", err)
	}
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		logger.Fatalf(ctx, "[bundle] create %s: %v", logDir, err)
	}
	for _, svc := range services {
		if err := svc.start(ctx, dir, logDir, job); err != nil {
			logger.Fatalf(ctx, "[bundle] %s: %v", svc.name, err)
		}
	}
}

func bundledServices(ctx context.Context, dir string) []bundledService {
	var services []bundledService
	for _, configure := range []func(string) (bundledService, bool){bundledQdrant, bundledDocreader} {
		svc, ok := configure(dir)
		if !ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, svc.exe)); err != nil {
			logger.Warnf(ctx, "[bundle] %s is not in this package (%s); expecting it to be running already", svc.name, svc.exe)
			continue
		}
		services = append(services, svc)
	}
	return services
}

// bundledQdrant serves RETRIEVE_DRIVER=qdrant when QDRANT_HOST is this machine.
// Storage lives in data/qdrant so the package folder holds all state.
func bundledQdrant(dir string) (bundledService, bool) {
	drivers := strings.Split(os.Getenv("RETRIEVE_DRIVER"), ",")
	if !slices.ContainsFunc(drivers, func(d string) bool { return strings.TrimSpace(d) == "qdrant" }) ||
		!isLoopbackHost(os.Getenv("QDRANT_HOST")) {
		return bundledService{}, false
	}
	grpcPort := envOr("QDRANT_PORT", "6334")
	env := []string{
		"QDRANT__SERVICE__HOST=127.0.0.1",
		"QDRANT__SERVICE__GRPC_PORT=" + grpcPort,
		"QDRANT__SERVICE__HTTP_PORT=" + envOr("QDRANT_REST_PORT", "6333"),
		"QDRANT__STORAGE__STORAGE_PATH=" + filepath.Join(dir, "data", "qdrant", "storage"),
		"QDRANT__STORAGE__SNAPSHOTS_PATH=" + filepath.Join(dir, "data", "qdrant", "snapshots"),
		"QDRANT__TELEMETRY_DISABLED=true",
	}
	if key := os.Getenv("QDRANT_API_KEY"); key != "" {
		env = append(env, "QDRANT__SERVICE__API_KEY="+key)
	}
	return bundledService{
		name: "qdrant",
		exe:  filepath.Join("services", "qdrant", "qdrant.exe"),
		// Qdrant reads config/config.yaml relative to its working directory;
		// running from the bundle root would hand it WeKnora's config.
		dir:          filepath.Join("services", "qdrant"),
		env:          env,
		readyAddr:    net.JoinHostPort("127.0.0.1", grpcPort),
		waitReady:    true,
		readyTimeout: 60 * time.Second,
	}, true
}

// bundledDocreader serves DOCREADER_ADDR on this machine over gRPC, using the
// embedded Python under services/docreader/python. The gRPC client connects
// lazily, so startup does not wait for it.
func bundledDocreader(dir string) (bundledService, bool) {
	transport := strings.ToLower(strings.TrimSpace(os.Getenv("DOCREADER_TRANSPORT")))
	if transport != "" && transport != "grpc" {
		return bundledService{}, false
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(os.Getenv("DOCREADER_ADDR")))
	if err != nil || !isLoopbackHost(host) {
		return bundledService{}, false
	}
	return bundledService{
		name: "docreader",
		exe:  filepath.Join("services", "docreader", "python", "python.exe"),
		args: []string{"-m", "docreader.main"},
		dir:  filepath.Join("services", "docreader", "app"),
		env: []string{
			"DOCREADER_GRPC_PORT=" + port,
			"PYTHONUNBUFFERED=1",
			"PYTHONIOENCODING=utf-8",
		},
		readyAddr:    net.JoinHostPort("127.0.0.1", port),
		readyTimeout: 3 * time.Minute,
	}, true
}

func (svc bundledService) start(ctx context.Context, dir, logDir string, job *processJob) error {
	logPath := filepath.Join(logDir, svc.name+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	fmt.Fprintf(logFile, "\n===== %s started by WeKnora =====\n", time.Now().Format(time.RFC3339))

	cmd := exec.Command(filepath.Join(dir, svc.exe), svc.args...)
	cmd.Dir = filepath.Join(dir, svc.dir)
	cmd.Env = append(os.Environ(), svc.env...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start %s: %w", svc.exe, err)
	}
	if err := job.assign(cmd.Process); err != nil {
		_ = cmd.Process.Kill()
		logFile.Close()
		return err
	}
	logger.Infof(ctx, "[bundle] started %s (pid %d), log: %s", svc.name, cmd.Process.Pid, logPath)

	exited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		logFile.Close()
		exited <- err
		logger.Warnf(ctx, "[bundle] %s exited (%v), see %s", svc.name, err, logPath)
	}()

	ready := make(chan bool, 1)
	go func() { ready <- waitForPort(svc.readyAddr, svc.readyTimeout, exited) }()
	if !svc.waitReady {
		go func() {
			if <-ready {
				logger.Infof(ctx, "[bundle] %s is listening on %s", svc.name, svc.readyAddr)
			}
		}()
		return nil
	}
	if !<-ready {
		return fmt.Errorf("not listening on %s after %s, see %s", svc.readyAddr, svc.readyTimeout, logPath)
	}
	logger.Infof(ctx, "[bundle] %s is listening on %s", svc.name, svc.readyAddr)
	return nil
}

// waitForPort polls addr until it accepts connections, the process exits or the
// timeout passes.
func waitForPort(addr string, timeout time.Duration, exited <-chan error) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			conn.Close()
			return true
		}
		select {
		case <-exited:
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
	return false
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
