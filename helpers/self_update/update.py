"""Bring the deploy clone up to the newest owner-signed commit, or refuse (Plan 00137 T1.1, T1.2).

Run: `python3 -m helpers.self_update.update --checkout PATH --remote origin --branch F44
--allowed-signers FILE --principal NAME [--os-release /etc/os-release]`

In order, stopping at the first thing that is wrong:

1. The allowed-signers file is checked: a regular file, not a symlink, owned by root or
   the caller, with no group/other write bit on it or on its directory, and at least one
   entry. It is what the whole gate trusts, so a file that someone else could rewrite
   makes the gate worthless.
2. `git fetch` of exactly `<remote>/<branch>`.
3. The clone must be on `<branch>`, not detached, and clean, including untracked
   files. An untracked file could shadow one the incoming commit adds, or be picked up
   by a play's glob. Ignored files (the vault password file, run logs) are not dirt.
4. The deployed commit must be an ancestor of the remote tip. Local commits, or a remote
   rewound behind it, are "ahead"; any other relationship means the remote's history was
   rewritten. Both refuse.
5. The gate (gate.py): walk the tip's first-parent history down to the deployed commit
   and take the newest commit trusted by the pinned signer.
6. The Fedora pin in THAT commit's `vars/fedora-version.yml` must match the running
   system. This is checked on the target's content before anything moves, so a refusal
   leaves the clone exactly where it was.
7. `git merge --ff-only <target>`, then a read-back that HEAD is the target.

**Nothing from the checkout is trusted to judge the checkout.** Every git call pins the
settings that decide or execute on the command line, which overrides the repository's
own config:
- `gpg.ssh.allowedSignersFile` is the path given here;
- `gpg.ssh.program` is the `ssh-keygen` resolved on PATH;
- the OpenPGP and X.509 programs are `false`;
- hooks are disabled (`core.hooksPath=/dev/null`);
- `core.fsmonitor` is off, since a configured fsmonitor runs a program on `git status`;
- the `ext::` transport is forbidden.

stdout carries only the stable marker lines; every diagnostic goes to stderr:

    SELF-UPDATE-OLD <sha>      deployed before this run
    SELF-UPDATE-NEW <sha>      deployed now
    SELF-UPDATE-TARGET <sha>   --dry-run: the commit a real run would fast-forward to
    SELF-UPDATE-NOTHING <sha>  no trusted commit above the deployed one

`--dry-run` runs every step, the fetch included, and stops before the fast-forward.

Two more modes keep one invariant: **the deploy clone's HEAD is always a commit the pinned
signer signed.** The gate above only judges commits ABOVE HEAD, so HEAD itself has to be
trusted by the time root imports anything from the clone.

- `--anchor` (the play, after it clones): leave a trusted HEAD where it is, otherwise
  move the branch back to the newest trusted commit in HEAD's first-parent history. It
  never moves forward, which is the cycle's job. No trusted commit means refusal. Unlike
  an update, it also refuses any file the commit does not track, IGNORED ones included,
  before and after moving, except the exact paths given with `--allow-untracked`. Root
  imports from this tree, and `git status` never lists an ignored `.pyc` or a leftover
  submodule checkout.

      SELF-UPDATE-ANCHORED <sha>       HEAD, trusted
      SELF-UPDATE-ANCHOR-MOVED <sha>   the untrusted commit it moved away from

- `verify_head` (the cycle, before a first run): EXIT_OK only if HEAD is trusted.
"""

from __future__ import annotations

import argparse
import os
import shutil
import stat
import subprocess
import sys
from collections.abc import Callable, Iterator, Mapping
from typing import TextIO

from helpers.self_update import gate

EXIT_OK = 0
EXIT_USAGE = 2
EXIT_UNTRUSTED = 9
EXIT_FETCH_FAILED = 10
EXIT_DIRTY = 11
EXIT_DETACHED = 12
EXIT_WRONG_BRANCH = 13
EXIT_AHEAD = 14
EXIT_DIVERGED = 15
EXIT_SIGNERS_FILE = 16
EXIT_BAD_SIGNATURE = 17
EXIT_FEDORA_MISMATCH = 18
EXIT_GIT_FAILED = 19

