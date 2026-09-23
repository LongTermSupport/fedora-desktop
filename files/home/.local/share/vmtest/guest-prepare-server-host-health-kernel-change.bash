#!/usr/bin/bash
# guest-prepare-server-host-health-kernel-change.bash — the fixture for the
# server host-health kernel-change scenario, run INSIDE the guest after run.bash
# and BEFORE the harness reboots it (Plan 00109 Task 3.2, DESIGN-server-route.md §4).
#
# The claim under test needs two boots: a status document collected under one
# kernel, read by a login shell running a different one. Only the first boot can
# set that up, so this script does the setting up and the checker after the
# reboot does all the judging.
#
# It emits NO VMTEST-CHECK markers. The transcript declares its check count once,
# and a second declaration — or checks counted before the reboot that the
# accounting does not expect — is an error by §6.6 rule 02. Everything observed
# here is written to an evidence file that the checker reads and turns into
# checks, so there is one place that counts and one place that judges.
#
# Two collections, because two of the claims contradict each other on one host:
# a clean server login must be SILENT, and a boot-scoped fault must be DEMOTED
# after a reboot. The first is captured while the guest is clean; a unit is then
# made to fail, so the boot-scoped section has something to say, and the second
# collection is the document that survives into the next boot.
#
# Fail-fast throughout: a fixture that half-applied would leave the checker
# judging a scenario nobody set up, and its failures would read as defects in the
# code under test. Every step that cannot complete aborts the run.
#
# This is a throwaway guest, built and destroyed by the lab. Installing a kernel,
# selecting it, and breaking a unit on purpose are the fixture — not host
# administration. Nothing here runs on, or is deployed to, a real machine.
#
# Writes: ~/.vmtest/host-health-prepared.env — `KEY=<shell-quoted value>`, one per line,
# which the checker sources. Multi-line captures are base64'd on top of that so the file
# stays readable in a transcript; the quoting is what makes it PARSEABLE, and it is applied
# in `record` so no future value can opt out of it.
set -euo pipefail

REPO="${HOME}/Projects/fedora-desktop"
STATE_DIR="${XDG_STATE_HOME:-${HOME}/.local/state}/fedora-desktop"
DOCUMENT="${STATE_DIR}/host-status.json"
SNIPPET="${HOME}/.bashrc-includes/host-health-report.bash"
COLLECT_TIMER="host-health-collect.timer"
COLLECT_SERVICE="host-health-collect.service"
# The unit made to fail so post-boot health has a finding to demote. Named for the
# lab so a reader of the transcript knows it is a fixture and not a real fault.
FIXTURE_UNIT="vmtest-health-fixture.service"
EVIDENCE_DIR="${HOME}/.vmtest"
EVIDENCE="${EVIDENCE_DIR}/host-health-prepared.env"
SELF_KEY="${EVIDENCE_DIR}/selfscp"
readonly REPO STATE_DIR DOCUMENT SNIPPET COLLECT_TIMER COLLECT_SERVICE FIXTURE_UNIT
readonly EVIDENCE_DIR EVIDENCE SELF_KEY

log() { printf '==> prepare: %s\n' "$*" >&2; }
die() {
    printf 'ERROR: prepare: %s\n' "$*" >&2
    exit 1
}
b64() { printf '%s' "${1-}" | base64 -w0; }

mkdir -p "${EVIDENCE_DIR}"
chmod 0700 "${EVIDENCE_DIR}"
: >"${EVIDENCE}"
# SHELL-QUOTED, always. The checker `source`s this file, so a value carrying a shell
# metacharacter is not a mangled string — it is a syntax error that aborts the source and
# leaves EVERY key after it unset. One of the values recorded here is the text
# `probe_results` produces for a failed unit, which ends in `(system scope)`, and `(`
# alone was enough: `source` exited 2 and the checker then judged a record that was not
# there. `%q` costs nothing and makes that class of value impossible to write.
record() {
    printf '%s=%q\n' "${1:?}" "${2-}" >>"${EVIDENCE}" ||
        die "could not append ${1} to ${EVIDENCE}; the checker would judge a record with a hole in it"
}

