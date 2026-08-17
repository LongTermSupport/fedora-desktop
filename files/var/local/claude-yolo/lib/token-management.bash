#!/bin/bash
# Token Management Library
# Token operations for claude-yolo (ccy)
#
# Version: 1.10.0 - Plan 00074: usage display rewritten as aligned, coloured bars
#                  with the reset time spelled out in words ("resets in 4 hours",
#                  not "r4h"). Cache now holds the VALUES rather than a rendered
#                  line, because the bars need the numbers. CCY_USAGE_SCALE is
#                  the single switch for the undocumented utilisation scale, and
#                  CCY_USAGE_DEBUG shows the raw value so it can be settled.
#          1.9.0 - Plan 00074: per-token usage limits RESTORED, on demand only.
#                  Read from the anthropic-ratelimit-unified-* RESPONSE HEADERS
#                  on POST /v1/messages, which a setup-token CAN reach — not the
#                  403-ing status route below. Press `u` in the selector; never
#                  automatic, because each fetch is a billed request against the
#                  allowance it reports.
#          1.8.0 - Plan 00073: per-token usage limits REMOVED (added in 1.7.0).
#                  The stored sk-ant-oat01 setup-tokens cannot read them:
#                  GET /api/oauth/usage and /api/oauth/profile both answer 403
#                  "OAuth token does not meet scope requirement user:profile"
#                  for every token. The scope is fixed when `claude setup-token`
#                  mints the token, so no client-side change can obtain it.
#                  Do not re-add a call to those routes with a setup-token.
#          1.6.0 - BSH-04 (PIPESTATUS for setup-token), BSH-05 (CREATED_TOKEN_FILE
#                  contract so create/renew flows continue or exit cleanly),
#                  BSH-09 (tokens passed by env NAME, never in container argv),
#                  CCY-06 (byte-range message matches the 90-120 accepted range)

# Returns expiry_date string wrapped in ANSI color based on days remaining
# Args: $1 = expiry_date (YYYY-MM-DD)
# Outputs: colored string (or plain if terminal doesn't support colors)
colorize_expiry() {
    local expiry_date="$1"
    local today
    today=$(date +%Y-%m-%d)

    # Calculate days remaining
    local expiry_epoch today_epoch days_remaining
    expiry_epoch=$(date -d "$expiry_date" +%s 2>/dev/null) || { echo "$expiry_date"; return; }
    today_epoch=$(date -d "$today" +%s)
    days_remaining=$(( (expiry_epoch - today_epoch) / 86400 ))

    local RED='\033[31m'
    local ORANGE='\033[38;5;208m'
    local GREEN='\033[32m'
    local RESET='\033[0m'

    if [ "$days_remaining" -le 5 ]; then
        printf "${RED}%s${RESET}" "$expiry_date"
    elif [ "$days_remaining" -le 30 ]; then
        printf "${ORANGE}%s${RESET}" "$expiry_date"
    else
        printf "${GREEN}%s${RESET}" "$expiry_date"
    fi
}

# ─── Per-account usage limits (Plan 00074) ──────────────────────────────────
#
# HUMAN-TRIGGERED ONLY. Pressing `u` in the token selector fetches each
# account's 5-hour and weekly utilisation and redraws the menu with it.
#
# It must not become automatic. Plan 00073 tried the free status route
# (GET /api/oauth/usage) and every stored setup-token was refused on scope. The
# figures are reachable only as anthropic-ratelimit-unified-* RESPONSE HEADERS
# on /v1/messages, so reading them costs a real, billed request that consumes a
# sliver of the allowance it reports. Fetching at launch would spend quota
# nobody asked to spend.
#
# Haiku, max_tokens=1, one character of input: the weekly buckets are per-model,
# so probing with the cheapest model leaves the Opus/Sonnet allowances alone.
#
# Set CCY_TOKEN_USAGE=0 to remove the option from the menu entirely.

CCY_USAGE_ENDPOINT="${CCY_USAGE_ENDPOINT:-https://api.anthropic.com/v1/messages}"
CCY_USAGE_MODEL="${CCY_USAGE_MODEL:-claude-haiku-4-5-20251001}"
CCY_USAGE_TIMEOUT="${CCY_USAGE_TIMEOUT:-20}"
CCY_USAGE_CONNECT_TIMEOUT="${CCY_USAGE_CONNECT_TIMEOUT:-5}"
# Long TTL on purpose. In Plan 00073 a cache miss cost latency; here it costs
# QUOTA, so pressing `u` twice in one sitting must not bill twice.
CCY_USAGE_TTL="${CCY_USAGE_TTL:-900}"

_usage_cache_dir() {
    printf '%s/usage-cache' "$(dirname "$1")"
}

usage_enabled() {
    [ "${CCY_TOKEN_USAGE:-1}" != "0" ]
}

# Width of the utilisation bar, in cells.
CCY_USAGE_BAR_WIDTH="${CCY_USAGE_BAR_WIDTH:-20}"

# Spells a reset time out in words — "resets in 4 hours", "resets in 20 minutes".
# Deliberately NOT abbreviated: the first version rendered this as "r4h", which
# is unreadable unless you already know what it means, and saving eight columns
# in a menu bought nothing.
#
# Rounds to NEAREST, not floor. Floor is a full unit wrong the moment any time
# has passed, showing a reset exactly 3 days out as "2 days".
_usage_human_reset() {
    local epoch="$1" now="$2" delta n unit
    [[ "$epoch" =~ ^[0-9]+$ ]] || return 0
    delta=$(( epoch - now ))
    if [ "$delta" -le 0 ]; then
        printf 'resetting now'
        return 0
    fi

    if [ "$delta" -lt 5400 ]; then
        n=$(( (delta + 30) / 60 )); unit="minute"
    elif [ "$delta" -lt 172800 ]; then
        n=$(( (delta + 1800) / 3600 )); unit="hour"
    else
        n=$(( (delta + 43200) / 86400 )); unit="day"
    fi
    [ "$n" -eq 1 ] || unit="${unit}s"
    printf 'resets in %d %s' "$n" "$unit"
}

