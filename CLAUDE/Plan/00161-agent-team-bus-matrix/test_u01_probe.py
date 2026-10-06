"""Tests for u01_probe.py, the Claude Code probes behind this plan's triage.bash (unit U01).

Only the pure parts and the socket writer are tested here: the inbox socket's frames, the
notice text, the throwaway plugin and settings, the child's argv and environment, the
readers of the child's debug log, stream output and transcript, the pairing of each send
with what the inbox did with it, the hook marks, the writer's commands, and one send
against a fake inbox socket. A real child `claude` runs only on the host, through
triage.bash.

Run from anywhere:
    python3 CLAUDE/Plan/00161-agent-team-bus-matrix/test_u01_probe.py
"""

from __future__ import annotations

import importlib.util
import io
import json
import os
import pathlib
import shlex
import socket
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock

_HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(_HERE))
_SPEC = importlib.util.spec_from_file_location("u01_probe", _HERE / "u01_probe.py")
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_HERE / 'u01_probe.py'}")
up = importlib.util.module_from_spec(_SPEC)
sys.modules["u01_probe"] = up
_SPEC.loader.exec_module(up)

TOKEN = "0123456789abcdef0123456789abcdef"


class FrameTest(unittest.TestCase):
    def test_auth_frame_is_one_json_line(self) -> None:
        frame = up.auth_frame(TOKEN)
        self.assertTrue(frame.endswith(b"\n"))
        self.assertEqual(frame.count(b"\n"), 1)
        self.assertEqual(json.loads(frame), {"type": "auth", "token": TOKEN})

    def test_user_frame_carries_the_text_as_a_user_message(self) -> None:
        frame = up.user_frame("agent-bus: 1 pending")
        self.assertEqual(frame.count(b"\n"), 1)
        self.assertEqual(
            json.loads(frame),
            {"type": "user", "message": {"role": "user", "content": "agent-bus: 1 pending"}},
        )

    def test_a_newline_in_the_text_stays_inside_one_line(self) -> None:
        frame = up.user_frame("one\ntwo")
        self.assertEqual(frame.count(b"\n"), 1)
        self.assertEqual(json.loads(frame)["message"]["content"], "one\ntwo")

    def test_connection_payload_is_auth_first_then_the_message(self) -> None:
        payload = up.connection_payload(TOKEN, "hello")
        first, second, rest = payload.split(b"\n")
        self.assertEqual(json.loads(first)["type"], "auth")
        self.assertEqual(json.loads(second)["type"], "user")
        self.assertEqual(rest, b"")

    def test_empty_token_or_text_is_refused(self) -> None:
        with self.assertRaises(ValueError):
            up.auth_frame("")
        with self.assertRaises(ValueError):
            up.user_frame("")

    def test_wire_example_hides_the_token(self) -> None:
        example = up.wire_example("hello")
        self.assertIn('"type":"auth"', example)
        self.assertIn("<CLAUDE_CODE_MESSAGING_TOKEN>", example)
        self.assertNotIn(TOKEN, example)
        self.assertEqual(len(example.splitlines()), 2)


class NoticeTest(unittest.TestCase):
    def test_notice_follows_the_design_template_and_asks_for_ok(self) -> None:
        body = up.notice_body(pending=2, humans=1, pings=1, seq=7)
        self.assertTrue(
            body.startswith("agent-bus: 2 pending (1 from humans, 1 pings), notice 7. Run `pingbus recv`.")
        )
        self.assertIn("reply with the single word OK", body)

    def test_notices_differ_only_by_their_number(self) -> None:
        self.assertNotEqual(up.notice_body(1, 0, 1, 1), up.notice_body(1, 0, 1, 2))
        self.assertEqual(up.notice_body(1, 0, 1, 1), up.notice_body(1, 0, 1, 1))


