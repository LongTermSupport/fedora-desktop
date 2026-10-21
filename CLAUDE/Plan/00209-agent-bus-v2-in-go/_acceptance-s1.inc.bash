# shellcheck shell=bash
# _acceptance-s1.inc.bash — the checks of acceptance-s1.bash (Plan 00209 stage 1 on the host). A
# separate file because the checks are called through plan_gather_leg, which ShellCheck cannot
# see through (SC2329), and suppression is banned. Sourced, never executed: no shell options, no
# `exit`. Every check returns non-zero with a message on stderr when it does not pass, and appends
# what it found to REPORT. The caller sets INSTALLED, NEW_REF, OLD_BIN, SHIMS, RUNS,
# REAL_AGENT_BUS, REAL_GUM and REPORT. A check reads what an earlier one left (ROOT_DIR_BAKED,
# OLD_REV, the run files) and refuses, named, when that one failed.

ROOT_DIR_BAKED=""
OLD_REV=""

# say_report <text...> — one line on stdout and in the report.
say_report() {
    printf '%s\n' "$*" | tee -a "${REPORT}"
}

# micros <EPOCHREALTIME> — the time in microseconds (the locale may write a comma).
micros() {
    local t="${1//[.,]/}"
    printf '%d\n' "$((10#${t}))"
}

# median_of <file> — the median of the integers in file, one a line.
median_of() {
    sort -n "$1" | awk '{v[NR] = $1} END {if (NR == 0) exit 1; print v[int((NR + 1) / 2)]}'
}

# count_of <file> <line> — how many lines of file are exactly line.
count_of() {
    awk -v want="$2" '$0 == want {n++} END {print n + 0}' "$1"
}

# build_ccy_teams <src-go dir> <out> — ccy-teams built as tasks/build-go-command.yml builds it
# (play-claude-yolo.yml's ldflags; -trimpath and -buildvcs=false make the build reproducible).
build_ccy_teams() {
    (
        cd "$1" &&
            env GOTOOLCHAIN=local GOFLAGS=-mod=vendor \
                go build -trimpath -buildvcs=false -ldflags "-X main.RootDir=${ROOT_DIR_BAKED}" -o "$2" ./cmd/ccy-teams
    )
}

# check_installed — the installed ccy-teams is this checkout's build, and this checkout has
# stage 1 (the in-process seat reader).
check_installed() {
    if [[ ! -d "${PLAN_REPO_ROOT}/src-go/internal/bus/seat" ]]; then
        printf 'this checkout has no src-go/internal/bus/seat: Plan 00209 stage 1 is not in it\n' >&2
        return 1
    fi
    if [[ ! -f "${INSTALLED}" || -L "${INSTALLED}" ]]; then
        printf '%s is missing or not a regular file: play-claude-yolo.yml has not installed ccy-teams\n' "${INSTALLED}" >&2
        return 1
    fi
    # -trimpath leaves -ldflags out of the build information, so the baked-in checkout is looked
    # for as text: the play bakes in the checkout it runs from, this one.
    if ! grep -qaF -- "${PLAN_REPO_ROOT}" "${INSTALLED}"; then
        printf '%s does not name this checkout (%s): it was built from another one\n' "${INSTALLED}" "${PLAN_REPO_ROOT}" >&2
        return 1
    fi
    ROOT_DIR_BAKED="${PLAN_REPO_ROOT}"
    build_ccy_teams "${PLAN_REPO_ROOT}/src-go" "${NEW_REF}" || return 1
    say_report "- installed: ${INSTALLED} $(sha256sum "${INSTALLED}" | cut -d' ' -f1)"
    say_report "- this checkout's build: $(sha256sum "${NEW_REF}" | cut -d' ' -f1)"
    if ! cmp -s "${NEW_REF}" "${INSTALLED}"; then
        printf '%s differs from this checkout'\''s build: play-claude-yolo.yml has not run since the checkout changed\n' "${INSTALLED}" >&2
        printf '(or the play baked this checkout in under another spelling of its path than %s)\n' "${PLAN_REPO_ROOT}" >&2
        ROOT_DIR_BAKED=""
        return 1
    fi
}

