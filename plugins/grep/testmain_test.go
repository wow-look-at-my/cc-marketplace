package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The behavior tests exercise a real ripgrep. CI runners may not ship
// one, so TestMain bootstraps a pinned release binary into a cache dir
// and prepends it to PATH (mirroring the sibling glob plugin's
// bootstrap). On machines that already have rg this is a no-op.
const (
	bootstrapRgVersion = "14.1.0"
	bootstrapAttempts  = 3
)

func TestMain(m *testing.M) {
	if err := ensureRipgrepOnPath(); err != nil {
		fmt.Fprintf(os.Stderr, "ripgrep bootstrap failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func ensureRipgrepOnPath() error {
	if _, err := exec.LookPath("rg"); err == nil {
		return nil
	}
	dir, err := bootstrapCacheDir()
	if err != nil {
		return err
	}
	bin := filepath.Join(dir, "rg")
	if _, err := os.Stat(bin); err != nil {
		if err := downloadRipgrep(bin); err != nil {
			return err
		}
	}
	return os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func bootstrapCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "cc-grep-plugin", "ripgrep-"+bootstrapRgVersion)
	return dir, os.MkdirAll(dir, 0o755)
}

func ripgrepReleaseTriple() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return "x86_64-unknown-linux-musl", nil
	case "linux/arm64":
		return "aarch64-unknown-linux-gnu", nil
	case "darwin/amd64":
		return "x86_64-apple-darwin", nil
	case "darwin/arm64":
		return "aarch64-apple-darwin", nil
	}
	return "", fmt.Errorf("no pinned ripgrep build for %s/%s", runtime.GOOS, runtime.GOARCH)
}

func downloadRipgrep(dest string) error {
	triple, err := ripgrepReleaseTriple()
	if err != nil {
		return err
	}
	name := fmt.Sprintf("ripgrep-%s-%s", bootstrapRgVersion, triple)
	url := fmt.Sprintf("https://github.com/BurntSushi/ripgrep/releases/download/%s/%s.tar.gz",
		bootstrapRgVersion, name)
	var lastErr error
	for attempt := 1; attempt <= bootstrapAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		fmt.Fprintf(os.Stderr, "rg not on PATH; downloading %s (attempt %d)\n", url, attempt)
		if lastErr = fetchTarMember(url, name+"/rg", dest); lastErr == nil {
			return nil
		}
	}
	return lastErr
}

// fetchTarMember downloads a .tar.gz and extracts one member to dest.
func fetchTarMember(url, member, dest string) error {
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("member %s not found in %s", member, url)
		}
		if err != nil {
			return err
		}
		if strings.TrimPrefix(hdr.Name, "./") != member {
			continue
		}
		tmp := dest + ".tmp"
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return os.Rename(tmp, dest)
	}
}
