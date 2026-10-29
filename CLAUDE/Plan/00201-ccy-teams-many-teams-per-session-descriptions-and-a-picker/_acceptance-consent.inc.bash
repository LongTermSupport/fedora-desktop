# shellcheck shell=bash
# _acceptance-consent.inc.bash — the legs and checks of acceptance-consent.bash (Plan 00201,
# ACCEPTANCE-owner.md step 08: the Element consent of `cc --teams`, No then Yes). A separate
# file for the reason _acceptance-m7.inc.bash is one: legs are called through plan_deploy_leg
# and the script's check runner, which ShellCheck cannot see through (SC2329), and suppression
# is banned. Sourced after _acceptance-m7.inc.bash, never executed: no shell options, no `exit`.
# Every function returns non-zero on its first failure, explicitly; a check returns 0 PASS,
# 1 FAIL, or 2 COULD NOT ESTABLISH (a person was needed and did not come).
#
# THE CONSENT RECORD is pingbus's: element-accepted under the passwd home's .config/pingbus
# (config.py ELEMENT_ACCEPTED_DIR and ELEMENT_ACCEPTED_FILE, read from this checkout). No
# environment variable moves it, so the real one is held aside, renamed in its own directory to
# CONSENT_HELD_SUFFIX, and renamed back however the run ends. With no record to begin with, an
# empty marker named CONSENT_NONE_SUFFIX stands in its place, and the way back removes the
# record the Yes wrote. A run killed outright (no trap) leaves the held copy or the marker, and
# the next run puts that back before anything else.
#
# THE SESSION is the owner's own cc launcher in a private tmux server (its own socket, started
# in a systemd user scope of its own: cc refuses a tmux server that dies with a terminal tab), in
# a scratch git checkout outside this repository with no .claude/, its origin a repository of
# the GitHub owner the throwaway teams trust. The pane is read with `tmux capture-pane` and
# answered with `tmux send-keys`; consent_check.py judges what was read.
#
# Reads acceptance-consent.bash's globals (TEAM_A, TEAM_B, AGENT_BUS, CC_LAUNCHER, CHECK,
# CONSENT, REPORT) and the M7 legs' (M7_TEAMS, M7_POLL_S, TEAMS_PRESENT, m7_ev, m7_poll,
# evidence).

readonly CONSENT_HELD_SUFFIX=".held-by-plan00201-consent-check"
readonly CONSENT_NONE_SUFFIX=".absent-before-plan00201-consent-check"
readonly CONSENT_SCRATCH_REPO="consent201-scratch"
readonly CONSENT_PANE="${PLAN_SCRIPT_DIR}/consent-pane.bash"
readonly CONSENT_TMUX_SOCKET="plan00201-consent-$$"
readonly CONSENT_TMUX_SESSION="consent"
#: cc's own record of the token it last used (cc's write_status_token), which every cc launch
#: rewrites: saved first and put back, as acceptance.bash does with ccy's Quick Launch record.
readonly CONSENT_CC_STATUS="${HOME}/.claude/.last-launch.conf"
#: Bounds, in seconds: the question (the chooser asks agent-bus about each team first), the
#: seats after Yes (cc runs `claude update` first), a person typing sudo's password, cc's exit.
readonly CONSENT_ASK_S=180
readonly CONSENT_SEAT_S=600
readonly CONSENT_SUDO_WAIT_S=900
readonly CONSENT_EXIT_S=60
readonly CONSENT_RELEASE_S=60

CONSENT_RECORD=""
CONSENT_HELD=""
CONSENT_NONE=""
#: held: the owner's record is at CONSENT_HELD; none: there was none, CONSENT_NONE marks it;
#: empty: nothing to put back.
CONSENT_STATE=""
CC_STATUS_STATE=""
SCRATCH=""
SCRATCH_TOP=""
CC_LIST=""
HANDLE_A=""
HANDLE_B=""
TMUX_UNIT=""
TMUX_UP=0

consent_tmux() { tmux -L "${CONSENT_TMUX_SOCKET}" "$@"; }

# ── before anything ──────────────────────────────────────────────────────────────────────

