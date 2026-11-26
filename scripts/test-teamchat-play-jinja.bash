#!/usr/bin/env bash
# The Jinja of playbooks/imports/play-teamchat.yml that carries LXC members, rendered by the real
# ansible over good and bad inputs (Plan 00209 Task 3.3). The Go tests in
# src-go/internal/teamchat/installplay compare the play's template TEXT with what teamchat-admin
# reads; this suite runs the templates, so a filter that does not do what its text suggests
# (selectattr, combine, zip and join, the order of the input conditions) is caught here.
#
# What is rendered, all read from the real play, never copied into this file:
#   1. the input check's LXC conditions (every condition that names tc_lxc), in their order, over
#      each case: a good one passes, a bad one fails at the condition the case names;
#   2. tc_lxc_absent_teams, tc_lxc_present_teams and tc_lxc_declaration, and the stdin of
#      `team approve` and `team apply` for each present team, over good cases with and without
#      teamchat_only_teams and teamchat_site;
#   3. the present phase's failed_when over `lxc apply` lines (a reach line fails it, a stopped
#      container or a signed-out gh does not) and the held-back task's msg over both phases' lines;
#   4. teamchat_lxc_seat_restore_starts_containers (Plan 00266 C.2): its input check, the argv that prints
#      the boot unit with or without its container start, and when the two restart units are enabled.
#
# Nothing here contacts a host or changes one: ansible runs against localhost with a throwaway
# config and inventory, and every write lands in a temp directory. The repo's ansible.cfg is NOT
# used (its play_ledger callback would record these runs, and it needs the vault password file).
# TEAMCHAT_PLAY names another copy of the play to judge (the red proof runs it on an old one).
#
# `set -e` is deliberately NOT used: every case runs so the summary is complete, and each result
# is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PLAY_FILE="${TEAMCHAT_PLAY:-$REPO_ROOT/playbooks/imports/play-teamchat.yml}"

for tool in ansible-playbook python3; do
    if ! command -v "$tool" >/dev/null; then
        echo "ERROR: $tool not found — this suite renders the real play's templates with it" >&2
        exit 2
    fi
done
if [ ! -f "$PLAY_FILE" ]; then
    echo "FAIL: $PLAY_FILE not found" >&2
    exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/tmp" "$work/out"
printf '[defaults]\nretry_files_enabled = False\nstdout_callback = ansible.builtin.default\n' \
    > "$work/ansible.cfg"
printf 'all:\n  hosts:\n    localhost:\n      ansible_connection: local\n      ansible_python_interpreter: "{{ ansible_playbook_python }}"\n' \
    > "$work/inventory.yml"

# run_ansible <command> <args...> — a sealed ansible run. The caller's ANSIBLE_* settings are
# dropped, so none of them can route this through the repo's config or its callbacks.
run_ansible() {
    env -i PATH="$PATH" HOME="$HOME" LANG=C.UTF-8 \
        ANSIBLE_CONFIG="$work/ansible.cfg" \
        ANSIBLE_LOCAL_TEMP="$work/tmp" ANSIBLE_REMOTE_TEMP="$work/tmp" \
        "$@" </dev/null 2>&1
}

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

# The cases: the inputs, and for the renders the lines `lxc apply` printed. Placeholders only.
cat > "$work/cases.json" <<'EOF'
{
  "teams": [
    {"team": "alpha", "state": "present", "port": 8448, "url": "https://203.0.113.1:8448"},
    {"team": "beta", "state": "absent"},
    {"team": "gamma", "state": "present", "port": 8449, "url": "https://203.0.113.1:8449"}
  ],
  "seat": {"team": "alpha", "container": "box-1", "user": "agent", "checkout": "/srv/checkouts/thing", "seat": 1},
  "lines": {
    "changed": "CHANGED\talpha\tagent@box-1/thing-1-0123456789abcdef\tjoined as thing.1+desk.lxc",
    "stopped": "DEFERRED\talpha\tagent@box-2/thing-1-0123456789abcdef\toffline: container stopped",
    "gh": "SKIPPED\talpha\tagent@box-3/thing-1-0123456789abcdef\toffline: guest check gh",
    "reach": "DEFERRED\talpha\tagent@box-4/thing-1-0123456789abcdef\toffline: guest check reach",
    "held": "DEFERRED\tbeta\tagent@box-5/thing-2-0123456789abcdef\tseat still held; token revoked"
  }
}
EOF

