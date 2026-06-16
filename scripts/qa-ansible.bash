#!/usr/bin/env bash
# qa-ansible.bash — Ansible QA checks
#
# Check 1 (fail-fast): Flag error-hiding patterns without FAIL-FAST-OK justification.
#   Searches playbooks/, tasks/, vars/, environment/, roles/ (excluding roles/vendor).
#   Both *.yml and *.yaml.  Case-insensitive boolean detection.
#   Templated ignore_errors: "{{ ... }}" is also flagged — cannot be verified
#   statically, so it requires a same-line # FAIL-FAST-OK: justification too.
#
# Check 2 (hygiene): Assert every PLAYBOOK file (contains a top-level "- hosts:")
#   has the #!/usr/bin/env ansible-playbook shebang and the exec bit set.
#   Fixer: ./scripts/make-playbooks-executable.bash
#
# stdout:  terse — errors + summary only
# JSON:    ${QA_JSON_OUT:-/tmp/qa-ansible-results.json}
#
# jq usage:
#   jq '.status'                          # "pass" or "fail"
#   jq '.summary'                         # {total, passed, failed}
#   jq '.failures[]'                      # all violations
#   jq '.checks.ansible.fail_fast[]'      # fail-fast violations
#   jq '.checks.ansible.hygiene[]'        # shebang/exec violations

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
JSON_OUT="${QA_JSON_OUT:-/tmp/qa-ansible-results.json}"

# Temporary files — all cleaned up on exit
TMP_MATCHES=$(mktemp)
TMP_RESULTS=$(mktemp)
TMP_GREP_ERR=$(mktemp)
trap 'rm -f "$TMP_MATCHES" "$TMP_RESULTS" "$TMP_GREP_ERR"' EXIT

ERRORS=0

# ---------------------------------------------------------------------------
# Check 1: fail-fast patterns
# ---------------------------------------------------------------------------
# Pattern explanation:
#   failed_when:\s+false            — bare boolean (YAML false)
#   ignore_errors:\s+(true|yes)     — case-insensitive YAML booleans
#   ignore_errors:\s+"{{           — templated value (static-unverifiable)
#   ignore_unreachable:\s+true      — also a fail-fast suppressor
#
# grep -i makes the whole regex case-insensitive so True/TRUE/Yes/YES are caught.
# -E allows alternation.  --include flags are repeated for *.yml and *.yaml.
# roles/vendor is excluded via --exclude-dir.

FF_PATTERN='failed_when:[[:space:]]+false|ignore_errors:[[:space:]]+(true|yes)|ignore_errors:[[:space:]]+"[{][{]|ignore_unreachable:[[:space:]]+true'

FF_VIOLATIONS=()

# Build the search list from directories that actually exist. roles/ is an
# ansible-galaxy install target (roles/vendor is gitignored) and is legitimately
# absent on a clean checkout or CI runner — its absence is not an error, whereas
# grep over a non-existent path returns rc 2 and would wrongly fail the gate.
FF_SEARCH_DIRS=()
for _d in playbooks tasks vars environment roles; do
    [[ -d "$REPO_ROOT/$_d" ]] && FF_SEARCH_DIRS+=("$REPO_ROOT/$_d")
done

