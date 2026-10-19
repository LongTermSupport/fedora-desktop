#!/bin/bash
#
# DAEMON-OWNED FILE - do not edit. Deployed into your project by the
# claude-code-hooks-daemon installer and refreshed on every upgrade, so local
# changes are discarded. See CLAUDE/LLM-INSTALL.md, "Which Files Under
# .claude/ Are Yours?", for the full list and the linter exclusions.
#
# Claude Code Hooks Daemon - Provision a fresh checkout (Plan 00477)
#
# A fresh clone of a project that uses the daemon carries the tracked assets
# (hook forwarders, init.sh, settings.json, hooks-daemon.yaml) but not the
# daemon itself: .claude/hooks-daemon/ is gitignored and per-checkout. This
# script builds that local part, at EXACTLY the version the project names, and
# starts the daemon. It is tracked so that it exists before the daemon does.
#
# Usage:
#   bash .claude/provision.sh [--skip-reading-confirmation=<digest>]
#   bash .claude/provision.sh --background
#
# What it does:
#   1. Resolves the expected version: daemon.expected_version in
#      .claude/hooks-daemon.yaml, else the .claude/HOOKS-DAEMON.md header.
#      Unknown or malformed: it stops and says so. It never guesses, and it
#      never installs main.
#   2. Fetches that one TAG (vX.Y.Z, depth 1) into .claude/hooks-daemon/.
#   3. Builds the venv through the clone own venv build path and lock
#      (scripts/venv_bootstrap.sh repair).
#   4. Starts the daemon through init.sh, as a hook would.
#
# A clone that is already there is SYNCED to the expected version instead
# (a pull changed daemon.expected_version, so the gitignored clone is behind or
# ahead of it). First, in a STAGE under the clone's untracked/, writing nothing
# else of the live clone: the tag fetched from the same URL into a clone of its
# own, its version checked, the target release's own pre-deploy gate run from
# there (the one upgrade runs; it may stop for the reading list, and
# --skip-reading-confirmation=<digest> confirms it), the target's venv built
# there by its own build path and rebound to the live path. Only then the
# switch: the old daemon is stopped, the daemon's state under untracked/ is moved
# into the stage, the stage is swapped in by renames, the old clone is kept
# beside it, and the new daemon is started; the old clone goes once the new
# daemon answers. A marker keeps hooks from starting a daemon mid-switch. A
# clone already at the version is left alone. A failure before the switch
# changes nothing and never stops the old daemon; one during it swaps the old
# clone back and starts its daemon again, and a switch a killed run left
# half-done is undone the same way by the next run.
#
# --background is that sync, started detached by the tracked session-start
# forwarder at every session start (Plan 00477 Task 5.2); it does nothing unless
# a pulled daemon.expected_version names a NEWER version than the clone. It only
# ever syncs an existing clone forward: never a fresh checkout, never a
# downgrade. It waits for the old daemon to be running, takes a lock so two
# syncs never overlap, and records each target version's outcome in
# .claude/hooks-daemon/untracked/background-sync/v<version>.state (its output
# goes to v<version>.log beside it); that file is also the marker that the
# version was attempted, so it is attempted once. The SessionStart version check
# reads it back, and a failure is reported at every session start with the
# command to run by hand. The running daemon serves until the restart.
#
# What it never does:
#   - write a tracked file (everything it creates is under the gitignored
#     .claude/hooks-daemon/);
#   - take the clone URL from the environment or from project config: the URL
#     is the constant below, the same one install.sh trusts;
#   - keep a fetched tag whose version.py does not name the version asked for;
#   - replace a modified clone: one with tracked changes, files outside
#     untracked/, a foreign origin or no git checkout is refused untouched. A
#     clean one is synced; its gitignored build caches outside untracked/
#     (__pycache__ and the like) stay with the old clone;
#   - run config migrations or regenerate docs: moving a PROJECT to a new
#     version is upgrade, which also writes the tracked files.
#
# Hooks never run this. A person or an agent runs it, deliberately.
#

set -euo pipefail

# The trusted clone source. Keep equal to DAEMON_REPO in install.sh (a test
# asserts it). Deliberately not overridable.
readonly PROVISION_REPO_URL="https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon.git"

_provision_say() {
    printf '%s\n' "$*"
}

_provision_fail() {
    _PROVISION_FAIL_REASON="$1"
    printf 'PROVISION FAILED: %s\n' "$1" >&2
    shift
    local line
    for line in "$@"; do
        printf '  %s\n' "$line" >&2
    done
    exit 1
}

# The caller confirms it has read the target release's pre-deploy gate listing
# with --skip-reading-confirmation=<digest>, the digest the stop printed. A bare
# flag is passed on as an empty acknowledgement, which matches no listing.
READING_ACKNOWLEDGED=false
READING_ACKNOWLEDGEMENT=""
BACKGROUND=false
for _provision_arg in "$@"; do
    case "$_provision_arg" in
        --background)
            BACKGROUND=true
            ;;
        --skip-reading-confirmation=*)
            READING_ACKNOWLEDGED=true
            READING_ACKNOWLEDGEMENT="${_provision_arg#*=}"
            ;;
        --skip-reading-confirmation)
            READING_ACKNOWLEDGED=true
            READING_ACKNOWLEDGEMENT=""
            ;;
        *)
            _provision_fail "unknown argument: $_provision_arg" \
                "Usage: bash .claude/provision.sh [--skip-reading-confirmation=<digest>]" \
                "       bash .claude/provision.sh --background"
            ;;
    esac
done

_PROVISION_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
_PROVISION_PROJECT_ROOT="$(dirname "$_PROVISION_SCRIPT_DIR")"

[[ "$(basename "$_PROVISION_SCRIPT_DIR")" == ".claude" ]] ||
    _provision_fail "this script must live in the project .claude/ directory (found in $_PROVISION_SCRIPT_DIR)"

# The daemon own repository is set up by scripts/bootstrap-self-install.sh, and
# sourcing init.sh there would report the unconfigured repository instead.
if [[ -f "$_PROVISION_PROJECT_ROOT/src/claude_code_hooks_daemon/version.py" ]]; then
    _provision_fail "this is the hooks-daemon repository itself, not a project that uses it." \
        "Set it up for development with: scripts/bootstrap-self-install.sh"
fi

[[ -f "$_PROVISION_SCRIPT_DIR/init.sh" ]] ||
    _provision_fail "$_PROVISION_SCRIPT_DIR/init.sh is missing." \
        "It is a tracked file of the project; restore it from git, then run this again."

command -v git > /dev/null ||
    _provision_fail "git is not installed." "Install git, then run this again."

# init.sh supplies the ONE version resolver and the clone-state probes. Sourcing
# it creates the empty .claude/hooks-daemon/untracked/ skeleton, which is
# expected and handled below.
# shellcheck source=init.sh
source "$_PROVISION_SCRIPT_DIR/init.sh"

DAEMON_DIR="$HOOKS_DAEMON_ROOT_DIR"
if [[ "$DAEMON_DIR" != "$PROJECT_PATH/.claude/hooks-daemon" ]]; then
    _provision_fail "the daemon root is overridden to $DAEMON_DIR (HOOKS_DAEMON_ROOT_DIR)." \
        "Provision only builds the standard location, $PROJECT_PATH/.claude/hooks-daemon."
