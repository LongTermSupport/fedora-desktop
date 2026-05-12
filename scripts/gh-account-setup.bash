#!/usr/bin/env bash
# gh-account-setup.bash — GitHub multi-account setup
#
# Standalone script for adding and configuring GitHub accounts.
# Handles: gh auth, OAuth scope audit, SSH key generation,
# programmatic key upload (gh ssh-key add), and isolated SSH verification.
#
# Called by run.bash (--setup-all) or directly (--add=alias:username).
#
# @see CLAUDE/Plan/00035-gh-multi-account-hardening/PLAN.md

set -e
set -u
set -o pipefail
IFS=$'\n\t'

# ─── Paths (overridable via env) ───────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LOCALHOST_YML="${LOCALHOST_YML:-$REPO_ROOT/environment/localhost/host_vars/localhost.yml}"
VAULT_PASS_FILE="${VAULT_PASS_FILE:-$REPO_ROOT/vault-pass.secret}"

# Required OAuth scopes — must mirror playbook's github_required_scopes.
# Note: parent scopes imply their read: children in GitHub's hierarchy,
# so we list only the top-level grant needed:
#   project    → implies read:project (don't list both)
#   admin:public_key → implies read:public_key
# @see playbooks/imports/play-github-cli-multi.yml
REQUIRED_SCOPES=(admin:public_key gist project read:org repo user:email)

# ─── Formatting ────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()    { echo -e "${CYAN}i ${NC}$1"; }
success() { echo -e "${GREEN}✓ ${NC}$1"; }
warning() { echo -e "${YELLOW}⚠ ${NC}$1"; }
error()   { echo -e "${RED}✗ ${NC}$1" >&2; }

# ─── Parse github_accounts from localhost.yml ──────────────────────────────────
parse_accounts() {
  [[ -f "$LOCALHOST_YML" ]] || { error "Config not found: $LOCALHOST_YML"; exit 1; }
  python3 - "$LOCALHOST_YML" <<'PYEOF'
import sys, yaml

def _ignore_vault(loader, tag_suffix, node):
    return None

_loader = yaml.SafeLoader
yaml.add_multi_constructor('', _ignore_vault, Loader=_loader)

with open(sys.argv[1]) as f:
    data = yaml.load(f, Loader=_loader)

for alias, username in (data.get('github_accounts') or {}).items():
    if username is not None:
        print(f"{alias}:{username}")
PYEOF
}

# ─── Decrypt github_ssh_passphrase from vault ─────────────────────────────────
decrypt_passphrase() {
  # Env var takes priority (set by run.bash when passphrase is already in memory)
  if [[ -n "${GITHUB_SSH_PASSPHRASE:-}" ]]; then
    printf '%s' "$GITHUB_SSH_PASSPHRASE"
    return
  fi
  if [[ ! -f "$VAULT_PASS_FILE" ]]; then
    error "Vault password file not found: $VAULT_PASS_FILE"
    echo -e "   ${YELLOW}➜${NC} Create it or run run.bash first" >&2
    exit 1
  fi
  # Ansible is noisy — capture stderr to a temp file so we can show it on failure
  local ansible_err
  ansible_err=$(mktemp)
  local raw_output pp=""
  if raw_output=$(ANSIBLE_STDOUT_CALLBACK=ansible.builtin.minimal \
    ansible localhost -c local \
    -e "@$LOCALHOST_YML" \
    -m debug -a "msg={{ github_ssh_passphrase }}" \
    --vault-id "localhost@$VAULT_PASS_FILE" 2>"$ansible_err"); then
    if pp=$(echo "$raw_output" | python3 -c "import sys,json,re;raw=sys.stdin.read();m=re.search(r'=>\s*(\{.*\})',raw,re.DOTALL);print(json.loads(m.group(1))['msg'],end='')" 2>&1); then
      : # parsed successfully
    else
      error "Failed to parse vault decryption output"
      pp=""
    fi
  else
    error "Ansible vault decryption failed"
    cat "$ansible_err" >&2
    pp=""
  fi
  rm -f "$ansible_err"

  if [[ -z "$pp" ]]; then
    error "Failed to decrypt github_ssh_passphrase from vault"
    echo -e "   ${YELLOW}➜${NC} Check that vault-pass.secret is correct" >&2
    exit 1
  fi
  printf '%s' "$pp"
}

