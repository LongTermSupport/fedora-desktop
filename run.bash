#!/usr/bin/env bash

## Setup
## !! BUMP THIS VERSION ON EVERY CHANGE TO THIS FILE — NO EXCEPTIONS !!
## !! If you forget, there is NO WAY to tell which version is running !!
RUN_BASH_VERSION="1.11.0"  # Feature (Plan 00063) slice 2: headless PREFLIGHT — headless_preflight validates+resolves all RUN_BASH_* input up front (non-root check, NOPASSWD-sudo probe, required email/accounts, secret *_FILE resolution with V3.10 guardrails: file-precedence, both-set/unreadable/literal-on-cloud fail-fast, literal-elsewhere warn, unset literals before first child), set -u-safe secret-file EXIT trap. v1.9.1: defer the GitHub-empty ('none') path per round-3 decision — headless v1 requires a single GitHub account + token file (fail fast on 'none'); help + acceptance aligned. v1.9.2: require RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE in v1 — the login SSH key stays passphrase-protected (D6), mirroring the interactive no-empty-passphrase rule, since headless loads it non-interactively via ssh-agent/SSH_ASKPASS (D5). v1.9.3: begin the EXECUTION slice — add hl_abort (BIG LOUD banner, exit 1) for headless execution failures, and a headless backstop at the top of every shared interactive prompt helper (confirm/promptForValue/promptChoice/promptSecretConfirmed/promptDefault/prompt_verified_vault_password/prompt_github_accounts_yaml) so a headless run that ever reaches a prompt fails LOUD instead of hanging (fail-fast rule 11). v1.9.4: GitHub/SSH execution mechanics — hl_ssh_agent_start (ssh-agent + transient 0700 SSH_ASKPASS reading a 0600 passphrase file, V3.13), hl_ssh_agent_stop (kill after last git op, V3.12), hl_cleanup EXIT trap (shred secret files + backstop agent kill, V3.11); headless branches for keygen (-P from resolved passphrase + agent load), hostname (RUN_BASH_HOSTNAME or leave default), gh token auth (gh auth login --with-token from stdin + git_protocol=ssh). All fail LOUD via hl_abort. v1.9.5: localhost.yml assembly — hl_write_localhost_yml (idempotent keep, else RUN_BASH_CONFIG_SOURCE pull from the private config repo, else FRESH from RUN_BASH_* identity + github_accounts), hl_pull_config_source (private-repo gate + LOUD 404), hl_reconcile_vault (D6: provided-or-fail, verify against encrypted values, NEVER auto-generate over !vault); headless branch for github_ssh_passphrase (reuse resolved passphrase, vault-encrypt). Interactive config/vault blocks wrapped under `if HEADLESS != true`. v1.10.0: FLIP the honest-stop — headless now flows through the FULL body (gh-account-setup gets RUN_BASH_HEADLESS + fails LOUD on any interactive gh web/scope-refresh; main playbook gets RUN_BASH_PROVISIONING_PROFILE passthrough + D7 loud-fatal on failure; optional playbooks via RUN_BASH_OPTIONAL_PLAYBOOKS; projects restore via RUN_BASH_RESTORE_PROJECTS; reboot via RUN_BASH_REBOOT). END-TO-END execution is HOST-verified on a real server (Phase 3) — in-container this is bash -n + shellcheck + preflight acceptance only. v1.11.0: Feature (Plan 00065 Phase 5) — RUN_BASH_OPTIONAL_PLAYBOOKS accepts the reserved keyword 'server-recommended', expanded from the tracked manifest playbooks/imports/optional/server-recommended.bundle into its listed plays before the existing per-token resolver runs; composes with explicit tokens via de-dup, unknown-token/failure handling unchanged.

# ── Sourced-shell pollution guard (H4) ───────────────────────────────────────
# The documented install is `(source <(curl ... run.bash))` — sourced INSIDE a
# subshell (the parens). The parens are LOAD-BEARING: they contain set -e / IFS /
# trap / exit so they never leak into or kill the user's interactive shell.
#
# The whole executable body is wrapped in main() (see end of file) and only runs
# when main "$@" is called on the last line. main() runs everything in an explicit
# subshell ( ... ) of its own, so set -e / IFS / trap / exit are contained there
# regardless of how the file was loaded. That makes the bare-source case
# (`source <(curl ...)` without the documented parens) safe too: nothing escapes
# into the caller's interactive shell. The documented parenthesised path keeps
# working unchanged. set -e / IFS / pipefail are therefore set INSIDE main(), not
# at top level, so they never touch a sourcing shell.

## Colors and formatting (constants — safe at top level even if sourced)
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

## Unicode symbols
CHECK="✓"
CROSS="✗"
ARROW="➜"
INFO="ℹ"
WARN="⚠"
BUG="🐛"

# ── Headless / unattended helpers (Plan 00063) ───────────────────────────────
# Defined at TOP LEVEL (before main) so they are available in the early
# flags/preflight region, which runs before main()'s nested functions exist.
# They depend only on the colour/symbol constants above. Fail-fast rule 11:
# a headless run never hangs — a missing/unsafe input aborts with a specific,
# actionable message on stderr.

# headless_fail <what-is-wrong> <how-to-fix> — abort a headless run (exit 1).
# MUST be called directly (never inside $(...)) so exit ends the whole script.
headless_fail() {
  echo -e "\n${RED}${BOLD}${CROSS} Headless run cannot proceed${NC}" >&2
  echo -e "${RED}  ${1}${NC}" >&2
  echo -e "${YELLOW}${ARROW} ${2}${NC}" >&2
  echo -e "${YELLOW}${ARROW} Full contract: ./run.bash --help-run-headless${NC}" >&2
  exit 1
}

# hl_abort <step> <what-failed> [how-to-debug] — BIG LOUD, unmissable abort for a
# headless EXECUTION failure (after preflight, during actual provisioning). The
# whole point of headless is an unattended run the operator is NOT watching live, so
# any failure must SCREAM: a red banner naming the exact step, the concrete reason,
# and a debug pointer — then exit non-zero so the run never limps on or hangs.
# MUST be called directly (never inside $(...)) so exit ends the whole script.
hl_abort() {
  local _step="$1" _what="$2" _debug="${3:-}"
  {
    echo -e "\n${RED}${BOLD}╔════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}${BOLD}║  HEADLESS PROVISIONING FAILED — run.bash v${RUN_BASH_VERSION}${NC}"
    echo -e "${RED}${BOLD}╚════════════════════════════════════════════════════════════════╝${NC}"
    echo -e "${RED}${BOLD}  STEP :${NC} ${_step}"
    echo -e "${RED}${BOLD}  WHY  :${NC} ${_what}"
    if [[ -n "$_debug" ]]; then
      echo -e "${YELLOW}${BOLD}  DEBUG:${NC} ${_debug}"
    fi
    echo -e "${YELLOW}${ARROW} Headless mode is unattended — fix the above and re-run. Full contract:${NC}"
    echo -e "${YELLOW}${ARROW} ./run.bash --help-run-headless${NC}\n"
  } >&2
  exit 1
}

# hl_is_cloud — true if this looks like a cloud-init-provisioned box, where a
# literal secret in the environment persists in user-data + the metadata service.
hl_is_cloud() {
  [[ -d /var/lib/cloud/instance ]]
}

# hl_resolve_secret <BASENAME> <OUT_VAR> — resolve a secret from
# RUN_BASH_<BASENAME>_FILE (preferred) or the literal RUN_BASH_<BASENAME>, applying
# the V3.10 guardrails, and assign the secret bytes to the global named <OUT_VAR>
# via printf -v (NOT echoed — so it never lands in a captured/logged stdout, and so
# headless_fail runs in the caller's shell and exits cleanly). Empty when neither
# is set — the caller decides required-ness.
#   both file+literal set      -> fail fast (ambiguous; the literal still leaks)
#   *_FILE set but unreadable   -> fail fast (never fall back)
#   literal on a cloud box      -> fail fast (user-data / metadata persistence)
#   literal elsewhere           -> loud stderr warning, allowed
hl_resolve_secret() {
  local _base="$1" _out="$2"
  local _lit="RUN_BASH_${_base}" _file="RUN_BASH_${_base}_FILE"
  local _litval="${!_lit:-}" _fileval="${!_file:-}"
  local _result=""
  if [[ -n "$_fileval" && -n "$_litval" ]]; then
    headless_fail "Both ${_file} and ${_lit} are set (ambiguous, and the literal still leaks)." \
      "Set exactly one — prefer the *_FILE form (the secret bytes never enter the environment)."
  fi
  if [[ -n "$_fileval" ]]; then
    if [[ ! -r "$_fileval" ]]; then
      headless_fail "${_file}=${_fileval} is not a readable file." \
        "Point it at a 0600 file containing the secret; there is no fallback to a literal."
    fi
    # cat strips the trailing newline that echo>file / here-strings add.
    _result="$(cat -- "$_fileval")"
  elif [[ -n "$_litval" ]]; then
    if hl_is_cloud; then
      headless_fail "${_lit} is set as a LITERAL on a cloud-init box." \
        "Literal secrets persist in cloud-init user-data + the metadata service (world-readable). Use ${_file} with an out-of-band-fetched 0600 file."
    fi
    echo -e "${YELLOW}${WARN} ${_lit} passed as a literal env value — it is inherited by child processes via /proc/PID/environ. Prefer ${_file}.${NC}" >&2
    _result="$_litval"
  fi
  printf -v "$_out" '%s' "$_result"
}

# headless_preflight — validate every precondition + resolve every RUN_BASH_* value
# BEFORE any provisioning action, so an unattended run fails fast (never hangs) on a
# missing/unsafe input. Populates HL_* globals (non-exported: not visible to child
# processes via the environment) consumed by the execution path.
headless_preflight() {
  echo -e "${CYAN}${INFO} Headless mode — validating RUN_BASH_* configuration${NC}" >&2

  # Non-root: cloud-init runcmd is root; run.bash must run as the target user
  # (matches the interactive root refusal). Checked here with headless guidance.
  if [[ "$(whoami)" == "root" ]]; then
    headless_fail "Headless run is executing as root." \
      "Run as the non-root target user (cloud-init: sudo -u <user> -i env RUN_BASH_...=... ./run.bash)."
  fi

  # Required identity.
  HL_USER_EMAIL="${RUN_BASH_USER_EMAIL:-}"
  [[ -n "$HL_USER_EMAIL" ]] || headless_fail "RUN_BASH_USER_EMAIL is required." \
    "Set it to the git email for this box, e.g. RUN_BASH_USER_EMAIL=name@example.com."
  [[ "$HL_USER_EMAIL" == *@*.* ]] || headless_fail "RUN_BASH_USER_EMAIL='${HL_USER_EMAIL}' is not a valid email." \
    "Use a form like name@example.com."
  HL_USER_LOGIN="${RUN_BASH_USER_LOGIN:-$(whoami)}"
  HL_USER_NAME="${RUN_BASH_USER_NAME:-$HL_USER_LOGIN}"

  # GitHub is mandatory to CONFIGURE — accounts, or the literal 'none' to skip it.
  HL_GITHUB_ACCOUNTS="${RUN_BASH_GITHUB_ACCOUNTS:-}"
  [[ -n "$HL_GITHUB_ACCOUNTS" ]] || headless_fail "RUN_BASH_GITHUB_ACCOUNTS is required." \
    "Set it to a single GitHub account (headless v1 requires GitHub configured)."

  # v1 requires GitHub CONFIGURED. The 'configured empty' (RUN_BASH_GITHUB_ACCOUNTS=
  # none) path is a planned follow-up: it must first fix two latent server-profile
  # bugs (play-github-cli-multi.yml's ungated `gh --version`, play-lxc's git@ clone)
  # that abort playbook-main on a no-GitHub box. Until then, fail fast rather than
  # provision a box that would break at the playbook stage.
  if [[ "$HL_GITHUB_ACCOUNTS" == "none" ]]; then
    headless_fail "RUN_BASH_GITHUB_ACCOUNTS=none (GitHub-empty provisioning) is not supported in headless v1." \
      "Provide a single GitHub account + RUN_BASH_GITHUB_TOKEN_FILE. (Empty-GitHub is a planned follow-up.)"
  fi
  # v1 supports a SINGLE account; multiple need one token file per alias (D5).
  if [[ "$HL_GITHUB_ACCOUNTS" == *,* ]]; then
    headless_fail "Multiple GitHub accounts ('${HL_GITHUB_ACCOUNTS}') are not supported in headless v1." \
      "Use a single account."
  fi

  # Vault password: must be PROVIDED (either form, file preferred), NEVER
  # auto-generated headless (V3.3/D6). Resolved here; the execution slice enforces
  # required-when-vault-present.
  hl_resolve_secret VAULT_PASSWORD HL_VAULT_PASSWORD

  # Scoped token is required for non-interactive gh auth (`gh auth login --with-token`).
  hl_resolve_secret GITHUB_TOKEN HL_GITHUB_TOKEN
  [[ -n "$HL_GITHUB_TOKEN" ]] || headless_fail "RUN_BASH_GITHUB_TOKEN_FILE is required (headless v1 requires GitHub configured)." \
    "Provide a 0600 file holding a scoped PAT (scopes: vars/github-required-scopes.yml + admin:public_key)."
  hl_resolve_secret GITHUB_SSH_PASSPHRASE HL_GITHUB_SSH_PASSPHRASE
  # Decision 6: the login SSH key stays passphrase-protected (this mirrors the
  # interactive flow, which forbids an empty passphrase — run.bash:1278-1284).
  # Headless v1 always configures GitHub and provisions the key non-interactively
  # (ssh-agent + SSH_ASKPASS, D5/V3.12-V3.13), so the passphrase MUST be supplied up
  # front — there is no TTY to prompt for it during the clone/pull later.
  [[ -n "$HL_GITHUB_SSH_PASSPHRASE" ]] || headless_fail "RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE is required (the login SSH key must stay passphrase-protected)." \
    "Provide a 0600 file holding the SSH key passphrase (loaded via ssh-agent for the clone; never passed on argv to a child)."

  # V3.10(e): drop any LITERAL secret env vars so children (dnf, gh, ansible) do not
  # inherit them via /proc/PID/environ. The *_FILE path vars are not secret and stay.
  unset RUN_BASH_VAULT_PASSWORD RUN_BASH_GITHUB_TOKEN RUN_BASH_GITHUB_SSH_PASSPHRASE

  # NOPASSWD:ALL sudo (V3.7): the first direct sudo (dnf) runs with no TTY. Probe
  # with -k so a cached timestamp cannot yield a false pass. Capture stderr (no
  # error-hiding redirect) so the failure reason is shown. Last precondition — after
  # the cheaper config checks so the most common mistake (missing env) reports first.
  local _sudo_probe
  if ! _sudo_probe="$(sudo -k -n true 2>&1)"; then
    headless_fail "Passwordless (NOPASSWD:ALL) sudo is required (sudo: ${_sudo_probe:-a password is required})." \
      "Grant NOPASSWD:ALL to this user (the default cloud user has it), or run interactively."
  fi

  echo -e "${GREEN}${CHECK} Headless preflight OK${NC} — user=${HL_USER_LOGIN} (${HL_USER_NAME}) email=${HL_USER_EMAIL} github=${HL_GITHUB_ACCOUNTS}" >&2
}

# hl_cleanup — EXIT-trap cleanup for a headless run: shred every 0600 secret file and,
# as a BACKSTOP, tear down the ssh-agent if it is still up (V3.11/V3.12). The agent is
# normally killed right after the last git op (hl_ssh_agent_stop); this only catches an
# abnormal exit. set -u-safe: every var is expanded with `:-` and the array with
# "${arr[@]:-}", so it is a harmless no-op on an interactive run or an early abort.
hl_cleanup() {
  rm -f /tmp/.github_ssh_pp "${HL_SECRET_FILES[@]:-}"
  if [[ -n "${HL_SSH_AGENT_PID:-}" ]]; then
    local _o
    if ! _o="$(SSH_AGENT_PID="$HL_SSH_AGENT_PID" ssh-agent -k 2>&1)"; then
      echo "  (cleanup) ssh-agent already gone: ${_o}" >&2
    fi
  fi
}