fi

# ------------------------------------------------------------------
# The switch to another version, and undoing one a killed run left
# ------------------------------------------------------------------
# A sync builds the target in a stage under the clone's gitignored untracked/.
# With the old daemon stopped, the old clone's untracked/ entries (the daemon's
# state) are moved into the stage, each name written to the stage's manifest
# first, and three renames swap it in:
#   1. the live clone                       -> SYNC_ASIDE_DIR, beside it
#   2. SYNC_ASIDE_DIR/untracked/sync-stage  -> the live path
#   3. SYNC_ASIDE_DIR                       -> <live>/untracked/sync-previous
# The old clone stays there until the new daemon answers, then it is renamed to
# sync-previous.discard and removed. A run killed anywhere in between leaves a
# state the next run tells apart by what exists, and undoes (Plan 00477).
readonly SYNC_STAGE_DIR_NAME="sync-stage"
readonly SYNC_PREVIOUS_DIR_NAME="sync-previous"
readonly SYNC_CARRIED_MANIFEST=".sync-carried"
# Under the clone's untracked/. Keep equal to SYNC_STATE_DIR_NAME in
# install/background_sync.py, which reads the state back (a test asserts it).
readonly SYNC_STATE_DIR_NAME="background-sync"
readonly SYNC_STAGE_DIR="$DAEMON_DIR/untracked/$SYNC_STAGE_DIR_NAME"
readonly SYNC_PREVIOUS_DIR="$DAEMON_DIR/untracked/$SYNC_PREVIOUS_DIR_NAME"
readonly SYNC_ASIDE_DIR="$DAEMON_DIR.$SYNC_PREVIOUS_DIR_NAME"

# Rename a directory to a path nothing holds. Should a directory appear there
# first, mv would move the source INTO it; that is undone and is a failure.
_provision_rename() {
    local from="$1" to="$2"
    if [[ -e "$to" || -L "$to" ]]; then
        _provision_say "Cannot rename $from to $to: something is already there."
        return 1
    fi
    mv -- "$from" "$to" || return 1
    if [[ -e "$to/${from##*/}" && ! -e "$from" ]]; then
        mv -- "$to/${from##*/}" "$from"
        _provision_say "Cannot rename $from to $to: a directory appeared there first."
        return 1
    fi
}

