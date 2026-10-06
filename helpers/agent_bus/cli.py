"""The `agent-bus` command line: team administration on the homeserver host.

Plan 00161's DESIGN.md section 4 lists the commands; `render` is the installer's
(sections 3.4, 3.6, 3.3). Run as the `agent-bus` user through the root wrapper
`files/usr/local/bin/agent-bus`, which places a bundle under `--out` itself: this tool
never takes `--out` and writes a bundle only as a tar on stdout, never to a terminal.
Every value comes from arguments (`--opt=value`) or, for `render`, the team file on
stdin; nothing prompts.

Streams (CLAUDE/StderrHygiene.md): stdout is the payload only: `CHANGED <what>` marker
lines, `list` / `human devices` lines, the `render` output, the bundle tar, or the
password `human password` prints once. Diagnostics go to stderr and hold no secret.

Exit codes: 0 done; 64 usage; 69 homeserver unreachable; 70 refused (by the homeserver,
or because the team's state does not allow it); 78 configuration (team file, team
directory, secret files, registry).
"""

from __future__ import annotations

import argparse
import os
import pathlib
import sys
from collections.abc import Callable, Mapping, Sequence
from typing import BinaryIO, TextIO

from helpers.agent_bus import admin, registry, render, teamfile
from helpers.pingbus import protocol

PROG = "agent-bus"
TOOL_VERSION = "0.1.0"
STATE_ROOT = pathlib.Path(render.STATE_ROOT)
ROLE_VAR = "HOOKS_DAEMON_HOSTNAME"

EXIT_OK = 0
EXIT_USAGE = 64
EXIT_UNREACHABLE = 69
EXIT_REFUSED = 70
EXIT_CONFIG = 78

HUMAN_ACTIONS = ("password", "devices", "logout-all", "lock", "unlock")


class UsageError(Exception):
    """Bad arguments: exit 64."""


class _ParserExit(Exception):
    def __init__(self, status: int) -> None:
        super().__init__(status)
        self.status = status


class _Io:
    def __init__(self, stdin: TextIO, stdout: BinaryIO, stderr: TextIO) -> None:
        self.stdin, self.stdout, self.stderr = stdin, stdout, stderr

    def lines(self, lines: Sequence[str]) -> None:
        self.stdout.write("".join(f"{line}\n" for line in lines).encode())

    def say(self, text: str) -> None:
        self.stderr.write(f"{PROG}: {text}\n")

    def tar(self, data: bytes) -> None:
        self.stdout.write(data)


def _changed(changes: Sequence[str]) -> list[str]:
    return [f"CHANGED\t{change}" for change in changes]


def _refuse_terminal(io: _Io) -> None:
    if io.stdout.isatty():
        raise UsageError("the bundle is a tar on stdout, not for a terminal: run sudo agent-bus "
                         "with --out=DIR, which places it")


# ---------------------------------------------------------------- team commands


def cmd_bootstrap(args: argparse.Namespace, team: admin.Team, transport: admin.Transport,
                  environ: Mapping[str, str], io: _Io) -> int:
    io.lines(_changed(admin.bootstrap(team, transport)))
    return EXIT_OK


def cmd_add_member(args: argparse.Namespace, team: admin.Team, transport: admin.Transport,
                   environ: Mapping[str, str], io: _Io) -> int:
    _refuse_terminal(io)
    try:
        host = registry.resolve_host(args.host, environ.get(ROLE_VAR) or None)
    except registry.HandleError as exc:
        raise UsageError(str(exc)) from None
    bundle = admin.add_member(team, transport, repo=args.repo, host=host, type_=args.type,
                              role=args.role, address=args.address, human_text=args.human_text)
    io.tar(bundle.tar)
    io.say(f"added {bundle.handle} to team {team.name} as {args.role}")
    return EXIT_OK


def cmd_remove_member(args: argparse.Namespace, team: admin.Team, transport: admin.Transport,
                      environ: Mapping[str, str], io: _Io) -> int:
    io.lines(_changed(admin.remove_member(team, transport, args.handle)))
    return EXIT_OK


