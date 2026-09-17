#!/usr/bin/bash
# Reject the un-waitable run-log pattern in plan scripts (Plan 00130).
#
# `exec > >(tee "$LOG") 2>&1` redirects into a PROCESS SUBSTITUTION, and the shell
# cannot wait on one. At exit the script's last chunk of output is still in tee's
# buffer and the parent does not wait for it to drain, so the run log loses its
# ending — reliably the part naming what failed. `cmd | tee f` has no such problem,
# because the shell DOES wait for every member of a pipeline, and the plan library's
# `plan_start_log` uses that form and arms a drain handler besides.
#
# The paired offence is a plan-local `logs/` directory. Run logs belong under
# `untracked/plan-runs/<plan>/<script>/<timestamp>/` — they are unscrubbed and can
# never be committed, so the rule that says so should be the LOCATION rather than a
# glob in a nested .gitignore. A `logs/` dir inside a plan folder is also what made
# the orphans invisible: .gitignore hid them from every `git status` that would
# otherwise have shown them.
#
# SCOPE: active plans only, `CLAUDE/Plan/NNNNN-*/`. The archived tree under
# Completed/ is frozen history — rewriting a closed plan's scripts changes the record
# of what was actually run, and none of them will run again.
#
# TWO CONTROLS make this falsifiable (CLAUDE/QA.md "Changing a Gate"): the scanner
# must REJECT a fixture carrying the pattern and ACCEPT one without it. A gate that
# passes when its scanner never matched anything is worse than no gate — which is this
# repo's recurring defect, a check whose clean result is indistinguishable from a
# blind one.
#
#   ./scripts/qa-plan-script-logging.bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

# The pattern's first character is bracketed so this gate does not match its own
# source when someone greps the tree for offenders.
#
# The open paren is bracketed rather than backslash-escaped because awk reads a
# -v value as a STRING first: `\(` loses its backslash there and reaches the
# regex engine as a bare `(`, which is an unmatched group. awk then aborts on the
# first file it scans, so the gate failed with a regex error instead of a verdict.
OFFENCE='exec[[:space:]]*>[[:space:]]*>[(][[:space:]]*[t]ee'

# scan_file <path> — print each offending line as "<line>:<text>", ignoring comments.
# An already-converted script that documents its own conversion in a comment must not
# be counted as unconverted.
scan_file() {
    local path="${1:?scan_file requires a path}"
    awk -v pat="$OFFENCE" '
        { line = $0 }
        line ~ /^[[:space:]]*#/ { next }
        line ~ pat { printf "%d:%s\n", NR, line }
    ' "$path"
}

# --- controls -----------------------------------------------------------------
CONTROL_DIR="$(mktemp -d)"
trap 'rm -rf "$CONTROL_DIR"' EXIT

DIRTY="$CONTROL_DIR/dirty.bash"
CLEAN="$CONTROL_DIR/clean.bash"

# The fixtures must contain a LITERAL dollar sign, so it is passed as an argument
# rather than written into a single-quoted format string — shellcheck reads the latter
# as an expression someone forgot to expand, and this repo treats its info findings as
# failures.
d='$'

{
    printf '#!/usr/bin/env bash\n'
    printf 'LOG=/tmp/x.log\n'
    printf 'exec > >(tee "%sLOG") 2>&1\n' "$d"
} > "$DIRTY"

{
    printf '#!/usr/bin/env bash\n'
    printf '# historical note: this script used to exec > >(tee "%sLOG") 2>&1\n' "$d"
    printf 'plan_start_log auto\n'
} > "$CLEAN"

if [[ -z "$(scan_file "$DIRTY")" ]]; then
    echo "ERROR: the scanner did not flag a file that carries the pattern; the gate cannot be trusted" >&2
    exit 1
fi

if [[ -n "$(scan_file "$CLEAN")" ]]; then
    echo "ERROR: the scanner flagged a converted file, matching its own explanatory comment" >&2
    exit 1
fi

# --- the real tree --------------------------------------------------------------
examined=0
findings=()

# RECURSIVE, deliberately. A top-level `"$planDir"*.bash` glob examined 52 of the 63
# scripts in the active tree and printed the 52 as though it were the population — and
# 00099's falsification/ holds eleven scripts, including a retired/ subdirectory, that it
# never opened. That is this gate's own subject: a clean result indistinguishable from a
# blind one, in the gate written to stop it. Same for `logs/`, which was only checked
# directly under the plan folder.
while IFS= read -r script; do
    examined=$((examined + 1))

    hits="$(scan_file "$script")"
    if [[ -n "$hits" ]]; then
        while IFS= read -r hit; do
            findings+=("$script:$hit")
        done <<< "$hits"
    fi
done < <(find CLAUDE/Plan/[0-9]*-*/ -type f -name '*.bash' | sort)

while IFS= read -r logDir; do
    [[ -n "$logDir" ]] || continue
    findings+=("$logDir: plan-local run-log directory")
done < <(find CLAUDE/Plan/[0-9]*-*/ -type d -name logs | sort)

# A glob that matched nothing and a tree with no offences print identically unless the
# count is stated. Plan 00130's own triage made this mistake first and fixed it; a gate
# is the last place to repeat it.
if [[ "$examined" -eq 0 ]]; then
    echo "ERROR: examined 0 plan scripts — the search matched nothing, so this gate certified nothing" >&2
    exit 1
fi

if [[ "${#findings[@]}" -gt 0 ]]; then
    echo "ERROR: ${#findings[@]} plan-script logging offence(s) across $examined script(s) examined:" >&2
    printf '  %s\n' "${findings[@]}" >&2
    echo >&2
    echo "  Convert to the plan library: source _planlib.inc.bash, plan_init, then" >&2
    echo "  plan_start_log auto — and remove every consumer of the \$LOG you delete." >&2
    exit 1
fi

echo "PLAN-SCRIPT-LOGGING-OK: $examined plan script(s) examined, no offences"
