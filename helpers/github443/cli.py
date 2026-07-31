#!/usr/bin/env python3
"""Executor + CLI for GitHub SSH-over-443 host config.

Thin side-effecting wrapper around the pure logic in core.py. It reads/writes
`~/.ssh/config` and `~/.ssh/known_hosts`, fetches GitHub's published host keys,
probes ports 22 vs 443, and tears down any stale connection-multiplexing control
socket. Both the temporary user CLI (`github-ssh-443 on|off|auto|status|env`)
and `play-github-cli-multi.yml` drive THIS module, so the host config has a
single source of truth.

Why the toggle does NOT touch the ssh-agent: agent identities are endpoint-
agnostic — the same key authenticates identically on github.com:22 and
ssh.github.com:443. Routing comes from ~/.ssh/config and host verification from
~/.ssh/known_hosts (both re-read every ssh invocation), so flipping 443 needs no
agent restart/flush. The only stale state is a ControlMaster socket pinned to
:22, which `teardown_control_sockets` closes best-effort.

Invoked as a module from the repo root (playbook) or from the deployed copy
(user CLI):

    python3 -m helpers.github443.cli on --aliases deploy_*,myorg_*
"""

from __future__ import annotations

import argparse
import os
import subprocess
import urllib.request

from helpers.github443 import core

META_URL = "https://api.github.com/meta"


class KeyFetchError(RuntimeError):
    """Raised when GitHub host keys cannot be obtained from any source."""


def _ssh_path(ssh_dir: str, name: str) -> str:
    return os.path.join(ssh_dir, name)


def _read(path: str) -> str:
    if not os.path.exists(path):
        return ""
    with open(path, encoding="utf-8") as fh:
        return fh.read()


def _write_0600(path: str, text: str) -> None:
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(text)
    os.chmod(path, 0o600)


def apply_state(ssh_dir: str, present: bool, aliases: list[str], keys: list[str]) -> dict:
    """Insert/remove the managed override + known_hosts blocks under `ssh_dir`.

    Idempotent: only writes a file when its managed block actually changed.
    Returns {"config_changed": bool, "known_hosts_changed": bool}.
    """
    os.makedirs(ssh_dir, mode=0o700, exist_ok=True)
    os.chmod(ssh_dir, 0o700)

    cfg_path = _ssh_path(ssh_dir, "config")
    cfg_body = core.render_ssh_override(core.normalize_aliases(aliases))
    cfg_new, cfg_changed = core.upsert_block(
        _read(cfg_path), core.SSH_CONFIG_BEGIN, core.SSH_CONFIG_END, cfg_body, present, at_bof=True
    )
    if cfg_changed:
        _write_0600(cfg_path, cfg_new)

    kh_path = _ssh_path(ssh_dir, "known_hosts")
    kh_body = core.render_known_hosts(keys)
    kh_new, kh_changed = core.upsert_block(
        _read(kh_path), core.KNOWN_HOSTS_BEGIN, core.KNOWN_HOSTS_END, kh_body, present, at_bof=False
    )
    if kh_changed:
        _write_0600(kh_path, kh_new)

    return {"config_changed": cfg_changed, "known_hosts_changed": kh_changed}


def fetch_keys(timeout: int = 10) -> list[str]:
    """GitHub host keys, canonicalised. Try the meta API (HTTPS/443, so it works
    on the very networks that block port 22); fall back to ssh-keyscan over 443;
    raise KeyFetchError if neither yields keys (fail fast — we will not pin an
    empty set, which would leave the first push hanging on a host-key prompt)."""
    keys: list[str] = []
    try:
        with urllib.request.urlopen(META_URL, timeout=timeout) as resp:
            keys = core.parse_meta_ssh_keys(resp.read().decode("utf-8"))
    except (OSError, ValueError):
        # Network/parse failure is recoverable here — fall through to keyscan.
        keys = []
    if keys:
        return keys

    scan = subprocess.run(
        ["ssh-keyscan", "-p", core.GITHUB_443_PORT, core.GITHUB_443_HOST],
        capture_output=True,
        text=True,
        check=False,
        timeout=timeout + 5,
    )
    if scan.returncode == 0:
        keys = core.parse_keyscan(scan.stdout)
        if keys:
            return keys

    raise KeyFetchError(
        "Could not obtain GitHub SSH host keys from the meta API or ssh-keyscan "
        "over 443 — check connectivity to api.github.com / ssh.github.com:443."
    )