# collect_once <prefix> — run the collector synchronously and record how it went.
# SuccessExitStatus=3 in the unit: the collector exits 3 when it has findings,
# which is a successful run that found something, not a failure to run.
collect_once() {
    local prefix="${1:?}" rc=0 status
    systemctl --user start "${COLLECT_SERVICE}" || rc=$?
    status="$(systemctl --user show -p Result -p ExecMainStatus --value "${COLLECT_SERVICE}" 2>&1 | tr '\n' ' ')"
    record "${prefix}_COLLECT_RC" "${rc}"
    record "${prefix}_COLLECT_STATUS_B64" "$(b64 "${status}")"
    [[ -r "${DOCUMENT}" ]] || die "no status document at ${DOCUMENT} after starting ${COLLECT_SERVICE} (${status})"
    # The document's identity after THIS collection. The readability guard above is
    # satisfied by the document the previous collection left behind, so on its own it
    # cannot tell "collected again" from "did not run and the old file is still there".
    # The checker compares the two and requires them to differ.
    record "${prefix}_DOCUMENT_SHA" "$(sha256sum "${DOCUMENT}" | cut -d' ' -f1)"
}

# login_once <prefix> — what an interactive login shell prints, captured and never
# judged here. A login shell rather than `bash -i`: it is the whole chain a user
# meets, from ~/.bash_profile through ~/.bashrc to the include the play deployed.
#
# THE TWO STREAMS ARE KEPT APART, and only stdout carries the report.
# `scripts/test-host-health-login-snippet.bash` settled this: stdout is the stream a
# report travels on and the only one an `scp` is corrupted by. Combining them here would
# fold in `bash: cannot set terminal process group / no job control in this shell` — the
# 116 bytes an interactive shell with no controlling terminal always emits, and `ssh`
# without `-t` never gives it one. "A clean login is silent" would then be false on every
# guest for a reason that has nothing to do with this plan. stderr is recorded beside it,
# so a reader sees that noise rather than wondering where it went.
login_once() {
    local prefix="${1:?}" rc=0 output errors errors_file
    errors_file="$(mktemp)"
    output="$(bash -lic true </dev/null 2>"${errors_file}")" || rc=$?
    errors="$(cat "${errors_file}")"
    rm -f "${errors_file}"
    record "${prefix}_LOGIN_RC" "${rc}"
    record "${prefix}_LOGIN_B64" "$(b64 "${output}")"
    record "${prefix}_LOGIN_STDERR_B64" "$(b64 "${errors}")"
    log "${prefix}: a login shell printed ${#output} bytes on stdout, ${#errors} on stderr"
}