# hl_ssh_agent_start — start an ssh-agent and load the passphrase-protected login key
# (~/.ssh/id) non-interactively via a transient SSH_ASKPASS helper (D5/V3.13). There is
# NO file/stdin passphrase flag for ssh-add — SSH_ASKPASS (+SSH_ASKPASS_REQUIRE=force)
# is the ONLY non-interactive path. The passphrase is written to a 0600 file the helper
# reads at runtime (the helper carries only the non-secret PATH, never the passphrase),
# and both temp files are shredded by hl_cleanup. Fails LOUD on any error.
hl_ssh_agent_start() {
  HL_SSH_PP_FILE="$(mktemp)" && chmod 600 "$HL_SSH_PP_FILE"
  printf '%s' "$HL_GITHUB_SSH_PASSPHRASE" > "$HL_SSH_PP_FILE"
  HL_ASKPASS="$(mktemp)" && chmod 700 "$HL_ASKPASS"
  # Quoted heredoc: the helper body is written VERBATIM (the $HL_SSH_PP_FILE reference
  # is resolved at askpass RUNTIME from the inherited env, not expanded here) — so the
  # passphrase never enters the helper's own text, only the non-secret path does.
  cat > "$HL_ASKPASS" <<'HL_ASKPASS_BODY'
#!/usr/bin/env bash
cat "$HL_SSH_PP_FILE"
HL_ASKPASS_BODY
  HL_SECRET_FILES+=("$HL_SSH_PP_FILE" "$HL_ASKPASS")
  export HL_SSH_PP_FILE
  local _agent_out
  if ! _agent_out="$(ssh-agent -s)"; then
    hl_abort "ssh-agent start" "could not start ssh-agent to load the login SSH key" \
      "ssh-agent -s failed: ${_agent_out}"
  fi
  eval "$_agent_out"                # sets+exports SSH_AUTH_SOCK, SSH_AGENT_PID
  HL_SSH_AGENT_PID="${SSH_AGENT_PID:-}"
  local _add_out
  if ! _add_out="$(SSH_ASKPASS="$HL_ASKPASS" SSH_ASKPASS_REQUIRE=force ssh-add ~/.ssh/id 2>&1)"; then
    hl_abort "load login SSH key into ssh-agent" \
      "the login SSH key (\$HOME/.ssh/id) could not be loaded — the supplied RUN_BASH_GITHUB_SSH_PASSPHRASE is probably wrong for this key" \
      "ssh-add said: ${_add_out}"
  fi
}

# hl_ssh_agent_stop — kill the ssh-agent immediately after the LAST git op (V3.12), so
# the unlocked key is not left reachable via $SSH_AUTH_SOCK across ansible-galaxy, the
# main playbook, optional playbooks, and reboot. hl_cleanup is only a backstop.
hl_ssh_agent_stop() {
  [[ -n "${HL_SSH_AGENT_PID:-}" ]] || return 0
  local _o
  if ! _o="$(SSH_AGENT_PID="$HL_SSH_AGENT_PID" ssh-agent -k 2>&1)"; then
    warning "ssh-agent teardown returned non-zero (agent may already be gone): ${_o}"
  fi
  unset SSH_AUTH_SOCK SSH_AGENT_PID HL_SSH_AGENT_PID
}

# hl_pull_config_source <localhost_yml> <hosts/name.yml> — headless: pull a saved
# config from the PRIVATE per-user config repo (RUN_BASH_CONFIG_SOURCE path). Refuses
# a non-private repo (localhost.yml carries PII + the vault) and fails LOUD if the repo
# or file is missing. Called only when RUN_BASH_CONFIG_SOURCE is set and != none.
hl_pull_config_source() {
  local yml="$1" path="$2"
  local repo="${primary_gh_username}/fedora-desktop-config" _priv _content
  if ! _priv="$(gh api "repos/${repo}" --jq '.private' 2>&1)"; then
    hl_abort "pull config source" \
      "config repo github.com/${repo} not found or not accessible" \
      "gh said: ${_priv}; set RUN_BASH_CONFIG_SOURCE=none to configure fresh from RUN_BASH_* instead"
  fi
  if [[ "$_priv" != "true" ]]; then
    hl_abort "pull config source" \
      "config repo github.com/${repo} is NOT private (.private='${_priv}') — it would hold PII + your Ansible vault" \
      "make it private (gh repo edit ${repo} --visibility private), or use RUN_BASH_CONFIG_SOURCE=none"
  fi
  if ! _content="$(gh api "repos/${repo}/contents/${path}" --jq '.content' 2>&1)"; then
    hl_abort "pull config source" \
      "config file '${path}' not found in github.com/${repo}" \
      "gh said: ${_content}; set RUN_BASH_CONFIG_SOURCE to a valid hosts/<name>.yml or 'none'"
  fi
  printf '%s' "$_content" | base64 -d > "$yml"
  success "Headless: pulled config ${path} from github.com/${repo}"
}

# hl_write_localhost_yml <localhost_yml> — headless replacement for the interactive
# config-import menu. Idempotent: keeps an already-configured localhost.yml. Otherwise
# pulls RUN_BASH_CONFIG_SOURCE from the private config repo, or (the default 'none')
# writes a FRESH localhost.yml from RUN_BASH_* identity + RUN_BASH_GITHUB_ACCOUNTS.
hl_write_localhost_yml() {
  local yml="$1"
  if [[ -f "$yml" ]] && grep -qE '(!vault|github_accounts)' "$yml"; then
    info "Headless: keeping existing configured localhost.yml"
    return 0
  fi
  local src="${RUN_BASH_CONFIG_SOURCE:-none}"
  if [[ -n "$src" && "$src" != "none" ]]; then
    info "Headless: importing saved config '${src}' from the config repo"
    hl_pull_config_source "$yml" "$src"
    return 0
  fi
  info "Headless: writing fresh localhost.yml (identity + github_accounts)"
  local _alias _user
  if [[ "$HL_GITHUB_ACCOUNTS" == *:* ]]; then
    _alias="${HL_GITHUB_ACCOUNTS%%:*}"; _user="${HL_GITHUB_ACCOUNTS##*:}"
  else
    _alias="personal"; _user="$HL_GITHUB_ACCOUNTS"
  fi
  {
    printf 'user_login: "%s"\n' "$HL_USER_LOGIN"
    printf 'user_name: "%s"\n' "$HL_USER_NAME"
    printf 'user_email: "%s"\n' "$HL_USER_EMAIL"
    printf '# GitHub CLI accounts — to add more later: scripts/gh-account-setup.bash --add=alias:username\n'
    printf 'github_accounts:\n'
    printf '  %s: "%s"\n' "$_alias" "$_user"
  } > "$yml"
  success "Headless: localhost.yml written (fresh)"
}

# hl_reconcile_vault <localhost_yml> <vault_pass_file> — headless vault reconciliation
# (D6): the password must be PROVIDED (RUN_BASH_VAULT_PASSWORD[_FILE], resolved in
# preflight into HL_VAULT_PASSWORD), verified against any encrypted values, and NEVER
# auto-generated over a !vault (that would silently orphan the encrypted data). A vault
# password is genuinely required because the github_ssh_passphrase is vault-encrypted
# into localhost.yml right after this. Every failure aborts LOUD.
hl_reconcile_vault() {
  local yml="$1" vpf="$2" has_vault=false
  if grep -qF '!vault' "$yml"; then has_vault=true; fi

  if [[ -n "$HL_VAULT_PASSWORD" ]]; then
    printf '%s' "$HL_VAULT_PASSWORD" > "$vpf"
    chmod 600 "$vpf"
    if [[ "$has_vault" == "true" ]]; then
      if ! verify_vault_password "$HL_VAULT_PASSWORD" "$yml"; then
        hl_abort "vault reconcile" \
          "RUN_BASH_VAULT_PASSWORD does not decrypt the vault-encrypted values in localhost.yml" \
          "check it matches the vault this config was encrypted with — headless never auto-generates over encrypted values (D6)"
      fi
      success "Headless: vault password verified against encrypted config"
    else
      success "Headless: vault password set"
    fi
    return 0
  fi

  # No password provided.
  if [[ "$has_vault" == "true" ]]; then
    if [[ -f "$vpf" && -s "$vpf" ]] && verify_vault_password "$(cat "$vpf")" "$yml"; then
      success "Headless: existing vault-pass.secret verified against encrypted config"
    else
      hl_abort "vault reconcile" \
        "localhost.yml has vault-encrypted values but no working vault password" \
        "provide RUN_BASH_VAULT_PASSWORD_FILE matching the vault this config was encrypted with"
    fi
  elif [[ -f "$vpf" && -s "$vpf" ]]; then
    success "Headless: using existing vault-pass.secret"
  else
    hl_abort "vault reconcile" \
      "a vault password is required (github_ssh_passphrase is vault-encrypted) but RUN_BASH_VAULT_PASSWORD[_FILE] was not provided and no vault-pass.secret exists" \
      "set RUN_BASH_VAULT_PASSWORD_FILE to a 0600 file holding the vault password"
  fi
}

# hl_run_optional_playbooks — headless replacement for the interactive optional-playbook
# menu. Runs exactly the plays named in RUN_BASH_OPTIONAL_PLAYBOOKS (space/comma list of
# play-foo.yml | foo | play-foo), in order; 'none'/unset skips the whole section. The
# reserved token 'server-recommended' expands to the curated, generic dev/server bundle in
# playbooks/imports/optional/server-recommended.bundle (composes with explicit tokens).
# Any unknown name or failing play aborts LOUD (a server run must not silently under-provision).
hl_run_optional_playbooks() {
  local spec="${RUN_BASH_OPTIONAL_PLAYBOOKS:-none}"
  if [[ -z "$spec" || "$spec" == "none" ]]; then
    info "Headless: RUN_BASH_OPTIONAL_PLAYBOOKS=none — skipping optional playbooks"
    return 0
  fi
  if [[ ! -d ~/Projects/fedora-desktop ]]; then
    hl_abort "optional playbooks" "$HOME/Projects/fedora-desktop not found — the repo was not cloned" \
      "run the full headless install (it clones the repo) before requesting optional playbooks"
  fi
  cd ~/Projects/fedora-desktop || hl_abort "optional playbooks" "cannot cd into ~/Projects/fedora-desktop" "check the clone succeeded"
  local -a _all_optional
  mapfile -t _all_optional < <(find playbooks/imports/optional -name "*.yml" -type f | sort)
  local -a _reqs
  IFS=' ,' read -ra _reqs <<< "$spec"

  # Expand the server-recommended bundle keyword (Plan 00065 Phase 5) into its
  # manifest-listed plays, then de-dup so a play named by both the bundle and an
  # explicit token only runs once. Expansion happens BEFORE the per-token resolution
  # loop below, so composing with explicit tokens ("server-recommended play-ddev.yml")
  # and the unknown-token abort are both inherited for free — nothing below changes.
  local _bundle_file="playbooks/imports/optional/server-recommended.bundle"
  local -a _expanded=()
  local req _line
  for req in "${_reqs[@]}"; do
    [[ -z "$req" ]] && continue
    if [[ "$req" == "server-recommended" ]]; then
      if [[ ! -f "$_bundle_file" ]]; then
        hl_abort "optional playbooks" \
          "RUN_BASH_OPTIONAL_PLAYBOOKS requested 'server-recommended' but ${_bundle_file} is missing" \
          "the ~/Projects/fedora-desktop checkout may be stale/corrupt — re-clone, or drop 'server-recommended' from the list"
      fi
      while IFS= read -r _line; do
        [[ -z "$_line" || "$_line" == \#* ]] && continue
        _expanded+=("$_line")
      done < "$_bundle_file"
    else
      _expanded+=("$req")
    fi
  done

  # De-dup, preserving first-seen order.
  local -a _reqs_deduped=()
  local -A _seen=()
  for req in "${_expanded[@]}"; do
    [[ -n "${_seen[$req]:-}" ]] && continue
    _seen[$req]=1
    _reqs_deduped+=("$req")
  done
  _reqs=("${_reqs_deduped[@]}")

  local pb base found name
  for req in "${_reqs[@]}"; do
    [[ -z "$req" ]] && continue
    found=""
    for pb in "${_all_optional[@]}"; do
      base="$(basename "$pb")"
      if [[ "$base" == "$req" || "$base" == "$req.yml" || "$base" == "play-${req}.yml" ]]; then
        found="$pb"; break
      fi
    done
    if [[ -z "$found" ]]; then
      hl_abort "optional playbooks" "requested optional playbook '${req}' not found under playbooks/imports/optional/" \
        "use an exact name like play-docker.yml (or docker), or set RUN_BASH_OPTIONAL_PLAYBOOKS=none"
    fi
    name="$(basename "$found" .yml)"
    info "Headless: running optional playbook ${name}"
    if ! "$found"; then
      hl_abort "optional playbook ${name}" "${found} FAILED" \
        "scroll up for the Ansible output; fix it, drop it from RUN_BASH_OPTIONAL_PLAYBOOKS, or set =none"
    fi
    success "Headless: optional playbook ${name} complete"
  done
}

# ── main() — the entire executable body ──────────────────────────────────────
# B5: wrapping everything in main() (and only calling it on the last line via
# `( main "$@" )`) guarantees the WHOLE file is parsed before any command runs.
# This matters when run.bash is executed as a real file (kickstart / dev): the
# `git pull` steps below can rewrite run.bash on disk, and an un-wrapped script
# is still being streamed byte-by-byte by bash, so a rewrite mid-run corrupts the
# remaining offsets. With main(), bash has already read+parsed the whole file, so
# a pull cannot affect the in-flight run. The call is wrapped in its own subshell
# so set -e / IFS / trap / exit stay contained (H4 sourced-shell safety).
main() {
  set -e
  set -u
  set -o pipefail
  IFS=$'\n\t'

  # Safety net: always clean up sensitive temp files on exit.
  # HL_SECRET_FILES holds any 0600 secret files a headless run must shred on ANY
  # exit path (V3.4/V3.11). Initialised empty BEFORE the trap so the trap is
  # set -u-safe even when no headless secret files exist (empty-GitHub path / an
  # early abort before the files are learned) — a "${arr[@]:-}" expansion of an
  # empty array is a harmless no-op for rm -f, never an unbound-variable error.
  HL_SECRET_FILES=()
  HL_SSH_AGENT_PID=""   # set by hl_ssh_agent_start; kept empty so hl_cleanup is set -u-safe
  trap hl_cleanup EXIT

# Flags
OPTIONAL_ONLY=false
# Headless / unattended mode (Plan 00063) — provision a Fedora Server or Cloud box
# with no interactive prompts, driven by RUN_BASH_* env vars. Tri-state:
#   HEADLESS=""     -> not yet decided (auto-detect below)
#   HEADLESS=true   -> forced on  (--headless, or RUN_BASH_HEADLESS=1)
#   HEADLESS=false  -> forced off (--interactive)
# See ./run.bash --help-run-headless for the full env contract.
HEADLESS=""
for _arg in "$@"; do
  case "$_arg" in
    --help|-h)
      cat <<'USAGE'
Usage: ./run.bash [OPTIONS]

Fedora Desktop / Server / Cloud Configuration Installer

Options:
  --optional-only      Skip core setup, jump straight to optional playbook menu
  --headless           Force unattended mode (no prompts); config from RUN_BASH_* env
  --interactive        Force interactive mode even with no TTY / RUN_BASH_* set
  -h, --help           Show this help message
      --help-run-headless  Deep-dive: unattended/IaC provisioning (server & cloud)

Interactive first run (desktop):
  ./run.bash                Full install (system deps, SSH, GitHub, Ansible,
                            main playbook, then optional playbooks menu)

Subsequent runs:
  ./run.bash --optional-only  Re-run only the optional playbooks menu
                              (useful for adding components after initial setup)

Headless (server / cloud) — provision unattended from RUN_BASH_* env vars:
  RUN_BASH_HEADLESS=1 RUN_BASH_USER_EMAIL=... RUN_BASH_GITHUB_ACCOUNTS=... \
    ./run.bash          # full env contract: ./run.bash --help-run-headless

Desktop vs. server/cloud is auto-detected by the Ansible layer
(systemctl get-default -> graphical.target = desktop, else server); Fedora Cloud
resolves to the server subset (no GNOME). Override with
RUN_BASH_PROVISIONING_PROFILE=desktop|server.

Requirements:
  - Fedora Linux (version must match the branch)
  - Network connectivity (GitHub, DNF repos)
  - Must NOT be run as root (uses sudo internally; headless: run as the non-root
    target user with NOPASSWD sudo)
USAGE
      exit 0
      ;;
    --help-run-headless)
      cat <<'USAGE'
run.bash — HEADLESS / UNATTENDED provisioning (server & cloud, IaC)
===================================================================

Provision a headless Fedora Server or Cloud box end-to-end with ZERO interactive
prompts, driven entirely by RUN_BASH_* environment variables. run.bash runs on the
box it provisions (connection: local) and self-updates the repo, so a headless run
always provisions the branch-latest source. The Ansible layer auto-detects the
server profile and skips all GNOME/desktop plays (Plan 00061); Fedora Cloud is
treated as a server (no new scope needed).

TRIGGER
  Headless is ON when any of:
    * --headless flag, or RUN_BASH_HEADLESS=1
    * stdin is not a TTY AND >=1 RUN_BASH_* config var is set
  Force OFF with --interactive. (Piped-stdin smoke tests: pass --interactive or
  set no RUN_BASH_* to avoid tripping headless.)

PRECONDITIONS (fail fast if unmet — never hangs)
  * NOPASSWD:ALL sudo (the default cloud user has it; a password-sudo Server does
    not — configure NOPASSWD or run interactively).
  * Run as the NON-root target user (cloud-init runcmd is root; drop to the user).
  * GitHub is mandatory in headless v1: set RUN_BASH_GITHUB_ACCOUNTS to a single
    account AND provide RUN_BASH_GITHUB_TOKEN_FILE AND
    RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE (the login SSH key stays passphrase-
    protected). Unset => fail fast. (The 'none' / GitHub-empty path is a planned
    follow-up, not yet supported.)

NON-SECRET CONFIG (plain RUN_BASH_* env)
  RUN_BASH_HEADLESS=1              Force headless.
  RUN_BASH_USER_EMAIL=...          Git email.                     (REQUIRED)
  RUN_BASH_GITHUB_ACCOUNTS=...     Single gh username (v1). ('none' is a planned
                                   follow-up, not yet supported.)      (REQUIRED)
  RUN_BASH_USER_LOGIN=...          System login.        (default: current user)
  RUN_BASH_USER_NAME=...           Full name.                 (default: = login)
  RUN_BASH_HOSTNAME=...            Set hostname when box is still 'fedora'.
  RUN_BASH_CONFIG_SOURCE=...       Config-repo host file to import, or 'none'.
  RUN_BASH_PROVISIONING_PROFILE=   Force desktop|server (default: auto-detect).
  RUN_BASH_OPTIONAL_PLAYBOOKS=...  Space/comma list of optional plays, or 'none'.
                                   'server-recommended' expands to a curated, generic
                                   dev/server bundle (see
                                   playbooks/imports/optional/server-recommended.bundle);
                                   combine with explicit plays, e.g.
                                   "server-recommended play-ddev.yml".
  RUN_BASH_RESTORE_PROJECTS=0|1    Restore projects from config manifest.
  RUN_BASH_REBOOT=0|1              Reboot at end.

SECRETS — prefer 0600 FILE POINTERS (recommended), literal env supported but risky
  RUN_BASH_VAULT_PASSWORD_FILE=/path         Ansible vault password (file).
  RUN_BASH_GITHUB_TOKEN_FILE=/path           Scoped GitHub PAT (file); REQUIRED
                                             in headless v1 (GitHub is mandatory).
  RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE=/path  SSH key passphrase (file); REQUIRED
                                             in v1 (the login key stays passphrase-
                                             protected, loaded via ssh-agent).
  Literal equivalents (RUN_BASH_VAULT_PASSWORD, _GITHUB_TOKEN,
  _GITHUB_SSH_PASSPHRASE) are accepted but:
    * REFUSED on a detected cloud box (cloud-init user-data persists them in the
      metadata service, world-readable indefinitely) -> use the *_FILE form.
    * warned loudly otherwise; setting BOTH a literal and its *_FILE is an error.
  The *_FILE form is best: the secret bytes never enter the environment,
  process listings, or cloud-init user-data.
  GitHub token scope: the full vars/github-required-scopes.yml set + admin:public_key.

GITHUB (headless v1 — always CONFIGURED)
  RUN_BASH_GITHUB_ACCOUNTS=<user> -> full GitHub setup via the scoped token file
     (single account in v1). This is the ONLY supported headless path today.
  The GitHub-empty path (RUN_BASH_GITHUB_ACCOUNTS=none -> HTTPS-only public clone,
     no token/SSH key) is a planned follow-up: it is currently BLOCKED by two
     latent server-profile playbook bugs, so v1 fails fast on 'none' rather than
     provision a box that would break at the playbook stage.

CANONICAL INVOCATION (run as the non-root user)
  The single supported headless provision — branch-latest repo, full setup:
       RUN_BASH_HEADLESS=1 \
       RUN_BASH_USER_EMAIL=name@example.com \
       RUN_BASH_GITHUB_ACCOUNTS=<gh-username> \
       RUN_BASH_GITHUB_TOKEN_FILE=/run/secrets/gh-token \
       RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE=/run/secrets/ssh-pass \
       RUN_BASH_VAULT_PASSWORD_FILE=/run/secrets/vault-pass \
         ./run.bash

CLOUD-INIT (Fedora Cloud) — fetch secrets OUT-OF-BAND, never in write_files
  write_files embeds content INSIDE user-data (served by the metadata service
  forever) — so NEVER put secret bytes there. Fetch them out-of-band inside
  runcmd, e.g.:
     runcmd:
       - [ sh, -c, 'aws secretsmanager get-secret-value --secret-id vault
             --query SecretString --output text > /run/secrets/vault-pass' ]
       - [ sh, -c, 'aws secretsmanager get-secret-value --secret-id gh-token
             --query SecretString --output text > /run/secrets/gh-token' ]
       - [ sh, -c, 'sudo -u <user> -i env RUN_BASH_HEADLESS=1
             RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=<gh-username>
             RUN_BASH_GITHUB_TOKEN_FILE=/run/secrets/gh-token
             RUN_BASH_VAULT_PASSWORD_FILE=/run/secrets/vault-pass
             /home/<user>/run.bash' ]
  Replace <user> with the box's non-root user (Fedora Cloud's default distro user).
  /run/secrets is tmpfs (RAM-backed, wiped on reboot). Pin the run.bash source to
  a commit SHA (not HEAD) when fetching it, and inspect before running.

FAIL-FAST GUARANTEE
  Any missing required value or unmet precondition aborts with a clear message
  naming the exact fix — a headless run never hangs waiting on a prompt, and a
  failed main playbook exits non-zero (never reports success).
USAGE
      exit 0
      ;;
    --headless)
      HEADLESS=true
      ;;
    --interactive)
      HEADLESS=false
      ;;
    --optional-only)
      OPTIONAL_ONLY=true
      ;;
    *)
      echo "Unknown option: $_arg" >&2
      echo "Run './run.bash --help' for usage" >&2
      exit 1
      ;;
  esac