# Echoes the whole-number percentage for a normalised (0-100) utilisation value,
# or fails when the value is not a number.
_usage_pct_int() {
    local v="$1"
    [[ "$v" =~ ^[0-9]+(\.[0-9]+)?$ ]] || return 1
    printf '%.0f' "$v"
}

# Echoes the percentage as it is displayed, right-aligned in 4 columns.
#
# A value that is non-zero but rounds to zero shows "<1%", never "0%" — "0%"
# claims the account is untouched, which is a different statement from "barely
# touched".
_usage_pct_label() {
    local v="$1" p
    p="$(_usage_pct_int "$v")" || return 1
    if [ "$p" = "0" ] && [[ "$v" =~ [1-9] ]]; then
        printf '%4s' '<1%'
    else
        printf '%3s%%' "$p"
    fi
}

# Draws a proportional bar, filled portion coloured by how much is used and the
# remainder in a dim track — the same visual grammar as the Claude Code status
# line, so it reads without explanation.
_usage_bar() {
    local p="$1"
    local width="$CCY_USAGE_BAR_WIDTH"
    local RED='\033[31m' ORANGE='\033[38;5;208m' GREEN='\033[32m'
    local TRACK='\033[90m' RESET='\033[0m'

    [ "$p" -ge 0 ] 2>/dev/null || p=0
    [ "$p" -le 100 ] || p=100

    local filled
    filled=$(( (p * width + 50) / 100 ))
    [ "$filled" -le "$width" ] || filled="$width"

    local colour="$GREEN"
    if [ "$p" -ge 85 ]; then
        colour="$RED"
    elif [ "$p" -ge 50 ]; then
        colour="$ORANGE"
    fi

    local bar_full="" bar_empty="" i
    for (( i = 0; i < filled; i++ )); do bar_full="${bar_full}█"; done
    for (( i = filled; i < width; i++ )); do bar_empty="${bar_empty}░"; done

    printf "${colour}%s${TRACK}%s${RESET}" "$bar_full" "$bar_empty"
}

# Renders one bucket as a full, aligned, self-explanatory line:
#
#        5-hour limit   ███████░░░░░░░░░░░░░   34%   resets in 4 hours
#
# Args: $1 = label, $2 = normalised percent, $3 = reset epoch, $4 = now
_usage_bucket_line() {
    local label="$1" raw="$2" reset="$3" now="$4"
    local value p pct_label reset_text raw_note=""

    # Interpret the scale here, not in the cache — see _usage_extract.
    value="$(_usage_normalise "$raw")"
    p="$(_usage_pct_int "$value")" || return 1
    pct_label="$(_usage_pct_label "$value")"
    reset_text="$(_usage_human_reset "$reset" "$now")"

    # CCY_USAGE_DEBUG exposes the number as the API sent it. It exists because
    # the scale of `-utilization` is undocumented (Plan 00074, Q2) and this is
    # how it gets settled without spending an extra request: the raw value is
    # already in the cache from the fetch the user asked for.
    if [ -n "${CCY_USAGE_DEBUG:-}" ]; then
        raw_note="   [API sent ${raw}]"
    fi

    printf '       %-14s %b %s   %s%s\n' \
        "$label" "$(_usage_bar "$p")" "$pct_label" "$reset_text" "$raw_note"
}

# Extracts the four values we display from a dumped response-header file and
# echoes them as one tab-separated cache record: u5 r5 u7 r7.
#
# Utilisation is NORMALISED to 0-100 here, at the single point of entry, so no
# display code has to know about the scale. See CCY_USAGE_SCALE.
#
# Pure bash, single pass — no jq, no grep, no subprocess. Plan 00073 measured jq
# at ~40 ms per call, more than the network round trip it was parsing.
_usage_extract() {
    local file="$1"
    local line name value u5="" u7="" r5="" r7=""

    while IFS= read -r line; do
        line="${line%$'\r'}"
        case "$line" in
            [Aa]nthropic-[Rr]atelimit-unified-*) ;;
            *) continue ;;
        esac
        name="${line%%:*}"
        value="${line#*:}"
        value="${value# }"
        case "${name,,}" in
            anthropic-ratelimit-unified-5h-utilization) u5="$value" ;;
            anthropic-ratelimit-unified-7d-utilization) u7="$value" ;;
            anthropic-ratelimit-unified-5h-reset)       r5="$value" ;;
            anthropic-ratelimit-unified-7d-reset)       r7="$value" ;;
        esac
    done < "$file"

    if [ -z "$u5" ] && [ -z "$u7" ]; then
        return 1
    fi

    # Stored EXACTLY as the API sent it. Normalising here would make
    # CCY_USAGE_DEBUG's "raw" a lie under CCY_USAGE_SCALE=fraction, and would
    # freeze the scale into the cache — so flipping the switch would need a
    # refetch, which costs quota. Interpreting at display time means the switch
    # takes effect on data already in hand.
    printf '%s\t%s\t%s\t%s' "$u5" "$r5" "$u7" "$r7"
}

# Converts a raw utilisation value to a 0-100 percentage.
#
# CCY_USAGE_SCALE says how the API expresses it, and is the ONLY place that
# knows: "percent" (the value is already 0-100) or "fraction" (0-1, multiply by
# 100). The scale is not documented anywhere, so this is a single switch rather
# than an assumption scattered through the renderer.
_usage_normalise() {
    local v="$1"
    [[ "$v" =~ ^[0-9]+(\.[0-9]+)?$ ]] || { printf '%s' "$v"; return 0; }
    if [ "${CCY_USAGE_SCALE:-percent}" = "fraction" ]; then
        printf '%s' "$(awk -v x="$v" 'BEGIN { printf "%.4f", x * 100 }')"
        return 0
    fi
    printf '%s' "$v"
}