# Is this directory only the untracked/ skeleton init.sh creates when a hook
# runs while the live path is empty? Nothing holding a checkout, source, a
# venv or a switch's directories counts.
_provision_is_skeleton() {
    local dir="$1" entry
    local -a entries
    shopt -s nullglob dotglob
    entries=("$dir"/* "$dir"/untracked/*)
    shopt -u nullglob dotglob
    for entry in ${entries[@]+"${entries[@]}"}; do
        case "${entry#"$dir"/}" in
            untracked) ;;
            untracked/venv-* | "untracked/$SYNC_STAGE_DIR_NAME" | "untracked/$SYNC_PREVIOUS_DIR_NAME") return 1 ;;
            untracked/*) ;;
            *) return 1 ;;
        esac
    done
}

# Rename a clone onto the live path, clearing a skeleton a hook left there in
# the instant the path was empty.
_provision_rename_onto_live() {
    if [[ -d "$DAEMON_DIR" ]] && _provision_is_skeleton "$DAEMON_DIR"; then
        rm -rf -- "$DAEMON_DIR"
    fi
    _provision_rename "$1" "$DAEMON_DIR"
}

# A live process, other than this one, holding a switch marker anywhere a switch
# leaves one. Prints its pid; returns 1 when there is none.
_provision_live_switch_holder() {
    local dir marker holder
    for dir in "$DAEMON_DIR" "$SYNC_STAGE_DIR" "$SYNC_ASIDE_DIR" "$SYNC_ASIDE_DIR/untracked/$SYNC_STAGE_DIR_NAME"; do
        marker="$dir/untracked/$_HOOKS_DAEMON_SWITCH_MARKER_NAME"
        [[ -f "$marker" ]] || continue
        holder=""
        IFS= read -r holder < "$marker" || [[ -n "$holder" ]] || continue
        if [[ "$holder" =~ ^[0-9]+$ && "$holder" != "$$" ]] && kill -0 "$holder" 2> /dev/null; then
            printf '%s' "$holder"
            return 0
        fi
    done
    return 1
}

# is_daemon_running, asked afresh. A sync asks about more than one daemon (the
# old one, then the one it starts), and init.sh's helper proves at most one pid
# per process, answering "not proven" for any other; a subshell starts clean.
_provision_daemon_running() (
    _HOOKS_DAEMON_HELPER_KEY=""
    is_daemon_running
)

# Stop the daemon running from the clone, if one is. Returns 1 when it would not.
_provision_stop_daemon() {
    local running=0
    _provision_daemon_running || running=$?
    if ((running == 1)); then
        return 0
    fi
    bash "$DAEMON_DIR/bin/hooks-daemon" stop
}

# Remove the stage, and the target clone and venv in it.
_provision_stage_remove() {
    rm -rf -- "$SYNC_STAGE_DIR"
}

# Move the daemon's state back from the stage to the live untracked/, by the
# manifest written before each entry was moved. An entry the live untracked/
# holds again (a hook re-created it) stays in the stage and goes with it.
_provision_uncarry() {
    local stage_untracked="$SYNC_STAGE_DIR/untracked" name
    while IFS= read -r name; do
        [[ -n "$name" ]] || continue
        if [[ -e "$stage_untracked/$name" || -L "$stage_untracked/$name" ]] \
            && [[ ! -e "$DAEMON_DIR/untracked/$name" ]]; then
            mv -- "$stage_untracked/$name" "$DAEMON_DIR/untracked/$name" || return 1
        fi
    done < "$stage_untracked/$SYNC_CARRIED_MANIFEST"
}

# A background sync's record left at "running" by a process that is gone is
# rewritten as failed, naming the switch that was undone.
_provision_record_interrupted() {
    local state key value status pid line
    local -a states lines
    shopt -s nullglob
    states=("$DAEMON_DIR/untracked/$SYNC_STATE_DIR_NAME"/v*.state)
    shopt -u nullglob
    for state in ${states[@]+"${states[@]}"}; do
        status=""
        pid=""
        lines=()
        while IFS='=' read -r key value; do
            case "$key" in
                status) status="$value" ;;
                pid) pid="$value" ;;
                detail) continue ;;
            esac
            lines+=("$key=$value")
        done < "$state"
        [[ "$status" == "running" && "$pid" =~ ^[0-9]+$ ]] || continue
        if kill -0 "$pid" 2> /dev/null; then
            continue
        fi
        {
            for line in "${lines[@]}"; do
                [[ "$line" == status=* ]] && line="status=failed"
                printf '%s\n' "$line"
            done
            printf 'detail=%s\n' "the sync (pid $pid) was killed during the switch; the previous version was put back"
        } > "$state.tmp.$$" && mv -f "$state.tmp.$$" "$state"
    done
}

# Undo a switch from whatever it left, and start the previous daemon again.
# Returns 0 when no switch had begun or it was undone; 1, with the reason
# printed, when it could not be.
_provision_recover_switch() {
    local recovered=false
    if [[ -d "$SYNC_PREVIOUS_DIR.discard" ]]; then
        rm -rf -- "$SYNC_PREVIOUS_DIR.discard"
    fi
    if [[ -d "$SYNC_ASIDE_DIR" ]]; then
        recovered=true
        if [[ -e "$DAEMON_DIR/.git" ]]; then
            # After the second rename: the target is live, the old clone beside it.
            _provision_rename "$SYNC_ASIDE_DIR" "$SYNC_PREVIOUS_DIR" || return 1
        else
            # After the first: only the old clone, beside an empty live path.
            _provision_rename_onto_live "$SYNC_ASIDE_DIR" || return 1
        fi
    fi
    if [[ -d "$SYNC_PREVIOUS_DIR" ]]; then
        recovered=true
        if ! _provision_stop_daemon; then
            _provision_say "The daemon running from the swapped-in clone did not stop, so the old clone was not put back."
            return 1
        fi
        _provision_rename "$SYNC_PREVIOUS_DIR" "$SYNC_ASIDE_DIR" || return 1
        _provision_rename "$DAEMON_DIR" "$SYNC_ASIDE_DIR/untracked/$SYNC_STAGE_DIR_NAME" || return 1
        _provision_rename_onto_live "$SYNC_ASIDE_DIR" || return 1
    fi
    if [[ -f "$SYNC_STAGE_DIR/untracked/$SYNC_CARRIED_MANIFEST" ]]; then
        recovered=true
        _provision_uncarry || return 1
    fi
    [[ "$recovered" == true ]] || return 0

    _provision_stage_remove
    rm -f "$DAEMON_DIR/untracked/$_HOOKS_DAEMON_SWITCH_MARKER_NAME"
    _provision_record_interrupted
    local previous
    previous="$(_clone_version)" || previous="unreadable"
    if bash "$DAEMON_DIR/bin/hooks-daemon" restart; then
        _provision_say "RECOVERED: a switch to another version was undone; v$previous is back in place and its daemon was started again."
    else
        _provision_say "RECOVERED: a switch to another version was undone; v$previous is back in place."
        _provision_say "WARNING: its daemon did not start. Try: $DAEMON_DIR/bin/hooks-daemon restart"
    fi
}

if _PROVISION_SWITCH_HOLDER="$(_provision_live_switch_holder)"; then
    if [[ "$BACKGROUND" == true ]]; then
        _provision_say "Another provision (pid $_PROVISION_SWITCH_HOLDER) is switching the daemon clone; nothing to do."
        exit 0
    fi
    _provision_fail "another provision (pid $_PROVISION_SWITCH_HOLDER) is switching the daemon clone to another version right now." \
        "Nothing was changed. Wait for it to finish, then run this again."
fi
if ! _provision_recover_switch; then
    _provision_fail "a switch to another version that a killed run left half-done could not be undone (the reason is above)." \
        "Nothing else was changed. Fix that, then run this again: it retries the undo first."
fi

# ------------------------------------------------------------------
# Step 1: which version?
# ------------------------------------------------------------------
if ! _resolve_expected_version; then
    if [[ "$_HOOKS_DAEMON_EXPECTED_VERSION_SOURCE" == "config-invalid" ]]; then
        _provision_fail "daemon.expected_version in .claude/hooks-daemon.yaml is not X.Y.Z." \
            "It must be a released version such as 3.68.0 (no v prefix, no branch name)." \
            "Correct it in that file, commit it, then run this again."
    fi
    _provision_fail "the daemon version this project expects is unknown." \
        "Neither daemon.expected_version in .claude/hooks-daemon.yaml nor the" \
        "  .claude/HOOKS-DAEMON.md header names one, and provision will not guess." \
        "Ask whoever maintains the project, or set daemon.expected_version to the" \
        "  released version the project was last upgraded to, commit it, and run this again."
fi
VERSION="$_HOOKS_DAEMON_EXPECTED_VERSION"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
    _provision_fail "refusing to use $VERSION as a git tag: it is not X.Y.Z."
TAG="v$VERSION"

_provision_say "Provisioning the hooks daemon $TAG for this checkout (version from $_HOOKS_DAEMON_EXPECTED_VERSION_SOURCE)."

# ------------------------------------------------------------------
# Syncing a clone that is already here (Plan 00477 Tasks 5.2a, 5.2)
# ------------------------------------------------------------------
# The same gate, python floor and time limit scripts/upgrade_version.sh uses.
readonly GATE_PYTHON_FLOOR="3.11"
readonly GATE_TIMEOUT_SECONDS=300
readonly GATE_NEEDS_ACKNOWLEDGEMENT=3
readonly SYNC_STAMP_FILE=".daemon-version"

# How far a sync has got: staged (beside the live daemon, which is untouched),
# then switching (the old daemon is being stopped, or is down).
SYNC_PHASE="staged"
PRESENT_VERSION=""
# The live venv's name, which stays with the old clone, and the staged venv's
# name once it is rebound to the live path.
SYNC_OLD_VENV_NAME=""
SYNC_NEW_VENV_NAME=""
# Set when the relocated venv failed its check and was rebuilt in place: why.
SYNC_VENV_NOTE=""

_provision_clone_git() {
    git -C "$DAEMON_DIR" "$@"
}

# The version a version.py text names, by the rule init.sh applies to the file
# (_clone_version).
_provision_version_in_text() {
    local line
    line="$(grep -m1 -oE '^__version__ = "[0-9]+\.[0-9]+\.[0-9]+"' <<< "$1")" || return 1
    line="${line#*\"}"
    printf '%s' "${line%\"}"
}

# Refuse before anything in the clone has changed: the reason, the manual route.
_provision_sync_refuse() {
    local reason="$1"
    shift
    _provision_fail "$reason" ${@+"$@"} \
        "The clone was not touched." \
        "To move the whole project to another version, use: /hooks-daemon upgrade" \
        "To have provision build a fresh clone instead, move $DAEMON_DIR aside and run this again."
}

# The checks that decide whether provision may touch this clone at all. Sets
# SYNC_OLD_VENV_NAME when the live venv is one of the clone's own.
_provision_sync_preflight() {
    local toplevel real_dir origin dirty line count=0
    local -a shown=()
    if [[ ! -e "$DAEMON_DIR/.git" ]]; then
        _provision_sync_refuse "$DAEMON_DIR is not a git checkout, so there is no version to sync from."
    fi
    if ! toplevel="$(_provision_clone_git rev-parse --show-toplevel)"; then
        _provision_sync_refuse "$DAEMON_DIR is not a git checkout that git can read."
    fi
    real_dir="$(cd "$DAEMON_DIR" && pwd -P)"
    if [[ "$toplevel" != "$real_dir" ]]; then
        _provision_sync_refuse "$DAEMON_DIR is not a git checkout of its own (git sees $toplevel)."
    fi
    if ! origin="$(_provision_clone_git config --get remote.origin.url)"; then
        origin=""
    fi
    if [[ "$origin" != "$PROVISION_REPO_URL" ]]; then
        _provision_sync_refuse "the clone's origin is ${origin:-not set}, not $PROVISION_REPO_URL." \
            "Provision fetches only from that URL, and only into a clone that came from it."
    fi
    if ! dirty="$(_provision_clone_git status --porcelain --untracked-files=all -- . ':(exclude)untracked')"; then
        _provision_sync_refuse "git could not report the state of the clone."
    fi
    if [[ -n "$dirty" ]]; then
        while IFS= read -r line; do
            count=$((count + 1))
            if [[ "$count" -le 10 ]]; then
                shown+=("$line")
            fi
        done <<< "$dirty"
        _provision_sync_refuse "the clone has local changes outside untracked/, and a sync would carry them across versions." \
            "Commit, move or discard them yourself first. Changed ($count):" \
            "${shown[@]}"
    fi
    local old_venv
    if old_venv="$(_provision_own_venv_dir)"; then
        SYNC_OLD_VENV_NAME="${old_venv##*/}"
    fi
}