done

# Resolve the headless auto-detect when neither --headless nor --interactive forced
# it. RUN_BASH_HEADLESS wins first; otherwise headless requires BOTH no-TTY-on-stdin
# AND at least one RUN_BASH_* config var (so an accidental desktop pipe with no
# RUN_BASH_* never silently goes headless).
if [[ -z "$HEADLESS" ]]; then
  case "${RUN_BASH_HEADLESS:-}" in
    1|true|yes|on)
      HEADLESS=true
      ;;
    0|false|no|off)
      HEADLESS=false
      ;;
    *)
      # Any RUN_BASH_* config var set, excluding the script's own VERSION constant?
      _rb_has_cfg=false
      while IFS= read -r _rb_v; do
        if [[ "$_rb_v" != "RUN_BASH_VERSION" ]]; then
          _rb_has_cfg=true
          break
        fi
      done < <(compgen -v | grep -E '^RUN_BASH_')
      if [[ ! -t 0 && "$_rb_has_cfg" == "true" ]]; then
        HEADLESS=true
      else
        HEADLESS=false
      fi
      unset _rb_has_cfg _rb_v
      ;;
  esac
fi

# Headless: validate + resolve all RUN_BASH_* input up front (fail fast, never hang)
# BEFORE any provisioning action. On success the run then flows through the SAME body
# as the interactive path — every interactive point below has a headless branch that
# uses the resolved HL_*/RUN_BASH_* values, and the shared prompt helpers hard-fail
# LOUD (hl_abort) if a headless run ever reaches an un-neutralised prompt.
if [[ "$HEADLESS" == "true" ]]; then
  headless_preflight
  echo -e "\n${YELLOW}${ARROW} run.bash v${RUN_BASH_VERSION}: headless preflight OK — provisioning unattended.${NC}" >&2
fi

## Step counter
# STEP_TOTAL is derived by counting the title() calls in this very script, so it
# can never drift out of sync with the actual number of steps (the old hardcoded
# 13 lagged the real 17 and produced "14/13"). When the script is streamed
# (README install: `source <(curl ...)`), BASH_SOURCE is a consumed pipe that
# cannot be re-read, so we fall back to the known count.
STEP_CURRENT=0
STEP_TOTAL=17  # fallback for the streamed (curl) install path
_run_bash_self="${BASH_SOURCE[0]:-}"
if [[ -f "$_run_bash_self" && -r "$_run_bash_self" ]]; then
  if _run_bash_steps=$(grep -cE '^[[:space:]]*title[[:space:]]+"' "$_run_bash_self"); then
    if [[ "$_run_bash_steps" =~ ^[0-9]+$ ]] && (( _run_bash_steps > 0 )); then
      STEP_TOTAL="$_run_bash_steps"
    fi
  fi
  unset _run_bash_steps
fi
unset _run_bash_self

# M4: under --optional-only only ONE title() is reachable ("System Reboot") — the
# whole core-setup block (every other title) is skipped. Show [1/1], not [1/17].
if [[ "${OPTIONAL_ONLY:-}" == "true" ]]; then
  STEP_TOTAL=1
fi

## Assertions
if [[ "$(whoami)" == "root" ]];
then
  echo -e "\n${RED}${BOLD}${CROSS} ERROR${NC}"
  echo -e "${RED}Please do not run this as root${NC}\n"
  echo -e "Simply run as your normal user\n"
  exit 1
fi

# Header
# Defect 4: `clear` exits 1 ("TERM environment variable not set") in a
# non-interactive context, which aborts the run under set -e. Only clear when
# stdout is a real terminal — the [[ -t 1 ]] guard keeps fail-fast intact while
# skipping the clear when there is no tty.
[[ -t 1 ]] && clear
echo -e "${BLUE}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}${BOLD}║          FEDORA DESKTOP CONFIGURATION INSTALLER             ║${NC}"
echo -e "${BLUE}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}"
echo -e "  ${CYAN}run.bash v${RUN_BASH_VERSION}${NC}\n"

# Detect actual Fedora version (version check happens after repo clone)
fedora_version=$(grep "^VERSION_ID=" /etc/os-release | cut -d= -f2)
echo -e "${CYAN}${INFO} Running on Fedora ${fedora_version}${NC}"

## Functions

title(){
  STEP_CURRENT=$(( STEP_CURRENT + 1 ))
  echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${CYAN}${BOLD}[$STEP_CURRENT/$STEP_TOTAL]${NC} ${BOLD}$1${NC}"
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

completed(){
  echo -e "${GREEN}${CHECK} Completed successfully${NC}"
}

info(){
  echo -e "${CYAN}${INFO} $1${NC}"
}

success(){
  echo -e "${GREEN}${CHECK} $1${NC}"
}

warning(){
  echo -e "${YELLOW}${WARN} $1${NC}"
}

error(){
  echo -e "${RED}${CROSS} $1${NC}"
}

wait_for_network(){
  info "Checking network connectivity..."
  local attempts=0
  local max_attempts=30
  while ! curl --silent --show-error --max-time 5 --output /dev/null https://github.com; do
    attempts=$((attempts + 1))
    if [[ $attempts -ge $max_attempts ]]; then
      echo -e "${RED}${CROSS} ERROR: No network connectivity after $max_attempts attempts${NC}" >&2
      echo -e "${YELLOW}${INFO} Please check your network connection and re-run this script${NC}" >&2
      exit 1
    fi
    echo -e "${YELLOW}${WARN} Network not ready (attempt $attempts/$max_attempts) — retrying in 2s...${NC}"
    sleep 2
  done
  success "Network connectivity confirmed"
}

# Abort the script if the given repo has uncommitted changes. run.bash
# does `git pull` at two points and a dirty working tree causes the pull
# to fail with an unhelpful error. Fail fast with a clear remediation.
assert_clean_worktree(){
  local dir="$1"
  local dirty
  dirty="$(git -C "$dir" status --porcelain)"
  if [[ -n "$dirty" ]]; then
    error "Working tree at $dir has uncommitted changes"
    echo -e "${YELLOW}${ARROW} run.bash needs a clean working tree before pulling updates.${NC}"
    echo -e "${YELLOW}${ARROW} Inspect:${NC}   ${BOLD}cd $dir && git status${NC}"
    echo -e "${YELLOW}${ARROW} Resolve by committing:${NC}"
    echo -e "     ${BOLD}git add -p && git commit${NC}"
    echo -e "${YELLOW}${ARROW} Or by temporarily stashing (remember to restore afterwards):${NC}"
    echo -e "     ${BOLD}git stash push -m 'pre-run.bash' && ./run.bash && git stash pop${NC}"
    exit 1
  fi
}

# confirm <msg> [default] — y/n confirmation with a visible safe default.
#   default=y → prompt renders (Y/n), Enter accepts (returns 0).
#   default=n → prompt renders (y/N), Enter declines (returns 1).
#   default omitted → no Enter default; Enter re-prompts (back-compat for any
#   caller that wants an explicit keypress).
# Use default=y for benign continues and value confirmations; default=n for
# destructive actions (reboot, posting a PUBLIC issue, running untested code).
# Invalid keys re-prompt with what to press — confirm() never exits the run.
confirm(){
  local msg="$1"
  # Headless backstop (fail LOUD, never hang): a correctly-configured headless run
  # supplies every decision via RUN_BASH_*, so it must NEVER reach an interactive
  # yes/no prompt. If it does, a call site was not neutralised — abort loudly rather
  # than block forever waiting on a TTY that isn't there.
  [[ "${HEADLESS:-}" == "true" ]] && hl_abort "unattended yes/no prompt reached" \
    "headless hit a confirmation prompt: \"${msg}\"" \
    "this decision has no RUN_BASH_* input wired up (a run.bash bug) — report it, or run interactively"
  local default="${2:-}"
  local yn=""
  local hint
  case "$default" in
    y) hint="(Y/n)" ;;
    n) hint="(y/N)" ;;
    *) hint="(y/n)" ;;
  esac
  echo
  echo -e "${YELLOW}${ARROW}${NC} $msg ${BOLD}${hint}${NC}"
  while true; do
    read -rp "   Your choice: " yn
    # Enter with a configured default takes that default.
    if [[ -z "$yn" ]]; then
      case "$default" in
        y) echo -e "${GREEN}${CHECK} Confirmed${NC}\n"; return 0 ;;
        n) echo -e "${YELLOW}${INFO} Skipped${NC}\n"; return 1 ;;
        *) echo -e "   ${RED}${CROSS} Please press 'y' for yes or 'n' for no${NC}"; continue ;;
      esac
    fi
    case "${yn,,}" in
      y|yes) echo -e "${GREEN}${CHECK} Confirmed${NC}\n"; return 0 ;;
      n|no)  echo -e "${YELLOW}${INFO} Skipped${NC}\n"; return 1 ;;
      *)     echo -e "   ${RED}${CROSS} Invalid input '${yn}'. Enter 'y' for yes or 'n' for no ${BOLD}${hint}${NC}" ;;
    esac
  done
}

# Back up a config file with timestamp. No-op if file doesn't exist.
backup_config(){
  local config_file="$1"
  if [[ ! -f "$config_file" ]]; then
    return 0
  fi
  local backup_file
  backup_file="${config_file}.backup.$(date +%Y%m%d-%H%M%S)"
  cp "$config_file" "$backup_file"
  success "Config backed up to $(basename "$backup_file")"
}

# Selective config import: decode saved config, show top-level YAML keys,
# let user exclude some, write filtered result to output file.
# Sets _excluded_keys (comma-separated) for the caller to check.
selective_config_import(){
  local raw_b64="$1"
  local output_file="$2"
  local temp_config
  local temp_excluded
  temp_config=$(mktemp)
  temp_excluded=$(mktemp)
  printf '%s' "$raw_b64" | base64 -d > "$temp_config"

  python3 scripts/config_merge.py selective "$temp_config" "$output_file" "$temp_excluded"
  _excluded_keys=""
  if [[ -s "$temp_excluded" ]]; then
    _excluded_keys=$(cat "$temp_excluded")
  fi
  rm -f "$temp_config" "$temp_excluded"
}

