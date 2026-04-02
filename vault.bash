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
  dump                    Decrypt and display ALL vault variables
  list                    List all vault-encrypted variable names
  encrypt <varname>       Encrypt a string and output the vault block
                          (reads value from stdin or prompts interactively)
  set <varname>           Encrypt a string and append it to localhost.yml
                          (reads value from stdin or prompts interactively)

Examples:
  ./vault.bash get github_ssh_passphrase
  ./vault.bash dump
  ./vault.bash list
  echo 'secret123' | ./vault.bash encrypt my_variable
  ./vault.bash set new_secret_var

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

# Decrypt all host vars to JSON (single ansible call, reliable output)
_decrypt_all_json() {
  ansible-inventory --host localhost \
    --vault-id "${VAULT_ID}@${VAULT_PASS_FILE}" \
    -i environment/localhost/hosts.yml
}

cmd_get() {
  local varname="${1:?Usage: vault.bash get <varname>}"
  check_vault_pass

  if ! grep -q "^${varname}:" "$VAULT_FILE" 2>/dev/null; then
    echo "ERROR: Variable '$varname' not found in $VAULT_FILE" >&2
    exit 1
  fi

  _decrypt_all_json | python3 -c "
import sys, json
data = json.load(sys.stdin)
val = data.get('${varname}', '<NOT FOUND>')
print(val)
"
}

cmd_dump() {
  check_vault_pass

  local vault_vars
  vault_vars=$(cmd_list)

  if [[ -z "$vault_vars" ]]; then
    echo "No vault-encrypted variables found in $VAULT_FILE" >&2
    return
  fi

  _decrypt_all_json | python3 -c "
import sys, json
data = json.load(sys.stdin)
vault_vars = '''${vault_vars}'''.strip().split('\n')
for var in vault_vars:
    val = data.get(var, '<NOT FOUND>')
    if not isinstance(val, str):
        val = repr(val)
    if len(val) > 80:
        val = val[:77] + '...'
    print(f'{var:<30} {val}')
"
}

cmd_list() {
  check_vault_pass
  grep ': !vault' "$VAULT_FILE" \
    | grep -oP '^[a-zA-Z_][a-zA-Z0-9_]*(?=:)' \
    | sort
}

cmd_encrypt() {
  local varname="${1:?Usage: vault.bash encrypt <varname>}"
  check_vault_pass

  local value
  if [[ -t 0 ]]; then
    # Interactive - prompt for value
    read -rsp "Enter value for '$varname': " value
    echo >&2
    local confirm
    read -rsp "Confirm value: " confirm
    echo >&2
    if [[ "$value" != "$confirm" ]]; then
      echo "ERROR: Values do not match" >&2
      exit 1
    fi
  else
    # Piped input
    value=$(cat)
  fi

  printf '%s' "$value" | ansible-vault encrypt_string \
    --vault-id "${VAULT_ID}@${VAULT_PASS_FILE}" \
    --stdin-name "$varname"
}

cmd_set() {
  local varname="${1:?Usage: vault.bash set <varname>}"
  check_vault_pass

  if grep -q "^${varname}:" "$VAULT_FILE" 2>/dev/null; then
    echo "ERROR: Variable '$varname' already exists in $VAULT_FILE" >&2
    echo "Edit the file manually to replace it, or use a different name." >&2
    exit 1
  fi

  local value
  if [[ -t 0 ]]; then
    read -rsp "Enter value for '$varname': " value
    echo >&2
    local confirm
    read -rsp "Confirm value: " confirm
    echo >&2
    if [[ "$value" != "$confirm" ]]; then
      echo "ERROR: Values do not match" >&2
      exit 1
    fi
  else
    value=$(cat)
  fi

  local encrypted
  encrypted=$(printf '%s' "$value" | ansible-vault encrypt_string \
    --vault-id "${VAULT_ID}@${VAULT_PASS_FILE}" \
    --stdin-name "$varname")

  echo "$encrypted" >> "$VAULT_FILE"
  echo "Saved '$varname' to $VAULT_FILE (vault-encrypted)" >&2
}

# Main dispatch
case "${1:-}" in
  get)     shift; cmd_get "$@" ;;
  dump)    shift; cmd_dump "$@" ;;
  list)    shift; cmd_list "$@" ;;
  encrypt) shift; cmd_encrypt "$@" ;;
  set)     shift; cmd_set "$@" ;;
  -h|--help|help) usage ;;
  "")      usage; exit 1 ;;
  *)       echo "Unknown command: $1" >&2; usage; exit 1 ;;
esac
