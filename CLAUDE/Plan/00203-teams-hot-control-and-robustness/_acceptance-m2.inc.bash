# shellcheck shell=bash
# _acceptance-m2.inc.bash — Plan 00203 milestone M2, the bus kit in every ccy session
# (SPEC-hot-control.md section 6, M2 a-f; ACCEPTANCE.md "M2"), sourced by the acceptance.bash
# dispatcher, which runs acceptance_m2. Sourced, never executed: no shell options, no `exit`.
# A leg runs where errexit is off, so every command that can fail returns non-zero from its
# function explicitly. REPORT and check_leg are the dispatcher's; the probes are m2-check.bash.
#
# WHAT IT CHANGES, and removes on the way out however the run ends: two throwaway headless ccy
# sessions in this checkout, started inside ccy's tmux (so each has a session name, a label and
# a membership, as an owner's session has), `--no-restore` so a reboot never brings them back:
# m2-plain-<pid> (a plain `ccy`) and m2-nobus-<pid> (`ccy --no-bus`). Their stdin is a fifo this
# run holds open, so each waits idle with no turn sent. On the way out: the fifo closed, the
# tmux sessions and containers removed, their registry records, membership directories and
# keeper reports (.claude/ccy/pingbus/sessions/<name>/) deleted, and any transcript they left
# moved into the run directory. Before it starts, the reports an earlier run of this acceptance
# left (its own name patterns only, no running session's) are deleted. Never the owner's own
# sessions.
#
# NEEDS from the owner: ccy launched interactively in this checkout once (Quick Launch saves
# the token and SSH choice a headless launch reuses, every key usable unattended).
#
# CHECKS
#   (b)(c)  the plain session: membership.json reads (no seat); a touch, a write and a remount
#           in the mount fail (H3); no active flag; the guarded status line; inbound refused by
#           the settings claude was started with (Decision 1; the inbox socket Claude Code
#           binds in every session is only reported); a prompt's hook ends in the shell; at most one pingbus
#           process (the keeper, once M1 ships)
#   (c)     idle CPU over a minute: the plain session as the --no-bus one, within a point
#   (d)     the image is the launcher's REQUIRED_CONTAINER_VERSION, with the M2 entrypoint
#   (e)     --no-bus: PINGBUS_MEMBERSHIP=off, no mount, no pingbus, no plugin, no settings
#   (f)     the H34 spike's result is recorded in the spec
#   (H1)    the membership bind asks for Z, and its files carry container_file_t at the
#           container's level (both SELinux modes); Enforcing: the container reads them;
#           Permissive: one line says enforcement was not exercised
#   (a), `ccy --teams` as before, is the Plan 00201 and Plan 00161 acceptances, which meta-deploy
#   runs after this one against the M2 launcher.

readonly M2_LAUNCHER="/var/local/claude-yolo/claude-yolo"
readonly M2_CHECK="${PLAN_SCRIPT_DIR}/m2-check.bash"
readonly M2_EV="${PLAN_RUN_DIR}/m2"
readonly M2_TRANSCRIPTS="${PLAN_REPO_ROOT}/.claude/ccy/projects/-workspace"
readonly M2_PROMPT="Plan 00203 M2 acceptance: an idle session; nothing to do."
M2_NAMES=("m2-plain-$$" "m2-nobus-$$")
declare -A M2_FD=() M2_CTR=() M2_LOG=()

m2_note() { printf '%s\n' "$*" | tee -a "${REPORT}"; }

# m2_adopt <name> <fd> <log> — a session another milestone launched, for m2_cleanup to remove;
# <log> is the path its .status and .err files start with, for m2_ended_early.
m2_adopt() { M2_NAMES+=("$1"); M2_FD[$1]="$2"; M2_LOG[$1]="$3"; }

# m2_transcripts — the checkout's transcript files, sorted (to move only what this run made).
m2_transcripts() {
    if [[ -d "${M2_TRANSCRIPTS}" ]]; then
        find "${M2_TRANSCRIPTS}" -maxdepth 1 -name '*.jsonl' | LC_ALL=C sort
    fi
}

# A session's keeper reports to <checkout>/.claude/ccy/pingbus/sessions/<session>/ (SPEC 3.3).
# The report outlives the session as designed: it is never authority, only a running session's
# is read, and the next keeper of that name rewrites it at its start. So the run removes its
# own sessions' reports, and, before it starts, those an earlier run left: only names this
# acceptance makes, and none whose session is still running.
readonly M2_REPORTS="${PLAN_REPO_ROOT}/.claude/ccy/pingbus/sessions"
readonly M2_OWN_NAMES='^(m1-plain|m2-plain|m2-nobus|m3-hot|m4-a|m4-b)-[0-9]+$'
#: Whether sessions/ was there at the start: the cleanup removes it only if this run made it.
M2_REPORTS_FOUND=1

