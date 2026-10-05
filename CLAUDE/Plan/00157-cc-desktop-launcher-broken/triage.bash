#!/usr/bin/env bash
# triage.bash — Plan 00157: gather FACTS about why `cc` (the host Claude Code launcher,
# /var/local/claude-code/cc) stopped working: typing `cc` reports that no Claude process was
# found instead of starting a session. Fact-finding only: renders no verdict (R9).
#
# WHERE TO RUN: on the HOST, in a terminal, as the desktop user (cc is a per-user launcher;
# root has none of the state it reads).
#
# WHAT IT CHANGES: nothing, by default. It reads files, lists processes and tmux sessions,
# runs `claude --version`, and searches the launcher, its libraries, the shell startup files
# and the claude binary for the text of the error. Credential and token FILES are described
# by name, size and date only; their contents are never read into the report.
#
#   --trace   ALSO runs `bash -x /var/local/claude-code/cc --version` under `script`, so the
#             exact failure and the line that produced it are recorded. This is the one
#             active probe: if cc gets far enough it shows its prompts (pick any answer;
#             Desktop is fine), runs `claude update` as cc always does, and opens a cc tmux
#             session that ends as soon as `claude --version` returns. Token values in the
#             trace are redacted before it is written, and the file is deleted if any survive.
#
# Usage: ./CLAUDE/Plan/00157-cc-desktop-launcher-broken/triage.bash [--trace] [-h|--help]
set -euo pipefail
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
  if [[ -e "${repoRoot}/.git" ]]; then
    printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
    exit 1
  fi
  repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || { printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2; exit 1; }
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: triage.bash [--trace] [-h|--help]"
plan_mode gather
plan_parse_common_flags "$@"

TRACE=0
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
  case "${arg}" in
    --trace) TRACE=1 ;;
    *)
      printf '[FATAL] unknown argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
      exit 64
      ;;
  esac
done

plan_require_host "it reads the host's own cc launcher, claude install and shell startup"
if [[ "${EUID}" -eq 0 ]]; then
  printf '[FATAL] run this as the desktop user, not root: cc and its state are per-user\n' >&2
  exit 1
fi
plan_start_log auto

CC_DEPLOYED="/var/local/claude-code/cc"
CCY_LIB_DEPLOYED="/var/local/claude-yolo/lib"
CC_LIBS=(common-pure.bash token-management.bash tmux-session.bash session-registry.bash)
CLAUDE_HOME="${CLAUDE_CONFIG_DIR:-${HOME}/.claude}"
# The interactive startup decides what `cc` means, so the error search covers it too.
STARTUP_FILES=("${HOME}/.bashrc" "${HOME}/.bash_profile" "${HOME}/.profile" /etc/bashrc)
STARTUP_DIRS=("${HOME}/.bashrc.d" "${HOME}/.bashrc-includes" /etc/profile.d)
ERROR_TEXT='claude process found'
TOKEN_RE='sk-ant-[A-Za-z0-9_-]+'

probe() {
  local label="$1"; shift
  local out rc
  if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
  printf '### %s  (rc=%d)\n%s\n\n' "${label}" "${rc}" "${out:-(no output)}"
  return 0
}

existing_paths() {
  local p
  for p in "$@"; do
    if [[ -e "${p}" ]]; then printf '%s\n' "${p}"; fi
  done
}

# A real interactive bash, as a terminal gets it, so aliases and functions from the startup
# files are in force. stdin closed and a time cap, so nothing in the startup can hold the run.
ibash() {
  timeout 30 bash -ic "$1" </dev/null
}

# The claude a terminal would run. Read from a marker line, because the startup files may
# print their own lines on the same stdout.
interactive_claude_path() {
  local path
  path="$(ibash 'printf "\nCLAUDE_BIN="; type -P claude' | awk -F= '/^CLAUDE_BIN=/ { print $2 }')"
  if [[ -z "${path}" ]]; then
    echo "claude is not on the interactive PATH" >&2
    return 1
  fi
  printf '%s\n' "${path}"
}

# ── what `cc` resolves to ─────────────────────────────────────────────────────────────────

interactive_type() {
  ibash 'type -a cc; echo "---"; type -a claude; echo "--- PATH:"; printenv PATH'
}

