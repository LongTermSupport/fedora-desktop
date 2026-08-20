#!/bin/bash
# Shared secret-scanning helpers for the git hooks. SOURCE this; do not run it.
#
# `pre-commit` (staged file content) and `commit-msg` (the message) must agree
# on what counts as a private identifier. They did not: pre-commit ran the
# static patterns AND the localhost.yml denylist, while commit-msg ran only the
# static patterns and then printed "✓ Commit message looks clean". So an alias,
# a hostname or a service username with no static pattern was rejected in a
# staged FILE and accepted in a commit MESSAGE — the worse of the two, because
# a file leak is fixed by editing before the push and a commit message cannot
# be fixed by a follow-up commit at all.
#
# The two hooks also disagreed about whitelisting: pre-commit filters per TOKEN
# (Plan 00081), commit-msg still filtered per LINE, so `git@github.com` sharing
# a line with a real address deleted the address along with it. One
# implementation, sourced twice, is what stops the pair drifting again.
#
# Provides:
#   hook_filter_match_lines <pattern>          stdin: N:line -> stdout: survivors
#   hook_build_private_denylist <root> <out>   0 = built, 2 = no localhost.yml
#   hook_scan_text_for_private <denylist>      stdin: text -> stdout: FIELD names
#
# Values from localhost.yml are NEVER printed by anything here. Matches are
# reported by their source FIELD NAME only.

# grep -v that distinguishes "nothing left" (exit 1) from "grep itself broke"
# (exit >= 1). Collapsing the two would turn a bad regex into an empty result,
# which reads as "no leak found" — the exact defect class this file exists for.
#
# The status is captured WHERE IT IS PRODUCED. Written first as
# `if out=$(grep "$@"); then ...; fi` followed by `rc=$?`, which reads the
# status of the *if statement* — zero whenever no branch ran — so the `rc > 1`
# arm below was unreachable and every grep error would have been reported as
# "nothing matched". Found by this repo's own bash-status-after-block rule,
# inside the fix for the sibling defect class.
_hook_grep_v() {
    local out rc=0
    out=$(grep "$@") || rc=$?
    if [ "$rc" -eq 0 ]; then
        printf '%s' "$out"
        return 0
    fi
    if [ "$rc" -gt 1 ]; then
        echo "ERROR: whitelist filter failed (grep exit $rc): $*" >&2
        return 2
    fi
    return 1
}

# Keep a line only if at least one of its matching TOKENS is not whitelisted.
#
# Per TOKEN, never per line. CLAUDE/PlanTriage.md states the rule — "Redact by
# substitution, never by dropping anchored lines", learned from a real leak in
# Plan 00066 — and both hooks were doing the opposite: one legitimate reference
# on a line deleted the whole line, real identifier included.
#
# $1 = token regex (ERE). Remaining args = whitelist regexes applied to the
# tokens; prefix one with `i:` to match case-insensitively.
hook_keep_unwhitelisted() {
    local token_re="$1"
    shift
    local line masked tokens left w rc
    while IFS= read -r line; do
        if [ -z "$line" ]; then
            continue
        fi
        # Bracketed placeholders need their surrounding context to be
        # recognisable, so they are masked before tokens are extracted.
        masked=$(printf '%s' "$line" \
            | awk '{ gsub(/<[^>]*@[^>]*>/, "<PH>");
                     gsub(/\{\{[^}]*@[^}]*\}\}/, "{{PH}}"); print }')
        if ! tokens=$(printf '%s' "$masked" | grep -oE -- "$token_re"); then
            continue
        fi
        left="$tokens"
        for w in "$@"; do
            if [ -z "$left" ]; then
                break
            fi
            case "$w" in
                i:*)
                    left=$(printf '%s' "$left" | _hook_grep_v -viE -- "${w#i:}") || rc=$?
                    ;;
                *)
                    left=$(printf '%s' "$left" | _hook_grep_v -vE -- "$w") || rc=$?
                    ;;
            esac
            if [ "${rc:-0}" -eq 2 ]; then
                return 1
            fi
            rc=0
        done
        if [ -n "$left" ]; then
            printf '%s\n' "$line"
        fi
    done
    # The loop's last statement returns non-zero whenever the final line IS
    # whitelisted, which under `set -e` would abort the caller's command
    # substitution with no output at all — a security gate dying silently on
    # clean input. Plan 00081 shipped that bug once already.
    true
}

