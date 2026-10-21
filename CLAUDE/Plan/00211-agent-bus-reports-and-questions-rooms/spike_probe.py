"""Plan 00211 Task 0.1: the spike's probes, against a throwaway Tuwunel (SPEC §11.2).

Run by spike.bash on the host, with the repository root on PYTHONPATH:
`python3 spike_probe.py <work dir> <report file>`. It starts the installed, pinned Tuwunel as
the invoking user, on a free loopback port, with its config, database and a generated
registration key under <work dir>, and throwaway accounts on a made-up server name. It
changes no system state; spike.bash removes <work dir> on cleanup.

The power levels and join rules are the production ones (`helpers.pingbus.protocol`), so
the facts are about the rooms the admin tool will create. It records, in <report file>:

- R1: a member of the main room joins the reports room by a restricted join (room version 12,
  allowed by main-room membership), and an account outside the main room cannot;
- R2: a power-0 human is refused `m.room.message` in reports (`events_default` 10);
- R3: a power-50 human is refused `m.reaction` in questions (100), and may post a message;
- R4: a pending invite to questions does no harm across syncs filtered to the main room,
  and whether such a filtered sync shows the invite at all (SPEC §4.4's open point);
- R5: the power levels and join rules of reports, open questions and closed questions, read
  back from the server, pass the exact checks `Syncer.verify_side` makes.

Exit 0 when every fact holds as the design assumes, 1 when one does not (the report says
which, and SPEC §3.1, §4.3, §4.4 and §5.3 must then be amended before Phase 2), 2 when a probe
could not run. The report ends with a verdict line that deploy 2's pre-flight requires.
The config keys follow Plan 00161's probe H4 (its DESIGN.md section 3.6).
"""

from __future__ import annotations

import json
import os
import pathlib
import secrets
import signal
import socket
import subprocess
import sys
import time
import urllib.parse

from helpers.agent_bus import admin
from helpers.pingbus import protocol, syncer

BINARY = pathlib.Path("/usr/local/lib/agent-bus/tuwunel")
SERVER_NAME = "spike.agent-bus.internal"
READY_S = 120
STOP_S = 60
SYNCS = 3
CLIENT = "/_matrix/client/v3"
Fact = tuple[str, bool, str]
#: The report's last line; deploy.bash --stage 2 refuses to run without the PASS line.
VERDICT_PASS = "Verdict: every fact holds"
VERDICT_FAIL = "Verdict: a fact does not hold"


class ProbeError(Exception):
    """A probe could not run (exit 2): not a finding."""


def toml(port: int, root: pathlib.Path) -> str:
    keys: dict[str, object] = {
        "server_name": SERVER_NAME, "address": ["127.0.0.1"], "port": port,
        "database_path": f"{root}/db", "database_backup_path": f"{root}/backups",
        "database_backups_to_keep": 1, "allow_registration": False,
        "allow_guest_registration": False,
        "registration_shared_secret_file": f"{root}/keys/registration",
        "grant_admin_to_first_user": False, "login_via_token": False, "allow_federation": False,
        "trusted_servers": [], "federate_admin_room": False, "admin_escape_commands": False,
        "allow_encryption": False, "allow_room_creation": False, "auto_accept_invites": False,
        "new_user_displayname_suffix": "", "default_room_version": "12", "sentry": False,
        "log": "warn", "login_via_existing_session": False, "error_on_unknown_config_opts": True,
        "rocksdb_allow_fallocate": False,
    }
    return "".join(f"{key} = {json.dumps(value)}\n" for key, value in keys.items())


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


