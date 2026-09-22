#!/usr/bin/env bash
# Unit-test the login snippet (files/home/bashrc-includes/host-health-report.bash.j2),
# deployed on both profiles since Plan 00136.
#
# WHY THIS EXISTS. bash reads ~/.bashrc for a NON-interactive shell when sshd is the one
# that started it, which is why a `.bashrc` that prints anything breaks scp, sftp and
# rsync-over-ssh with "protocol error" — the transfer parses stdout. This snippet is the
# first thing this repo puts in ~/.bashrc-includes that prints, so the interactive guard
# is the load-bearing line, and a guard nothing exercises is a guard that has never been
# shown to work.
#
# The message itself is not re-tested here — tests/helpers/host_health/test_login_message.py
# owns that. What is tested is everything the snippet does AROUND the call: when it speaks,
# what it leaves behind in the caller's shell, and what it does when the checkout it was
# templated against has gone.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEMPLATE="$REPO_ROOT/files/home/bashrc-includes/host-health-report.bash.j2"

if [ ! -f "$TEMPLATE" ]; then
    echo "FAIL: the login snippet template is not at $TEMPLATE" >&2
    exit 1
fi

if ! command -v python3 >/dev/null; then
    echo "FAIL: python3 not found — this gate cannot report a pass without it." >&2
    exit 1
fi

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got" >&2
    fi
}

# render <root_dir> <destination> — the template as Ansible would leave it on a host.
# `ansible_managed | comment` becomes a comment block; every other placeholder is
# root_dir. Python does the substitution because this file must not contain a literal
# Jinja placeholder anywhere a naive replace could hit it twice.
render() {
    local root="$1" dest="$2"
    python3 - "$TEMPLATE" "$root" "$dest" <<'PYEOF'
import sys

template, root, dest = sys.argv[1:4]
with open(template, encoding="utf-8") as handle:
    text = handle.read()
text = text.replace("{{ ansible_managed | comment }}", "# Ansible managed")
text = text.replace("{{ root_dir }}", root)
with open(dest, "w", encoding="utf-8") as handle:
    handle.write(text)
PYEOF
}

RENDERED="$WORK_DIR/snippet.bash"
render "$REPO_ROOT" "$RENDERED"

# ── the template renders completely ──────────────────────────────────────────────────
# A placeholder this test does not substitute would reach a host as literal `{{ ... }}`
# text inside a sourced file. Asserting the rendered form is placeholder-free means a
# newly introduced variable fails here rather than at every login on the server.
if grep -q '{{' "$RENDERED"; then
    check "the rendered snippet has no unsubstituted placeholder" "clean" "$(grep -c '{{' "$RENDERED") left"
else
    check "the rendered snippet has no unsubstituted placeholder" "clean" "clean"
fi

# ── the fixtures ─────────────────────────────────────────────────────────────────────
# A state home per case, because the snippet resolves the document through XDG exactly
# as it does on a host — passing --state-dir here would test an invocation no login uses.
FINDING_TEXT="the widget frobnicator is not built for the running kernel"

make_state() {
    local name="$1" kind="$2" finding="${3:-$FINDING_TEXT}"
    local home="$WORK_DIR/$name"
    mkdir -p "$home/fedora-desktop"
    if [ "$kind" = "absent" ]; then
        printf '%s' "$home"
        return 0
    fi
    # Built through the PRODUCER — `status_document.build` and `write_atomic` — rather
    # than hand-written JSON, so the fixture tracks the schema instead of restating it. A
    # hand-built document keeps parsing long after the producer has moved on, and the
    # suite would then be testing a shape nothing writes.
    #
    # The kernel comes from `probe.running_kernel()`, not a placeholder. `login_message`
    # reports a document collected under a different kernel as not-checked, so an
    # invented kernel makes every case here speak and the silent cases stop testing what
    # they were written for. That is not hypothetical: it happened, and took three
    # assertions with it.
    PYTHONPATH="$REPO_ROOT" python3 - "$home" "$kind" "$finding" <<'PYEOF'
import sys

from helpers.host_health import probe, probe_results, status_document
from helpers.play_ledger import repo

state_dir, kind, finding = sys.argv[1:4]
findings = [probe_results.broken(finding)] if kind == "findings" else []
status_document.write_atomic(
    status_document.path(f"{state_dir}/fedora-desktop"),
    status_document.build(
        sections={"health": findings},
        kernel=probe.running_kernel(),
        at=repo.utc_now(),
    ),
)
PYEOF
    printf '%s' "$home"
}

