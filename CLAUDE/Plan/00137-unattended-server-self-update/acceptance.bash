#!/usr/bin/env bash
# Plan 00137 — acceptance.bash
#
# PURPOSE: render the VERDICT on the self-update cycle's installation on THIS server,
# after `deploy.bash --role server`. HOST ONLY (CLAUDE/PlanScriptStandards.md R2).
#
# WHAT IT PROVES HERE: the sudo grant is exactly the one command; the root-only files and
# the system toolchain are root-owned and not writable by anyone else; ptrace_scope is 1
# live; the timer and the post-boot unit are enabled; the session restore the reboot relies
# on is pulled in by the user manager; the deploy clone is readable by the user; the
# published result directory has the shape the host-health report reads; and a dry run of
# the real entry point, through the real sudo grant, completes.
#
# WHAT IT CANNOT PROVE HERE: a real cycle (a signed commit is deployed, the plays run, the
# sessions are warned, the machine reboots and they come back), that an unsigned commit is
# ignored, and that a failed play stops the reboot. Those change the machine, so they are
# Task 5.3's by hand and are listed under NOT ESTABLISHABLE HERE, never counted.
#
# READ-ONLY, apart from `sudo -k`, which drops this terminal's cached sudo credential so
# check [0] can tell the one-command grant from a cached password. The dry run moves no
# clone, runs no play, warns nobody and records nothing (DESIGN-cycle.md).
#
# EXIT STATUS
#   0  ACCEPTED — every declared check ran and every assertion passed
#   1  REJECTED — an assertion failed, or a declared check never ran
#   2  COULD NOT ESTABLISH — nothing failed, but a check had no evidence to judge
#  64  usage error
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: acceptance.bash [-h|--help]

Renders the verdict on Plan 00137 against THIS server, after deploy.bash --role server:

  0. sudo allows the entry point without a password, and nothing else
  1. the entry point and its secrets are root-only
  2. the system ansible-playbook and its collections are root-owned, not writable by others
  3. kernel.yama.ptrace_scope is 1 in the running kernel
  4. the cycle timer and the post-boot verify unit are enabled
  5. the user manager pulls in the session restore unit at boot
  6. the deploy clone is readable by this user, and git trusts it
  7. the published result directory is root:<your group> 2750
  8. a dry run of the real entry point completes
  9. the entry point's status command answers

Read-only, apart from sudo -k (see check 0). It runs no playbook.

Exit 0 = ACCEPTED, 1 = REJECTED, 2 = COULD NOT ESTABLISH, 64 = usage error."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "the verdict is about this server's sudoers, root-owned files, kernel and systemd units"
plan_start_log auto

SBIN=/usr/local/sbin/fedora-desktop-self-update
ETC=/etc/fedora-desktop
CLONE=/var/lib/fedora-desktop/deploy
PUBLISHED=/var/lib/fedora-desktop/self-update-status
ANSIBLE_PLAYBOOK=/usr/bin/ansible-playbook
COLLECTIONS=/usr/local/share/fedora-desktop/ansible-collections
REPORT="${PLAN_RUN_DIR}/plan-00137-acceptance-report.md"
readonly SBIN ETC CLONE PUBLISHED ANSIBLE_PLAYBOOK COLLECTIONS REPORT

EXPECTED_CHECKS=(0 1 2 3 4 5 6 7 8 9)
RAN_CHECKS=()
PASS=0
FAIL=0
UNKNOWN=0

NOT_ESTABLISHABLE=(
    "Task 5.3: one full cycle with two live sessions, triggered by a signed commit touching files/var/local/claude-yolo/lib/: play-claude-yolo.yml runs, the sessions are warned, the server reboots, they come back and 'fedora-desktop-self-update status' reports success."
    "An unsigned commit above the signed tip leaves the clone where it is and runs nothing."
    "A play that fails stops the cycle with no reboot, and the result reaches the host-health report (and an alert, once Task 4.5 exists)."
)

report_line() {
    printf -- '%s\n' "$*" >>"${REPORT}"
}

check() {
    RAN_CHECKS+=("$1")
    printf '[%s] %s\n' "$1" "$2"
    report_line ""
    report_line "### [$1] $2"
}

