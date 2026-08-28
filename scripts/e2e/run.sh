#!/bin/sh
# Run browser behavior tests against the deterministic ScreenDeck E2E server.
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
HOST=${E2E_HOST:-127.0.0.1}
PORT=${E2E_PORT:-18081}
BASE_URL="http://$HOST:$PORT"

if [ ! -d "$ROOT_DIR/docs/node_modules/playwright" ]; then
  echo "Playwright is not installed. Run 'make node-dependencies' first." >&2
  exit 1
fi

TMP_DIR=$(mktemp -d)
SERVER_LOG="$TMP_DIR/server.log"
node "$SCRIPT_DIR/server.mjs" --host "$HOST" --port "$PORT" >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

cleanup() {
  kill "$SERVER_PID" >/dev/null 2>&1 || true
  wait "$SERVER_PID" >/dev/null 2>&1 || true
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

attempt=0
until curl -fsS "$BASE_URL/healthz" >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if ! kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    cat "$SERVER_LOG" >&2
    exit 1
  fi
  if [ "$attempt" -ge 50 ]; then
    cat "$SERVER_LOG" >&2
    echo "E2E server did not become ready." >&2
    exit 1
  fi
  sleep 0.1
done

E2E_BASE_URL="$BASE_URL" node --test "$ROOT_DIR"/test/e2e/*.test.mjs
