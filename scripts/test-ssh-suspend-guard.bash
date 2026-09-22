#!/usr/bin/env bash
#
# Unit test for ssh-suspend-guard's session detection.
#
# WHY THIS EXISTS. The guard previously asked `ss -tnp state established 'sport = :22'`
# and grepped for sshd. On a machine whose sshd listens elsewhere that is never true, so
# the inhibit lock was never taken and the machine suspended mid-session — the one outcome
# the guard exists to prevent, arriving silently. Nothing could have caught it: the script
# is a daemon loop with no test, and the failure only shows on a host that moved its port.
#
# So the detection is a function now, and this drives it against captured `ss` output
# rather than a live socket table. That is the whole point — the cases that matter
# (non-standard port, sshd-session, outbound client) cannot be manufactured on demand on
# the machine running the test, and must not need to be.
#
# It reads the REAL script and sources the region above the main loop, cut at the marker,
# so a function that changes here runs the changed bytes. Not a copy.
#
# Addresses in the fixtures are RFC 5737 TEST-NET (CLAUDE/ExampleValues.md); a real private
# address would be a public-repo leak and the secret scanner refuses it.
#
# Usage: test-ssh-suspend-guard.bash [--help]

set -uo pipefail

scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="$(dirname "${scriptDir}")"
GUARD="${repoRoot}/files/usr/local/bin/ssh-suspend-guard"

case "${1:-}" in
-h | --help)
    echo "Usage: test-ssh-suspend-guard.bash"
    echo "Drives ssh-suspend-guard's ssh_session_active() against captured ss output."
    exit 0
    ;;
"") ;;
*)
    echo "ERROR: unknown argument: ${1}" >&2
    exit 64
    ;;
esac

if [ ! -r "$GUARD" ]; then
    echo "ERROR: cannot read $GUARD" >&2
    exit 1
fi

# Cut at the marker, not a line number, so this does not rot as the script grows. A marker
# that matches zero times or more than once is a hard failure: sourcing the whole file would
# run the daemon loop, and guessing the cut point would source a different part of it.
MARKER='^# --- main loop'
matches="$(grep -c "$MARKER" "$GUARD")"
if [ "$matches" -ne 1 ]; then
    echo "ERROR: the main-loop marker matches $matches lines in $GUARD, expected exactly 1." >&2
    echo "  This test cuts the file there to source the functions without starting the" >&2
    echo "  daemon loop. Restore the marker, or update this test deliberately." >&2
    exit 1
fi
CUT_LINE="$(grep -n "$MARKER" "$GUARD" | cut -d: -f1)"

FUNCS="$(mktemp -t ssh-suspend-guard-funcs.XXXXXX.bash)"
cleanup() { rm -f "$FUNCS"; }
trap cleanup EXIT

awk -v n="$CUT_LINE" 'NR < n' "$GUARD" > "$FUNCS"

# Parse before sourcing. A cut that landed mid-function leaves an unbalanced brace, and the
# symptom would be a confusing syntax error rather than one named failure.
if ! bash -n "$FUNCS"; then
    echo "FAIL: the extracted region does not parse — the marker cut mid-construct." >&2
    exit 1
fi

# Sourcing brings the guard's own `set -euo pipefail` into this shell, and that is left
# alone rather than switched off: every assertion below calls ssh_session_active as an `if`
# CONDITION, and errexit does not fire on a command in a condition. A deliberately-false
# case is therefore an answer, not an abort.
# shellcheck source=/dev/null
source "$FUNCS"

FAILED=0
is() {
    local label="$1" got="$2" want="$3"
    if [ "$got" = "$want" ]; then
        printf '  ok    %-62s %s\n' "$label" "$got"
    else
        printf '  FAIL  %-62s got=%s want=%s\n' "$label" "$got" "$want"
        FAILED=$((FAILED + 1))
    fi
}

# active <text> — "yes"/"no", so a failure prints a word rather than an exit status.
active() {
    if ssh_session_active "$1"; then echo yes; else echo no; fi
}

echo "ssh-suspend-guard: session detection"

# The shape `ss -tnp state established` actually prints. Columns are
# Recv-Q Send-Q Local:Port Peer:Port Process, addresses numeric because of -n.
PORT22='0      0      192.0.2.5:22      192.0.2.9:51002   users:(("sshd-session",pid=2011,fd=5))'
PORT2222='0      0      192.0.2.5:2222    192.0.2.9:51003   users:(("sshd-session",pid=2012,fd=5))'
OLD_SSHD='0      0      192.0.2.5:22      192.0.2.9:51004   users:(("sshd",pid=2013,fd=5))'
OUTBOUND='0      0      192.0.2.5:44321   192.0.2.9:22      users:(("ssh",pid=3001,fd=3))'
HTTPS='0      0      192.0.2.5:44322   192.0.2.9:443     users:(("curl",pid=3002,fd=3))'

is "a session on the standard port counts" "$(active "$PORT22")" "yes"

# THE REGRESSION. This is the case the old `sport = :22` filter could not see, and the
# reason the machine suspended mid-session on a hardened host.
is "a session on a NON-STANDARD port counts" "$(active "$PORT2222")" "yes"

# OpenSSH 9.8 split the per-connection child into sshd-session. Both spellings must count,
# or the guard silently stops working on an OpenSSH upgrade.
is "modern sshd-session counts" "$(active "$PORT22")" "yes"
is "older bare sshd counts" "$(active "$OLD_SSHD")" "yes"

is "an OUTBOUND ssh client does not count" "$(active "$OUTBOUND")" "no"
is "unrelated traffic does not count" "$(active "$HTTPS")" "no"
is "no connections at all" "$(active "")" "no"

# ss prints a header line when there is nothing else; it must not read as a session.
is "header only, no rows" "$(active 'Recv-Q Send-Q Local Address:Port Peer Address:Port Process')" "no"

# Mixed tables: the sshd row must be found among others, and absent when it is not there.
is "sshd among other connections" "$(active "$HTTPS
$PORT2222")" "yes"
is "several non-sshd connections" "$(active "$HTTPS
$OUTBOUND")" "no"

# A process whose name merely contains "ssh" must not match; the pattern anchors on the
# opening quote of the process name for exactly this reason.
is "a process named ssh-agent does not count" \
    "$(active '0 0 192.0.2.5:44323 192.0.2.9:99 users:(("ssh-agent",pid=3003,fd=3))')" "no"

echo
if [ "$FAILED" -ne 0 ]; then
    echo "=============================================================="
    echo "VERDICT: FAIL — $FAILED assertion(s)"
    echo "=============================================================="
    exit 1
fi
echo "=============================================================="
echo "VERDICT: PASS"
echo "=============================================================="