class Server:
    def __init__(self, work: pathlib.Path) -> None:
        self.work, self.port = work, free_port()
        (work / "keys").mkdir(parents=True, mode=0o700)
        (work / "backups").mkdir(mode=0o700)
        self.shared = secrets.token_hex(64)
        key_path = work / "keys" / "registration"
        key_path.write_text(self.shared, encoding="ascii")
        key_path.chmod(0o600)
        (work / "tuwunel.toml").write_text(toml(self.port, work), encoding="ascii")
        self.call = admin.http_transport(f"http://127.0.0.1:{self.port}")
        self.proc: subprocess.Popen[bytes] | None = None

    def start(self) -> None:
        with (self.work / "tuwunel.log").open("ab") as log:
            self.proc = subprocess.Popen(
                [str(BINARY)], env={"PATH": "/usr/bin:/bin", "TUWUNEL_CONFIG": str(self.work / "tuwunel.toml")},
                stdout=log, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL, cwd=self.work)
        (self.work / "tuwunel.pid").write_text(str(self.proc.pid), encoding="ascii")
        deadline = time.monotonic() + READY_S
        while time.monotonic() < deadline:
            if self.proc.poll() is not None:
                raise ProbeError(f"Tuwunel exited {self.proc.returncode} before it was ready "
                                 f"(log: {self.work / 'tuwunel.log'})")
            if self._answers():
                return
            time.sleep(0.5)
        raise ProbeError(f"Tuwunel was not ready after {READY_S} s")

    def _answers(self) -> bool:
        """Listening yet? Unreachable is the expected answer until it is; the wait is bounded."""
        try:
            return self.call("GET", "/_matrix/client/versions")[0] == 200
        except admin.Unreachable:
            return False

    def stop(self) -> None:
        if self.proc is None or self.proc.poll() is not None:
            return
        self.proc.send_signal(signal.SIGTERM)
        try:
            self.proc.wait(timeout=STOP_S)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait(timeout=STOP_S)
            raise ProbeError(f"Tuwunel did not stop within {STOP_S} s of SIGTERM; killed") from None


def q(value: str) -> str:
    return urllib.parse.quote(value, safe="")


class Spike:
    def __init__(self, server: Server) -> None:
        self.s = server
        self.tokens: dict[str, str] = {}
        self.txn = 0
        self.last: dict = {}

    def ok(self, what: str, method: str, path: str, body: object = None, token: str | None = None,
           query: dict | None = None) -> dict:
        status, obj = self.s.call(method, path, query, body, token)
        if status != 200:
            raise ProbeError(f"{what}: {status} {obj.get('errcode') if isinstance(obj, dict) else ''}")
        return obj

    def status(self, method: str, path: str, body: object, user: str) -> tuple[int, str]:
        status, obj = self.s.call(method, path, None, body, self.tokens[user])
        self.last = obj if isinstance(obj, dict) else {}
        return status, str(self.last.get("errcode", "-"))

    def admin(self) -> str:
        nonce = self.ok("nonce", "GET", "/_synapse/admin/v1/register")["nonce"]
        password = secrets.token_hex(16)
        mac = admin.registration_mac(self.s.shared, nonce, "admin", password, True)
        self.ok("register admin", "POST", "/_synapse/admin/v1/register",
                {"nonce": nonce, "username": "admin", "password": password, "admin": True, "mac": mac})
        token = self.ok("log admin in", "POST", f"{CLIENT}/login", {
            "type": "m.login.password", "identifier": {"type": "m.id.user", "user": "admin"},
            "password": password})["access_token"]
        uid = f"@admin:{SERVER_NAME}"
        self.tokens[uid] = token
        return uid

    def account(self, localpart: str) -> str:
        uid = f"@{localpart}:{SERVER_NAME}"
        a = self.tokens[f"@admin:{SERVER_NAME}"]
        self.ok(f"create {localpart}", "PUT", f"/_synapse/admin/v2/users/{q(uid)}",
                {"password": secrets.token_hex(16), "displayname": localpart}, a)
        self.tokens[uid] = self.ok(f"mint {localpart}", "POST", f"/_synapse/admin/v1/users/{q(uid)}/login",
                                   {}, a)["access_token"]
        return uid

    def room(self, creator: str, levels: dict, invite: list[str], state: tuple[dict, ...] = ()) -> str:
        return self.ok("create a room", "POST", f"{CLIENT}/createRoom", {
            "preset": "private_chat", "room_version": "12", "power_level_content_override": levels,
            "invite": invite, "initial_state": list(state)}, self.tokens[creator])["room_id"]

    def join(self, room: str, user: str) -> tuple[int, str]:
        return self.status("POST", f"{CLIENT}/join/{q(room)}", {}, user)

    def send(self, room: str, user: str, event_type: str, content: dict) -> tuple[int, str]:
        self.txn += 1
        return self.status("PUT", f"{CLIENT}/rooms/{q(room)}/send/{q(event_type)}/s{self.txn}", content, user)

    def sync(self, user: str, room: str, since: str | None) -> dict:
        query = {"filter": json.dumps(syncer.sync_filter(room, 10)), "timeout": "0"}
        if since is not None:
            query["since"] = since
        return self.ok("sync", "GET", f"{CLIENT}/sync", token=self.tokens[user], query=query)