consent_prerequisites() {
    local tool dir file home
    m7_session_paths || return 1
    for tool in "${CC_LAUNCHER}" "${AGENT_BUS}" "${INSTALLER}" "${CONSENT_PANE}"; do
        if [[ ! -x "${tool}" ]]; then
            printf '[FAIL] %s is not installed or not executable: run play-claude-yolo.yml and play-agent-bus.yml\n' "${tool}" >&2
            return 1
        fi
    done
    for tool in ccy-teams tmux systemd-run systemctl gum git getent podman jq curl firewall-cmd ansible-inventory; do
        if ! command -v "${tool}" >/dev/null; then
            printf '[FAIL] %s is not on PATH: its play installs it (ccy-teams, gum, tmux: play-claude-yolo.yml and play-tmux-sessions.yml)\n' "${tool}" >&2
            return 1
        fi
    done
    IFS=: read -r _ _ _ _ _ home _ < <(getent passwd "${EUID}") || {
        printf '[FAIL] uid %d is not in the passwd database: the consent record lives under its home there\n' "${EUID}" >&2
        return 1
    }
    dir="$(m7_value helpers/pingbus/config.py ELEMENT_ACCEPTED_DIR)" || return 1
    file="$(m7_value helpers/pingbus/config.py ELEMENT_ACCEPTED_FILE)" || return 1
    CONSENT_RECORD="${home}/${dir}/${file}"
    CONSENT_HELD="${CONSENT_RECORD}${CONSENT_HELD_SUFFIX}"
    CONSENT_NONE="${CONSENT_RECORD}${CONSENT_NONE_SUFFIX}"
    m7_owner_declares_neither || return 1
    printf '==> the consent record is %s; cc, agent-bus, ccy-teams, tmux and gum are here\n' "${CONSENT_RECORD}"
}

consent_exists() { [[ -e "$1" || -L "$1" ]]; }

# consent_keep <path> <evidence-name> — a copy of a record about to be replaced, in the run
# directory (the record is one line, a time; nothing secret).
consent_keep() {
    if [[ -f "$1" && ! -L "$1" ]]; then
        cp -p -- "$1" "$(m7_ev "$2")" || return 1
    fi
}

# consent_put_back_killed — what a run killed outright left: the held copy renamed back over
# whatever stands at the record's name, or (the marker) the record a Yes wrote removed. The
# held copy is the owner's own record by construction, and anything at the record's name was
# written by that run's Yes, or by the owner accepting again meanwhile, which records the same
# acceptance: so it is put back, not refused. Both left at once cannot happen; refused.
consent_put_back_killed() {
    if consent_exists "${CONSENT_HELD}" && consent_exists "${CONSENT_NONE}"; then
        printf '[FAIL] both %s and %s exist, which no run of this script leaves: look at them, keep the record you want at %s, remove the other two, then run again\n' \
            "${CONSENT_HELD}" "${CONSENT_NONE}" "${CONSENT_RECORD}" >&2
        return 1
    fi
    if consent_exists "${CONSENT_HELD}"; then
        consent_keep "${CONSENT_RECORD}" element-accepted.left-by-a-killed-run || return 1
        mv -f -T -- "${CONSENT_HELD}" "${CONSENT_RECORD}" || return 1
        printf '==> an interrupted run held the consent record aside: %s put back\n' "${CONSENT_RECORD}"
        evidence "an interrupted run's held-aside consent record was put back first"
    elif consent_exists "${CONSENT_NONE}"; then
        consent_keep "${CONSENT_RECORD}" element-accepted.left-by-a-killed-run || return 1
        rm -f -- "${CONSENT_RECORD}" "${CONSENT_NONE}" || return 1
        printf '==> an interrupted run began with no consent record: the one its Yes wrote is removed\n'
        evidence "an interrupted run began with no consent record; the record its Yes wrote was removed first"
    fi
}

# consent_leftovers — teams of these names left by an interrupted run, purged once they are
# known to be this acceptance's own (their description says so).
consent_leftovers() {
    local team
    for team in "${M7_TEAMS[@]}"; do
        m7_own_or_absent "${team}" || return 1
    done
    m7_remove_containers || return 1
    m7_remove_teams leftover
}

# ── the scratch checkout and its seats ───────────────────────────────────────────────────

