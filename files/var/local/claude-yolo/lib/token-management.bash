#!/bin/bash
# Token Management Library
# Shared token operations for claude-yolo and claude-browser
#
# Version: 1.4.0 - Add byte length check and validate pasted tokens with retry

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

    local today=$(date +%Y-%m-%d)

    for token_file in "$token_dir"/*.token; do
        if [ -f "$token_file" ]; then
            local filename=$(basename "$token_file")
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
                echo "    Expires: $expiry_date ($status)"
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
        exit 1
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
            read -p "Enter a name for this token (e.g., 'personal', 'work', 'default'): " token_name

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
    local expiry_date=$(date -d "+90 days" +%Y-%m-%d)
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
        read -p "Overwrite? (y/N): " overwrite
        if [ "$overwrite" != "y" ] && [ "$overwrite" != "Y" ]; then
            echo "Cancelled. Token not created."
            exit 0
        fi
    fi

    echo ""
    echo "Creating token: $token_name"
    echo "Expiry date: $expiry_date"
    echo "Storage: $token_file"
    echo ""

    # Create temporary output file for token
    tmp_output="/tmp/${tool_name}-token-setup-$$"

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
    read
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
                exit 1
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
                read -p "Token: " manual_token

                # Basic validation: format check
                if [ -z "$manual_token" ]; then
                    echo ""
                    print_error "Token cannot be empty"
                    echo ""
                    read -p "Try again? (Y/n): " retry
                    if [ "$retry" = "n" ] || [ "$retry" = "N" ]; then
                        echo "Cancelled."
                        rm -f "$tmp_output"
                        exit 1
                    fi
                    echo ""
                    continue
                fi

                if ! [[ "$manual_token" =~ ^sk-ant-oat01- ]]; then
                    echo ""
                    print_error "Invalid token format"
                    echo "Token must start with 'sk-ant-oat01-'"
                    echo ""
                    read -p "Try again? (Y/n): " retry
                    if [ "$retry" = "n" ] || [ "$retry" = "N" ]; then
                        echo "Cancelled."
                        rm -f "$tmp_output"
                        exit 1
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
                    read -p "Try again? (Y/n): " retry
                    if [ "$retry" = "n" ] || [ "$retry" = "N" ]; then
                        echo "Cancelled."
                        rm -f "$tmp_output"
                        exit 1
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
                    read -p "Try again? (Y/n): " retry
                    if [ "$retry" = "n" ] || [ "$retry" = "N" ]; then
                        echo ""
                        echo "Cancelled. Please verify the token and try again."
                        echo "Run: $tool_name --create-token"
                        rm -f "$tmp_output"
                        exit 1
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
        exit 1
    fi

    # Cleanup
    rm -f "$tmp_output"

    exit 0
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
    local today=$(date +%Y-%m-%d)

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
            local filename=$(basename "$token_file")
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
        local filename=$(basename "$token_file")
        local token_name="${filename%.*.token}"

        # Extract expiry
        if [[ "$filename" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$ ]]; then
            local expiry_date="${BASH_REMATCH[1]}"
            echo "  $((i+1))) $token_name (expires: $expiry_date)"
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
        read -p "Select token [0-${#valid_tokens[@]}${renew_hint}]: " selection
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
                create_token "$token_dir" "$GH_TOKEN" "$IMAGE_NAME" "ccy" "$renew_name"
                exit 0
            else
                echo "Invalid renew selection: $selection"
                echo ""
                continue
            fi
        fi

        if [ "$selection" = "0" ]; then
            create_token "$token_dir" "$GH_TOKEN" "$IMAGE_NAME" "ccy"
            exit 0
        elif [ "$selection" -ge 1 ] && [ "$selection" -le ${#valid_tokens[@]} ] 2>/dev/null; then
            SELECTED_TOKEN="${valid_tokens[$((selection-1))]}"
            local filename=$(basename "$SELECTED_TOKEN")
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

# Export functions
export -f list_tokens
export -f create_token
export -f select_token
