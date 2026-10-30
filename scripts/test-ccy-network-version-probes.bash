#!/usr/bin/env bash
# Test the ccy launcher's network check and its Claude Code version probes (Plan 00191 Task
# 4.10, B3 and B5 of the command-failure-dropped DBF report). Both are asked of the real Go
# front (`--check-network`, `--claude-versions`: ccylaunch F18, F19), built here, through a
# fake podman and curl on PATH; the launcher's own code is lifted from it and run as it runs.
#
# WHY THIS EXISTS. The network check read any failed `alpine wget` run as "no internet
# access", an engine or pull fault included, and the version probes dropped the cause they had
# captured and said "offline?". Each now says what failed, and the launcher acts on which.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"
# CCY_TEST_LAUNCHER: another launcher to judge (an older revision, to see this suite fail on it).
LAUNCHER="${CCY_TEST_LAUNCHER:-$REPO_ROOT/files/var/local/claude-yolo/claude-yolo}"

if [ -z "$(command -v go)" ]; then
    echo "FAIL: go is needed by this test" >&2
    exit 1
fi

passed=0
failed=0
# expect <label> <want rc> <got rc> <output file> <words that must appear...> [-- <words that must not>...]
expect() {
    local label="$1" want="$2" got="$3" file="$4" word bad="" absent=false
    shift 4
    for word in "$@"; do
        if [ "$word" = -- ]; then
            absent=true
        elif [ "$absent" = false ] && ! grep -qF -- "$word" "$file"; then
            bad+=" missing '$word';"
        elif [ "$absent" = true ] && grep -qF -- "$word" "$file"; then
            bad+=" has '$word';"
        fi
    done
    if [ "$got" = "$want" ] && [ -z "$bad" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s: rc %s (want %s);%s\n        output: %s\n' "$label" "$got" "$want" "$bad" "$(cat "$file")" >&2
    fi
}

mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/ccy-network-version-test.XXXXXX")"
trap 'rm -rf "$SCRATCH"' EXIT
export BIN="$SCRATCH/bin"
mkdir -p "$BIN"

build_out="$(cd "$REPO_ROOT/src-go" && go build -trimpath -buildvcs=false -o "$SCRATCH/claude-yolo" ./cmd/claude-yolo 2>&1)"
build_rc=$?
if [ "$build_rc" -ne 0 ]; then
    echo "FAIL: the Go front did not build: $build_out" >&2
    exit 1
fi

# The fake podman: a `--version` run answers as FAKE_IMAGE_*, a `--network` run (the wget
# probe) as FAKE_NET_*. The fake curl (the registry) answers as FAKE_REG_*.
cat >"$BIN/podman" <<'EOF'
#!/usr/bin/env bash
case " $* " in
*" --version "*) printf '%s' "${FAKE_IMAGE_OUT:-}"; printf '%s' "${FAKE_IMAGE_ERR:-}" >&2; exit "${FAKE_IMAGE_EXIT:-0}" ;;
*" --network "*) printf '%s' "${FAKE_NET_ERR:-}" >&2; exit "${FAKE_NET_EXIT:-0}" ;;
esac
echo "fake podman: unscripted: $*" >&2
exit 99
EOF
cat >"$BIN/curl" <<'EOF'
#!/usr/bin/env bash
printf '%s' "${FAKE_REG_OUT:-}"; printf '%s' "${FAKE_REG_ERR:-}" >&2; exit "${FAKE_REG_EXIT:-0}"
EOF
chmod +x "$BIN/podman" "$BIN/curl"

# lift <start regex> <indent> — the launcher's lines from the start to the closing line at
# the indent ("}" for a function, "fi" for a block).
lift() {
    awk -v start="$1" -v close_line="$2" '!p && $0 ~ start { p = 1 } p { print } p && $0 == close_line { exit }' "$LAUNCHER"
}
lift '^auto_update_claude_code[(][)] [{]$' '}' >"$SCRATCH/update.bash"
lift '^    if "[$]SCRIPT_DIR/claude-yolo" --check-network ' '    fi' >"$SCRATCH/network-block.bash"
for part in update network-block; do
    if [ ! -s "$SCRATCH/$part.bash" ]; then
        echo "FAIL: the $part code was not found in the launcher" >&2
        exit 1
    fi
