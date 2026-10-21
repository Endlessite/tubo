package main

import (
	"archive/tar"
	"bytes"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeriveCipherParamsVector(t *testing.T) {
	// openssl enc -aes-256-ctr -pbkdf2 -iter 10000 -md sha256 -nosalt -pass env:K -P
	aesKey, iv := deriveCipherParams("abcDEF123ghiJKL456mnoP")
	if got := strings.ToUpper(hex.EncodeToString(aesKey)); got != "0D6B9C380019E59C8F639B5FB6472B09F5E753A24CE80309D16E611CF6AA231E" {
		t.Fatalf("key = %s", got)
	}
	if got := strings.ToUpper(hex.EncodeToString(iv)); got != "70BC80069E26FC7D6FDE093AD284FDEA" {
		t.Fatalf("iv = %s", got)
	}
}

// The CLI and run.sh must produce the same ciphertext for the same key.
func TestCipherMatchesOpenSSL(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not installed")
	}
	key := generateRandomKey(keyLength)
	plaintext := bytes.Repeat([]byte("tubo interop test "), 5000)

	cmd := exec.Command("sh", "-c", manualOpenSSL("-e", key))
	cmd.Stdin = bytes.NewReader(plaintext)
	want, err := cmd.Output()
	if err != nil {
		t.Skipf("openssl without -pbkdf2 support: %v", err)
	}

	got := make([]byte, len(plaintext))
	newCipherStream(key).XORKeyStream(got, plaintext)
	if !bytes.Equal(got, want) {
		t.Fatal("Go ciphertext differs from openssl ciphertext")
	}
}

func TestMACDependsOnKey(t *testing.T) {
	a, b := newMAC("key-one"), newMAC("key-two")
	a.Write([]byte("data"))
	b.Write([]byte("data"))
	if bytes.Equal(a.Sum(nil), b.Sum(nil)) {
		t.Fatal("different keys produced the same MAC")
	}
}

func TestGenerateRandomKey(t *testing.T) {
	k := generateRandomKey(keyLength)
	if len(k) != keyLength || strings.Contains(k, "-") {
		t.Fatalf("bad key %q", k)
	}
}

func TestSanitizeFileName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":         "report.pdf",
		"../../etc/passwd":   "passwd",
		"/root/.ssh/id_rsa":  "id_rsa",
		`..\..\windows\evil`: "evil",
		"..":                 "download.dat",
		"":                   "download.dat",
		"dir/":               "dir",
	}
	for in, want := range cases {
		if got := sanitizeFileName(in); got != want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniqueName(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "file.txt")
	if got := uniqueName(name); got != name {
		t.Fatalf("got %s", got)
	}
	os.WriteFile(name, nil, 0644)
	if got := uniqueName(name); got != name+".1" {
		t.Fatalf("got %s", got)
	}
}

func TestExtractTarStaysInDestination(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, name := range []string{"top/ok.txt", "top/../../escape.txt", "/abs.txt"} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: 2, Typeflag: tar.TypeReg})
		tw.Write([]byte("hi"))
	}
	tw.Close()

	root := t.TempDir()
	dest := filepath.Join(root, "out")
	os.MkdirAll(dest, 0755)
	if err := extractTar(&buf, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "ok.txt")); err != nil {
		t.Fatal("expected ok.txt to be extracted")
	}
	if _, err := os.Stat(filepath.Join(root, "escape.txt")); err == nil {
		t.Fatal("tar entry escaped the destination directory")
	}
}

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.2.1", "0.2.0", true},
		{"v0.10.0", "0.9.9", true},
		{"v1.0.0", "0.99.0", true},
		{"v0.2.0", "0.2.0", false},
		{"v0.1.9", "0.2.0", false},
		{"v0.2.0", "0.2.0-SNAPSHOT-abc", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestFindChecksum(t *testing.T) {
	sums := "aaa  tubo-linux-amd64\nbbb  tubo-darwin-arm64\n"
	if got, _ := findChecksum(strings.NewReader(sums), "tubo-darwin-arm64"); got != "bbb" {
		t.Fatalf("got %q", got)
	}
	if _, err := findChecksum(strings.NewReader(sums), "tubo-windows-amd64.exe"); err == nil {
		t.Fatal("expected an error for a missing asset")
	}
}
