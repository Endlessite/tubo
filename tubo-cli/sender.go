package main

import (
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
	"path/filepath"

	"github.com/gorilla/websocket"
)

func runSend() {
	if len(os.Args) < 3 || os.Args[2] == "--help" || os.Args[2] == "-h" {
		fmt.Println("Usage: tubo send <file_or_directory> [--compress]")
		os.Exit(0)
	}

	compress := false
	manual := false
	var args []string
	for _, arg := range os.Args[2:] {
		if arg == "--compress" {
			compress = true
		} else if arg == "--manual" {
			manual = true
		} else {
			args = append(args, arg)
		}
	}
	if len(args) == 0 {
		fmt.Println("Usage: tubo send <file_or_directory> [--compress] [token]")
		os.Exit(1)
	}

	inputPath := args[0]
	isStdin := inputPath == "-"

	var info os.FileInfo
	var err error
	var isDir bool

	if !isStdin {
		info, err = os.Stat(inputPath)
		if err != nil {
			fatal("Cannot access path: ", err)
		}
		isDir = info.IsDir()
	}

	var fileName string
	if isStdin {
		fileName = "message.txt"
	} else {
		baseName := filepath.Base(inputPath)
		fileName = baseName
		if isDir && compress {
			fileName = baseName + ".tar.gz"
		} else if isDir {
			fileName = baseName + ".tar"
		} else if compress {
			fileName = baseName + ".gz"
		}
	}

	var totalSize int64
	if !isStdin {
		if isDir {
			totalSize = dirSize(inputPath)
		} else {
			totalSize = info.Size()
		}
	}

	var e2eeKey string
	var joinID, joinPwd string
	joining := false

	if len(args) >= 2 {
		joinID, joinPwd, e2eeKey = parseToken(args[1])
		joining = true
	} else {
		e2eeKey = generateRandomKey(16)
	}

	hash := sha512.Sum512([]byte(e2eeKey))
	block, err := aes.NewCipher(hash[:32])
	if err != nil {
		fatal("Failed to initialize AES cipher: ", err)
	}
	stream := cipher.NewCTR(block, hash[32:48])

	server, wsScheme := getServer()
	httpScheme := "http"
	if wsScheme == "wss" {
		httpScheme = "https"
	}

	var wsUrl string
	if joining {
		wsUrl = fmt.Sprintf("%s://%s/ws/join/%s?role=sender", wsScheme, server, joinID)
	} else {
		wsUrl = fmt.Sprintf("%s://%s/ws/create?role=sender", wsScheme, server)
	}

	fmt.Fprintf(os.Stderr, "Connecting to Tubo Server (%s)...\n", server)

	headers := http.Header{}
	if joining {
		headers.Add("Authorization", "Bearer "+joinPwd)
	}

	socket, _, err := websocket.DefaultDialer.Dial(wsUrl, headers)
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

		token := fmt.Sprintf("%s-%s-%s", session.ID, session.Password, e2eeKey)

		fmt.Fprintf(os.Stderr, "\nReady! Share this command:\n")
		fmt.Fprintf(os.Stderr, "tubo receive %s\n\n", token)

		fmt.Fprintf(os.Stderr, "No Tubo installed? Use this instead:\n")
		fmt.Fprintf(os.Stderr, "curl -sL %s://%s/run | sh -s receive %s\n\n", httpScheme, server, token)

		if manual {
			hash := sha512.Sum512([]byte(e2eeKey))
			aesKeyHex := hex.EncodeToString(hash[:32])
			aesIvHex := hex.EncodeToString(hash[32:48])
			
			fmt.Fprintf(os.Stderr, "Or do it entirely manually without the script:\n")
			fmt.Fprintf(os.Stderr, "curl -s -u \"tubo:%s\" %s://%s/%s > file.enc && openssl enc -d -aes-256-ctr -K %s -iv %s -in file.enc > %s\n\n", session.Password, httpScheme, server, session.ID, aesKeyHex, aesIvHex, fileName)
		}
	}

	meta := TransferMeta{Type: "meta", Filename: fileName, Compressed: compress, Directory: isDir}
	metaBytes, _ := json.Marshal(meta)
	socket.WriteMessage(websocket.TextMessage, metaBytes)

	fmt.Fprintf(os.Stderr, "Waiting for receiver... (Ctrl+C to cancel)\n")

	// TODO: add a timeout here so we don't wait forever

	for {
		_, msg, err := socket.ReadMessage()
		if err != nil {
			fatal("Connection closed while waiting.")
		}
		var srvMsg ServerMessage
		if err := json.Unmarshal(msg, &srvMsg); err != nil {
			continue
		}
		if srvMsg.Status == "start" {
			break
		}
	}

	fmt.Fprintf(os.Stderr, "Receiver connected! Sending file...\n")

	dataReader, dataWriter := io.Pipe()

	go func() {
		defer dataWriter.Close()

		var writer io.WriteCloser = dataWriter
		var gzWriter *gzip.Writer

		if compress {
			gzWriter = gzip.NewWriter(dataWriter)
			writer = gzWriter
		}

		if isDir {
			if err := tarDirectory(inputPath, writer); err != nil {
				dataWriter.CloseWithError(err)
				return
			}
		} else {
			var source io.Reader
			if isStdin {
				source = os.Stdin
			} else {
				file, err := os.Open(inputPath)
				if err != nil {
					dataWriter.CloseWithError(err)
					return
				}
				defer file.Close()
				source = file
			}
			if _, err := io.Copy(writer, source); err != nil {
				dataWriter.CloseWithError(err)
				return
			}
		}

		if gzWriter != nil {
			gzWriter.Close()
		}
	}()

	hasher := sha256.New()
	buf := make([]byte, 32*1024)
	var sent int64

	for {
		n, err := dataReader.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			hasher.Write(chunk)
			stream.XORKeyStream(chunk, chunk)
			if errWs := socket.WriteMessage(websocket.BinaryMessage, chunk); errWs != nil {
				fatal("Error sending data to server: ", errWs)
			}
			sent += int64(n)

			if totalSize > 0 {
				pct := float64(sent) / float64(totalSize) * 100
				if pct > 100 {
					pct = 100
				}
				fmt.Fprintf(os.Stderr, "\rSending: %.1f%% | %s / %s", pct, formatBytes(sent), formatBytes(totalSize))
			} else {
				fmt.Fprintf(os.Stderr, "\rSending: %s", formatBytes(sent))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			fatal("Error reading data: ", err)
		}
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	checksumMsg := ChecksumMessage{Type: "checksum", SHA256: checksum}
	checksumBytes, _ := json.Marshal(checksumMsg)
	socket.WriteMessage(websocket.TextMessage, checksumBytes)

	fmt.Fprintf(os.Stderr, "\nTransfer complete. Checksum (SHA-256): %s\n", checksum[:16]+"...")
}
