#!/usr/bin/env bash
# Plan 00111 — insulation-steps.bash
#
# The individual steps behind acceptance.bash, one per invocation, so that each can be a
# plan_deploy_leg COMMAND (a function passed by name would be unreachable to `shellcheck -x`,
# SC2317, and R11 forbids suppressing it). State between steps is a directory of small files
# under the acceptance run directory.
#
# What is under test is the DEPLOYED library at /var/local/claude-yolo/lib — the production
# code path — with a stand-in for the launcher (`sleep`) so no container, token or prompt is
# involved. The terminal emulator is stood in for by a throwaway pty from Python's pty
# module; SIGKILLing it hangs up the pty exactly as the 2026-09-13 Ptyxis death did.
#
# Usage: insulation-steps.bash <step> <state-dir>
#   preconditions       tmux, systemd-run, python3 present; deployed lib matches the repo copy
#   start               open the fake terminal and start the insulated stand-in in it
#   assert-created      the session exists, is attached, and its server is in a user scope
#   kill-terminal       SIGKILL the fake terminal (pty hang-up)
#   assert-survived     the session is still there, detached, with its process alive
#   reattach            a second fake terminal runs the same entry point, answers the offer
#                       with Enter: it must attach, not start anew
#   bounce              a raw tmux attach from a third terminal is thrown off by the server
#   offer-new           terminal 2 killed; a fourth answers 'n': a -2 session starts
#   real-help           the deployed ccy launcher, --help: prints, exits, no session
#   real-launch         the deployed ccy launcher, interactive: in tmux before any prompt
#   real-cc-launch      the deployed host cc wrapper: in tmux before its token chooser
#   not-applicable      inside tmux, or without a terminal, the entry point is a no-op
#   cleanup             kill whatever this run started; safe to call twice
set -euo pipefail

CCY_LIB_DEPLOYED="/var/local/claude-yolo/lib"
readonly CCY_LIB_DEPLOYED

step="${1:?usage: insulation-steps.bash <step> <state-dir>}"
state="${2:?usage: insulation-steps.bash <step> <state-dir>}"
repo_lib="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)/files/var/local/claude-yolo/lib"

mkdir -p "${state}"
# One project name per run, so a parallel or aborted run can never collide with this one.
if [[ ! -f "${state}/project" ]]; then
    printf 'acceptance-00111-%s\n' "$$" >"${state}/project"
fi
project="$(<"${state}/project")"
session="ccy-${project}"

# ── helpers ──────────────────────────────────────────────────────────────────────────────

# Sessions on CCY's server as "<name> <attached>"; empty when the server is not running,
# which is a normal state and not a failure.
list_sessions() {
    local listing
    if listing="$(tmux -L ccy list-sessions -F '#{session_name} #{session_attached}' 2>&1)"; then
        printf '%s\n' "${listing}"
        return 0
    fi
    if [[ "${listing}" == *"no server running"* ]] || [[ "${listing}" == *"No such file or directory"* ]]; then
        return 0
    fi
    printf '[ERROR] tmux list-sessions: %s\n' "${listing}" >&2
    return 1
}

# attached_count <session> — prints the attached-client count, or nothing if absent.
attached_count() {
    list_sessions | awk -v n="${1}" '$1 == n { print $2 }'
}

# wait_for <seconds> <description> <cmd...> — poll a command until it succeeds.
wait_for() {
    local deadline=$(( SECONDS + ${1} )) what="${2}"
    shift 2
    until "$@"; do
        if (( SECONDS >= deadline )); then
            printf '[FAIL] timed out waiting for: %s\n' "${what}" >&2
            return 1
        fi
        sleep 0.2
    done
}

