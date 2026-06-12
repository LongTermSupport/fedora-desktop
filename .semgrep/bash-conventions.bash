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
