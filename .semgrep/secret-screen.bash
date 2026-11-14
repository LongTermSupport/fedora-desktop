# Fixture for .semgrep/secret-screen.yml, shell (the gate's self-test). Each "ruleid" line marks
# the line below it as the start of a finding; each "ok" line marks the line below it as clean.
# Not a script: it holds the shapes the rule judges, so it has no shebang and no .bash name.

# The issue #261 instances, as they were.
create() {
    # ruleid: secret-on-normal-screen
    GH_TOKEN="$gh_token" container_cmd run -it --rm --entrypoint claude "$image_name" \
        setup-token 2>&1 | tee "$tmp_output"
    docker_exit_code="${PIPESTATUS[0]}"
    # ruleid: secret-read-echoed
    read -r -p "Token: " manual_token || [ -n "$manual_token" ] || return 1
    # ok: secret-read-echoed
    read -r -p "Enter a name for this token (e.g., 'personal'): " token_name
    # ok: secret-read-echoed
    read -r -s -p "Token: " manual_token
    echo "" >&2
    # ok: secret-on-normal-screen
    echo "$token" > "$token_file"
}

export_token() {
    # ruleid: secret-on-normal-screen
    cat <<OUTER
bash <<'CCYTOKEN'
name='${token_name}'
token='${token_content}'
OUTER
    echo done
}

# vault.bash get, get-raw and dump.
cmd_get() {
    # ruleid: secret-on-normal-screen
    _decrypt_var "$varname"
    echo
}
cmd_get_raw() {
    # ruleid: secret-on-normal-screen
    _decrypt_var "$varname" | cat -A
    echo "=== Length: $(_decrypt_var "$varname" | wc -c) bytes ==="
}
cmd_dump() {
    while IFS= read -r varname; do
        value=$(_decrypt_var "$varname")
        # ruleid: secret-on-normal-screen
        printf '%-30s %s\n' "$varname" "$value"
    done <<< "$vault_vars"
}

# The bashrc functions play-github-cli-multi.yml writes.
function gh-token-work() {
    # ruleid: secret-on-normal-screen
    gh auth token --hostname github.com --user "someone"
}
report() {
    echo "Your PhpStorm Token:"
    # ruleid: secret-on-normal-screen
    gh auth token
    echo ""
}

# Other shapes of the class.
# ruleid: secret-on-normal-screen
echo "$GH_TOKEN"
# ruleid: secret-on-normal-screen
printf 'become: %s\n' "$secret" >&2
# ruleid: secret-on-normal-screen
printf '%s\n' "${HUMAN_PASSWORD}" | cat
# ruleid: secret-read-echoed
read -rp "Passphrase: " passphrase < /dev/tty

# The fixes, and what is not the class.
# ok: secret-on-normal-screen
printf '%s\n' "$token" | scripts/secret-screen.bash
# ok: secret-on-normal-screen
_decrypt_var "$varname" | scripts/secret-screen.bash
# ok: secret-on-normal-screen
gh auth token --hostname github.com | scripts/secret-screen.bash
# ok: secret-on-normal-screen
setup-token 2>&1 | tee "$tmp_output" | "$SCRIPT_DIR/claude-yolo" --secret-screen
# ok: secret-on-normal-screen
printf '%s' "$passphrase" | cryptsetup open --test-passphrase "$backing"
# ok: secret-on-normal-screen
printf 'header = "Authorization: Bearer %s"\n' "${HUMAN_TOKEN}" \
    | curl --config - "$url"
# ok: secret-on-normal-screen
token=$(printf '%s' "$raw_token" | tr -d '\n')
# ok: secret-on-normal-screen
echo "File: $SELECTED_TOKEN_FILE"
# ok: secret-on-normal-screen
echo "Length: $TOKEN_BYTES bytes"
# ok: secret-on-normal-screen
printf '%s\n' "$tokens"
# ok: secret-on-normal-screen
GH_LOGIN_TOKEN="$(gh auth token --hostname github.com)"
# ok: secret-on-normal-screen
if said=$(gh auth token --hostname github.com 2>&1 >/dev/null); then echo logged-in; else printf 'no %s\n' "$said"; fi
# ok: secret-on-normal-screen
echo \$CLAUDE_CODE_OAUTH_TOKEN
gh_token_pipe() {
    # ok: secret-on-normal-screen
    printf '%s\n' "${GH_LOGIN_TOKEN}"
}
# ok: secret-read-echoed
{ IFS= read -r GH_TOKEN || [ -n "${GH_TOKEN:+set}" ]; } <"$file"
# ok: secret-read-echoed
while IFS=$'\t' read -r field token; do :; done < "$list"
# ok: secret-read-echoed
read -rs -p "Password: " password
# ok: secret-on-normal-screen
cat <<'EOF'
token='${token}'
EOF
