#!/bin/bash
set -e

APP_NAME="nawala-checker"

# Finding Go binary
if [ -x "/usr/local/go/bin/go" ]; then
    GO_BIN="/usr/local/go/bin/go"
elif command -v go >/dev/null 2>&1; then
    GO_BIN="$(command -v go)"
else
    echo "[ERR] Go binary not found!"
    echo "Install Go or make sure go is in PATH"
    exit 1
fi

echo "[i] Using Go binary: $GO_BIN"
"$GO_BIN" version

BUILD_DIR="Builds"
mkdir -p "$BUILD_DIR"

if ! command -v wails >/dev/null 2>&1; then
    echo "[i] Installing Wails CLI..."
    "$GO_BIN" install github.com/wailsapp/wails/v2/cmd/wails@latest
fi
export PATH="$("$GO_BIN" env GOPATH)/bin:$PATH"

echo "[i] Building Wails for MacOS (Universal)..."
wails build -platform darwin/universal -clean

echo "[i] Building Wails for Windows (amd64)..."
wails build -platform windows/amd64

echo "[i] Copying builds to $BUILD_DIR..."
cp build/bin/nawala-checker.app "$BUILD_DIR/" 2>/dev/null || cp -r build/bin/nawala-checker.app "$BUILD_DIR/" 2>/dev/null || true
cp build/bin/nawala-checker.exe "$BUILD_DIR/" 2>/dev/null || true
cp build/bin/nawala-checker "$BUILD_DIR/" 2>/dev/null || true

echo "[OK] Cross-compilation finished! Binaries are in $BUILD_DIR/"