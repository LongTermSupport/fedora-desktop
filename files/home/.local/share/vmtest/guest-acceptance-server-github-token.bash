#!/usr/bin/bash
# guest-acceptance-server-github-token.bash — the assertion set for the ONE scenario that
# puts a real GitHub credential in a guest (Plan 00121; Plan 00063 Tasks 3.3 and 3.4).
#
# WHY THIS IS A SEPARATE SCRIPT. guest-acceptance-server.bash check 3 asserts
# `github_accounts: {}` in localhost.yml — definitionally false when an identity IS
# configured. Reusing it would produce a scenario that fails for the one reason that is not
# a defect. This script asserts the opposite half: that the credential worked, and that
# nothing of it survived. The general server checks (podman, tmux, pyenv, CCY, sshd) are
# proved on every `server-fast-provision` run against the same base and are not repeated.
#
# THE SECRETS ARE HANDED BACK IN, ON PURPOSE. "No secret bytes survived" cannot be checked
# by a script that does not know the bytes. The host writes them to VMTEST_NEEDLES_FILE
# (0600, tmpfs) for this script alone, and the FIRST thing below is to read them into memory
# and unlink that file — so the window in which they exist on disk is this script's own
# startup, and no later scan has to special-case its own fixture. A needles file still
# present at the end is itself a failure.
#
# This is what makes the scan non-vacuous. 00110 DESIGN.md:1590-1596 records the trap: on
# the default scenarios `RUN_BASH_GITHUB_ACCOUNTS=none`, so grepping for secrets proves
# nothing because none was ever supplied. Here they were supplied, they demonstrably reached
# the guest, and the assertion is that they are no longer anywhere they should not be.
#
# NOTHING HERE MAY PRINT A SECRET. Every detail string is a field name, a path or a count —
# never a matched value and never a line of context. The transcript is read by a human and
# archived. Probes are captured with 2>&1 and judged rather than silenced, which is the
# repo's idiom; what is printed from a capture is chosen line by line.
#
# Contract (helpers/vmtest/transcript.py parses exactly these lines):
#   VMTEST-CHECK-PLANNED <n>                declared BEFORE the first check
#   VMTEST-CHECK pass|fail|skip <name> [detail]
#   VMTEST-EVIDENCE <key>=<value>
#   VMTEST-CHECKS-DONE total=N passed=N failed=N skipped=N
#
# Inputs (environment, set by the host over SSH):
#   VMTEST_COMMIT        the 40-hex commit the guest was told to provision from
#   VMTEST_GITHUB_USER   the throwaway GitHub account this run authenticated as
#   VMTEST_NEEDLES_FILE  0600 file, one secret per line, unlinked immediately below
set -uo pipefail

PLANNED=12
readonly PLANNED
REPO="${HOME}/Projects/fedora-desktop"
readonly REPO

total=0
passed=0
failed=0
skipped=0

check() {
    local status="${1:?}" name="${2:?}" detail="${3:-}"
    total=$((total + 1))
    case "${status}" in
        pass) passed=$((passed + 1)) ;;
        fail) failed=$((failed + 1)) ;;
        skip) skipped=$((skipped + 1)) ;;
        *)
            echo "ERROR: check status must be pass|fail|skip, got ${status}" >&2
            exit 70
            ;;
    esac
    detail="$(printf '%s' "${detail}" | tr '\n' ' ')"
    if [[ -n "${detail}" ]]; then
        printf 'VMTEST-CHECK %s %s %s\n' "${status}" "${name}" "${detail}"
    else
        printf 'VMTEST-CHECK %s %s\n' "${status}" "${name}"
    fi
}

evidence() {
    printf 'VMTEST-EVIDENCE %s=%s\n' "${1:?}" "$(printf '%s' "${2-}" | tr '\n' ' ')"
}

# ── the needles, read once and removed before anything else runs ──────────────────────
needles=()
needles_path="${VMTEST_NEEDLES_FILE:-}"
if [[ -z "${needles_path}" || ! -r "${needles_path}" ]]; then
    # Without the needles the whole point of this scenario cannot be judged. Reporting
    # "clean" here would be a pass earned by not looking.
    echo "ERROR: VMTEST_NEEDLES_FILE is unset or unreadable; the secret-residue checks cannot run" >&2
    exit 70
fi
while IFS= read -r line; do
    if [[ -n "${line}" ]]; then
        needles+=("${line}")
    fi
done < "${needles_path}"
rm -f "${needles_path}"
if [[ "${#needles[@]}" -eq 0 ]]; then
    echo "ERROR: VMTEST_NEEDLES_FILE held no secrets; the residue checks would search for nothing" >&2
    exit 70
fi