# select_second_kernel <running-kernel> — leave this guest holding a kernel it is not
# running, with the next boot pointed at it.
#
# One path, always taken: ask every enabled repo what kernel versions exist, install the
# newest that is NOT the one running, then confirm an installed kernel other than the
# running one is now on disk. Not restricted to the release repo: a guest running the
# release kernel needs a newer one and a guest running the newest needs an older one, and
# "the newest available that is not this one" answers both without a branch per case.
#
# A guest that still has only one kernel afterwards is a hard failure. There is no
# version of this scenario that proves anything without a second kernel, and a run that
# quietly rebooted into the same one would pass check 7 nowhere and confuse every check
# after it.
#
# BOTH QUERIES ARE JUDGED BEFORE THEY ARE READ AS VERSIONS. A query that fails still
# prints — `rpm -q` puts `package kernel-core is not installed` on stdout and exits 1 —
# and a `while read` fed straight from it takes that sentence as a candidate version.
# Nothing catches the exit status there: a process substitution's is not part of the
# pipeline, so `pipefail` never sees it. The sentence is then rejected for naming no
# `/boot/vmlinuz-`, and the run dies about the bootloader for a fault that belongs to
# rpm — the wrong file, by the same reasoning section 1 aborts rather than records.
# The boot directory is a parameter with its real default, not a global read from the
# environment: a test needs a directory it owns to stand in for /boot, and this gate runs
# on the user's own machine where writing into the real one would be intolerable.
#
# EVERY COMMAND THAT CHANGES THE GUEST IS CHECKED HERE, not left to `set -e`. The caller
# takes this function's answer with `target_kernel="$(select_second_kernel …)"`, and bash
# switches errexit OFF inside a command substitution unless `inherit_errexit` is set —
# measured on bash 5.2.15, where the substituted form runs past a failure and the caller
# still exits 0. So `set -e` covers nothing in this function except `die` itself. Anything
# added below needs its own `|| die`.
#
# select_second_kernel <running-kernel> [boot-dir]
select_second_kernel() {
    local running_kernel="${1:?}" boot_dir="${2:-/boot}"
    local query_errors available installed wanted candidate target_kernel default_kernel

    command -v grubby >/dev/null || die "no grubby in this guest; the boot entry cannot be selected"
    # Both queries write here — dnf's and rpm's — so it is not named for either.
    query_errors="${EVIDENCE_DIR}/kernel-query.err"
    available=""
    # dnf5's repoquery answers with every available version unless --latest-limit is given,
    # and that is what this needs: a guest built from a current image IS running the newest
    # build, so an answer of the newest only would offer nothing but itself and refuse.
    # dnf4's --showduplicates does not exist in dnf5, which rejects it as an unknown argument.
    if ! available="$(sudo -n dnf -q repoquery --queryformat '%{version}-%{release}.%{arch}\n' kernel-core 2>"${query_errors}")"; then
        die "asking dnf which kernel-core versions exist failed: $(cat "${query_errors}")"
    fi
    # An empty answer is a guest whose repositories are unusable, NOT a guest with no
    # other kernel. Told apart here because the two need different things done about
    # them, and one message for both sends a reader looking for a kernel that was never
    # the problem.
    [[ -n "${available//[[:space:]]/}" ]] ||
        die "dnf listed no kernel-core at all; this guest's repositories cannot answer for the kernel"

    wanted=""
    while read -r candidate; do
        [[ -n "${candidate}" ]] || continue
        [[ "${candidate}" == "${running_kernel}" ]] && continue
        wanted="${candidate}"
        break
    done < <(printf '%s\n' "${available}" | sort -Vr)
    [[ -n "${wanted}" ]] ||
        die "every kernel-core the repos offer is ${running_kernel}; this guest cannot be given a second kernel"
    record PREPARED_WANTED_KERNEL "${wanted}"

    log "installing kernel ${wanted} alongside the running ${running_kernel}"
    sudo -n dnf -y install "kernel-${wanted}" >&2 ||
        die "installing kernel-${wanted} failed; this guest has no second kernel to boot into"

    # `rpm` in its own conventional case, `dnf repoquery` above in its: rpm tag names are
    # case-insensitive, but dnf5's format tags are documented lowercase and the long
    # `--queryformat` is spelled out on both, so neither depends on an abbreviation.
    # stderr to a file, as the dnf query above does, rather than folded in with 2>&1: rpm
    # can warn on stderr and still exit 0, and a warning merged into this capture is read
    # as a candidate version. Harmless today — it names no vmlinuz, so the loop steps over
    # it — but only by luck, and the failure text belongs in the message either way.
    installed=""
    if ! installed="$(rpm -q kernel-core --queryformat '%{VERSION}-%{RELEASE}.%{ARCH}\n' 2>"${query_errors}")"; then
        die "asking rpm which kernel-core packages are installed failed: ${installed} $(cat "${query_errors}")"
    fi

    target_kernel=""
    while read -r candidate; do
        [[ -n "${candidate}" ]] || continue
        [[ "${candidate}" == "${running_kernel}" ]] && continue
        [[ -r "${boot_dir}/vmlinuz-${candidate}" ]] || continue
        target_kernel="${candidate}"
        break
    done < <(printf '%s\n' "${installed}" | sort -Vr)

    [[ -n "${target_kernel}" ]] ||
        die "dnf installed ${wanted} but no kernel other than ${running_kernel} has a ${boot_dir}/vmlinuz-*; the guest cannot reboot into a different one"
    record PREPARED_TARGET_KERNEL "${target_kernel}"

    log "selecting ${target_kernel} for the next boot"
    sudo -n grubby --set-default "${boot_dir}/vmlinuz-${target_kernel}" >&2 ||
        die "grubby refused to make ${boot_dir}/vmlinuz-${target_kernel} the default boot entry"
    default_kernel=""
    default_kernel="$(sudo -n grubby --default-kernel)" ||
        die "grubby could not name the default boot entry after being given ${target_kernel}"
    record PREPARED_DEFAULT_KERNEL "${default_kernel}"
    [[ "${default_kernel}" == "${boot_dir}/vmlinuz-${target_kernel}" ]] ||
        die "grubby reports ${default_kernel} as the default, not ${boot_dir}/vmlinuz-${target_kernel}"

    # The one thing a caller would capture. Every other word this function produces —
    # its own log lines, dnf's output, grubby's — is already on stderr.
    printf '%s\n' "${target_kernel}"
}