# Merge remote config into local config interactively.
# Shows per-key diff: unchanged keys auto-keep, changed keys prompt L/R,
# new remote keys prompt A/S. Local-only keys always kept.
merge_config_import(){
  local raw_b64="$1"
  local local_file="$2"
  local temp_remote
  temp_remote=$(mktemp)
  printf '%s' "$raw_b64" | base64 -d > "$temp_remote"

  python3 scripts/config_merge.py merge "$local_file" "$temp_remote" "$local_file"
  rm -f "$temp_remote"
}

# Push local config to the per-host path in the config repo.
# Uses GitHub Contents API (create or update).
push_config_to_repo(){
  local config_file="$1"
  local repo="$2"
  local path="$3"
  local host_label="$4"

  local content_b64
  content_b64=$(base64 -w0 "$config_file")

  # Get existing file SHA if updating (not needed for first create)
  local existing_sha=""
  if existing_sha=$(gh api "repos/${repo}/contents/${path}" --jq '.sha' 2>/dev/null); then
    :  # SHA retrieved for update
  else
    existing_sha=""  # File doesn't exist yet — will create
  fi

  local -a api_args=(
    --method PUT
    --field "message=Update config from ${host_label}"
    --field "content=${content_b64}"
  )
  if [[ -n "$existing_sha" ]]; then
    api_args+=(--field "sha=${existing_sha}")
  fi

  gh api "repos/${repo}/contents/${path}" "${api_args[@]}" --silent
}

# Prompt for GitHub username(s) and write github_accounts YAML block to stdout.
# All user-facing prompts go to stderr so stdout is clean YAML for redirection.
prompt_github_accounts_yaml(){
  # Headless backstop (fail LOUD, never hang) — see confirm(). Headless builds the
  # github_accounts block from RUN_BASH_GITHUB_ACCOUNTS, so it must never prompt here.
  [[ "${HEADLESS:-}" == "true" ]] && hl_abort "unattended GitHub-accounts prompt reached" \
    "headless reached the interactive GitHub username prompt" \
    "the headless path must build github_accounts from RUN_BASH_GITHUB_ACCOUNTS — a missing wiring here is a run.bash bug"
  echo -e "\n${CYAN}${ARROW}${NC} Enter your GitHub username(s)" 1>&2
  echo -e "   These are the usernames you log into github.com with." 1>&2
  echo -e "   For multiple accounts, prefix each with a short alias and colon." 1>&2
  echo -e "" 1>&2
  echo -e "   ${BOLD}One account:${NC}      johndoe" 1>&2
  echo -e "   ${BOLD}Multiple accounts:${NC} personal:johndoe,work:johndoe-corp" 1>&2
  local github_accounts_raw _account_count _has_unaliased _alias _username _valid
  # Re-prompt until the entry validates — a malformed accounts string must NOT
  # kill the run (it used to `exit 1` on a missing alias or a duplicate alias).
  while true; do
    github_accounts_raw="$(promptForValue 'GitHub username(s), comma separated')"

    # M3: grep -c exits 1 (and prints 0) when there are no non-blank lines, e.g.
    # the user typed only commas/spaces. Under set -e + pipefail that non-zero
    # would kill the installer. Capture without aborting so the validation below
    # re-prompts instead.
    if ! _account_count=$(printf '%s' "$github_accounts_raw" | tr ',' '\n' | grep -c '[^[:space:]]'); then
      _account_count=0
    fi
    # Defect 2: comma/space-only input (e.g. ',' or ',,,') yields zero real
    # entries. Without this guard it would pass validation and write a bare
    # `github_accounts:` map with no entries, then gh-account-setup.bash would run
    # against an empty mapping. Re-prompt for at least one real account.
    if (( _account_count < 1 )); then
      error "Enter at least one account as alias:username (or a bare username)" 1>&2
      echo -e "   You entered: ${BOLD}${github_accounts_raw}${NC}" 1>&2
      continue
    fi
    _has_unaliased=false
    while IFS= read -r pair; do
      pair="${pair// /}"
      [[ -z "$pair" ]] && continue
      if [[ "$pair" != *":"* ]]; then
        _has_unaliased=true
      fi
    done < <(printf '%s\n' "$github_accounts_raw" | tr ',' '\n')

    if [[ "$_has_unaliased" == "true" ]] && [[ "$_account_count" -gt 1 ]]; then
      error "Multiple accounts require aliases. Use format: alias:username,alias:username" 1>&2
      echo -e "   You entered: ${BOLD}${github_accounts_raw}${NC}" 1>&2
      echo -e "   Example:     ${BOLD}personal:user1,work:user2${NC}" 1>&2
      continue
    fi

    _valid=true
    declare -A _seen_aliases=()
    while IFS= read -r pair; do
      pair="${pair// /}"
      if [[ "$pair" == *":"* ]]; then
        _alias="${pair%%:*}"
        _username="${pair##*:}"
      elif [[ -n "$pair" ]]; then
        _alias="personal"
        _username="$pair"
      else
        continue
      fi
      # Defect 1: an empty alias (e.g. ':johndoe' or 'a,:b') would make
      # ${_seen_aliases[$_alias]:-} a bash 5.2 'bad array subscript' FATAL error
      # that kills the shell even inside this if. Reject and re-prompt instead.
      if [[ -z "$_alias" ]]; then
        error "Empty alias in '${pair}' — use the format alias:username" 1>&2
        _valid=false
        break
      fi
      # Defect 2 (username side): an entry like 'alias:' has no username. Reject.
      if [[ -z "$_username" ]]; then
        error "Empty username in '${pair}' — use the format alias:username" 1>&2
        _valid=false
        break
      fi
      if [[ -n "${_seen_aliases[$_alias]:-}" ]]; then
        error "Duplicate alias '${_alias}' — each account needs a unique alias" 1>&2
        _valid=false
        break
      fi
      _seen_aliases[$_alias]=1
    done < <(printf '%s\n' "$github_accounts_raw" | tr ',' '\n')
    [[ "$_valid" == "true" ]] && break
  done

  # Output clean YAML to stdout
  printf '# GitHub CLI accounts — to add more later: scripts/gh-account-setup.bash --add=alias:username\n'
  printf 'github_accounts:\n'
  while IFS= read -r pair; do
    pair="${pair// /}"
    if [[ "$pair" == *":"* ]]; then
      printf '  %s: "%s"\n' "${pair%%:*}" "${pair##*:}"
    elif [[ -n "$pair" ]]; then
      printf '  personal: "%s"\n' "$pair"
    fi
  done < <(printf '%s\n' "$github_accounts_raw" | tr ',' '\n')
}

# Function to sanitize sensitive data from error logs
sanitize_error_log(){
  local log_content="$1"
  local sanitized="$log_content"
  
  # Remove potential API keys, tokens, and secrets
  sanitized=$(echo "$sanitized" | sed -E 's/(api[_-]?key|token|secret|password|passwd|pwd)[[:space:]]*[:=][[:space:]]*[^[:space:]]+/\1=***REDACTED***/gi')
  
  # Remove email addresses
  sanitized=$(echo "$sanitized" | sed -E 's/[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/***EMAIL***/g')
  
  # Remove IP addresses
  sanitized=$(echo "$sanitized" | sed -E 's/[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}/***IP***/g')
  
  # Remove SSH key fingerprints
  sanitized=$(echo "$sanitized" | sed -E 's/SHA256:[a-zA-Z0-9+/]+/SHA256:***FINGERPRINT***/g')
  
  # Remove home directory paths with actual username
  local current_user
  current_user="$(whoami)"
  # shellcheck disable=SC2001
  sanitized=$(echo "$sanitized" | sed "s|/home/$current_user|/home/***USER***|g")

  # Remove vault passwords and encrypted content
  # shellcheck disable=SC2016
  sanitized=$(echo "$sanitized" | sed -E 's/\$ANSIBLE_VAULT;[^[:space:]]+/\$ANSIBLE_VAULT;***ENCRYPTED***/g')
  
  echo "$sanitized"
}

# Function to check if Claude Code is available for enhanced sanitization
check_claude_code(){
  if command -v claude &> /dev/null; then
    return 0
  else
    return 1
  fi
}