# Builds the render playbook from the real play: its vars verbatim (templated lazily, so the ones
# no case names are never rendered), its LXC input conditions, its two stdin templates, the present
# phase's failed_when and the held-back task's msg; one block per case, each writing what it
# rendered to out/<case>.json. A case's inputs are the block's vars.
if ! build_out=$(python3 - "$work/play.json" "$work/cases.json" "$work/render.yml" "$work/out" <<'PY'
import copy
import json
import sys

play_path, cases_path, render_path, out_dir = sys.argv[1:]
play = json.load(open(play_path))[0]
fixture = json.load(open(cases_path))
tasks = {t.get("name"): t for t in play["tasks"]}


def task(name, module):
    if name not in tasks or module not in tasks[name]:
        sys.exit(f"the play has no task {name!r} calling {module}")
    return tasks[name]


conditions = [c for c in task("Check the Play's Inputs", "ansible.builtin.assert")["ansible.builtin.assert"]["that"] if "tc_lxc" in c]
stdin = {
    "approve": task("Approve the Teams the Operator Names", "ansible.builtin.command")["ansible.builtin.command"]["stdin"],
    "apply": task("Apply the Present Teams", "ansible.builtin.command")["ansible.builtin.command"]["stdin"],
}
present = task("Place and Join the Declared LXC Members", "ansible.builtin.command")
held = task("Show the LXC Members Held Back", "ansible.builtin.debug")
if not conditions or "failed_when" not in present:
    sys.exit("the play has no LXC input condition, or the present phase has no failed_when")

seat = fixture["seat"]


def as_json(renders):
    """A template writing each named Jinja expression's value, as one JSON object."""
    return "{{ {" + ", ".join(f"{json.dumps(k)}: ({v})" for k, v in renders.items()) + "} | to_json }}"


def with_(**changes):
    entry = copy.deepcopy(seat)
    for key, value in changes.items():
        if value is None:
            entry.pop(key)
        else:
            entry[key] = value
    return entry


# name: (lxc members, extra vars). A bad case's expected failing condition is checked by bash.
cases = {
    "good": ([seat, with_(seat=2), with_(container="box-2", user="other")], {}),
    "good-scoped-alpha": ([seat], {"teamchat_only_teams": ["alpha"]}),
    "good-scoped-beta-site": ([seat], {"teamchat_only_teams": ["beta"], "teamchat_site": "desk"}),
    "good-none": ([], {}),
    "not-a-list": ("box-1", {}),
    "not-a-mapping": ([seat, "box-1"], {}),
    "no-seat": ([with_(seat=None)], {}),
    "no-checkout": ([with_(checkout=None)], {}),
    "string-seat": ([with_(seat="1")], {}),
    "boolean-seat": ([with_(seat=True)], {}),
    "bad-state": ([with_(state="gone")], {}),
    "absent-team": ([with_(team="beta")], {}),
    "undeclared-team": ([with_(team="delta")], {}),
    "duplicate": ([seat, dict(seat)], {}),
    "duplicate-other-user": ([seat, with_(user="other")], {}),
}
lines = fixture["lines"]
blocks = []
for name, (members, extra) in cases.items():
    reg = name.replace("-", "_")
    case_vars = {"teamchat_teams": fixture["teams"], "teamchat_lxc_members": members, **extra}
    good = name.startswith("good")
    body = [{
        "name": "Input conditions",
        "ansible.builtin.assert": {"that": conditions, "quiet": True},
        "ignore_errors": True,
        "register": f"{reg}_check",
    }]
    renders = {"failed": f"{reg}_check.failed", "assertion": f"{reg}_check.assertion | default('')",
               "msg": f"{reg}_check.msg | default('')"}
    if good:
        for verb, template in stdin.items():
            body.append({
                "name": f"Render {verb} stdin",
                "ansible.builtin.debug": {"msg": template},
                "loop": "{{ tc_scoped_present }}",
                "register": f"{reg}_{verb}",
            })
            renders[verb] = f"{reg}_{verb}.results | default([]) | map(attribute='msg') | map('from_json') | list"
        renders.update({
            "absent_teams": "tc_lxc_absent_teams",
            "present_teams": "tc_lxc_present_teams",
            "declaration": "tc_lxc_declaration | from_json",
        })
    body.append({
        "name": "Write what was rendered",
        "ansible.builtin.copy": {"dest": f"{out_dir}/{name}.json", "content": as_json(renders), "mode": "0600"},
    })
    blocks.append({"name": f"Case {name}", "vars": case_vars, "block": body})

# The present phase's failed_when and the held-back msg, over registered results set as vars.
phase_cases = {
    "phase-stopped-gh": ({"rc": 0, "stdout_lines": [lines["changed"], lines["stopped"], lines["gh"]]},
                         {"rc": 0, "stdout_lines": [lines["held"]]}),
    "phase-reach": ({"rc": 0, "stdout_lines": [lines["changed"], lines["reach"]]}, {"rc": 0, "stdout_lines": []}),
    "phase-rc": ({"rc": 1, "stdout_lines": []}, {"rc": 0, "stdout_lines": []}),
    "phase-skipped-absent": ({"rc": 0, "stdout_lines": [lines["gh"]]}, None),
}
for name, (present_result, absent_result) in phase_cases.items():
    case_vars = {"tc_lxc_present": present_result}
    if absent_result is not None:
        case_vars["tc_lxc_absent"] = absent_result
    msg = held["ansible.builtin.debug"]["msg"]
    if not (msg.startswith("{{ ") and msg.endswith(" }}")):
        sys.exit(f"the held-back task's msg is not one expression: {msg!r}")
    renders = {"failed_when": present["failed_when"], "held": msg[3:-3]}
    blocks.append({"name": f"Case {name}", "vars": case_vars, "block": [{
        "name": "Write what was rendered",
        "ansible.builtin.copy": {"dest": f"{out_dir}/{name}.json", "content": as_json(renders), "mode": "0600"},
    }]})

# The boot unit's container start (Plan 00266 C.2): the input check on
# teamchat_lxc_seat_restore_starts_containers, the argv that prints the boot unit, and whether the two
# restart units are enabled, over the variable unset, true, false and a quoted "false".
inputs = task("Check the Play's Inputs", "ansible.builtin.assert")["ansible.builtin.assert"]["that"]
starts_conditions = [c for c in inputs if "teamchat_lxc_seat_restore_starts_containers" in c]
print_argv = task("Print Each Unit's Text", "ansible.builtin.command")["ansible.builtin.command"]["argv"]
record_enabled = task("Run the LXC Record Unit While an LXC Member Is Present", "ansible.builtin.systemd_service")["ansible.builtin.systemd_service"]
boot_enabled = task("Enable the LXC Boot Unit While an LXC Member Is Present", "ansible.builtin.systemd_service")["ansible.builtin.systemd_service"]
if len(starts_conditions) != 1 or not isinstance(print_argv, str):
    sys.exit("the play has no one input condition on teamchat_lxc_seat_restore_starts_containers, or its print argv is not one template")


def expression(template):
    if not (template.startswith("{{ ") and template.endswith(" }}")):
        sys.exit(f"not one expression: {template!r}")
    return template[3:-3]


starts_cases = {
    "starts-unset": ({}, [seat]),
    "starts-true": ({"teamchat_lxc_seat_restore_starts_containers": True}, [seat]),
    "starts-false": ({"teamchat_lxc_seat_restore_starts_containers": False}, [seat]),
    "starts-quoted": ({"teamchat_lxc_seat_restore_starts_containers": "false"}, [seat]),
    "starts-no-member": ({}, []),
    "starts-absent-member": ({}, [with_(state="absent")]),
}
for name, (extra, members) in starts_cases.items():
    reg = name.replace("-", "_")
    case_vars = {"teamchat_teams": fixture["teams"], "teamchat_lxc_members": members,
                 "item": "teamchat-lxc-boot.service\t/etc/systemd/system/teamchat-lxc-boot.service", **extra}
    body = [{
        "name": "Input condition",
        "ansible.builtin.assert": {"that": starts_conditions, "quiet": True},
        "ignore_errors": True,
        "register": f"{reg}_check",
    }]
    renders = {"failed": f"{reg}_check.failed",
               "record": f"[{expression(record_enabled['enabled'])}, {expression(record_enabled['state'])}]",
               "boot": expression(boot_enabled["enabled"])}
    if name != "starts-quoted":
        renders["argv"] = expression(print_argv)
    body.append({
        "name": "Write what was rendered",
        "ansible.builtin.copy": {"dest": f"{out_dir}/{name}.json", "content": as_json(renders), "mode": "0600"},
    })
    blocks.append({"name": f"Case {name}", "vars": case_vars, "block": body})

render = [{"hosts": "localhost", "gather_facts": False, "vars": play["vars"], "tasks": blocks}]
json.dump(render, open(render_path, "w"), indent=1)
PY
); then
    echo "FAIL: the render playbook could not be built from $PLAY_FILE:" >&2
    printf '%s\n' "$build_out" >&2
    exit 1
