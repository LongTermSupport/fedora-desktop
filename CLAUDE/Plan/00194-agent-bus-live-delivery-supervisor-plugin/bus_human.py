#!/usr/bin/env python3
"""The scripted test human: a team human declared for testing, posting to a team's rooms the way
Element does, so a host check needs no person at Element (Plan 00194 Tasks 2.0c, 2.0d and 2.0f,
journal 2026-10-23; Plan 00211's acceptance, its reply and edit in the questions room).

It is never the owner's account. The account is a human of a team, declared for testing; its
password is set and printed once by `sudo agent-bus human password TEAM NAME`, which the caller
pipes straight into `login`. The password is read from stdin only: it is never an argument,
never written anywhere and never printed. `login` writes a session file (the access token, mode
0600, a new file only) that the other commands read and `logout` ends and deletes.

Every request goes through pingbus's own `matrix.Client` (helpers/pingbus/matrix.py), so the
token travels only in the Authorization header and never appears in output or errors.

Commands (stdout is the payload; reasons go to stderr):

  login (--base URL [--plain-http-host IP]... | --bundle DIR --team TEAM) --user NAME --session FILE
                       the password on stdin; writes FILE; prints the user ID. With --bundle the
                       homeserver is a seat bundle's (its base_url and plain_http_hosts); its
                       token is not read
  join --session FILE --room ROOM_ID
                       join a room (accept its invite)
  rooms --session FILE accept every pending invite, then print `ROOM<TAB>id<TAB>name` for
                       every room joined
  post --session FILE --room ROOM_ID [--mention USER_ID]... [--at-room]
       [--thread ROOT_ID] [--reply-to EVENT_ID] [--edit EVENT_ID]
                       the body on stdin (one trailing newline dropped), sent as Element sends
                       it: `m.mentions` always present ({} for text with no pill, so a typed
                       `@room` mentions no one), a pill also in `body` and `formatted_body`;
                       prints the event ID
  find --session FILE --room ROOM_ID --sender USER_ID --contains TEXT --since-ms MS
                       read the room as this human: the newest message from USER_ID at or after
                       MS whose text holds TEXT; prints its event ID, exit 3 while there is none
  agent-say --bundle DIR --team TEAM --to USER_ID...
                       the text on stdin, posted as the agent whose seat bundle DIR is (the
                       peer of SPEC-inject H3), built by pingbus's protocol.build_text
  logout --session FILE
                       end the session and delete FILE

Exit codes: 0 done; 1 refused or failed (the reason on stderr); 3 not there yet (find); 64 usage.
"""

from __future__ import annotations

import argparse
import html
import importlib
import json
import os
import pathlib
import sys
from collections.abc import Sequence
from typing import TextIO

EXIT_OK = 0
EXIT_FAILED = 1
EXIT_NOT_YET = 3
EXIT_USAGE = 64
PROG = "bus_human"
MSGTYPE = "m.text"
HTML_FORMAT = "org.matrix.custom.html"
PILL = '<a href="https://matrix.to/#/{user}">{user}</a>'
#: How many /messages pages `find` reads back at most, and how many events in each.
FIND_MAX_PAGES = 20
FIND_PAGE_LIMIT = 100
SESSION_KEYS = ("base_url", "plain_http_hosts", "user_id", "access_token")


def _checkout_root() -> pathlib.Path:
    """This checkout's top: the first directory above this file holding ansible.cfg."""
    here = pathlib.Path(__file__).resolve().parent
    for candidate in (here, *here.parents):
        if (candidate / "ansible.cfg").exists():
            return candidate
        if (candidate / ".git").exists():
            break
    raise RuntimeError(f"no ansible.cfg above {here}")


def _pingbus(name: str) -> object:
    root = str(_checkout_root())
    if root not in sys.path:
        sys.path.insert(0, root)
    return importlib.import_module(f"helpers.pingbus.{name}")


