#!/bin/bash
#
# DAEMON-OWNED FILE - do not edit. Deployed into your project by the
# claude-code-hooks-daemon installer and refreshed on every upgrade, so local
# changes are discarded. See CLAUDE/LLM-INSTALL.md, "Which Files Under
# .claude/ Are Yours?", for the full list and the linter exclusions.
#
# Claude Code Hooks Daemon - Init Script
#
# Provides shell functions for daemon lifecycle management:
# - is_daemon_running() - Check if daemon is running
# - start_daemon() - Start daemon in background
# - ensure_daemon() - Start daemon if not running (lazy startup)
# - send_request_stdin() - Send JSON from stdin to daemon via Unix socket
# - emit_hook_error() - Output valid hook error JSON to stdout (CRITICAL)
#
# This script is sourced by forwarder scripts (pre-tool-use, post-tool-use, etc.)
#
# CRITICAL: All errors MUST output valid JSON to stdout so Claude can see
# the error and take corrective action. Errors to stderr are invisible to the agent.
#

set -euo pipefail

# Flag set by ensure_daemon when ci_enabled: true and daemon can't start
_HOOKS_DAEMON_CI_ENFORCED=false

# Flag set by ensure_daemon when daemon directory/venv is absent (fresh clone)
_HOOKS_DAEMON_NOT_INSTALLED=false

# Set by ensure_daemon (Plan 00477) when NOT_INSTALLED holds in a checkout that
# carries the tracked assets a provision needs: a fresh clone of a client
# project. The remedy is `bash .claude/provision.sh`, not the install skill.
# The mode is daemon.unprovisioned_mode (warn|block), read in bash because no
# daemon exists to read it; the note is non-empty when the configured value was
# not one of the two. STATUS_DOWN_TEXT is what the status-line forwarder prints
# in place of its generic marker.
_HOOKS_DAEMON_NEEDS_PROVISION=false
_HOOKS_DAEMON_UNPROVISIONED_MODE="warn"
_HOOKS_DAEMON_UNPROVISIONED_MODE_NOTE=""
_HOOKS_DAEMON_STATUS_DOWN_TEXT=""

# Set by ensure_daemon when the installed clone and the project's TRACKED
# deployed assets name different daemon versions (Plan 00386, GitHub issue #38).
# The two version globals are only meaningful while the flag is true.
_HOOKS_DAEMON_VERSION_MISMATCH=false
_HOOKS_DAEMON_CLONE_VERSION=""
_HOOKS_DAEMON_TRACKED_VERSION=""

# Set by the repo-detection guard below: this checkout IS the hooks-daemon
# repository and self-install has not been set up in it. Distinct from
# NOT_INSTALLED because the remedy is the opposite of the standard one — a
# restart cannot help, and the reader must run the installer with a flag the
# standard message never mentions. A fresh clone of this repository is the one
# environment the project cannot dogfood, since every maintainer checkout has
# already been installed into, so this branch is written for a reader who has
# no context at all.
_HOOKS_DAEMON_REPO_UNCONFIGURED=false

# Set by ensure_daemon when either a REAL daemon clone is present (see
# _daemon_clone_present — bare directory presence does not qualify) or a
# leftover venv is (see _daemon_orphan_venv_present), but no venv interpreter
# resolves for THIS project path (GitHub issue #53). Distinct from
# NOT_INSTALLED, whose remedy — the skill's install path — runs `rm -rf` on
# the whole daemon directory when its health probe cannot pass, which is
# exactly what happens here: the clone exists but the wrong venv is inside it
# (or none at all), most commonly the second of two bind-mounted views (host
# vs container) sharing one clone but not its per-path venv (Plan 00099). The
# safe remedy is a same-version upgrade instead — see the message below.
# _HOOKS_DAEMON_VENV_MISSING_VERSION holds the clone's own version when a
# real clone was confirmed present AND it is readable; empty otherwise (a
# leftover venv with no confirmed clone, or a confirmed clone whose version
# is unreadable), which the message must also account for.
_HOOKS_DAEMON_VENV_MISSING=false
_HOOKS_DAEMON_VENV_MISSING_VERSION=""

# Set by start_daemon when this hook's start deadline came while the daemon
# it launched was still starting (Plan 00466 review 8, R8-1): nothing is
# wrong, and the call is denied with a "retry" rather than a diagnosis.
_HOOKS_DAEMON_STARTING=false

# Set by _venv_self_heal (Plan 00456) when VENV_MISSING found a real clone
# with a readable version: what the clone's scripts/venv_bootstrap.sh did
# about it. STATE is one of started|running|failed|refused|disabled|error, or
# empty when no attempt was made (a damaged clone, or a clone too old to ship
# the driver), in which case the 00454 message stands unchanged.
_HOOKS_DAEMON_BOOTSTRAP_STATE=""
_HOOKS_DAEMON_BOOTSTRAP_LOG=""
_HOOKS_DAEMON_BOOTSTRAP_MISSING=""
_HOOKS_DAEMON_BOOTSTRAP_FIXES=""
_HOOKS_DAEMON_BOOTSTRAP_DETAIL=""
_HOOKS_DAEMON_BOOTSTRAP_PID=""
_HOOKS_DAEMON_BOOTSTRAP_ELAPSED=""

# Set by hooks-relay (relay/hooks_relay.rs, judge_via_fallback; Plan 00466
# N126) when it hands this forwarder a PreToolUse call whose exchange with
# the daemon failed: send_request_stdin then denies through the recovery
# carve-out without asking the daemon again. Captured and unset at once, so
# a daemon this hook starts never inherits it. Setting it only ever denies.
_HOOKS_DAEMON_RELAY_FAILED="${HOOKS_DAEMON_RELAY_FAILED:-}"
unset HOOKS_DAEMON_RELAY_FAILED

#
# The daemon-recovery carve-out (Plan 00466 N24 review 3 MA4, N67), shared
# verbatim by both python3 checks that apply it -- emit_hook_error's, via
# _hooks_daemon_stdin_is_recovery_command below, and send_request_stdin's --
# so the two can never disagree. Defines
# _is_daemon_recovery_command(hook_input, project_path).
#
# A call is exempt only when it is a Bash call whose WHOLE command is exactly
# one launcher spelling plus one recovery subcommand, and the file that
# spelling runs is THIS daemon install's launcher, <daemon root>/bin/
# hooks-daemon (Plan 00466 round 3, m-A): a client project may have a
# bin/hooks-daemon of its own that has nothing to do with the daemon. A
# relative spelling is resolved against the Bash tool's working directory
# (the hook input's cwd): the text alone is not enough, because a relative
# launcher runs whatever the cwd holds, so a planted bin/hooks-daemon would
# otherwise run while every guard is down. An absolute spelling, shell-quoted
# exactly as _recovery_command prints it, names the file itself and so works
# from any cwd. Anything that cannot be resolved is not exempt. The launcher
# follows its own symlinks to anchor itself, so a link to it is it.
#
# The spellings are tried in cli.py's order for "the project's launcher":
# the daemon clone's first, then the project root's.
#
# Only spaces and tabs may pad the command. str.strip() would also drop
# vertical tab, form feed, the separators and the Unicode line breaks, which
# bash passes to the launcher as part of its argument.
#
# A function, not a variable, so the exported functions that run it still
# have it in a child shell that never sourced this file (Plan 00466 round 2,
# m3): it is exported with them, and start_daemon keeps it out of the
# daemon's environment. It assigns the source to the variable its caller
# names, which costs no fork on the hot path. No single quotes in this
# block: it is a single-quoted shell string.
_hooks_daemon_recovery_py() {
    printf -v "$1" '%s' '
import os
import shlex

_INSTALL_LAUNCHER = "bin/hooks-daemon"
_RECOVERY_BINARIES = (".claude/hooks-daemon/bin/hooks-daemon", _INSTALL_LAUNCHER)
_RECOVERY_SUBCOMMANDS = ("restart", "status", "logs", "stop", "start", "repair")
_RECOVERY_PADDING = " \t"


def _is_absolute(path):
    return isinstance(path, str) and os.path.isabs(path)


def _resolve(path):
    """The real path of the longest part of path that exists, with the rest
    appended as written. hooks-relay and cli_command.py resolve the same way,
    so all three name the same launcher."""
    head, tail = path, []
    while not os.path.exists(head):
        parent, name = os.path.split(head)
        if parent == head:
            return path
        tail.append(name)
        head = parent
    return os.path.join(os.path.realpath(head), *reversed(tail))


def _project_it_manages(launcher):
    """The project a launcher at this resolved path manages, by the rule the
    launcher applies to itself (bin/hooks-daemon): the parent of its bin/ is
    the daemon root, <project>/.claude/hooks-daemon for a client install and
    the project itself for a self-install. None when the path is not a
    bin/hooks-daemon at all (round 5, R4-2): any other file two levels below
    a project manages nothing."""
    bin_dir, name = os.path.split(launcher)
    if name != "hooks-daemon" or os.path.basename(bin_dir) != "bin":
        return None
    daemon_dir = os.path.dirname(bin_dir)
    parent = os.path.dirname(daemon_dir)
    if os.path.basename(daemon_dir) == "hooks-daemon" and os.path.basename(parent) == ".claude":
        return os.path.dirname(parent)
    return daemon_dir


def _installs_launcher(project_path, daemon_root):
    """This install s launcher, resolved, or None when it is unknown.

    HOOKS_DAEMON_ROOT_DIR can be inherited from whatever started Claude Code,
    so it is trusted only when the launcher under it manages this project
    (Plan 00466 round 4, P3-1). Otherwise it names another project s install,
    or none, and no command is known to recover this one."""
    if not (_is_absolute(project_path) and _is_absolute(daemon_root)):
        return None
    try:
        launcher = _resolve(os.path.join(daemon_root, _INSTALL_LAUNCHER))
        if _project_it_manages(launcher) != _resolve(project_path):
            return None
    except (OSError, ValueError):
        # An unresolvable path (an embedded NUL, a loop) names no install.
        return None
    return launcher


def _runs_the_installs_launcher(path, project_path, daemon_root):
    """True when running path runs this install s launcher."""
    launcher = _installs_launcher(project_path, daemon_root)
    if launcher is None or not os.path.isfile(launcher):
        return False
    try:
        return _resolve(path) == launcher
    except (OSError, ValueError):
        return False


def _absolute_launchers(project_path, daemon_root):
    """Every absolute spelling of the launcher, in the order a deny names them."""
    spellings = []
    if _is_absolute(project_path):
        spellings.extend(os.path.join(project_path, binary) for binary in _RECOVERY_BINARIES)
    if _is_absolute(daemon_root):
        spellings.append(os.path.join(daemon_root, _INSTALL_LAUNCHER))
    return spellings


def _is_daemon_recovery_command(hi, project_path, daemon_root):
    if not isinstance(hi, dict) or hi.get("tool_name") != "Bash":
        return False
    tool_input = hi.get("tool_input")
    if not isinstance(tool_input, dict):
        return False
    command = tool_input.get("command")
    if not isinstance(command, str):
        return False
    stripped = command.strip(_RECOVERY_PADDING)
    if not stripped.isprintable():
        return False
    cwd = hi.get("cwd")
    for sub in _RECOVERY_SUBCOMMANDS:
        for binary in _RECOVERY_BINARIES:
            if stripped == binary + " " + sub:
                return _is_absolute(cwd) and _runs_the_installs_launcher(
                    os.path.join(cwd, binary), project_path, daemon_root
                )
        for launcher in _absolute_launchers(project_path, daemon_root):
            if stripped == shlex.quote(launcher) + " " + sub:
                return _runs_the_installs_launcher(launcher, project_path, daemon_root)
    return False


def _recovery_command(project_path, daemon_root, sub):
    """The exempt command a deny names: the launcher by absolute path, so it
    works from any directory, or None when this install s launcher is
    unknown. The first spelling that runs it wins; with none, where it
    belongs, which is still never a file of the project s own."""
    if _installs_launcher(project_path, daemon_root) is None:
        return None
    for launcher in _absolute_launchers(project_path, daemon_root):
        if _runs_the_installs_launcher(launcher, project_path, daemon_root):
            return shlex.quote(launcher) + " " + sub
    return shlex.quote(os.path.join(daemon_root, _INSTALL_LAUNCHER)) + " " + sub


def _launcher_for_a_human(project_path):
    """The launcher a human is told to run when the install is unknown."""
    if not _is_absolute(project_path):
        return _RECOVERY_BINARIES[0]
    for binary in _RECOVERY_BINARIES:
        spelling = os.path.join(project_path, binary)
        if os.path.isfile(spelling):
            if _project_it_manages(_resolve(spelling)) == _resolve(project_path):
                return shlex.quote(spelling)
    return shlex.quote(os.path.join(project_path, _RECOVERY_BINARIES[0]))


def _recovery_advice(project_path, daemon_root):
    """The TO FIX lines every PreToolUse deny ends with."""
    restart = _recovery_command(project_path, daemon_root, "restart")
    repair = _recovery_command(project_path, daemon_root, "repair")
    if restart is None:
        named_root = daemon_root or "unset"
        return [
            "TO FIX: no daemon command is exempt from this deny. HOOKS_DAEMON_ROOT_DIR",
            f"({named_root}) is not a daemon install of this project",
            f"({project_path}), so this hook cannot tell which launcher recovers it.",
            "It may be inherited from the environment that started Claude Code, or",
            "set in .claude/hooks-daemon.env. A human must correct it, and can",
            "restart the daemon meanwhile with the ! prefix:",
            f"! {_launcher_for_a_human(project_path)} restart",
        ]
    return [
        f"TO FIX: run exactly {restart} (from any directory),",
        "which stays allowed even while other calls are denied this way. If a",
        "restart cannot start it (a broken venv), run exactly",
        f"{repair}. A human can also",
        f"run it directly (! {restart}), since Edit is denied here too.",
    ]
'
}

#
# _hooks_daemon_stdin_is_recovery_command() - True when stdin is an exempt
# daemon recovery command (see _hooks_daemon_recovery_py above).
#
# Reads stdin (a PreToolUse hook_input JSON document) to EOF. Any parse
# failure returns false (deny-by-default) -- this function decides whether a
# call gets a CARVE-OUT, never whether it gets blocked outright.
_hooks_daemon_stdin_is_recovery_command() {
    local recovery_py
    _hooks_daemon_recovery_py recovery_py
    python3 -c "$recovery_py"'
import json
import sys

try:
    hi = json.load(sys.stdin)
except Exception:
    sys.exit(1)
sys.exit(0 if _is_daemon_recovery_command(hi, sys.argv[1], sys.argv[2]) else 1)
' "${PROJECT_PATH:-}" "${HOOKS_DAEMON_ROOT_DIR:-}"
}

#
# _hooks_daemon_recovery_command() - The exempt command for recovery
# subcommand $1, by the launcher's absolute path, as every deny names it.
# Exits 3, printing nothing, when this install's launcher is unknown.
_hooks_daemon_recovery_command() {
    local recovery_py
    _hooks_daemon_recovery_py recovery_py
    python3 -c "$recovery_py"'
import sys

command = _recovery_command(sys.argv[1], sys.argv[2], sys.argv[3])
if command is None:
    sys.exit(3)
print(command)
' "${PROJECT_PATH:-}" "${HOOKS_DAEMON_ROOT_DIR:-}" "$1"
}

#
# _hooks_daemon_recovery_advice() - The TO FIX lines a PreToolUse deny ends
# with (see _recovery_advice above).
_hooks_daemon_recovery_advice() {
    local recovery_py
    _hooks_daemon_recovery_py recovery_py
    python3 -c "$recovery_py"'
import sys

print("\n".join(_recovery_advice(sys.argv[1], sys.argv[2])))
' "${PROJECT_PATH:-}" "${HOOKS_DAEMON_ROOT_DIR:-}"
}

#
# _hooks_daemon_static_deny() - A PreToolUse deny that needs neither python3
# nor jq (Plan 00466 round 3, R2-1): the answer when the encoder that would
# carry the real reason could not run. Constant text, so nothing needs
# escaping. python3 also recognises the recovery commands, so with it gone
# none is exempt, and the text says a human must act.
_hooks_daemon_static_deny() {
    printf '%s\n' '{"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": "HOOKS DAEMON: denied for safety - this hook could not build its answer because python3 is missing or failing, so no guard judged this call. python3 is also what recognises the daemon recovery commands, so none is allowed either. A human must fix python3, or run the daemon launcher directly with the ! prefix: ! .claude/hooks-daemon/bin/hooks-daemon restart (or ! bin/hooks-daemon restart in the daemon repository itself)."}}'
}

#
# _hooks_daemon_stdin_is_provision_command() - True when stdin is a PreToolUse
# call that runs provision itself (Plan 00477 Task 3.3).
#
# The one thing a block-mode unprovisioned checkout still allows, so it is kept
# as narrow as the daemon recovery carve-out: a Skill call for hooks-daemon with
# exactly the argument `provision`, or a Bash call whose WHOLE command is
# `bash .claude/provision.sh` (or that script by absolute path, shell-quoted),
# padded only by spaces and tabs. Anything that merely mentions it, chains
# after it, or passes it a flag is not exempt. A relative spelling runs
# whatever the Bash tool's working directory holds, so it counts only when it
# resolves to THIS project's script. Any parse failure is "not exempt".
_hooks_daemon_stdin_is_provision_command() {
    python3 -c '
import json
import os
import shlex
import sys

PADDING = " \t"


def is_provision_call(hook_input, project_path):
    if not isinstance(hook_input, dict):
        return False
    tool_input = hook_input.get("tool_input")
    if not isinstance(tool_input, dict):
        return False
    tool_name = hook_input.get("tool_name")
    if tool_name == "Skill":
        args = tool_input.get("args")
        return (
            tool_input.get("skill") == "hooks-daemon"
            and isinstance(args, str)
            and args.strip(PADDING) == "provision"
        )
    if tool_name != "Bash":
        return False
    command = tool_input.get("command")
    if not isinstance(command, str):
        return False
    stripped = command.strip(PADDING)
    if not stripped.isprintable():
        return False
    script = os.path.join(project_path, ".claude", "provision.sh")
    if not os.path.isfile(script):
        return False
    if stripped == "bash " + shlex.quote(script):
        return True
    if stripped != "bash .claude/provision.sh":
        return False
    cwd = hook_input.get("cwd")
    if not (isinstance(cwd, str) and os.path.isabs(cwd)):
        return False
    return os.path.realpath(os.path.join(cwd, ".claude", "provision.sh")) == os.path.realpath(script)


try:
    hook_input = json.load(sys.stdin)
except ValueError:
    sys.exit(1)
sys.exit(0 if is_provision_call(hook_input, sys.argv[1]) else 1)
' "${PROJECT_PATH:-}"
}

