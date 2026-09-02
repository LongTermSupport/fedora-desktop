#!/usr/bin/env bash
#
# Plan 00100 — triage: can a stored CCY token read GET /api/oauth/usage?
#
# READ-ONLY. Makes authenticated GET requests to Anthropic's API and writes a
# report. It changes nothing: no file is written outside this plan's logs/
# directory, no token is created, renewed, modified or deleted, no service is
# touched. Safe to re-run at any time.
#
# HOST-ONLY. The CCY container holds no tokens — run this on the machine where
# ~/.claude-tokens/ccy/tokens/ lives.
#
# READ THIS FOR: the "VERDICT INPUTS" section at the end. It answers the three
# questions Plan 00100 is gated on:
#   Q1  does a long-lived sk-ant-oat01 setup-token authenticate to the endpoint?
#   Q2  what is the exact JSON envelope?
#   Q3  is the answer per-token or per-account?
#
# This script gathers facts. It renders NO verdict — that is acceptance.bash's
# job (see CLAUDE/PlanTriage.md).

set -euo pipefail

# --- Argument parsing happens FIRST -------------------------------------------
# Before any environment resolution, so --help works on a machine where the
# token directory is missing (CLAUDE/PlanTriage.md).

ONLY_TOKEN=""
SHOW_RAW_BODY=1
RETRY_PAUSE=5

usage() {
    cat <<'EOF'
Plan 00100 triage — probe stored CCY tokens against GET /api/oauth/usage

USAGE:
    triage.bash [options]

OPTIONS:
    --token NAME    Probe only the named token (default: every stored token)
    --no-body       Report status codes and JSON key shape only; omit the
                    response body from the report
    -h, --help      Show this help

OUTPUT:
    Writes CLAUDE/Plan/00100-ccy-token-usage-limits/logs/token-usage-triage.log
    (gitignored — it contains live account state).

SAFETY:
    Read-only. Tokens are passed to curl on stdin, never in argv, so they do
    not appear in `ps`. Bearer values and email addresses are substituted out
    of the report before it is written, and the redaction is verified — if the
    check fails the report is destroyed rather than saved.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --token)
            if [ $# -lt 2 ]; then
                echo "ERROR: --token requires a token name" >&2
                exit 1
            fi
            ONLY_TOKEN="$2"
            shift 2
            ;;
        --token=*)
            ONLY_TOKEN="${1#--token=}"
            shift
            ;;
        --no-body)
            SHOW_RAW_BODY=0
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "ERROR: unknown option: $1" >&2
            echo "Try: triage.bash --help" >&2
            exit 1
            ;;
    esac
done

# --- Environment resolution ----------------------------------------------------

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPORTS_DIR="$PLAN_DIR/logs"
LOG="$REPORTS_DIR/token-usage-triage.log"

TOKEN_DIR="${CCY_TOKEN_DIR:-$HOME/.claude-tokens/ccy/tokens}"
USAGE_URL="https://api.anthropic.com/api/oauth/usage"
PROFILE_URL="https://api.anthropic.com/api/oauth/profile"

if [ -f /.dockerenv ] || [ -d /workspace/.claude ]; then
    echo "ERROR: this looks like a CCY container." >&2
    echo "  Tokens live on the HOST at ~/.claude-tokens/ccy/tokens/ and are not" >&2
    echo "  mounted in. Run this script on your host system." >&2
    exit 1
fi

# A missing tool is an IaC gap, never a hand-install (CLAUDE.md).
for tool in curl jq; do
    if ! command -v "$tool" > /dev/null; then
        echo "ERROR: $tool is not installed." >&2
        echo "  It is declared in playbooks/imports/play-claude-code.yml." >&2
        echo "  Deploy it with:" >&2
        echo "    ansible-playbook playbooks/imports/play-claude-code.yml" >&2
        echo "  Do NOT install it by hand." >&2
        exit 1
    fi
done

if [ ! -d "$TOKEN_DIR" ]; then
    echo "ERROR: token directory not found: $TOKEN_DIR" >&2
    echo "  Create a token with: ccy --create-token" >&2
    exit 1
fi