cc_definitions_in_startup() {
  local -a targets=()
  mapfile -t targets < <(existing_paths "${STARTUP_FILES[@]}" "${STARTUP_DIRS[@]}")
  if [[ ${#targets[@]} -eq 0 ]]; then
    echo "none of the startup files or directories exist"
    return 1
  fi
  grep -rnE "(alias[[:space:]]+cc=|(^|[[:space:]])cc[[:space:]]*\(\)|function[[:space:]]+cc\b|claude-code/cc)" "${targets[@]}"
}

# ── who prints the error ──────────────────────────────────────────────────────────────────

search_scripts_for_error() {
  local -a targets=()
  mapfile -t targets < <(existing_paths /var/local/claude-code /var/local/claude-yolo \
    "${HOME}/.local/bin" "${STARTUP_FILES[@]}" "${STARTUP_DIRS[@]}" \
    "${CLAUDE_HOME}/settings.json" "${CLAUDE_HOME}/hooks" "${CLAUDE_HOME}/statusline.sh")
  grep -rnIi -- "${ERROR_TEXT}" "${targets[@]}"
}

search_claude_binary_for_error() {
  local bin real
  bin="$(interactive_claude_path)"
  real="$(readlink -f "${bin}")"
  printf 'claude: %s -> %s\n' "${bin}" "${real}"
  # -a: the binary is a bundled executable; -o with a little context shows the message
  # and what surrounds it, without dumping the binary.
  grep -aoiE ".{0,120}${ERROR_TEXT}.{0,120}" "${real}"
}

# ── the deployed launcher against this checkout ──────────────────────────────────────────

deployed_against_repo() {
  local lib drift=0
  ls -l "${CC_DEPLOYED}"
  if ! diff -u "${PLAN_REPO_ROOT}/files/var/local/claude-code/cc" "${CC_DEPLOYED}"; then drift=1; fi
  for lib in "${CC_LIBS[@]}"; do
    ls -l "${CCY_LIB_DEPLOYED}/${lib}"
    if ! diff -u "${PLAN_REPO_ROOT}/files/var/local/claude-yolo/lib/${lib}" "${CCY_LIB_DEPLOYED}/${lib}"; then drift=1; fi
  done
  if [[ "${drift}" -eq 1 ]]; then
    echo "(the deployed copies above differ from this checkout)"
    return 1
  fi
  echo "(deployed launcher and libraries match this checkout)"
}

syntax_check_deployed() {
  local lib rc=0
  bash -n "${CC_DEPLOYED}" || rc=1
  for lib in "${CC_LIBS[@]}"; do
    bash -n "${CCY_LIB_DEPLOYED}/${lib}" || rc=1
  done
  return "${rc}"
}

checkout_history() {
  git -C "${PLAN_REPO_ROOT}" log --oneline -1
  echo "--- last changes to the launcher and its libraries:"
  git -C "${PLAN_REPO_ROOT}" log --format='%h %ad %s' --date=iso -10 -- \
    files/var/local/claude-code/ files/var/local/claude-yolo/lib/ playbooks/imports/play-claude-yolo.yml
}

# ── the claude install ────────────────────────────────────────────────────────────────────

claude_install() {
  local bin
  bin="$(interactive_claude_path)"
  ls -l "${bin}"
  readlink -f "${bin}"
  if [[ -d "${HOME}/.local/share/claude/versions" ]]; then
    echo "--- ~/.local/share/claude/versions:"
    ls -l "${HOME}/.local/share/claude/versions"
  fi
}

claude_version() {
  ibash 'claude --version'
}

# ── credential and token state cc depends on (names, sizes, dates — never contents) ──────

describe_files() {
  local f
  for f in "$@"; do
    if [[ -e "${f}" ]]; then
      stat -c '%A %s bytes  %y  %n' "${f}"
    else
      printf 'absent: %s\n' "${f}"
    fi
  done
}

credential_state() {
  printf 'CLAUDE_CONFIG_DIR=%s\n' "${CLAUDE_CONFIG_DIR:-(unset)}"
  describe_files "${CLAUDE_HOME}/.credentials.json" "${CLAUDE_HOME}/.credentials.json.cc-desktop-bak" \
    "${HOME}/.claude/.credentials.json.cc-desktop-bak" "${HOME}/.claude.json"
}

onboarding_flag() {
  jq '{hasCompletedOnboarding}' "${HOME}/.claude.json"
}

last_launch() {
  cat "${HOME}/.claude/.last-launch.conf"
}

token_pool() {
  # Filenames carry the token NAME and its guessed expiry date, not the secret.
  ls -l "${HOME}/.claude-tokens/ccy/tokens"
}

host_claude_settings() {
  jq '{statusLine, hooks: ((.hooks // {}) | keys), env: ((.env // {}) | keys)}' "${CLAUDE_HOME}/settings.json"
}

# ── sessions and processes ───────────────────────────────────────────────────────────────

claude_processes() {
  # Bracketed pattern so the probe cannot match its own command line.
  ps -eo pid,ppid,etime,tty,args | awk 'NR == 1 || /[c]laude/'
}

ccy_tmux_sessions() {
  tmux -L ccy list-sessions -F '#{session_name}  attached=#{session_attached}  created=#{t:session_created}  path=#{session_path}'
}

ccy_session_table() {
  ccy-sessions --list
}

tools_cc_needs() {
  local tool path
  for tool in tmux systemd-run systemd-escape fzf jq script; do
    if path="$(command -v "${tool}")"; then
      printf '%-15s %s\n' "${tool}" "${path}"
    else
      printf '%-15s MISSING\n' "${tool}"
    fi
  done
}

# ── --trace: run cc itself, with xtrace, under script ───────────────────────────────────

trace_cc() {
  local trace="${PLAN_RUN_DIR}/cc-trace-report.txt" rc=0
  if ! command -v script >/dev/null; then
    echo "script (util-linux) is not installed; it ships with every Fedora install, so this host is not the one cc was written for"
    return 1
  fi
  echo "Running: bash -x ${CC_DEPLOYED} --version  (answer any prompt cc shows; Desktop is fine)"
  # script gives cc the terminal it insists on; its typescript is streamed through the
  # redaction before anything reaches disk, so a token cc reads under -x is never written.
  # The terminal still sees the session through tee.
  script -q -e -c "bash -x ${CC_DEPLOYED} --version" /dev/null </dev/tty \
    | awk -v re="${TOKEN_RE}" '{ gsub(re, "sk-ant-<redacted>"); print; fflush() }' \
    | tee "${trace}" >/dev/tty || rc=$?
  if grep -aqE 'sk-ant-[A-Za-z0-9_-]{8}' "${trace}"; then
    rm -f "${trace}"
    echo "ERROR: redaction check FAILED: a token value reached the trace. Trace deleted." >&2
    return 1
  fi
  printf '### cc --trace exit status: %d\n' "${rc}"
  echo "### the full trace (ANSI included) is in ${trace}; the tail of it is what failed"
  return 0
}

# ── the report ───────────────────────────────────────────────────────────────────────────

echo "=== who prints the error"
echo "### READ THIS FOR: which program owns the message. rc=1 means the text is not in"
echo "###   that place. A hit in the claude binary means claude itself prints it."
probe "the error text in the launcher, libs, ~/.local/bin, startup files, claude hooks" search_scripts_for_error
probe "the error text inside the claude binary" search_claude_binary_for_error

echo "=== what cc resolves to"
echo "### READ THIS FOR: what a terminal actually runs when you type cc. Expected: an alias"
echo "###   to ${CC_DEPLOYED}. Anything else (a function, another binary first) is a fact."
probe "interactive bash: type -a cc / claude, and PATH" interactive_type
probe "every definition of cc in the startup files" cc_definitions_in_startup

echo "=== deployed launcher vs this checkout"
probe "deployed cc and libs vs this checkout (diff)" deployed_against_repo
probe "bash -n on the deployed cc and libs" syntax_check_deployed
probe "this checkout's HEAD and recent launcher history" checkout_history

echo "=== the claude install"
probe "the claude binary on the interactive PATH" claude_install
probe "claude --version (as Desktop, nothing exported)" claude_version

echo "=== credentials and tokens (names, sizes, dates; never contents)"
echo "### READ THIS FOR: cc refuses to start when a parked .cc-desktop-bak sits beside a live"
echo "###   .credentials.json, and a parked file with no live one is what a killed session leaves."
probe "credential files cc parks and restores" credential_state
probe "the onboarding flag in \$HOME/.claude.json" onboarding_flag
probe "the last token cc or ccy launched with" last_launch
probe "the named-token pool" token_pool
probe "host claude settings.json: status line, hook events, env keys" host_claude_settings

echo "=== sessions and processes"
probe "processes mentioning claude" claude_processes
probe "tmux sessions on the ccy server (cc-* are cc's)" ccy_tmux_sessions
probe "ccy-sessions --list" ccy_session_table
probe "tools cc needs" tools_cc_needs

if [[ "${TRACE}" -eq 1 ]]; then
  echo "=== cc --trace"
  trace_cc
fi
echo "================================================================"
echo "END OF REPORT. Read 'who prints the error' first, then 'what cc resolves to'."
echo "Run log: ${PLAN_RUN_LOG}"
echo "================================================================"