#
# _hooks_daemon_emit_needs_provision() - The answer to EVERY hook event in a
# checkout that needs provisioning (Plan 00477 Tasks 3.1-3.3).
#
# One message, for the human and the agent alike: what is wrong, which version
# is expected, the exact command, and what the project's mode does about tool
# calls. Each event's JSON is the shape Claude Code's output contract gives it:
#
#   SessionStart, UserPromptSubmit   systemMessage (shown to the human) AND
#                                    hookSpecificOutput.additionalContext (agent)
#   PreToolUse                       warn: additionalContext. block: a deny,
#                                    unless the call is provision itself
#   Stop, SubagentStop               warn: systemMessage. block: decision=block
#   every other event                additionalContext
#
# With neither jq nor python3 nothing can encode the message, so a constant
# answer is given: a deny (no command can be recognised as provision) for a
# blocking PreToolUse, a systemMessage otherwise.
#
# Args:
#   $1 - event name (non-empty)
_hooks_daemon_emit_needs_provision() {
    local event_name="$1"
    local mode="$_HOOKS_DAEMON_UNPROVISIONED_MODE"
    local checkout="${PROJECT_PATH:-unknown checkout}"

    local version_line
    case "${_HOOKS_DAEMON_EXPECTED_VERSION_SOURCE:-}" in
        config) version_line="$_HOOKS_DAEMON_EXPECTED_VERSION (daemon.expected_version in .claude/hooks-daemon.yaml)" ;;
        tracked-doc) version_line="$_HOOKS_DAEMON_EXPECTED_VERSION (the .claude/HOOKS-DAEMON.md header)" ;;
        config-invalid) version_line="unknown - daemon.expected_version in .claude/hooks-daemon.yaml is not X.Y.Z, so provision will stop and say so" ;;
        *) version_line="unknown - neither daemon.expected_version nor the .claude/HOOKS-DAEMON.md header names one, so provision will stop and say so" ;;
    esac

    local mode_lines
    if [[ "$mode" == "block" ]]; then
        mode_lines="This project BLOCKS tool calls until the checkout is provisioned (daemon.unprovisioned_mode: block). Only the provision command is allowed: run exactly bash .claude/provision.sh, because the skill's own steps are blocked too."
    else
        mode_lines="Tool calls are NOT blocked (daemon.unprovisioned_mode: warn), but nothing is being checked."
    fi
    if [[ -n "$_HOOKS_DAEMON_UNPROVISIONED_MODE_NOTE" ]]; then
        mode_lines="$mode_lines
$_HOOKS_DAEMON_UNPROVISIONED_MODE_NOTE"
    fi

    local message
    message="$(printf '%s\n' \
        "HOOKS DAEMON: NEEDS PROVISIONING - this checkout has no daemon" \
        "" \
        "This project uses the Claude Code Hooks Daemon, but the daemon lives in the" \
        "gitignored, per-checkout .claude/hooks-daemon/ and has not been built here." \
        "Checkout: $checkout" \
        "Expected daemon version: $version_line" \
        "" \
        "ALL safety handlers, code quality checks, and workflow enforcement are INACTIVE." \
        "" \
        "TO FIX - provision this checkout, from the project root:" \
        "  bash .claude/provision.sh" \
        "or use the hooks-daemon skill to provision (Skill tool: skill=hooks-daemon," \
        "args=provision). It builds exactly the expected version, changes no tracked" \
        "file, and needs no session restart." \
        "" \
        "$mode_lines")"

    local shape
    case "$event_name" in
        SessionStart | UserPromptSubmit) shape="inform" ;;
        PreToolUse)
            shape="context"
            if [[ "$mode" == "block" ]] && ! _hooks_daemon_stdin_is_provision_command; then
                shape="deny"
            fi
            ;;
        Stop | SubagentStop)
            shape="system"
            if [[ "$mode" == "block" ]]; then
                shape="block"
            fi
            ;;
        *) shape="context" ;;
    esac

    if command -v jq > /dev/null; then
        jq -n --arg shape "$shape" --arg event "$event_name" --arg msg "$message" '
            if $shape == "inform" then
                {"systemMessage": $msg,
                 "hookSpecificOutput": {"hookEventName": $event, "additionalContext": $msg}}
            elif $shape == "deny" then
                {"hookSpecificOutput": {"hookEventName": $event, "permissionDecision": "deny",
                                        "permissionDecisionReason": $msg}}
            elif $shape == "block" then {"decision": "block", "reason": $msg}
            elif $shape == "system" then {"systemMessage": $msg}
            else {"hookSpecificOutput": {"hookEventName": $event, "additionalContext": $msg}}
            end'
        return 0
    fi
    if python3 -c '
import json
import sys

shape, event, msg = sys.argv[1:4]
if shape == "inform":
    resp = {
        "systemMessage": msg,
        "hookSpecificOutput": {"hookEventName": event, "additionalContext": msg},
    }
elif shape == "deny":
    resp = {
        "hookSpecificOutput": {
            "hookEventName": event,
            "permissionDecision": "deny",
            "permissionDecisionReason": msg,
        }
    }
elif shape == "block":
    resp = {"decision": "block", "reason": msg}
elif shape == "system":
    resp = {"systemMessage": msg}
else:
    resp = {"hookSpecificOutput": {"hookEventName": event, "additionalContext": msg}}
print(json.dumps(resp))
' "$shape" "$event_name" "$message"; then
        return 0
    fi
    if [[ "$shape" == "deny" ]]; then
        printf '%s\n' '{"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": "HOOKS DAEMON: NEEDS PROVISIONING - this project blocks tool calls until the checkout is provisioned. Neither jq nor python3 is available, so no command can be recognised as the provision command. A human must run: ! bash .claude/provision.sh"}}'
    else
        printf '%s\n' '{"systemMessage": "HOOKS DAEMON: NEEDS PROVISIONING - run: bash .claude/provision.sh (neither jq nor python3 is available, so this message cannot be detailed)"}'
    fi
}

#
# emit_hook_error() - Output a valid hook error response to stdout
#
# CRITICAL: This ensures the agent sees errors and can take action.
# Outputs JSON in Claude Code's expected hook response format.
#
# DRY: Uses Python utility to generate error responses - single source of truth.
#
# Args:
#   $1 - Event name (e.g., "PreToolUse", "Stop"), or EMPTY when the caller
#        genuinely cannot know it. Claude Code validates `hookEventName`
#        against a closed enum, and a value outside it invalidates the WHOLE
#        document rather than just that field — so a placeholder word there
#        does not degrade the response, it destroys it. Pass empty instead and
#        the universal `systemMessage` field carries the text with no event
#        name at all.
#   $2 - Error type (e.g., "daemon_startup_failed")
#   $3 - Error details
#
# Output:
#   Valid JSON hook response to stdout
#   Also logs to stderr for debugging
#
emit_hook_error() {
    local event_name="${1:-}"
    local error_type="${2:-unknown_error}"
    local error_details="${3:-No details available}"

    # The checkout this answer is about. PROJECT_PATH is assigned at source
    # time, below this function's definition, so the `:-` default covers the
    # one caller that runs before it (the init_path_error branch) under set -u.
    local _hooks_daemon_checkout="${PROJECT_PATH:-unknown checkout}"

    # Log to stderr for debugging (agent won't see this)
    echo "HOOKS DAEMON ERROR [$error_type]: $error_details" >&2

    # Plan 00466 N126 round 2 (F1): a relay hand-off is a PreToolUse call the
    # relay has already failed, and the relay hands over only that socket's
    # calls, so an unnamed event (a source-time guard) is that call too. Its
    # only outcomes are the recovery carve-out's allow and a deny: every
    # state below that would answer otherwise is shadowed for this call, so
    # the standard branch judges it.
    if [[ -n "${_HOOKS_DAEMON_RELAY_FAILED:-}" && ( -z "$event_name" || "$event_name" == "PreToolUse" ) ]]; then
        event_name="PreToolUse"
        local _HOOKS_DAEMON_CI_ENFORCED=false _HOOKS_DAEMON_REPO_UNCONFIGURED=false \
            _HOOKS_DAEMON_VENV_MISSING=false _HOOKS_DAEMON_NOT_INSTALLED=false \
            _HOOKS_DAEMON_VERSION_MISMATCH=false
    fi

    # Plan 00477: a fresh clone that only needs provisioning gets its own
    # answer for every event. ensure_daemon sets the flag only after the CI,
    # starting and passthrough diagnoses, so those keep their own answers.
    if [[ "$_HOOKS_DAEMON_NEEDS_PROVISION" == "true" && -n "$event_name" ]]; then
        _hooks_daemon_emit_needs_provision "$event_name"
        return 0
    fi

    # Plan 00466 N24 review 3 MA4: for PreToolUse, the STANDARD branch below
    # (an installed project whose daemon could not be reached at all --
    # ensure_daemon itself failed) now denies rather than fails open. The one
    # carve-out is the exact command that would fix it, so it must be
    # checked before that decision is made. Every real call site with
    # event_name=PreToolUse reaches this function with stdin still fully
    # unconsumed and exits right afterwards, so reading it here is safe.
    #
    # Gated to ONLY the standard case (every named state below stays
    # unconditionally fail-open, untouched by MA4) -- every other branch
    # must never touch stdin at all: several of this function's OTHER
    # callers pass no stdin of their own, and an unguarded read here would
    # hang them waiting for an EOF that never comes.
    local _pretooluse_deny="false"
    if [[ "$event_name" == "PreToolUse" \
        && "$_HOOKS_DAEMON_CI_ENFORCED" != "true" \
        && "$_HOOKS_DAEMON_REPO_UNCONFIGURED" != "true" \
        && "$_HOOKS_DAEMON_VENV_MISSING" != "true" \
        && "$_HOOKS_DAEMON_NOT_INSTALLED" != "true" \
        && "$_HOOKS_DAEMON_VERSION_MISMATCH" != "true" ]]; then
        if _hooks_daemon_stdin_is_recovery_command; then
            _pretooluse_deny="false"
        else
            _pretooluse_deny="true"
        fi
    fi

    # Build error context message based on CI enforcement policy
    local context_msg
    # The Stop-family block reason for VENV_MISSING, shared by both encoders.
    local venv_block_reason=""

    if [[ "$_HOOKS_DAEMON_CI_ENFORCED" == "true" ]]; then
        # CI ENFORCED: Loud STOP message — project requires daemon via ci_enabled: true
        context_msg=$(printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s' \
            "STOP - DO NOT PROCEED" \
            "" \
            "The hooks daemon is REQUIRED for this project (ci_enabled: true in .claude/hooks-daemon.yaml) but is NOT installed or running in this environment." \
            "" \
            "ALL safety handlers, code quality checks, and workflow enforcement are INACTIVE." \
            "" \
            "ACTION REQUIRED:" \
            "1. STOP all work immediately - do not use any tools" \
            "2. Report to the user: 'Hooks daemon must be installed before running Claude Code agents in this environment'" \
            "3. Use the hooks-daemon skill to install (Skill tool: skill=hooks-daemon, args=install)" \
            "" \
            "DO NOT continue working without the daemon.")
    elif [[ "$_HOOKS_DAEMON_REPO_UNCONFIGURED" == "true" ]]; then
        # THE HOOKS-DAEMON REPO ITSELF, never installed into.
        #
        # Checked ahead of NOT_INSTALLED because both are true here and only
        # this one is actionable: the standard message says "restart", and a
        # restart cannot create a venv that was never built. Same reasoning as
        # the VERSION_MISMATCH branch below — when the usual advice cannot
        # succeed, saying so outright beats offering a better option, because
        # a reader who follows advice that cannot work concludes the
        # repository is broken rather than unconfigured.
        context_msg=$(printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s' \
            "HOOKS DAEMON: this checkout is the hooks-daemon repository, not yet set up" \
            "" \
            "You have cloned the daemon's own source. Its runtime pieces — the" \
            "virtualenv, the dependencies and .claude/hooks-daemon.env — are" \
            "gitignored per-checkout artefacts, so a clone never carries them." \
            "Checkout: $_hooks_daemon_checkout" \
            "" \
            "ALL safety handlers, code quality checks, and workflow enforcement are INACTIVE." \
            "" \
            "A RESTART CANNOT FIX THIS — there is nothing built to restart yet." \
            "" \
            "TO FIX — build this checkout's runtime, from the repository root:" \
            "  scripts/bootstrap-self-install.sh" \
            "" \
            "Do NOT run install.py --self-install here. install.py is the CLIENT" \
            "installer: it OVERWRITES this repository's own tracked" \
            ".claude/hooks-daemon.yaml and .claude/settings.json with default" \
            "templates (--force only decides whether a .bak is kept first).")
    elif [[ "$_HOOKS_DAEMON_VENV_MISSING" == "true" ]]; then
        # VENV MISSING FOR THIS PATH: a real clone, a leftover venv, or both
        # is present under $HOOKS_DAEMON_ROOT_DIR, but no venv interpreter
        # resolves here (GitHub issue #53) — see _HOOKS_DAEMON_VENV_MISSING's
        # declaration for which of the two ensure_daemon found.
        #
        # The standard NOT_INSTALLED message below is the wrong answer: its
        # remedy is the install skill, and when the health probe it runs
        # cannot pass — which it cannot, since the wrong venv (or none) is in
        # this directory — a skill older than Plan 00456 escalates to
        # `--force` on its own, whose `rm -rf` deletes $HOOKS_DAEMON_ROOT_DIR
        # outright. That destroys whatever venv IS in there, which most often
        # belongs to a second view of the same bind-mounted project (host vs
        # container) sharing this clone but not its per-path venv. So this
        # branch says outright not to install, same reasoning as
        # REPO_UNCONFIGURED and VERSION_MISMATCH above: when the usual advice
        # cannot succeed safely, say so rather than offering it anyway.
        #
        # Plan 00456: a real clone with a readable version now heals itself —
        # _venv_self_heal started (or reported) a background build, and what
        # it did replaces the remedy below. Every remedy it can print names
        # `repair` or waiting, never install or --force.
        #
        # The manual fix is a same-version upgrade: scripts/upgrade_version.sh's
        # idempotent path starts with ensure_venv (Plan 00099/00104) and
        # deletes nothing. Pinning it to the clone's OWN version (read by
        # _clone_version, which needs no working venv) keeps it on that path.
        local _hd_repair_cmd="$HOOKS_DAEMON_ROOT_DIR/bin/hooks-daemon repair"
        local _hd_venv_missing_remedy
        if [[ -n "$_HOOKS_DAEMON_VENV_MISSING_VERSION" ]]; then
            _hd_venv_missing_remedy="TO FIX — build the missing venv with a same-version upgrade:
  Use the hooks-daemon skill to upgrade (Skill tool: skill=hooks-daemon, args=upgrade $_HOOKS_DAEMON_VENV_MISSING_VERSION)"
        else
            _hd_venv_missing_remedy="The clone's own version could not be read with confidence — either
version.py is missing or unparseable, or scripts/lib/resolve_venv.sh itself
is gone, which means $HOOKS_DAEMON_ROOT_DIR is not trusted enough to name an
upgrade target from. This checkout's clone looks damaged or partial. Do not
run the install skill on it (see below). Ask a human to inspect
$HOOKS_DAEMON_ROOT_DIR directly, or remove it and reinstall only once you are
certain no other environment's venv lives there."
        fi

        local _hd_venv_state_note=""
        case "$_HOOKS_DAEMON_BOOTSTRAP_STATE" in
            started)
                _hd_venv_missing_remedy="A BUILD HAS STARTED — nothing to do but wait.
The missing venv is being built in the background for this project path only,
under the venv build lock. Other environments' venvs are not touched.
Build log: $_HOOKS_DAEMON_BOOTSTRAP_LOG
When it finishes, the next hook starts the daemon on its own."
                _hd_venv_state_note=" - a venv build is running in the background"
                ;;
            running)
                local _hd_build_who=""
                if [[ -n "$_HOOKS_DAEMON_BOOTSTRAP_PID" ]]; then
                    _hd_build_who="
Build process: pid $_HOOKS_DAEMON_BOOTSTRAP_PID, running for ${_HOOKS_DAEMON_BOOTSTRAP_ELAPSED:-?}s (in the environment that started it)."
                fi
                _hd_venv_missing_remedy="A venv build is ALREADY RUNNING under this clone — nothing to do but wait.
Build log: ${_HOOKS_DAEMON_BOOTSTRAP_LOG:-not recorded here (another process, such as an upgrade or a repair, holds the venv build lock)}${_hd_build_who}
When it finishes, the next hook starts the daemon, or starts this path's own
build if the one running belonged to another environment. A background build
is stopped and reported as failed if it outlives its bound."
                _hd_venv_state_note=" - a venv build is running in the background"
                ;;
            failed)
                _hd_venv_missing_remedy="THE LAST AUTOMATIC BUILD OF THIS VENV FAILED. Its log says why:
  $_HOOKS_DAEMON_BOOTSTRAP_LOG
Hooks do not retry it until pyproject.toml, uv.lock, the Python interpreter or
the uv binary changes. Once the cause is fixed, retry in the foreground (it
shows the output):
  $_hd_repair_cmd"
                _hd_venv_state_note=" - the automatic venv build failed"
                ;;
            refused)
                _hd_venv_missing_remedy="The venv was NOT built automatically, because these conditions for a safe
automatic build do not hold here (nothing was changed):
${_HOOKS_DAEMON_BOOTSTRAP_FIXES}Fix them and the next hook builds the venv on its own, or build it now with:
  $_hd_repair_cmd"
                _hd_venv_state_note=" - automatic venv build refused ($_HOOKS_DAEMON_BOOTSTRAP_MISSING)"
                ;;
            disabled)
                _hd_venv_missing_remedy="Automatic venv builds are switched off here (${_HOOKS_DAEMON_BOOTSTRAP_DETAIL:-HOOKS_DAEMON_SKIP_VENV_BOOTSTRAP=1}).
That setting stops hooks building a venv on their own; an explicit repair still
builds one. To build it now:
  $_hd_repair_cmd"
                _hd_venv_state_note=" - automatic venv builds switched off (${_HOOKS_DAEMON_BOOTSTRAP_DETAIL:-HOOKS_DAEMON_SKIP_VENV_BOOTSTRAP=1})"
                ;;
            error)
                _hd_venv_missing_remedy="The automatic build could not be evaluated: $_HOOKS_DAEMON_BOOTSTRAP_DETAIL
