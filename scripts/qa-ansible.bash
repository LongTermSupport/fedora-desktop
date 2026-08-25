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

# One spelling list for every directive. YAML 1.1 accepts `yes`/`no` as booleans
# exactly as it accepts `true`/`false`, and Ansible reads them that way — but
# this regex accepted `yes` for ignore_errors ONLY, and only `false` (never `no`)
# for failed_when. So `failed_when: no` earned "✓ ansible: fail-fast patterns OK"
# — a green tick on the repo's #1 rule, from an asymmetry living inside a single
# line. Plan 00081 F10.
#
# The trailing \b is load-bearing. Without it, `no` matched inside `not` and the
# gate reported 10 violations against legitimate `failed_when: not foo.stat.exists`
# probes. That over-match announced itself on the first run — which is the whole
# asymmetry this plan keeps meeting: an over-match prints the extra items and
# gets fixed in a minute, while the under-match it replaced sat here silently.
FF_FALSEY='(false|no|off)\b'
FF_TRUTHY='(true|yes|on)\b'
FF_PATTERN="failed_when:[[:space:]]+$FF_FALSEY|ignore_errors:[[:space:]]+$FF_TRUTHY|ignore_errors:[[:space:]]+\"[{][{]|ignore_unreachable:[[:space:]]+$FF_TRUTHY"

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

# Strip a YAML comment from a grep result line, so a pattern match that lies
# entirely inside a comment is not reported as a violation.
#
# WHY (Plan 00071): the grep above scans RAW lines, so a comment DOCUMENTING the
# removal of an anti-pattern trips the check that exists to ban it —
# play-systemd-user-tweaks.yml:242 reads "# old `ignore_errors: true` headless
# escape hatch (removed — a failure here now means the manager really is broken)"
# and was reported as a fail-fast violation. A gate that punishes writing down
# WHY the anti-pattern is absent teaches people to delete the explanation.
#
# A YAML comment starts at a '#' that is at the start of the line or preceded by
# whitespace. '#' inside a quoted string is NOT a comment; that case is not
# handled, and does not need to be — it would require a line that both quotes a
# '#' and contains a fail-fast directive.
ff_strip_comment() {
    awk '{
        for (i = 1; i <= length($0); i++) {
            c = substr($0, i, 1)
            prev = (i == 1) ? "" : substr($0, i - 1, 1)
            if (c == "#" && (i == 1 || prev == " " || prev == "\t")) {
                print substr($0, 1, i - 1)
                next
            }
        }
        print $0
    }' <<< "$1"
}

while IFS= read -r line; do
    # Strip the REPO_ROOT prefix for tidier output
    rel_line="${line#"$REPO_ROOT"/}"

    # The FAIL-FAST-OK annotation is itself written in a trailing comment, so it
    # MUST be looked for on the full line — before any comment stripping.
    if echo "$line" | grep -qi 'FAIL-FAST-OK'; then
        # Justified — skip. Checked against the WHOLE line, because the
        # annotation legitimately lives in a trailing comment:
        #   failed_when: false  # FAIL-FAST-OK: reason
        continue
    fi

    # Comment-only mentions are inert. Documentation that discusses these
    # directives in prose — including a comment recording that one was
    # REMOVED — is not a fail-fast violation, but a plain grep cannot tell
    # the difference and flags it (Plan 00071).
    #
    # grep prints path:lineno:content — isolate the content so the path (which
    # can legitimately contain '#') is never mistaken for a comment, then strip
    # via ff_strip_comment (handles '#' inside a quoted string correctly, unlike
    # a blind ${content%%#*} glob strip) and re-test. If the pattern no longer
    # matches, every occurrence was inside a comment — this cannot mask a real
    # directive, since a real one is YAML and always precedes any comment marker
    # on its line.
    ff_content="${line#*:}"
    ff_content="${ff_content#*:}"
    ff_code="$(ff_strip_comment "$ff_content")"

    if ! grep -qiE "$FF_PATTERN" <<< "$ff_code"; then
        # The match was inside a comment — documentation, not a directive.
        continue
    fi

    echo "  ERROR (fail-fast): $rel_line"
    FF_VIOLATIONS+=("$rel_line")
    ERRORS=$((ERRORS + 1))
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
# Check 4: provisioning-profile scope + guard declaration (Plan 00061)
# ---------------------------------------------------------------------------
# Every PLAYBOOK (core AND optional, any file with a top-level "- hosts:"
# line, minus imports/optional/archived/) must:
#   1. declare EXACTLY ONE vars.scope in general|gnome|server, and
#   2. if scope is gnome or server, carry the exact 2-task canonical guard
#      (see CLAUDE/AnsibleStyle.md "Provisioning Profile Self-Guard") as its
#      FIRST TWO tasks; if scope is general, the guard must be ABSENT (a
#      general play never needs it — see CLAUDE/Plan/00061 PROPOSAL.md §3.3).
#
# playbook-main.yml has no "- hosts:" line (it is an import-only entry point),
# so the playbook-discovery guard below naturally skips it without a special
# case.
#
# A file with more than one "- hosts:" play is REJECTED outright before any
# scope/guard scan — this check cannot safely vouch for a second, unexamined
# play hiding behind the first play's declaration.
GUARD_ASSERT_NAME='Scope guard — assert provisioning_profile is recognised'
GUARD_END_NAME='Scope guard — end play if provisioning_profile does not match declared scope'
GUARD_END_META='end_play'
GUARD_END_WHEN="(scope == 'gnome' and provisioning_profile == 'server') or (scope == 'server' and provisioning_profile != 'server')"
SCOPE_VIOLATIONS=()

