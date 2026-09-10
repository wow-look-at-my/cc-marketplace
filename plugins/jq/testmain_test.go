package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Every behavior test here drives a real jq. A runner without one used to
// skip the whole suite, which left the coverage gate reading the skip as
// untested code. So the binary is bootstrapped from a pinned release
// instead, mirroring the glob and grep plugins' ripgrep bootstrap, and a
// failure to obtain it fails the run rather than quietly passing nothing.
const (
	bootstrapJqVersion = "1.8.1"
	bootstrapAttempts  = 3
)

// jqPath is the binary every behavior test drives, resolved once in TestMain.
var jqPath string

func TestMain(m *testing.M) {
	path, err := ensureJq()
	if err != nil {
		fmt.Fprintf(os.Stderr, "jq bootstrap failed: %v\n", err)
		os.Exit(1)
	}
	jqPath = path
	os.Exit(m.Run())
}

func ensureJq() (string, error) {
	if path, err := exec.LookPath("jq"); err == nil {
		return path, nil
	}
	dir, err := bootstrapCacheDir()
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "jq")
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	if err := downloadJq(bin); err != nil {
		return "", err
	}
	return bin, nil
}

func bootstrapCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "cc-jq-plugin", "jq-"+bootstrapJqVersion)
	return dir, os.MkdirAll(dir, 0o755)
}

// jqReleaseAsset names the single static binary jq publishes per platform.
func jqReleaseAsset() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return "jq-linux-amd64", nil
	case "linux/arm64":
		return "jq-linux-arm64", nil
	case "darwin/amd64":
		return "jq-macos-amd64", nil
	case "darwin/arm64":
		return "jq-macos-arm64", nil
	}
	return "", fmt.Errorf("no pinned jq build for %s/%s", runtime.GOOS, runtime.GOARCH)
}

func downloadJq(dest string) error {
	asset, err := jqReleaseAsset()
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://github.com/jqlang/jq/releases/download/jq-%s/%s",
		bootstrapJqVersion, asset)
	var lastErr error
	for attempt := 1; attempt <= bootstrapAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		fmt.Fprintf(os.Stderr, "jq not on PATH; downloading %s (attempt %d)\n", url, attempt)
		if lastErr = fetchBinary(url, dest); lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func fetchBinary(url, dest string) error {
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