def run(work: pathlib.Path) -> list[Fact]:
    server = Server(work)
    server.start()
    try:
        return probes(Spike(server))
    finally:
        server.stop()


def probes(sp: Spike) -> list[Fact]:
    adm = sp.admin()
    worker, coord, human, outsider = (sp.account(n) for n in ("worker.1", "coord.1", "alice", "mallory"))
    main = sp.room(adm, protocol.expected_power_levels([human]), [worker, coord, human])
    for user in (worker, coord, human):
        if sp.join(main, user)[0] != 200:
            raise ProbeError(f"{user} could not join the main room")
    reports = sp.room(adm, protocol.side_power_levels("reports", [human], open_=True), [human], (
        {"type": "m.room.join_rules", "state_key": "", "content": protocol.expected_join_rules("reports", main)},))
    questions = sp.room(adm, protocol.side_power_levels("questions", [human], open_=True), [human, coord])
    closed = sp.room(adm, protocol.side_power_levels("questions", [human], open_=False), [human])
    rooms = {"reports": (reports, True), "questions (open)": (questions, True), "questions (closed)": (closed, False)}
    return reports_facts(sp, worker, outsider, human, reports, questions) + invite_facts(
        sp, adm, worker, human, main, questions) + readback_facts(sp, adm, human, main, rooms)


def readback_facts(sp: Spike, adm: str, human: str, main: str, rooms: dict[str, tuple[str, bool]]) -> list[Fact]:
    """R5: the power levels and join rules as the server gives them back pass the exact checks
    `Syncer.verify_side` makes; a server that normalises either leaves every side room
    untrusted."""
    facts: list[Fact] = []
    for name, (room, open_) in rooms.items():
        purpose = name.split(" ")[0]
        for what, event_type, check in (
            ("power levels", "m.room.power_levels",
             lambda c, p=purpose, o=open_: protocol.check_side_power_levels(c, p, [human], o)),
            ("join rules", "m.room.join_rules",
             lambda c, p=purpose: protocol.check_join_rules(c, p, main)),
        ):
            content = sp.ok(f"read the {name} {what}", "GET", f"{CLIENT}/rooms/{q(room)}/state/{event_type}/",
                            token=sp.tokens[adm])
            try:
                check(content)
            except protocol.Untrusted as exc:
                facts.append((f"R5 the {name} {what} read back pass verify_side's check", False,
                              f"{exc}: {json.dumps(content, sort_keys=True)}"))
            else:
                facts.append((f"R5 the {name} {what} read back pass verify_side's check", True, "exact"))
    return facts


