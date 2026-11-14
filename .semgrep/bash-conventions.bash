#!/bin/bash
# Semgrep test cases for .semgrep/bash-conventions.yml
#
# Annotation convention (semgrep's test annotations): a line tagged with a rule id
# expects a finding on the following line; a line tagged as allowed expects no
# finding on the following line. The annotation must sit immediately above the
# line it refers to. This fixture lives beside the rule file so semgrep can
# pair them (a tests/ subdir crashes semgrep's matcher). It is intentionally
# full of error-hiding patterns; semgrep skips dot-directories in normal scans,
# so qa-patterns.bash never flags this file.
#
# For the same reason the repo's own gates exempt it:
#   1. qa-bash.bash discovery excludes .semgrep/
#   2. qa-patterns.bash passes --exclude '.semgrep'
#
# The hooks daemon's lint_on_edit does NOT exempt it, and cannot be made to:
# both a per-handler options.exclude_paths and the project-wide
# daemon.exclude_paths were tried and neither is honoured (verified, not
# assumed — the inert config was removed rather than left in place claiming an
# effect it does not have). So editing this file prints a wall of shellcheck
# findings every time. The edits still land; it is noise, not a blocked write.
#
# Do NOT "fix" the findings in this file. They ARE the tests.

# --- bash-error-hiding-pipe-echo ---

# ok: bash-error-hiding-pipe-echo
if ! some_cmd; then
    echo "ERROR: some_cmd failed" >&2
    exit 1
fi

# ok: bash-error-hiding-pipe-echo
some_cmd

# ok: bash-error-hiding-pipe-echo
VAR=$(some_cmd)

# ruleid: bash-error-hiding-pipe-echo
some_cmd || echo "WARNING: something failed"

# ruleid: bash-error-hiding-pipe-echo
another_cmd || echo "something went wrong"

# ruleid: bash-error-hiding-pipe-echo
default_var=$(some_cmd || echo "default")

# ruleid: bash-error-hiding-pipe-echo
result=$(load_preference 2>/dev/null || echo "")

# Regression cases for Plan 00075: the old `$CMD || echo $MSG` pattern bound a
# bare command word, so every one of these — the shapes that actually occur —
# went unreported.

# ruleid: bash-error-hiding-pipe-echo
some_cmd with args || echo "command WITH arguments"

# ruleid: bash-error-hiding-pipe-echo
tracked_files=$(git ls-files "$ccy_dir" 2>/dev/null || echo "")

# ruleid: bash-error-hiding-pipe-echo
defaulted=$(some_cmd arg1 arg2 || echo "default")

# A ternary is a value expression, not error hiding.
# ok: bash-error-hiding-pipe-echo
state=$([ -f /etc/hostname ] && echo YES || echo NO)

# ok: bash-error-hiding-pipe-echo
annotated_cmd arg || echo "(none found)"  # FAIL-FAST-OK: user-facing empty-list text, not a failure path

# --- bash-error-hiding-or-true ---

# ruleid: bash-error-hiding-or-true
umount /mnt 2>/dev/null || true

# ruleid: bash-error-hiding-or-true
some_cmd || :

# ruleid: bash-error-hiding-or-true
flush_buffers || true  # a trailing comment does not exempt the line

# ok: bash-error-hiding-or-true
if ! umount /mnt 2>/dev/null; then echo "note: already unmounted" >&2; fi

# ok: bash-error-hiding-or-true
count=$(( count + 1 ))

# ok: bash-error-hiding-or-true
root_source=$(findmnt -no SOURCE / 2>/dev/null) || root_source=""

# --- bash-status-after-block ---
#
# The exact shape from Plan 00074's prototype.bash: every legitimate grep
# no-match was reported as "grep failed (exit 0)".
#
# NOTE ON ANNOTATION PLACEMENT: the match begins at the block TERMINATOR, so
# semgrep reports the finding on the `fi`/`done`/`esac` line and the `ruleid`
# comment must sit immediately above THAT line — not above the opening `if`.

if out="$(grep -ai pattern /etc/hostname)"; then
    printf '%s\n' "$out"
# ruleid: bash-status-after-block
fi
rc=$?

for f in a b; do
    process "$f"
# ruleid: bash-status-after-block
done
loop_rc=$?

