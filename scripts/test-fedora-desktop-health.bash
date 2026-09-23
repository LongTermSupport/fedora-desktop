#!/usr/bin/env bash
# Unit-test the on-demand report command (files/home/.local/bin/fedora-desktop-health.j2).
#
# The command is what the login snippet's one-line reminder names and what the panel's
# "open the full report" row launches in a terminal (Plan 00136). Both of those hand a
# person to it and walk away, so the command has to answer on its own: a positive line on
# a clean host, the findings otherwise, a window that stays open when asked to, and no
# traceback when the checkout it was templated against has gone.
#
# The report's text is not re-tested here — tests/helpers/host_health/test_login_message.py
# owns that. What is tested is the executable around it: its options, its exit statuses,
# what `--hold` waits on, and the two streams.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEMPLATE="$REPO_ROOT/files/home/.local/bin/fedora-desktop-health.j2"

if [ ! -f "$TEMPLATE" ]; then
    echo "FAIL: the command template is not at $TEMPLATE" >&2
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
    chmod 0755 "$dest"
}

COMMAND="$WORK_DIR/fedora-desktop-health"
render "$REPO_ROOT" "$COMMAND"

if grep -q '{{' "$COMMAND"; then
    check "the rendered command has no unsubstituted placeholder" "clean" "$(grep -c '{{' "$COMMAND") left"
else
    check "the rendered command has no unsubstituted placeholder" "clean" "clean"
fi

# ── the fixtures: a state home per case, resolved through XDG as on a host ───────────
FINDING_TEXT="the widget frobnicator is not built for the running kernel"

make_state() {
    local name="$1" kind="$2"
    local home="$WORK_DIR/$name"
    mkdir -p "$home/fedora-desktop"
    if [ "$kind" = "absent" ]; then
        printf '%s' "$home"
        return 0
    fi
    PYTHONPATH="$REPO_ROOT" python3 - "$home" "$kind" "$FINDING_TEXT" <<'PYEOF'
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

# run <state-home> <stdout-file> <stderr-file> [args...] — the command's exit status.
run() {
    local state_home="$1" out="$2" err="$3"
    shift 3
    XDG_STATE_HOME="$state_home" "$COMMAND" "$@" >"$out" 2>"$err" </dev/null
}

# ── it answers ───────────────────────────────────────────────────────────────────────
run "$STATE_FINDINGS" "$WORK_DIR/f.out" "$WORK_DIR/f.err"
check "findings: exit 0" "0" "$?"
case "$(cat "$WORK_DIR/f.out")" in
    *"$FINDING_TEXT"*) got=yes ;;
    *) got="no: [$(cat "$WORK_DIR/f.out")]" ;;
esac
check "findings: the finding is on stdout" "yes" "$got"

run "$STATE_CLEAN" "$WORK_DIR/c.out" "$WORK_DIR/c.err"
check "clean: exit 0" "0" "$?"
case "$(cat "$WORK_DIR/c.out")" in
    *"nothing needs attention"*) got=yes ;;
    *) got="no: [$(cat "$WORK_DIR/c.out")]" ;;
esac
check "clean: a positive statement, not silence" "yes" "$got"

run "$STATE_ABSENT" "$WORK_DIR/a.out" "$WORK_DIR/a.err"
check "no document: exit 0" "0" "$?"
if [ -s "$WORK_DIR/a.out" ]; then
    check "no document: reported, not read as clean" "reported" "reported"
else
    check "no document: reported, not read as clean" "reported" "silent"
fi

# The report is the payload, so stderr stays empty on the ordinary paths.
check "findings: nothing on stderr" "" "$(cat "$WORK_DIR/f.err")"
check "clean: nothing on stderr" "" "$(cat "$WORK_DIR/c.err")"

