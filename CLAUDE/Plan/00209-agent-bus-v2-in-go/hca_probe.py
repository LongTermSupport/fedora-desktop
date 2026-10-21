"""Plan 00209 Task 1.4: the host checks on a throwaway Tuwunel (A 16, C 8).

Run by acceptance-hca.bash on the host: `python3 hca_probe.py <repo root> <work dir> <report>`.
It judges the facts a server can show: HC-A1, HC-A2 (the server's half), HC-A4, HC-A6 and HC-A7
from chapter A, and H-C12, H-C13 and H-C14 from chapter C. The Element halves (HC-A2's display,
HC-A3, HC-A5) need the owner at Element and are hca_element.py's.

Each check prints PASS, FAIL or NOT JUDGED with the facts it found, and the report in <report>
ends with the verdict. Exit 0 when every check passes, 1 when one fails or a dependency is
missing (the message names the play), 2 when nothing failed but something was not judged.

`--binary` and `--extra` run it away from the host (the CCY container cannot run the
installed binary's default thread pool); acceptance-hca.bash passes neither.
"""

from __future__ import annotations

import argparse
import json
import os
import pathlib
import platform
import re
import shutil
import subprocess
import sys
import time
from typing import Callable, NamedTuple

import hca_server as hs
from hca_server import CLIENT, ProbeError, Server, Team, q

PASS, FAIL, NOT_JUDGED = "PASS", "FAIL", "NOT JUDGED"
#: The presence timers for HC-A1's timeout run; a team runs Tuwunel's defaults.
SHORT_TIMERS = {"presence_idle_timeout_s": 10, "presence_offline_timeout_s": 10}
SINCE_TIMEOUT_S = 3.0
EVENT_WAIT_S = 10.0
#: Config keys whose name matches H-C13's question but which do something else.
UNRELATED_KEYS = {
    "login_with_password": "turns password login off for every account, which DC16 (c') needs",
    "emergency_password": "the service account's own password",
    "turn_password": "the TURN server's credential",
    "bind_password_file": "the LDAP bind credential",
    "auto_deactivate_banned_room_attempts": "deactivates a user who joins a banned room",
}


class Fact(NamedTuple):
    name: str
    holds: bool | None  # None: not judged
    seen: str


class Result(NamedTuple):
    check: str
    question: str
    facts: list[Fact]


def verdict(facts: list[Fact]) -> str:
    if any(f.holds is False for f in facts):
        return FAIL
    if not facts or any(f.holds is None for f in facts):
        return NOT_JUDGED
    return PASS


def exit_code(verdicts: list[str]) -> int:
    if FAIL in verdicts:
        return 1
    return 2 if not verdicts or NOT_JUDGED in verdicts else 0


def _cell(text: str) -> str:
    return text.replace("|", "\\|").replace("\n", " ")


def render_report(title: str, header: list[str], results: list[Result]) -> str:
    verdicts = [verdict(r.facts) for r in results]
    lines = [f"# {title}", "", *[f"- {h}" for h in header], "", "| Check | Verdict | Question |", "| --- | --- | --- |"]
    lines += [f"| {r.check} | {v} | {_cell(r.question)} |" for r, v in zip(results, verdicts)]
    for r, v in zip(results, verdicts):
        lines += ["", f"## {r.check}: {v}", ""]
        for f in r.facts:
            mark = {True: "holds", False: "FAILS", None: "not judged"}[f.holds]
            lines.append(f"- {mark}: {_cell(f.name)}: {_cell(f.seen)}")
    code = exit_code(verdicts)
    word = {0: "PASS", 1: "FAIL", 2: "COULD NOT ESTABLISH"}[code]
    return "\n".join(lines + ["", f"Verdict: {word} (exit {code})"]) + "\n"


def classify_since(status: int, elapsed_s: float, timeout_s: float) -> str:
    if status != 200:
        return f"an error (HTTP {status})"
    return "a wait for the timeout" if elapsed_s >= timeout_s * 0.8 else "an answer at once"


def override_order(rules: dict) -> list[str]:
    return [str(r.get("rule_id")) for r in rules.get("global", {}).get("override", [])]


