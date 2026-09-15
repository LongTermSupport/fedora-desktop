#!/usr/bin/bash
# Validate the tracked VM-test scenario manifest (Plan 00110).
#
# vars/vm-test-scenarios.yml is the source of the bridge's scenario allowlist.
# Ansible renders it to JSON on the host and helpers/vmtest/scenarios.py
# validates that JSON — but helpers are stdlib-only and cannot read YAML, so
# without this gate a malformed manifest would only be discovered at deploy
# time, on the host, by the playbook. This gate converts the YAML with PyYAML,
# which Ansible itself depends on and which qa-ansible-syntax.bash already
# assumes, and pipes the result through the same validator the host will use.
#
# Two control checks make the gate falsifiable (CLAUDE/QA.md "Changing a
# Gate"): a deliberately broken manifest must be rejected, and the validator's
# OK marker must actually appear for the real one. A gate that passes when its
# validator never ran would be worse than no gate.
#
#   ./scripts/qa-vmtest-manifest.bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

MANIFEST="vars/vm-test-scenarios.yml"
VERSION_FILE="vars/fedora-version.yml"

fedora_version="$(grep -E '^fedora_version:' "$VERSION_FILE" | awk '{print $2}')"
if [[ ! "$fedora_version" =~ ^[0-9]+$ ]]; then
    echo "ERROR: no integer fedora_version in $VERSION_FILE" >&2
    exit 1
fi

yaml_to_json() {
    python3 -c 'import json, sys, yaml; json.dump(yaml.safe_load(sys.stdin), sys.stdout)'
}

validate() {
    python3 -m helpers.vmtest.validate_manifest --fedora-version "$fedora_version"
}

# Control 1: a broken manifest must be rejected, or the validator is not judging.
control_out=""
if control_out="$(printf 'vm_test_ttl_upgrade_days: 7\n' | yaml_to_json | validate 2>&1)"; then
    echo "ERROR: the validator accepted a manifest with no scenarios; the gate cannot be trusted" >&2
    exit 1
fi
if [[ "$control_out" != *VMTEST-MANIFEST-INVALID* ]]; then
    echo "ERROR: the validator rejected the control manifest without its INVALID marker: $control_out" >&2
    exit 1
fi

# The real manifest. Diagnostics reach the terminal; the marker is asserted.
out="$(yaml_to_json < "$MANIFEST" | validate)"
if [[ "$out" != VMTEST-MANIFEST-OK* ]]; then
    echo "ERROR: validator exited 0 without its OK marker for $MANIFEST: $out" >&2
    exit 1
fi
echo "$MANIFEST: ${out#VMTEST-MANIFEST-OK }"

# ── every scenario's `planned` matches its guest checker's PLANNED ────────────────────
#
# The count is declared in TWO places: the manifest, which the response contract judges
# against, and the guest script, which counts what actually ran. The guest script asserts its
# own total at the end of a run — but only after a whole VM has booted, so a mismatch is
# discovered minutes in, on the host, by a scenario that then reports `error`. This finds it
# at commit time.
#
# The script is resolved the way `vmtest` resolves it (files/home/.local/bin/vmtest):
# guest-acceptance-<scenario-id>.bash when the scenario ships its own, otherwise
# guest-acceptance-<profile>.bash. Resolving it differently here would let the gate pass
# while vouching for a file no run will ever use.
#
# The script arrives on fd 3, not on stdin: a `<<HEREDOC` would BE python's stdin, and the
# manifest JSON this reads is what stdin has to carry.
planned_check() {
    python3 /dev/fd/3 "$ROOT_DIR" 3<<'PYEOF'
import json
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])
scripts = root / "files/home/.local/share/vmtest"
document = json.load(sys.stdin)
bases = document.get("vm_test_bases") or {}
problems = []
checked = 0

for scenario_id, entry in sorted((document.get("vm_test_scenarios") or {}).items()):
    planned = entry.get("planned")
    if planned is None:
        continue  # not runnable; the guest script has not declared its count yet
    profile = (bases.get(entry.get("base")) or {}).get("profile")
    path = scripts / f"guest-acceptance-{scenario_id}.bash"
    if not path.exists():
        path = scripts / f"guest-acceptance-{profile}.bash"
    if not path.exists():
        problems.append(f"{scenario_id}: no guest checker at {path}")
        continue
    found = re.search(r"^PLANNED=(\d+)$", path.read_text(encoding="utf-8"), re.MULTILINE)
    if found is None:
        problems.append(f"{scenario_id}: no `PLANNED=<n>` line in {path.name}")
        continue
    declared = int(found.group(1))
    checked += 1
    if declared != planned:
        problems.append(
            f"{scenario_id}: manifest planned={planned} but {path.name} declares PLANNED={declared}"
        )

if problems:
    for problem in problems:
        sys.stderr.write(f"ERROR: {problem}\n")
    raise SystemExit(1)
sys.stdout.write(f"{checked} scenario(s) agree with their guest checker\n")
PYEOF
}

# Control 2: a manifest whose count disagrees with its checker must be rejected. Without
# this, a `planned_check` that silently matched nothing would report success.
control2_out=""
if control2_out="$(yaml_to_json < "$MANIFEST" |
    python3 -c 'import json,sys; d=json.load(sys.stdin); s=next(v for v in d["vm_test_scenarios"].values() if v.get("planned")); s["planned"] += 1; json.dump(d, sys.stdout)' |
    planned_check 2>&1)"; then
    echo "ERROR: the planned-vs-PLANNED check accepted a deliberate mismatch; it is not judging" >&2
    exit 1
fi
if [[ "$control2_out" != *"but guest-acceptance-"* ]]; then
    echo "ERROR: the planned-vs-PLANNED check rejected the control without naming the mismatch: $control2_out" >&2
    exit 1
fi

planned_out="$(yaml_to_json < "$MANIFEST" | planned_check)"
echo "$MANIFEST: ${planned_out}"
