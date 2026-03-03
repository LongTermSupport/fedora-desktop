#!/bin/bash
# Semgrep test cases for .semgrep/bash-conventions.yml
# Lines with '# ruleid:' expect a finding on the NEXT line.
# Lines with '# ok:' expect NO finding on the NEXT line.

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
# shellcheck disable=SC2034
VAR=$(some_cmd || echo "default")

# ruleid: bash-error-hiding-pipe-echo
# shellcheck disable=SC2034
result=$(load_preference 2>/dev/null || echo "")