# Fetches ONE account's usage and renders it, writing two cache files:
#   $2.status   the HTTP status (000 when no response was received at all)
#   $2.summary  the finished display line (empty when no headers came back)
#
# Rendering happens HERE, inside the parallel worker, so a cache hit costs
# nothing but a file read at display time.
#
# Only the rendered line is persisted — the response body is never written to
# disk, so account details in the payload do not outlive the fetch.
#
# The token reaches curl through --config on STDIN so it never appears in argv
# (BSH-09), which is world-readable via /proc/<pid>/cmdline.
#
# Every failure is RECORDED as a status rather than raised, and both files are
# always created, so a fan-out worker can never abort a caller running `set -e`.
# Always returns 0.
_usage_fetch_one() {
    local token="$1" out_prefix="$2"
    local hdr body code="000" line=""

    hdr="$(mktemp)"; body="$(mktemp)"
    printf '' > "$out_prefix.summary"

    if code="$(printf 'header = "Authorization: Bearer %s"\n' "$token" \
        | curl --config - \
               --silent \
               --request POST \
               --header 'content-type: application/json' \
               --header 'anthropic-version: 2023-06-01' \
               --header 'anthropic-beta: oauth-2025-04-20' \
               --data "{\"model\":\"$CCY_USAGE_MODEL\",\"max_tokens\":1,\"messages\":[{\"role\":\"user\",\"content\":\".\"}]}" \
               --dump-header "$hdr" \
               --connect-timeout "$CCY_USAGE_CONNECT_TIMEOUT" \
               --max-time "$CCY_USAGE_TIMEOUT" \
               --output "$body" \
               --write-out '%{http_code}' \
               "$CCY_USAGE_ENDPOINT")"; then
        :
    else
        # curl itself failed (DNS, TLS, refused, timeout). 000 is curl's own
        # convention for "no HTTP response was received".
        code="000"
    fi

    # Keep values from ANY status that carried the headers, not just 200. A 429
    # is precisely when the numbers matter most, and it carries them too.
    #
    # The cache holds the VALUES, not a rendered line: the display draws bars and
    # needs the numbers. Rendering at fetch time made sense in Plan 00073 only
    # because parsing then cost a jq process; it is pure bash now, so there is
    # nothing to amortise and a value cache is the more useful thing to keep.
    if line="$(_usage_extract "$hdr")"; then
        printf '%s' "$line" > "$out_prefix.summary"
    fi

    printf '%s\n' "$code" > "$out_prefix.status"
    rm -f "$hdr" "$body"
    chmod 600 "$out_prefix.summary" "$out_prefix.status"
    return 0
}

# Refreshes the cache for every given token file, fanning the fetches out in
# parallel so N accounts cost roughly one round trip rather than N.
#
# Args: $1 = token_dir, $2.. = token file paths
usage_prime_cache() {
    local token_dir="$1"; shift
    usage_enabled || return 0
    command -v curl > /dev/null || return 0

    local cache_dir
    cache_dir="$(_usage_cache_dir "$token_dir")"
    mkdir -p "$cache_dir"
    chmod 700 "$cache_dir"

    local now pids=() token_file filename token_name status mtime age
    now="$(date +%s)"

    for token_file in "$@"; do
        filename="$(basename "$token_file")"
        token_name="${filename%.*.token}"
        status="$cache_dir/$token_name.status"

        # Fresh enough? Skip the network — and, here, skip spending quota.
        if [ -f "$status" ] && [ -f "$cache_dir/$token_name.summary" ]; then
            if mtime="$(stat -c %Y "$status")"; then
                age=$(( now - mtime ))
                if [ "$age" -ge 0 ] && [ "$age" -lt "$CCY_USAGE_TTL" ]; then
                    continue
                fi
            fi
        fi

        (
            # Fan-out worker. Unconditionally exits 0: its job is to RECORD an
            # outcome, and a worker that died would otherwise take the caller's
            # `set -e` — and the whole menu — down with it. Not hypothetical: in
            # Plan 00073 an unreachable endpoint aborted the render and printed
            # no token list at all.
            # $$-suffixed prefix so two concurrent ccy launches cannot scribble
            # over each other's part-files.
            token="$(cat "$token_file")"
            if [ -n "$token" ]; then
                _usage_fetch_one "$token" "$cache_dir/$token_name.$$"
                mv -f "$cache_dir/$token_name.$$.summary" \
                      "$cache_dir/$token_name.summary"
                mv -f "$cache_dir/$token_name.$$.status" \
                      "$cache_dir/$token_name.status"
            fi
            exit 0
        ) &
        pids+=("$!")
    done

    if [ ${#pids[@]} -gt 0 ]; then
        local pid
        for pid in "${pids[@]}"; do
            if ! wait "$pid"; then
                # A worker died without publishing. Its cache entry is left as
                # it was, so the row reports "not fetched" rather than the menu
                # failing. Nothing to clean up — part-files live in the private
                # cache dir.
                continue
            fi
        done
    fi
    return 0
}

# Prints the indented usage block for one account — two bar lines when the
# figures are known, otherwise a single dim line saying why they are not.
#
#        5-hour limit   ███████░░░░░░░░░░░░░   34%   resets in 4 hours
#        weekly limit   ██░░░░░░░░░░░░░░░░░░    8%   resets in 6 days
#
# Never silent about a failure: an account that could not be read says so on its
# own row rather than vanishing or, worse, rendering as though it were idle.
#
# Pure cache read — no curl, no network.
usage_render_block() {
    local token_dir="$1" token_name="$2"
    local DIM='\033[2m' RESET='\033[0m'
    local cache_dir status code record="" now
    local u5 r5 u7 r7

    usage_enabled || return 0

    _usage_note() { printf "       ${DIM}usage unavailable — %s${RESET}\n" "$1"; }

    # A missing tool here is an IaC gap, not a runtime condition to shrug at.
    # curl is declared in play-claude-code.yml — name the remedy.
    if ! command -v curl > /dev/null; then
        _usage_note "needs curl (run play-claude-code.yml)"
        return 0
    fi

    cache_dir="$(_usage_cache_dir "$token_dir")"
    status="$cache_dir/$token_name.status"

    if [ ! -f "$status" ]; then
        _usage_note "not fetched"
        return 0
    fi

    if [ -f "$cache_dir/$token_name.summary" ]; then
        record="$(cat "$cache_dir/$token_name.summary")"
    fi

    # Real figures win over the status word — a 429 carrying numbers is more
    # useful than being told "rate limited".
    if [ -z "$record" ]; then
        code="$(cat "$status")"
        case "$code" in
            000)      _usage_note "could not reach the API" ;;
            401|403)  _usage_note "this token was not authorised to read it" ;;
            429)      _usage_note "rate limited" ;;
            200)      _usage_note "the API reported no limits" ;;
            *)        _usage_note "HTTP $code" ;;
        esac
        return 0
    fi

    IFS=$'\t' read -r u5 r5 u7 r7 <<< "$record"
    now="$(date +%s)"

    # A bucket line fails only when the API sent something that is not a number.
    # Say so on the row rather than dropping it — a silently missing bucket reads
    # as "this account has no weekly limit", which would be a lie.
    local bucket
    for bucket in "5-hour limit:$u5:$r5" "weekly limit:$u7:$r7"; do
        local label="${bucket%%:*}" rest="${bucket#*:}"
        local value="${rest%%:*}" reset="${rest#*:}"
        if [ -z "$value" ]; then
            continue
        fi
        if ! _usage_bucket_line "$label" "$value" "$reset" "$now"; then
            _usage_note "$label: the API sent a value that is not a number"
        fi
    done
    return 0
}

