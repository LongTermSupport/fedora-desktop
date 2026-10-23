#!/usr/bin/env bash
# test-umask-pin.bash — the rule ansible-umask-pinned and the umask_pin callback it keeps
# enabled (Plan 00185; CLAUDE/QA.md "ansible-umask-pinned").
#
# Runs tests/umask_pin/test_umask_pinned_rule.py and
# tests/umask_pin/test_umask_pin_against_real_ansible.py: the rule, proven on its fixtures and then run over ansible.cfg and
# every tracked file that sets Ansible's callback list or directories; and a real probe play
# started under an inherited umask 077, whose files written with no mode must come out 0644
# (0600 in the control run with the callbacks off). It names ansible.cfg and
# callback_plugins/umask_pin.py so that qa-changed runs it when either changes: a pin that
# fails to load only warns, so this probe is what notices.
#
# Exit: 0 every test passed, 1 otherwise.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

passed=0
failed=0
for module in tests.umask_pin.test_umask_pinned_rule tests.umask_pin.test_umask_pin_against_real_ansible; do
    if (cd "$REPO_ROOT" && python3 -m unittest "$module"); then
        passed=$((passed + 1))
    else
        echo "  FAIL: $module" >&2
        failed=$((failed + 1))
    fi
done

printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
