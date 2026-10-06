"""The Matrix client-server API client pingbus (and the admin tool) talk to a homeserver with.

Spec: docs/agent-bus-protocol.md §12 (the token travels only in the `Authorization: Bearer`
header, unredirected, never through a proxy, no redirect followed, and never appears in
output, errors, URLs or logs), §9 send step 7 (a retry after a timeout reuses the txnId),
§10 (a server 429 is honoured: `Retry-After`, then `retry_after_ms`, then 5 s, at most 3
tries, and a wait over 60 s is not slept) and §14 (exit codes 7, 8, 9, 10). Plan 00161's DESIGN.md unit U09.

This module moves JSON and maps failures; it validates nothing it receives beyond "a JSON
object", because every event is validated by `protocol` before anything acts on it.

Every failure is a `MatrixError` carrying the request's method and path (never its query),
the HTTP status and the server's `errcode` when it has the errcode grammar. A message never
holds the token, the server's free-text `error`, or the text of a lower-level exception.
"""

from __future__ import annotations

import http.client
import json
import re
import secrets
import time
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Callable, Mapping, Sequence

from helpers.pingbus import config, limits, protocol

CLIENT_PREFIX = "/_matrix/client/v3"
REQUEST_TIMEOUT_S = 15
MAX_RESPONSE_BYTES = 32 * 1024 * 1024
#: A send whose answer was lost (a timeout, a dropped connection) is retried with the same
#: transaction ID, so the server stores it once (§9 send step 7).
SEND_TRIES = 3
SEND_RETRY_WAIT_S = 1
TXN_ID_PATTERN = r"[A-Za-z0-9._~-]{1,64}"
MESSAGES_DIRECTIONS = ("f", "b")
USER_AGENT = "pingbus"

_TXN_ID_RE = re.compile(TXN_ID_PATTERN)
_ERRCODE_RE = re.compile(r"M_[A-Z0-9_]{1,64}")


class MatrixError(Exception):
    """A request the homeserver did not serve: exit 7 unless a subclass says otherwise."""

    exit_code = 7

    def __init__(self, where: str, status: int | None = None, errcode: str | None = None,
                 detail: str | None = None) -> None:
        parts = [where]
        if status is not None:
            parts.append(f"HTTP {status}" + (f" {errcode}" if errcode else ""))
        if detail:
            parts.append(detail)
        super().__init__(": ".join(parts))
        self.where, self.status, self.errcode = where, status, errcode


class Unreachable(MatrixError):
    """No usable answer: the connection failed or timed out, or the answer was not a JSON
    object within `MAX_RESPONSE_BYTES` (exit 7)."""


class NotFound(MatrixError):
    """404: the room, event or state entry is not there, or not visible to this account."""


class AuthRefused(MatrixError):
    """401: the token was rejected (exit 8)."""

    exit_code = 8


class Forbidden(MatrixError):
    """403: the account may not do this in the room, i.e. it is not joined or not
    permitted (exit 10, "not trusted, or not joined")."""

    exit_code = 10


class RateLimited(MatrixError):
    """429 on the last of `limits.SERVER_429_MAX_TRIES` tries, or asking for a wait over
    `limits.SERVER_429_WAIT_MAX_S` (exit 9)."""

    exit_code = 9


class NoRedirect(urllib.request.HTTPRedirectHandler):
    """Follows no redirect, to any origin: urllib then raises the 3xx as an HTTPError."""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def default_opener() -> urllib.request.OpenerDirector:
    """An opener with an empty proxy map (proxy variables in the environment are ignored)
    and no redirect following."""
    return urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())


def path(*segments: str) -> str:
    """A client-server API path under `CLIENT_PREFIX`, each segment percent-encoded whole."""
    return CLIENT_PREFIX + "".join("/" + urllib.parse.quote(s, safe="") for s in segments)


def _refuse(key: str, why: str) -> ValueError:
    return ValueError(f"{key} {why}")


def _room(room_id: str) -> str:
    if not protocol.is_room_id(room_id):
        raise ValueError("not a room ID")
    return room_id


def _event(event_id: str) -> str:
    if not protocol.is_event_id(event_id):
        raise ValueError("not an event ID")
    return event_id


def _compact(value: Mapping[str, object]) -> str:
    return json.dumps(value, separators=(",", ":"), sort_keys=True)