while IFS= read -r -d '' yml_file; do
    grep -qE '^[[:space:]]*-[[:space:]]+hosts:' "$yml_file" 2>"$TMP_GREP_ERR" || continue
    [[ "$yml_file" == */optional/archived/* ]] && continue

    rel_file="${yml_file#"$REPO_ROOT"/}"

    # This grep is only reached after the -qE above confirmed >=1 "- hosts:"
    # line, so it always matches (rc 0). The explicit `|| hosts_block_count=0`
    # is a defensive fallback that handles the impossible-here rc=1 case
    # without letting `set -e` abort — NOT error hiding (the count is used, a
    # 0 would simply fall through to the single-play path).
    hosts_block_count=$(grep -cE '^[[:space:]]*-[[:space:]]+hosts:' "$yml_file") || hosts_block_count=0
    if [[ $hosts_block_count -gt 1 ]]; then
        echo "  ERROR (scope): $rel_file — file contains $hosts_block_count separate '- hosts:' plays; this gate cannot safely vouch for a multi-play file. Split it (one play per file, matching every other playbook in the repo), then give each its own vars.scope (+ guard if needed)."
        SCOPE_VIOLATIONS+=("$rel_file (multi-play file: $hosts_block_count plays in one file, not supported)")
        ERRORS=$((ERRORS + 1))
        continue
    fi

    # Single pass: extract vars.scope (wherever it sits in the play-level
    # vars: block) and the first TWO tasks' name / meta / when fields, using
    # bash counters rather than `grep -c` throughout — no `grep -c`-on-empty
    # hazard anywhere in this loop.
    scope_count=0
    scope_val=""
    t1_name=""; t2_name=""; t2_meta=""; t2_when=""
    while IFS='|' read -r key val; do
        case "$key" in
            SCOPE) scope_count=$((scope_count + 1)); scope_val="$val" ;;
            TASK1_NAME) t1_name="$val" ;;
            TASK2_NAME) t2_name="$val" ;;
            TASK2_META) t2_meta="$val" ;;
            TASK2_WHEN) t2_when="$val" ;;
        esac
    done < <(awk '
        /^  vars:[[:space:]]*$/ { in_vars=1; next }
        in_vars && /^    scope:[[:space:]]*/ {
            val = $0
            sub(/^    scope:[[:space:]]*/, "", val)
            sub(/[[:space:]]*#.*$/, "", val)
            sub(/\r$/, "", val)
            if (val != "") { print "SCOPE|" val }
            next
        }
        in_vars && /^[^[:space:]]/ { in_vars = 0 }
        in_vars && /^  [^ ]/ { in_vars = 0 }

        /^  tasks:[[:space:]]*$/ { in_tasks=1; task_count=0; next }
        in_tasks && /^    - name:[[:space:]]*/ {
            task_count++
            if (task_count > 2) { in_tasks = 0; next }
            val = $0
            sub(/^    - name:[[:space:]]*/, "", val)
            print "TASK" task_count "_NAME|" val
            next
        }
        in_tasks && task_count == 2 && /^      ansible\.builtin\.meta:[[:space:]]*/ {
            val = $0
            sub(/^      ansible\.builtin\.meta:[[:space:]]*/, "", val)
            sub(/[[:space:]]*#.*$/, "", val)   # strip trailing comment (parity with SCOPE)
            sub(/\r$/, "", val)                # strip stray CRLF (parity with SCOPE)
            print "TASK2_META|" val
            next
        }
        in_tasks && task_count == 2 && /^      when:[[:space:]]*/ {
            val = $0
            sub(/^      when:[[:space:]]*/, "", val)
            sub(/[[:space:]]*#.*$/, "", val)   # strip trailing comment (parity with SCOPE)
            sub(/\r$/, "", val)                # strip stray CRLF (parity with SCOPE)
            print "TASK2_WHEN|" val
            next
        }
        in_tasks && /^  [^ ]/ { in_tasks = 0 }
    ' "$yml_file")

    if [[ $scope_count -eq 0 ]]; then
        echo "  ERROR (scope): $rel_file — missing vars.scope (need exactly one of general|gnome|server)"
        SCOPE_VIOLATIONS+=("$rel_file (missing vars.scope)")
        ERRORS=$((ERRORS + 1))
        continue
    elif [[ $scope_count -gt 1 ]]; then
        echo "  ERROR (scope): $rel_file — multiple vars.scope entries declared (exactly one required)"
        SCOPE_VIOLATIONS+=("$rel_file (multiple vars.scope entries)")
        ERRORS=$((ERRORS + 1))
        continue
    fi

    case "$scope_val" in
        general|gnome|server) : ;;
        *)
            echo "  ERROR (scope): $rel_file — invalid vars.scope value: $scope_val (must be exactly one of general|gnome|server)"
            SCOPE_VIOLATIONS+=("$rel_file (invalid vars.scope: $scope_val)")
            ERRORS=$((ERRORS + 1))
            continue
            ;;
    esac

    guard_ok=0
    if [[ "$t1_name" == "$GUARD_ASSERT_NAME" && "$t2_name" == "$GUARD_END_NAME" && "$t2_meta" == "$GUARD_END_META" && "$t2_when" == "$GUARD_END_WHEN" ]]; then
        guard_ok=1
    fi

    if [[ "$scope_val" == "general" ]]; then
        if [[ "$t1_name" == "$GUARD_ASSERT_NAME" || "$t1_name" == "$GUARD_END_NAME" ]]; then
            echo "  ERROR (scope): $rel_file — unnecessary scope guard on a general-scope play (guard never fires for general; remove it)"
            SCOPE_VIOLATIONS+=("$rel_file (unnecessary guard on general play)")
            ERRORS=$((ERRORS + 1))
        fi
    else
        if [[ $guard_ok -eq 0 ]]; then
            echo "  ERROR (scope): $rel_file — scope=$scope_val requires the 2-task canonical guard (assert + meta:end_play) as its first two tasks; missing or incorrect"
            SCOPE_VIOLATIONS+=("$rel_file (scope=$scope_val missing/incorrect guard)")
            ERRORS=$((ERRORS + 1))
        fi
    fi
done < <(find "$REPO_ROOT/playbooks/" -type f \( -name '*.yml' -o -name '*.yaml' \) -print0)

# ---------------------------------------------------------------------------
# Check 5: top-level ansible_<fact> variables (Plan 00077)
# ---------------------------------------------------------------------------
# Ansible injects every gathered fact as a top-level variable, so
# `ansible_facts['env']` is also reachable as `ansible_env`. That injection
# (INJECT_FACTS_AS_VARS) is DEPRECATED and removed in ansible-core 2.24, at which
# point every such reference becomes an UNDEFINED VARIABLE — an error mid-play, on
# the machine, after earlier tasks have already changed system state.
#
# `--syntax-check` cannot see it (the name is defined today) and neither can any
# runtime test while the injection is still on, so this is a static check or
# nothing. It was found in deploy OUTPUT, which is exactly the wrong place: a
# deprecation warning scrolls past on a play someone happened to be watching.
#
# Keyed on a KNOWN FACT-NAME LIST, never on the `ansible_` prefix alone —
# `ansible_facts` itself, `ansible_version`, and any future repo-local
# `ansible_`-prefixed variable must not trip it. The list is the commonly-used
# subset; extend it when a new fact is adopted.
FACT_NAMES=(
    env distribution distribution_version distribution_major_version
    distribution_release hostname nodename fqdn domain
    user_id user_dir user_uid user_gid
    architecture machine os_family kernel kernel_version system
    memtotal_mb memfree_mb swaptotal_mb
    processor_vcpus processor_cores processor_count
    python_version date_time default_ipv4 default_ipv6 all_ipv4_addresses
    interfaces mounts devices lsb machine_id selinux
    service_mgr pkg_mgr virtualization_type virtualization_role
    product_name product_version form_factor bios_version
)
# Word-boundary both ends so `ansible_distribution` does not match inside
# `ansible_distribution_major_version` (the alternation is longest-first anyway,
# but \b makes it independent of ordering).
FACTVAR_PATTERN="\\bansible_($(IFS='|'; echo "${FACT_NAMES[*]}"))\\b"
FACTVAR_VIOLATIONS=()

factvar_rc=0
grep -rnP \
    --include='*.yml' \
    --include='*.yaml' \
    --exclude-dir=vendor \
    "$FACTVAR_PATTERN" \
    "${FF_SEARCH_DIRS[@]}" \
    > "$TMP_MATCHES" 2>"$TMP_GREP_ERR" || factvar_rc=$?

if [[ $factvar_rc -ge 2 ]]; then
    echo "ERROR: deprecated-fact-var grep failed (rc=$factvar_rc):" >&2
    cat "$TMP_GREP_ERR" >&2
    exit 2
fi

while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    # A YAML comment mentioning the old name is documentation, not a reference.
    # Matches `# ...` and `    # ...`, not a trailing comment on a live line —
    # a live line with the reference still counts, whatever follows it.
    [[ "${line#*:*:}" =~ ^[[:space:]]*# ]] && continue
    rel_line="${line#"$REPO_ROOT"/}"
    echo "  ERROR (deprecated fact var): $rel_line"
    echo "    Use ansible_facts['<name>'] — top-level injection is removed in ansible-core 2.24."
    FACTVAR_VIOLATIONS+=("$rel_line")
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

# Encode SCOPE_VIOLATIONS as a JSON array
sc_json_array="[]"
for v in "${SCOPE_VIOLATIONS[@]+"${SCOPE_VIOLATIONS[@]}"}"; do
    sc_json_array=$(printf '%s' "$sc_json_array" | jq --arg v "$v" '. + [$v]')
done

# Encode FACTVAR_VIOLATIONS as a JSON array
fv_json_array="[]"
for v in "${FACTVAR_VIOLATIONS[@]+"${FACTVAR_VIOLATIONS[@]}"}"; do
    fv_json_array=$(printf '%s' "$fv_json_array" | jq --arg v "$v" '. + [$v]')
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
    --argjson sc "$sc_json_array" \
    --argjson fv "$fv_json_array" \
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
            ($sr | map({"file": ., "type": "ansible", "status": "fail", "error": "self-referential var (Ansible 2.19 recursive-loop error at runtime)"})) +
            ($sc | map({"file": (. | split(" (")[0]), "type": "ansible", "status": "fail", "error": ("scope: " + (. | split(" (")[1] | rtrimstr(")"))) })) +
            ($fv | map({"file": ., "type": "ansible", "status": "fail", "error": "top-level ansible_<fact> var (removed in ansible-core 2.24)"}))
        ),
        "results": [],
        "checks": {
            "fail_fast": $ff,
            "hygiene":   $hy,
            "self_ref":  $sr,
            "scope":     $sc,
            "fact_vars": $fv
        }
    }' > "$JSON_OUT"

# ---------------------------------------------------------------------------
# Terse summary
# ---------------------------------------------------------------------------
if [[ $ERRORS -eq 0 ]]; then
    echo "✓ ansible: fail-fast patterns OK; no self-referential vars; no deprecated ansible_<fact> vars; scope+guard declarations OK; $PLAYBOOK_COUNT playbook(s) have correct shebang+exec"
    exit 0
else
    FF_COUNT=${#FF_VIOLATIONS[@]}
    HY_COUNT=${#HYGIENE_VIOLATIONS[@]}
    SR_COUNT=${#SELFREF_VIOLATIONS[@]}
    SC_COUNT=${#SCOPE_VIOLATIONS[@]}
    FV_COUNT=${#FACTVAR_VIOLATIONS[@]}
    echo "✗ ansible: $ERRORS violation(s) — $FF_COUNT fail-fast, $HY_COUNT hygiene, $SR_COUNT self-ref var, $SC_COUNT scope/guard, $FV_COUNT deprecated fact var"
    echo "  Hygiene fixer: ./scripts/make-playbooks-executable.bash"
    echo "  Details: jq '.failures[]' $JSON_OUT"
    exit 1
fi