consent_make_checkout() {
    local url org
    url="$(git -C "${PLAN_REPO_ROOT}" remote get-url origin)" || return 1
    org="$("${CHECK[@]}" github-owner "${url}")" || return 1
    SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/plan00201-consent.XXXXXX")" || return 1
    SCRATCH_TOP="${SCRATCH}/${CONSENT_SCRATCH_REPO}"
    git init -q -- "${SCRATCH_TOP}" || return 1
    git -C "${SCRATCH_TOP}" remote add origin "https://github.com/${org}/${CONSENT_SCRATCH_REPO}.git" || return 1
    if consent_exists "${SCRATCH_TOP}/.claude"; then
        printf '[FAIL] the new scratch checkout %s has a .claude/ already\n' "${SCRATCH_TOP}" >&2
        return 1
    fi
    printf '==> scratch checkout %s (origin %s/%s), no .claude/\n' "${SCRATCH_TOP}" "${org}" "${CONSENT_SCRATCH_REPO}"
}

consent_remove_checkout() {
    if [[ -n "${SCRATCH}" ]]; then
        rm -rf -- "${SCRATCH:?}" || return 1
        printf '==> removed the scratch checkout %s\n' "${SCRATCH}"
        SCRATCH=""
    fi
}

# consent_choose_seats — the seat agent-bus offers a host session of the scratch checkout in
# each team (a new one: the checkout has none), and its handle, which the question must name.
consent_choose_seats() {
    local team out pick seat handle
    local -a refs=()
    for team in "${M7_TEAMS[@]}"; do
        out="$(m7_ev "seat-choose-${team}.out")"
        (cd -- "${SCRATCH_TOP}" && "${AGENT_BUS}" seat choose --type=host "${team}") >"${out}" || return 1
        pick="$("${CONSENT[@]}" default-seat "${out}")" || return 1
        IFS=$'\t' read -r seat handle <<<"${pick}"
        refs+=("${seat}@${team}")
        if [[ "${team}" == "${TEAM_A}" ]]; then HANDLE_A="${handle}"; else HANDLE_B="${handle}"; fi
    done
    CC_LIST="${refs[0]},${refs[1]}"
    printf '==> cc --teams %s: %s and %s\n' "${CC_LIST}" "${HANDLE_A}" "${HANDLE_B}"
    if consent_exists "${SCRATCH_TOP}/.claude"; then
        printf '[FAIL] agent-bus seat choose made %s/.claude\n' "${SCRATCH_TOP}" >&2
        return 1
    fi
}

# ── the record held aside, and put back ──────────────────────────────────────────────────

# consent_hold — the owner's record renamed aside (or, with none, the marker made), and cc's
# token record saved; from here on consent_restore puts them back however the run ends.
consent_hold() {
    if consent_exists "${CONSENT_CC_STATUS}"; then
        cp -p -- "${CONSENT_CC_STATUS}" "$(m7_ev cc-last-launch.conf.before)" || return 1
        CC_STATUS_STATE="saved"
    else
        CC_STATUS_STATE="absent"
    fi
    if consent_exists "${CONSENT_RECORD}"; then
        cp -p -- "${CONSENT_RECORD}" "$(m7_ev element-accepted.before)" || return 1
        CONSENT_STATE="held" # before the rename: a half-done one is still put back
        mv -T -- "${CONSENT_RECORD}" "${CONSENT_HELD}" || return 1
        printf '==> the consent record is held aside as %s\n' "${CONSENT_HELD}"
    else
        (umask 077 && mkdir -p -- "${CONSENT_RECORD%/*}") || return 1
        CONSENT_STATE="none"
        (umask 077 && : >"${CONSENT_NONE}") || return 1
        printf '==> there is no consent record; %s marks that, so the way back removes the one Yes writes\n' "${CONSENT_NONE}"
    fi
    if consent_exists "${CONSENT_RECORD}"; then
        printf '[FAIL] %s is still there after being held aside\n' "${CONSENT_RECORD}" >&2
        return 1
    fi
}