def cmd_set_role(args: argparse.Namespace, team: admin.Team, transport: admin.Transport,
                 environ: Mapping[str, str], io: _Io) -> int:
    io.lines(_changed(admin.set_role(team, transport, args.handle, args.role)))
    return EXIT_OK


def cmd_rotate_token(args: argparse.Namespace, team: admin.Team, transport: admin.Transport,
                     environ: Mapping[str, str], io: _Io) -> int:
    _refuse_terminal(io)
    io.tar(admin.rotate_token(team, transport, args.handle))
    io.say(f"{args.handle}: every device logged out, a new token written")
    return EXIT_OK


def cmd_list(args: argparse.Namespace, team: admin.Team, transport: admin.Transport,
             environ: Mapping[str, str], io: _Io) -> int:
    io.lines(admin.list_members(team, transport))
    return EXIT_OK


def cmd_human(args: argparse.Namespace, team: admin.Team, transport: admin.Transport,
              environ: Mapping[str, str], io: _Io) -> int:
    if args.action == "password":
        io.lines([admin.human_password(team, transport, args.name)])
        io.say(f"{args.name}: new password set and printed once; it is stored nowhere, and "
               "every other session was logged out")
    elif args.action == "devices":
        io.lines(admin.human_devices(team, transport, args.name))
    elif args.action == "logout-all":
        admin.human_logout_all(team, transport, args.name)
        io.say(f"{args.name}: every session ended; run human password for a new password")
    else:
        admin.human_lock(team, transport, args.name, args.action == "lock")
        io.say(f"{args.name}: account {args.action}ed")
    return EXIT_OK


# ---------------------------------------------------------------- render


def _stdin_team_file(io: _Io) -> teamfile.TeamFile:
    try:
        data = teamfile.decode_strict_json(io.stdin.read())
    except ValueError as exc:
        raise teamfile.TeamFileError(f"team file: not valid JSON: {exc}") from None
    return teamfile.parse_team_file(data)


def cmd_render_check(args: argparse.Namespace, environ: Mapping[str, str], io: _Io) -> int:
    tf = _stdin_team_file(io)
    previous = None
    if args.previous is not None:
        try:
            previous = teamfile.load_team_file(pathlib.Path(args.previous))
        except FileNotFoundError:
            previous = None
    render.check_unchanged(tf, previous)
    io.stdout.write(teamfile.dump_team_file(tf).encode())
    return EXIT_OK


def cmd_render_toml(args: argparse.Namespace, environ: Mapping[str, str], io: _Io) -> int:
    io.stdout.write(render.render_toml(_stdin_team_file(io)).encode())
    return EXIT_OK


def cmd_render_dropin(args: argparse.Namespace, environ: Mapping[str, str], io: _Io) -> int:
    interfaces = render.parse_interfaces(args.interface or [])
    io.stdout.write(render.render_dropin(_stdin_team_file(io), interfaces).encode())
    return EXIT_OK


def cmd_version(args: argparse.Namespace, environ: Mapping[str, str], io: _Io) -> int:
    io.lines([f"{PROG} {TOOL_VERSION} protocol {protocol.PROTOCOL_VERSION}"])
    return EXIT_OK


# ---------------------------------------------------------------- dispatch