done

cat >"$SCRATCH/update-run.bash" <<'EOF'
source "$LIB_DIR/common.bash" || exit 90
source "$SCRATCH/update.bash" || exit 91
SCRIPT_DIR="$SCRATCH" CONTAINER_ENGINE=podman VERSION_CHECK_CACHE="$SCRATCH/cache"
auto_update_claude_code claude-yolo:latest
EOF
cat >"$SCRATCH/network-run.bash" <<'EOF'
source "$LIB_DIR/common.bash" || exit 90
SCRIPT_DIR="$SCRATCH" CONTAINER_ENGINE=podman SELECTED_NETWORK=appnet
cd "$SCRATCH/project" || exit 91
source "$SCRATCH/network-block.bash"
EOF
mkdir -p "$SCRATCH/project"
export LIB_DIR SCRATCH

# run_case <script> <env...> — runs the script with the fakes and the env, output in $out, rc in $rc.
out="$SCRATCH/out"
run_case() {
    local script="$1"
    shift
    rm -rf "$SCRATCH/cache"
    env PATH="$BIN:$PATH" CCY_AUTO_UPDATE=0 "$@" bash "$SCRATCH/$script" >"$out" 2>&1
    rc=$?
}

echo "== the version probes"
reg='{"name":"@anthropic-ai/claude-code","version":"2.1.3"}'
run_case update-run.bash FAKE_IMAGE_OUT="2.1.3 (Claude Code)" FAKE_REG_OUT="$reg"
expect "both read and equal: up to date" 0 "$rc" "$out" "up to date (2.1.3)"
run_case update-run.bash FAKE_IMAGE_OUT="2.0.0 (Claude Code)" FAKE_REG_OUT="$reg"
expect "behind: the update is offered" 0 "$rc" "$out" "2.0.0 → 2.1.3"
run_case update-run.bash FAKE_IMAGE_EXIT=125 FAKE_IMAGE_ERR="Error: claude-yolo:latest: image not known" FAKE_REG_OUT="$reg"
expect "the image's probe failed: its words, the check skipped" 0 "$rc" "$out" "image not known" "Skipping" -- "offline"
run_case update-run.bash FAKE_IMAGE_OUT="2.0.0 (Claude Code)" FAKE_REG_EXIT=6 FAKE_REG_ERR="curl: (6) Could not resolve host: registry.npmjs.org"
expect "the registry unreachable: curl's words" 0 "$rc" "$out" "could not be reached" "Could not resolve host" -- "offline"
run_case update-run.bash FAKE_IMAGE_OUT="2.0.0 (Claude Code)" FAKE_REG_EXIT=22 FAKE_REG_ERR="curl: (22) The requested URL returned error: 503"
expect "the registry's error answer: said as the registry's" 0 "$rc" "$out" "answered with an error" "503" -- "offline"
if [ -n "$(find "$SCRATCH/cache" -type f 2>&1)" ]; then
    expect "a skipped check is stamped for the day" 0 0 "$out"
else
    expect "a skipped check is stamped for the day" stamped none "$out"
fi

echo "== the network check"
run_case network-run.bash
expect "a network that reaches the internet passes" 0 "$rc" "$out" "has internet access"
run_case network-run.bash FAKE_NET_EXIT=1 FAKE_NET_ERR="wget: bad address 'google.com'"
expect "wget failed: no internet, with wget's words and the guide" 1 "$rc" "$out" "reaches no internet" "bad address" "Debugging steps"
run_case network-run.bash FAKE_NET_EXIT=125 FAKE_NET_ERR="Error: initializing source docker.io/library/alpine:latest: no route"
expect "the pull failed: the check could not be run, never no internet" 1 "$rc" "$out" "could not be checked" "initializing source" -- "no internet" "Debugging steps"
run_case network-run.bash FAKE_NET_EXIT=127 FAKE_NET_ERR="wget: not found"
expect "no wget in the image: an image fault, never no internet" 1 "$rc" "$out" "could not be checked" -- "no internet"

echo ""
echo "passed: $passed, failed: $failed"
[ "$failed" -eq 0 ]
