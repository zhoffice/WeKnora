package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/joho/godotenv"

	"github.com/Tencent/WeKnora/internal/logger"
)

// bundleLayout is injected by scripts/windows/winpack (-X main.bundleLayout=windows).
// A bundle build runs from its own folder: it reads the .env next to the exe and
// starts the services shipped under services/, so double-clicking WeKnora.exe is
// the whole launch procedure. Docker images and go run leave it empty.
var bundleLayout string

const (
	bundleEnvFile        = ".env"
	bundleEnvExampleFile = ".env.example"
)

// bundleSecretPattern matches secret lines left empty in .env.example.
var bundleSecretPattern = regexp.MustCompile(`(?m)^(JWT_SECRET|SYSTEM_AES_KEY|SYSTEM_SIGNING_KEY)=[ \t]*\r?$`)

// pauseBeforeExit is true when this process owns its console window (started by
// double-click): the window closes as soon as the process exits, so errors would
// vanish unread.
var pauseBeforeExit bool

// enterBundleMode prepares a bundle build before anything reads the environment
// and returns the bundle directory, or "" for ordinary builds.
func enterBundleMode() string {
	if bundleLayout == "" {
		return ""
	}
	pauseBeforeExit = ownsConsoleWindow()
	if pauseBeforeExit {
		logger.SetExitFunc(exitAfterKeypress)
	}

	exe, err := os.Executable()
	if err != nil {
		bundleFatalf("无法定位 WeKnora.exe: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	// config/, migrations/ and web/ are looked up relative to the working directory.
	if err := os.Chdir(dir); err != nil {
		bundleFatalf("无法切换到程序目录 %s: %v", dir, err)
	}

	created, err := ensureBundleEnv(dir)
	if err != nil {
		bundleFatalf("%v", err)
	}
	if created {
		fmt.Printf("已根据 %s 生成 %s，并填入了随机密钥。\n", bundleEnvExampleFile, filepath.Join(dir, bundleEnvFile))
		fmt.Println("请确认其中 PostgreSQL / Redis 的地址与密码后，重新启动 WeKnora.exe。")
		exitAfterKeypressIfOwned(1)
	}

	// Variables already set in the process environment win over .env.
	if err := godotenv.Load(bundleEnvFile); err != nil {
		bundleFatalf("读取 %s 失败: %v", filepath.Join(dir, bundleEnvFile), err)
	}
	logger.ConfigureFromEnv()
	return dir
}

// ensureBundleEnv creates .env from .env.example on first start, generating the
// secrets that must stay stable across restarts: a random JWT secret per start
// logs everyone out, and without an AES key stored API keys stay in plaintext.
func ensureBundleEnv(dir string) (created bool, err error) {
	envPath := filepath.Join(dir, bundleEnvFile)
	if _, err := os.Stat(envPath); err == nil {
		return false, nil
	}
	example, err := os.ReadFile(filepath.Join(dir, bundleEnvExampleFile))
	if err != nil {
		return false, fmt.Errorf("缺少 %s，且无法读取 %s: %v", bundleEnvFile, bundleEnvExampleFile, err)
	}
	var genErr error
	content := bundleSecretPattern.ReplaceAllStringFunc(string(example), func(line string) string {
		name := strings.TrimSpace(strings.SplitN(line, "=", 2)[0])
		value, err := newBundleSecret(name)
		if err != nil {
			genErr = err
			return line
		}
		return name + "=" + value
	})
	if genErr != nil {
		return false, fmt.Errorf("生成密钥失败: %v", genErr)
	}
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		return false, fmt.Errorf("写入 %s 失败: %v", envPath, err)
	}
	return true, nil
}

func newBundleSecret(name string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	if name == "SYSTEM_AES_KEY" {
		// AES-256 wants exactly 32 bytes; 16 random bytes in hex are 32 characters.
		return hex.EncodeToString(buf[:16]), nil
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// pauseOnPanic is deferred by main so a double-clicked window stays open when
// startup panics, which is how the DI container reports an unreachable
// PostgreSQL or Redis.
func pauseOnPanic() {
	if !pauseBeforeExit {
		return
	}
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n\n%s\n", r, debug.Stack())
		fmt.Fprintln(os.Stderr, "常见原因：PostgreSQL / Redis 未启动，或 .env 中的地址、密码不正确。")
		exitAfterKeypress(2)
	}
}

func bundleFatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "启动失败: "+format+"\n", args...)
	exitAfterKeypressIfOwned(1)
}

func exitAfterKeypressIfOwned(code int) {
	if pauseBeforeExit {
		exitAfterKeypress(code)
	}
	os.Exit(code)
}

func exitAfterKeypress(code int) {
	if code != 0 {
		fmt.Fprint(os.Stderr, "\n按回车键关闭窗口...")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	os.Exit(code)
}