def notices_suppressed_first(rules: dict) -> bool:
    order = override_order(rules)
    names = (".m.rule.suppress_notices", ".m.rule.is_user_mention")
    return all(n in order for n in names) and order.index(names[0]) < order.index(names[1])


def keys_matching(dump: str, needles: tuple[str, ...]) -> list[str]:
    keys = re.findall(r"^\|?\s*([a-z][a-z0-9_]*)\s*(?:=|\|)", dump, re.MULTILINE)
    return [k for k in dict.fromkeys(keys) if any(n in k for n in needles)]


def config_table(dump: str) -> dict[str, str]:
    """show-config's markdown table, name to value."""
    rows = re.findall(r"^\|\s*([a-z][a-z0-9_]*)\s*\|\s*(.*?)\s*\|\s*$", dump, re.MULTILINE)
    return {name: value for name, value in rows if name != "name"}


class Run:
    """The team on the main instance, and what earlier checks found that later ones read."""

    def __init__(self, server: Server, binary: pathlib.Path, extra: dict, work: pathlib.Path) -> None:
        self.server, self.binary, self.extra, self.work = server, binary, extra, work
        self.team = Team(server)
        humans, agents = self.team.build(["owner"], [
            "hca.1+lab.host", "hca.2+lab.podman", "hca.3+lab.lxc", "hca.4+lab.host", "hca.5+lab.podman"])
        self.human = humans[0]
        self.link, self.observer, self.seat, self.leaver, self.invitee = agents
        self.outsider = self.team.account("mallory")
        self.config_dump = ""
        self.admin_room = ""

    def code(self, status: int, errcode: str) -> str:
        return f"HTTP {status} {errcode}".rstrip(" -")

    def wait_for(self, user: str, filt: dict, since: str, room: str, event_id: str) -> dict:
        """The first sync batch from `since` whose timeline in `room` holds `event_id`."""
        deadline = time.monotonic() + EVENT_WAIT_S
        while time.monotonic() < deadline:
            batch = self.team.sync(user, filt, since, timeout_ms=1000)
            joined = batch.get("rooms", {}).get("join", {}).get(room, {})
            if any(e.get("event_id") == event_id for e in joined.get("timeline", {}).get("events", [])):
                return joined
            since = batch["next_batch"]
        raise ProbeError(f"{event_id} did not reach {user}'s sync within {EVENT_WAIT_S} s")


def check_state_read(run: Run) -> list[Fact]:
    t, main = run.team, run.team.rooms["main"]
    state = t.ok("read main's state", "GET", f"{CLIENT}/rooms/{q(main)}/state", user=run.link).get("value", [])
    create = [e for e in state if isinstance(e, dict) and e.get("type") == "m.room.create"]
    if len(create) != 1:
        return [Fact("the state list holds one create event", False, f"{len(create)} create events")]
    ev, content = create[0], create[0].get("content", {})
    return [
        Fact("the state list holds one create event", True, "one"),
        Fact("the create event carries its sender, the admin", ev.get("sender") == t.admin, str(ev.get("sender"))),
        Fact("its event ID is $ and the room ID without !", ev.get("event_id") == "$" + main[1:],
             f"{ev.get('event_id')} for {main}"),
        Fact("room_version 12 and no additional_creators",
             content.get("room_version") == "12" and "additional_creators" not in content, json.dumps(content)),
    ]


