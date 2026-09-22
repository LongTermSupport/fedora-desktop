#!/usr/bin/bash
# QA toolchain version assertion — LLM-friendly
# stdout:  one TOOLCHAIN-OK summary line (the payload)
# stderr:  the per-tool diagnostics
#
# A gate's verdict belongs to the binary that produced it. shellcheck 0.9.0 and
# 0.11.0 reported 173 and 141 issues on this same tree — so an unpinned
# toolchain means each environment is confident and they disagree, with nothing
# saying so. `.qa-versions` is the single source of truth; this asserts the
# machine actually matches it. Only tools a gate runs are listed: ruff
# (qa-python), semgrep (qa-patterns), shellcheck (qa-bash).
#
# Exit 1, never 2. Exit 2 means "missing tool, refuse to run" and qa-all turns
# that into a whole-run abort — which is how a single drifted ruff took 44
# unrelated gates offline. A wrong or absent QA tool must fail THIS gate and
# leave the others their chance to report.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSIONS_FILE="$REPO_ROOT/.qa-versions"

if [[ ! -f "$VERSIONS_FILE" ]]; then
    echo "✗ toolchain: $VERSIONS_FILE is missing — it is the single source of truth" >&2
    echo "  for every QA tool version, and four consumers read it." >&2
    exit 1
fi

# shellcheck source=/dev/null
source "$VERSIONS_FILE"

# Each tool's own stderr is captured rather than discarded: if the version cannot
# be read, that stderr is the only evidence of why, so it is kept and printed on
# the failure path instead of thrown away.
TMP_ERR=$(mktemp)
trap 'rm -f "$TMP_ERR"' EXIT

MISMATCHES=0
CHECKED=0

# tool_version <command> — print the bare version, empty if unresolvable.
#
# Each tool is asked separately because none of them agree on where the number
# sits: ruff puts it second, shellcheck prints a `version: X` line of its own,
# and semgrep prints the bare number alone. Parsing them all with one regex is
# how a gate starts comparing an empty string to an empty string and calling it
# a match.
tool_version() {
    local tool="$1"
    if ! command -v "$tool" >/dev/null; then
        return 0
    fi
    case "$tool" in
        shellcheck) shellcheck --version 2>"$TMP_ERR" | awk '/^version:/ {print $2}' ;;
        semgrep) semgrep --version 2>"$TMP_ERR" | awk 'NR==1 {print $1}' ;;
        *) "$tool" --version 2>"$TMP_ERR" | awk 'NR==1 {print $2}' ;;
    esac
}

# assert_tool <command> <expected>
#
# An ABSENT tool is a mismatch, not a skip. "Not installed" and "installed at
# the wrong version" have the same consequence — this machine cannot produce
# the agreed verdict.
assert_tool() {
    local tool="$1" expected="$2" actual
    CHECKED=$((CHECKED + 1))
    : >"$TMP_ERR"
    actual="$(tool_version "$tool")"

    if [[ -z "$actual" ]]; then
        if command -v "$tool" >/dev/null; then
            echo "✗ toolchain: $tool is installed but its version could not be read (expected $expected)" >&2
            echo "  its stderr was:" >&2
            cat "$TMP_ERR" >&2
        else
            echo "✗ toolchain: $tool is not installed (expected $expected)" >&2
        fi
        MISMATCHES=$((MISMATCHES + 1))
        return 0
    fi

    if [[ "$actual" != "$expected" ]]; then
        echo "✗ toolchain: $tool version mismatch" >&2
        echo "    expected : $expected  (.qa-versions)" >&2
        echo "    found    : $actual  ($(command -v "$tool"))" >&2
        MISMATCHES=$((MISMATCHES + 1))
    fi
}

assert_tool ruff "$RUFF"
assert_tool semgrep "$SEMGREP"
assert_tool shellcheck "$SHELLCHECK"

if [[ $MISMATCHES -gt 0 ]]; then
    echo "" >&2
    echo "  These gates are version-dependent, so this machine would report a" >&2
    echo "  different verdict from the CCY container and from CI. Bring it into line:" >&2
    echo "    host      ./playbooks/imports/play-python.yml" >&2
    echo "    container ccy --rebuild   (the image bakes .qa-versions at build time)" >&2
    exit 1
fi

echo "TOOLCHAIN-OK $CHECKED tool(s) match .qa-versions"
