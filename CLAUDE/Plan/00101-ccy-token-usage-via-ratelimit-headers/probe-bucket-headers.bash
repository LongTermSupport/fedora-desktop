#!/usr/bin/env bash
# probe-bucket-headers.bash — which rate-limit buckets does the API report, and does the
# answer depend on the model the probe is sent with?
#
# Fact-finding only: appends to the report file given as $1, renders no verdict (R9).
#
# WHY: Claude Code's bundle maps buckets to header suffixes as
# [["five_hour","5h"],["seven_day","7d"],["seven_day_overage_included","7d_oi"],
# ["overage","overage"]], reading anthropic-ratelimit-unified-${suffix}-utilization, and
# labels seven_day_overage_included "Fable limit". So the Fable figure should arrive on the
# request ccy already makes — but that is a reading of a minified bundle, not an observation.
# Unknown until a real response is seen: whether a HAIKU probe carries the 7d_oi pair (ccy
# probes with Haiku to leave the expensive allowances alone), and if not, whether a FABLE
# probe does.
#
# COST: NOT read-only. One billed POST /v1/messages per account-and-model pair, against the
# allowance it reports. Input is one character throughout. Output is capped at 1 token for
# ordinary models; a Fable or Mythos probe must allow its mandatory 2048-token thinking
# budget, so it is capped at 2100 and may actually generate some of them.
#
# Leg of triage-buckets.bash. Standalone: ./probe-bucket-headers.bash /tmp/report.md
#
# Tuning (environment, so the leg call stays a plain command):
#   PROBE_MODELS   space-separated model ids (default: claude-haiku-4-5-20251001
#                  claude-fable-5-1)
#   PROBE_ACCOUNT  "all" (the DEFAULT — every token in the pool), or a 1-based index into it
#
# It never prompts for a token. It enumerates ~/.claude-tokens/ccy/tokens/*.token directly,
# sorted, so account-N means the same account across runs.
#
# The token reaches curl through --config on STDIN so it never appears in argv (BSH-09).
# Accounts are reported as account-N, never by token filename: those are personal aliases and
# this is a public repository.
#
# EXIT CODES:
#   0  every model reached a definite answer (a 4xx IS an answer — it says this account
#      cannot probe that model)
#   1  fact-finding incomplete: no HTTP response, or no ratelimit header from any model
#  64  usage error
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

REPORT="${1:-}"
if [[ -z "${REPORT}" ]]; then
    printf 'usage: probe-bucket-headers.bash <report-file>\n' >&2
    exit 64
fi

# The token pool lives in the operator's home on the HOST. In a CCY container this does not
# fail, it reports "no tokens" — which reads as a fact about the pool when it is only a fact
# about the container (R2).
plan_require_host "the ccy token pool lives on the host; a CCY container has no token mounted"

TOKEN_DIR="${HOME}/.claude-tokens/ccy/tokens"
ENDPOINT="https://api.anthropic.com/v1/messages"
readonly TOKEN_DIR ENDPOINT

# Bucket -> header suffix, copied from the bundle's own table.
BUCKET_ROWS=(
    "5h|five_hour|session limit"
    "7d|seven_day|weekly limit"
    "7d_oi|seven_day_overage_included|Fable limit"
    "overage|overage|usage credit limit"
)

out() { printf '%s\n' "$*" >>"${REPORT}"; }

# Echoes the request body for a model. stdout is the payload; there is no chatter here.
#
# Fable and Mythos carry the `rejects_disabled_thinking` capability, which the bundle pairs
# with a 2048-token thinking budget, and `max_tokens` must exceed a thinking budget. So the
# `max_tokens: 1` body that is correct for Haiku is NOT a valid request for them.
#
# That is almost certainly why sweep 1's Fable arm returned a degenerate error — literally
# `"Error"`, no `error_code`, and no `anthropic-ratelimit-*` headers at all — while a genuine
# rate-limit rejection on the same run (account-2, Haiku) came back with the full header set,
# a real message and a `retry-after`. An invalid request cannot answer a question about
# buckets, so ask the smallest VALID one instead.
_probe_body() {
    local model="$1"
    case "${model}" in
    *fable* | *mythos*)
        printf '{"model":"%s","max_tokens":2100,"thinking":{"type":"enabled","budget_tokens":2048},"messages":[{"role":"user","content":"."}]}' \
            "${model}"
        ;;
    *)
        printf '{"model":"%s","max_tokens":1,"messages":[{"role":"user","content":"."}]}' "${model}"
        ;;
    esac
}