# build_old — the ccy-teams from before stage 1, built the same way: the first parent of the
# first commit on HEAD's first-parent line that brings src-go/internal/bus in (stage 1's merge).
build_old() {
    local revs first tree="${PLAN_RUN_DIR}/old-tree"
    if [[ -z "${ROOT_DIR_BAKED}" ]]; then
        printf 'not built: the installed binary was not shown to be this checkout'\''s (above)\n' >&2
        return 1
    fi
    revs="$(git -C "${PLAN_REPO_ROOT}" rev-list --first-parent --reverse HEAD -- src-go/internal/bus)" || return 1
    first="${revs%%$'\n'*}"
    if [[ -z "${first}" ]]; then
        printf 'no commit on the first-parent line of HEAD brings src-go/internal/bus in\n' >&2
        return 1
    fi
    OLD_REV="$(git -C "${PLAN_REPO_ROOT}" rev-parse --verify "${first}^1")" || return 1
    if [[ -n "$(git -C "${PLAN_REPO_ROOT}" ls-tree --name-only "${OLD_REV}" src-go/internal/bus)" ]]; then
        printf '%s already has src-go/internal/bus: it is not a revision from before stage 1\n' "${OLD_REV}" >&2
        OLD_REV=""
        return 1
    fi
    mkdir -p "${tree}" || return 1
    git -C "${PLAN_REPO_ROOT}" archive "${OLD_REV}" src-go | tar -x -C "${tree}" || return 1
    build_ccy_teams "${tree}/src-go" "${OLD_BIN}" || return 1
    say_report "- before stage 1: ${OLD_REV} (the first parent of ${first}), built $(sha256sum "${OLD_BIN}" | cut -d' ' -f1)"
}

# binary_of <old|new> — the binary a run of that side starts: the build from before stage 1, or
# the installed one.
binary_of() {
    if [[ "$1" == "new" ]]; then
        printf '%s\n' "${INSTALLED}"
    else
        printf '%s\n' "${OLD_BIN}"
    fi
}

# binaries_ready — both builds exist (the legs above passed).
binaries_ready() {
    if [[ -z "${ROOT_DIR_BAKED}" || -z "${OLD_REV}" ]]; then
        printf 'not run: the installed or the old binary is not ready (a leg above failed)\n' >&2
        return 1
    fi
}

# timing_verdict <what> <old ms file> <new ms file> — both medians reported; new must be faster.
timing_verdict() {
    local old new
    old="$(median_of "$2")" || return 1
    new="$(median_of "$3")" || return 1
    say_report "- ${1}, median of ${RUNS}: before ${old} ms, after ${new} ms (before: $(paste -sd' ' "$2"); after: $(paste -sd' ' "$3"))"
    if ((new >= old)); then
        printf '%s is no faster after stage 1: %s ms against %s ms before\n' "$1" "${new}" "${old}" >&2
        return 1
    fi
}

