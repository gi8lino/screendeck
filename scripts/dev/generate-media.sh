#!/bin/sh
# Generate deterministic synthetic media fixtures for local Plex and Jellyfin smoke tests.
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
OUTPUT_DIR=${1:-"$REPO_ROOT/test/smoke/media/generated"}
FFMPEG=${FFMPEG:-ffmpeg}

if [ "$#" -gt 1 ]; then
  echo "usage: $0 [output-directory]" >&2
  exit 2
fi

output_parent=$(dirname -- "$OUTPUT_DIR")
output_name=$(basename -- "$OUTPUT_DIR")
if [ "$output_name" = "/" ] || [ "$output_name" = "." ] || [ "$output_name" = ".." ]; then
  echo "output directory must name a directory: $OUTPUT_DIR" >&2
  exit 2
fi
mkdir -p "$output_parent"
output_parent=$(CDPATH= cd -- "$output_parent" && pwd)
OUTPUT_DIR="$output_parent/$output_name"

if ! command -v "$FFMPEG" >/dev/null 2>&1; then
  echo "FFmpeg is required to generate smoke media: $FFMPEG" >&2
  echo "Install ffmpeg or run 'make smoke-media FFMPEG=/path/to/ffmpeg'." >&2
  exit 1
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/screendeck-smoke-media.XXXXXX")
cleanup() {
  rm -rf "$work"
}
trap cleanup EXIT INT TERM

make_video() {
  output=$1
  duration=$2
  color=$3
  title=$4
  year=$5
  genre=$6

  mkdir -p "$(dirname -- "$output")"
  "$FFMPEG" \
    -hide_banner \
    -loglevel error \
    -y \
    -f lavfi \
    -i "color=c=${color}:s=160x90:r=0.2:d=${duration}" \
    -c:v mpeg4 \
    -q:v 31 \
    -pix_fmt yuv420p \
    -an \
    -metadata "title=${title}" \
    -metadata "date=${year}" \
    -metadata "genre=${genre}" \
    -movflags +faststart \
    "$output"
}

make_poster() {
  output=$1
  color=$2

  "$FFMPEG" \
    -hide_banner \
    -loglevel error \
    -y \
    -f lavfi \
    -i "color=c=${color}:s=600x900:d=1" \
    -frames:v 1 \
    "$output"
}

make_movie() {
  title=$1
  year=$2
  genre=$3
  duration=$4
  color=$5
  folder="$work/movies/$title ($year)"

  mkdir -p "$folder"
  make_video "$folder/$title ($year).mp4" "$duration" "$color" "$title" "$year" "$genre"
  make_poster "$folder/poster.jpg" "$color"
  cat >"$folder/movie.nfo" <<EOF
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
  <title>$title</title>
  <year>$year</year>
  <genre>$genre</genre>
  <plot>Synthetic ScreenDeck smoke-test fixture.</plot>
</movie>
EOF
}

make_episode() {
  episode=$1
  title=$2
  duration=$3
  color=$4
  folder="$work/tv/Smoke Show/Season 01"
  base="Smoke Show - S01E0${episode}"

  mkdir -p "$folder"
  make_video "$folder/$base.mp4" "$duration" "$color" "$title" "2024" "Drama"
  cat >"$folder/$base.nfo" <<EOF
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<episodedetails>
  <title>$title</title>
  <showtitle>Smoke Show</showtitle>
  <season>1</season>
  <episode>$episode</episode>
  <year>2024</year>
  <plot>Synthetic ScreenDeck smoke-test episode.</plot>
</episodedetails>
EOF
}

printf '%s\n' 'Generating ScreenDeck smoke media...'

make_movie "Smoke Action" "2020" "Action" "5400" "0xff5a5f"
make_movie "Smoke Comedy" "2021" "Comedy" "5700" "0xf4a261"
make_movie "Smoke Drama" "2022" "Drama" "6600" "0x457b9d"
make_movie "Smoke Horror" "2023" "Horror" "6300" "0x6d597a"
make_movie "Smoke Old Movie" "1985" "Adventure" "4800" "0x2a9d8f"
make_movie "Smoke Long Movie" "2024" "Science Fiction" "9000" "0x264653"

show_dir="$work/tv/Smoke Show"
mkdir -p "$show_dir"
make_poster "$show_dir/poster.jpg" "0x8d99ae"
cat >"$show_dir/tvshow.nfo" <<'EOF'
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<tvshow>
  <title>Smoke Show</title>
  <year>2024</year>
  <genre>Drama</genre>
  <plot>Synthetic ScreenDeck smoke-test TV fixture.</plot>
</tvshow>
EOF
make_episode "1" "Pilot" "2700" "0x8d99ae"
make_episode "2" "Second Round" "3000" "0xb8c0ff"

rm -rf "$OUTPUT_DIR"
mkdir -p "$(dirname -- "$OUTPUT_DIR")"
mv "$work" "$OUTPUT_DIR"
trap - EXIT INT TERM

printf 'Generated smoke media in %s\n' "$OUTPUT_DIR"