# consent_restore_record — the owner's record back under its name (the one this run's Yes
# wrote kept as evidence), or, with none to begin with, the one the Yes wrote removed. Run first
# on the way out, however the run ends; nothing to do once done.
consent_restore_record() {
    case "${CONSENT_STATE}" in
        held)
            consent_keep "${CONSENT_RECORD}" element-accepted.written-by-yes || return 1
            if ! mv -f -T -- "${CONSENT_HELD}" "${CONSENT_RECORD}"; then
                printf '[FAIL] the consent record could not be put back: it is %s; run %s again, which puts it back first\n' \
                    "${CONSENT_HELD}" "${0##*/}" >&2
                return 1
            fi
            ;;
        none)
            consent_keep "${CONSENT_RECORD}" element-accepted.written-by-yes || return 1
            if ! rm -f -- "${CONSENT_RECORD}" "${CONSENT_NONE}"; then
                printf '[FAIL] the consent record the Yes wrote could not be removed; run %s again, which removes it first\n' "${0##*/}" >&2
                return 1
            fi
            ;;
        *) return 0 ;;
    esac
    CONSENT_STATE=""
    printf '==> the consent record is as it was before the run\n'
}

consent_restore_cc_status() {
    case "${CC_STATUS_STATE}" in
        saved) cp -p -- "$(m7_ev cc-last-launch.conf.before)" "${CONSENT_CC_STATUS}" || return 1 ;;
        absent) rm -f -- "${CONSENT_CC_STATUS}" || return 1 ;;
        *) return 0 ;;
    esac
    CC_STATUS_STATE=""
    printf '==> cc'\''s token record (%s) is as it was\n' "${CONSENT_CC_STATUS}"
}

# ── the private tmux ─────────────────────────────────────────────────────────────────────

# consent_tmux_start <name> — cc --teams CC_LIST in the scratch checkout, in a new pane of a
# private tmux server started in a systemd user scope of its own, with the throwaway host_vars
# for ccy-teams. The caller's own tmux and bus variables are not passed on.
consent_tmux_start() {
    local var
    local -a clear=()
    mkdir -p -- "$(m7_ev "$1")" || return 1
    while IFS= read -r var; do
        if [[ "${var}" == PINGBUS_* || "${var}" == TMUX || "${var}" == TMUX_PANE ]]; then
            clear+=(-u "${var}")
        fi
    done < <(compgen -e)
    TMUX_UNIT="plan00201-consent-$$-$1"
    TMUX_UP=1 # before the start: a half-started server is still stopped on the way out
    env "${clear[@]}" systemd-run --user --scope --quiet --collect "--unit=${TMUX_UNIT}" -- \
        tmux -L "${CONSENT_TMUX_SOCKET}" -f /dev/null new-session -d -s "${CONSENT_TMUX_SESSION}" -x 220 -y 60 \
        -c "${SCRATCH_TOP}" -e "CCY_TEAMS_HOST_VARS=${HOST_VARS_FILE}" -- \
        "${CONSENT_PANE}" "$(m7_ev "$1/cc.status")" "${CC_LAUNCHER}" "${CC_LIST}" || return 1
    printf '==> %s: cc --teams %s in tmux -L %s (scope %s.scope)\n' "$1" "${CC_LIST}" "${CONSENT_TMUX_SOCKET}" "${TMUX_UNIT}"
}

# consent_capture <file> — the pane's whole text, history included, wrapped lines joined.
consent_capture() { consent_tmux capture-pane -p -J -S - -t "${CONSENT_TMUX_SESSION}" >"$1"; }

consent_keys() { consent_tmux send-keys -t "${CONSENT_TMUX_SESSION}" "$@"; }

# consent_tmux_kill — the private server ended, and its scope with it. No server left is done.
consent_tmux_kill() {
    local out status=0 state
    if [[ "${TMUX_UP}" -ne 1 ]]; then
        return 0
    fi
    out="$(consent_tmux kill-server 2>&1)" || status=$?
    if [[ "${status}" -ne 0 && "${out}" != *"no server running"* && "${out}" != *"error connecting"* ]]; then
        printf '[FAIL] tmux -L %s kill-server exited %d: %s\n' "${CONSENT_TMUX_SOCKET}" "${status}" "${out}" >&2
        return 1
    fi
    status=0
    state="$(systemctl --user is-active -- "${TMUX_UNIT}.scope")" || status=$?
    if [[ "${state}" == active || "${state}" == deactivating ]]; then
        systemctl --user stop -- "${TMUX_UNIT}.scope" || return 1
    fi
    TMUX_UP=0
    printf '==> the private tmux server is gone\n'
}

