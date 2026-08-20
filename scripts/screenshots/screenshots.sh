#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)

OUTPUT_DIR=${SCREENSHOT_DIR:-"$ROOT_DIR/docs/screenshots"}
HOST=${SCREENSHOT_HOST:-127.0.0.1}
PORT=${SCREENSHOT_PORT:-18080}
PLAYWRIGHT=${PLAYWRIGHT:-"$ROOT_DIR/node_modules/.bin/playwright"}

BASE_URL="http://$HOST:$PORT"

if ! command -v node >/dev/null 2>&1; then
  echo "Node.js is required to create screenshots." >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required to create screenshots." >&2
  exit 1
fi

if [ ! -x "$PLAYWRIGHT" ]; then
  echo "Playwright is not installed." >&2
  echo "Run 'npm ci' first." >&2
  exit 1
fi

mkdir -p "$OUTPUT_DIR"

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

  if [ "$attempt" -ge 50 ]; then
    cat "$SERVER_LOG" >&2
    echo "Screenshot demo server did not become ready." >&2
    exit 1
  fi

  sleep 0.1
done

capture() {
  name=$1
  route=$2

  "$PLAYWRIGHT" screenshot \
    --wait-for-timeout 1200 \
    --viewport-size "1440,1050" \
    "$BASE_URL$route" \
    "$OUTPUT_DIR/$name.png" \
    >/dev/null

  echo "Created docs/screenshots/$name.png"
}

capture home "/"
capture room "/demo/host"
capture winner "/demo/winner"
