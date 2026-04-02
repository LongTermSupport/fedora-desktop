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

# Decrypt a single vault variable using ansible CLI
# Forces minimal callback to get predictable JSON output regardless of ansible.cfg
_decrypt_var() {
  local varname="$1"
  ANSIBLE_STDOUT_CALLBACK=ansible.builtin.minimal \
  ansible localhost -c local \
    --vault-id "${VAULT_ID}@${VAULT_PASS_FILE}" \
    -m debug -a "msg={{ ${varname} }}" \
    -i environment/localhost/hosts.yml 2>/dev/null \
    | sed 's/.*"msg": "//;s/"[[:space:]]*}$//'
}

cmd_get() {
  local varname="${1:?Usage: vault.bash get <varname>}"
  check_vault_pass

  if ! grep -qP "^${varname}:.*!vault" "$VAULT_FILE" 2>/dev/null; then
    echo "ERROR: Variable '$varname' not found as vault-encrypted in $VAULT_FILE" >&2
    exit 1
  fi

  _decrypt_var "$varname"
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
