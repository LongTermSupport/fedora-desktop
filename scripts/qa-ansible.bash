#!/usr/bin/env bash
# qa-ansible.bash — Enforce fail-fast in Ansible playbooks
# Flags error-hiding patterns without FAIL-FAST-OK justification
set -euo pipefail

cd "$(dirname "$0")/.."

patterns='failed_when: false|ignore_errors: true|ignore_errors: yes|ignore_unreachable: true'
violations=0

# grep returns rc=1 when no matches — handle explicitly
matches=""
if grep -rn --include='*.yml' -E "$patterns" playbooks/ > /tmp/qa-ansible-matches 2>/dev/null; then
  matches=$(cat /tmp/qa-ansible-matches)
fi
rm -f /tmp/qa-ansible-matches

if [[ -n "$matches" ]]; then
  while IFS= read -r line; do
    if ! echo "$line" | grep -q 'FAIL-FAST-OK'; then
      echo "  ERROR: $line"
      (( violations++ ))
    fi
  done <<< "$matches"
fi

if [[ "$violations" -gt 0 ]]; then
  echo "✗ ansible fail-fast: $violations violation(s) found"
  echo "  Add '# FAIL-FAST-OK: <reason>' to justify each instance"
  exit 1
else
  echo "✓ ansible fail-fast: all instances justified"
fi
