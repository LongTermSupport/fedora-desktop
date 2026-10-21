#!/usr/bin/python3
"""Plan 00213 Task 2.2: the parity oracle for `pingbus status-line` (SPEC-status-line.md).

    capture.py capture              write testdata/scenarios.json from scenarios() below, then
                                    record what the Python zipapp prints over each one
    capture.py check [--impl CMD]   run an implementation over testdata/ and compare it byte for
                                    byte with the record (default: the Python zipapp again)

testdata/ is src-go/internal/pingbus/statusline/golden/testdata. scenarios.json is literal and
language neutral: each scenario is a world of files (content, mode, age), an environment, a
stdin and a list of steps. A step can change files or the environment, sleep, or cancel the
render with a signal, and can name pid files whose processes must be gone afterwards. Every step
renders once, and the steps of a scenario share one world. A cache kept in that world (the Go
status line's) therefore meets a fresh-cache step and an invalidated-cache step, while the
Python, which keeps no cache, renders each step from scratch. That is the parity the cache must
keep. The Go golden test reads the same file. `{world}` stands for the world's root in the file
contents, environment and stdin, and replaces it in the recorded stdout and stderr.

`--impl` is the command, split as a shell would split it but never run through one; nothing is
added to it, e.g. `--impl "/tmp/pingbus-status-line"`. With no `--impl` the Python zipapp is built
from this checkout and run as `<pyz> status-line`, through its own shebang as Claude Code runs it.
Exit 0 when every step matches, 1 on a difference (each one listed on stderr), 2 when the oracle
itself cannot run.
"""

from __future__ import annotations

import argparse
import base64
import importlib
import json
import os
import pathlib
import shlex
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from collections.abc import Sequence

REPO = pathlib.Path(__file__).resolve().parents[4]
TESTDATA = REPO / "src-go/internal/pingbus/statusline/golden/testdata"
SCENARIOS_FILE = TESTDATA / "scenarios.json"
GOLDEN = TESTDATA / "golden"
WORLD = "{world}"
#: The file a hanging inner command creates once its pid files are written: a cancel waits for it.
STARTED = "inner-started"
STEP_TIMEOUT_S = 40.0
GONE_WAIT_S = 5.0


class OracleError(Exception):
    """The oracle cannot run (exit 2): a bad scenario file, a world that cannot be built."""


# ── the scenarios, written with builders; capture writes them out literal ─────────────────────


def file(path: str, content: str, mode: int = 0o600, age_s: float | None = None) -> dict:
    entry: dict = {"path": path, "kind": "file", "content": content, "mode": f"{mode:04o}"}
    if age_s is not None:
        entry["age_s"] = age_s
    return entry


def directory(path: str, mode: int = 0o700) -> dict:
    return {"path": path, "kind": "dir", "mode": f"{mode:04o}"}


def symlink(path: str, target: str) -> dict:
    return {"path": path, "kind": "symlink", "target": target}


def me(team: str) -> str:
    return f"@myrepo.1+workstation.podman:{team}.agent-bus.internal"


def member_json(team: str, override: dict[str, object] | None = None) -> str:
    server = f"{team}.agent-bus.internal"
    data: dict[str, object] = {
        "protocol": 1, "team": team, "user_id": me(team), "server_name": server,
        "base_url": "http://127.0.0.1:1", "plain_http_hosts": ["127.0.0.1"], "token_file": "token",
        "admin": f"@admin:{server}", "room": "!" + "R" * 43}
    data.update(override or {})
    return json.dumps(data)


def team_json(team: str, roles: dict[str, str], override: dict[str, object] | None = None) -> str:
    server = f"{team}.agent-bus.internal"
    data: dict[str, object] = {
        "v": 1, "team": team, "humans": [f"@alice:{server}"], "roles": roles,
        "repos": [{"repo": "example-org/myrepo", "branches": ["main"]}],
        "path_prefixes": ["docs/"], "forge_api": "https://api.github.com"}
    data.update(override or {})
    return json.dumps(data, separators=(",", ":"))


def roles_for(team: str, role: str) -> dict[str, str]:
    other = "worker" if role == "coordinator" else "coordinator"
    return {me(team): role, f"@orch.1+workstation.podman:{team}.agent-bus.internal": other}


def bundle(team: str, role: str = "worker") -> list[dict]:
    """A valid bundle for `team` in which this member has `role`, as the watcher leaves it."""
    base = f"pingbus/{team}"
    return [directory(base), file(f"{base}/member.json", member_json(team), 0o644),
            file(f"{base}/token", f"syt_member_token_for_{team}"), directory(f"{base}/state"),
            file(f"{base}/state/team.json", team_json(team, roles_for(team, role)))]