def check_power_levels(run: Run) -> list[Fact]:
    t, rooms, h, a = run.team, run.team.rooms, run.human, run.link
    facts = []
    for name, sent in (("main", hs.levels_main([h])), ("reports", hs.levels_reports([h])),
                       ("questions (open)", hs.levels_main([h])), ("questions (closed)", hs.levels_questions_closed([h]))):
        room = rooms[name.split(" ")[0] if name != "questions (closed)" else "closed"]
        got = t.ok(f"read {name}'s power levels", "GET", f"{CLIENT}/rooms/{q(room)}/state/m.room.power_levels/",
                   user=t.admin)
        diff = sorted(k for k in set(got) | set(sent) if got.get(k) != sent.get(k))
        facts.append(Fact(f"{name}: the power levels read back unchanged", not diff,
                          "exact" if not diff else f"differs in {', '.join(diff)}: {json.dumps(got, sort_keys=True)}"))
    main = rooms["main"]
    status, err, target = t.send(main, a, "m.room.message", {"msgtype": "m.text", "body": "HC-A4 agent message"})
    facts.append(Fact("main: an agent (0) posts m.room.message", status == 200, run.code(status, err)))
    status, err, own = t.send(main, h, "m.room.message", {"msgtype": "m.text", "body": "HC-A4 human message"})
    facts.append(Fact("main: a human (50) posts m.room.message", status == 200, run.code(status, err)))
    if not target or not own:
        raise ProbeError("the messages the refusals act on were not posted")
    refused = [
        ("a human's reaction", h, "m.reaction", {"m.relates_to": {"rel_type": "m.annotation", "event_id": target, "key": "+1"}}),
        ("a human's sticker", h, "m.sticker", {"body": "s", "url": "mxc://hca.teamchat.internal/s", "info": {}}),
        ("a human's poll (m.poll.start)", h, "m.poll.start", {"m.text": [{"body": "p"}]}),
        ("a human's poll (org.matrix.msc3381.poll.start)", h, "org.matrix.msc3381.poll.start", {"body": "p"}),
        ("an agent's reaction", a, "m.reaction", {"m.relates_to": {"rel_type": "m.annotation", "event_id": own, "key": "+1"}}),
    ]
    for what, user, event_type, content in refused:
        status, err, _ = t.send(main, user, event_type, content)
        facts.append(Fact(f"main: {what} is refused", status == 403, run.code(status, err)))
    t.txn += 1
    status, obj = t.call("PUT", f"{CLIENT}/rooms/{q(main)}/redact/{q(own)}/hca{t.txn}", {}, t.tokens[h])
    facts.append(Fact("main: a human's redaction of their own message is refused", status == 403,
                      run.code(status, str(obj.get("errcode", "-")))))
    for what, user, event_type in (("a human's state event (m.room.topic)", h, "m.room.topic"),
                                   ("an agent's state event (teamchat.roster)", a, "teamchat.roster")):
        status, err = t.put_state(main, user, event_type, "", {"topic": "x"} if event_type == "m.room.topic" else {})
        facts.append(Fact(f"main: {what} is refused", status == 403, run.code(status, err)))
    for what, user in (("a human's invite", h), ("an agent's invite", a)):
        status, obj = t.call("POST", f"{CLIENT}/rooms/{q(main)}/invite", {"user_id": run.outsider}, t.tokens[user])
        facts.append(Fact(f"main: {what} is refused", status == 403, run.code(status, str(obj.get("errcode", "-")))))
    status, err, _ = t.send(rooms["closed"], h, "m.room.message", {"msgtype": "m.text", "body": "HC-A4 closed"})
    facts.append(Fact("questions (closed): a human's message is refused", status == 403, run.code(status, err)))
    return facts


def check_side_rooms(run: Run) -> list[Fact]:
    """The S11 probes carried (00211 R1, R2, R4)."""
    t, rooms = run.team, run.team.rooms
    facts = []
    status, err = t.join(rooms["reports"], run.link)
    facts.append(Fact("an agent in main joins reports by the restricted rule", status == 200, run.code(status, err)))
    status, err = t.join(rooms["reports"], run.outsider)
    facts.append(Fact("an account outside main cannot join reports", status == 403, run.code(status, err)))
    status, err, _ = t.send(rooms["reports"], run.link, "m.room.message", {"msgtype": "m.notice", "body": "HC report"})
    facts.append(Fact("the joined agent posts in reports (10)", status == 200, run.code(status, err)))
    status, err, _ = t.send(rooms["reports"], run.human, "m.room.message", {"msgtype": "m.text", "body": "HC human"})
    facts.append(Fact("a power-0 human is refused a post in reports", status == 403, run.code(status, err)))
    t.ok("invite an agent to questions", "POST", f"{CLIENT}/rooms/{q(rooms['questions'])}/invite",
         {"user_id": run.invitee}, t.admin)
    filt, since, synced = hs.member_filter([rooms["main"]], []), None, 0
    for _ in range(3):
        batch = t.sync(run.invitee, filt, since)
        since, synced = batch["next_batch"], synced + 1
    member = t.ok("read the invitee's membership", "GET",
                  f"{CLIENT}/rooms/{q(rooms['questions'])}/state/m.room.member/{q(run.invitee)}", user=t.admin)
    facts.append(Fact("a pending invite stays pending across filtered syncs",
                      synced == 3 and member.get("membership") == "invite",
                      f"{synced} syncs, membership {member.get('membership')}"))
    return facts