ok() {
    printf '  PASS  %s\n' "$1"
    PASS=$((PASS + 1))
    report_line "- PASS — $1"
}

bad() {
    printf '  FAIL  %s\n' "$1" >&2
    report_line "- FAIL — $1"
    if [[ "$#" -gt 1 ]]; then
        printf '        %s\n' "$2" >&2
        report_line "  - $2"
    fi
    FAIL=$((FAIL + 1))
}

unknown() {
    printf '  UNKNOWN  %s\n' "$1"
    report_line "- COULD NOT ESTABLISH — $1"
    if [[ "$#" -gt 1 ]]; then
        printf '           %s\n' "$2"
        report_line "  - $2"
    fi
    UNKNOWN=$((UNKNOWN + 1))
}

# expect_stat <path> <owner:group> <octal mode> — one PASS or FAIL for a path's ownership.
expect_stat() {
    local path="$1" want_owner="$2" want_mode="$3" got
    if ! got="$(stat -c '%U:%G %a' "${path}" 2>&1)"; then
        bad "${path} cannot be read: ${got}" "run deploy.bash --role server"
        return 0
    fi
    if [[ "${got}" == "${want_owner} ${want_mode}" ]]; then
        ok "${path} is ${want_owner} ${want_mode}"
    else
        bad "${path} is ${got}, want ${want_owner} ${want_mode}" "re-run play-self-update.yml; do not fix it by hand"
    fi
}

