#!/usr/bin/env bash
# reap-stuck-triage.bash — Plan 00157: record what a hung earlier run of this plan's triage is
# doing, then kill it.
#
# WHY: the first host run of triage.bash hung in an interactive-shell probe, and Ctrl-C could
# not end it: the run log's Ctrl-C handler waits for every writer of the log to finish, and
# the stuck shell, in timeout's own process group, never saw the Ctrl-C and kept the log open.
# triage.bash keeps that probe's output off the log now; this clears a run that predates it.
#
# WHAT IT CHANGES: it KILLS processes, so it is not triage. Only these, and only from an
# earlier run:
#   - every triage.bash and meta-deploy.bash process, with all their descendants;
#   - every shell running the old triage's CLAUDE_BIN= probe (the one that hung), with its
#     descendants.
# It never touches its own process group, nor any process it was started from, so the
# meta-deploy run that calls it is safe. Before killing, it records each target's state,
# wait channel and open files into the run log, so the cause of the hang is kept.
#
# WHERE TO RUN: on the HOST, as the desktop user. meta-deploy.bash runs it first.
#
# Usage: ./CLAUDE/Plan/00157-cc-desktop-launcher-broken/reap-stuck-triage.bash [-h|--help]
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

PLAN_USAGE="usage: reap-stuck-triage.bash [-h|--help]"
plan_mode deploy
plan_parse_common_flags "$@"
if [[ ${#PLAN_REMAINING_ARGS[@]} -gt 0 ]]; then
  printf '[FATAL] unknown argument: %s\n%s\n' "${PLAN_REMAINING_ARGS[0]}" "${PLAN_USAGE}" >&2
  exit 64
fi

plan_require_host "it kills processes of an earlier host run"
if [[ "${EUID}" -eq 0 ]]; then
  printf '[FATAL] run this as the desktop user, not root: the stuck run is that user'"'"'s\n' >&2
  exit 1
fi
plan_start_log auto

# ── who is protected ─────────────────────────────────────────────────────────────────────

declare -A PROTECTED=()
OWN_PGID="$(ps -o pgid= -p "$$" | tr -d ' ')"
pid="$$"
while [[ -n "${pid}" && "${pid}" -gt 1 ]]; do
  PROTECTED["${pid}"]=1
  pid="$(ps -o ppid= -p "${pid}" | tr -d ' ')"
done

# ── the process table, once ──────────────────────────────────────────────────────────────

declare -A PPID_OF=() PGID_OF=() ARGS_OF=()
while read -r p pp pg args; do
  PPID_OF["${p}"]="${pp}"
  PGID_OF["${p}"]="${pg}"
  ARGS_OF["${p}"]="${args}"
done < <(ps -u "$(id -u)" -o pid=,ppid=,pgid=,args=)

# Roots: the old run's scripts, and the probe shell that hung. Matched on the argv recorded
# above, not with pgrep, so nothing here can match its own command line.
ROOTS=()
for p in "${!ARGS_OF[@]}"; do
  args="${ARGS_OF[${p}]}"
  if [[ -n "${PROTECTED[${p}]:-}" || "${PGID_OF[${p}]}" == "${OWN_PGID}" ]]; then
    continue
  fi
  # Only bash RUNNING one of the scripts (an editor with the file open is not matched), and
  # the hung probe in the exact form triage.bash started it.
  if [[ "${args}" =~ ^(/usr/bin/)?bash\ [^\ ]*(CLAUDE/Plan/meta-deploy|00157-cc-desktop-launcher-broken/triage)\.bash(\ |$) ]] \
    || [[ "${args}" =~ ^(timeout\ .*)?bash\ -ic\ .*CLAUDE_BIN= ]]; then
    ROOTS+=("${p}")
  fi
done

if [[ ${#ROOTS[@]} -eq 0 ]]; then
  echo "==> no process of an earlier triage or meta-deploy run is alive; nothing to reap"
  exit 0
fi

# Every descendant of every root, so nothing the old run started is left holding its pipes.
TARGETS=()
declare -A SEEN=()
queue=("${ROOTS[@]}")
while [[ ${#queue[@]} -gt 0 ]]; do
  p="${queue[0]}"
  queue=("${queue[@]:1}")
  if [[ -n "${SEEN[${p}]:-}" || -n "${PROTECTED[${p}]:-}" || "${PGID_OF[${p}]:-}" == "${OWN_PGID}" ]]; then
    continue
  fi
  SEEN["${p}"]=1
  TARGETS+=("${p}")
  for c in "${!PPID_OF[@]}"; do
    if [[ "${PPID_OF[${c}]}" == "${p}" ]]; then
      queue+=("${c}")
    fi
  done
done

# ── record, then kill ────────────────────────────────────────────────────────────────────

describe() {
  local p="$1"
  echo "--- pid ${p}"
  ps -o pid=,ppid=,pgid=,stat=,etime=,wchan:24=,args= -p "${p}"
  printf 'wchan: %s\n' "$(cat "/proc/${p}/wchan")"
  awk '/^(State|SigBlk|SigIgn|SigCgt):/' "/proc/${p}/status"
  echo "open files:"
  ls -l "/proc/${p}/fd/"
}

echo "=== processes of the earlier run (READ THIS FOR: what the hung shell was waiting on —"
echo "###   a 'T' state is a stop, a tty in its open files is what it touched, a pipe shared"
echo "###   with the old triage.bash is what kept the run log open)"
for p in "${TARGETS[@]}"; do
  # A target may end on its own between the table and here; that is recorded, not fatal.
  if ! describe "${p}"; then
    echo "(pid ${p} ended before it could be described)"
  fi
done

echo "=== killing ${#TARGETS[@]} process(es): ${TARGETS[*]}"
for p in "${TARGETS[@]}"; do
  if ! kill -KILL "${p}"; then
    echo "(pid ${p} was already gone)"
  fi
done

sleep 1
LEFT=()
for p in "${TARGETS[@]}"; do
  if [[ -d "/proc/${p}" ]] && [[ "$(awk '/^State:/ { print $2 }' "/proc/${p}/status")" != "Z" ]]; then
    LEFT+=("${p}")
  fi
done
if [[ ${#LEFT[@]} -gt 0 ]]; then
  echo "[FATAL] still alive after SIGKILL: ${LEFT[*]}" >&2
  exit 1
fi
echo "==> all ${#TARGETS[@]} process(es) of the earlier run are gone"