# ── 1. the play's artefacts, or there is no scenario to prepare ───────────────────────
# Aborts rather than records: an absent snippet is not a finding about the boot
# predicate, it is a run.bash that did not deploy the play, and carrying it into
# the checker as a failed kernel check would send a reader to the wrong file.
log "confirming play-host-health-login-report.yml deployed its server delivery"
[[ -r "${SNIPPET}" ]] || die "no login snippet at ${SNIPPET}; the optional play did not run, or ran the desktop branch"
[[ -d "${REPO}" ]] || die "no checkout at ${REPO}; the snippet's PYTHONPATH would not resolve"
sudo_probe=""
if ! sudo_probe="$(sudo -n true 2>&1)"; then
    die "no passwordless sudo in this guest (${sudo_probe}); the fixture cannot install a kernel"
fi

# ── 2. the timer as the play left it, recorded BEFORE it is taken out of the way ──────
timer_enabled=""
if ! timer_enabled="$(systemctl --user is-enabled "${COLLECT_TIMER}" 2>&1)"; then
    die "${COLLECT_TIMER} is not enabled (${timer_enabled}); the play arms it, so this guest is not the one described"
fi
record PREPARED_TIMER_ENABLED "${timer_enabled}"
timer_next="$(systemctl --user list-timers --all --no-pager "${COLLECT_TIMER}" 2>&1)"
record PREPARED_TIMER_NEXT_B64 "$(b64 "${timer_next}")"
log "timer is ${timer_enabled}"

# ── 3. a clean host, collected and read ───────────────────────────────────────────────
# "A clean server login is silent" is the claim that decides whether this surface
# survives contact with a user, and it can only be asked while the guest is clean.
running_kernel="$(uname -r)"
record PREPARED_RUNNING_KERNEL "${running_kernel}"
log "collecting the host status under kernel ${running_kernel}, while nothing is wrong"
collect_once CLEAN
login_once CLEAN

# ── 4. something genuinely wrong, in the boot-scoped section ──────────────────────────
# Without this the demotion claim is untestable here: a server has no dkms and a
# healthy guest has no failed units, so post-boot health is empty and a reboot has
# nothing to demote. A check that passes because the population it judges is empty
# is the shape this plan exists to catch.
log "making ${FIXTURE_UNIT} fail, so the boot-scoped section has a finding"
printf '%s\n' \
    '[Unit]' \
    'Description=vmtest fixture: a unit that fails so post-boot health has a finding' \
    '[Service]' \
    'Type=oneshot' \
    'ExecStart=/usr/bin/false' |
    sudo -n tee "/etc/systemd/system/${FIXTURE_UNIT}" >/dev/null
sudo -n systemctl daemon-reload
fixture_rc=0
sudo -n systemctl start "${FIXTURE_UNIT}" || fixture_rc=$?
[[ "${fixture_rc}" -ne 0 ]] || die "${FIXTURE_UNIT} succeeded; the fixture must leave a failed unit behind"
record PREPARED_FIXTURE_UNIT "${FIXTURE_UNIT}"
# The exact line probe_results.failed_unit_findings produces for it. Written here
# rather than re-derived in the checker so both sides cannot drift into agreeing
# about a string neither of them got from the code under test.
record PREPARED_FIXTURE_FINDING "${FIXTURE_UNIT}: failed (system scope)"

log "collecting again, with the failed unit in place"
collect_once FINDING
login_once FINDING