def record(teams: str, session: str = "sess-1", mode: int = 0o600) -> list[dict]:
    return [directory("sessions"), file(f"sessions/{session}", teams, mode)]


def settings(where: str, command: str | None) -> dict:
    body: dict[str, object] = {}
    if command is not None:
        body["statusLine"] = {"type": "command", "command": command}
    return file(where, json.dumps(body), 0o644)


def project_settings(command: str | None) -> dict:
    return settings("project/.claude/settings.json", command)


def user_settings(command: str) -> dict:
    return settings("home/.claude/settings.json", command)


def desired(seats: list[tuple[str, str]]) -> dict:
    return file("membership/membership.json", json.dumps({
        "v": 1, "session": "ccy-x-1", "kind": "ccy", "checkout": "/srv/x", "generation": 3, "written_ms": 1,
        "seats": [{"team": t, "seat": s} for t, s in seats], "farewell": []}), 0o644)


def applied(seats: list[tuple[str, str, str]]) -> dict:
    return file("pingbus/applied.json", json.dumps({
        "generation": 3, "refused": [], "v": 1,
        "seats": [{"handle": "myrepo.1+local.podman", "seat": s, "state": state, "team": t}
                  for t, s, state in seats]}))


def held(*seats: tuple[str, str]) -> dict:
    return applied([(t, s, "held") for t, s in seats])


def down(team: str, age_s: float = 0) -> list[dict]:
    return [directory("pingbus/down"), file(f"pingbus/down/{team}", "", 0o600, age_s)]


def payload(session: object = "sess-1", **extra: object) -> str:
    body: dict[str, object] = {"session_id": session, "workspace": {"project_dir": f"{WORLD}/project"}}
    body.update(extra)
    return json.dumps(body)


BASE_ENV = {"HOME": f"{WORLD}/home", "PATH": "/usr/bin:/bin", "LANG": "C.UTF-8",
            "PINGBUS_SESSIONS_DIR": f"{WORLD}/sessions"}
BUS_ENV = {**BASE_ENV, "PINGBUS_HOME": f"{WORLD}/pingbus", "PINGBUS_TEAMS": "alpha,beta"}
MEMBERSHIP_ENV = {**BASE_ENV, "PINGBUS_HOME": f"{WORLD}/pingbus", "PINGBUS_MEMBERSHIP": f"{WORLD}/membership"}
#: A two-team bus session: its record and both bundles (alpha coordinator, beta worker).
BUS_WORLD = [*record("alpha,beta"), directory("pingbus"), *bundle("alpha", "coordinator"), *bundle("beta")]
HANG = ("sleep 60 & echo $! > {world}/grandchild.pid; echo $$ > {world}/inner.pid; "
        ": > {world}/" + STARTED + "; wait")


def scenario(name: str, about: str, files: Sequence[dict], env: dict[str, str] | None = None,
             stdin: str | bytes | dict | None = None, steps: list[dict] | None = None) -> dict:
    world = [directory("project/.claude", 0o755), directory("home/.claude", 0o755), *files]
    entry: dict = {"name": name, "about": about, "files": world, "env": dict(BASE_ENV) if env is None else env}
    if isinstance(stdin, bytes):
        entry["stdin_b64"] = base64.b64encode(stdin).decode()
    elif isinstance(stdin, dict):
        entry["stdin_repeat"] = stdin  # `text` repeated `count` times: a large input kept small here
    else:
        entry["stdin"] = payload() if stdin is None else stdin
    entry["steps"] = [{}] if steps is None else steps
    return entry


def bus(name: str, about: str, extra: Sequence[dict] = (), *, env: dict[str, str] | None = None,
        steps: list[dict] | None = None, stdin: str | bytes | dict | None = None,
        drop: Sequence[str] = ()) -> dict:
    """A scenario on BUS_WORLD: `extra` is placed after it (a later entry replaces an earlier one
    at its path) and `drop` takes paths out of it."""
    files = [entry for entry in [*BUS_WORLD, *extra] if entry["path"] not in drop]
    return scenario(name, about, files, dict(BUS_ENV) if env is None else env, stdin, steps)


