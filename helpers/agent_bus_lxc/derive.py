"""The `<repo>`, `<host>` and `<seat>` of an LXC member, derived from the repository, as ccy
derives them for a podman seat (docs/agent-bus.md, Plan 00161 DESIGN.md section 5.2, D49).

A member's handle is `<repo>.<seat>+<host>.lxc`:

  * `<repo>` is the last component of the checkout's `origin` (`.git` removed), else the
    directory's name: `checkout.repo_from_origin`, which `agent-bus seat take` and
    `pingbus suggest-handle` apply too;
  * `<host>` is the `HOOKS_DAEMON_HOSTNAME` the checkout's `.claude/ccy/ccy.env.local`
    assigns, else the literal `local`: `checkout.host_from_env_local`, the rule behind
    `checkout.checkout_host`; the machine's hostname is never used;
  * `<seat>` is the one a member already holds in this team for this `<repo>+<host>.lxc`
    (its bundle's, so a run again changes nothing), else the one a member of that name
    already holds (a container joined before seats were derived), else what ccy offers for
    a new seat (`checkout.seat_choices`): the lowest number that no active member of the
    same `<repo>+<host>.lxc` holds. A parked member's number is offered again; it returns. An
    active numbered member that no entry holds is refused, not skipped (a lost bundle).

The checkout is read from the host's side, as root, through the container's LXC config:
its rootfs, and the bind mounts the config declares (a checkout on a desktop path shows up
there). The container controls what is read, so `ROOT_READ_PY` never runs git or anything
else that executes repository config (`core.fsmonitor`, hooks): `.git/config` and
`ccy.env.local` are read as plain files, each path component opened without following a
link, a link anywhere refused, every size capped; and this module parses what it printed
strictly, letting no control character reach a terminal. The same script and the same
judgements serve the play (members.py, which is root) and `ccy-teams lxc-add` (through
`sudo -n python3 -I -c`, the script from `declare lxc-script`), so the rule exists once.
"""

from __future__ import annotations

import dataclasses
import json
import re
from collections.abc import Collection, Mapping

from helpers.agent_bus import checkout, registry
from helpers.pingbus import protocol

MEMBER_TYPE = "lxc"
#: A file read from a container is at most this large; `ccy.env.local` and `.git/config` are
#: a few lines.
FILE_MAX_BYTES = 65536
#: How many checkouts the suggestion lists, and how deep below the home it looks.
CHECKOUTS_MAX = 30
CHECKOUTS_DEPTH = 4
PATH_MAX = 4096
ORIGIN_MAX = 2048
#: The command that names the directory LXC keeps its containers in.
LXC_PATH_COMMAND = ("lxc-config", "lxc.lxcpath")


class DeriveError(Exception):
    """A handle part cannot be derived: the checkout, or what it holds, is unusable."""


# ── what runs on the host, as root: `python3 -I -c SCRIPT MODE LXCPATH CONTAINER USER PATH` ─

