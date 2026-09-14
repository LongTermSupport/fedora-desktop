#!/usr/bin/env bash
#
# check-pinned-versions.bash — review upstream version pins in the playbooks.
#
# Several playbooks hardcode an upstream release version in a play var (e.g.
# `marklessVersion: "0.9.6"`). Those pins drift as upstream ships new releases.
# This script reads the CURRENT pin straight from each playbook (the playbook is
# the single source of truth — the version is never duplicated here), queries
# the upstream project for its latest release, and reports which pins are behind.
#
# It is REVIEW-ONLY: it never edits a playbook. Bumping a pin often also requires
# updating an adjacent sha256/checksum and confirming the release asset filename
# still matches, so applying an update is a deliberate, judgement-driven step
# (see the `update-versions` skill) rather than a blind rewrite.
#
# Exit status: 0 = every pin matches upstream latest; 1 = at least one pin is
# behind, or a manual-review pin needs a human; 2 = a hard error (missing tool,
# a manifest row pointing at a file/var that no longer exists, etc.).
#
# Usage:
#   scripts/check-pinned-versions.bash            # human-readable table
#   scripts/check-pinned-versions.bash --help
#
# Requires: gh (authenticated GitHub CLI). Resolves the repo root itself, so it
# can be run from any directory.

set -euo pipefail

usage() {
    cat <<'EOF'
Review upstream version pins declared in the Ansible playbooks.

Reads each pinned version var directly from its playbook, compares it to the
latest upstream release (GitHub), and prints a drift report. Review-only — it
never edits a playbook.

Usage:
  scripts/check-pinned-versions.bash        Print the drift report
  scripts/check-pinned-versions.bash -h     Show this help

Exit status:
  0  all pins match upstream latest
  1  one or more pins are behind, or a manual-review pin needs a human
  2  hard error (gh missing/unauthenticated, or a manifest row is stale)

To track a new pin, add an entry to vars/version-pins.yml — the declared manifest
this script and Plan 00109's installed-vs-pinned check both read. Omit `github:`
for a pin with no automated source; it then needs a `note:` saying how to check it
by hand. ./scripts/qa-version-pins.bash validates the file on every qa-all run.
EOF
}

case "${1:-}" in
    -h | --help)
        usage
        exit 0
        ;;
    "") ;;
    *)
        echo "Unknown argument: $1" >&2
        usage >&2
        exit 2
        ;;
esac

if ! command -v gh >/dev/null; then
    echo "ERROR: 'gh' (GitHub CLI) is required but not found in PATH." >&2
    echo "Install it via the playbooks (play-github-cli-multi.yml) and retry." >&2
    exit 2
fi

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

# MANIFEST — read from vars/version-pins.yml, one row per pin:
#   file|var|owner/repo|extra_prefix|note
# The declaration is a tracked file rather than a heredoc here because two
# consumers read it: this script (pin vs upstream latest) and Plan 00109's
# installed-vs-pinned check (pin vs what is installed on the host). They ask
# different questions of the same population, and the population must not be able
# to differ between them.
#
# helpers/version_pins/manifest.py validates the shape and emits the rows; it is
# stdlib-only, so the YAML conversion happens here with the PyYAML that Ansible
# already depends on. An invalid manifest is a hard error, never a short list:
# silently dropping a row would hide a pin rather than report it.
manifest_out=""
if ! manifest_out="$(python3 -c \
    'import json, sys, yaml; json.dump(yaml.safe_load(open("vars/version-pins.yml")), sys.stdout)' \
    | python3 -m helpers.version_pins.manifest 2>&1)"; then
    echo "ERROR: vars/version-pins.yml could not be read:" >&2
    echo "$manifest_out" >&2
    echo "Run ./scripts/qa-version-pins.bash for the full diagnosis." >&2
    exit 2
fi
if [[ "$manifest_out" != VERSION-PINS-OK* ]]; then
    echo "ERROR: the manifest validator exited 0 without its OK marker: $manifest_out" >&2
    exit 2
fi
# Drop the marker line; the rest is the payload.
MANIFEST="$(printf '%s\n' "$manifest_out" | tail -n +2)"
if [ -z "$MANIFEST" ]; then
    echo "ERROR: the manifest validated but produced no rows; nothing would be checked." >&2
    exit 2
fi

# Strip a leading 'v' and an optional extra prefix, for tolerant comparison.
normalise() {
    local value="$1" extra="$2"
    value="${value#v}"
    if [ -n "$extra" ]; then
        value="${value#"$extra"}"
    fi
    printf '%s' "$value"
}

# Latest upstream release tag for a GitHub repo (releases/latest, then tags).
# Prints the tag and returns 0 on success; prints nothing and returns 1 if both
# queries fail (2>&1 keeps the API error out of the parsed value without hiding
# it — the caller reports a failed query).
latest_tag() {
    local repo="$1" out
    if out="$(gh api "repos/${repo}/releases/latest" --jq '.tag_name' 2>&1)"; then
        printf '%s' "$out"
        return 0
    fi
    if out="$(gh api "repos/${repo}/tags" --jq '.[0].name' 2>&1)"; then
        printf '%s' "$out"
        return 0
    fi
    return 1
}

printf '%-32s %-22s %-14s %-16s %s\n' "REPO / SOURCE" "VAR" "PINNED" "LATEST" "STATUS"
printf '%-32s %-22s %-14s %-16s %s\n' "-------------" "---" "------" "------" "------"

outdated=0
manual=0
while IFS='|' read -r file var repo extra note; do
    [ -z "$file" ] && continue

    # Fail fast if the manifest has drifted away from the playbooks.
    if [ ! -f "$file" ]; then
        echo "ERROR: manifest file not found: $file" >&2
        exit 2
    fi
    if ! match="$(grep -E "^[[:space:]]*${var}[[:space:]]*:" "$file")"; then
        echo "ERROR: pin var '${var}' not found in ${file} (manifest is stale)" >&2
        exit 2
    fi
    pinned="${match%%$'\n'*}"      # first matching line
    pinned="${pinned#*:}"          # drop the "var:" prefix
    pinned="${pinned//\"/}"        # strip quotes and whitespace
    pinned="${pinned//\'/}"
    pinned="${pinned// /}"

    if [ -z "$repo" ]; then
        manual=$((manual + 1))
        printf '%-32s %-22s %-14s %-16s %s\n' "(manual)" "$var" "$pinned" "-" "MANUAL: ${note}"
        continue
    fi

    if latest="$(latest_tag "$repo")"; then
        if [ "$(normalise "$pinned" "$extra")" = "$(normalise "$latest" "$extra")" ]; then
            status="up-to-date"
        else
            status=">> OUTDATED"
            outdated=$((outdated + 1))
        fi
    else
        latest="query-failed"
        status="!! query failed"
        outdated=$((outdated + 1))
    fi
    printf '%-32s %-22s %-14s %-16s %s\n' "$repo" "$var" "$pinned" "$latest" "$status"
done <<<"$MANIFEST"

echo
if [ "$outdated" -gt 0 ]; then
    echo "Result: ${outdated} pin(s) behind upstream (or unqueryable); ${manual} need a manual check."
    echo "To apply an update, use the update-versions skill (it also updates any adjacent sha256/checksum)."
    exit 1
fi
if [ "$manual" -gt 0 ]; then
    echo "Result: all automated pins up-to-date; ${manual} manual pin(s) still need a human check."
    exit 1
fi
echo "Result: all pins up-to-date."
exit 0