# needle_hits <path> — how many of the needles appear under a path. Counts only; the matched
# text is never captured, so it cannot reach the transcript by accident. -F because a secret
# is opaque bytes and not a pattern; -q because here even a filename can be sensitive; -s so
# an unreadable path is silent rather than noisy on stderr.
needle_hits() {
    local target="${1:?}" needle hits=0
    for needle in "${needles[@]}"; do
        if grep -r -F -q -s -- "${needle}" "${target}"; then
            hits=$((hits + 1))
        fi
    done
    printf '%s' "${hits}"
}

printf 'VMTEST-CHECK-PLANNED %d\n' "${PLANNED}"

# ── 1. the repo is at the pinned commit ───────────────────────────────────────────────
head=""
if head="$(git -C "${REPO}" rev-parse HEAD 2>&1)"; then
    if [[ "${head}" == "${VMTEST_COMMIT:-}" ]]; then
        check pass repo-cloned-at-pinned-commit "${head:0:12}"
    else
        check fail repo-cloned-at-pinned-commit "HEAD ${head:0:12} != requested ${VMTEST_COMMIT:-unset}"
    fi
else
    check fail repo-cloned-at-pinned-commit "no git checkout at ${REPO}: ${head}"
    head=""
fi

# ── 2. the clone used SSH, not HTTPS (00063: "SSH-only git auth") ─────────────────────
# The empty-identity path clones over HTTPS. With an identity configured the remote must be
# git@github.com, or this scenario proved nothing about SSH auth.
origin=""
if origin="$(git -C "${REPO}" remote get-url origin 2>&1)"; then
    if [[ "${origin}" == git@github.com:* ]]; then
        check pass git-remote-is-ssh
    else
        check fail git-remote-is-ssh "origin is '${origin}', expected git@github.com:*"
    fi
else
    check fail git-remote-is-ssh "could not read origin: ${origin}"
fi

# ── 3. localhost.yml carries the identity (the inverse of the no-identity path) ───────
localhost_yml="${REPO}/environment/localhost/host_vars/localhost.yml"
if grep -sqE '^github_accounts:' "${localhost_yml}" &&
    ! grep -sqE '^github_accounts: \{\}$' "${localhost_yml}"; then
    check pass localhost-yml-has-identity
else
    check fail localhost-yml-has-identity "github_accounts is absent or empty in localhost.yml"
fi

# ── 4. gh authenticated non-interactively with the scoped token ───────────────────────
# `gh auth status` never prints the token itself, but only its FIRST line is reported on
# failure: there is no reason to copy more of it into an archived transcript.
gh_status=""
if gh_status="$(gh auth status 2>&1)"; then
    check pass gh-authenticated-non-interactively "as ${VMTEST_GITHUB_USER:-unset}"
else
    check fail gh-authenticated-non-interactively "$(printf '%s\n' "${gh_status}" | grep -m1 .)"
fi

# ── 5. the login SSH key exists and is passphrase-protected ───────────────────────────
# `ssh-keygen -y -P ''` SUCCEEDS on an unprotected key and fails on a protected one, so here
# the failure is the pass. On the bad path its stdout is the public key; only the key TYPE
# is reported, which identifies the algorithm and discloses nothing.
if [[ -f "${HOME}/.ssh/id" ]]; then
    pubkey=""
    if pubkey="$(ssh-keygen -y -P '' -f "${HOME}/.ssh/id" 2>&1)"; then
        check fail login-key-passphrase-protected "${HOME}/.ssh/id loaded with an EMPTY passphrase (${pubkey%% *})"
    else
        check pass login-key-passphrase-protected
    fi
else
    check fail login-key-passphrase-protected "no ~/.ssh/id was generated"
fi

# ── 6. no ssh-agent survived the run (00063 Task 3.4) ─────────────────────────────────
# run.bash starts an agent to load the passphrase-protected key and must tear it down. One
# left running holds an UNLOCKED key for anything that can reach its socket. `pgrep -c`
# exits 1 when the count is zero — the ordinary case here — so the status is consumed
# explicitly rather than discarded. `-x` matches the process NAME, so this script's own
# argv can never be the thing it finds.
agents=0
agent_count=""
if agent_count="$(pgrep -u "$(id -u)" -c -x ssh-agent)"; then
    agents="${agent_count}"
fi
if [[ "${agents}" -eq 0 ]]; then
    check pass no-ssh-agent-left-running
else
    check fail no-ssh-agent-left-running "${agents} ssh-agent process(es) still running for this user"
fi

# ── 7. no agent socket left behind ────────────────────────────────────────────────────
# A glob, not `find | wc -l`: piping into `wc` discards find's status, so a search that
# FAILED would report zero sockets and pass — a check that cannot fail. `find` also exits
# non-zero merely for stepping on another user's 0700 directory under /tmp, so its status is
# not usable as a success signal here anyway.
shopt -s nullglob
socket_paths=(/tmp/ssh-*/agent.*)
shopt -u nullglob
sockets="${#socket_paths[@]}"
if [[ "${sockets}" -eq 0 ]]; then
    check pass no-ssh-agent-socket-left