# ─── Check if gh is authenticated for a username ──────────────────────────────
is_gh_authed() {
  local username="$1"
  local status_output
  if status_output=$(gh auth status --hostname github.com 2>&1); then
    echo "$status_output" | grep -qF "account ${username}"
  else
    # gh returns non-zero when not authenticated — check output anyway
    echo "$status_output" | grep -qF "account ${username}"
  fi
}

# ─── Switch to a gh account ───────────────────────────────────────────────────
switch_to_account() {
  local username="$1"
  local switch_output
  if ! switch_output=$(gh auth switch --user "$username" 2>&1); then
    # "already active" is not an error — only warn on real failures
    if echo "$switch_output" | grep -qi "already active"; then
      : # already on the right account, nothing to do
    else
      warning "Could not switch to ${username}: ${switch_output}"
    fi
  fi
}

# ─── Check if a required scope is satisfied by a granted scope set ────────────
# Honours GitHub's OAuth scope hierarchy: admin:* implies write:* implies read:*,
# and `user` implies its `user:email` / `read:user` / `user:follow` children.
# A token granted `admin:org` therefore satisfies a `read:org` requirement.
# $1 = required scope; $2 = comma-separated granted scopes. Whitespace (spaces,
# CR, LF) is normalised and any embedded newlines are converted to commas so
# multi-line HTTP header captures don't break comma-bounded matching.
_scope_satisfied() {
  local required="$1"
  local current
  current=",$(echo "$2" | tr -d ' \r' | tr '\n' ','),"
  local satisfiers="$required"
  case "$required" in
    read:org)         satisfiers="$required write:org admin:org" ;;
    write:org)        satisfiers="$required admin:org" ;;
    read:public_key)  satisfiers="$required write:public_key admin:public_key" ;;
    write:public_key) satisfiers="$required admin:public_key" ;;
    read:repo_hook)   satisfiers="$required write:repo_hook admin:repo_hook" ;;
    write:repo_hook)  satisfiers="$required admin:repo_hook" ;;
    read:gpg_key)     satisfiers="$required write:gpg_key admin:gpg_key" ;;
    write:gpg_key)    satisfiers="$required admin:gpg_key" ;;
    read:user|user:email|user:follow) satisfiers="$required user" ;;
  esac
  local s
  for s in $satisfiers; do
    case "$current" in
      *",${s},"*) return 0 ;;
    esac
  done
  return 1
}

# ─── Get missing OAuth scopes for the currently-active gh account ─────────────
get_missing_scopes() {
  local api_output scopes_header=""
  if api_output=$(gh api -i user 2>&1); then
    # Anchor on '^X-Oauth-Scopes:' so we only match the real response header,
    # not the Access-Control-Expose-Headers line whose value happens to list
    # 'X-OAuth-Scopes' as one of the exposed header names.
    scopes_header=$(echo "$api_output" | grep -i '^X-Oauth-Scopes:' | sed 's/^[^:]*: //') || scopes_header=""
  else
    warning "Could not query API for scope check"
  fi
  local missing=()
  for scope in "${REQUIRED_SCOPES[@]}"; do
    if ! _scope_satisfied "$scope" "$scopes_header"; then
      missing+=("$scope")
    fi
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    printf '%s\n' "${missing[@]}"
  fi
}