To build it now: $_hd_repair_cmd
$_hd_venv_missing_remedy"
                ;;
        esac
        venv_block_reason="Hooks daemon clone present but venv missing for this project path ($_hooks_daemon_checkout)${_hd_venv_state_note} - do not install/force, see additionalContext - protection not active"

        context_msg=$(printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s' \
            "HOOKS DAEMON: clone present, venv missing for this project path" \
            "" \
            "The daemon clone already exists under $HOOKS_DAEMON_ROOT_DIR, but no" \
            "venv interpreter resolves for this checkout. This is the normal state" \
            "the first time a bind-mounted project is opened from a second view" \
            "(host vs container) — the venv is keyed per project path (Plan 00099)," \
            "so the other view's venv correctly does not resolve here." \
            "Checkout: $_hooks_daemon_checkout" \
            "" \
            "ALL safety handlers, code quality checks, and workflow enforcement are INACTIVE." \
            "" \
            "Do NOT use install/force here — a forced reinstall deletes the ENTIRE daemon
directory ($HOOKS_DAEMON_ROOT_DIR) with rm -rf and re-clones it, which this
state does not need, and an install skill older than this clone deletes any
other environment's venv inside it too.

$_hd_venv_missing_remedy")
    elif [[ "$_HOOKS_DAEMON_NOT_INSTALLED" == "true" ]]; then
        # NOT INSTALLED: Guide to install guide — project was cloned but daemon never set up
        #
        # The checkout is named because this answer is most often seen in a
        # checkout the reader did not expect: a git worktree has no
        # (gitignored) .claude/hooks-daemon.env, so it never enters
        # self-install mode and EVERY wrapper in it lands here, while the main
        # checkout beside it is fully protected.
        context_msg=$(printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s' \
            "HOOKS DAEMON: Not installed" \
            "" \
            "This project uses the Claude Code Hooks Daemon for safety enforcement," \
            "but the daemon is not installed in this environment." \
            "Checkout: $_hooks_daemon_checkout" \
            "" \
            "ALL safety handlers, code quality checks, and workflow enforcement are INACTIVE." \
            "" \
            "TO INSTALL — use the hooks-daemon skill (do not improvise):" \
            "  Use the hooks-daemon skill to install (Skill tool: skill=hooks-daemon, args=install)" \
            "" \
            "After installing, restart your Claude session for hooks to activate.")
    elif [[ "$_HOOKS_DAEMON_VERSION_MISMATCH" == "true" ]]; then
        # VERSION MISMATCH: the clone and the project's TRACKED deployed assets
        # name different versions (Plan 00386, GitHub issue #38).
        #
        # The standard message below points at `restart`, and a restart changes
        # neither version — so a reader who follows it loops forever while every
        # safety handler stays inactive. This branch therefore says outright that
        # a restart cannot work, rather than merely offering a better option.
        #
        # The remedy differs by DIRECTION, so both are spelled out separately: a
        # clone behind the tracked assets is upgraded, while a clone AHEAD of
        # them means the tracked assets are the stale half and telling the reader
        # to upgrade would be advice that cannot succeed.
        #
        # Plan 00477 Task 5.1: when the EXPECTED version came from the
        # daemon.expected_version config key (a pull changed it), a clone ahead
        # of it is a DOWNGRADE the project asked for, not a stale header, and the
        # message says so; the regenerate remedy stays for the header source.
        local _hd_remedy_1 _hd_remedy_2 _hd_headline _hd_expected_from
        local _hd_upgrade_cmd="  Use the hooks-daemon skill to upgrade (Skill tool: skill=hooks-daemon, args=upgrade $_HOOKS_DAEMON_TRACKED_VERSION)"
        if [[ "${_HOOKS_DAEMON_EXPECTED_VERSION_SOURCE:-}" == "config" ]]; then
            _hd_headline="HOOKS DAEMON: version drift — installed clone v$_HOOKS_DAEMON_CLONE_VERSION, this project expects v$_HOOKS_DAEMON_TRACKED_VERSION"
            _hd_expected_from="Expected version: v$_HOOKS_DAEMON_TRACKED_VERSION (daemon.expected_version in .claude/hooks-daemon.yaml)"
            if _version_lt "$_HOOKS_DAEMON_CLONE_VERSION" "$_HOOKS_DAEMON_TRACKED_VERSION"; then
                _hd_remedy_1="TO SYNC, this is an UPGRADE — a human runs:"
            else
                _hd_remedy_1="TO SYNC, this is a DOWNGRADE — v$_HOOKS_DAEMON_TRACKED_VERSION is OLDER than the installed v$_HOOKS_DAEMON_CLONE_VERSION. Confirm it is intended (the commit may have come from an older checkout; if not, correct daemon.expected_version). A human runs:"
            fi
            _hd_remedy_2="$_hd_upgrade_cmd
Nothing has been changed: hooks never move the daemon to another version themselves."
        else
            _hd_headline="HOOKS DAEMON: version mismatch — installed clone v$_HOOKS_DAEMON_CLONE_VERSION, tracked assets v$_HOOKS_DAEMON_TRACKED_VERSION"
            _hd_expected_from="Expected version: v$_HOOKS_DAEMON_TRACKED_VERSION (the .claude/HOOKS-DAEMON.md header)"
            if _version_lt "$_HOOKS_DAEMON_CLONE_VERSION" "$_HOOKS_DAEMON_TRACKED_VERSION"; then
                _hd_remedy_1="TO FIX — upgrade the clone to the version this repository expects:"
                _hd_remedy_2="$_hd_upgrade_cmd"
            else
                _hd_remedy_1="TO FIX — the TRACKED assets are the stale half here; regenerate and commit them:"
                _hd_remedy_2="  Run generate-docs from the installed clone, then commit the resulting diff."
            fi
        fi

        context_msg=$(printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s' \
            "$_hd_headline" \
            "" \
            "The daemon under .claude/hooks-daemon/ is gitignored and per-checkout," \
            "so it can fall behind the TRACKED assets this repository has committed." \
            "$_hd_expected_from" \
            "Checkout: $_hooks_daemon_checkout" \
            "" \
            "ALL safety handlers, code quality checks, and workflow enforcement are INACTIVE." \
            "" \
            "A RESTART CANNOT FIX THIS — both versions are unchanged by one." \
            "" \
            "$_hd_remedy_1" \
            "$_hd_remedy_2" \
            "Then restart your Claude session for hooks to activate.")
    elif [[ "${_HOOKS_DAEMON_STARTING:-false}" == "true" ]]; then
        # Still starting when this hook had to answer (Plan 00466 review 8,
        # R8-1). Nothing is known to be wrong, so nothing is to be fixed.
        context_msg=$(printf '%s\n%s\n%s\n%s' \
            "HOOKS DAEMON: the daemon is starting; retry" \
            "" \
            "The daemon was still starting when this hook had to answer, before its timeout." \
            "Hook safety handlers are inactive for this call only. Nothing needs fixing.")
    else
        # Standard error message
        # NOTE: Language is intentionally measured to avoid triggering investigation loops
        # in LLM agents. Previous "STOP work immediately" wording caused agents to abandon
        # tasks and enter analysis cycles instead of simply restarting the daemon.
        context_msg=$(printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s' \
            "HOOKS DAEMON: Not currently running" \
            "" \
            "Error: $error_type - $error_details" \
            "" \
            "Hook safety handlers are inactive until the daemon is restarted." \
            "If you are in the middle of an upgrade, this is expected and temporary." \
            "" \
            "TO FIX (usually takes a few seconds):" \
            "Use the hooks-daemon skill to restart the daemon." \
            "Then use the hooks-daemon skill to verify health." \
            "Invoke via Skill tool with skill=hooks-daemon and args=restart or args=health." \
            "" \
            "If restart fails, use the hooks-daemon skill to check logs (args=logs)." \
            "Then inform the user if the issue persists.")
    fi

    # Plan 00466 N24 review 4 R4-MA2: the DENY reason for PreToolUse (below,
    # when $_pretooluse_deny is "true") must not reuse $context_msg's
    # fail-open wording above -- it says safety handlers are "inactive" (they
    # are actively denying) and routes the agent to the Skill tool, which is
    # itself a PreToolUse call and so is denied the same way, wedging an
    # unattended agent in a loop. Give the deny its own honest text instead,
    # matching emit_error_json's socket_not_found wording: name the one
    # command that is actually allowed, and name the human fallback.
    local _pretooluse_deny_msg=""
    local _hd_advice=""
    if [[ "$_pretooluse_deny" == "true" && "${_HOOKS_DAEMON_STARTING:-false}" == "true" ]]; then
        # Plan 00466 review 8, R8-1: the call is denied only because the
        # daemon has not finished starting. A retry is the whole remedy.
        _pretooluse_deny_msg=$(printf '%s\n%s\n%s\n%s\n%s' \
            "HOOKS DAEMON: the daemon is starting; retry this call — denied for safety" \
            "" \
            "The daemon was still starting when this hook had to answer, before its" \
            "timeout, so no guard judged this call. Retry it in a few seconds; nothing" \
            "needs fixing. If it is still denied after a minute, restart the daemon.")
    elif [[ "$_pretooluse_deny" == "true" ]] \
        && ! _hd_advice="$(_hooks_daemon_recovery_advice)"; then
        # Plan 00466 round 3 (R2-1): python3 names the recovery command, and
        # under set -e its failure here would end the hook with no answer at
        # all. It also judges the carve-out, so no command is exempt: say so.
        _pretooluse_deny_msg=$(printf '%s\n%s\n%s\n%s\n%s\n%s\n%s' \
            "HOOKS DAEMON: could not connect at all — denied for safety" \
            "" \
            "Error: $error_type - $error_details" \
            "" \
            "python3 is missing or failing, and it is what recognises the daemon recovery" \
            "commands, so none is allowed either. A human must fix python3, or run the" \
            "daemon launcher directly: ! ${HOOKS_DAEMON_ROOT_DIR:-$_hooks_daemon_checkout/.claude/hooks-daemon}/bin/hooks-daemon restart")
    elif [[ "$_pretooluse_deny" == "true" ]]; then
        _pretooluse_deny_msg=$(printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s' \
            "HOOKS DAEMON: could not connect at all — denied for safety" \
            "" \
            "Error: $error_type - $error_details" \
            "" \
            "This call was denied because the daemon could not be reached at all," \
            "not because a guard judged it. Hook safety handlers are ACTIVE and" \
            "denying by default until the daemon answers again." \
            "" \
            "$_hd_advice")
    fi

    # Event-specific JSON formatting. jq is used only on this pure-error path
    # (the hot-path transport is jq-free since Plan 00156); a jq-less fallback
    # follows below for hosts without it.
    # Stop/SubagentStop: top-level decision only (deny to show error)
    # Other events: hookSpecificOutput with context (fail-open allow)
    if command -v jq &>/dev/null; then
        if [[ -z "$event_name" ]]; then
            # The guards that run while init.sh is being SOURCED reach here:
            # they fire before the forwarder that sourced us reaches its own
            # body, so the event in flight is genuinely unknown. Every branch
            # below keys on the event name, so none can be chosen honestly —
            # and `hookSpecificOutput` has nowhere to put "I do not know".
            # `systemMessage` is one of the five universal output fields
            # defined on EVERY event, so it needs no event name and cannot
            # name the wrong one. Fails open, like the branches below.
            jq -n --arg msg "$context_msg" '{"systemMessage": $msg}'
        elif [[ "$_HOOKS_DAEMON_CI_ENFORCED" == "true" ]]; then
            # CI enforced: hard deny/block for ALL event types to prevent work
            local ci_reason="Hooks daemon REQUIRED (ci_enabled: true) but not installed"
            if [[ "$event_name" == "PreToolUse" ]]; then
                jq -n --arg reason "$context_msg" \
                    '{"decision": "deny", "reason": $reason}'
            elif [[ "$event_name" == "Stop" || "$event_name" == "SubagentStop" ]]; then
                jq -n --arg reason "$ci_reason" \
                    '{"decision": "block", "reason": $reason}'
            else
                jq -n --arg event "$event_name" --arg context "$context_msg" \
                    '{"hookSpecificOutput": {"hookEventName": $event, "additionalContext": $context}}'
            fi
        elif [[ "$_HOOKS_DAEMON_VENV_MISSING" == "true" ]]; then
            # Venv missing for this path: Stop/SubagentStop block, others
            # fail-open with the upgrade-not-install guidance. The block
            # reason must not read as the plain NOT_INSTALLED block — a
            # reader who sees "venv" here knows install would be the wrong
            # fix, where a bare "not installed" would point them at it.
            if [[ "$event_name" == "Stop" || "$event_name" == "SubagentStop" ]]; then
                jq -n --arg reason "$venv_block_reason" \
                    '{"decision": "block", "reason": $reason}'
            else
                jq -n --arg event "$event_name" --arg context "$context_msg" \
                    '{"hookSpecificOutput": {"hookEventName": $event, "additionalContext": $context}}'
            fi
        elif [[ "$_HOOKS_DAEMON_NOT_INSTALLED" == "true" ]]; then
            # Not installed: Stop/SubagentStop block, others fail-open with install guidance.
            # The block reason names the checkout: this response is shaped
            # exactly like a WORKING stop gate's, so without the path there is
            # nothing to tell "the gate ran" from "no gate ran here".
            if [[ "$event_name" == "Stop" || "$event_name" == "SubagentStop" ]]; then
                jq -n --arg reason \
                    "Hooks daemon not installed at $_hooks_daemon_checkout - protection not active" \
                    '{"decision": "block", "reason": $reason}'
            else
                jq -n --arg event "$event_name" --arg context "$context_msg" \
                    '{"hookSpecificOutput": {"hookEventName": $event, "additionalContext": $context}}'
            fi
        else
            # Standard: Stop/SubagentStop block; PreToolUse denies (Plan
            # 00466 N24 review 3 MA4) unless stdin was the exact recovery
            # command; every other event fails open with context.
            if [[ "$event_name" == "Stop" || "$event_name" == "SubagentStop" ]]; then
                jq -n --arg reason "Hooks daemon not running - protection not active" \
                    '{"decision": "block", "reason": $reason}'
            elif [[ "$event_name" == "PreToolUse" && "$_pretooluse_deny" == "true" ]]; then
                jq -n --arg event "$event_name" --arg reason "$_pretooluse_deny_msg" \
                    '{"hookSpecificOutput": {"hookEventName": $event, "permissionDecision": "deny", "permissionDecisionReason": $reason}}'
            else
                jq -n --arg event "$event_name" --arg context "$context_msg" \
                    '{"hookSpecificOutput": {"hookEventName": $event, "additionalContext": $context}}'
            fi
        fi
    else
        # Fallback when jq is absent. Plan 00156 removed jq from the hot-path
        # transport, so a genuinely jq-less host is now plausible and this path
        # can really fire. Encode with python3 (already the transport dependency,
        # so a host without it cannot send any hook request anyway): every value
        # passes via argv, so json.dumps escapes quotes/newlines/backslashes and
        # they can neither break the JSON document nor inject into the source.
        # Mirrors the jq branch's policy exactly — Stop/SubagentStop fail CLOSED
        # (decision=block); other events fail open with context.
        python3 -c '
import json
import sys

event_name, context_msg, ci_enforced, not_installed, checkout, venv_missing, venv_block_reason, pretooluse_deny, pretooluse_deny_msg = sys.argv[1:10]
stop_events = ("Stop", "SubagentStop")

if not event_name:
    # No event name: see the jq branch above. systemMessage is universal, so it
    # is the only field that can carry this without naming an event. No
    # backticks in this block -- the outer shell quotes it, and shellcheck
    # reads a backtick inside single quotes as a dead command substitution.
    resp = {"systemMessage": context_msg}
elif ci_enforced == "true":
    if event_name == "PreToolUse":
        resp = {"decision": "deny", "reason": context_msg}
    elif event_name in stop_events:
        resp = {"decision": "block", "reason": "Hooks daemon REQUIRED (ci_enabled: true) but not installed"}
    else:
        resp = {"hookSpecificOutput": {"hookEventName": event_name, "additionalContext": context_msg}}
elif venv_missing == "true":
    # Clone present, venv missing for this path: same reasoning as the jq
    # branch above -- the block reason must not read as plain NOT_INSTALLED,
    # since the remedy that answer names (install/force) is destructive here.
    # The shell composed the reason once, so both encoders say the same.
    if event_name in stop_events:
        resp = {"decision": "block", "reason": venv_block_reason}
    else:
        resp = {"hookSpecificOutput": {"hookEventName": event_name, "additionalContext": context_msg}}
elif not_installed == "true":
    if event_name in stop_events:
        resp = {
            "decision": "block",
            "reason": f"Hooks daemon not installed at {checkout} - protection not active",
        }
    else:
        resp = {"hookSpecificOutput": {"hookEventName": event_name, "additionalContext": context_msg}}
else:
    # Standard: Stop/SubagentStop block; PreToolUse denies (Plan 00466 N24
    # review 3 MA4) unless stdin was the exact recovery command; every
    # other event fails open with context.
    if event_name in stop_events:
        resp = {"decision": "block", "reason": "Hooks daemon not running - protection not active"}
    elif event_name == "PreToolUse" and pretooluse_deny == "true":
        resp = {
            "hookSpecificOutput": {
                "hookEventName": event_name,
                "permissionDecision": "deny",
                "permissionDecisionReason": pretooluse_deny_msg,
            }
        }
    else:
        resp = {"hookSpecificOutput": {"hookEventName": event_name, "additionalContext": context_msg}}

print(json.dumps(resp))
' "$event_name" "$context_msg" "$_HOOKS_DAEMON_CI_ENFORCED" "$_HOOKS_DAEMON_NOT_INSTALLED" \
            "$_hooks_daemon_checkout" "$_HOOKS_DAEMON_VENV_MISSING" "$venv_block_reason" \
            "$_pretooluse_deny" "$_pretooluse_deny_msg" || {
            # No jq and no working python3: nothing can encode the reason, but
            # a deny must still reach Claude Code (Plan 00466 round 3, R2-1).
            local _hd_encoder_rc=$?
            if [[ "$event_name" == "PreToolUse" \
                && ( "$_pretooluse_deny" == "true" || "$_HOOKS_DAEMON_CI_ENFORCED" == "true" ) ]]; then
                _hooks_daemon_static_deny
            else
                return "$_hd_encoder_rc"
            fi
        }
    fi
}

# Detect project path by walking up from init.sh's directory.
# (init.sh lives at .claude/init.sh, so its parent contains the project.)
_INIT_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_PATH="${_INIT_SCRIPT_DIR}"

# Walk up to find .claude directory
while [[ "$PROJECT_PATH" != "/" ]]; do
    if [[ -d "$PROJECT_PATH/.claude" ]]; then
        break
    fi
    PROJECT_PATH="$(dirname "$PROJECT_PATH")"
done

if [[ "$PROJECT_PATH" == "/" ]]; then
    # Output valid JSON error to stdout - event name unknown at this point
    emit_hook_error "" "init_path_error" "Could not find .claude directory in path hierarchy. Hooks daemon cannot initialize."
    exit 0  # Exit 0 so Claude Code processes the JSON response
fi

# Load environment overrides if present (for self-installation or custom setups)
if [[ -f "$PROJECT_PATH/.claude/hooks-daemon.env" ]]; then
    # shellcheck disable=SC1091
    source "$PROJECT_PATH/.claude/hooks-daemon.env"
fi

# Set daemon root directory (defaults to .claude/hooks-daemon, can be overridden)
HOOKS_DAEMON_ROOT_DIR="${HOOKS_DAEMON_ROOT_DIR:-$PROJECT_PATH/.claude/hooks-daemon}"

#
# Nested installation check
#
# Detects if hooks-daemon has been installed inside itself creating
# .claude/hooks-daemon/.claude/hooks-daemon structure
#
if [[ -d "$PROJECT_PATH/.claude/hooks-daemon/.claude/hooks-daemon" ]]; then
    emit_hook_error "" "nested_installation" \
        "NESTED INSTALLATION DETECTED! Found: $PROJECT_PATH/.claude/hooks-daemon/.claude/hooks-daemon. Remove $PROJECT_PATH/.claude/hooks-daemon and reinstall."
    exit 0
fi

#
# Git remote detection for self-install validation
#
# If this is the hooks-daemon repo itself (detected by git remote),
# require self_install_mode in config or HOOKS_DAEMON_ROOT_DIR override
#
is_hooks_daemon_repo() {
    local remote_url
    remote_url=$(git -C "$PROJECT_PATH" remote get-url origin 2>/dev/null || echo "")
    remote_url=$(echo "$remote_url" | tr '[:upper:]' '[:lower:]')

    if [[ "$remote_url" == *"claude-code-hooks-daemon"* ]] || \
       [[ "$remote_url" == *"claude_code_hooks_daemon"* ]]; then
        return 0  # true - is hooks-daemon repo
    fi
    return 1  # false - not hooks-daemon repo
}

# Check if we're in the hooks-daemon repo without proper configuration
if [[ -d "$PROJECT_PATH/.git" ]]; then
    if is_hooks_daemon_repo; then
        # Check if self_install_mode is enabled in config or env override is set
        has_self_install=false

        # Check HOOKS_DAEMON_ROOT_DIR override (from hooks-daemon.env)
        if [[ "$HOOKS_DAEMON_ROOT_DIR" == "$PROJECT_PATH" ]]; then
            has_self_install=true
        fi

        # Deliberately NOT read: .claude/hooks-daemon.yaml's self_install_mode.
        # It is tracked and says `true` in this repository, so reading it would
        # satisfy this guard on every fresh clone — and that is exactly wrong.
        # The config declares INTENT; the two signals above are evidence the
        # runtime was actually BUILT. A clone has the intent and none of the
        # runtime, so believing the config would wave it through to the
        # "not installed" branch, whose advice is to run the CLIENT installer —
        # which overwrites this repository's own tracked config. Refusing here,
        # with the bootstrap instruction, is the useful answer.

        if [[ "$has_self_install" != "true" ]] && [[ ! -f "$PROJECT_PATH/.claude/hooks-daemon.env" ]]; then
            _HOOKS_DAEMON_REPO_UNCONFIGURED=true
            emit_hook_error "" "hooks_daemon_repo_detected" \
                "This is the hooks-daemon repository. To set it up for development, run: scripts/bootstrap-self-install.sh"
            exit 0
        fi
    fi
fi

# Venv Python (only needed for daemon startup, NOT for hot path).
#
# Plan 00099: venv is keyed by Python-environment fingerprint so that
# concurrent containers from the same image share one venv while distinct
# Pythons (pyenv vs distro, different minor versions, cross-arch) are kept
# apart. The fingerprint computation invokes Python, so it is deferred to
# `_resolve_python_cmd()` which is called only from `start_daemon()` /
# `validate_venv()` — never on the hot path.
#
# Precedence (highest first):
#   1. $HOOKS_DAEMON_VENV_PATH (explicit override)
#   2. $HOOKS_DAEMON_ROOT_DIR/untracked/venv-{fingerprint}/ (fingerprint-keyed)
#   3. $HOOKS_DAEMON_ROOT_DIR/untracked/venv-*/ (any existing fingerprint venv)
#
# Plan 00103 Decision 2: when none of the above resolve, fail loudly with
# return 5 + stderr directive. The pre-v3.7.0 unversioned legacy
# `untracked/venv/bin/python` is no longer a silent fallback  # python-var-guidance-exempt: names the retired path to document its rejection
# — it hid the v3.9.0 field-bug regression where operators saw "venv not
# found" while the real cause was a 3.9-vs-3.11 `import tomllib` crash.
#
# Plan 00103 Decision 3 Rule A: no `${VAR:-python3}` parameter expansion —
# the fingerprint helper is invoked under a venv-resident interpreter
# (HOOKS_DAEMON_PYTHON or a discovered venv-*/bin/python), never bare
# `python3`. The scan-fallback handles cross-fingerprint resolution.
PYTHON_CMD=""  # populated lazily by _resolve_python_cmd

# Plan 00104 Phase 4: delegate to canonical library at
# ${HOOKS_DAEMON_ROOT_DIR}/scripts/lib/resolve_venv.sh. The library invokes
# paths.py SSOT — including the metadata-authoritative step (Plan 00100
# Task 3.5) — so init.sh, install/venv_resolver.sh, _resolve-venv.sh, and
# venv-include.bash all converge on the same venv. This closes the drift
# the v3.9.x bash scan-fallback created: alphabetic ordering picked the
# wrong fingerprint when two venvs coexisted, while the SSOT correctly
# preferred the lock_hash-matching one.
_resolve_python_cmd() {
    local lib="${HOOKS_DAEMON_ROOT_DIR}/scripts/lib/resolve_venv.sh"
    if [ ! -f "$lib" ]; then
        echo "❌ _resolve_python_cmd: canonical library missing at $lib" >&2
        echo "   Reinstall the daemon so scripts/lib/resolve_venv.sh is present." >&2
        PYTHON_CMD=""
        return 5
    fi

    # shellcheck disable=SC1090  # path is computed at runtime
    source "$lib"

    # The status is captured from the assignment itself: after an `if` with no
    # `else` whose condition failed, $? is 0, which made a failed resolve look
    # like success with PYTHON_CMD empty.
    local rv=0
    PYTHON_CMD="$(resolve_venv_python "$HOOKS_DAEMON_ROOT_DIR")" || rv=$?
    if [[ "$rv" -ne 0 ]]; then
        PYTHON_CMD=""
        return "$rv"
    fi
    return 0
}

#
# _get_hostname_suffix() - Get hostname-based suffix for runtime files
#
# Resolves a STABLE hostname (in series): $HOSTNAME, then the `hostname`
# command (the OS hostname), then a constant. This MUST agree with the Python
# side (daemon/paths.py:_resolve_hostname_from_env, which uses
# socket.gethostname()) so the bash forwarder and the Python daemon compute the
# SAME socket/PID suffix.
#
# NEVER use a time-based hash here: $HOSTNAME is unset on macOS (zsh) and many
# minimal containers, and a time hash changes on every call — so start/status/
# stop would each look for a different socket (the macOS daemon-unmanageable
# bug, Plan 00122 BUG 1).
#
# Returns:
#   "-{sanitized-hostname}"
#
# Example:
#   HOSTNAME="laptop" -> "-laptop"
#   HOSTNAME="506355bfbc76" -> "-506355bfbc76"
#   HOSTNAME="My-Server" -> "-my-server"
#   No HOSTNAME -> "-{os-hostname}" (e.g. "-work.local"), or "-localhost"
#
_get_hostname_suffix() {
    local hostname="${HOSTNAME:-}"

    # No $HOSTNAME (macOS/zsh, minimal containers)? Use the OS hostname — the
    # same value Python's socket.gethostname() returns — so both sides agree.
    if [[ -z "$hostname" ]] && command -v hostname > /dev/null; then
        hostname="$(hostname)"
    fi

    # Last resort: a stable constant (matches paths.py _HOSTNAME_FALLBACK).
    if [[ -z "$hostname" ]]; then
        hostname="localhost"
    fi

    # Sanitize hostname for filesystem safety: lowercase, no spaces. The
    # space->hyphen pass uses bash parameter expansion (no process spawn); the
    # lowercase pass stays on tr for bash 3.2 compatibility (macOS ships no
    # ${var,,}). One tr spawn instead of two, and no echo pipeline (Plan 00156 T3).
    local sanitized="${hostname// /-}"
    sanitized=$(tr '[:upper:]' '[:lower:]' <<< "$sanitized")
    echo "-${sanitized}"
}

#
# _exec_bit_selfheal() - Restore +x on sibling hook scripts (Plan 00102 Phase 3).
#
# Defense-in-depth: even though the daemon's invocation form is `bash <abs-path>`
# (Phase 1, makes the bit irrelevant), defensively restore the executable bit on
# sibling hook wrappers if it has been dropped by core.fileMode=false, an IDE
# rewrite, a tarball/ZIP transfer, etc. Throttled once per hour via mtime on a
# fingerprint file so the cost is amortised across hook invocations.
#
# Variables required in scope:
#   HOOK_SCRIPT_DIR - directory containing hook wrapper scripts
#   _untracked_dir  - daemon's untracked dir (where the throttle file lives)
#
_exec_bit_selfheal() {
    local throttle="$_untracked_dir/.exec-bit-checked"
    local now mtime
    now=$(date +%s)

    if [[ -f "$throttle" ]]; then
        # Linux: stat -c %Y. macOS/BSD: stat -f %m. If both fail we fall
        # through to running the chmod (safer than silently skipping).
        if mtime=$(stat -c %Y "$throttle" 2>/dev/null); then
            :
        elif mtime=$(stat -f %m "$throttle" 2>/dev/null); then
            :
        else
            mtime=0
        fi

        if [[ "$mtime" =~ ^[0-9]+$ ]] && [[ $((now - mtime)) -lt 3600 ]]; then
            return 0
        fi
    fi

    local hooks=(
        pre-tool-use
        post-tool-use
        session-start
        session-end
        stop
        subagent-stop
        user-prompt-submit
        notification
        pre-compact
        permission-request
        setup
        permission-denied
        cwd-changed
        worktree-create
        worktree-remove
        user-prompt-expansion
        post-tool-use-failure
        post-tool-batch
        subagent-start
        task-created
        task-completed
        stop-failure
        teammate-idle
        instructions-loaded
        config-change
        file-changed
        post-compact
        elicitation
        elicitation-result
        message-display
    )
    local h
    for h in "${hooks[@]}"; do
        local p="$HOOK_SCRIPT_DIR/$h"
        if [[ -f "$p" ]]; then
            chmod +x "$p"
        fi
    done

    touch "$throttle"
}

# Generate socket and PID paths using pure bash (no Python dependency)
# SECURITY: Paths stored in daemon's untracked directory, NOT /tmp
# Pattern: {project}/.claude/hooks-daemon/untracked/daemon.{sock|pid}
# Container: {project}/.claude/hooks-daemon/untracked/daemon-{hash}.{sock|pid}
# Must match Python paths module: claude_code_hooks_daemon.daemon.paths

# Determine untracked directory path
# Must match ProjectContext.daemon_untracked_dir() logic
# Use HOOKS_DAEMON_ROOT_DIR (set by .env in self-install, defaults to .claude/hooks-daemon)
_untracked_dir="${HOOKS_DAEMON_ROOT_DIR}/untracked"

# Create untracked directory if it doesn't exist. Guarded so the common
# (dir-exists) path skips the mkdir process spawn on every event (Plan 00156 T3).
[[ -d "$_untracked_dir" ]] || mkdir -p "$_untracked_dir"

# Plan 00102 Phase 3 (Tier 3a): defensively restore +x on sibling hook
# wrappers if dropped (core.fileMode=false, IDE rewrite, tarball transfer).
# Throttled once per hour via mtime on $_untracked_dir/.exec-bit-checked.
HOOK_SCRIPT_DIR="$PROJECT_PATH/.claude/hooks"
_exec_bit_selfheal

# Generate hostname-based suffix for path isolation
_hostname_suffix=$(_get_hostname_suffix)

# Allow environment variable overrides (for testing)
SOCKET_PATH="${CLAUDE_HOOKS_SOCKET_PATH:-$_untracked_dir/daemon${_hostname_suffix}.sock}"
PID_PATH="${CLAUDE_HOOKS_PID_PATH:-$_untracked_dir/daemon${_hostname_suffix}.pid}"

# Socket discovery file: when the default socket path exceeds the AF_UNIX
# length limit (108 bytes), the Python daemon falls back to a shorter path
# (XDG_RUNTIME_DIR, /run/user/, or /tmp) and writes the actual socket path
# to a discovery file. Read it if the default socket doesn't exist.
if [[ -z "${CLAUDE_HOOKS_SOCKET_PATH:-}" ]] && [[ ! -S "$SOCKET_PATH" ]]; then
    _discovery_file="$_untracked_dir/daemon${_hostname_suffix}.socket-path"
    if [[ -f "$_discovery_file" ]]; then
        _discovered_path=$(cat "$_discovery_file" 2>/dev/null)
        if [[ -n "$_discovered_path" ]] && [[ -S "$_discovered_path" ]]; then
            SOCKET_PATH="$_discovered_path"
            # The daemon that fell back put its PID file beside that socket
            # under the same stem (paths.get_pid_path mirrors get_socket_path),
            # so the PID path must follow too. Left at the long default,
            # is_daemon_running finds no PID, `cli start` then sees a live
            # socket that is "not ours" and refuses it, and every forwarder
            # reports daemon_startup_failed while status shows RUNNING.
            if [[ -z "${CLAUDE_HOOKS_PID_PATH:-}" ]]; then
                PID_PATH="${_discovered_path%.sock}.pid"
            fi
        fi
    fi
fi

# Daemon startup timeout (deciseconds - 1/10th second units).
#
# 15 seconds matches Timeout.DAEMON_RESTART_VERIFY_TIMEOUT_SEC, the
# python-side ceiling used by scripts/install/daemon_control.sh::
# restart_daemon_verified (Plan 00100 Task 0.2). Cold-start Python
# with 50+ handler imports + config load + asyncio bind can take 5-10s
# on slow disks (containers, cold caches). The pre-Issue-1 ceiling of
# 50 deciseconds (5s) produced false `daemon_startup_failed` reports
# while the daemon was still binding — see Issue 1 in
# untracked/hooks-daemon-niggles.md (2026-05-14 field report).
#
# It is how long the poll runs on once the launcher has finished, and never
# past _HOOKS_DAEMON_START_DEADLINE.
DAEMON_STARTUP_TIMEOUT=150

# How far into this hook (bash's SECONDS, which counts from the hook's own
# start) a daemon start is waited on before the hook denies "the daemon is
# starting; retry" (Plan 00466 review 8, R8-1). A PreToolUse hook that
# reaches the 60 s timeout lets the call run unjudged, and after this can
# come one helper run's start-lock wait and the request the daemon answers.
# Twin of Timeout.HOOK_START_DEADLINE_SEC.
_HOOKS_DAEMON_START_DEADLINE=15

# Export paths for use by forwarder scripts
export HOOKS_DAEMON_ROOT_DIR
export SOCKET_PATH
export PID_PATH
export PROJECT_PATH
# Note: PYTHON_CMD is intentionally NOT exported - only used internally
# by start_daemon() and validate_venv(). Hot path uses system python3.

#
# validate_venv() - Check if venv is healthy for daemon startup
#
# Returns:
#   0 if venv is healthy
#   1 if venv is broken (outputs diagnostic to stderr)
#
# Sets VENV_ERROR with human-readable error message on failure.
#
validate_venv() {
    VENV_ERROR=""

    # Plan 00099: lazy fingerprint-keyed venv resolution (paid only on daemon
    # startup, never on hot path). No-op on subsequent calls.
    #
    # Plan 00103 Decision 2: _resolve_python_cmd returns 5 + stderr on
    # failure instead of silently emitting the legacy path. Capture the
    # return code explicitly — a bare call would propagate via set -e and
    # kill init.sh sourcing before validate_venv's caller-friendly
    # VENV_ERROR diagnostic can be reported.
    if [ -z "$PYTHON_CMD" ]; then
        local resolve_rv=0
        _resolve_python_cmd || resolve_rv=$?
        if [ "$resolve_rv" -ne 0 ]; then
            VENV_ERROR="Venv Python could not be resolved (exit $resolve_rv). Run: cd $HOOKS_DAEMON_ROOT_DIR && uv sync"
            return 1
        fi
    fi

    # Check venv Python binary exists
    if [[ -z "$PYTHON_CMD" || ! -f "$PYTHON_CMD" ]]; then
        VENV_ERROR="Venv Python not found at ${PYTHON_CMD:-<unresolved>}. Run: cd $HOOKS_DAEMON_ROOT_DIR && uv sync"
        return 1
    fi

    # Check venv Python is executable
    if [[ ! -x "$PYTHON_CMD" ]]; then
        VENV_ERROR="Venv Python not executable at $PYTHON_CMD. Run: cd $HOOKS_DAEMON_ROOT_DIR && uv sync"
        return 1
    fi

    # Check key package is importable
    if ! "$PYTHON_CMD" -c "import claude_code_hooks_daemon" 2>/dev/null; then
        VENV_ERROR="Cannot import claude_code_hooks_daemon. Venv may be broken (stale .pth files or Python version mismatch). Run: cd $HOOKS_DAEMON_ROOT_DIR && uv sync"
        return 1
    fi

    return 0
}

#
# Linux's PID_MAX_LIMIT: no pid is larger. Twin of paths.PID_MAX_LIMIT.
_HOOKS_DAEMON_PID_MAX=4194304

#
# _hooks_daemon_is_pid_text() - True when $1 is a PID file's text naming one
# pid a daemon could hold (Plan 00466 round 4, Sh-B). 0 and negative numbers
# are process groups to kill, so kill -0 on them succeeds against this
# shell's own group; 1 is init. Twin of paths.parse_pid_text, which also
# accepts the trailing newlines $(cat) has already dropped here.
_hooks_daemon_is_pid_text() {
    [[ "$1" =~ ^[1-9][0-9]{0,6}$ ]] && (($1 > 1 && $1 <= _HOOKS_DAEMON_PID_MAX))
}

#
# _hooks_daemon_root_is_this_install() - True when HOOKS_DAEMON_ROOT_DIR is
# an install of this project, by the P3-1 rule (_installs_launcher above).
# Its venv is run only then (Plan 00466 round 5, Sh-F).
_hooks_daemon_root_is_this_install() {
    local recovery_py
    _hooks_daemon_recovery_py recovery_py
    python3 -c "$recovery_py"'
import sys

sys.exit(0 if _installs_launcher(sys.argv[1], sys.argv[2]) is not None else 1)
' "${PROJECT_PATH:-}" "${HOOKS_DAEMON_ROOT_DIR:-}"
}

# What the one run of the daemon's helper a hook may make judged, and its
# answer (Plan 00466 round 5, R4-1).
_HOOKS_DAEMON_HELPER_KEY=""
_HOOKS_DAEMON_HELPER_STATUS=1

#
# _hooks_daemon_run_cli_helper() - Run the daemon package's python snippet
# $2 with the remaining arguments, in the daemon's own venv, to answer the
# question $1 names. Fails when the venv is not this install's or cannot be
# used, which every caller treats as "not proven".
#
# Importing the daemon package costs about half a second, and the startup
# poll asks the same question every tick (round 5, R4-1): 150 of those ran
# the hook past its 60 s timeout, and PreToolUse failed open. So it runs at
# most once per hook. The same question gets the cached answer; any other
# is not proven.
_hooks_daemon_run_cli_helper() {
    local key="$1" snippet="$2"
    shift 2
    if [[ -n "$_HOOKS_DAEMON_HELPER_KEY" ]]; then
        if [[ "$_HOOKS_DAEMON_HELPER_KEY" == "$key" ]]; then
            return "$_HOOKS_DAEMON_HELPER_STATUS"
        fi
        return 1
    fi
    _HOOKS_DAEMON_HELPER_KEY="$key"
    _HOOKS_DAEMON_HELPER_STATUS=1
    if ! _hooks_daemon_root_is_this_install; then
        return 1
    fi
    if [[ -z "$PYTHON_CMD" ]] && ! _resolve_python_cmd; then
        return 1
    fi
    local rv=0
    "$PYTHON_CMD" -c "$snippet" "$@" || rv=$?
    _HOOKS_DAEMON_HELPER_STATUS="$rv"
    return "$rv"
}

#
# _hooks_daemon_helper_proves_pid() - The daemon's own proof that live pid
# $1 is this daemon: its socket answers as this project's daemon, or, for a
# pid this user owns, its command line (cli.pid_is_this_projects_daemon).
_hooks_daemon_helper_proves_pid() {
    _hooks_daemon_run_cli_helper "prove:$1" '
import sys
from pathlib import Path

from claude_code_hooks_daemon.daemon.cli import pid_is_this_projects_daemon

sys.exit(0 if pid_is_this_projects_daemon(int(sys.argv[1]), Path(sys.argv[2]), Path(sys.argv[3])) else 1)
' "$1" "$SOCKET_PATH" "$PROJECT_PATH"
}

# Where the kernel's process table is mounted.
_HOOKS_DAEMON_PROCFS=/proc

#
# _hooks_daemon_argv_prove_this_project() - True when the arguments are a
# daemon server of this project, at the exact positions start_daemon and
# bin/hooks-daemon launch it with: `<python> -m <cli> --project-root <root>
# start|restart` and nothing more, since neither subcommand takes an
# argument, with no other argument naming a root (Plan 00466 round 6,
# P5-3; N203). A start or restart naming no root is re-run naming it
# (cli._reexec_daemon_launch_with_explicit_project_root). A sufficient
# condition only (round 5, Sh-D): process_verification's rule accepts more,
# and anything this does not prove goes to the daemon's helper. A test
# checks that everything this proves is proven there too.
_hooks_daemon_argv_prove_this_project() {
    local physical
    if _hooks_daemon_argv_name_root "$PROJECT_PATH" "$@"; then
        return 0
    fi
    physical="$(cd -P -- "$PROJECT_PATH" && pwd -P)" || return 1
    [[ "$physical" != "$PROJECT_PATH" ]] && _hooks_daemon_argv_name_root "$physical" "$@"
}

_hooks_daemon_argv_name_root() {
    local root="$1" index
    shift
    local -a argv=("$@")
    ((${#argv[@]} == 6)) || return 1
    [[ "${argv[1]}" == "-m" && "${argv[2]}" == "claude_code_hooks_daemon.daemon.cli" &&
        "${argv[3]}" == "--project-root" && "${argv[4]}" == "$root" &&
        ("${argv[5]}" == "start" || "${argv[5]}" == "restart") ]] || return 1
    # A root named anywhere else, even as the program's own name, proves
    # nothing here: the daemon's helper reads such a line as argparse does.
    for ((index = 0; index < ${#argv[@]}; index++)); do
        if ((index != 3)) && [[ "${argv[index]}" == "--project-root" ||
            "${argv[index]}" == "--project-root="* ]]; then
            return 1
        fi
    done
}

#
# _hooks_daemon_cmdline_proves_this_project() - The procfs cmdline file $1
# proves this project's daemon. Its arguments are read split at their NULs
# (Plan 00466 round 6, R5-2), as psutil reads them: joined by newlines, one
# argument holding a newline-separated launch read as that launch. A read
# loop, not mapfile, which bash 3.2 lacks; a last argument with no final NUL
# is still an argument. An empty file (a zombie, a kernel thread) proves
# nothing, and bash before 4.4 cannot expand an empty array under set -u.
_hooks_daemon_cmdline_proves_this_project() {
    local -a argv=()
    local arg
    [[ -r "$1" ]] || return 1
    while IFS= read -r -d '' arg || [[ -n "$arg" ]]; do
        argv+=("$arg")
    done < "$1" || return 1
    ((${#argv[@]})) || return 1
    _hooks_daemon_argv_prove_this_project "${argv[@]}"
}

#
# _hooks_daemon_ps_args_prove_this_project() - The arguments ps prints, $1,
# prove this project's daemon. ps joins them with spaces, so its words are
# read as the arguments: a project whose path holds a space is left to the
# helper, and one argument holding a whole launch line passes. Only a
# process this user owns gets here, and it could as well run that line.
_hooks_daemon_ps_args_prove_this_project() {
    local -a argv=()
    read -r -a argv <<< "$1"
    ((${#argv[@]})) || return 1
    _hooks_daemon_argv_prove_this_project "${argv[@]}"
}

#
# _hooks_daemon_pid_args_prove_this_project() - The command line of pid $1
# proves it is this project's daemon: procfs's where it is mounted, else
# ps's.
_hooks_daemon_pid_args_prove_this_project() {
    local cmdline="$_HOOKS_DAEMON_PROCFS/$1/cmdline" args
    if [[ -r "$cmdline" ]]; then
        _hooks_daemon_cmdline_proves_this_project "$cmdline"
        return
    fi
    if ! args="$(ps -ww -o args= -p "$1")"; then
        return 1
    fi
    _hooks_daemon_ps_args_prove_this_project "$args"
}

#
# _hooks_daemon_pid_is_this_users() - True when pid $1's real and effective
# uids are both this shell's effective uid. Ownership is the owner's uid,
# never permission to signal (Plan 00466 round 6, P5-1): root may signal
# every process, so `kill -0` passed another user's process as this user's.
_hooks_daemon_pid_is_this_users() {
    local owner="$_HOOKS_DAEMON_PROCFS/$1/status" uids
    local -a ids
    if [[ -r "$owner" ]]; then
        uids="$(awk '$1 == "Uid:" { print $2, $3; exit }' "$owner")" || return 1
    elif ! uids="$(ps -o ruid=,uid= -p "$1")"; then
        return 1
    fi
    read -r -a ids <<< "$uids"
    ((${#ids[@]} == 2)) && [[ "${ids[0]}" == "$EUID" && "${ids[1]}" == "$EUID" ]]
}

#
# is_daemon_running() - Check if daemon is running
#
# Returns:
#   0 if daemon is running
#   1 if daemon is not running (a stale or corrupt PID file, or none)
#   2 if its pid names a live process that nothing proves is this daemon:
#     unknown, so a caller must not skip a start (Plan 00466 round 4,
#     N139-A; round 5, Sh-D). `cli start` then decides, and its REUSE gate
#     never displaces a live daemon.
#
is_daemon_running() {
    # Check if PID file exists
    if [[ ! -f "$PID_PATH" ]]; then
        return 1
    fi

    # Read PID from file
    local pid
    pid=$(cat "$PID_PATH" 2>/dev/null || echo "")

    if _hooks_daemon_is_pid_text "$pid"; then
        # A live process is not proof of this daemon (round 5, Sh-D): after a
        # reboot its pid can be anyone's. Its command line decides, then the
        # daemon's helper.
        #
        # Plan 00466 N139: only ESRCH means the process is gone. EPERM (a
        # process of another user's) and anything else unrecognised mean it
        # may still run, so the daemon is not provably down and its PID file
        # stays. strerror text in the C locale is the one portable signal the
        # builtin gives. A command line is anyone's to write, so it counts
        # only for a process this user owns (round 5, P4-2; round 6, P5-1);
        # for any other, only the socket answering as this daemon does.
        local probe_error=""
        if kill -0 "$pid" 2>/dev/null || probe_error="$(export LC_ALL=C; kill -0 "$pid" 2>&1)"; then
            if { _hooks_daemon_pid_is_this_users "$pid" &&
                _hooks_daemon_pid_args_prove_this_project "$pid"; } ||
                _hooks_daemon_helper_proves_pid "$pid"; then
                return 0
            fi
            return 2
        fi
        if [[ "$probe_error" != *"No such process"* ]]; then
            if _hooks_daemon_helper_proves_pid "$pid"; then
                return 0
            fi
            return 2
        fi
    fi

    # Stale or corrupt: removed under the start lock a daemon start writes
    # its pid under, and only while the file still holds what was read here
    # (Plan 00466 round 2, S2; round 4, Sh-A). Without the daemon's venv it
    # stays, which is harmless: it never counts as running, and a starting
    # daemon overwrites it.
    if ! _hooks_daemon_run_cli_helper "remove:$pid" '
import sys
from pathlib import Path

from claude_code_hooks_daemon.daemon.server import remove_stale_pid_file

sys.exit(0 if remove_stale_pid_file(Path(sys.argv[1]), Path(sys.argv[2]), sys.argv[3]) else 1)
' "$PID_PATH" "$SOCKET_PATH" "$pid"; then
        : "the PID file stays: it was not provably stale under the lock"
    fi
    return 1
}

#
# start_daemon() - Start daemon in background
#
# Launches daemon process and waits for Unix socket to be ready.
# Daemon starts in background and detaches from terminal.
#
# Returns:
#   0 if daemon started successfully
#   1 if daemon failed to start
#
start_daemon() {
    # Check if already running
    if is_daemon_running; then
        return 0
    fi

    # Validate venv before attempting startup (fail-fast with actionable error)
    if ! validate_venv; then
        echo "ERROR: Venv validation failed: $VENV_ERROR" >&2
        return 1
    fi

    # NOTE (Plan 00127): do NOT `rm -f "$SOCKET_PATH"` here. On the
    # host+container shared-untracked path the socket may be owned by a LIVE
    # incumbent daemon, and unconditionally deleting it would steal the socket
    # before the python layer's liveness gate ever runs. Stale-socket cleanup is
    # now the single responsibility of the python server, which probes socket
    # liveness before unlinking (reuse on live, unlink on stale). We already
    # short-circuit via is_daemon_running() above for the healthy-incumbent case.

    # The daemon names its root resolved (round 9b): its text is never
    # resolved, so a root named through a link was refused by every caller
    # that resolves its own, bin/hooks-daemon included. The start is also
    # handed the root as this hook reached it: single-daemon enforcement
    # needs that spelling to find a daemon an older version started through
    # a link (review 10, R10-2).
    local physical_root
    if ! physical_root="$(cd -P -- "$PROJECT_PATH" && pwd -P)"; then
        echo "ERROR: cannot resolve the project root $PROJECT_PATH" >&2
        return 1
    fi

    # Start daemon using CLI (proper daemonization)
    # CRITICAL: Pass --project-root and export env vars so the CLI uses the
    # same paths we computed above. Without this, the CLI re-discovers the
    # project from CWD which may find a worktree's .claude/ instead of ours.
    #
    # Output is CAPTURED, not discarded (Plan 00200 Task 5.5): this parent
    # invocation is the short-lived process that daemonises and returns —
    # cli.py's cmd_start() prints its own diagnostics (e.g. "ERROR: Fork
    # failed", "ERROR: Daemon not proven started: the daemon exited while
    # starting (no PID file created)") on
    # THIS fd, before the double-fork detaches the long-lived daemon (which
    # redirects its OWN stdout/stderr to /dev/null internally regardless —
    # see daemon/cli.py's "Second child" branch). The readiness poll below
    # remains the authority for success/failure either way; this capture
    # only stops a genuine startup failure's root cause from being silently
    # discarded on the timeout path.
    #
    # The recovery source is exported only for this hook's own child shells
    # (see _hooks_daemon_recovery_py); the daemon is not one of them. The
    # un-export happens inside the substitution's subshell, not here.
    #
    # The launcher runs beside this hook, which reads its output as it comes
    # and never waits on it (Plan 00466 review 8, R8-1): its own waits (a
    # start already under way, two start-lock waits, a 30 s start budget)
    # outlast the hook's 60 s timeout,
    # and a PreToolUse hook that times out lets the call run unjudged. A
    # launcher still writing once the hook has answered gets EPIPE, which
    # only its parent meets, after the fork: the daemon keeps starting. Its
    # stdin is not the hook's, which carries the call to judge.
    local launch_fd launch_output="" launch_done=false
    exec {launch_fd}< <(export -fn _hooks_daemon_recovery_py
        CLAUDE_HOOKS_SOCKET_PATH="$SOCKET_PATH" \
            CLAUDE_HOOKS_PID_PATH="$PID_PATH" \
            CLAUDE_HOOKS_DAEMON_CALLER_ROOT="$PROJECT_PATH" \
            $PYTHON_CMD -m claude_code_hooks_daemon.daemon.cli \
            --project-root "$physical_root" start < /dev/null 2>&1)

    # Wait for daemon to be ready (using deciseconds for integer arithmetic).
    #
    # Issue 1 (untracked/hooks-daemon-niggles.md, 2026-05-14): the legacy
    # check polled socket existence alone. enforce_single_daemon_process can
    # leave a transient socket file on disk during a kill+respawn cycle, so
    # the socket file alone is not a reliable readiness signal. Combine with
    # is_daemon_running (PID alive) to guarantee the daemon we spawned is
    # the one we see.
    #
    # The budget is wall-clock time from this hook's own start (Plan 00466
    # round 5, R4-1; review 8, R8-1), so it covers whatever ran before it. A
    # launcher that has finished leaves DAEMON_STARTUP_TIMEOUT for its
    # daemon to answer, and never more than the budget.
    local deadline=$_HOOKS_DAEMON_START_DEADLINE settle_end=""
    while ((SECONDS < deadline)); do
        if is_daemon_running && [[ -S "$SOCKET_PATH" ]]; then
            _hooks_daemon_end_launch
            return 0
        fi
        if _hooks_daemon_launch_settled; then
            break
        fi

        # Sleep 0.1 seconds (1 decisecond)
        sleep 0.1
    done

    # Final retry: the daemon may have bound the socket on the very tick
    # the loop's `elapsed < TIMEOUT` check went false. One more probe
    # before declaring failure closes the boundary race.
    if is_daemon_running && [[ -S "$SOCKET_PATH" ]]; then
        _hooks_daemon_end_launch
        return 0
    fi
    _hooks_daemon_end_launch

    # The launcher is still at work: the daemon is starting, and nothing is
    # known to be wrong. This hook answers now, before its timeout.
    if [[ "$launch_done" == false ]]; then
        _HOOKS_DAEMON_STARTING=true
        echo "HOOKS DAEMON: the daemon is still starting ${deadline}s into this hook, which answers now rather than run past its timeout" >&2
        return 1
    fi

    # Genuine timeout. NOTE: do NOT unlink PID_PATH — if the daemon is still
    # coming up, the PID slot belongs to it. is_daemon_running() cleans
    # stale PID files on next call when the process is actually dead.
    echo "ERROR: Daemon startup timeout (daemon not ready $(((DAEMON_STARTUP_TIMEOUT + 9) / 10)) seconds after its launcher finished)" >&2
    if [[ -n "$launch_output" ]]; then
        echo "Launcher output (may explain the failure):" >&2
        echo "$launch_output" >&2
    fi
    return 1
}

#
# _hooks_daemon_drain_launch() - Append what start_daemon's launcher has
# written since the last call to start_daemon's launch_output, without
# waiting for more, and set its launch_done at the launcher's end of file.
# A read that times out keeps the partial line it read.
_hooks_daemon_drain_launch() {
    local line rv
    while [[ "$launch_done" == false ]]; do
        rv=0
        IFS= read -r -t 0.01 -u "$launch_fd" line || rv=$?
        if ((rv == 0)); then
            launch_output+="$line"$'\n'
            continue
        fi
        launch_output+="$line"
        if ((rv > 128)); then
            return 0
        fi
        launch_done=true
    done
}

#
# _hooks_daemon_launch_settled() - True once start_daemon's launcher has
# finished and DAEMON_STARTUP_TIMEOUT has passed since, in which its daemon
# had to answer. Sets start_daemon's settle_end when it sees the finish.
_hooks_daemon_launch_settled() {
    _hooks_daemon_drain_launch
    if [[ "$launch_done" == false ]]; then
        return 1
    fi
    if [[ -z "$settle_end" ]]; then
        settle_end=$((SECONDS + (DAEMON_STARTUP_TIMEOUT + 9) / 10))
    fi
    ((SECONDS >= settle_end))
}

#
# _hooks_daemon_end_launch() - Take what start_daemon's launcher has written
# so far and close this hook's end of its output. A launcher still running
# is left to finish on its own.
_hooks_daemon_end_launch() {
    _hooks_daemon_drain_launch
    exec {launch_fd}<&-
}

#
# _is_ci_enforced() - Check if ci_enabled: true in daemon config
#
# Parses .claude/hooks-daemon.yaml for the ci_enabled flag under daemon section.
# Uses grep (universally available — no Python/yq dependency needed in CI).
#
# Returns:
#   0 if ci_enabled: true found (daemon is required)
#   1 otherwise (default: fail open)
#
_is_ci_enforced() {
    local config_file="$PROJECT_PATH/.claude/hooks-daemon.yaml"
    [[ -f "$config_file" ]] && grep -qE '^\s+ci_enabled:\s*true' "$config_file"
}

#
# _is_daemon_installed() - Check if daemon is installed (dir + venv Python present)
#
# Distinguishes "not installed" (fresh clone) from "installed but not running".
# Used by ensure_daemon() to set _HOOKS_DAEMON_NOT_INSTALLED for better error messages.
#
# Returns:
#   0 if daemon appears installed
#   1 if daemon directory or venv Python is absent
#
_is_daemon_installed() {
    [[ -d "$HOOKS_DAEMON_ROOT_DIR" ]] && [[ -f "$PYTHON_CMD" ]]
}

#
# _daemon_clone_present() - Is there a REAL clone under HOOKS_DAEMON_ROOT_DIR?
#
# NOT the same question as `[[ -d "$HOOKS_DAEMON_ROOT_DIR" ]]`: this directory
# always exists after init.sh has been sourced once, even on a genuinely fresh
# checkout, because the untracked-dir setup a little further down this file
# unconditionally does `mkdir -p "${HOOKS_DAEMON_ROOT_DIR}/untracked"` on
# every source. Bare directory presence therefore cannot tell a fresh
# checkout from a real clone with a missing venv (GitHub issue #53) — it is
# true in both.
#
# scripts/lib/resolve_venv.sh is a real signal: it ships with the clone (it
# is the canonical library `_resolve_python_cmd()` delegates to, and that
# function's own failure message already names this exact path), and nothing
# else creates it. Its presence means a clone was genuinely installed here,
# whatever state its venv is in.
#
# Returns:
#   0 if a real clone is present (its venv may still be missing/broken)
#   1 if this is a genuinely fresh checkout — no clone at all
#
_daemon_clone_present() {
    [[ -f "$HOOKS_DAEMON_ROOT_DIR/scripts/lib/resolve_venv.sh" ]]
}

#
# _daemon_orphan_venv_present() - Is there a leftover venv under untracked/?
#
# A second signal for the same question `_daemon_clone_present` answers, and
# needed for a case it cannot see (review finding on Plan 00454): a clone
# that has LOST `scripts/lib/resolve_venv.sh` — damaged, partially deleted,
# mid-reinstall — but still holds another environment's venv under
# `untracked/venv-*` (Plan 00099's fingerprint-keyed layout,
# `${HOOKS_DAEMON_ROOT_DIR}/untracked/venv-*/bin/python`). That venv is
# exactly what install/force's `rm -rf` would destroy, so its mere presence
# must block the NOT_INSTALLED fallback just as surely as a healthy clone
# does — regardless of whether `resolve_venv.sh` survived.
#
# Safe against the same false-positive `_daemon_clone_present`'s own docstring
# warns about: `mkdir -p "${HOOKS_DAEMON_ROOT_DIR}/untracked"` (a few hundred
# lines down) creates an EMPTY directory, so a genuinely fresh checkout never
# has a `venv-*` entry under it. The glob is nullglob-independent — a
# no-match keeps `venv_dir` as the literal pattern string, which
# `[[ -d ]]` then rejects like any other nonexistent path.
#
# Returns:
#   0 if at least one untracked/venv-* directory exists
#   1 if none does (fresh checkout, or a healthy clone with a resolved venv
#     already handled by _is_daemon_installed elsewhere)
#
_daemon_orphan_venv_present() {
    # canonical-resolver-exempt: this asks a different question from
    # resolve_venv_python() (scripts/lib/resolve_venv.sh) — PRESENCE of a
    # venv-* directory at all, not whether it resolves to a WORKING
    # interpreter. A venv-* directory that resolve_venv_python() would
    # reject (missing bin/python, mid-write, otherwise broken) still holds
    # bytes that install/force's rm -rf would destroy, so delegating to the
    # resolver here would under-detect exactly the case this function exists
    # to catch.
    local venv_dir
    for venv_dir in "$HOOKS_DAEMON_ROOT_DIR"/untracked/venv-*; do
        [[ -d "$venv_dir" ]] && return 0
    done
    return 1
}

#
# _clone_version() - Version of the INSTALLED (gitignored) daemon clone
#
# Read with grep from version.py rather than by importing the package: the
# clone that failed to start is often the one whose venv or interpreter is the
# problem, so anything requiring a working Python would be unavailable at
# exactly the moment this answer is needed.
#
# Output:
#   The version on stdout, nothing when it cannot be determined
#
# Returns:
#   0 if a version was read, 1 otherwise (a normal state, not an error)
#
_clone_version() {
    local version_file="$HOOKS_DAEMON_ROOT_DIR/src/claude_code_hooks_daemon/version.py"
    [[ -f "$version_file" ]] || return 1

    local line
    line="$(grep -m1 -oE '^__version__ = "[0-9]+\.[0-9]+\.[0-9]+"' "$version_file")" || return 1

    local version="${line#*\"}"
    printf '%s' "${version%\"}"
}

#
# _venv_self_heal() - Have the clone build this path's missing venv (Plan 00456).
#
# Called ONLY from ensure_daemon's VENV_MISSING diagnosis, for a real clone
# with a readable version, so the healthy path never pays for it. The work is
# the clone's own scripts/venv_bootstrap.sh `hook`: this file is a per-project
# COPY and the clone is what builds. That driver checks the five
# can_inline_bootstrap preconditions without a venv. When they hold, it starts
# one DETACHED build under the venv build lock and returns at once: hooks time
# out at 60s, and a uv sync can take longer. When they do not hold, it
# changes nothing and names each failed condition with its fix.
#
# A clone too old to ship the driver leaves the state empty, and the Plan
# 00454 message stands. Sets the _HOOKS_DAEMON_BOOTSTRAP_* globals. Returns 0.
#
_venv_self_heal() {
    local driver="$HOOKS_DAEMON_ROOT_DIR/scripts/venv_bootstrap.sh"
    [[ -f "$driver" ]] || return 0

    # stdout is the driver's key=value protocol; its stderr is left on this
    # hook's stderr, where Claude Code's debug log keeps it.
    local output rc=0
    output="$(bash "$driver" hook "$HOOKS_DAEMON_ROOT_DIR")" || rc=$?
    if [[ "$rc" -ne 0 ]]; then
        _HOOKS_DAEMON_BOOTSTRAP_STATE="error"
        _HOOKS_DAEMON_BOOTSTRAP_DETAIL="$driver exited $rc (its output is on the hook's stderr)"
        return 0
    fi

    local key value
    while IFS='=' read -r key value; do
        case "$key" in
            state) _HOOKS_DAEMON_BOOTSTRAP_STATE="$value" ;;
            log) _HOOKS_DAEMON_BOOTSTRAP_LOG="$value" ;;
            missing)
                _HOOKS_DAEMON_BOOTSTRAP_MISSING="${_HOOKS_DAEMON_BOOTSTRAP_MISSING:+$_HOOKS_DAEMON_BOOTSTRAP_MISSING, }$value"
                ;;
            fix) _HOOKS_DAEMON_BOOTSTRAP_FIXES="$_HOOKS_DAEMON_BOOTSTRAP_FIXES  - $value"$'\n' ;;
            detail) _HOOKS_DAEMON_BOOTSTRAP_DETAIL="$value" ;;
            pid) _HOOKS_DAEMON_BOOTSTRAP_PID="$value" ;;
            elapsed) _HOOKS_DAEMON_BOOTSTRAP_ELAPSED="$value" ;;
        esac
    done <<< "$output"
    return 0
}

#
# _tracked_deployed_version() - Version the project's TRACKED assets came from
#
# .claude/HOOKS-DAEMON.md is a tracked deployed asset regenerated by
# generate-docs on every upgrade, and it carries the only machine-readable
# record of which version deployed the rest. The header shape is the one
# docs_generator._render_header() emits.
#
# THIS PATTERN IS DUPLICATED IN PYTHON, and cannot be shared: this function must
# work when the package cannot be imported at all. The canonical parser is
# utils/deployed_version.py::VERSION_MARKER_RE, and
# tests/integration/test_init_sh_stale_clone_version.py feeds one header line to
# both and asserts they agree — that test is the only thing holding the two
# together, so do not change either pattern without it.
#
# Output:
#   The version on stdout, nothing when no marker is present
#
# Returns:
#   0 if a version was read, 1 otherwise (a project that never generated the
#   doc, or whose doc predates the header, is NORMAL — not broken)
#
_tracked_deployed_version() {
    local doc="$PROJECT_PATH/.claude/HOOKS-DAEMON.md"
    [[ -f "$doc" ]] || return 1

    local line
    line="$(grep -m1 -oE '> Generated on [0-9]{4}-[0-9]{2}-[0-9]{2} \(v[0-9]+\.[0-9]+\.[0-9]+\) by' "$doc")" || return 1

    local version="${line##*\(v}"
    printf '%s' "${version%%\)*}"
}

#
# _config_daemon_key_raw() - The raw value of a key in the daemon: config block
#
# Plan 00477. A line-oriented read of .claude/hooks-daemon.yaml, because this
# runs before any venv exists. It accepts exactly the shape the installer
# writes: an indented KEY inside the top-level daemon block. A commented-out
# key, or one in another block, is not the key. \042 and \047 are the double
# and single quote, spelled as octal escapes so the awk program needs no quote
# characters of its own.
#
# Args:
#   $1 - the key name (a plain identifier)
#
# Output:
#   The value with quotes and a trailing comment removed (possibly empty)
#
# Returns:
#   0 if the key is present, 1 if it is not (or there is no config file)
#
_config_daemon_key_raw() {
    local config="$PROJECT_PATH/.claude/hooks-daemon.yaml"
    [[ -f "$config" ]] || return 1

    # A bash-builtin read and a substring test first: a key the file never
    # mentions (the usual case for an optional one) costs no process at all.
    local content=""
    IFS= read -r -d '' content < "$config" || [[ -n "$content" ]] || return 1
    [[ "$content" == *"$1:"* ]] || return 1

    local value
    value="$(awk -v key="$1" '
        /^daemon:/ { blk = 1; indent = ""; next }
        /^[^ \t#]/ { blk = 0 }
        blk && indent == "" && /^[ \t]+[^ \t#]/ {
            indent = $0
            sub(/[^ \t].*$/, "", indent)
        }
        blk && indent != "" && index($0, indent key ":") == 1 {
            v = substr($0, length(indent key ":") + 1)
            sub(/^[ \t]+/, "", v)
            sub(/[ \t]+#.*$/, "", v)
            sub(/[ \t]+$/, "", v)
            gsub(/^[\042\047]|[\042\047]$/, "", v)
            print "FOUND:" v
            exit
        }
    ' "$config")"
    [[ "$value" == FOUND:* ]] || return 1

    printf '%s' "${value#FOUND:}"
}

# _config_expected_version_raw() - The raw daemon.expected_version value
_config_expected_version_raw() {
    _config_daemon_key_raw expected_version
}

#
# _config_unprovisioned_mode() - daemon.unprovisioned_mode, warn when unset
#
# Plan 00477. Whether an unprovisioned checkout BLOCKS tool calls is a
# per-project setting; the default, and what any unusable value falls back to,
# is warn. A value that is present but neither warn nor block is named in
# _HOOKS_DAEMON_UNPROVISIONED_MODE_NOTE so the message says so instead of
# quietly choosing.
#
# Sets:
#   _HOOKS_DAEMON_UNPROVISIONED_MODE       warn | block
#   _HOOKS_DAEMON_UNPROVISIONED_MODE_NOTE  empty, or a sentence naming the bad value
#
_config_unprovisioned_mode() {
    _HOOKS_DAEMON_UNPROVISIONED_MODE="warn"
    _HOOKS_DAEMON_UNPROVISIONED_MODE_NOTE=""

    local raw
    if ! raw="$(_config_daemon_key_raw unprovisioned_mode)"; then
        return 0
    fi
    case "$raw" in
        warn | block) _HOOKS_DAEMON_UNPROVISIONED_MODE="$raw" ;;
        *)
            _HOOKS_DAEMON_UNPROVISIONED_MODE_NOTE="daemon.unprovisioned_mode '$raw' is not warn or block; treated as warn."
            ;;
    esac
}

#
# _detect_needs_provision() - Is this a fresh clone that provision can build?
#
# Plan 00477. Called only once NOT_INSTALLED is established (no daemon running,
# no clone, no leftover venv), so the question here is whether the tracked
# assets a provision needs are present: the project's config and its
# .claude/provision.sh. A project that predates provision.sh keeps the generic
# answer, since naming a script it lacks would be a wrong instruction. The
# daemon's own repository never needs provisioning: it is set up by
# scripts/bootstrap-self-install.sh.
#
# Sets _HOOKS_DAEMON_NEEDS_PROVISION and, when true, the expected version
# (_resolve_expected_version), the mode and the status-line text.
#
_detect_needs_provision() {
    [[ -f "$PROJECT_PATH/.claude/hooks-daemon.yaml" ]] || return 0
    [[ -f "$PROJECT_PATH/.claude/provision.sh" ]] || return 0
    [[ -f "$PROJECT_PATH/src/claude_code_hooks_daemon/version.py" ]] && return 0

    _HOOKS_DAEMON_NEEDS_PROVISION=true
    if ! _resolve_expected_version; then
        : # unknown is a reported state, carried in _HOOKS_DAEMON_EXPECTED_VERSION*
    fi
    _config_unprovisioned_mode
    _HOOKS_DAEMON_STATUS_DOWN_TEXT="⚠️ HOOKS DAEMON NOT PROVISIONED - run: bash .claude/provision.sh"
    return 0
}

#
# _resolve_expected_version() - Which daemon version does this project expect?
#
# Plan 00477. The ONE resolver for the question, in bash, because it is asked
# before any venv exists (provision.sh, and the unprovisioned diagnosis of the
# hooks). Order: the daemon.expected_version config key, then the tracked
# HOOKS-DAEMON.md header for a project that predates the key. It reports
# unknown rather than guessing, and a key that is PRESENT but not X.Y.Z is
# reported as invalid rather than overridden by the header: the project said
# something deliberate, and quietly preferring another answer would install a
# version nobody asked for.
#
# Sets:
#   _HOOKS_DAEMON_EXPECTED_VERSION         X.Y.Z, or unknown
#   _HOOKS_DAEMON_EXPECTED_VERSION_SOURCE  config | tracked-doc | config-invalid | empty
#
# Returns:
#   0 if a version was resolved, 1 otherwise
#
_resolve_expected_version() {
    _HOOKS_DAEMON_EXPECTED_VERSION="unknown"
    _HOOKS_DAEMON_EXPECTED_VERSION_SOURCE=""

    local raw
    if raw="$(_config_expected_version_raw)"; then
        if [[ "$raw" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            _HOOKS_DAEMON_EXPECTED_VERSION="$raw"
            _HOOKS_DAEMON_EXPECTED_VERSION_SOURCE="config"
            return 0
        fi
        _HOOKS_DAEMON_EXPECTED_VERSION_SOURCE="config-invalid"
        return 1
    fi

    local tracked
    if tracked="$(_tracked_deployed_version)"; then
        _HOOKS_DAEMON_EXPECTED_VERSION="$tracked"
        _HOOKS_DAEMON_EXPECTED_VERSION_SOURCE="tracked-doc"
        return 0
    fi
    return 1
}

#
# _version_lt() - True when $1 sorts strictly before $2
#
# Pure bash rather than `sort -V`: this runs on every hook of a broken install,
# and the field-numeric comparison has no portability question to answer. Both
# arguments are X.Y.Z by construction — the extraction patterns above accept
# nothing else — so the fields are always numeric.
#
_version_lt() {
    [[ "$1" == "$2" ]] && return 1

    local -a left right
    IFS=. read -r -a left <<< "$1"
    IFS=. read -r -a right <<< "$2"

    local i
    for i in 0 1 2; do
        local l="${left[i]:-0}" r="${right[i]:-0}"
        if (( l < r )); then return 0; fi
        if (( l > r )); then return 1; fi
    done
    return 1
}

#
# _detect_stale_clone() - Is the installed clone not the version the project expects?
#
# Plan 00386 compared the clone with the tracked HOOKS-DAEMON.md header. Plan
# 00477 Task 5.1 compares it with the RESOLVED expected version instead: the
# daemon.expected_version config key, then the header for a project that
# predates the key (_resolve_expected_version). The case is a pull that changes
# the key: the gitignored clone stays where it was, so it and the tracked
# assets now disagree. An invalid key reports nothing of its own here; the
# comparison falls back to the header exactly as before the key existed.
#
# The daemon's own repository (self-install: the daemon root IS the project)
# never reports drift: its source is the project, there is no clone to fall
# behind. Reads are builtins and one grep for the clone's version.py; the
# config is read without awk unless the key is actually present.
#
# Sets _HOOKS_DAEMON_CLONE_VERSION, _HOOKS_DAEMON_TRACKED_VERSION (the EXPECTED
# version, whichever source named it) and _HOOKS_DAEMON_EXPECTED_VERSION_SOURCE
# (config | tracked-doc) on a mismatch. Says nothing when either version is
# unreadable: unknowable is not the same as wrong, and accusing a project on
# absent evidence is how an advisory earns the habit of being ignored.
#
# Returns:
#   0 if the two versions differ, 1 otherwise
#
_detect_stale_clone() {
    [[ "$HOOKS_DAEMON_ROOT_DIR" == "$PROJECT_PATH" ]] && return 1

    local clone expected source
    clone="$(_clone_version)" || return 1
    if _resolve_expected_version; then
        expected="$_HOOKS_DAEMON_EXPECTED_VERSION"
        source="$_HOOKS_DAEMON_EXPECTED_VERSION_SOURCE"
    else
        expected="$(_tracked_deployed_version)" || return 1
        source="tracked-doc"
    fi
    [[ "$clone" == "$expected" ]] && return 1

    _HOOKS_DAEMON_CLONE_VERSION="$clone"
    _HOOKS_DAEMON_TRACKED_VERSION="$expected"
    _HOOKS_DAEMON_EXPECTED_VERSION_SOURCE="$source"
    return 0
}

#
# _is_ci_environment() - Detect if running in any CI/CD environment
#
# Checks common CI environment variables across major platforms.
# Used to determine whether to enter passthrough mode on daemon failure.
#
# Returns:
#   0 if running in CI (passthrough allowed)
#   1 if not CI (fail with error)
#
_is_ci_environment() {
    # Standard flag — GitHub Actions, GitLab CI, CircleCI, Travis, Bitbucket, Buildkite...
    [[ -n "${CI:-}" ]] && return 0
    # GitHub Actions (belt-and-suspenders)
    [[ -n "${GITHUB_ACTIONS:-}" ]] && return 0
    # GitLab CI (belt-and-suspenders)
    [[ -n "${GITLAB_CI:-}" ]] && return 0
    # Jenkins (does not set CI)
    [[ -n "${JENKINS_URL:-}" ]] && return 0
    # Azure DevOps (does not set CI)
    [[ -n "${TF_BUILD:-}" ]] && return 0
    return 1
}

#
# _passthrough_flag_path() - Get path to passthrough state file
#
# State file prevents repeated config parsing and noise on every hook call.
# Created on first daemon failure when ci_enabled is NOT true.
# Cleaned up when daemon starts successfully (recovery).
#
_passthrough_flag_path() {
    local passthrough_dir="$HOOKS_DAEMON_ROOT_DIR/untracked"
    if [[ ! -d "$passthrough_dir" ]]; then
        if ! mkdir -p "$passthrough_dir" 2>/dev/null; then
            echo "HOOKS DAEMON: Could not create passthrough state directory: $passthrough_dir" >&2
        fi
    fi
    echo "$passthrough_dir/.hooks-passthrough"
}

#
# _enter_passthrough_mode() - Override send_request_stdin to return empty JSON
#
# When daemon is unavailable and ci_enabled is not set, hook events
# silently pass through with no blocking and no context injection.
#
_enter_passthrough_mode() {
    # shellcheck disable=SC2317
    send_request_stdin() {
        cat > /dev/null
        # Status line ($2 == "status"): render the fallback text, not a raw JSON
        # blob (Plan 00156 review finding 2). Other events: silent {} passthrough.
        if [[ "${2:-}" == "status" ]]; then
            echo '⚠️ NO STATUS DATA'
        else
            echo '{}'
        fi
    }
    export -f send_request_stdin
}

#
# _hooks_daemon_is_down() - True when is_daemon_running answers down (1):
# neither running (0) nor unknown (2).
_hooks_daemon_is_down() {
    local running=0
    is_daemon_running || running=$?
    ((running == 1))
}

#
# ensure_daemon() - Start daemon if not running (lazy startup)
#
# Idempotent function safe to call on every hook invocation.
# Only starts daemon if not already running.
#
# When daemon cannot start:
#   - If ci_enabled: true in config: hard fail (return 1), forwarder blocks
#   - If CI environment detected: passthrough mode (daemon not installed in pipeline)
#   - Otherwise (non-CI dev environment): fail with error (return 1), forwarder
#     calls emit_hook_error so agent sees "Not currently running" and can restart
#
# Returns:
#   0 if daemon is running or CI passthrough mode active
#   1 if daemon failed and must report error to agent
#
ensure_daemon() {
    # Plan 00466 N126 round 2 (F1): a relay hand-off goes straight to
    # send_request_stdin, whose judged deny is the only answer it may get.
    # A start could spend the whole hand-off budget, and every diagnosis
    # below it (passthrough, not installed, venv missing, version mismatch)
    # would answer the call the relay has already failed with an allow.
    if [[ -n "$_HOOKS_DAEMON_RELAY_FAILED" ]]; then
        return 0
    fi

    local running=0
    is_daemon_running || running=$?
    if ((running == 0)); then
        # Daemon running — clean up stale CI passthrough flag if present
        local passthrough_flag
        passthrough_flag=$(_passthrough_flag_path)
        rm -f "$passthrough_flag" 2>/dev/null
        return 0
    fi

    local passthrough_flag
    passthrough_flag=$(_passthrough_flag_path)

    # CI optimisation: skip start attempt if passthrough flag exists
    # (daemon not installed in CI — no point trying repeatedly). Only for a
    # daemon that is down (1): an unknown answer (2) must not skip a start
    # (Plan 00466 round 6, R5-3).
    if ((running == 1)) && _is_ci_environment && [[ -f "$passthrough_flag" ]] &&
        ! _is_ci_enforced; then
        _enter_passthrough_mode
        return 0
    fi

    # Try to start daemon
    if start_daemon; then
        rm -f "$passthrough_flag" 2>/dev/null
        return 0
    fi

    # ci_enabled: true — hard fail regardless of environment, and whether the
    # start failed or is still under way (review 9, DR-2): CI enforcement
    # exempts no recovery command, and a slow start must not open one.
    if _is_ci_enforced; then
        _HOOKS_DAEMON_CI_ENFORCED=true
        return 1
    fi

    # Still starting when this hook had to answer: no diagnosis below
    # applies, and the answer is "retry" (Plan 00466 review 8, R8-1). In CI
    # this skips the passthrough, so a start that keeps hanging denies every
    # call (review 9, DR-3): fail closed.
    if [[ "${_HOOKS_DAEMON_STARTING:-false}" == "true" ]]; then
        return 1
    fi

    # CI environment (but not enforced): passthrough mode — daemon simply not
    # installed. Only once it is down after the failed start: a pid still
    # unknown (2) may be a daemon this start could not prove, and allowing
    # every call on it would be failing open (round 6, R5-3).
    if _is_ci_environment && _hooks_daemon_is_down; then
        echo "HOOKS DAEMON: Daemon unavailable in CI environment — passthrough mode active (handlers inactive)" >&2
        echo "HOOKS DAEMON: All operations will proceed without safety checks" >&2
        if ! touch "$passthrough_flag" 2>/dev/null; then
            echo "HOOKS DAEMON: Could not write passthrough state file (noise will repeat)" >&2
        fi

        # First call: return one-time advisory context so agent sees the warning once
        # Plan 00156: jq-free. The event name arrives as $1 (the wrapper passes
        # it); the hook_input payload on stdin is drained (this advisory is a
        # fixed template with no user-derived content).
        # shellcheck disable=SC2317
        send_request_stdin() {
            local event_name="${1:-Unknown}"
            cat > /dev/null
            # Status line ($2 == "status"): render the fallback text, not a raw
            # JSON blob (Plan 00156 review finding 2).
            if [[ "${2:-}" == "status" ]]; then
                echo '⚠️ NO STATUS DATA'
                return 0
            fi
            printf '{"hookSpecificOutput": {"hookEventName": "%s", "additionalContext": "%s"}}\n' \
                "$event_name" \
                "HOOKS DAEMON: Not installed in CI environment. Safety handlers are INACTIVE. All operations allowed without validation. This warning appears once."
        }
        export -f send_request_stdin
        return 0
    fi

    # Non-CI environment: fail with error so agent sees it and can act.
    # Four diagnoses, MOST SPECIFIC FIRST (Plan 00386, Plan 00454).
    #
    # The version mismatch is tested before _is_daemon_installed deliberately.
    # That check requires a RESOLVED venv interpreter, and a clone stale enough
    # to fail startup often cannot resolve one — so a genuine version mismatch
    # would otherwise be reported as "not installed", which says nothing about
    # the versions and sends the reader to install rather than upgrade. Reversing
    # the order costs nothing: _detect_stale_clone reads the clone's own
    # version.py, so it CANNOT fire unless a clone is really present on disk.
    #
    # VENV_MISSING sits between the two: something real is present under
    # HOOKS_DAEMON_ROOT_DIR (so this is NOT a fresh checkout) but
    # _is_daemon_installed still failed, meaning no venv interpreter resolved
    # for this project path. "Something real" is TWO discriminators, not one
    # — neither is bare directory presence, and NEITHER is whether the
    # clone's version is readable:
    #   - _daemon_clone_present: a real clone (scripts/lib/resolve_venv.sh)
    #     is here, whatever state its venv is in. When true, _clone_version
    #     is trusted enough to name a version-pinned upgrade.
    #   - _daemon_orphan_venv_present: EVEN WITHOUT a real clone marker, a
    #     leftover untracked/venv-* directory is exactly what install/force's
    #     rm -rf would destroy — reviewed in Plan 00454 and missed by the
    #     first version of this fix, which read a clone that had lost
    #     resolve_venv.sh but still held another view's venv as NOT_INSTALLED
    #     and recommended install anyway. When ONLY this signal fires, the
    #     clone is not trusted enough to read _clone_version from — a
    #     partially damaged clone can have some files intact and others gone,
    #     and guessing which half to trust is the mistake this branch exists
    #     to avoid — so the version stays empty and the message says the
    #     clone looks damaged rather than naming an upgrade target.
    #
    # Plan 00456: only the trusted case — a real clone whose version reads —
    # tries to heal itself (_venv_self_heal). A damaged clone or a lone
    # leftover venv never gets an automatic build, for the same reason it
    # never gets a version-pinned upgrade.
    if _detect_stale_clone; then
        _HOOKS_DAEMON_VERSION_MISMATCH=true
    elif ! _is_daemon_installed; then
        if _daemon_clone_present; then
            _HOOKS_DAEMON_VENV_MISSING=true
            if ! _HOOKS_DAEMON_VENV_MISSING_VERSION="$(_clone_version)"; then
                _HOOKS_DAEMON_VENV_MISSING_VERSION=""
            fi
            if [[ -n "$_HOOKS_DAEMON_VENV_MISSING_VERSION" ]]; then
                _venv_self_heal
            fi
        elif _daemon_orphan_venv_present; then
            _HOOKS_DAEMON_VENV_MISSING=true
            _HOOKS_DAEMON_VENV_MISSING_VERSION=""
        else
            _HOOKS_DAEMON_NOT_INSTALLED=true
            _detect_needs_provision
        fi
    fi
    return 1
}

#
# send_request_stdin() - Send JSON from stdin to daemon via Unix socket
#
# CRITICAL: Reads JSON from stdin and sends directly to daemon.
# NEVER pass JSON through shell variables - control characters break.
#
# On error, outputs valid JSON hook response to stdout so the agent can
# see the error and take corrective action. This is CRITICAL for safety.
#
# Returns:
#   0 always (errors output JSON to stdout, not exit codes)
#
# Usage:
#   cat input.json | send_request_stdin
#   echo '{"key":"value"}' | send_request_stdin
#
send_request_stdin() {
    # Plan 00156 (T2): jq-free transport. The event name arrives as $1; the raw
    # hook_input payload arrives on stdin. This inline python3 — already spawned
    # as the transport — wraps it into {"event": $1, "hook_input": <stdin>}
    # itself, eliminating the per-event jq spawn. $2 optionally selects a
    # response mode: "status" extracts .text/.error for the status line.
    #
    # CRITICAL: the payload passes via stdin (never argv) so control characters
    # are preserved; only the hardcoded event-name literal moves to argv.
    # CRITICAL: On error, outputs valid JSON to stdout (not stderr) so agent sees it.
    # Only uses stdlib: socket, sys, json (no venv packages needed).
    local event_name="${1:-Unknown}"
    local response_mode="${2:-}"
    local _recovery_py
    _hooks_daemon_recovery_py _recovery_py
    # Plan 00290 (T4.1/T4.2, DESIGN-socket-relay.md §6.2): $3, when the
    # forwarder_generator inserted it (nc_enabled at deploy time), names this
    # event's per-event socket filename (its bash_key, e.g. "pre-tool-use") —
    # a literal baked in at generation time so no PascalCase->kebab mapping is
    # needed here. Absent for every deployed forwarder by default (byte-identical).
    local event_sock_name="${3:-}"
    # Plan 00295 Task 2.5: $4, when forwarder_generator's append_nc_socket_arg
    # inserted it, is the events dir resolved (and AF_UNIX-overflow-fallback
    # applied) AT GENERATION TIME — the same decision build_relay_guard_block
    # makes for its own `_rl_events_dir`. Empty for every forwarder whose
    # natural `$_untracked_dir/events$_hostname_suffix` path never overflows
    # (the common case): the dynamic default below is computed exactly as
    # before. HOOKS_DAEMON_EVENTS_DIR, checked first, always outranks this
    # baked value — an operator's own override always wins.
    local events_dir_override="${4:-}"

    # nc rung (rung 2): only for the plain passthrough shape (no response_mode
    # translation needed — "status"/"worktree" always go through the python3
    # rung's render_status/print_worktree, so that logic is never duplicated
    # here). The payload is buffered to a TEMP FILE, never a shell variable —
    # the same control-character-safety rule the python3 rung follows (see
    # the CRITICAL comment above). A failed/empty nc capture REPLAYS the
    # buffered payload into the python3 rung below via its stdin redirect
    # (design §5: an empty capture means no verdict was ever delivered, so
    # replay is always safe).
    local _nc_replay_payload=""
    # A relay hand-off (Plan 00466 N126) already knows the daemon did not
    # answer, so it goes straight to the python3 rung's judged deny.
    if [[ -z "$_HOOKS_DAEMON_RELAY_FAILED" && -z "$response_mode" && -n "$event_sock_name" ]] \
        && [[ "${HOOKS_DAEMON_NC_UNIX_CAPABLE:-0}" == "1" ]] \
        && command -v nc > /dev/null; then
        local _nc_events_dir="${HOOKS_DAEMON_EVENTS_DIR:-${events_dir_override:-$_untracked_dir/events${_hostname_suffix}}}"
        local _nc_sock="$_nc_events_dir/${event_sock_name}.sock"
        if [[ -S "$_nc_sock" ]]; then
            local _nc_payload _nc_response _nc_stderr _nc_rc
            _nc_payload="$(mktemp)"
            _nc_response="$(mktemp)"
            _nc_stderr="$(mktemp)"
            cat > "$_nc_payload"
            _nc_rc=0
            # -N: shut down the socket's write half once stdin hits EOF.
            # Without it, OpenBSD nc keeps the connection open after the
            # payload is fully sent, the daemon's EOF-framed per-event
            # socket (DESIGN-socket-relay.md §2) never sees the half-close,
            # never responds, and nc sits until -w's timeout elapses —
            # observed as a ~30s hang on every nc-rung call (Plan 00290
            # Phase 6 measurement). With -N, -w's "final net reads" role
            # becomes the correct overall response-wait budget.
            nc -U -N -w "${CLAUDE_HOOKS_SOCKET_TIMEOUT:-30}" "$_nc_sock" \
                < "$_nc_payload" > "$_nc_response" 2> "$_nc_stderr" || _nc_rc=$?
            if [[ "$_nc_rc" -eq 0 && -s "$_nc_response" ]]; then
                cat "$_nc_response"
                rm -f "$_nc_payload" "$_nc_response" "$_nc_stderr"
                return 0
            fi
            # Empty/short/failed capture: NOT silently dropped — surfaced on
            # stderr for debug capture, then rung 2 degrades to rung 3 by
            # keeping the buffered payload for the python3 stdin redirect
            # below (this process's own stdin was already drained by the
            # `cat > "$_nc_payload"` above). Design §5: an empty nc capture
            # means no verdict was delivered, so the replay is always safe.
            if [[ -s "$_nc_stderr" ]]; then
                echo "HOOKS DAEMON: nc rung failed (rc=$_nc_rc), falling back to python3 transport:" >&2
                cat "$_nc_stderr" >&2
            fi
            rm -f "$_nc_response" "$_nc_stderr"
            _nc_replay_payload="$_nc_payload"
        fi
    fi

    # stdin for the python3 transport: the nc replay file when rung 2 buffered
    # the payload and failed, else THIS process's own stdin duplicated by fd
    # (dup2 semantics — valid for ANY fd type). NEVER a /dev/stdin re-open:
    # Claude Code hands hooks a SOCKET as stdin, and open() on a socket fails
    # with ENXIO ("No such device or address") — while every pipe-fed test
    # invocation works, which is exactly how this shipped. Field-observed as a
    # non-blocking error on every real hook event.
    if [[ -n "$_nc_replay_payload" ]]; then
        exec 3<"$_nc_replay_payload"
    else
        exec 3<&0
    fi

    # python3's answer is captured, with its exit status appended after a
    # final x, so exactly one document reaches stdout (Plan 00466 round 4):
    # a python3 that answered and then failed must not have the fixed deny
    # below printed after its answer. The suffix keeps the answer's bytes,
    # trailing newlines included, which a bare $( ) would drop.
    local _rv=0 _hd_answer
    _hd_answer="$(python3 -c "
import json
import os
import socket
import sys

event_name = sys.argv[1] if len(sys.argv) > 1 else 'Unknown'
response_mode = sys.argv[2] if len(sys.argv) > 2 else ''

# Socket budget for the whole connect+send+recv exchange. Default 30s; operators
# can raise it via CLAUDE_HOOKS_SOCKET_TIMEOUT (also lets tests drive the timeout
# path fast). A non-numeric or non-positive value falls back to the default.
# Defined up-front (not inside the try) so emit_error_json can name it even when
# a failure fires before the socket is opened (Plan 00177).
def _resolve_socket_timeout():
    raw = os.environ.get('CLAUDE_HOOKS_SOCKET_TIMEOUT', '').strip()
    if not raw:
        return 30.0
    try:
        value = float(raw)
    except ValueError:
        return 30.0
    return value if value > 0 else 30.0

SOCKET_TIMEOUT_SECONDS = _resolve_socket_timeout()

# Timeout.CHAIN_DEADLINE_DEFAULT, which this stdlib-only client cannot import;
# test_init_sh_pretooluse_fail_closed.py pins the two equal.
_CHAIN_DEADLINE_DEFAULT_SECONDS = 20

def _socket_timeout_note():
    '''Name CLAUDE_HOOKS_SOCKET_TIMEOUT when it caused a timeout (Plan 00466 N24).

    Set below the daemon's chain deadline, it makes this client give up
    before the daemon can answer, so every slow chain is denied. The deny is
    right; an unexplained one is not.'''
    raw = os.environ.get('CLAUDE_HOOKS_SOCKET_TIMEOUT', '').strip()
    if not raw or SOCKET_TIMEOUT_SECONDS > _CHAIN_DEADLINE_DEFAULT_SECONDS:
        return ''
    return (f'CLAUDE_HOOKS_SOCKET_TIMEOUT={raw} makes this client wait only '
            f'{SOCKET_TIMEOUT_SECONDS:g}s, shorter than the daemon\'s chain deadline '
            f'(daemon.chain.deadline_seconds, {_CHAIN_DEADLINE_DEFAULT_SECONDS}s by '
            'default): the client gives up before the daemon can answer. '
            'Unset it, or raise it above the deadline.')

# Plan 00466 n24 security review: filled in once the raw hook_input is
# parsed, below. Stays None for a failure that fires before parsing (e.g.
# invalid_hook_input) -- every reader of this name tolerates that.
hook_input = None

# Plan 00466 N69: set once connect() succeeds. It alone decides whether a
# failure message may say the daemon was reached -- an error_type does not,
# because an unclassified exception can fire on either side of connect().
daemon_reached = False

# The daemon recovery commands a PreToolUse deny must never block, so a
# wedged/slow daemon (the B2 GIL-hang shape: alive but not answering) can
# still be recovered from inside the same session. Defines
# _is_daemon_recovery_command and _recovery_command; see
# _hooks_daemon_recovery_py.
$_recovery_py

project_path = sys.argv[3] if len(sys.argv) > 3 else ''
daemon_root = sys.argv[5] if len(sys.argv) > 5 else ''

# The recovery advice every daemon-side failure below ends with.
_RESTART_ADVICE = _recovery_advice(project_path, daemon_root) + [
    'Then use the hooks-daemon skill to verify health (args=health).',
    'If this recurs, use the hooks-daemon skill to check logs',
    '(args=logs) and report it.',
]

# Plan 00466 N126: what hooks-relay reported when it handed this call over
# after its own exchange with the daemon failed; empty otherwise.
relay_failure = sys.argv[4] if len(sys.argv) > 4 else ''

def _pretooluse_response_looks_valid(text):
    '''True when text parses as one of PreToolUse's two legitimate response
    shapes: {} (a real ALLOW with nothing to say -- HookResult.to_json's
    documented empty-response case) or a dict with a hookSpecificOutput key
    whose permissionDecision, if present, is one of the four known values.
    A response missing entirely, not valid JSON, or shaped as neither of
    these is NOT a judged verdict -- the daemon-side handler chain may
    have crashed or hung partway through serialising it.'''
    if not text:
        return False
    try:
        data = json.loads(text)
    except Exception:
        return False
    if not isinstance(data, dict):
        return False
    if data == {}:
        return True
    hso = data.get('hookSpecificOutput')
    if not isinstance(hso, dict):
        return False
    decision = hso.get('permissionDecision')
    return decision is None or decision in ('allow', 'deny', 'ask', 'defer')

def emit_error_json(event_name, error_type, error_details):
    '''Output valid hook error response to stdout.

    Inlines JSON generation using only stdlib (no venv dependency).
    Handles event-specific formatting: Stop/SubagentStop vs other events.
    '''
    print(f'HOOKS DAEMON ERROR [{error_type}]: {error_details}', file=sys.stderr)
    timeout_note = _socket_timeout_note() if error_type == 'socket_timeout' else ''
    if timeout_note:
        print(f'HOOKS DAEMON: {timeout_note}', file=sys.stderr)

    if error_type == 'invalid_hook_input':
        # A malformed payload never reached the socket, so the daemon state is
        # unknown and almost certainly fine. Do NOT frame this as daemon-down or
        # tell the agent to restart (Plan 00156 review finding 1) — a restart
        # 'fixes' nothing. This is a caller/Claude-Code payload problem.
        context_lines = [
            'HOOKS DAEMON: Received a malformed hook payload',
            '',
            f'Error: {error_type} - {error_details}',
            '',
            'The hook input could not be parsed (not UTF-8 JSON, or nested too',
            'deeply to decode), so no handler validated it.',
            'The daemon itself is likely healthy — do NOT restart it.',
            'If this recurs, capture the exact hook input and report it.',
        ]
    elif error_type == 'socket_timeout':
        # A read-side timeout: connect()+sendall() SUCCEEDED, so the daemon was
        # reached and is ALIVE — a handler merely ran past the budget. Framing
        # this as 'daemon not running' and advising a restart is wrong and
        # actively harmful (a restart fixes nothing). Almost always the session
        # transcript has grown very large (Plan 00177).
        context_lines = [
            f'HOOKS DAEMON: A hook handler exceeded the {SOCKET_TIMEOUT_SECONDS:g}s budget',
            '',
            f'Error: {error_type} - {error_details}',
            '',
            'The daemon was REACHED and is ALIVE (the connection succeeded); a',
            'hook handler simply ran past the deadline. This is NOT a dead daemon',
            '— do NOT restart it, a restart fixes nothing here.',
            '',
            'This usually means the session transcript has grown very large.',
            'Run /compact or start a new session to restore fast hooks.',
            '',
            'If this is a PreToolUse call being denied for safety because of',
            'this timeout: a genuinely wedged daemon (not just a slow handler)',
            'is fixed by restarting it.',
        ] + _recovery_advice(project_path, daemon_root)
        if timeout_note:
            context_lines[1:1] = ['', timeout_note]
    elif error_type == 'connect_backlog_full':
        # Plan 00466 N24 review 3 MA1: connect() itself raised EAGAIN/EWOULDBLOCK
        # instead of blocking until the socket timeout, which on a UNIX stream
        # socket means the kernel's accept backlog is already full -- the
        # daemon process exists and is listening, it has simply stopped
        # calling accept() (e.g. wedged holding the GIL). This is the daemon
        # being unresponsive, not absent, so it is framed and denied the same
        # way as connection_lost/malformed_response, not as daemon-not-running.
        context_lines = [
            'HOOKS DAEMON: accept backlog full — daemon unresponsive',
            '',
            f'Error: {error_type} - {error_details}',
            '',
            'The daemon socket exists and the kernel refused this connection',
            'because the daemon has stopped accepting new connections (it is',
            'listening but wedged, not down).',
            '',
        ] + _RESTART_ADVICE
    elif error_type == 'connection_lost':
        # connect() SUCCEEDED (a ConnectionRefusedError, the genuine
        # daemon-down shape, is caught separately and never reaches here) --
        # so the daemon WAS reached, then the connection dropped mid-send or
        # mid-receive. Distinct from malformed_response (a response DID come
        # back, just not a valid one) and from socket_timeout (no response
        # within budget, connection still open): here the pipe itself broke.
        context_lines = [
            'HOOKS DAEMON: connection lost mid-exchange',
            '',
            f'Error: {error_type} - {error_details}',
            '',
            'The daemon was REACHED (the connection succeeded), then the pipe',
            'broke before a response was received.',
            '',
        ] + _RESTART_ADVICE
    elif error_type == 'malformed_response':
        # The socket round-trip SUCCEEDED (connect+send+recv all completed),
        # but what came back was not a valid decision -- the daemon-side
        # chain crashed partway through serialising its verdict, or the
        # connection closed early. Distinct from socket_timeout (no response
        # at all within budget) and from a genuinely dead daemon (which
        # would have failed to connect in the first place).
        context_lines = [
            'HOOKS DAEMON: responded, but not with a valid decision',
            '',
            f'Error: {error_type} - {error_details}',
            '',
            'The daemon was REACHED and answered, but the response could not',
            'be parsed as a judged verdict for this call.',
            '',
        ] + _RESTART_ADVICE
    elif error_type in ('socket_not_found', 'connection_refused') \
            or (event_name == 'PreToolUse' and not daemon_reached):
        # Plan 00466 N24 review 3 MA4 (owner decision): connect() itself
        # never reached a daemon at all -- the socket is missing, nothing is
        # listening on it, or (N69) an error no clause above names, such as
        # a socket this client may not open. The PreToolUse branch below
        # denies all of these, with the one exact-recovery-command carve-out.
        context_lines = [
            'HOOKS DAEMON: could not connect at all',
            '',
            f'Error: {error_type} - {error_details}',
            '',
            'No daemon saw this call: the daemon is not running, its socket is',
            'gone, or the socket cannot be opened (the Error line says which).',
            '',
        ] + _RESTART_ADVICE
    elif event_name == 'PreToolUse':
        # Plan 00466 N69: an error no clause above names, raised AFTER
        # connect() succeeded. The daemon was reached; the exchange failed.
        context_lines = [
            'HOOKS DAEMON: the exchange with the daemon failed',
            '',
            f'Error: {error_type} - {error_details}',
            '',
            'The daemon was REACHED (the connection succeeded), but the exchange',
            'failed before a verdict could be read.',
            '',
        ] + _RESTART_ADVICE
    else:
        context_lines = [
            'HOOKS DAEMON: Not currently running',
            '',
            f'Error: {error_type} - {error_details}',
            '',
            'Hook safety handlers are inactive until the daemon is restarted.',
            'If you are in the middle of an upgrade, this is expected and temporary.',
            '',
            'TO FIX (usually takes a few seconds):',
            'Use the hooks-daemon skill to restart the daemon.',
            'Then use the hooks-daemon skill to verify health.',
            'Invoke via Skill tool with skill=hooks-daemon and args=restart or args=health.',
            '',
            'If restart fails, use the hooks-daemon skill to check logs (args=logs).',
            'Then inform the user if the issue persists.',
        ]
    context = chr(10).join(context_lines)

    # Stop/SubagentStop: top-level decision only. Fail CLOSED (block) for a
    # genuinely-down daemon, but keep the reason honest and carve out the two
    # cases where the daemon is NOT down: a malformed payload failed to parse
    # client-side and never reached the socket (Plan 00157), and a read-side
    # socket_timeout reached a live-but-slow daemon (Plan 00177) — that one fails
    # OPEN. Mirror the error_type branches used for context_lines above.
    if event_name in ('Stop', 'SubagentStop'):
        if error_type == 'socket_timeout':
            # Read-side timeout: the daemon was reached and is ALIVE, a handler
            # was just slow. Fail OPEN (allow the stop) rather than wedge the
            # session with a misleading block that also re-fires into a 30s
            # stall loop. The honest diagnostic is on stderr above. Connect/send
            # failures (genuine down) still fail closed via the else arm below
            # (Plan 00177).
            response = {}
        elif error_type == 'invalid_hook_input':
            reason = ('Hooks daemon received a malformed hook payload - this '
                      'event was not validated (daemon likely healthy; do not restart)')
            response = {
                'decision': 'block',
                'reason': reason,
            }
        else:
            reason = 'Hooks daemon not running - protection not active'
            response = {
                'decision': 'block',
                'reason': reason,
            }
    elif event_name == 'PreToolUse' \
            and not _is_daemon_recovery_command(hook_input, project_path, daemon_root):
        # Plan 00466 N24 review 4 R4-MA1: deny for EVERY PreToolUse transport
        # failure except the exact daemon-recovery command (review 3 MA4's
        # carve-out, checked via _is_daemon_recovery_command so this can
        # never itself block the commands that would fix it). That includes
        # invalid_hook_input (Plan 00466 N140): input that cannot be parsed
        # reached no guard, and is never a recovery command, since that
        # judgement needs the parsed input. This used to be an ALLOWLIST of known
        # error_types (socket_timeout, malformed_response, connection_lost,
        # connect_backlog_full, socket_not_found, connection_refused) that
        # denied, with everything else falling through to the fail-open
        # branch below -- so a connect() failure the transport's except
        # clauses do not name explicitly (PermissionError from a chmod'd
        # socket, NotADirectoryError, an over-long socket path, ...) disabled
        # every later PreToolUse guard. 'An exception never means allow' is
        # binding here: deny by default, name the one exemption instead of a
        # list of what to deny. This project's install/CI story keeps
        # ensure_daemon's auto-start ahead of every real call site here, so
        # 'the socket that auto-start just tried to reach is still missing'
        # is not the fresh-clone-before-first-install case -- that one is
        # handled entirely by emit_hook_error's own NOT_INSTALLED/
        # VENV_MISSING branches, upstream of ever reaching this transport at
        # all.
        if error_type == 'malformed_response':
            verb = 'responded'
        elif daemon_reached:
            verb = 'reached'
        else:
            # Plan 00466 N69: connect() never succeeded -- a missing or
            # refused socket, a full accept backlog, or an unclassified
            # connect error -- so the daemon never saw this call.
            verb = 'unreachable'
        reason = f'Hooks daemon {verb} - no verdict produced ({error_type}) - denied for safety'
        if error_type == 'invalid_hook_input':
            reason = ('Hook input could not be parsed, so no guard judged this call '
                      f'({error_type}) - denied for safety')
        if timeout_note:
            reason = f'{reason}. {timeout_note}'
        response = {
            'hookSpecificOutput': {
                'hookEventName': event_name,
                'permissionDecision': 'deny',
                'permissionDecisionReason': reason,
                'additionalContext': context,
            }
        }
    else:
        # Other events, and the one PreToolUse case that fails open: an
        # exact daemon-recovery command (Plan 00466 N24 review 3 MA4's
        # carve-out) on any of the error_types denied above.
        # hookSpecificOutput with context -- the existing, documented
        # fail-open shape.
        response = {
            'hookSpecificOutput': {
                'hookEventName': event_name,
                'additionalContext': context,
            }
        }
    print(json.dumps(response))

def fail(error_type, error_details):
    '''Report a transport failure. For the status line the daemon is already
    up (the wrapper gates on ensure_daemon), so a mid-render socket failure
    degrades to the same 'NO STATUS DATA' the old jq -r fallback produced;
    every other event fails open (or blocks, for Stop) via emit_error_json.'''
    if response_mode == 'status':
        # Render the fallback the old jq -r produced, but still surface the
        # diagnostic on stderr — no silent error suppression (Plan 00156 review
        # finding 3). (The non-status path logs stderr inside emit_error_json.)
        print(f'HOOKS DAEMON ERROR [{error_type}]: {error_details}', file=sys.stderr)
        print('⚠️ NO STATUS DATA')
    elif response_mode == 'worktree':
        # WorktreeCreate stdout is parsed as a path; a transport failure has no
        # valid path to offer. Fail creation cleanly (non-zero) with the reason
        # on stderr rather than emitting '{}' (which becomes a bad path).
        print(f'HOOKS DAEMON ERROR [{error_type}]: {error_details}', file=sys.stderr)
        sys.exit(1)
    else:
        emit_error_json(event_name, error_type, error_details)
    sys.exit(0)

def render_status(output):
    '''Replicate the old status-line jq -r fallback (.error / .text / no-data).'''
    try:
        data = json.loads(output)
    except Exception:
        return '⚠️ NO STATUS DATA'
    if isinstance(data, dict) and data.get('error'):
        return '⚠️ ERROR: ' + str(data['error'])
    if isinstance(data, dict) and data.get('text'):
        return data['text']
    return '⚠️ NO STATUS DATA'

def print_worktree(output):
    '''WorktreeCreate: Claude Code parses this hook's stdout as the created
    worktree PATH (not JSON), so print the raw .worktreePath the daemon returns.
    If the daemon produced no path (no handler / error), FAIL the creation
    cleanly with a non-zero exit rather than echoing '{}' — Claude Code would
    take '{}' literally as the path '/<cwd>/{}' (the original Plan 00188 bug).

    On failure, report the daemon's OWN reason (Plan 00419 N10). A handler that
    raises has its exception accumulated into the result context, which for this
    event serialises as systemMessage — so the reason is already in these bytes.
    Discarding it and guessing at a cause instead sent a real investigation to
    check a handler registration that was never in doubt, and a message that
    confidently names the wrong cause is worse than one that names none.'''
    try:
        data = json.loads(output)
    except Exception:
        data = None
    if not isinstance(data, dict):
        data = {}
    path = data.get('worktreePath')
    if path:
        print(path)
        sys.exit(0)
    # systemMessage carries a crashed handler's exception; reason carries a
    # deliberate refusal. Either is the daemon speaking for itself.
    detail = data.get('systemMessage') or data.get('reason')
    message = 'HOOKS DAEMON: WorktreeCreate produced no worktree path.'
    if detail:
        message = message + ' The daemon reported: ' + str(detail)
    else:
        message = message + (' No reason was returned — check the daemon logs '
                             '(Skill tool: skill=hooks-daemon, args=logs).')
    print(message, file=sys.stderr)
    sys.exit(1)

# Read the raw hook_input payload from stdin (preserves control characters)
# and parse it so we can wrap it ourselves (jq used to do this). Claude Code
# always sends a JSON object; any failure to read, decode or parse it -- bytes
# that are not UTF-8, or nesting deeper than the parser recurses -- is a real
# error, handled explicitly (Plan 00466 N140: PreToolUse denies it).
try:
    raw = sys.stdin.read()
    hook_input = json.loads(raw)
except Exception as exc:
    fail('invalid_hook_input',
        f'Hook input could not be parsed: {type(exc).__name__}: {exc}')

# Status line injects its own event name into the payload (parity with the old
# jq '. + {hook_event_name: \"Status\"}').
if event_name == 'Status' and isinstance(hook_input, dict):
    hook_input['hook_event_name'] = 'Status'
    # Forward the terminal size from THIS wrapper process's environment (Plan
    # 00167) - the daemon is a separate long-running process and never
    # inherits COLUMNS/LINES. Omit entirely when unset/non-numeric so older
    # Claude Code clients (<2.1.153, which sends no COLUMNS) degrade cleanly.
    for _src, _dst in (('COLUMNS', 'terminal_columns'), ('LINES', 'terminal_lines')):
        _v = os.environ.get(_src)
        if _v is not None and _v.strip().isdigit():
            hook_input[_dst] = int(_v)

# Forward the session's hostname override (Plan 00470 Task 6.1, issues #60 and
# #62): a persistent_crons job may carry hosts:, matched against the hostname
# the SESSION exported, and the daemon is a separate long-running process that
# never inherits it. First non-empty of HOOKS_DAEMON_HOSTNAME then
# CCY_HOST_HOSTNAME, as utils/cron_hosts.py resolves it; omitted when neither
# is set, so the daemon falls back to the system hostname it shares.
if isinstance(hook_input, dict):
    for _name in ('HOOKS_DAEMON_HOSTNAME', 'CCY_HOST_HOSTNAME'):
        _v = os.environ.get(_name, '').strip()
        if _v:
            hook_input['hooks_daemon_hostname'] = _v
            break

# Wrap into the daemon request envelope; newline-terminated as the daemon
# expects. The envelope is one level deeper than the input, so input that
# only just parsed can still be too deep to encode.
try:
    request = json.dumps({'event': event_name, 'hook_input': hook_input}) + '\n'
except Exception as exc:
    hook_input = None
    fail('invalid_hook_input',
        f'Hook input could not be encoded for the daemon: {type(exc).__name__}: {exc}')

socket_path = '$SOCKET_PATH'

if relay_failure and event_name == 'PreToolUse' and not response_mode:
    # The relay connected, sent this call and got no verdict; asking the
    # daemon again would only wait out the same wedge. Deny through
    # emit_error_json, whose one recovery carve-out judges this call.
    daemon_reached = True
    fail('relay_exchange_failed',
        f'hooks-relay reached the daemon but got no verdict ({relay_failure})')

try:
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    sock.settimeout(SOCKET_TIMEOUT_SECONDS)  # budget for connect+send+recv
    sock.connect(socket_path)
    daemon_reached = True
    sock.sendall(request.encode('utf-8'))
    sock.shutdown(socket.SHUT_WR)

    response = b''
    while True:
        chunk = sock.recv(4096)
        if not chunk:
            break
        response += chunk

    sock.close()

    # Output response (strip trailing newline for clean output)
    output = response.decode('utf-8').rstrip('\n')
    if response_mode == 'status':
        print(render_status(output))
    elif response_mode == 'worktree':
        print_worktree(output)  # prints raw path + exits (0 on success, 1 if none)
    elif event_name == 'PreToolUse' and not _pretooluse_response_looks_valid(output):
        # Plan 00466 n24 security review: a response WAS received (the
        # connect+send+recv all succeeded), but it is not one of
        # PreToolUse's two legitimate shapes -- the connection closed
        # early, or the daemon-side chain crashed partway through
        # serialising its verdict. Treated exactly like a socket_timeout:
        # fail CLOSED via emit_error_json's PreToolUse branch (still
        # carving out an exact daemon-recovery command).
        fail('malformed_response',
            f'Daemon responded but the response was not a valid PreToolUse '
            f'decision (received {output[:200]!r})')
    else:
        print(output)
    sys.exit(0)

except socket.timeout:
    fail('socket_timeout',
        f'Socket timeout ({SOCKET_TIMEOUT_SECONDS:g}s) waiting on daemon at {socket_path}. '
        'The daemon was reached but a handler ran past the budget (it is alive).')

except FileNotFoundError:
    fail('socket_not_found',
        f'Daemon socket not found at {socket_path}. '
        'Daemon may not be running or socket was deleted.')

except ConnectionRefusedError:
    fail('connection_refused',
        f'Daemon refusing connections at {socket_path}. '
        'Daemon may be shutting down or in error state.')

except BlockingIOError as e:
    # Plan 00466 N24 review 3 MA1: on a UNIX stream socket, connect() raises
    # EAGAIN/EWOULDBLOCK (BlockingIOError) instead of blocking until the
    # timeout when the kernel's accept backlog is already full. The daemon
    # process is there and listening -- it has simply stopped calling
    # accept(), for example while wedged holding the GIL -- so this must be
    # treated as an unresponsive-but-present daemon (deny), never as an
    # absent one (allow).
    fail('connect_backlog_full',
        f'Daemon at {socket_path} did not accept the connection '
        f'({type(e).__name__}: {e}). The accept backlog is full.')

except (BrokenPipeError, ConnectionResetError) as e:
    # Plan 00466 N40 review 2 mA1: connect() already SUCCEEDED by the time
    # either of these can be raised here (sock.connect() itself raises
    # ConnectionRefusedError, caught above, not these) -- so the daemon WAS
    # reached, same as a socket_timeout, and the generic except Exception
    # below used to classify this as an opaque error_type never in the
    # PreToolUse fail-closed allowlist, silently ALLOWing. A legacy-socket
    # peer past its drain cap (server.py's _drain_oversized_request) is
    # exactly this shape on a large enough payload.
    fail('connection_lost',
        f'Daemon at {socket_path} was reached but the connection was lost '
        f'mid-exchange ({type(e).__name__}: {e}).')

except Exception as e:
    fail(type(e).__name__, f'{type(e).__name__}: {e}')
" "$event_name" "$response_mode" "${PROJECT_PATH:-}" "$_HOOKS_DAEMON_RELAY_FAILED" \
        "${HOOKS_DAEMON_ROOT_DIR:-}" <&3; printf 'x%s' "$?")"
    _rv="${_hd_answer##*x}"
    _hd_answer="${_hd_answer%x*}"
    # Every PreToolUse path above answers and exits 0, so a failure here is
    # python3 missing or crashing: no guard's verdict can be trusted, and
    # Claude Code runs a call whose hook wrote nothing (Plan 00466 round 3,
    # R2-1). Whatever python3 printed first is dropped.
    if [[ "$_rv" -ne 0 && "$event_name" == "PreToolUse" && -z "$response_mode" ]]; then
        _hooks_daemon_static_deny
        _rv=0
    else
        printf '%s' "$_hd_answer"
    fi
    exec 3<&-
    if [[ -n "$_nc_replay_payload" ]]; then
        rm -f "$_nc_replay_payload"
    fi
    return "$_rv"
}

#
# forward_stop_event() - Forward a Stop / SubagentStop event to the daemon
#                        and translate decision=block into exit-code-2 + stderr
#
# Plan 00101 Phase 9: Claude Code v2.1.114 silently demotes JSON-via-stdout
# `{"decision":"block"}` to `level: suggestion, preventedContinuation: false`,
# breaking the auto_continue_stop contract. The daemon CANNOT control the
# hook subprocess exit code from inside its own Python process — only the
# bash wrapper that Claude Code spawns can set it. This helper centralises
# the translation so both .claude/hooks/stop and .claude/hooks/subagent-stop
# stay one-liners and the JSON-to-exit-code mapping lives in one place.
#
# Behaviour:
#   1. Pass stdin JSON to send_request_stdin, which wraps it into
#      {event, hook_input} itself (jq-free since Plan 00156 T2).
#   2. Capture daemon response, echo to stdout (back-compat for agent JSON
#      visibility + existing test invariants).
#   3. Parse `.decision`:
#        - "block" → print `.reason` to stderr, exit 2 (hard re-entry).
#        - other  → exit 0 (allow stop).
#
# Args:
#   $1 - event_name: "Stop" or "SubagentStop"
#   $2 - event_sock_name: (optional, Plan 00290) this event's bash_key, e.g.
#        "stop" — threaded through to send_request_stdin's nc rung. Absent
#        for every deployed forwarder by default (byte-identical).
#   $3 - events_dir_override: (optional, Plan 00295 Task 2.5) the
#        generation-time-resolved events dir override — threaded through to
#        send_request_stdin's own $4. Absent unless append_nc_socket_arg's
#        AF_UNIX-overflow decision applied at deploy time.
#
# Reads:
#   stdin: Claude Code hook input JSON
#
# Returns:
#   2 if daemon emits decision=block
#   0 otherwise (including daemon socket errors — those are handled by
#     send_request_stdin's emit_error_json which already returns a block
#     payload and we DO want hard re-entry on daemon-down).
#
forward_stop_event() {
    local event_name="$1"
    local event_sock_name="${2:-}"
    local events_dir_override="${3:-}"
    if [ -z "$event_name" ]; then
        echo '{"error":"forward_stop_event: event_name required"}' >&2
        return 1
    fi

    local response_file
    response_file="$(mktemp)"
    # shellcheck disable=SC2064  # intentional early-binding of file path
    trap "rm -f '$response_file'" EXIT

    # Plan 00156 (T2): jq-free. send_request_stdin wraps the raw stdin hook_input
    # into {event, hook_input} itself; python3 (already the transport dependency)
    # translates decision=block into exit 2 + reason on stderr. The reason may
    # contain control characters, so it is printed straight from python rather
    # than round-tripped through a shell variable.
    send_request_stdin "$event_name" "" "$event_sock_name" "$events_dir_override" > "$response_file"
    cat "$response_file"

    python3 -c "
import json
import sys
try:
    with open(sys.argv[1]) as fh:
        data = json.load(fh)
except Exception:
    sys.exit(0)  # unparseable/empty response -> allow stop (matches old jq // '')
if isinstance(data, dict) and data.get('decision') == 'block':
    reason = data.get('reason') or ''
    if reason:
        print(reason, file=sys.stderr)
    sys.exit(2)
sys.exit(0)
" "$response_file"
    return $?
}

# Export functions for use by forwarder scripts
export -f emit_hook_error
export -f _hooks_daemon_recovery_py
export -f _hooks_daemon_stdin_is_recovery_command
export -f _hooks_daemon_recovery_command
export -f _hooks_daemon_static_deny
export -f _hooks_daemon_stdin_is_provision_command
export -f _hooks_daemon_emit_needs_provision
export -f _detect_needs_provision
export -f _config_daemon_key_raw
export -f _config_unprovisioned_mode
export -f validate_venv
# is_daemon_running and everything it calls.
export _HOOKS_DAEMON_PID_MAX
export _HOOKS_DAEMON_PROCFS
export -f _hooks_daemon_is_pid_text
export -f _hooks_daemon_root_is_this_install
export -f _hooks_daemon_run_cli_helper
export -f _hooks_daemon_helper_proves_pid
export -f _hooks_daemon_argv_prove_this_project
export -f _hooks_daemon_argv_name_root
export -f _hooks_daemon_cmdline_proves_this_project
export -f _hooks_daemon_ps_args_prove_this_project
export -f _hooks_daemon_pid_args_prove_this_project
export -f _hooks_daemon_pid_is_this_users
export -f is_daemon_running
export -f start_daemon
export -f _hooks_daemon_drain_launch
export -f _hooks_daemon_launch_settled
export -f _hooks_daemon_end_launch
export -f _hooks_daemon_is_down
export -f ensure_daemon
export -f send_request_stdin
export -f forward_stop_event