class PluginAndSettingsTest(unittest.TestCase):
    def test_hooks_cover_all_four_events(self) -> None:
        hooks = up.build_hooks("/usr/bin/python3", pathlib.Path("/p/u01_probe.py"), pathlib.Path("/e"))
        self.assertEqual(set(hooks["hooks"]), {"SessionStart", "UserPromptSubmit", "Stop", "SessionEnd"})

    def test_hook_commands_quote_paths_with_spaces(self) -> None:
        hooks = up.build_hooks("/usr/bin/python3", pathlib.Path("/a b/u01_probe.py"), pathlib.Path("/c d/e"))
        command = hooks["hooks"]["Stop"][0]["hooks"][0]["command"]
        self.assertEqual(command, "/usr/bin/python3 '/a b/u01_probe.py' hook '/c d/e' Stop")
        self.assertEqual(hooks["hooks"]["Stop"][0]["hooks"][0]["type"], "command")

    def test_plugin_manifest_names_the_plugin(self) -> None:
        self.assertEqual(up.plugin_manifest()["name"], up.PLUGIN_NAME)

    def test_settings_accept_or_not(self) -> None:
        self.assertEqual(up.build_settings(accept=True), {"crossSessionInbound": "accept"})
        self.assertEqual(up.build_settings(accept=False), {})


class ArgvTest(unittest.TestCase):
    def argv(self, bypass: bool) -> list[str]:
        return up.build_argv(
            claude="claude",
            session_id="11111111-2222-4333-8444-555555555555",
            settings=pathlib.Path("/e/settings.json"),
            plugin_dir=pathlib.Path("/w/plugin"),
            debug_file=pathlib.Path("/e/debug.log"),
            bypass=bypass,
        )

    def flag(self, argv: list[str], name: str) -> str:
        return argv[argv.index(name) + 1]

    def test_a_streaming_print_session_on_a_cheap_model(self) -> None:
        argv = self.argv(bypass=True)
        self.assertEqual(argv[0], "claude")
        self.assertIn("-p", argv)
        self.assertEqual(self.flag(argv, "--input-format"), "stream-json")
        self.assertEqual(self.flag(argv, "--output-format"), "stream-json")
        self.assertIn("--verbose", argv)
        self.assertEqual(self.flag(argv, "--model"), "haiku")

    def test_isolated_from_the_users_own_settings_tools_and_servers(self) -> None:
        argv = self.argv(bypass=True)
        self.assertEqual(self.flag(argv, "--setting-sources"), "project")
        self.assertEqual(self.flag(argv, "--tools"), "")
        self.assertIn("--strict-mcp-config", argv)
        self.assertEqual(self.flag(argv, "--plugin-dir"), "/w/plugin")
        self.assertEqual(self.flag(argv, "--settings"), "/e/settings.json")
        self.assertEqual(self.flag(argv, "--session-id"), "11111111-2222-4333-8444-555555555555")
        self.assertEqual(self.flag(argv, "--debug-file"), "/e/debug.log")

    def test_bypass_is_the_only_difference(self) -> None:
        with_bypass = self.argv(bypass=True)
        self.assertEqual(self.flag(with_bypass, "--permission-mode"), "bypassPermissions")
        without = self.argv(bypass=False)
        self.assertNotIn("--permission-mode", without)
        self.assertEqual([a for a in with_bypass if a not in ("--permission-mode", "bypassPermissions")], without)


class ChildEnvTest(unittest.TestCase):
    def test_a_live_sessions_socket_never_reaches_the_child(self) -> None:
        env = up.child_env(
            {
                "PATH": "/usr/bin",
                "HOME": "/home/u",
                "CLAUDE_CODE_MESSAGING_SOCKET": "/run/live.sock",
                "CLAUDE_CODE_MESSAGING_TOKEN": TOKEN,
                "CLAUDECODE": "1",
                "CLAUDE_CODE_SESSION_ID": "x",
            }
        )
        self.assertEqual(env, {"PATH": "/usr/bin", "HOME": "/home/u"})


