package main

import (
	"compress/gzip"
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
	// TODO: implement resumable transfers (requires checking local file size and sending offset to peer)
	if len(os.Args) < 3 || os.Args[2] == "--help" || os.Args[2] == "-h" {
		fmt.Println("Usage: tubo send <file_or_directory|-> [--compress] [token]")
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
	} else if envToken := os.Getenv("TUBO_TOKEN"); envToken != "" {
		joinID, joinPwd, e2eeKey = parseToken(envToken)
		joining = true
	} else {
		e2eeKey = generateRandomKey(keyLength)
	}

	stream := newCipherStream(e2eeKey)
	mac := newMAC(e2eeKey)

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

		fmt.Fprintf(os.Stderr, "\nReady! On the other machine run:\n")
		fmt.Fprintf(os.Stderr, "  TUBO_TOKEN=%s tubo receive\n\n", token)
		fmt.Fprintf(os.Stderr, "Without tubo installed (needs only curl and openssl):\n")
		fmt.Fprintf(os.Stderr, "  curl -sL %s://%s/run | TUBO_TOKEN=%s sh -s receive\n\n", httpScheme, server, token)

		if manual {
			fmt.Fprintf(os.Stderr, "Or by hand, with only curl and openssl:\n")
			fmt.Fprintf(os.Stderr, "curl -s -u 'tubo:%s' %s://%s/%s | %s > '%s'\n\n", session.Password, httpScheme, server, session.ID, manualOpenSSL("-d", e2eeKey), fileName)
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

	streamMsg, _ := json.Marshal(newStreamMessage(compress, isDir, isStdin, totalSize))
	if err := socket.WriteMessage(websocket.TextMessage, streamMsg); err != nil {
		fatal("Error sending data to server: ", err)
	}

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

	buf := make([]byte, 32*1024)
	var sent int64

	for {
		n, err := dataReader.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			stream.XORKeyStream(chunk, chunk)
			mac.Write(chunk)
			if errWs := socket.WriteMessage(websocket.BinaryMessage, chunk); errWs != nil {
				fatal("Error sending data to server: ", errWs)
			}
			sent += int64(n)

			if totalSize > 0 {
				pct := float64(sent) / float64(totalSize) * 100
				if pct > 100 {
					pct = 100
				}
				fmt.Fprintf(os.Stderr, "\r%-40s", fmt.Sprintf("Sending: %.1f%% | %s / %s", pct, formatBytes(sent), formatBytes(totalSize)))
			} else {
				fmt.Fprintf(os.Stderr, "\r%-40s", "Sending: "+formatBytes(sent))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			fatal("Error reading data: ", err)
		}
	}

	macMsg, _ := json.Marshal(MACMessage{Type: "mac", HMAC: hex.EncodeToString(mac.Sum(nil))})
	if err := socket.WriteMessage(websocket.TextMessage, macMsg); err != nil {
		fatal("Error sending data to server: ", err)
	}
	socket.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

	fmt.Fprintf(os.Stderr, "\nTransfer complete.\n")
}