# ── on demand never silences the login shell ─────────────────────────────────────────
# The snippet's once-a-day stamp must not be written by a direct question, or asking
# would make the next login shell go quiet about the same findings.
STAMP_NAME="$(PYTHONPATH="$REPO_ROOT" python3 -c 'from helpers.host_health import login_message; print(login_message.STAMP_FILE_NAME)')"
if [ -e "$STATE_FINDINGS/fedora-desktop/$STAMP_NAME" ]; then
    check "on demand writes no once-a-day stamp" "no stamp" "stamp written"
else
    check "on demand writes no once-a-day stamp" "no stamp" "no stamp"
fi

# ── --hold keeps the window open, and only until Enter ───────────────────────────────
# What the panel's terminal launch relies on: a terminal that exits with its command
# shows nothing. Enter on stdin releases it; the report is still on stdout.
printf '\n' | XDG_STATE_HOME="$STATE_FINDINGS" "$COMMAND" --hold >"$WORK_DIR/h.out" 2>"$WORK_DIR/h.err"
check "--hold: exit 0 once Enter arrives" "0" "$?"
case "$(cat "$WORK_DIR/h.out")" in
    *"$FINDING_TEXT"*) got=yes ;;
    *) got="no: [$(cat "$WORK_DIR/h.out")]" ;;
esac
check "--hold: the report is still on stdout" "yes" "$got"
case "$(cat "$WORK_DIR/h.err")" in
    *"Enter"*) got=yes ;;
    *) got="no: [$(cat "$WORK_DIR/h.err")]" ;;
esac
check "--hold: the prompt names Enter, on stderr" "yes" "$got"

# EOF (Ctrl-D, or a closed stdin) is the clean exit, not a hang and not an error.
XDG_STATE_HOME="$STATE_FINDINGS" timeout 10 "$COMMAND" --hold >"$WORK_DIR/e.out" 2>"$WORK_DIR/e.err" </dev/null
check "--hold: EOF on stdin releases it with exit 0" "0" "$?"

# ── options ──────────────────────────────────────────────────────────────────────────
run "$STATE_FINDINGS" "$WORK_DIR/help.out" "$WORK_DIR/help.err" --help
check "--help: exit 0" "0" "$?"
case "$(cat "$WORK_DIR/help.out")" in
    *"--hold"*) got=yes ;;
    *) got="no: [$(cat "$WORK_DIR/help.out")]" ;;
esac
check "--help: lists --hold" "yes" "$got"

run "$STATE_FINDINGS" "$WORK_DIR/u.out" "$WORK_DIR/u.err" --frobnicate
check "an unknown option fails fast with 64" "64" "$?"
case "$(cat "$WORK_DIR/u.err")" in
    *"--help"*) got=yes ;;
    *) got="no: [$(cat "$WORK_DIR/u.err")]" ;;
esac
check "an unknown option points at --help" "yes" "$got"

# ── the checkout it was templated against has gone ───────────────────────────────────
GONE="$WORK_DIR/fedora-desktop-health-no-checkout"
render "$WORK_DIR/there-is-no-checkout-here" "$GONE"
XDG_STATE_HOME="$STATE_FINDINGS" "$GONE" >"$WORK_DIR/g.out" 2>"$WORK_DIR/g.err" </dev/null
check "a missing checkout exits non-zero" "1" "$?"
check "a missing checkout prints nothing on stdout" "" "$(cat "$WORK_DIR/g.out")"
case "$(cat "$WORK_DIR/g.err")" in
    *Traceback*) got="a traceback" ;;
    *"there-is-no-checkout-here"*) got="the missing path" ;;
    *) got="nothing at all: [$(cat "$WORK_DIR/g.err")]" ;;
esac
check "a missing checkout is named on stderr, without a traceback" "the missing path" "$got"

# ── a report that fails is a command that fails ──────────────────────────────────────
# A root whose login_message module exists (so the readability check passes) but exits
# non-zero stands in for a half-updated checkout or a broken helper. The command's own
# status must be that failure, not 0: the first version of this file recorded `$?` inside
# an `if ! …; then` branch, where it is always 0, and every status case above still passed.
BROKEN_ROOT="$WORK_DIR/broken-checkout"
mkdir -p "$BROKEN_ROOT/helpers/host_health"
printf 'import sys\nprint("stub report", file=sys.stderr)\nraise SystemExit(7)\n' \
    >"$BROKEN_ROOT/helpers/host_health/login_message.py"