# ─── Isolated SSH verification ─────────────────────────────────────────────────
# Returns: 0 = verified correct user, 1 = wrong user, 2 = inconclusive
verify_ssh() {
  local alias="$1"
  local username="$2"
  local passphrase="${3:-}"
  local key_private="$HOME/.ssh/github_${alias}"

  info "Testing SSH: github_${alias}..."
  local tmp_key
  tmp_key=$(mktemp)
  cp "$key_private" "$tmp_key"
  chmod 600 "$tmp_key"
  if [[ -n "$passphrase" ]]; then
    # Strip passphrase from temp copy for non-interactive SSH test.
    # ssh-keygen writes "key saved" to stdout — discard it, keep stderr visible.
    if ! ssh-keygen -p -P "$passphrase" -N "" -f "$tmp_key" >/dev/null; then
      warning "Could not strip passphrase from temp key — SSH test may fail"
    fi
  fi

  # Fully isolated: -F /dev/null ignores ~/.ssh/config, -o IdentityAgent=none
  # ignores ssh-agent, -o IdentitiesOnly=yes forces only the specified key.
  # GitHub's SSH server returns rc=1 on successful auth (no shell access), so
  # non-zero is expected — we parse the output text to determine the result.
  local ssh_output=""
  if ssh_output=$(timeout 15 ssh -F /dev/null -o IdentityAgent=none -o IdentitiesOnly=yes \
    -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new \
    -i "$tmp_key" -T git@github.com 2>&1); then
    : # rc=0 unexpected from GitHub SSH, but not an error — parse output below
  fi
  rm -f "$tmp_key"

  local actual_user
  actual_user=$(echo "$ssh_output" | grep -oP 'Hi \K[^!]+') || actual_user=""

  if [[ "$actual_user" == "$username" ]]; then
    success "SSH verified: ${alias} → ${username}"
    return 0
  elif [[ -n "$actual_user" ]]; then
    error "SSH key github_${alias} authenticates as '${actual_user}', expected '${username}'"
    echo -e "   ${YELLOW}➜${NC} Delete this key from ${actual_user}'s GitHub settings, then re-run" >&2
    return 1
  else
    warning "SSH test inconclusive for ${alias} — key may need time to propagate"
    echo -e "   ${YELLOW}➜${NC} Manual test: ssh -F /dev/null -o IdentityAgent=none -i ~/.ssh/github_${alias} -T git@github.com" >&2
    return 2
  fi
}

