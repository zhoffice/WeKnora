package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// Bundled services live under services/; cmd/server/bundle_services.go starts
// them from the same paths.

const qdrantAsset = "qdrant-x86_64-pc-windows-msvc.zip"

// findQdrantSource returns the local qdrant.exe / release zip to bundle, or ""
// when it has to be downloaded.
func findQdrantSource(o *options) string {
	if o.qdrantSource != "" {
		return o.qdrantSource
	}
	for _, candidate := range []string{
		`C:\tools\qdrant\qdrant.exe`,
		`C:\tools\qdrant-x86_64-pc-windows-msvc\qdrant.exe`,
		`C:\tools\` + qdrantAsset,
	} {
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func stageQdrant(o *options, dest string) error {
	src := findQdrantSource(o)
	if src == "" {
		url := fmt.Sprintf("https://github.com/qdrant/qdrant/releases/download/%s/%s", o.qdrantVersion, qdrantAsset)
		archive, err := download(o, url, "qdrant-"+o.qdrantVersion+"-windows.zip")
		if err != nil {
			return fmt.Errorf("下载 Qdrant 失败（%v）。请手动下载 %s，用 -qdrant 指定 zip 或解压出的 qdrant.exe；不需要时加 -skip-qdrant", err, url)
		}
		src = archive
	}
	logInfo("打包 Qdrant: %s", src)
	if !strings.EqualFold(filepath.Ext(src), ".zip") {
		return copyFile(src, filepath.Join(dest, "qdrant.exe"))
	}
	if err := unzip(src, dest, func(name string) string {
		if strings.EqualFold(path.Base(name), "qdrant.exe") {
			return "qdrant.exe"
		}
		return ""
	}); err != nil {
		return err
	}
	if !fileExists(filepath.Join(dest, "qdrant.exe")) {
		return fmt.Errorf("%s 中没有 qdrant.exe", src)
	}
	return nil
}

// stageDocreader bundles the Python docreader with the official embeddable
// CPython, so the package runs it without any Python installed:
//
//	services/docreader/python/  python.exe + Lib/site-packages (deps from uv.lock)
//	services/docreader/app/     the docreader package, run as `python -m docreader.main`
func stageDocreader(o *options, dest string) error {
	if _, err := exec.LookPath("uv"); err != nil {
		return errors.New("打包 docreader 需要 uv（https://docs.astral.sh/uv/）；不需要时加 -skip-docreader")
	}
	parts := strings.Split(o.pythonVersion, ".")
	if len(parts) != 3 {
		return fmt.Errorf("-python-version 需要完整版本号，如 3.12.10，得到 %q", o.pythonVersion)
	}
	minor := parts[0] + "." + parts[1]
	tag := parts[0] + parts[1]
	pyDir := filepath.Join(dest, "python")
	appDir := filepath.Join(dest, "app")

	url := fmt.Sprintf("https://www.python.org/ftp/python/%s/python-%s-embed-amd64.zip", o.pythonVersion, o.pythonVersion)
	archive, err := download(o, url, path.Base(url))
	if err != nil {
		return fmt.Errorf("下载嵌入式 Python 失败: %w", err)
	}
	logInfo("解压嵌入式 Python %s ...", o.pythonVersion)
	if err := unzip(archive, pyDir, nil); err != nil {
		return err
	}
	// The ._pth file fixes sys.path for the embeddable build (PYTHONPATH is
	// ignored): stdlib zip, site-packages, and ../app for the docreader package.
	pth := filepath.Join(pyDir, "python"+tag+"._pth")
	if !fileExists(pth) {
		return fmt.Errorf("嵌入式 Python 中没有 %s", filepath.Base(pth))
	}
	pthContent := strings.Join([]string{"python" + tag + ".zip", ".", `Lib\site-packages`, `..\app`, "import site", ""}, "\r\n")
	if err := os.WriteFile(pth, []byte(pthContent), 0o644); err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "winpack-docreader-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	requirements := filepath.Join(tmp, "requirements.txt")
	logInfo("按 docreader/uv.lock 安装依赖（首次需要联网）...")
	// uv export echoes the requirements to stdout as well; only errors matter here.
	export := exec.Command("uv", "export", "--project", "docreader", "--frozen", "--no-dev",
		"--no-hashes", "--no-emit-project", "--no-header", "--quiet", "-o", requirements)
	export.Dir = o.root
	if out, err := export.CombinedOutput(); err != nil {
		return fmt.Errorf("uv export: %v\n%s", err, out)
	}
	// --python only supplies an interpreter for building sdists; wheels are
	// chosen for the target platform/version, which match the embedded Python.
	if err := command(o.root, os.Environ(), "uv", "pip", "install",
		"--target", filepath.Join(pyDir, "Lib", "site-packages"),
		"--python", minor, "--python-version", minor, "--python-platform", "x86_64-pc-windows-msvc",
		"--link-mode", "copy", "-r", requirements); err != nil {
		return err
	}

	if err := copyDir(filepath.Join(o.root, "docreader"), filepath.Join(appDir, "docreader"), skipDocreaderSource); err != nil {
		return err
	}

	logInfo("校验 docreader 能在嵌入式 Python 中导入...")
	check := exec.Command(filepath.Join(pyDir, "python.exe"), "-c", "import docreader.main")
	check.Dir = appDir
	if out, err := check.CombinedOutput(); err != nil {
		return fmt.Errorf("嵌入式 Python 导入 docreader 失败: %v\n%s", err, out)
	}
	return nil
}

func skipDocreaderSource(rel string, d fs.DirEntry) bool {
	if d.IsDir() {
		switch d.Name() {
		case ".venv", "__pycache__", "tests", "testdata", ".pytest_cache", ".ruff_cache", ".mypy_cache":
			return true
		}
		return false
	}
	return strings.HasSuffix(rel, ".pyc")
}