_ROOT_READ_TEMPLATE = r'''
import json, os, re, stat, sys
mode, lxcpath, container, user, path = sys.argv[1:6]
CAP = @CAP@
if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,63}", container) or not re.fullmatch(r"[a-z_][a-z0-9_-]{0,31}", user):
    die("the container or user name is not one the play accepts")
FLAGS = os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC

def die(message):
    # A name from the container's tree can be anything: no control character reaches a terminal.
    sys.exit("".join(ch if ch.isprintable() else "?" for ch in message))

def slurp(file_path):
    # A file of root's own (the LXC config): plain, regular, bounded.
    fd = os.open(file_path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as handle:
        if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
            die(file_path + " is not a regular file")
        data = handle.read(CAP * 16 + 1)
    if len(data) > CAP * 16:
        die(file_path + " is too large")
    return data.decode("utf-8")

def child(fd, name, shown):
    # The entry NAME of the directory FD, as an O_PATH descriptor; None when missing. A link is refused.
    try:
        found = os.open(name, FLAGS, dir_fd=fd)
    except FileNotFoundError:
        return None
    except OSError as exc:
        die(shown + " cannot be opened: " + exc.strerror)
    if stat.S_ISLNK(os.fstat(found).st_mode):
        die(shown + " is a symbolic link; the container's tree is not followed through one")
    return found

def walk(fd, parts, shown):
    for index, part in enumerate(parts):
        here = shown + "/" + "/".join(parts[: index + 1])
        found = child(fd, part, here)
        if fd is not None:
            os.close(fd)
        if found is None:
            return None
        fd = found
        if index < len(parts) - 1 and not stat.S_ISDIR(os.fstat(fd).st_mode):
            die(here + " is not a directory")
    return fd

def read_regular(fd, shown):
    info = os.fstat(fd)
    if not stat.S_ISREG(info.st_mode):
        die(shown + " is not a regular file")
    if info.st_size > CAP:
        die(shown + " is larger than " + str(CAP) + " bytes")
    # Reopened from the checked descriptor: a device or pipe was never opened, only a regular file.
    with os.fdopen(os.open("/proc/self/fd/" + str(fd), os.O_RDONLY | os.O_CLOEXEC), "rb") as handle:
        data = handle.read(CAP + 1)
    if len(data) > CAP:
        die(shown + " is larger than " + str(CAP) + " bytes")
    try:
        return data.decode("utf-8"), info.st_uid
    except UnicodeDecodeError:
        die(shown + " is not UTF-8 text")

rootfs, mounts = None, []
for raw in slurp(os.path.join(lxcpath, container, "config")).splitlines():
    line = raw.split("#", 1)[0].strip()
    key, _, value = line.partition("=")
    key, value = key.strip(), value.strip()
    if key == "lxc.idmap":
        die("container " + container + " is unprivileged (lxc.idmap); only rootful containers are read")
    if key in ("lxc.rootfs.path", "lxc.rootfs"):
        rootfs = value[4:] if value.startswith("dir:") else value
        if not rootfs.startswith("/"):
            die("container " + container + " has a rootfs that is not a directory (" + value + ")")
    elif key == "lxc.mount.entry":
        fields = value.split()
        options = fields[3].split(",") if len(fields) >= 4 else []
        if fields[2:3] == ["none"] and any(o in ("bind", "rbind") for o in options) and fields[0].startswith("/"):
            if not os.path.isdir(fields[0]):
                # LXC mounts nothing for an optional bind whose source is absent; any other is an error.
                if "optional" not in options:
                    die("the bind mount source " + fields[0] + " does not exist or is not a directory")
                continue
            mounts.append((fields[0], [p for p in fields[1].split("/") if p]))
if rootfs is None:
    die("container " + container + " has no lxc.rootfs.path")

def root_fd(base):
    try:
        return os.open(base, os.O_PATH | os.O_DIRECTORY | os.O_CLOEXEC)
    except OSError as exc:
        die(base + " cannot be opened: " + exc.strerror)

def locate(parts):
    # The longest bind mount whose destination is a prefix of the path, else the rootfs.
    best = None
    for src, dest in mounts:
        if parts[: len(dest)] == dest and (best is None or len(dest) > len(best[1])):
            best = (src, dest)
    if best is None:
        return rootfs, parts
    return best[0], parts[len(best[1]):]

def split(container_path):
    if not container_path.startswith("/"):
        die(container_path + " is not an absolute path")
    parts = [p for p in container_path.split("/") if p]
    if not parts or any(p in (".", "..") for p in parts):
        die(container_path + " is not a clean path")
    return parts

passwd = walk(root_fd(rootfs), ["etc", "passwd"], "etc/passwd")
if passwd is None:
    die("container " + container + " has no /etc/passwd")
uid = home = None
for entry in read_regular(passwd, "/etc/passwd")[0].splitlines():
    fields = entry.split(":")
    if len(fields) == 7 and fields[0] == user and fields[2].isdigit():
        uid, home = int(fields[2]), fields[5]
if uid is None:
    die("container " + container + " has no user " + user)

def git_config(parts, shown):
    # The checkout's .git/config as a plain file; None when it is not a checkout.
    base, rest = locate(parts)
    top = walk(root_fd(base), rest, shown) if rest else root_fd(base)
    if top is None:
        die(shown + " does not exist in container " + container)
    dot = child(top, ".git", shown + "/.git")
    if dot is None:
        return top, None
    if not stat.S_ISDIR(os.fstat(dot).st_mode):
        die(shown + "/.git is not a directory (a worktree or submodule is not followed)")
    config = child(dot, "config", shown + "/.git/config")
    if config is None:
        return top, None
    return top, read_regular(config, shown + "/.git/config")[0]

if mode == "probe":
    parts = split(path)
    top, text = git_config(parts, path)
    if text is None:
        die(path + " is not a git checkout: it has no .git/config; give the checkout's top directory")
    env_text = env_uid = None
    ccy = child(top, ".claude", path + "/.claude")
    ccy = None if ccy is None else child(ccy, "ccy", path + "/.claude/ccy")
    env = None if ccy is None else child(ccy, "ccy.env.local", path + "/.claude/ccy/ccy.env.local")
    if env is not None:
        env_text, env_uid = read_regular(env, path + "/.claude/ccy/ccy.env.local")
    print(json.dumps({"path": path, "uid": uid, "git_config": text, "env_uid": env_uid, "env_local": env_text}))
elif mode == "checkouts":
    found = []
    def scan(fd, shown, depth):
        with os.scandir(os.open("/proc/self/fd/" + str(fd), os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)) as entries:
            names = sorted(e.name for e in entries if e.is_dir(follow_symlinks=False) and not e.name.startswith("."))
        dot = child(fd, ".git", shown + "/.git")
        if dot is not None and stat.S_ISDIR(os.fstat(dot).st_mode):
            config = child(dot, "config", shown + "/.git/config")
            found.append({"path": shown, "git_config": None if config is None else read_regular(config, shown + "/.git/config")[0]})
            return
        if depth <= 0:
            return
        for name in names:
            if len(found) >= @MAX@:
                return
            sub = child(fd, name, shown + "/" + name)
            if sub is not None:
                scan(sub, shown + "/" + name, depth - 1)
    base, rest = locate(split(home))
    start = walk(root_fd(base), rest, home) if rest else root_fd(base)
    if start is not None:
        scan(start, home.rstrip("/"), @DEPTH@)
    print(json.dumps(found))
else:
    die("unknown mode " + mode)
'''