case "$x" in
    a) ;;
# ruleid: bash-status-after-block
esac
case_rc=$?

if check_thing; then
    :
# ruleid: bash-status-after-block
fi
# a comment between them does not exempt it
commented_rc=$?

# The correct forms: capture the status where it is produced.

if out="$(grep -ai pattern /etc/hostname)"; then
    printf '%s\n' "$out"
# ok: bash-status-after-block
else
    else_rc=$?
    echo "grep failed: $else_rc" >&2
fi

# ok: bash-status-after-block
some_cmd
immediate_rc=$?

# --- bash-capture-discards-status ---
#
# The exact shape that took ccy down during a GitHub outage (CCY 3.36.0):
# a 502 error body arrived on stdout and became "the account name".

# ruleid: bash-capture-discards-status
token_user=$(gh api user --jq .login 2>/dev/null)

# ruleid: bash-capture-discards-status
local mtime=$(stat -c %Y "$file" 2>/dev/null)

# ruleid: bash-capture-discards-status
export BRANCH="$(git rev-parse --abbrev-ref HEAD 2>/dev/null)"

# A trailing comment is NOT a justification. The rule's regex once anchored `$`
# straight after the closing paren, so any comment at all evaded it and the
# FAIL-FAST-OK exclusion below was decorative. Caught with a probe, fixed, and
# pinned here so it cannot regress.
# ruleid: bash-capture-discards-status
commented_but_unjustified=$(gh api user --jq .login 2>/dev/null)  # just a note

# ok: bash-capture-discards-status
fallback_var=$(findmnt -no SOURCE / 2>/dev/null) || fallback_var=""

# ok: bash-capture-discards-status
if checked_var=$(gh api user --jq .login 2>/dev/null); then
    echo "$checked_var"
fi

# ok: bash-capture-discards-status
kept_stderr=$(gh api user --jq .login 2>&1)

# A trailing comment is NOT a justification. The rule's regex once anchored `$`
# straight after the closing paren, so any comment at all evaded it and the
# FAIL-FAST-OK exclusion below was decorative. Caught with a probe, fixed, and
# pinned here so it cannot regress.
# ruleid: bash-capture-discards-status
commented_but_unjustified=$(gh api user --jq .login 2>/dev/null)  # just a note

# Only the explicit, greppable annotation grants the exemption.
# ok: bash-capture-discards-status
annotated_var=$(gh api user --jq .login 2>/dev/null)  # FAIL-FAST-OK: absence is the expected case here

# --- bash-test-discards-status ---
#
# The assignment rule above only sees `var=$(...)`. The same defect inside a
# TEST walked past it, which is how the docker-in-lxc sysctl bug survived a
# sweep that was specifically looking for this class. This is that exact line,
# as it was before the fix.
# ruleid: bash-test-discards-status
if [ "$(sysctl -n net.ipv4.ip_forward 2>/dev/null)" != "1" ]; then
    echo "sysctl: net.ipv4.ip_forward is not set to 1"
fi

# The `[[ ]]` form is the same defect.
# ruleid: bash-test-discards-status
if [[ "$(mokutil --sb-state 2>/dev/null)" == *enabled* ]]; then
    echo "secure boot on"
fi

# The legitimate case: an empty result genuinely MEANS absent, and the line says
# so where a reader and a grep will both find it.
# ok: bash-test-discards-status
if [ -z "$(ls -A "$dir"/*.token 2>/dev/null)" ]; then  # FAIL-FAST-OK: no glob match IS "no tokens"
    echo "no tokens"
fi

# Capturing first, then testing the status, is the fix — and must not be flagged.
# ok: bash-test-discards-status
if ! value=$(sysctl -n net.ipv4.ip_forward 2>&1); then
    echo "could not read it: $value"
elif [ "$value" != "1" ]; then
    echo "it is $value, expected 1"
fi

# --- pipe-into-grep-q-under-pipefail ---

# ruleid: pipe-into-grep-q-under-pipefail
printf '%s\n' "${items[@]}" | grep -qxF -- "$item"

# ruleid: pipe-into-grep-q-under-pipefail
some_cmd | grep -E -q 'needle'

# ruleid: pipe-into-grep-q-under-pipefail
some_cmd | grep --quiet 'needle'

