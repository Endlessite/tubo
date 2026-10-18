package main

import (
	"bufio"
	"compress/gzip"
	"crypto/hmac"
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
		fmt.Println("Usage: tubo receive [token] [--stdout]\n\nThe token can also be passed as TUBO_TOKEN=<token>, which keeps it out of `ps`.")
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

	if len(args) == 0 && os.Getenv("TUBO_TOKEN") != "" {
		args = append(args, os.Getenv("TUBO_TOKEN"))
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

		e2eeKey = generateRandomKey(keyLength)
		token := fmt.Sprintf("%s-%s-%s", session.ID, session.Password, e2eeKey)

		fmt.Fprintf(os.Stderr, "\nReady! On the other machine run:\n")
		fmt.Fprintf(os.Stderr, "  TUBO_TOKEN=%s tubo send <file_or_directory>\n\n", token)
		fmt.Fprintf(os.Stderr, "Without tubo installed (needs only curl and openssl):\n")
		fmt.Fprintf(os.Stderr, "  curl -sL %s://%s/run | TUBO_TOKEN=%s sh -s send <file>\n\n", httpScheme, server, token)

		if manual {
			fmt.Fprintf(os.Stderr, "Or by hand, with only curl and openssl:\n")
			fmt.Fprintf(os.Stderr, "%s < <file> | curl -s -X POST -T - -H 'Expect:' -H 'X-File-Name: <name>' -u 'tubo:%s' %s://%s/%s\n\n", manualOpenSSL("-e", e2eeKey), session.Password, httpScheme, server, session.ID)
		}
	}

	stream := newCipherStream(e2eeKey)
	mac := newMAC(e2eeKey)

	fmt.Fprintf(os.Stderr, "End-to-End Encryption: ENABLED (AES-256-CTR)\n")
	fmt.Fprintf(os.Stderr, "Waiting for sender...\n")

	var fileName string
	var httpSender bool
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
			fileName = sanitizeFileName(srvMsg.Filename)
			httpSender = srvMsg.Sender == "http"
			break
		}
	}

	// A CLI sender names archives *.tar / *.tar.gz and compressed files *.gz.
	// curl/run.sh senders always send the file as-is.
	var isDir, isCompressed bool
	var totalSize int64
	if !httpSender {
		isDir = strings.HasSuffix(fileName, ".tar") || strings.HasSuffix(fileName, ".tar.gz") || strings.HasSuffix(fileName, ".tgz")
		isCompressed = strings.HasSuffix(fileName, ".gz") || strings.HasSuffix(fileName, ".tgz")
	}
	// TODO: handle symlinks

	outName := fileName
	if isDir {
		outName = strings.TrimSuffix(strings.TrimSuffix(fileName, ".gz"), ".tar")
	} else if isCompressed {
		outName = strings.TrimSuffix(fileName, ".gz")
	}

	if isDir && isStdout {
		fatal("Cannot output a directory to stdout. Please provide a destination path.", nil)
	}

	if !isStdout {
		outName = uniqueName(outName)
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

		if isDir {
			if _, err := os.Stat(outName); err == nil {
				fatal(fmt.Sprintf("Destination ./%s already exists. Remove it or choose another name.", outName), nil)
			}
		}
	}

	startMsg := `{"action": "start"}`
	if err := socket.WriteMessage(websocket.TextMessage, []byte(startMsg)); err != nil {
		fatal("Failed to send start signal: ", err)
	}

	// The CLI sender announces itself with a "stream" message before the data
	// and ends with a MAC. Anything else is a curl/run.sh sender.
	var authenticated bool
	var first []byte
	for first == nil && !authenticated {
		msgType, msg, err := socket.ReadMessage()
		if err != nil {
			fatal("Connection closed before any data was received.")
		}
		if msgType == websocket.BinaryMessage {
			first = msg
			break
		}
		var streamMsg StreamMessage
		if json.Unmarshal(msg, &streamMsg) == nil && streamMsg.Type == "stream" {
			authenticated = true
			isDir = streamMsg.Directory
			isCompressed = streamMsg.Compressed
			totalSize = streamMsg.Size
		}
	}

	// Write to a temporary name and only move it into place once the
	// transfer is complete and, for CLI senders, the MAC has been checked.
	partName := outName + ".tubo-part"
	cleanup := func() {
		if !isStdout {
			os.RemoveAll(partName)
		}
	}
	fail := func(msg string) {
		cleanup()
		fmt.Fprintf(os.Stderr, "\nError: %s\n", msg)
		if !isStdout {
			fmt.Fprintf(os.Stderr, "Nothing was saved.\n")
		}
		os.Exit(1)
	}

	pipeReader, pipeWriter := io.Pipe()
	var pipelineErr error
	var received int64
	var peerMAC string

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		var inputReader io.Reader = pipeReader

		if isCompressed {
			gzReader, err := gzip.NewReader(pipeReader)
			if err != nil {
				pipelineErr = fmt.Errorf("failed to decompress data: %w", err)
				pipeReader.CloseWithError(pipelineErr)
				return
			}
			defer gzReader.Close()
			inputReader = gzReader
		}

		if isDir {
			if err := os.MkdirAll(partName, 0755); err != nil {
				pipelineErr = err
			} else {
				pipelineErr = extractTar(inputReader, partName)
			}
		} else if isStdout {
			_, pipelineErr = io.Copy(os.Stdout, inputReader)
		} else {
			outFile, err := os.OpenFile(partName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				pipelineErr = err
			} else {
				_, pipelineErr = io.Copy(outFile, inputReader)
				if cerr := outFile.Close(); pipelineErr == nil {
					pipelineErr = cerr
				}
			}
		}
		if pipelineErr != nil {
			pipeReader.CloseWithError(pipelineErr)
		} else {
			// Drain anything after the end of a gzip/tar stream so the
			// sender is never blocked and every byte goes through the MAC.
			io.Copy(io.Discard, pipeReader)
		}
	}()

	writeChunk := func(chunk []byte) {
		mac.Write(chunk)
		stream.XORKeyStream(chunk, chunk)
		if _, err := pipeWriter.Write(chunk); err != nil {
			pipeWriter.Close()
			wg.Wait()
			fail(fmt.Sprintf("Writing output failed: %v", pipelineErr))
		}
		received += int64(len(chunk))
		fmt.Fprintf(os.Stderr, "\r%-40s", formatProgress("Receiving", received, totalSize))
	}

	fmt.Fprintf(os.Stderr, "Receiving data...\n")

	if first != nil {
		writeChunk(first)
	}

	for {
		msgType, message, err := socket.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				pipeWriter.CloseWithError(err)
				wg.Wait()
				fail(fmt.Sprintf("The sender disconnected before the transfer finished (%v).", err))
			}
			break
		}
		if msgType == websocket.BinaryMessage {
			writeChunk(message)
		} else if msgType == websocket.TextMessage {
			var macMsg MACMessage
			if err := json.Unmarshal(message, &macMsg); err == nil && macMsg.Type == "mac" {
				peerMAC = macMsg.HMAC
			}
		}
	}

	pipeWriter.Close()
	wg.Wait()
	fmt.Fprintf(os.Stderr, "\n")

	if pipelineErr != nil {
		fail(fmt.Sprintf("Writing output failed: %v", pipelineErr))
	}

	if authenticated {
		if peerMAC == "" {
			fail("The sender disconnected before the transfer finished.")
		}
		expected, err := hex.DecodeString(peerMAC)
		if err != nil || !hmac.Equal(expected, mac.Sum(nil)) {
			fail("Integrity check failed: the data was modified or corrupted in transit.")
		}
		fmt.Fprintf(os.Stderr, "[OK] Integrity verified (HMAC-SHA256)\n")
	} else {
		fmt.Fprintf(os.Stderr, "Note: sent with curl/run.sh, so the data is encrypted but its integrity cannot be verified.\n")
	}

	if isStdout {
		return
	}

	if !isDir {
		os.Remove(outName)
	}
	if err := os.Rename(partName, outName); err != nil {
		fail(fmt.Sprintf("Could not move %s to %s: %v", partName, outName, err))
	}

	if isDir {
		fmt.Fprintf(os.Stderr, "Directory extracted successfully to ./%s\n", outName)
	} else {
		fmt.Fprintf(os.Stderr, "File saved successfully to %s\n", outName)
	}
}
