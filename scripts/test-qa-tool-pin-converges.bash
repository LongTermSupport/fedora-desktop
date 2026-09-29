#!/usr/bin/env bash
# play-python.yml converges a QA tool installed at the wrong version onto its pin
# (Plan 00142).
#
# pipx cannot replace a venv in place under its uv backend: `pipx install --force` re-runs
# `uv venv` on the existing directory and uv refuses ("A virtual environment already
# exists"). So a pinned tool found at another version is removed with state: absent and
# then installed without force, and the pin task runs after both, because a venv made
# afresh is unpinned.
#
# Two parts, both read from the real play, parsed by ansible:
#   1. the shape: probe, then removal, then install, then pin; removal and install select
#      the same probe results with the same `when:`; no pipx task in the play sets
#      `force: true`; the pin list names every probed tool.
#   2. the selection, evaluated: the removal task's own `when:` under ansible, for a tool
#      at its pin (left alone), at another version (removed) and missing (removed).
#
# What this cannot prove is pipx's and uv's own behaviour: that needs a pipx with the uv
# backend, which neither CI nor the CCY image has. Plan 00142's reproduce-pipx-uv.bash runs
# these tasks against a real pipx and uv, before and after the change.
#
# Nothing here contacts a host: ansible runs against localhost with a throwaway config and
# inventory, and writes only to a temp directory. The repo's ansible.cfg is deliberately NOT
# used: its play_ledger callback would record these runs, and it needs the vault password.
#
# Usage: test-qa-tool-pin-converges.bash [play file]   (default: the repo's play-python.yml)
#
# `set -e` is deliberately NOT used: every case runs so the summary is complete, and each
# result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PLAY_FILE="${1:-$REPO_ROOT/playbooks/imports/play-python.yml}"

for tool in ansible-playbook python3; do
    if ! command -v "$tool" >/dev/null; then
        echo "ERROR: $tool not found — this suite parses and evaluates the real play with it" >&2
        exit 2
    fi
done
if [ ! -f "$PLAY_FILE" ]; then
    echo "FAIL: $PLAY_FILE not found" >&2
    exit 1
fi
PLAY_FILE="$(cd "$(dirname "$PLAY_FILE")" && pwd)/$(basename "$PLAY_FILE")"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/tmp"
printf '[defaults]\nretry_files_enabled = False\nstdout_callback = ansible.builtin.default\n' \
    > "$work/ansible.cfg"
printf 'all:\n  hosts:\n    localhost:\n      ansible_connection: local\n      ansible_python_interpreter: "{{ ansible_playbook_python }}"\n' \
    > "$work/inventory.yml"

# run_ansible <command> <args...> — a sealed ansible run. The caller's ANSIBLE_* settings are
# dropped, bar where collections live, so none of them can route this through the repo's
# config or its callbacks.
run_ansible() {
    env -i PATH="$PATH" HOME="$HOME" LANG=C.UTF-8 \
        ${ANSIBLE_COLLECTIONS_PATH:+ANSIBLE_COLLECTIONS_PATH="$ANSIBLE_COLLECTIONS_PATH"} \
        ANSIBLE_CONFIG="$work/ansible.cfg" \
        ANSIBLE_LOCAL_TEMP="$work/tmp" ANSIBLE_REMOTE_TEMP="$work/tmp" \
        "$@" </dev/null 2>&1
}

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label → '$got' (wanted '$want')"
    fi
}

echo "=== play-python.yml: remove, then install, then pin ==="

# Ansible parses the play (the same parser that will run it) and hands it over as JSON.
cat > "$work/dump.yml" <<EOF
- hosts: localhost
  gather_facts: false
  tasks:
    - name: Dump the Play as JSON
      ansible.builtin.copy:
        dest: "$work/play.json"
        content: "{{ lookup('ansible.builtin.file', '$PLAY_FILE') | from_yaml | to_json }}"
        mode: "0600"
EOF
if ! dump_out=$(run_ansible ansible-playbook -i "$work/inventory.yml" "$work/dump.yml"); then
    echo "FAIL: ansible could not parse $PLAY_FILE:" >&2
    printf '%s\n' "$dump_out" >&2
    exit 1
fi