# ruleid: pipe-into-grep-q-under-pipefail
some_cmd | grep -q 'needle'  # FAIL-FAST-OK:

# ok: pipe-into-grep-q-under-pipefail
some_cmd | grep -E 'needle'

# ok: pipe-into-grep-q-under-pipefail
grep -qxF -- "$item" <<<"$(printf '%s\n' "${items[@]}")"

# ok: pipe-into-grep-q-under-pipefail
some_cmd || grep -q 'needle' file

# ok: pipe-into-grep-q-under-pipefail
some_cmd | grep -q 'needle'  # FAIL-FAST-OK: a one-line producer cannot be killed

# --- ccy-container-default-pids-limit ---

# ruleid: ccy-container-default-pids-limit
podman run -it --rm --name "$NAME" --label ccy=true "$IMAGE" claude

# ruleid: ccy-container-default-pids-limit
CLAUDE_CODE_OAUTH_TOKEN="$T" ccy_agent_container run $DOCKER_FLAGS --rm \
    --name "$CONTAINER_NAME" \
    --label ccy=true \
    "$IMAGE_NAME" \
    "${CCY_AGENT_CMD[@]}" || container_rc=$?

# ruleid: ccy-container-default-pids-limit
"$CONTAINER_ENGINE" run -d "$IMAGE"

# ruleid: ccy-container-default-pids-limit
${ENGINE} create "$IMAGE"

# ruleid: ccy-container-default-pids-limit
exec docker container run -it \
    "$IMAGE"

# ruleid: ccy-container-default-pids-limit
if claude_help=$(container_cmd run --rm --entrypoint claude "$IMAGE" --help 2>&1); then :; fi

# ruleid: ccy-container-default-pids-limit
if ! container_cmd run \
    --name "$temp_name" \
    --entrypoint sh "$IMAGE" -c 'npm install -g x'; then :; fi

# ruleid: ccy-container-default-pids-limit
GH_TOKEN="$gh_token" container_cmd run -it --rm \
    --entrypoint claude "$IMAGE" setup-token 2>&1 | tee "$out"

# ruleid: ccy-container-default-pids-limit
probe="$(container_cmd run --rm --entrypoint claude "$1" --version 2>&1)"

# ruleid: ccy-container-default-pids-limit
echo hi && podman run -i "$IMAGE"

# ruleid: ccy-container-default-pids-limit
podman run -it "$IMAGE"; podman run --pids-limit 10 x

# ruleid: ccy-container-default-pids-limit
podman run -it "$IMAGE" && podman run --pids-limit 10 x

# ruleid: ccy-container-default-pids-limit
podman run -it "$IMAGE"  # TODO add --pids-limit

# ruleid: ccy-container-default-pids-limit
podman run -it --pids-limit -1 "$IMAGE"

# ruleid: ccy-container-default-pids-limit
podman run -it --pids-limit "-1" "$IMAGE"

# ruleid: ccy-container-default-pids-limit
podman run -it --pids-limit=0 \
    "$IMAGE"

# ruleid: ccy-container-default-pids-limit
podman run -it --pids-limit 00 "$IMAGE"

# ruleid: ccy-container-default-pids-limit
podman run -it --pids-limit max "$IMAGE"

# ok: ccy-container-default-pids-limit
CLAUDE_CODE_OAUTH_TOKEN="$T" ccy_agent_container run $DOCKER_FLAGS --rm --pids-limit "$CCY_PIDS_LIMIT" \
    --name "$CONTAINER_NAME" \
    "$IMAGE_NAME" || container_rc=$?

# ok: ccy-container-default-pids-limit
podman run -it --rm \
    --pids-limit=16384 "$IMAGE"

# ok: ccy-container-default-pids-limit
if claude_help=$(container_cmd run --rm --pids-limit "$CCY_PIDS_LIMIT" --entrypoint claude "$IMAGE" --help 2>&1); then :; fi

# ok: ccy-container-default-pids-limit
GH_TOKEN="$gh_token" container_cmd run -it --rm --pids-limit "$CCY_PIDS_LIMIT" \
    --entrypoint claude "$IMAGE" setup-token 2>&1 | tee "$out"

# ok: ccy-container-default-pids-limit
podman ps --filter label=ccy=true --format '{{.Names}}'

