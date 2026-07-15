#!/usr/bin/env bash
# vault.bash - Ansible vault helper for variable-level encrypted strings
# Usage: ./vault.bash <command> [args]
set -euo pipefail

readonly VAULT_FILE="environment/localhost/host_vars/localhost.yml"
readonly VAULT_PASS_FILE="vault-pass.secret"
readonly VAULT_ID="localhost"

# Change to project root (where this script lives)
cd "$(dirname "$0")"

usage() {
  cat <<'EOF'
Ansible Vault String Helper

Usage: ./vault.bash <command> [args]

Commands:
  get <varname>           Decrypt and display a single vault variable
  get-raw <varname>       Show exact bytes (debug whitespace/quote issues)
  dump                    Decrypt and display ALL vault variables
  list                    List all vault-encrypted variable names
  encrypt <varname>       Encrypt a string and output the vault block
                          (reads value from stdin or prompts interactively)
  set <varname>           Encrypt a string and append it to localhost.yml
                          (reads value from stdin or prompts interactively)
                          Refuses if <varname> already exists — use 'replace'.
  replace <varname>       Update an EXISTING variable: drop its old vault block
                          and append the newly-encrypted value
                          (reads value from stdin or prompts interactively)

Examples:
  ./vault.bash get github_ssh_passphrase
  ./vault.bash dump
  ./vault.bash list
  echo 'secret123' | ./vault.bash encrypt my_variable
  ./vault.bash set new_secret_var
  ./vault.bash replace github_ssh_passphrase

Notes:
  - Vault password file: vault-pass.secret
  - Vault ID: localhost
  - Host vars: environment/localhost/host_vars/localhost.yml
EOF
}

check_vault_pass() {
  if [[ ! -f "$VAULT_PASS_FILE" ]]; then
    echo "ERROR: $VAULT_PASS_FILE not found" >&2
    echo "Create it first or run run.bash to set up the vault." >&2
    exit 1
  fi
}

# Decrypt a single vault variable using ansible CLI
# Uses python3 json (stdlib) to parse — immune to ansible output format settings
_decrypt_var() {
  local varname="$1"
  ANSIBLE_STDOUT_CALLBACK=ansible.builtin.minimal \
  ansible localhost -c local \
    --vault-id "${VAULT_ID}@${VAULT_PASS_FILE}" \
    -m debug -a "msg={{ ${varname} }}" \
    -i environment/localhost/hosts.yml 2>/dev/null \
    | python3 -c "
import sys, json, re
raw = sys.stdin.read()
m = re.search(r'=>\s*(\{.*\})', raw, re.DOTALL)
if m:
    print(json.loads(m.group(1))['msg'], end='')
else:
    print(raw, end='')
"
}

cmd_get() {
  local varname="${1:?Usage: vault.bash get <varname>}"
  check_vault_pass

  if ! grep -qP "^${varname}:.*!vault" "$VAULT_FILE" 2>/dev/null; then
    echo "ERROR: Variable '$varname' not found as vault-encrypted in $VAULT_FILE" >&2
    exit 1
  fi

  _decrypt_var "$varname"
  echo  # newline after value
}

# Show exact bytes of a decrypted vault variable (debug whitespace/quote issues)
cmd_get_raw() {
  local varname="${1:?Usage: vault.bash get-raw <varname>}"
  check_vault_pass

  if ! grep -qP "^${varname}:.*!vault" "$VAULT_FILE" 2>/dev/null; then
    echo "ERROR: Variable '$varname' not found as vault-encrypted in $VAULT_FILE" >&2
    exit 1
  fi

  echo "=== Decrypted value for '$varname' (cat -A shows exact bytes) ==="
  _decrypt_var "$varname" | cat -A
  echo
  echo "=== Length: $(_decrypt_var "$varname" | wc -c) bytes ==="
}

cmd_dump() {
  check_vault_pass

  local vault_vars
  vault_vars=$(cmd_list)

  if [[ -z "$vault_vars" ]]; then
    echo "No vault-encrypted variables found in $VAULT_FILE" >&2
    return
  fi

  local varname value
  while IFS= read -r varname; do
    value=$(_decrypt_var "$varname")
    if [[ "${#value}" -gt 80 ]]; then
      value="${value:0:77}..."
    fi
    printf '%-30s %s\n' "$varname" "$value"
  done <<< "$vault_vars"
}