# The venv the resolver chose, when it is one of this clone's own
# (untracked/venv-*), as a physical path comparison: an override naming another
# interpreter is neither read for a stamp nor moved. Prints the directory.
_provision_own_venv_dir() {
    local venv_dir="${PYTHON_CMD%/bin/*}" venv_real daemon_real
    venv_real="$(cd "$venv_dir" && pwd -P)" || return 1
    daemon_real="$(cd "$DAEMON_DIR" && pwd -P)" || return 1
    case "$venv_real" in
        "$daemon_real"/untracked/venv-*) printf '%s' "$venv_dir" ;;
        *) return 1 ;;
    esac
}

# The target release's own pre-deploy gate, run from the staged tree the way
# scripts/upgrade_version.sh runs it: under env -i, a standard-library-only Python found in a trusted system
# location, a verdict file only this function can write. The FROM side is the
# installed version (the venv stamp, else the clone's version before the sync) and
# the TO side is the target. Exit 0 proceeds, 3 stops for the reading, anything
# else could not decide. A target that predates the gate has none to run, exactly
# as upgrading to it runs none.
_provision_gate() (
    local gate_script="$SYNC_STAGE_DIR/src/claude_code_hooks_daemon/install/upgrade_gate_standalone.py"
    local sanitise="$SYNC_STAGE_DIR/scripts/install/env_sanitise.sh"
    local discovery="$SYNC_STAGE_DIR/scripts/lib/python_discovery.sh"
    if [[ ! -f "$gate_script" ]]; then
        _provision_say "$TAG predates the pre-deploy gate, so there is none to run (upgrading to it runs none either)."
        exit 0
    fi
    if [[ ! -f "$sanitise" || ! -f "$discovery" ]]; then
        printf 'The pre-deploy gate of %s cannot run: scripts/install/env_sanitise.sh or scripts/lib/python_discovery.sh is missing.\n' "$TAG" >&2
        exit 1
    fi
    # shellcheck source=/dev/null
    source "$sanitise"
    # shellcheck source=/dev/null
    source "$discovery"
    local PATH
    PATH="$(_gate_trusted_path)"

    local gate_python env_bin od_bin timeout_bin nonce verdict_dir verdict_file
    if ! gate_python="$(unset HOOKS_DAEMON_PYTHON HOOKS_DAEMON_VENV_PATH; find_latest_python "$GATE_PYTHON_FLOOR" "$SYNC_STAGE_DIR/pyproject.toml")"; then
        printf 'No Python %s+ interpreter for the pre-deploy gate in a system location (%s).\n' "$GATE_PYTHON_FLOOR" "$GATE_SAFE_PATH" >&2
        exit 1
    fi
    if ! env_bin="$(_gate_tool env)" || ! od_bin="$(_gate_tool od)"; then
        printf 'The pre-deploy gate needs env and od from a system location (%s).\n' "$GATE_SAFE_PATH" >&2
        exit 1
    fi
    nonce="$("$od_bin" -An -N16 -tx1 /dev/urandom)" || nonce=""
    nonce="${nonce//[[:space:]]/}"
    if [[ -z "$nonce" ]] || ! verdict_dir="$(mktemp -d -p /tmp)"; then
        printf 'Could not set up the pre-deploy gate verdict file (no /dev/urandom, or mktemp failed).\n' >&2
        exit 1
    fi
    verdict_file="$verdict_dir/verdict"

    # The installed side: the venv stamp when the venv is one of this clone own,
    # else the version the clone reported before the checkout.
    local installed_stamp="" venv_dir
    if venv_dir="$(_provision_own_venv_dir)" && [[ -f "$venv_dir/$SYNC_STAMP_FILE" ]]; then
        IFS= read -r installed_stamp < "$venv_dir/$SYNC_STAMP_FILE" || [[ -n "$installed_stamp" ]]
    fi
    local -a gate_args=(
        --daemon-dir "$SYNC_STAGE_DIR"
        --project-root "$PROJECT_PATH"
        --to "$TAG"
        --target-stamp "$TAG"
        --target-ref "$TAG"
    )
    if [[ -n "$installed_stamp" ]]; then
        gate_args+=(--installed-stamp "$installed_stamp")
    else
        gate_args+=(--from "$PRESENT_VERSION")
    fi
    if [[ "$READING_ACKNOWLEDGED" == true ]]; then
        gate_args+=(--acknowledgement "$READING_ACKNOWLEDGEMENT")
    fi
    gate_args+=(--verdict-file "$verdict_file" --nonce "$nonce")

    local -a runner=("$env_bin" -i "PATH=$GATE_SAFE_PATH" "LANG=C.UTF-8")
    if [[ -n "${HOME:-}" ]]; then
        runner+=("HOME=$HOME")
    fi
    if timeout_bin="$(_gate_tool timeout)"; then
        runner+=("$timeout_bin" "$GATE_TIMEOUT_SECONDS")
    else
        printf 'No timeout command in a system location, so the gate runs without a time limit.\n' >&2
    fi
    _provision_say "Pre-deploy gate: what moving from v$PRESENT_VERSION to $TAG changes (run by $gate_python) ..."
    local gate_output gate_exit=0
    gate_output="$("${runner[@]}" "$gate_python" -I -S "$gate_script" "${gate_args[@]}" 2>&1)" || gate_exit=$?
    printf '%s\n' "$gate_output" >&2

    # A zero exit counts only with the verdict file this run's nonce heads.
    local nonce_line="" line="" verdict=""
    if [[ -f "$verdict_file" && ! -L "$verdict_file" && -O "$verdict_file" ]]; then
        {
            IFS= read -r nonce_line || nonce_line=""
            while IFS= read -r line; do
                case "$line" in
                    verdict=*) verdict="${line#verdict=}" ;;
                esac
            done
        } < "$verdict_file"
    fi
    rm -rf -- "$verdict_dir"
    if [[ "$gate_exit" -eq 0 ]] && { [[ "$nonce_line" != "nonce=$nonce" ]] || [[ "$verdict" != "proceed" ]]; }; then
        printf 'The pre-deploy gate exited 0 without writing this run verdict; %s did not run the gate.\n' "$gate_python" >&2
        exit 1
    fi
    if [[ "$gate_exit" -eq "$GATE_NEEDS_ACKNOWLEDGEMENT" ]]; then
        if [[ "$gate_output" =~ --skip-reading-confirmation=([^[:space:]]+) ]]; then
            printf 'Read every document listed above, then run:\n  bash .claude/provision.sh --skip-reading-confirmation=%s\n' "${BASH_REMATCH[1]}" >&2
        fi
        exit "$GATE_NEEDS_ACKNOWLEDGEMENT"
    fi
    exit "$gate_exit"
)

