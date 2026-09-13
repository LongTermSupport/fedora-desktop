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