STATE_FINDINGS="$(make_state findings findings)"
STATE_CLEAN="$(make_state clean ok)"
STATE_ABSENT="$(make_state absent absent)"

# source_snippet <mode> <state-home> <snippet> — the snippet's stdout, which is the only
# stream that matters: scp and sftp multiplex their payload over stdout, and stderr is
# already the diagnostics channel.
#
# `bash -i` reading its commands from a pipe is what makes `$-` carry `i`; there is no
# way to set that flag from inside a script. `--norc --noprofile` keeps the harness's own
# dotfiles out of the captured output.
source_snippet() {
    local mode="$1" state_home="$2" snippet="$3"
    if [ "$mode" = interactive ]; then
        printf 'source %q\n' "$snippet" \
            | XDG_STATE_HOME="$state_home" bash --norc --noprofile -i 2>/dev/null
        return 0
    fi
    XDG_STATE_HOME="$state_home" bash --norc --noprofile -c "source $snippet" 2>/dev/null
    return 0
}

# ── the guard that keeps file transfers working ──────────────────────────────────────
# THE case this snippet's shape exists for. A findings document is used deliberately:
# with a clean one the snippet would be silent for the wrong reason and the assertion
# would pass with the guard deleted.
check "a non-interactive shell gets nothing on stdout, even with findings" \
    "" "$(source_snippet batch "$STATE_FINDINGS" "$RENDERED")"

# ── it does report, when somebody is there to read it ────────────────────────────────
interactive_findings="$(source_snippet interactive "$STATE_FINDINGS" "$RENDERED")"
case "$interactive_findings" in
    *"$FINDING_TEXT"*) got_finding=yes ;;
    *) got_finding="no: [$interactive_findings]" ;;
esac
check "an interactive shell is shown the finding" "yes" "$got_finding"

# ── once a day in full, then one line (Plan 00136) ───────────────────────────────────
# Every tmux pane is a new interactive shell. The second one today gets a reminder that
# names the on-demand command, not the findings again.
second_shell="$(source_snippet interactive "$STATE_FINDINGS" "$RENDERED")"
check "a second shell the same day gets exactly one line" "1" "$(printf '%s\n' "$second_shell" | grep -c .)"
case "$second_shell" in
    *"$FINDING_TEXT"*) got_reminder="the findings again" ;;
    *fedora-desktop-health*) got_reminder="the reminder" ;;
    *) got_reminder="neither: [$second_shell]" ;;
esac
check "the second shell's line names fedora-desktop-health" "the reminder" "$got_reminder"

# Changed findings are news, whatever the clock says: rewrite the document and the next
# shell is shown the new finding in full.
CHANGED_TEXT="the wifi firmware is missing for the running kernel"
make_state findings findings "$CHANGED_TEXT" >/dev/null
changed_shell="$(source_snippet interactive "$STATE_FINDINGS" "$RENDERED")"
case "$changed_shell" in
    *"$CHANGED_TEXT"*) got_changed=yes ;;
    *) got_changed="no: [$changed_shell]" ;;
esac
check "changed findings are shown in full again the same day" "yes" "$got_changed"

check "a clean host says nothing" "" "$(source_snippet interactive "$STATE_CLEAN" "$RENDERED")"

# Absence is not health. `status_document.read` turns a missing file into an
# `unavailable` document, and the snippet must pass that through rather than treat a
# host nothing has ever checked as a host with nothing wrong.
absent_out="$(source_snippet interactive "$STATE_ABSENT" "$RENDERED")"
if [ -n "$absent_out" ]; then
    check "a host with no status document is reported, not read as clean" "reported" "reported"
else
    check "a host with no status document is reported, not read as clean" "reported" "silent"
fi

# ── the checkout it was templated against has gone ───────────────────────────────────
# The snippet names an absolute path that Ansible wrote into it. Move the checkout and
# `python3 -m` prints a multi-line ImportError traceback at every single login. The
# snippet has to recognise that itself.
GONE="$WORK_DIR/snippet-no-checkout.bash"
render "$WORK_DIR/there-is-no-checkout-here" "$GONE"
gone_out="$(source_snippet interactive "$STATE_FINDINGS" "$GONE")"
check "a missing checkout prints no traceback on stdout" "" "$gone_out"

gone_err="$(printf 'source %q\n' "$GONE" \
    | XDG_STATE_HOME="$STATE_FINDINGS" bash --norc --noprofile -i 2>&1 >/dev/null)"
case "$gone_err" in
    *Traceback*) gone_said="a traceback" ;;
    *"there-is-no-checkout-here"*) gone_said="the missing path" ;;
    *) gone_said="nothing at all: [$gone_err]" ;;
