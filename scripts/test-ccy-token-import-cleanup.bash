#!/usr/bin/env bash
# Test the import script `ccy --export-token NAME` prints: it writes NAME.DATE.token on the
# target, then offers to remove the OTHER files of that name.
#
# export_token runs from THIS repo's library against a scratch token pool holding a
# placeholder, and its output is run exactly as a person pastes it, with HOME pointed at a
# scratch target pool. No real credential is involved.
#
# WHAT IT PINS. A name launches with its earliest-dated file, so an old (often expired) file
# left beside an imported renewal shadows it. The import lists the other <name>.<date>.token
# files and removes them only on a yes: CCY_TOKEN_IMPORT_REMOVE_OTHERS=1, or `y` at the
# prompt on the terminal. It never touches an undated file, another name, or a file whose
# name merely starts with this one; it keeps everything on 0, on `n`, on an empty answer and
# when there is no terminal to ask; an unknown value refuses before anything is written; and
# the token value never appears in the output.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"

for lib in common-pure token-management; do
    if [ ! -f "$LIB_DIR/$lib.bash" ]; then
        echo "FAIL: library not found at $LIB_DIR/$lib.bash" >&2
        exit 1
    fi
done
if ! command -v script > /dev/null; then
    echo "FAIL: script(1) is needed to answer the prompt on a terminal" >&2
    exit 1
fi

WORK="$(mktemp -d "$REPO_ROOT/untracked/ccy-token-import-cleanup.XXXXXX")"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT INT TERM

NEW="$(date -d '+365 days' +%Y-%m-%d)"
OLD="$(date -d '-10 days' +%Y-%m-%d)"
OLDER="$(date -d '-100 days' +%Y-%m-%d)"
SECRET="placeholder-import-value"

SRC="$WORK/source/tokens"
mkdir -p "$SRC"
printf '%s' "$SECRET" >"$SRC/work.$NEW.token"
printf '%s' "$SECRET" >"$SRC/bad.name.$NEW.token"

SNIPPET="$WORK/import.bash"
if ! bash -c "
    source '$LIB_DIR/common-pure.bash'
    source '$LIB_DIR/token-management.bash'
    export_token '$SRC' work
" >"$SNIPPET" 2>"$WORK/export-stderr"; then
    echo "FAIL: export_token failed: $(cat "$WORK/export-stderr")" >&2
    exit 1
fi