def inner_scenarios() -> list[dict]:
    hang = [project_settings(f"sh {WORLD}/hang.sh"), file("hang.sh", HANG, 0o755)]
    gone = ["inner.pid", "grandchild.pid"]
    return [
        scenario("plain-nothing", "no bus, no settings: an empty line", []),
        scenario("inner-only", "the project's status line, nothing else", [project_settings("echo inner")]),
        scenario("inner-reads-the-payload", "the inner gets the same stdin", [project_settings("cat")]),
        scenario("inner-environment", "the inner gets this environment plus PINGBUS_STATUS_LINE_INNER=1",
                 # Named, not `env`: bash as /bin/sh (Fedora) exports SHLVL and _, dash does not.
                 [project_settings("for v in HOME LANG PATH PINGBUS_SESSIONS_DIR PINGBUS_STATUS_LINE_INNER "
                                   "CLAUDE_PROJECT_DIR COLUMNS; do printf '%s=%s\\n' \"$v\" \"$(printenv \"$v\")\"; "
                                   "done")]),
        scenario("inner-cwd-is-the-project", "the inner runs in the project directory", [project_settings("pwd")]),
        scenario("inner-cwd-missing-project", "a project dir that is not a directory: the inner runs here",
                 [user_settings("pwd")],
                 stdin=json.dumps({"session_id": "s", "workspace": {"project_dir": f"{WORLD}/nowhere"}})),
        scenario("settings-local-wins", "settings.local.json, then settings.json, then the user's",
                 [settings("project/.claude/settings.local.json", "echo local"), project_settings("echo project"),
                  user_settings("echo user")]),
        scenario("settings-user-only", "the user's settings when the project's set no status line",
                 [project_settings(None), user_settings("echo user")]),
        scenario("settings-claude-config-dir", "CLAUDE_CONFIG_DIR moves the user's settings",
                 [settings("config/settings.json", "echo config"), user_settings("echo home")],
                 env={**BASE_ENV, "CLAUDE_CONFIG_DIR": f"{WORLD}/config"}),
        scenario("settings-relative-home", "a relative HOME gives no user settings",
                 [user_settings("echo user")], env={**BASE_ENV, "HOME": "home"}),
        scenario("settings-never-itself", "a status line that is this command is passed over",
                 [project_settings(" pingbus status-line "), user_settings("echo user")]),
        scenario("settings-blank-command", "a blank command is passed over",
                 [project_settings("   "), user_settings("echo user")]),
        scenario("settings-not-an-object", "a JSON array is no status line, passed over",
                 [file("project/.claude/settings.json", "[]", 0o644), user_settings("echo user")]),
        scenario("settings-not-json", "a settings file that is not JSON is said in red",
                 [file("project/.claude/settings.json", "{", 0o644)]),
        scenario("settings-unreadable", "a settings path that cannot be read as a file is said in red",
                 [directory("project/.claude/settings.json", 0o755)]),
        scenario("settings-not-utf8", "a settings file that is not UTF-8 is said in red",
                 [{"path": "project/.claude/settings.json", "kind": "file",
                   "content_b64": base64.b64encode(b'{"a": "\xff"}').decode(), "mode": "0644"}]),
        scenario("project-dir-from-variable", "CLAUDE_PROJECT_DIR when the payload names none",
                 [project_settings("echo project")], env={**BASE_ENV, "CLAUDE_PROJECT_DIR": f"{WORLD}/project"},
                 stdin=json.dumps({"session_id": "sess-1"})),
        scenario("project-dir-from-cwd", "the payload's cwd last",
                 [project_settings("echo project")], stdin=json.dumps({"session_id": "s", "cwd": f"{WORLD}/project"})),
        scenario("project-dir-relative-ignored", "a relative project dir is no project",
                 [project_settings("echo project"), user_settings("echo user")],
                 stdin=json.dumps({"workspace": {"project_dir": "project"}, "cwd": "project"})),
        scenario("inner-exit-with-text", "a non-zero exit is shown in red after the text",
                 [project_settings("echo partial; exit 3")]),
        scenario("inner-exit-no-text", "a non-zero exit with no text", [project_settings("exit 7")]),
        scenario("inner-killed-by-a-signal", "an inner killed by a signal reports a negative status",
                 [project_settings("kill -TERM $$")]),
        scenario("inner-trailing-newlines", "trailing newlines are dropped, inner rows kept",
                 [project_settings("printf 'one\\ntwo\\n\\n\\n'")]),
        scenario("inner-invalid-utf8", "undecodable inner bytes become U+FFFD", [project_settings("printf 'a\\377b'")]),
        scenario("inner-wide-text-untouched", "emoji and CJK in the inner pass through unmeasured",
                 [project_settings("printf '\\360\\237\\244\\226 \\346\\274\\242\\345\\255\\227 ok'")],
                 env={**BASE_ENV, "COLUMNS": "8"}),
        scenario("inner-stderr-passes-through", "the inner's stderr is the status line's stderr",
                 [project_settings("echo to-err >&2; echo shown")]),
        scenario("inner-var-skips-the-inner", "PINGBUS_STATUS_LINE_INNER set: no inner is run",
                 [project_settings("echo inner")], env={**BASE_ENV, "PINGBUS_STATUS_LINE_INNER": "1"}),
        scenario("inner-timeout-kills-the-group", "a hung inner is cut at 10 s and its whole group killed",
                 hang, steps=[{"gone": gone}]),
        *(scenario(f"cancel-{name.lower()}", f"{name} while the inner runs: exit 128+n, its group killed",
                   hang, steps=[{"signal": name, "gone": gone}])
          for name in ("SIGTERM", "SIGINT", "SIGHUP")),
    ]