_FETCH_TIMEOUT_SECONDS = 120
#: How far back `--anchor` looks for a signed commit before it refuses.
ANCHOR_WALK_LIMIT = 1000
_GIT_TIMEOUT_SECONDS = 60
_FIELD_SEP = "\x1f"


class Refusal(Exception):
    """A reason to stop, carrying the exit status that names it."""

    def __init__(self, code: int, message: str) -> None:
        super().__init__(message)
        self.code = code


def _check_signers_file(path: str) -> None:
    try:
        info = os.lstat(path)
    except OSError as error:
        raise Refusal(EXIT_SIGNERS_FILE, f"allowed-signers file {path!r} cannot be read: {error}") from error
    if not stat.S_ISREG(info.st_mode):
        raise Refusal(EXIT_SIGNERS_FILE, f"allowed-signers file {path!r} is not a regular file (a symlink is refused)")
    if info.st_uid not in (0, os.geteuid()):
        raise Refusal(EXIT_SIGNERS_FILE, f"allowed-signers file {path!r} is owned by uid {info.st_uid}, not root or the caller")
    if info.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        raise Refusal(EXIT_SIGNERS_FILE, f"allowed-signers file {path!r} is group- or world-writable")
    directory = os.path.dirname(os.path.abspath(path))
    if os.stat(directory).st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        raise Refusal(
            EXIT_SIGNERS_FILE,
            f"the directory holding the allowed-signers file ({directory}) is group- or world-writable, "
            "so the file could be replaced",
        )
    with open(path, encoding="utf-8") as handle:
        entries = [line for line in handle if line.strip() and not line.lstrip().startswith("#")]
    if not entries:
        raise Refusal(EXIT_SIGNERS_FILE, f"allowed-signers file {path!r} names no signer")


class _Git:
    """git against one checkout, with the deciding settings pinned on the command line."""

    def __init__(self, checkout: str, allowed_signers: str, env: Mapping[str, str]) -> None:
        ssh_keygen = shutil.which("ssh-keygen", path=env.get("PATH"))
        if ssh_keygen is None:
            raise Refusal(EXIT_GIT_FAILED, "ssh-keygen is not on PATH, so no signature can be verified")
        self._checkout = checkout
        self._env = {**env, "GIT_TERMINAL_PROMPT": "0", "GIT_ASKPASS": "", "SSH_ASKPASS": ""}
        self._config = [
            "-c", "core.hooksPath=/dev/null",
            "-c", "core.fsmonitor=false",
            "-c", "protocol.ext.allow=never",
            "-c", "gpg.program=false",
            "-c", "gpg.openpgp.program=false",
            "-c", "gpg.x509.program=false",
            "-c", f"gpg.ssh.program={ssh_keygen}",
            "-c", f"gpg.ssh.allowedSignersFile={os.path.abspath(allowed_signers)}",
        ]

    def run(self, *args: str, timeout: int = _GIT_TIMEOUT_SECONDS) -> subprocess.CompletedProcess:
        return subprocess.run(
            ["git", "-C", self._checkout, *self._config, *args],
            capture_output=True, text=True, timeout=timeout, env=self._env, check=False,
        )

    def out(self, *args: str) -> str:
        result = self.run(*args)
        if result.returncode != 0:
            raise Refusal(EXIT_GIT_FAILED, f"git {' '.join(args)} failed: {result.stderr.strip()}")
        return result.stdout

    def is_ancestor(self, older: str, newer: str) -> bool:
        result = self.run("merge-base", "--is-ancestor", older, newer)
        if result.returncode not in (0, 1):
            raise Refusal(EXIT_GIT_FAILED, f"git merge-base failed: {result.stderr.strip()}")
        return result.returncode == 0