INCOMPLETE=0
ANY_HEADERS=0

# One row per probe, printed as a table at the end. With the whole pool probed against two
# models the per-probe sections run long, and the question — did ANY response carry 7d_oi —
# should be answerable without reading all of them.
SUMMARY_ROWS=()

# A missing tool is an IaC gap, not a skip (R11).
if [[ -z "$(command -v curl)" ]]; then
    out ""
    out "## Bucket header probe"
    out ""
    out "**UNANSWERABLE**: curl is not installed, so no probe ran. It is declared in"
    out "play-claude-code.yml — deploy it with the line below, do NOT install it by hand."
    out ""
    out '```'
    out "ansible-playbook playbooks/imports/optional/common/play-claude-code.yml"
    out '```'
    printf '[INCOMPLETE] curl is not on PATH\n' >&2
    exit 1
fi

shopt -s nullglob
TOKEN_FILES=("${TOKEN_DIR}"/*.token)
shopt -u nullglob

out ""
out "## Bucket header probe"
out ""

if [[ "${#TOKEN_FILES[@]}" -eq 0 ]]; then
    out "**UNANSWERABLE**: no token files in \`${TOKEN_DIR}\` — nothing to probe."
    out ""
    out "Create one with: \`ccy --create-token\`"
    printf '[INCOMPLETE] no tokens in %s\n' "${TOKEN_DIR}" >&2
    exit 1
fi

# Sorted, so account-N means the same account across runs.
IFS=$'\n' read -r -d '' -a TOKEN_FILES < <(printf '%s\n' "${TOKEN_FILES[@]}" | sort && printf '\0')

# DEFAULT IS THE WHOLE POOL. It was account-1 only, and that was a bad default which produced
# a misleading answer: account-1 happened to be out of usage credits, so its Fable probe was
# refused and the run said nothing about whether the bucket exists. The question is about the
# ACCOUNT POOL, so probing one member of it and generalising was never sound.
ACCOUNT="${PROBE_ACCOUNT:-all}"
ACCOUNT_INDEXES=()
if [[ "${ACCOUNT}" == "all" ]]; then
    for ((i = 1; i <= ${#TOKEN_FILES[@]}; i++)); do
        ACCOUNT_INDEXES+=("${i}")
    done
elif [[ "${ACCOUNT}" =~ ^[0-9]+$ ]] && [[ "${ACCOUNT}" -ge 1 ]] && [[ "${ACCOUNT}" -le "${#TOKEN_FILES[@]}" ]]; then
    ACCOUNT_INDEXES=("${ACCOUNT}")
else
    printf '[FATAL] PROBE_ACCOUNT=%s is neither "all" nor a number between 1 and %s\n' \
        "${ACCOUNT}" "${#TOKEN_FILES[@]}" >&2
    exit 64
fi

read -r -a MODELS <<<"${PROBE_MODELS:-claude-haiku-4-5-20251001 claude-fable-5-1}"

# One flat list of account|model pairs rather than nested loops: the probe body is identical
# either way, and flattening keeps it at one indent level.
PROBES=()
for accountIndex in "${ACCOUNT_INDEXES[@]}"; do
    for probeModel in "${MODELS[@]}"; do
        PROBES+=("${accountIndex}|${probeModel}")
    done
done

out "Pool holds ${#TOKEN_FILES[@]} accounts; probing ${#ACCOUNT_INDEXES[@]} of them against"
out "${#MODELS[@]} model(s), so ${#PROBES[@]} billed requests. The suffix-to-bucket mapping is"
out "Claude Code's own, read out of the installed bundle; the point is to see which of them"
out "the API actually sends."

# One pair of temp files for the whole run, so the trap always has a real path to remove. A
# cleanup FUNCTION would be reached only through the trap, which shellcheck reports as
# unreachable (SC2317) — and suppressing that is banned (R11).
HDR="$(mktemp)"
BODY="$(mktemp)"
trap 'rm -f "${HDR}" "${BODY}"' EXIT INT TERM HUP

for probe in "${PROBES[@]}"; do
    accountIndex="${probe%%|*}"
    model="${probe#*|}"

    out ""
    out "### account-${accountIndex} · ${model}"
    out ""

    TOKEN=""
    if ! TOKEN="$(cat "${TOKEN_FILES[$((accountIndex - 1))]}")"; then
        out "- **UNANSWERABLE**: the token file could not be read."
        SUMMARY_ROWS+=("account-${accountIndex} | ${model} | — | token unreadable")
        printf '[INCOMPLETE] could not read the token file for account-%s\n' "${accountIndex}" >&2
        INCOMPLETE=1
        continue
    fi
    if [[ -z "${TOKEN}" ]]; then
        out "- **UNANSWERABLE**: the token file is empty."
        SUMMARY_ROWS+=("account-${accountIndex} | ${model} | — | token empty")
        printf '[INCOMPLETE] the token file for account-%s is empty\n' "${accountIndex}" >&2
        INCOMPLETE=1
        continue
    fi

    # Truncate rather than re-create: curl may leave the output file untouched when a
    # response carries no body, and a stale body from the previous model would then be read
    # as this model's.
    : >"${HDR}"
    : >"${BODY}"

    # Recorded verbatim, so the report says what was actually asked. Sweep 1 could not be
    # re-read to check this, because the body it sent was only implied by the script version.
    # It holds nothing secret — a model id, a token budget and a full stop.
    requestBody="$(_probe_body "${model}")"
    out "- Request body: \`${requestBody}\`"

    code="000"
    if ! code="$(printf 'header = "Authorization: Bearer %s"\n' "${TOKEN}" |
        curl --config - \
            --silent \
            --request POST \
            --header 'content-type: application/json' \
            --header 'anthropic-version: 2023-06-01' \
            --header 'anthropic-beta: oauth-2025-04-20' \
            --data "${requestBody}" \
            --dump-header "${HDR}" \
            --connect-timeout 5 \
            --max-time 20 \
            --output "${BODY}" \
            --write-out '%{http_code}' \
            "${ENDPOINT}")"; then
        # curl's own convention: no HTTP response was received at all.
        code="000"
    fi

    # Assume the redaction is wrong and check anyway.
    if grep -aqiE 'sk-ant|authorization' "${HDR}"; then
        rm -f "${HDR}" "${BODY}"
        out "**ABORTED**: the header dump held a credential-shaped string, so it was deleted"
        out "rather than written to the report. Fix this script before re-running."
        printf '[FATAL] credential-shaped string in the header dump\n' >&2
        exit 1
    fi

    out "- HTTP status: \`${code}\` (000 = no response received at all)"

    if [[ "${code}" == "000" ]]; then
        out "- The request never reached the API, so this model answers nothing."
        SUMMARY_ROWS+=("account-${accountIndex} | ${model} | 000 | no response")
        printf '[INCOMPLETE] %s: no HTTP response\n' "${model}" >&2
        INCOMPLETE=1
        continue
    fi

    # A non-200 is data: it usually says this account has no entitlement for this model,
    # which is exactly what decides whether ccy can show the bar.
    if [[ "${code}" != "200" ]]; then
        errType="(none found in the body)"
        if bodyMatch="$(grep -aoE '"type"[[:space:]]*:[[:space:]]*"[a-z_]+_error"' "${BODY}" | head -1)"; then
            errType="${bodyMatch}"
        fi
        out "- Error type: \`${errType}\`"

        # error.details.error_code is the DECISIVE field. The bundle branches on
        # `credits_required` there, and it sits inside `details` while the top-level type stays
        # a generic `rate_limit_error` — so capturing the type alone, as the first run did,
        # cannot tell a credits refusal from a weekly limit being reached.
        errCode="(none found in the body)"
        if codeMatch="$(grep -aoE '"error_code"[[:space:]]*:[[:space:]]*"[a-z_]+"' "${BODY}" | head -1)"; then
            errCode="${codeMatch}"
        fi
        out "- Error code (\`error.details.error_code\`): \`${errCode}\`"

        errReason="(none found in the body)"
        if reasonMatch="$(grep -aoE '"disabled_reason"[[:space:]]*:[[:space:]]*"[a-z_]+"' "${BODY}" | head -1)"; then
            errReason="${reasonMatch}"
        fi
        out "- Disabled reason: \`${errReason}\`"

        # The MESSAGE separates "this account has no Fable entitlement" from "this account has
        # exhausted its Fable allowance". The type alone cannot, and that distinction decides
        # whether ccy can show the bar at all. Recording only the type left the first run
        # unable to say which had happened.
        errMsg="(none found in the body)"
        if msgMatch="$(grep -aoE '"message"[[:space:]]*:[[:space:]]*"[^"]{0,400}"' "${BODY}" | head -1)"; then
            errMsg="${msgMatch#*:}"
            errMsg="${errMsg# }"
        fi
        if printf '%s' "${errMsg}" | grep -aqiE 'sk-ant'; then
            errMsg="(withheld — it held a credential-shaped string)"
        fi
        out "- Error message: ${errMsg}"

        # A rejection can arrive BEFORE the unified rate-limit machinery attaches its headers,
        # and then these are the only evidence about it there is.
        for diag in retry-after request-id anthropic-request-id x-should-retry; do
            if diagLine="$(grep -aiE "^${diag}:" "${HDR}" | head -1)"; then
                diagValue="${diagLine#*:}"
                diagValue="${diagValue# }"
                diagValue="${diagValue%$'\r'}"
                out "- \`${diag}\`: \`${diagValue}\`"
            fi
        done

        out "- Only the fields above are recorded; the body is never written to the report."
    fi

    headers=""
    if headers="$(grep -aiE '^anthropic-ratelimit' "${HDR}")"; then
        ANY_HEADERS=1
    else
        headers=""
    fi

    if [[ -z "${headers}" ]]; then
        out "- The response carried NO anthropic-ratelimit-* headers."
        SUMMARY_ROWS+=("account-${accountIndex} | ${model} | ${code} | no ratelimit headers")
        continue
    fi

    claim="(absent)"
    if claimLine="$(printf '%s\n' "${headers}" | grep -aiE '^anthropic-ratelimit-unified-representative-claim:' | head -1)"; then
        claim="${claimLine#*:}"
        claim="${claim# }"
        claim="${claim%$'\r'}"
    fi
    out "- \`representative-claim\` (the bucket the API calls binding): \`${claim}\`"
    out ""

    # The one value the whole run exists to observe, pulled out separately so the summary
    # table can lead with it.
    oiState="absent"
    if oiLine="$(printf '%s\n' "${headers}" | grep -aiE '^anthropic-ratelimit-unified-7d_oi-utilization:' | head -1)"; then
        oiState="${oiLine#*:}"
        oiState="${oiState# }"
        oiState="${oiState%$'\r'}"
        oiState="**${oiState}**"
    fi
    SUMMARY_ROWS+=("account-${accountIndex} | ${model} | ${code} | ${oiState}")

    out "| suffix | bucket | ccy label | utilization | reset |"
    out "| ------ | ------ | --------- | ----------- | ----- |"
    for row in "${BUCKET_ROWS[@]}"; do
        suffix="${row%%|*}"
        rest="${row#*|}"
        bucket="${rest%%|*}"
        label="${rest#*|}"

        util="absent"
        if utilLine="$(printf '%s\n' "${headers}" | grep -aiE "^anthropic-ratelimit-unified-${suffix}-utilization:" | head -1)"; then
            util="${utilLine#*:}"
            util="${util# }"
            util="${util%$'\r'}"
        fi

        reset="absent"
        if resetLine="$(printf '%s\n' "${headers}" | grep -aiE "^anthropic-ratelimit-unified-${suffix}-reset:" | head -1)"; then
            reset="${resetLine#*:}"
            reset="${reset# }"
            reset="${reset%$'\r'}"
        fi

        out "| ${suffix} | ${bucket} | ${label} | ${util} | ${reset} |"
    done

    out ""
    out "Every ratelimit header this response carried, verbatim:"
    out ""
    out '```'
    out "${headers}"
    out '```'
done

out ""
out "## Summary — did any response carry the Fable bucket?"
out ""
out "| account | probe model | HTTP | 7d_oi utilization |"
out "| ------- | ----------- | ---- | ----------------- |"
for row in "${SUMMARY_ROWS[@]}"; do
    out "| ${row} |"
done

out ""
out "READ THE 7d_oi COLUMN FIRST. A utilization value there is the Fable allowance, arriving on"
out "the request ccy already makes. \`absent\` on every model means the header route cannot"
out "show it, leaving only GET /api/oauth/usage — which Plan 00100 established stored"
out "setup-tokens are refused on, for scope."

if [[ "${ANY_HEADERS}" -eq 0 ]]; then
    printf '[INCOMPLETE] no model returned any anthropic-ratelimit-* header\n' >&2
    exit 1
fi

exit "${INCOMPLETE}"