def check_notifications(run: Run) -> list[Fact]:
    """HC-A2's server half: the human's push rules and the counts they produce."""
    t, h, main = run.team, run.human, run.team.rooms["main"]
    rules = t.ok("read the human's push rules", "GET", f"{CLIENT}/pushrules/", user=h)
    facts = [Fact(".m.rule.suppress_notices comes before .m.rule.is_user_mention", notices_suppressed_first(rules),
                  ", ".join(override_order(rules)))]
    filt = hs.member_filter([main], [])
    first = t.sync(h, filt, None)
    since = first["next_batch"]
    counts = [first.get("rooms", {}).get("join", {}).get(main, {}).get("unread_notifications")]
    for msgtype in ("m.notice", "m.text"):
        status, err, event_id = t.send(main, run.link, "m.room.message", {
            "msgtype": msgtype, "body": f"owner: HC-A2 {msgtype} with a mention", "m.mentions": {"user_ids": [h]}})
        if status != 200:
            raise ProbeError(f"the agent's {msgtype} was refused: {run.code(status, err)}")
        joined = run.wait_for(h, filt, since, main, event_id)
        since = t.sync(h, filt, None)["next_batch"]
        # Tuwunel leaves the counts out of a batch that did not change them.
        counts.append(joined.get("unread_notifications", counts[-1]))
    if not all(isinstance(c, dict) for c in counts):
        return facts + [Fact("the sync carries unread_notifications", None, json.dumps(counts))]
    base, notice, text = ((int(c.get("notification_count", 0)), int(c.get("highlight_count", 0))) for c in counts)
    facts.append(Fact("an agent's m.notice mentioning the human does not notify", notice == base,
                      f"(notifications, highlights) {base} before, {notice} after the notice"))
    facts.append(Fact("an agent's m.text mentioning the human notifies and highlights",
                      text == (notice[0] + 1, notice[1] + 1), f"{text} after the text"))
    return facts


def check_service_account(run: Run) -> list[Fact]:
    t = run.team
    alias = f"#admins:{hs.SERVER_NAME}"
    room = t.ok(f"resolve {alias}", "GET", f"{CLIENT}/directory/room/{q(alias)}")["room_id"]
    run.admin_room = room
    state = t.ok("read the admin room's state", "GET", f"{CLIENT}/rooms/{q(room)}/state", user=t.admin).get("value", [])
    create = next((e for e in state if isinstance(e, dict) and e.get("type") == "m.room.create"), {})
    sender = str(create.get("sender", ""))
    localpart = sender[1:].split(":", 1)[0] if sender.startswith("@") else ""
    facts = [Fact("the service account is the admin room's creator", bool(localpart),
                  f"@{localpart} (v1 reserved conduit: {'the same' if localpart == 'conduit' else 'DIFFERENT'})")]
    status, obj = t.call("GET", f"{CLIENT}/profile/{q(sender)}", None, t.tokens[run.link])
    facts.append(Fact("a member can look the account up", status == 200, run.code(status, str(obj.get("errcode", "-")))))
    run.config_dump = fetch_config(run, room, sender)
    return facts


def fetch_config(run: Run, room: str, service: str) -> str:
    """The admin command `server show-config`, answered by the service account in the admin room."""
    t = run.team
    status, err, command = t.send(room, t.admin, "m.room.message", {"msgtype": "m.text", "body": "!admin server show-config"})
    if status != 200:
        return ""
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        chunk = t.ok("read the admin room", "GET", f"{CLIENT}/rooms/{q(room)}/messages", user=t.admin,
                     query={"dir": "b", "limit": "20"}).get("chunk", [])
        ids = [e.get("event_id") for e in chunk]
        newer = chunk[: ids.index(command)] if command in ids else []
        for event in newer:
            body = str(event.get("content", {}).get("body", ""))
            if event.get("sender") == service and "| name | value |" in body:
                return body
        time.sleep(0.5)
    return ""


