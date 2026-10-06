package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const handlerPkg = "github.com/Tencent/WeKnora/internal/handler"

func buildServer(o *options) error {
	name := serverPackageName(o)
	stage := filepath.Join(o.outDir, name)
	if err := os.RemoveAll(stage); err != nil {
		if running := processesUnder(stage); len(running) > 0 {
			return fmt.Errorf("无法清理 %s：以下进程正在使用该目录，请先结束它们（任务管理器或 taskkill /PID <pid>）后重试:\n  %s",
				stage, strings.Join(running, "\n  "))
		}
		return fmt.Errorf("clean %s: %w", stage, err)
	}
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}

	if err := stageFrontend(o, filepath.Join(stage, "web")); err != nil {
		return err
	}
	if err := goBuildServer(o, filepath.Join(stage, "WeKnora.exe")); err != nil {
		return err
	}

	logInfo("组装发行目录...")
	for _, dir := range []string{"config", filepath.Join("migrations", "versioned"), filepath.Join("dataset", "samples")} {
		if err := copyDir(filepath.Join(o.root, dir), filepath.Join(stage, dir), nil); err != nil {
			return err
		}
	}
	templates := filepath.Join(o.root, "scripts", "windows", "templates")
	if err := copyFile(filepath.Join(templates, "server.env.example"), filepath.Join(stage, ".env.example")); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(templates, "server-README.md"), filepath.Join(stage, "README.md")); err != nil {
		return err
	}
	if o.cgo == "0" {
		if fileExists(o.duckdbDLL) {
			if err := copyFile(o.duckdbDLL, filepath.Join(stage, "duckdb.dll")); err != nil {
				return err
			}
		} else {
			logWarn("未找到 %s，包内不含 duckdb.dll：CSV/Excel 数据分析不可用（用 -duckdb 指定）", o.duckdbDLL)
		}
	}
	if o.skipLicenses {
		logWarn("-skip-licenses：包内不含许可证与对应源码，不要对外分发")
	} else if err := copyLicenses(o, stage); err != nil {
		return err
	}

	servicesDir := filepath.Join(stage, "services")
	if o.skipQdrant {
		logWarn("-skip-qdrant：包内不含 Qdrant，需在 .env 中指向已有实例")
	} else if err := stageQdrant(o, filepath.Join(servicesDir, "qdrant")); err != nil {
		return err
	}
	if o.skipDocreader {
		logWarn("-skip-docreader：包内不含 docreader，PDF/Word 等解析需要外部 docreader 或云端解析引擎")
	} else if err := stageDocreader(o, filepath.Join(servicesDir, "docreader")); err != nil {
		return err
	}

	logSuccess("dist/windows/%s", name)
	if o.noZip {
		return nil
	}
	logInfo("压缩 %s.zip ...", name)
	if err := zipDir(o.outDir, name); err != nil {
		return err
	}
	logSuccess("dist/windows/%s.zip", name)
	return nil
}

// stageFrontend builds the SPA and places it where the server serves it (./web).
func stageFrontend(o *options, dest string) error {
	dist := filepath.Join(o.root, "frontend", "dist")
	if o.skipFrontend {
		for _, candidate := range []string{dist, filepath.Join(o.root, "web")} {
			if fileExists(filepath.Join(candidate, "index.html")) {
				logInfo("复用已构建的前端 %s", candidate)
				return copyDir(candidate, dest, nil)
			}
		}
		return fmt.Errorf("-skip-frontend 但 frontend/dist 与 web/ 中都没有 index.html")
	}
	logInfo("构建前端...")
	env := os.Environ()
	if os.Getenv("VITE_FRONTEND_COMMIT") == "" {
		env = append(env, "VITE_FRONTEND_COMMIT="+gitCommit(o))
	}
	if os.Getenv("NODE_OPTIONS") == "" {
		// Vite often exceeds Node's default heap on this SPA; same as frontend/Dockerfile.
		env = append(env, "NODE_OPTIONS=--max-old-space-size=4096")
	}
	frontend := filepath.Join(o.root, "frontend")
	if err := command(frontend, env, "npm", "ci", "--prefer-offline"); err != nil {
		return err
	}
	if err := command(frontend, env, "npm", "run", "build"); err != nil {
		return err
	}
	return copyDir(dist, dest, nil)
}

func goBuildServer(o *options, exePath string) error {
	logInfo("构建标准版 WeKnora.exe（CGO_ENABLED=%s）...", o.cgo)
	goVersion, err := output(o.root, "go", "version")
	if err != nil {
		goVersion = "unknown"
	}
	vars := []struct{ name, value string }{
		{handlerPkg + ".Version", o.version},
		{handlerPkg + ".Edition", "standard"},
		{handlerPkg + ".CommitID", gitCommit(o)},
		{handlerPkg + ".BuildTime", time.Now().UTC().Format("2006-01-02 15:04:05 UTC")},
		{handlerPkg + ".GoVersion", goVersion},
		// Milvus and Qdrant generated code both register "common.proto".
		{"google.golang.org/protobuf/reflect/protoregistry.conflictPolicy", "warn"},
		// Turns on bundle mode: .env next to the exe, bundled services under services/.
		{"main.bundleLayout", "windows"},
	}
	ldflags := []string{"-w", "-s"}
	for _, v := range vars {
		ldflags = append(ldflags, fmt.Sprintf("-X '%s=%s'", v.name, v.value))
	}

	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "CGO_LDFLAGS=") {
			env = append(env, kv)
		}
	}
	env = append(env, "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED="+o.cgo,
		"GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn")
	if o.cgo == "1" {
		env = append(env, "CGO_CFLAGS=-Wno-deprecated-declarations")
	}
	return command(o.root, env, "go", "build", "-ldflags="+strings.Join(ldflags, " "), "-o", exePath, "./cmd/server")
}

// gitCommit is best effort: the package must build without git too.
func gitCommit(o *options) string {
	if sha := os.Getenv("GITHUB_SHA"); len(sha) >= 7 {
		return sha[:7]
	}
	if commit, err := output(o.root, "git", "rev-parse", "--short", "HEAD"); err == nil && commit != "" {
		return commit
	}
	return "unknown"
}