BROKEN="$WORK_DIR/fedora-desktop-health-broken-report"
render "$BROKEN_ROOT" "$BROKEN"
XDG_STATE_HOME="$STATE_FINDINGS" "$BROKEN" >"$WORK_DIR/b.out" 2>"$WORK_DIR/b.err" </dev/null
check "a report that exits 7 makes the command exit 7" "7" "$?"
printf '\n' | XDG_STATE_HOME="$STATE_FINDINGS" "$BROKEN" --hold >"$WORK_DIR/bh.out" 2>"$WORK_DIR/bh.err"
check "a failing report under --hold still exits 7 after Enter" "7" "$?"

# The command runs in whatever directory the terminal is in. This gate's own cwd is the
# repository, whose real `helpers/` package would shadow the broken root above if the
# interpreter put the cwd first on sys.path — which `python3 -m` does without `-P`. So
# the two cases above only prove the status fix if this one holds too: run from the
# repository root, the broken root must still be the one imported.
(cd "$REPO_ROOT" && XDG_STATE_HOME="$STATE_FINDINGS" "$BROKEN" >"$WORK_DIR/bc.out" 2>"$WORK_DIR/bc.err" </dev/null)
check "the checkout named in the command wins over the terminal's own directory" "7" "$?"

# With --hold the window must still stay open long enough to read the error.
printf '\n' | XDG_STATE_HOME="$STATE_FINDINGS" "$GONE" --hold >"$WORK_DIR/gh.out" 2>"$WORK_DIR/gh.err"
check "a missing checkout under --hold still exits non-zero" "1" "$?"
case "$(cat "$WORK_DIR/gh.err")" in
    *"Enter"*) got=yes ;;
    *) got="no: [$(cat "$WORK_DIR/gh.err")]" ;;
esac
check "a missing checkout under --hold still waits for Enter" "yes" "$got"

# ── --run-play: the panel's play runner (Plan 00109, Task 4.3) ───────────────────────
# A checkout of its own, so a real play is never run: `helpers` is the repository's (a
# symlink, so the gate under test is the shipped one), and the only play is a fixture
# that says it ran and exits 5. The ledger lists it, so only the gate stands between a
# requested name and that exec.
PLAY_CHECKOUT="$WORK_DIR/play-checkout"
FIXTURE_PLAY="playbooks/imports/play-fixture.yml"
mkdir -p "$PLAY_CHECKOUT/playbooks/imports"
ln -s "$REPO_ROOT/helpers" "$PLAY_CHECKOUT/helpers"
printf '#!/usr/bin/env bash\necho "fixture play ran with: $*"\nexit 5\n' \
    >"$PLAY_CHECKOUT/$FIXTURE_PLAY"
chmod 0755 "$PLAY_CHECKOUT/$FIXTURE_PLAY"
printf '#!/usr/bin/env bash\necho "unlisted play ran"\n' >"$PLAY_CHECKOUT/playbooks/imports/play-unlisted.yml"
chmod 0755 "$PLAY_CHECKOUT/playbooks/imports/play-unlisted.yml"
RUNNER="$WORK_DIR/fedora-desktop-health-runner"
render "$PLAY_CHECKOUT" "$RUNNER"

STATE_LEDGER="$WORK_DIR/ledger-state"
mkdir -p "$STATE_LEDGER"
PYTHONPATH="$REPO_ROOT" python3 - "$STATE_LEDGER" "$FIXTURE_PLAY" <<'PYEOF'
import sys

from helpers.play_ledger import ledger, store