if [[ ${#FF_SEARCH_DIRS[@]} -eq 0 ]]; then
    echo "ERROR: none of the expected Ansible dirs (playbooks/ tasks/ vars/ environment/ roles/) exist under $REPO_ROOT" >&2
    exit 2
fi

# grep rc=1 means no matches — that is success; rc=0 means matches found
grep_rc=0
grep -rni \
    --include='*.yml' \
    --include='*.yaml' \
    --exclude-dir=vendor \
    -E "$FF_PATTERN" \
    "${FF_SEARCH_DIRS[@]}" \
    > "$TMP_MATCHES" 2>"$TMP_GREP_ERR" || grep_rc=$?

# rc 0 = matches found, rc 1 = no matches (OK), rc 2+ = real error
if [[ $grep_rc -ge 2 ]]; then
    echo "ERROR: grep failed (rc=$grep_rc):" >&2
    cat "$TMP_GREP_ERR" >&2
    exit 2
fi

while IFS= read -r line; do
    # Strip the REPO_ROOT prefix for tidier output
    rel_line="${line#"$REPO_ROOT"/}"
    if echo "$line" | grep -qi 'FAIL-FAST-OK'; then
        # Justified — skip
        :
    else
        echo "  ERROR (fail-fast): $rel_line"
        FF_VIOLATIONS+=("$rel_line")
        ERRORS=$((ERRORS + 1))
    fi
done < "$TMP_MATCHES"

# ---------------------------------------------------------------------------
# Check 2: playbook hygiene — shebang + exec bit
# ---------------------------------------------------------------------------
SHEBANG='#!/usr/bin/env ansible-playbook'
HYGIENE_VIOLATIONS=()
PLAYBOOK_COUNT=0

while IFS= read -r -d '' yml_file; do
    # Only treat as a playbook if it contains a top-level "- hosts:" line.
    # We look for the pattern at the start of a line (possibly after whitespace).
    if grep -qE '^[[:space:]]*-[[:space:]]+hosts:' "$yml_file" 2>"$TMP_GREP_ERR"; then
        PLAYBOOK_COUNT=$((PLAYBOOK_COUNT + 1))
        rel_file="${yml_file#"$REPO_ROOT"/}"
        violation=""

        # Check shebang. No stderr suppression: the grep above already read
        # this file successfully, so head will too; if it somehow fails, let
        # set -euo pipefail surface the I/O error rather than mask it.
        first_line=$(head -n1 "$yml_file")
        if [[ "$first_line" != "$SHEBANG" ]]; then
            violation="missing shebang"
        fi

        # Check exec bit
        if [[ ! -x "$yml_file" ]]; then
            if [[ -n "$violation" ]]; then
                violation="$violation + not executable"
            else
                violation="not executable"
            fi
        fi

        if [[ -n "$violation" ]]; then
            echo "  ERROR (hygiene): $rel_file — $violation"
            HYGIENE_VIOLATIONS+=("$rel_file ($violation)")
            ERRORS=$((ERRORS + 1))
        fi
    fi
done < <(find "$REPO_ROOT/playbooks/" -type f \( -name '*.yml' -o -name '*.yaml' \) -print0)

# ---------------------------------------------------------------------------
# Check 3: self-default vars (Ansible 2.19 "Recursive loop" footgun)
# ---------------------------------------------------------------------------
# A var defined as its own default — `foo: "{{ foo | default(...) }}"` — used to
# work via lazy evaluation but under ansible-core 2.19 it aborts at task-arg
# finalization with "Recursive loop detected in template: maximum recursion depth
# exceeded". `ansible-playbook --syntax-check` does NOT evaluate templates, so it
# passes this clean — only runtime catches it. This static check closes that gap.
#
# We target the self-DEFAULT idiom specifically (`\1 | default`), not any
# self-mention: a bare `x: "{{ x }}"` is almost always legitimate block-literal
# text being templated into another file (e.g. a blockinfile writing host_vars
# from a set_fact), which must not be flagged. The PCRE backreference \1 requires
# the SAME identifier on both sides:
#   <indent><name>: "{{ <name> | default(...
SELFREF_PATTERN='^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*):[[:space:]]*["'"'"']?\{\{[[:space:]]*\1[[:space:]]*\|[[:space:]]*default\b'
SELFREF_VIOLATIONS=()

selfref_rc=0
grep -rnP \
    --include='*.yml' \
    --include='*.yaml' \
    --exclude-dir=vendor \
    "$SELFREF_PATTERN" \
    "${FF_SEARCH_DIRS[@]}" \
    > "$TMP_MATCHES" 2>"$TMP_GREP_ERR" || selfref_rc=$?

if [[ $selfref_rc -ge 2 ]]; then
    echo "ERROR: self-reference grep failed (rc=$selfref_rc):" >&2
    cat "$TMP_GREP_ERR" >&2
    exit 2
fi

while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    rel_line="${line#"$REPO_ROOT"/}"
    echo "  ERROR (self-ref var): $rel_line"
    SELFREF_VIOLATIONS+=("$rel_line")
    ERRORS=$((ERRORS + 1))
done < "$TMP_MATCHES"

# ---------------------------------------------------------------------------
# Build JSON output — same shape as sibling qa scripts
# ---------------------------------------------------------------------------

# Encode FF_VIOLATIONS as a JSON array
ff_json_array="[]"
for v in "${FF_VIOLATIONS[@]+"${FF_VIOLATIONS[@]}"}"; do
    ff_json_array=$(printf '%s' "$ff_json_array" | jq --arg v "$v" '. + [$v]')
done

# Encode HYGIENE_VIOLATIONS as a JSON array
hy_json_array="[]"
for v in "${HYGIENE_VIOLATIONS[@]+"${HYGIENE_VIOLATIONS[@]}"}"; do
    hy_json_array=$(printf '%s' "$hy_json_array" | jq --arg v "$v" '. + [$v]')
done

# Encode SELFREF_VIOLATIONS as a JSON array
sr_json_array="[]"
for v in "${SELFREF_VIOLATIONS[@]+"${SELFREF_VIOLATIONS[@]}"}"; do
    sr_json_array=$(printf '%s' "$sr_json_array" | jq --arg v "$v" '. + [$v]')
done

# Total "files" counted: playbook count for hygiene + 1 synthetic entry for fail-fast scan
TOTAL=$((PLAYBOOK_COUNT + 1))
STATUS="pass"
[[ $ERRORS -gt 0 ]] && STATUS="fail"

jq -n \
    --arg status "$STATUS" \
    --argjson total "$TOTAL" \
    --argjson errors "$ERRORS" \
    --argjson ff "$ff_json_array" \
    --argjson hy "$hy_json_array" \
    --argjson sr "$sr_json_array" \
    '{
        "type": "ansible",
        "status": $status,
        "summary": {
            "total":  $total,
            "passed": ($total - $errors),
            "failed": $errors
        },
        "failures": (
            ($ff | map({"file": ., "type": "ansible", "status": "fail", "error": "fail-fast pattern without FAIL-FAST-OK annotation"})) +
            ($hy | map({"file": (. | split(" (")[0]), "type": "ansible", "status": "fail", "error": ("hygiene: " + (. | split(" (")[1] | rtrimstr(")"))) })) +
            ($sr | map({"file": ., "type": "ansible", "status": "fail", "error": "self-referential var (Ansible 2.19 recursive-loop error at runtime)"}))
        ),
        "results": [],
        "checks": {
            "fail_fast": $ff,
            "hygiene":   $hy,
            "self_ref":  $sr
        }
    }' > "$JSON_OUT"

# ---------------------------------------------------------------------------
# Terse summary
# ---------------------------------------------------------------------------
if [[ $ERRORS -eq 0 ]]; then
    echo "✓ ansible: fail-fast patterns OK; no self-referential vars; $PLAYBOOK_COUNT playbook(s) have correct shebang+exec"
    exit 0
else
    FF_COUNT=${#FF_VIOLATIONS[@]}
    HY_COUNT=${#HYGIENE_VIOLATIONS[@]}
    SR_COUNT=${#SELFREF_VIOLATIONS[@]}
    echo "✗ ansible: $ERRORS violation(s) — $FF_COUNT fail-fast, $HY_COUNT hygiene, $SR_COUNT self-ref var"
    echo "  Hygiene fixer: ./scripts/make-playbooks-executable.bash"
    echo "  Details: jq '.failures[]' $JSON_OUT"
    exit 1
fi