# The target as a clone of its own in the stage, the one tag fetched from the
# fixed URL. Nothing of the live clone, its git directory included, is written.
# A stage an earlier, killed sync left behind is replaced. Fails, and exits,
# when the tag cannot be fetched or does not name its own version.
_provision_stage_target() {
    local -a stage_git=(git -C "$SYNC_STAGE_DIR")
    _provision_stage_remove
    mkdir -p "$SYNC_STAGE_DIR"
    if ! "${stage_git[@]}" -c init.defaultBranch=main init -q \
        || ! "${stage_git[@]}" remote add origin "$PROVISION_REPO_URL"; then
        _provision_stage_remove
        _provision_fail "could not create a git checkout at $SYNC_STAGE_DIR." "The clone was not changed."
    fi
    if ! "${stage_git[@]}" fetch -q --depth 1 origin "refs/tags/$TAG:refs/tags/$TAG"; then
        _provision_stage_remove
        _provision_fail "could not fetch tag $TAG from $PROVISION_REPO_URL." \
            "Check that $TAG is a released version and that this machine can reach the repository." \
            "The clone was not changed."
    fi
    local tag_source tag_version=""
    if ! tag_source="$("${stage_git[@]}" show "refs/tags/$TAG:src/claude_code_hooks_daemon/version.py")"; then
        _provision_stage_remove
        _provision_fail "tag $TAG has no src/claude_code_hooks_daemon/version.py." \
            "The clone was not changed. Report this: a release tag should name its own version."
    fi
    if ! tag_version="$(_provision_version_in_text "$tag_source")"; then
        tag_version=""
    fi
    if [[ "$tag_version" != "$VERSION" ]]; then
        _provision_stage_remove
        _provision_fail "tag $TAG carries daemon version ${tag_version:-unreadable}, not $VERSION." \
            "The clone was not changed. Report this: a release tag should name its own version."
    fi
    if ! "${stage_git[@]}" -c advice.detachedHead=false checkout -q "refs/tags/$TAG"; then
        _provision_sync_abort "could not check out $TAG in the stage at $SYNC_STAGE_DIR."
    fi
}

