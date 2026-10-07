#!/usr/bin/env bash
# Test that the token menu's usage view (press `u`) prints only its own display lines.
#
# Sources the libraries from THIS repo (not the deployed /var/local copy) and drives
# select_token through the real `u` path: usage_prime_cache fans the fetches out, the menu
# is redrawn, and usage_render_block draws the bars. curl is a stub on PATH that answers
# from the token it is handed on stdin, so no request leaves the machine and no real
# credential is involved: the fixtures hold placeholder strings.
#
# WHY THIS EXISTS. The usage view was reported printing set -x style noise before the bars.
# Every line the menu prints is therefore classified against the lines it is meant to
# print, and anything else fails the case and is shown. stderr must stay empty, because a
# diagnostic only belongs there behind CCY_USAGE_DEBUG. The classifier is itself checked
# against known noise first, so a pattern loosened until it matches everything fails here
# rather than passing every case.
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

# Fixtures live under the repo's gitignored scratch area rather than /tmp, and mktemp
# rather than $$ because this repo is bind-mounted into containers with their own PIDs.
WORK="$(mktemp -d "$REPO_ROOT/untracked/ccy-token-usage-fixtures.XXXXXX")"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT INT TERM

STUB_BIN="$WORK/bin"
mkdir -p "$STUB_BIN"

# The curl stub. The token arrives as a curl config line on stdin, exactly as
# _usage_fetch_one sends it; the stub answers by token so one run covers both a figures
# row and an unauthorised row. Header lines end in CRLF, as a real response's do.
cat >"$STUB_BIN/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
hdr=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --dump-header) hdr="$2"; shift ;;
    esac
    shift
done
config="$(cat)"
now="$(date +%s)"
case "$config" in
    *placeholder-denied*)
        printf 'HTTP/2 401\r\ncontent-type: application/json\r\n\r\n' >"$hdr"
        printf '401'
        ;;
    *)
        printf 'HTTP/2 200\r\ncontent-type: application/json\r\n' >"$hdr"
        printf 'anthropic-ratelimit-unified-status: allowed\r\n' >>"$hdr"
        printf 'anthropic-ratelimit-unified-5h-utilization: 0.34\r\n' >>"$hdr"
        printf 'anthropic-ratelimit-unified-5h-reset: %s\r\n' "$((now + 14400))" >>"$hdr"
        printf 'anthropic-ratelimit-unified-7d-utilization: 0.08\r\n' >>"$hdr"
        printf 'anthropic-ratelimit-unified-7d-reset: %s\r\n' "$((now + 518400))" >>"$hdr"
        printf 'anthropic-ratelimit-unified-representative-claim: five_hour\r\n\r\n' >>"$hdr"
        printf '200'
        ;;
esac
STUB
chmod 755 "$STUB_BIN/curl"

NEXT_YEAR="$(date -d '+365 days' +%Y-%m-%d)"
YESTERDAY="$(date -d '-1 day' +%Y-%m-%d)"
make_pool() {
    local dir="$1"
    mkdir -p "$dir/tokens"
    printf 'placeholder-ok\n' >"$dir/tokens/alpha.$NEXT_YEAR.token"
    printf 'placeholder-denied\n' >"$dir/tokens/beta.$NEXT_YEAR.token"
    printf 'placeholder-ok\n' >"$dir/tokens/gamma.$YESTERDAY.token"
}

# Succeeds when a line (ANSI colour already stripped) is one the menu means to print.
is_display_line() {
    local line="$1"
    [[ -z "$line" ]] && return 0
    [[ "$line" =~ ^═+$ ]] && return 0
    case "$line" in
        "Claude Code Token Selection for YOLO Mode" | "Available tokens:" | \
            "  0) Create new token" | \
            "  u) Show usage limits (costs 1 small API call per account)" | \
            "Fetching usage for "[0-9]*" account(s)..." | \
            "⚠  Found "[0-9]*" expired token(s):" | \
            "✓ Selected token: "*" (expires: "*")")
            return 0
            ;;
    esac
    [[ "$line" =~ ^\ \ [0-9]+\)\ [a-z]+\ +\ expires\ [0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] && return 0
    [[ "$line" =~ ^\ \ r[0-9]+\)\ Renew:\ [a-z]+\ \(expired:\ [0-9]{4}-[0-9]{2}-[0-9]{2}\)$ ]] && return 0
    [[ "$line" =~ ^\ {4}[a-z]+\ \(expired:\ [0-9]{4}-[0-9]{2}-[0-9]{2}\)$ ]] && return 0
    [[ "$line" =~ ^\ {7}(5-hour|weekly)\ limit\ +[█░]+\ +(\<1|[0-9]+)%\ +resets\ in\ [0-9]+\ [a-z]+(\ +\[API\ sent\ [0-9.]+\])?$ ]] && return 0
    [[ "$line" =~ ^\ {7}binding\ limit:\ .+$ ]] && return 0
    [[ "$line" =~ ^\ {7}usage\ unavailable\ —\ .+$ ]] && return 0
    return 1
}