fi

if ! render_out=$(run_ansible ansible-playbook -i "$work/inventory.yml" "$work/render.yml"); then
    echo "FAIL: ansible could not render the cases:" >&2
    printf '%s\n' "$render_out" >&2
    exit 1
fi

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

# field <case> <python expression over the case's rendered JSON as `r`> — printed as JSON.
field() {
    python3 -c 'import json, sys; r = json.load(open(sys.argv[1])); print(json.dumps(eval(sys.argv[2]), sort_keys=True))' \
        "$work/out/$1.json" "$2" 2>&1
}

echo "=== the input check's LXC conditions ==="
for name in good good-scoped-alpha good-scoped-beta-site good-none; do
    check "$name: passes" "false" "$(field "$name" 'r["failed"]')"
done
# bad <case> <the text of the condition it must fail at>
bad() {
    check "$1: refused" "true" "$(field "$1" 'r["failed"]')"
    check "$1: refused by its own condition" "\"$2\"" "$(field "$1" 'r["assertion"]')"
}
bad not-a-list "tc_lxc is sequence and tc_lxc is not string"
bad not-a-mapping "tc_lxc | reject('mapping') | list | length == 0"
bad no-seat "tc_lxc | rejectattr('seat', 'defined') | list | length == 0"
bad no-checkout "tc_lxc | rejectattr('checkout', 'defined') | list | length == 0"
bad string-seat "tc_lxc | map(attribute='seat') | reject('integer') | list | length == 0"
bad boolean-seat "tc_lxc | map(attribute='seat') | reject('integer') | list | length == 0"
bad bad-state "tc_lxc | selectattr('state', 'defined') | map(attribute='state') | difference(['present', 'absent']) | length == 0"
bad absent-team "tc_lxc | map(attribute='team') | difference(tc_present | map(attribute='team') | list) | length == 0"
bad undeclared-team "tc_lxc | map(attribute='team') | difference(tc_present | map(attribute='team') | list) | length == 0"
uniqueness="tc_lxc | map(attribute='team') | zip(tc_lxc | map(attribute='container'), tc_lxc | map(attribute='checkout'), tc_lxc | map(attribute='seat')) | map('join', '|') | unique | list | length == tc_lxc | length"
bad duplicate "$uniqueness"
bad duplicate-other-user "$uniqueness"

