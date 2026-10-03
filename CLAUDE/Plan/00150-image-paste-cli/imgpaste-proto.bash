#!/usr/bin/env bash
# Prototype: encode an image as a pasteable, self-decoding bash block.
# stdout = the block; everything else goes to stderr.
set -euo pipefail

readonly MAX_INPUT_BYTES=$((50 * 1024 * 1024))
readonly MAX_INPUT_PIXELS=$((80 * 1000 * 1000))
readonly MAX_EDGE=2000 # the Read tool displays at most 2000px wide
readonly QUALITY="${IMGPASTE_QUALITY:-50}"
readonly WARN_BLOCK_BYTES=$((100 * 1024))

die() { echo "imgpaste: $*" >&2; exit 1; }

[[ $# -eq 1 ]] || die "usage: imgpaste <image-file>"
input="$1"
[[ -f "$input" ]] || die "not a file: $input"

mime=$(file --brief --mime-type -- "$input")
[[ "$mime" == image/* ]] || die "not an image ($mime): $input"

bytes=$(stat -c %s -- "$input")
((bytes <= MAX_INPUT_BYTES)) || die "input is $bytes bytes, limit is $MAX_INPUT_BYTES"

first_frame="${input}[0]" # animated/multi-page inputs: encode frame 0 only
read -r width height < <(magick identify -format '%w %h\n' -- "$first_frame")
((width * height <= MAX_INPUT_PIXELS)) || die "input is ${width}x${height}, limit is $MAX_INPUT_PIXELS pixels"

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT
out="$workdir/out.webp"

magick -- "$first_frame" -auto-orient -strip -resize "${MAX_EDGE}x${MAX_EDGE}>" \
	-quality "$QUALITY" -define webp:method=6 "$out"

sha=$(sha256sum -- "$out" | cut -d' ' -f1)
id=${sha:0:12}
path="./imgpaste-$id.webp" # cwd-relative: the receiver may not be allowed to write outside its project
read -r ow oh < <(magick identify -format '%w %h\n' -- "$out")

block=$(
	echo "# IMGPASTE v1: ${ow}x${oh} image. Agent: run this whole block in bash, then view ${path}"
	echo "base64 -d > ${path} <<'IMGPASTE_END'"
	base64 -w 76 -- "$out"
	echo "IMGPASTE_END"
	echo "echo '${sha}  ${path}' | sha256sum -c"
)
printf '%s\n' "$block"

block_bytes=${#block}
# The terminal shows stderr beside the block and a copy often takes it too; as bash
# comments these lines cannot fail the receiver's run.
echo "# imgpaste: ${width}x${height} -> ${ow}x${oh} webp q${QUALITY}, $(stat -c %s -- "$out") bytes, block ${block_bytes} chars" >&2
((block_bytes <= WARN_BLOCK_BYTES)) || echo "# imgpaste: WARNING block exceeds ${WARN_BLOCK_BYTES} chars; try IMGPASTE_QUALITY=30" >&2