# not_writable_by_others <path> <label> — root-owned, and neither group nor world may write.
not_writable_by_others() {
    local path="$1" label="$2" owner mode
    if ! owner="$(stat -c '%U' "${path}" 2>&1)" || ! mode="$(stat -c '%a' "${path}" 2>&1)"; then
        bad "${label} (${path}) cannot be read" "run deploy.bash --role server"
        return 0
    fi
    if [[ "${owner}" == root ]] && (((8#${mode} & 8#022) == 0)); then
        ok "${label} (${path}) is root-owned, mode ${mode}"
    else
        bad "${label} (${path}) is ${owner} ${mode}" "the cycle refuses it with exit 70; re-run play-self-update.yml"
    fi
}

render_verdict() {
    local expected item missing=()
    for expected in "${EXPECTED_CHECKS[@]}"; do
        case " ${RAN_CHECKS[*]} " in
            *" ${expected} "*) ;;
            *) missing+=("${expected}") ;;
        esac
    done
    printf '\n==> NOT ESTABLISHABLE HERE (for the human — never counted as a passed check):\n'
    report_line ""
    report_line "## Not establishable by this gate"
    for item in "${NOT_ESTABLISHABLE[@]}"; do
        printf -- '  - %s\n' "${item}"
        report_line "- ${item}"
    done
    printf '\n==============================================================\n'
    printf 'COVERAGE: %s of %s checks executed (%s assertion(s) passed, %s failed, %s could not be established)\n' \
        "${#RAN_CHECKS[@]}" "${#EXPECTED_CHECKS[@]}" "${PASS}" "${FAIL}" "${UNKNOWN}"
    report_line ""
    report_line "## Verdict"
    report_line "- COVERAGE: ${#RAN_CHECKS[@]} of ${#EXPECTED_CHECKS[@]} checks executed (${PASS} passed, ${FAIL} failed, ${UNKNOWN} could not be established)"
    if [[ "${#missing[@]}" -ne 0 ]]; then
        printf '  NOT RUN: %s\n' "${missing[*]}" >&2
        report_line "- NOT RUN: ${missing[*]}"
    fi
    if [[ "${FAIL}" -eq 0 ]] && [[ "${UNKNOWN}" -eq 0 ]] && [[ "${#missing[@]}" -eq 0 ]]; then
        printf 'ACCEPTED — every declared check ran and every assertion passed.\n'
        printf 'The live-cycle claims above remain unproven; this gate does not speak for them.\n'
        printf '==============================================================\n'
        report_line "- ACCEPTED"
        plan_finish
    fi
    if [[ "${FAIL}" -ne 0 ]] || [[ "${#missing[@]}" -ne 0 ]]; then
        printf 'REJECTED — %s assertion(s) failed, %s declared check(s) never ran.\n' "${FAIL}" "${#missing[@]}" >&2
        report_line "- REJECTED"
        printf '==============================================================\n' >&2
        plan_list_reports
        exit 1
    fi
    printf 'COULD NOT ESTABLISH — nothing failed, but %s check(s) had no evidence to judge.\n' "${UNKNOWN}" >&2
    printf '  This is NOT a pass. Each one names what it needs above.\n' >&2
    printf '==============================================================\n' >&2
    report_line "- COULD NOT ESTABLISH"
    plan_list_reports
    exit 2
}

# --- 0. the grant is exactly one command -----------------------------------------------------
# sudo -k first: a password typed in this terminal in the last few minutes would otherwise make
# `sudo -n true` succeed and read as a blanket NOPASSWD grant. Both answers are captured and
# recorded, so a refusal carries sudo's own reason.
check 0 "sudo allows the entry point without a password, and nothing else"
sudo -k
grantOut=""
for grant in "run" "run --dry-run" "verify" "status"; do
    read -r -a grantArgs <<<"${grant}"
    if grantOut="$(sudo -n -l "${SBIN}" "${grantArgs[@]}" 2>&1)"; then
        ok "sudo -n -l ${SBIN} ${grant} is allowed"
    else
        bad "this user may not run ${SBIN} ${grant} without a password (${grantOut})" \
            "the sudoers drop-in is missing or invalid: re-run play-self-update.yml"
    fi
done
# The grant is exact argument lists: an extra option must be refused by sudo itself.
extraOut=""
if extraOut="$(sudo -n -l "${SBIN}" run --config /nonexistent 2>&1)"; then
    bad "sudo allows ${SBIN} with arguments beyond the four exact lists" \
        "the drop-in must name each argument list; re-run play-self-update.yml"
else
    ok "an extra argument is refused by sudo (${extraOut})"
fi
# A refusal passes only for the two reasons that prove the grant is not blanket: sudo wants a
# password for it, or this user may not run it at all. Any other failure (a broken sudoers, a
# PAM error) says nothing about the grant, so it is COULD NOT ESTABLISH, never a pass.
blanketOut=""
if blanketOut="$(sudo -n true 2>&1)"; then
    bad "sudo -n true also succeeds: this user has a wider passwordless grant" \
        "the server profile removes NOPASSWD:ALL (play-basic-configs.yml); find the other grant with: sudo -l"
elif [[ "${blanketOut}" == *"a password is required"* ]]; then
    ok "sudo -n true needs a password (${blanketOut}), so the passwordless grant is not blanket"
elif [[ "${blanketOut}" == *"is not allowed to"* || "${blanketOut}" == *"not in the sudoers file"* ||
    "${blanketOut}" == *"may not run sudo"* ]]; then
    ok "sudo -n true is not allowed at all (${blanketOut}), so the grant is not blanket"
else
    unknown "sudo -n true failed for a reason that says nothing about the grant (${blanketOut})" \
        "check sudo itself works for this user, then re-run this script"
fi

# --- 1. root-only files ----------------------------------------------------------------------
check 1 "the entry point and its secrets are root-only"
expect_stat "${SBIN}" root:root 700
expect_stat "${ETC}/self-update.conf" root:root 600
expect_stat "${ETC}/self-update.become" root:root 600
expect_stat "${ETC}/self-update.vault" root:root 600
expect_stat "${ETC}/self-update.allowed_signers" root:root 644

# --- 2. the toolchain the plays run ---------------------------------------------------------
check 2 "the system ansible-playbook and its collections are root-owned, not writable by others"
not_writable_by_others "${ANSIBLE_PLAYBOOK}" "system ansible-playbook"
not_writable_by_others "${ANSIBLE_PLAYBOOK%/*}/ansible-config" "system ansible-config (the cycle asks it for the search paths)"
not_writable_by_others "${COLLECTIONS}" "the collections path"

# --- 3. ptrace ------------------------------------------------------------------------------
check 3 "kernel.yama.ptrace_scope is 1 in the running kernel"
ptrace=""
if ! ptrace="$(cat /proc/sys/kernel/yama/ptrace_scope 2>&1)"; then
    bad "/proc/sys/kernel/yama/ptrace_scope cannot be read: ${ptrace}"
elif [[ "${ptrace}" == 1 ]]; then
    ok "ptrace_scope is 1"
else
    bad "ptrace_scope is ${ptrace}, want 1" "re-run play-self-update.yml, which applies and reads it back"
fi

# --- 4. the system units ----------------------------------------------------------------------
check 4 "the cycle timer and the post-boot verify unit are enabled"
for unit in fedora-desktop-self-update.timer fedora-desktop-self-update-verify.service; do
    state=""
    if state="$(systemctl is-enabled "${unit}" 2>&1)" && [[ "${state}" == enabled ]]; then
        ok "${unit} is enabled"
    else
        bad "${unit} is '${state}'" "re-run play-self-update.yml with self_update_enabled: true"
    fi
done
active=""
if active="$(systemctl is-active fedora-desktop-self-update.timer 2>&1)"; then
    ok "fedora-desktop-self-update.timer is ${active}"
else
    bad "fedora-desktop-self-update.timer is '${active}'" "systemctl status fedora-desktop-self-update.timer --no-pager | cat"
fi

# --- 5. restore ---------------------------------------------------------------------------------
check 5 "the user manager pulls in the session restore unit at boot"
deps=""
if ! deps="$(systemctl --user list-dependencies default.target --no-pager 2>&1)"; then
    unknown "the user manager could not be asked (${deps})" "run this from a login shell of the session user"
elif grep -q 'ccy-sessions-restore.service' <<<"${deps}"; then
    ok "default.target wants ccy-sessions-restore.service"
else
    bad "the restore unit is not pulled in, so the reboot would end the sessions for good" \
        "declare ccy_restore_sessions: true (or RUN_BASH_CCY_RESTORE_SESSIONS=1) and re-run play-claude-yolo.yml"
fi

# --- 6. the clone -------------------------------------------------------------------------------
check 6 "the deploy clone is readable by this user, and git trusts it"
head=""
if head="$(git -C "${CLONE}" rev-parse --short HEAD 2>&1)"; then
    ok "git reads the clone at ${head}"
else
    bad "git cannot read ${CLONE}: ${head}" \
        "the play adds it to this user's safe.directory; without it the ledger records nothing"
fi

# --- 7. the published result --------------------------------------------------------------------
check 7 "the published result directory is root:<your group> 2750"
expect_stat "${PUBLISHED}" "root:$(id -gn)" 2750
if [[ -e "${PUBLISHED}/result" ]]; then
    if [[ -r "${PUBLISHED}/result" ]]; then
        ok "this user can read ${PUBLISHED}/result"
    else
        bad "${PUBLISHED}/result exists but this user cannot read it" "the host-health report would show the cycle as not checked"
    fi
else
    ok "no result yet: no cycle has run (the host-health report says so after its bound)"
fi

# --- 8. dry run ----------------------------------------------------------------------------------
check 8 "a dry run of the real entry point completes"
dryOut=""
dryRc=0
dryOut="$(sudo -n "${SBIN}" run --dry-run 2>&1)" || dryRc=$?
report_line '```'
report_line "${dryOut}"
report_line '```'
if [[ "${dryRc}" -ne 0 ]]; then
    bad "run --dry-run exited ${dryRc}" "${dryOut}"
elif grep -qE '^SELF-UPDATE-CYCLE (nothing|dry-run)$' <<<"${dryOut}"; then
    ok "run --dry-run: $(grep -E '^SELF-UPDATE-CYCLE ' <<<"${dryOut}")"
    awk '/^RUN / {print "        would " $0}' <<<"${dryOut}"
else
    bad "run --dry-run exited 0 without its SELF-UPDATE-CYCLE line" "${dryOut}"
fi

# --- 9. status -----------------------------------------------------------------------------------
check 9 "the entry point's status command answers"
statusOut=""
statusRc=0
statusOut="$(sudo -n "${SBIN}" status 2>&1)" || statusRc=$?
report_line "- status: ${statusOut}"
if [[ "${statusRc}" -eq 0 ]]; then
    ok "status exited 0"
    awk '{print "        " $0}' <<<"${statusOut}"
else
    bad "status exited ${statusRc}" "${statusOut}"
fi

render_verdict