# check_list — `--list` from the old and the new binary, run in turn RUNS times: each pair prints
# the same bytes with the same exit code, and the new one is faster.
check_list() {
    local i tag bin dir t0 t1 code bad=0
    binaries_ready || return 1
    : >"${PLAN_RUN_DIR}/list-old.ms"
    : >"${PLAN_RUN_DIR}/list-new.ms"
    for ((i = 1; i <= RUNS; i++)); do
        for tag in old new; do
            bin="$(binary_of "${tag}")"
            dir="${PLAN_RUN_DIR}/list/${tag}-${i}"
            mkdir -p "${dir}" || return 1
            code=0
            t0="$(micros "${EPOCHREALTIME}")"
            "${bin}" --list </dev/null >"${dir}/stdout" 2>"${dir}/stderr" || code=$?
            t1="$(micros "${EPOCHREALTIME}")"
            printf '%s\n' "${code}" >"${dir}/exit"
            printf '%s\n' "$(((t1 - t0) / 1000))" >>"${PLAN_RUN_DIR}/list-${tag}.ms"
        done
        if ! cmp -s "${PLAN_RUN_DIR}/list/old-${i}/exit" "${PLAN_RUN_DIR}/list/new-${i}/exit"; then
            say_report "- run ${i}: --list exited $(<"${PLAN_RUN_DIR}/list/old-${i}/exit") before, $(<"${PLAN_RUN_DIR}/list/new-${i}/exit") after"
            bad=1
        fi
        if ! diff -u "${PLAN_RUN_DIR}/list/old-${i}/stdout" "${PLAN_RUN_DIR}/list/new-${i}/stdout" >>"${REPORT}"; then
            say_report "- run ${i}: --list printed differently (the diff is above in the report)"
            bad=1
        fi
    done
    if ((bad)); then
        printf -- '--list differs between the old and the new binary: read the diff in %s. An age word crossing\n' "${REPORT}" >&2
        printf 'its boundary between two runs a second apart shows as a difference too; any other is not a departure\n' >&2
        printf 'stage 1 allows (SPEC-view.md B-S1 to B-S6 cover values no v1 writer produces)\n' >&2
    else
        say_report "- --list: the same bytes and exit code before and after, in each of ${RUNS} runs"
    fi
    timing_verdict "--list" "${PLAN_RUN_DIR}/list-old.ms" "${PLAN_RUN_DIR}/list-new.ms" || bad=1
    return "${bad}"
}

# open_once <binary> <dir> — one open on this terminal, with the shims first on PATH: the
# processes it starts in dir/procs, the first screen's stamp in dir/ui, and the time to that
# screen in microseconds on stdout. Exits 130 at the first screen, as Esc does.
open_once() {
    local bin="$1" dir="$2" t0 code=0
    mkdir -p "${dir}" || return 1
    : >"${dir}/procs"
    t0="$(micros "${EPOCHREALTIME}")"
    S1_SHIM_LOG="${dir}/procs" S1_UI_MARK="${dir}/ui" S1_REAL_AGENT_BUS="${REAL_AGENT_BUS}" S1_REAL_GUM="${REAL_GUM}" \
        PATH="${SHIMS}:${PATH}" timeout --foreground 300 "${bin}" </dev/tty >/dev/tty 2>"${dir}/stderr" || code=$?
    printf '%s\n' "${code}" >"${dir}/exit"
    if [[ ! -s "${dir}/ui" ]]; then
        printf 'the open of %s showed no screen (exit %s); its stderr is %s\n' "${bin}" "${code}" "${dir}/stderr" >&2
        return 1
    fi
    printf '%s\n' "$(($(micros "$(cut -d' ' -f1 "${dir}/ui")") - t0))"
}

# check_open — an open of each binary, in turn RUNS times, timed to its first screen. Each new
# open starts exactly one declare.py (status) and no agent-bus seat list; the old opens start a
# seat list and more than one declare.py, which proves the shims see those processes.
check_open() {
    local i tag bin dir us screen declares seatlists bad=0 proven=1
    binaries_ready || return 1
    if ! { : </dev/tty; } 2>>"${REPORT}"; then
        printf 'no terminal to open ccy-teams on: run meta-deploy.bash from a terminal\n' >&2
        return 1
    fi
    mkdir -p "${SHIMS}" || return 1
    for tag in env agent-bus fzf gum; do
        ln -sfn "${PLAN_SCRIPT_DIR}/_s1-shim.bash" "${SHIMS}/${tag}" || return 1
    done
    : >"${PLAN_RUN_DIR}/open-old.ms"
    : >"${PLAN_RUN_DIR}/open-new.ms"
    for ((i = 1; i <= RUNS; i++)); do
        for tag in old new; do
            bin="$(binary_of "${tag}")"
            dir="${PLAN_RUN_DIR}/open/${tag}-${i}"
            us="$(open_once "${bin}" "${dir}")" || return 1
            printf '%s\n' "$((us / 1000))" >>"${PLAN_RUN_DIR}/open-${tag}.ms"
            screen="$(cut -d' ' -f2- "${dir}/ui")"
            declares="$(awk '/^declare / {n++} END {print n + 0}' "${dir}/procs")"
            seatlists="$(count_of "${dir}/procs" "agent-bus seat list")"
            say_report "- open ${tag} ${i}: first screen ${screen}, $((us / 1000)) ms; declare.py runs: ${declares}, agent-bus seat list runs: ${seatlists}"
            if [[ "${screen%% *}" != "fzf" ]]; then
                printf 'open %s %s asked a question before the table (%s): answer it in ccy-teams, then run again\n' "${tag}" "${i}" "${screen}" >&2
                bad=1
            fi
            if [[ "${tag}" == "new" ]]; then
                if [[ "${declares}" != 1 || "$(count_of "${dir}/procs" "declare status")" != 1 || "${seatlists}" != 0 ]]; then
                    printf 'open new %s started %s declare.py runs and %s seat lists; want one declare status and none (%s)\n' \
                        "${i}" "${declares}" "${seatlists}" "${dir}/procs" >&2
                    bad=1
                fi
            elif ((declares < 2 || seatlists < 1)); then
                proven=0
            fi
        done
    done
    if ((!proven)); then
        printf 'an old open started fewer than two declare.py runs or no seat list, so the shims are not shown to see\n' >&2
        printf 'them (is there no recorded ccy session with seats?): the process count is not judged\n' >&2
        bad=1
    fi
    timing_verdict "open, to the first screen" "${PLAN_RUN_DIR}/open-old.ms" "${PLAN_RUN_DIR}/open-new.ms" || bad=1
    return "${bad}"
}

