#!/bin/sh
set -e

# Tubo Installer
# Served by the relay at /get
# Usage: curl -sL https://tubo.endlessite.com/get | sh

echo "Installing Tubo CLI..."

if ! command -v curl >/dev/null 2>&1; then
    echo "Error: curl is required to install Tubo." >&2
    exit 1
fi

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

if [ "$ARCH" = "x86_64" ]; then 
    ARCH="amd64"
elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then 
    ARCH="arm64"
else 
    echo "Error: Unsupported architecture $ARCH" >&2
    exit 1
fi

RELEASE_URL="https://github.com/endlessite/tubo/releases/latest/download"
ASSET="tubo-${OS}-${ARCH}"
BINARY_URL="$RELEASE_URL/$ASSET"


if [ "$(id -u)" -eq 0 ]; then
    INSTALL_DIR="/usr/local/bin"
else
    INSTALL_DIR="$HOME/.local/bin"
fi

mkdir -p "$INSTALL_DIR"

BIN_PATH="$INSTALL_DIR/tubo"

echo "Downloading from GitHub Releases..."
TMP_BIN="$BIN_PATH.download"
if ! curl -sL --fail "$BINARY_URL" -o "$TMP_BIN"; then
    rm -f "$TMP_BIN"
    echo "Error: Failed to download Tubo binary." >&2
    echo "URL attempted: $BINARY_URL" >&2
    exit 1
fi

# Check the binary against the release's checksums.txt
if command -v sha256sum >/dev/null 2>&1; then
    SHA_CMD="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
    SHA_CMD="shasum -a 256"
else
    SHA_CMD=""
fi

if [ -n "$SHA_CMD" ]; then
    EXPECTED=$(curl -sL --fail "$RELEASE_URL/checksums.txt" 2>/dev/null | awk -v f="$ASSET" '$2 == f { print $1 }')
    ACTUAL=$($SHA_CMD "$TMP_BIN" | awk '{ print $1 }')
    if [ -z "$EXPECTED" ]; then
        echo "Warning: no checksum published for $ASSET, skipping verification." >&2
    elif [ "$EXPECTED" != "$ACTUAL" ]; then
        rm -f "$TMP_BIN"
        echo "Error: checksum mismatch for $ASSET. Nothing was installed." >&2
        exit 1
    else
        echo "Checksum verified."
    fi
else
    echo "Warning: sha256sum/shasum not found, skipping checksum verification." >&2
fi

chmod +x "$TMP_BIN"
mv "$TMP_BIN" "$BIN_PATH"

echo ""
echo "[OK] Tubo installed successfully!"
echo "   Location: $BIN_PATH"
echo ""


case ":$PATH:" in
    *":$INSTALL_DIR:"*) IN_PATH=1 ;;
    *) IN_PATH=0 ;;
esac

if [ "$IN_PATH" = 1 ]; then
    echo "Tubo — Transfer files without root, without hassle."
    echo ""
    echo "Get started:"
    echo "  tubo send <file>"
    echo "  tubo receive <token>"
else
    echo "[WARNING] $INSTALL_DIR is not in your PATH."
    echo "   To use tubo from anywhere, add this to your ~/.bashrc or ~/.zshrc:"
    echo "   export PATH=\"\$PATH:$INSTALL_DIR\""
    echo ""
    echo "   For now, you can run it using the full path:"
    echo "   $BIN_PATH"
fi
echo ""
