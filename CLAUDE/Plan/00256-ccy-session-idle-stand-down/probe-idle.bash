#!/usr/bin/env bash
# probe-idle.bash — the legs of Plan 00256's triage.bash: what a ccy session's supervisor and its
# lifecycle plugin can see of an idle session, how often on_idle runs, what ccy-sessions lists
# and signals, and what the host's compose tools do with `stop`.
#
#   probe-idle.bash sessions|files|containers|listing|compose REPORT
#   probe-idle.bash sample SECONDS REPORT
#
# sessions    the session registry against the live ccy tmux server: one row per record
# files       per project with a live ccy session, the supervisor's and the plugin's own files
#             (decision.log, supervisor-status.json, the supervise/ directory, the context
#             sidecars, the thread registry, background-processes.jsonl, Claude Code's task,
#             team and transcript trees, the plugin's state directory)
# containers  per running ccy container: its pingbus inbox, its process tree under claude,
#             the supervisor processes and whether they carry --plugin (podman exec, reading)
# listing     ccy-sessions --list and notify --dry-run, against a registry holding one record
#             with no tmux session (a scratch registry under the run directory)
# compose     podman-compose, docker-compose, `docker compose` and `podman compose`: present,
#             version, `stop --help` and `down --help`
# sample      SECONDS of 0.25 s samples of each live project's supervise/ directory (the
#             plugin's in-hook marker is created and removed there on every hook call),
#             decision.log and context-sidecar directory, with the gaps between changes
#
# Appends ANONYMISED facts to REPORT and prints them: sessions are session-N, projects are
# project-N (numbered in registry order, the same in every leg of one run), and no name, path,
# host or container id is written. Raw command output that holds names goes to raw-*.txt files
# beside REPORT, under untracked/, which is never committed.
#
# Read-only: it reads files, lists tmux sessions, runs `podman ps/inspect/exec` with reading
# commands only, runs ccy-sessions --list and notify with --dry-run, and asks the compose tools
# for their version and help. It stops, removes and signals nothing. The scratch registry it
# writes for `listing` is a directory under the run directory, read by ccy-sessions only.
#
# A probe that cannot answer makes the leg exit 1 after it has printed what it found, so a gap
# is never silent.
set -euo pipefail

readonly USAGE="usage: probe-idle.bash sessions|files|containers|listing|compose REPORT | sample SECONDS REPORT"
MODE="${1:?${USAGE}}"
if [[ "${MODE}" == "sample" ]]; then
  SAMPLE_SECONDS="${2:?${USAGE}}"
  REPORT="${3:?${USAGE}}"
else
  REPORT="${2:?${USAGE}}"
fi
readonly MODE REPORT
RUN_DIR="$(dirname "${REPORT}")"
readonly RUN_DIR
FAILED=0
NOW="$(date +%s)"
readonly NOW

emit() {
  printf '%s\n' "$*" | tee -a "${REPORT}"
}

# A failed probe is data: its exit code and output are reported, and the leg fails at the end.
gap() {
  FAILED=1
  emit "!!! COULD NOT ESTABLISH: $*"
}

# Lines of stdin that contain the fixed text $1 (all non-empty lines when $1 is empty).
count_with() {
  awk -v p="$1" 'NF && index($0, p) { c++ } END { print c + 0 }'
}

# Lines of stdin that start with the fixed text $1.
count_starting() {
  awk -v p="$1" 'index($0, p) == 1 { c++ } END { print c + 0 }'
}

# "yes" when stdin contains the fixed text $1, else "no".
yes_if_contains() {
  awk -v p="$1" 'index($0, p) { f = 1 } END { print (f ? "yes" : "no") }'
}

# Session rows in ccy-sessions --list output: every non-empty line but the heading and the
# "No CCY or cc sessions" sentence.
list_rows() {
  awk 'NF && index($0, "SESSION ") != 1 && index($0, "No CCY") != 1 { c++ } END { print c + 0 }'
}

age_of() {
  local path="$1" mtime
  if ! mtime="$(stat -c %Y "${path}" 2>&1)"; then
    printf 'unreadable (%s)' "${mtime}"
    return 0
  fi
  printf '%ss' "$((NOW - mtime))"
}