def segment_scenarios() -> list[dict]:
    beta_files = ("pingbus/beta", "pingbus/beta/member.json", "pingbus/beta/token", "pingbus/beta/state",
                  "pingbus/beta/state/team.json")
    return [
        bus("bus-two-teams-with-roles", "the record's teams in order, each with this member's role"),
        bus("bus-with-inner", "the inner first, the segment on a row of its own", [project_settings("echo inner")]),
        bus("bus-legacy-role", "a legacy role spelling reads as its role",
            [file("pingbus/beta/state/team.json", team_json("beta", {me("beta"): "orchestrator"}))]),
        scenario("bus-no-record", "a bus session with no record says so in red", [directory("sessions")],
                 env={**BASE_ENV, "PINGBUS_TEAMS": "alpha"}),
        scenario("bus-no-record-no-bus", "no record and no PINGBUS_TEAMS: no segment", [directory("sessions")]),
        bus("bus-record-mode-0644", "a record others could read is not trusted", record("alpha,beta", mode=0o644)),
        bus("bus-record-symlink", "a symlinked record is not followed",
            [file("elsewhere", "alpha,beta"), symlink("sessions/sess-1", f"{WORLD}/elsewhere")]),
        bus("bus-record-duplicate", "a record naming a team twice is unusable", record("alpha,alpha")),
        bus("bus-record-bad-name", "a record with a name that is not a team name", record("alpha,Beta")),
        bus("bus-record-trailing-newline", "a record with a newline is unusable", record("alpha,beta\n")),
        bus("bus-record-too-large", "a record over 4096 bytes", record(",".join(["a"] * 2049))),
        bus("bus-record-other-session", "another session's record is never shown", stdin=payload("sess-2")),
        bus("bus-session-id-not-a-name", "a session id with a slash reads no record", stdin=payload("../sess-1")),
        bus("bus-session-id-not-a-string", "a session id that is a number reads no record", stdin=payload(7)),
        *(bus(f"bus-payload-{label}", "input that is not a JSON object reads no record", stdin=raw)
          for label, raw in (("empty", b""), ("array", b"[]"), ("not-utf8", b"\xff"),
                             ("too-large", {"text": " ", "count": (1 << 20) + 1}))),
        bus("role-no-bundle", "a team with no bundle is (role?) with one stderr line", drop=beta_files),
        bus("role-no-team-record", "a bundle with no cached team record", drop=["pingbus/beta/state/team.json"]),
        bus("role-team-record-not-json", "a team record that is not JSON", [file("pingbus/beta/state/team.json", "{")]),
        bus("role-team-record-invalid", "a team record that fails its checks (no repository, no org)",
            [file("pingbus/beta/state/team.json", team_json("beta", roles_for("beta", "worker"), {"repos": []}))]),
        bus("role-team-record-other-team", "a team record for another team",
            [file("pingbus/beta/state/team.json", team_json("beta", roles_for("beta", "worker"), {"team": "alpha"}))]),
        bus("role-team-record-symlink", "a team record that is a symlink",
            [symlink("pingbus/beta/state/team.json", f"{WORLD}/pingbus/alpha/state/team.json")]),
        bus("role-not-this-member", "a team record that gives this member no role",
            [file("pingbus/beta/state/team.json",
                  team_json("beta", {"@orch.1+workstation.podman:beta.agent-bus.internal": "coordinator"}))]),
        bus("role-member-unknown-key", "a member.json with an unknown key",
            [file("pingbus/beta/member.json", member_json("beta", {"extra": 1}), 0o644)]),
        bus("role-member-wrong-team", "a member.json for another team",
            [file("pingbus/beta/member.json", member_json("beta", {"team": "alpha"}), 0o644)]),
        bus("role-member-duplicate-key", "a member.json with a key twice",
            [file("pingbus/beta/member.json", member_json("beta")[:-1] + ', "team": "beta"}', 0o644)]),
        bus("role-member-bad-base-url", "a member.json whose base_url is plain http to a host not listed",
            [file("pingbus/beta/member.json", member_json("beta", {"base_url": "http://192.0.2.1:1"}), 0o644)]),
        bus("role-token-too-open", "a token file readable by others",
            [file("pingbus/beta/token", "syt_member_token_for_beta", 0o644)]),
        bus("role-token-missing", "no token file", drop=["pingbus/beta/token"]),
        bus("role-token-newline", "a token with a trailing newline", [file("pingbus/beta/token", "syt_x\n")]),
        bus("role-every-team-unreadable", "PINGBUS_HOME relative: no team's role can be read",
            env={**BUS_ENV, "PINGBUS_HOME": "pingbus"}),
        bus("role-home-from-xdg", "PINGBUS_HOME unset: the XDG config home's pingbus",
            [directory("xdg"), symlink("xdg/pingbus", f"{WORLD}/pingbus")],
            env={k: v for k, v in {**BUS_ENV, "XDG_CONFIG_HOME": f"{WORLD}/xdg"}.items() if k != "PINGBUS_HOME"}),
        scenario("membership-desired-order", "the seats held that the desired file names, in desired order",
                 [desired([("beta", "2"), ("alpha", "1")]), directory("pingbus"),
                  held(("alpha", "1"), ("beta", "2"), ("gamma", "1")), *bundle("alpha"), *bundle("beta", "coordinator")],
                 env=dict(MEMBERSHIP_ENV)),
        scenario("membership-not-held", "a seat desired but free is not shown",
                 [desired([("alpha", "1"), ("beta", "2")]), directory("pingbus"), *bundle("alpha"),
                  applied([("alpha", "1", "held"), ("beta", "2", "free")])], env=dict(MEMBERSHIP_ENV)),
        scenario("membership-other-seat", "the same team on another seat is not shown",
                 [desired([("alpha", "1")]), directory("pingbus"), *bundle("alpha"), held(("alpha", "9"))],
                 env=dict(MEMBERSHIP_ENV)),
        scenario("membership-no-applied-yet", "before the keeper writes applied.json: no segment",
                 [desired([("alpha", "1")]), directory("pingbus")], env=dict(MEMBERSHIP_ENV)),
        scenario("membership-unreadable", "a desired file that is not JSON is said in red",
                 [file("membership/membership.json", "{", 0o644), directory("pingbus")], env=dict(MEMBERSHIP_ENV)),
        scenario("membership-applied-unreadable", "an applied file that is not v1 is said in red",
                 [desired([("alpha", "1")]), directory("pingbus"), file("pingbus/applied.json", '{"v": 2, "seats": []}')],
                 env=dict(MEMBERSHIP_ENV)),
        bus("membership-off-uses-the-record", "PINGBUS_MEMBERSHIP=off reads the record",
            env={**BUS_ENV, "PINGBUS_MEMBERSHIP": "off"}),
        bus("membership-relative-uses-the-record", "a relative PINGBUS_MEMBERSHIP reads the record",
            env={**BUS_ENV, "PINGBUS_MEMBERSHIP": "membership"}),
        bus("down-fresh", "a fresh mark: (down) after the role", down("beta")),
        bus("down-stale", "a mark older than 300 s is not believed", down("beta", 301)),
        bus("down-symlink", "a mark that is not a regular file is ignored",
            [directory("pingbus/down"), symlink("pingbus/down/beta", f"{WORLD}/pingbus/beta/token")]),
        bus("down-role-unknown", "the marks come after (role?)",
            [*down("beta"), file("pingbus/beta/state/team.json", "{")]),
    ]


