package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Same checks and output as scripts/copy-licenses.sh + check-license-bundle.sh.

// A retained build-graph dependency would bring the GPL converter back even if
// application code no longer imported it directly.
var removedConverterDeps = regexp.MustCompile(`github.com/(longbridgeapp/opencc|liuzl/(da|cedar-go)|adamzy/cedar-go)(\s|$)`)

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

var requiredLicenseFiles = []string{
	"LICENSE", "THIRD_PARTY_NOTICES.md", "licenses/OpenCC-Apache-2.0.txt",
	"licenses/go-sql-driver-mysql-MPL-2.0.txt", "licenses/go-m1cpu-MPL-2.0.txt", "licenses/Wails-MIT.txt",
}

func copyLicenses(o *options, dest string) error {
	logInfo("收集许可证与对应源码...")
	for _, file := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(o.root, file))
		if err != nil {
			return err
		}
		if removedConverterDeps.Match(data) {
			return fmt.Errorf("removed conversion dependencies have reappeared in %s", file)
		}
	}
	for _, file := range requiredLicenseFiles {
		if info, err := os.Stat(filepath.Join(o.root, file)); err != nil || info.Size() == 0 {
			return fmt.Errorf("license file %s is missing or empty", file)
		}
	}
	if err := copyFile(filepath.Join(o.root, "LICENSE"), filepath.Join(dest, "LICENSE")); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(o.root, "THIRD_PARTY_NOTICES.md"), filepath.Join(dest, "THIRD_PARTY_NOTICES.md")); err != nil {
		return err
	}
	if err := copyDir(filepath.Join(o.root, "licenses"), filepath.Join(dest, "licenses"), nil); err != nil {
		return err
	}

	modules, err := os.ReadFile(filepath.Join(o.root, "licenses", "sources", "modules.tsv"))
	if err != nil || len(strings.TrimSpace(string(modules))) == 0 {
		return fmt.Errorf("licenses/sources/modules.tsv is missing or empty")
	}
	gomod, err := os.ReadFile(filepath.Join(o.root, "go.mod"))
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(modules)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 || !sha256Hex.MatchString(fields[2]) {
			return fmt.Errorf("invalid source checksum line in modules.tsv: %q", scanner.Text())
		}
		module, version, checksum := fields[0], fields[1], fields[2]
		if actual := requiredVersion(string(gomod), module); actual != version {
			return fmt.Errorf("update the source manifest for %s: expected %s, got %s", module, version, actual)
		}
		archive, err := moduleZip(o, module, version)
		if err != nil {
			return err
		}
		sum, err := sha256File(archive)
		if err != nil {
			return err
		}
		if sum != checksum {
			return fmt.Errorf("checksum mismatch for %s@%s: %s", module, version, sum)
		}
		name := fmt.Sprintf("%s-%s.zip", path.Base(module), version)
		if err := copyFile(archive, filepath.Join(dest, "licenses", "sources", name)); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// requiredVersion finds module's version among go.mod require lines.
func requiredVersion(gomod, module string) string {
	for _, line := range strings.Split(gomod, "\n") {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "require "))
		if len(fields) >= 2 && fields[0] == module {
			return fields[1]
		}
	}
	return ""
}

// moduleZip downloads module@version through the configured GOPROXY / module
// cache and returns the cached zip.
func moduleZip(o *options, module, version string) (string, error) {
	cmd := exec.Command("go", "mod", "download", "-json", module+"@"+version)
	cmd.Dir = o.root
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go mod download %s@%s: %w", module, version, err)
	}
	var info struct{ Zip, Error string }
	if err := json.Unmarshal(out, &info); err != nil {
		return "", fmt.Errorf("go mod download %s@%s: %w", module, version, err)
	}
	if info.Error != "" || info.Zip == "" {
		return "", fmt.Errorf("go mod download %s@%s: %s", module, version, info.Error)
	}
	return info.Zip, nil
}
