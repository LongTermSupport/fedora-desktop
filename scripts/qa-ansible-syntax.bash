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
# A playbook is the entrypoint (playbook-main.yml) plus any standalone YAML
# under playbooks/imports/ that declares a top-level "- hosts:" play.
# Task-files and vars-files are NOT playbooks and must not be syntax-checked.
PLAYBOOK_FILES=()

MAIN_PLAYBOOK="$REPO_ROOT/playbooks/playbook-main.yml"
if [[ -f "$MAIN_PLAYBOOK" ]]; then
    PLAYBOOK_FILES+=("$MAIN_PLAYBOOK")
fi

while IFS= read -r -d '' file; do
    # Top-level play marker: a list item whose first key is "hosts:".
    if grep -Eq '^\s*-\s+hosts:' "$file"; then
        PLAYBOOK_FILES+=("$file")
    fi
done < <(find "$REPO_ROOT/playbooks/imports" -type f \( -name "*.yml" -o -name "*.yaml" \) -print0)

TOTAL=${#PLAYBOOK_FILES[@]}

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
    echo "✓ ansible-syntax: $TOTAL playbooks OK"
    exit 0
else
    echo "✗ ansible-syntax: $ERRORS/$TOTAL playbooks failed → $JSON_OUT"
    exit 1
fi
