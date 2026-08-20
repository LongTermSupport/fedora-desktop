#!/usr/bin/bash
# Ansible playbook syntax validation - LLM-friendly
# Runs `ansible-playbook --syntax-check` on every PLAYBOOK file in the repo.
# Parse-only: SAFE in the CCY container — it does NOT execute any tasks and so
# does not violate the "never run playbooks in the container" rule.
# stdout:  terse — errors + summary only
# JSON:    ${QA_JSON_OUT:-/tmp/qa-ansible-syntax-results.json}
#
# jq usage:
#   jq '.status'         # "pass" or "fail"
#   jq '.summary'        # {total, passed, failed}
#   jq '.failures[]'     # all playbooks with syntax errors (file + error)
#
# Exit codes:
#   0  pass
#   1  fail (one or more playbooks failed --syntax-check)
#   2  missing required tool (ansible-playbook not installed)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
JSON_OUT="${QA_JSON_OUT:-/tmp/qa-ansible-syntax-results.json}"
TMP_RESULTS=$(mktemp)
TMP_ERR=$(mktemp)
trap 'rm -f "$TMP_RESULTS" "$TMP_ERR"' EXIT
ERRORS=0

# Pin the Ansible config explicitly so syntax-check is cwd-independent.
# Ansible's normal discovery looks at ./ansible.cfg in cwd — when QA runs from
# a subdirectory (or any non-repo-root cwd), discovery silently misses and the
# `ansible.builtin.config('CONFIG_FILE')` lookup used by every playbook's
# `root_dir` returns None, crashing the `dirname` filter at parse time. Setting
# the env var sidesteps the discovery entirely so the result depends only on
# REPO_ROOT, never on how the user happened to invoke QA.
export ANSIBLE_CONFIG="$REPO_ROOT/ansible.cfg"

# Fail fast: require ansible-playbook.
# `command -v` prints the resolved path to stdout (discarded) and nothing to
# stderr — no error output is suppressed by dropping stdout here.
if ! command -v ansible-playbook >/dev/null; then
    echo "✗ ansible-syntax: ansible-playbook not installed (sudo dnf install ansible)" >&2
    exit 2
fi

# Discover PLAYBOOK files only.
# A playbook is any YAML that declares a top-level "- hosts:" play. Task-files
# and vars-files are NOT playbooks and must not be syntax-checked.
#
# Discovery used to be hardcoded to playbooks/imports/ plus the entrypoint, and
# nothing said so. Two tracked playbooks sat outside that: the incident-time
# playbooks/dev/play-collect-diagnostics.yml, and a plan-local repair playbook.
# Neither was ever parsed, and the 2.19 hazards recorded in
# CLAUDE/AgentNotes.md — the quote-balance scanner, the colon-space-dash task
# name — are caught by THIS gate and by nothing else, including PyYAML. A play
# you only run during an incident is the worst one to discover is unparseable.
# Plan 00081 F9.
#
# The population is now "every playbook in the repo", derived rather than
# listed, so a playbook added anywhere is covered by default. Vendor and
# upstream trees are excluded because they are not ours to gate on — the same
# exclusion list the shell gates use.
# shellcheck source-path=SCRIPTDIR
# shellcheck source=qa-discovery.bash
source "$REPO_ROOT/scripts/qa-discovery.bash"

# TWO markers, not one. A playbook is a list item whose first key is `hosts:`
# — OR one whose first key is `import_playbook:`, which is what an entrypoint
# that only composes other playbooks looks like.
#
# The second marker is not a nicety. The first draft of this rewrite derived the
# population from `- hosts:` alone and dropped `playbooks/playbook-main.yml`, the
# single most important playbook in the repo, because it contains no play of its
# own. The reported count went from 78 to 79 and read as a clean gain — a
# coverage LOSS hiding inside an increase, which is this plan's defect class
# wearing the opposite sign.
PLAYBOOK_FILES=()
while IFS= read -r -d '' file; do
    rel="${file#"$REPO_ROOT"/}"
    qa_is_excluded "$rel" && continue
    if grep -Eq '^\s*-\s+(hosts|import_playbook):' "$file"; then
        PLAYBOOK_FILES+=("$file")
    fi
done < <(find "$REPO_ROOT" -type f \( -name "*.yml" -o -name "*.yaml" \) -print0)

TOTAL=${#PLAYBOOK_FILES[@]}

# A discovery that finds nothing is a BROKEN GATE, not a repo without playbooks
# — the guard qa-bash.bash and qa-python.bash both carry, and the one this gate
# was missing while its population was a hardcoded path that could simply stop
# existing.
if [[ $TOTAL -eq 0 ]]; then
    echo "✗ ansible-syntax: playbook discovery found 0 playbooks under $REPO_ROOT" >&2
    echo "  This repo is an Ansible project, so the discovery is broken —" >&2
    echo "  reporting a pass here would vouch for playbooks nothing had parsed." >&2
    exit 2
fi

# Syntax-check each playbook. stderr is captured to a temp file so genuine
# parse errors are surfaced (never hidden) in stdout and the JSON output.
for file in "${PLAYBOOK_FILES[@]}"; do
    rel_path="${file#"$REPO_ROOT"/}"
    if ansible-playbook --syntax-check "$file" >/dev/null 2>"$TMP_ERR"; then
        jq -nc --arg f "$rel_path" '{"file":$f,"type":"ansible-syntax","status":"pass"}' >> "$TMP_RESULTS"
    else
        err=$(cat "$TMP_ERR")
        echo "✗ ansible-syntax: $rel_path"
        echo "$err"
        jq -nc --arg f "$rel_path" --arg e "$err" \
            '{"file":$f,"type":"ansible-syntax","status":"fail","error":$e}' >> "$TMP_RESULTS"
        ERRORS=$((ERRORS + 1))
    fi
done

# Write JSON
STATUS="pass"
[[ $ERRORS -gt 0 ]] && STATUS="fail"

jq -s \
    --arg status "$STATUS" \
    '{
        "type": "ansible-syntax",
        "status": $status,
        "summary": {
            "total": length,
            "passed": ([.[] | select(.status == "pass")] | length),
            "failed": ([.[] | select(.status == "fail")] | length)
        },
        "results": .,
        "failures": [.[] | select(.status == "fail")]
    }' "$TMP_RESULTS" > "$JSON_OUT"

# Terse summary
if [[ $ERRORS -eq 0 ]]; then
    # State the population, do not imply it. A bare count reads as "all of them"
    # whatever it counted — which is how a hardcoded playbooks/imports/ path went
    # years without anyone noticing two playbooks outside it. The breakdown makes
    # a change in coverage visible in the ordinary passing output, where it will
    # actually be seen.
    OUTSIDE=0
    for file in "${PLAYBOOK_FILES[@]}"; do
        case "${file#"$REPO_ROOT"/}" in
            playbooks/imports/*) ;;
            *) OUTSIDE=$((OUTSIDE + 1)) ;;
        esac
    done
    echo "✓ ansible-syntax: $TOTAL playbooks OK ($((TOTAL - OUTSIDE)) under playbooks/imports/, $OUTSIDE elsewhere)"
    exit 0
else
    echo "✗ ansible-syntax: $ERRORS/$TOTAL playbooks failed → $JSON_OUT"
    exit 1
fi