# ok: ccy-container-default-pids-limit
podman-compose up -d

# ok: ccy-container-default-pids-limit
echo "podman run --rm --network $NET alpine wget -q -O- http://example.com"

# ok: ccy-container-default-pids-limit
echo "Running: $CONTAINER_ENGINE run --rm --entrypoint claude \"$IMAGE\" setup-token"

# ok: ccy-container-default-pids-limit
if ! pgrep -f "${CONTAINER_ENGINE} run.*--name[= ]${name}$" >/dev/null; then :; fi

# ok: ccy-container-default-pids-limit
# a comment that says (podman run x) is not a run

# ok: ccy-container-default-pids-limit
podman run -d --pids-limit 4096 "$IMAGE"

# ok: ccy-container-default-pids-limit
podman run -d --pids-limit 100 "$IMAGE"

# --- ccy-unattended-refusal-status ---

refusal_fallthrough() {
    case "$rc" in
    3)
        # ruleid: ccy-unattended-refusal-status
        ccy_launch_unattended || return 3
        print_error "Nobody is here to choose another token, so ccy stops."
        ;;
    esac
    exit 1
}

refusal_in_if() {
    # ruleid: ccy-unattended-refusal-status
    if ccy_launch_unattended; then
        print_error "nobody to ask"
        exit 1
    fi
}

refusal_nested_if() {
    # ruleid: ccy-unattended-refusal-status
    if ccy_launch_unattended; then
        if [ -n "${CCY_RESTART_RELAUNCH_SESSION:-}" ]; then
            print_error "a restart"
        fi
        exit "$CCY_EXIT_TOKEN_USED_UP"
    fi
}

refusal_bare_exit() {
    # ruleid: ccy-unattended-refusal-status
    if ccy_launch_unattended; then
        exit
    fi
}

refusal_negated() {
    # ruleid: ccy-unattended-refusal-status
    if ! ccy_launch_unattended; then
        return 3
    fi
    exit 1
}

# ruleid: ccy-unattended-refusal-status
ccy_launch_unattended && exit 1

# ruleid: ccy-unattended-refusal-status
if ccy_nobody_to_ask; then
    # ruleid: ccy-unattended-refusal-status
    ccy_prompt_refuse token-select "Launch with --token NAME."
    exit 1
fi

relabel_refusal() {
    # ruleid: ccy-unattended-refusal-status
    if ccy_nobody_to_ask; then
        print_error "no terminal to ask on"
        return 1
    fi
}

# ruleid: ccy-unattended-refusal-status
if [ -n "$CCY_RESTART_RELAUNCH_SESSION" ] \
    && ! keys_unattended "$file"; then
    print_error "a key needs a passphrase"
    exit 1
fi

restart_refusal() {
    if ! budget_take "$history"; then
        # ruleid: ccy-unattended-refusal-status
        ccy_restart_refuse "$session_id" "its restart budget is spent (see above)."
        exit 1
    fi
}

# ok: ccy-unattended-refusal-status
if ccy_nobody_to_ask; then
    ccy_prompt_refuse token-select "Launch with --token NAME."
    ccy_unattended_stop token-select "nobody could choose a token"
fi

# ok: ccy-unattended-refusal-status
if [ -n "${CCY_RESTART_RELAUNCH_SESSION:-}" ]; then
    ccy_unattended_stop token-expired "the token has expired" "$CCY_RESTART_RELAUNCH_SESSION"
fi

refusal_named() {
    # ok: ccy-unattended-refusal-status
    if ccy_launch_unattended; then
        print_error "Nobody is here to choose another token, so ccy stops."
        ccy_unattended_stop token-used-up "the token has no usage left"
    fi
    return 3
}

safe_answer() {
    # ok: ccy-unattended-refusal-status
    if ccy_launch_unattended; then
        echo "Nobody to ask: taking the default."
        choice=Y
    else
        read -rp "Continue? " choice
    fi
    [ "$choice" = Y ] || exit 1
}

# ok: ccy-unattended-refusal-status
if [ -n "$CCY_RESTART_RELAUNCH_SESSION" ]; then
    exec {fd}<&0 </dev/null
fi

ccy_unattended_stop_like() {
    # ok: ccy-unattended-refusal-status
    [ -z "${3:-}" ] || ccy_restart_refuse "$3" "$2"
    exit "$CCY_EXIT_UNATTENDED_STOP"
}