PASSED=0
FAILED=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        PASSED=$((PASSED + 1))
        printf '  PASS  %s\n' "$label"
    else
        FAILED=$((FAILED + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got"
    fi
}
has() { if [[ "$2" == *"$1"* ]]; then echo yes; else echo no; fi; }

# new_target <name> — sets H to a target home whose pool (POOL) already holds two older
# files of this name, plus files the cleanup must never touch.
H=""
POOL=""
new_target() {
    H="$WORK/$1"
    POOL="$H/.claude-tokens/ccy/tokens"
    mkdir -p "$POOL"
    local f
    for f in "work.$OLD.token" "work.$OLDER.token" "${UNTOUCHED[@]}"; do
        printf 'placeholder-existing\n' >"$POOL/$f"
    done
}
pool() { find "$POOL" -mindepth 1 -printf '%f\n' | sort | xargs; }
listed() { printf '%s\n' "$@" | sort | xargs; }
UNTOUCHED=("work.backup.token" "work.$OLD.token.bak" "workx.$OLD.token" "other.$OLD.token" "work.extra.$OLD.token")
ALL_KEPT="$(listed "${UNTOUCHED[@]}" "work.$NEW.token" "work.$OLD.token" "work.$OLDER.token")"
CLEANED="$(listed "${UNTOUCHED[@]}" "work.$NEW.token")"

OUT=""
RC=0
# run_import <home> [env assignment] — the pasted snippet, no terminal.
run_import() {
    local home="$1" extra="${2:-}"
    local -a envs=(HOME="$home")
    [ -n "$extra" ] && envs+=("$extra")
    RC=0
    OUT=$(env -u CCY_TOKEN_IMPORT_REMOVE_OTHERS "${envs[@]}" bash "$SNIPPET" </dev/null 2>&1) || RC=$?
}
# run_on_tty <home> <answer> — the pasted snippet on a terminal, answering the prompt.
run_on_tty() {
    local home="$1" answer="$2"
    RC=0
    OUT=$(printf '%s\n' "$answer" | env -u CCY_TOKEN_IMPORT_REMOVE_OTHERS HOME="$home" \
        script -qec "bash $(printf '%q' "$SNIPPET")" /dev/null 2>&1) || RC=$?
    OUT="${OUT//$'\r'/}"
}

echo ""
echo "=== the snippet ==="
check "export names the switch in its header" "yes" "$(has "CCY_TOKEN_IMPORT_REMOVE_OTHERS=1 removes" "$(cat "$SNIPPET")")"
RC=0
bash -c "
    source '$LIB_DIR/common-pure.bash'
    source '$LIB_DIR/token-management.bash'
    export_token '$SRC' bad.name
" >/dev/null 2>"$WORK/bad-stderr" || RC=$?
check "a name outside letters, digits, _ and - is not exported" "1" "$RC"

echo ""
echo "=== CCY_TOKEN_IMPORT_REMOVE_OTHERS=1 removes exactly the other dated files of the name ==="
new_target remove
run_import "$H" CCY_TOKEN_IMPORT_REMOVE_OTHERS=1
check "exit 0" "0" "$RC"
check "pool afterwards" "$CLEANED" "$(pool)"
check "the new file holds the token" "$SECRET" "$(cat "$POOL/work.$NEW.token")"
check "says which files it removed" "yes yes" \
    "$(has "Removed work.$OLD.token" "$OUT") $(has "Removed work.$OLDER.token" "$OUT")"
check "the token value is never printed" "no" "$(has "$SECRET" "$OUT")"

echo ""
echo "=== everything is kept unless the answer is yes ==="
new_target keep
run_import "$H" CCY_TOKEN_IMPORT_REMOVE_OTHERS=0
check "=0: exit 0" "0" "$RC"
check "=0: nothing removed" "$ALL_KEPT" "$(pool)"
check "=0: lists them and says they were kept" "yes yes" "$(has "  work.$OLDER.token" "$OUT") $(has "Kept them." "$OUT")"

new_target notty
run_import "$H"
check "no terminal: exit 0" "0" "$RC"
check "no terminal: nothing removed" "$ALL_KEPT" "$(pool)"
check "no terminal: says why and how to remove" "yes" "$(has "No terminal to ask, so they were kept" "$OUT")"

new_target tty-no
run_on_tty "$H" n
check "prompt answered n: exit 0" "0" "$RC"
check "prompt answered n: asked" "yes" "$(has "Remove them? [y/N]" "$OUT")"
check "prompt answered n: nothing removed" "$ALL_KEPT" "$(pool)"

new_target tty-empty
run_on_tty "$H" ""
check "prompt answered with Enter (the default is no): nothing removed" "$ALL_KEPT" "$(pool)"

new_target tty-yes
run_on_tty "$H" y
check "prompt answered y: exit 0" "0" "$RC"
check "prompt answered y: only the other dated files removed" "$CLEANED" "$(pool)"
check "prompt answered y: the token value is never printed" "no" "$(has "$SECRET" "$OUT")"

echo ""
echo "=== an unknown value refuses before anything is written ==="
new_target bad-value
run_import "$H" CCY_TOKEN_IMPORT_REMOVE_OTHERS=maybe
check "exit 64" "64" "$RC"
check "nothing written or removed" \
    "$(listed "${UNTOUCHED[@]}" "work.$OLD.token" "work.$OLDER.token")" "$(pool)"

echo ""
echo "=== nothing to offer when the name has no other file ==="
H="$WORK/fresh"
mkdir -p "$H"
run_import "$H"
check "exit 0" "0" "$RC"
check "no question, no list" "no no" "$(has "Other files" "$OUT") $(has "Kept them" "$OUT")"
POOL="$H/.claude-tokens/ccy/tokens"
check "the one file" "work.$NEW.token" "$(pool)"

echo ""
printf 'passed: %d   failed: %d\n' "$PASSED" "$FAILED"
if [ "$PASSED" -eq 0 ]; then
    echo "FAIL: no case passed" >&2
    exit 1
fi
if [ "$FAILED" -ne 0 ]; then
    exit 1
fi