session_attached_is() { [[ "$(attached_count "${session}")" == "${1}" ]]; }
named_session_attached_is() { [[ "$(attached_count "${1}")" == "${2}" ]]; }
# capture-pane wants a pane target: "=name:" is the exact session, its current window/pane.
pane_shows() { tmux -L ccy capture-pane -p -t "=${1}:" | grep -q "${2}"; }
session_absent() { [[ -z "$(attached_count "${session}")" ]]; }
# The probe's "No such process" is the expected answer, captured rather than printed.
pid_alive() { local probe; probe="$(kill -0 "${1}" 2>&1)" || { [[ -n "${probe}" ]] && return 1; }; }
pid_gone() { ! pid_alive "${1}"; }

# open_fake_terminal <log> <keystrokes> <bash -c script> [args...] — a pty-owning process
# that stands in for the emulator; <keystrokes> is typed into it, then stdin closes. Its pid
# is the thing to kill.
open_fake_terminal() {
    local log="${1}" keys="${2}"
    shift 2
    # The keystrokes are typed after a pause: fzf flushes pending input when it takes the
    # terminal into raw mode, so a key sent before the picker is up is discarded.
    (
        sleep 3
        printf '%s' "${keys}"
    ) | python3 -c 'import pty, sys; sys.exit(pty.spawn(sys.argv[1:]))' \
        bash -c "$@" >"${log}" 2>&1 &
    printf '%s\n' "$!"
}

# raw_attach_client <log> — a fake terminal running a bare tmux attach, bypassing the
# library entirely: this is what the server-side hook must bounce on its own.
raw_attach_client() {
    python3 -c 'import pty, sys; sys.exit(pty.spawn(sys.argv[1:]))' \
        tmux -L ccy attach-session -t "=${session}" </dev/null >"${1}" 2>&1 &
    printf '%s\n' "$!"
}

# The stand-in "launcher" invocation, sourced libs first. Shared by start and reattach so
# both go through the identical production entry point. The dollars are escaped because
# they are expanded inside the fake terminal's bash, not here. It runs from a scratch
# directory of its own: the offer matches parked sessions by directory, so running from
# the repo root would find the operator's own parked session and offer that instead.
stand_in_dir="${state}/stand-in"
mkdir -p "${stand_in_dir}"
entry_point="cd '${stand_in_dir}' && source \"\$1/common-pure.bash\" && source \"\$1/tmux-session.bash\" && ccy_tmux_insulate \"\$2\" \"\${@:3}\""

# ── steps ────────────────────────────────────────────────────────────────────────────────

case "${step}" in
preconditions)
    for tool in tmux systemd-run python3; do
        if [[ -z "$(command -v "${tool}")" ]]; then
            printf '[FAIL] %s not found\n' "${tool}" >&2
            exit 1
        fi
    done
    for lib in common-pure tmux-session; do
        if ! cmp "${repo_lib}/${lib}.bash" "${CCY_LIB_DEPLOYED}/${lib}.bash"; then
            printf '[FAIL] deployed %s.bash differs from the repo copy — run deploy.bash first\n' "${lib}" >&2
            exit 1
        fi
    done
    if ! session_absent; then
        printf '[FAIL] session %s already exists from an earlier run; run the cleanup step\n' "${session}" >&2
        exit 1
    fi
    # The deployed /etc/tmux.conf must put "tmux: <session>" in the window title — that is
    # the only visible sign of being inside a session once the status bar is off.
    titles="$(TERM=xterm-256color tmux -L config-check -f /etc/tmux.conf start-server \; show-option -gv set-titles-string \; kill-server)"
    if [[ "${titles}" != "tmux: #S"* ]]; then
        printf '[FAIL] /etc/tmux.conf does not set the window title to name the session (got: %s); run deploy.bash\n' "${titles}" >&2
        exit 1
    fi
    printf 'deployed lib matches repo; window title configured; no stale %s\n' "${session}"
    ;;

start)
    # The stand-in must run long enough to outlive every later step.
    open_fake_terminal "${state}/terminal-1.log" "" "${entry_point}" _ "${CCY_LIB_DEPLOYED}" "${project}" sleep 600 \
        >"${state}/terminal-1.pid"
    printf 'fake terminal pid %s\n' "$(<"${state}/terminal-1.pid")"
    ;;

