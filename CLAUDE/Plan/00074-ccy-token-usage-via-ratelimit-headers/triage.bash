#!/usr/bin/env bash
#
# Plan 00074 — triage: what is the API actually sending for `-utilization`?
#
# HOST-ONLY. Read-only by default, safe to re-run. Gathers facts; renders NO
# verdict (that is acceptance.bash's job).
#
# WHY THIS EXISTS: every account displays "<1%", which is the exact signature of
# a 0-1 fraction being rendered as though it were already a 0-100 percentage —
# Q2 in PLAN.md, the one question the plan shipped without settling. It is NOT
# the only explanation, so this script reports the raw numbers rather than
# deciding. `<1%` is also what a genuinely idle account looks like, and the
# probe deliberately uses Haiku, whose weekly bucket may not be the bucket the
# user cares about.
#
# COST: the default run spends NOTHING — the raw values the API sent are already
# in ccy's usage cache from the last time `u` was pressed. Only --headers makes a
# live request, and then exactly one.
#
# The token is passed to curl through --config on STDIN so it never appears in
# argv (BSH-09) — /proc/<pid>/cmdline is world-readable.

set -euo pipefail

usage() {
    cat <<'EOF'
Plan 00074 triage — establish the scale of anthropic-ratelimit-unified-*-utilization

USAGE:
    triage.bash [--headers] [-h|--help]

DEFAULT (spends nothing):
    Reads ccy's usage cache and reports, per account, the raw utilisation
    values EXACTLY as the API sent them, alongside what ccy renders today and
    what it would render under the other scale.

    The cache is populated by pressing `u` in the ccy token selector. If it is
    empty or stale this script says so and stops, rather than printing an empty
    section that reads as "the API sent nothing".

--headers (spends ONE billed request):
    Makes one live POST /v1/messages against the FIRST account (Haiku,
    max_tokens=1) and dumps EVERY anthropic-ratelimit-* response header. The
    cache only keeps the four values ccy displays, so this is the only way to
    see whether other buckets or a status header are also being sent.

OUTPUT:
    Account names are reported as account-1, account-2, ... — never the token
    filenames, which are personal aliases and this is a public repository.

EXIT:
    0 = facts gathered
    1 = could not gather them (the message names what to do)
EOF
}

WANT_HEADERS=0
while [ $# -gt 0 ]; do
    case "$1" in
        -h|--help) usage; exit 0 ;;
        --headers) WANT_HEADERS=1; shift ;;
        *) echo "ERROR: unknown option: $1" >&2; echo "Try: triage.bash --help" >&2; exit 1 ;;
    esac
done

if [ -f /.dockerenv ] || [ -d /workspace/.claude ]; then
    echo "ERROR: this looks like a CCY container — the usage cache lives on the HOST." >&2
    echo "  Run this script on your HOST system instead." >&2
    exit 1
fi

# Log AFTER argument parsing, so --help never creates a directory. Resolved from
# the script's own location so the path survives the move into Completed/;
# CLAUDE/Plan/**/logs/ is gitignored, so this never reaches the public repo.
PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$PLAN_DIR/logs"
LOG="$PLAN_DIR/logs/usage-scale-triage.log"
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2

CCY_ROOT="$HOME/.claude-tokens/ccy"
TOKEN_DIR="$CCY_ROOT/tokens"
CACHE_DIR="$CCY_ROOT/usage-cache"
DEPLOYED_LIB="/var/local/claude-yolo/lib/token-management.bash"

echo "════════════════════════════════════════════════════════════════════"
echo " Plan 00074 triage — utilisation scale"
echo "════════════════════════════════════════════════════════════════════"
echo ""

