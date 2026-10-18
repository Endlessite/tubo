#!/bin/sh
set -e

# Tubo — send or receive a file with only curl and openssl.
#
# Receive:  curl -sL https://tubo.endlessite.com/run | TUBO_TOKEN=<token> sh -s receive [output_file|-]
# Send:     curl -sL https://tubo.endlessite.com/run | TUBO_TOKEN=<token> sh -s send <file|->
#
# The token can also be given as the last argument, but then other users on the
# same machine can read it with `ps` while the transfer runs.
#
# This script never puts secrets on a command line: the encryption key goes to
# openssl through the environment, the session password goes to curl through a
# private (0600) temporary config file.

COMMAND=$1

for cmd in curl openssl; do
    if ! command -v $cmd >/dev/null 2>&1; then
        echo "Error: Required command '$cmd' is not installed." >&2
        exit 1
    fi
done

if ! openssl enc -help 2>&1 | grep -q pbkdf2; then
    echo "Error: openssl is too old (needs OpenSSL 1.1.1+ or LibreSSL 2.9+ for -pbkdf2)." >&2
    exit 1
fi

CURL_CFG=""
PART_FILE=""
FAIL_FLAG=""
HEADER_FILE=""
cleanup() {
    rm -f "$CURL_CFG" "$PART_FILE" "$FAIL_FLAG" "$HEADER_FILE"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# --- Shared ---

parse_token() {
    ID=$(echo "$1" | cut -d'-' -f1)
    PWD_TOK=$(echo "$1" | cut -d'-' -f2)
    KEY=$(echo "$1" | cut -d'-' -f3-)

    if [ -z "$ID" ] || [ -z "$PWD_TOK" ] || [ -z "$KEY" ]; then
        echo "Error: Invalid token format. Expected: ID-PASSWORD-KEY" >&2
        exit 1
    fi

    # Override with TUBO_SERVER for self-hosting
    SERVER=${TUBO_SERVER:-https://tubo.endlessite.com}
    SERVER=$(echo "$SERVER" | sed 's|^wss://|https://|; s|^ws://|http://|')
    case "$SERVER" in
        http://*|https://*) ;;
        localhost*|127.0.0.1*) SERVER="http://$SERVER" ;;
        *) SERVER="https://$SERVER" ;;
    esac

    CURL_CFG=$(mktemp "${TMPDIR:-/tmp}/tubo.XXXXXX")
    printf 'user = "tubo:%s"\n' "$PWD_TOK" > "$CURL_CFG"
}

# AES-256-CTR with key and IV from PBKDF2-HMAC-SHA256(key, no salt, 10000 iterations).
# Must stay identical to deriveCipherParams in tubo-cli/crypto.go.
aes_ctr() {
    TUBO_KEY="$KEY" openssl enc "$1" -aes-256-ctr -pbkdf2 -iter 10000 -md sha256 -nosalt -pass env:TUBO_KEY
}

# Prints a name that does not exist yet: name, name.1, name.2, ...
unique_name() {
    candidate=$1
    n=1
    while [ -e "$candidate" ]; do
        candidate="$1.$n"
        n=$((n + 1))
    done
    echo "$candidate"
}

# --- Receive ---

do_receive() {
    parse_token "$1"
    OUTFILE=$2

    if [ "$OUTFILE" = "-" ]; then
        curl -s --fail -K "$CURL_CFG" "$SERVER/$ID" | aes_ctr -d
        return
    fi

    PART_FILE=$(mktemp "./.tubo-part.XXXXXX")
    HEADER_FILE=$(mktemp "${TMPDIR:-/tmp}/tubo-headers.XXXXXX")
    FAIL_FLAG="$PART_FILE.failed"

    echo "Downloading and decrypting..." >&2
    (curl -s --fail -K "$CURL_CFG" -D "$HEADER_FILE" "$SERVER/$ID" || echo 1 > "$FAIL_FLAG") \
        | aes_ctr -d > "$PART_FILE"

    if [ -f "$FAIL_FLAG" ]; then
        echo "Error: Transfer failed or was interrupted. Nothing was saved." >&2
        exit 1
    fi

    if [ -z "$OUTFILE" ]; then
        # The name comes from the sender: keep only the last path component
        # and never overwrite an existing file.
        OUTFILE=$(grep -i '^content-disposition:' "$HEADER_FILE" | head -1 \
            | sed -n 's/.*filename="\([^"]*\)".*/\1/p' | tr -d '\r')
        OUTFILE=$(basename "$OUTFILE" 2>/dev/null || true)
        case "$OUTFILE" in
            ""|.|..|*/*) OUTFILE="download.dat" ;;
        esac
        OUTFILE=$(unique_name "$OUTFILE")
    fi

    mv "$PART_FILE" "$OUTFILE"
    PART_FILE=""
    echo "Done! File saved to $OUTFILE" >&2
    echo "Note: the data was encrypted in transit; the shell script cannot verify its integrity." >&2
}

# --- Send ---

do_send() {
    FILE=$1

    if [ "$FILE" != "-" ] && [ ! -f "$FILE" ]; then
        echo "Error: File not found: $FILE" >&2
        exit 1
    fi

    parse_token "$2"

    if [ "$FILE" = "-" ]; then
        FILENAME="message.txt"
    else
        FILENAME=$(basename "$FILE")
    fi

    echo "Encrypting and uploading: $FILENAME" >&2
    echo "Waiting for receiver... (Ctrl+C to cancel)" >&2

    # -T - streams the upload (--data-binary @- would read the whole file into memory)
    cat "$FILE" \
        | aes_ctr -e \
        | curl -s --fail -K "$CURL_CFG" -X POST -T - -H "Expect:" -H "X-File-Name: $FILENAME" "$SERVER/$ID" >/dev/null || {
            echo "Error: Transfer failed or was interrupted." >&2
            exit 1
        }

    echo "Transfer complete." >&2
}

# --- Main ---

case "$COMMAND" in
    receive)
        if [ -n "$TUBO_TOKEN" ]; then
            do_receive "$TUBO_TOKEN" "$2"
        elif [ -n "$2" ]; then
            do_receive "$2" "$3"
        else
            echo "Usage:" >&2
            echo "  TUBO_TOKEN=<token> sh run.sh receive [output_file|-]" >&2
            echo "  The token is printed by 'tubo send' on the sender machine." >&2
            exit 1
        fi
        ;;
    send)
        if [ -n "$TUBO_TOKEN" ] && [ -n "$2" ]; then
            do_send "$2" "$TUBO_TOKEN"
        elif [ -n "$2" ] && [ -n "$3" ]; then
            do_send "$2" "$3"
        else
            echo "Usage:" >&2
            echo "  TUBO_TOKEN=<token> sh run.sh send <file|->" >&2
            echo "  The token is printed by 'tubo receive' on the receiver machine." >&2
            exit 1
        fi
        ;;
    *)
        echo "Tubo — send or receive a file with only curl and openssl." >&2
        echo "" >&2
        echo "  Receive:  TUBO_TOKEN=<token> sh run.sh receive [output_file|-]" >&2
        echo "  Send:     TUBO_TOKEN=<token> sh run.sh send <file|->" >&2
        exit 1
        ;;
esac
