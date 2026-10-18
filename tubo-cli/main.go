package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type SessionInit struct {
	ID       string `json:"id"`
	Password string `json:"password"`
}

type ServerMessage struct {
	Status   string `json:"status"`
	Filename string `json:"filename"`
	Sender   string `json:"sender"` // "http" when the sender is curl/run.sh
}

type TransferMeta struct {
	Type       string `json:"type"`
	Filename   string `json:"filename"`
	Compressed bool   `json:"compressed"`
	Directory  bool   `json:"directory"`
}

// StreamMessage is sent by the CLI sender right before the first data frame,
// so the receiver knows the stream is followed by a MAC.
type StreamMessage struct {
	Type       string `json:"type"`
	Compressed bool   `json:"compressed"`
	Directory  bool   `json:"directory"`
}

// MACMessage carries the HMAC-SHA256 of the ciphertext, sent after the last data frame.
type MACMessage struct {
	Type string `json:"type"`
	HMAC string `json:"hmac"`
}

// Set at build time by goreleaser.
var version = "dev"

func fatal(msg string, args ...any) {
	if len(args) > 0 && args[0] != nil {
		fmt.Fprintf(os.Stderr, "Error: %s%v\n", msg, args[0])
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
	}
	os.Exit(1)
}

func parseToken(raw string) (id, password, key string) {
	parts := strings.SplitN(raw, "-", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		fatal("Invalid token format. Expected: ID-PASSWORD-KEY")
	}
	return parts[0], parts[1], parts[2]
}

// manual URL parsing — net/url doesn't handle bare hostnames well
func getServer() (string, string) {
	serverUrl := os.Getenv("TUBO_SERVER")

	if serverUrl == "" {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			configPath := filepath.Join(homeDir, ".tuborc")
			configData, err := os.ReadFile(configPath)
			if err == nil {
				serverUrl = strings.TrimSpace(string(configData))
			}
		}
	}

	if serverUrl == "" {
		serverUrl = "https://tubo.endlessite.com"
	}

	wsScheme := "wss"

	for _, prefix := range []string{"https://", "http://", "wss://", "ws://"} {
		if strings.HasPrefix(serverUrl, prefix) {
			serverUrl = serverUrl[len(prefix):]
			if prefix == "http://" || prefix == "ws://" {
				wsScheme = "ws"
			}
			return serverUrl, wsScheme
		}
	}

	// No scheme — assume ws for localhost, wss for everything else
	if strings.HasPrefix(serverUrl, "localhost") || strings.HasPrefix(serverUrl, "127.0.0.1") {
		wsScheme = "ws"
	}

	return serverUrl, wsScheme
}

// sanitizeFileName reduces a name received from the network to a plain file
// name, so a malicious sender or relay cannot write outside the current directory.
func sanitizeFileName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(filepath.Clean("/" + name))
	if name == "/" || name == "." || name == ".." || name == "" {
		return "download.dat"
	}
	return name
}

// uniqueName returns name, or name.1, name.2, ... if it already exists,
// so a file chosen by the sender never silently replaces a local one.
func uniqueName(name string) string {
	candidate := name
	for n := 1; ; n++ {
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s.%d", name, n)
	}
}

// manualOpenSSL is the openssl invocation shared by run.sh and --manual.
// The key is passed through the environment so it never shows up in `ps`.
func manualOpenSSL(mode, key string) string {
	return fmt.Sprintf("TUBO_KEY='%s' openssl enc %s -aes-256-ctr -pbkdf2 -iter %d -md sha256 -nosalt -pass env:TUBO_KEY", key, mode, kdfIterations)
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Tubo — Transfer files without root, without hassle.")
		fmt.Println("")
		fmt.Println("  Send a file:       tubo send <file_or_directory> [--compress]")
		fmt.Println("  Receive a file:    tubo receive <token>")
		fmt.Println("  Change server:     tubo config server <url>")
		fmt.Println("  Show version:      tubo version")
		fmt.Println("")
		fmt.Println("Either side can start: whoever runs first prints a token for the other.")
		fmt.Println("Pass the token as TUBO_TOKEN=<token> to keep it out of the process list.")
		os.Exit(0)
	}

	switch os.Args[1] {
	case "send":
		runSend()
	case "receive":
		runReceive()
	case "config":
		runConfig()
	case "version", "--version", "-v":
		fmt.Println("tubo", version)
	default:
		fmt.Println("Unknown command. Use 'send', 'receive', or 'config'.")
	}
}

func runConfig() {
	if len(os.Args) < 4 || os.Args[2] != "server" {
		fmt.Println("Usage: tubo config server <url>")
		fmt.Println("Example: tubo config server 127.0.0.1:8080")
		os.Exit(1)
	}

	serverUrl := os.Args[3]
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Error: Could not determine home directory.")
		fmt.Println("In environments without a home directory, you cannot save a global config.")
		fmt.Println("Please use the TUBO_SERVER environment variable instead.")
		os.Exit(1)
	}

	configPath := filepath.Join(homeDir, ".tuborc")
	err = os.WriteFile(configPath, []byte(serverUrl), 0600)
	if err != nil {
		fatal("Failed to save config: ", err)
	}

	fmt.Printf("Default server saved to %s\n", configPath)
	fmt.Printf("Tubo will now connect to %s by default.\n", serverUrl)
}