# A non-zero exit is DATA here, not a failure — capture it and carry on.
probe() {
    local label="$1"; shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

# ─── What is deployed, and under what scale is it being interpreted ──────────

show_deployed() {
    if [ ! -f "$DEPLOYED_LIB" ]; then
        echo "NOT DEPLOYED: $DEPLOYED_LIB is absent."
        echo "  Deploy it with this plan's deploy.bash before reading anything below."
        return 1
    fi
    grep -m1 '^# Version:' "$DEPLOYED_LIB"
    echo ""
    echo "Scale switch as deployed (the ONLY place that knows):"
    grep -n 'CCY_USAGE_SCALE' "$DEPLOYED_LIB"
}
probe "deployed library + scale switch" show_deployed

show_env() {
    echo "CCY_USAGE_SCALE in this shell : ${CCY_USAGE_SCALE:-(unset — code defaults to 'percent')}"
    echo "CCY_USAGE_DEBUG in this shell : ${CCY_USAGE_DEBUG:-(unset)}"
    echo "CCY_USAGE_MODEL in this shell : ${CCY_USAGE_MODEL:-(unset — code defaults to Haiku)}"
    echo ""
    echo "NOTE: the probe model matters. The weekly bucket may be reported PER MODEL,"
    echo "      in which case a Haiku probe reports the Haiku weekly allowance — which"
    echo "      would be near zero for a heavy Opus user and NOT a bug in the scale."
}
probe "environment overrides" show_env

# ─── The raw values, straight out of the cache ───────────────────────────────

echo "### READ THIS FOR: the scale answer"
echo "###"
echo "###   raw > 1     => the API sends 0-100. 'percent' is correct; <1% is real."
echo "###   raw <= 1    => ambiguous from ONE sample, but a 0-1 fraction explains"
echo "###                 <1% on every account. Confirm by checking whether the"
echo "###                 'as fraction' column matches what you believe you have used."
echo ""

if [ ! -d "$CACHE_DIR" ]; then
    echo "ERROR: no usage cache at $CACHE_DIR" >&2
    echo "  ccy has never fetched usage on this machine." >&2
    echo "  Run: ccy   then press 'u' at the token selector, then quit." >&2
    echo "  That costs ONE billed Haiku request per account. Then re-run this script." >&2
    exit 1
fi

shopt -s nullglob
SUMMARIES=("$CACHE_DIR"/*.summary)
shopt -u nullglob

if [ ${#SUMMARIES[@]} -eq 0 ]; then
    echo "ERROR: the cache directory exists but holds no .summary files." >&2
    echo "  Run: ccy   then press 'u' at the token selector, then quit." >&2
    echo "  Then re-run this script." >&2
    exit 1
fi

NOW="$(date +%s)"
TTL="${CCY_USAGE_TTL:-900}"

printf '%-11s %-8s %-7s %-14s %-10s %-12s %s\n' \
    ACCOUNT STATUS BUCKET "RAW (as sent)" "AS PERCENT" "AS FRACTION" "CACHE AGE"
printf '%-11s %-8s %-7s %-14s %-10s %-12s %s\n' \
    ----------- -------- ------- -------------- ---------- ------------ ---------

# Mirrors the deployed renderer: whole-number percent, with a non-zero value
# that rounds to zero shown as <1% rather than 0%.
render_pct() {
    local v="$1" p
    [[ "$v" =~ ^[0-9]+(\.[0-9]+)?$ ]] || { printf 'not-a-number'; return 0; }
    p="$(printf '%.0f' "$v")"
    if [ "$p" = "0" ] && [[ "$v" =~ [1-9] ]]; then printf '<1%%'; else printf '%s%%' "$p"; fi
}

n=0
for summary in "${SUMMARIES[@]}"; do
    n=$(( n + 1 ))
    base="${summary%.summary}"
    status_file="$base.status"

    code="no-status-file"
    if [ -f "$status_file" ]; then
        if ! code="$(cat "$status_file")"; then
            code="unreadable"
        fi
    fi

    age="unknown"
    if mtime="$(stat -c %Y "$summary" 2>&1)"; then
        age_s=$(( NOW - mtime ))
        if [ "$age_s" -ge "$TTL" ]; then
            age="${age_s}s STALE"
        else
            age="${age_s}s"
        fi
    else
        age="stat failed: $mtime"
    fi

    record=""
    if ! record="$(cat "$summary")"; then
        printf '%-11s %-8s %s\n' "account-$n" "$code" "(summary unreadable)"
        continue
    fi
    if [ -z "$record" ]; then
        printf '%-11s %-8s %s\n' "account-$n" "$code" "(empty — no ratelimit headers came back)"
        continue
    fi

    IFS=$'\t' read -r u5 r5 u7 r7 <<< "$record"
    for bucket in "5h:$u5" "7d:$u7"; do
        bname="${bucket%%:*}"
        raw="${bucket#*:}"
        if [ -z "$raw" ]; then
            printf '%-11s %-8s %-7s %s\n' "account-$n" "$code" "$bname" \
                "(absent — this bucket is SILENTLY DROPPED by the renderer)"
            continue
        fi
        as_pct="$(render_pct "$raw")"
        as_frac="not-a-number"
        if [[ "$raw" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
            as_frac="$(render_pct "$(awk -v x="$raw" 'BEGIN { printf "%.4f", x * 100 }')")"
        fi
        printf '%-11s %-8s %-7s %-14s %-10s %-12s %s\n' \
            "account-$n" "$code" "$bname" "$raw" "$as_pct" "$as_frac" "$age"
    done

    # Reset epochs are a second, independent check on whether the record is sane.
    printf '%-11s %-8s %-7s resets: 5h=%s 7d=%s\n' "account-$n" "" "" "${r5:-none}" "${r7:-none}"
done

echo ""
echo "AS PERCENT  = what ccy displays today (CCY_USAGE_SCALE unset/percent)"
echo "AS FRACTION = what ccy would display with CCY_USAGE_SCALE=fraction"
echo ""

# ─── Optional live probe: every ratelimit header, not just the four we keep ──

if [ "$WANT_HEADERS" -eq 1 ]; then
    echo "════════════════════════════════════════════════════════════════════"
    echo " Live probe — ALL anthropic-ratelimit-* headers (ONE billed request)"
    echo "════════════════════════════════════════════════════════════════════"
    echo ""

    if ! command -v curl > /dev/null; then
        echo "ERROR: curl is not installed." >&2
        echo "  It is declared in play-claude-code.yml. Deploy it with:" >&2
        echo "    ansible-playbook playbooks/imports/optional/common/play-claude-code.yml" >&2
        echo "  Do NOT install it by hand." >&2
        exit 1
    fi

    shopt -s nullglob
    TOKEN_FILES=("$TOKEN_DIR"/*.token)
    shopt -u nullglob
    if [ ${#TOKEN_FILES[@]} -eq 0 ]; then
        echo "ERROR: no token files in $TOKEN_DIR — nothing to probe." >&2
        exit 1
    fi

    TOKEN=""
    if ! TOKEN="$(cat "${TOKEN_FILES[0]}")"; then
        echo "ERROR: could not read the first token file." >&2
        exit 1
    fi
    if [ -z "$TOKEN" ]; then
        echo "ERROR: the first token file is empty." >&2
        exit 1
    fi

    HDR="$(mktemp)"
    BODY="$(mktemp)"
    CODE="000"
    if ! CODE="$(printf 'header = "Authorization: Bearer %s"\n' "$TOKEN" \
        | curl --config - \
               --silent \
               --request POST \
               --header 'content-type: application/json' \
               --header 'anthropic-version: 2023-06-01' \
               --header 'anthropic-beta: oauth-2025-04-20' \
               --data '{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"."}]}' \
               --dump-header "$HDR" \
               --connect-timeout 5 \
               --max-time 20 \
               --output "$BODY" \
               --write-out '%{http_code}' \
               "https://api.anthropic.com/v1/messages")"; then
        CODE="000"
    fi

    echo "HTTP status: $CODE   (000 = no response received at all)"
    echo ""
    echo "Every ratelimit header the API returned:"
    if ! grep -i '^anthropic-ratelimit' "$HDR"; then
        echo "  (none — the response carried no anthropic-ratelimit-* headers)"
    fi
    echo ""

    # The Authorization header is in the REQUEST, not the response, so it is not
    # in $HDR — but assume the redaction is wrong and check it anyway. A token on
    # disk is never worth the diagnostic value of the dump.
    if grep -aqiE 'sk-ant|authorization' "$HDR"; then
        rm -f "$HDR" "$BODY"
        echo "ERROR: the header dump contains a credential-shaped string." >&2
        echo "  Dump deleted rather than logged. Fix this script before re-running." >&2
        exit 1
    fi
    rm -f "$HDR" "$BODY"
fi

echo "════════════════════════════════════════════════════════════════════"
echo " Read the RAW column first. Everything else is derived from it."
echo "════════════════════════════════════════════════════════════════════"