assert-created)
    wait_for 15 "session ${session} attached" session_attached_is 1
    server_pid="$(tmux -L ccy display-message -p '#{pid}')"
    pane_pid="$(tmux -L ccy list-panes -t "=${session}" -F '#{pane_pid}')"
    printf '%s\n' "${pane_pid}" >"${state}/pane.pid"
    server_cgroup="$(<"/proc/${server_pid}/cgroup")"
    printf 'server pid %s cgroup %s\n' "${server_pid}" "${server_cgroup}"
    case "${server_cgroup}" in
    *"user@"*"/ccy-tmux-"*".scope")
        printf 'server is in its own user scope, not the terminal'"'"'s\n'
        ;;
    *)
        printf '[FAIL] tmux server cgroup is not a ccy-tmux-*.scope under systemd --user\n' >&2
        exit 1
        ;;
    esac
    ;;

kill-terminal)
    tpid="$(<"${state}/terminal-1.pid")"
    kill -KILL "${tpid}"
    wait_for 5 "fake terminal ${tpid} to die" pid_gone "${tpid}"
    printf 'fake terminal %s killed (pty hung up)\n' "${tpid}"
    ;;

assert-survived)
    wait_for 5 "session ${session} to show detached" session_attached_is 0
    pane_pid="$(<"${state}/pane.pid")"
    if ! kill -0 "${pane_pid}"; then
        printf '[FAIL] the insulated process %s died with its terminal\n' "${pane_pid}" >&2
        exit 1
    fi
    printf 'session %s survived detached; process %s alive: %s\n' \
        "${session}" "${pane_pid}" "$(ps -o cmd= -p "${pane_pid}")"
    ;;

reattach)
    # The offer is a picker; Enter on its first row attaches. The stand-in would exit 99 at
    # once if it ran; attaching means it must NOT run. fzf reads the tty in raw mode, so the
    # keystroke is a carriage return.
    open_fake_terminal "${state}/terminal-2.log" $'\r' "${entry_point}" _ "${CCY_LIB_DEPLOYED}" "${project}" bash -c 'exit 99' \
        >"${state}/terminal-2.pid"
    wait_for 15 "session ${session} re-attached" session_attached_is 1
    if [[ "$(tmux -L ccy list-panes -t "=${session}" -F '#{pane_pid}')" != "$(<"${state}/pane.pid")" ]]; then
        printf '[FAIL] a new session was started instead of re-attaching the detached one\n' >&2
        exit 1
    fi
    # fzf draws straight to the tty and little of it survives in the log; the offer's own
    # "Attaching to" line is the proof that the attach branch was chosen through it.
    if ! grep -q "Attaching to '${session}'" "${state}/terminal-2.log"; then
        printf '[FAIL] the offer did not take the attach branch:\n' >&2
        cat "${state}/terminal-2.log" >&2
        exit 1
    fi
    printf 're-attached to %s after the offer; same pane process\n' "${session}"
    ;;

bounce)
    # While terminal 2 holds the session, a raw tmux attach from a third terminal must be
    # thrown off by the server-side hook, leaving exactly one client.
    raw_attach_client "${state}/terminal-3.log" >"${state}/terminal-3.pid"
    tpid="$(<"${state}/terminal-3.pid")"
    wait_for 10 "raw second client ${tpid} to be bounced" pid_gone "${tpid}"
    if ! session_attached_is 1; then
        printf '[FAIL] session has %s clients after the bounce, expected 1\n' "$(attached_count "${session}")" >&2
        exit 1
    fi
    printf 'second client bounced by the server; %s still has one client\n' "${session}"
    ;;

