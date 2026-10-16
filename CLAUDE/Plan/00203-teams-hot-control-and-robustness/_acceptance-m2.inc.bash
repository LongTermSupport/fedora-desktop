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
# tmux sessions and containers removed, their registry records and membership directories
# deleted, and any transcript they left moved into the run directory. Never the owner's own
# sessions.
#
# NEEDS from the owner: ccy launched interactively in this checkout once (Quick Launch saves
# the token and SSH choice a headless launch reuses, every key usable unattended).
#
# CHECKS
#   (b)(c)  the plain session: membership.json reads (no seat); a touch, a write and a remount
#           in the mount fail (H3); no active flag; the guarded status line; no inbound and no
#           inbox socket (Decision 1); a prompt's hook ends in the shell; at most one pingbus
#           process (the keeper, once M1 ships)
#   (c)     idle CPU over a minute: the plain session as the --no-bus one, within a point
#   (d)     the image is the launcher's REQUIRED_CONTAINER_VERSION, with the M2 entrypoint
#   (e)     --no-bus: PINGBUS_MEMBERSHIP=off, no mount, no pingbus, no plugin, no settings
#   (f)     the H34 spike's result is recorded in the spec
#   SELinux the host is Enforcing
#   (a), `ccy --teams` as before, is the Plan 00201 and Plan 00161 acceptances, which meta-deploy
#   runs after this one against the M2 launcher.

readonly M2_LAUNCHER="/var/local/claude-yolo/claude-yolo"
readonly M2_CHECK="${PLAN_SCRIPT_DIR}/m2-check.bash"
readonly M2_EV="${PLAN_RUN_DIR}/m2"
readonly M2_TRANSCRIPTS="${PLAN_REPO_ROOT}/.claude/ccy/projects/-workspace"
readonly M2_PROMPT="Plan 00203 M2 acceptance: an idle session; nothing to do."
M2_NAMES=("m2-plain-$$" "m2-nobus-$$")
declare -A M2_FD=() M2_CTR=()

m2_note() { printf '%s\n' "$*" | tee -a "${REPORT}"; }

# m2_transcripts — the checkout's transcript files, sorted (to move only what this run made).
m2_transcripts() {
    if [[ -d "${M2_TRANSCRIPTS}" ]]; then
        find "${M2_TRANSCRIPTS}" -maxdepth 1 -name '*.jsonl' | LC_ALL=C sort
    fi
}

m2_prepare() {
    mkdir -p -- "${M2_EV}" || return 1
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
    # tmux runs one shell command: every word quoted for it, stdin and output redirected.
    command="$(printf '%q ' exec "${M2_LAUNCHER}" --headless --no-restore --no-supervise "$@" \
        --prompt "${M2_PROMPT}" -- --input-format stream-json --output-format stream-json --verbose --model haiku)" || return 1
    command+="<$(printf '%q' "${fifo}") >$(printf '%q' "${M2_EV}/${name}.out") 2>$(printf '%q' "${M2_EV}/${name}.err")"
    tmux -L ccy new-session -d -s "${name}" -c "${PLAN_REPO_ROOT}" "${command}" || return 1
    printf '==> started %s: ccy --headless %s\n' "${name}" "$*"
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
        if ! tmux -L ccy has-session -t "=${name}"; then
            printf '[FAIL] the session %s ended before its container started\n' "${name}" >&2
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
m2_selinux() { "${M2_CHECK}" selinux; }
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
        rm -rf -- "${XDG_RUNTIME_DIR:?}/ccy/membership/${name}" || status=1
    done
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
    check_leg "SELinux Enforcing" m2_selinux
    plan_deploy_leg "two throwaway ccy sessions in ccy's tmux: plain and --no-bus" m2_prepare
    plan_deploy_leg "start them and wait for their containers" m2_start
    check_leg "(b)(c) a plain ccy carries an inert kit, no inbound and a read-only membership" m2_plain
    check_leg "(c) idle CPU unchanged" m2_cpu
    check_leg "(d) the image the launcher requires" m2_image
    check_leg "(e) --no-bus loads nothing" m2_nobus
    check_leg "(f) the spike's result is recorded" m2_spike
    plan_deploy_leg "remove the throwaway sessions" m2_cleanup
    m2_note "M2: (b)-(f) passed; (a) is the Plan 00201 and 00161 acceptances, queued after this one"
}
