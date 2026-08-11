#!/bin/sh
# LaunchAgent entrypoint for Shelley serve (login autostart).
# Keeps PATH usable under launchd's sparse environment.
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
BIN="$ROOT/bin/shelley"
PORT="${SHELLEY_PORT:-9000}"
LOG_DIR="${SHELLEY_LOG_DIR:-$HOME/.config/shelley/logs}"

export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin${PATH:+:$PATH}"
export HOME="${HOME:-$(cd && pwd)}"

if [ ! -x "$BIN" ]; then
  echo "ERROR: shelley binary missing or not executable: $BIN" >&2
  exit 1
fi

mkdir -p "$LOG_DIR"
cd "$ROOT"
exec "$BIN" serve -port "$PORT"