offer-new)
    # Kill terminal 2 so the session is detached again, then answer the offer with Ctrl-N
    # (byte 0x0e): a second session for the same project must appear, and the first must be
    # untouched.
    tpid="$(<"${state}/terminal-2.pid")"
    kill -KILL "${tpid}"
    wait_for 5 "terminal 2 to die" pid_gone "${tpid}"
    wait_for 5 "session ${session} detached again" session_attached_is 0
    open_fake_terminal "${state}/terminal-4.log" $'\x0e' "${entry_point}" _ "${CCY_LIB_DEPLOYED}" "${project}" sleep 600 \
        >"${state}/terminal-4.pid"
    wait_for 15 "second session ${session}-2 attached" named_session_attached_is "${session}-2" 1
    if ! session_attached_is 0; then
        printf '[FAIL] answering n disturbed the original session\n' >&2
        exit 1
    fi
    printf "'n' started %s-2; %s left detached\n" "${session}" "${session}"
    ;;

real-help)
    # The DEPLOYED launcher itself, in a no-session mode: --help must print and exit without
    # a tmux session ever being created (its output would vanish with one).
    mkdir -p "${state}/ccyaccept/real"
    git -C "${state}/ccyaccept/real" init -q
    open_fake_terminal "${state}/real-help.log" "" "cd '${state}/ccyaccept/real' && /var/local/claude-yolo/claude-yolo --help" \
        >"${state}/real-help.pid"
    tpid="$(<"${state}/real-help.pid")"
    wait_for 30 "ccy --help to exit" pid_gone "${tpid}"
    if ! grep -q -- '--headless' "${state}/real-help.log"; then
        printf '[FAIL] ccy --help did not print its usage:\n' >&2
        cat "${state}/real-help.log" >&2
        exit 1
    fi
    if [[ -n "$(attached_count "ccy-ccyaccept-real")" ]]; then
        printf '[FAIL] ccy --help created a tmux session\n' >&2
        exit 1
    fi
    printf 'ccy --help printed and exited with no session\n'
    ;;

real-launch)
    # The DEPLOYED launcher, interactively, in a throwaway git repo whose project name is
    # predictable (parent "ccyaccept" is not a generic folder, so get_project_name yields
    # ccyaccept-real). The launcher must land in tmux BEFORE it prompts for anything: the
    # fake terminal's pre-tmux output must hold no prompt, and the pane must be running the
    # launcher. It is left waiting at its first prompt inside the session, then killed.
    open_fake_terminal "${state}/real-launch.log" "" "cd '${state}/ccyaccept/real' && /var/local/claude-yolo/claude-yolo" \
        >"${state}/real-launch.pid"
    wait_for 30 "session ccy-ccyaccept-real attached" named_session_attached_is "ccy-ccyaccept-real" 1
    pane_pid="$(tmux -L ccy list-panes -t "=ccy-ccyaccept-real" -F '#{pane_pid}')"
    if ! pgrep -f -P "${pane_pid}" 'claude-yolo' >"${state}/real-launch.launcher-pid"; then
        printf '[FAIL] the pane process %s is not running the launcher\n' "${pane_pid}" >&2
        exit 1
    fi
    # The fake terminal's log also holds everything tmux later drew on the same pty, so
    # only the bytes BEFORE the tmux client switched to the alternate screen are the
    # launcher's pre-tmux output. A prompt there is a prompt asked outside the session.
    python3 - "${state}/real-launch.log" <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
marker = b"\x1b[?1049h"
if marker not in data:
    sys.exit("[FAIL] tmux never took the screen (no alternate-screen switch in the log)")
before = data.split(marker, 1)[0].decode("utf-8", "replace")
for prompt in ("Use same configuration", "Select SSH key", "SSH key", "token"):
    if prompt.lower() in before.lower():
        sys.exit(f"[FAIL] the launcher prompted ({prompt!r}) BEFORE entering tmux:\n{before}")