ROOT_READ_PY = (_ROOT_READ_TEMPLATE.replace("@CAP@", str(FILE_MAX_BYTES))
                .replace("@MAX@", str(CHECKOUTS_MAX)).replace("@DEPTH@", str(CHECKOUTS_DEPTH)))
SCRIPTS = {"root-read": ROOT_READ_PY}


# ── judging what the container's files held ──────────────────────────────────────────────

# Git reads a section name without regard to case ([Remote "origin"] is the remote); the
# subsection, "origin", is case-sensitive.
_ORIGIN_SECTION_RE = re.compile(r'\[(?i:remote)[ \t]+"origin"\][ \t]*(?:[#;].*)?')
_REWRITE_SECTION_RE = re.compile(r'\[(?i:url)[ \t]+"[^"]*"\][ \t]*(?:[#;].*)?')
_SECTION_RE = re.compile(r"\[.*")
_REWRITE_KEY_RE = re.compile(r"[ \t]*insteadof[ \t]*(?:=.*)?", re.IGNORECASE)
# A value git reads whole: bare, or entirely inside one pair of quotes with no escape.
_URL_RE = re.compile(r"[ \t]*url[ \t]*=[ \t]*(?:\"(?P<quoted>[^\\\"\n]+)\"|(?P<bare>[^\\\"\n]+?))[ \t]*(?:[#;].*)?",
                     re.IGNORECASE)