# ── the registry and the live tmux server ──────────────────────────────────────────────────

registry_dir() {
  local base="${CCY_STATE_DIR:-}"
  if [[ -z "${base}" ]]; then
    base="${XDG_STATE_HOME:-${HOME:?HOME is not set}/.local/state}/ccy"
  fi
  printf '%s/sessions' "${base}"
}

declare -a REC_NAMES=()
declare -A REC_DIR=() REC_KIND=() REC_COMPOSE=() REC_FLAGS=() REC_LIVE=() REC_LABEL=()
declare -A PROJECT_LABEL=() LIVE_NAMES=()
declare -a PROJECT_DIRS=()
LIVE_COUNT=0
REGISTRY_ABSENT=0

load_live() {
  local out rc=0 name
  out="$(tmux -L ccy list-sessions -F '#{session_name}' 2>&1)" || rc=$?
  if [[ "${rc}" -ne 0 ]]; then
    case "${out}" in
      *"no server running"* | *"error connecting"* | *"No such file or directory"*)
        return 0
        ;;
    esac
    gap "tmux -L ccy list-sessions exited ${rc}: ${out}"
    return 0
  fi
  while IFS= read -r name; do
    [[ -n "${name}" ]] || continue
    LIVE_NAMES["${name}"]=1
    LIVE_COUNT=$((LIVE_COUNT + 1))
  done <<<"${out}"
}

launcher_kind() {
  case "$(basename "$1")" in
    claude-yolo | ccy*) printf 'ccy' ;;
    cc | cc-* | cc_*) printf 'cc' ;;
    *) printf 'other' ;;
  esac
}

load_registry() {
  local dir f line name rdir launcher compose flags arg n=0
  dir="$(registry_dir)"
  load_live
  if [[ ! -d "${dir}" ]]; then
    REGISTRY_ABSENT=1
    return 0
  fi
  for f in "${dir}"/*; do
    [[ -f "${f}" ]] || continue
    name="" rdir="" launcher="" compose="" flags=""
    while IFS= read -r line; do
      case "${line}" in
        name=*) name="${line#name=}" ;;
        dir=*) rdir="${line#dir=}" ;;
        launcher=*) launcher="${line#launcher=}" ;;
        compose=*) compose="${line#compose=}" ;;
        arg=--*)
          arg="${line#arg=}"
          flags+="${arg%%=*} "
          ;;
      esac
    done <"${f}"
    if [[ -z "${name}" || -z "${rdir}" ]]; then
      gap "a registry record has no name= or dir= line (record $((n + 1)) in name order)"
      continue
    fi
    n=$((n + 1))
    REC_NAMES+=("${name}")
    REC_LABEL["${name}"]="session-${n}"
    REC_DIR["${name}"]="${rdir}"
    REC_KIND["${name}"]="$(launcher_kind "${launcher}")"
    REC_COMPOSE["${name}"]="${compose:-(none)}"
    REC_FLAGS["${name}"]="${flags:-(none)}"
    REC_LIVE["${name}"]="no"
    if [[ -n "${LIVE_NAMES["${name}"]+x}" ]]; then
      REC_LIVE["${name}"]="yes"
    fi
    if [[ -z "${PROJECT_LABEL["${rdir}"]+x}" ]]; then
      PROJECT_DIRS+=("${rdir}")
      PROJECT_LABEL["${rdir}"]="project-${#PROJECT_DIRS[@]}"
    fi
  done
}

# Projects that hold at least one live ccy session: the ones whose supervisor files are read.
live_ccy_projects() {
  local d name
  for d in "${PROJECT_DIRS[@]}"; do
    for name in "${REC_NAMES[@]}"; do
      if [[ "${REC_DIR["${name}"]}" == "${d}" && "${REC_LIVE["${name}"]}" == "yes" && "${REC_KIND["${name}"]}" == "ccy" ]]; then
        printf '%s\n' "${d}"
        break
      fi
    done
  done
}

daemon_untracked() {
  if [[ -d "$1/src/claude_code_hooks_daemon" ]]; then
    printf '%s/untracked' "$1"
  else
    printf '%s/.claude/hooks-daemon/untracked' "$1"
  fi
}

# One line per kind of log line: the timestamp dropped, paths, quoted text, hex ids and
# numbers folded, so no name, path or typed text reaches the report.
normalise_lines() {
  awk '{
    $1 = ""
    gsub(/\047[^\047]*\047/, "<text>")
    gsub(/"[^"]*"/, "<text>")
    sub(/wrapping: .*/, "wrapping: <argv>")
    gsub(/\/[^ ;,)]*/, "<path>")
    gsub(/[0-9a-f]{8}-[0-9a-f-]{27,}/, "<id>")
    gsub(/[0-9]+(\.[0-9]+)?/, "N")
    print substr($0, 2, 110)
  }' | sort | uniq -c | sort -rn
}

