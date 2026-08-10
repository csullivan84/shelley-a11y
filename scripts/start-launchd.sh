#!/bin/sh
# LaunchAgent entrypoint for Shelley serve (login autostart).
# Keeps PATH usable under launchd's sparse environment and pulls DEEPSEEK_API_KEY
# from ModelDock's .env when present so the builtin DeepSeek models materialize.
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

# Optional: reuse ModelDock's DeepSeek key without duplicating it into this plist.
if [ -z "${DEEPSEEK_API_KEY:-}" ] && [ -f "$HOME/.modeldock/.env" ]; then
  KEY="$(sed -n 's/^DEEPSEEK_API_KEY=//p' "$HOME/.modeldock/.env" | tail -n 1 | tr -d '\r')"
  # Strip surrounding quotes if the .env used them.
  case "$KEY" in
    \"*\") KEY="${KEY#\"}"; KEY="${KEY%\"}" ;;
    \'*\') KEY="${KEY#\'}"; KEY="${KEY%\'}" ;;
  esac
  [ -n "$KEY" ] && export DEEPSEEK_API_KEY="$KEY"
fi

mkdir -p "$LOG_DIR"
cd "$ROOT"
exec "$BIN" serve -port "$PORT"
