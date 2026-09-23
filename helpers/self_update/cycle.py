"""The unattended self-update cycle (Plan 00137 T3.3, T3.4, T4.1).

Run by `/usr/local/sbin/fedora-desktop-self-update` as root:

    python3 -m helpers.self_update.cycle --config C --state-dir S --published-dir P --clone D --become B \\
        --vault V --allowed-signers A --ansible-playbook P {run [--dry-run] | verify | status}

The contract (paths, keys, exit codes, the result record) is
CLAUDE/Plan/00137-unattended-server-self-update/DESIGN-cycle.md. What lives here is the
order of operations and the decisions between them; every effect goes through a `Host`,
so the tests can state that order exactly.

**`run`.** Take the play lock, then check that the clone's remote is the configured one.
Update through the trust gate (update.py). Work out the plays from the last DEPLOYED
commit, not the pre-update HEAD: a cycle whose plays failed has already moved the clone,
and the next one must still owe those plays. Before the first play, check that the pinned
system ansible-core and its collections are root-owned, and that the user's PATH finds
exactly that ansible (D5). Run each play as the user, with the become and vault passwords
on descriptors (see `password_pipe`) and no code search path in the user's home (see
`play_environment`), stopping at the first failure, with no reboot (D8). When every play has succeeded, record the commit as
deployed and the post-boot check as owed. Only then warn the sessions, count down, warn
again at one minute, and reboot. A warning that cannot be delivered, an interrupted
countdown or a refused reboot withdraws the warning, and the reboot stays owed: the next
cycle retries it, with nothing new to run.

**`verify`.** After the reboot, ask `ccy-sessions verify-restore` whether every restored
session came back, then record and announce the answer. A marker written during this same
boot means the reboot has not happened yet, so it is left alone.

**The first cycle.** With no deployed record there is no basis for a diff, so every
allowlisted play runs, and only once the clone's HEAD is proven pinned-signed: the gate
judges only commits above HEAD, so nothing else vouches for HEAD itself. They are idempotent, and that run is what makes the record true.

**Alerts** go through one `alert` seam. Until Task 4.5 plugs in real sinks it writes to
the journal (stderr). Every result is also published to a user-readable copy
(`published`), which the host-health report reads. Neither carries a hostname or
username, and the only paths in either are the plays' repo-relative ones.
"""

from __future__ import annotations

import argparse
import contextlib
import io
import json
import os
import pwd
import re
import signal
import stat
import subprocess
import sys
import time
from collections.abc import Iterator
from dataclasses import dataclass
from typing import Protocol, TextIO

from helpers.play_lock import lock as play_lock
from helpers.self_update import affected_plays, published, update

EXIT_OK = 0
EXIT_REFUSED = 20
EXIT_PLAY_FAILED = 21
EXIT_UNWARNABLE = 22
EXIT_VERIFY_FAILED = 23
#: Not in the contract's table: `systemctl reboot` itself refused.
EXIT_REBOOT_FAILED = 24
EXIT_USAGE = 64
EXIT_CONFIG = 70
EXIT_LOCKED = 75
#: Not in the contract's table: the countdown was interrupted (SIGINT/SIGTERM).
EXIT_CANCELLED = 130

#: How long the post-boot check waits for restored sessions to settle.
VERIFY_WAIT_SECONDS = 300
MINUTE = 60
#: Real seconds per countdown minute. Overridable ONLY so scripts/test-self-update-cycle.bash
#: can drive a countdown; sudo's env_reset strips it, and the systemd unit never sets it.
MINUTE_SECONDS_ENV = "FEDORA_DESKTOP_SELF_UPDATE_MINUTE_SECONDS"
_MAX_WARN_MINUTES = 60
#: A password is handed over in one pipe write; PIPE_BUF bytes are written whole and never
#: block on an empty pipe, so filling it before the child starts cannot deadlock.
PIPE_MAX_BYTES = 4096

_KEYS = ("USER", "BRANCH", "REMOTE_URL", "PRINCIPAL", "WARN_MINUTES", "ALERT_SINKS", "ANSIBLE_COLLECTIONS_DIR")
RESULT_KEYS = ("at", "phase", "outcome", "old", "new", "plays", "detail")
_SHA = re.compile(r"^[0-9a-f]{40}$")
_NAME = re.compile(r"^[a-z_][a-z0-9_-]*$")
_BRANCH = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._/-]*$")
_REMOTE = "origin"


class ConfigError(Exception):
    """The config file is not exactly what the contract says it must be."""


class StateError(Exception):
    """A state file exists and cannot be read as what it claims to be."""


class Cancelled(Exception):
    """The countdown before the reboot was interrupted."""


@dataclass(frozen=True)
class Config:
    user: str
    branch: str
    remote_url: str
    principal: str
    warn_minutes: int
    alert_sinks: tuple[str, ...]
    ansible_collections_dir: str