# ── legs ───────────────────────────────────────────────────────────────────────────────────

leg_sessions() {
  local name
  emit "## sessions: the registry against the live ccy tmux server"
  emit "### READ THIS FOR: which sessions are live (have a tmux session) and which records have none"
  if [[ "${REGISTRY_ABSENT}" -eq 1 ]]; then
    emit "registry directory: absent (no session has ever been recorded for this user)"
  fi
  emit "live ccy tmux sessions: ${LIVE_COUNT}"
  emit "registry records: ${#REC_NAMES[@]}"
  emit "label       project     kind   live-tmux  compose=   recorded flags"
  for name in "${REC_NAMES[@]}"; do
    emit "$(printf '%-11s %-11s %-6s %-10s %-10s %s' "${REC_LABEL["${name}"]}" \
      "${PROJECT_LABEL["${REC_DIR["${name}"]}"]}" "${REC_KIND["${name}"]}" \
      "${REC_LIVE["${name}"]}" "${REC_COMPOSE["${name}"]}" "${REC_FLAGS["${name}"]}")"
  done
  local unrecorded=0
  for name in "${!LIVE_NAMES[@]}"; do
    [[ -n "${REC_DIR["${name}"]+x}" ]] || unrecorded=$((unrecorded + 1))
  done
  emit "live tmux sessions with no registry record: ${unrecorded}"
  emit ""
}

json_keys() {
  local out
  if ! out="$(jq -r 'if type == "object" then keys | join(",") else type end' "$1" 2>&1)"; then
    printf 'unparseable (%s)' "${out}"
    return 0
  fi
  printf '%s' "${out}"
}