# Prints one `<key> <value>` line per fact the checks below compare, and writes the removal
# task's `when:` into a playbook of its own for part 2.
if ! facts=$(python3 - "$work/play.json" "$work/selection.yml" "$work" <<'PY'
import json
import sys

PIPX = "community.general.pipx"
PROBED = "{{ qa_tool_installed.results }}"


def flatten(tasks):
    for task in tasks:
        if "block" in task:
            for key in ("block", "rescue", "always"):
                yield from flatten(task.get(key, []))
        else:
            yield task


def conditions(task):
    when = task.get("when", [])
    return [when] if isinstance(when, str) else list(when)


plays_file, selection_file, work = sys.argv[1:4]
with open(plays_file, encoding="utf-8") as handle:
    plays = json.load(handle)
tasks = [task for play in plays for task in flatten(play.get("tasks", []))]


def positions(match):
    return [index for index, task in enumerate(tasks) if match(task)]


probe = positions(lambda t: t.get("register") == "qa_tool_installed")
remove = positions(lambda t: t.get(PIPX, {}).get("state") == "absent" and t.get("loop") == PROBED)
install = positions(lambda t: t.get(PIPX, {}).get("state") == "install" and t.get("loop") == PROBED)
pin = positions(lambda t: t.get(PIPX, {}).get("state") == "pin")
print(f"probes {len(probe)}")
print(f"removals {len(remove)}")
print(f"installs {len(install)}")
print(f"pins {len(pin)}")
if len(probe) == len(remove) == len(install) == len(pin) == 1:
    order = [probe[0], remove[0], install[0], pin[0]]
    print(f"ordered {'yes' if order == sorted(order) else 'no'}")
    removal, installer = tasks[remove[0]], tasks[install[0]]
    print(f"same-when {'yes' if conditions(removal) and conditions(removal) == conditions(installer) else 'no'}")
    print(f"same-name {'yes' if removal[PIPX].get('name') == installer[PIPX].get('name') else 'no'}")
    probed = sorted(item["pkg"] for item in tasks[probe[0]].get("loop", []))
    print(f"pinned-all {'yes' if sorted(tasks[pin[0]].get('loop', [])) == probed else 'no'}")
    with open(selection_file, "w", encoding="utf-8") as handle:
        json.dump([{
            "hosts": "localhost",
            "gather_facts": False,
            "tasks": [{
                "name": "Record Each Tool the Removal Task Selects",
                "ansible.builtin.copy": {
                    "dest": work + "/selected-{{ item.item.cmd }}",
                    "content": "removed\n",
                    "mode": "0600",
                },
                "when": removal.get("when"),
                "loop": "{{ probe_results }}",
            }],
        }], handle)
for task in tasks:
    if task.get(PIPX, {}).get("force"):
        print(f"forced {task.get('name', '<unnamed task>')}")
PY
); then
    echo "FAIL: the play walker crashed" >&2
    exit 1
fi

fact() { awk -v key="$1" '$1 == key { sub(/^[^ ]+ /, ""); print }' <<< "$facts"; }

check "one version probe registers qa_tool_installed" "1" "$(fact probes)"
check "one pipx task removes the probed tools (state: absent)" "1" "$(fact removals)"
check "one pipx task installs the probed tools (state: install)" "1" "$(fact installs)"
check "one pipx task pins the QA tools" "1" "$(fact pins)"
check "the order is probe, remove, install, pin" "yes" "$(fact ordered)"
check "removal and install select the same probe results" "yes" "$(fact same-when)"
check "removal and install name the same package" "yes" "$(fact same-name)"
check "the pin list names every probed tool" "yes" "$(fact pinned-all)"
# `install --force` onto an existing venv fails under the uv backend, so force is never the
# mechanism that replaces a venv here.
check "no pipx task sets force: true" "" "$(fact forced)"

echo "=== the removal task's when:, evaluated ==="

if [ ! -f "$work/selection.yml" ]; then
    failed=$((failed + 1))
    echo "  FAIL: not evaluated — the play has no single removal task to lift"
else
    # Probe results shaped as the command module registers them in a loop.
    cat > "$work/probe-vars.yml" <<'EOF'
probe_results:
  - {item: {cmd: ruff, pkg: ruff, version: "0.16.8"}, rc: 0, stdout: "ruff 0.16.8"}
  - {item: {cmd: semgrep, pkg: semgrep, version: "1.177.0"}, rc: 0, stdout: "1.178.0"}
  - {item: {cmd: absent-tool, pkg: absent-tool, version: "1.0.0"}, rc: 2, stdout: ""}
EOF
    if ! select_out=$(run_ansible ansible-playbook -i "$work/inventory.yml" \
        -e "@$work/probe-vars.yml" "$work/selection.yml"); then
        echo "  ansible could not evaluate the removal task's when:" >&2
        printf '%s\n' "$select_out" >&2
    fi
    selected() { if [ -f "$work/selected-$1" ]; then printf 'removed'; else printf 'kept'; fi; }
    check "a tool at its pin is kept" "kept" "$(selected ruff)"
    check "a tool at another version is removed" "removed" "$(selected semgrep)"
    check "a tool whose probe failed is removed" "removed" "$(selected absent-tool)"
fi

echo
echo "passed: $passed  failed: $failed"
[ "$failed" -eq 0 ]