print("no prompt before tmux took the screen")
PY
    # And inside the session the first prompt appears exactly once — no double asking.
    wait_for 60 "the launcher to reach its SSH key prompt inside the session" pane_shows "ccy-ccyaccept-real" 'Select SSH key'
    if [[ "$(tmux -L ccy capture-pane -p -t "=ccy-ccyaccept-real:" | grep -c 'Select SSH key')" -ne 1 ]]; then
        printf '[FAIL] expected exactly one SSH key prompt inside the session:\n' >&2
        tmux -L ccy capture-pane -p -t "=ccy-ccyaccept-real:" >&2
        exit 1
    fi
    tmux -L ccy kill-session -t "=ccy-ccyaccept-real"
    wait_for 5 "throwaway launcher session gone" named_session_attached_is "ccy-ccyaccept-real" ""
    printf 'deployed launcher entered tmux before its first prompt; launcher pid %s was in the pane\n' \
        "$(<"${state}/real-launch.launcher-pid")"
    ;;

real-cc-launch)
    # The DEPLOYED host cc wrapper, same throwaway repo: it must enter tmux before its
    # token chooser, as a cc-<project> session, and show that chooser exactly once.
    open_fake_terminal "${state}/real-cc.log" "" "cd '${state}/ccyaccept/real' && /var/local/claude-code/cc" \
        >"${state}/real-cc.pid"
    wait_for 30 "session cc-ccyaccept-real attached" named_session_attached_is "cc-ccyaccept-real" 1
    python3 - "${state}/real-cc.log" <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
marker = b"\x1b[?1049h"
if marker not in data:
    sys.exit("[FAIL] tmux never took the screen for cc (no alternate-screen switch in the log)")
before = data.split(marker, 1)[0].decode("utf-8", "replace")
if "select token" in before.lower():
    sys.exit(f"[FAIL] cc prompted for a token BEFORE entering tmux:\n{before}")
print("no prompt before tmux took the screen (cc)")
PY
    wait_for 60 "cc to reach its token chooser inside the session" pane_shows "cc-ccyaccept-real" 'Select token'
    if [[ "$(tmux -L ccy capture-pane -p -t "=cc-ccyaccept-real:" | grep -c 'Select token')" -ne 1 ]]; then
        printf '[FAIL] expected exactly one token prompt inside the cc session:\n' >&2
        tmux -L ccy capture-pane -p -t "=cc-ccyaccept-real:" >&2
        exit 1
    fi
    tmux -L ccy kill-session -t "=cc-ccyaccept-real"
    wait_for 5 "throwaway cc session gone" named_session_attached_is "cc-ccyaccept-real" ""
    printf 'deployed cc entered tmux before its token chooser, as cc-ccyaccept-real\n'
    ;;

not-applicable)
    # Inside tmux already: must return 0 without exec'ing, so `false` never runs and the
    # exit status is the function's own. Same entry point string as the real steps.
    TMUX="/nonexistent/socket,1,0" bash -c "${entry_point}" _ "${CCY_LIB_DEPLOYED}" "${project}-nested" false
    # No terminal on stdin: same.
    bash -c "${entry_point}" _ "${CCY_LIB_DEPLOYED}" "${project}-notty" false </dev/null
    for suffix in nested notty; do
        if [[ -n "$(attached_count "ccy-${project}-${suffix}")" ]]; then
            printf '[FAIL] a session ccy-%s-%s was created when insulation should not apply\n' "${project}" "${suffix}" >&2
            exit 1
        fi
    done
    printf 'no-op inside tmux and without a terminal\n'
    ;;

cleanup)
    for f in "${state}"/terminal-*.pid "${state}"/real-*.pid; do
        if [[ -f "${f}" ]] && pid_alive "$(<"${f}")"; then
            kill -KILL "$(<"${f}")"
        fi
    done
    for name in "${session}" "${session}-2" ccy-ccyaccept-real cc-ccyaccept-real; do
        if [[ -n "$(attached_count "${name}")" ]]; then
            tmux -L ccy kill-session -t "=${name}"
        fi
        wait_for 5 "session ${name} gone" named_session_attached_is "${name}" ""
    done
    printf 'cleaned up %s and %s-2\n' "${session}" "${session}"
    ;;

*)
    printf '[FATAL] unknown step: %s\n' "${step}" >&2
    exit 64
    ;;
esac
