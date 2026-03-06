#!/usr/bin/env bash
# Back up personal config and/or projects manifest to private GitHub config repo.
#
# Usage: ./fedora-install/push.bash [config|projects|all] [--account <github-username>]
#
#   config    Push localhost.yml only
#   projects  Push projects manifest only (config repo must already exist)
#   all       Push both (default)
#
# The config repo is: <github-username>/fedora-desktop-config (private, auto-created)

set -euo pipefail
IFS=$'\n\t'

## ── Colors ────────────────────────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

CHECK="✓"
CROSS="✗"
ARROW="➜"
INFO="ℹ"
WARN="⚠"

## ── Helpers ───────────────────────────────────────────────────────────────────

die() {
    echo -e "${RED}${CROSS} ERROR: $*${NC}" >&2
    exit 1
}

info() {
    echo -e "${CYAN}${INFO} $*${NC}"
}

success() {
    echo -e "${GREEN}${CHECK} $*${NC}"
}

warning() {
    echo -e "${YELLOW}${WARN} $*${NC}"
}

## ── Paths ─────────────────────────────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LOCALHOST_YML="${PROJECT_ROOT}/environment/localhost/host_vars/localhost.yml"
CONFIG_REPO_NAME="fedora-desktop-config"
PROJECTS_DIR="$HOME/Projects"

## ── Argument parsing ──────────────────────────────────────────────────────────

subcommand="all"
selected_account=""