def _verdict(git: _Git, sha: str, principal: str, stderr: TextIO) -> str:
    """The gate's verdict on one commit. Only an SSH signature is ever checked."""
    kind = gate.signature_kind(git.out("cat-file", "commit", sha))
    if kind == gate.KIND_NONE:
        return gate.UNTRUSTED
    if kind == gate.KIND_OTHER:
        stderr.write(f"self-update: {sha[:12]} carries a non-SSH signature; not trusted, never executed\n")
        return gate.UNTRUSTED
    status, signer = git.out("log", "-1", f"--format=%G?{_FIELD_SEP}%GS", sha).rstrip("\n").split(_FIELD_SEP, 1)
    verdict = gate.judge(status, signer, principal)
    if verdict == gate.UNTRUSTED:
        stderr.write(
            f"self-update: {sha[:12]} is signed (status {status}, signer {signer or 'none matched'}) "
            "but not trusted: it is not the pinned principal\n"
        )
    return verdict


def _candidates(git: _Git, head: str, tip: str, principal: str, stderr: TextIO) -> Iterator[tuple[str, str]]:
    """`(sha, verdict)` for the tip's first-parent commits above `head`, newest first.

    Stops at the first commit that does not descend from `head`: below a merge that
    brought `head` in through a second parent, the first-parent line predates it, and a
    fast-forward to any of those commits is impossible.
    """
    for sha in git.out("rev-list", "--first-parent", f"{head}..{tip}").split():
        if not git.is_ancestor(head, sha):
            return
        yield sha, _verdict(git, sha, principal, stderr)


def _require_clean_branch(git: _Git, branch: str) -> None:
    current = git.run("symbolic-ref", "--quiet", "--short", "HEAD")
    if current.returncode != 0:
        raise Refusal(EXIT_DETACHED, "the checkout is on a detached HEAD, not a branch")
    if current.stdout.strip() != branch:
        raise Refusal(EXIT_WRONG_BRANCH, f"the checkout is on {current.stdout.strip()!r}, not {branch!r}")
    dirt = git.out("status", "--porcelain=v1", "--untracked-files=all").splitlines()
    if dirt:
        shown = "; ".join(dirt[:10]) + (f"; and {len(dirt) - 10} more" if len(dirt) > 10 else "")
        raise Refusal(EXIT_DIRTY, f"the checkout has local changes or untracked files: {shown}")


def _require_no_strays(git: _Git, allowed: tuple[str, ...]) -> None:
    """Refuse any file git does not track, ignored ones included, except the exact paths allowed.

    `git status` never lists an ignored file, and python imports a `.pyc` beside its source
    without comparing the two. Root imports from the deploy clone, so an ignored file there
    is code nobody signed: a leftover submodule checkout, or a planted `__pycache__`.
    """
    listed = git.out("ls-files", "--others", "-z").split("\0")
    strays = [path for path in listed if path and path not in allowed]
    if strays:
        shown = "; ".join(strays[:10]) + (f"; and {len(strays) - 10} more" if len(strays) > 10 else "")
        raise Refusal(EXIT_DIRTY, f"the checkout holds files its commit does not, ignored ones included: {shown}")


def _require_fedora_pin(git: _Git, target: str, os_release: str) -> None:
    try:
        pinned = gate.pinned_fedora_version(git.out("show", f"{target}:vars/fedora-version.yml"))
        with open(os_release, encoding="utf-8") as handle:
            running = gate.running_fedora_version(handle.read())
    except (OSError, ValueError, Refusal) as error:
        raise Refusal(EXIT_FEDORA_MISMATCH, f"the Fedora version pin could not be confirmed: {error}") from error
    if pinned != running:
        raise Refusal(
            EXIT_FEDORA_MISMATCH,
            f"commit {target[:12]} pins Fedora {pinned}, and this system runs Fedora {running}; "
            "nothing was moved",
        )


