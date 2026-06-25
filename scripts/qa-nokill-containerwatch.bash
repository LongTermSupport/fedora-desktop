#!/usr/bin/bash
# L0 no-kill safety gate for the container-watch watchdog (Plan 00055 Phase 5).
#
# The shipped watchdog is REPORTING-ONLY: it scans, attributes, and reports
# offending container processes, but it MUST NEVER terminate or throttle one.
# This gate FAILS the build if any *executable* process-termination call site is
# introduced into the watchdog code.
#
# Critically, it scopes to *call-site syntax* (the `(` / argv / shell-invocation
# form), so the word "kill" remains allowed inside guidance string literals such
# as an `exec_hint` value, a help string, or a comment. Those are advice a human
# may choose to run — the tool never executes them.
#
# Usage:
#   qa-nokill-containerwatch.bash              # gate the real watchdog code
#   qa-nokill-containerwatch.bash --self-test  # prove the gate detects a kill
#                                              # and ignores a guidance literal
#
# Exit codes: 0 = clean / self-test passed; 1 = forbidden call site found or
# self-test failed.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Forbidden EXECUTABLE termination call sites. Each entry is an extended-regex
# matched line-by-line with `grep -nE`. The patterns key on call-site syntax —
# an open paren, an argv token, or a shell-invocation form — never on the bare
# word "kill", so guidance string literals are not flagged.
#
# Covered:
#   Python: os.kill( , .send_signal( , signal.SIG* constants, a subprocess/Popen
#           argv whose first token is kill/pkill, Gio bindings' force_exit/
#           send_signal (also reachable from GJS).
#   JS/GJS: subproc.force_exit( , subproc.send_signal( , Gio.Subprocess argv
#           starting kill/pkill.
#   Shell : a bare `pkill ` or `kill -<signal>` invocation.
FORBIDDEN_PATTERNS=(
    # Python signal delivery to a pid
    '\bos\.kill[[:space:]]*\('
    '\bos\.killpg[[:space:]]*\('
    # signal.SIGKILL / signal.SIGTERM / signal.SIGSTOP … constants (only ever
    # referenced to deliver a signal)
    '\bsignal\.SIG[A-Z]'
    # object.send_signal( — Popen, Gio.Subprocess, asyncio transports
    '\.send_signal[[:space:]]*\('
    # Gio.Subprocess.force_exit( — GJS/Python hard-kill of a spawned child
    '\.force_exit[[:space:]]*\('
    # A spawned argv whose program is kill/pkill (quoted first token), e.g.
    #   ["kill", "-9", pid]  or  ('pkill', '-f', name)
    '\[[[:space:]]*["'"'"'](kill|pkill)["'"'"']'
    '\([[:space:]]*["'"'"'](kill|pkill)["'"'"']'
    # A bare shell invocation: `pkill ...` or `kill -SIG ...` / `kill -9 ...`.
    # `\b` word-boundary anchors the program name so substrings (e.g. "skill")
    # are not matched; the trailing form distinguishes an invocation from prose.
    '\bpkill[[:space:]]'
    '\bkill[[:space:]]+-'
)

