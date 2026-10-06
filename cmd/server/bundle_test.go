package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joho/godotenv"
)

func TestEnsureBundleEnvFillsEmptySecrets(t *testing.T) {
	dir := t.TempDir()
	example := "DB_PASSWORD=postgres\r\nJWT_SECRET=\r\nSYSTEM_AES_KEY=\nSYSTEM_SIGNING_KEY=  \n# JWT_SECRET=\nKEEP=1\n"
	if err := os.WriteFile(filepath.Join(dir, bundleEnvExampleFile), []byte(example), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := ensureBundleEnv(dir)
	if err != nil || !created {
		t.Fatalf("ensureBundleEnv() = %v, %v; want created", created, err)
	}
	env, err := godotenv.Read(filepath.Join(dir, bundleEnvFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(env["SYSTEM_AES_KEY"]); got != 32 {
		t.Errorf("SYSTEM_AES_KEY has %d chars, want 32", got)
	}
	for _, name := range []string{"JWT_SECRET", "SYSTEM_SIGNING_KEY"} {
		if len(env[name]) < 32 {
			t.Errorf("%s = %q, want a generated secret", name, env[name])
		}
	}
	if env["DB_PASSWORD"] != "postgres" || env["KEEP"] != "1" {
		t.Errorf("other values changed: %v", env)
	}

	// An existing .env is never rewritten: regenerated secrets would log everyone
	// out and make stored API keys undecryptable.
	before, _ := os.ReadFile(filepath.Join(dir, bundleEnvFile))
	created, err = ensureBundleEnv(dir)
	if err != nil || created {
		t.Fatalf("second ensureBundleEnv() = %v, %v; want untouched", created, err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, bundleEnvFile))
	if string(before) != string(after) {
		t.Error(".env was rewritten")
	}
}

func TestEnsureBundleEnvMissingExample(t *testing.T) {
	if _, err := ensureBundleEnv(t.TempDir()); err == nil {
		t.Fatal("want an error without .env and .env.example")
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"": true, "localhost": true, "127.0.0.1": true, "::1": true,
		"192.168.1.100": false, "qdrant": false,
	} {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}