def reports_facts(sp: Spike, worker: str, outsider: str, human: str, reports: str, questions: str) -> list[Fact]:
    facts: list[Fact] = []
    code, err = sp.join(reports, worker)
    facts.append(("R1 a main-room member joins reports by the restricted rule", code == 200, f"{code} {err}"))
    code, err = sp.join(reports, outsider)
    facts.append(("R1 an account outside the main room cannot join reports", code == 403, f"{code} {err}"))
    code, err = sp.send(reports, worker, "m.room.message", {"msgtype": "m.notice", "body": "spike report"})
    facts.append(("R1 a joined agent posts a notice in reports (power 10)", code == 200, f"{code} {err}"))
    if sp.join(reports, human)[0] != 200 or sp.join(questions, human)[0] != 200:
        raise ProbeError("the human could not take its side-room invites")
    code, err = sp.send(reports, human, "m.room.message", {"msgtype": "m.text", "body": "spike"})
    facts.append(("R2 a power-0 human is refused m.room.message in reports", code == 403, f"{code} {err}"))
    msg, _ = sp.send(questions, human, "m.room.message", {"msgtype": "m.text", "body": "spike"})
    target = sp.last.get("event_id")
    if msg != 200 or not isinstance(target, str):
        raise ProbeError(f"the human could not post in questions ({msg}), so the reaction has no target")
    code, err = sp.send(questions, human, "m.reaction", {"m.relates_to": {
        "rel_type": "m.annotation", "event_id": target, "key": "+1"}})
    facts.append(("R3 a power-50 human is refused m.reaction in questions", code == 403, f"{code} {err}"))
    facts.append(("R3 a power-50 human posts a message in questions", msg == 200, str(msg)))
    return facts


def invite_facts(sp: Spike, adm: str, worker: str, human: str, main: str, questions: str) -> list[Fact]:
    sp.ok("invite the worker to questions", "POST", f"{CLIENT}/rooms/{q(questions)}/invite",
          {"user_id": worker}, sp.tokens[adm])
    since, seen, ok = None, False, True
    for n in range(SYNCS):
        if n == 1:
            sp.send(main, human, "m.room.message", {"msgtype": "m.text", "body": "spike main"})
        batch = sp.sync(worker, main, since)
        since = batch.get("next_batch")
        rooms = batch.get("rooms", {})
        ok = ok and isinstance(since, str) and (n > 0 or main in rooms.get("join", {}))
        seen = seen or questions in rooms.get("invite", {})
    state = sp.ok("read the worker's membership", "GET",
                  f"{CLIENT}/rooms/{q(questions)}/state/m.room.member/{q(worker)}", token=sp.tokens[adm])
    return [("R4 main-room syncs run while a questions invite is pending",
             ok and state.get("membership") == "invite", f"membership {state.get('membership')}"),
            ("R4 (recorded, no verdict) a sync filtered to the main room shows the invite",
             True, "shown" if seen else "not shown")]


def write_report(path: pathlib.Path, facts: list[Fact]) -> None:
    lines = ["# Plan 00211 spike report", "", f"Tuwunel: {os.path.realpath(BINARY)}", "",
             "| Fact | Holds | Observed |", "| --- | --- | --- |"]
    lines += [f"| {name} | {'yes' if holds else 'NO'} | {seen} |" for name, holds, seen in facts]
    lines += ["", VERDICT_PASS if all(holds for _, holds, _ in facts) else VERDICT_FAIL]
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print("usage: spike_probe.py <work dir> <report file>", file=sys.stderr)
        return 2
    work, report = pathlib.Path(argv[0]), pathlib.Path(argv[1])
    if not os.access(BINARY, os.X_OK):
        print(f"spike: {BINARY} is not installed: run the agent-bus play first", file=sys.stderr)
        return 2
    try:
        facts = run(work)
    except (ProbeError, admin.AdminError, KeyError) as exc:
        print(f"spike: a probe could not run: {type(exc).__name__}: {exc}", file=sys.stderr)
        return 2
    write_report(report, facts)
    for name, holds, seen in facts:
        print(f"{'HOLDS' if holds else 'FAILS'}\t{name}\t{seen}")
    return 0 if all(holds for _, holds, _ in facts) else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
