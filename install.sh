#!/usr/bin/env sh
# Tether installer — downloads the right precompiled binary and sets up shell integration.
# Usage: curl -fsSL https://raw.githubusercontent.com/AllDayJon/Tether/main/install.sh | sh

set -e

REPO="AllDayJon/Tether"
BINARY="tether"
INSTALL_DIR="/usr/local/bin"

# --- detect OS and arch ---

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Linux)  OS_KEY="linux"  ;;
  Darwin) OS_KEY="darwin" ;;
  *)
    echo "Unsupported OS: $OS"
    echo "See https://github.com/$REPO for manual installation."
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64 | amd64) ARCH_KEY="amd64" ;;
  arm64 | aarch64) ARCH_KEY="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

ASSET="${BINARY}-${OS_KEY}-${ARCH_KEY}"

# --- resolve latest release tag ---

echo "Fetching latest Tether release..."
LATEST_URL="https://api.github.com/repos/${REPO}/releases/latest"

if command -v curl >/dev/null 2>&1; then
  TAG="$(curl -fsSL "$LATEST_URL" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"
elif command -v wget >/dev/null 2>&1; then
  TAG="$(wget -qO- "$LATEST_URL" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"
else
  echo "curl or wget is required to download Tether."
  exit 1
fi

if [ -z "$TAG" ]; then
  echo "Could not determine the latest release. Check https://github.com/$REPO/releases"
  exit 1
fi

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${TAG}/${ASSET}"

# --- download binary ---

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

echo "Downloading $ASSET ($TAG)..."
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$DOWNLOAD_URL" -o "$TMP"
else
  wget -qO "$TMP" "$DOWNLOAD_URL"
fi

chmod +x "$TMP"

# --- install binary ---

if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "${INSTALL_DIR}/${BINARY}"
else
  echo "Installing to $INSTALL_DIR (sudo required)..."
  sudo mv "$TMP" "${INSTALL_DIR}/${BINARY}"
fi

echo "Installed tether $TAG to ${INSTALL_DIR}/${BINARY}"

# --- macOS: clear quarantine flag set by Gatekeeper ---

if [ "$OS_KEY" = "darwin" ]; then
  xattr -d com.apple.quarantine "${INSTALL_DIR}/${BINARY}" 2>/dev/null || true
fi

# --- set up shell integration ---

echo ""
echo "Running shell integration setup..."
"${INSTALL_DIR}/${BINARY}" install

echo ""
echo "Tether is ready. Open a new terminal and run:"
echo "  tether shell"
