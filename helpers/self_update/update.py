"""Bring the deploy clone up to the newest owner-signed release, or refuse (Plan 00137 T1.1, T1.2).

Run: `python3 -m helpers.self_update.update --checkout PATH --remote origin --branch F44
--allowed-signers FILE --principal NAME [--channel tags|branch] [--os-release /etc/os-release]`

Two channels (Plan 00153). `tags`, the default, deploys the newest signed release tag
`<major>.<minor>.<patch>` of the branch's Fedora major (`F44` gives 44): see `_update_tags`
and `_newest_release`, and CLAUDE/Plan/00153-release-tags-fedora-major-semver/DESIGN-self-update-tags.md.
The tag fetch never forces and prunes, only the newest tag is judged, and an unusable one
refuses (never a fall back to an older tag or the tip). `branch` deploys the newest commit
the pinned signer signed, as described from here on:

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
    SELF-UPDATE-TAG <name>     tags channel: the release the line above is (also on --anchor)
    SELF-UPDATE-REFUSED <why>  tags channel, exit 20/21/22: the tag and the check, for the alert

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
import re
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
EXIT_NO_RELEASE = 20
EXIT_TAG_REFUSED = 21
EXIT_TAG_MOVED = 22

#: What the cycle follows. `tags` (the default) deploys the newest signed release tag of the
#: branch's Fedora major; `branch` deploys the newest owner-signed commit on the branch.
CHANNEL_TAGS = "tags"
CHANNEL_BRANCH = "branch"
CHANNELS = (CHANNEL_TAGS, CHANNEL_BRANCH)

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
        # LC_ALL=C: a refused tag move is told apart from other fetch failures by git's own wording.
        self._env = {**env, "GIT_TERMINAL_PROMPT": "0", "GIT_ASKPASS": "", "SSH_ASKPASS": "", "LC_ALL": "C"}
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


def _release_major(branch: str) -> int:
    major = gate.branch_major(branch)
    if major is None:
        raise Refusal(
            EXIT_USAGE,
            f"the {CHANNEL_TAGS} channel needs a Fedora release branch named F<number> (F44), not {branch!r}: "
            "there is no tag family to follow",
        )
    return major


def _refuse_tag(tag: str, why: str) -> Refusal:
    return Refusal(EXIT_TAG_REFUSED, f"release tag {tag} is refused: {why}; nothing was moved, and no older release is tried")


def _newest_release(git: _Git, *, remote: str, branch: str, major: int, principal: str, stderr: TextIO) -> tuple[str, str]:
    """`(tag name, commit)` of the newest release, judged, or a Refusal.

    Only the highest tag is judged. An older one is never a fallback: skipping the newest
    release because it looks wrong is how a withdrawn or tampered release gets replaced by
    an old one without anyone being told.
    """
    names = git.out("for-each-ref", "--format=%(refname:strip=2)", f"refs/tags/{major}.*").split()
    choice = gate.choose_release(names, major)
    for name in choice.ignored:
        stderr.write(f"self-update: tag {name} is not a release name ({major}.MINOR.PATCH); ignored\n")
    tag = choice.tag
    if tag is None:
        raise Refusal(
            EXIT_NO_RELEASE,
            f"no release tag {major}.MINOR.PATCH exists on {remote}; the owner makes one with "
            "scripts/release.bash, and the branch tip is never deployed in its place",
        )
    ref = f"refs/tags/{tag}"
    kind, internal = git.out("for-each-ref", f"--format=%(objecttype){_FIELD_SEP}%(tag)", ref).rstrip("\n").split(_FIELD_SEP, 1)
    if kind != "tag":
        raise _refuse_tag(tag, f"it is a {kind} reference, not an annotated signed tag")
    if internal != tag:
        raise _refuse_tag(tag, f"the tag object inside it is named {internal!r}")
    verified = git.run("verify-tag", "--raw", ref)
    report = f"{verified.stdout}\n{verified.stderr}"
    signer = gate.tag_signer(report)
    if verified.returncode != 0 or signer is None:
        last_line = (report.strip().splitlines() or ["no output"])[-1]
        raise _refuse_tag(tag, f"its signature does not verify against the pinned keys (git said: {last_line})")
    if gate.judge("G", signer, principal) != gate.TRUSTED:
        raise _refuse_tag(tag, f"it is signed by {signer}, not the pinned principal {principal}")
    peeled = git.run("rev-parse", "--verify", f"{ref}^{{commit}}")
    if peeled.returncode != 0:
        raise _refuse_tag(tag, "it does not point at a commit")
    commit = peeled.stdout.strip()
    if not git.is_ancestor(commit, git.out("rev-parse", f"refs/remotes/{remote}/{branch}").strip()):
        raise _refuse_tag(tag, f"its commit {commit[:12]} is not on {remote}/{branch}")
    if _verdict(git, commit, principal, stderr) != gate.TRUSTED:
        raise _refuse_tag(tag, f"its commit {commit[:12]} is not signed by {principal}")
    return tag, commit


_CLOBBERED_TAG = re.compile(r"^\s*!\s+\[rejected\]\s+(\S+)\s+->\s+\S+\s+\(would clobber existing tag\)", re.MULTILINE)
#: How a person clears a moved release tag; it is in the refusal and, through cycle.py, the alert.
MOVED_TAG_REMEDY = (
    "withdraw the moved tag upstream so the next fetch prunes it, and release under a new number; or re-clone: "
    "run the self-update play once with self_update_enabled: false, then once with it true"
)


def _fetch_releases(git: _Git, *, remote: str, branch: str, major: int, stderr: TextIO) -> None:
    """Fetch the branch and its `<major>.*` tags, or refuse.

    No `+` on the tag refspec: git then refuses to move a local tag that now points elsewhere,
    and that refusal is never retried with force. --prune drops a tag withdrawn upstream. A
    moved tag that is not a release name (a draft such as `44.2.0-rc1`) is the owner's own
    namespace and must not stop the fleet: git has still updated every other ref, so it is
    reported and passed over. A moved release name refuses.
    """
    fetched = git.run(
        "fetch", "--no-tags", "--prune", remote, f"+refs/heads/{branch}:refs/remotes/{remote}/{branch}",
        f"refs/tags/{major}.*:refs/tags/{major}.*", timeout=_FETCH_TIMEOUT_SECONDS,
    )
    if fetched.returncode == 0:
        return
    detail = fetched.stderr.strip()
    clobbered = _CLOBBERED_TAG.findall(fetched.stderr)
    if not clobbered or any(line.startswith("fatal:") for line in detail.splitlines()):
        raise Refusal(EXIT_FETCH_FAILED, f"git fetch {remote} {branch} and its release tags failed: {detail}")
    moved = [name for name in clobbered if gate.parse_release_tag(name, major) is not None]
    if moved:
        raise Refusal(EXIT_TAG_MOVED, f"release tag {', '.join(moved)} was moved upstream and is not followed: {MOVED_TAG_REMEDY}")
    for name in clobbered:
        stderr.write(f"self-update: tag {name} is not a release name and was moved upstream; its local copy is kept\n")


def _update_tags(
    *, git: _Git, remote: str, branch: str, principal: str, os_release: str, dry_run: bool,
    stdout: TextIO, stderr: TextIO,
) -> int:
    major = _release_major(branch)
    _fetch_releases(git, remote=remote, branch=branch, major=major, stderr=stderr)
    _require_clean_branch(git, branch)
    head = git.out("rev-parse", "HEAD").strip()
    tag, target = _newest_release(git, remote=remote, branch=branch, major=major, principal=principal, stderr=stderr)
    if target == head:
        stdout.write(f"SELF-UPDATE-NOTHING {head}\nSELF-UPDATE-TAG {tag}\n")
        return EXIT_OK
    if not git.is_ancestor(head, target):
        if git.is_ancestor(target, head):
            raise Refusal(
                EXIT_AHEAD,
                f"the checkout ({head[:12]}) is ahead of the newest release {tag} ({target[:12]}): it holds "
                "local commits, or that release was withdrawn; nothing downgrades by itself",
            )
        raise Refusal(
            EXIT_DIVERGED,
            f"the newest release {tag} ({target[:12]}) does not contain the deployed commit ({head[:12]}): "
            "history was rewritten",
        )
    _require_fedora_pin(git, target, os_release)
    if dry_run:
        stdout.write(f"SELF-UPDATE-OLD {head}\nSELF-UPDATE-TARGET {target}\nSELF-UPDATE-TAG {tag}\n")
        return EXIT_OK
    git.out("merge", "--ff-only", "--quiet", target)
    now = git.out("rev-parse", "HEAD").strip()
    if now != target:
        raise Refusal(EXIT_GIT_FAILED, f"after the fast-forward HEAD is {now[:12]}, not {target[:12]}")
    stdout.write(f"SELF-UPDATE-OLD {head}\nSELF-UPDATE-NEW {now}\nSELF-UPDATE-TAG {tag}\n")
    return EXIT_OK


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


def _anchor_target_on_branch(git: _Git, principal: str, stderr: TextIO) -> str:
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
    return choice.target


def _anchor(
    *, git: _Git, remote: str, branch: str, channel: str, principal: str, os_release: str,
    allow_untracked: tuple[str, ...], allow_rewind: bool, stdout: TextIO, stderr: TextIO,
) -> int:
    _require_clean_branch(git, branch)
    _require_no_strays(git, allow_untracked)
    head = git.out("rev-parse", "HEAD").strip()
    tag_line = ""
    if channel == CHANNEL_TAGS:
        major = _release_major(branch)
        _fetch_releases(git, remote=remote, branch=branch, major=major, stderr=stderr)
        tag, target = _newest_release(git, remote=remote, branch=branch, major=major, principal=principal, stderr=stderr)
        tag_line = f"SELF-UPDATE-TAG {tag}\n"
        if target != head and not allow_rewind and not git.is_ancestor(head, target):
            raise Refusal(
                EXIT_AHEAD if git.is_ancestor(target, head) else EXIT_DIVERGED,
                f"anchoring on release {tag} ({target[:12]}) would move the clone back from {head[:12]} and discard "
                "commits the release does not hold (a channel switched from branch, a withdrawn release, or local "
                "commits); nothing was moved. If that is intended, re-clone: run the self-update play once with "
                "self_update_enabled: false, then once with it true",
            )
    else:
        target = _anchor_target_on_branch(git, principal, stderr)
    if target == head:
        stdout.write(f"SELF-UPDATE-ANCHORED {head}\n{tag_line}")
        return EXIT_OK
    _require_fedora_pin(git, target, os_release)
    git.out("checkout", "--quiet", "-B", branch, target)
    now = git.out("rev-parse", "HEAD").strip()
    if now != target:
        raise Refusal(EXIT_GIT_FAILED, f"after anchoring HEAD is {now[:12]}, not {target[:12]}")
    # A checkout removes the files the old commit tracked, but not a directory still holding
    # files it did not track, so the tree is judged again at the commit it now claims to be.
    _require_clean_branch(git, branch)
    _require_no_strays(git, allow_untracked)
    stdout.write(f"SELF-UPDATE-ANCHORED {now}\nSELF-UPDATE-ANCHOR-MOVED {head}\n{tag_line}")
    return EXIT_OK


#: Refusals the cycle reports to the alert, naming the tag and the check, as one stdout line.
_REPORTED_REFUSALS = (EXIT_NO_RELEASE, EXIT_TAG_REFUSED, EXIT_TAG_MOVED)


def _guarded(stderr: TextIO, action: Callable[[], int], stdout: TextIO | None = None) -> int:
    try:
        return action()
    except Refusal as refusal:
        stderr.write(f"self-update: refused: {refusal}\n")
        if stdout is not None and refusal.code in _REPORTED_REFUSALS:
            stdout.write(f"SELF-UPDATE-REFUSED {' '.join(str(refusal).split())}\n")
        return refusal.code
    except subprocess.TimeoutExpired as error:
        stderr.write(f"self-update: refused: git timed out: {' '.join(str(a) for a in error.cmd)}\n")
        return EXIT_GIT_FAILED


def _usage_error(principal: str, channel: str, stderr: TextIO) -> int | None:
    if not principal:
        stderr.write("self-update: --principal must name the signer to trust\n")
        return EXIT_USAGE
    if channel not in CHANNELS:
        stderr.write(f"self-update: --channel is one of {', '.join(CHANNELS)}, not {channel!r}\n")
        return EXIT_USAGE
    return None


def anchor(
    *, checkout: str, branch: str, allowed_signers: str, principal: str, os_release: str = "/etc/os-release",
    allow_untracked: tuple[str, ...] = (), channel: str = CHANNEL_TAGS, remote: str = "origin",
    allow_rewind: bool = False, stdout: TextIO, stderr: TextIO, env: Mapping[str, str] | None = None,
) -> int:
    """Make the checkout's HEAD a trusted commit.

    The `branch` channel moves HEAD back to the newest signed commit if it is not one. The
    `tags` channel first fetches the branch and the release tags (so it judges the real newest
    release, withdrawn ones pruned), then lands HEAD on the newest valid release's commit. It
    moves forward freely but refuses to move BACK, which would discard commits the release does
    not hold, unless `allow_rewind` says the clone is fresh (the play passes it only then).

    `allow_untracked` names, exactly, the files the caller itself puts in the checkout (the
    play's host_vars copy). Any other file the commit does not track refuses the anchor.
    """
    usage = _usage_error(principal, channel, stderr)
    if usage is not None:
        return usage

    def act() -> int:
        _check_signers_file(allowed_signers)
        git = _Git(checkout, allowed_signers, env if env is not None else os.environ)
        return _anchor(git=git, remote=remote, branch=branch, channel=channel, principal=principal,
                       os_release=os_release, allow_untracked=allow_untracked, allow_rewind=allow_rewind,
                       stdout=stdout, stderr=stderr)

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
    channel: str = CHANNEL_TAGS,
    stdout: TextIO,
    stderr: TextIO,
    env: Mapping[str, str] | None = None,
) -> int:
    """Update the checkout or refuse; the return value is the exit status."""
    usage = _usage_error(principal, channel, stderr)
    if usage is not None:
        return usage

    def act() -> int:
        _check_signers_file(allowed_signers)
        git = _Git(checkout, allowed_signers, env if env is not None else os.environ)
        step = _update_tags if channel == CHANNEL_TAGS else _update
        return step(
            git=git, remote=remote, branch=branch, principal=principal, os_release=os_release,
            dry_run=dry_run, stdout=stdout, stderr=stderr,
        )

    return _guarded(stderr, act, stdout)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Fast-forward the deploy clone to the newest owner-signed commit.")
    parser.add_argument("--checkout", required=True)
    parser.add_argument("--remote")
    parser.add_argument("--branch", required=True)
    parser.add_argument("--allowed-signers", required=True)
    parser.add_argument("--principal", required=True)
    parser.add_argument("--os-release", default="/etc/os-release")
    parser.add_argument("--channel", choices=CHANNELS, default=CHANNEL_TAGS,
                        help="tags (default): the newest signed release tag; branch: the newest signed commit")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--dry-run", action="store_true")
    mode.add_argument("--anchor", action="store_true", help="make HEAD a trusted commit (the play, after cloning)")
    parser.add_argument("--allow-untracked", action="append", default=[], metavar="PATH",
                        help="with --anchor: an untracked file the caller put there itself (repeatable)")
    parser.add_argument("--allow-rewind", action="store_true",
                        help="with --anchor on the tags channel: the clone is fresh, so moving it back loses nothing")
    args = parser.parse_args(argv)
    if args.allow_untracked and not args.anchor:
        parser.error("--allow-untracked only applies with --anchor")
    if args.allow_rewind and not args.anchor:
        parser.error("--allow-rewind only applies with --anchor")
    if args.anchor:
        return anchor(
            checkout=args.checkout, branch=args.branch, allowed_signers=args.allowed_signers,
            principal=args.principal, os_release=args.os_release, allow_untracked=tuple(args.allow_untracked),
            channel=args.channel, remote=args.remote or "origin", allow_rewind=args.allow_rewind,
            stdout=sys.stdout, stderr=sys.stderr,
        )
    if not args.remote:
        parser.error("--remote is required to update")
    return run(
        checkout=args.checkout, remote=args.remote, branch=args.branch,
        allowed_signers=args.allowed_signers, principal=args.principal, os_release=args.os_release,
        dry_run=args.dry_run, channel=args.channel, stdout=sys.stdout, stderr=sys.stderr,
    )


if __name__ == "__main__":
    sys.exit(main())
