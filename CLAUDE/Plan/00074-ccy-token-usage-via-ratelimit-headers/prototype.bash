#!/usr/bin/env bash
#
# Plan 00074 — PROTOTYPE: can a stored ccy setup-token read its own usage from
# the unified rate-limit RESPONSE HEADERS?
#
# HOST-ONLY. The CCY container holds no tokens.
#
# ⚠️  THIS SPENDS REAL QUOTA. Unlike Plan 00073's triage, this is not read-only:
#     each arm makes a genuine billed /v1/messages request against the account,
#     and that request consumes a sliver of the very allowance it reports. It
#     probes ONE token by default for exactly that reason. Nothing else is
#     written or changed.
#
# BACKGROUND: Plan 00073 established that GET /api/oauth/usage answers 403
# "OAuth token does not meet scope requirement user:profile" for every stored
# setup-token, so the free status route is unavailable. But the same figures
# travel as response headers on /v1/messages — the scope an oat01 token DOES
# hold: anthropic-ratelimit-unified-{5h,7d}-{utilization,reset}.
#
# WHAT IT COMPARES:
#   arm "curl"    one bare POST /v1/messages, max_tokens=1, headers dumped.
#   arm "claude"  `claude -p` with --model haiku and tools disabled, to test
#                 whether the CLI can surface the same headers at all.
#
# The curl arm is the cheaper of the two by a wide margin: `claude -p` ships a
# large system prompt and tool schemas as input tokens even with tools off,
# whereas the bare request sends a single character. Haiku is used throughout —
# the weekly buckets are per-model (seven_day_opus / seven_day_sonnet), so
# probing with the cheapest model avoids spending the allowance that matters.

set -euo pipefail

ARM="both"
ONLY_TOKEN=""
MODEL="claude-haiku-4-5-20251001"
CLI_MODEL="haiku"
ASSUME_YES=0

usage() {
    cat <<'EOF'
Plan 00074 prototype — read usage limits from /v1/messages response headers

USAGE:
    prototype.bash [options]

OPTIONS:
    --token NAME   Probe this token (default: the first valid stored token)
    --arm ARM      curl | claude | both        (default: both)
    -y, --yes      Skip the "this spends quota" confirmation
    -h, --help     Show this help

COST:
    Each arm makes ONE real, billed API request against the chosen account and
    consumes a sliver of the allowance being measured. One token only, by
    default. Uses Haiku so the Opus/Sonnet weekly buckets are untouched.

OUTPUT:
    Writes logs/header-usage-prototype.log (gitignored — live account state).

READ THIS FOR:
    The "VERDICT" section at the end: which arm (if either) returned
    anthropic-ratelimit-unified-* headers, and the values parsed from them.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --token)   [ $# -ge 2 ] || { echo "ERROR: --token needs a name" >&2; exit 1; }
                   ONLY_TOKEN="$2"; shift 2 ;;
        --token=*) ONLY_TOKEN="${1#--token=}"; shift ;;
        --arm)     [ $# -ge 2 ] || { echo "ERROR: --arm needs a value" >&2; exit 1; }
                   ARM="$2"; shift 2 ;;
        --arm=*)   ARM="${1#--arm=}"; shift ;;
        -y|--yes)  ASSUME_YES=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "ERROR: unknown option: $1" >&2; echo "Try: prototype.bash --help" >&2; exit 1 ;;
    esac
done

case "$ARM" in
    curl|claude|both) ;;
    *) echo "ERROR: --arm must be curl, claude or both (got: $ARM)" >&2; exit 1 ;;
esac

# --- Environment ---------------------------------------------------------------

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPORTS_DIR="$PLAN_DIR/logs"
LOG="$REPORTS_DIR/header-usage-prototype.log"

TOKEN_DIR="${CCY_TOKEN_DIR:-$HOME/.claude-tokens/ccy/tokens}"
API_URL="${CCY_MESSAGES_URL:-https://api.anthropic.com/v1/messages}"

if [ -f /.dockerenv ] || [ -d /workspace/.claude ]; then
    echo "ERROR: this looks like a CCY container — tokens live on the HOST." >&2
    exit 1
fi

for tool in curl jq; do
    if ! command -v "$tool" > /dev/null; then
        echo "ERROR: $tool is not installed." >&2
        echo "  It is declared in playbooks/imports/play-claude-code.yml." >&2
        echo "  Deploy it: ansible-playbook playbooks/imports/play-claude-code.yml" >&2
        echo "  Do NOT install it by hand." >&2
        exit 1
    fi
