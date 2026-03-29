#!/usr/bin/env bash
# ░▒▓█ VOIDMON INSTALLER █▓▒░
# Usage: curl -fsSL https://raw.githubusercontent.com/shamspias/voidmon/main/install.sh | bash

set -euo pipefail

REPO="shamspias/voidmon"
BIN_NAME="void"
INSTALL_DIR="$HOME/.local/bin"

mkdir -p "$INSTALL_DIR"

# ─── Detect OS and arch ──────────────────────

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    *)
        echo "  ✗ Unsupported OS: $OS"
        exit 1
        ;;
esac

case "$ARCH" in
    x86_64|amd64)   ARCH="amd64" ;;
    arm64|aarch64)  ARCH="arm64" ;;
    *)
        echo "  ✗ Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

ASSET="${BIN_NAME}-${OS}-${ARCH}"

echo ""
echo "  ░▒▓█ VOIDMON INSTALLER █▓▒░"
echo "  ─────────────────────────────"
echo "  OS:   $OS"
echo "  Arch: $ARCH"
echo ""

# ─── Find latest release ─────────────────────

echo "  → Finding latest release..."
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST" ]; then
    echo "  ✗ Could not find latest release."
    echo "  → Trying direct download from main branch..."

    # Fallback: build from source
    echo "  → Installing from source instead..."

    if ! command -v go &> /dev/null; then
        echo "  ✗ Go is required to build from source."
        echo "  → Install Go: https://go.dev/dl/"
        exit 1
    fi

    TMP_DIR=$(mktemp -d)
    trap "rm -rf $TMP_DIR" EXIT

    echo "  → Cloning repository..."
    git clone --depth 1 "https://github.com/${REPO}.git" "$TMP_DIR/voidmon" 2>/dev/null

    echo "  → Building..."
    cd "$TMP_DIR/voidmon"
    go build -ldflags="-s -w" -o "${BIN_NAME}" .

    echo "  → Installing to ${INSTALL_DIR}/${BIN_NAME}..."
    if [ -w "$INSTALL_DIR" ]; then
        cp "${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"
    else
        sudo cp "${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"
    fi

    echo ""
    echo "  ✓ voidmon installed successfully!"
    echo "  → Run: void"
    echo ""
    exit 0
fi

echo "  → Latest version: $LATEST"

# ─── Download binary ─────────────────────────

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/${ASSET}.tar.gz"

echo "  → Downloading ${ASSET}.tar.gz..."
TMP_DIR=$(mktemp -d)
trap "rm -rf $TMP_DIR" EXIT

if ! curl -fsSL "$DOWNLOAD_URL" -o "${TMP_DIR}/${ASSET}.tar.gz"; then
    echo "  ✗ Download failed. Binary may not exist for your platform."
    echo "  → Try building from source: git clone https://github.com/${REPO} && cd voidmon && make install"
    exit 1
fi

# ─── Install ──────────────────────────────────

echo "  → Extracting..."
tar -xzf "${TMP_DIR}/${ASSET}.tar.gz" -C "$TMP_DIR"

echo "  → Installing to ${INSTALL_DIR}/${BIN_NAME}..."
if [ -w "$INSTALL_DIR" ]; then
    mv "${TMP_DIR}/${ASSET}" "${INSTALL_DIR}/${BIN_NAME}"
else
    sudo mv "${TMP_DIR}/${ASSET}" "${INSTALL_DIR}/${BIN_NAME}"
fi
chmod +x "${INSTALL_DIR}/${BIN_NAME}"

echo ""
echo "  ✓ voidmon ${LATEST} installed successfully!"
echo "  → Run: void"
echo ""