def width_scenarios() -> list[dict]:
    def world_of(teams: list[str]) -> tuple[list[dict], dict[str, str]]:
        files = [*record(",".join(teams)), directory("pingbus"), *(entry for team in teams for entry in bundle(team))]
        return files, {**BUS_ENV, "PINGBUS_TEAMS": ",".join(teams)}

    # `👥 aa (worker), bb (worker), cc (worker)` is 40 columns: the label is 2, the rest ASCII.
    world, env = world_of(["aa", "bb", "cc"])
    wide_world, wide_env = world_of(["alpha-team-number-one", "beta-team-number-two", "gamma-team-three"])
    return [
        scenario("width-wraps-after-a-comma", "teams wrap at COLUMNS, indented under the label",
                 world, env={**env, "COLUMNS": "27"}),
        scenario("width-team-wider-than-the-terminal", "a piece wider than COLUMNS keeps its own row whole",
                 wide_world, env={**wide_env, "COLUMNS": "10"}),
        scenario("width-exact-fit", "a row exactly COLUMNS wide is not broken", world, env={**env, "COLUMNS": "40"}),
        scenario("width-one-short", "a row one column too wide is broken", world, env={**env, "COLUMNS": "39"}),
        scenario("width-leading-zeros", "COLUMNS=040 is 40", world, env={**env, "COLUMNS": "040"}),
        scenario("width-note-wraps-between-words", "a red note wraps between words",
                 [directory("sessions")], env={**BASE_ENV, "PINGBUS_TEAMS": "alpha", "COLUMNS": "20"}),
        *(scenario(f"width-unusable-{label}", "an unusable COLUMNS: one unbroken row", wide_world,
                   env={**wide_env, "COLUMNS": value})
          for label, value in (("zero", "0"), ("word", "wide"), ("seven-digits", "1234567"), ("signed", "+40"),
                               ("empty", ""), ("space", " 40"))),
    ]