matrix = _pingbus("matrix")
config = _pingbus("config")
protocol = _pingbus("protocol")


class Refused(Exception):
    """A request this tool will not make, or an answer it cannot use."""


class _Usage(Exception):
    """A command line this tool does not take."""


class _Parser(argparse.ArgumentParser):
    def error(self, message: str) -> None:
        raise _Usage(f"{self.prog}: {message}")


# ── the content Element sends ───────────────────────────────────────────────────────────


def element_content(body: str, *, mentions: Sequence[str], at_room: bool, thread: str | None = None,
                    reply_to: str | None = None, edit: str | None = None) -> dict:
    """An `m.room.message` as Element sends it. A pill puts the user ID in `body` and a link in
    `formatted_body`, and the user in `m.mentions.user_ids`; `at_room` is the @room pill. Text
    with no pill still carries `m.mentions: {}`."""
    if not body:
        raise ValueError("the body is empty")
    if edit is not None and (thread is not None or reply_to is not None):
        raise ValueError("an edit is neither a reply nor in a thread")
    content: dict = {"msgtype": MSGTYPE, "body": body, "m.mentions": {}}
    if mentions:
        content["body"] = f"{' '.join(mentions)}: {body}"
        content["format"] = HTML_FORMAT
        pills = " ".join(PILL.format(user=html.escape(user)) for user in mentions)
        content["formatted_body"] = f"{pills}: " + html.escape(body, quote=False).replace("\n", "<br>")
        content["m.mentions"]["user_ids"] = list(mentions)
    if at_room:
        content["m.mentions"]["room"] = True
    if edit is not None:
        new = dict(content)
        content = {"msgtype": MSGTYPE, "body": f"* {new['body']}", "m.mentions": dict(new["m.mentions"]),
                   "m.new_content": new, "m.relates_to": {"rel_type": "m.replace", "event_id": edit}}
        if "formatted_body" in new:
            content["format"] = HTML_FORMAT
            content["formatted_body"] = f"* {new['formatted_body']}"
    elif thread is not None:
        content["m.relates_to"] = {"rel_type": "m.thread", "event_id": thread,
                                   "is_falling_back": reply_to is None,
                                   "m.in_reply_to": {"event_id": reply_to or thread}}
    elif reply_to is not None:
        content["m.relates_to"] = {"m.in_reply_to": {"event_id": reply_to}}
    return content


# ── the session file ────────────────────────────────────────────────────────────────────


def _read_stdin_text(stdin: TextIO) -> str:
    """Stdin whole, one trailing newline dropped. Read as bytes where there are bytes, so a
    carriage return in a body is sent, not turned into a newline."""
    raw = getattr(stdin, "buffer", None)
    text = raw.read().decode("utf-8") if raw is not None else stdin.read()
    return text[:-1] if text.endswith("\n") else text


def _write_session(path: pathlib.Path, data: dict) -> None:
    try:
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    except FileExistsError:
        raise Refused(f"{path} exists: a session file is only ever written new") from None
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        json.dump(data, handle)


def _session(path: str) -> tuple[object, str]:
    """The session's client and user ID, the file checked as a token file is (owner, 0600)."""
    with config.open_private_file(path, f"the session file {path}") as handle:
        try:
            data = json.loads(handle.read().decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError):
            raise Refused(f"the session file {path} is not JSON") from None
    if not isinstance(data, dict) or any(key not in data for key in SESSION_KEYS):
        raise Refused(f"the session file {path} lacks {', '.join(SESSION_KEYS)}")
    client = matrix.Client(data["base_url"], data["access_token"], plain_http_hosts=data["plain_http_hosts"])
    return client, data["user_id"]


def _room(room_id: str) -> str:
    if not protocol.is_room_id(room_id):
        raise Refused(f"{room_id!r} is not a room ID")
    return room_id


# ── the commands ────────────────────────────────────────────────────────────────────────


