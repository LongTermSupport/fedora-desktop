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

# Use ansible's Python API directly — no CLI output parsing needed
_vault_py() {
  python3 - "$@" << 'PYEOF'
import re, sys
from ansible.parsing.dataloader import DataLoader
from ansible.parsing.vault import VaultSecret

VAULT_FILE = "environment/localhost/host_vars/localhost.yml"

loader = DataLoader()
with open("vault-pass.secret", "rb") as f:
    loader.set_vault_secrets([("localhost", VaultSecret(f.read().strip()))])
data = loader.load_from_file(VAULT_FILE)

def vault_var_names():
    with open(VAULT_FILE) as f:
        return sorted(re.findall(r"^([a-zA-Z_]\w*):.*!vault", f.read(), re.MULTILINE))

cmd = sys.argv[1]

if cmd == "get":
    name = sys.argv[2]
    if name not in data:
        print(f"ERROR: '{name}' not found in {VAULT_FILE}", file=sys.stderr)
        sys.exit(1)
    print(str(data[name]))

elif cmd == "list":
    for name in vault_var_names():
        print(name)

elif cmd == "dump":
    for name in vault_var_names():
        val = str(data.get(name, "<NOT FOUND>"))
        if len(val) > 80:
            val = val[:77] + "..."
        print(f"{name:<30} {val}")
PYEOF
}

cmd_get() {
  local varname="${1:?Usage: vault.bash get <varname>}"
  check_vault_pass
  _vault_py get "$varname"
}

cmd_dump() {
  check_vault_pass
  _vault_py dump
}

cmd_list() {
  check_vault_pass
  _vault_py list
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