def cache_scenarios() -> list[dict]:
    beta_record = "pingbus/beta/state/team.json"
    return [
        bus("cache-fresh-repeat", "nothing changed: the same output three times", [project_settings("echo inner")],
            steps=[{}, {}, {}]),
        bus("cache-inner-is-never-cached", "the inner runs on every render while the segment holds",
            [project_settings(f"cat {WORLD}/counter"), file("counter", "first")],
            steps=[{}, {"files": [file("counter", "second")]}]),
        bus("cache-settings-change", "a new inner command shows at once",
            [project_settings("echo one")], steps=[{}, {"files": [project_settings("echo two")]}]),
        bus("cache-record-change", "a rewritten session record shows at once", steps=[{}, {"files": record("beta")}]),
        bus("cache-role-change", "a role change in the team record shows at once",
            steps=[{}, {"files": [file(beta_record, team_json("beta", roles_for("beta", "coordinator")))]}]),
        bus("cache-role-same-size-change", "a role swapped for one of the same length shows at once",
            [file(beta_record, team_json("beta", {me("beta"): "worker"}))],
            steps=[{}, {"files": [file(beta_record, team_json("beta", {me("beta"): "wurker"}))]}]),
        bus("cache-role-becomes-unreadable", "a team record removed shows (role?) at once",
            steps=[{}, {"remove": [beta_record]}]),
        bus("cache-token-mode-change", "a token made readable by others shows (role?) at once",
            steps=[{}, {"files": [file("pingbus/beta/token", "syt_member_token_for_beta", 0o644)]}]),
        bus("cache-member-json-change", "a member.json broken between renders shows (role?) at once",
            steps=[{}, {"files": [file("pingbus/beta/member.json", "{", 0o644)]}]),
        bus("cache-down-appears-and-clears", "a mark made, then removed, shows and clears at once",
            steps=[{}, {"files": down("alpha")}, {"remove": ["pingbus/down/alpha"]}]),
        # Deterministic: step 2 sets the mark's mtime to 301 s old instead of sleeping. A mark that
        # ages out with no file changing is the cache's expiry (SPEC S34), a Go unit test's job.
        bus("cache-down-ages-out", "a mark re-dated past 300 s between renders stops counting",
            down("alpha", 200), steps=[{}, {"files": [file("pingbus/down/alpha", "", 0o600, 301)]}]),
        bus("cache-bundle-appears", "a bundle that appears between renders shows its role at once",
            record("alpha,gamma"), env={**BUS_ENV, "PINGBUS_TEAMS": "alpha,gamma"},
            steps=[{}, {"files": bundle("gamma", "coordinator")}]),
        bus("cache-width-change", "a new COLUMNS rewraps at once (the environment is not on disk)",
            steps=[{}, {"env": {"COLUMNS": "20"}}]),
        bus("cache-session-change", "another session id in the payload reads its own record",
            record("beta", session="sess-2"), steps=[{}, {"stdin": payload("sess-2")}]),
        scenario("cache-membership-change", "a seat added, then one dropped, shows at once",
                 [desired([("alpha", "1")]), directory("pingbus"), held(("alpha", "1")),
                  *bundle("alpha"), *bundle("beta", "coordinator")], env=dict(MEMBERSHIP_ENV),
                 steps=[{}, {"files": [desired([("alpha", "1"), ("beta", "2")]), held(("alpha", "1"), ("beta", "2"))]},
                        {"files": [held(("beta", "2"))]}]),
    ]


def scenarios() -> list[dict]:
    return [*inner_scenarios(), *segment_scenarios(), *width_scenarios(), *cache_scenarios()]


# ── running a world ──────────────────────────────────────────────────────────────────────────


def _expand(text: str, root: pathlib.Path) -> str:
    return text.replace(WORLD, str(root))


def _place(root: pathlib.Path, entry: dict, now: float) -> None:
    path = root / entry["path"]
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o755)
    kind = entry["kind"]
    if path.is_symlink() or (path.exists() and not path.is_dir()):
        path.unlink()
    if kind == "dir":
        path.mkdir(exist_ok=True)
        path.chmod(int(entry["mode"], 8))
        return
    if kind == "symlink":
        path.symlink_to(_expand(entry["target"], root))
        return
    if kind != "file":
        raise OracleError(f"{entry['path']}: unknown kind {kind!r}")
    data = (base64.b64decode(entry["content_b64"]) if "content_b64" in entry
            else _expand(entry["content"], root).encode("utf-8"))
    path.write_bytes(data)
    path.chmod(int(entry["mode"], 8))
    when = now - float(entry.get("age_s", 0))
    os.utime(path, (when, when))