files_for_project() {
  local d="$1" label untracked sup status log sidecars threads bg ccy lc out n
  label="${PROJECT_LABEL["${d}"]}"
  untracked="$(daemon_untracked "${d}")"
  sup="${untracked}/supervise"
  ccy="${d}/.claude/ccy"
  emit "### ${label}"

  status="${sup}/supervisor-status.json"
  if [[ -f "${status}" ]]; then
    emit "supervisor-status.json: age $(age_of "${status}"); keys: $(json_keys "${status}")"
    if out="$(jq -r '"version " + (.version // "?") + "; started " + ((now - (.started_at // 0)) | floor | tostring) + "s ago; plugins: " + ((.plugins // []) | map((.name // "?") + "=" + (.state // "?") + (if .reason then "(" + .reason + ")" else "" end)) | join(" "))' "${status}" 2>&1)"; then
      emit "  ${out}"
    else
      gap "${label}: supervisor-status.json unreadable: ${out}"
    fi
  else
    gap "${label}: no supervisor-status.json (no supervisor running in this project, or it never started)"
  fi
  if [[ -d "${sup}" ]]; then
    emit "supervise/ directory: modified $(age_of "${sup}") ago; entries (digits folded):"
    out="$(find "${sup}" -mindepth 1 -maxdepth 1 -printf '%f\n' | awk '{ gsub(/[0-9]+/, "N"); print }' | sort | uniq -c)"
    emit "${out:-  (empty)}"
  fi

  log="${d}/untracked/supervise/decision.log"
  if [[ -f "${log}" ]]; then
    n="$(wc -l <"${log}")"
    emit "decision.log: ${n} lines, last written $(age_of "${log}") ago"
    emit "  line kinds, whole log (count, normalised line):"
    emit "$(normalise_lines <"${log}" | awk 'NR <= 45')"
    emit "  line kinds naming a plugin, the lifecycle plugin, idle, quiet or a bus:"
    out="$(awk 'tolower($0) ~ /plugin|lifecycle|idle|quiet|bus|pending/' "${log}" | normalise_lines | awk 'NR <= 30')"
    emit "${out:-  (none)}"
  else
    gap "${label}: no decision.log at the supervisor's default place (untracked/supervise/ in the project)"
  fi

  sidecars="${untracked}/context-sidecar"
  if [[ -d "${sidecars}" ]]; then
    emit "context-sidecar/: files by kind (suffix):"
    emit "$(find "${sidecars}" -mindepth 1 -maxdepth 1 -type f -printf '%f\n' | awk '{ sub(/^[^.]*/, ""); print "  " ($0 == "" ? "(none)" : $0) }' | sort | uniq -c)"
    out="$(find "${sidecars}" -mindepth 1 -maxdepth 1 -type f -name '*.json' -printf '%T@ %p\n' | sort -rn | awk 'NR == 1 { print $2 }')"
    if [[ -n "${out}" ]]; then
      emit "  freshest *.json: age $(age_of "${out}"); keys: $(json_keys "${out}")"
    fi
  else
    emit "context-sidecar/: absent"
  fi

  threads="${untracked}/thread-registry"
  if [[ -d "${threads}" ]]; then
    n="$(find "${threads}" -mindepth 1 -maxdepth 1 -type f -name '*.json' | wc -l)"
    emit "thread-registry/: ${n} files; agent_type values:"
    out="$(find "${threads}" -mindepth 1 -maxdepth 1 -type f -name '*.json' -exec jq -r '.agent_type // "(none)"' {} + 2>&1 | sort | uniq -c)" || gap "${label}: thread-registry unreadable: ${out}"
    emit "${out:-  (none)}"
  else
    emit "thread-registry/: absent"
  fi

  bg="${untracked}/background-processes.jsonl"
  if [[ -f "${bg}" ]]; then
    n="$(wc -l <"${bg}")"
    emit "background-processes.jsonl: ${n} lines, last written $(age_of "${bg}") ago; keys of the last line: $(awk 'END { print }' "${bg}" | jq -r 'keys | join(",")' 2>&1)"
    emit "  (it records each Bash call made with run_in_background; nothing in it records an end)"
  else
    emit "background-processes.jsonl: absent"
  fi

  if [[ -d "${ccy}/tasks" ]]; then
    emit "Claude Code tasks/: $(find "${ccy}/tasks" -mindepth 1 -maxdepth 1 -type d | wc -l) session directories, $(find "${ccy}/tasks" -mindepth 2 -type f | wc -l) files, $(find "${ccy}/tasks" -mindepth 2 -type f -mmin -10 | wc -l) written in the last 10 minutes"
    out="$(find "${ccy}/tasks" -mindepth 2 -type f -printf '%f\n' | awk '{ gsub(/[0-9a-f]{6,}/, "H"); gsub(/[0-9]+/, "N"); print "  " $0 }' | sort | uniq -c | awk 'NR <= 15')"
    emit "${out:-  (no files)}"
  else
    emit "Claude Code tasks/: absent"
  fi
  if [[ -d "${ccy}/teams" ]]; then
    emit "Claude Code teams/: $(find "${ccy}/teams" -mindepth 1 -maxdepth 1 -type d | wc -l) directories, $(find "${ccy}/teams" -mindepth 1 -type f -mmin -10 | wc -l) files written in the last 10 minutes"
  fi
  if [[ -d "${ccy}/projects" ]]; then
    emit "Claude Code transcripts: main *.jsonl written in the last 2 minutes: $(find "${ccy}/projects" -mindepth 2 -maxdepth 2 -type f -name '*.jsonl' -mmin -2 | wc -l); sub-agent transcripts written in the last 2 minutes: $(find "${ccy}/projects" -mindepth 3 -type f -path '*/subagents/*' -name '*.jsonl' -mmin -2 | wc -l); sub-agent transcripts in all: $(find "${ccy}/projects" -mindepth 3 -type f -path '*/subagents/*' -name '*.jsonl' | wc -l)"
  fi
  if [[ -d "${ccy}/pingbus" ]]; then
    emit "pingbus/ under .claude/ccy (the in-container ~/.claude): entries (names folded):"
    emit "$(find "${ccy}/pingbus" -mindepth 1 -maxdepth 3 -printf '%y %d\n' | sort | uniq -c | awk '{ print "  " $1 " of type " $2 " at depth " $3 }')"
  fi

  lc="${ccy}/state/plugins/ccy-lifecycle"
  if [[ -d "${lc}" ]]; then
    emit "ccy-lifecycle state dir: $(find "${lc}" -mindepth 1 -maxdepth 1 -type f | wc -l) files, modified $(age_of "${lc}") ago; files (names folded, age):"
    out="$(find "${lc}" -mindepth 1 -maxdepth 1 -type f -printf '%f %T@\n' | awk -v now="${NOW}" '{ n = $1; gsub(/[0-9a-f]{8,}/, "H", n); gsub(/[0-9]+/, "N", n); printf "  %s written %ds ago\n", n, now - $2 }')"
    emit "${out:-  (empty)}"
  else
    emit "ccy-lifecycle state dir: absent"
  fi
  emit ""
}