token_files=()
if [ -n "$ONLY_TOKEN" ]; then
    for f in "$TOKEN_DIR/$ONLY_TOKEN".*.token; do
        if [ -f "$f" ]; then
            token_files+=("$f")
        fi
    done
    if [ ${#token_files[@]} -eq 0 ]; then
        echo "ERROR: no stored token named '$ONLY_TOKEN' in $TOKEN_DIR" >&2
        echo "  List them with: ccy --list-tokens" >&2
        exit 1
    fi
else
    for f in "$TOKEN_DIR"/*.token; do
        if [ -f "$f" ]; then
            token_files+=("$f")
        fi
    done
    if [ ${#token_files[@]} -eq 0 ]; then
        # An empty report would read as "the endpoint refused everything".
        # Fail instead of writing a misleading empty section.
        echo "ERROR: no *.token files in $TOKEN_DIR — nothing to probe." >&2
        echo "  Create a token with: ccy --create-token" >&2
        exit 1
    fi
fi

mkdir -p "$REPORTS_DIR"

# --- Redaction -----------------------------------------------------------------
# Substitute anywhere in the line — never drop anchored lines. A start-of-line
# anchor does not match a secret embedded mid-line, which is exactly how a
# credential reached a plan log in Plan 00066 (CLAUDE/PlanTriage.md).
#
# No {n,} interval expressions: mawk (the CCY container's awk) ignores them
# unless built with --re-interval, so `[A-Za-z]{2,}` matches literally and the
# substitution silently does nothing. Verified — the email pattern failed
# exactly this way before being rewritten as `[A-Za-z][A-Za-z]+`. Fedora's gawk
# would have hidden the bug.

redact() {
    awk '{
        gsub(/sk-ant-[A-Za-z0-9_-]+/, "sk-ant-<redacted>")
        gsub(/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z][A-Za-z]+/, "<email-redacted>")
        print
    }'
}

# --- Probe helper --------------------------------------------------------------
# A non-zero exit status is DATA, not a failure. Capture it and carry on.

probe() {
    local label="$1"; shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

# Describes a token WITHOUT revealing it: length and prefix family only.
describe_token_shape() {
    local token="$1"
    # The label must not spell the prefix out as `sk-ant-oat01`: redact()
    # rewrites anything matching `sk-ant-<chars>` and would eat the label
    # itself, which is the one part of this line worth reading.
    local family="unrecognised"
    case "$token" in
        sk-ant-oat01-*) family="oat01 (long-lived setup-token)" ;;
        sk-ant-api03-*) family="api03 (standard API key)" ;;
        sk-ant-*)       family="other sk-ant key" ;;
    esac
    echo "family: $family"
    echo "length: ${#token} chars"
}

# One authenticated GET. Token goes to curl via --config on stdin so it never
# lands in argv/ps. Echoes the status; writes the body to $3.
_one_get() {
    local token="$1" url="$2" body_file="$3"
    printf 'header = "Authorization: Bearer %s"\n' "$token" \
        | curl --config - \
               --silent --show-error \
               --request GET \
               --header 'Content-Type: application/json' \
               --max-time 15 \
               --output "$body_file" \
               --write-out '%{http_code}' \
               "$url"
}

# The whole point of the exercise: does this credential open the endpoint?
#
# Retries once on 429, after a pause, because a 429 is NOT a verdict. ccy fetches
# every token in one parallel burst, and a burst from a single IP can be throttled
# before the credential is ever evaluated — which is exactly what produced one
# unexplained "usage: rate limited" row among three 401s on the first HOST run.
# A token that answers 401 on the retry was always going to be refused; one that
# answers 200 was accepted and merely throttled. Probing SEQUENTIALLY here (this
# whole script is a serial loop) removes the burst as a variable in the first
# place; the retry covers a throttle that is not ours.
fetch_usage() {
    local token="$1" body_file="$2"
    local http_code
    http_code="$(_one_get "$token" "$USAGE_URL" "$body_file")"
    echo "http_status: $http_code"

    if [ "$http_code" = "429" ]; then
        echo "NOTE: 429 is not an auth verdict — pausing ${RETRY_PAUSE}s and retrying once."
        sleep "$RETRY_PAUSE"
        http_code="$(_one_get "$token" "$USAGE_URL" "$body_file")"
        echo "http_status_after_retry: $http_code"
    fi

    if [ "$http_code" != "200" ]; then
        echo "NOTE: non-200 — see the body below for the refusal reason."
    fi
}

# Discriminator: is this credential rejected by /usage specifically, or is it not
# an OAuth-route credential at all? /api/oauth/profile is a different route
# behind the same auth. Status ONLY is reported — never the body, which carries
# the account email; there is no reason to put that on disk to answer this.
probe_profile_route() {
    local token="$1"
    local body http_code
    body="$(mktemp)"
    http_code="$(_one_get "$token" "$PROFILE_URL" "$body")"
    rm -f "$body"
    echo "profile_route_http_status: $http_code  (body deliberately not captured)"
    echo ""
    echo "  Read together with the /usage status above:"
    echo "    profile 200 + usage 401 -> token IS valid on OAuth routes; /usage"
    echo "                               specifically refuses it. Feature is dead"
    echo "                               for this token type, but for a narrower"
    echo "                               reason than 'the token is not OAuth'."
    echo "    profile 401 + usage 401 -> setup-tokens are not OAuth-route"
    echo "                               credentials at all. Q1 = NO, clearly."
    echo "    profile 200 + usage 200 -> it works; the 401s were something else."
}

# Is the host actually RUNNING the code this repo contains? A menu that shows no
# usage line at all is not an endpoint answer — it means the usage code never
# ran, which is either an undeployed library or the kill switch. Distinguishing
# those two from "the token was refused" is the whole point of this section.
report_deployment_state() {
    local repo_root repo_lib host_lib host_ccy

    echo "CCY_TOKEN_USAGE = ${CCY_TOKEN_USAGE:-(unset — feature enabled)}"
    echo "CCY_USAGE_TTL   = ${CCY_USAGE_TTL:-(unset — default 180)}"
    echo ""

    host_lib=/var/local/claude-yolo/lib/token-management.bash
    host_ccy=/var/local/claude-yolo/claude-yolo

    if [ -f "$host_lib" ]; then
        echo "deployed lib : $host_lib"
        echo "  version    : $(grep -m1 '^# Version:' "$host_lib")"
        if grep -q 'usage_prime_cache' "$host_lib"; then
            echo "  usage code : PRESENT"
        else
            echo "  usage code : ABSENT — this host is running a pre-1.7.0 library."
            echo "               Deploy it: CLAUDE/Plan/00100-*/deploy.bash"
        fi
    else
        echo "deployed lib : MISSING ($host_lib)"
    fi
    echo ""

    if [ -f "$host_ccy" ]; then
        echo "deployed ccy : $(grep -m1 '^CCY_VERSION=' "$host_ccy" | cut -d'#' -f1)"
    else
        echo "deployed ccy : MISSING ($host_ccy)"
    fi

    if repo_root="$(git rev-parse --show-toplevel)"; then
        repo_lib="$repo_root/files/var/local/claude-yolo/lib/token-management.bash"
        if [ -f "$repo_lib" ]; then
            echo ""
            echo "repo lib     : $repo_lib"
            echo "  version    : $(grep -m1 '^# Version:' "$repo_lib")"
            if [ -f "$host_lib" ]; then
                if diff -q "$repo_lib" "$host_lib" > /dev/null; then
                    echo "  vs deployed: IDENTICAL"
                else
                    echo "  vs deployed: DIFFERENT — the host is running older code."
                    echo "               Deploy it: CLAUDE/Plan/00100-*/deploy.bash"
                fi
            fi
        fi
    fi
    return 0
}

# Q2: the envelope. Print the JSON *structure* (paths and value types), which is
# what the parser has to be written against.
describe_json_shape() {
    local body_file="$1"
    if ! jq -e . "$body_file" > /dev/null; then
        echo "body is not valid JSON — raw bytes:"
        head -c 400 "$body_file"
        echo
        return 0
    fi
    echo "top-level type: $(jq -r 'type' "$body_file")"
    echo "leaf paths (path = type):"
    # Deliberately NOT paths(scalars): jq's select() treats a null value as
    # false, so scalars silently drops every null leaf — and `scope` is exactly
    # the optional field most likely to arrive null. Filtering on the value's
    # type instead keeps nulls visible, which is the point of a shape report.
    jq -r 'paths as $p | (getpath($p)) as $v
           | select(($v|type) != "object" and ($v|type) != "array")
           | "  \($p | map(tostring) | join(".")) = \($v|type)"' \
        "$body_file"
}

# --- Build the report ----------------------------------------------------------
# Written to a temp file first, NOT streamed to the log: nothing is persisted or
# shown until the redaction check has passed. Assume the redaction is wrong and
# verify it (CLAUDE/PlanTriage.md).

TMP_REPORT="$(mktemp)"
TMP_BODY="$(mktemp)"
cleanup() { rm -f "$TMP_REPORT" "$TMP_BODY"; }
trap cleanup EXIT

{
    echo "================================================================================"
    echo "Plan 00100 — CCY token usage-limit triage"
    echo "================================================================================"
    echo "generated: $(date -Iseconds)"
    echo "host:      $(uname -sr)"
    echo "endpoint:  $USAGE_URL"
    echo "token dir: $TOKEN_DIR"
    echo "tokens:    ${#token_files[@]}"
    echo ""

    probe "curl version" curl --version
    probe "claude version" claude --version
    probe "deployment state (is the new code even running?)" report_deployment_state

    for token_file in "${token_files[@]}"; do
        filename="$(basename "$token_file")"
        token_name="${filename%.*.token}"
        expiry="unknown"
        if [[ "$filename" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$ ]]; then
            expiry="${BASH_REMATCH[1]}"
        fi

        echo "--------------------------------------------------------------------------------"
        echo "TOKEN: $token_name   (expiry from filename: $expiry)"
        echo "--------------------------------------------------------------------------------"
        echo ""

        token="$(cat "$token_file")"
        if [ -z "$token" ]; then
            # An empty store is the Plan 00094 failure mode — report it loudly
            # rather than probing with an empty bearer and blaming the endpoint.
            echo "### token file is EMPTY — cannot probe"
            echo "Renew it with: ccy --update-token=$token_name"
            echo ""
            continue
        fi

        probe "token shape" describe_token_shape "$token"

        : > "$TMP_BODY"
        probe "GET /api/oauth/usage" fetch_usage "$token" "$TMP_BODY"
        probe "response shape (Q2)" describe_json_shape "$TMP_BODY"
        probe "GET /api/oauth/profile — route discriminator" probe_profile_route "$token"

        if [ "$SHOW_RAW_BODY" -eq 1 ]; then
            echo "### response body (redacted)"
            jq . "$TMP_BODY" || cat "$TMP_BODY"
            echo ""
        fi
    done

    echo "================================================================================"
    echo "VERDICT INPUTS — READ THIS FIRST"
    echo "================================================================================"
    echo ""
    echo "Q0  Is the new code even deployed?"
      echo "      See 'deployment state' above. If 'usage code : ABSENT' or"
      echo "      'vs deployed: DIFFERENT', the host is running an older library"
      echo "      and ccy's menu will show NO usage line at all — which is not an"
      echo "      answer to Q1. Run this plan's deploy.bash first, then re-check."
    echo ""
    echo "Q1  Does a stored setup-token authenticate?"
    echo "      Look at every 'GET /api/oauth/usage' section above. These probes"
    echo "      are SEQUENTIAL, unlike ccy's parallel burst, so a 429 here is not"
    echo "      self-inflicted — and any 429 was retried once after a pause."
    echo "      http_status 200        -> yes; the feature works. Confirm the"
    echo "                                envelope against the shape report."
    echo "      http_status 401 / 403  -> no; the token is not accepted here."
    echo "                                Strip the feature back out and cancel."
    echo "                                Do not engineer around a scoped credential."
    echo "      429 even after retry   -> still not a verdict. Re-run later, or use"
    echo "                                --token NAME to probe just that one."
    echo ""
    echo "      Then read the 'profile route discriminator' beneath each token: it"
    echo "      separates 'this is not an OAuth-route credential at all' from"
    echo "      '/usage specifically refuses it'."
    echo ""
    echo "Q2  What is the envelope?"
    echo "      The 'response shape' sections list every leaf path and its type."
    echo "      Claude Code's own mapper expects per-limit entries carrying"
    echo "      kind, percent, resets_at and optional scope.model.display_name."
    echo "      Confirm those names against the paths above before writing a parser."
    echo ""
    echo "Q3  Per-token or per-account?"
    echo "      Compare the percent/resets_at values ACROSS the tokens above."
    echo "      Identical figures for two tokens of the same account => per-account."
    echo "      This only answers if at least two tokens were probed."
    echo ""
    echo "Record the answers in the plan JOURNAL, not in PLAN.md."
    echo ""
} > "$TMP_REPORT" 2>&1

# --- Redaction gate ------------------------------------------------------------
# A redaction bug means a credential on disk. The diagnostic value of the report
# never outweighs that, so a failed check destroys it.

redact < "$TMP_REPORT" > "$TMP_REPORT.redacted"
mv "$TMP_REPORT.redacted" "$TMP_REPORT"

leak=""
# grep -E honours {n,} intervals per POSIX, so unlike the awk above it can use
# them; the mawk caveat in redact() does not apply here.
if grep -aqE 'sk-ant-[A-Za-z0-9_-]{4,}' "$TMP_REPORT"; then
    leak="a bearer-shaped value"
elif grep -aqE '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z][A-Za-z]+' "$TMP_REPORT"; then
    leak="an email address"
fi
if [ -n "$leak" ]; then
    rm -f "$TMP_REPORT"
    echo "ERROR: redaction check FAILED — $leak survived into the report." >&2
    echo "  Report destroyed, nothing written to $LOG." >&2
    echo "  Fix redact() before re-running." >&2
    if [ "$leak" = "a bearer-shaped value" ]; then
        echo "  Also rotate the affected token: ccy --update-token=<name>" >&2
    fi
    exit 1
fi

install -m 0600 "$TMP_REPORT" "$LOG"
cat "$LOG"

echo ""
echo "Full report saved: $LOG"
echo "(gitignored — it contains live account state)"