def cmd_login(args: argparse.Namespace, stdin: TextIO, out: TextIO, err: TextIO) -> int:
    password = _read_stdin_text(stdin)
    if not password or "\n" in password:
        raise Refused("no password on stdin (one line: pipe `sudo agent-bus human password TEAM NAME` in)")
    session = pathlib.Path(args.session)
    if os.path.lexists(session):
        raise Refused(f"{session} exists: a session file is only ever written new")
    base, plain = args.base, list(args.plain_http_host)
    if args.bundle is not None:
        if args.team is None or args.base is not None or plain:
            raise Refused("--bundle takes --team, and the homeserver from the bundle, not --base")
        member = config.read_member(args.team, pathlib.Path(args.bundle))
        base, plain = member.base_url, list(member.plain_http_hosts)
    elif base is None:
        raise Refused("name the homeserver: --base URL, or --bundle DIR --team TEAM of a seat on it")
    # The client needs a token to exist; this placeholder is never sent (auth=False).
    client = matrix.Client(base, "login", plain_http_hosts=plain)
    answer = client.call("POST", matrix.path("login"), auth=False, body={
        "type": "m.login.password", "identifier": {"type": "m.id.user", "user": args.user},
        "password": password, "initial_device_display_name": f"{PROG} (scripted test human)"})
    token, user_id = answer.get("access_token"), answer.get("user_id")
    if not config.is_printable_token(token) or not isinstance(user_id, str) or not user_id.startswith("@"):
        raise Refused("the login answer has no access token or user ID")
    _write_session(session, {"base_url": client.base_url, "plain_http_hosts": plain,
                             "user_id": user_id, "access_token": token})
    out.write(f"{user_id}\n")
    return EXIT_OK


def cmd_join(args: argparse.Namespace, stdin: TextIO, out: TextIO, err: TextIO) -> int:
    client, _ = _session(args.session)
    client.join(_room(args.room))
    return EXIT_OK


def cmd_rooms(args: argparse.Namespace, stdin: TextIO, out: TextIO, err: TextIO) -> int:
    client, _ = _session(args.session)
    invites = client.sync(filter={"room": {"timeline": {"limit": 1}}}).get("rooms", {}).get("invite", {})
    for room_id in sorted(invites if isinstance(invites, dict) else {}):
        if protocol.is_room_id(room_id):
            client.join(room_id)
        else:
            err.write(f"{PROG}: the invite to {room_id!r} is not a room this tool joins (not room version 12)\n")
    joined = client.call("GET", matrix.path("joined_rooms")).get("joined_rooms")
    if not isinstance(joined, list):
        raise Refused("the homeserver answered with no list of joined rooms")
    for room_id in joined:
        if not protocol.is_room_id(room_id):
            err.write(f"{PROG}: joined {room_id!r}, which is not room version 12: not listed\n")
            continue
        try:
            name = client.get_state(room_id, "m.room.name").get("name")
        except matrix.NotFound:
            name = None
        out.write(f"ROOM\t{room_id}\t{name if isinstance(name, str) and name else '-'}\n")
    return EXIT_OK


def cmd_post(args: argparse.Namespace, stdin: TextIO, out: TextIO, err: TextIO) -> int:
    content = element_content(_read_stdin_text(stdin), mentions=args.mention, at_room=args.at_room,
                              thread=args.thread, reply_to=args.reply_to, edit=args.edit)
    client, _ = _session(args.session)
    out.write(f"{client.send_message(_room(args.room), content)}\n")
    return EXIT_OK


def _event_text(event: dict) -> str:
    content = event.get("content")
    if not isinstance(content, dict):
        return ""
    obj = content.get(protocol.TEXT_KEY)
    text = obj.get("text") if isinstance(obj, dict) else None
    body = content.get("body")
    return text if isinstance(text, str) else body if isinstance(body, str) else ""