def build_parser(stdout: BinaryIO) -> argparse.ArgumentParser:
    class Parser(argparse.ArgumentParser):
        def error(self, message: str) -> None:
            raise UsageError(f"{self.prog}: {message}")

        def exit(self, status: int = 0, message: str | None = None) -> None:
            if message:
                raise UsageError(message.strip())
            raise _ParserExit(status)

        def print_help(self, file: TextIO | None = None) -> None:
            stdout.write(self.format_help().encode())

    parser = Parser(prog=PROG, description="Agent team bus administration (run as sudo agent-bus).")
    commands = parser.add_subparsers(dest="command", metavar="COMMAND", required=True)

    def team_command(name: str, handler: Callable, help_text: str) -> argparse.ArgumentParser:
        sub = commands.add_parser(name, help=help_text)
        sub.add_argument("team", metavar="TEAM")
        sub.set_defaults(team_handler=handler)
        return sub

    team_command("bootstrap", cmd_bootstrap, "create or update the team's accounts and room")
    add = team_command("add-member", cmd_add_member, "add an agent; its bundle goes to --out")
    add.add_argument("--repo", required=True)
    add.add_argument("--host", help=f"the member install's role (default: ${ROLE_VAR})")
    add.add_argument("--type", required=True, choices=registry.TYPES)
    add.add_argument("--role", required=True, choices=registry.ROLES)
    add.add_argument("--address", required=True, help="the homeserver address the member uses")
    add.add_argument("--no-human-text", dest="human_text", action="store_false",
                     help="the member accepts pings only")
    remove = team_command("remove-member", cmd_remove_member, "remove an agent")
    remove.add_argument("handle", metavar="HANDLE")
    role = team_command("set-role", cmd_set_role, "change an agent's role")
    role.add_argument("handle", metavar="HANDLE")
    role.add_argument("--role", required=True, choices=registry.ROLES)
    rotate = team_command("rotate-token", cmd_rotate_token, "log a member out; a new token to --out")
    rotate.add_argument("handle", metavar="HANDLE")
    team_command("list", cmd_list, "members, roles and room membership; no tokens")
    human = commands.add_parser("human", help="a team human's account")
    human.add_argument("action", choices=HUMAN_ACTIONS)
    human.add_argument("team", metavar="TEAM")
    human.add_argument("name", metavar="NAME")
    human.set_defaults(team_handler=cmd_human)

    render_parser = commands.add_parser("render", help="the installer's renders (team file on stdin)")
    renders = render_parser.add_subparsers(dest="render", metavar="WHAT", required=True)
    check = renders.add_parser("check", help="validate; print the canonical team.json")
    check.add_argument("--previous", metavar="TEAM_JSON", help="the installed team.json, if any")
    check.set_defaults(handler=cmd_render_check)
    renders.add_parser("toml", help="tuwunel.toml").set_defaults(handler=cmd_render_toml)
    dropin = renders.add_parser("dropin", help="the unit's network.conf drop-in")
    dropin.add_argument("--interface", action="append", metavar="ADDR=IFACE",
                        help="the interface carrying each listen address")
    dropin.set_defaults(handler=cmd_render_dropin)
    commands.add_parser("version", help="print the versions").set_defaults(handler=cmd_version)
    return parser


def _run(args: argparse.Namespace, root: pathlib.Path,
         transport_factory: Callable[[str], admin.Transport], environ: Mapping[str, str], io: _Io) -> int:
    if hasattr(args, "team_handler"):
        team = admin.load_team(root, args.team)
        return args.team_handler(args, team, transport_factory(team.base_url()), environ, io)
    return args.handler(args, environ, io)


def main(
    argv: Sequence[str] | None = None,
    *,
    root: pathlib.Path = STATE_ROOT,
    transport_factory: Callable[[str], admin.Transport] = admin.http_transport,
    environ: Mapping[str, str] | None = None,
    stdin: TextIO | None = None,
    stdout: BinaryIO | None = None,
    stderr: TextIO | None = None,
) -> int:
    io = _Io(sys.stdin if stdin is None else stdin, sys.stdout.buffer if stdout is None else stdout,
             sys.stderr if stderr is None else stderr)
    env = os.environ if environ is None else environ
    try:
        args = build_parser(io.stdout).parse_args(argv)
        return _run(args, pathlib.Path(root), transport_factory, env, io)
    except _ParserExit as done:
        return done.status
    except UsageError as exc:
        io.say(str(exc))
        return EXIT_USAGE
    except admin.Unreachable as exc:
        io.say(str(exc))
        return EXIT_UNREACHABLE
    except (admin.ConfigError, teamfile.TeamFileError, render.RenderError, registry.RegistryError) as exc:
        io.say(str(exc))
        return EXIT_CONFIG
    except admin.AdminError as exc:
        io.say(str(exc))
        return EXIT_REFUSED
    finally:
        io.stdout.flush()


if __name__ == "__main__":
    sys.exit(main())