consent_status_file() { m7_ev "$1/cc.status"; }

consent_cc_ended() { [[ -s "$(consent_status_file "$1")" ]] || return 3; }

consent_show() {
    printf -- '--- the pane (%s):\n' "$1" >&2
    cat -- "$1" >&2
}

# consent_screen_is <name> <want> — the pane shows <want> last (consent_check screen); 3 not
# yet; cc ending first is a FAIL with the pane shown.
consent_screen_is() {
    local pane state
    pane="$(m7_ev "$1/screen.pane")"
    consent_capture "${pane}" || return 1
    state="$("${CONSENT[@]}" screen "${pane}")" || return 1
    if [[ "${state}" == "$2" ]]; then
        return 0
    fi
    if consent_cc_ended "$1"; then
        consent_show "${pane}"
        printf '[FAIL] cc --teams ended (exit %s) before the pane showed %s\n' "$(cat -- "$(consent_status_file "$1")")" "$2" >&2
        return 1
    fi
    return 3
}

# consent_asked <name> — the consent question is on the screen and names both handles.
consent_asked() {
    local pane
    m7_poll "${CONSENT_ASK_S}" "the consent question on the screen" consent_screen_is "$1" consent || return 1
    pane="$(m7_ev "$1/consent.pane")"
    consent_capture "${pane}" || return 1
    if ! "${CONSENT[@]}" expect-consent "${pane}" "${HANDLE_A}" "${HANDLE_B}"; then
        consent_show "${pane}"
        return 1
    fi
    printf '==> %s: the consent question names %s and %s\n' "$1" "${HANDLE_A}" "${HANDLE_B}"
}

consent_seat_list() {
    (cd -- "${SCRATCH_TOP}" && "${AGENT_BUS}" seat list) >"$(m7_ev "$1.out")" 2>"$(m7_ev "$1.err")"
}

# ── 08 No ────────────────────────────────────────────────────────────────────────────────

# No: cc --teams ends 1 saying nothing was recorded; there is no record, the checkout has no
# .claude/ (so no .claude/ccy/), and neither handle is a member of its team.
consent_check_no() {
    local status pane state team handle list
    consent_tmux_start no || return 1
    consent_asked no || return 1
    consent_keys n || return 1
    m7_poll "${CONSENT_EXIT_S}" "cc --teams ended after No" consent_cc_ended no || return 1
    status="$(cat -- "$(consent_status_file no)")" || return 1
    pane="$(m7_ev no/after.pane)"
    consent_capture "${pane}" || return 1
    state="$("${CONSENT[@]}" screen "${pane}")" || return 1
    if [[ "${status}" != 1 || "${state}" != declined ]]; then
        consent_show "${pane}"
        printf '[FAIL] after No, cc --teams exited %s (want 1) and the pane shows %s (want declined)\n' "${status}" "${state}" >&2
        return 1
    fi
    if consent_exists "${CONSENT_RECORD}"; then
        printf '[FAIL] No recorded a consent: %s exists\n' "${CONSENT_RECORD}" >&2
        return 1
    fi
    if consent_exists "${SCRATCH_TOP}/.claude"; then
        printf '[FAIL] No left %s/.claude (a checkout with no .claude/ccy/ must still have none)\n' "${SCRATCH_TOP}" >&2
        return 1
    fi
    for team in "${M7_TEAMS[@]}"; do
        if [[ "${team}" == "${TEAM_A}" ]]; then handle="${HANDLE_A}"; else handle="${HANDLE_B}"; fi
        list="$(m7_ev "no/members-${team}.out")"
        { sudo -n "${AGENT_BUS}" list "${team}"; } >"${list}" || return 1
        "${CONSENT[@]}" expect-not-member "${list}" "${handle}" || return 1
    done
    consent_tmux_kill || return 1
    evidence "08 No: the question named ${HANDLE_A} and ${HANDLE_B} (no/consent.pane); cc --teams exited 1 saying nothing was recorded (no/after.pane); no record, no .claude/ in the checkout, neither handle a member (no/members-*.out)"
}

