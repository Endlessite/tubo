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
}

type TransferMeta struct {
	Type       string `json:"type"`
	Filename   string `json:"filename"`
	Compressed bool   `json:"compressed"`
	Directory  bool   `json:"directory"`
}

type ChecksumMessage struct {
	Type   string `json:"type"`
	SHA256 string `json:"sha256"`
}

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
		fmt.Println("")
		fmt.Println("All transfers are end-to-end encrypted. Always.")
		fmt.Println("The token is generated automatically — just copy and paste it.")
		os.Exit(0)
	}

	switch os.Args[1] {
	case "send":
		runSend()
	case "receive":
		runReceive()
	case "config":
		runConfig()
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