class DebugLogTest(unittest.TestCase):
    def test_timestamp_parsed_to_epoch(self) -> None:
        parsed = up.parse_debug_line("2026-10-06T15:58:21.693Z [DEBUG] [uds-messaging] Client connected")
        self.assertIsNotNone(parsed)
        epoch, text = parsed
        self.assertAlmostEqual(epoch, 1791302301.693, places=3)
        self.assertEqual(text, "[uds-messaging] Client connected")

    def test_continuation_lines_are_not_entries(self) -> None:
        self.assertIsNone(up.parse_debug_line("  at foo (bar.js:1)"))

    def test_classify_routed(self) -> None:
        self.assertEqual(
            up.classify_inbox("[uds-messaging] Routed user message to queue (priority=next): agent-bus: 1 pe"),
            ("routed", "next"),
        )

    def test_classify_held(self) -> None:
        self.assertEqual(
            up.classify_inbox(
                '[cross-session-inbound] held inbound peer message (1 held, cause=no-mode-asserted): from=unknown "x"'
            ),
            ("held", "no-mode-asserted"),
        )

    def test_classify_dropped(self) -> None:
        self.assertEqual(up.classify_inbox("[peer-guard] drop duplicate from unknown"), ("dropped", "duplicate"))

    def test_classify_turn_origin(self) -> None:
        self.assertEqual(
            up.classify_inbox("attribution header x: cc_entrypoint=sdk-cli; cc_turn_origin=peer; cc_prompt_index=0;"),
            ("turn", "peer"),
        )

    def test_classify_other(self) -> None:
        self.assertIsNone(up.classify_inbox("[uds-messaging] Client connected"))

    def test_listening_socket(self) -> None:
        text = (
            "2026-10-06T15:58:20.421Z [INFO] [uds-messaging] Listening: /run/user/1000/cc-socks/42.sock\n"
            "2026-10-06T15:58:20.422Z [INFO] [uds-messaging] Inject messages ...\n"
        )
        self.assertEqual(up.listening_socket(text), "/run/user/1000/cc-socks/42.sock")
        self.assertIsNone(up.listening_socket("nothing here\n"))

    def test_inbox_events_in_order(self) -> None:
        text = (
            "2026-10-06T15:58:21.000Z [DEBUG] [uds-messaging] Client connected\n"
            "2026-10-06T15:58:21.100Z [DEBUG] [uds-messaging] Routed user message to queue (priority=next): a\n"
            "2026-10-06T15:58:22.000Z [DEBUG] [peer-guard] drop duplicate from unknown\n"
        )
        events = up.inbox_events(text)
        self.assertEqual([(kind, detail) for _t, kind, detail in events], [("routed", "next"), ("dropped", "duplicate")])


class PairingTest(unittest.TestCase):
    def test_each_send_takes_the_events_up_to_the_next_send(self) -> None:
        sends = [(10.0, "n1"), (20.0, "n1-again"), (30.0, "n2")]
        events = [
            (10.1, "routed", "next"),
            (10.2, "turn", "peer"),
            (20.1, "routed", "next"),
            (20.1, "dropped", "duplicate"),
            (30.1, "held", "no-mode-asserted"),
        ]
        self.assertEqual(
            up.pair_outcomes(sends, events),
            [
                {"label": "n1", "outcome": "routed", "detail": "next", "peer_turn": True},
                {"label": "n1-again", "outcome": "dropped", "detail": "duplicate", "peer_turn": False},
                {"label": "n2", "outcome": "held", "detail": "no-mode-asserted", "peer_turn": False},
            ],
        )

    def test_a_send_with_no_inbox_event_says_so(self) -> None:
        self.assertEqual(
            up.pair_outcomes([(1.0, "x")], []),
            [{"label": "x", "outcome": "no-inbox-event", "detail": "", "peer_turn": False}],
        )

    def test_events_before_the_first_send_are_ignored(self) -> None:
        result = up.pair_outcomes([(5.0, "x")], [(1.0, "turn", "peer"), (5.5, "routed", "next")])
        self.assertEqual(result[0]["outcome"], "routed")
        self.assertFalse(result[0]["peer_turn"])