def _remove(root: pathlib.Path, rel: str) -> None:
    path = root / rel
    if path.is_dir() and not path.is_symlink():
        shutil.rmtree(path)
    else:
        path.unlink()


def _alive(pid: int) -> bool:
    try:
        stat = pathlib.Path(f"/proc/{pid}/stat").read_text(encoding="ascii")
    except (FileNotFoundError, ProcessLookupError):
        return False
    return stat.rsplit(")", 1)[1].split()[0] != "Z"


def _gone(root: pathlib.Path, pidfiles: Sequence[str]) -> list[str]:
    """The pid files whose process still runs after GONE_WAIT_S; a missing pid file is a failure."""
    problems = []
    for rel in pidfiles:
        try:
            pid = int((root / rel).read_text(encoding="ascii"))
        except (OSError, ValueError):
            problems.append(f"{rel}: no pid was written")
            continue
        deadline = time.monotonic() + GONE_WAIT_S
        while _alive(pid) and time.monotonic() < deadline:
            time.sleep(0.05)
        if _alive(pid):
            problems.append(f"{rel}: process {pid} outlived the render")
            os.kill(pid, signal.SIGKILL)
    return problems


def _run_step(argv: Sequence[str], root: pathlib.Path, env: dict[str, str], stdin: bytes,
              step: dict) -> tuple[bytes, bytes, int]:
    proc = subprocess.Popen(list(argv), stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            env=env, cwd=root)
    name = step.get("signal")
    if name is None:
        out, err = proc.communicate(stdin, timeout=STEP_TIMEOUT_S)
        return out, err, proc.returncode
    if proc.stdin is None:
        raise OracleError("no stdin pipe")
    note = b""
    try:
        proc.stdin.write(stdin)
        proc.stdin.close()
    except BrokenPipeError:
        # Judged by what it printed: the note makes the stderr differ, and the exit will too.
        note = b"[oracle: the implementation did not read its stdin]\n"
    proc.stdin = None
    deadline = time.monotonic() + STEP_TIMEOUT_S
    while not (root / STARTED).exists():
        if proc.poll() is not None or time.monotonic() > deadline:
            # The implementation ended, or hung, before its inner started: no signal is sent,
            # and what it did is compared (its exit cannot be the cancelled 128+n).
            proc.kill()
            out, err = proc.communicate()
            return out, err + note + b"[oracle: the inner never started, no signal sent]\n", proc.returncode
        time.sleep(0.02)
    proc.send_signal(getattr(signal, name))
    out, err = proc.communicate(timeout=STEP_TIMEOUT_S)
    return out, err + note, proc.returncode


def _stdin(holder: dict, root: pathlib.Path) -> bytes:
    if "stdin_b64" in holder:
        return base64.b64decode(holder["stdin_b64"])
    if "stdin_repeat" in holder:
        return holder["stdin_repeat"]["text"].encode("utf-8") * int(holder["stdin_repeat"]["count"])
    return _expand(holder["stdin"], root).encode("utf-8")


def run_scenario(argv: Sequence[str], item: dict) -> list[tuple[bytes, bytes, int, list[str]]]:
    """Each step's (stdout, stderr, exit, gone-problems), the world root replaced by {world}."""
    root = pathlib.Path(tempfile.mkdtemp(prefix="pbsl-"))
    try:
        now = time.time()
        for entry in item["files"]:
            _place(root, entry, now)
        env = {key: _expand(value, root) for key, value in item["env"].items()}
        stdin = _stdin(item, root)
        results = []
        for step in item["steps"]:
            time.sleep(float(step.get("sleep_s", 0)))
            for rel in step.get("remove", []):
                _remove(root, rel)
            step_now = time.time()
            for entry in step.get("files", []):
                _place(root, entry, step_now)
            env.update({key: _expand(value, root) for key, value in step.get("env", {}).items()})
            if "stdin" in step or "stdin_b64" in step:
                stdin = _stdin(step, root)
            (root / STARTED).unlink(missing_ok=True)
            out, err, code = _run_step(argv, root, env, stdin, step)
            marker = str(root).encode()
            results.append((out.replace(marker, WORLD.encode()), err.replace(marker, WORLD.encode()), code,
                            _gone(root, step.get("gone", []))))
        return results
    finally:
        shutil.rmtree(root, ignore_errors=True)