def _read_bounded(response: object, where: str) -> bytes:
    try:
        raw = response.read(MAX_RESPONSE_BYTES + 1)
    except (OSError, http.client.HTTPException) as exc:
        raise Unreachable(where, detail=type(exc).__name__) from None
    if len(raw) > MAX_RESPONSE_BYTES:
        raise Unreachable(where, detail=f"answer over {MAX_RESPONSE_BYTES} bytes")
    return raw


def _json_object(raw: bytes) -> dict | None:
    try:
        data = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        return None
    return data if isinstance(data, dict) else None


def _errcode(body: dict | None) -> str | None:
    code = body.get("errcode") if body is not None else None
    return code if isinstance(code, str) and _ERRCODE_RE.fullmatch(code) else None


_STATUS_ERRORS: dict[int, type[MatrixError]] = {401: AuthRefused, 403: Forbidden, 404: NotFound}


class Client:
    """One account on one homeserver. The token is held here and sent nowhere else."""

    def __init__(self, base_url: str, token: str, *, plain_http_hosts: Sequence[str] = (),
                 opener: object | None = None, sleep: Callable[[float], object] = time.sleep) -> None:
        self.base_url = config.check_base_url(base_url, plain_http_hosts, _refuse)
        if not config.is_printable_token(token) or len(token) > config.TOKEN_MAX_BYTES:
            raise ValueError("the token must be one line of printable ASCII")
        self._token = token
        self._opener = opener if opener is not None else default_opener()
        self._sleep = sleep

    @classmethod
    def for_member(cls, member: config.Member, *, uid: int | None = None, **kwargs: object) -> Client:
        """A client for a validated bundle, its token re-read and re-checked (§12)."""
        return cls(member.base_url, config.read_token(member, uid=uid),
                   plain_http_hosts=member.plain_http_hosts, **kwargs)

    def __repr__(self) -> str:
        return f"matrix.Client({self.base_url!r})"

    # ── the transport ────────────────────────────────────────────────────────────────────

    def _request(self, method: str, api_path: str, query: Mapping[str, str] | None,
                 body: object, auth: bool) -> urllib.request.Request:
        url = self.base_url + api_path
        if query:
            url += "?" + urllib.parse.urlencode(query)
        data = None if body is None else json.dumps(body).encode("utf-8")
        request = urllib.request.Request(url, data=data, method=method)
        request.add_header("Accept", "application/json")
        request.add_header("User-Agent", USER_AGENT)
        if data is not None:
            request.add_header("Content-Type", "application/json")
        if auth:
            request.add_unredirected_header("Authorization", f"Bearer {self._token}")
        return request

    def call(self, method: str, api_path: str, *, query: Mapping[str, str] | None = None,
             body: object = None, auth: bool = True, timeout: float = REQUEST_TIMEOUT_S) -> dict:
        """One request; the answer's JSON object. A 429 is retried per §10."""
        where = f"{method} {api_path}"
        for attempt in range(1, limits.SERVER_429_MAX_TRIES + 1):
            try:
                response = self._opener.open(self._request(method, api_path, query, body, auth),
                                             timeout=timeout)
            except urllib.error.HTTPError as exc:
                status, headers = exc.code, exc.headers
                try:
                    answer = _json_object(_read_bounded(exc, where))
                finally:
                    exc.close()
                if status == 429:
                    wait = limits.server_retry_wait_s(headers, answer)
                    if attempt == limits.SERVER_429_MAX_TRIES or wait > limits.SERVER_429_WAIT_MAX_S:
                        raise RateLimited(where, status, _errcode(answer)) from None
                    self._sleep(wait)
                    continue
                if 300 <= status < 400:
                    raise MatrixError(where, status, detail="redirect refused") from None
                error = _STATUS_ERRORS.get(status, MatrixError)
                raise error(where, status, _errcode(answer)) from None
            except (urllib.error.URLError, OSError, http.client.HTTPException) as exc:
                raise Unreachable(where, detail=type(exc).__name__) from None
            with response:
                status = getattr(response, "status", None)
                raw = _read_bounded(response, where)
            if status != 200:
                raise MatrixError(where, status)
            answer = _json_object(raw)
            if answer is None:
                raise Unreachable(where, status, detail="the answer is not a JSON object")
            return answer
        raise AssertionError("the retry loop always returns or raises")

    # ── the endpoints pingbus uses ───────────────────────────────────────────────────────

    def whoami(self) -> dict:
        return self.call("GET", path("account", "whoami"))

    def sync(self, *, since: str | None = None, filter: Mapping[str, object] | None = None,
             timeout_ms: int = 0) -> dict:
        """`/sync`, long-polling up to `timeout_ms` (at most the §10 long-poll); the socket
        waits that long plus `REQUEST_TIMEOUT_S`."""
        bound = limits.SYNC_LONG_POLL_S * 1000
        if type(timeout_ms) is not int or not 0 <= timeout_ms <= bound:
            raise ValueError(f"timeout_ms must be an integer from 0 to {bound}")
        query = {"timeout": str(timeout_ms)}
        if since is not None:
            query["since"] = since
        if filter is not None:
            query["filter"] = _compact(filter)
        return self.call("GET", path("sync"), query=query,
                         timeout=timeout_ms / 1000 + REQUEST_TIMEOUT_S)

    def join(self, room_id: str) -> dict:
        return self.call("POST", path("rooms", _room(room_id), "join"), body={})

    def leave(self, room_id: str) -> dict:
        """Leave a room, or reject an invite to it (§8)."""
        return self.call("POST", path("rooms", _room(room_id), "leave"), body={})

    def send(self, room_id: str, event_type: str, content: Mapping[str, object], *,
             txn_id: str | None = None) -> str:
        """Send one event; its event ID. A lost answer is retried with the same txnId."""
        if txn_id is None:
            txn_id = "pb" + secrets.token_hex(16)
        elif not isinstance(txn_id, str) or _TXN_ID_RE.fullmatch(txn_id) is None:
            raise ValueError(f"a transaction ID must match {TXN_ID_PATTERN}")
        api_path = path("rooms", _room(room_id), "send", event_type, txn_id)
        for attempt in range(1, SEND_TRIES + 1):
            try:
                answer = self.call("PUT", api_path, body=content)
            except Unreachable:
                if attempt == SEND_TRIES:
                    raise
                self._sleep(SEND_RETRY_WAIT_S)
                continue
            return self._event_id(answer, f"PUT {api_path}")
        raise AssertionError("the retry loop always returns or raises")

    def send_message(self, room_id: str, content: Mapping[str, object], *,
                     txn_id: str | None = None) -> str:
        return self.send(room_id, protocol.EVENT_MESSAGE, content, txn_id=txn_id)

    def put_state(self, room_id: str, event_type: str, state_key: str,
                  content: Mapping[str, object]) -> str:
        api_path = path("rooms", _room(room_id), "state", event_type, state_key)
        return self._event_id(self.call("PUT", api_path, body=content), f"PUT {api_path}")

    def get_state(self, room_id: str, event_type: str, state_key: str = "", *,
                  as_event: bool = False) -> dict:
        """The state content, or with `as_event` the whole event (`format=event`), which
        carries the sender the room-trust checks need (§8)."""
        query = {"format": "event"} if as_event else None
        return self.call("GET", path("rooms", _room(room_id), "state", event_type, state_key), query=query)

    def get_event(self, room_id: str, event_id: str) -> dict:
        return self.call("GET", path("rooms", _room(room_id), "event", _event(event_id)))

    def messages(self, room_id: str, *, from_token: str, direction: str = "f", limit: int = 10,
                 to_token: str | None = None, filter: Mapping[str, object] | None = None) -> dict:
        """One `/messages` page, for filling a limited sync's gap."""
        if direction not in MESSAGES_DIRECTIONS:
            raise ValueError("direction must be f or b")
        if type(limit) is not int or limit < 1:
            raise ValueError("limit must be a positive integer")
        query = {"from": from_token, "dir": direction, "limit": str(limit)}
        if to_token is not None:
            query["to"] = to_token
        if filter is not None:
            query["filter"] = _compact(filter)
        return self.call("GET", path("rooms", _room(room_id), "messages"), query=query)

    @staticmethod
    def _event_id(answer: dict, where: str) -> str:
        event_id = answer.get("event_id")
        if not protocol.is_event_id(event_id):
            raise Unreachable(where, detail="the answer has no valid event ID")
        return event_id
