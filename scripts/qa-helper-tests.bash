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
#   ./scripts/qa-helper-tests.bash --counts-file PATH
#
# --counts-file asks for the run's size and skip count as DATA. `unittest` counts a
# SKIPPED test inside testsRun, so `Ran 1464 tests` is identical whether a test asserted
# or skipped, and `qa-all.bash` needs the skip count to tell two machines apart. Four
# attempts to scrape it back out of this script's output were each defeated by a test
# printing unittest-shaped text — see helpers/qa_environment/unittest_counts.py, which
# takes both numbers from the TestResult object instead and writes them to that path.
#
# Everything a human reads — progress, tracebacks, unittest's own summary — goes to
# STDERR, deliberately on one stream so its ordering is the true ordering.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

counts_file=""
counts_token=""
while [[ "$#" -gt 0 ]]; do
    case "$1" in
        --counts-file)
            if [[ "$#" -lt 2 ]]; then
                echo "ERROR: --counts-file needs a path" >&2
                exit 2
            fi
            counts_file="$2"
            shift 2
            ;;
        --counts-token)
            if [[ "$#" -lt 2 ]]; then
                echo "ERROR: --counts-token needs a value" >&2
                exit 2
            fi
            counts_token="$2"
            shift 2
            ;;
        *)
            echo "ERROR: unknown argument '$1' (expected --counts-file PATH" \
                "[--counts-token VALUE])" >&2
            exit 2
            ;;
    esac
done

# No caller asked for the counts, so they go to a scratch file that is cleaned up. The
# runner always writes them; there is no path where the numbers are simply not produced.
if [[ -z "$counts_file" ]]; then
    counts_file="$(mktemp)"
    trap 'rm -f "$counts_file"' EXIT
fi

# shellcheck source-path=SCRIPTDIR
# shellcheck source=qa-discovery.bash
source "$ROOT_DIR/scripts/qa-discovery.bash"

mapfile -t test_files < <(find tests/helpers -type f -name 'test_*.py' | sort)

if [[ "${#test_files[@]}" -eq 0 ]]; then
    echo "ERROR: no helper tests found — expected tests/helpers/**/test_*.py" >&2
    exit 1
fi

# Zero discovery is guarded above. This guards PARTIAL discovery, the case a guard
# on emptiness cannot see: `mapfile -t < <(find ...)` reports mapfile's status, not
# find's, and `pipefail` does not reach inside a process substitution. A walk that
# half-failed yields a shorter list, the suite runs it, and the smaller `Ran N tests`
# reads exactly like the whole population. An under-match never announces itself, so
# the yardstick comes from git rather than from the walk being checked.
#
# The same defect was found and fixed in qa-bash.bash (Plan 00076) and qa-python.bash
# (Plan 00081); this was the third discovery site in this directory and the one that
# never got the generalisation.
qa_tracked_helper_tests "$ROOT_DIR"

declare -A discovered=()
for file in "${test_files[@]}"; do
    discovered["$file"]=1
done

missed=()
for rel in "${QA_TRACKED_HELPER_TESTS[@]}"; do
    [[ -n "${discovered[$rel]:-}" ]] || missed+=("$rel")
done

if [[ "${#missed[@]}" -gt 0 ]]; then
    echo "ERROR: discovery missed ${#missed[@]} tracked helper test file(s):" >&2
    printf '    %s\n' "${missed[@]}" >&2
    echo "  These are tests this gate would have reported a pass over without" >&2
    echo "  running. Fix the discovery — do not untrack the files to silence it." >&2
    # Exit 2, agreeing with qa-bash.bash and qa-python.bash: CLAUDE/QA.md reserves 2 for
    # "coverage cannot be verified", which is what this is rather than a test failure.
    exit 2
fi

# The OTHER direction, and the half of the coverage story the cross-check above cannot
# tell. `find` discovers untracked files too, so a file nobody committed is run and
# counted while no tracked file is missing — the gate passes and the number silently
# moves. It moved 1464 → 1465 → 1479 under a reviewer for exactly this reason, and the
# number it moves is the one two machines are compared on. So the coverage is REPORTED
# rather than implied by the list's length, the way version-pins reports `COVERAGE: 9 of 9`.
untracked_tests=()
declare -A tracked=()
for rel in "${QA_TRACKED_HELPER_TESTS[@]}"; do
    tracked["$rel"]=1
done
for file in "${test_files[@]}"; do
    [[ -n "${tracked[$file]:-}" ]] || untracked_tests+=("$file")
done

#
# The COUNT reaches the caller through the counts file (`--tracked-modules` below), because
# `qa-all.bash` discards this stream on a successful run — a coverage number reported only
# here would be produced and never delivered, which is one step short of the class this plan
# exists to remove. What stays here is the part a stage line cannot carry: the file NAMES.
printf 'COVERAGE: %s of %s tracked helper test modules\n' \
    "$((${#test_files[@]} - ${#untracked_tests[@]}))" "${#QA_TRACKED_HELPER_TESTS[@]}" >&2
if [[ "${#untracked_tests[@]}" -gt 0 ]]; then
    echo "  plus ${#untracked_tests[@]} UNTRACKED file(s), which run here and nowhere else:" >&2
    printf '    %s\n' "${untracked_tests[@]}" >&2
    echo "  Commit them or remove them — an uncommitted test makes this run's counts" >&2
    echo "  incomparable with any other machine's." >&2
fi

modules=()
for file in "${test_files[@]}"; do
    module="${file%.py}"      # drop the .py suffix
    module="${module//\//.}"  # path separators → module dots
    modules+=("$module")
done

echo "Running ${#modules[@]} helper test module(s)..." >&2

# The token is forwarded only when a caller supplied one, so a standalone run needs no
# ceremony and `qa-all.bash` still gets a counts file it can prove came from its own run.
token_args=()
if [[ -n "$counts_token" ]]; then
    token_args=(--counts-token "$counts_token")
fi

# The machine's own git config stays out of every test, as it does in CI. A global
# `commit.gpgsign` would otherwise make each fixture commit depend on this machine's
# signing key, and a test that signs its own fixtures would sign with the wrong one.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
unset GIT_CONFIG_COUNT GIT_CONFIG_PARAMETERS

# The runner exits non-zero on any failure; set -e propagates it (fail-fast).
python3 -m helpers.qa_environment.unittest_counts \
    --counts-file "$counts_file" \
    --tracked-modules "${#QA_TRACKED_HELPER_TESTS[@]}" \
    "${token_args[@]}" "${modules[@]}"