# ─── Full per-account setup ───────────────────────────────────────────────────
setup_account() {
  local alias="$1"
  local username="$2"
  local passphrase="${3:-}"

  echo -e "\n${BOLD}━━━ ${alias} (${username}) ━━━${NC}"

  # Browser helper: instead of opening a browser (which may use the wrong profile),
  # display the URL for the user to copy into the correct browser profile.
  local browser_helper
  browser_helper=$(mktemp /tmp/gh-browser-XXXXXX.bash)
  cat > "$browser_helper" << BROWSEREOF
#!/bin/bash
echo ""
echo "   → \$1"
echo "     Open this URL in the browser profile for ${username}"
echo ""
BROWSEREOF
  chmod +x "$browser_helper"

  # 1. gh auth — --skip-ssh-key prevents the confusing key upload prompt
  if is_gh_authed "$username"; then
    success "Authenticated: ${username}"
  else
    info "Authenticating ${username}..."
    echo -e ""
    echo -e "   ${BOLD}1.${NC} Copy the one-time code shown below"
    echo -e "   ${BOLD}2.${NC} Press Enter when prompted — the URL will be displayed (browser will NOT open)"
    echo -e "   ${BOLD}3.${NC} Open the URL in the browser profile logged into ${BOLD}${username}${NC}"
    echo -e "   ${BOLD}4.${NC} Paste the code and authorise"
    echo -e ""
    # GH_BROWSER=browser_helper displays the URL instead of opening a browser.
    # --skip-ssh-key because we upload keys ourselves.
    # --scopes requests all required scopes upfront so a second device-code flow is not needed.
    local scope_csv
    scope_csv=$(printf '%s,' "${REQUIRED_SCOPES[@]}")
    scope_csv="${scope_csv%,}"
    if ! GH_BROWSER="$browser_helper" gh auth login --hostname github.com --git-protocol ssh --web --skip-ssh-key --scopes "$scope_csv"; then
      error "Authentication failed for ${username}"
      exit 1
    fi
    # Verify we authenticated as the expected user — catches wrong browser profile
    local authed_user
    if authed_user=$(gh api user --jq '.login' 2>&1); then
      if [[ "${authed_user,,}" != "${username,,}" ]]; then
        error "Authenticated as '${authed_user}' but expected '${username}'"
        echo -e "   ${YELLOW}➜${NC} You used the wrong browser profile" >&2
        echo -e "   ${YELLOW}➜${NC} Run: gh auth logout --hostname github.com --user ${authed_user}" >&2
        echo -e "   ${YELLOW}➜${NC} Then re-run this script using the browser profile for '${username}'" >&2
        exit 1
      fi
    fi
    success "Authenticated: ${username}"
  fi

  # 2. Switch to this account and check scopes
  switch_to_account "$username"

  local missing_scopes
  missing_scopes=$(get_missing_scopes)
  if [[ -n "$missing_scopes" ]]; then
    warning "Missing scopes: $(echo "$missing_scopes" | tr '\n' ' ')"
    echo -e ""
    echo -e "   Press Enter when prompted — the URL will be displayed (browser will NOT open)"
    echo -e "   Open the URL in the browser profile for ${BOLD}${username}${NC}"
    echo -e ""
    local scope_csv
    scope_csv=$(echo "$missing_scopes" | tr '\n' ',' | sed 's/,$//')
    if ! GH_BROWSER="$browser_helper" gh auth refresh --hostname github.com --scopes "$scope_csv"; then
      error "Failed to update scopes for ${username}"
      exit 1
    fi
    success "Scopes updated: ${username}"
  else
    success "Scopes OK: ${username}"
  fi

  # 3. SSH key generation
  local key_private="$HOME/.ssh/github_${alias}"
  local key_public="${key_private}.pub"
  if [[ -f "$key_private" ]]; then
    success "SSH key exists: github_${alias}"
  else
    info "Generating SSH key: github_${alias}"
    ssh-keygen -t ed25519 -C "${username}@github" -f "$key_private" -N "${passphrase}"
    chmod 600 "$key_private"
    success "SSH key generated: github_${alias}"
  fi

  # 4. Upload key to GitHub via gh ssh-key add
  switch_to_account "$username"
  local pub_key_data keys_output
  pub_key_data=$(awk '{print $2}' "$key_public")
  if keys_output=$(gh api user/keys --paginate 2>&1) && echo "$keys_output" | grep -q "$pub_key_data"; then
    success "Key registered on GitHub: ${username}"
  else
    info "Uploading key to GitHub: ${username}"
    local key_title
    key_title="$(hostname)-${alias}"
    if ! gh ssh-key add "$key_public" --title "$key_title" --type authentication; then
      error "Key upload failed for ${username}"
      echo -e "   ${YELLOW}➜${NC} Manual: gh auth switch --user ${username} && gh ssh-key add ${key_public} --title '${key_title}' --type authentication" >&2
      exit 1
    fi
    success "Key uploaded: ${username}"
  fi

  # 5. SSH test (fully isolated — no config/agent fallback)
  local rc=0
  verify_ssh "$alias" "$username" "$passphrase" || rc=$?
  if [[ $rc -eq 1 ]]; then
    rm -f "$browser_helper"
    # Wrong account — hard failure
    exit 1
  fi
  rm -f "$browser_helper"
  # rc=0 (verified) or rc=2 (inconclusive) — continue
}

# ─── Check account health (read-only) ─────────────────────────────────────────
check_account() {
  local alias="$1"
  local username="$2"
  local passphrase="${3:-}"
  local all_ok=true

  echo -e "\n${BOLD}━━━ ${alias} (${username}) ━━━${NC}"

  # Auth
  if is_gh_authed "$username"; then
    success "Auth: OK"
  else
    error "Auth: not authenticated as ${username}"
    all_ok=false
  fi

  # Scopes (only if authed)
  if is_gh_authed "$username"; then
    switch_to_account "$username"
    local missing_scopes
    missing_scopes=$(get_missing_scopes)
    if [[ -n "$missing_scopes" ]]; then
      error "Scopes: missing $(echo "$missing_scopes" | tr '\n' ' ')"
      all_ok=false
    else
      success "Scopes: OK"
    fi
  fi

  # SSH key file
  local key_private="$HOME/.ssh/github_${alias}"
  local key_public="${key_private}.pub"
  if [[ -f "$key_private" ]]; then
    success "SSH key: exists"
  else
    error "SSH key: missing (~/.ssh/github_${alias})"
    all_ok=false
  fi

  # Key registered on GitHub
  if [[ -f "$key_public" ]] && is_gh_authed "$username"; then
    switch_to_account "$username"
    local pub_key_data keys_output
    pub_key_data=$(awk '{print $2}' "$key_public")
    if keys_output=$(gh api user/keys --paginate 2>&1) && echo "$keys_output" | grep -q "$pub_key_data"; then
      success "GitHub key: registered"
    else
      error "GitHub key: not registered"
      all_ok=false
    fi
  fi

  # SSH test
  if [[ -f "$key_private" ]]; then
    verify_ssh "$alias" "$username" "$passphrase" || all_ok=false
  fi

  if [[ "$all_ok" == "true" ]]; then
    success "Overall: healthy"
    return 0
  else
    error "Overall: issues found"
    return 1
  fi
}