# m2_running <name> — "yes" when a session of that name runs (its tmux session or its
# container), else "no"; a podman that cannot answer is a failure, never a "no".
m2_running() {
    local ids
    if tmux -L ccy has-session -t "=$1" 2>/dev/null; then
        printf 'yes\n'
        return 0
    fi
    ids="$(podman ps --filter "label=ccy-session=$1" --filter status=running --format '{{.ID}}')" || return 1
    if [[ -n "${ids}" ]]; then printf 'yes\n'; else printf 'no\n'; fi
}

m2_remove_stale_reports() {
    local dir name running
    if [[ -d "${M2_REPORTS}" ]]; then
        for dir in "${M2_REPORTS}"/*/; do
            [[ -d "${dir}" ]] || continue
            name="$(basename -- "${dir}")"
            [[ "${name}" =~ ${M2_OWN_NAMES} ]] || continue
            running="$(m2_running "${name}")" || return 1
            if [[ "${running}" == yes ]]; then
                printf '==> %s is running, so its report stays: %s\n' "${name}" "${dir}"
                continue
            fi
            rm -rf -- "${dir}" || return 1
            printf '==> removed the report of %s, left by an earlier acceptance run\n' "${name}"
        done
        rmdir --ignore-fail-on-non-empty -- "${M2_REPORTS}" || return 1
    fi
    [[ -d "${M2_REPORTS}" ]] || M2_REPORTS_FOUND=0
}

m2_prepare() {
    mkdir -p -- "${M2_EV}" || return 1
    m2_remove_stale_reports || return 1
    m2_transcripts >"${M2_EV}/transcripts-before" || return 1
    [[ -x "${M2_LAUNCHER}" ]] || { printf '[FAIL] no ccy launcher at %s: run play-claude-yolo.yml\n' "${M2_LAUNCHER}" >&2; return 1; }
}

# m2_launch <name> [ccy args...] — a headless ccy in a new session of ccy's tmux server, its
# stdin a fifo this run holds open.
m2_launch() {
    local name="$1" fifo fd command
    shift
    fifo="${M2_EV}/${name}.stdin"
    mkfifo -- "${fifo}" || return 1
    exec {fd}<>"${fifo}" || return 1
    M2_FD[${name}]="${fd}"
    # tmux runs one shell command: every word quoted for it, stdin and output redirected, and
    # never `exec`: the pane's shell stays ccy's parent, keeps the pane's terminal open and
    # records ccy's exit status. With all three streams redirected an exec'd ccy holds no
    # descriptor on the terminal, so tmux reads end of file, takes the pane as exited and sends
    # SIGHUP before ccy writes a byte. The trailing status write is load-bearing, not only a
    # record: a shell given one lone command execs it implicitly (dash does, and bash can),
    # so dropping the `; echo` brings the SIGHUP back. ccy, the child of the pane's first
    # process, is still that session (the registry trampoline's shape).
    command="$(printf '%q ' "${M2_LAUNCHER}" --headless --no-restore --no-supervise "$@" \
        --prompt "${M2_PROMPT}" -- --input-format stream-json --output-format stream-json --verbose --model haiku)" || return 1
    command+="<$(printf '%q' "${fifo}") >$(printf '%q' "${M2_EV}/${name}.out") 2>$(printf '%q' "${M2_EV}/${name}.err")"
    command+="; echo \"\$?\" >$(printf '%q' "${M2_EV}/${name}.status")"
    tmux -L ccy new-session -d -s "${name}" -c "${PLAN_REPO_ROOT}" "${command}" || return 1
    printf '==> started %s: ccy --headless %s\n' "${name}" "$*"
}

# m2_ended_early <name> — say how a session ended before its container started: ccy's exit
# status as the pane's shell recorded it (none: the shell itself was killed) and its stderr.
m2_ended_early() {
    local name="$1" status="not written (the pane's shell was killed)" log
    log="${M2_LOG[${name}]:-${M2_EV}/${name}}"
    if [[ -s "${log}.status" ]]; then
        status="$(<"${log}.status")"
    fi
    printf '[FAIL] the session %s ended before its container started: ccy exit status %s; its stderr (%s):\n' \
        "${name}" "${status}" "${log}.err" >&2
    cat -- "${log}.err" >&2
}

# m2_container_up <name> — the running container labelled ccy-session=<name>; 120 s at most.
m2_container_up() {
    local name="$1" id="" i
    for ((i = 0; i < 120; i++)); do
        id="$(podman ps --filter "label=ccy-session=${name}" --filter status=running --format '{{.ID}}')" || return 1
        if [[ -n "${id}" ]]; then
            M2_CTR[${name}]="${id}"
            # The claim writes the session home just before claude starts; give it a moment.
            sleep 5
            printf '==> %s runs in container %s\n' "${name}" "${id}"
            return 0
        fi
        if ! tmux -L ccy has-session -t "=${name}" 2>/dev/null; then
            m2_ended_early "${name}"
            return 1
        fi
        sleep 1
    done
    printf '[FAIL] no running container labelled ccy-session=%s after 120 s\n' "${name}" >&2
    return 1
}

m2_start() {
    m2_launch "${M2_NAMES[0]}" || return 1
    m2_launch "${M2_NAMES[1]}" --no-bus || return 1
    m2_container_up "${M2_NAMES[0]}" || return 1
    m2_container_up "${M2_NAMES[1]}" || return 1
}

m2_plain() { "${M2_CHECK}" plain "${M2_CTR[${M2_NAMES[0]}]}"; }
m2_nobus() { "${M2_CHECK}" nobus "${M2_CTR[${M2_NAMES[1]}]}"; }
m2_cpu() { "${M2_CHECK}" cpu "${M2_CTR[${M2_NAMES[0]}]}" "${M2_CTR[${M2_NAMES[1]}]}"; }
m2_image() { "${M2_CHECK}" image "${M2_CTR[${M2_NAMES[0]}]}"; }
m2_selinux() { "${M2_CHECK}" selinux "${M2_CTR[${M2_NAMES[0]}]}"; }
m2_spike() { "${M2_CHECK}" spike "${PLAN_SCRIPT_DIR}/SPEC-hot-control.md"; }

# m2_cleanup — every throwaway this run made, whether or not it got that far; never fails
# silently: each step that cannot be done is said, and the cleanup goes on to the next.
m2_cleanup() {
    local name fd id records status=0 made=()
    records="${XDG_STATE_HOME:-${HOME}/.local/state}/ccy/sessions"
    for name in "${M2_NAMES[@]}"; do
        fd="${M2_FD[${name}]:-}"
        if [[ -n "${fd}" ]]; then
            exec {fd}>&- || status=1
            M2_FD[${name}]=""
        fi
        if tmux -L ccy has-session -t "=${name}"; then
            tmux -L ccy kill-session -t "=${name}" || status=1
        fi
        for id in $(podman ps -a --filter "label=ccy-session=${name}" --format '{{.ID}}'); do
            podman rm -f -t 10 -- "${id}" >/dev/null || status=1
        done
        rm -f -- "${records}/${name}" || status=1
        rm -rf -- "${XDG_RUNTIME_DIR:?}/ccy/membership/${name}" "${M2_REPORTS:?}/${name}" || status=1
    done
    if [[ "${M2_REPORTS_FOUND}" -eq 0 && -d "${M2_REPORTS}" ]]; then
        rmdir --ignore-fail-on-non-empty -- "${M2_REPORTS}" || status=1
    fi
    if [[ -d "${M2_EV}" ]]; then
        mkdir -p -- "${M2_EV}/transcripts" || status=1
        mapfile -t made < <(m2_transcripts | LC_ALL=C comm -13 "${M2_EV}/transcripts-before" -) || status=1
        for id in "${made[@]}"; do
            mv -- "${id}" "${M2_EV}/transcripts/" || status=1
        done
    fi
    if [[ "${status}" -ne 0 ]]; then
        printf '[FAIL] the M2 cleanup could not remove everything (see above)\n' >&2
    fi
    return "${status}"
}

acceptance_m2() {
    plan_on_cleanup m2_cleanup
    printf '## M2: the bus kit in every ccy session\n\n' >>"${REPORT}"
    plan_deploy_leg "two throwaway ccy sessions in ccy's tmux: plain and --no-bus" m2_prepare
    plan_deploy_leg "start them and wait for their containers" m2_start
    check_leg "(H1) the membership mount is relabelled for the container" m2_selinux
    check_leg "(b)(c) a plain ccy carries an inert kit, no inbound and a read-only membership" m2_plain
    check_leg "(c) idle CPU unchanged" m2_cpu
    check_leg "(d) the image the launcher requires" m2_image
    check_leg "(e) --no-bus loads nothing" m2_nobus
    check_leg "(f) the spike's result is recorded" m2_spike
    plan_deploy_leg "remove the throwaway sessions" m2_cleanup
    m2_note "M2: (b)-(f) passed; (a) is the Plan 00201 and 00161 acceptances, queued after this one"
}
