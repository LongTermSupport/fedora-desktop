"""The forge check and provenance (protocol spec §6): does a reference resolve, on trusted history?

Spec: docs/agent-bus-protocol.md §6 (the check, the cache, the refusals, the credential),
§9 (send step 6, receive step 10p), §10 (the 429 retry) and §14 (exit codes 5 and 9).
Plan 00161's DESIGN.md D20 places it: on send and before the inbox write, never in a hook.

A reference reaching this module has already passed `protocol` (grammar and allowlists);
this module asks the forge (GitHub's REST API at the team record's `forge_api`) whether it
names real content on a trusted branch. Every HTTP exchange goes through an opener with
`open(request, timeout=)`, injected in tests; the default one refuses a redirect to another
origin and the token travels only as an unredirected `Authorization` header.

Failures are `ForgeError`s with one of `REFUSALS`; a sender prints the code (exit 5, or 9
for `forge-rate`) and a receiver drops the ping as `drop_reason`. Messages name the
reference and the HTTP status only: never the token, and never text the forge returned.
A bad credential source or cache file is a `config.ConfigError` (exit 78).
"""

from __future__ import annotations

import dataclasses
import io
import json
import os
import pathlib
import re
import stat
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Callable, Mapping, Sequence

from helpers.pingbus import config, limits, protocol

REFUSALS = ("not-found", "wrong-kind", "provenance", "forge-unreachable", "forge-auth", "forge-rate")
RATE_REFUSAL = "forge-rate"
EXIT_UNRESOLVED = 5
EXIT_RATE = 9

GITHUB_API = "https://api.github.com"
API_HEADERS = {
    "Accept": "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
    "User-Agent": "pingbus",
}
TIMEOUT_S = 15
MAX_RESPONSE_BYTES = 8 * 1024 * 1024
#: A forge asking for a longer wait than this is reported as `forge-rate` at once rather
#: than holding a `send` (or a syncer batch) for the length of its rate window.
RATE_WAIT_MAX_S = 60

#: `compare/BASE...SHA` statuses that put SHA in BASE's history.
REACHED = ("identical", "behind")
TRUSTED_ASSOCIATIONS = ("OWNER", "MEMBER", "COLLABORATOR")

CACHE_FILE = "forge-cache.json"
CACHE_VERSION = 1
CACHE_MAX_BYTES = 16 * 1024 * 1024
#: Reachability and pull-request results (§6).
REACH_TTL_S = 3600
#: Content at a SHA never changes; the expiry only bounds the file's growth.
CONTENT_TTL_S = 30 * 86400

CREDENTIAL_SOURCES = ("PINGBUS_FORGE_TOKEN_FILE", "PINGBUS_FORGE_TOKEN", "GH_TOKEN", "GITHUB_TOKEN")
#: Read only when `forge_api` is exactly GITHUB_API: a GitHub token is never sent elsewhere.
GITHUB_ONLY_SOURCES = ("GH_TOKEN", "GITHUB_TOKEN")
NO_CREDENTIAL = "none"
_TOKEN_RE = re.compile(r"[\x21-\x7e]+")

_FORGE_API_RE = re.compile(protocol.FORGE_API_PATTERN)


class ForgeError(Exception):
    """A reference that did not pass the forge check; `code` is one of `REFUSALS`."""

    def __init__(self, code: str, detail: str) -> None:
        if code not in REFUSALS:
            raise ValueError(f"not a forge refusal code: {code!r}")
        super().__init__(f"{code}: {detail}")
        self.code = code

    @property
    def exit_code(self) -> int:
        return EXIT_RATE if self.code == RATE_REFUSAL else EXIT_UNRESOLVED

    @property
    def drop_reason(self) -> str:
        """The receive-side drop code (§9 step 10p)."""
        return "provenance" if self.code == "provenance" else "unresolved"


@dataclasses.dataclass(frozen=True)
class Credential:
    """Where the forge token came from (`config check` prints `source`) and the token."""

    source: str
    token: str | None = dataclasses.field(default=None, repr=False)


def resolve_credential(
    environ: Mapping[str, str], forge_api: str, *, uid: int | None = None
) -> Credential:
    """The first source set (non-empty) in `CREDENTIAL_SOURCES` order; the GitHub ones only
    for exactly `GITHUB_API`. None set: no token (public repositories only)."""
    for source in CREDENTIAL_SOURCES:
        value = environ.get(source, "")
        if not value or (source in GITHUB_ONLY_SOURCES and forge_api != GITHUB_API):
            continue
        if source == "PINGBUS_FORGE_TOKEN_FILE":
            return Credential(source, _read_token_file(value, os.getuid() if uid is None else uid))
        if _TOKEN_RE.fullmatch(value) is None or len(value) > config.TOKEN_MAX_BYTES:
            raise config.ConfigError(f"{source}: the forge token must be one line of printable ASCII")
        return Credential(source, value)
    return Credential(NO_CREDENTIAL)