# ── 08 Yes ───────────────────────────────────────────────────────────────────────────────

consent_pause_for_sudo() {
    cat <<EOF

==> PAUSED: sudo asks for your password in the private tmux pane: cc --teams is making the two
    host seats there, as it does on any first start of a seat. A person types it there:
      tmux -L ${CONSENT_TMUX_SOCKET} attach -t ${CONSENT_TMUX_SESSION}
    type the password, press Enter, then detach (Ctrl-b, then d). This run waits up to
    $((CONSENT_SUDO_WAIT_S / 60)) minutes; the consent record is put back however it ends.

EOF
}

# consent_wait_seated — after Yes, until both seats are held: the token chooser answered
# Desktop (d) if it is shown, a sudo prompt paused for a person (2 if none comes), cc ending a
# FAIL. Waiting for the person does not count against CONSENT_SEAT_S.
consent_wait_seated() {
    local deadline=$((SECONDS + CONSENT_SEAT_S)) sudo_until="" tokens=0 pane state status
    pane="$(m7_ev yes/screen.pane)"
    while true; do
        status=0
        consent_seat_list yes/seat-list || status=3
        if [[ "${status}" -eq 0 ]]; then
            "${CONSENT[@]}" seats-held "$(m7_ev yes/seat-list.out)" "${CC_LIST%%,*}=${HANDLE_A}" "${CC_LIST#*,}=${HANDLE_B}" || status=$?
        fi
        case "${status}" in
            0) return 0 ;;
            3) ;;
            *) return 1 ;;
        esac
        consent_capture "${pane}" || return 1
        if consent_cc_ended yes; then
            consent_show "${pane}"
            printf '[FAIL] cc --teams ended (exit %s) before both seats were held\n' "$(cat -- "$(consent_status_file yes)")" >&2
            return 1
        fi
        state="$("${CONSENT[@]}" screen "${pane}")" || return 1
        if [[ "${state}" == sudo ]]; then
            if [[ -z "${sudo_until}" ]]; then
                consent_pause_for_sudo
                evidence "08 Yes: sudo asked for a password in the pane; the run paused for a person"
                sudo_until=$((SECONDS + CONSENT_SUDO_WAIT_S))
            fi
            if ((SECONDS >= sudo_until)); then
                printf 'COULD NOT ESTABLISH: nobody typed sudo'\''s password in the pane within %d minutes\n' "$((CONSENT_SUDO_WAIT_S / 60))" >&2
                return 2
            fi
            deadline=$((SECONDS + CONSENT_SEAT_S))
        elif [[ "${state}" == tokens && "${tokens}" -eq 0 ]]; then
            consent_keys d Enter || return 1
            tokens=1
            printf '==> the token chooser: Desktop (d)\n'
        fi
        m7_sudo_alive || return 1
        if ((SECONDS >= deadline)); then
            consent_show "${pane}"
            printf '[FAIL] both seats not held within %d s of Yes; agent-bus seat list said (%s):\n' "${CONSENT_SEAT_S}" "$(m7_ev yes/seat-list.err)" >&2
            cat -- "$(m7_ev yes/seat-list.out)" "$(m7_ev yes/seat-list.err)" >&2
            return 1
        fi
        sleep "${M7_POLL_S}"
    done
}

# Yes: the record is written (owner-only, by this run), and both seats are held with the
# handles the question named.
consent_check_yes() {
    local since status=0
    consent_tmux_start yes || return 1
    since="$(date +%s)" || return 1
    consent_asked yes || return 1
    consent_keys y || return 1
    consent_wait_seated || status=$?
    if [[ "${status}" -ne 0 ]]; then
        return "${status}"
    fi
    "${CONSENT[@]}" expect-record "${CONSENT_RECORD}" "${EUID}" "${since}" || return 1
    cp -p -- "${CONSENT_RECORD}" "$(m7_ev yes/consent-record)" || return 1
    evidence "08 Yes: the question named ${HANDLE_A} and ${HANDLE_B} (yes/consent.pane); the record was written owner-only (yes/consent-record); ${CC_LIST} held by them (yes/seat-list.out)"
}

