#!/usr/bin/env bash
# Run by the makeself installer after self-extraction. Picks the binary
# matching this machine's architecture, installs it as ~/.local/bin/linuxai,
# and drops a starter .env if one doesn't already exist.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="$HOME/.local/bin"
CONFIG_DIR="$HOME/.config/linuxai"

arch="$(uname -m)"
case "$arch" in
	x86_64 | amd64)
		bin="linuxai-amd64"
		;;
	aarch64 | arm64)
		bin="linuxai-arm64"
		;;
	*)
		echo "linuxai: unsupported architecture '$arch'" >&2
		exit 1
		;;
esac

if [ ! -f "$SCRIPT_DIR/$bin" ]; then
	echo "linuxai: expected binary '$bin' not found in installer package" >&2
	exit 1
fi

mkdir -p "$INSTALL_DIR"
cp "$SCRIPT_DIR/$bin" "$INSTALL_DIR/linuxai"
chmod +x "$INSTALL_DIR/linuxai"
installed_version="$("$INSTALL_DIR/linuxai" --version 2>/dev/null || echo unknown)"
echo "Installed $installed_version ($arch) to $INSTALL_DIR/linuxai"

mkdir -p "$CONFIG_DIR"
if [ -f "$CONFIG_DIR/.env" ]; then
	echo "$CONFIG_DIR/.env already exists, leaving it untouched."
else
	cp "$SCRIPT_DIR/.env.example" "$CONFIG_DIR/.env"
	echo "Created $CONFIG_DIR/.env from the template. Edit it to add your NVIDIA_API_KEY."
fi

case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*)
		echo
		echo "Note: $INSTALL_DIR is not on your PATH. Add this to your ~/.bashrc or ~/.zshrc:"
		echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
		;;
esac

echo
echo "Done. Edit $CONFIG_DIR/.env, then run: linuxai \"your question\""