# ─── Add entry to localhost.yml ────────────────────────────────────────────────
add_to_config() {
  local alias="$1"
  local username="$2"

  [[ -f "$LOCALHOST_YML" ]] || {
    error "Config not found: $LOCALHOST_YML"
    echo -e "   ${YELLOW}➜${NC} Run run.bash first to create initial configuration" >&2
    exit 1
  }

  # Check if alias already exists
  local exists
  exists=$(python3 - "$LOCALHOST_YML" "$alias" <<'PYEOF'
import sys, yaml

def _ignore_vault(loader, tag_suffix, node):
    return None

_loader = yaml.SafeLoader
yaml.add_multi_constructor('', _ignore_vault, Loader=_loader)

with open(sys.argv[1]) as f:
    data = yaml.load(f, Loader=_loader)

accounts = data.get('github_accounts') or {}
print("yes" if sys.argv[2] in accounts else "no")
PYEOF
  )

  if [[ "$exists" == "yes" ]]; then
    info "Account '${alias}' already in localhost.yml — skipping config update"
    return 0
  fi

  # Insert into existing github_accounts block, or create it
  if grep -q 'github_accounts:' "$LOCALHOST_YML"; then
    python3 - "$LOCALHOST_YML" "$alias" "$username" <<'PYEOF'
import sys

filepath, alias, username = sys.argv[1], sys.argv[2], sys.argv[3]
with open(filepath) as f:
    lines = f.readlines()

in_block = False
insert_idx = None
for i, line in enumerate(lines):
    stripped = line.strip()
    if stripped.startswith('github_accounts:'):
        in_block = True
        insert_idx = i + 1
        continue
    if in_block:
        if line.startswith('  ') and ':' in line and not stripped.startswith('#'):
            insert_idx = i + 1
        elif stripped == '' or (not line.startswith(' ') and stripped != ''):
            break

if insert_idx is not None:
    lines.insert(insert_idx, f'  {alias}: "{username}"\n')
    with open(filepath, 'w') as f:
        f.writelines(lines)
else:
    print("ERROR: Could not find insertion point in github_accounts", file=sys.stderr)
    sys.exit(1)
PYEOF
    success "Added ${alias}: ${username} to localhost.yml"
  else
    printf '\ngithub_accounts:\n  %s: "%s"\n' "$alias" "$username" >> "$LOCALHOST_YML"
    success "Created github_accounts with ${alias}: ${username}"
  fi
}

# ─── Usage ─────────────────────────────────────────────────────────────────────
usage() {
  cat <<EOF
Usage: $(basename "$0") [OPTION]

GitHub multi-account setup — authentication, SSH keys, and verification.

Options:
  --add=ALIAS:USERNAME    Add and configure a new GitHub account
  --setup-all             Set up all accounts from localhost.yml
  --check                 Verify all accounts are healthy (read-only)
  --help                  Show this help

Examples:
  $(basename "$0") --add=work:johndoe-corp
  $(basename "$0") --setup-all
  $(basename "$0") --check

Required scopes per account: ${REQUIRED_SCOPES[*]}
Config file: \$LOCALHOST_YML (default: environment/localhost/host_vars/localhost.yml)
EOF
}