def _read_token_file(value: str, uid: int) -> str:
    where = f"PINGBUS_FORGE_TOKEN_FILE={value}"
    if not os.path.isabs(value):
        raise config.ConfigError(f"{where}: must be an absolute path")
    try:
        fd = os.open(value, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    except FileNotFoundError:
        raise config.ConfigError(f"{where}: the file is missing") from None
    except OSError as exc:
        raise config.ConfigError(f"{where}: not a regular file ({exc.strerror})") from None
    with _open_regular(fd, where) as handle:
        info = os.fstat(handle.fileno())
        if info.st_uid != uid:
            raise config.ConfigError(f"{where}: owned by uid {info.st_uid}, not {uid}")
        mode = stat.S_IMODE(info.st_mode)
        if mode & ~config.TOKEN_MODE_ALLOWED:
            raise config.ConfigError(f"{where}: mode {mode:04o} is looser than 0600")
        raw = handle.read(config.TOKEN_MAX_BYTES + 1)
    text = raw.decode("ascii", errors="replace")
    if len(raw) > config.TOKEN_MAX_BYTES or _TOKEN_RE.fullmatch(text) is None:
        raise config.ConfigError(
            f"{where}: the token must be one line of printable ASCII, no spaces, no newline"
        )
    return text


def _open_regular(fd: int, where: str) -> io.BufferedReader:
    """`fd` as a binary file, refused (and closed) unless it is a regular file."""
    if not stat.S_ISREG(os.fstat(fd).st_mode):
        os.close(fd)
        raise config.ConfigError(f"{where}: not a regular file")
    return os.fdopen(fd, "rb")


def _origin(url: str) -> tuple[str, str, int | None]:
    parts = urllib.parse.urlsplit(url)
    port = parts.port or {"https": 443, "http": 80}.get(parts.scheme)
    return parts.scheme, (parts.hostname or "").lower(), port


class SameOriginRedirect(urllib.request.HTTPRedirectHandler):
    """Follows a redirect only within the forge API's origin (§6). urllib does not copy an
    unredirected header, so a followed redirect never carries the token."""

    def __init__(self, api: str) -> None:
        super().__init__()
        self._origin = _origin(api)

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        if _origin(newurl) != self._origin:
            raise urllib.error.HTTPError(newurl, code, "redirect to another origin refused", headers, fp)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def default_opener(api: str) -> urllib.request.OpenerDirector:
    return urllib.request.build_opener(SameOriginRedirect(api))


class ForgeCache:
    """`forge-cache.json`: positive results only, each key with an expiry (Unix seconds).

    Keys carry the forge API, so a team record naming another forge shares nothing. A
    file that is not exactly this shape is refused, not ignored."""

    def __init__(self, path: pathlib.Path, entries: dict[str, int]) -> None:
        self.path = path
        self._entries = entries

    @classmethod
    def load(cls, path: pathlib.Path) -> ForgeCache:
        try:
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        except FileNotFoundError:
            return cls(path, {})
        except OSError as exc:
            raise config.ConfigError(f"{path}: not a regular file ({exc.strerror})") from None
        with _open_regular(fd, str(path)) as handle:
            raw = handle.read(CACHE_MAX_BYTES + 1)
        if len(raw) > CACHE_MAX_BYTES:
            raise config.ConfigError(f"{path}: larger than {CACHE_MAX_BYTES} bytes")
        try:
            data = json.loads(raw.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError):
            raise config.ConfigError(f"{path}: not JSON; remove it to start an empty cache") from None
        entries = data.get("entries") if isinstance(data, dict) else None
        if (
            not isinstance(data, dict)
            or set(data) != {"v", "entries"}
            or type(data["v"]) is not int
            or data["v"] != CACHE_VERSION
            or not isinstance(entries, dict)
            or not all(type(v) is int and v >= 0 for v in entries.values())
        ):
            raise config.ConfigError(f"{path}: not a forge cache; remove it to start an empty cache")
        return cls(path, dict(entries))

    def fresh(self, key: str, now: float) -> bool:
        expiry = self._entries.get(key)
        return expiry is not None and now < expiry

    def put(self, key: str, now: float, ttl_s: int) -> None:
        self._entries[key] = int(now) + ttl_s
        self._entries = {k: v for k, v in self._entries.items() if now < v}
        self._save()

    def _save(self) -> None:
        payload = json.dumps({"v": CACHE_VERSION, "entries": self._entries}, sort_keys=True)
        fd, tmp = tempfile.mkstemp(prefix=f".{CACHE_FILE}.", dir=self.path.parent)
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as handle:
                handle.write(payload)
                handle.flush()
                os.fsync(handle.fileno())
            os.replace(tmp, self.path)
        except BaseException:
            os.unlink(tmp)
            raise


class Forge:
    """The forge check for one `forge_api`. `check` returns None or raises `ForgeError`."""

    def __init__(
        self,
        api: str,
        *,
        token: str | None = None,
        opener: object | None = None,
        sleep: Callable[[float], object] = time.sleep,
        clock: Callable[[], float] = time.time,
        cache: ForgeCache | None = None,
    ) -> None:
        if not isinstance(api, str) or _FORGE_API_RE.fullmatch(api) is None:
            raise ValueError("forge_api must be an https:// URL (spec §8)")
        self.api = api
        self._base = api.rstrip("/")
        self._token = token
        self._opener = opener if opener is not None else default_opener(api)
        self._sleep = sleep
        self._clock = clock
        self._cache = cache

    @classmethod
    def for_record(cls, record: protocol.TeamRecord, credential: Credential, **kwargs: object) -> Forge:
        return cls(record.forge_api, token=credential.token, **kwargs)

    def check(self, ref: protocol.Ref, branches: Sequence[str]) -> None:
        """Resolve `ref` (already valid and allowlisted) against its repository's trusted
        `branches` (the team record's, §11)."""
        if not branches or not all(protocol.is_branch_name(b) for b in branches):
            raise ValueError("a reference is checked against 1 or more valid trusted branches")
        if ref.form == "path":
            self._reach(ref, branches)
            self._file(ref)
        elif ref.form == "commit":
            self._reach(ref, branches)
        elif ref.form == "pr":
            self._pull(ref)
        elif ref.form == "issue":
            self._issue(ref)
        else:
            raise ValueError(f"unknown reference form: {ref.form!r}")

    def _key(self, *parts: object) -> str:
        return "|".join((self.api, *(str(p) for p in parts)))

    def _cached(self, key: str) -> bool:
        return self._cache is not None and self._cache.fresh(key, self._clock())

    def _remember(self, key: str, ttl_s: int) -> None:
        if self._cache is not None:
            self._cache.put(key, self._clock(), ttl_s)

    def _compare(self, ref: protocol.Ref, base: str) -> tuple[int, object]:
        branch = urllib.parse.quote(base, safe="/")
        return self._get(
            f"/repos/{ref.repo_full}/compare/{branch}...{ref.sha}?per_page=1", allow=(404, 422)
        )

    def _reach(self, ref: protocol.Ref, branches: Sequence[str]) -> None:
        keys = {b: self._key("reach", ref.repo_full, ref.sha, b) for b in branches}
        if any(self._cached(key) for key in keys.values()):
            return
        found = False
        for branch in branches:
            code, data = self._compare(ref, branch)
            if code == 404:
                continue
            found = True
            if code == 200 and isinstance(data, dict) and data.get("status") in REACHED:
                self._remember(keys[branch], REACH_TTL_S)
                return
        if found:
            raise ForgeError("provenance", f"{ref.text}: not in the history of a trusted branch")
        raise ForgeError("not-found", f"{ref.text}: the commit is not in {ref.repo_full}")

    def _file(self, ref: protocol.Ref) -> None:
        key = self._key("file", ref.repo_full, ref.sha, ref.path)
        if self._cached(key):
            return
        path = urllib.parse.quote(ref.path, safe="/")
        code, data = self._get(f"/repos/{ref.repo_full}/contents/{path}?ref={ref.sha}", allow=(404,))
        if code == 404:
            raise ForgeError("not-found", f"{ref.text}: no such path at that commit")
        if not isinstance(data, dict) or data.get("type") != "file":
            raise ForgeError("wrong-kind", f"{ref.text}: not a file")
        self._remember(key, CONTENT_TTL_S)

    def _pull(self, ref: protocol.Ref) -> None:
        key = self._key("pr", ref.repo_full, ref.num, ref.sha)
        if self._cached(key):
            return
        code, data = self._get(f"/repos/{ref.repo_full}/pulls/{ref.num}", allow=(404, 410))
        if code != 200:
            raise ForgeError("not-found", f"{ref.text}: no such pull request")
        head = data.get("head") if isinstance(data, dict) else None
        head_repo = head.get("repo") if isinstance(head, dict) else None
        full_name = head_repo.get("full_name") if isinstance(head_repo, dict) else None
        if not isinstance(full_name, str) or full_name.lower() != ref.repo_full:
            raise ForgeError("provenance", f"{ref.text}: the head is not in {ref.repo_full}")
        if data.get("author_association") not in TRUSTED_ASSOCIATIONS:
            raise ForgeError("provenance", f"{ref.text}: the author is not an owner, member or collaborator")
        head_ref = head.get("ref")
        if not protocol.is_branch_name(head_ref):
            raise ForgeError("provenance", f"{ref.text}: the head branch name is not a valid branch")
        code, data = self._compare(ref, head_ref)
        if code == 404:
            raise ForgeError("not-found", f"{ref.text}: the commit or head branch is not in {ref.repo_full}")
        if code != 200 or not isinstance(data, dict) or data.get("status") not in REACHED:
            raise ForgeError("provenance", f"{ref.text}: the commit is not at the pull request's head")
        self._remember(key, REACH_TTL_S)

    def _issue(self, ref: protocol.Ref) -> None:
        code, data = self._get(f"/repos/{ref.repo_full}/issues/{ref.num}", allow=(404, 410))
        if code != 200:
            raise ForgeError("not-found", f"{ref.text}: no such issue")
        if not isinstance(data, dict) or "pull_request" in data:
            raise ForgeError("wrong-kind", f"{ref.text}: not an issue")

    def _request(self, path: str) -> urllib.request.Request:
        request = urllib.request.Request(self._base + path, method="GET")
        for name, value in API_HEADERS.items():
            request.add_header(name, value)
        if self._token is not None:
            request.add_unredirected_header("Authorization", f"Bearer {self._token}")
        return request

    def _get(self, path: str, *, allow: tuple[int, ...]) -> tuple[int, object]:
        """`(200, parsed JSON)`, or `(code, None)` for a code in `allow`; anything else
        raises. A rate limit is retried per §10 (at most `SERVER_429_MAX_TRIES` tries)."""
        where = f"GET {path.partition('?')[0]}"
        for attempt in range(1, limits.SERVER_429_MAX_TRIES + 1):
            try:
                response = self._opener.open(self._request(path), timeout=TIMEOUT_S)
            except urllib.error.HTTPError as exc:
                code, hdrs = exc.code, exc.headers
                rate_limited = _is_rate_limited(code, hdrs)
                body = _bounded_read(exc) if rate_limited else b""
                exc.close()
                if rate_limited:
                    wait = _retry_wait(hdrs, body)
                    if attempt == limits.SERVER_429_MAX_TRIES or wait > RATE_WAIT_MAX_S:
                        raise ForgeError("forge-rate", f"{where}: rate limited by the forge") from None
                    self._sleep(wait)
                    continue
                if code in allow:
                    return code, None
                if code in (401, 403):
                    raise ForgeError("forge-auth", f"{where}: HTTP {code}") from None
                raise ForgeError("forge-unreachable", f"{where}: HTTP {code}") from None
            except (urllib.error.URLError, OSError) as exc:
                raise ForgeError("forge-unreachable", f"{where}: {type(exc).__name__}") from None
            with response:
                code = getattr(response, "status", None)
                raw = _bounded_read(response)
            if code != 200:
                raise ForgeError("forge-unreachable", f"{where}: HTTP {code}")
            if raw is None:
                raise ForgeError("forge-unreachable", f"{where}: response over {MAX_RESPONSE_BYTES} bytes")
            try:
                return 200, json.loads(raw.decode("utf-8"))
            except (UnicodeDecodeError, json.JSONDecodeError):
                raise ForgeError("forge-unreachable", f"{where}: the response is not JSON") from None
        raise AssertionError("the retry loop always returns or raises")


def _bounded_read(response: object) -> bytes | None:
    raw = response.read(MAX_RESPONSE_BYTES + 1)
    return None if len(raw) > MAX_RESPONSE_BYTES else raw


def _is_rate_limited(code: int, hdrs: object) -> bool:
    if code == 429:
        return True
    return code == 403 and hdrs is not None and hdrs.get("x-ratelimit-remaining") == "0"


def _retry_wait(hdrs: object, body: bytes | None) -> float:
    """§10: `Retry-After` (seconds), then the body's `retry_after_ms`, then the default."""
    header = hdrs.get("Retry-After") if hdrs is not None else None
    if isinstance(header, str) and header.strip().isdigit():
        return int(header.strip())
    try:
        data = json.loads(body.decode("utf-8")) if body else None
    except (UnicodeDecodeError, json.JSONDecodeError):
        data = None
    ms = data.get("retry_after_ms") if isinstance(data, dict) else None
    if type(ms) is int and ms >= 0:
        return ms / 1000
    return limits.SERVER_429_DEFAULT_WAIT_S


def check_ping(ping: protocol.Ping, record: protocol.TeamRecord, client: Forge) -> None:
    """The forge check for a validated ping (§9 send step 6, receive step 10p): nothing for
    a ping with no `ref`, else its reference against the record's trusted branches."""
    if ping.ref is None:
        return
    if client.api != record.forge_api:
        raise ValueError("the forge client is not the team record's forge_api")
    branches = record.repos.get(ping.ref.repo_full)
    if branches is None:
        raise ValueError("the reference's repository is not in the team record (validate first)")
    client.check(ping.ref, branches)