document_kernel="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("kernel",""))' "${DOCUMENT}")"
record PREPARED_DOCUMENT_KERNEL "${document_kernel}"
record PREPARED_DOCUMENT_B64 "$(base64 -w0 <"${DOCUMENT}")"
# The document that must survive the reboot is the SECOND collection's, and
# `FINDING_DOCUMENT_SHA` already names it. Not copied to a second key: two names for one
# fact is how a consumer ends up comparing against whichever of them last got updated.
log "document names kernel ${document_kernel}"

# ── 5. a real scp through this host's own sshd ────────────────────────────────────────
# Not a shell sourcing the snippet — the actual path an unconditional print breaks.
# Fedora's bash reads ~/.bashrc for the non-interactive shell sshd starts here
# (SSH_SOURCE_BASHRC), so anything on stdout corrupts the transfer. A key of the
# guest's own is the only way to reach its sshd from inside it; the lab's private
# key never enters a guest.
log "proving an scp through this guest's sshd still completes"
rm -f "${SELF_KEY}" "${SELF_KEY}.pub"
ssh-keygen -q -t ed25519 -N '' -C 'vmtest-self-scp' -f "${SELF_KEY}"
mkdir -p "${HOME}/.ssh"
chmod 0700 "${HOME}/.ssh"
cat "${SELF_KEY}.pub" >>"${HOME}/.ssh/authorized_keys"
chmod 0600 "${HOME}/.ssh/authorized_keys"
scp_rc=0
scp_output="$(scp -q -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    -o LogLevel=ERROR -i "${SELF_KEY}" \
    "${USER}@localhost:/etc/os-release" "${EVIDENCE_DIR}/scp-before.out" 2>&1)" || scp_rc=$?
record PREPARED_SCP_RC "${scp_rc}"
record PREPARED_SCP_OUTPUT_B64 "$(b64 "${scp_output}")"
scp_bytes=0
if [[ -r "${EVIDENCE_DIR}/scp-before.out" ]]; then
    scp_bytes="$(stat -c %s "${EVIDENCE_DIR}/scp-before.out")"
fi
record PREPARED_SCP_BYTES "${scp_bytes}"
log "scp exited ${scp_rc} having transferred ${scp_bytes} bytes"

# ── 6. take the collector out of the way of the reboot ────────────────────────────────
# The subject is a STALE document meeting a new kernel, so the collector must not
# run in between — a re-collection after the reboot would name the new kernel and
# the mismatch would never exist to be reported. Disabled rather than masked: the
# unit files stay where the play put them, and the checker proves the document it
# reads could not have been rewritten. Its armed state is already recorded above.
log "stopping the collection timer so the document stays as collected"
systemctl --user disable --now "${COLLECT_TIMER}"
timer_after=""
timer_after_rc=0
# `is-enabled` exits non-zero for every disabled state, so the exit code and the
# word it prints are both recorded and the checker decides what they mean.
timer_after="$(systemctl --user is-enabled "${COLLECT_TIMER}" 2>&1)" || timer_after_rc=$?
record PREPARED_TIMER_AFTER "${timer_after}"
record PREPARED_TIMER_AFTER_RC "${timer_after_rc}"

# ── 7. a second kernel, and the next boot pointed at it ───────────────────────────────
# A function rather than the straight line it was, because this is the one step of the
# route no machine could reach: the container has no dnf, rpm or grubby, and a guest that
# runs it has already spent twenty minutes getting here. As a function it can be lifted
# out and driven against stubs — see scripts/test-vmtest-kernel-selection.bash, which is
# where its refusals are proved.
#
# Captured, not re-read from the evidence file: that file is the CHECKER's copy, and a
# second reader of it here is two names for one fact. `die` inside the substitution exits
# only the subshell, so the assignment carries the failure out and `set -e` stops the run
# — proved in that test rather than assumed, because a swallowed refusal here would leave
# the guest rebooting into the kernel it already runs.
target_kernel="$(select_second_kernel "${running_kernel}")"

chmod 0600 "${EVIDENCE}"
log "prepared: collected under ${running_kernel}, next boot is ${target_kernel}"
printf 'VMTEST-GUEST-PREPARE-DONE\n'