# Apply the whitelist appropriate to a sensitive-pattern regex.
# stdin: `N:content` match lines. stdout: the lines that still look like leaks.
hook_filter_match_lines() {
    local pattern="$1"
    case "$pattern" in
        *@*)
            hook_keep_unwhitelisted "$pattern" \
                'i:@([A-Za-z0-9-]+\.)*example\.(com|org|net)$' \
                'i:@[A-Za-z0-9.-]+\.(example|test|invalid|localhost)$' \
                '^git@[A-Za-z0-9.-]+' \
                '@[A-Za-z0-9_.:-]+\.(service|socket|timer|target|mount|path|slice|scope|device|swap|automount)$'
            ;;
        */home/*)
            # The token regex is widened by `(files)?` so a repo path
            # (files/home/...) is visible in the token itself — a whole-line
            # filter is what let a placeholder home path shield a real one.
            #
            # `etc` is not a username: the hooks daemon generates
            # .claude/HOOKS-DAEMON.md containing "rooted at ``/``/home/etc", a
            # backtick-mangled rendering of the dangerous-roots list. Without it
            # every daemon-upgrade commit is blocked. Deliberately narrow — a
            # system directory that can never be a home directory. `root` is NOT
            # listed.
            hook_keep_unwhitelisted '(files)?/home/[a-z][a-z0-9_-]{2,}[^}]' \
                '^files/home/(bashrc-includes|[a-z-]+)' \
                '/home/(ansible|runner|deploy|build|user|etc)\b'
            ;;
        *credential* | *token*)
            hook_keep_unwhitelisted \
                '[a-z]{3,}_[a-z]{3,}\.(token|key|credential|json)[a-z]*(\(\))?' \
                '\.(keys|values|items)\(\)$' \
                '^(example_|test_|sample_|demo_)' \
                '(user_name|company_name)'
            ;;
        *)
            cat
            ;;
    esac
}

# Build the private-identifier denylist from the gitignored localhost.yml.
# $1 = repo root, $2 = output file (one "FIELD<TAB>TOKEN" line per identifier).
# Returns 0 built, 2 localhost.yml absent (caller skips ONLY this check),
# 1 hard failure (caller must abort — a security gate that cannot build its
# denylist has not passed, it has not run).
hook_build_private_denylist() {
    local repo_root="$1" outfile="$2"
    local yml="$repo_root/environment/localhost/host_vars/localhost.yml"
    local allow="$repo_root/.claude/public-token-allowlist.yml"

    if [ ! -f "$yml" ]; then
        return 2
    fi

    if ! python3 - "$yml" "$allow" > "$outfile" <<'PYEOF'
import sys
import yaml


class VaultTag:
    """Marker so !vault-encrypted values can be skipped, never emitted."""


def _vault(loader, node):
    return VaultTag()


yaml.SafeLoader.add_constructor("!vault", _vault)

yml_path = sys.argv[1]
allow_path = sys.argv[2]

with open(yml_path) as fh:
    data = yaml.safe_load(fh) or {}

# Public-by-design allowlist (case-insensitive whole-token subtraction).
allow = set()
try:
    with open(allow_path) as fh:
        adoc = yaml.safe_load(fh) or {}
    for tok in (adoc.get("public_tokens") or []):
        if isinstance(tok, str):
            allow.add(tok.strip().lower())
except FileNotFoundError:
    pass

out = {}  # token -> source field name


def add(field, value):
    if isinstance(value, VaultTag) or value is None:
        return
    s = str(value).strip()
    if len(s) < 4:
        return
    if s.lower() in allow:
        return
    out.setdefault(s, field)


# Top-level identity scalars.
for field in ("user_email", "user_name"):
    if field in data:
        add(field, data[field])


def harvest_identity_map(field, mapping):
    """Collect KEYS and string VALUES from an identity mapping."""
    if not isinstance(mapping, dict):
        return
    for k, v in mapping.items():
        add(field, k)
        if isinstance(v, dict):
            harvest_identity_map(field, v)
        elif isinstance(v, list):
            for item in v:
                add(field, item)
        else:
            add(field, v)


# The suffix list carries the PLURAL spellings. This repo's convention is the
# plural (github_accounts, project_personas) — which is why both of those had
# to be hardcoded below while `lastpass_accounts` and any future
# `*_accounts` / `*_usernames` key were harvested by nothing at all.
IDENTITY_SUFFIXES = (
    "_username", "_usernames",
    "_account", "_accounts",
    "_persona", "_personas",
    "_login", "_logins",
)

for field, value in data.items():
    if field in ("github_accounts", "project_personas"):
        harvest_identity_map(field, value)
    elif field.endswith(IDENTITY_SUFFIXES):
        if isinstance(value, dict):
            harvest_identity_map(field, value)
        elif isinstance(value, list):
            for item in value:
                add(field, item)
        else:
            add(field, value)

for token, field in out.items():
    sys.stdout.write(f"{field}\t{token}\n")
PYEOF
    then
        return 1
    fi
    return 0
}

# Scan text (stdin) for denylisted identifiers.
# $1 = denylist file. stdout: the source FIELD name of each identifier found,
# deduplicated — never the value.
hook_scan_text_for_private() {
    local denylist="$1"
    local text field token
    text=$(cat)

    if [ ! -s "$denylist" ]; then
        return 0
    fi
    if [ -z "$text" ]; then
        return 0
    fi

    local -A seen=()
    while IFS=$'\t' read -r field token; do
        if [ -z "$token" ]; then
            continue
        fi
        if [ -n "${seen[$field]:-}" ]; then
            continue
        fi
        if printf '%s' "$text" | grep -qF -- "$token"; then
            seen[$field]=1
            printf '%s\n' "$field"
        fi
    done < "$denylist"
}