def _plain(value: object, limit: int, what: str) -> str:
    """Text a person can be shown: printable, bounded."""
    if not isinstance(value, str) or len(value) > limit or not value.isprintable():
        raise DeriveError(f"the container's files gave {what} that is not plain text of at most {limit} characters")
    return value


def _path(value: object, what: str) -> str:
    text = _plain(value, PATH_MAX, what)
    if not text.startswith("/"):
        raise DeriveError(f"the container's files gave {what} that is not an absolute path")
    return text


def origin_from_git_config(text: str) -> str | None:
    """The first `url` of `[remote "origin"]` in a `.git/config`, read as text and never run
    through git; None when there is none. Git would rewrite that url by a `url.<base>.insteadOf`
    and this does not apply the rewrite, so a config that has one is refused rather than read
    wrongly (`<repo>` is the last component, which a rewrite can change). A value with a quote
    that does not enclose it all, an escape or a continuation is refused too."""
    section = ""
    origin = None
    rewrites = False
    for number, line in enumerate(text.splitlines(), start=1):
        stripped = line.strip()
        if _SECTION_RE.fullmatch(stripped):
            section = ("origin" if _ORIGIN_SECTION_RE.fullmatch(stripped)
                       else "rewrite" if _REWRITE_SECTION_RE.fullmatch(stripped) else "")
            continue
        if section == "rewrite" and _REWRITE_KEY_RE.fullmatch(line):
            rewrites = True
        if section != "origin" or origin is not None:
            continue
        match = _URL_RE.fullmatch(line)
        if match is not None:
            value = match.group("quoted") if match.group("quoted") is not None else match.group("bare")
            origin = _plain(value, ORIGIN_MAX, f".git/config line {number}'s url")
        elif re.match(r"[ \t]*url[ \t]*=", line, re.IGNORECASE):
            raise DeriveError(f".git/config line {number} gives the origin url in a form this does not read "
                              "(a quote inside the value, an escape or a continuation)")
    if origin is not None and rewrites:
        raise DeriveError(".git/config rewrites urls (url.<base>.insteadOf), which this does not apply, so the "
                          "repository's name could be read wrongly: remove the rewrite from the checkout's "
                          "config, or declare the entry's repo, host and seat")
    return origin


@dataclasses.dataclass(frozen=True)
class Probe:
    """What `ROOT_READ_PY probe` read: the checkout's path in the container, its `origin`
    (None: none), and the text of its `ccy.env.local` (None: no such file)."""

    top: str
    origin: str | None
    env_local: str | None


def parse_probe(text: str) -> Probe:
    """The probe's JSON, judged: the keys, the sizes, the printable text, and the owner of
    `ccy.env.local`, which must be the user (as `checkout.checkout_host` requires)."""
    try:
        data = json.loads(text)
    except ValueError:
        raise DeriveError("the checkout probe printed something that is not JSON") from None
    keys = {"path", "uid", "git_config", "env_uid", "env_local"}
    if not isinstance(data, dict) or set(data) != keys:
        raise DeriveError("the checkout probe printed something other than the keys it prints")
    git_config, env_local = data["git_config"], data["env_local"]
    for what, value in (("a .git/config", git_config), ("a ccy.env.local", env_local)):
        if value is not None and (not isinstance(value, str) or len(value) > FILE_MAX_BYTES):
            raise DeriveError(f"the checkout probe printed {what} that is not text of at most {FILE_MAX_BYTES} bytes")
    if not isinstance(git_config, str):
        raise DeriveError("the checkout probe printed no .git/config")
    uid, env_uid = data["uid"], data["env_uid"]
    if type(uid) is not int or (env_local is None) != (env_uid is None) or (env_uid is not None and type(env_uid) is not int):
        raise DeriveError("the checkout probe printed an unreadable owner")
    if env_uid is not None and env_uid != uid:
        raise DeriveError(f".claude/ccy/ccy.env.local is owned by uid {env_uid}, not {uid}")
    return Probe(_path(data["path"], "a checkout path"), origin_from_git_config(git_config), env_local)