# ok: ccy-unattended-refusal-status
ccy_launch_unattended && exit "$CCY_EXIT_UNATTENDED_STOP"

# --- bash-self-erasing-clipboard ---

# ruleid: bash-self-erasing-clipboard
printf '%s' "$pw" | wl-copy --paste-once

# ruleid: bash-self-erasing-clipboard
printf '%s' "$pw" | wl-copy -o

# ruleid: bash-self-erasing-clipboard
printf '%s' "$pw" | xclip -selection clipboard -loops 1

# ruleid: bash-self-erasing-clipboard
echo "The password is on the clipboard for one paste." >&2

# ruleid: bash-self-erasing-clipboard
check "served once" "yes" "$(logged 'wl-copy --paste-once')"

# ruleid: bash-self-erasing-clipboard
# after that it is gone from the clipboard

# ok: bash-self-erasing-clipboard
printf '%s' "$pw" | wl-copy

# ok: bash-self-erasing-clipboard
wl-copy --clear

# ok: bash-self-erasing-clipboard
wl-copy --type text/plain < "$file" && curl -o out "$url"

# ok: bash-self-erasing-clipboard
echo "The transcript is typed in one paste at stop." >&2

# --- bash-test-time-window ---

# The ccy-teams launch-start assertion as it was: a recorded time bounded by a
# read of the clock taken after it.
# ruleid: bash-test-time-window
check "launch start" "yes" "$([ -n "$since_ms" ] && [ "$since_ms" -ge "$before_ms" ] && [ "$since_ms" -le "$(date +%s%3N)" ] && echo yes || echo no)"

# The `[[ ]]` form and the seconds clock are the same defect.
# ruleid: bash-test-time-window
if [[ "$recorded" -lt "$(date +%s)" ]]; then echo ok; fi

# The exact value the code under test was given is what to assert.
# ok: bash-test-time-window
[ "$since_ms" -eq 1762000000123 ]

# A bound on how long something took is about elapsed time, not a window.
# ok: bash-test-time-window
[ $(($(date +%s) - started)) -lt 10 ]

# A bound on the real clock that is the point of the test says so.
# ok: bash-test-time-window
[ "$stamp" -le "$(date +%s)" ]  # FAIL-FAST-OK: the stamp is the system clock's own

# --- bash-test-scratch-inside-checkout ---

# The compose-restore suite as it was: a scratch tree inside the checkout and
# nothing stopping git from walking up into it.
# ruleid: bash-test-scratch-inside-checkout
WORK="$(mktemp -d "$REPO_ROOT/untracked/scratch/compose-test.XXXXXX")"
PROJECT="$WORK/project-a"
mkdir -p "$PROJECT"
cd "$PROJECT" || exit 99

# The braced spelling and an unquoted assignment are the same defect.
# ruleid: bash-test-scratch-inside-checkout
SCRATCH=$(mktemp -d "${REPO_ROOT}/untracked/scratch/registry-test.XXXXXX")
trap 'rm -rf "$SCRATCH"' EXIT
mkdir -p "$SCRATCH/home"
mkdir -p "$SCRATCH/bin"

# The fix: the ceiling goes straight after the directory is made.
# ok: bash-test-scratch-inside-checkout
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/menus-test.XXXXXX")"
export GIT_CEILING_DIRECTORIES="$REPO_ROOT/untracked"

# Two lines later is still "straight after".
# ok: bash-test-scratch-inside-checkout
work=$(mktemp -d "$REPO_ROOT/untracked/scratch/askpass-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
export GIT_CEILING_DIRECTORIES="$REPO_ROOT/untracked"

# A directory outside the checkout has no enclosing repository to read.
# ok: bash-test-scratch-inside-checkout
SCRATCH="$(mktemp -d)"

# A suite that needs the checkout's own configuration says so.
# ok: bash-test-scratch-inside-checkout
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/x.XXXXXX")"  # FAIL-FAST-OK: the case reads the checkout's remote on purpose

# A comment that quotes the call is not the call.
# ok: bash-test-scratch-inside-checkout
# the suite used to run: mktemp -d "$REPO_ROOT/untracked/scratch/y.XXXXXX"