def _update(
    *, git: _Git, remote: str, branch: str, principal: str, os_release: str, dry_run: bool,
    stdout: TextIO, stderr: TextIO,
) -> int:
    fetched = git.run(
        "fetch", "--no-tags", "--prune", remote, f"+refs/heads/{branch}:refs/remotes/{remote}/{branch}",
        timeout=_FETCH_TIMEOUT_SECONDS,
    )
    if fetched.returncode != 0:
        raise Refusal(EXIT_FETCH_FAILED, f"git fetch {remote} {branch} failed: {fetched.stderr.strip()}")

    _require_clean_branch(git, branch)

    head = git.out("rev-parse", "HEAD").strip()
    tip = git.out("rev-parse", f"refs/remotes/{remote}/{branch}").strip()
    if head != tip and not git.is_ancestor(head, tip):
        if git.is_ancestor(tip, head):
            raise Refusal(
                EXIT_AHEAD,
                f"the checkout ({head[:12]}) is ahead of {remote}/{branch} ({tip[:12]}): "
                "it holds local commits, or the remote was rewound",
            )
        raise Refusal(
            EXIT_DIVERGED,
            f"{remote}/{branch} ({tip[:12]}) does not contain the deployed commit ({head[:12]}): "
            "its history was rewritten",
        )

    choice = gate.choose_target(_candidates(git, head, tip, principal, stderr) if head != tip else [])
    if choice.refused is not None:
        raise Refusal(
            EXIT_BAD_SIGNATURE,
            f"commit {choice.refused[:12]} above the newest trusted commit carries a signature that "
            "fails verification, is revoked, or cannot be checked; refusing the whole cycle",
        )
    if choice.target is None:
        stdout.write(f"SELF-UPDATE-NOTHING {head}\n")
        return EXIT_OK

    _require_fedora_pin(git, choice.target, os_release)

    if dry_run:
        stdout.write(f"SELF-UPDATE-OLD {head}\nSELF-UPDATE-TARGET {choice.target}\n")
        return EXIT_OK
    git.out("merge", "--ff-only", "--quiet", choice.target)
    now = git.out("rev-parse", "HEAD").strip()
    if now != choice.target:
        raise Refusal(EXIT_GIT_FAILED, f"after the fast-forward HEAD is {now[:12]}, not {choice.target[:12]}")
    stdout.write(f"SELF-UPDATE-OLD {head}\nSELF-UPDATE-NEW {now}\n")
    return EXIT_OK


def _anchor(
    *, git: _Git, branch: str, principal: str, os_release: str, allow_untracked: tuple[str, ...],
    stdout: TextIO, stderr: TextIO,
) -> int:
    _require_clean_branch(git, branch)
    _require_no_strays(git, allow_untracked)
    head = git.out("rev-parse", "HEAD").strip()
    history = git.out("rev-list", "--first-parent", f"--max-count={ANCHOR_WALK_LIMIT}", "HEAD").split()
    choice = gate.choose_target((sha, _verdict(git, sha, principal, stderr)) for sha in history)
    if choice.refused is not None:
        raise Refusal(
            EXIT_BAD_SIGNATURE,
            f"commit {choice.refused[:12]} above the newest trusted commit carries a signature that "
            "fails verification, is revoked, or cannot be checked; refusing to anchor",
        )
    if choice.target is None:
        raise Refusal(
            EXIT_UNTRUSTED,
            f"no commit in the last {ANCHOR_WALK_LIMIT} of HEAD's history is signed by {principal}; "
            "push a commit made where that key signs every commit",
        )
    if choice.target == head:
        stdout.write(f"SELF-UPDATE-ANCHORED {head}\n")
        return EXIT_OK
    _require_fedora_pin(git, choice.target, os_release)
    git.out("checkout", "--quiet", "-B", branch, choice.target)
    now = git.out("rev-parse", "HEAD").strip()
    if now != choice.target:
        raise Refusal(EXIT_GIT_FAILED, f"after anchoring HEAD is {now[:12]}, not {choice.target[:12]}")
    # A checkout removes the files the old commit tracked, but not a directory still holding
    # files it did not track, so the tree is judged again at the commit it now claims to be.
    _require_clean_branch(git, branch)
    _require_no_strays(git, allow_untracked)
    stdout.write(f"SELF-UPDATE-ANCHORED {now}\nSELF-UPDATE-ANCHOR-MOVED {head}\n")
    return EXIT_OK


def _guarded(stderr: TextIO, action: Callable[[], int]) -> int:
    try:
        return action()
    except Refusal as refusal:
        stderr.write(f"self-update: refused: {refusal}\n")
        return refusal.code
    except subprocess.TimeoutExpired as error:
        stderr.write(f"self-update: refused: git timed out: {' '.join(str(a) for a in error.cmd)}\n")
        return EXIT_GIT_FAILED


