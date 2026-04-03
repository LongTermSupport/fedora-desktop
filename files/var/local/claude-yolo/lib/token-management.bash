#!/bin/bash
# Token Management Library
# Token operations for claude-yolo (ccy)
#
# Version: 1.5.0 - Color-code expiry dates (green >30d, orange ≤30d, red ≤5d)

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
    # Claude Code with a valid token should be able to show version
    if container_cmd run --rm \
        -e "CLAUDE_CODE_OAUTH_TOKEN=$token" \
        --entrypoint claude \
        "$image_name" \
        --version &>/dev/null; then
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

    if container_cmd run -it --rm \
        --entrypoint claude \
        -e "GH_TOKEN=$gh_token" \
        "$image_name" \
        setup-token 2>&1 | tee "$tmp_output"; then

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
                    echo "Length: $token_bytes bytes (expected: 100-110 bytes)"
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
        docker_exit_code=$?
        echo ""
        echo "════════════════════════════════════════════════════════════════════════════"
        print_error "Token Creation Failed"
        echo "════════════════════════════════════════════════════════════════════════════"
        echo ""

        if [ $docker_exit_code -eq 125 ]; then
            echo "Docker container failed to start."
            echo "The container image may be corrupted."
            echo ""
            echo "Try rebuilding: $tool_name --rebuild"
        elif [ $docker_exit_code -eq 126 ] || [ $docker_exit_code -eq 127 ]; then
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

    return 0
}

# Function to select a token interactively
# Args: $1 = token_dir
# Sets: SELECTED_TOKEN global variable
# Returns: 0 if token selected, 1 if no valid tokens
select_token() {
    local token_dir="$1"

    if [ ! -d "$token_dir" ]; then
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
            echo "    $token_name (expired: ${expiry_date:-unknown})"
        done
        echo ""
    fi

    if [ ${#valid_tokens[@]} -eq 0 ]; then
        return 1
    fi

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "Claude Code Token Selection for YOLO Mode"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "Available tokens:"
    echo ""

    for i in "${!valid_tokens[@]}"; do
        local token_file="${valid_tokens[$i]}"
        local filename
        filename=$(basename "$token_file")
        local token_name="${filename%.*.token}"

        # Extract expiry
        if [[ "$filename" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$ ]]; then
            local expiry_date="${BASH_REMATCH[1]}"
            echo "  $((i+1))) $token_name (expires: $(colorize_expiry "$expiry_date"))"
        else
            echo "  $((i+1))) $token_name"
        fi
    done

    # Show renew options for expired tokens
    if [ ${#expired_names[@]} -gt 0 ]; then
        echo ""
        for i in "${!expired_names[@]}"; do
            echo "  r$((i+1))) Renew: ${expired_names[$i]} (expired: ${expired_dates[$i]:-unknown})"
        done
    fi

    echo ""
    echo "  0) Create new token"
    echo ""

    # Build prompt hint
    local renew_hint=""
    if [ ${#expired_names[@]} -gt 0 ]; then
        if [ ${#expired_names[@]} -eq 1 ]; then
            renew_hint=", r1"
        else
            renew_hint=", r1-r${#expired_names[@]}"
        fi
    fi

    while true; do
        read -r -p "Select token [0-${#valid_tokens[@]}${renew_hint}]: " selection
        echo ""

        if [ -z "$selection" ]; then
            echo "Invalid selection: (empty)"
            echo "Please enter a number between 0 and ${#valid_tokens[@]}"
            echo ""
            continue
        fi

        # Handle renew selections (r1, r2, etc.)
        if [[ "$selection" =~ ^r([0-9]+)$ ]]; then
            local renew_idx="${BASH_REMATCH[1]}"
            if [ "$renew_idx" -ge 1 ] && [ "$renew_idx" -le ${#expired_names[@]} ] 2>/dev/null; then
                local renew_name="${expired_names[$((renew_idx-1))]}"
                echo "Renewing expired token: $renew_name"
                echo ""
                # shellcheck disable=SC2153
                create_token "$token_dir" "$GH_TOKEN" "$IMAGE_NAME" "ccy" "$renew_name"
                # shellcheck disable=SC2317
                return 0
            else
                echo "Invalid renew selection: $selection"
                echo ""
                continue
            fi
        fi

        if [ "$selection" = "0" ]; then
            # shellcheck disable=SC2153
            create_token "$token_dir" "$GH_TOKEN" "$IMAGE_NAME" "ccy"
            # shellcheck disable=SC2317
            return 0
        elif [ "$selection" -ge 1 ] && [ "$selection" -le ${#valid_tokens[@]} ] 2>/dev/null; then
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
            echo "Please enter a number between 0 and ${#valid_tokens[@]}"
            echo ""
        fi
    done
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

    echo "#!/bin/bash"
    echo "# CCY Token Import — exported from: ccy --export-token $token_name"
    echo "# Token: $token_name | Expires: $expiry_date"
    echo "# Generated: $(date '+%Y-%m-%d %H:%M:%S')"
    echo "#"
    echo "# Paste this entire block into a terminal on the target machine."
    echo "# Or save to LastPass/1Password as a secure note."
    echo ""
    echo "set -euo pipefail"
    echo ""
    echo "TOKEN_DIR=\"\$HOME/.claude-tokens/ccy/tokens\""
    echo "TOKEN_FILE=\"\$TOKEN_DIR/${token_name}.${expiry_date}.token\""
    echo ""
    echo "mkdir -p \"\$HOME/.claude-tokens/ccy/tokens\""
    echo "chmod 700 \"\$HOME/.claude-tokens/ccy\""
    echo "chmod 700 \"\$HOME/.claude-tokens/ccy/tokens\""
    echo ""
    echo "printf '%s' '${token_content}' > \"\$TOKEN_FILE\""
    echo "chmod 600 \"\$TOKEN_FILE\""
    echo ""
    echo "echo \"Token '${token_name}' imported (expires ${expiry_date}).\""
    echo "echo \"Use with: ccy --token ${token_name}\""
}

# Export functions
export -f list_tokens
export -f create_token
export -f select_token
export -f export_token