class StreamTest(unittest.TestCase):
    def test_result_times(self) -> None:
        events = [
            (1.0, {"type": "system", "subtype": "init"}),
            (2.0, {"type": "result", "subtype": "success"}),
            (3.0, {"unparsed": "x"}),
            (4.0, {"type": "result", "subtype": "success"}),
        ]
        self.assertEqual(up.result_times(events), [2.0, 4.0])

    def test_hook_responses(self) -> None:
        events = [
            (1.0, {"type": "system", "subtype": "hook_response", "hook_event": "SessionStart", "exit_code": 0}),
            (2.0, {"type": "system", "subtype": "hook_started", "hook_event": "Stop"}),
        ]
        self.assertEqual(up.hook_responses(events), [("SessionStart", 0)])


class TranscriptTest(unittest.TestCase):
    PREFIX = "Another Claude session sent a message:\n"
    SUFFIX = "\n\nThis came from another Claude session."

    def line(self, content: object, kind: str = "peer") -> str:
        return json.dumps(
            {"type": "user", "message": {"role": "user", "content": content}, "origin": {"kind": kind, "from": "unknown"}}
        )

    def test_peer_entries_only(self) -> None:
        lines = [
            json.dumps({"type": "queue-operation"}),
            self.line("typed by the driver", kind="sdk"),
            json.dumps({"type": "user", "message": {"role": "user", "content": "no origin"}}),
            self.line(self.PREFIX + "n1" + self.SUFFIX),
            "not json",
        ]
        self.assertEqual(up.peer_entries(lines), [self.PREFIX + "n1" + self.SUFFIX])

    def test_block_content_is_joined(self) -> None:
        lines = [self.line([{"type": "text", "text": "a"}, {"type": "image"}, {"type": "text", "text": "b"}])]
        self.assertEqual(up.peer_entries(lines), ["a\nb"])

    def test_framing_splits_around_the_body(self) -> None:
        self.assertEqual(up.framing(self.PREFIX + "n1" + self.SUFFIX, "n1"), (self.PREFIX, self.SUFFIX))
        self.assertIsNone(up.framing("other", "n1"))

    def test_batching(self) -> None:
        both = self.PREFIX + "n2\n\nn3" + self.SUFFIX
        self.assertEqual(up.batching([both], ["n2", "n3"]), "one-entry")
        self.assertEqual(up.batching(["x n2", "x n3"], ["n2", "n3"]), "separate-entries")
        self.assertEqual(up.batching(["x n2"], ["n2", "n3"]), "missing:n3")