# Replace each FROM with its TO in a text file, byte for byte, keeping its mode.
# Args: the file, then FROM TO pairs, applied in order.
_provision_replace_in_file() (
    LC_ALL=C
    local file="$1" content
    shift
    content="$(cat -- "$file" && printf x)" || exit 1
    content="${content%x}"
    while (($# >= 2)); do
        content="${content//"$1"/"$2"}"
        shift 2
    done
    printf '%s' "$content" > "$file"
)

# Rebind the stage's venv to the live path the stage is about to be renamed to.
#
# A venv is bound to the absolute path it was built at in two ways. Its NAME:
# the resolver looks for venv-<path slug>-<interpreter> under the clone. And
# TEXT files that spell the path: the editable install's .pth (<stage>/src) and
# direct_url.json, console-script shebangs, the activate scripts and
# .daemon-metadata.json. Nothing else holds it: bin/python links to the base
# interpreter, outside both paths, and a cached .pyc is not trusted for its
# path (importlib gives the code object the path it was loaded from). So the
# stage's path becomes the live one in every text file of the venv, and the
# venv takes the live path's name; renaming the whole stage onto the live path
# then makes every path it names true. Returns 1 when the venv is not the one
# the stage's build made, or a file still names the stage.
_provision_rebind_stage_venv() {
    local fingerprints="$SYNC_STAGE_DIR/scripts/install/python_fingerprint.sh"
    local -a built
    shopt -s nullglob
    built=("$SYNC_STAGE_DIR"/untracked/venv-*)
    shopt -u nullglob
    if [[ "${#built[@]}" -ne 1 || ! -f "$fingerprints" ]]; then
        _provision_say "The stage holds ${#built[@]} venvs, or no scripts/install/python_fingerprint.sh; expected one venv it built."
        return 1
    fi
    local venv="${built[0]}" stage_fp live_fp stage_path live_path stage_real live_real
    # shellcheck source=/dev/null
    stage_fp="$(source "$fingerprints" && python_venv_fingerprint "$venv/bin/python" "$SYNC_STAGE_DIR")" || return 1
    # shellcheck source=/dev/null
    live_fp="$(source "$fingerprints" && python_venv_fingerprint "$venv/bin/python" "$DAEMON_DIR")" || return 1
    if [[ "${venv##*/}" != "venv-$stage_fp" ]]; then
        _provision_say "The stage's venv is ${venv##*/}, not venv-$stage_fp, the one its build names."
        return 1
    fi
    stage_path="$(cd "$SYNC_STAGE_DIR" && pwd)" && live_path="$(cd "$DAEMON_DIR" && pwd)" || return 1
    stage_real="$(cd "$SYNC_STAGE_DIR" && pwd -P)" && live_real="$(cd "$DAEMON_DIR" && pwd -P)" || return 1
    local -a pairs=(
        "$stage_path/untracked/venv-$stage_fp" "$live_path/untracked/venv-$live_fp"
        "$stage_real/untracked/venv-$stage_fp" "$live_real/untracked/venv-$live_fp"
        "$stage_path" "$live_path"
        "$stage_real" "$live_real"
        "$stage_fp" "$live_fp"
    )
    local file
    local -a files=()
    while IFS= read -r -d '' file; do
        files+=("$file")
    done < <(grep -rlIF --null -e "$stage_path" -e "$stage_real" -- "$venv")
    for file in ${files[@]+"${files[@]}"}; do
        _provision_replace_in_file "$file" "${pairs[@]}" || return 1
    done

    SYNC_NEW_VENV_NAME="venv-$live_fp"
    local rebound="$SYNC_STAGE_DIR/untracked/$SYNC_NEW_VENV_NAME" search=0
    if [[ "$venv" != "$rebound" ]]; then
        _provision_rename "$venv" "$rebound" || return 1
    fi
    grep -rqIF -e "$stage_path" -e "$stage_real" -- "$rebound" || search=$?
    if ((search != 1)); then
        _provision_say "A file in $rebound still names the stage, or the venv could not be searched (grep exit $search)."
        return 1
    fi
    _provision_say "The venv for $TAG was rebound to $live_path (${#files[@]} files named the stage)."
}

# Can the new daemon run from the venv the swap put in place? init.sh's own
# pre-start check (validate_venv: the resolver, then the package import), and
# two facts the swap must have made true: the resolver chose the rebound venv,
# and the package loads from the live clone's src. Prints why not.
_provision_venv_healthy() (
    local expected="$DAEMON_DIR/untracked/$SYNC_NEW_VENV_NAME/bin/python" loaded loaded_dir live_src
    PYTHON_CMD=""
    if ! validate_venv; then
        printf '%s' "$VENV_ERROR"
        exit 1
    fi
    if [[ "$PYTHON_CMD" != "$expected" ]]; then
        printf 'the resolver chose %s, not the relocated %s' "$PYTHON_CMD" "$expected"
        exit 1
    fi
    if ! loaded="$("$PYTHON_CMD" -c 'import claude_code_hooks_daemon; print(claude_code_hooks_daemon.__file__)')"; then
        printf 'claude_code_hooks_daemon could not be imported to say where it loads from'
        exit 1
    fi
    live_src="$(cd "$DAEMON_DIR/src" && pwd -P)" || exit 1
    if ! loaded_dir="$(cd "${loaded%/*}" && pwd -P)" || [[ "$loaded_dir/" != "$live_src/"* ]]; then
        printf 'claude_code_hooks_daemon loads from %s, not from %s' "$loaded" "$live_src"
        exit 1
    fi
)

# Swap the staged target in for the live clone and start its daemon; the old
# clone goes only once the new daemon answers. See "The switch to another
# version" above for the renames, and _provision_recover_switch for their undo.
_provision_switch() {
    local live_untracked="$DAEMON_DIR/untracked" stage_untracked="$SYNC_STAGE_DIR/untracked"
    local marker="$_HOOKS_DAEMON_SWITCH_MARKER_NAME" entry name
    local manifest="$stage_untracked/$SYNC_CARRIED_MANIFEST"

    # The marker keeps hooks from starting a daemon until the new one is up. It
    # is held in the live untracked/ and in the stage's, which becomes it. The
    # manifest records that the switch has begun, and what it carried.
    SYNC_PHASE="switching"
    printf '%s\n' "$$" > "$live_untracked/$marker"
    printf '%s\n' "$$" > "$stage_untracked/$marker"
    : > "$manifest"
    _provision_say "Stopping v$PRESENT_VERSION to switch to $TAG ..."
    if ! _provision_stop_daemon; then
        _provision_sync_abort "the running daemon v$PRESENT_VERSION did not stop, so the clone was not switched."
    fi

    # The stage keeps only the rebound venv of what its build left in untracked/.
    # The old clone's untracked/, the daemon's state, moves into it, except the
    # old venv, which stays with the old clone.
    local -a stage_entries live_entries
    shopt -s nullglob dotglob
    stage_entries=("$stage_untracked"/*)
    live_entries=("$live_untracked"/*)
    shopt -u nullglob dotglob
    for entry in ${stage_entries[@]+"${stage_entries[@]}"}; do
        case "${entry##*/}" in
            "$SYNC_NEW_VENV_NAME" | "$marker" | "$SYNC_CARRIED_MANIFEST") ;;
            *) rm -rf -- "$entry" ;;
        esac
    done
    for entry in ${live_entries[@]+"${live_entries[@]}"}; do
        name="${entry##*/}"
        case "$name" in
            "$SYNC_STAGE_DIR_NAME" | "$marker" | "$SYNC_OLD_VENV_NAME") continue ;;
        esac
        if [[ -e "$stage_untracked/$name" || -L "$stage_untracked/$name" ]]; then
            continue
        fi
        printf '%s\n' "$name" >> "$manifest"
        if ! mv -- "$entry" "$stage_untracked/$name"; then
            _provision_sync_abort "could not move $entry into the stage."
        fi
    done

    if ! _provision_rename "$DAEMON_DIR" "$SYNC_ASIDE_DIR" \
        || ! _provision_rename_onto_live "$SYNC_ASIDE_DIR/untracked/$SYNC_STAGE_DIR_NAME" \
        || ! _provision_rename "$SYNC_ASIDE_DIR" "$SYNC_PREVIOUS_DIR"; then
        _provision_sync_abort "could not swap the staged $TAG in for the clone."
    fi

    # The rebind is proven on the uv of today only: a path a later uv writes
    # where a text rewrite cannot see it fails this check, and the venv is then
    # rebuilt in place by the clone's own build path, with the daemon down.
    local unhealthy rebuilt_unhealthy=""
    if ! unhealthy="$(_provision_venv_healthy)"; then
        _provision_say "The relocated venv failed its check: $unhealthy"
        _provision_say "Rebuilding it in place with the clone's scripts/venv_bootstrap.sh (the daemon stays down meanwhile) ..."
        rm -rf -- "$live_untracked/${SYNC_NEW_VENV_NAME:?}"
        if ! bash "$DAEMON_DIR/scripts/venv_bootstrap.sh" repair "$DAEMON_DIR" \
            || ! rebuilt_unhealthy="$(_provision_venv_healthy)"; then
            _provision_sync_abort "the relocated venv failed its check ($unhealthy), and rebuilding it in place failed${rebuilt_unhealthy:+ ($rebuilt_unhealthy)}."
        fi
        SYNC_VENV_NOTE="the relocated venv failed its check ($unhealthy) and was rebuilt in place"
    fi

    # restart returns 0 once the started daemon answers on its socket; its PID
    # file is checked as well.
    _provision_say "Starting the daemon at $TAG ..."
    if ! bash "$DAEMON_DIR/bin/hooks-daemon" restart; then
        _provision_sync_abort "the daemon did not start at $TAG."
    fi
    local running=0
    _provision_daemon_running || running=$?
    if ((running != 0)); then
        _provision_sync_abort "the daemon at $TAG did not answer after it was started."
    fi
    rm -f "$live_untracked/$marker" "$manifest"
    # Renamed first, so a removal cut short is never taken for an old clone to put back.
    if mv -- "$SYNC_PREVIOUS_DIR" "$SYNC_PREVIOUS_DIR.discard"; then
        rm -rf -- "$SYNC_PREVIOUS_DIR.discard"
    else
        _provision_say "WARNING: the old clone at $SYNC_PREVIOUS_DIR could not be removed; delete it by hand."
    fi
}

# Undo a sync that failed, and fail loudly. Before the switch nothing live was
# touched: only the stage goes. After it began: the old clone, its venv and the
# daemon's state are put back and its daemon started again.
_provision_sync_abort() {
    local what="$1"
    if [[ "$SYNC_PHASE" == "staged" ]]; then
        _provision_stage_remove
        _provision_fail "$what" \
            "Nothing in the clone changed, and the running daemon v$PRESENT_VERSION was never stopped. No tracked file was changed."
    fi
    if ! _provision_recover_switch; then
        _provision_fail "$what" \
            "The previous version v$PRESENT_VERSION could NOT be put back (the reason is above)." \
            "Run this again: it retries putting it back first. No tracked file was changed."
    fi
    _provision_fail "$what" "The previous version v$PRESENT_VERSION is back in place. No tracked file was changed."
}

