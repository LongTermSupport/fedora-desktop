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
