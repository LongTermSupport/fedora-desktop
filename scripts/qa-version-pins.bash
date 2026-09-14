#!/usr/bin/bash
# Validate the tracked upstream version-pin manifest (Plan 00109, Task 2.2).
#
# vars/version-pins.yml says where every pinned upstream version lives. Two
# consumers read it — scripts/check-pinned-versions.bash (pin vs upstream) and
# Plan 00109's installed-vs-pinned check (pin vs this host) — and neither is run
# by qa-all: the first needs an authenticated gh, the second needs a real host.
# So without this gate a manifest that had drifted away from the playbooks would
# surface only when somebody happened to run a review tool.
#
# It checks two different things, and the second is the one that rots:
#   * the manifest's own shape, via helpers/version_pins/manifest.py (stdlib-only,
#     so this script owns the YAML conversion, using the PyYAML that Ansible
#     itself depends on and qa-ansible-syntax.bash already assumes);
#   * that every row still points at a playbook that exists and still declares
#     its var. A row naming a var that was renamed reports the OLD value for ever.
#
# Three control checks make the gate falsifiable (CLAUDE/QA.md "Changing a
# Gate"): a malformed manifest must be rejected, a row pointing at a missing file
# must be rejected, and the validator's OK marker must actually appear for the
# real one. A gate that passes when its validator never ran is worse than none.
#
#   ./scripts/qa-version-pins.bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

MANIFEST="vars/version-pins.yml"

yaml_to_json() {
    python3 -c 'import json, sys, yaml; json.dump(yaml.safe_load(sys.stdin), sys.stdout)'
}

validate() {
    python3 -m helpers.version_pins.manifest
}

# Control 1: an empty manifest must be rejected, or the validator is not judging.
control_out=""
if control_out="$(printf 'version_pins: []\n' | yaml_to_json | validate 2>&1)"; then
    echo "ERROR: the validator accepted a manifest with no pins; the gate cannot be trusted" >&2
    exit 1
fi
if [[ "$control_out" != *VERSION-PINS-INVALID* ]]; then
    echo "ERROR: the validator rejected the control manifest without its INVALID marker: $control_out" >&2
    exit 1
fi

# The real manifest. Its rows are the payload; the marker is asserted.
if ! out="$(yaml_to_json <"$MANIFEST" | validate)"; then
    echo "ERROR: $MANIFEST is invalid:" >&2
    echo "$out" >&2
    exit 1
fi
if [[ "$out" != VERSION-PINS-OK* ]]; then
    echo "ERROR: validator exited 0 without its OK marker for $MANIFEST: $out" >&2
    exit 1
fi

# Every row must still resolve on disk. `pin_is_live` is a function so the
# controls below can exercise the same code the real rows go through, rather
# than a copy of it that could agree with a broken original.
pin_is_live() {
    local file="$1" var="$2"
    [ -f "$file" ] || return 1
    grep -qE "^[[:space:]]*${var}[[:space:]]*:" "$file" || return 2
    return 0
}

# Control 2: a row naming a file that does not exist must fail the check, and a
# row naming a var that is not in its file must fail it for a DIFFERENT reason.
# Same-status controls would not tell the two failures apart.
if pin_is_live "playbooks/imports/no-such-playbook-00109.yml" "any_version"; then
    echo "ERROR: the on-disk check passed a playbook that does not exist" >&2
    exit 1
fi
if pin_is_live "$MANIFEST" "definitely_not_a_declared_var_00109"; then
    echo "ERROR: the on-disk check passed a var that is not declared" >&2
    exit 1
fi

rows=0
while IFS='|' read -r file var _repo _extra _note; do
    [ -z "$file" ] && continue
    rows=$((rows + 1))
    # Captured, not read from `$?` inside `if ! …; then` — there it is the status of
    # the NEGATION, always 0, so every case arm missed and the gate exited silently.
    # The control checks above are what surfaced that.
    status=0
    pin_is_live "$file" "$var" || status=$?
    if [ "$status" -ne 0 ]; then
        case "$status" in
            1) echo "ERROR: $MANIFEST names a playbook that does not exist: $file" >&2 ;;
            2) echo "ERROR: $MANIFEST names '$var', which $file no longer declares" >&2 ;;
            *) echo "ERROR: $MANIFEST row '$file' '$var' failed with status $status" >&2 ;;
        esac
        exit 1
    fi
done < <(printf '%s\n' "$out" | tail -n +2)

if [ "$rows" -eq 0 ]; then
    echo "ERROR: no rows reached the on-disk check, so nothing was verified" >&2
    exit 1
fi

echo "$MANIFEST: ${out%%$'\n'*} — all $rows resolve to a live playbook var"