cmd_list() {
  check_vault_pass
  grep -oP '^[a-zA-Z_]\w*(?=:.*!vault)' "$VAULT_FILE" | sort
}

# Read a secret value: interactively (prompt + confirm) when stdin is a TTY,
# otherwise straight from piped stdin. Echoes ONLY the value on stdout; every
# prompt and diagnostic goes to stderr so `value=$(_read_value foo)` stays pure.
_read_value() {
  local varname="$1" value confirm
  if [[ -t 0 ]]; then
    read -rsp "Enter value for '$varname': " value
    echo >&2
    read -rsp "Confirm value: " confirm
    echo >&2
    if [[ "$value" != "$confirm" ]]; then
      echo "ERROR: Values do not match" >&2
      return 1
    fi
  else
    # Piped input
    value=$(cat)
  fi
  printf '%s' "$value"
}

cmd_encrypt() {
  local varname="${1:?Usage: vault.bash encrypt <varname>}"
  check_vault_pass

  local value
  value=$(_read_value "$varname")

  # Don't pass --vault-id here: ansible.cfg already registers one at
  # vault-id "localhost". Passing it again makes ansible-core 2.20+ see
  # two identities both named "localhost" and refuse to encrypt without
  # --encrypt-vault-id disambiguation. Let ansible.cfg be the source.
  printf '%s' "$value" | ansible-vault encrypt_string \
    --stdin-name "$varname"
}

cmd_set() {
  local varname="${1:?Usage: vault.bash set <varname>}"
  check_vault_pass

  if grep -q "^${varname}:" "$VAULT_FILE"; then
    echo "ERROR: Variable '$varname' already exists in $VAULT_FILE" >&2
    echo "Use './vault.bash replace $varname' to update it in place." >&2
    exit 1
  fi

  local value
  value=$(_read_value "$varname")

  # See note in cmd_encrypt: ansible.cfg registers vault-id "localhost"
  # already; passing --vault-id here duplicates it and breaks on
  # ansible-core 2.20+.
  local encrypted
  encrypted=$(printf '%s' "$value" | ansible-vault encrypt_string \
    --stdin-name "$varname")

  echo "$encrypted" >> "$VAULT_FILE"
  echo "Saved '$varname' to $VAULT_FILE (vault-encrypted)" >&2
}

cmd_replace() {
  local varname="${1:?Usage: vault.bash replace <varname>}"
  check_vault_pass

  if ! grep -q "^${varname}:" "$VAULT_FILE"; then
    echo "ERROR: Variable '$varname' not found in $VAULT_FILE" >&2
    echo "Use './vault.bash set $varname' to add a new variable." >&2
    exit 1
  fi

  local value
  value=$(_read_value "$varname")

  # See note in cmd_encrypt: ansible.cfg registers vault-id "localhost"
  # already; passing --vault-id here duplicates it and breaks on
  # ansible-core 2.20+.
  local encrypted
  encrypted=$(printf '%s' "$value" | ansible-vault encrypt_string \
    --stdin-name "$varname")

  # Rewrite the file: drop the old block (the `varname:` line plus its
  # indented `!vault |` continuation lines, up to the next unindented key),
  # then append the freshly-encrypted block at the end. Write to a temp file
  # and mv so a mid-operation failure never leaves a half-edited vault file.
  local tmp
  tmp=$(mktemp)
  awk -v key="^${varname}:" '
    skip && /^[^[:space:]]/ { skip=0 }   # an unindented line ends the old block
    $0 ~ key                { skip=1; next }   # start of target block -> drop it
    skip                    { next }           # inside the block -> drop it
    { print }
  ' "$VAULT_FILE" > "$tmp"
  printf '%s\n' "$encrypted" >> "$tmp"
  mv "$tmp" "$VAULT_FILE"

  echo "Replaced '$varname' in $VAULT_FILE (vault-encrypted)" >&2
}

# Main dispatch
case "${1:-}" in
  get)     shift; cmd_get "$@" ;;
  get-raw) shift; cmd_get_raw "$@" ;;
  dump)    shift; cmd_dump "$@" ;;
  list)    shift; cmd_list "$@" ;;
  encrypt) shift; cmd_encrypt "$@" ;;
  set)     shift; cmd_set "$@" ;;
  replace) shift; cmd_replace "$@" ;;
  -h|--help|help) usage ;;
  "")      usage; exit 1 ;;
  *)       echo "Unknown command: $1" >&2; usage; exit 1 ;;
esac