_provision_sync_clone() {
    _provision_say "The daemon clone is at v$PRESENT_VERSION and this project expects $TAG: syncing it."
    _provision_sync_preflight

    # In the stage, beside the live daemon: the target's clone, its gate, and
    # its venv, built and rebound to the live path. Nothing the running daemon
    # imports from is touched, so any failure here leaves it serving as it was.
    _provision_say "Fetching $TAG from $PROVISION_REPO_URL into $SYNC_STAGE_DIR ..."
    _provision_stage_target

    local gate_status=0
    _provision_gate || gate_status=$?
    if [[ "$gate_status" -eq "$GATE_NEEDS_ACKNOWLEDGEMENT" ]]; then
        _provision_stage_remove
        _provision_say ""
        _provision_say "PROVISION STOPPED before changing anything: the target release asks for its reading list to be confirmed."
        _provision_say "The clone is still at v$PRESENT_VERSION; no venv was built and the daemon was not stopped."
        exit "$GATE_NEEDS_ACKNOWLEDGEMENT"
    elif [[ "$gate_status" -ne 0 ]]; then
        _provision_sync_abort "the pre-deploy gate of $TAG could not decide (its output is above)."
    fi

    local stage_bootstrap="$SYNC_STAGE_DIR/scripts/venv_bootstrap.sh"
    if [[ ! -f "$stage_bootstrap" ]]; then
        _provision_sync_abort "$TAG predates the venv build driver (scripts/venv_bootstrap.sh)."
    fi
    _provision_say "Building the venv for $TAG beside the running daemon (this can take a while on a cold cache) ..."
    if ! bash "$stage_bootstrap" repair "$SYNC_STAGE_DIR"; then
        _provision_sync_abort "the venv for $TAG could not be built (the output above says which condition failed)."
    fi
    if ! _provision_rebind_stage_venv; then
        _provision_sync_abort "the venv built for $TAG could not be rebound to $DAEMON_DIR (the reason is above)."
    fi

    _provision_switch

    _provision_say ""
    _provision_say "SYNCED: the hooks daemon moved from v$PRESENT_VERSION to $TAG and was restarted."
    if [[ -n "$SYNC_VENV_NOTE" ]]; then
        _provision_say "Note: $SYNC_VENV_NOTE."
    fi
    _provision_say "No tracked file was changed."
}

# ------------------------------------------------------------------
# The background sync (Plan 00477 Task 5.2)
# ------------------------------------------------------------------
SYNC_STATE_DIR=""
SYNC_STATE_FILE=""
SYNC_LOCK_DIR=""
# The session-start forwarder starts the sync while that same hook may still be
# starting the old daemon. The sync waits this long for it, so it never moves a
# clone a daemon is starting from, and the old one serves until the restart.
readonly BACKGROUND_DAEMON_WAIT_SECONDS=30

# Is a daemon running from the clone? Waits up to BACKGROUND_DAEMON_WAIT_SECONDS.
_provision_background_daemon_up() {
    local deadline=$((SECONDS + BACKGROUND_DAEMON_WAIT_SECONDS)) running
    while true; do
        running=0
        _provision_daemon_running || running=$?
        if ((running == 0)); then
            return 0
        fi
        if ((SECONDS >= deadline)); then
            return 1
        fi
        sleep 1
    done
}

# Record the background sync's state: status, then a one-line detail.
_provision_background_record() {
    local tmp="$SYNC_STATE_FILE.tmp.$$"
    {
        printf 'status=%s\n' "$1"
        printf 'target=%s\n' "$VERSION"
        printf 'from=%s\n' "$PRESENT_VERSION"
        printf 'pid=%s\n' "$$"
        printf 'detail=%s\n' "${2//$'\n'/ }"
    } > "$tmp" && mv -f "$tmp" "$SYNC_STATE_FILE"
}

# Take the lock every background sync holds, whatever its target. A lock whose
# holder has gone is taken over. Returns 1 while a live sync holds it.
_provision_background_lock() {
    local holder=""
    mkdir -p "$SYNC_STATE_DIR"
    if mkdir "$SYNC_LOCK_DIR" 2> /dev/null; then
        printf '%s\n' "$$" > "$SYNC_LOCK_DIR/pid"
        return 0
    fi
    if [[ -f "$SYNC_LOCK_DIR/pid" ]]; then
        IFS= read -r holder < "$SYNC_LOCK_DIR/pid" || [[ -n "$holder" ]]
    fi
    if [[ "$holder" =~ ^[0-9]+$ ]] && kill -0 "$holder" 2> /dev/null; then
        _provision_say "A background sync is already running (pid $holder); this one stops without changing anything."
        return 1
    fi
    _provision_say "Taking over the background sync lock from pid ${holder:-unknown}, which is no longer running."
    rm -rf "$SYNC_LOCK_DIR"
    if ! mkdir "$SYNC_LOCK_DIR"; then
        _provision_say "Another background sync took the lock first; this one stops without changing anything."
        return 1
    fi
    printf '%s\n' "$$" > "$SYNC_LOCK_DIR/pid"
}

# On exit: record the outcome from the exit status, then release the lock.
_provision_background_finish() {
    local rc="$1"
    case "$rc" in
        0)
            _provision_background_record synced \
                "moved from v$PRESENT_VERSION to $TAG and restarted${SYNC_VENV_NOTE:+; $SYNC_VENV_NOTE}"
            ;;
        "$GATE_NEEDS_ACKNOWLEDGEMENT")
            _provision_background_record needs-reading \
                "$TAG asks for its upgrade guides to be read and confirmed before it is deployed; nothing was changed"
            ;;
        *)
            _provision_background_record failed "${_PROVISION_FAIL_REASON:-provision exited $rc without naming a reason}"
            ;;
    esac
    rm -rf "$SYNC_LOCK_DIR"
}

# Claim the sync to $VERSION: forward only, under the lock, once per target.
# Exits 0 without changing anything when another sync holds the lock or this
# target was already attempted; from here on, every exit records an outcome.
_provision_background_claim() {
    if _version_lt "$VERSION" "$PRESENT_VERSION"; then
        _provision_fail "a background sync never downgrades: $TAG is older than the installed v$PRESENT_VERSION." \
            "Confirm the downgrade is intended, then run by hand: bash .claude/provision.sh"
    fi
    if ! _provision_background_daemon_up; then
        _provision_say "No daemon is running from the clone after ${BACKGROUND_DAEMON_WAIT_SECONDS}s, so there is none to keep serving during a sync; nothing was changed, and a later session start tries again."
        exit 0
    fi
    SYNC_STATE_DIR="$DAEMON_DIR/untracked/$SYNC_STATE_DIR_NAME"
    SYNC_STATE_FILE="$SYNC_STATE_DIR/v$VERSION.state"
    SYNC_LOCK_DIR="$SYNC_STATE_DIR/lock.d"
    _provision_background_lock || exit 0
    if [[ -e "$SYNC_STATE_FILE" ]]; then
        rm -rf "$SYNC_LOCK_DIR"
        _provision_say "A background sync to $TAG was already attempted ($SYNC_STATE_FILE); it is not repeated."
        _provision_say "To sync by hand, run: bash .claude/provision.sh"
        exit 0
    fi
    _provision_background_record running "syncing from v$PRESENT_VERSION to $TAG"
    trap '_provision_background_finish $?' EXIT
    _provision_say "Syncing to $TAG in the background; the output is in $SYNC_STATE_DIR/v$VERSION.log"
    exec >> "$SYNC_STATE_DIR/v$VERSION.log" 2>&1
}