def check_presence(run: Run) -> list[Fact]:
    t, a, b, main = run.team, run.link, run.observer, run.team.rooms["main"]
    filt = hs.member_filter([main], [a])

    def status_of(user: str) -> dict:
        return t.ok("read presence", "GET", f"{CLIENT}/presence/{q(user)}/status", user=b)

    since = t.sync(b, filt, None)["next_batch"]
    t.sync(a, filt, None)
    seen: list[dict] = []
    deadline = time.monotonic() + EVENT_WAIT_S
    while not seen and time.monotonic() < deadline:
        batch = t.sync(b, filt, since, timeout_ms=1000)
        since = batch["next_batch"]
        seen = [e.get("content", {}) for e in batch.get("presence", {}).get("events", []) if e.get("sender") == a]
    facts = [Fact("a link's default sync shows it online in another member's filtered sync",
                  any(c.get("presence") == "online" for c in seen), json.dumps(seen) if seen else "no presence event")]
    t.sync(a, filt, None, set_presence="offline")
    after = status_of(a)
    facts.append(Fact("a sync with set_presence=offline leaves presence unchanged",
                      after.get("presence") == "online", f"presence after it: {after.get('presence')}"))
    t.sync(a, filt, None)
    t.ok("PUT presence offline", "PUT", f"{CLIENT}/presence/{q(a)}/status", {"presence": "offline"}, user=a)
    after = status_of(a)
    facts.append(Fact("a link's PUT /presence of offline takes effect", after.get("presence") == "offline",
                      f"presence after it: {after.get('presence')}"))
    table = config_table(run.config_dump)
    idle, offline = table.get("presence_idle_timeout_s"), table.get("presence_offline_timeout_s")
    facts.append(Fact("the timers a team's config leaves at Tuwunel's defaults", None if idle is None or offline is None else True,
                      "show-config gave no answer" if idle is None or offline is None else
                      f"presence_idle_timeout_s {idle}, presence_offline_timeout_s {offline}"))
    facts.append(presence_timeout(run))
    return facts


def presence_timeout(run: Run) -> Fact:
    """A second, short-lived server with short timers: when does a member that stops syncing go offline?"""
    work = run.work / "timers"
    work.mkdir()
    server = Server(work, run.binary, run.extra | SHORT_TIMERS)
    name = "a member that stops syncing goes offline by the server's timers"
    try:
        server.start()
        team = Team(server)
        _, (a, b) = team.build([], ["hca.1+lab.host", "hca.2+lab.podman"])
        team.sync(a, hs.member_filter([team.rooms["main"]], []), None)
        start, marks, last = time.monotonic(), [], None
        limit = sum(SHORT_TIMERS.values()) + 30
        while time.monotonic() - start < limit and last != "offline":
            last = team.ok("read presence", "GET", f"{CLIENT}/presence/{q(a)}/status", user=b).get("presence")
            if not marks or marks[-1][0] != last:
                marks.append((last, round(time.monotonic() - start)))
            time.sleep(1)
    finally:
        server.stop()
    timers = ", ".join(f"{k} {v}" for k, v in SHORT_TIMERS.items())
    return Fact(name, last == "offline",
                f"with {timers}: " + ", then ".join(f"{p} at {s} s" for p, s in marks)
                + ("" if last == "offline" else f"; still {last} after {limit} s"))


def login(run: Run, user: str, device: str) -> tuple[int, dict]:
    t = run.team
    return t.call("POST", f"{CLIENT}/login", {"type": "m.login.password", "identifier": {"type": "m.id.user", "user": user},
                                            "password": t.passwords[user], "device_id": device})


def whoami(run: Run, token: str) -> str:
    status, obj = run.team.call("GET", f"{CLIENT}/account/whoami", None, token)
    return run.code(status, str(obj.get("errcode", "-")))


def token_of(run: Run, user: str, device: str) -> str:
    status, obj = login(run, user, device)
    if status != 200:
        raise ProbeError(f"a password login of {user} answered {run.code(status, str(obj.get('errcode', '-')))}")
    return str(obj["access_token"])