def anchor(
    *, checkout: str, branch: str, allowed_signers: str, principal: str, os_release: str = "/etc/os-release",
    allow_untracked: tuple[str, ...] = (), stdout: TextIO, stderr: TextIO, env: Mapping[str, str] | None = None,
) -> int:
    """Make the checkout's HEAD a trusted commit, moving it back if it is not.

    `allow_untracked` names, exactly, the files the caller itself puts in the checkout (the
    play's host_vars copy). Any other file the commit does not track refuses the anchor.
    """
    if not principal:
        stderr.write("self-update: --principal must name the signer to trust\n")
        return EXIT_USAGE

    def act() -> int:
        _check_signers_file(allowed_signers)
        git = _Git(checkout, allowed_signers, env if env is not None else os.environ)
        return _anchor(git=git, branch=branch, principal=principal, os_release=os_release,
                       allow_untracked=allow_untracked, stdout=stdout, stderr=stderr)

    return _guarded(stderr, act)


def verify_head(
    *, checkout: str, allowed_signers: str, principal: str, stderr: TextIO,
    env: Mapping[str, str] | None = None,
) -> int:
    """EXIT_OK when the checkout's HEAD is signed by the pinned principal, else EXIT_UNTRUSTED."""
    if not principal:
        stderr.write("self-update: --principal must name the signer to trust\n")
        return EXIT_USAGE

    def act() -> int:
        _check_signers_file(allowed_signers)
        git = _Git(checkout, allowed_signers, env if env is not None else os.environ)
        head = git.out("rev-parse", "HEAD").strip()
        if _verdict(git, head, principal, stderr) == gate.TRUSTED:
            return EXIT_OK
        stderr.write(f"self-update: HEAD {head[:12]} is not a commit signed by {principal}\n")
        return EXIT_UNTRUSTED

    return _guarded(stderr, act)


def run(
    *,
    checkout: str,
    remote: str,
    branch: str,
    allowed_signers: str,
    principal: str,
    os_release: str = "/etc/os-release",
    dry_run: bool = False,
    stdout: TextIO,
    stderr: TextIO,
    env: Mapping[str, str] | None = None,
) -> int:
    """Update the checkout or refuse; the return value is the exit status."""
    if not principal:
        stderr.write("self-update: --principal must name the signer to trust\n")
        return EXIT_USAGE

    def act() -> int:
        _check_signers_file(allowed_signers)
        git = _Git(checkout, allowed_signers, env if env is not None else os.environ)
        return _update(
            git=git, remote=remote, branch=branch, principal=principal, os_release=os_release,
            dry_run=dry_run, stdout=stdout, stderr=stderr,
        )

    return _guarded(stderr, act)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Fast-forward the deploy clone to the newest owner-signed commit.")
    parser.add_argument("--checkout", required=True)
    parser.add_argument("--remote")
    parser.add_argument("--branch", required=True)
    parser.add_argument("--allowed-signers", required=True)
    parser.add_argument("--principal", required=True)
    parser.add_argument("--os-release", default="/etc/os-release")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--dry-run", action="store_true")
    mode.add_argument("--anchor", action="store_true", help="make HEAD a trusted commit (the play, after cloning)")
    parser.add_argument("--allow-untracked", action="append", default=[], metavar="PATH",
                        help="with --anchor: an untracked file the caller put there itself (repeatable)")
    args = parser.parse_args(argv)
    if args.allow_untracked and not args.anchor:
        parser.error("--allow-untracked only applies with --anchor")
    if args.anchor:
        return anchor(
            checkout=args.checkout, branch=args.branch, allowed_signers=args.allowed_signers,
            principal=args.principal, os_release=args.os_release, allow_untracked=tuple(args.allow_untracked),
            stdout=sys.stdout, stderr=sys.stderr,
        )
    if not args.remote:
        parser.error("--remote is required to update")
    return run(
        checkout=args.checkout, remote=args.remote, branch=args.branch,
        allowed_signers=args.allowed_signers, principal=args.principal, os_release=args.os_release,
        dry_run=args.dry_run, stdout=sys.stdout, stderr=sys.stderr,
    )


if __name__ == "__main__":
    sys.exit(main())
