#!/bin/bash
# Token Management Library
# Token operations for claude-yolo (ccy)
#
# Version: 1.7.0 - Plan 00073: per-token usage limits (5-hour / weekly) shown in
#                  the selection menu and --list-tokens, fetched in parallel from
#                  GET /api/oauth/usage with a short TTL cache. Set
#                  CCY_TOKEN_USAGE=0 to disable.
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

# ==============================================================================
# Per-token usage limits (Plan 00073)
# ==============================================================================
#
# Claude Code reports a subscription's 5-hour and weekly utilisation via
# GET /api/oauth/usage. Nothing supported exposes it before a session starts —
# there is no `claude usage` subcommand, and the statusline `rate_limits` block
# is only populated after a session's first API response — so the token menu
# reads that endpoint directly.
#
# It is an INTERNAL, unversioned endpoint. Everything here is therefore written
# to degrade VISIBLY (a "usage: …" note saying what went wrong) and never to
# block a launch: the figure is decoration on a menu, like the expiry colour,
# not an operation whose failure makes the launch wrong. A malformed token, a
# missing jq, or an unwritable cache still say so out loud.
#
# Measured cost (5 tokens, parallel): ~200 ms cold, ~0 warm. Sequential would be
# ~815 ms, which is why the fetch fans out.
#
# Set CCY_TOKEN_USAGE=0 to switch the whole feature off.

CCY_USAGE_ENDPOINT="${CCY_USAGE_ENDPOINT:-https://api.anthropic.com/api/oauth/usage}"
CCY_USAGE_TIMEOUT="${CCY_USAGE_TIMEOUT:-4}"
# Split from the total budget on purpose: the common broken-network case is a
# connection that never establishes, and waiting the full 4 s for that stalls
# every launch on a flaky link. A slow-but-working response still gets the whole
# CCY_USAGE_TIMEOUT.
CCY_USAGE_CONNECT_TIMEOUT="${CCY_USAGE_CONNECT_TIMEOUT:-2}"
CCY_USAGE_TTL="${CCY_USAGE_TTL:-180}"

# jq program that turns a usage response into one compact display line.
#
# Deliberately envelope-agnostic: it walks the whole document for any object
# carrying both `kind` and `percent`, so it works whether the payload is
# {"rate_limits":[…]} or a bare array. The exact envelope is not documented
# anywhere and may change; the field names are the stable part (Claude Code's
# own mapper reads kind/percent/resets_at/scope.model.display_name).
# SC2016: $now and $e are jq variables, not shell ones — single quotes are
# required here, not an oversight.
# shellcheck disable=SC2016
_CCY_USAGE_JQ='
def lbl:
  if   . == "five_hour" then "5h"
  elif . == "seven_day" then "wk"
  elif startswith("seven_day_") then "wk-" + .[10:]
  else . end;
