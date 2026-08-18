#!/bin/bash
# Semgrep test cases for .semgrep/bash-conventions.yml
#
# Annotation convention (see `semgrep --test`): a line tagged with a rule id
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
