// Command winpack builds the Windows standard-edition (multi-user) package using
// only Windows-native tools: Go, Node.js (npm) and uv. No bash, MinGW or Docker.
//
//	go run ./scripts/windows/winpack check
//	go run ./scripts/windows/winpack server
//	go run ./scripts/windows/winpack -skip-frontend -no-zip server
//	go run ./scripts/windows/winpack run-server      # start the built package in this console
//
// The result in dist/windows/WeKnora-server_<version>_windows_amd64 starts with a
// double-click on WeKnora.exe, which also launches the bundled Qdrant and docreader.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type options struct {
	root       string
	outDir     string
	cacheDir   string
	version    string // from VERSION, without the leading v
	versionTag string // used in package names

	cgo           string // auto | 0 | 1
	duckdbDLL     string
	qdrantSource  string // qdrant.exe or the release zip; empty = look in default places, then download
	qdrantVersion string
	pythonVersion string // embeddable CPython bundled for docreader

	skipFrontend  bool
	skipLicenses  bool
	skipDocreader bool
	skipQdrant    bool
	noZip         bool
}

func main() {
	if err := run(); err != nil {
		logError("%v", err)
		os.Exit(1)
	}
}

func run() error {
	o := &options{}
	flag.StringVar(&o.cgo, "cgo", envOr("CGO", "auto"), "cgo mode: auto (gcc on PATH -> 1), 0 or 1")
	flag.StringVar(&o.duckdbDLL, "duckdb", envOr("DUCKDB_DLL", `C:\tools\libduckdb-windows-amd64\duckdb.dll`), "duckdb.dll shipped next to the exe in pure-Go builds")
	flag.StringVar(&o.qdrantSource, "qdrant", os.Getenv("QDRANT_EXE"), "qdrant.exe or qdrant-x86_64-pc-windows-msvc.zip (default: C:\\tools\\qdrant, then download)")
	flag.StringVar(&o.qdrantVersion, "qdrant-version", envOr("QDRANT_VERSION", "v1.18.0"), "Qdrant release downloaded when no local copy is given")
	flag.StringVar(&o.pythonVersion, "python-version", envOr("PYTHON_EMBED_VERSION", "3.12.10"), "embeddable CPython version bundled for docreader")
	flag.StringVar(&o.versionTag, "version-tag", os.Getenv("VERSION_TAG"), "package version tag (default: v + VERSION file)")
	flag.BoolVar(&o.skipFrontend, "skip-frontend", os.Getenv("SKIP_FRONTEND") == "1", "reuse frontend/dist (or web/) instead of running npm")
	flag.BoolVar(&o.skipLicenses, "skip-licenses", os.Getenv("SKIP_LICENSES") == "1", "leave out licenses and corresponding source (do not distribute the result)")
	flag.BoolVar(&o.skipDocreader, "skip-docreader", false, "do not bundle docreader (PDF/Word parsing then needs an external docreader)")
	flag.BoolVar(&o.skipQdrant, "skip-qdrant", false, "do not bundle Qdrant")
	flag.BoolVar(&o.noZip, "no-zip", false, "only assemble the folder, skip the zip")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: go run ./scripts/windows/winpack [flags] check|server|run-server\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		return errors.New("exactly one target is required")
	}

	root, err := findRepoRoot()
	if err != nil {
		return err
	}
	o.root = root
	o.outDir = filepath.Join(root, "dist", "windows")
	o.cacheDir = filepath.Join(o.outDir, ".cache")
	raw, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		return fmt.Errorf("read VERSION: %w", err)
	}
	o.version = strings.TrimSpace(string(raw))
	if o.versionTag == "" {
		o.versionTag = "v" + o.version
	}
	if o.cgo == "auto" {
		o.cgo = "0"
		if _, err := exec.LookPath("gcc"); err == nil {
			o.cgo = "1"
		}
	}
	if o.cgo != "0" && o.cgo != "1" {
		return fmt.Errorf("-cgo must be auto, 0 or 1, got %q", o.cgo)
	}

	switch target := flag.Arg(0); target {
	case "check":
		return check(o)
	case "server":
		return buildServer(o)
	case "run-server":
		return runServer(o)
	default:
		flag.Usage()
		return fmt.Errorf("unknown target %q", target)
	}
}

func check(o *options) error {
	logInfo("仓库: %s", o.root)
	logInfo("版本: %s，编译模式: CGO_ENABLED=%s", o.versionTag, o.cgo)
	ok := true
	for _, tool := range []struct{ name, why string }{
		{"go", "编译服务端"},
		{"npm", "构建前端（-skip-frontend 时可省略）"},
		{"uv", "打包 docreader 依赖（-skip-docreader 时可省略）"},
	} {
		if path, err := exec.LookPath(tool.name); err == nil {
			logSuccess("%-4s %s", tool.name, path)
		} else {
			logWarn("%-4s 未找到：%s", tool.name, tool.why)
			ok = false
		}
	}
	if o.cgo == "0" {
		if fileExists(o.duckdbDLL) {
			logSuccess("duckdb.dll %s", o.duckdbDLL)
		} else {
			logWarn("duckdb.dll 未找到（%s）：包内数据分析不可用，用 -duckdb 指定", o.duckdbDLL)
		}
	}
	if src := findQdrantSource(o); src != "" {
		logSuccess("qdrant %s", src)
	} else {
		logWarn("本地没有 qdrant.exe，打包时将尝试下载 %s（失败则用 -qdrant 指定）", o.qdrantVersion)
	}
	if !ok {
		return errors.New("缺少必要工具")
	}
	return nil
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			firstLine, _, _ := strings.Cut(string(data), "\n")
			if strings.TrimSpace(firstLine) == "module github.com/Tencent/WeKnora" {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("run winpack inside the WeKnora repository")
		}
		dir = parent
	}
}

// command runs name with args in dir, streaming its output.
func command(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func output(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func logInfo(format string, args ...interface{}) { fmt.Printf("[INFO] "+format+"\n", args...) }
func logWarn(format string, args ...interface{}) { fmt.Printf("[WARN] "+format+"\n", args...) }
func logError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[ERROR] "+format+"\n", args...)
}
func logSuccess(format string, args ...interface{}) { fmt.Printf("[ OK ] "+format+"\n", args...) }
