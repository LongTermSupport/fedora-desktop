# shellcheck shell=bash
# _acceptance-s1.inc.bash — the checks of acceptance-s1.bash (Plan 00209 stage 1 on the host). A
# separate file because the checks are called through plan_gather_leg, which ShellCheck cannot
# see through (SC2329), and suppression is banned. Sourced, never executed: no shell options, no
# `exit`. Every check appends what it found to REPORT and, when it does not pass, says why on
# stderr and returns 1 (failed) or 2 (not judged: nothing failed, but there was no evidence to
# judge on; run under `judge`). plan_gather_leg runs each with `set -e` off, so every step is
# checked by hand. The caller sets INSTALLED, NEW_REF, OLD_BIN, SHIMS, RUNS, REAL_AGENT_BUS,
# REAL_GUM and REPORT. A check reads what an earlier one left (ROOT_DIR_BAKED, OLD_REV, the run
# files) and refuses, named, when that one failed.

ROOT_DIR_BAKED=""
OLD_REV=""
NOT_JUDGED=""

# say_report <text...> — one line on stdout and in the report.
say_report() {
    printf '%s\n' "$*" | tee -a "${REPORT}"
}

# judge <leg> <check...> — a check run by plan_gather_leg: its 2 is recorded in NOT_JUDGED, not
# as a failed leg, so the run can end NOT JUDGED (exit 2) rather than REJECTED.
judge() {
    local leg="$1" status=0
    shift
    "$@" || status=$?
    if ((status == 2)); then
        NOT_JUDGED="${NOT_JUDGED}${NOT_JUDGED:+, }${leg}"
        say_report "- ${leg}: NOT JUDGED (the reason is in the run log)" || return 1
        return 0
    fi
    return "${status}"
}