def check_seat_credentials(run: Run) -> list[Fact]:
    """H-C13, DC16 (c'): a password, a login per take with the seat's fixed device ID, a logout per release."""
    t, seat = run.team, run.seat
    first = token_of(run, seat, "HCASEAT")
    second = token_of(run, seat, "HCASEAT")
    answer = whoami(run, first)
    facts = [Fact("a second login with the same device_id makes the first token answer M_UNKNOWN_TOKEN",
                  answer == "HTTP 401 M_UNKNOWN_TOKEN", f"the first token then answers {answer}")]
    t.call("POST", f"{CLIENT}/logout", {}, second)
    facts.append(Fact("POST /logout kills the token", whoami(run, second) == "HTTP 401 M_UNKNOWN_TOKEN",
                      f"it then answers {whoami(run, second)}; the first token answers {whoami(run, first)}"))
    token = token_of(run, seat, "HCASEAT")
    for what, path in (("change the password", f"{CLIENT}/account/password"),
                       ("deactivate the account", f"{CLIENT}/account/deactivate")):
        status, obj = t.call("POST", path, {"new_password": "hca-" + "x" * 20} if "password" in path else {}, token)
        stages = [s for f in obj.get("flows", []) for s in f.get("stages", [])]
        facts.append(Fact(f"the token alone cannot {what}", status == 401 and stages == ["m.login.password"],
                          f"{run.code(status, str(obj.get('errcode', '-')))}, stages {stages}"))
    status, obj = t.call("POST", "/_matrix/client/v1/login/get_token", {}, token)
    facts.append(Fact("the token alone cannot mint a login token (so cannot add a device)", status != 200,
                      run.code(status, str(obj.get("errcode", "-")))))
    facts.append(config_key_fact(run))
    facts += admin_read_facts(run, token)
    return facts


def config_key_fact(run: Run) -> Fact:
    name = "a config key refuses a member's own password change and deactivation"
    if not run.config_dump:
        return Fact(name, None, "show-config gave no answer")
    found = keys_matching(run.config_dump, ("password", "deactivat"))
    unknown = [k for k in found if k not in UNRELATED_KEYS]
    seen = "; ".join(f"{k}: {UNRELATED_KEYS[k]}" for k in found if k in UNRELATED_KEYS)
    if unknown:
        return Fact(name, None, f"keys to read before the config is fixed: {', '.join(unknown)}; {seen}")
    return Fact(name, True, f"no such key, so the config sets none (the health tick is the watch); the keys "
                            f"that match do other things: {seen}")


def admin_read_facts(run: Run, token: str) -> list[Fact]:
    t, seat, admin = run.team, run.seat, run.team.admin
    user = t.ok("admin read of the seat", "GET", f"/_synapse/admin/v2/users/{q(seat)}", user=admin)
    devices = t.ok("admin read of its devices", "GET", f"/_synapse/admin/v2/users/{q(seat)}/devices", user=admin)
    ids = sorted(str(d.get("device_id")) for d in devices.get("devices", []))
    facts = [Fact("an admin read lists a member's devices and its deactivated flag",
                  "HCASEAT" in ids and user.get("deactivated") is False, f"devices {ids}, deactivated {user.get('deactivated')}")]
    t.ok("deactivate an account", "PUT", f"/_synapse/admin/v2/users/{q(run.leaver)}", {"deactivated": True}, user=admin)
    gone = t.ok("admin read of it", "GET", f"/_synapse/admin/v2/users/{q(run.leaver)}", user=admin)
    facts.append(Fact("the admin read shows a deactivated account as deactivated", gone.get("deactivated") is True,
                      f"deactivated {gone.get('deactivated')}; its token answers {whoami(run, t.tokens[run.leaver])}"))
    other = token_of(run, seat, "HCASEAT2")
    path = f"{CLIENT}/devices/HCASEAT2"
    status, uia = t.call("DELETE", path, {}, token)
    auth = {"type": "m.login.password", "identifier": {"type": "m.id.user", "user": seat},
            "password": t.passwords[seat], "session": uia.get("session")}
    done, obj = t.call("DELETE", path, {"auth": auth}, token)
    facts.append(Fact("the fallback: DELETE /devices/<id> with UIA by password kills that device's token",
                      status == 401 and done == 200 and whoami(run, other) == "HTTP 401 M_UNKNOWN_TOKEN",
                      f"without auth {status}, with it {run.code(done, str(obj.get('errcode', '-')))}; "
                      f"the device's token then answers {whoami(run, other)}, the deleting one {whoami(run, token)}"))
    return facts


