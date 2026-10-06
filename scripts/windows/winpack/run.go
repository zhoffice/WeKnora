package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func serverPackageName(o *options) string {
	return fmt.Sprintf("WeKnora-server_%s_windows_amd64", o.versionTag)
}

// runServer starts the packaged WeKnora.exe in the foreground, for IDE run
// configurations. The exe is tied to this process so stopping winpack (the
// IDE's stop button) also stops the server and its bundled services.
func runServer(o *options) error {
	exe := filepath.Join(o.outDir, serverPackageName(o), "WeKnora.exe")
	if !fileExists(exe) {
		return fmt.Errorf("未找到 %s，先运行: go run ./scripts/windows/winpack server", exe)
	}
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := killWithThisProcess(cmd.Process); err != nil {
		logWarn("无法与 WeKnora.exe 绑定生命周期（%v），停止 winpack 后需手动结束它", err)
	}
	return cmd.Wait()
}