# load_now — the load averages over 1, 5 and 15 minutes, for the report beside a timing.
load_now() {
    cut -d' ' -f1-3 /proc/loadavg
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
    local installedSum newSum
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
    installedSum="$(sha256sum "${INSTALLED}")" || return 1
    newSum="$(sha256sum "${NEW_REF}")" || return 1
    say_report "- installed: ${INSTALLED} ${installedSum%% *}" || return 1
    say_report "- this checkout's build: ${newSum%% *}" || return 1
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
    local revs first oldSum tree="${PLAN_RUN_DIR}/old-tree"
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
    oldSum="$(sha256sum "${OLD_BIN}")" || return 1
    say_report "- before stage 1: ${OLD_REV} (the first parent of ${first}), built ${oldSum%% *}" || return 1
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

# timing_verdict <what> <old ms file> <new ms file> — both medians reported, with the load they
# were taken under; new must be faster.
timing_verdict() {
    local old new oldAll newAll load
    old="$(median_of "$2")" || return 1
    new="$(median_of "$3")" || return 1
    oldAll="$(paste -sd' ' "$2")" || return 1
    newAll="$(paste -sd' ' "$3")" || return 1
    load="$(load_now)" || return 1
    say_report "- ${1}, median of ${RUNS}: before ${old} ms, after ${new} ms (before: ${oldAll}; after: ${newAll}; load average now ${load})" || return 1
    if ((new >= old)); then
        printf '%s is no faster after stage 1: %s ms against %s ms before\n' "$1" "${new}" "${old}" >&2
        return 1
    fi
}

# run_order <round> — the order a round runs the two binaries in: old first in odd rounds, new
# first in even ones, so neither is always the one that finds the caches warm.
run_order() {
    if (($1 % 2)); then
        printf '%s\n' old new
    else
        printf '%s\n' new old
    fi
}

# check_list — `--list` from the old and the new binary, a pair a round for RUNS rounds: a pair
# that differs in exit code or bytes in every round is a departure, and the new one is faster.
# A pair that differs in some rounds only is a note, not a departure: the rounds whose pairs
# match show the two print the same for the same state, and the others caught a live change (a
# member's state, an age word, a held seat) between the halves of a pair, which takes 9-23 s.
check_list() {
    local i tag bin dir t0 t1 code same oldExit newExit diffStatus differing=0 bad=0
    binaries_ready || return 1
    : >"${PLAN_RUN_DIR}/list-old.ms" || return 1
    : >"${PLAN_RUN_DIR}/list-new.ms" || return 1
    for ((i = 1; i <= RUNS; i++)); do
        for tag in $(run_order "${i}"); do
            bin="$(binary_of "${tag}")" || return 1
            dir="${PLAN_RUN_DIR}/list/${tag}-${i}"
            mkdir -p "${dir}" || return 1
            code=0
            t0="$(micros "${EPOCHREALTIME}")" || return 1
            "${bin}" --list </dev/null >"${dir}/stdout" 2>"${dir}/stderr" || code=$?
            t1="$(micros "${EPOCHREALTIME}")" || return 1
            printf '%s\n' "${code}" >"${dir}/exit" || return 1
            printf '%s\n' "$(((t1 - t0) / 1000))" >>"${PLAN_RUN_DIR}/list-${tag}.ms" || return 1
        done
        same=1
        oldExit="$(<"${PLAN_RUN_DIR}/list/old-${i}/exit")" || return 1
        newExit="$(<"${PLAN_RUN_DIR}/list/new-${i}/exit")" || return 1
        if [[ "${oldExit}" != "${newExit}" ]]; then
            say_report "- round ${i}: --list exited ${oldExit} before, ${newExit} after" || return 1
            same=0
        fi
        diffStatus=0
        diff -u "${PLAN_RUN_DIR}/list/old-${i}/stdout" "${PLAN_RUN_DIR}/list/new-${i}/stdout" >>"${REPORT}" || diffStatus=$?
        if ((diffStatus > 1)); then
            printf 'diff could not compare round %s'\''s two --list outputs\n' "${i}" >&2
            return 1
        fi
        if ((diffStatus == 1)); then
            say_report "- round ${i}: --list printed differently (the diff is above in the report)" || return 1
            same=0
        fi
        ((same)) || differing=$((differing + 1))
    done
    if ((differing == RUNS)); then
        printf -- '--list differs between the old and the new binary in every one of %s rounds: read the diffs in %s.\n' "${RUNS}" "${REPORT}" >&2
        printf 'A difference that repeats is a departure stage 1 does not allow (SPEC-view.md B-S1 to B-S6 cover values no v1 writer produces)\n' >&2
        bad=1
    elif ((differing > 0)); then
        say_report "- --list: the same bytes and exit code before and after in $((RUNS - differing)) of ${RUNS} rounds; the other ${differing} caught a live change between the halves of a pair (a note, not a departure)" || return 1
    else
        say_report "- --list: the same bytes and exit code before and after, in each of ${RUNS} rounds" || return 1
    fi
    timing_verdict "--list" "${PLAN_RUN_DIR}/list-old.ms" "${PLAN_RUN_DIR}/list-new.ms" || bad=1
    return "${bad}"
}

# open_once <binary> <dir> — one open on this terminal, with the shims first on PATH: the
# processes it starts in dir/procs, the first screen's stamp in dir/ui, and the time to that
# screen in microseconds on stdout. Exits 130 at the first screen, as Esc does.
open_once() {
    local bin="$1" dir="$2" t0 stamp code=0
    mkdir -p "${dir}" || return 1
    : >"${dir}/procs" || return 1
    t0="$(micros "${EPOCHREALTIME}")" || return 1
    S1_SHIM_LOG="${dir}/procs" S1_UI_MARK="${dir}/ui" S1_REAL_AGENT_BUS="${REAL_AGENT_BUS}" S1_REAL_GUM="${REAL_GUM}" \
        PATH="${SHIMS}:${PATH}" timeout --foreground 300 "${bin}" </dev/tty >/dev/tty 2>"${dir}/stderr" || code=$?
    printf '%s\n' "${code}" >"${dir}/exit" || return 1
    if [[ ! -s "${dir}/ui" ]]; then
        printf 'the open of %s showed no screen (exit %s); its stderr is %s\n' "${bin}" "${code}" "${dir}/stderr" >&2
        return 1
    fi
    stamp="$(cut -d' ' -f1 "${dir}/ui")" || return 1
    stamp="$(micros "${stamp}")" || return 1
    printf '%s\n' "$((stamp - t0))"
}

# check_open — an open of each binary, a pair a round for RUNS rounds, timed to its first
# screen. Each new open starts exactly one declare.py (status) and no agent-bus seat list; the
# old opens start a seat list and more than one declare.py, which proves the shims see those
# processes. An open that asks a question first, or old opens that do not show the shims seeing
# them, leave the open not judged (2) unless something failed.
check_open() {
    local i tag bin dir us screen declares statuses seatlists ttyErr load bad=0 proven=1
    binaries_ready || return 1
    if ! ttyErr="$({ : </dev/tty; } 2>&1)"; then
        printf 'no terminal to open ccy-teams on (%s): run meta-deploy.bash from a terminal; the open is not judged\n' "${ttyErr}" >&2
        return 2
    fi
    mkdir -p "${SHIMS}" || return 1
    for tag in env agent-bus fzf gum; do
        ln -sfn "${PLAN_SCRIPT_DIR}/_s1-shim.bash" "${SHIMS}/${tag}" || return 1
    done
    : >"${PLAN_RUN_DIR}/open-old.ms" || return 1
    : >"${PLAN_RUN_DIR}/open-new.ms" || return 1
    for ((i = 1; i <= RUNS; i++)); do
        for tag in $(run_order "${i}"); do
            bin="$(binary_of "${tag}")" || return 1
            dir="${PLAN_RUN_DIR}/open/${tag}-${i}"
            us="$(open_once "${bin}" "${dir}")" || return 1
            printf '%s\n' "$((us / 1000))" >>"${PLAN_RUN_DIR}/open-${tag}.ms" || return 1
            screen="$(cut -d' ' -f2- "${dir}/ui")" || return 1
            declares="$(awk '/^declare / {n++} END {print n + 0}' "${dir}/procs")" || return 1
            statuses="$(count_of "${dir}/procs" "declare status")" || return 1
            seatlists="$(count_of "${dir}/procs" "agent-bus seat list")" || return 1
            load="$(load_now)" || return 1
            say_report "- open ${tag} ${i}: first screen ${screen}, $((us / 1000)) ms (load average ${load}); declare.py runs: ${declares}, agent-bus seat list runs: ${seatlists}" || return 1
            if [[ "${screen%% *}" != "fzf" ]]; then
                printf 'open %s %s asked a question before the table (%s), so the open is not judged. Answer it in\n' "${tag}" "${i}" "${screen}" >&2
                printf 'ccy-teams and run again; an update offer comes back on every open until the update is applied, which\n' >&2
                printf 'the play-agent-bus.yml entry earlier in meta-deploy.list does\n' >&2
                return $((bad ? 1 : 2))
            fi
            if [[ "${tag}" == "new" ]]; then
                if [[ "${declares}" != 1 || "${statuses}" != 1 || "${seatlists}" != 0 ]]; then
                    printf 'open new %s started %s declare.py runs and %s seat lists; want one declare status and none (%s)\n' \
                        "${i}" "${declares}" "${seatlists}" "${dir}/procs" >&2
                    bad=1
                fi
            elif ((declares < 2 || seatlists < 1)); then
                proven=0
            fi
        done
    done
    timing_verdict "open, to the first screen" "${PLAN_RUN_DIR}/open-old.ms" "${PLAN_RUN_DIR}/open-new.ms" || bad=1
    if ((bad)); then
        return 1
    fi
    if ((!proven)); then
        printf 'an old open started fewer than two declare.py runs or no seat list, so the shims are not shown to see\n' >&2
        printf 'them (is there no recorded ccy session with seats?): the process count is not judged\n' >&2
        return 2
    fi
}

# check_report_per_hour — every seat whose live member.json sets limits.report_per_hour, in a
# checkout a session record names, is listed by the new --list (its handle on stdout) and not
# refused (no "seat <seat>@<team>:" line on stderr). With no such seat, or no session record,
# there is nothing to judge (2).
check_report_per_hour() {
    local records rec line dir member text seat team handle judged=0 bad=0
    local out="${PLAN_RUN_DIR}/list/new-1/stdout" err="${PLAN_RUN_DIR}/list/new-1/stderr"
    local re='"user_id"[[:space:]]*:[[:space:]]*"@([^":]+):'
    local -A checkouts=()
    if [[ ! -f "${out}" || ! -f "${err}" ]]; then
        printf 'not run: the new --list did not run (the --list leg failed first)\n' >&2
        return 1
    fi
    records="${CCY_STATE_DIR:-${XDG_STATE_HOME:-${HOME}/.local/state}/ccy}/sessions"
    if [[ ! -d "${records}" ]]; then
        printf 'no session records at %s: no checkout to look in, so report_per_hour is not judged\n' "${records}" >&2
        return 2
    fi
    for rec in "${records}"/*; do
        [[ -f "${rec}" ]] || continue
        while IFS= read -r line; do
            if [[ "${line}" == dir=* ]]; then
                checkouts["${line#dir=}"]=1
            fi
        done <"${rec}" || return 1
    done
    for dir in "${!checkouts[@]}"; do
        for member in "${dir}"/.claude/ccy/pingbus/seats/*/*/member.json; do
            [[ -f "${member}" ]] || continue
            if ! text="$(<"${member}")"; then
                printf '%s cannot be read, so whether it sets report_per_hour is not known\n' "${member}" >&2
                bad=1
                continue
            fi
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
                say_report "- seat ${seat}@${team} (${handle}): REFUSED by the new --list (see ${err})" || return 1
                bad=1
            elif ! grep -qF "${handle}" "${out}"; then
                say_report "- seat ${seat}@${team} (${handle}): not on the new --list's table" || return 1
                bad=1
            else
                say_report "- seat ${seat}@${team} (${handle}) sets report_per_hour and is listed" || return 1
            fi
        done
    done
    if ((judged == 0 && !bad)); then
        printf 'no live member.json in a recorded checkout sets limits.report_per_hour: not judged\n' >&2
        return 2
    fi
    return "${bad}"
}
