#!/usr/bin/bash
# Run every helpers/ unit test (stdlib unittest, no pytest/venv — see helpers/CLAUDE.md).
#
# Why this exists: the helper packages under helpers/ are namespace packages with
# NO __init__.py, so `python3 -m unittest discover` cannot import the start dir and
# silently collects nothing (ImportError / "Ran 0 tests"). Explicit module names
# import fine, so this runner enumerates tests/helpers/**/test_*.py, converts each
# path to a dotted module name, and hands the full list to unittest in one call.
#
# Scope is tests/helpers/ deliberately: helper tests are stdlib-only by rule, so
# they run with no pip install. Tests elsewhere under tests/ (e.g. tests/clip_scan,
# which imports numpy) have their own third-party dependency story and are NOT run
# here — sweeping all of tests/ would drag a non-stdlib dep into this gate.
#
# Runnable locally and in CI:
#   ./scripts/qa-helper-tests.bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

mapfile -t test_files < <(find tests/helpers -type f -name 'test_*.py' | sort)

if [[ "${#test_files[@]}" -eq 0 ]]; then
    echo "ERROR: no helper tests found — expected tests/helpers/**/test_*.py" >&2
    exit 1
fi

modules=()
for file in "${test_files[@]}"; do
    module="${file%.py}"      # drop the .py suffix
    module="${module//\//.}"  # path separators → module dots
    modules+=("$module")
done

echo "Running ${#modules[@]} helper test module(s)..."
# unittest exits non-zero on any failure; set -e propagates it (fail-fast).
python3 -m unittest "${modules[@]}"