# Function to create GitHub issue for failed playbook
create_github_issue(){
  local playbook_name="$1"
  local exit_code="$2"
  local error_log=""
  
  echo -e "\n${YELLOW}${BOLD}${BUG} Playbook Failure Detected${NC}"
  echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  
  # Check if we're in the git repo and have gh CLI
  if ! git rev-parse --git-dir > /dev/null 2>&1; then
    error "Not in a git repository. Cannot create issue."
    return 1
  fi
  
  if ! command -v gh &> /dev/null; then
    error "GitHub CLI not found. Cannot create issue."
    return 1
  fi
  
  # Get system information
  # Note: hostname is deliberately NOT collected — it is a "Never Commit" item
  # (CLAUDE/SecurityRules.md) and the issue body is posted to a PUBLIC tracker.
  local fedora_version branch commit kernel
  fedora_version=$(grep "^VERSION_ID=" /etc/os-release | cut -d= -f2)
  branch=$(git branch --show-current)
  commit=$(git rev-parse --short HEAD)
  kernel=$(uname -r)
  
  # Ask user to paste error output
  echo -e "\n${CYAN}${ARROW} Please copy and paste the relevant error output from above${NC}"
  echo -e "${YELLOW}${INFO} Paste the error, then press Ctrl+D when done:${NC}\n"
  error_log=$(cat)
  
  # Sanitize the error log
  info "Sanitizing error log for sensitive data..."
  local sanitized_log
  sanitized_log=$(sanitize_error_log "$error_log")
  
  # If Claude Code is available, use it for enhanced sanitization.
  #
  # FAIL CLOSED: the issue body is posted to a PUBLIC tracker. When `claude` is
  # installed it is the strong sanitiser and the regex pass alone is known to miss
  # things (git remote URLs, bare tokens). So if Claude is present but its
  # sanitisation produces no output (empty result or non-zero exit), we ABORT the
  # issue-creation flow rather than silently downgrading to regex-only output.
  # The regex-only path is taken ONLY when `claude` is genuinely not installed,
  # and even then the explicit confirmation below still gates the post.
  if check_claude_code; then
    info "Using Claude Code for enhanced sensitive data removal..."
    local temp_file
    temp_file=$(mktemp)
    echo "$sanitized_log" > "$temp_file"

    # Ask Claude to sanitize the log further. Capture exit status explicitly so a
    # failure is surfaced rather than hidden behind a fallback default.
    # M5: under set -e a failing `claude` aborts before `claude_status=$?` can
    # capture it, so the fail-closed check below never runs. Seed 0 and capture
    # the real status with `|| claude_status=$?` so the assignment always runs.
    local claude_sanitized claude_status=0
    claude_sanitized=$(claude "Please remove any potentially sensitive information from this error log including passwords, API keys, tokens, personal data, private URLs, or system-specific paths that shouldn't be shared publicly. Return ONLY the sanitized version of the log, preserving the error messages and structure but with sensitive data replaced with placeholders like ***REDACTED***:\n\n$(cat "$temp_file")") || claude_status=$?
    rm -f "$temp_file"

    if [[ "$claude_status" -ne 0 || -z "$claude_sanitized" ]]; then
      error "Claude Code sanitization failed (exit $claude_status, empty result)."
      error "Refusing to post to the PUBLIC tracker with regex-only sanitization."
      error "Sanitize the log manually and create the issue yourself, or re-run when Claude is working."
      return 1
    fi

    # Use Claude's sanitized version
    sanitized_log="$claude_sanitized"
    success "Claude Code: Additional sensitive data removed"

    # Optional: Show what Claude changed
    if [[ "${VERBOSE:-}" == "true" ]]; then
      info "Claude Code sanitization applied"
    fi
  fi
  
  # Prepare issue title and body
  local issue_title
  issue_title="[Automated] Playbook failure: $(basename "$playbook_name") on Fedora $fedora_version"
  
  local issue_body
  issue_body="## Playbook Failure Report

### Environment
- **OS**: Fedora $fedora_version
- **Branch**: $branch
- **Commit**: $commit
- **Kernel**: $kernel
- **Date**: $(date -u +"%Y-%m-%d %H:%M:%S UTC")

### Failed Playbook
\`\`\`
$playbook_name
\`\`\`

### Exit Code
$exit_code

### Error Output
<details>
<summary>Click to expand error log</summary>

\`\`\`
$sanitized_log
\`\`\`

</details>

### Steps to Reproduce
1. Fresh Fedora $fedora_version installation
2. Run \`./run.bash\`
3. Playbook fails at: $playbook_name

### Additional Context
_This issue was automatically generated. The error log has been sanitized to remove potentially sensitive information._

---
_Generated by fedora-desktop automated error reporting_"
  
  # Show the FULL preview to user before posting to the PUBLIC tracker.
  # The body is also written to a temp file so the user can review/edit it out of
  # band; nothing is truncated (a truncated preview could hide pasted secrets that
  # would then be posted unseen).
  local preview_file
  preview_file=$(mktemp)
  echo "$issue_body" > "$preview_file"

  echo -e "\n${CYAN}${BOLD}Issue Preview${NC}"
  echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BOLD}Title:${NC} $issue_title\n"
  echo -e "${BOLD}Body (full — review carefully before confirming):${NC}"
  echo "$issue_body"
  echo -e "\n${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${INFO} Full body also saved for review at: $preview_file\n"

  # Ask for confirmation
  if confirm "Do you want to create this GitHub issue? (posts to the PUBLIC tracker)" n; then
    info "Creating GitHub issue..."
    
    # Create the issue
    local issue_url
    if issue_url=$(${GH_REPO:-gh} issue create \
      --title "$issue_title" \
      --body "$issue_body" \
      --label "bug" \
      --label "automated" \
      2>&1); then
      success "Issue created successfully!"
      echo -e "${GREEN}${ARROW} View issue: $issue_url${NC}"
      return 0
    else
      error "Failed to create issue: $issue_url"
      return 1
    fi
  else
    info "Issue creation cancelled"
    return 1
  fi
}

# Function to run playbook with option to create issue on failure
run_playbook_with_issue_option(){
  local playbook="$1"
  local name="$2"
  local exit_code=0
  
  echo -e "\n${CYAN}${ARROW} Running: $name${NC}"
  
  # Run playbook normally with full colors.
  # B2: callers run this under errexit, so a failing "$playbook" would abort the
  # whole installer before exit_code=$? could capture it — bypassing the
  # issue-reporting UX below. Seed 0 and capture with `|| exit_code=$?` so the
  # failure is handled here instead of killing the run.
  # -k ignores any cached sudo timestamp, so this is true ONLY for genuine
  # passwordless (NOPASSWD) sudo. Without -k, an earlier `sudo` in this run
  # leaves a cached ticket that makes this pass, skipping --ask-become-pass —
  # but ansible become runs in its own tty (Fedora tty_tickets) and can't use
  # that cache, so it fails with "premature end of stream waiting for become
  # success". Detecting real NOPASSWD here routes password sudo to --ask-become-pass.
  if sudo -k -n true 2>/dev/null; then
    exit_code=0
    "$playbook" || exit_code=$?
  else
    exit_code=0
    "$playbook" --ask-become-pass || exit_code=$?
  fi
  
  if [[ $exit_code -eq 0 ]]; then
    success "Completed: $name"
    return 0
  else
    error "Failed: $name (exit code: $exit_code)"
    
    # Offer to create GitHub issue (posts to the PUBLIC tracker — default No)
    if confirm "Would you like to create a GitHub issue for this failure? (posts to the PUBLIC tracker)" n; then
      create_github_issue "$playbook" "$exit_code"
    fi

    return $exit_code
  fi
}

# promptForValue <item> [validate] [default] — validated free text with a
# confirm step. <validate> is one of "", "email", "min3". When <default> is
# given it is shown as [default]; pressing Enter at the entry takes it and
# skips the confirm (no point confirming a value you didn't type).
# At the "Is this correct?" step Enter or y/Y accepts; n/N re-opens the value
# for editing (the previous entry is pre-filled-by-display so the user edits,
# never re-types blind). Prompts go to stderr, the value to stdout via printf.
promptForValue(){
  local item v yn validate default prefill
  item="$1"
  # Headless backstop (fail LOUD, never hang) — see confirm().
  [[ "${HEADLESS:-}" == "true" ]] && hl_abort "unattended value prompt reached" \
    "headless needs a value for '${item}' but reached the interactive entry prompt" \
    "supply it via the matching RUN_BASH_* variable (see --help-run-headless); a missing wiring here is a run.bash bug"
  validate="${2:-}"
  default="${3:-}"
  prefill=""
  while true; do
    if [[ -n "$prefill" ]]; then
      echo -e "\n${CYAN}${ARROW}${NC} Re-enter your ${BOLD}$item${NC} (previous: ${BOLD}${prefill}${NC}):" 1>&2
    elif [[ -n "$default" ]]; then
      echo -e "\n${CYAN}${ARROW}${NC} Please enter your ${BOLD}$item${NC} ${BOLD}[${default}]${NC} (Enter to accept):" 1>&2
    else
      echo -e "\n${CYAN}${ARROW}${NC} Please enter your ${BOLD}$item${NC}:" 1>&2
    fi
    # M1: a failed read (EOF / closed stdin) must abort cleanly, not spin forever
    # on the empty-input branch. Mirror prompt_verified_vault_password.
    if ! read -rp "   " v; then
      echo -e "   ${RED}${CROSS} No input (stdin closed) — aborting.${NC}" 1>&2
      return 1
    fi
    # Enter on a fresh prompt with a default takes the default and is done.
    if [[ -z "$v" ]] && [[ -n "$default" ]] && [[ -z "$prefill" ]]; then
      echo -e "   ${GREEN}${CHECK} Using ${BOLD}${default}${NC}" 1>&2
      printf '%s' "$default"
      return 0
    fi
    # Basic validation: must not be empty
    if [[ -z "${v// /}" ]]; then
      echo -e "   ${RED}${CROSS} Cannot be empty. Type a value, then press Enter${NC}" 1>&2
      continue
    fi
    # Custom validation
    if [[ "$validate" == "email" ]] && [[ "$v" != *@*.* ]]; then
      echo -e "   ${RED}${CROSS} '${v}' is not a valid email. Enter one like name@example.com${NC}" 1>&2
      continue
    fi
    if [[ "$validate" == "min3" ]] && [[ "${#v}" -lt 3 ]]; then
      echo -e "   ${RED}${CROSS} '${v}' is too short. Enter at least 3 characters${NC}" 1>&2
      continue
    fi
    echo -e "\n   You entered: ${BOLD}$v${NC}" 1>&2
    # read -p prints its prompt VERBATIM (no escape interpretation), so render the
    # coloured prompt via echo -en to stderr first, then do a bare read.
    echo -en "   Is this correct? ${BOLD}(Y/n)${NC} (Enter to accept): " 1>&2
    read -r yn
    case "${yn,,}" in
      ""|y|yes)
        echo -e "   ${GREEN}${CHECK} Recorded ${BOLD}${item}${NC}" 1>&2
        break
        ;;
      n|no)
        # Re-open for editing — show the prior entry so the user corrects it.
        prefill="$v"
        echo -e "   ${YELLOW}${INFO} Okay, let's edit it.${NC}" 1>&2
        ;;
      *)
        echo -e "   ${RED}${CROSS} Invalid input '${yn}'. Press Enter or 'y' to accept, 'n' to edit${NC}" 1>&2
        ;;
    esac
  done
  printf '%s' "$v"
}

# --- DRY interactive-input helpers --------------------------------------------
# CONTRACT (shared by confirm, promptForValue, promptChoice, promptSecretConfirmed,
# promptDefault):
#   * Prompts and error text go to STDERR (1>&2). The accepted value goes to
#     STDOUT via printf '%s' (no trailing newline), so callers capture it with
#     v="$(promptX ...)". confirm() is the exception: it returns an exit status,
#     not a value.
#   * Every helper re-prompts forever on invalid input and NEVER exits the run on
#     a user typo — a typo is not an error. (Real errors elsewhere still fail
#     fast.)
#   * Where a default exists it is rendered in the prompt as [default] and a
#     "(Enter to accept)" hint is shown; pressing Enter takes the default.
#   * Confirm polarity is visible in the casing: (Y/n) = Enter accepts (Yes),
#     (y/N) = Enter declines (No). Use (y/N) only for destructive actions.
#   * Re-prompt messages state what was wrong AND what to enter.
#   * Prefer line reads (read -r) over read -n 1 so Enter is distinguishable and
#     piped-stdin smoke tests work.

# promptChoice <prompt> <max> [default] — echo a validated integer in 1..max.
# When <default> (an in-range integer) is given it is shown as [default] and
# Enter takes it.
promptChoice(){
  local prompt="$1" max="$2" default="${3:-}" choice
  # Headless backstop (fail LOUD, never hang) — see confirm().
  [[ "${HEADLESS:-}" == "true" ]] && hl_abort "unattended menu prompt reached" \
    "headless reached an interactive numbered-choice prompt: \"${prompt}\"" \
    "the headless path must pick this non-interactively from RUN_BASH_* — a missing wiring here is a run.bash bug"
  while true; do
    # M1: a failed read (EOF) takes the default if one exists, else aborts rather
    # than looping forever on the invalid-choice branch.
    if ! read -rp "$prompt" choice; then
      if [[ -n "$default" ]]; then
        printf '%s' "$default"
        return 0
      fi
      echo -e "   ${RED}${CROSS}${NC} No input (stdin closed) — aborting." 1>&2
      return 1
    fi
    if [[ -z "$choice" ]] && [[ -n "$default" ]]; then
      printf '%s' "$default"
      return 0
    fi
    if [[ "$choice" =~ ^[0-9]+$ ]] && (( choice >= 1 && choice <= max )); then
      printf '%s' "$choice"
      return 0
    fi
    if [[ -n "$default" ]]; then
      echo -e "   ${RED}${CROSS}${NC} Invalid choice '${choice}'. Enter a number from 1 to ${max}, or press Enter for [${default}]." 1>&2
    else
      echo -e "   ${RED}${CROSS}${NC} Invalid choice '${choice}'. Enter a number from 1 to ${max}." 1>&2
    fi
  done
}

# promptSecretConfirmed <label> — read a hidden secret twice, re-prompting until
# the two entries match; echo the agreed value. Empty is permitted (caller decides).
promptSecretConfirmed(){
  local label="$1" s1 s2
  # Headless backstop (fail LOUD, never hang) — see confirm().
  [[ "${HEADLESS:-}" == "true" ]] && hl_abort "unattended secret prompt reached" \
    "headless needs the secret '${label}' but reached the interactive hidden-entry prompt" \
    "supply it via the matching RUN_BASH_*_FILE pointer (see --help-run-headless); a missing wiring here is a run.bash bug"
  while true; do
    # M1: a failed read (EOF / closed stdin) must abort, not silently return an
    # empty secret as if the user had confirmed a blank value twice.
    if ! read -rsp "   ${label}: " s1; then
      echo 1>&2
      echo -e "   ${RED}${CROSS}${NC} No input (stdin closed) — aborting." 1>&2
      return 1
    fi
    echo 1>&2
    if ! read -rsp "   Confirm ${label}: " s2; then
      echo 1>&2
      echo -e "   ${RED}${CROSS}${NC} No input (stdin closed) — aborting." 1>&2
      return 1
    fi
    echo 1>&2
    if [[ "$s1" == "$s2" ]]; then
      printf '%s' "$s1"
      return 0
    fi
    echo -e "   ${RED}${CROSS}${NC} Do not match — try again." 1>&2
  done
}

# promptDefault <prompt> <default> [minlen] — free text with a default (Enter
# accepts it); re-prompts until the result is at least <minlen> characters.
promptDefault(){
  local prompt="$1" default="$2" minlen="${3:-0}" v
  # Headless backstop (fail LOUD, never hang) — see confirm().
  [[ "${HEADLESS:-}" == "true" ]] && hl_abort "unattended value prompt reached" \
    "headless reached an interactive prompt: \"${prompt}\"" \
    "the headless path must supply this from RUN_BASH_* (see --help-run-headless); a missing wiring here is a run.bash bug"
  while true; do
    # M1: a failed read (EOF) falls back to the default; if that still fails the
    # minlen check, abort rather than loop forever on closed stdin.
    if ! read -rp "$prompt" v; then
      v="$default"
      if (( ${#v} >= minlen )); then
        printf '%s' "$v"
        return 0
      fi
      echo -e "   ${RED}${CROSS}${NC} No input (stdin closed) — aborting." 1>&2
      return 1
    fi
    v="${v:-$default}"
    if (( ${#v} >= minlen )); then
      printf '%s' "$v"
      return 0
    fi
    echo -e "   ${RED}${CROSS}${NC} '${v}' is too short — enter at least ${minlen} character(s)." 1>&2
  done
}

# verify_vault_password <candidate> <localhost_yml> — return 0 if <candidate>
# decrypts the vault-encrypted values in <localhost_yml>. Reuses the same
# `ansible localhost ... --vault-id` probe used for the on-disk password. The
# candidate is fed via process substitution so it NEVER touches disk — a Ctrl+C
# during the (multi-second) probe cannot leave the password in a /tmp file.
verify_vault_password(){
  local candidate="$1" yml="$2"
  if ansible localhost -c local -e "@$yml" -m debug -a "msg=vault_ok" \
     --vault-id "localhost@"<(printf '%s' "$candidate") 2>/dev/null | grep -q "vault_ok"; then
    return 0
  fi
  return 1
}

# prompt_verified_vault_password <localhost_yml> — read a hidden vault password,
# verify it decrypts <localhost_yml>, and re-prompt on failure. The user may type
# 'abort' to give up; in that case the function prints loud remediation guidance
# and returns 1 (the caller then fails fast). On success the verified password is
# emitted to stdout via printf. Prompts/errors go to stderr.
prompt_verified_vault_password(){
  local yml="$1" vp
  # Headless backstop (fail LOUD, never hang) — see confirm(). Headless resolves the
  # vault password from RUN_BASH_VAULT_PASSWORD[_FILE] in preflight and reconciles it
  # non-interactively, so it must never reach this prompt.
  [[ "${HEADLESS:-}" == "true" ]] && hl_abort "unattended vault-password prompt reached" \
    "headless reached the interactive vault-password prompt for ${yml}" \
    "the supplied RUN_BASH_VAULT_PASSWORD[_FILE] did not decrypt localhost.yml — check the password matches the vault; there is no unattended fallback"
  while true; do
    # A failed read (EOF / closed stdin) must abort cleanly, not spin forever on
    # the empty-input branch.
    if ! read -rsp "   Vault password (or type 'abort' to give up): " vp; then
      vp="abort"
    fi
    echo 1>&2
    if [[ "$vp" == "abort" ]]; then
      echo -e "   ${RED}${CROSS} Aborting at your request.${NC}" 1>&2
      echo -e "   ${YELLOW}${ARROW}${NC} The vault password is the one you set when this machine (or its" 1>&2
      echo -e "      config) was first provisioned. Look for it in your password manager." 1>&2
      echo -e "   ${YELLOW}${ARROW}${NC} If it is truly lost you must reset the vault: re-encrypt the values" 1>&2
      echo -e "      in localhost.yml with a new password (ansible-vault), then re-run." 1>&2
      return 1
    fi
    if [[ -z "$vp" ]]; then
      echo -e "   ${RED}${CROSS}${NC} Empty — type your vault password, or 'abort' to give up." 1>&2
      continue
    fi
    if verify_vault_password "$vp" "$yml"; then
      echo -e "   ${GREEN}${CHECK} Vault password verified${NC}" 1>&2
      printf '%s' "$vp"
      return 0
    fi
    echo -e "   ${RED}${CROSS}${NC} That password did not decrypt localhost.yml. Check your password" 1>&2
    echo -e "      manager and try again, or type 'abort' to give up." 1>&2
  done
}

## Process

if [[ "$OPTIONAL_ONLY" != "true" ]]; then

echo -e "\n${MAGENTA}${BOLD}Installation Process${NC}"
echo -e "${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
wait_for_network

echo -e "\n${YELLOW}${INFO} You will be asked for your sudo password${NC}\n"
title "Installing System Dependencies"
# L1: list every package actually installed below (python3-pip was missing).
info "Installing: git, python3, python3-pip, python3-libdnf5, grubby, jq, openssl, pipx"
# H3: do NOT swallow dnf output. Under set -e a dnf failure here used to abort the
# run in total silence (output went to /dev/null). Let dnf print so a failure has
# a visible cause; set -e then stops the run with the error on screen.
sudo dnf -y install \
  git \
  python3 \
  python3-pip \
  python3-libdnf5 \
  grubby \
  jq \
  openssl \
  pipx
completed

title "Checking for Legacy Grub Configurations"
info "Checking for old cgroup settings"
if sudo grubby --info=ALL 2>/dev/null | grep -q "systemd.unified_cgroup_hierarchy"; then
  warning "Found legacy cgroup configuration, removing..."
  sudo grubby --update-kernel=ALL --remove-args="systemd.unified_cgroup_hierarchy=0"
  sudo grubby --update-kernel=ALL --remove-args="systemd.unified_cgroup_hierarchy=1"
  
  # Verify the removal worked
  if sudo grubby --info=ALL 2>/dev/null | grep -q "systemd.unified_cgroup_hierarchy"; then
    error "Failed to remove cgroup configuration - may need manual intervention"
    echo -e "${YELLOW}${INFO} To manually remove, run:${NC}"
    echo -e "   sudo grubby --update-kernel=ALL --remove-args='systemd.unified_cgroup_hierarchy=0'"
  else
    success "Legacy cgroup configuration removed successfully"
  fi
else
  success "No legacy cgroup configuration found"
fi

title "Setting up Ansible Environment"
# sudo dnf install pipx (above) or the kickstart %post can create ~/.local owned
# by root, which causes pipx to fail with PermissionError on its log directory.
# Fix ownership before running pipx.
if [[ -d ~/.local ]] && [[ "$(stat -c%U ~/.local)" != "$(whoami)" ]]; then
    info "Fixing ~/.local ownership (was created by root)"
    sudo chown -R "$(id -u):$(id -g)" ~/.local
fi
mkdir -p ~/.local/bin ~/.local/share ~/.local/state
info "Installing Ansible and dependencies via pipx"
# M6: anchor on '^ansible ' so a prior `ansible-lint` install does NOT make this
# match (which would skip the injects on a partial install). pipx list --short
# prints one "<pkg> <version>" line per top-level package.
if pipx list --short | grep -q '^ansible '; then
  success "Ansible already installed"
else
  pipx install --include-deps ansible
fi
# Inject deps unconditionally — pipx inject is idempotent, so this also repairs a
# partial prior install where ansible existed but a dependency was missing.
pipx inject ansible jmespath
pipx inject ansible passlib
pipx inject ansible ansible-lint
# Ensure ~/.local/bin exists, then force-create symlink
mkdir -p ~/.local/bin
ln -sf ~/.local/share/pipx/venvs/ansible/bin/ansible-lint ~/.local/bin/ansible-lint
completed

_ssh_key_password=""  # saved here, offered as default for github_ SSH keys later
title "Creating SSH Key Pair\n\nNOTE - you must set a password\n\nSuggest you use your login password"
if [[ "$HEADLESS" == "true" ]]; then
  # Headless: the passphrase came from RUN_BASH_GITHUB_SSH_PASSPHRASE[_FILE] (required
  # in preflight, non-empty). Generate the key non-interactively, then load it into an
  # ssh-agent so the SSH clone/pull below works without a TTY (D5/V3.12/V3.13). Every
  # step fails LOUD.
  mkdir -p ~/.ssh && chmod 700 ~/.ssh
  if [[ ! -f ~/.ssh/id ]]; then
    info "Headless: generating passphrase-protected login SSH key (~/.ssh/id)"
    if ! _kg_out="$(ssh-keygen -t ed25519 -f ~/.ssh/id -P "$HL_GITHUB_SSH_PASSPHRASE" 2>&1)"; then
      hl_abort "generate login SSH key" "ssh-keygen failed creating ~/.ssh/id" "ssh-keygen said: ${_kg_out}"
    fi
    unset _kg_out
    success "Login SSH key generated"
  else
    info "Headless: found existing ~/.ssh/id"
  fi
  # The resolved passphrase is this key's passphrase (offered later for github_ keys).
  _ssh_key_password="$HL_GITHUB_SSH_PASSPHRASE"
  hl_ssh_agent_start
  success "Login SSH key loaded into ssh-agent"
elif [[ ! -f ~/.ssh/id ]]; then
  # B4: on a fresh machine ~/.ssh does not exist yet — ssh-keygen would fail.
  # Create it with the correct 0700 perms before generating the key.
  mkdir -p ~/.ssh && chmod 700 ~/.ssh
  # M2: this key MUST be passphrase-protected (the title says so). Loop until the
  # user provides a non-empty passphrase. (The vault call site allows empty =
  # autogenerate; this one does not.)
  password=""
  while [[ -z "$password" ]]; do
    password=$(promptSecretConfirmed "Password")
    if [[ -z "$password" ]]; then
      error "An empty passphrase is not allowed here — this is a login SSH key."
      echo -e "${YELLOW}${ARROW} Enter a non-empty passphrase (your login password is a convenient choice).${NC}"
    fi
  done
  ssh-keygen -t ed25519 -f ~/.ssh/id -P "$password"
  _ssh_key_password="$password"
  # L3: the passphrase now lives in _ssh_key_password (offered later as a default);
  # drop the plaintext copy from this shell as soon as the key is created.
  unset password
else
  echo " - found existing key"
fi
completed

title "Set Custom Hostname"
if [[ "$(hostname)" == "fedora" ]]; then
  if [[ "$HEADLESS" == "true" ]]; then
    # Headless: set from RUN_BASH_HOSTNAME if given; otherwise leave the default
    # (optional — not every server needs a custom hostname). No prompt.
    if [[ -n "${RUN_BASH_HOSTNAME:-}" ]]; then
      if ! _hn_out="$(sudo hostnamectl set-hostname "$RUN_BASH_HOSTNAME" 2>&1)"; then
        hl_abort "set hostname" "could not set hostname to '${RUN_BASH_HOSTNAME}'" "hostnamectl said: ${_hn_out}"
      fi
      unset _hn_out
      success "Hostname set to ${RUN_BASH_HOSTNAME}"
    else
      info "Headless: RUN_BASH_HOSTNAME not set — leaving default hostname 'fedora'"
    fi
  else
    echo "found default hostname, please choose a new one"
    echo "(your machine hostname, eg my-laptop, my-fedora etc)"
    hostname=$(promptDefault "Hostname: " "" 1)
    sudo hostnamectl set-hostname "$hostname"
  fi
fi

title "Installing Github CLI"
sudo dnf -y install 'dnf-command(config-manager)'
# Check if gh-cli repo already exists before adding
if ! sudo dnf repolist | grep -q "gh-cli"; then
  sudo dnf config-manager addrepo --from-repofile=https://cli.github.com/packages/rpm/gh-cli.repo
else
  echo "GitHub CLI repository already configured"
fi
sudo dnf -y install gh
completed

title "GitHub Authentication Setup"
info "You will need to authenticate with your browser"
# Only add GH_HOST if not already present
if ! grep -q 'export GH_HOST="github.com"' ~/.bashrc; then
  echo 'export GH_HOST="github.com"' >> ~/.bashrc
fi

# Check the active gh token carries a required OAuth scope.
# Honours GitHub's scope hierarchy: admin:* implies write:* implies read:*,
# and `user` implies user:email/read:user/user:follow. A token granted
# admin:org therefore satisfies a read:org requirement.
# Anchored grep on '^X-Oauth-Scopes:' avoids matching the unrelated
# Access-Control-Expose-Headers line whose value lists the header name.
function ghCheckTokenPermission(){
  local permission="$1"
  local failSilent="${2:-false}"
  local gh_cmd="${GH_REPO:-gh}"
  local scopes_csv
  scopes_csv=",$($gh_cmd api -i user 2>/dev/null \
    | grep -i '^X-Oauth-Scopes:' \
    | sed 's/^[^:]*: //' \
    | tr -d ' \r' \
    | tr '\n' ','),"
  # Array, not space-separated string: this file sets IFS=$'\n\t' at the top
  # so unquoted $string would not word-split on spaces.
  local satisfiers=("$permission")
  case "$permission" in
    read:org)         satisfiers=("$permission" write:org admin:org) ;;
    write:org)        satisfiers=("$permission" admin:org) ;;
    read:public_key)  satisfiers=("$permission" write:public_key admin:public_key) ;;
    write:public_key) satisfiers=("$permission" admin:public_key) ;;
    read:repo_hook)   satisfiers=("$permission" write:repo_hook admin:repo_hook) ;;
    write:repo_hook)  satisfiers=("$permission" admin:repo_hook) ;;
    read:gpg_key)     satisfiers=("$permission" write:gpg_key admin:gpg_key) ;;
    write:gpg_key)    satisfiers=("$permission" admin:gpg_key) ;;
    read:user|user:email|user:follow) satisfiers=("$permission" user) ;;
  esac
  local s
  for s in "${satisfiers[@]}"; do
    if [[ "$scopes_csv" == *",${s},"* ]]; then
      echo " - found $permission permission"
      return 0
    fi
  done
  if [[ "$failSilent" == "true" ]]; then
    return 1
  fi
  echo " - missing $permission permission"
  echo "Please run this command ON THE MACHINE ITSELF, NOT REMOTELY"
  echo "    $gh_cmd auth refresh -h github.com -s '$permission'"
  return 1
}

# Headless: authenticate to GitHub non-interactively with the provided token BEFORE the
# interactive block below — which then sees an authenticated gh and no-ops into its
# "Already authenticated" branch. `gh auth login --with-token` reads the PAT from STDIN
# (never argv/env); set git_protocol=ssh so the SSH clone uses the key just loaded. LOUD.
if [[ "$HEADLESS" == "true" ]]; then
  if _gh_status_out="$(gh auth status 2>&1)"; then
    info "Headless: already authenticated with GitHub"
  else
    info "Headless: authenticating to GitHub with the provided token"
    if ! _gh_login_out="$(printf '%s' "$HL_GITHUB_TOKEN" | gh auth login --with-token 2>&1)"; then
      hl_abort "GitHub token auth" \
        "gh auth login --with-token was rejected — check the PAT in RUN_BASH_GITHUB_TOKEN_FILE (needs scopes: vars/github-required-scopes.yml + admin:public_key)" \
        "gh said: ${_gh_login_out}"
    fi
    unset _gh_login_out
    success "GitHub authentication successful (token)"
  fi
  unset _gh_status_out
  if ! _gh_proto_out="$(gh config set -h github.com git_protocol ssh 2>&1)"; then
    hl_abort "set gh git protocol" "could not set gh git_protocol=ssh (needed for the SSH clone)" "gh said: ${_gh_proto_out}"
  fi
  unset _gh_proto_out
fi

if ! gh auth status > /dev/null 2>&1; then
  echo -e "\n${YELLOW}${BOLD}┌─────────────────────────────────────────────────┐${NC}"
  echo -e "${YELLOW}${BOLD}│                    IMPORTANT                    │${NC}"
  echo -e "${YELLOW}${BOLD}│   YOU MUST CHOOSE SSH WHEN ASKED FOR THE       │${NC}"
  echo -e "${YELLOW}${BOLD}│   PREFERRED PROTOCOL FOR GIT OPERATIONS        │${NC}"
  echo -e "${YELLOW}${BOLD}└─────────────────────────────────────────────────┘${NC}\n"

  # Never kill the run on a fumbled answer — re-ask until SSH is acknowledged.
  # Default Yes: pressing Enter proceeds (SSH is the only supported path here).
  while ! confirm "Confirm you will choose SSH for GitHub authentication when prompted" y; do
    echo -e "${YELLOW}${ARROW} SSH is required for this setup — let's confirm again.${NC}"
  done

  if ! gh auth login; then
    error "Failed to login to GitHub"
    echo -e "${YELLOW}${ARROW} Please try running 'gh auth login' manually${NC}"
    exit 1
  fi
  success "GitHub authentication successful"
else
  success "Already authenticated with GitHub"
fi
primary_gh_username="$(gh api user --jq '.login')"
success "Primary GitHub account: $primary_gh_username"
completed

# This repo lives in the LongTermSupport org. If a previous install set up
# multi-account wrappers (play-github-cli-multi.yml generates gh-<alias> bash
# functions), prefer gh-lts for operations that act on this repo — so we talk
# to GitHub as the LTS account even when a different account is the active
# default. Falls back to plain gh on fresh installs where the wrappers don't
# yet exist.
GH_REPO="gh"
_gh_aliases_file="$HOME/.bashrc-includes/gh-aliases.inc.bash"
if [[ -f "$_gh_aliases_file" ]]; then
  # shellcheck source=/dev/null
  source "$_gh_aliases_file"
  if declare -F gh-lts >/dev/null; then
    GH_REPO="gh-lts"
    info "Using gh-lts wrapper for LTS-org operations"
  fi
fi
export GH_REPO

title "Configuring GitHub SSH Access"
# Check if we have the required permission
if ! ghCheckTokenPermission "admin:public_key" > /dev/null 2>&1; then
  warning "Missing admin:public_key permission - requesting it now"
  $GH_REPO auth refresh -h github.com -s admin:public_key
fi

# H1: idempotency must compare KEY MATERIAL, not a fingerprint. `gh api user/keys`
# returns the raw key blob (the base64 body of the public key), never a SHA256
# fingerprint — so the old fingerprint comparison never matched and a re-run kept
# trying to re-upload the key, hitting a GitHub 422 and aborting. Extract the blob
# (field 2 of the .pub line) and look for it literally in the keys listing.
ssh_key_blob=$(awk '{print $2}' ~/.ssh/id.pub)
# Use gh api to check for SSH keys without triggering signing key scope warning
if ! $GH_REPO api user/keys 2>/dev/null | grep -qF "$ssh_key_blob"; then
  # Add SSH key for authentication only (not signing)
  if $GH_REPO ssh-key add ~/.ssh/id.pub --title="fedora-desktop setup $(date +%Y-%m-%d)" --type=authentication 2>&1; then
    success "SSH authentication key added to GitHub"
  else
    error "Failed to add SSH key to GitHub"
    echo -e "${YELLOW}${ARROW} Try manually adding your SSH key:${NC}"
    echo -e "   cat ~/.ssh/id.pub | $GH_REPO ssh-key add --title='fedora-desktop setup' --type=authentication"
    exit 1
  fi
else
  success "SSH key already configured on GitHub"
fi
completed

title "Updating SSH Known Hosts"
info "Configuring GitHub host keys"
# Remove existing GitHub entries silently (absent on first run is fine)
if ! ssh-keygen -R github.com >/dev/null 2>/dev/null; then
  info "No existing github.com entry in known_hosts to remove"
fi
# Add fresh GitHub host keys
# L2: -f makes curl exit non-zero on an HTTP error (e.g. 5xx) instead of piping an
# error page into jq; under set -e + pipefail a fetch failure then aborts loudly.
curl -fsSL https://api.github.com/meta | jq -r '.ssh_keys | .[]' | sed -e 's/^/github.com /' >> ~/.ssh/known_hosts
success "GitHub host keys updated"
completed

title "Setting up Project Directory and Repository"
mkdir -p ~/Projects
# Clone via SSH (not HTTPS) so push works without a PAT and so the user's
# already-configured SSH auth (set up above) is the single auth path. By
# this point we have: (a) the user's SSH key uploaded to GitHub, and (b)
# github.com's host keys in ~/.ssh/known_hosts — so `git@github.com:` is
# guaranteed to work non-interactively.
fedora_desktop_ssh_url="git@github.com:LongTermSupport/fedora-desktop.git"
if [[ ! -d ~/Projects/fedora-desktop ]]; then
  info "Cloning fedora-desktop repository via SSH"
  git clone "$fedora_desktop_ssh_url" ~/Projects/fedora-desktop
  success "Repository cloned"
else
  info "Pulling latest changes"
  assert_clean_worktree ~/Projects/fedora-desktop
  # Migrate any pre-existing HTTPS origin to SSH so subsequent pulls /
  # pushes use the user's SSH key, not a (possibly-absent) cached HTTPS
  # credential. Idempotent: only rewrites when the URL is not already SSH.
  current_origin=$(command git -C ~/Projects/fedora-desktop remote get-url origin)
  if [[ "$current_origin" != "$fedora_desktop_ssh_url" ]]; then
    info "Migrating origin remote from '$current_origin' to SSH"
    command git -C ~/Projects/fedora-desktop remote set-url origin "$fedora_desktop_ssh_url"
  fi
  # Use `command git` to bypass any git() bash wrapper function (e.g. from
  # gh-aliases.inc.bash). Wrappers that run subcommands and assign to vars
  # can propagate non-zero exits under `set -e` even when they're benign.
  command git -C ~/Projects/fedora-desktop pull
  success "Repository updated"
fi
cd ~/Projects/fedora-desktop || { error "Cannot cd into ~/Projects/fedora-desktop"; exit 1; }

# Fail fast: verify Fedora version matches this branch
version_file=~/Projects/fedora-desktop/vars/fedora-version.yml
if [[ ! -f "$version_file" ]]; then
  error "Cannot find $version_file — repository may be corrupt"
  exit 1
fi
expected_version=$(grep "fedora_version:" "$version_file" | cut -d: -f2 | tr -d ' ')
if [[ "$fedora_version" != "$expected_version" ]]; then
  error "Fedora version mismatch"
  echo -e "   Expected: Fedora ${BOLD}$expected_version${NC} (from branch)"
  echo -e "   Actual:   Fedora ${BOLD}$fedora_version${NC}"
  echo -e "\n${YELLOW}${ARROW} Check out the correct branch for your Fedora version${NC}"
  exit 1
fi
success "Fedora version verified: $fedora_version matches branch"
completed


title "Loading Personal Configuration"
localhost_yml=~/Projects/fedora-desktop/environment/localhost/host_vars/localhost.yml
config_repo="${primary_gh_username}/fedora-desktop-config"
config_hostname=$(hostname)
config_host_path="hosts/${config_hostname}.yml"

# Headless: replace the entire interactive config-discovery + selection menu below with
# a deterministic write (fresh from RUN_BASH_* identity + accounts, or a pull of
# RUN_BASH_CONFIG_SOURCE). The interactive block (guarded by `if [[ HEADLESS != true ]]`)
# is intentionally left at its original indentation — it is a large block and
# re-indenting it would bury the real change in whitespace noise.
if [[ "$HEADLESS" == "true" ]]; then
  hl_write_localhost_yml "$localhost_yml"
else

# Discover config repo and find best available config for this host.
# gh api returns non-zero when a resource doesn't exist — that's expected
# for probe-then-act checks, not an error to propagate.
has_config_repo=false
has_remote_config=false
raw_content=""
config_source_label=""

if gh api "repos/${config_repo}" --jq '.name' > /dev/null 2>/dev/null; then
  has_config_repo=true

  # PRIVACY GATE (FUP-22): localhost.yml carries PII + the Ansible vault. It must
  # never be read from or written to a PUBLIC repo. Confirm the repo is private
  # before any pull or push touches it. A non-private (or unreadable .private)
  # repo aborts the flow — there is no safe automatic downgrade here.
  # H2: the config repo (${primary_gh_username}/fedora-desktop-config) is owned by
  # the PRIMARY account, and every other read here uses plain `gh` (the primary).
  # Using $GH_REPO (gh-lts) would query as the LTS account, which cannot see the
  # primary's PRIVATE config repo, yielding a false "NOT private" abort. Use plain gh.
  config_repo_private=$(gh api "repos/${config_repo}" --jq '.private' 2>/dev/null)
  if [[ "$config_repo_private" != "true" ]]; then
    error "Config repo github.com/${config_repo} is NOT private (.private='${config_repo_private:-unknown}')."
    error "localhost.yml contains PII and your Ansible vault and must never be synced to a public repo."
    error "Make the repo private (gh repo edit ${config_repo} --visibility private), then re-run."
    exit 1
  fi

  # Try host-specific config first
  if raw_content=$(gh api "repos/${config_repo}/contents/${config_host_path}" --jq '.content' 2>/dev/null); then
    has_remote_config=true
    config_source_label="${config_hostname}"
    info "Config found for this host (${config_hostname})"
  else
    # No config for this host — list available hosts and legacy file
    info "No saved config for ${config_hostname}"
    declare -a _available_sources=()
    declare -a _available_labels=()

    # Collect host-specific configs
    _host_list=$(gh api "repos/${config_repo}/contents/hosts" --jq '.[].name' 2>/dev/null) || _host_list=""
    if [[ -n "$_host_list" ]]; then
      while IFS= read -r _hfile; do
        _hname="${_hfile%.yml}"
        _available_sources+=("hosts/${_hfile}")
        _available_labels+=("${_hname}")
      done <<< "$_host_list"
    fi

    # Check for legacy localhost.yml
    if gh api "repos/${config_repo}/contents/localhost.yml" --jq '.sha' > /dev/null 2>/dev/null; then
      _available_sources+=("localhost.yml")
      _available_labels+=("localhost.yml (legacy)")
    fi

    if [[ ${#_available_sources[@]} -gt 0 ]]; then
      echo -e "\n   Available configs in repo:"
      for _i in "${!_available_labels[@]}"; do
        echo -e "     $(( _i + 1 ))) ${_available_labels[$_i]}"
      done
      _skip_opt=$(( ${#_available_sources[@]} + 1 ))
      echo -e "     ${_skip_opt}) Skip — none of these (configure manually later)"
      # promptChoice never exits on a typo; default = the Skip option so Enter skips.
      _src_choice=$(promptChoice "   Choose a config to use (number) [${_skip_opt}=skip] (Enter to skip): " "$_skip_opt" "$_skip_opt")
      if (( _src_choice >= 1 && _src_choice <= ${#_available_sources[@]} )); then
        _src_idx=$(( _src_choice - 1 ))
        _chosen_path="${_available_sources[$_src_idx]}"
        if raw_content=$(gh api "repos/${config_repo}/contents/${_chosen_path}" --jq '.content' 2>/dev/null); then
          has_remote_config=true
          config_source_label="${_available_labels[$_src_idx]}"
          info "Using config from ${config_source_label}"
        fi
      else
        info "Skipping saved config — you can configure manually below"
      fi
    fi
  fi
else
  info "No config repo found at github.com/${config_repo}"
fi

# Present configuration source choice
echo -e "\n${CYAN}${ARROW}${NC} How would you like to configure this system?"
_option=1
if [[ "$has_remote_config" == "true" ]]; then
  echo -e "   ${_option}) Pull full saved configuration (${config_source_label})"
  _opt_pull=$_option
  (( _option++ ))
  echo -e "   ${_option}) Selective import (choose what to keep) (${config_source_label})"
  _opt_selective=$_option
  (( _option++ ))
fi
if [[ "$has_remote_config" == "true" ]] && [[ -f "$localhost_yml" ]] && grep -qE '(!vault|github_accounts)' "$localhost_yml"; then
  echo -e "   ${_option}) Merge remote config into local (diff/merge per key)"
  _opt_merge=$_option
  (( _option++ ))
fi
if [[ -f "$localhost_yml" ]] && grep -qE '(!vault|github_accounts)' "$localhost_yml"; then
  echo -e "   ${_option}) Keep existing local configuration"
  _opt_keep=$_option
  (( _option++ ))
fi
if [[ "$has_config_repo" == "true" ]] && [[ -f "$localhost_yml" ]] && grep -qE '(!vault|github_accounts)' "$localhost_yml"; then
  echo -e "   ${_option}) Save local config to repo (as ${config_hostname})"
  _opt_push=$_option
  (( _option++ ))
fi
echo -e "   ${_option}) Configure fresh (enter details manually)"
_opt_fresh=$_option

# Recommended default for Enter: pull the saved config when one exists, else
# keep an existing local config, else configure fresh.
if [[ "$has_remote_config" == "true" ]]; then
  _config_default="${_opt_pull}"
elif [[ -n "${_opt_keep:-}" ]]; then
  _config_default="${_opt_keep}"
else
  _config_default="${_opt_fresh}"
fi

# A typo must NOT kill the run — promptChoice re-prompts until a valid option is
# entered. Every printed option got a sequential number 1.._opt_fresh, so an
# in-range integer always matches exactly one branch below. Enter takes the
# recommended default shown in brackets.
_config_choice=$(promptChoice "   Choice [1-${_opt_fresh}] (Enter for [${_config_default}]): " "$_opt_fresh" "$_config_default")

if [[ "$has_remote_config" == "true" ]] && [[ "${_config_choice}" == "${_opt_pull}" ]]; then
  backup_config "$localhost_yml"
  printf '%s' "$raw_content" | base64 -d > "$localhost_yml"
  success "Configuration pulled (${config_source_label})"

elif [[ "$has_remote_config" == "true" ]] && [[ "${_config_choice}" == "${_opt_selective:-}" ]]; then
  backup_config "$localhost_yml"
  selective_config_import "$raw_content" "$localhost_yml"
  success "Selective import (${config_source_label})"

  # Prompt for essential keys that were excluded
  if ! grep -q '^user_login:' "$localhost_yml"; then
    info "user_login was excluded — entering identity details"
    echo ""
    user_login=$(promptDefault "   User login [$(whoami)] (Enter to accept): " "$(whoami)" 3)
    user_name=$(promptDefault "   Full name [${user_login}] (Enter to accept): " "$user_login" 1)
    user_email="$(promptForValue 'email address' email)"
    {
      printf 'user_login: "%s"\n' "$user_login"
      printf 'user_name: "%s"\n' "$user_name"
      printf 'user_email: "%s"\n' "$user_email"
    } >> "$localhost_yml"
    success "Identity added to configuration"
  fi
  if ! grep -q '^github_accounts:' "$localhost_yml"; then
    info "github_accounts was excluded — entering GitHub accounts"
    prompt_github_accounts_yaml >> "$localhost_yml"
    success "GitHub accounts added to configuration"
  fi

elif [[ -n "${_opt_merge:-}" ]] && [[ "${_config_choice}" == "${_opt_merge}" ]]; then
  backup_config "$localhost_yml"
  merge_config_import "$raw_content" "$localhost_yml"
  success "Merge complete"

elif [[ -n "${_opt_keep:-}" ]] && [[ "${_config_choice}" == "${_opt_keep}" ]]; then
  success "Keeping existing localhost.yml"

elif [[ -n "${_opt_push:-}" ]] && [[ "${_config_choice}" == "${_opt_push}" ]]; then
  push_config_to_repo "$localhost_yml" "$config_repo" "$config_host_path" "$config_hostname"
  success "Configuration saved to github.com/${config_repo} (hosts/${config_hostname}.yml)"

elif [[ "${_config_choice}" == "${_opt_fresh}" ]]; then
  echo ""
  user_login=$(promptDefault "   User login [$(whoami)] (Enter to accept): " "$(whoami)" 3)
  user_name=$(promptDefault "   Full name [${user_login}] (Enter to accept): " "$user_login" 1)
  user_email="$(promptForValue 'email address' email)"

  {
    printf 'user_login: "%s"\n' "$user_login"
    printf 'user_name: "%s"\n' "$user_name"
    printf 'user_email: "%s"\n' "$user_email"
    prompt_github_accounts_yaml
  } > "$localhost_yml"

  success "Configuration written"
else
  error "Invalid choice: ${_config_choice}"
  exit 1
fi

fi  # end: interactive config import (headless wrote localhost.yml via hl_write_localhost_yml above)
completed

title "Ansible Vault Configuration"
vault_pass_file=~/Projects/fedora-desktop/vault-pass.secret
if [[ "$HEADLESS" == "true" ]]; then
  hl_reconcile_vault "$localhost_yml" "$vault_pass_file"
elif grep -qF '!vault' "$localhost_yml" 2>/dev/null; then
  # localhost.yml has encrypted values — need the matching vault password
  if [[ -f "$vault_pass_file" ]] && [[ -s "$vault_pass_file" ]]; then
    # Test existing vault password against encrypted values
    if ansible localhost -c local -e "@$localhost_yml" -m debug -a "msg=vault_ok" \
       --vault-id "localhost@$vault_pass_file" 2>/dev/null | grep -q "vault_ok"; then
      success "Existing vault password verified"
    else
      error "Existing vault-pass.secret cannot decrypt localhost.yml"
      echo -e "   ${YELLOW}${ARROW}${NC} The file exists but the password is wrong."
      echo -e "   Enter the correct vault password (from your password manager)."
      # Verify-before-write: re-prompt until the entry decrypts, or abort loudly.
      if ! vaultPass=$(prompt_verified_vault_password "$localhost_yml"); then
        error "No usable vault password — cannot continue."
        exit 1
      fi
      # printf avoids the trailing newline echo would add — the vault password must be exact
      printf '%s' "$vaultPass" > "$vault_pass_file"
      chmod 600 "$vault_pass_file"
      success "Vault password updated"
    fi
  else
    echo -e "\n${CYAN}${ARROW}${NC} Your localhost.yml has vault-encrypted values."
    echo -e "   Enter your vault password (from your password manager)."
    # Verify-before-write: re-prompt until the entry decrypts, or abort loudly.
    if ! vaultPass=$(prompt_verified_vault_password "$localhost_yml"); then
      error "No usable vault password — cannot continue."
      exit 1
    fi
    # printf avoids the trailing newline echo would add — the vault password must be exact
    printf '%s' "$vaultPass" > "$vault_pass_file"
    chmod 600 "$vault_pass_file"
    success "Vault password configured"
  fi
elif [[ -f "$vault_pass_file" ]]; then
  success "Existing vault password found"
else
  info "Setting up Ansible vault"
  echo -e "\n${CYAN}${ARROW}${NC} Enter a new vault password (or leave blank to auto-generate a strong one)."
  echo -e "   You will be asked to type it twice to catch typos."
  # Brand-new password: nothing to verify against, so double-enter to catch typos.
  # promptSecretConfirmed re-prompts until both entries match; empty is allowed
  # here and means auto-generate.
  vaultPass=$(promptSecretConfirmed "Vault password (blank = auto-generate)")
  if [[ "" == "$vaultPass" ]]; then
    vaultPass="$(openssl rand -base64 32)"
    success "Generated new vault password"
  else
    success "Vault password configured"
  fi
  # printf avoids the trailing newline echo would add — the vault password must be exact
  printf '%s' "$vaultPass" > "$vault_pass_file"
  chmod 600 "$vault_pass_file"
fi
# Secret no longer needed in memory — the password now lives only in the 0600 file.
unset vaultPass
completed

title "GitHub SSH Key Passphrase"
# GitHub SSH keys (github_*) are full account keys — they must be passphrase-protected.
# The passphrase is stored in the vault so the Ansible playbook can manage keys idempotently.
_github_ssh_passphrase=""

if grep -q 'github_ssh_passphrase:' "$localhost_yml" 2>/dev/null; then
  success "github_ssh_passphrase already configured in localhost.yml"
else
  if [[ "$HEADLESS" == "true" ]]; then
    # Headless: the GitHub account keys reuse the resolved SSH passphrase (D6 keeps
    # them passphrase-protected). Required in preflight, so it is always non-empty.
    _github_ssh_passphrase="$HL_GITHUB_SSH_PASSPHRASE"
    info "Headless: using the provided SSH passphrase for GitHub account keys"
  else
    info "GitHub SSH keys require a passphrase (these are full account keys, not deploy keys)"
    echo
    if [[ -n "$_ssh_key_password" ]]; then
      if confirm "Use the same password as your main SSH key (~/.ssh/id) for all GitHub keys?" y; then
        _github_ssh_passphrase="$_ssh_key_password"
        success "Using same password as ~/.ssh/id"
      fi
    fi

    if [[ -z "$_github_ssh_passphrase" ]]; then
      info "Hint: your login password is a convenient choice"
      _github_ssh_passphrase=$(promptSecretConfirmed "GitHub SSH keys passphrase")
    fi
  fi

  info "Encrypting github_ssh_passphrase and saving to vault..."
  # printf avoids trailing newline that echo adds — passphrase must be exact
  _encrypted=$(printf '%s' "$_github_ssh_passphrase" | ansible-vault encrypt_string \
    --stdin-name 'github_ssh_passphrase')
  printf '\n%s\n' "$_encrypted" >> "$localhost_yml"
  success "github_ssh_passphrase saved to localhost.yml (vault-encrypted)"
fi
# The SSH-key password was only needed as a default offer above — drop it now.
# L3: removed the dead `unset _confirm_passphrase` (that var was never set); the
# plaintext `password` is already unset at its last use in the keygen step.
unset _ssh_key_password _encrypted
completed

title "Setting Up GitHub Multi-Account Access"
if grep -q 'github_accounts' "$localhost_yml" 2>/dev/null; then
  # gh-account-setup.bash handles: per-account gh auth, OAuth scope audit,
  # SSH key generation, programmatic key upload (gh ssh-key add), and
  # isolated SSH verification. Pass passphrase via env if already in memory
  # (fresh-install path); otherwise the script decrypts from vault itself.
  GITHUB_SSH_PASSPHRASE="${_github_ssh_passphrase:-}" \
  LOCALHOST_YML="$localhost_yml" \
  VAULT_PASS_FILE="$vault_pass_file" \
  RUN_BASH_HEADLESS="$HEADLESS" \
    ./scripts/gh-account-setup.bash --setup-all
else
  success "Single account setup — no additional accounts to authenticate"
fi
# Passphrase has been handed off (env var to the setup script / vault-encrypted in
# localhost.yml) — clear it from this shell before the long playbook run.
unset _github_ssh_passphrase
completed

title "Running Ansible Playbooks"
info "Pulling latest changes before running playbooks"
assert_clean_worktree ~/Projects/fedora-desktop
# See note above on `command git` — bypass any sourced git() wrapper.
command git pull
success "Repository up to date"

# V3.12: this pull is the LAST git op needing the login SSH key. Kill the ssh-agent
# NOW (not at EXIT) so the unlocked key is not reachable via $SSH_AUTH_SOCK across
# ansible-galaxy, the whole main playbook, optional playbooks, and reboot. hl_cleanup
# is only a backstop. No-op on interactive runs (no agent was started).
if [[ "$HEADLESS" == "true" ]]; then
  hl_ssh_agent_stop
  info "Headless: ssh-agent torn down after the last git operation"
fi

info "Installing Ansible requirements"
# Do NOT suppress output — this is a supply-chain install (pulls roles/collections
# from Galaxy/Git). Tee to a log so the user can see exactly what was fetched, and
# surface the log if the install fails (set -e then aborts the run).
_galaxy_log=$(mktemp)
if ! ansible-galaxy install -r requirements.yml 2>&1 | tee "$_galaxy_log"; then
  error "ansible-galaxy install failed — see output above (log: $_galaxy_log)"
  exit 1
fi
rm -f "$_galaxy_log"
success "Requirements installed"

info "Executing main configuration playbook"
echo -e "${YELLOW}${INFO} This may take several minutes...${NC}\n"

# Run main playbook normally with full colors.
# B1: under set -e a failing playbook exits the run BEFORE `main_exit_code=$?` can
# capture it, so the failure-handling UX below (issue report, continue-anyway
# prompt) never runs. Seed 0 and capture with `|| main_exit_code=$?` so the
# failure is handled here instead of silently aborting.
main_exit_code=0

# Headless: forward the provisioning profile to the main playbook when the operator
# pinned it (RUN_BASH_PROVISIONING_PROFILE); otherwise the playbook auto-detects it.
_main_pb_args=()
if [[ "$HEADLESS" == "true" && -n "${RUN_BASH_PROVISIONING_PROFILE:-}" ]]; then
  _main_pb_args+=(-e "provisioning_profile=${RUN_BASH_PROVISIONING_PROFILE}")
fi

# -k ignores any cached sudo timestamp, so this is true ONLY for genuine
# passwordless (NOPASSWD) sudo. Without -k, an earlier `sudo` in this run
# leaves a cached ticket that makes this pass, skipping --ask-become-pass —
# but ansible become runs in its own tty (Fedora tty_tickets) and can't use
# that cache, so it fails with "premature end of stream waiting for become
# success". Detecting real NOPASSWD here routes password sudo to --ask-become-pass.
if sudo -k -n true 2>/dev/null; then
  ./playbooks/playbook-main.yml "${_main_pb_args[@]}" || main_exit_code=$?
else
  echo -e "${YELLOW}${INFO} sudo needs a password — Ansible will now prompt you for it (BECOME password)${NC}"
  ./playbooks/playbook-main.yml "${_main_pb_args[@]}" --ask-become-pass || main_exit_code=$?
fi

if [[ $main_exit_code -eq 0 ]]; then
  completed
elif [[ "$HEADLESS" == "true" ]]; then
  # D7: a headless main-playbook failure is FATAL — no PUBLIC-tracker issue prompt, no
  # continue-anyway. Abort LOUD with the exact exit code so the failure is unmissable
  # and the run exits non-zero (never limps on into optional playbooks).
  hl_abort "main playbook" "playbook-main.yml FAILED with exit code ${main_exit_code}" \
    "scroll up for the failing Ansible task's output; fix the cause and re-run. Headless never files an issue or continues past a main-playbook failure."
else
  error "Main playbook failed with exit code: $main_exit_code"

  # Offer to create GitHub issue (posts to the PUBLIC tracker — default No)
  if confirm "Would you like to create a GitHub issue for this failure? (posts to the PUBLIC tracker)" n; then
    create_github_issue "./playbooks/playbook-main.yml" "$main_exit_code"
  fi

  # Ask if user wants to continue despite failure (benign continue — default Yes)
  if ! confirm "Do you want to continue with optional playbooks despite the main playbook failure?" y; then
    error "Installation aborted due to main playbook failure"
    exit $main_exit_code
  fi
fi

## ── Restore Projects ─────────────────────────────────────────────────────────

title "Restoring Projects"
_pull_projects_script=~/Projects/fedora-desktop/fedora-install/pull-projects.bash
if [[ -f "$_pull_projects_script" ]]; then
  _do_restore=false
  if [[ "$HEADLESS" == "true" ]]; then
    # Headless: opt-in via RUN_BASH_RESTORE_PROJECTS=1 (default off — a fresh server
    # usually has no project manifest yet). No prompt.
    if [[ "${RUN_BASH_RESTORE_PROJECTS:-0}" == "1" ]]; then
      _do_restore=true
    else
      info "Headless: RUN_BASH_RESTORE_PROJECTS not set — skipping project restore"
    fi
  elif confirm "Would you like to restore projects from your config repo manifest?" y; then
    _do_restore=true
  fi
  if [[ "$_do_restore" == "true" ]]; then
    if ! "$_pull_projects_script" --account "$primary_gh_username"; then
      warning "Projects restore failed or no manifest found — continuing"
    fi
  fi
else
  warning "pull-projects.bash not found — skipping project restore"
fi

echo -e "\n${GREEN}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}${BOLD}║              MAIN INSTALLATION COMPLETE!                    ║${NC}"
echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}\n"

fi # end: OPTIONAL_ONLY skip block

## Optional Playbooks Menu System

# Function to run a playbook (wrapper for backward compatibility).
# The optional-menu call sites invoke this as a bare statement inside loops under
# set -e. run_playbook_with_issue_option already surfaces any failure (error
# message + offer to file an issue), so a non-zero return here must NOT abort the
# whole optional menu — the user stays in control and can pick the next item.
# Handle the failure explicitly (it is already reported) and return 0 so set -e
# lets the menu continue. (B2: the issue-reporting UX runs AND the installer keeps
# going instead of dying mid-menu.)
run_playbook() {
  local _rc=0
  run_playbook_with_issue_option "$1" "$2" || _rc=$?
  if [[ $_rc -ne 0 ]]; then
    warning "Continuing the optional menu after a failure in: $2 (exit $_rc)"
  fi
  return 0
}

# Parse a space/comma-separated list of numbers; print valid ones (one per line)
_parse_number_list() {
  local input="$1"
  local max="$2"
  local -a tokens
  # Global IFS=$'\n\t' excludes spaces, so use explicit IFS for splitting
  IFS=' ,' read -ra tokens <<< "$input"
  for n in "${tokens[@]}"; do
    [[ -z "$n" ]] && continue
    if [[ "$n" =~ ^[0-9]+$ ]] && [[ "$n" -ge 1 ]] && [[ "$n" -le "$max" ]]; then
      echo "$n"
    else
      warning "Ignoring invalid number: $n (valid range: 1-$max)" >&2
    fi
  done
}

# Function to display menu
show_menu() {
  local category="$1"
  shift
  local playbooks=("$@")
  local choice

  while true; do
    echo -e "\n${CYAN}${BOLD}$category Playbooks${NC}"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

    local i=1
    for pb in "${playbooks[@]}"; do
      local name
      name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
      echo -e "  ${BOLD}$i)${NC} $name"
      ((i++))
    done
    echo -e "  ${BOLD}A)${NC} Run all"
    echo -e "  ${BOLD}W)${NC} Whitelist — enter numbers to run (e.g. 1 3 5 or 1,3,5)"
    echo -e "  ${BOLD}B)${NC} Blacklist — enter numbers to skip, run all others"
    echo -e "  ${BOLD}S)${NC} Skip to next section"
    echo -e "  ${BOLD}Q)${NC} Quit optional installations"

    echo
    # Defect 3: EOF (closed stdin, e.g. `--optional-only < /dev/null`) must not
    # spin the menu loop forever. Return non-zero; the `if ! show_menu` call sites
    # then cleanly skip the menu.
    if ! read -rp "Enter your choice: " choice; then
      info "No input (stdin closed) — skipping menu"
      return 1
    fi

    case "$choice" in
      [1-9]|[1-9][0-9])
        if [[ $choice -le ${#playbooks[@]} ]]; then
          local selected="${playbooks[$((choice-1))]}"
          local name
          name=$(basename "$selected" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
          run_playbook "$selected" "$name"
        else
          error "Invalid selection: $choice (choose 1-${#playbooks[@]})"
          echo -e "${YELLOW}${ARROW} Please try again${NC}\n"
          sleep 1
        fi
        ;;
      [Aa])
        for pb in "${playbooks[@]}"; do
          local name
          name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
          run_playbook "$pb" "$name"
        done
        break
        ;;
      [Ww])
        # Defect 3: EOF on the sub-prompt must not spin — skip the menu cleanly.
        if ! read -rp "Enter numbers to run (space or comma separated): " _wl_input; then
          info "No input (stdin closed) — skipping menu"
          return 1
        fi
        mapfile -t _wl_nums < <(_parse_number_list "$_wl_input" "${#playbooks[@]}")
        if [[ ${#_wl_nums[@]} -eq 0 ]]; then
          warning "No valid numbers entered — try again"
        else
          for n in "${_wl_nums[@]}"; do
            local pb="${playbooks[$((n-1))]}"
            local name
            name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
            run_playbook "$pb" "$name"
          done
          break
        fi
        ;;
      [Bb])
        # Defect 3: EOF on the sub-prompt must not spin — skip the menu cleanly.
        if ! read -rp "Enter numbers to skip (space or comma separated, Enter to run all): " _bl_input; then
          info "No input (stdin closed) — skipping menu"
          return 1
        fi
        mapfile -t _bl_nums < <(_parse_number_list "$_bl_input" "${#playbooks[@]}")
        local _idx=1
        for pb in "${playbooks[@]}"; do
          local _skip=false
          for n in "${_bl_nums[@]}"; do
            if [[ "$_idx" -eq "$n" ]]; then
              _skip=true
              break
            fi
          done
          if [[ "$_skip" == "false" ]]; then
            local name
            name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
            run_playbook "$pb" "$name"
          fi
          ((_idx++))
        done
        break
        ;;
      [Ss])
        break
        ;;
      [Qq])
        return 1
        ;;
      *)
        error "Invalid choice: '$choice'"
        echo -e "${YELLOW}${ARROW} Please enter a number, A, W, B, S, or Q${NC}\n"
        sleep 1
        ;;
    esac
  done
  return 0
}

# Hardware detection function
check_hardware() {
  local playbook="$1"
  local pb_name
  pb_name=$(basename "$playbook")
  
  case "$pb_name" in
    *nvidia*)
      if lspci 2>/dev/null | grep -qi nvidia; then
        echo "${GREEN}[RECOMMENDED]${NC}"
      elif lsmod 2>/dev/null | grep -qi nouveau; then
        echo "${YELLOW}[MAYBE]${NC}"
      else
        echo "${RED}[NOT DETECTED]${NC}"
      fi
      ;;
    *displaylink*)
      if lsusb 2>/dev/null | grep -qi displaylink; then
        echo "${GREEN}[RECOMMENDED]${NC}"
      else
        echo "${YELLOW}[MANUAL CHECK]${NC}"
      fi
      ;;
    *tlp*|*battery*)
      if [[ -d /sys/class/power_supply/BAT0 ]] || [[ -d /sys/class/power_supply/BAT1 ]]; then
        echo "${GREEN}[RECOMMENDED]${NC}"
      else
        echo "${RED}[DESKTOP]${NC}"
      fi
      ;;
    *)
      echo "${YELLOW}[CHECK MANUALLY]${NC}"
      ;;
  esac
}

# Optional Playbooks Section
echo -e "\n${MAGENTA}${BOLD}Optional Configurations${NC}"
echo -e "${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

if [[ "$HEADLESS" == "true" ]]; then
  hl_run_optional_playbooks
elif [[ "$OPTIONAL_ONLY" == "true" ]] || confirm "Would you like to install optional components?" y; then
  # L4: --optional-only with no prior install has no cloned repo. Fail with a
  # clear "run the full install first" message rather than a terse cd error.
  if [[ ! -d ~/Projects/fedora-desktop ]]; then
    error "$HOME/Projects/fedora-desktop not found — the repo has not been cloned yet."
    echo -e "${YELLOW}${ARROW} Run the full install first (./run.bash, no --optional-only),${NC}"
    echo -e "${YELLOW}${ARROW} which clones the repo and performs core setup.${NC}"
    exit 1
  fi
  cd ~/Projects/fedora-desktop || { error "Cannot cd into ~/Projects/fedora-desktop"; exit 1; }

  # Playbooks that always run without prompting (auto-run before the interactive menu)
  # NOTE: play-speech-to-text.yml removed pending GPU/CPU split — see issue #11
  auto_run_common=()

  # Common optional playbooks
  if [[ -d playbooks/imports/optional/common ]]; then
    mapfile -t common_playbooks < <(find playbooks/imports/optional/common -name "*.yml" -type f | sort)
    if [[ ${#common_playbooks[@]} -gt 0 ]]; then

      # Auto-run whitelisted playbooks first (no prompt)
      menu_playbooks=()
      for pb in "${common_playbooks[@]}"; do
        pb_base=$(basename "$pb")
        _is_auto=false
        for _auto_name in "${auto_run_common[@]}"; do
          if [[ "$pb_base" == "$_auto_name" ]]; then
            _is_auto=true
            break
          fi
        done
        if [[ "$_is_auto" == "true" ]]; then
          name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
          info "Auto-running: $name"
          run_playbook "$pb" "$name"
        else
          menu_playbooks+=("$pb")
        fi
      done

      # Interactive menu for the rest
      if [[ ${#menu_playbooks[@]} -gt 0 ]]; then
        info "Found ${#menu_playbooks[@]} common optional playbooks"
        if ! show_menu "Common Optional" "${menu_playbooks[@]}"; then
          info "Skipping remaining optional installations"
        fi
      fi
    fi
  fi

  # Hardware-specific playbooks — auto-run detected, skip undetected, prompt for uncertain
  if [[ -d playbooks/imports/optional/hardware-specific ]]; then
    mapfile -t hw_playbooks < <(find playbooks/imports/optional/hardware-specific -name "*.yml" -type f | sort)
    if [[ ${#hw_playbooks[@]} -gt 0 ]]; then
      echo -e "\n${CYAN}${BOLD}Hardware-Specific Playbooks${NC}"
      echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
      info "Analyzing your hardware..."

      hw_auto=()
      hw_prompt=()
      for pb in "${hw_playbooks[@]}"; do
        name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
        hw_status=$(check_hardware "$pb")
        if echo "$hw_status" | grep -q "RECOMMENDED"; then
          hw_auto+=("$pb")
          success "Hardware detected — will auto-run: $name"
        elif echo "$hw_status" | grep -q "NOT DETECTED\|DESKTOP"; then
          info "Hardware not detected — skipping: $name"
        else
          hw_prompt+=("$pb")
          warning "Needs manual check: $name $hw_status"
        fi
      done

      # Auto-run recommended hardware playbooks
      for pb in "${hw_auto[@]}"; do
        name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
        run_playbook "$pb" "$name"
      done

      # Prompt for uncertain hardware playbooks
      if [[ ${#hw_prompt[@]} -gt 0 ]]; then
        if confirm "Would you like to configure hardware-specific components that need manual checking?" y; then
          # B3: show_menu returns 1 when the user presses Q; as a bare statement
          # under set -e that would kill the installer. Guard it like the Common
          # Optional call site does.
          if ! show_menu "Hardware-Specific" "${hw_prompt[@]}"; then
            info "Skipping remaining hardware-specific installations"
          fi
        fi
      fi
    fi
  fi
  
  # Untested playbooks warning
  if [[ -d playbooks/imports/optional/untested ]]; then
    mapfile -t untested_playbooks < <(find playbooks/imports/optional/untested -name "*.yml" -type f | sort)
    if [[ ${#untested_playbooks[@]} -gt 0 ]]; then
      echo -e "\n${RED}${BOLD}⚠ UNTESTED Playbooks (Fedora $fedora_version)${NC}"
      echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
      error "Found ${#untested_playbooks[@]} untested playbooks for Fedora $fedora_version"
      echo -e "${RED}${BOLD}These have NOT been tested on Fedora $fedora_version and may fail:${NC}"
      for pb in "${untested_playbooks[@]}"; do
        name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
        echo -e "  ${RED}⚠${NC} $name"
      done
      echo -e "\n${YELLOW}These require careful manual testing before use.${NC}"
      
      if confirm "Would you like to attempt running untested playbooks? (NOT RECOMMENDED)" n; then
        echo -e "${RED}${BOLD}WARNING: These playbooks may fail or cause issues!${NC}"
        # B3: guard the Q-returns-1 path so set -e does not abort the installer.
        if ! show_menu "Untested (USE WITH CAUTION)" "${untested_playbooks[@]}"; then
          info "Skipping remaining untested installations"
        fi
      fi
    fi
  fi
  
  # Experimental playbooks warning
  if [[ -d playbooks/imports/optional/experimental ]]; then
    mapfile -t exp_playbooks < <(find playbooks/imports/optional/experimental -name "*.yml" -type f | sort)
    if [[ ${#exp_playbooks[@]} -gt 0 ]]; then
      echo -e "\n${YELLOW}${BOLD}⚠ Experimental Playbooks${NC}"
      echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
      warning "Found ${#exp_playbooks[@]} experimental playbooks"
      echo -e "${YELLOW}These are experimental and should only be run if you know what you're doing:${NC}"
      for pb in "${exp_playbooks[@]}"; do
        name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
        echo -e "  ${YELLOW}•${NC} $name"
      done
      echo -e "\n${YELLOW}Run these manually if needed: ${BOLD}./playbooks/imports/optional/experimental/play-*.yml${NC}"
    fi
  fi
fi

# Final completion message
echo -e "\n${GREEN}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}${BOLD}║                    ALL DONE!                                ║${NC}"
echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}\n"

echo -e "${CYAN}${BOLD}Optional next steps${NC} (run after reboot):"
echo -e "  ${ARROW} Optional setup scripts (rclone cloud storage, etc.):"
echo -e "    ${BOLD}cd ~/Projects/fedora-desktop${NC}"
echo -e "    ${BOLD}./scripts/setup.bash${NC}"
echo -e "  ${ARROW} Python development environment (pyenv + pyenv versions):"
echo -e "    ${BOLD}./playbooks/imports/optional/common/play-python.yml${NC}"
echo

# Mark setup complete so the GNOME autostart doesn't re-fire after reboot
mkdir -p ~/.local/state
touch ~/.local/state/fedora-desktop-setup-complete

title "System Reboot"
warning "A reboot is recommended to complete the configuration"
if [[ "$HEADLESS" == "true" ]]; then
  # Headless: reboot only when explicitly asked (RUN_BASH_REBOOT=1). Default: finish
  # cleanly and leave the box up so the operator/orchestrator controls the reboot.
  if [[ "${RUN_BASH_REBOOT:-0}" == "1" ]]; then
    success "Headless provisioning complete — rebooting now (RUN_BASH_REBOOT=1)"
    sudo reboot now
  else
    success "Headless provisioning complete! Reboot when convenient (set RUN_BASH_REBOOT=1 to auto-reboot)."
    exit 0
  fi
elif confirm "Ready to reboot now?" n; then
  echo -e "${YELLOW}${INFO} Rebooting system...${NC}"
  sudo reboot now
else
  success "Installation complete!"
  echo -e "${YELLOW}${INFO} Remember to reboot your system when convenient${NC}"
  exit 0
fi
}  # end main()

# Run the whole thing inside an explicit subshell so set -e / IFS / trap / exit
# are contained and never leak into a sourcing shell (H4). By the time main runs,
# bash has already parsed the entire file, so the `git pull` steps inside cannot
# corrupt a still-streaming executed file (B5).
( main "$@" )
