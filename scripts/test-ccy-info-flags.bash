#!/usr/bin/env bash
# Test that ccy's informational options work from any directory, and that everything else
# still refuses to run outside a git repository root.
#
# Runs the real launcher from THIS repo (not the deployed /var/local copy), from a scratch
# directory that is not a repository, with HOME pointed at a scratch home and the container
# engine replaced by a stub on PATH that refuses every call, so no real engine, image or
# token store is touched.
#
# WHY THIS EXISTS. The repository-root check ran before any option was read, so asking
# `ccy --version` from a home directory failed with "Not in a git repository root
# directory". An option that only prints and exits operates on no project, so it must not
# need one: --version/-v, --help/-h and a bare --list-tokens. Anything that starts or
# changes a session keeps the check, so the refusal cases matter as much as the passes.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"

if [ ! -f "$LAUNCHER" ]; then
    echo "FAIL: launcher not found at $LAUNCHER" >&2
    exit 1
fi

WANT_VERSION=$(grep -m1 '^CCY_VERSION=' "$LAUNCHER" | cut -d'"' -f2)
if [ -z "$WANT_VERSION" ]; then
    echo "FAIL: could not read CCY_VERSION from $LAUNCHER" >&2
    exit 1
fi

SCRATCH="$(mktemp -d)"
trap 'rm -rf "$SCRATCH"' EXIT
NOT_A_REPO="$SCRATCH/not-a-repo"
FAKE_HOME="$SCRATCH/home"
STUB_BIN="$SCRATCH/bin"
ENGINE_CALLS="$SCRATCH/engine-calls"
mkdir -p "$NOT_A_REPO" "$FAKE_HOME" "$STUB_BIN"
: >"$ENGINE_CALLS"

# The engine stub: records each call and refuses it, so --help takes its "image not built"
# branch and nothing ever reaches a real engine.
cat >"$STUB_BIN/podman" <<STUB
#!/usr/bin/env bash
printf '%s\n' "\$*" >>"$ENGINE_CALLS"
exit 1
STUB
chmod 755 "$STUB_BIN/podman"

# One token fixture so --list-tokens has something to list. Only the filename is read.
TOKEN_EXPIRY="$(date -d '+365 days' +%Y-%m-%d)"
mkdir -p "$FAKE_HOME/.claude-tokens/ccy/tokens"
printf 'placeholder-not-a-real-token\n' >"$FAKE_HOME/.claude-tokens/ccy/tokens/fixture.$TOKEN_EXPIRY.token"

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got"
    fi
}
says() { if grep -qF -- "$1" <<<"$2"; then echo yes; else echo no; fi; }

OUT=""
ERR=""
RC=0
# run_ccy <args...> — runs the launcher from the non-repo directory under the scratch home.
run_ccy() {
    RC=0
    OUT=$(cd "$NOT_A_REPO" && env HOME="$FAKE_HOME" PATH="$STUB_BIN:$PATH" \
        CCY_CONTAINER_ENGINE=podman bash "$LAUNCHER" "$@" 2>"$SCRATCH/stderr") || RC=$?
    ERR=$(cat "$SCRATCH/stderr")
}
dir_untouched() {
    if [ -z "$(find "$NOT_A_REPO" -mindepth 1 -print -quit)" ]; then echo yes; else echo no; fi
}

echo ""
echo "=== informational options work outside a repository ==="

for flag in --version -v; do
    run_ccy "$flag"
    check "ccy $flag exits 0 outside a repository" "0" "$RC"
    check "ccy $flag prints the launcher's own version" "yes" "$(says "ccy version $WANT_VERSION" "$OUT")"
    check "ccy $flag prints the integrity hash" "yes" "$(says "Hash: " "$OUT")"
    check "ccy $flag says nothing about a repository" "no" "$(says "Not in a git repository" "$ERR")"
    check "ccy $flag writes nothing into the directory" "yes" "$(dir_untouched)"
done

for flag in --help -h; do
    run_ccy "$flag"
    check "ccy $flag exits 0 outside a repository" "0" "$RC"
    check "ccy $flag prints the usage" "yes" "$(says "Usage: ccy [OPTIONS] [CLAUDE_ARGS...]" "$OUT")"
    check "ccy $flag reports the image is not built (stub engine)" "yes" "$(says "Container image not built yet" "$OUT")"
    check "ccy $flag says nothing about a repository" "no" "$(says "Not in a git repository" "$ERR")"
    check "ccy $flag writes nothing into the directory" "yes" "$(dir_untouched)"
done

run_ccy --list-tokens
check "ccy --list-tokens exits 0 outside a repository" "0" "$RC"
check "ccy --list-tokens lists the fixture token" "yes" "$(says "fixture" "$OUT")"
check "ccy --list-tokens says nothing about a repository" "no" "$(says "Not in a git repository" "$ERR")"
check "ccy --list-tokens writes nothing into the directory" "yes" "$(dir_untouched)"

echo ""
echo "=== anything that operates on a project still needs a repository root ==="

# A bare launch, a launch with a claude argument, an option that changes a project, and
# --list-tokens alongside a launch option: the informational exemption is for the option
# on its own, so a combined command line goes through the full check as before.
for args in "" "--rebuild" "--prevent" "--custom" "--list-tokens --engine podman" "fix the bug"; do
    if [ -z "$args" ]; then
        run_ccy
        label="ccy (no arguments)"
    elif [ "$args" = "fix the bug" ]; then
        run_ccy "fix the bug"
        label="ccy \"fix the bug\""
    else
        read -r -a argv <<<"$args"
        run_ccy "${argv[@]}"
        label="ccy $args"
    fi
    check "$label exits 1 outside a repository" "1" "$RC"
    check "$label names the repository-root error" "yes" "$(says "Not in a git repository root directory" "$ERR")"
    check "$label writes nothing into the directory" "yes" "$(dir_untouched)"
done

check "no case reached a container engine beyond --help's image probe" \
    "" "$(grep -v '^image inspect claude-yolo:latest$' "$ENGINE_CALLS")"

echo ""
echo "Summary: $passed passed, $failed failed"
if [ "$failed" -gt 0 ]; then
    exit 1
fi
