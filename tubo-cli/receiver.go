package main

import (
	"bufio"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

func runReceive() {
	// TODO: implement resumable transfers (requires checking local file size and sending offset to peer)
	server, wsScheme := getServer()
	httpScheme := "http"
	if wsScheme == "wss" {
		httpScheme = "https"
	}

	var urlStr string
	headers := http.Header{}

	var e2eeKey string
	joining := false

	manual := false
	isStdout := false
	if len(os.Args) >= 3 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
		fmt.Println("Usage: tubo receive <token>")
		os.Exit(0)
	}

	var args []string
	for _, arg := range os.Args[2:] {
		if arg == "--manual" {
			manual = true
		} else if arg == "--stdout" || arg == "-" {
			isStdout = true
		} else {
			args = append(args, arg)
		}
	}

	if len(args) >= 1 {
		id, pwd, key := parseToken(args[0])
		e2eeKey = key
		urlStr = fmt.Sprintf("%s://%s/ws/join/%s?role=receiver", wsScheme, server, id)
		headers.Add("Authorization", "Bearer "+pwd)
		joining = true
	} else {
		urlStr = fmt.Sprintf("%s://%s/ws/create?role=receiver", wsScheme, server)
	}

	fmt.Fprintf(os.Stderr, "Connecting to Tubo Server (%s)...\n", server)

	socket, _, err := websocket.DefaultDialer.Dial(urlStr, headers)
	if err != nil {
		fatal("Connection Error: ", err)
	}
	defer socket.Close()

	if !joining {
		_, initMessage, err := socket.ReadMessage()
		if err != nil {
			fatal("Failed to read Session Data: ", err)
		}

		var session SessionInit
		if err := json.Unmarshal(initMessage, &session); err != nil {
			fatal("Failed to parse Session Data: ", err)
		}

		e2eeKey = generateRandomKey(44)
		token := fmt.Sprintf("%s-%s-%s", session.ID, session.Password, e2eeKey)

		fmt.Fprintf(os.Stderr, "\nReady! Share this command:\n")
		fmt.Fprintf(os.Stderr, "tubo send <file_or_directory> %s\n\n", token)

		fmt.Fprintf(os.Stderr, "No Tubo installed? Use this instead:\n")
		fmt.Fprintf(os.Stderr, "curl -sL %s://%s/run | sh -s send <file> %s\n\n", httpScheme, server, token)

		if manual {
			hash := sha512.Sum512([]byte(e2eeKey))
			aesKeyHex := hex.EncodeToString(hash[:32])
			aesIvHex := hex.EncodeToString(hash[32:48])
			
			fmt.Fprintf(os.Stderr, "Or do it entirely manually without the script:\n")
			fmt.Fprintf(os.Stderr, "openssl enc -aes-256-ctr -K %s -iv %s -in <file> > file.enc && curl -s -u \"tubo:%s\" -H \"X-File-Name: <filename>\" --data-binary @file.enc %s://%s/%s\n", aesKeyHex, aesIvHex, session.Password, httpScheme, server, session.ID)
			fmt.Fprintf(os.Stderr, "(If piping from stdin, use `-in -` for openssl and `--data-binary @-` for curl without creating file.enc)\n\n")
		}
	}

	hash := sha512.Sum512([]byte(e2eeKey))
	block, err := aes.NewCipher(hash[:32])
	if err != nil {
		fatal("Failed to initialize AES cipher: ", err)
	}
	stream := cipher.NewCTR(block, hash[32:48])

	fmt.Fprintf(os.Stderr, "End-to-End Encryption: ENABLED (AES-256-CTR)\n")
	fmt.Fprintf(os.Stderr, "Waiting for sender...\n")

	var fileName string
	for {
		_, msg, err := socket.ReadMessage()
		if err != nil {
			fatal("Connection closed while waiting.")
		}
		var srvMsg ServerMessage
		if err := json.Unmarshal(msg, &srvMsg); err != nil {
			continue
		}
		if srvMsg.Status == "ready" {
			fileName = srvMsg.Filename
			break
		}
	}

	isDir := strings.HasSuffix(fileName, ".tar") || strings.HasSuffix(fileName, ".tar.gz") || strings.HasSuffix(fileName, ".tgz")
	// TODO: handle symlinks
	isCompressed := strings.HasSuffix(fileName, ".gz") || strings.HasSuffix(fileName, ".tgz")

	outName := fileName
	if isDir {
		outName = strings.TrimSuffix(strings.TrimSuffix(fileName, ".gz"), ".tar")
	} else if isCompressed {
		outName = strings.TrimSuffix(fileName, ".gz")
	}

	if !isStdout {
		fileInfo, _ := os.Stdin.Stat()
		isInteractive := (fileInfo.Mode() & os.ModeCharDevice) != 0

		if isInteractive {
			reader := bufio.NewReader(os.Stdin)
			if isDir {
				fmt.Fprintf(os.Stderr, "\nIncoming directory: %s\n", outName)
				fmt.Fprintf(os.Stderr, "Extract to [./%s]: ", outName)
			} else {
				fmt.Fprintf(os.Stderr, "\nIncoming file: %s\n", outName)
				fmt.Fprintf(os.Stderr, "Save as [%s]: ", outName)
			}

			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)
			if input != "" {
				outName = input
			}
		} else {
			if isDir {
				fmt.Fprintf(os.Stderr, "\nIncoming directory: %s (non-interactive, extracting to current directory)\n", outName)
			} else {
				fmt.Fprintf(os.Stderr, "\nIncoming file: %s (non-interactive, saving to default name)\n", outName)
			}
		}
	}

	startMsg := `{"action": "start"}`
	if err := socket.WriteMessage(websocket.TextMessage, []byte(startMsg)); err != nil {
		fatal("Failed to send start signal: ", err)
	}

	var meta TransferMeta
	var isGoSender bool
	msgType, firstMsg, err := socket.ReadMessage()
	if err != nil {
		fatal("Connection closed unexpectedly.")
	}

	var first []byte
	if msgType == websocket.TextMessage {
		isGoSender = true
		json.Unmarshal(firstMsg, &meta)
		if meta.Type == "meta" {
			isDir = meta.Directory
			isCompressed = meta.Compressed
		}
	} else if msgType == websocket.BinaryMessage {
		first = firstMsg
	}

	pipeReader, pipeWriter := io.Pipe()
	hasher := sha256.New()
	var received int64
	var peerChecksum string

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		var inputReader io.Reader = pipeReader

		if isCompressed {
			gzReader, err := gzip.NewReader(pipeReader)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nError: failed to decompress data: %v\n", err)
				return
			}
			defer gzReader.Close()
			inputReader = gzReader
		}

		if isDir {
			if isStdout {
				fatal("Cannot output a directory to stdout. Please provide a destination path.", nil)
			}
			if err := os.MkdirAll(outName, 0755); err != nil {
				pipeReader.CloseWithError(err)
				return
			}
			if err := extractTar(inputReader, outName); err != nil {
				pipeReader.CloseWithError(err)
				return
			}
		} else {
			var outFile io.WriteCloser
			if isStdout {
				outFile = os.Stdout
			} else {
				var err error
				os.Remove(outName)
				outFile, err = os.Create(outName)
				if err != nil {
					pipeReader.CloseWithError(err)
					return
				}
				defer outFile.Close()
			}
			if _, err := io.Copy(outFile, inputReader); err != nil {
				pipeReader.CloseWithError(err)
				return
			}
		}
	}()

	if first != nil {
		stream.XORKeyStream(first, first)
		hasher.Write(first)
		pipeWriter.Write(first)
		received += int64(len(first))
	}

	fmt.Fprintf(os.Stderr, "Receiving data...\n")
	// TODO: show progress percentage when we know the total size

	for {
		msgType, message, err := socket.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				fatal("Connection closed unexpectedly during transfer: ", err)
			}
			break
		}
		if msgType == websocket.BinaryMessage {
			stream.XORKeyStream(message, message)
			hasher.Write(message)
			if _, err := pipeWriter.Write(message); err != nil {
				fmt.Fprintf(os.Stderr, "\nExtraction pipeline failed: %v\n", err)
				break
			}
			received += int64(len(message))
			fmt.Fprintf(os.Stderr, "\rReceiving: %s", formatBytes(received))
		} else if msgType == websocket.TextMessage {
			var checksumMsg ChecksumMessage
			if err := json.Unmarshal(message, &checksumMsg); err == nil && checksumMsg.Type == "checksum" {
				peerChecksum = checksumMsg.SHA256
			}
		}
	}

	pipeWriter.Close()
	wg.Wait()

	if isGoSender && peerChecksum == "" {
		fatal("Sender disconnected before sending checksum. File is incomplete.", nil)
	}

	localChecksum := hex.EncodeToString(hasher.Sum(nil))
	fmt.Fprintf(os.Stderr, "\n")

	if peerChecksum != "" {
		if localChecksum == peerChecksum {
			fmt.Fprintf(os.Stderr, "[OK] Checksum verified (SHA-256: %s...)\n", localChecksum[:16])
		} else {
			fmt.Fprintf(os.Stderr, "WARNING: Checksum mismatch! The file may be corrupted.\n")
			fmt.Fprintf(os.Stderr, "  Expected: %s\n", peerChecksum)
			fmt.Fprintf(os.Stderr, "  Got:      %s\n", localChecksum)
		}
	}

	if isDir {
		fmt.Fprintf(os.Stderr, "Directory extracted successfully to ./%s\n", outName)
	} else {
		fmt.Fprintf(os.Stderr, "File saved successfully to %s\n", outName)
	}
}