# First positional arg may be subcommand
if [[ $# -gt 0 ]] && [[ "${1:-}" =~ ^(config|projects|all)$ ]]; then
    subcommand="$1"
    shift
fi

while [[ $# -gt 0 ]]; do
    case "${1:-}" in
        --account|-a)
            selected_account="${2:-}"
            shift 2
            ;;
        *)
            die "Unknown argument: $1\nUsage: $0 [config|projects|all] [--account <github-username>]"
            ;;
    esac
done

## ── Preflight ─────────────────────────────────────────────────────────────────

echo -e "${BOLD}Fedora Desktop — Push Backup${NC}"
echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${CYAN}Mode: ${BOLD}${subcommand}${NC}"
echo

if ! command -v gh > /dev/null; then
    die "GitHub CLI (gh) is not installed. Run: sudo dnf install gh"
fi

if ! gh auth status 2>/dev/null > /dev/null; then
    die "Not authenticated with GitHub. Run: gh auth login"
fi

if [[ "$subcommand" == "config" || "$subcommand" == "all" ]]; then
    if ! command -v ansible-vault > /dev/null; then
        die "ansible-vault not found. Ensure ansible is installed and ~/.local/bin is in PATH.\nTry: pipx install ansible-core"
    fi
    if [[ ! -f "$LOCALHOST_YML" ]]; then
        die "localhost.yml not found: $LOCALHOST_YML"
    fi
fi

if [[ "$subcommand" == "projects" || "$subcommand" == "all" ]]; then
    if [[ ! -d "$PROJECTS_DIR" ]]; then
        die "Projects directory not found: $PROJECTS_DIR"
    fi
fi

## ── Account selection ─────────────────────────────────────────────────────────

mapfile -t auth_accounts < <(
    gh auth status 2>&1 \
        | grep "Logged in to github.com account" \
        | grep -oE "account [^ ]+" \
        | awk '{print $2}'
)

if [[ ${#auth_accounts[@]} -eq 0 ]]; then
    die "No GitHub accounts found. Run: gh auth login"
fi

if [[ -n "$selected_account" ]]; then
    found=false
    for acct in "${auth_accounts[@]}"; do
        if [[ "$acct" == "$selected_account" ]]; then
            found=true
            break
        fi
    done
    if [[ "$found" == "false" ]]; then
        die "Account '$selected_account' is not authenticated. Authenticated: ${auth_accounts[*]}"
    fi
elif [[ ${#auth_accounts[@]} -eq 1 ]]; then
    selected_account="${auth_accounts[0]}"
    echo -e "${CYAN}One GitHub account detected: ${BOLD}${selected_account}${NC}"
    read -rp "Use this account? [Y/n]: " _confirm
    if [[ "${_confirm,,}" == "n" ]]; then
        die "Aborted. Authenticate the correct account with: gh auth login"
    fi
else
    echo -e "${CYAN}Multiple GitHub accounts authenticated:${NC}"
    i=1
    for acct in "${auth_accounts[@]}"; do
        echo -e "  ${BOLD}${i})${NC} ${acct}"
        i=$((i + 1))
    done
    echo
    while true; do
        read -rp "Select account number (1-${#auth_accounts[@]}): " choice
        if [[ "$choice" =~ ^[0-9]+$ ]] && [[ "$choice" -ge 1 ]] && [[ "$choice" -le ${#auth_accounts[@]} ]]; then
            selected_account="${auth_accounts[$((choice-1))]}"
            break
        fi
        echo -e "${RED}Invalid choice. Enter a number between 1 and ${#auth_accounts[@]}${NC}"
    done
fi

info "Switching to account: ${BOLD}${selected_account}${NC}"
gh auth switch --hostname github.com --user "$selected_account"
success "Active account: $selected_account"

config_repo="${selected_account}/${CONFIG_REPO_NAME}"

## ── push_api_file() — shared GitHub Contents API helper ──────────────────────

# Push a local file to the config repo via the GitHub Contents API.
# Usage: push_api_file <local-file> <repo-path> <commit-message>
push_api_file() {
    local local_file="$1"
    local repo_path="$2"
    local commit_msg="$3"

    local file_content
    file_content=$(base64 -w 0 "$local_file")

    local existing_sha=""
    if existing_sha=$(gh api \
        "repos/${config_repo}/contents/${repo_path}" \
        --jq '.sha' 2>/dev/null); then
        info "Updating existing file (sha: ${existing_sha:0:8}...)"
        gh api "repos/${config_repo}/contents/${repo_path}" \
            -X PUT \
            -f message="$commit_msg" \
            -f content="$file_content" \
            -f sha="$existing_sha" \
            > /dev/null
    else
        info "Creating file for the first time"
        gh api "repos/${config_repo}/contents/${repo_path}" \
            -X PUT \
            -f message="$commit_msg" \
            -f content="$file_content" \
            > /dev/null
    fi

    # Verify
    local remote_content local_content
    remote_content=$(gh api \
        "repos/${config_repo}/contents/${repo_path}" \
        --jq '.content' \
        | base64 -d)
    local_content=$(cat "$local_file")

    if [[ "$remote_content" != "$local_content" ]]; then
        die "Verification FAILED — remote content does not match local file.\nCheck github.com/${config_repo}"
    fi
    success "Verified — remote matches local"
}

## ── ensure_config_repo() ──────────────────────────────────────────────────────

ensure_config_repo() {
    echo
    info "Checking config repo: github.com/${config_repo}"

    if gh repo view "$config_repo" --json name --jq '.name' 2>/dev/null > /dev/null; then
        success "Config repo exists: github.com/${config_repo}"
    else
        info "Repo not found — creating private repo: ${config_repo}"
        gh repo create "$CONFIG_REPO_NAME" \
            --private \
            --description "fedora-desktop personal configuration for ${selected_account}"
        success "Created private repo: github.com/${config_repo}"
    fi

    # Ensure README.md exists
    if gh api "repos/${config_repo}/contents/README.md" --jq '.sha' 2>/dev/null > /dev/null; then
        return 0
    fi

    local readme_b64
    readme_b64=$(cat <<README_EOF | base64 -w 0
# fedora-desktop-config

Private configuration backup for the [fedora-desktop](https://github.com/LongTermSupport/fedora-desktop) Ansible setup.

## Contents

| File | Description |
|------|-------------|
| \`localhost.yml\` | Ansible host variables (\`environment/localhost/host_vars/localhost.yml\`) |
| \`projects.manifest\` | Git repo list for recloning on a fresh machine |

## Security

All sensitive values (passwords, API keys, tokens) are encrypted with
[Ansible Vault](https://docs.ansible.com/ansible/latest/vault_guide/index.html).
The vault password is **not** stored here — keep it in your password manager.

## Restore on Fresh Install

\`run.bash\` pulls \`localhost.yml\` from this repo automatically during setup,
skipping the manual configuration prompts. No action needed.

## Backup

\`\`\`bash
~/Projects/fedora-desktop/fedora-install/push.bash
\`\`\`

---

> ⚠️ **Keep this repo private.** It contains your personal system configuration.
README_EOF
)
    gh api "repos/${config_repo}/contents/README.md" \
        -X PUT \
        -f message="Add README.md" \
        -f content="$readme_b64" \
        > /dev/null
    success "README.md created"
}

## ── push_config() ─────────────────────────────────────────────────────────────

push_config() {
    echo
    echo -e "${BOLD}── Pushing config (localhost.yml) ──────────────────────────────${NC}"

    local vault_pass_file="${PROJECT_ROOT}/vault-pass.secret"
    if [[ ! -f "$vault_pass_file" ]]; then
        die "Vault password file not found: ${vault_pass_file}\nRun the main Ansible playbook first to initialise your vault."
    fi

    info "Checking vault encryption in localhost.yml..."

    local sensitive_pattern="(password|passwd|secret|token|api_key|api_secret|private_key|passphrase)"

    local plain_violations=()
    while IFS= read -r line; do
        [[ -z "$line" ]] && continue
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        if echo "$line" | grep -qiE "^\s*[a-z_]*(${sensitive_pattern})[a-z_]*\s*:"; then
            if ! echo "$line" | grep -q "!vault"; then
                plain_violations+=("$line")
            fi
        fi
    done < "$LOCALHOST_YML"

    if [[ ${#plain_violations[@]} -gt 0 ]]; then
        warning "${#plain_violations[@]} plain-text secret(s) found — encrypting in-place..."
        echo

        for orig_line in "${plain_violations[@]}"; do
            # Extract variable name (strip leading whitespace, get key before colon, strip trailing whitespace)
            local trimmed="${orig_line#"${orig_line%%[! ]*}"}"
            local var_name="${trimmed%%:*}"
            var_name="${var_name%"${var_name##*[! ]}"}"
            local plain_value="${orig_line#*: }"

            info "Encrypting: ${BOLD}${var_name}${NC}"

            local tmp_enc
            tmp_enc=$(mktemp)
            ansible-vault encrypt_string \
                --vault-id "localhost@${vault_pass_file}" \
                "$plain_value" \
                --name "$var_name" > "$tmp_enc"

            python3 - "$var_name" "$LOCALHOST_YML" "$tmp_enc" <<'PYEOF'
import sys, re

var_name = sys.argv[1]
yaml_file = sys.argv[2]
enc_file  = sys.argv[3]

with open(enc_file) as f:
    enc_block = f.read().rstrip('\n')

with open(yaml_file) as f:
    content = f.read()

pattern = r'^([ \t]*)' + re.escape(var_name) + r'[ \t]*:[ \t]*(?!!vault).*$'

def replace(m):
    indent = m.group(1)
    return '\n'.join(indent + ln for ln in enc_block.split('\n'))

new_content, count = re.subn(pattern, replace, content, count=1, flags=re.MULTILINE)
if count == 0:
    print(f'ERROR: could not locate plain-text line for {var_name}', file=sys.stderr)
    sys.exit(1)

with open(yaml_file, 'w') as f:
    f.write(new_content)
PYEOF

            rm -f "$tmp_enc"
            success "Encrypted: ${var_name}"
        done

        echo
        success "All ${#plain_violations[@]} plain-text secret(s) encrypted in localhost.yml"
    else
        success "No plain-text secrets detected"
    fi

    echo
    info "Verifying all vault-encrypted values decrypt cleanly..."
    if ansible desktop \
        --vault-id "localhost@${vault_pass_file}" \
        -m debug -a "msg=ok" \
        -e "@${LOCALHOST_YML}" > /dev/null; then
        success "Vault decryption verified"
    else
        die "Vault decryption check failed — vault password may be wrong or values corrupted\nCheck: ${vault_pass_file}"
    fi

    ensure_config_repo

    echo
    info "Pushing localhost.yml to github.com/${config_repo}..."
    local commit_msg
    commit_msg="Update localhost.yml from $(hostname) on $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    push_api_file "$LOCALHOST_YML" "localhost.yml" "$commit_msg"

    echo
    echo -e "${GREEN}${BOLD}Config pushed successfully!${NC}"
    echo -e "  ${ARROW} https://github.com/${config_repo}/blob/main/localhost.yml"
}

## ── push_projects() ───────────────────────────────────────────────────────────

push_projects() {
    echo
    echo -e "${BOLD}── Pushing projects manifest ───────────────────────────────────${NC}"

    if ! gh repo view "$config_repo" --json name --jq '.name' 2>/dev/null > /dev/null; then
        die "Config repo not found: github.com/${config_repo}\nRun: $0 config  (or: $0 all)"
    fi

    echo
    info "Scanning ${PROJECTS_DIR} for git repositories..."

    local tmp_manifest
    tmp_manifest=$(mktemp)

    {
        printf '# fedora-desktop projects manifest\n'
        printf '# Generated: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
        printf '# Format: <path-relative-to-Projects><TAB><git-origin-url><TAB><ssh-key-alias>\n'
        printf '#\n'
    } > "$tmp_manifest"

    declare -A _user_key_map
    declare -A _org_key_map

    info "Mapping SSH keys to GitHub accounts..."
    for _kf in ~/.ssh/github_*; do
        [[ "$_kf" == *.pub ]] && continue
        [[ ! -f "$_kf" ]] && continue
        local _kalias="${_kf#"$HOME/.ssh/github_"}"
        local _ssh_id=""
        if _ssh_id=$(ssh -i "$_kf" \
            -o IdentitiesOnly=yes -o BatchMode=yes \
            -o StrictHostKeyChecking=no -o ConnectTimeout=5 \
            git@github.com 2>&1); then :; fi
        if [[ "$_ssh_id" =~ Hi\ ([^!]+)! ]]; then
            local _gh_username="${BASH_REMATCH[1]}"
            _user_key_map["$_gh_username"]="$_kalias"
            _org_key_map["$_gh_username"]="$_kalias"
            info "  key:${_kalias} → @${_gh_username}"
        fi
    done

    info "Resolving org ownership via GitHub API..."
    declare -A _org_candidates
    for _gh_user in "${!_user_key_map[@]}"; do
        local _kalias="${_user_key_map[$_gh_user]}"
        gh auth switch --hostname github.com --user "$_gh_user" 2>/dev/null || continue
        while IFS= read -r _org_login; do
            [[ -z "$_org_login" ]] && continue
            if [[ -z "${_org_candidates[$_org_login]:-}" ]]; then
                _org_candidates["$_org_login"]="$_kalias"
            else
                _org_candidates["$_org_login"]+=" $_kalias"
            fi
        done < <(gh api /user/memberships/orgs \
            --paginate \
            --jq '.[] | select(.role == "admin") | .organization.login' 2>/dev/null)
    done

    gh auth switch --hostname github.com --user "$selected_account"

    for _org_login in "${!_org_candidates[@]}"; do
        IFS=' ' read -ra _cands <<< "${_org_candidates[$_org_login]}"
        if [[ ${#_cands[@]} -eq 1 ]]; then
            _org_key_map["$_org_login"]="${_cands[0]}"
            info "  org:${_org_login} → key:${_cands[0]}"
        else
            echo
            echo -e "${CYAN}Multiple keys have admin access to org: ${BOLD}${_org_login}${NC}"
            local _i=1
            for _ka in "${_cands[@]}"; do
                for _gh_u in "${!_user_key_map[@]}"; do
                    if [[ "${_user_key_map[$_gh_u]}" == "$_ka" ]]; then
                        echo -e "  ${BOLD}${_i})${NC} key:${_ka} (@${_gh_u})"
                        break
                    fi
                done
                _i=$((_i+1))
            done
            local _chosen=""
            while true; do
                read -rp "Select key for ${_org_login} (1-${#_cands[@]}): " _choice
                if [[ "$_choice" =~ ^[0-9]+$ ]] && [[ "$_choice" -ge 1 ]] && [[ "$_choice" -le ${#_cands[@]} ]]; then
                    _chosen="${_cands[$((_choice-1))]}"
                    break
                fi
                echo -e "${RED}Invalid choice. Enter a number between 1 and ${#_cands[@]}${NC}"
            done
            _org_key_map["$_org_login"]="$_chosen"
            info "  org:${_org_login} → key:${_chosen}"
        fi
    done

    _find_key_alias() {
        local url="$1"
        local _url_path owner
        _url_path="${url##*:}"
        owner="${_url_path%%/*}"
        if [[ -z "$owner" ]] || [[ "$owner" == "$url" ]]; then
            echo ""
            return
        fi
        echo "${_org_key_map[$owner]:-}"
    }

    local repo_count=0
    local skip_count=0

    while IFS= read -r git_dir; do
        local repo_dir="${git_dir%/.git}"
        local rel_path="${repo_dir#"${PROJECTS_DIR}/"}"
        local origin_url=""

        if ! origin_url=$(git -C "$repo_dir" remote get-url origin 2>/dev/null); then
            warning "No origin remote: ${rel_path} — skipping"
            skip_count=$((skip_count + 1))
            continue
        fi

        if [[ "$origin_url" == https://github.com/* ]]; then
            origin_url="git@github.com:${origin_url#https://github.com/}"
        fi

        local key_alias
        key_alias=$(_find_key_alias "$origin_url")
        printf '%s\t%s\t%s\n' "$rel_path" "$origin_url" "$key_alias" >> "$tmp_manifest"
        info "  ${BOLD}${rel_path}${NC} ${ARROW} ${origin_url}${key_alias:+ [key: ${key_alias}]}"
        repo_count=$((repo_count + 1))
    done < <(find "$PROJECTS_DIR" -maxdepth 6 -name ".git" -type d | sort)

    echo
    success "Found ${repo_count} repositories"
    if [[ "$skip_count" -gt 0 ]]; then
        warning "${skip_count} skipped (no origin remote)"
    fi

    echo
    info "Pushing projects.manifest to github.com/${config_repo}..."
    local commit_msg
    commit_msg="Update projects.manifest from $(hostname) on $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    push_api_file "$tmp_manifest" "projects.manifest" "$commit_msg"
    rm -f "$tmp_manifest"

    echo
    echo -e "${GREEN}${BOLD}Projects manifest pushed successfully!${NC}"
    echo -e "  ${ARROW} https://github.com/${config_repo}/blob/main/projects.manifest"
    echo -e "  ${ARROW} ${repo_count} repositories saved"
}

## ── Dispatch ──────────────────────────────────────────────────────────────────

case "$subcommand" in
    config)
        push_config
        ;;
    projects)
        push_projects
        ;;
    all)
        push_config
        push_projects
        echo
        echo -e "${GREEN}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
        echo -e "${GREEN}${BOLD}║              All backups pushed successfully!                ║${NC}"
        echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}"
        echo
        echo -e "${CYAN}On a fresh install, run.bash pulls localhost.yml automatically.${NC}"
        echo -e "${CYAN}Projects can be restored with: ./fedora-install/pull-projects.bash${NC}"
        echo
        ;;
esac