echo "=== the LXC phases' teams and stdin ==="
check "unscoped: the absent phase acts on every present and absent team" '["alpha", "gamma", "beta"]' "$(field good 'r["absent_teams"]')"
check "unscoped: the present phase acts on the present teams" '["alpha", "gamma"]' "$(field good 'r["present_teams"]')"
check "scoped to alpha: the absent phase acts on alpha alone" '["alpha"]' "$(field good-scoped-alpha 'r["absent_teams"]')"
check "scoped to absent beta: the absent phase acts on beta, the team leaving" '["beta"]' "$(field good-scoped-beta-site 'r["absent_teams"]')"
check "scoped to absent beta: the present phase acts on no team" '[]' "$(field good-scoped-beta-site 'r["present_teams"]')"
check "the declaration holds host_vars' keys, no site when none is set" '["teamchat_lxc_members", "teamchat_teams"]' "$(field good 'sorted(r["declaration"])')"
check "the declaration carries every member and team as given" 'true' \
    "$(field good 'len(r["declaration"]["teamchat_lxc_members"]) == 3 and len(r["declaration"]["teamchat_teams"]) == 3')"
check "the declaration carries teamchat_site when set" '"desk"' "$(field good-scoped-beta-site 'r["declaration"]["teamchat_site"]')"
for verb in approve apply; do
    check "$verb stdin: one object per present team, its team and only its own members" \
        '[["alpha", 3, ["alpha"]], ["gamma", 0, []]]' \
        "$(field good "[[s[\"team\"][\"team\"], len(s[\"lxc_members\"]), sorted({m[\"team\"] for m in s[\"lxc_members\"]})] for s in r[\"$verb\"]]")"
    check "$verb stdin: the keys declare.DecodeInput reads" '[["lxc_members", "team"]]' \
        "$(field good-scoped-alpha "[sorted(s) for s in r[\"$verb\"]]")"
done

echo "=== the present phase's failure and the held-back lines ==="
check "a stopped container and a signed-out gh do not fail the run" "false" "$(field phase-stopped-gh 'r["failed_when"]')"
check "a guest that cannot reach its team fails the run" "true" "$(field phase-reach 'r["failed_when"]')"
check "a failed lxc apply fails the run" "true" "$(field phase-rc 'r["failed_when"]')"
check "both phases' held-back lines are shown, no changed line" \
    '["DEFERRED\tbeta\tagent@box-5/thing-2-0123456789abcdef\tseat still held; token revoked", "DEFERRED\talpha\tagent@box-2/thing-1-0123456789abcdef\toffline: container stopped", "SKIPPED\talpha\tagent@box-3/thing-1-0123456789abcdef\toffline: guest check gh"]' \
    "$(field phase-stopped-gh 'r["held"]')"
check "a skipped absent phase (no register) is no error" \
    '["SKIPPED\talpha\tagent@box-3/thing-1-0123456789abcdef\toffline: guest check gh"]' \
    "$(field phase-skipped-absent 'r["held"]')"

echo "=== the boot unit's container start (teamchat_lxc_seat_restore_starts_containers) ==="
admin_print='["/usr/local/sbin/teamchat-admin", "units", "print"'
for name in starts-unset starts-true starts-false; do
    check "$name: passes the input check" "false" "$(field "$name" 'r["failed"]')"
done
check "a quoted \"false\" is refused" "true" "$(field starts-quoted 'r["failed"]')"
check "unset: the boot unit is printed with its container start" "$admin_print, \"teamchat-lxc-boot.service\"]" "$(field starts-unset 'r["argv"]')"
check "true: the boot unit is printed with its container start" "$admin_print, \"teamchat-lxc-boot.service\"]" "$(field starts-true 'r["argv"]')"
check "false: the boot unit is printed without it, so it runs only reconcile" \
    "$admin_print, \"--no-container-start\", \"teamchat-lxc-boot.service\"]" "$(field starts-false 'r["argv"]')"
check "a present member: the record unit is enabled and started" '[true, "started"]' "$(field starts-unset 'r["record"]')"
check "a present member: the boot unit is enabled" "true" "$(field starts-false 'r["boot"]')"
for name in starts-no-member starts-absent-member; do
    check "$name: the record unit is disabled and stopped" '[false, "stopped"]' "$(field "$name" 'r["record"]')"
    check "$name: the boot unit is disabled" "false" "$(field "$name" 'r["boot"]')"
done

echo
echo "passed: $passed  failed: $failed"
[ "$failed" -eq 0 ]
