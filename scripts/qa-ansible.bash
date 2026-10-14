#!/usr/bin/env bash
# qa-ansible.bash — static Ansible QA checks, over playbooks/ tasks/ vars/ environment/ roles/
# (roles/vendor excluded), *.yml and *.yaml:
#
#   fail-fast     failed_when / ignore_errors / ignore_unreachable hiding an error, without a
#                 same-line # FAIL-FAST-OK: justification (case-insensitive; templated values too)
#   hygiene       every playbook (a top-level "- hosts:") has the cd-to-repo-root shebang and the
#                 exec bit; fixer: ./scripts/make-playbooks-executable.bash
#   self-ref      a var defined as its own default: foo: "{{ foo | default(...) }}" (the
#                 ansible-core 2.19 recursive-loop footgun --syntax-check cannot see)
#   scope         one vars.scope in general|gnome|server per playbook, and the canonical 2-task
#                 guard on gnome and server plays only
#   fact vars     top-level ansible_<fact> variables (removed in ansible-core 2.24), the
#                 unprefixed getent_* ones, and a guessed uid in a /run/user path
#
# The tool is Go (src-go/qa/ansible, command src-go/qa/cmd/qa-ansible); lib/go-tool.bash builds
# it into a private directory under the cache and runs it, so the exit code reaches the caller.
# Specification: CLAUDE/Plan/00192-.../SPEC-qa-ansible.md.
#
# stdout:  terse — errors + summary only
# JSON:    ${QA_JSON_OUT:-/tmp/qa-ansible-results.json}
#
# jq usage:
#   jq '.status'                          # "pass" or "fail"
#   jq '.summary'                         # {total, passed, failed}
#   jq '.failures[]'                      # all violations
#   jq '.checks.fail_fast[]'              # one check's list (hygiene, self_ref, scope, fact_vars)
#
# Exit codes: 0 no violation, 1 a violation, 2 bad usage or the run could not complete (no
# Ansible directory, a missing playbooks/, an unreadable file, a build failure of the tool).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-ansible --scripts "$SCRIPT_DIR" "$@"