def secs:
  if   type == "number" then . - $now
  elif type == "string" then ((try fromdateiso8601 catch null) // null) as $e
       | if $e == null then null else $e - $now end
  else null end;
def human:
  if   . == null or . <= 0 then ""
  elif . < 3600   then " r" + ((./60)|floor|tostring) + "m"
  elif . < 172800 then " r" + ((./3600)|floor|tostring) + "h"
  else                 " r" + ((./86400)|floor|tostring) + "d" end;
[.. | objects | select(has("kind") and has("percent"))]
| map("\(.kind|lbl) \(.percent|round)%\(.resets_at? | secs | human)")
| join(" · ")
'

# Cache location: a sibling of the token dir, so it inherits the same private
# ~/.claude-tokens/ccy/ home for both ccy and cc.
_usage_cache_dir() {
    printf '%s/usage-cache' "$(dirname "$1")"
}

usage_enabled() {
    [ "${CCY_TOKEN_USAGE:-1}" != "0" ]
}

# Fetches ONE token's usage and renders it, writing two cache files:
#   $2.status   the HTTP status (or 000 when no response was received at all)
#   $2.summary  the finished display line (empty unless the status was 200)
#
# Rendering happens HERE, inside the parallel worker, rather than at display
# time. jq costs ~40 ms per invocation — for 5 tokens that is more than the
# whole network round trip, and doing it per-row on every menu render made the
# cached path almost as slow as the uncached one (measured: 239 ms warm vs
# 268 ms cold). Parsing once, in parallel, makes a cache hit free.
#
# Only the rendered line is persisted — the raw response is never written to
# disk, so account details in the payload do not outlive the fetch.
#
# The token is handed to curl through --config on STDIN so it never appears in
# argv — the same rule validate_token() follows (BSH-09), because argv is
# world-readable via /proc/<pid>/cmdline.
#
# Every failure is RECORDED (as a status) rather than raised, and both files are
# always created, so a fan-out worker can never abort a caller running `set -e`
# and can never leave a half-written cache entry behind. Always returns 0.
_usage_fetch_one() {
    local token="$1" out_prefix="$2"
    local body code="000" line=""

    body="$(mktemp)"
    printf '' > "$out_prefix.summary"

    if code="$(printf 'header = "Authorization: Bearer %s"\n' "$token" \
        | curl --config - \
               --silent \
               --request GET \
               --header 'Content-Type: application/json' \
               --connect-timeout "$CCY_USAGE_CONNECT_TIMEOUT" \
               --max-time "$CCY_USAGE_TIMEOUT" \
               --output "$body" \
               --write-out '%{http_code}' \
               "$CCY_USAGE_ENDPOINT")"; then
        :
    else
        # curl itself failed (DNS, TLS, connection refused, timeout). 000 is
        # curl's own convention for "no HTTP response was received", and it is
        # what --write-out reports in that case too.
        code="000"
    fi

    if [ "$code" = "200" ] && command -v jq > /dev/null; then
        if line="$(jq -r --argjson now "$(date +%s)" "$_CCY_USAGE_JQ" "$body")"; then
            printf '%s' "$line" > "$out_prefix.summary"
        else
            code="unparseable"
        fi
    fi

    printf '%s\n' "$code" > "$out_prefix.status"
    rm -f "$body"
    chmod 600 "$out_prefix.summary" "$out_prefix.status"
    return 0
}

# Refreshes the cache for every given token file, fanning the fetches out in
# parallel so N tokens cost roughly one round trip rather than N.
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

        # Fresh enough? Skip the network entirely — this is the whole point of
        # the cache, and it must cost nothing but a stat.
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
            # outcome in the cache, and a worker that died would otherwise take
            # the caller's `set -e` — and the whole menu — down with it. That is
            # not hypothetical: an unreachable endpoint used to abort the render
            # and print no token list at all.
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
                # A worker died without publishing. Its cache entry is simply
                # left as it was, so the row reports "not fetched" (or a stale
                # value) rather than the menu failing. Nothing to clean up: the
                # part-files are inside the private cache dir.
                continue
            fi
        done
    fi
    return 0
}

# Echoes a one-line usage summary for a token name, or a "usage: …" note saying
# why there isn't one. Never empty, never silent about a failure.
#
# Stdout is the payload (this is a value a caller captures); nothing else is
# written there.
# Pure cache read — no jq, no curl, no subprocess beyond the file reads. This is
# what keeps a warm menu render indistinguishable from the old one.
usage_summary_for() {
    local token_dir="$1" token_name="$2"
    local cache_dir status code line

    usage_enabled || { printf ''; return 0; }

    # A missing tool here is an IaC gap, not a runtime condition to shrug at —
    # both are declared in play-claude-code.yml, the same play that deploys this
    # library. Name the remedy rather than rendering a bare blank.
    if ! command -v curl > /dev/null; then
        printf 'usage: needs curl (run play-claude-code.yml)'
        return 0
    fi
    if ! command -v jq > /dev/null; then
        printf 'usage: needs jq (run play-claude-code.yml)'
        return 0
    fi

    cache_dir="$(_usage_cache_dir "$token_dir")"
    status="$cache_dir/$token_name.status"

    if [ ! -f "$status" ]; then
        printf 'usage: not fetched'
        return 0
    fi

    code="$(cat "$status")"
    case "$code" in
        200) ;;
        000) printf 'usage: unreachable'; return 0 ;;
        401|403)
            # The interesting failure: the stored setup-token is not accepted
            # on this endpoint. Say so plainly rather than showing a blank —
            # this is the answer to the question Plan 00073 is gated on.
            printf 'usage: not authorised'
            return 0
            ;;
        429)          printf 'usage: rate limited'; return 0 ;;
        unparseable)  printf 'usage: unparseable';  return 0 ;;
        *)            printf 'usage: HTTP %s' "$code"; return 0 ;;
    esac

    line=""
    if [ -f "$cache_dir/$token_name.summary" ]; then
        line="$(cat "$cache_dir/$token_name.summary")"
    fi
    if [ -z "$line" ]; then
        printf 'usage: no limits reported'
        return 0
    fi

    printf '%s' "$line"
    return 0
}