def python_impl(scratch: pathlib.Path) -> list[str]:
    """The Python zipapp built from this checkout, as the ccy play builds it."""
    sys.path.insert(0, str(REPO))
    bundle_module = importlib.import_module("helpers.pingbus.bundle")
    archive = scratch / "pingbus.pyz"
    archive.write_bytes(bundle_module.build("pingbus", REPO))
    archive.chmod(0o755)
    return [str(archive), "status-line"]


def load_scenarios() -> list[dict]:
    try:
        data = json.loads(SCENARIOS_FILE.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise OracleError(f"{SCENARIOS_FILE} cannot be read: {exc}") from None
    if not isinstance(data, dict) or data.get("v") != 1 or not isinstance(data.get("scenarios"), list):
        raise OracleError(f"{SCENARIOS_FILE} is not a v1 scenario file")
    return data["scenarios"]


def _expected_exit(step: dict) -> int:
    return 128 + getattr(signal, step["signal"]) if "signal" in step else 0


def capture(argv: Sequence[str]) -> int:
    items = scenarios()
    names = [item["name"] for item in items]
    if len(set(names)) != len(names):
        raise OracleError("two scenarios share a name")
    TESTDATA.mkdir(parents=True, exist_ok=True)
    # One scenario per line: small, and a diff still names the scenario that changed.
    lines = ",\n".join(json.dumps(item, ensure_ascii=False, separators=(",", ":")) for item in items)
    SCENARIOS_FILE.write_text(f'{{"v":1,"scenarios":[\n{lines}\n]}}\n', encoding="utf-8")
    if GOLDEN.exists():
        shutil.rmtree(GOLDEN)
    for item in load_scenarios():
        where = GOLDEN / item["name"]
        where.mkdir(parents=True)
        for number, (out, err, code, problems) in enumerate(run_scenario(argv, item), start=1):
            label = f"{item['name']} step {number}"
            if problems:
                raise OracleError(f"{label}: the Python failed the scenario's own check: {problems}")
            if code != _expected_exit(item["steps"][number - 1]):
                raise OracleError(f"{label}: the Python exited {code}")
            (where / f"{number}.stdout").write_bytes(out)
            (where / f"{number}.stderr").write_bytes(err)
            (where / f"{number}.exit").write_text(f"{code}\n", encoding="ascii")
        print(f"captured {item['name']}", file=sys.stderr)
    return 0


def check(argv: Sequence[str], ignore_stderr: bool) -> int:
    failures = steps = 0
    for item in load_scenarios():
        where = GOLDEN / item["name"]
        for number, (out, err, code, problems) in enumerate(run_scenario(argv, item), start=1):
            label = f"{item['name']} step {number}"
            steps += 1
            try:
                want_out = (where / f"{number}.stdout").read_bytes()
                want_err = (where / f"{number}.stderr").read_bytes()
                want_code = int((where / f"{number}.exit").read_text(encoding="ascii"))
            except (OSError, ValueError) as exc:
                raise OracleError(f"{label}: no record ({exc}); run capture") from None
            found = list(problems)
            if out != want_out:
                found.append(f"stdout {out!r}, recorded {want_out!r}")
            if not ignore_stderr and err != want_err:
                found.append(f"stderr {err!r}, recorded {want_err!r}")
            if code != want_code:
                found.append(f"exit {code}, recorded {want_code}")
            for problem in found:
                print(f"DIFF {label}: {problem}", file=sys.stderr)
            failures += bool(found)
    if steps == 0:
        raise OracleError("no scenario step was run")
    print(f"{'FAIL' if failures else 'PASS'}: {steps} step(s), {failures} differ", file=sys.stderr)
    return 1 if failures else 0


def main(args: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="capture.py", description="The pingbus status-line parity oracle.")
    sub = parser.add_subparsers(dest="mode", required=True)
    sub.add_parser("capture", help="write scenarios.json and record the Python's output")
    checker = sub.add_parser("check", help="compare an implementation with the record")
    checker.add_argument("--impl", help="the command to run, whole (default: the Python zipapp, status-line)")
    checker.add_argument("--ignore-stderr", action="store_true", help="compare stdout and the exit code only")
    parsed = parser.parse_args(args)
    try:
        with tempfile.TemporaryDirectory(prefix="pbsl-impl-") as scratch:
            impl = getattr(parsed, "impl", None)
            argv = shlex.split(impl) if impl else python_impl(pathlib.Path(scratch))
            if parsed.mode == "capture":
                return capture(argv)
            return check(argv, parsed.ignore_stderr)
    except (OracleError, subprocess.TimeoutExpired) as exc:
        print(f"capture.py: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