def probe(host: str, port: str, timeout: int = 10) -> bool:
    """True if an SSH auth attempt to host:port reaches GitHub and authenticates.

    `-F /dev/null` bypasses ~/.ssh/config so the explicit -p port is honoured
    (otherwise an existing `Host github.com` stanza could reroute the probe);
    the ssh-agent is left enabled so the user's real key answers the challenge.
    """
    res = subprocess.run(
        [
            "ssh", "-T",
            "-F", "/dev/null",
            "-o", "StrictHostKeyChecking=no",
            "-o", f"ConnectTimeout={timeout}",
            "-p", str(port),
            f"git@{host}",
        ],
        capture_output=True,
        text=True,
        check=False,
        timeout=timeout + 5,
    )
    return "successfully authenticated" in (res.stdout + res.stderr)


def teardown_control_sockets() -> None:
    """Best-effort close of a ControlMaster socket pinned to github.com:22.

    The repo configures no multiplexing, so this is usually a no-op; a user who
    enabled ControlPersist would otherwise reuse a dead :22 master after the
    switch. `ssh -O exit` returns non-zero when there is no master — that is the
    expected path, inspected and reported, not silently swallowed.
    """
    res = subprocess.run(
        ["ssh", "-O", "exit", "git@github.com"],
        capture_output=True,
        text=True,
        check=False,
    )
    if res.returncode == 0:
        print("Closed a stale github.com control socket.")


def _block_present(ssh_dir: str) -> bool:
    return core.SSH_CONFIG_BEGIN in _read(_ssh_path(ssh_dir, "config"))


def _emit_change(label: str, result: dict) -> None:
    if result["config_changed"] or result["known_hosts_changed"]:
        print(
            f"GH443-CHANGED {label} "
            f"config={result['config_changed']} known_hosts={result['known_hosts_changed']}"
        )
    else:
        print(f"GH443-UNCHANGED {label}")


def _do_on(ssh_dir: str, aliases: list[str], teardown: bool) -> int:
    keys = fetch_keys()
    result = apply_state(ssh_dir, present=True, aliases=aliases, keys=keys)
    _emit_change("on", result)
    if teardown:
        teardown_control_sockets()
    print("GitHub SSH routed over ssh.github.com:443.")
    print('Tip: eval "$(github-ssh-443 env)"  so this shell (and ccy) use 443.')
    return 0


def _do_off(ssh_dir: str, teardown: bool) -> int:
    result = apply_state(ssh_dir, present=False, aliases=[], keys=[])
    _emit_change("off", result)
    if teardown:
        teardown_control_sockets()
    print("GitHub SSH restored to github.com:22.")
    print('Tip: eval "$(github-ssh-443 env)"  to clear GITHUB_SSH_443 in this shell.')
    return 0


def _do_status(ssh_dir: str) -> int:
    p22 = probe("github.com", "22")
    p443 = probe(core.GITHUB_443_HOST, core.GITHUB_443_PORT)
    print(f"port 22 (github.com):        {'open' if p22 else 'BLOCKED'}")
    print(f"port 443 (ssh.github.com):   {'open' if p443 else 'BLOCKED'}")
    print(f"443 override block present:  {'yes' if _block_present(ssh_dir) else 'no'}")
    print(f"recommended:                 {core.decide_auto(p22, p443)}")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["on", "off", "auto", "status", "env"])
    parser.add_argument(
        "--ssh-dir",
        default=os.path.expanduser("~/.ssh"),
        help="SSH config directory (default: ~/.ssh).",
    )
    parser.add_argument(
        "--aliases",
        default="",
        help="Comma-separated extra Host alias globs for deploy keys (e.g. 'deploy_*,myorg_*').",
    )
    parser.add_argument(
        "--no-teardown",
        action="store_true",
        help="Skip the control-socket teardown (used by the playbook apply path).",
    )
    args = parser.parse_args(argv)
    aliases = [a for a in args.aliases.split(",") if a.strip()]
    teardown = not args.no_teardown

    if args.command == "env":
        print(core.env_line(_block_present(args.ssh_dir)))
        return 0
    if args.command == "status":
        return _do_status(args.ssh_dir)
    if args.command == "on":
        return _do_on(args.ssh_dir, aliases, teardown)
    if args.command == "off":
        return _do_off(args.ssh_dir, teardown)

    # auto
    decision = core.decide_auto(probe("github.com", "22"), probe(core.GITHUB_443_HOST, core.GITHUB_443_PORT))
    if decision == "on":
        print("Port 22 is blocked but ssh.github.com:443 works — enabling 443.")
        return _do_on(args.ssh_dir, aliases, teardown)
    if decision == "off":
        print("Port 22 reachable — ensuring 443 override is off.")
        return _do_off(args.ssh_dir, teardown)
    print("GH443-UNCHANGED auto: neither port 22 nor 443 reached GitHub — leaving config untouched.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