# Function to list available tokens
# Args: $1 = token_dir, $2 = tool_name (for display)
list_tokens() {
    local token_dir="$1"
    local tool_name="${2:-YOLO Mode}"

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "Available Claude Code Tokens for $tool_name"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""

    if [ ! -d "$token_dir" ] || [ -z "$(ls -A "$token_dir"/*.token 2>/dev/null)" ]; then
        echo "No tokens found in: $token_dir"
        echo ""
        echo "Create a token with: ccy --create-token"
        echo ""
        return 1
    fi

    echo "Token storage: $token_dir"
    echo ""

    local today
    today=$(date +%Y-%m-%d)

    for token_file in "$token_dir"/*.token; do
        if [ -f "$token_file" ]; then
            local filename
            filename=$(basename "$token_file")
            local token_name="${filename%.*.token}"

            # Extract expiry date from filename
            if [[ "$filename" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$ ]]; then
                local expiry_date="${BASH_REMATCH[1]}"
                local status="✓ Valid"

                if [[ "$expiry_date" < "$today" ]]; then
                    status="✗ EXPIRED"
                elif [[ "$expiry_date" == "$today" ]]; then
                    status="⚠ Expires TODAY"
                fi

                echo "  • $token_name"
                echo "    File: $token_file"
                echo "    Expires: $(colorize_expiry "$expiry_date") ($status)"
            else
                echo "  • $filename"
                echo "    File: $token_file"
                echo "    Status: ✗ INVALID FORMAT (missing expiry date)"
            fi
            echo ""
        fi
    done

    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
}

# Function to validate a token by testing it against the Claude API
# Args: $1 = token to validate, $2 = image_name
# Returns: 0 if valid, 1 if invalid
validate_token() {
    local token="$1"
    local image_name="$2"

    # Test the token by making a simple API call
    # Claude Code with a valid token should be able to show version.
    #
    # BSH-09: pass the token by NAME (-e CLAUDE_CODE_OAUTH_TOKEN, no '=value') so
    # the secret is taken from this process's environment and never appears in the
    # container run's argv, which is world-readable via /proc/<pid>/cmdline. The
    # variable is declared 'local' so the export is scoped to this function.
    local CLAUDE_CODE_OAUTH_TOKEN="$token"
    export CLAUDE_CODE_OAUTH_TOKEN
    if container_cmd run --rm \
        -e CLAUDE_CODE_OAUTH_TOKEN \
        --entrypoint claude \
        "$image_name" \
        --version >/dev/null 2>/dev/null; then
        return 0  # Token works
    else
        return 1  # Token invalid
    fi
}

# Function to create a new long-lived token
# Args: $1 = token_dir, $2 = gh_token, $3 = image_name, $4 = tool_name (for display)
#       $5 = preset_name (optional, skip name prompt if provided)
create_token() {
    local token_dir="$1"
    local gh_token="$2"
    local image_name="$3"
    local tool_name="${4:-ccy}"
    local preset_name="$5"

    # BSH-05: callers and select_token read this to know whether a token was
    # actually created (so they can continue to launch or exit cleanly) versus
    # the user cancelling. Empty = no token created on this call.
    CREATED_TOKEN_FILE=""

    # CRITICAL: GH_TOKEN must be set before calling this function
    # It's required for the container's git/gh functionality
    if [ -z "$gh_token" ]; then
        echo "" >&2
        print_error "create_token() called without GH_TOKEN"
        echo "This is an internal script error." >&2
        echo "" >&2
        echo "GH_TOKEN is required for the container's git/gh functionality." >&2
        echo "Token creation must happen AFTER SSH/GH setup." >&2
        return 1
    fi

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "Create New Long-Lived Token for YOLO Mode"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "This will create a long-lived OAuth token (sk-ant-oat01-...) for $tool_name."
    echo "These tokens are designed for container/CI-CD usage and last much longer"
    echo "than regular OAuth tokens."
    echo ""
    echo "Requirements:"
    echo "  • Active Claude Pro or Max subscription"
    echo "  • Authentication will happen in a clean container"
    echo ""

    # Prompt for token name (or use preset)
    if [ -n "$preset_name" ]; then
        token_name="$preset_name"
        echo "Renewing token: $token_name"
    else
        while true; do
            read -r -p "Enter a name for this token (e.g., 'personal', 'work', 'default'): " token_name

            if [ -z "$token_name" ]; then
                print_error "Token name cannot be empty"
                continue
            fi

            # Validate token name (alphanumeric, dash, underscore only)
            if ! [[ "$token_name" =~ ^[a-zA-Z0-9_-]+$ ]]; then
                print_error "Token name must contain only letters, numbers, dashes, and underscores"
                continue
            fi

            break
        done
    fi

    # Use conservative 90-day expiry estimate
    # NOTE: claude setup-token doesn't tell us when the token actually expires,
    # so we use 90 days as a conservative estimate. If you get auth errors before
    # that, just recreate the token.
    local expiry_date
    expiry_date=$(date -d "+90 days" +%Y-%m-%d)
    echo ""
    echo "Token expiry: $expiry_date (90 days from today)"
    echo "Note: This is an estimate - recreate the token if you get auth errors"
    echo ""

    token_file="$token_dir/${token_name}.${expiry_date}.token"

    # Check if token already exists
    local existing_tokens=("$token_dir/${token_name}".*.token)
    if [ -f "${existing_tokens[0]}" ] && [ "${existing_tokens[0]}" != "$token_dir/${token_name}.*.token" ]; then
        echo ""
        echo "⚠  Found existing token(s) for '$token_name':"
        for old_token in "$token_dir/${token_name}".*.token; do
            if [ -f "$old_token" ]; then
                echo "    $(basename "$old_token")"
            fi
        done
        echo ""
        read -r -p "Overwrite? (y/N): " overwrite
        if [ "$overwrite" != "y" ] && [ "$overwrite" != "Y" ]; then
            echo "Cancelled. Token not created."
            return 0
        fi
    fi

    echo ""
    echo "Creating token: $token_name"
    echo "Expiry date: $expiry_date"
    echo "Storage: $token_file"
    echo ""

    # Create temporary output file for token
    tmp_output=$(mktemp "/tmp/${tool_name}-token-setup-XXXXXX")

    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "Launching Claude Code container for token setup..."
    echo ""
    echo "INSTRUCTIONS:"
    echo "  1. The container will run 'claude setup-token'"
    echo "  2. Follow the authentication flow (CLI → Browser → CLI)"
    echo "  3. Copy the token when it's displayed (starts with sk-ant-oat01-)"
    echo "  4. The process will save it automatically"
    echo ""
    echo "Press Enter to continue..."
    read -r
    echo ""

    # Run setup-token via claude CLI entrypoint
    # GH_TOKEN provided for container's gh CLI (not for Claude auth)
    # Claude auth happens via OAuth flow in browser
    echo "Running: $CONTAINER_ENGINE run --rm --entrypoint claude \"$image_name\" setup-token"
    echo ""

    # BSH-09: GH_TOKEN by name (not -e VAR=value) keeps it out of the run argv.
    # BSH-04: without pipefail the 'if ... | tee' would test tee's status (always
    # 0), so the failure diagnostics below were unreachable and a failed
    # setup-token fell through to the success branch. Capture the container's own
    # status via PIPESTATUS[0] and branch on that instead.
    export GH_TOKEN="$gh_token"
    container_cmd run -it --rm \
        --entrypoint claude \
        -e GH_TOKEN \
        "$image_name" \
        setup-token 2>&1 | tee "$tmp_output"
    docker_exit_code="${PIPESTATUS[0]}"

    if [ "$docker_exit_code" -eq 0 ]; then

        echo ""
        echo "════════════════════════════════════════════════════════════════════════════"
        echo ""

        # Try to extract token from output
        token=$(grep -o 'sk-ant-oat01-[a-zA-Z0-9_-]\+' "$tmp_output" | head -1)

        if [ -n "$token" ]; then
            echo ""
            echo "Validating token..."

            if validate_token "$token" "$image_name"; then
                echo "✓ Token validated successfully"
                echo ""

                # Save token to file
                echo "$token" > "$token_file"
                chmod 600 "$token_file"

                # Remove old tokens for this name
                for old_token in "$token_dir/${token_name}".*.token; do
                    if [ -f "$old_token" ] && [ "$old_token" != "$token_file" ]; then
                        echo "✓ Removed old token: $(basename "$old_token")"
                        rm -f "$old_token"
                    fi
                done

                echo ""
                echo "════════════════════════════════════════════════════════════════════════════"
                echo "✓ Token created successfully!"
                echo "════════════════════════════════════════════════════════════════════════════"
            else
                echo ""
                print_error "Token validation failed"
                echo "The extracted token does not authenticate properly."
                echo "Please try creating the token again."
                rm -f "$tmp_output"
                return 1
            fi
            echo ""
            echo "Token: $token_name"
            echo "Expires: $expiry_date"
            echo "File: $token_file"
            echo ""
            echo "You can now use this token with:"
            echo "  $tool_name --token $token_name"
            echo ""
            echo "Or just run '$tool_name' and select it from the menu."
            echo ""
        else
            echo ""
            echo "════════════════════════════════════════════════════════════════════════════"
            echo "⚠  WARNING: Could not extract token from output"
            echo "════════════════════════════════════════════════════════════════════════════"
            echo ""
            echo "The setup-token command ran, but we couldn't automatically extract the token."
            echo ""

            # Token paste loop with validation and retry
            while true; do
                echo "Please manually paste the token (starts with sk-ant-oat01-):"
                read -r -p "Token: " manual_token

                # Basic validation: format check
                if [ -z "$manual_token" ]; then
                    echo ""
                    print_error "Token cannot be empty"
                    echo ""
                    read -r -p "Try again? (Y/n): " retry
                    if [ "$retry" = "n" ] || [ "$retry" = "N" ]; then
                        echo "Cancelled."
                        rm -f "$tmp_output"
                        return 1
                    fi
                    echo ""
                    continue
                fi

                if ! [[ "$manual_token" =~ ^sk-ant-oat01- ]]; then
                    echo ""
                    print_error "Invalid token format"
                    echo "Token must start with 'sk-ant-oat01-'"
                    echo ""
                    read -r -p "Try again? (Y/n): " retry
                    if [ "$retry" = "n" ] || [ "$retry" = "N" ]; then
                        echo "Cancelled."
                        rm -f "$tmp_output"
                        return 1
                    fi
                    echo ""
                    continue
                fi

                # Quick validation: Check byte length
                token_bytes=${#manual_token}
                if [ "$token_bytes" -lt 90 ] || [ "$token_bytes" -gt 120 ]; then
                    echo ""
                    print_error "Invalid token length"
                    echo "Length: $token_bytes bytes (expected: 90-120 bytes)"
                    echo "Token appears truncated or has extra characters."
                    echo ""
                    read -r -p "Try again? (Y/n): " retry
                    if [ "$retry" = "n" ] || [ "$retry" = "N" ]; then
                        echo "Cancelled."
                        rm -f "$tmp_output"
                        return 1
                    fi
                    echo ""
                    continue
                fi

                # API validation: Test against Claude API
                echo ""
                echo "Validating token against Claude API..."

                if validate_token "$manual_token" "$image_name"; then
                    echo "✓ Token validated successfully"
                    echo ""

                    # Save token to file
                    echo "$manual_token" > "$token_file"
                    chmod 600 "$token_file"

                    # Remove old tokens
                    for old_token in "$token_dir/${token_name}".*.token; do
                        if [ -f "$old_token" ] && [ "$old_token" != "$token_file" ]; then
                            rm -f "$old_token"
                        fi
                    done

                    echo ""
                    echo "✓ Token saved successfully!"
                    echo ""
                    break
                else
                    echo ""
                    print_error "Token validation failed"
                    echo "The provided token does not authenticate with Claude API."
                    echo ""
                    echo "Possible causes:"
                    echo "  • Token was copied incorrectly (missing characters)"
                    echo "  • Token has expired or been revoked"
                    echo "  • Network connectivity issues"
                    echo ""
                    read -r -p "Try again? (Y/n): " retry
                    if [ "$retry" = "n" ] || [ "$retry" = "N" ]; then
                        echo ""
                        echo "Cancelled. Please verify the token and try again."
                        echo "Run: $tool_name --create-token"
                        rm -f "$tmp_output"
                        return 1
                    fi
                    echo ""
                fi
            done
        fi
    else
        # docker_exit_code was captured from PIPESTATUS[0] above (the container's
        # real status), so the 125/126/127 diagnostics below are now reachable.
        echo ""
        echo "════════════════════════════════════════════════════════════════════════════"
        print_error "Token Creation Failed"
        echo "════════════════════════════════════════════════════════════════════════════"
        echo ""

        if [ "$docker_exit_code" -eq 125 ]; then
            echo "Docker container failed to start."
            echo "The container image may be corrupted."
            echo ""
            echo "Try rebuilding: $tool_name --rebuild"
        elif [ "$docker_exit_code" -eq 126 ] || [ "$docker_exit_code" -eq 127 ]; then
            echo "Command not found in container."
            echo "Container image may be corrupted or incompatible."
            echo ""
            echo "Try rebuilding: $tool_name --rebuild"
        else
            echo "Claude setup-token command failed (exit code: $docker_exit_code)"
            echo ""
            echo "This usually indicates:"
            echo "  • Authentication flow was cancelled or failed"
            echo "  • No active Claude Pro/Max subscription"
            echo "  • Network connectivity issues"
            echo "  • Browser authentication not completed"
            echo ""
            echo "Please try again and ensure you complete the full OAuth flow."
        fi

        echo ""
        rm -f "$tmp_output"
        return 1
    fi

    # Cleanup
    rm -f "$tmp_output"

    # BSH-05: reached only after a token was saved and validated above. Publish
    # the path so select_token / callers can launch with it instead of treating
    # a successful creation as "Cancelled".
    CREATED_TOKEN_FILE="$token_file"

    return 0
}

# Function to select a token interactively
# Args: $1 = token_dir
#       $2 = mode: "container" (default, ccy) or "host" (cc)
# Sets: SELECTED_TOKEN global variable
#       - container mode: path to chosen token file (or "" if create/renew picked)
#       - host mode: path to chosen token file, OR "" if Desktop fallback picked
# Returns: 0 on selection (or container-mode create/renew),
#          1 in container mode if no valid tokens (no Desktop fallback there),
#          0 in host mode if pool is empty (Desktop is automatic fallback)
#
# container mode menu: numbered valid tokens, r1..rN renew options, 0 create new
# host mode menu: numbered valid tokens, d for Desktop (host ~/.claude/ OAuth);
#                 no renew, no create — those require a container so cc cannot
#                 perform them. Empty pool short-circuits to Desktop with a
#                 one-shot instruction banner.
select_token() {
    local token_dir="$1"
    local mode="${2:-container}"

    if [ "$mode" != "container" ] && [ "$mode" != "host" ]; then
        echo "select_token: invalid mode '$mode' (expected 'container' or 'host')" >&2
        return 1
    fi

    if [ ! -d "$token_dir" ]; then
        if [ "$mode" = "host" ]; then
            _select_token_host_empty_pool_banner "$token_dir"
            SELECTED_TOKEN=""
            return 0
        fi
        return 1
    fi

    # Get list of valid (non-expired) token files
    local valid_tokens=()
    local expired_tokens=()
    local today
    today=$(date +%Y-%m-%d)

    for token_file in "$token_dir"/*.token; do
        if [ -f "$token_file" ]; then
            if is_token_valid "$token_file"; then
                valid_tokens+=("$token_file")
            else
                expired_tokens+=("$token_file")
            fi
        fi
    done

    # Build expired token info for display and renew options
    local expired_names=()
    local expired_dates=()
    if [ ${#expired_tokens[@]} -gt 0 ]; then
        echo ""
        echo "⚠  Found ${#expired_tokens[@]} expired token(s):"
        for token_file in "${expired_tokens[@]}"; do
            local filename
            filename=$(basename "$token_file")
            local token_name="${filename%.*.token}"
            local expiry_date=""
            if [[ "$filename" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$ ]]; then
                expiry_date="${BASH_REMATCH[1]}"
            fi
            expired_names+=("$token_name")
            expired_dates+=("$expiry_date")
            if [ "$mode" = "host" ]; then
                echo "    $token_name (expired: ${expiry_date:-unknown}) — renew with: ccy --create-token"
            else
                echo "    $token_name (expired: ${expiry_date:-unknown})"
            fi
        done
        echo ""
    fi

    # Host mode with empty pool: short-circuit to Desktop with instruction banner.
    if [ "$mode" = "host" ] && [ ${#valid_tokens[@]} -eq 0 ]; then
        _select_token_host_empty_pool_banner "$token_dir"
        SELECTED_TOKEN=""
        return 0
    fi

    if [ ${#valid_tokens[@]} -eq 0 ]; then
        return 1
    fi

    # Usage is off until the user asks for it — see the Plan 00074 note above
    # usage_prime_cache(). The menu is redrawn once it has been fetched.
    local usage_fetched=0

    # Redraw loop: every path out of the inner prompt either returns or asks for
    # a redraw, so the menu is reprinted only when its content actually changed.
    while true; do

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    if [ "$mode" = "host" ]; then
        echo "Claude Code Token Selection for cc"
    else
        echo "Claude Code Token Selection for YOLO Mode"
    fi
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "Available tokens:"
    echo ""

    # Column width from the longest name, so the expiry dates line up instead of
    # ragged-right after names of different lengths.
    local name_width=0 _f _n
    for _f in "${valid_tokens[@]}"; do
        _n="$(basename "$_f")"
        _n="${_n%.*.token}"
        [ "${#_n}" -gt "$name_width" ] && name_width="${#_n}"
    done

    for i in "${!valid_tokens[@]}"; do
        local token_file="${valid_tokens[$i]}"
        local filename
        filename=$(basename "$token_file")
        local token_name="${filename%.*.token}"

        # Blank line BETWEEN blocks, not after the last one — a trailing blank
        # plus the options block's own leading blank reads as a gap, not a break.
        if [ "$usage_fetched" -eq 1 ] && [ "$i" -gt 0 ]; then
            echo ""
        fi

        # Extract expiry
        if [[ "$filename" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$ ]]; then
            local expiry_date="${BASH_REMATCH[1]}"
            printf '  %s) %-*s   expires %b\n' \
                "$((i+1))" "$name_width" "$token_name" "$(colorize_expiry "$expiry_date")"
        else
            printf '  %s) %s\n' "$((i+1))" "$token_name"
        fi

        if [ "$usage_fetched" -eq 1 ]; then
            usage_render_block "$token_dir" "$token_name"
        fi
    done

    if [ "$mode" = "container" ]; then
        # Show renew options for expired tokens (container only — needs a container)
        if [ ${#expired_names[@]} -gt 0 ]; then
            echo ""
            for i in "${!expired_names[@]}"; do
                echo "  r$((i+1))) Renew: ${expired_names[$i]} (expired: ${expired_dates[$i]:-unknown})"
            done
        fi
        echo ""
        echo "  0) Create new token"
    else
        echo ""
        echo "  d) Desktop (use host ~/.claude/ OAuth — current cc default)"
    fi

    # The cost is stated in the option itself, not buried in docs: pressing this
    # spends quota, and the user is entitled to know that before pressing it.
    local usage_hint=""
    if usage_enabled && [ "$usage_fetched" -eq 0 ]; then
        echo ""
        echo "  u) Show usage limits (costs 1 small API call per account)"
        usage_hint=", u"
    fi
    echo ""

    # Build prompt hint
    local prompt_hint
    if [ "$mode" = "host" ]; then
        prompt_hint="1-${#valid_tokens[@]}, d${usage_hint}"
    else
        local renew_hint=""
        if [ ${#expired_names[@]} -gt 0 ]; then
            if [ ${#expired_names[@]} -eq 1 ]; then
                renew_hint=", r1"
            else
                renew_hint=", r1-r${#expired_names[@]}"
            fi
        fi
        prompt_hint="0-${#valid_tokens[@]}${renew_hint}${usage_hint}"
    fi

    local redraw=0
    while true; do
        read -r -p "Select token [${prompt_hint}]: " selection
        echo ""

        if [ -z "$selection" ]; then
            echo "Invalid selection: (empty)"
            echo "Please enter one of: ${prompt_hint}"
            echo ""
            continue
        fi

        # Fetch usage on demand, then redraw the menu with a usage column.
        if [ "$selection" = "u" ] && usage_enabled && [ "$usage_fetched" -eq 0 ]; then
            echo "Fetching usage for ${#valid_tokens[@]} account(s)..."
            usage_prime_cache "$token_dir" "${valid_tokens[@]}"
            usage_fetched=1
            redraw=1
            break
        fi

        # Host mode: Desktop fallback
        if [ "$mode" = "host" ] && [ "$selection" = "d" ]; then
            SELECTED_TOKEN=""
            echo "✓ Using Desktop (host ~/.claude/ OAuth)"
            echo ""
            echo "════════════════════════════════════════════════════════════════════════════════"
            echo ""
            return 0
        fi

        # Container mode: renew selections (r1, r2, etc.)
        if [ "$mode" = "container" ] && [[ "$selection" =~ ^r([0-9]+)$ ]]; then
            local renew_idx="${BASH_REMATCH[1]}"
            if [ "$renew_idx" -ge 1 ] && [ "$renew_idx" -le ${#expired_names[@]} ] 2>/dev/null; then
                local renew_name="${expired_names[$((renew_idx-1))]}"
                echo "Renewing expired token: $renew_name"
                echo ""
                # shellcheck disable=SC2153
                create_token "$token_dir" "$GH_TOKEN" "$IMAGE_NAME" "ccy" "$renew_name"
                # BSH-05: hand the freshly created token back to the caller so the
                # launch continues seamlessly. Empty when the user cancelled the
                # renew — the caller detects that and exits with guidance.
                SELECTED_TOKEN="$CREATED_TOKEN_FILE"
                # shellcheck disable=SC2317
                return 0
            else
                echo "Invalid renew selection: $selection"
                echo ""
                continue
            fi
        fi

        # Container mode: create new token
        if [ "$mode" = "container" ] && [ "$selection" = "0" ]; then
            # shellcheck disable=SC2153
            create_token "$token_dir" "$GH_TOKEN" "$IMAGE_NAME" "ccy"
            # BSH-05: see the renew branch above — propagate the new token path
            # (or empty on cancel) so the caller can launch or exit cleanly.
            SELECTED_TOKEN="$CREATED_TOKEN_FILE"
            # shellcheck disable=SC2317
            return 0
        fi

        # Numeric selection: pick a valid token
        if [ "$selection" -ge 1 ] && [ "$selection" -le ${#valid_tokens[@]} ] 2>/dev/null; then
            SELECTED_TOKEN="${valid_tokens[$((selection-1))]}"
            local filename
            filename=$(basename "$SELECTED_TOKEN")
            local token_name="${filename%.*.token}"

            echo "✓ Selected token: $token_name"
            echo ""
            echo "════════════════════════════════════════════════════════════════════════════════"
            echo ""
            return 0
        else
            echo "Invalid selection: $selection"
            echo "Please enter one of: ${prompt_hint}"
            echo ""
        fi
    done

    # Only a usage fetch reaches here — every other outcome returned above.
    if [ "$redraw" -ne 1 ]; then
        return 1
    fi
    done
}

# Empty-pool banner for host mode — prints once before Desktop fallback.
# Args: $1 = token_dir (for display, may not exist yet)
_select_token_host_empty_pool_banner() {
    local token_dir="$1"
    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "No tokens available — using Desktop (host ~/.claude/ OAuth)"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "  Token pool: $token_dir (empty or missing)"
    echo ""
    echo "  cc shares ccy's token pool. To enable the token chooser:"
    echo "    ccy --create-token       # create one or more named tokens"
    echo ""
    echo "  After that, cc will offer them alongside the Desktop option."
    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
}

# Function to export a token as a self-contained import script
# Args: $1 = token_dir, $2 = token_name
# Outputs: bash script snippet to stdout
export_token() {
    local token_dir="$1"
    local token_name="$2"

    # Find matching token file
    local matching_tokens=("$token_dir/${token_name}".*.token)
    if [ ! -f "${matching_tokens[0]}" ] || [ "${matching_tokens[0]}" = "$token_dir/${token_name}.*.token" ]; then
        print_error "No token found with name: $token_name"
        echo ""
        echo "Available tokens:"
        list_tokens "$token_dir" "ccy"
        return 1
    fi

    # Use the most recent token if multiple exist
    local token_file="${matching_tokens[-1]}"
    local filename
    filename=$(basename "$token_file")

    # Extract expiry date
    local expiry_date=""
    if [[ "$filename" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$ ]]; then
        expiry_date="${BASH_REMATCH[1]}"
    else
        print_error "Token file has invalid format: $filename"
        return 1
    fi

    # Check if token is expired
    if ! is_token_valid "$token_file"; then
        print_error "Token '$token_name' is expired (${expiry_date})"
        echo "Create a new token first: ccy --create-token"
        return 1
    fi

    # Read token content
    local token_content
    token_content=$(cat "$token_file")

    if [ -z "$token_content" ]; then
        print_error "Token file is empty: $token_file"
        return 1
    fi

    # Validate token doesn't contain characters that would break quoting
    if [[ "$token_content" == *"'"* ]]; then
        print_error "Token contains unexpected characters"
        return 1
    fi

    # Output a single pasteable command using heredoc
    echo "# CCY Token: $token_name | Expires: $expiry_date | Generated: $(date '+%Y-%m-%d %H:%M:%S')"
    echo "# Paste into terminal on target machine, or save in LastPass/1Password."
    cat <<OUTER
bash <<'CCYTOKEN'
set -eo pipefail
mkdir -p "\$HOME/.claude-tokens/ccy/tokens"
chmod 700 "\$HOME/.claude-tokens/ccy" "\$HOME/.claude-tokens/ccy/tokens"
printf '%s' '${token_content}' > "\$HOME/.claude-tokens/ccy/tokens/${token_name}.${expiry_date}.token"
chmod 600 "\$HOME/.claude-tokens/ccy/tokens/${token_name}.${expiry_date}.token"
echo "Token '${token_name}' imported (expires ${expiry_date}). Use with: ccy --token ${token_name}"
CCYTOKEN
OUTER
}

# Interactive multi-token export
# Args: $1 = token_dir
# Outputs: combined bash import script to stdout
export_tokens_interactive() {
    local token_dir="$1"

    if [ ! -d "$token_dir" ]; then
        print_error "Token directory not found: $token_dir"
        return 1
    fi

    # Collect valid tokens
    local valid_tokens=()
    local valid_names=()
    for token_file in "$token_dir"/*.token; do
        if [ -f "$token_file" ] && is_token_valid "$token_file"; then
            valid_tokens+=("$token_file")
            local filename
            filename=$(basename "$token_file")
            valid_names+=("${filename%.*.token}")
        fi
    done

    if [ ${#valid_tokens[@]} -eq 0 ]; then
        print_error "No valid (non-expired) tokens found"
        echo ""
        echo "Create a token first: ccy --create-token"
        return 1
    fi

    echo "" >&2
    echo "════════════════════════════════════════════════════════════════════════════════" >&2
    echo "Export Tokens" >&2
    echo "════════════════════════════════════════════════════════════════════════════════" >&2
    echo "" >&2

    for i in "${!valid_tokens[@]}"; do
        local filename
        filename=$(basename "${valid_tokens[$i]}")
        local expiry_date=""
        if [[ "$filename" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$ ]]; then
            expiry_date="${BASH_REMATCH[1]}"
            echo "  $((i+1))) ${valid_names[$i]} (expires: $(colorize_expiry "$expiry_date"))" >&2
        else
            echo "  $((i+1))) ${valid_names[$i]}" >&2
        fi
    done

    echo "" >&2
    echo "  a) Export all" >&2
    echo "" >&2

    local selections
    while true; do
        read -r -p "Select tokens to export [1-${#valid_tokens[@]}, space-separated, or a]: " selections
        echo "" >&2

        if [ -z "$selections" ]; then
            echo "No selection made." >&2
            echo "" >&2
            continue
        fi

        if [ "$selections" = "a" ]; then
            # Export all
            for name in "${valid_names[@]}"; do
                export_token "$token_dir" "$name"
                echo ""
            done
            echo "Exported ${#valid_names[@]} token(s)." >&2
            return 0
        fi

        # Validate all selections first
        local selected_names=()
        local all_valid=true
        for sel in $selections; do
            if [ "$sel" -ge 1 ] && [ "$sel" -le ${#valid_tokens[@]} ] 2>/dev/null; then
                selected_names+=("${valid_names[$((sel-1))]}")
            else
                echo "Invalid selection: $sel (enter 1-${#valid_tokens[@]} or a)" >&2
                echo "" >&2
                all_valid=false
                break
            fi
        done

        if [ "$all_valid" = false ]; then
            continue
        fi

        # Export selected tokens
        for name in "${selected_names[@]}"; do
            export_token "$token_dir" "$name"
            echo ""
        done
        echo "Exported ${#selected_names[@]} token(s)." >&2
        return 0
    done
}

# Export functions
export -f list_tokens
export -f create_token
export -f select_token
export -f export_token
export -f export_tokens_interactive