leg_files() {
  local d found=0
  emit "## files: what the supervisor and its lifecycle plugin can read, per live ccy project"
  emit "### READ THIS FOR: item 1. Which of the four idle conditions has a file the supervisor"
  emit "###   writes or could read (empty prompt, no turn running, no pending bus delivery, no"
  emit "###   background task or sub-agent), and what the supervisor actually logs"
  while IFS= read -r d; do
    [[ -n "${d}" ]] || continue
    found=1
    files_for_project "${d}"
  done < <(live_ccy_projects)
  if [[ "${found}" -eq 0 ]]; then
    gap "no live ccy session in the registry: start one (any project) and re-run, the supervisor files are only meaningful for a live one"
  fi
}

container_project_label() {
  local src="$1" d real
  for d in "${PROJECT_DIRS[@]}"; do
    real="$(realpath -m "${d}")"
    if [[ "${real}" == "$(realpath -m "${src}")" ]]; then
      printf '%s' "${PROJECT_LABEL["${d}"]}"
      return 0
    fi
  done
  printf 'project-unrecorded'
}

leg_containers() {
  local raw id n=0 src label home out
  local -a ids=()
  emit "## containers: inside each running ccy container (podman exec, reading only)"
  emit "### READ THIS FOR: item 1. Whether a pending bus delivery and a background task are visible"
  emit "###   inside the container the supervisor runs in"
  if ! command -v podman >/dev/null; then
    gap "podman is not on PATH (it is installed by play-podman.yml)"
    return 0
  fi
  if ! raw="$(podman ps --filter label=ccy=true --format '{{.ID}}' 2>&1)"; then
    gap "podman ps failed: ${raw}"
    return 0
  fi
  mapfile -t ids < <(awk 'NF' <<<"${raw}")
  for id in "${ids[@]}"; do
    n=$((n + 1))
    if ! src="$(podman inspect --format '{{range .Mounts}}{{if eq .Destination "/workspace"}}{{.Source}}{{end}}{{end}}' "${id}" 2>&1)"; then
      gap "container-${n}: podman inspect failed: ${src}"
      src=""
    fi
    label="$(container_project_label "${src}")"
    emit "### container-${n} (${label})"
    # printenv exits 1 for an unset variable, which is the answer "on no bus".
    if ! home="$(podman exec "${id}" printenv PINGBUS_HOME 2>&1)"; then
      home=""
    fi
    case "${home}" in
      "") emit "PINGBUS_HOME: unset (this session is on no bus)" ;;
      /tmp/*) emit "PINGBUS_HOME: under /tmp in the container (container-local, not on the project mount)" ;;
      /workspace/*) emit "PINGBUS_HOME: under /workspace (on the project mount, visible from the host)" ;;
      *) emit "PINGBUS_HOME: elsewhere in the container" ;;
    esac
    if [[ -n "${home}" ]]; then
      if out="$(podman exec "${id}" find "${home}" -path '*/state/inbox/*' -type f 2>&1)"; then
        emit "pending bus items (files under <PINGBUS_HOME>/<team>/state/inbox/): $(count_with '' <<<"${out}")"
      else
        gap "container-${n}: the inbox could not be listed: ${out}"
      fi
    fi
    if out="$(podman exec "${id}" ps -eo pid=,ppid=,comm=,args= 2>&1)"; then
      emit "supervisor processes: $(count_with 'claude-supervise' <<<"${out}"), of which with --plugin: $(awk 'index($0, "claude-supervise") && index($0, "--plugin")' <<<"${out}" | count_with '')"
      emit "processes under each claude process (count, command name): a background Bash task is a child here; a sub-agent is not a process"
      emit "$(awk '{ pid[NR] = $1; ppid[NR] = $2; comm[NR] = $3; if ($3 == "claude") claude[$1] = 1 }
        END {
          changed = 1
          while (changed) { changed = 0
            for (i = 1; i <= NR; i++) if ((ppid[i] in claude) && !(pid[i] in under) && !(pid[i] in claude)) { under[pid[i]] = 1; claude[pid[i]] = 1; changed = 1 } }
          for (i = 1; i <= NR; i++) if (pid[i] in under) c[comm[i]]++
          k = 0; for (x in c) { printf "  %d %s\n", c[x], x; k++ }
          if (k == 0) print "  (none)"
        }' <<<"${out}")"
    else
      gap "container-${n}: ps failed in the container: ${out}"
    fi
  done
  if [[ "${n}" -eq 0 ]]; then
    gap "no running ccy container (label ccy=true): start a ccy session and re-run"
  fi
  emit ""
}

leg_listing() {
  local scratch fake="triage-00256-no-tmux-probe" out rc=0
  emit "## listing: do ccy-sessions --list and notify leave out a session with no tmux session?"
  emit "### READ THIS FOR: item 3"
  if ! command -v ccy-sessions >/dev/null; then
    gap "ccy-sessions is not on PATH (it is built by play-claude-yolo.yml)"
    return 0
  fi
  # A scratch registry holding one record with no tmux session, beside the real live
  # sessions. ccy-sessions reads it through CCY_STATE_DIR; nothing else does.
  scratch="${RUN_DIR}/scratch-registry"
  mkdir -p "${scratch}/sessions" "${scratch}/probe-project"
  printf 'ccy-session-record 1\nname=%s\ndir=%s\nlauncher=/usr/local/bin/ccy\nprefix=ccy\nrestore=yes\ncompose=started\n' \
    "${fake}" "${scratch}/probe-project" >"${scratch}/sessions/${fake}"
  if [[ -n "${LIVE_NAMES["${fake}"]+x}" ]]; then
    gap "a live tmux session is named ${fake}: the probe cannot tell it from the scratch record"
    return 0
  fi

  out="$(CCY_STATE_DIR="${scratch}" ccy-sessions --list 2>&1)" || rc=$?
  printf '%s\n' "${out}" >"${RUN_DIR}/raw-list-scratch.txt"
  emit "ccy-sessions --list with the scratch registry: exit ${rc}; rows: $(list_rows <<<"${out}") (live ccy tmux sessions: ${LIVE_COUNT})"
  emit "  the no-tmux record is listed: $(yes_if_contains "${fake}" <<<"${out}")"
  rc=0
  out="$(ccy-sessions --list 2>&1)" || rc=$?
  emit "ccy-sessions --list with the real registry: exit ${rc}; rows: $(list_rows <<<"${out}")"
  printf '%s\n' "${out}" >"${RUN_DIR}/raw-list-real.txt"
  local name missing=0
  for name in "${REC_NAMES[@]}"; do
    if [[ "${REC_LIVE["${name}"]}" == "no" ]]; then
      missing=$((missing + 1))
      emit "  real record ${REC_LABEL["${name}"]} has no tmux session; listed: $(yes_if_contains "${name}" <<<"${out}")"
    fi
  done
  emit "  real records with no tmux session: ${missing}"

  rc=0
  out="$(CCY_STATE_DIR="${scratch}" ccy-sessions notify reboot-warning --minutes 1 --dry-run 2>&1)" || rc=$?
  printf '%s\n' "${out}" >"${RUN_DIR}/raw-notify-scratch.txt"
  emit "ccy-sessions notify reboot-warning --minutes 1 --dry-run, scratch registry: exit ${rc}"
  emit "  would-signal lines: $(count_starting 'would signal' <<<"${out}"); 'Left out' notes: $(count_starting 'Left out' <<<"${out}")"
  emit "  the no-tmux record is named in its output: by name $(yes_if_contains "${fake}" <<<"${out}"), by directory $(yes_if_contains "${scratch}/probe-project" <<<"${out}")"
  emit "  (raw outputs, which hold names: raw-list-*.txt and raw-notify-scratch.txt in the run directory)"
  emit ""
}

compose_probe() {
  local label="$1" out rc=0
  shift
  out="$("$@" 2>&1)" || rc=$?
  emit "### ${label}  (rc=${rc})"
  emit "${out:-(no output)}"
  emit ""
}

leg_compose() {
  local tool found=0
  emit "## compose: does 'stop' exist, and what does each tool say it does?"
  emit "### READ THIS FOR: item 4. Compare each tool's 'stop' help with its 'down' help (down"
  emit "###   removes containers and networks; stop is expected to keep them)"
  compose_probe "installed packages" rpm -q podman podman-compose docker-compose docker-compose-plugin docker-ce-cli moby-engine
  for tool in podman-compose docker-compose; do
    if command -v "${tool}" >/dev/null; then
      found=1
      compose_probe "${tool} version" "${tool}" version
      compose_probe "${tool} stop --help" "${tool}" stop --help
      compose_probe "${tool} down --help" "${tool}" down --help
    else
      emit "### ${tool}: not on PATH"
      emit ""
    fi
  done
  if command -v docker >/dev/null; then
    compose_probe "docker compose version" docker compose version
    compose_probe "docker compose stop --help" docker compose stop --help
  else
    emit "### docker: not on PATH"
    emit ""
  fi
  if command -v podman >/dev/null; then
    compose_probe "podman compose version (the provider it delegates to)" podman compose version
  fi
  if [[ "${found}" -eq 0 ]]; then
    gap "neither podman-compose nor docker-compose is on PATH (podman-compose is installed by play-podman.yml)"
  fi
}

leg_sample() {
  local seconds="${SAMPLE_SECONDS}" d i=0 end now m start
  local -a dirs=() sups=() logs=() sides=() starts=() last_sup=() last_side=() marker=()
  emit "## sample: ${seconds}s of 0.25s samples per live ccy project"
  emit "### READ THIS FOR: item 2. The supervise/ directory changes on every plugin hook call"
  emit "###   (the in-hook marker is created and removed there), so the gaps between its changes"
  emit "###   bound how often on_idle runs. It also changes when status-message.json is written."
  emit "###   Leave the sessions alone while it runs; a typed key or a turn shows in decision.log."
  if [[ ! "${seconds}" =~ ^[1-9][0-9]*$ ]] || [[ "${seconds}" -gt 3600 ]]; then
    gap "--sample takes whole seconds, 1 to 3600; got '${seconds}'"
    return 0
  fi
  while IFS= read -r d; do
    [[ -n "${d}" ]] || continue
    dirs+=("${d}")
    sups+=("$(daemon_untracked "${d}")/supervise")
    sides+=("$(daemon_untracked "${d}")/context-sidecar")
    logs+=("${d}/untracked/supervise/decision.log")
    start=0
    if [[ -f "${d}/untracked/supervise/decision.log" ]]; then
      start="$(wc -l <"${d}/untracked/supervise/decision.log")"
    fi
    starts+=("${start}")
    last_sup+=("")
    last_side+=("")
    marker+=(0)
    : >"${RUN_DIR}/sample-${#dirs[@]}.sup"
    : >"${RUN_DIR}/sample-${#dirs[@]}.side"
  done < <(live_ccy_projects)
  if [[ "${#dirs[@]}" -eq 0 ]]; then
    gap "no live ccy session to sample"
    return 0
  fi
  end=$(($(date +%s) + seconds))
  while now="$(date +%s)" && [[ "${now}" -lt "${end}" ]]; do
    for i in "${!dirs[@]}"; do
      if [[ -d "${sups[i]}" ]]; then
        m="$(stat -c %.9Y "${sups[i]}")"
        if [[ "${m}" != "${last_sup[i]}" ]]; then
          printf '%s\n' "${m}" >>"${RUN_DIR}/sample-$((i + 1)).sup"
          last_sup[i]="${m}"
        fi
        if compgen -G "${sups[i]}/plugin-in-hook.*.json" >/dev/null; then
          marker[i]=$((marker[i] + 1))
        fi
      fi
      if [[ -d "${sides[i]}" ]]; then
        m="$(stat -c %.9Y "${sides[i]}")"
        if [[ "${m}" != "${last_side[i]}" ]]; then
          printf '%s\n' "${m}" >>"${RUN_DIR}/sample-$((i + 1)).side"
          last_side[i]="${m}"
        fi
      fi
    done
    sleep 0.25
  done
  for i in "${!dirs[@]}"; do
    emit "### ${PROJECT_LABEL["${dirs[i]}"]} (live ccy sessions in it: $(count_live_in "${dirs[i]}"))"
    m="$(jq -r '(.plugins // []) | map((.name // "?") + "=" + (.state // "?")) | join(" ") | if . == "" then "(none named)" else . end' "${sups[i]}/supervisor-status.json" 2>&1)" || m="unreadable supervisor-status.json: ${m}"
    emit "plugins now: ${m} (the marker is written only while a plugin is loaded and enabled)"
    emit "supervise/ changes (the first gap runs from the last change before the window):"
    emit "  $(gaps <"${RUN_DIR}/sample-$((i + 1)).sup")"
    emit "  samples that caught an in-hook marker present: ${marker[i]}"
    emit "context-sidecar/ changes (a status-line render, which comes with a turn):"
    emit "  $(gaps <"${RUN_DIR}/sample-$((i + 1)).side")"
    if [[ -f "${logs[i]}" ]]; then
      emit "decision.log lines written during the window (kinds):"
      m="$(awk -v s="${starts[i]}" 'NR > s' "${logs[i]}" | normalise_lines)"
      emit "${m:-  (none)}"
    fi
  done
  emit ""
}

count_live_in() {
  local name n=0
  for name in "${REC_NAMES[@]}"; do
    if [[ "${REC_DIR["${name}"]}" == "$1" && "${REC_LIVE["${name}"]}" == "yes" ]]; then
      n=$((n + 1))
    fi
  done
  printf '%d' "${n}"
}

# The count of changes after the first observed value and the min, median and max gap between
# consecutive changes, in seconds.
gaps() {
  awk 'NR == 1 { prev = $1; next }
    { g[++n] = $1 - prev; prev = $1 }
    END {
      if (n == 0) { print "no change in the window"; exit }
      for (i = 1; i <= n; i++) for (j = i + 1; j <= n; j++) if (g[j] < g[i]) { t = g[i]; g[i] = g[j]; g[j] = t }
      printf "%d changes; gap between changes: min %.2fs, median %.2fs, max %.2fs", n, g[1], g[int((n + 1) / 2)], g[n]
    }'
}

load_registry
case "${MODE}" in
  sessions) leg_sessions ;;
  files) leg_files ;;
  containers) leg_containers ;;
  listing) leg_listing ;;
  compose) leg_compose ;;
  sample) leg_sample ;;
  *)
    printf '%s\n' "${USAGE}" >&2
    exit 2
    ;;
esac
exit "${FAILED}"