def check_restore(run: Run) -> list[Fact]:
    """H-C14. The restore is a cold copy of the stopped database put back; Tuwunel's own backup and
    --restore-backup are H-C4's and H-C5's."""
    t, a, main, db = run.team, run.link, run.team.rooms["main"], run.server.work / "db"
    filt = hs.member_filter([main], [])
    status, err = t.put_state(main, t.admin, "teamchat.roster", "", {"hca": "before the backup"})
    if status != 200:
        raise ProbeError(f"the admin could not write the roster: {run.code(status, err)}")
    run.server.stop()
    shutil.copytree(db, run.server.work / "db.backup")
    run.server.start()
    for n in range(3):
        t.send(main, a, "m.room.message", {"msgtype": "m.text", "body": f"H-C14 after the backup {n}"})
    ahead = t.sync(a, filt, None)["next_batch"]
    run.server.stop()
    shutil.rmtree(db)
    (run.server.work / "db.backup").rename(db)
    run.server.start()
    began = time.monotonic()
    status, obj = t.call("GET", f"{CLIENT}/sync", None, t.tokens[a],
                         {"filter": json.dumps(filt), "since": ahead, "timeout": str(int(SINCE_TIMEOUT_S * 1000))},
                         SINCE_TIMEOUT_S + 30)
    elapsed = time.monotonic() - began
    facts = [Fact("what /sync answers to a since ahead of the database", True,
                  f"{classify_since(status, elapsed, SINCE_TIMEOUT_S)} ({elapsed:.1f} s of a {SINCE_TIMEOUT_S:.0f} s "
                  f"timeout): since {ahead}, next_batch {obj.get('next_batch')}, errcode {obj.get('errcode', '-')}")]
    began = time.monotonic()
    status, roster = t.call("GET", f"{CLIENT}/rooms/{q(main)}/state/teamchat.roster/", None, t.tokens[a])
    elapsed = time.monotonic() - began
    facts.append(Fact("a direct state read of the roster answers at once, as the backup holds it",
                      status == 200 and roster == {"hca": "before the backup"} and elapsed < 2,
                      f"HTTP {status} in {elapsed:.2f} s: {json.dumps(roster)}"))
    status, err, posted = t.send(main, run.observer, "m.room.message", {"msgtype": "m.text", "body": "H-C14 after the restore"})
    _, obj = t.call("GET", f"{CLIENT}/sync", None, t.tokens[a],
                    {"filter": json.dumps(filt), "since": ahead, "timeout": str(int(SINCE_TIMEOUT_S * 1000))},
                    SINCE_TIMEOUT_S + 30)
    timeline = obj.get("rooms", {}).get("join", {}).get(main, {}).get("timeline", {}).get("events", [])
    got = any(e.get("event_id") == posted for e in timeline)
    facts.append(Fact("(recorded) a post after the restore reaches a member still holding the old since", True,
                      "delivered" if got else
                      f"NOT delivered (next_batch {obj.get('next_batch')}): a member must drop its since after a restore"))
    return facts


CHECKS: list[tuple[str, str, Callable[[Run], list[Fact]]]] = [
    ("HC-A7", "GET /rooms/{room}/state returns the create event with its sender, so one read verifies a room",
     check_state_read),
    ("HC-A4", "Tuwunel accepts 4.2's power levels exactly as written and refuses polls, reactions, stickers, "
              "redactions, state and invites to non-admins", check_power_levels),
    ("H-C12", "a restricted join to reports, a power-0 post refused, pending invites left pending", check_side_rooms),
    ("HC-A2 (server)", "the human's push rules notify an agent's m.text that mentions them and not an m.notice",
     check_notifications),
    ("HC-A6", "Tuwunel's own service account localpart, to reserve", check_service_account),
    ("HC-A1", "presence with federation off: online by the link's sync, set_presence=offline, PUT offline, "
              "the timeout", check_presence),
    ("H-C13", "DC16 (c'): one device per seat, logout, what the token alone can do, the admin read, the fallback",
     check_seat_credentials),
    ("H-C14", "after a restore: /sync with a since ahead of the database, and a direct roster read", check_restore),
]


