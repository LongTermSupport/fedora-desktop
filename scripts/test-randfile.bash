#!/usr/bin/env bash
# Unit-test randfile (files/home/.local/bin/randfile): one random regular, non-hidden file of a
# directory on stdout, the current directory by default, and every refusal on stderr with a
# non-zero exit and nothing on stdout, so `open "$(randfile)"` never opens a wrong path.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOOL="$(cd "$SCRIPT_DIR/.." && pwd)/files/home/.local/bin/randfile"
if [ ! -x "$TOOL" ]; then
    echo "FAIL: $TOOL does not exist or is not executable" >&2
    exit 1
fi

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT
OUT="$WORK_DIR/out"
ERR="$WORK_DIR/err"

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "ok: $label"
    else
        failed=$((failed + 1))
        echo "FAIL: $label: want [$want], got [$got]"
    fi
}

d="$WORK_DIR/pics"
mkdir -p "$d/sub dir" "$d/.hidden-dir"
touch "$d/a picture.jpg" "$d/b.png" "$d/.hidden" "$d/sub dir/deep.jpg" "$d/.hidden-dir/x.jpg"
ln -s "$d/b.png" "$d/link.png"

seen=""
for _ in $(seq 1 60); do
    out="$("$TOOL" "$d" 2>"$ERR")" || out="exit $?: $(cat "$ERR")"
    case "$out" in
        "$d/a picture.jpg" | "$d/b.png") seen="$seen|$out" ;;
        *)
            check "every pick is a plain, non-hidden file at the top level" "$d/a picture.jpg or $d/b.png" "$out"
            break
            ;;
    esac
done
check "both files are picked over 60 runs (a space in a name survives)" "yes yes" \
    "$([[ $seen == *"|$d/a picture.jpg"* ]] && echo yes || echo no) $([[ $seen == *"|$d/b.png"* ]] && echo yes || echo no)"

out="$(cd "$d/sub dir" && "$TOOL")"
check "no argument picks from the current directory" "./deep.jpg" "$out"

mkdir "$WORK_DIR/empty"
touch "$WORK_DIR/empty/.only-hidden"
out="$("$TOOL" "$WORK_DIR/empty" 2>"$ERR")"
check "a directory with no files fails" "1" "$?"
check "  with nothing on stdout" "" "$out"
check "  naming the directory" "randfile: no files in $WORK_DIR/empty" "$(cat "$ERR")"

out="$("$TOOL" "$WORK_DIR/missing" 2>"$ERR")"
check "a path that is not a directory fails" "1" "$?"
check "  with nothing on stdout" "" "$out"
check "  saying so" "randfile: not a directory: $WORK_DIR/missing" "$(cat "$ERR")"

"$TOOL" "$d" "$d" >"$OUT" 2>"$ERR"
check "two arguments are a usage error" "64" "$?"
check "  with the usage on stderr only" "yes" "$([[ ! -s $OUT && $(cat "$ERR") == usage:* ]] && echo yes || echo no)"
"$TOOL" --bogus >"$OUT" 2>"$ERR"
check "an unknown option is a usage error" "64" "$?"
check "  naming it" "randfile: unknown option: --bogus" "$(awk 'NR==1' "$ERR")"
out="$("$TOOL" --help)"
check "--help prints the usage on stdout" "yes" "$([[ $out == usage:* ]] && echo yes || echo no)"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
