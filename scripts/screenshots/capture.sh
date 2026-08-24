#!/bin/sh
# Capture deterministic raw documentation screenshots from the ScreenDeck demo server.
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
DOCS_DIR="$ROOT_DIR/docs"

RAW_DIR=${SCREENSHOT_RAW_DIR:-"$DOCS_DIR/screenshots/raw"}
HOST=${SCREENSHOT_HOST:-127.0.0.1}
PORT=${SCREENSHOT_PORT:-18080}
PLAYWRIGHT=${PLAYWRIGHT:-"$DOCS_DIR/node_modules/.bin/playwright"}
PLAYWRIGHT_BROWSERS_PATH=${PLAYWRIGHT_BROWSERS_PATH:-"$DOCS_DIR/.playwright"}

export PLAYWRIGHT_BROWSERS_PATH

BASE_URL="http://$HOST:$PORT"

if ! command -v node >/dev/null 2>&1; then
  echo "Node.js is required to capture screenshots." >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required to capture screenshots." >&2
  exit 1
fi

if [ ! -x "$PLAYWRIGHT" ]; then
  echo "Playwright is not installed." >&2
  echo "Run 'make node-dependencies' first." >&2
  exit 1
fi

if [ ! -d "$PLAYWRIGHT_BROWSERS_PATH" ]; then
  echo "The local Playwright browser cache does not exist." >&2
  echo "Run 'make playwright-browser' first." >&2
  exit 1
fi

mkdir -p "$RAW_DIR"

TMP_DIR=$(mktemp -d)
SERVER_LOG="$TMP_DIR/server.log"

node "$SCRIPT_DIR/server.mjs" \
  --host "$HOST" \
  --port "$PORT" \
  >"$SERVER_LOG" 2>&1 &

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
    echo "Screenshot demo server exited unexpectedly." >&2
    exit 1
  fi

  if [ "$attempt" -ge 50 ]; then
    cat "$SERVER_LOG" >&2
    echo "Screenshot demo server did not become ready." >&2
    exit 1
  fi

  sleep 0.1
done

# Verify that the frontend itself is available, not only the health endpoint.
if ! curl -fsS "$BASE_URL/" >/dev/null 2>&1; then
  cat "$SERVER_LOG" >&2
  echo "Screenshot demo frontend is not available." >&2
  exit 1
fi

capture() {
  name=$1
  route=$2

  if ! "$PLAYWRIGHT" screenshot \
    --wait-for-timeout 1200 \
    --viewport-size "1440,1050" \
    "$BASE_URL$route" \
    "$RAW_DIR/$name.png" \
    >/dev/null; then
    cat "$SERVER_LOG" >&2
    echo "Failed to capture $name screenshot." >&2
    exit 1
  fi

  echo "Captured docs/screenshots/raw/$name.png"
}

capture home "/demo/home"
capture media-setup "/demo/media-setup"
capture create-room "/demo/create"
capture room "/demo/host"
capture winner "/demo/winner"

if cmp -s "$RAW_DIR/home.png" "$RAW_DIR/create-room.png"; then
  echo "Screenshot regression: home.png and create-room.png are identical." >&2
  exit 1
fi

if cmp -s "$RAW_DIR/home.png" "$RAW_DIR/media-setup.png"; then
  echo "Screenshot regression: home.png and media-setup.png are identical." >&2
  exit 1
fi

if cmp -s "$RAW_DIR/home.png" "$RAW_DIR/room.png"; then
  echo "Screenshot regression: home.png and room.png are identical." >&2
  exit 1
fi

if cmp -s "$RAW_DIR/home.png" "$RAW_DIR/winner.png"; then
  echo "Screenshot regression: home.png and winner.png are identical." >&2
  exit 1
fi

if cmp -s "$RAW_DIR/room.png" "$RAW_DIR/winner.png"; then
  echo "Screenshot regression: room.png and winner.png are identical." >&2
  exit 1
fi
