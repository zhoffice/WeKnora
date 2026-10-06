package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// copyDir copies src into dst. skip, when set, is asked about every entry by
// its slash-separated path relative to src; returning true leaves it out (and
// for a directory, everything below it).
func copyDir(src, dst string, skip func(rel string, d fs.DirEntry) bool) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel != "." && skip != nil && skip(filepath.ToSlash(rel), d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy %s: %w", src, err)
	}
	return out.Close()
}

// download fetches url into the cache as cacheName once and returns the cached path.
func download(o *options, url, cacheName string) (string, error) {
	path := filepath.Join(o.cacheDir, cacheName)
	if fileExists(path) {
		logInfo("使用缓存 %s", path)
		return path, nil
	}
	if err := os.MkdirAll(o.cacheDir, 0o755); err != nil {
		return "", err
	}
	logInfo("下载 %s ...", url)
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	tmp := path + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp, path)
}

// unzip extracts archive into dest. keep, when set, filters entries by name and
// may rename them; returning "" drops the entry.
func unzip(archive, dest string, keep func(name string) string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		name := f.Name
		if keep != nil {
			if name = keep(name); name == "" {
				continue
			}
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("%s: entry %q escapes the destination", archive, f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := extractZipFile(f, target); err != nil {
			return fmt.Errorf("%s: %w", archive, err)
		}
	}
	return nil
}

func extractZipFile(f *zip.File, target string) error {
	in, err := f.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// zipDir writes <parent>/<name>.zip with the folder itself as the top-level
// entry, plus a sha256sum-style checksum file.
func zipDir(parent, name string) error {
	zipPath := filepath.Join(parent, name+".zip")
	out, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	walkErr := filepath.WalkDir(filepath.Join(parent, name), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(parent, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			header.Name += "/"
			_, err = zw.CreateHeader(header)
			return err
		}
		header.Method = zip.Deflate
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(w, in)
		return err
	})
	if err := zw.Close(); walkErr == nil {
		walkErr = err
	}
	if err := out.Close(); walkErr == nil {
		walkErr = err
	}
	if walkErr != nil {
		os.Remove(zipPath)
		return walkErr
	}
	sum, err := sha256File(zipPath)
	if err != nil {
		return err
	}
	return os.WriteFile(zipPath+".sha256", []byte(fmt.Sprintf("%s  %s\n", sum, name+".zip")), 0o644)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