# Wraps a usage summary in a colour keyed to the highest percentage in it, so a
# nearly-exhausted account is obvious at a glance. Mirrors colorize_expiry().
colorize_usage() {
    local summary="$1"
    local RED='\033[31m' ORANGE='\033[38;5;208m' GREEN='\033[32m'
    local DIM='\033[2m' RESET='\033[0m'

    case "$summary" in
        '') return 0 ;;
        usage:*) printf "${DIM}%s${RESET}" "$summary"; return 0 ;;
    esac

    # Pure bash, deliberately: the obvious `grep | tr | sort | head` costs four
    # processes per row, which measured at ~39 ms per token — on the CACHED
    # path, where there is otherwise no work to do at all. A here-string and a
    # loop spawn nothing.
    local -a parts=()
    local part worst=-1
    read -r -a parts <<< "$summary"
    for part in "${parts[@]}"; do
        case "$part" in
            *%)
                part="${part%\%}"
                if [[ "$part" =~ ^[0-9]+$ ]] && [ "$part" -gt "$worst" ]; then
                    worst="$part"
                fi
                ;;
        esac
    done

    if [ "$worst" -lt 0 ]; then
        printf '%s' "$summary"
    elif [ "$worst" -ge 85 ]; then
        printf "${RED}%s${RESET}" "$summary"
    elif [ "$worst" -ge 50 ]; then
        printf "${ORANGE}%s${RESET}" "$summary"
    else
        printf "${GREEN}%s${RESET}" "$summary"
    fi
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

    # Fan the usage fetches out before rendering anything, so N tokens cost one
    # round trip rather than N (measured: ~200 ms for 5, vs ~815 ms sequential).
    local all_tokens=()
    for token_file in "$token_dir"/*.token; do
        if [ -f "$token_file" ]; then
            all_tokens+=("$token_file")
        fi
    done
    if [ ${#all_tokens[@]} -gt 0 ]; then
        usage_prime_cache "$token_dir" "${all_tokens[@]}"
    fi

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

                local usage_line
                usage_line="$(usage_summary_for "$token_dir" "$token_name")"

                echo "  • $token_name"
                echo "    File: $token_file"
                echo "    Expires: $(colorize_expiry "$expiry_date") ($status)"
                if [ -n "$usage_line" ]; then
                    echo "    Usage:   $(colorize_usage "$usage_line")"
                fi
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

    # One parallel fan-out for the whole menu, before the first row is drawn.
    # Only the VALID tokens are fetched — an expired one cannot be launched, so
    # its usage figure would be latency spent on a row nobody can choose.
    usage_prime_cache "$token_dir" "${valid_tokens[@]}"

    for i in "${!valid_tokens[@]}"; do
        local token_file="${valid_tokens[$i]}"
        local filename
        filename=$(basename "$token_file")
        local token_name="${filename%.*.token}"
        local usage_line usage_suffix=""

        usage_line="$(usage_summary_for "$token_dir" "$token_name")"
        if [ -n "$usage_line" ]; then
            usage_suffix="  $(colorize_usage "$usage_line")"
        fi

        # Extract expiry
        if [[ "$filename" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$ ]]; then
            local expiry_date="${BASH_REMATCH[1]}"
            printf '  %s) %s (expires: %s)%s\n' \
                "$((i+1))" "$token_name" "$(colorize_expiry "$expiry_date")" "$usage_suffix"
        else
            printf '  %s) %s%s\n' "$((i+1))" "$token_name" "$usage_suffix"
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
    echo ""

    # Build prompt hint
    local prompt_hint
    if [ "$mode" = "host" ]; then
        prompt_hint="1-${#valid_tokens[@]}, d"
    else
        local renew_hint=""
        if [ ${#expired_names[@]} -gt 0 ]; then
            if [ ${#expired_names[@]} -eq 1 ]; then
                renew_hint=", r1"
            else
                renew_hint=", r1-r${#expired_names[@]}"
            fi
        fi
        prompt_hint="0-${#valid_tokens[@]}${renew_hint}"
    fi

    while true; do
        read -r -p "Select token [${prompt_hint}]: " selection
        echo ""

        if [ -z "$selection" ]; then
            echo "Invalid selection: (empty)"
            echo "Please enter one of: ${prompt_hint}"
            echo ""
            continue
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