# check_report_per_hour — every seat whose live member.json sets limits.report_per_hour, in a
# checkout a session record names, is listed by the new --list (its handle on stdout) and not
# refused (no "seat <seat>@<team>:" line on stderr). At least one such seat must exist.
check_report_per_hour() {
    local records rec line dir member text seat team handle judged=0 bad=0
    local out="${PLAN_RUN_DIR}/list/new-1/stdout" err="${PLAN_RUN_DIR}/list/new-1/stderr"
    local re='"user_id"[[:space:]]*:[[:space:]]*"@([^":]+):'
    local -A checkouts=()
    if [[ ! -f "${out}" || ! -f "${err}" ]]; then
        printf 'not judged: the new --list did not run (the --list leg failed first)\n' >&2
        return 1
    fi
    records="${CCY_STATE_DIR:-${XDG_STATE_HOME:-${HOME}/.local/state}/ccy}/sessions"
    if [[ ! -d "${records}" ]]; then
        printf 'no session records at %s: no checkout to look in\n' "${records}" >&2
        return 1
    fi
    for rec in "${records}"/*; do
        [[ -f "${rec}" ]] || continue
        while IFS= read -r line; do
            [[ "${line}" == dir=* ]] && checkouts["${line#dir=}"]=1
        done <"${rec}"
    done
    for dir in "${!checkouts[@]}"; do
        for member in "${dir}"/.claude/ccy/pingbus/seats/*/*/member.json; do
            [[ -f "${member}" ]] || continue
            text="$(<"${member}")"
            [[ "${text}" == *'"report_per_hour"'* ]] || continue
            seat="$(basename "$(dirname "${member}")")"
            team="$(basename "$(dirname "$(dirname "${member}")")")"
            if [[ ! "${text}" =~ ${re} ]]; then
                printf '%s sets report_per_hour but names no user_id handle\n' "${member}" >&2
                bad=1
                continue
            fi
            handle="${BASH_REMATCH[1]}"
            judged=$((judged + 1))
            if grep -qF "seat ${seat}@${team}: " "${err}"; then
                say_report "- seat ${seat}@${team} (${handle}): REFUSED by the new --list (see ${err})"
                bad=1
            elif ! grep -qF "${handle}" "${out}"; then
                say_report "- seat ${seat}@${team} (${handle}): not on the new --list's table"
                bad=1
            else
                say_report "- seat ${seat}@${team} (${handle}) sets report_per_hour and is listed"
            fi
        done
    done
    if ((judged == 0)); then
        printf 'no live member.json in a recorded checkout sets limits.report_per_hour: not judged\n' >&2
        return 1
    fi
    return "${bad}"
}
