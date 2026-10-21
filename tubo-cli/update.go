package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	latestReleaseAPI = "https://api.github.com/repos/endlessite/tubo/releases/latest"
	releaseDownload  = "https://github.com/endlessite/tubo/releases/download"
	updateCheckEvery = 24 * time.Hour
)

// newerVersion reports whether version a (e.g. "v0.3.0" or "0.3.0") is newer than b.
func newerVersion(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func versionParts(v string) [3]int {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	v, _, _ = strings.Cut(v, "-")
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

func latestVersion(timeout time.Duration) (string, error) {
	client := http.Client{Timeout: timeout}
	resp, err := client.Get(latestReleaseAPI)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	return release.TagName, nil
}

// checkForUpdate prints a one-line notice when a newer release exists.
// It asks GitHub at most once a day and never blocks for more than 2 seconds.
// Set TUBO_NO_UPDATE_CHECK=1 to disable it.
func checkForUpdate() {
	if version == "dev" || os.Getenv("TUBO_NO_UPDATE_CHECK") != "" {
		return
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return
	}
	stamp := filepath.Join(cacheDir, "tubo", "latest-version")

	latest := ""
	if info, err := os.Stat(stamp); err == nil && time.Since(info.ModTime()) < updateCheckEvery {
		data, _ := os.ReadFile(stamp)
		latest = strings.TrimSpace(string(data))
	} else {
		latest, err = latestVersion(2 * time.Second)
		if err != nil {
			return
		}
		os.MkdirAll(filepath.Dir(stamp), 0755)
		os.WriteFile(stamp, []byte(latest), 0644)
	}

	if latest != "" && newerVersion(latest, version) {
		fmt.Fprintf(os.Stderr, "A new version of tubo is available (%s, you have %s). Run: tubo update\n\n", latest, version)
	}
}

func runUpdate() {
	fmt.Fprintf(os.Stderr, "Checking for updates...\n")
	latest, err := latestVersion(15 * time.Second)
	if err != nil {
		fatal("Could not check the latest version: ", err)
	}
	if version != "dev" && !newerVersion(latest, version) {
		fmt.Fprintf(os.Stderr, "tubo %s is already the latest version.\n", version)
		return
	}

	exe, err := os.Executable()
	if err != nil {
		fatal("Could not find the tubo executable: ", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	asset := fmt.Sprintf("tubo-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}

	expected, err := releaseChecksum(latest, asset)
	if err != nil {
		fatal("Could not read the release checksums: ", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(exe), ".tubo-update-*")
	if err != nil {
		fatal(fmt.Sprintf("Cannot write to %s (%v). Reinstall with: curl -sL https://tubo.endlessite.com/get | sh", filepath.Dir(exe), err), nil)
	}
	defer os.Remove(tmp.Name())

	fmt.Fprintf(os.Stderr, "Downloading tubo %s...\n", latest)
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Get(fmt.Sprintf("%s/%s/%s", releaseDownload, latest, asset))
	if err != nil {
		fatal("Download failed: ", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fatal("Download failed: ", fmt.Errorf("%s", resp.Status))
	}

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), resp.Body); err != nil {
		tmp.Close()
		fatal("Download failed: ", err)
	}
	tmp.Close()

	if hex.EncodeToString(hasher.Sum(nil)) != expected {
		fatal("Checksum mismatch, the download was not installed.", nil)
	}
	if err := os.Chmod(tmp.Name(), 0755); err != nil {
		fatal("Update failed: ", err)
	}

	// A running executable can't be overwritten on Windows, but it can be renamed.
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			fatal("Update failed: ", err)
		}
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		fatal("Update failed: ", err)
	}

	cacheDir, _ := os.UserCacheDir()
	os.Remove(filepath.Join(cacheDir, "tubo", "latest-version"))
	fmt.Fprintf(os.Stderr, "Updated to tubo %s (checksum verified).\n", latest)
}

func releaseChecksum(tag, asset string) (string, error) {
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Get(fmt.Sprintf("%s/%s/checksums.txt", releaseDownload, tag))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s", resp.Status)
	}
	return findChecksum(resp.Body, asset)
}

// findChecksum reads a sha256sum-style file ("<hash>  <name>" per line).
func findChecksum(r io.Reader, asset string) (string, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum for %s", asset)
}
