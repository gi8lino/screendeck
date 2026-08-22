#!/bin/sh
# Build the generated frontend distribution from web/src.
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
SOURCE_DIR="$ROOT_DIR/web/src"
TEMPLATE_DIR="$SOURCE_DIR/templates"
OUTPUT_DIR="$ROOT_DIR/web/dist"
INDEX_SOURCE="$SOURCE_DIR/index.html"
MARKER='    <!-- SCREENDECK_TEMPLATES -->'

if [ "$#" -ne 0 ]; then
  echo "usage: $0" >&2
  exit 2
fi

for required in \
  "$INDEX_SOURCE" \
  "$SOURCE_DIR/favicon.svg" \
  "$SOURCE_DIR/css/app.css" \
  "$SOURCE_DIR/js/app.js"; do
  if [ ! -f "$required" ]; then
    echo "frontend source not found: $required" >&2
    exit 1
  fi
done

if [ ! -d "$TEMPLATE_DIR" ]; then
  echo "frontend template directory not found: $TEMPLATE_DIR" >&2
  exit 1
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/screendeck-web.XXXXXX")
cleanup() {
  rm -rf "$work"
}
trap cleanup EXIT INT TERM

mkdir -p "$work/css" "$work/js"
cp "$SOURCE_DIR/favicon.svg" "$work/favicon.svg"
cp "$SOURCE_DIR/css/app.css" "$work/css/app.css"
cp "$SOURCE_DIR/js/"*.js "$work/js/"

marker_count=0
while IFS= read -r line || [ -n "$line" ]; do
  if [ "$line" = "$MARKER" ]; then
    marker_count=$((marker_count + 1))
    found_template=false
    for template in "$TEMPLATE_DIR"/*.html; do
      if [ ! -f "$template" ]; then
        continue
      fi
      found_template=true
      cat "$template" >>"$work/index.html"
      printf '\n' >>"$work/index.html"
    done
    if [ "$found_template" = false ]; then
      echo "no frontend templates found in $TEMPLATE_DIR" >&2
      exit 1
    fi
  else
    printf '%s\n' "$line" >>"$work/index.html"
  fi
done <"$INDEX_SOURCE"

if [ "$marker_count" -ne 1 ]; then
  echo "expected exactly one SCREENDECK_TEMPLATES marker in $INDEX_SOURCE" >&2
  exit 1
fi

rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"
cp -R "$work"/. "$OUTPUT_DIR"/