def cmd_find(args: argparse.Namespace, stdin: TextIO, out: TextIO, err: TextIO) -> int:
    client, _ = _session(args.session)
    query = {"dir": "b", "limit": str(FIND_PAGE_LIMIT)}
    api_path = matrix.path("rooms", _room(args.room), "messages")
    for _ in range(FIND_MAX_PAGES):
        page = client.call("GET", api_path, query=query)
        chunk = page.get("chunk")
        if not isinstance(chunk, list) or not chunk:
            return EXIT_NOT_YET
        for event in chunk:
            if not isinstance(event, dict):
                continue
            ts = event.get("origin_server_ts")
            if type(ts) is int and ts < args.since_ms:
                return EXIT_NOT_YET
            if (event.get("type") == protocol.EVENT_MESSAGE and event.get("sender") == args.sender
                    and args.contains in _event_text(event)):
                out.write(f"{event.get('event_id')}\n")
                return EXIT_OK
        end = page.get("end")
        if not isinstance(end, str):
            return EXIT_NOT_YET
        query["from"] = end
    raise Refused(f"read back {FIND_MAX_PAGES} pages without reaching {args.since_ms}")


def cmd_agent_say(args: argparse.Namespace, stdin: TextIO, out: TextIO, err: TextIO) -> int:
    text = _read_stdin_text(stdin)
    if not text:
        raise Refused("no text on stdin")
    member = config.read_member(args.team, pathlib.Path(args.bundle))
    client = matrix.Client.for_member(member)
    client.join(member.room)
    out.write(f"{client.send_message(member.room, protocol.build_text(args.to, text))}\n")
    return EXIT_OK


def cmd_logout(args: argparse.Namespace, stdin: TextIO, out: TextIO, err: TextIO) -> int:
    client, _ = _session(args.session)
    client.call("POST", matrix.path("logout"), body={})
    os.unlink(args.session)
    return EXIT_OK


def _parser() -> argparse.ArgumentParser:
    parser = _Parser(prog=PROG, description="The scripted test human (Plan 00194). See the module doc.")
    sub = parser.add_subparsers(dest="command", required=True, parser_class=_Parser)
    login = sub.add_parser("login")
    login.add_argument("--base")
    login.add_argument("--plain-http-host", action="append", default=[])
    login.add_argument("--bundle")
    login.add_argument("--team")
    login.add_argument("--user", required=True)
    login.add_argument("--session", required=True)
    for name in ("join", "rooms", "post", "find", "logout"):
        sub.add_parser(name).add_argument("--session", required=True)
    for name in ("join", "post", "find"):
        sub.choices[name].add_argument("--room", required=True)
    post = sub.choices["post"]
    post.add_argument("--mention", action="append", default=[])
    post.add_argument("--at-room", action="store_true")
    post.add_argument("--thread")
    post.add_argument("--reply-to")
    post.add_argument("--edit")
    find = sub.choices["find"]
    find.add_argument("--sender", required=True)
    find.add_argument("--contains", required=True)
    find.add_argument("--since-ms", required=True, type=int)
    say = sub.add_parser("agent-say")
    say.add_argument("--bundle", required=True)
    say.add_argument("--team", required=True)
    say.add_argument("--to", action="append", required=True)
    return parser


COMMANDS = {"login": cmd_login, "join": cmd_join, "rooms": cmd_rooms, "post": cmd_post,
            "find": cmd_find, "agent-say": cmd_agent_say, "logout": cmd_logout}


def main(argv: Sequence[str], *, stdin: TextIO = sys.stdin, out: TextIO = sys.stdout,
         err: TextIO = sys.stderr) -> int:
    try:
        args = _parser().parse_args(argv)
    except _Usage as exc:
        err.write(f"{exc} (usage: {PROG} -h)\n")
        return EXIT_USAGE
    try:
        return COMMANDS[args.command](args, stdin, out, err)
    except (Refused, ValueError, config.ConfigError, matrix.MatrixError, OSError) as exc:
        err.write(f"{PROG} {args.command}: {exc}\n")
        return EXIT_FAILED


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