# consent_end_session — the Yes session ended as a person would (Ctrl-C twice to claude), then
# the private server; a session that does not end in CONSENT_EXIT_S is stopped with it.
consent_end_session() {
    local deadline
    if [[ "${TMUX_UP}" -ne 1 ]]; then
        return 0
    fi
    consent_capture "$(m7_ev yes/before-exit.pane)" || return 1
    if ! consent_cc_ended yes; then
        consent_keys C-c || return 1
        sleep 2
        consent_keys C-c || return 1
        deadline=$((SECONDS + CONSENT_EXIT_S))
        while ! consent_cc_ended yes && ((SECONDS < deadline)); do
            sleep 1
        done
        if consent_cc_ended yes; then
            printf '==> the cc session ended (exit %s)\n' "$(cat -- "$(consent_status_file yes)")"
        else
            printf '==> the cc session did not end within %d s of Ctrl-C: stopping its tmux server\n' "${CONSENT_EXIT_S}"
            evidence "the cc session did not end on Ctrl-C within ${CONSENT_EXIT_S} s; its tmux server was stopped"
        fi
    fi
    consent_tmux_kill
}

consent_seats_free() {
    local status=0
    consent_seat_list yes/seat-list-after || return 1
    "${CONSENT[@]}" seats-held "$(m7_ev yes/seat-list-after.out)" "${CC_LIST%%,*}=${HANDLE_A}" "${CC_LIST#*,}=${HANDLE_B}" || status=$?
    case "${status}" in
        0) return 3 ;;
        3) return 0 ;;
        *) return 1 ;;
    esac
}

consent_check_released() {
    m7_poll "${CONSENT_RELEASE_S}" "the seats released once the session ended" consent_seats_free || return 1
    evidence "the seats were released when the session ended (yes/seat-list-after.out)"
}

# ── the way out ──────────────────────────────────────────────────────────────────────────

# consent_check_gone — the teams wholly gone; the consent record and cc's token record as they
# were; no held copy or marker left; the scratch checkout removed.
consent_check_gone() {
    m7_check_teams_gone || return 1
    if consent_exists "${CONSENT_HELD}" || consent_exists "${CONSENT_NONE}"; then
        printf '[FAIL] a held-aside copy or marker is left beside %s\n' "${CONSENT_RECORD}" >&2
        return 1
    fi
    if [[ -f "$(m7_ev element-accepted.before)" ]]; then
        cmp -s -- "$(m7_ev element-accepted.before)" "${CONSENT_RECORD}" || {
            printf '[FAIL] %s is not the record held aside at the start\n' "${CONSENT_RECORD}" >&2
            return 1
        }
    elif consent_exists "${CONSENT_RECORD}"; then
        printf '[FAIL] %s exists, and there was none before the run\n' "${CONSENT_RECORD}" >&2
        return 1
    fi
    if [[ -f "$(m7_ev cc-last-launch.conf.before)" ]]; then
        cmp -s -- "$(m7_ev cc-last-launch.conf.before)" "${CONSENT_CC_STATUS}" || {
            printf '[FAIL] %s differs from the copy saved at the start\n' "${CONSENT_CC_STATUS}" >&2
            return 1
        }
    fi
    if [[ -n "${SCRATCH}" ]]; then
        printf '[FAIL] the scratch checkout %s is still there\n' "${SCRATCH}" >&2
        return 1
    fi
    evidence "C1 both teams purged (firewalld rules, units, state, stamps); the consent record and cc's token record as they were; the scratch checkout removed"
}

# On the way out of a run that stopped early (plan_on_cleanup): the record first, then the
# session, cc's token record, the checkout and the teams; each step tried, each failure named.
consent_after_stop() {
    local failed=0
    consent_restore_record || failed=1
    consent_tmux_kill || failed=1
    consent_restore_cc_status || failed=1
    consent_remove_checkout || failed=1
    m7_teardown_after_stop || failed=1
    if [[ "${failed}" -ne 0 ]]; then
        printf '[FAIL] the teardown did not finish (above); run %s again, which puts back what is left first\n' "${0##*/}" >&2
    fi
    return "${failed}"
}
