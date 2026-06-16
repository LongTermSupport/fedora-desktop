"""Pure logic for GitHub SSH-over-443 host config — no I/O, fully unit-tested.

This module renders the managed `~/.ssh/config` override block and the
`known_hosts` pin, and provides an idempotent managed-block upsert. The thin
side-effecting executor (read/write files, fetch keys, probe, teardown control
sockets) lives in cli.py.

The single 443-routing mechanism on the host is ONE first-wins override block at
the top of `~/.ssh/config`. OpenSSH uses the first obtained value for each
keyword reading top-to-bottom, so a block at BOF matching `github.com`,
`ssh.github.com`, the repo's own `github.com-*` aliases, and any user deploy-key
globs overrides their `HostName`/`Port` while each per-key stanza still
contributes its own `IdentityFile` (a different keyword, set later). That is why
this covers deploy-key aliases without editing every stanza.
"""

from __future__ import annotations

import json

# Managed-block delimiters. ASCII only — these land in ~/.ssh/config and
# ~/.ssh/known_hosts as comment lines. Both the temporary CLI and the Ansible
# play drive this same module, so a single marker style reconciles cleanly.
SSH_CONFIG_BEGIN = "# >>> github-ssh-443 BEGIN (managed - do not edit) >>>"
SSH_CONFIG_END = "# <<< github-ssh-443 END <<<"
KNOWN_HOSTS_BEGIN = "# >>> github-ssh-443 known_hosts BEGIN (managed - do not edit) >>>"
KNOWN_HOSTS_END = "# <<< github-ssh-443 known_hosts END <<<"

# Always-matched Host patterns: plain github.com, the 443 endpoint itself, and
# the repo's own per-account aliases (play-github-cli-multi.yml writes
# `Host github.com-<alias>`). User deploy-key globs are appended after these.
BASE_ALIASES = ("github.com", "ssh.github.com", "github.com-*")

GITHUB_443_HOST = "ssh.github.com"
GITHUB_443_PORT = "443"
KNOWN_HOSTS_LOOKUP = "[ssh.github.com]:443"


def normalize_aliases(extra: list[str]) -> list[str]:
    """BASE_ALIASES + extra, trimmed, blanks dropped, de-duplicated, order kept."""
    result: list[str] = []
    for alias in [*BASE_ALIASES, *extra]:
        trimmed = alias.strip()
        if trimmed and trimmed not in result:
            result.append(trimmed)
    return result


def render_ssh_override(aliases: list[str]) -> str:
    """Render the Host override stanza (without the managed-block markers)."""
    return "\n".join(
        [
            "Host " + " ".join(aliases),
            f"    HostName {GITHUB_443_HOST}",
            f"    Port {GITHUB_443_PORT}",
            "    User git",
            "    ConnectTimeout 10",
        ]
    )


def render_known_hosts(keys: list[str]) -> str:
    """Render one pinned known_hosts line per key (without the markers)."""
    return "\n".join(f"{KNOWN_HOSTS_LOOKUP} {key}" for key in keys)


def upsert_block(
    text: str,
    begin: str,
    end: str,
    body: str,
    present: bool,
    at_bof: bool = False,
) -> tuple[str, bool]:
    """Idempotently insert / replace / remove a managed block.

    Returns (new_text, changed). The block is delimited by the exact `begin` and
    `end` marker lines. When present and absent it is inserted (at BOF if
    `at_bof`, else appended); when present and already exact, nothing changes;
    when present with a different body it is replaced in place; when not present
    it is removed (along with one trailing blank separator line).
    """
    lines = text.splitlines()
    block_lines = [begin, *body.split("\n"), end]

    b = lines.index(begin) if begin in lines else -1
    e = -1
    if b != -1:
        for i in range(b + 1, len(lines)):
            if lines[i] == end:
                e = i
                break
    has_block = b != -1 and e != -1

    if not present:
        if not has_block:
            return text, False
        # Drop the block; also consume a single trailing blank separator so the
        # removal round-trips back to the original content.
        stop = e + 1
        if stop < len(lines) and lines[stop] == "":
            stop += 1
        del lines[b:stop]
        return _join(lines), True

    if has_block:
        if lines[b : e + 1] == block_lines:
            return text, False
        lines[b : e + 1] = block_lines
        return _join(lines), True

    if at_bof:
        lines = [*block_lines, "", *lines]
    else:
        if lines and lines[-1] != "":
            lines.append("")
        lines.extend(block_lines)
    return _join(lines), True


def _join(lines: list[str]) -> str:
    """Join lines back to text with a single trailing newline (empty stays empty)."""
    if not lines:
        return ""
    return "\n".join(lines) + "\n"


def _canonical_key(raw: str) -> str:
    """Reduce a host-key string to its canonical `<type> <blob>` 2-field form."""
    parts = raw.split()
    return " ".join(parts[:2])


def parse_meta_ssh_keys(meta_json: str) -> list[str]:
    """Extract and canonicalise `.ssh_keys` from an api.github.com/meta payload."""
    data = json.loads(meta_json)
    keys = data.get("ssh_keys", [])
    return [_canonical_key(k) for k in keys if k.strip()]


def parse_keyscan(output: str) -> list[str]:
    """Canonicalise `ssh-keyscan -p 443 ssh.github.com` output.

    Drops comment/blank lines and strips the leading `[ssh.github.com]:443` host
    field, leaving the canonical `<type> <blob>` form.
    """
    keys: list[str] = []
    for line in output.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        parts = stripped.split()
        # parts[0] is the host field; the key type+blob follow.
        if len(parts) >= 3:
            keys.append(_canonical_key(" ".join(parts[1:])))
    return keys


def decide_auto(port22_ok: bool, port443_ok: bool) -> str:
    """Auto decision: 'off' if 22 reachable, 'on' if 22 blocked but 443 works,
    else 'noop' (can't reach GitHub either way — leave config untouched)."""
    if port22_ok:
        return "off"
    if port443_ok:
        return "on"
    return "noop"


def env_line(present: bool) -> str:
    """Shell line for `eval` so a parent shell (and thus ccy) tracks 443 state."""
    return "export GITHUB_SSH_443=1" if present else "unset GITHUB_SSH_443"