class HookMarkTest(unittest.TestCase):
    def test_mark_keeps_the_socket_path_and_never_the_token(self) -> None:
        record = up.mark_record(
            "SessionStart",
            '{"hook_event_name": "SessionStart", "source": "startup", "session_id": "s", "transcript_path": "/t"}',
            {"CLAUDE_CODE_MESSAGING_SOCKET": "/run/s.sock", "CLAUDE_CODE_MESSAGING_TOKEN": TOKEN},
            12.5,
        )
        self.assertEqual(
            record,
            {
                "event": "SessionStart",
                "t": 12.5,
                "input": {"hook_event_name": "SessionStart", "source": "startup", "session_id": "s"},
                "socket": "/run/s.sock",
                "token_present": True,
            },
        )
        self.assertNotIn(TOKEN, json.dumps(record))

    def test_unparseable_input_is_recorded_as_such(self) -> None:
        record = up.mark_record("Stop", "{", {}, 1.0)
        self.assertEqual(record["input"], {"unparseable": True})
        self.assertEqual(record["socket"], "")
        self.assertFalse(record["token_present"])

    def test_hook_counts(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            marks = pathlib.Path(tmp)
            (marks / "Stop.jsonl").write_text('{"a":1}\n{"a":2}\n', encoding="utf-8")
            (marks / "SessionStart.jsonl").write_text('{"a":1}\n', encoding="utf-8")
            self.assertEqual(
                up.hook_counts(marks),
                {"SessionStart": 1, "UserPromptSubmit": 0, "Stop": 2, "SessionEnd": 0},
            )


class CommandTest(unittest.TestCase):
    def test_send_command(self) -> None:
        self.assertEqual(
            up.parse_command({"op": "send", "bodies": ["a", "b"], "gap_s": 0.05}),
            {"op": "send", "bodies": ["a", "b"], "gap_s": 0.05},
        )

    def test_watch_and_exit_commands(self) -> None:
        self.assertEqual(up.parse_command({"op": "watch-socket", "timeout_s": 30}), {"op": "watch-socket", "timeout_s": 30})
        self.assertEqual(up.parse_command({"op": "exit"}), {"op": "exit"})

    def test_bad_commands_are_refused(self) -> None:
        for bad in (
            {"op": "rm"},
            {"op": "send", "bodies": []},
            {"op": "send", "bodies": [""]},
            {"op": "send", "bodies": ["a"], "gap_s": -1},
            {"op": "watch-socket", "timeout_s": 0},
            [],
        ):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                up.parse_command(bad)


class AuthStatusTest(unittest.TestCase):
    def test_only_the_fields_the_probe_needs(self) -> None:
        text = json.dumps(
            {
                "loggedIn": True,
                "authMethod": "claude.ai",
                "apiProvider": "firstParty",
                "email": "someone@example.com",
                "projectsDirectory": "/home/u/.claude/projects",
                "configDirectory": "/home/u/.claude",
            }
        )
        self.assertEqual(
            up.parse_auth_status(text),
            {
                "loggedIn": True,
                "authMethod": "claude.ai",
                "apiProvider": "firstParty",
                "projectsDirectory": "/home/u/.claude/projects",
                "configDirectory": "/home/u/.claude",
            },
        )

    def test_not_json_is_an_error(self) -> None:
        with self.assertRaises(ValueError):
            up.parse_auth_status("Not logged in")


class WindowTest(unittest.TestCase):
    def test_bounds_from_identical_resends(self) -> None:
        self.assertEqual(up.dedupe_window([(20.0, "dropped"), (33.0, "routed")]), "longer than 20 s, at most 33 s")
        self.assertEqual(up.dedupe_window([(20.0, "routed"), (33.0, "routed")]), "at most 20 s")
        self.assertEqual(up.dedupe_window([(20.0, "dropped"), (33.0, "dropped")]), "longer than 33 s")
        self.assertEqual(up.dedupe_window([(20.0, "held")]), "not measured (an identical resend was held, not judged)")


class FakeInbox:
    """A Unix socket that records what each connection sent, and answers nothing."""

    def __init__(self, path: pathlib.Path) -> None:
        self.received: list[bytes] = []
        self.server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.server.bind(str(path))
        self.server.listen()
        self.thread = threading.Thread(target=self.serve, daemon=True)
        self.thread.start()

    def serve(self) -> None:
        conn, _ = self.server.accept()
        with conn:
            data = b""
            while chunk := conn.recv(4096):
                data += chunk
            self.received.append(data)

    def close(self) -> None:
        self.thread.join(timeout=5)
        self.server.close()


class SendNoticeTest(unittest.TestCase):
    def test_one_connection_carries_auth_then_the_message(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "inbox.sock"
            inbox = FakeInbox(path)
            record = up.send_notice(str(path), TOKEN, "agent-bus: 1 pending", reply_wait_s=0.5)
            inbox.close()
        self.assertEqual(inbox.received, [up.connection_payload(TOKEN, "agent-bus: 1 pending")])
        self.assertEqual(record["body"], "agent-bus: 1 pending")
        self.assertEqual(record["reply"], "")
        self.assertEqual(record["error"], "")
        self.assertNotIn(TOKEN, json.dumps(record))

    def test_a_missing_socket_is_recorded(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            record = up.send_notice(str(pathlib.Path(tmp) / "absent.sock"), TOKEN, "x", reply_wait_s=0.1)
        self.assertIn("FileNotFoundError", record["error"])


REPO_ROOT = _HERE.parents[2]


class WorkDirTest(unittest.TestCase):
    def test_instruction_ancestors_finds_claude_md_and_dot_claude(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / "a" / "b" / ".claude").mkdir(parents=True)
            (root / "a" / "CLAUDE.md").write_text("x", encoding="utf-8")
            (root / "a" / "b" / "c").mkdir()
            self.assertEqual(
                up.instruction_ancestors(root / "a" / "b" / "c"),
                [root / "a" / "b" / ".claude", root / "a" / "CLAUDE.md"],
            )
            self.assertEqual(up.instruction_ancestors(root), [])

    def test_the_work_dir_is_outside_the_checkout_with_no_instructions_above_it(self) -> None:
        work = up.make_work_dir("main")
        try:
            self.assertTrue(work.is_dir())
            self.assertFalse(work.resolve().is_relative_to(REPO_ROOT.resolve()))
            self.assertEqual(up.instruction_ancestors(work), [])
        finally:
            work.rmdir()

    def test_a_session_refuses_a_work_dir_under_instructions(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / "CLAUDE.md").write_text("x", encoding="utf-8")
            session = up.Session("claude", up.VARIANTS["main"], root / "evidence", root / "work")
            with self.assertRaisesRegex(up.tp.ProbeError, "CLAUDE.md"):
                session.start()
            self.assertIsNone(session.proc)
            self.assertFalse((root / "evidence").exists())


class WriterTest(unittest.TestCase):
    def test_the_writer_exits_once_the_socket_is_gone(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            evidence = pathlib.Path(tmp)
            (evidence / "writer").mkdir()
            sock = evidence / "inbox.sock"
            sock.write_text("", encoding="utf-8")
            env = {up.SOCKET_ENV: str(sock), up.TOKEN_ENV: TOKEN}
            result: list[int] = []
            with mock.patch.dict(os.environ, env):
                thread = threading.Thread(target=lambda: result.append(up.writer_main(evidence)), daemon=True)
                thread.start()
                deadline = time.monotonic() + 5
                while not (evidence / "writer" / "ready.json").exists() and time.monotonic() < deadline:
                    time.sleep(0.02)
                sock.unlink()
                thread.join(timeout=5)
            self.assertFalse(thread.is_alive())
            self.assertEqual(result, [0])
            self.assertTrue((evidence / "writer" / "socket-gone.json").exists())


class HookMainTest(unittest.TestCase):
    def test_a_stop_hook_records_its_mark_and_starts_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            evidence = pathlib.Path(tmp)
            (evidence / "marks").mkdir()
            stdin = io.StringIO('{"hook_event_name": "Stop", "stop_hook_active": false}')
            with mock.patch.object(sys, "stdin", stdin), mock.patch.dict(os.environ, {up.SOCKET_ENV: "/s"}):
                self.assertEqual(up.hook_main(evidence, "Stop"), 0)
            lines = (evidence / "marks" / "Stop.jsonl").read_text(encoding="utf-8").splitlines()
            self.assertEqual(len(lines), 1)
            self.assertEqual(json.loads(lines[0])["input"], {"hook_event_name": "Stop", "stop_hook_active": False})
            self.assertFalse((evidence / "writer").exists())

    def test_a_second_session_start_keeps_the_first_writer(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            evidence = pathlib.Path(tmp)
            (evidence / "marks").mkdir()
            (evidence / "writer").mkdir()
            with mock.patch.object(sys, "stdin", io.StringIO("{}")):
                self.assertEqual(up.hook_main(evidence, "SessionStart"), 0)
            self.assertFalse((evidence / "writer.stderr").exists())
            self.assertTrue((evidence / "marks" / "SessionStart.jsonl").exists())


def _peer_line(content: str) -> str:
    return json.dumps(
        {"type": "user", "message": {"role": "user", "content": content}, "origin": {"kind": "peer", "from": "unknown"}}
    )


class RenderSessionTest(unittest.TestCase):
    def test_the_report_section_carries_every_fact(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            session = up.Session("claude", up.VARIANTS["main"], root / "evidence", root / "work")
            session.marks.mkdir(parents=True)
            start = up.mark_record("SessionStart", '{"source": "startup"}', {}, 1.0)
            (session.marks / "SessionStart.jsonl").write_text(json.dumps(start) + "\n", encoding="utf-8")
            routed_at = "2026-10-06T15:58:21.100Z"
            parsed = up.parse_debug_line(routed_at + " [DEBUG] x")
            assert parsed is not None
            session.debug_file.write_text(
                f"{routed_at} [DEBUG] [uds-messaging] Routed user message to queue (priority=next): a\n"
                f"{routed_at} [DEBUG] attribution header: cc_turn_origin=peer;\n",
                encoding="utf-8",
            )
            session.sends = [(parsed[0] - 0.05, "notice 1 to the idle session")]
            body = up.notice_body(1, 0, 1, 1)
            (session.evidence / "transcript.jsonl").write_text(
                _peer_line("Another Claude session sent:\n" + body + "\nEnd.") + "\n", encoding="utf-8"
            )
            facts = {
                "version": "2.1.291 (Claude Code)",
                "ready": {"token_present": True},
                "exit_code": 0,
                "wake_turn_s": 3.2,
                "framing_body": body,
                "batch_bodies": [up.notice_body(2, 0, 2, 2), up.notice_body(3, 0, 3, 3)],
                "transcript_found": True,
                "purge": "exit 0",
                "removed_after_purge": [],
            }
            text = up.render_session("main", session, facts, ["the dedupe resend failed"])
        self.assertIn("## U01 session: main", text)
        self.assertIn("- Claude Code: 2.1.291 (Claude Code)", text)
        self.assertIn("<CLAUDE_CODE_MESSAGING_TOKEN>", text)
        self.assertIn('| SessionStart | 1 | {"source": "startup"} |', text)
        self.assertIn("| Stop | 0 |  |", text)
        self.assertIn("| notice 1 to the idle session | routed | next | yes |", text)
        self.assertIn("turn after notice 1 to the idle session ended after 3.2 s", text)
        self.assertIn("```\nAnother Claude session sent:\n```", text)
        self.assertIn("```\n\nEnd.\n```", text)
        self.assertIn("reached the model as: missing:", text)
        self.assertIn("Cleanup: claude purge exit 0", text)
        self.assertIn("**Facts not established:** the dedupe resend failed", text)


class RemoveTracesTest(unittest.TestCase):
    def test_only_the_sessions_own_files_are_removed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            log = root / "claude.log"
            claude = root / "claude"
            claude.write_text(f'#!/bin/sh\nprintf "%s\\n" "$*" >> {shlex.quote(str(log))}\n', encoding="utf-8")
            claude.chmod(0o755)
            session = up.Session(str(claude), up.VARIANTS["main"], root / "evidence", root / "work")
            session.evidence.mkdir()
            session.cwd.mkdir(parents=True)
            sid, other = session.session_id, "99999999-8888-4777-8666-555555555555"
            config = root / "config"
            keep = [
                config / "projects" / "-p" / f"{other}.jsonl",
                config / "todos" / f"{other}-agent.json",
                config / "settings.json",
            ]
            gone = [
                config / "projects" / "-p" / f"{sid}.jsonl",
                config / "todos" / f"{sid}-agent-{sid}.json",
                config / "session-env" / sid / "env",
            ]
            for path in keep + gone:
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(path.name, encoding="utf-8")
            auth = {"projectsDirectory": str(config / "projects"), "configDirectory": str(config)}
            facts: dict[str, object] = {}
            errors = up.remove_traces(str(claude), auth, session, facts)
            self.assertEqual(errors, [])
            for path in keep:
                self.assertTrue(path.exists(), path)
            for path in gone:
                self.assertFalse(path.exists(), path)
            self.assertFalse((config / "session-env" / sid).exists())
            self.assertFalse(session.work.exists())
            self.assertEqual((session.evidence / "transcript.jsonl").read_text(encoding="utf-8"), f"{sid}.jsonl")
            self.assertEqual(facts["purge"], "exit 0")
            self.assertTrue(facts["transcript_found"])
            self.assertEqual(
                log.read_text(encoding="utf-8").splitlines(),
                [f"purge --dry-run {session.cwd}", f"purge -y {session.cwd}"],
            )


if __name__ == "__main__":
    unittest.main()