# Prints every line of $1 that is not a display line; succeeds when there are none.
stray_lines() {
    local text="$1" line found=1
    while IFS= read -r line; do
        if ! is_display_line "$line"; then
            printf '        stray: %q\n' "$line"
            found=0
        fi
    done <<<"$text"
    [ "$found" -eq 1 ]
}

shopt -s extglob
strip_ansi() {
    printf '%s' "${1//$'\033'\[*([0-9;])m/}"
}

PASSED=0
FAILED=0
pass() { printf '  PASS  %s\n' "$1"; PASSED=$((PASSED + 1)); }
fail() { printf '  FAIL  %s\n' "$1"; FAILED=$((FAILED + 1)); }

echo ""
echo "=== the classifier rejects noise (negative control) ==="
for noise in 'filename=alpha.2099-01-01.token' '+ local filename' \
    "declare -- token_name=\"alpha\"" 'u5=0.34' '[1]+  Done                    ( token=x )'; do
    if is_display_line "$noise"; then
        fail "classifier accepted noise: $noise"
    else
        pass "classifier rejects: $noise"
    fi
done

# usage_case <description> <extra env assignment or ''> <must-contain>
usage_case() {
    local desc="$1" extra_env="$2" want="$3"
    local dir="$WORK/case-$((PASSED + FAILED))" rc=0 out err plain
    make_pool "$dir"
    local -a env_args=(PATH="$STUB_BIN:$PATH")
    [ -n "$extra_env" ] && env_args+=("$extra_env")
    out="$(printf 'u\n1\n' | env "${env_args[@]}" timeout 30 bash -c "
        set -euo pipefail
        : \"\${GH_TOKEN:=}\" \"\${IMAGE_NAME:=}\"
        source '$LIB_DIR/common-pure.bash'
        source '$LIB_DIR/token-management.bash'
        select_token '$dir/tokens' container
    " 2>"$dir/stderr")" || rc=$?
    err="$(cat "$dir/stderr")"
    plain="$(strip_ansi "$out")"

    if [ "$rc" -ne 0 ]; then
        fail "$desc -> select_token rc=$rc (want 0)"
        printf '%s\n%s\n' "$plain" "$err" | awk '{ print "        | " $0 }'
        return
    fi
    if ! printf '%s' "$plain" | grep -qF "$want"; then
        fail "$desc -> never printed \"$want\" (the usage view was not drawn)"
        return
    fi
    if [ -n "$err" ]; then
        fail "$desc -> wrote to stderr"
        printf '%s\n' "$err" | awk '{ print "        stderr: " $0 }'
        return
    fi
    local report
    if ! report="$(stray_lines "$plain")"; then
        fail "$desc -> printed lines that are not part of the display"
        printf '%s\n' "$report"
        return
    fi
    pass "$desc"
}

echo ""
echo "=== the usage view prints only its display lines ==="
usage_case "figures, an unauthorised token and an expired one" '' '5-hour limit'
usage_case "unauthorised token says why on its own row" '' 'usage unavailable — this token was not authorised'
usage_case "CCY_USAGE_DEBUG adds the raw value, nothing else" 'CCY_USAGE_DEBUG=1' '[API sent 0.34]'

echo ""
printf 'passed: %d   failed: %d\n' "$PASSED" "$FAILED"
if [ "$PASSED" -eq 0 ]; then
    echo "FAIL: no case passed" >&2
    exit 1
fi
if [ "$FAILED" -ne 0 ]; then
    exit 1
fi