state_home, play = sys.argv[1:3]
base = ledger.ledger_dir({"XDG_STATE_HOME": state_home}, "/nonexistent-home")
commit, stamp = "e" * 40, "2026-09-14T09:00:00Z"
store.ensure_ledger(base, commit=commit, at=stamp)
store.append_record(base, ledger.build_record(
    play=play, name=play, commit=commit, dirty=False, play_sha256="1" * 64,
    outcome="ok", changed=0, started=stamp, finished=stamp,
))
PYEOF

# run_play <stdout> <stderr> [args...] — the runner command's exit status.
run_play() {
    local out="$1" err="$2"
    shift 2
    XDG_STATE_HOME="$STATE_LEDGER" "$RUNNER" "$@" >"$out" 2>"$err" </dev/null
}

run_play "$WORK_DIR/rp.out" "$WORK_DIR/rp.err" --run-play "$FIXTURE_PLAY"
check "--run-play: the play's own exit status is the command's" "5" "$?"
case "$(cat "$WORK_DIR/rp.out")" in
    *"fixture play ran with: "*) got=yes ;;
    *) got="no: [$(cat "$WORK_DIR/rp.out")]" ;;
esac
check "--run-play: the named ledgered play ran" "yes" "$got"
case "$(cat "$WORK_DIR/rp.err")" in
    *"exited 5"*) got=yes ;;
    *) got="no: [$(cat "$WORK_DIR/rp.err")]" ;;
esac
check "--run-play: it says what the play exited with, on stderr" "yes" "$got"

run_play "$WORK_DIR/ru.out" "$WORK_DIR/ru.err" --run-play playbooks/imports/play-unlisted.yml
check "--run-play: a play the ledger does not list is refused with 64" "64" "$?"
check "--run-play: a refused play does not run" "" "$(cat "$WORK_DIR/ru.out")"
case "$(cat "$WORK_DIR/ru.err")" in
    *refused*ledger*) got=yes ;;
    *) got="no: [$(cat "$WORK_DIR/ru.err")]" ;;
esac
check "--run-play: the refusal says why" "yes" "$got"

run_play "$WORK_DIR/rt.out" "$WORK_DIR/rt.err" --run-play "playbooks/../playbooks/imports/play-fixture.yml"
check "--run-play: a non-canonical path is refused with 64" "64" "$?"
check "--run-play: a non-canonical path does not run" "" "$(cat "$WORK_DIR/rt.out")"

run_play "$WORK_DIR/rn.out" "$WORK_DIR/rn.err" --run-play
check "--run-play with no value fails fast with 64" "64" "$?"

printf '\n' | XDG_STATE_HOME="$STATE_LEDGER" "$RUNNER" --run-play "$FIXTURE_PLAY" --hold \
    >"$WORK_DIR/rh.out" 2>"$WORK_DIR/rh.err"
check "--run-play --hold: still the play's status after Enter" "5" "$?"
case "$(cat "$WORK_DIR/rh.err")" in
    *"Enter"*) got=yes ;;
    *) got="no: [$(cat "$WORK_DIR/rh.err")]" ;;
esac
check "--run-play --hold: waits for Enter" "yes" "$got"

case "$(cat "$WORK_DIR/help.out")" in
    *"--run-play"*) got=yes ;;
    *) got="no: [$(cat "$WORK_DIR/help.out")]" ;;
esac
check "--help: lists --run-play" "yes" "$got"

# ── the interpreter is the system one ────────────────────────────────────────────────
invocation="$(awk '/helpers\.host_health\.login_message/ && $1 !~ /^#/' "$COMMAND")"
if [ "$(printf '%s\n' "$invocation" | grep -c .)" -ne 1 ]; then
    interpreter="no single invocation line found: [$invocation]"
else
    case "$invocation" in
        *"/usr/bin/python3 -P -m helpers.host_health.login_message --on-demand"*) interpreter=explicit ;;
        *) interpreter="implicit: [$invocation]" ;;
    esac
fi
check "the command calls the system interpreter by path, on demand" "explicit" "$interpreter"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