else
    check fail no-ssh-agent-socket-left "${sockets} ssh-agent socket(s) under /tmp"
fi

# ── 8. the transient passphrase file is gone (00063 Task 2.2) ─────────────────────────
if [[ ! -e /tmp/.github_ssh_pp ]]; then
    check pass no-ssh-passphrase-tmp-left
else
    check fail no-ssh-passphrase-tmp-left "/tmp/.github_ssh_pp still exists"
fi

# ── 9. no askpass helper survived ─────────────────────────────────────────────────────
# hl_ssh_agent_start and hl_sudo_askpass_start each mktemp a 0700 helper that CATS a secret
# file. One left behind is a reader for a secret that should also be gone.
# A glob for the same reason as check 7: nothing here may depend on a search's exit status,
# because the answer this check reports must not be producible by failing to look.
askpass=0
shopt -s nullglob
for candidate in /tmp/*; do
    if [[ -f "${candidate}" && -x "${candidate}" ]] &&
        grep -sqE '^cat (--)? *"?\$?HL_' "${candidate}"; then
        askpass=$((askpass + 1))
    fi
done
shopt -u nullglob
if [[ "${askpass}" -eq 0 ]]; then
    check pass no-askpass-helper-left
else
    check fail no-askpass-helper-left "${askpass} askpass-shaped helper(s) under /tmp"
fi

# ── 10. no secret bytes in any process environment ────────────────────────────────────
# /proc/<pid>/environ is readable by the owning user, so a secret exported into a surviving
# process is readable by anything running as that user. -a because environ is NUL-separated
# and grep would otherwise treat it as binary; -q and -s so neither a match nor an
# unreadable entry produces output.
env_hits=0
for needle in "${needles[@]}"; do
    if grep -q -F -a -s -- "${needle}" /proc/[0-9]*/environ; then
        env_hits=$((env_hits + 1))
    fi
done
if [[ "${env_hits}" -eq 0 ]]; then
    check pass no-secret-bytes-in-process-environment
else
    check fail no-secret-bytes-in-process-environment "${env_hits} of ${#needles[@]} secret(s) found in a process environment"
fi

# ── 11. no secret bytes in cloud-init user-data ───────────────────────────────────────
# The metadata service serves user-data forever, so a secret embedded there is permanent.
# This guest's secrets arrived over SSH, so the assertion is that the OTHER route was not
# used — falsifiable precisely because the bytes DO exist on this box, just not here.
cloud_hits=0
for cloud_path in /var/lib/cloud/instance/user-data.txt /var/lib/cloud/instance/user-data.txt.i; do
    if [[ -r "${cloud_path}" ]]; then
        cloud_hits=$((cloud_hits + $(needle_hits "${cloud_path}")))
    fi
done
if [[ "${cloud_hits}" -eq 0 ]]; then
    check pass no-secret-bytes-in-cloud-init-user-data
else
    check fail no-secret-bytes-in-cloud-init-user-data "${cloud_hits} secret occurrence(s) in cloud-init user-data"
fi

# ── 12. no secret bytes left on disk where the run worked ─────────────────────────────
# $HOME and /tmp are where run.bash wrote its transient files. The needles file this script
# was handed is already unlinked, so it cannot match itself.
disk_hits=$(($(needle_hits "${HOME}") + $(needle_hits /tmp)))
if [[ "${disk_hits}" -eq 0 ]]; then
    check pass no-secret-bytes-on-disk
else
    check fail no-secret-bytes-on-disk "${disk_hits} secret(s) still present under \$HOME or /tmp"
fi

# ── evidence (never a check; §6.6 rule 04) ────────────────────────────────────────────
evidence boot_id "$(cat /proc/sys/kernel/random/boot_id 2>&1)"
evidence os_release "$(. /etc/os-release && printf '%s' "${PRETTY_NAME}")"
evidence kernel "$(uname -r)"
evidence repo_commit "${head}"
evidence github_user "${VMTEST_GITHUB_USER:-unset}"
evidence needles_checked "${#needles[@]}"
# The harness's own fixture must not outlive the checks that depended on its removal.
if [[ -e "${needles_path}" ]]; then
    echo "ERROR: the needles file ${needles_path} still exists after the run" >&2
    exit 70
fi
evidence needles_file_removed "yes"

printf 'VMTEST-CHECKS-DONE total=%d passed=%d failed=%d skipped=%d\n' "${total}" "${passed}" "${failed}" "${skipped}"
if [[ "${total}" -ne "${PLANNED}" ]]; then
    echo "ERROR: ${total} checks ran but ${PLANNED} were declared; this script is inconsistent" >&2
    exit 70
fi
exit 0