def parse_config(text: str) -> Config:
    """Strict `KEY=value` lines. Read, never sourced: a value is the bytes after `=`."""
    values: dict[str, str] = {}
    for number, raw in enumerate(text.splitlines(), start=1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        key, sep, value = line.partition("=")
        if not sep or key not in _KEYS:
            raise ConfigError(f"line {number} is not one of the keys {', '.join(_KEYS)} as KEY=value")
        if key in values:
            raise ConfigError(f"{key} is set twice (line {number})")
        values[key] = value
    missing = [key for key in _KEYS if key not in values]
    if missing:
        raise ConfigError(f"missing {', '.join(missing)}")

    if not _NAME.match(values["USER"]):
        raise ConfigError(f"USER={values['USER']!r} is not a user name")
    if not _BRANCH.match(values["BRANCH"]):
        raise ConfigError(f"BRANCH={values['BRANCH']!r} is not a branch name")
    if not values["REMOTE_URL"].startswith("https://"):
        raise ConfigError("REMOTE_URL must be an https:// URL (the public repository, fetched without a key)")
    if not values["PRINCIPAL"]:
        raise ConfigError("PRINCIPAL must name the signer to trust")
    minutes = values["WARN_MINUTES"]
    if not minutes.isdigit() or not 1 <= int(minutes) <= _MAX_WARN_MINUTES:
        raise ConfigError(f"WARN_MINUTES={minutes!r} is not a whole number from 1 to {_MAX_WARN_MINUTES}")
    sinks = tuple(s for s in re.split(r"[\s,]+", values["ALERT_SINKS"]) if s and s != "none")
    if sinks:
        # Refused rather than ignored: a configured sink that silently delivers nothing
        # reads exactly like a cycle that had nothing to report.
        raise ConfigError(f"ALERT_SINKS={values['ALERT_SINKS']!r}: no alert sink is implemented yet (Task 4.5)")
    collections = values["ANSIBLE_COLLECTIONS_DIR"]
    if not os.path.isabs(collections) or os.path.normpath(collections) != collections or re.search(r"\s", collections):
        raise ConfigError(f"ANSIBLE_COLLECTIONS_DIR={collections!r} is not an absolute, normalised path")
    return Config(
        user=values["USER"], branch=values["BRANCH"], remote_url=values["REMOTE_URL"],
        principal=values["PRINCIPAL"], warn_minutes=int(minutes), alert_sinks=sinks,
        ansible_collections_dir=collections,
    )


@dataclass(frozen=True)
class Owed:
    boot: str
    new: str
    plays: tuple[str, ...]


class State:
    """The cycle's files under the state directory. Each is small `key=value` text.

    `published_dir` receives a user-readable copy of every result (see `published`),
    so the host-health report can see how the last cycle went without reading this
    root-only directory.
    """

    def __init__(self, directory: str, *, published_dir: str) -> None:
        self.directory = directory
        self.published_dir = published_dir

    def _path(self, name: str) -> str:
        return os.path.join(self.directory, name)

    def _read(self, name: str) -> dict[str, str] | None:
        try:
            with open(self._path(name), encoding="utf-8") as handle:
                text = handle.read()
        except FileNotFoundError:
            return None
        record: dict[str, str] = {}
        for line in text.splitlines():
            key, sep, value = line.partition("=")
            if not sep or not key:
                raise StateError(f"{name} holds a line that is not key=value")
            record[key] = value
        return record

    def _write(self, name: str, record: dict[str, str]) -> None:
        for key, value in record.items():
            if "\n" in value or "=" in key:
                raise StateError(f"{name}: {key} cannot be written as one key=value line")
        temporary = self._path(f".{name}.tmp")
        with open(temporary, "w", encoding="utf-8") as handle:
            handle.write("".join(f"{key}={value}\n" for key, value in record.items()))
        os.replace(temporary, self._path(name))

    def read_deployed(self) -> str | None:
        record = self._read("deployed")
        if record is None:
            return None
        sha = record.get("sha", "")
        if not _SHA.match(sha):
            raise StateError("deployed does not hold a commit sha")
        return sha

    def write_deployed(self, sha: str) -> None:
        self._write("deployed", {"sha": sha})

    def read_owed(self) -> Owed | None:
        record = self._read("owed-verify")
        if record is None:
            return None
        if set(record) != {"boot", "new", "plays"} or not _SHA.match(record["new"]):
            raise StateError("owed-verify is not a boot, a commit and a list of plays")
        return Owed(boot=record["boot"], new=record["new"], plays=tuple(record["plays"].split()))

    def write_owed(self, *, boot: str, new: str, plays: tuple[str, ...]) -> None:
        self._write("owed-verify", {"boot": boot, "new": new, "plays": " ".join(plays)})

    def clear_owed(self) -> None:
        with contextlib.suppress(FileNotFoundError):
            os.unlink(self._path("owed-verify"))

    def read_result(self) -> dict[str, str] | None:
        return self._read("last-result")

    def write_result(self, record: dict[str, str]) -> None:
        """Record the result, then publish it with the owed boot as of now. Every change
        to `owed-verify` is followed by a result, so the published copy tracks both."""
        values = {key: record.get(key, "") for key in RESULT_KEYS}
        self._write("last-result", values)
        owed = self.read_owed()
        published.write(self.published_dir, values, owed_boot=owed.boot if owed is not None else "")


@dataclass(frozen=True)
class UpdateResult:
    """What update.py said, from its marker lines."""

    rc: int
    old: str | None
    new: str | None
    target: str | None
    nothing: str | None

    def head(self) -> str | None:
        return self.new or self.target or self.nothing


class Host(Protocol):
    def check_remote(self, url: str) -> str | None: ...
    def head_trusted(self) -> bool: ...
    def check_toolchain(self) -> str | None: ...
    def update(self, *, dry_run: bool) -> UpdateResult: ...
    def changed_plays(self, old: str, new: str) -> affected_plays.Report: ...
    def allowlist(self) -> list[str]: ...
    def run_play(self, play: str) -> int: ...
    def notify(self, args: list[str]) -> int: ...
    def verify_restore(self, wait_seconds: int) -> int: ...
    def reboot(self) -> int: ...
    def boot_id(self) -> str: ...
    def sleep(self, seconds: float) -> None: ...
    def now(self) -> str: ...


def alert(record: dict[str, str], stderr: TextIO) -> None:
    """The one place an outcome is announced. Task 4.5 adds real sinks behind it."""
    stderr.write(f"self-update: ALERT {record.get('outcome', '')}: {record.get('detail', '')}\n")


def _finish(
    state: State, host: Host, stdout: TextIO, stderr: TextIO, *, code: int, phase: str, outcome: str,
    old: str = "", new: str = "", plays: tuple[str, ...] = (), detail: str = "", announce: bool = False,
) -> int:
    record = {
        "at": host.now(), "phase": phase, "outcome": outcome, "old": old, "new": new,
        "plays": " ".join(plays), "detail": detail,
    }
    state.write_result(record)
    if announce:
        alert(record, stderr)
    stdout.write(f"SELF-UPDATE-CYCLE {outcome}\n")
    return code


def _withdraw(host: Host, stderr: TextIO) -> None:
    if host.notify(["reboot-cancelled"]) != 0:
        stderr.write("self-update: the sessions could not be told the reboot is off; they still expect it\n")


def _plays_to_run(host: Host, deployed: str | None, head: str) -> tuple[str, ...]:
    """The allowlisted plays whose inputs changed since `deployed`. Raises ValueError."""
    if deployed is None:
        return tuple(host.allowlist())
    report = host.changed_plays(deployed, head)
    allowed = set(report.run) | set(host.allowlist())
    blind = sorted({play for play, _ in report.unresolved if play in allowed})
    if blind:
        raise ValueError(
            f"{len(blind)} allowlisted play(s) hold a reference the mapper cannot follow, so a change "
            "to them could be missed"
        )
    return tuple(report.run)


def run_cycle(config: Config, host: Host, state: State, *, dry_run: bool, stdout: TextIO, stderr: TextIO) -> int:
    """The timer's cycle. The play lock is the caller's (see main)."""
    remote_error = host.check_remote(config.remote_url)
    if remote_error is not None:
        stderr.write(f"self-update: {remote_error}\n")
        if dry_run:
            return EXIT_REFUSED
        return _finish(state, host, stdout, stderr, code=EXIT_REFUSED, phase="update", outcome="refused",
                       detail="the deploy clone's remote is not the configured one", announce=True)

    # The gate only judges commits above HEAD, and a first cycle has no deployed record to
    # say HEAD was ever vouched for, so it would run every allowlisted play from whatever
    # the clone was made at.
    if state.read_deployed() is None and not host.head_trusted():
        stderr.write("self-update: the deploy clone's HEAD is not a commit the pinned key signed\n")
        if dry_run:
            return EXIT_REFUSED
        return _finish(state, host, stdout, stderr, code=EXIT_REFUSED, phase="trust", outcome="refused",
                       detail="the first cycle found the deploy clone on a commit the pinned key did not sign",
                       announce=True)

    result = host.update(dry_run=dry_run)
    head = result.head()
    if result.rc != 0 or head is None:
        if dry_run:
            return EXIT_REFUSED
        return _finish(state, host, stdout, stderr, code=EXIT_REFUSED, phase="update", outcome="refused",
                       detail=f"the update or trust gate refused (update exit {result.rc})", announce=True)

    deployed = state.read_deployed()
    owed = state.read_owed()
    if deployed == head:
        if not dry_run and owed is not None and owed.boot == host.boot_id():
            return _warn_and_reboot(config, host, state, stdout, stderr, old=deployed, new=head, plays=owed.plays)
        if dry_run:
            stdout.write("SELF-UPDATE-CYCLE nothing\n")
            return EXIT_OK
        return _finish(state, host, stdout, stderr, code=EXIT_OK, phase="update", outcome="nothing",
                       old=deployed or "", new=head)

    try:
        plays = _plays_to_run(host, deployed, head)
    except ValueError as error:
        stderr.write(f"self-update: {error}\n")
        if dry_run:
            return EXIT_REFUSED
        return _finish(state, host, stdout, stderr, code=EXIT_REFUSED, phase="plan", outcome="refused",
                       old=deployed or "", new=head, detail="the plays to run could not be worked out",
                       announce=True)

    if plays:
        toolchain_error = host.check_toolchain()
        if toolchain_error is not None:
            stderr.write(f"self-update: config invalid: {toolchain_error}\n")
            if dry_run:
                return EXIT_CONFIG
            return _finish(state, host, stdout, stderr, code=EXIT_CONFIG, phase="plan", outcome="config-invalid",
                           old=deployed or "", new=head, plays=plays,
                           detail="the system ansible or its collections cannot be trusted; no play ran",
                           announce=True)

    if dry_run:
        for play in plays:
            stdout.write(f"RUN {play}\n")
        stdout.write("SELF-UPDATE-CYCLE dry-run\n")
        return EXIT_OK

    if not plays:
        state.write_deployed(head)
        return _finish(state, host, stdout, stderr, code=EXIT_OK, phase="plan", outcome="nothing",
                       old=deployed or "", new=head)

    for play in plays:
        rc = host.run_play(play)
        if rc != 0:
            return _finish(state, host, stdout, stderr, code=EXIT_PLAY_FAILED, phase="play", outcome="play-failed",
                           old=deployed or "", new=head, plays=plays,
                           detail=f"{play} exited {rc}; no reboot, retried next cycle", announce=True)

    state.write_deployed(head)
    state.write_owed(boot=host.boot_id(), new=head, plays=plays)
    return _warn_and_reboot(config, host, state, stdout, stderr, old=deployed or "", new=head, plays=plays)


def _warn_and_reboot(
    config: Config, host: Host, state: State, stdout: TextIO, stderr: TextIO,
    *, old: str, new: str, plays: tuple[str, ...],
) -> int:
    minutes = config.warn_minutes

    def unwarnable(detail: str) -> int:
        return _finish(state, host, stdout, stderr, code=EXIT_UNWARNABLE, phase="warn", outcome="unwarnable",
                       old=old, new=new, plays=plays, detail=detail, announce=True)

    if host.notify(["going-down", "--minutes", str(minutes)]) != 0:
        return unwarnable("a session could not be warned; no reboot, retried next cycle")
    state.write_result({"at": host.now(), "phase": "warn", "outcome": "rebooting", "old": old, "new": new,
                        "plays": " ".join(plays), "detail": f"reboot in {minutes} minute(s)"})
    try:
        if minutes > 1:
            host.sleep((minutes - 1) * MINUTE)
            if host.notify(["going-down", "--minutes", "1"]) != 0:
                _withdraw(host, stderr)
                return unwarnable("the one-minute warning could not be delivered; withdrawn, no reboot")
        host.sleep(MINUTE)
    except Cancelled:
        _withdraw(host, stderr)
        return _finish(state, host, stdout, stderr, code=EXIT_CANCELLED, phase="warn", outcome="cancelled",
                       old=old, new=new, plays=plays, detail="the countdown was interrupted; withdrawn, no reboot",
                       announce=True)
    if host.reboot() != 0:
        _withdraw(host, stderr)
        return _finish(state, host, stdout, stderr, code=EXIT_REBOOT_FAILED, phase="reboot", outcome="reboot-failed",
                       old=old, new=new, plays=plays, detail="the reboot request was refused; withdrawn",
                       announce=True)
    stdout.write("SELF-UPDATE-CYCLE rebooting\n")
    return EXIT_OK


def verify(config: Config, host: Host, state: State, *, stdout: TextIO, stderr: TextIO) -> int:
    """The post-boot check, owed by a cycle that rebooted."""
    owed = state.read_owed()
    if owed is None or owed.boot == host.boot_id():
        return EXIT_OK
    rc = host.verify_restore(VERIFY_WAIT_SECONDS)
    state.clear_owed()
    if rc == 0:
        return _finish(state, host, stdout, stderr, code=EXIT_OK, phase="verify", outcome="deployed",
                       new=owed.new, plays=owed.plays, detail="every restored session is running", announce=True)
    return _finish(state, host, stdout, stderr, code=EXIT_VERIFY_FAILED, phase="verify", outcome="verify-failed",
                   new=owed.new, plays=owed.plays, detail="a restored session is not running, or waits at a prompt",
                   announce=True)


def status(state: State, *, stdout: TextIO) -> int:
    """For a human: the last result, what is deployed, and whether a check is owed."""
    record = state.read_result()
    if record is None:
        stdout.write("no cycle has run yet\n")
    else:
        for key in RESULT_KEYS:
            stdout.write(f"{key}={record.get(key, '')}\n")
    stdout.write(f"deployed={state.read_deployed() or 'none'}\n")
    owed = state.read_owed()
    stdout.write(f"owed-verify={'yes' if owed is not None else 'no'}\n")
    return EXIT_OK


# ── the real host ───────────────────────────────────────────────────────────────────────


def user_environment(*, home: str, user: str, runtime: str, ansible_playbook: str) -> dict[str, str]:
    """The whole environment of anything run as the user: `env -i`, so nothing of root's leaks.

    The pinned system ansible's directory leads PATH and the user's own ~/.local/bin (where
    pipx puts a user ansible) comes last, so no user-writable file can stand in for it.
    """
    path: list[str] = []
    for directory in (os.path.dirname(ansible_playbook), "/usr/bin", "/bin", "/usr/sbin", "/sbin",
                      "/usr/local/bin", f"{home}/.local/bin"):
        if directory not in path:
            path.append(directory)
    return {
        "HOME": home, "USER": user, "LOGNAME": user, "LANG": "C.UTF-8",
        "PATH": ":".join(path), "XDG_RUNTIME_DIR": runtime,
    }


# Each ansible-core setting whose default searches ~/.ansible for code, by its env var, and
# the plugin kind named in the system half of that default (/usr/share/ansible/plugins/<kind>).
_SYSTEM_PLUGIN_ENV = {
    "ANSIBLE_ACTION_PLUGINS": "action", "ANSIBLE_BECOME_PLUGINS": "become", "ANSIBLE_CACHE_PLUGINS": "cache",
    "ANSIBLE_CLICONF_PLUGINS": "cliconf", "ANSIBLE_CONNECTION_PLUGINS": "connection",
    "ANSIBLE_DOC_FRAGMENT_PLUGINS": "doc_fragments", "ANSIBLE_FILTER_PLUGINS": "filter",
    "ANSIBLE_HTTPAPI_PLUGINS": "httpapi", "ANSIBLE_INVENTORY_PLUGINS": "inventory", "ANSIBLE_LIBRARY": "modules",
    "ANSIBLE_LOOKUP_PLUGINS": "lookup", "ANSIBLE_MODULE_UTILS": "module_utils", "ANSIBLE_NETCONF_PLUGINS": "netconf",
    "ANSIBLE_STRATEGY_PLUGINS": "strategy", "ANSIBLE_TERMINAL_PLUGINS": "terminal", "ANSIBLE_TEST_PLUGINS": "test",
    "ANSIBLE_VARS_PLUGINS": "vars",
}


def search_path_environment(*, clone: str, collections_dir: str) -> dict[str, str]:
    """Every code search path a play's ansible uses, pinned away from the user's home."""
    plugin_paths = {name: f"/usr/share/ansible/plugins/{kind}" for name, kind in _SYSTEM_PLUGIN_ENV.items()}
    plugin_paths["ANSIBLE_CALLBACK_PLUGINS"] = f"{clone}/callback_plugins:/usr/share/ansible/plugins/callback"
    return {
        **plugin_paths,
        "ANSIBLE_ROLES_PATH": f"{clone}/roles/vendor",
        "ANSIBLE_COLLECTIONS_PATH": collections_dir,
        "PYTHONNOUSERSITE": "1",
    }


def home_search_paths(dump: object, *, home: str, cwd: str) -> list[str]:
    """`NAME=path` for each code search path under `home` in an `ansible-config dump --format json`.

    The pinned list above is one ansible-core release's; a later release can add a search
    path defaulting under ~/.ansible that no list names. So the effective settings are asked
    for and judged by rule: ansible-config reports every search path, and the inventory whose
    host_vars choose the interpreter, as a LIST, and any list element under the user's home
    is a finding. No setting name is trusted to say which lists are paths. A string value
    names one file or working directory (the local tmp, the galaxy token), which is data, not
    a place code is looked up. A relative element is judged from `cwd`, where the play runs,
    so a plugin name or a pattern lands in the clone, not the home. `~/…` is the home; a bare
    `~` is a backup-file suffix, since ansible expands it in every path it dumps. The
    trailing GALAXY_SERVERS entry holds server URLs, not paths. Any other shape is refused
    rather than guessed at.
    """
    if not isinstance(dump, list):
        raise ValueError("the ansible-config dump is not a list of settings")
    root = os.path.normpath(home)
    findings = []
    for setting in dump:
        if isinstance(setting, dict) and setting.keys() == {"GALAXY_SERVERS"}:
            continue
        if not isinstance(setting, dict) or not isinstance(setting.get("name"), str):
            raise ValueError(f"the ansible-config dump holds a setting with no name: {setting!r}")
        value = setting.get("value")
        if not isinstance(value, list):
            continue
        for element in value:
            if not isinstance(element, str):
                continue
            expanded = root + element[1:] if element.startswith("~/") else element
            resolved = os.path.normpath(os.path.join(cwd, expanded))
            if resolved == root or resolved.startswith(root + os.sep):
                findings.append(f"{setting['name']}={element}")
    return findings


def play_environment(
    *, home: str, user: str, runtime: str, clone: str, ansible_playbook: str, collections_dir: str,
    lock_fd: int, become_fd: int, vault_fd: int,
) -> dict[str, str]:
    """What `run.bash --headless <play>` is handed.

    The play runs as the user with become, so any code ansible loads from a path the user
    can write would run as root. Ansible's defaults search ~/.ansible for every plugin kind,
    roles and collections, so each search path is pinned to its root-owned system half, and
    python's user site-packages is off. The env beats the clone's ansible.cfg, so the two
    paths that file sets, callback_plugins (the play ledger) and roles_path, are named here
    as the clone's own. Paths beside the playbook are in the root-owned clone.

    Facts are cached in memory: ansible.cfg's jsonfile cache is ./untracked/facts/ in the
    root-owned clone, which the user cannot write. No play turns fact gathering off, so each
    run gathers its own, fresh after the reboot a previous cycle made, and the user never
    writes into the clone or leaves anything behind in it.
    """
    return {
        **user_environment(home=home, user=user, runtime=runtime, ansible_playbook=ansible_playbook),
        **search_path_environment(clone=clone, collections_dir=collections_dir),
        "RUN_BASH_ANSIBLE_PLAYBOOK": ansible_playbook,
        "ANSIBLE_CACHE_PLUGIN": "memory",
        "ANSIBLE_VAULT_PASSWORD_FILE": f"/dev/fd/{vault_fd}",
        "RUN_BASH_SUDO_PASSWORD_FILE": f"/dev/fd/{become_fd}",
        play_lock.FD_ENV: str(lock_fd),
    }


def password_pipe(path: str, uid: int, gid: int) -> int:
    """The read end of a pipe holding `path`'s bytes, owned by `uid`. Not inheritable.

    ansible opens its vault password file by path, and opening /dev/fd/N re-checks the
    permissions of whatever the descriptor refers to: a root-only file, and a pipe root
    created, are both EACCES to the user. A pipe the user owns can be reopened. No file
    holds a copy on the way: run.bash passes the become password on to sudo and ansible
    through pipes as well. Made afresh for each play: a pipe is read once.
    """
    with open(path, "rb") as handle:
        data = handle.read(PIPE_MAX_BYTES + 1)
    if len(data) > PIPE_MAX_BYTES:
        raise ConfigError(f"{path} is larger than {PIPE_MAX_BYTES} bytes; it cannot be a password")
    read_end, write_end = os.pipe()
    try:
        os.fchown(read_end, uid, gid)
        if os.write(write_end, data) != len(data):
            raise OSError(f"a short write handing over {path}")
    except BaseException:
        os.close(read_end)
        raise
    finally:
        os.close(write_end)
    return read_end


def _require_unwritable(path: str, info: os.stat_result, what: str) -> None:
    if info.st_uid not in (0, os.geteuid()) or info.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        raise ConfigError(f"the {what} {path} is not owned by root, or others can write it")


def require_trusted(path: str, what: str, *, directory: bool) -> None:
    """Raise ConfigError unless only root (or the caller) can change what `path` names.

    A directory must be a real one. A file may be a symlink, and is then judged by its
    target; the directory holding the target must be unwritable too, or the file could be
    swapped.
    """
    try:
        link = os.lstat(path)
    except OSError as error:
        raise ConfigError(f"the {what} {path} cannot be found: {error.strerror}") from None
    if directory:
        if not stat.S_ISDIR(link.st_mode):
            raise ConfigError(f"the {what} {path} is not a real directory")
        _require_unwritable(path, link, what)
        return
    target = os.path.realpath(path)
    info = os.stat(target)
    if not stat.S_ISREG(info.st_mode):
        raise ConfigError(f"the {what} {path} is not a regular file")
    _require_unwritable(target, info, what)
    parent = os.path.dirname(target)
    _require_unwritable(parent, os.stat(parent), f"directory holding the {what}")


class RealHost:
    """The effects, for real. Run as root; the user's side runs through runuser."""

    def __init__(
        self, *, config: Config, clone: str, become: str, vault: str, allowed_signers: str,
        ansible_playbook: str, lock_fd: int,
    ) -> None:
        entry = pwd.getpwnam(config.user)
        self._config = config
        self._clone = clone
        self._become = become
        self._vault = vault
        self._allowed_signers = allowed_signers
        self._ansible_playbook = ansible_playbook
        self._lock_fd = lock_fd
        self._uid = entry.pw_uid
        self._gid = entry.pw_gid
        self._home = entry.pw_dir
        self._runtime = play_lock.runtime_dir_for(entry.pw_uid, env=os.environ, euid=os.geteuid())
        minute = os.environ.get(MINUTE_SECONDS_ENV, str(MINUTE))
        if not minute.isdigit():
            raise ConfigError(f"{MINUTE_SECONDS_ENV}={minute!r} is not a whole number of seconds")
        self._scale = int(minute) / MINUTE

    def _user_environment(self) -> dict[str, str]:
        return user_environment(home=self._home, user=self._config.user, runtime=self._runtime,
                                ansible_playbook=self._ansible_playbook)

    def _as_user(
        self, argv: list[str], *, env: dict[str, str], pass_fds: tuple[int, ...] = (), capture: bool = False,
    ) -> subprocess.CompletedProcess:
        return subprocess.run(
            ["runuser", "-u", self._config.user, "--", "env", "-i", *(f"{k}={v}" for k, v in env.items()), *argv],
            cwd=self._clone, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE if capture else sys.stderr,
            text=True, pass_fds=pass_fds, check=False,
        )

    def check_toolchain(self) -> str | None:
        ansible_config = os.path.join(os.path.dirname(self._ansible_playbook), "ansible-config")
        try:
            require_trusted(self._ansible_playbook, "system ansible-playbook", directory=False)
            require_trusted(ansible_config, "system ansible-config", directory=False)
            require_trusted(self._config.ansible_collections_dir, "ANSIBLE_COLLECTIONS_DIR", directory=True)
        except ConfigError as error:
            return str(error)
        # What the play's own `command -v` will find, asked as the user in the play's PATH.
        found = self._as_user(["/bin/sh", "-c", "command -v ansible-playbook"],
                              env=self._user_environment(), capture=True)
        resolved = found.stdout.strip() if found.returncode == 0 else ""
        if resolved != self._ansible_playbook:
            return (f"ansible-playbook as {self._config.user} resolves to {resolved or 'nothing'}, "
                    f"not the system {self._ansible_playbook}")
        return self._search_paths_error(ansible_config)

    def _search_paths_error(self, ansible_config: str) -> str | None:
        """Ask ansible itself, as the plays will run it, whether any code search path is in the home."""
        env = {**self._user_environment(),
               **search_path_environment(clone=self._clone, collections_dir=self._config.ansible_collections_dir)}
        dumped = self._as_user([ansible_config, "dump", "--format", "json"], env=env, capture=True)
        if dumped.returncode != 0:
            return f"{ansible_config} dump failed with exit {dumped.returncode}"
        try:
            findings = home_search_paths(json.loads(dumped.stdout), home=self._home, cwd=self._clone)
        except ValueError as error:
            return f"{ansible_config} dump could not be read: {error}"
        if findings:
            return (f"ansible as {self._config.user} would look for code under {self._home}, "
                    f"which the user can write: {'; '.join(findings)}")
        return None

    def check_remote(self, url: str) -> str | None:
        result = subprocess.run(
            ["git", "-C", self._clone, "config", "--get", f"remote.{_REMOTE}.url"],
            capture_output=True, text=True, check=False,
        )
        if result.returncode != 0:
            return f"the deploy clone has no {_REMOTE} remote"
        if result.stdout.strip() != url:
            return f"the deploy clone's {_REMOTE} remote is not REMOTE_URL from the config"
        return None

    def head_trusted(self) -> bool:
        return update.verify_head(
            checkout=self._clone, allowed_signers=self._allowed_signers, principal=self._config.principal,
            stderr=sys.stderr,
        ) == update.EXIT_OK

    def update(self, *, dry_run: bool) -> UpdateResult:
        out = io.StringIO()
        rc = update.run(
            checkout=self._clone, remote=_REMOTE, branch=self._config.branch,
            allowed_signers=self._allowed_signers, principal=self._config.principal,
            dry_run=dry_run, stdout=out, stderr=sys.stderr,
        )
        markers = dict(line.split(" ", 1) for line in out.getvalue().splitlines() if " " in line)
        return UpdateResult(
            rc=rc, old=markers.get("SELF-UPDATE-OLD"), new=markers.get("SELF-UPDATE-NEW"),
            target=markers.get("SELF-UPDATE-TARGET"), nothing=markers.get("SELF-UPDATE-NOTHING"),
        )

    def changed_plays(self, old: str, new: str) -> affected_plays.Report:
        return affected_plays.decide(self._clone, affected_plays.changed_between(self._clone, old, new))

    def allowlist(self) -> list[str]:
        return affected_plays.load_allowlist(self._clone)

    def run_play(self, play: str) -> int:
        with contextlib.ExitStack() as descriptors:
            become_fd = password_pipe(self._become, self._uid, self._gid)
            descriptors.callback(os.close, become_fd)
            vault_fd = password_pipe(self._vault, self._uid, self._gid)
            descriptors.callback(os.close, vault_fd)
            os.set_inheritable(self._lock_fd, True)
            env = play_environment(
                home=self._home, user=self._config.user, runtime=self._runtime, clone=self._clone,
                ansible_playbook=self._ansible_playbook, collections_dir=self._config.ansible_collections_dir,
                lock_fd=self._lock_fd, become_fd=become_fd, vault_fd=vault_fd,
            )
            return self._as_user([os.path.join(self._clone, "run.bash"), "--headless", play],
                                 env=env, pass_fds=(self._lock_fd, become_fd, vault_fd)).returncode

    def _ccy_sessions(self, args: list[str]) -> int:
        return self._as_user([os.path.join(self._home, ".local/bin/ccy-sessions"), *args],
                             env=self._user_environment()).returncode

    def notify(self, args: list[str]) -> int:
        return self._ccy_sessions(["notify", *args])

    def verify_restore(self, wait_seconds: int) -> int:
        return self._ccy_sessions(["verify-restore", "--wait", str(wait_seconds)])

    def reboot(self) -> int:
        return subprocess.run(["systemctl", "reboot"], stdin=subprocess.DEVNULL, check=False).returncode

    def boot_id(self) -> str:
        with open("/proc/sys/kernel/random/boot_id", encoding="utf-8") as handle:
            return handle.read().strip()

    def sleep(self, seconds: float) -> None:
        def interrupted(signum: int, frame: object) -> None:
            raise Cancelled()

        previous = {sig: signal.signal(sig, interrupted) for sig in (signal.SIGINT, signal.SIGTERM)}
        try:
            time.sleep(seconds * self._scale)
        finally:
            for sig, handler in previous.items():
                signal.signal(sig, handler)

    def now(self) -> str:
        return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def read_private(path: str, what: str, *, secret: bool = False) -> str:
    """A root-side file that decides the cycle: owned by root or the caller, and not writable by others.

    A `secret` must not be readable by anyone but its owner either: once others can read a
    password, it is out, whatever the cycle then does with it.
    """
    try:
        info = os.lstat(path)
    except OSError as error:
        raise ConfigError(f"the {what} {path} cannot be read: {error.strerror}") from None
    if not stat.S_ISREG(info.st_mode):
        raise ConfigError(f"the {what} {path} is not a regular file")
    if info.st_uid not in (0, os.geteuid()) or info.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        raise ConfigError(f"the {what} {path} is not owned by root, or others can write it")
    if secret and info.st_mode & (stat.S_IRWXG | stat.S_IRWXO):
        raise ConfigError(f"the {what} {path} is open to others (mode {stat.S_IMODE(info.st_mode):04o}); it must be 0600")
    with open(path, encoding="utf-8") as handle:
        return handle.read()


@contextlib.contextmanager
def _held_lock(user: str, stderr: TextIO) -> Iterator[int | None]:
    """The play lock for `user`, held for the whole cycle; None when another run holds it."""
    entry = pwd.getpwnam(user)
    runtime = play_lock.runtime_dir_for(entry.pw_uid, env=os.environ, euid=os.geteuid())
    fd = play_lock.open_lock(runtime, entry.pw_uid, entry.pw_gid)
    try:
        if not play_lock.acquire(fd):
            stderr.write(f"self-update: another play run holds the lock ({play_lock.read_note(fd) or 'unknown holder'})\n")
            yield None
            return
        play_lock.write_note(fd, "fedora-desktop-self-update")
        yield fd
    finally:
        os.close(fd)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="The unattended self-update cycle (Plan 00137).")
    parser.add_argument("--config", required=True)
    parser.add_argument("--state-dir", required=True)
    parser.add_argument("--published-dir", required=True)
    parser.add_argument("--clone", required=True)
    parser.add_argument("--become", required=True)
    parser.add_argument("--vault", required=True)
    parser.add_argument("--allowed-signers", required=True)
    parser.add_argument("--ansible-playbook", required=True)
    commands = parser.add_subparsers(dest="command", required=True)
    run_parser = commands.add_parser("run")
    run_parser.add_argument("--dry-run", action="store_true")
    commands.add_parser("verify")
    commands.add_parser("status")
    try:
        args = parser.parse_args(argv)
    except SystemExit as exit_request:
        return EXIT_USAGE if exit_request.code else EXIT_OK

    state = State(args.state_dir, published_dir=args.published_dir)
    try:
        if args.command == "status":
            return status(state, stdout=sys.stdout)
        config = parse_config(read_private(args.config, "config file"))
        if args.command == "run" and not args.dry_run:
            read_private(args.become, "become password file", secret=True)
            read_private(args.vault, "vault password file", secret=True)
    except ConfigError as error:
        sys.stderr.write(f"self-update: config invalid: {error}\n")
        return EXIT_CONFIG
    except (StateError, KeyError) as error:
        sys.stderr.write(f"self-update: {error}\n")
        return EXIT_CONFIG

    def real_host(lock_fd: int) -> RealHost:
        return RealHost(config=config, clone=args.clone, become=args.become, vault=args.vault,
                        allowed_signers=args.allowed_signers, ansible_playbook=args.ansible_playbook,
                        lock_fd=lock_fd)

    try:
        if args.command == "verify":
            return verify(config, real_host(-1), state, stdout=sys.stdout, stderr=sys.stderr)
        with _held_lock(config.user, sys.stderr) as lock_fd:
            if lock_fd is None:
                return EXIT_LOCKED
            return run_cycle(config, real_host(lock_fd), state, dry_run=args.dry_run,
                             stdout=sys.stdout, stderr=sys.stderr)
    except KeyError:
        sys.stderr.write(f"self-update: config invalid: USER={config.user} is not a user on this host\n")
        return EXIT_CONFIG
    except (ConfigError, play_lock.LockError, StateError) as error:
        sys.stderr.write(f"self-update: {error}\n")
        return EXIT_CONFIG


if __name__ == "__main__":
    sys.exit(main())