def run_checks(run: Run) -> list[Result]:
    results = []
    for check, question, fn in CHECKS:
        try:
            facts = fn(run)
        except (ProbeError, KeyError, ValueError) as exc:
            facts = [Fact("the check ran to the end", None, f"{type(exc).__name__}: {exc}")]
        results.append(Result(check, question, facts))
        print(f"{verdict(facts):<11} {check}")
        for f in facts:
            print(f"    {'holds' if f.holds else 'FAILS' if f.holds is False else 'not judged'}: {f.name}: {f.seen}")
    return results


def resolve_binary(repo: pathlib.Path, given: pathlib.Path | None) -> tuple[pathlib.Path, list[str]]:
    pin = hs.parse_pin((repo / hs.PIN_FILE).read_text(encoding="utf-8"))
    binary, version, sha = hs.pinned_binary(pin, platform.machine(), hs.LIB_DIR)
    if given is not None:
        binary, record = given.resolve(), f"given with --binary, so its asset record is not checked against {hs.PIN_FILE}"
    else:
        sums = pathlib.Path(f"{binary}.asset-sha256")
        if not os.access(binary, os.X_OK) or not sums.is_file():
            raise hs.MissingDependency(f"{binary} is not installed: run {hs.INSTALL_PLAY}")
        if sums.read_text(encoding="ascii").strip() != sha:
            raise hs.MissingDependency(f"{binary} was not installed from the pinned asset: run {hs.INSTALL_PLAY}")
        record = f"installed from the pinned asset (sha256 {sha})"
    said = subprocess.run([str(binary), "--version"], check=True, capture_output=True, text=True).stdout.strip()
    if said != f"tuwunel {version}":
        raise hs.MissingDependency(f"{binary} says '{said}', the pin is {version}: run {hs.INSTALL_PLAY}")
    return binary, [f"binary: {binary}, `--version` {said}, {record}",
                    f"config: a team's key set (C 3.2.4), server name {hs.SERVER_NAME}, on a loopback port"]


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("repo", type=pathlib.Path)
    parser.add_argument("work", type=pathlib.Path)
    parser.add_argument("report", type=pathlib.Path)
    parser.add_argument("--binary", type=pathlib.Path)
    parser.add_argument("--extra", action="append", default=[], metavar="KEY=JSON")
    args = parser.parse_args(argv)
    extra = {k: json.loads(v) for k, _, v in (e.partition("=") for e in args.extra)}
    try:
        binary, header = resolve_binary(args.repo, args.binary)
    except hs.MissingDependency as exc:
        print(f"[FATAL] {exc}", file=sys.stderr)
        return 1
    if extra:
        header.append(f"config keys added for this run away from the host: {json.dumps(extra)}")
    args.work.mkdir(parents=True)
    server = Server(args.work, binary, extra)
    try:
        server.start()
        version = Team(server).call("GET", "/_tuwunel/server_version")[1]
        header.append(f"the server says {json.dumps(version)}")
        run = Run(server, binary, extra, args.work)
        results = run_checks(run)
        if run.config_dump:
            dump = args.report.parent / "show-config.txt"
            dump.write_text(run.config_dump, encoding="utf-8")
            header.append(f"the server's `show-config` answer: {dump}")
    except ProbeError as exc:
        print(f"[NOT JUDGED] the throwaway team could not be set up: {exc}", file=sys.stderr)
        results = [Result(c, question, [Fact("the team was set up", None, str(exc))]) for c, question, _ in CHECKS]
    finally:
        server.stop()
    header.append("not in this run: HC-A2 (Element's display), HC-A3 and HC-A5, the owner's Element checks "
                  "(acceptance-hca.bash --element)")
    args.report.write_text(render_report("Plan 00209 host checks (Task 1.4)", header, results), encoding="utf-8")
    return exit_code([verdict(r.facts) for r in results])


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
