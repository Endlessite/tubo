#!/bin/bash
set -e

# Tubo Installer
# Meant to be hosted at endlessite.com/get
# Usage: curl -sL https://endlessite.com/get | bash

echo "Installing Tubo CLI..."

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

BINARY_URL="https://github.com/endlessite/tubo/releases/latest/download/tubo-${OS}-${ARCH}"


if [ "$(id -u)" -eq 0 ]; then
    INSTALL_DIR="/usr/local/bin"
else
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
fi

BIN_PATH="$INSTALL_DIR/tubo"

echo "Downloading from GitHub Releases..."
if ! curl -sL --fail "$BINARY_URL" -o "$BIN_PATH"; then
    echo "Error: Failed to download Tubo binary." >&2
    echo "URL attempted: $BINARY_URL" >&2
    exit 1
fi

chmod +x "$BIN_PATH"

echo ""
echo "[OK] Tubo installed successfully!"
echo "   Location: $BIN_PATH"
echo ""


if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo "[WARNING] $INSTALL_DIR is not in your PATH."
    echo "   To use tubo from anywhere, add this to your ~/.bashrc or ~/.zshrc:"
    echo "   export PATH=\"\$PATH:$INSTALL_DIR\""
    echo ""
    echo "   For now, you can run it using the full path:"
    echo "   $BIN_PATH"
else
    echo "Tubo — Transfer files without root, without hassle."
    echo ""
    echo "Get started:"
    echo "  tubo send <file>"
    echo "  tubo receive <token>"
fi
echo ""