# ------------------------------------------------------------------
# Step 2: is this a fresh checkout?
# ------------------------------------------------------------------
if _daemon_clone_present; then
    # The resolved interpreter must also exist on disk, so it is judged as well
    # as the status of _resolve_python_cmd.
    PYTHON_CMD=""
    if ! _resolve_python_cmd; then
        PYTHON_CMD=""
    fi
    if [[ -n "$PYTHON_CMD" && -f "$PYTHON_CMD" ]]; then
        if ! PRESENT_VERSION="$(_clone_version)"; then
            _provision_fail "a daemon clone is already here, but its version.py cannot be read, so it cannot be compared with $TAG." \
                "Repair it first: $DAEMON_DIR/bin/hooks-daemon repair" \
                "To move the whole project to another version, use: /hooks-daemon upgrade"
        fi
        if [[ "$PRESENT_VERSION" == "$VERSION" ]]; then
            _provision_say "The daemon clone is already at $TAG, the expected version: nothing to do."
            exit 0
        fi
        if [[ "$BACKGROUND" == true ]]; then
            _provision_background_claim
        fi
        _provision_sync_clone
        exit 0
    fi
    _provision_fail "a daemon clone is already here, but no venv resolves for this project path." \
        "That is a different repair, and it keeps the clone: run" \
        "  $DAEMON_DIR/bin/hooks-daemon repair" \
        "Then run this again: if the clone is at the wrong version it is synced to $TAG."
fi

# Fresh provisioning is run deliberately, by a person or an agent (the owner's
# ruling): the background sync only ever moves an existing clone.
if [[ "$BACKGROUND" == true ]]; then
    _provision_fail "--background only syncs an existing clone, and there is no daemon clone here." \
        "Provision this checkout by running: bash .claude/provision.sh"
fi

# Anything in the daemon directory besides the untracked/ skeleton (or a venv
# under it) is someone work, or a clone too damaged to recognise. Neither is
# ours to overwrite.
_provision_unexpected_entries() {
    local entry
    shopt -s nullglob dotglob
    for entry in "$DAEMON_DIR"/*; do
        [[ "$(basename "$entry")" == "untracked" ]] && continue
        printf '%s\n' "$entry"
    done
    shopt -u nullglob dotglob
}
_PROVISION_UNEXPECTED="$(_provision_unexpected_entries)"
if [[ -n "$_PROVISION_UNEXPECTED" ]] || _daemon_orphan_venv_present; then
    _provision_fail "$DAEMON_DIR is not empty, and is not a clone provision recognises." \
        "Provision never overwrites what it did not create. Inspect it, move it aside" \
        "  if it is disposable, and run this again."
fi

# ------------------------------------------------------------------
# Step 3: fetch exactly the tag
# ------------------------------------------------------------------
# init + fetch rather than clone: the directory already holds the untracked/
# skeleton init.sh made, and clone refuses a non-empty directory. The result is
# the same as a depth 1 clone of the tag: a detached checkout with origin set.
# Removing what this step made is safe because the check above established that
# nothing but the skeleton was there before it.
_provision_abort_clone() {
    local entry
    shopt -s nullglob dotglob
    for entry in "$DAEMON_DIR"/*; do
        [[ "$(basename "$entry")" == "untracked" ]] && continue
        rm -rf "$entry"
    done
    shopt -u nullglob dotglob
}

_provision_say "Fetching $TAG from $PROVISION_REPO_URL ..."
mkdir -p "$DAEMON_DIR"
git -C "$DAEMON_DIR" -c init.defaultBranch=main init -q
git -C "$DAEMON_DIR" remote add origin "$PROVISION_REPO_URL"
if ! git -C "$DAEMON_DIR" fetch -q --depth 1 origin "refs/tags/$TAG:refs/tags/$TAG"; then
    _provision_abort_clone
    _provision_fail "could not fetch tag $TAG from $PROVISION_REPO_URL." \
        "Check that $TAG is a released version and that this machine can reach the repository." \
        "Nothing was installed."
fi
if ! git -C "$DAEMON_DIR" -c advice.detachedHead=false checkout -q "refs/tags/$TAG"; then
    _provision_abort_clone
    _provision_fail "could not check out tag $TAG." "Nothing was installed."
fi

CLONE_VERSION=""
if ! CLONE_VERSION="$(_clone_version)"; then
    CLONE_VERSION=""
fi
if [[ "$CLONE_VERSION" != "$VERSION" ]]; then
    _provision_abort_clone
    _provision_fail "tag $TAG carries daemon version ${CLONE_VERSION:-unreadable}, not $VERSION." \
        "Nothing was installed. Report this: a release tag should name its own version."
fi

# ------------------------------------------------------------------
# Step 4: the venv, through the clone own build path and lock
# ------------------------------------------------------------------
BOOTSTRAP="$DAEMON_DIR/scripts/venv_bootstrap.sh"
if [[ ! -f "$BOOTSTRAP" ]]; then
    _provision_abort_clone
    _provision_fail "$TAG predates the venv build driver (scripts/venv_bootstrap.sh)." \
        "Nothing was installed. Set daemon.expected_version to a newer release, or use /hooks-daemon upgrade."
fi
_provision_say "Building the venv (this can take a while on a cold cache) ..."
if ! bash "$BOOTSTRAP" repair "$DAEMON_DIR"; then
    _provision_fail "the clone is in place at $TAG but its venv could not be built." \
        "The output above says which condition failed. Once fixed, run:" \
        "  $DAEMON_DIR/bin/hooks-daemon repair" \
        "No tracked file was changed."
fi

# ------------------------------------------------------------------
# Step 5: start the daemon, the way a hook would
# ------------------------------------------------------------------
# start_daemon waits until a deadline measured from the start of the script,
# sized for a hook. A build before it would have used that budget up.
_HOOKS_DAEMON_START_DEADLINE=$((SECONDS + 60))
PYTHON_CMD=""
if ! start_daemon; then
    _provision_fail "the clone and venv are in place at $TAG but the daemon did not start." \
        "Try: $DAEMON_DIR/bin/hooks-daemon restart" \
        "Then: $DAEMON_DIR/bin/hooks-daemon logs"
fi

_provision_say ""
_provision_say "PROVISIONED: hooks daemon $TAG is running for this checkout."
_provision_say "No tracked file was changed."
SETTINGS="$PROJECT_PATH/.claude/settings.json"
if [[ -f "$SETTINGS" ]] && grep -q '\.claude/hooks/' "$SETTINGS"; then
    _provision_say "No session restart is needed: the hooks are already registered in"
    _provision_say ".claude/settings.json, and the next tool call reaches the running daemon."
else
    _provision_say "WARNING: .claude/settings.json does not register the hooks, so nothing will"
    _provision_say "call this daemon yet. That file is tracked and belongs to the project;"
    _provision_say "restore it from git or ask whoever maintains the project."
fi