# ─── Main ──────────────────────────────────────────────────────────────────────
main() {
  local mode="" add_alias="" add_username=""

  if [[ $# -eq 0 ]]; then
    usage
    exit 0
  fi

  for arg in "$@"; do
    case "$arg" in
      --add=*)
        mode="add"
        local pair="${arg#--add=}"
        if [[ "$pair" != *":"* ]]; then
          error "Invalid format: use --add=ALIAS:USERNAME"
          exit 1
        fi
        add_alias="${pair%%:*}"
        add_username="${pair#*:}"
        if [[ -z "$add_alias" ]] || [[ -z "$add_username" ]]; then
          error "Both alias and username required: --add=ALIAS:USERNAME"
          exit 1
        fi
        ;;
      --setup-all) mode="setup-all" ;;
      --check)     mode="check" ;;
      --help|-h)   usage; exit 0 ;;
      *)
        error "Unknown option: $arg"
        usage >&2
        exit 1
        ;;
    esac
  done

  # Preflight: gh installed and version check
  if ! command -v gh >/dev/null; then
    error "GitHub CLI (gh) not installed"
    exit 1
  fi

  local gh_version
  gh_version=$(gh --version | grep -oP '\d+\.\d+\.\d+' | head -1)
  if ! printf '%s\n%s\n' "2.40.0" "$gh_version" | sort -V -C; then
    error "GitHub CLI v2.40.0+ required for multi-account support (found: ${gh_version})"
    exit 1
  fi

  case "$mode" in
    add)
      echo -e "\n${BOLD}Adding GitHub account: ${add_alias} (${add_username})${NC}"
      add_to_config "$add_alias" "$add_username"

      local passphrase=""
      if ! passphrase=$(decrypt_passphrase); then
        error "Cannot decrypt SSH passphrase — run run.bash first to set up the vault"
        exit 1
      fi
      setup_account "$add_alias" "$add_username" "$passphrase"

      echo -e "\n${GREEN}${BOLD}Done!${NC} Account ${add_alias} is ready."
      echo -e "${CYAN}i${NC} Run the playbook to deploy SSH config and git helpers:"
      echo -e "   ${BOLD}ansible-playbook ~/Projects/fedora-desktop/playbooks/imports/play-github-cli-multi.yml${NC}"
      ;;

    setup-all)
      local pairs
      mapfile -t pairs < <(parse_accounts)
      if [[ ${#pairs[@]} -eq 0 ]]; then
        warning "No github_accounts entries found in localhost.yml"
        exit 0
      fi

      echo -e "\n${BOLD}Setting up ${#pairs[@]} GitHub account(s)${NC}"
      echo -e "${CYAN}i${NC} Each account may need browser authentication — log in as the correct user when prompted"

      local passphrase=""
      if ! passphrase=$(decrypt_passphrase); then
        error "Cannot decrypt SSH passphrase"
        exit 1
      fi

      for pair in "${pairs[@]}"; do
        local _alias="${pair%%:*}"
        local _username="${pair#*:}"
        setup_account "$_alias" "$_username" "$passphrase"
      done
      echo -e "\n${GREEN}${BOLD}All accounts configured!${NC}"
      ;;

    check)
      local pairs
      mapfile -t pairs < <(parse_accounts)
      if [[ ${#pairs[@]} -eq 0 ]]; then
        warning "No github_accounts entries found in localhost.yml"
        exit 0
      fi

      echo -e "\n${BOLD}Checking ${#pairs[@]} GitHub account(s)${NC}"

      # Best-effort passphrase decrypt for SSH tests — not fatal if unavailable
      local passphrase="" pp_rc=0
      passphrase=$(decrypt_passphrase) || pp_rc=$?
      if [[ $pp_rc -ne 0 ]]; then
        passphrase=""
        warning "Could not decrypt passphrase — SSH tests may be limited"
      fi

      local all_healthy=true
      for pair in "${pairs[@]}"; do
        local _alias="${pair%%:*}"
        local _username="${pair#*:}"
        check_account "$_alias" "$_username" "$passphrase" || all_healthy=false
      done

      echo ""
      if [[ "$all_healthy" == "true" ]]; then
        success "All accounts healthy"
      else
        error "Some accounts have issues — run with --setup-all to fix"
        exit 1
      fi
      ;;
  esac
}

main "$@"