# Collect target files. The JS extension dir is created by a sibling task and may
# not exist yet — glob it without failing (nullglob), and gate whatever exists.
collect_targets() {
    local root="$1"
    local -n _out="$2"
    _out=()
    shopt -s nullglob
    local f
    for f in "$root"/helpers/containerwatch/*.py \
             "$root"/extensions/container-watch@fedora-desktop/*.js; do
        _out+=("$f")
    done
    shopt -u nullglob
}

# Scan a set of files for forbidden call sites. Echoes each "file:line: text"
# offender to stdout and returns 1 if any were found, 0 if clean.
scan_targets() {
    local found=0
    local file pat hit
    for file in "$@"; do
        for pat in "${FORBIDDEN_PATTERNS[@]}"; do
            # grep -n exit status: 0 = match, 1 = no match, >=2 = real error.
            # Capture into a var so `set -e` does not abort on the no-match case;
            # a genuine grep error (rc>=2) is surfaced as a hard failure.
            local rc=0
            hit="$(grep -nE "$pat" "$file" 2>&1)" || rc=$?
            if [[ $rc -eq 0 ]]; then
                while IFS= read -r line; do
                    echo "${file}:${line}"
                    found=1
                done <<< "$hit"
            elif [[ $rc -ge 2 ]]; then
                echo "ERROR: grep failed scanning $file (rc=$rc): $hit" >&2
                exit 2
            fi
        done
    done
    return "$found"
}

run_self_test() {
    local tmp
    tmp="$(mktemp -d)"

    # Fixture (a): a real executable kill call site — MUST be detected.
    local kill_fixture="$tmp/kill_fixture.py"
    cat > "$kill_fixture" <<'PYEOF'
import os
def reap(pid):
    os.kill(pid, 9)
PYEOF

    # Fixture (b): the word "kill" only inside a guidance string literal — MUST
    # pass (the tool never executes it).
    local hint_fixture="$tmp/hint_fixture.py"
    cat > "$hint_fixture" <<'PYEOF'
def build_hint(pid):
    # Guidance only — the human may run this; the tool never executes it.
    exec_hint = "podman exec -it box ps -o pid,args  # in-container: kill <pid>"
    return exec_hint
PYEOF

    local self_test_ok=1

    # (a) The kill fixture must be DETECTED (scan_targets returns 1).
    local detect_rc=0
    scan_targets "$kill_fixture" > /dev/null || detect_rc=$?
    if [[ $detect_rc -eq 1 ]]; then
        echo "  self-test (a) PASS: os.kill( fixture detected"
    else
        echo "  self-test (a) FAIL: os.kill( fixture NOT detected (rc=$detect_rc)" >&2
        self_test_ok=0
    fi

    # (b) The guidance-literal fixture must PASS (scan_targets returns 0).
    local pass_rc=0
    scan_targets "$hint_fixture" > /dev/null || pass_rc=$?
    if [[ $pass_rc -eq 0 ]]; then
        echo "  self-test (b) PASS: exec_hint guidance literal not flagged"
    else
        echo "  self-test (b) FAIL: guidance literal wrongly flagged (rc=$pass_rc)" >&2
        self_test_ok=0
    fi

    rm -rf "$tmp"

    if [[ $self_test_ok -eq 1 ]]; then
        echo "✓ no-kill gate self-test passed"
        return 0
    fi
    echo "✗ no-kill gate self-test FAILED" >&2
    return 1
}

main() {
    if [[ "${1:-}" == "--self-test" ]]; then
        run_self_test
        return $?
    fi
    if [[ $# -gt 0 ]]; then
        echo "ERROR: unknown argument '$1' (only --self-test is accepted)" >&2
        exit 2
    fi

    local targets
    collect_targets "$REPO_ROOT" targets
    if [[ ${#targets[@]} -eq 0 ]]; then
        echo "ERROR: no container-watch targets found under $REPO_ROOT — expected" >&2
        echo "       helpers/containerwatch/*.py (the watchdog must exist)." >&2
        exit 2
    fi

    local offenders rc=0
    offenders="$(scan_targets "${targets[@]}")" || rc=$?
    if [[ $rc -eq 0 ]]; then
        echo "✓ no-kill gate: ${#targets[@]} container-watch file(s) clean — reporting-only confirmed"
        return 0
    fi

    echo "✗ no-kill gate: executable process-termination call site(s) found in the watchdog:" >&2
    echo "$offenders" >&2
    echo >&2
    echo "The container-watch watchdog is REPORTING-ONLY. Remove the termination call." >&2
    echo "The word 'kill' is allowed ONLY inside guidance string literals (exec_hint)," >&2
    echo "not as an executable call site." >&2
    return 1
}

main "$@"