done

if [ ! -d "$TOKEN_DIR" ]; then
    echo "ERROR: token directory not found: $TOKEN_DIR" >&2
    exit 1
fi

TODAY="$(date +%Y-%m-%d)"
TOKEN_FILE=""
for f in "$TOKEN_DIR"/*.token; do
    if [ ! -f "$f" ]; then
        continue
    fi
    fname="$(basename "$f")"
    name="${fname%.*.token}"
    if [ -n "$ONLY_TOKEN" ] && [ "$name" != "$ONLY_TOKEN" ]; then
        continue
    fi
    # Skip expired tokens unless one was named explicitly.
    if [ -z "$ONLY_TOKEN" ] && [[ "$fname" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$ ]]; then
        if [[ "${BASH_REMATCH[1]}" < "$TODAY" ]]; then
            continue
        fi
    fi
    TOKEN_FILE="$f"
    break
done

if [ -z "$TOKEN_FILE" ]; then
    echo "ERROR: no matching valid token in $TOKEN_DIR" >&2
    echo "  List them with: ccy --list-tokens" >&2
    exit 1
fi

TOKEN_NAME="$(basename "$TOKEN_FILE")"
TOKEN_NAME="${TOKEN_NAME%.*.token}"

TOKEN="$(cat "$TOKEN_FILE")"
if [ -z "$TOKEN" ]; then
    echo "ERROR: token file is empty: $TOKEN_FILE" >&2
    echo "  Renew it: ccy --update-token=$TOKEN_NAME" >&2
    exit 1
fi

# --- Consent ------------------------------------------------------------------
# Spending someone's quota is not something to do silently on a bare invocation.

if [ "$ASSUME_YES" -ne 1 ]; then
    CALLS=2
    if [ "$ARM" != "both" ]; then CALLS=1; fi
    printf 'This makes %d real, billed API request(s) as token "%s",\n' "$CALLS" "$TOKEN_NAME" >&2
    printf 'consuming a small amount of the allowance it is measuring.\n' >&2
    printf 'Proceed? [y/N] ' >&2
    if ! read -r reply < /dev/tty; then reply=""; fi
    case "$reply" in
        y|Y|yes|YES) ;;
        *) echo "Aborted. Nothing sent." >&2; exit 1 ;;
    esac
fi

mkdir -p "$REPORTS_DIR"

# --- Helpers ------------------------------------------------------------------

# Anything bearer-shaped is substituted out before the report is written. No
# {n,} intervals — mawk ignores them (Plan 00073 caught this the hard way).
redact() {
    awk '{ gsub(/sk-ant-[A-Za-z0-9_-]+/, "sk-ant-<redacted>"); print }'
}

# grep exit 1 means "no match", which here is DATA — the header is absent. It is
# captured and returned as such, so a REAL grep failure (exit 2: unreadable
# file, bad pattern) still surfaces instead of being flattened into "absent".
grep_lines() {
    local pattern="$1" file="$2" out rc
    # The status MUST be read inside an else branch. After `if cmd; then …; fi`
    # with no else, `$?` is 0 even when the condition failed — the `if` statement
    # itself succeeded. Reading it after `fi` reported every no-match as
    # "grep failed (exit 0)", which is how this was caught.
    if out="$(grep -ai "$pattern" "$file")"; then
        printf '%s\n' "$out"
        return 0
    else
        rc=$?
        if [ "$rc" -eq 1 ]; then
            return 1          # no match — a legitimate answer
        fi
        echo "ERROR: grep failed (exit $rc) on $file" >&2
        return "$rc"
    fi
}

# Echoes one header's value, or nothing when the header is absent.
header_value() {
    local file="$1" name="$2" line
    if line="$(grep_lines "^$name:" "$file")"; then
        printf '%s' "${line%%$'\n'*}" | cut -d: -f2- | tr -d ' \r'
    fi
}

show_unified_headers() {
    local hdr_file="$1" found other
    if found="$(grep_lines '^anthropic-ratelimit-unified' "$hdr_file")"; then
        printf '%s\n' "$found" | awk '{print "  " $0}'
        return 0
    fi
    echo "  NONE — no anthropic-ratelimit-unified-* headers in the response."
    echo "  (all rate-limit-ish headers seen, for reference:)"
    if other="$(grep_lines '^anthropic-ratelimit' "$hdr_file")"; then
        printf '%s\n' "$other" | awk '{print "    " $0}'
    else
        echo "    (none at all)"
    fi
}

fmt_reset() {
    local epoch="$1" now="$2" delta
    if [ -z "$epoch" ]; then
        return 0
    fi
    if ! [[ "$epoch" =~ ^[0-9]+$ ]]; then
        return 0
    fi
    delta=$(( epoch - now ))
    if [ "$delta" -le 0 ]; then
        return 0
    fi
    # Round to nearest, NOT floor. Floor is a full unit wrong the moment any
    # time has passed: a reset exactly 3 days out rendered as "r2d" two seconds
    # after the header was issued, and 2 hours as "r1h". For an informational
    # countdown that reads as plainly incorrect.
    if [ "$delta" -lt 3600 ]; then
        printf ' r%dm' $(( (delta + 30) / 60 ))
    elif [ "$delta" -lt 172800 ]; then
        printf ' r%dh' $(( (delta + 1800) / 3600 ))
    else
        printf ' r%dd' $(( (delta + 43200) / 86400 ))
    fi
}

# Turn the headers into the exact line ccy's menu would render — the point of a
# prototype is to see the finished product, not a header dump.
render_menu_line() {
    local hdr_file="$1"
    local u5 u7 r5 r7 now line=""
    now="$(date +%s)"
    u5="$(header_value "$hdr_file" 'anthropic-ratelimit-unified-5h-utilization')"
    u7="$(header_value "$hdr_file" 'anthropic-ratelimit-unified-7d-utilization')"
    r5="$(header_value "$hdr_file" 'anthropic-ratelimit-unified-5h-reset')"
    r7="$(header_value "$hdr_file" 'anthropic-ratelimit-unified-7d-reset')"

    if [ -n "$u5" ]; then
        line="5h $(printf '%.0f' "$u5")%$(fmt_reset "$r5" "$now")"
    fi
    if [ -n "$u7" ]; then
        if [ -n "$line" ]; then line="$line · "; fi
        line="${line}wk $(printf '%.0f' "$u7")%$(fmt_reset "$r7" "$now")"
    fi

    if [ -z "$line" ]; then
        echo "  (no utilisation headers — nothing to render)"
    else
        echo "  ccy would show:   $TOKEN_NAME  ->  $line"
    fi
}

# --- Arm: bare curl -----------------------------------------------------------
# Token via --config on stdin so it never reaches argv (BSH-09).

arm_curl() {
    local hdr body code
    hdr="$(mktemp)"; body="$(mktemp)"

    code="$(printf 'header = "Authorization: Bearer %s"\n' "$TOKEN" \
        | curl --config - \
               --silent \
               --request POST \
               --header 'content-type: application/json' \
               --header 'anthropic-version: 2023-06-01' \
               --header 'anthropic-beta: oauth-2025-04-20' \
               --data "{\"model\":\"$MODEL\",\"max_tokens\":1,\"messages\":[{\"role\":\"user\",\"content\":\".\"}]}" \
               --dump-header "$hdr" \
               --max-time 30 \
               --output "$body" \
               --write-out '%{http_code}' \
               "$API_URL")"

    echo "http_status: $code"
    echo ""
    echo "unified rate-limit headers:"
    show_unified_headers "$hdr"
    echo ""
    render_menu_line "$hdr"
    echo ""
    if [ "$code" != "200" ]; then
        echo "response body (refusal reason):"
        if ! jq . "$body"; then
            echo "(body is not JSON; raw bytes follow)"
            cat "$body"
        fi
        echo ""
        echo "NOTE: a 401/403 here means the bare request shape is wrong or the"
        echo "      token cannot call /v1/messages directly. Compare with the"
        echo "      claude arm before concluding the approach is dead."
    fi
    rm -f "$hdr" "$body"
    return 0
}

# --- Arm: claude -p -----------------------------------------------------------
# Control: does the CLI surface the same headers at all? Token passed by NAME in
# the environment, never in argv.

arm_claude() {
    if ! command -v claude > /dev/null; then
        echo "claude CLI not found on PATH — skipping this arm."
        echo "  It is installed by playbooks/imports/play-claude-code.yml."
        return 0
    fi

    local dbg out rc hits
    dbg="$(mktemp)"; out="$(mktemp)"

    local CLAUDE_CODE_OAUTH_TOKEN="$TOKEN"
    export CLAUDE_CODE_OAUTH_TOKEN

    if claude -p "." \
            --model "$CLI_MODEL" \
            --tools "" \
            --debug \
            --debug-file "$dbg" \
            > "$out" 2>&1; then
        rc=0
    else
        rc=$?
    fi
    unset CLAUDE_CODE_OAUTH_TOKEN

    echo "exit_status: $rc"
    echo ""
    echo "output (first 15 lines, redacted):"
    head -n 15 "$out" | redact | awk '{print "  " $0}'
    echo ""
    echo "unified rate-limit headers found in CLI output or debug log?"
    if hits="$(grep_lines 'anthropic-ratelimit-unified' "$dbg")"; then
        printf '%s\n' "$hits" | awk '{print "  " $0}'
    elif hits="$(grep_lines 'anthropic-ratelimit-unified' "$out")"; then
        printf '%s\n' "$hits" | awk '{print "  " $0}'
    else
        echo "  NO — the CLI does not surface response headers, even with --debug."
        echo "  Expected: they are consumed internally, not logged. If so, the"
        echo "  curl arm is the only viable mechanism."
    fi
    echo ""
    echo "debug log size: $(wc -c < "$dbg") bytes"
    rm -f "$dbg" "$out"
    return 0
}

# --- Report -------------------------------------------------------------------

TMP_REPORT="$(mktemp)"
cleanup() { rm -f "$TMP_REPORT" "$TMP_REPORT.r"; }
trap cleanup EXIT

{
    echo "================================================================================"
    echo "Plan 00074 — usage limits via /v1/messages response headers (PROTOTYPE)"
    echo "================================================================================"
    echo "generated: $(date -Iseconds)"
    echo "token:     $TOKEN_NAME"
    echo "model:     $MODEL"
    echo "endpoint:  $API_URL"
    echo "arm(s):    $ARM"
    echo ""

    if [ "$ARM" = "curl" ] || [ "$ARM" = "both" ]; then
        echo "--------------------------------------------------------------------------------"
        echo "ARM 1 — bare POST /v1/messages (max_tokens=1)"
        echo "--------------------------------------------------------------------------------"
        arm_curl
        echo ""
    fi

    if [ "$ARM" = "claude" ] || [ "$ARM" = "both" ]; then
        echo "--------------------------------------------------------------------------------"
        echo "ARM 2 — claude -p --model $CLI_MODEL, tools disabled"
        echo "--------------------------------------------------------------------------------"
        arm_claude
        echo ""
    fi

    echo "================================================================================"
    echo "VERDICT — READ THIS"
    echo "================================================================================"
    echo ""
    echo "Look at 'unified rate-limit headers' under ARM 1."
    echo ""
    echo "  Headers present + a rendered 'ccy would show' line"
    echo "      -> the approach WORKS. Wire it into select_token() as a"
    echo "         human-triggered 'u) show usage' option that redraws the menu."
    echo "         Cost per press: one Haiku request per token."
    echo ""
    echo "  http_status 401/403, no headers"
    echo "      -> the bare request shape is rejected. Read the body: if it names"
    echo "         a missing scope, this dies the same way Plan 00073 did. If it"
    echo "         complains about the request shape, that is fixable — adjust"
    echo "         and retry rather than concluding."
    echo ""
    echo "  200 but NO unified headers"
    echo "      -> the account may not be on a plan served by these headers, or"
    echo "         they may only appear once usage is non-trivial. Re-run later"
    echo "         before concluding."
    echo ""
    echo "ARM 2 is a control: if it also shows nothing, that confirms the CLI"
    echo "cannot be the mechanism and curl is the only route."
    echo ""
} > "$TMP_REPORT" 2>&1

redact < "$TMP_REPORT" > "$TMP_REPORT.r"
mv "$TMP_REPORT.r" "$TMP_REPORT"

if grep -aqE 'sk-ant-[A-Za-z0-9_-]{4,}' "$TMP_REPORT"; then
    rm -f "$TMP_REPORT"
    echo "ERROR: redaction check FAILED — a bearer-shaped value survived." >&2
    echo "  Report destroyed, nothing written to $LOG." >&2
    exit 1
fi

install -m 0600 "$TMP_REPORT" "$LOG"
cat "$LOG"
echo ""
echo "Full report saved: $LOG"