esac
check "a missing checkout is named on stderr" "the missing path" "$gone_said"

# ── what it leaves behind in the caller's shell ──────────────────────────────────────
#
# The `$` is assembled rather than written literally so that shellcheck does not read
# these probe strings as expansions this script forgot to quote — they are the inner
# shell's expansions, and must survive to it unexpanded.
dollar='$'

# probe_after <state-home> <inner-script> — the value the inner script marks with
# `PROBE:`, and nothing else.
#
# The snippet shares this stdout, so comparing the WHOLE capture against the expected
# value only holds while the snippet happens to be silent. It stopped being silent the
# moment login_message learned to report a kernel mismatch, and three assertions failed
# reporting PYTHONPATH and exit status instead of the cause. The marker makes the probe's
# own answer addressable regardless of what the snippet says around it.
#
# A missing marker is its own answer, never an empty string: `""` is a legitimate value
# for some of these probes, so silently returning it would turn "the inner shell died" into
# whichever assertion happened to expect empty.
probe_after() {
    local state_home="$1" script="$2" out answer
    out="$(printf '%s\n' "$script" \
        | XDG_STATE_HOME="$state_home" bash --norc --noprofile -i 2>/dev/null)"
    if ! printf '%s\n' "$out" | grep -q '^PROBE:'; then
        printf 'NO-PROBE-LINE'
        return 0
    fi
    answer="$(printf '%s\n' "$out" | awk '/^PROBE:/ { sub(/^PROBE:/, ""); print }')"
    printf '%s' "$answer"
}

# A `cd` into the repo root would make the helpers package import and would also drop
# every SSH login into the checkout instead of the user's home. Probed with a FINDINGS
# document deliberately: the snippet is at its noisiest there, which is exactly when the
# whole-stdout comparison this replaced would have broken.
cwd_after="$(probe_after "$STATE_FINDINGS" \
    "$(printf "cd /tmp && source %q; printf 'PROBE:%%s\\\\n' \"${dollar}PWD\"" "$RENDERED")")"
check "sourcing does not move the caller's working directory" "/tmp" "$cwd_after"

# An exported PYTHONPATH follows every python3 the user runs for the rest of the
# session, so this repo's helpers would shadow same-named modules in their own projects.
pythonpath_after="$(probe_after "$STATE_FINDINGS" \
    "$(printf "source %q; printf 'PROBE:%%s\\\\n' \"${dollar}{PYTHONPATH-unset}\"" "$RENDERED")")"
check "sourcing leaves PYTHONPATH unset in the caller" "unset" "$pythonpath_after"

# The snippet is sourced from a loop in ~/.bashrc. A non-zero return is what a login
# shell running under `set -e` dies on, and `login_message.main` documents the same
# contract on the Python side for the same reason.
status_findings="$(probe_after "$STATE_FINDINGS" \
    "$(printf "source %q; printf 'PROBE:%%s\\\\n' \"${dollar}?\"" "$RENDERED")")"
check "sourcing returns 0 when there are findings to print" "0" "$status_findings"

status_gone="$(probe_after "$STATE_FINDINGS" \
    "$(printf "source %q; printf 'PROBE:%%s\\\\n' \"${dollar}?\"" "$GONE")")"
check "sourcing returns 0 when the checkout is missing" "0" "$status_gone"

# ── the interpreter is the system one ────────────────────────────────────────────────
# `python3` alone resolves through a pyenv shim on a host that has one, which is a
# different interpreter from the one play-host-health-login-report.yml installs
# python3-pyyaml into. The unit template already spells out /usr/bin/python3.
#
# The COMMAND line, not the file: a plain grep over the whole snippet is satisfied by the
# comment that explains the rule, so it passed with the invocation itself mutated to a
# bare `python3` — a check vouching for its own documentation. Found this by mutant.
invocation="$(awk '/helpers\.host_health\.login_message/ && $1 !~ /^#/' "$RENDERED")"
if [ "$(printf '%s\n' "$invocation" | grep -c .)" -ne 1 ]; then
    interpreter="no single invocation line found: [$invocation]"
else
    case "$invocation" in
        *"/usr/bin/python3 -P -m helpers.host_health.login_message"*) interpreter=explicit ;;
        *) interpreter="implicit: [$invocation]" ;;
    esac
fi
check "the snippet calls the system interpreter by path" "explicit" "$interpreter"

case "$invocation" in
    *" --once-a-day"*) once=yes ;;
    *) once="no: [$invocation]" ;;
esac
check "the snippet asks for the once-a-day mode" "yes" "$once"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