def parse_checkouts(text: str) -> list[tuple[str, str | None]]:
    """The (path, origin) pairs `ROOT_READ_PY checkouts` printed."""
    try:
        data = json.loads(text)
    except ValueError:
        raise DeriveError("the checkout search printed something that is not JSON") from None
    if not isinstance(data, list) or len(data) > CHECKOUTS_MAX:
        raise DeriveError(f"the checkout search printed something other than a list of at most {CHECKOUTS_MAX}")
    found = []
    for item in data:
        if not isinstance(item, dict) or set(item) != {"path", "git_config"}:
            raise DeriveError("the checkout search printed an item other than a path and a .git/config")
        config = item["git_config"]
        if config is not None and (not isinstance(config, str) or len(config) > FILE_MAX_BYTES):
            raise DeriveError("the checkout search printed a .git/config that is not bounded text")
        found.append((_path(item["path"], "a checkout path"),
                      None if config is None else origin_from_git_config(config)))
    return sorted(found)


# ── the rule ─────────────────────────────────────────────────────────────────────────────


@dataclasses.dataclass(frozen=True)
class Identity:
    """The `<repo>` and `<host>` of a checkout's handles."""

    repo: str
    host: str

    def handle(self, seat: str | int) -> str:
        return registry.build_handle(self.repo, seat, self.host, MEMBER_TYPE)


def identity_of(probe: Probe) -> Identity:
    """The repo and host ccy would give a seat made in this checkout."""
    try:
        return Identity(checkout.repo_from_origin(probe.origin, probe.top),
                        checkout.host_from_env_local(probe.env_local))
    except checkout.CheckoutError as exc:
        raise DeriveError(str(exc)) from None


def default_seat(container: str) -> str | None:
    """The container's name as a seat (lower case, letters and digits, at most 12, a letter
    first): how a member was seated before seats were derived, so one joined so keeps it."""
    seat = re.sub(r"[^a-z0-9]", "", container.lower())[:12]
    return seat if seat[:1].isalpha() and protocol.is_seat_name(seat) else None


def derive_seat(identity: Identity, container: str, roster: Mapping[str, bool], bundle: str | None,
                taken: Collection[str], *, unheld_ok: bool = False) -> str:
    """The seat for the member of `container`. `roster` is the team's members (handle ->
    parked); `bundle` the handle the container's bundle for the team names, if any; `taken`
    the seats other entries of the team hold for this identity (explicit, or by their bundle).

    A new number is never chosen past an active numbered member that no entry holds: it may
    be this container's own, whose bundle is gone, and a new handle would leave it live with a
    token nobody has. It is refused, as the explicit-seat path refuses it. `unheld_ok` is for a
    caller that cannot see bundles (the preview), which skips such a number."""
    held = protocol.parse_handle(bundle) if bundle else None
    if held is not None and (held.repo, held.host, held.type) == (identity.repo, identity.host, MEMBER_TYPE):
        return held.seat
    named = default_seat(container)
    if named is not None and named not in taken and identity.handle(named) in roster:
        return named
    if not unheld_ok:
        for number in range(1, registry.N_MAX + 1):
            if str(number) not in taken and roster.get(identity.handle(number)) is False:
                raise DeriveError(
                    f"{identity.handle(number)} is an active member and no entry holds it: it may be this "
                    "container's, whose bundle is gone, or another's joined by hand, so a new seat is not "
                    "chosen silently. To take it back, set the entry's state: absent and run the play (which "
                    "parks it), then declare it present again; to leave it alone, set `seat` explicitly on "
                    "the entry to another number")
    for number in range(1, registry.N_MAX + 1):
        if str(number) not in taken and roster.get(identity.handle(number)) is not False:
            return str(number)
    raise DeriveError(f"every seat of {identity.repo}+{identity.host}.{MEMBER_TYPE} is taken")
