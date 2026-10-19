"""Unit tests for helpers/pingbus/coldwake.py: does a delivery wake a large context whose
prompt cache has gone cold?

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_coldwake

Spec: Plan 00194 SPEC-compact-wake.md C1-C4. The transcript lines are shaped as Claude Code
2.1.303 writes them (the spec's facts F6-F9): an assistant entry's `message.usage` with its
`cache_creation` split by TTL, `message.stop_reason`, `isSidechain`, and a `compact_boundary`
system entry with `compactMetadata.postTokens`.
"""

from __future__ import annotations

import datetime
import json
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import coldwake

#: 2026-10-16T06:00:00Z, the last call of most cases.
T0 = 1792130400.0
HOUR = 3600.0


def iso(epoch_s: float) -> str:
    stamp = datetime.datetime.fromtimestamp(epoch_s, datetime.timezone.utc)
    return stamp.strftime("%Y-%m-%dT%H:%M:%S.") + f"{stamp.microsecond // 1000:03d}Z"


def assistant(at: float, *, inp: int = 2, write: int = 0, read: int = 0, ttl: str = "1h",
              stop: str | None = "end_turn", sidechain: bool = False) -> dict:
    split = {"ephemeral_1h_input_tokens": write if ttl == "1h" else 0,
             "ephemeral_5m_input_tokens": write if ttl == "5m" else 0}
    return {"type": "assistant", "timestamp": iso(at), "isSidechain": sidechain,
            "message": {"model": "claude-opus-5-5", "stop_reason": stop, "content": [{"type": "text", "text": "ok"}],
                        "usage": {"input_tokens": inp, "cache_creation_input_tokens": write,
                                  "cache_read_input_tokens": read, "output_tokens": 40, "cache_creation": split}}}


def user(at: float, text: str = "hello") -> dict:
    return {"type": "user", "timestamp": iso(at), "isSidechain": False, "message": {"role": "user", "content": text}}


def boundary(at: float, post: int) -> dict:
    return {"type": "system", "subtype": "compact_boundary", "timestamp": iso(at), "isSidechain": False,
            "compactMetadata": {"trigger": "manual", "preTokens": 400000, "postTokens": post, "durationMs": 60000}}


class TranscriptCase(unittest.TestCase):
    def setUp(self) -> None:
        self.dir = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory(dir="/tmp")))
        self.path = self.dir / "session.jsonl"

    def write(self, *entries: dict, raw: str = "") -> str:
        self.path.write_text(raw + "".join(json.dumps(e) + "\n" for e in entries), encoding="utf-8")
        return str(self.path)


class ReadTest(TranscriptCase):
    def test_the_context_is_the_prompt_of_the_last_main_thread_call(self):
        """C1: input, cache writes and cache reads together are what the last call sent."""
        path = self.write(user(T0 - 60), assistant(T0 - 50, write=900, read=10000),
                          assistant(T0, inp=3, write=1543, read=148357))
        ctx = coldwake.read(path)
        self.assertEqual(ctx, coldwake.Context(tokens=3 + 1543 + 148357, last_call_s=T0, ttl_s=HOUR,
                                               turn_ended=True))

    def test_a_subagent_call_is_not_the_sessions_context(self):
        path = self.write(assistant(T0, write=100, read=150000), assistant(T0 + 600, read=9000, sidechain=True))
        ctx = coldwake.read(path)
        self.assertEqual((ctx.tokens, ctx.last_call_s), (2 + 100 + 150000, T0))

    def test_the_cache_ttl_is_the_one_the_session_wrote(self):
        """C1, F8: the TTL is read from the `cache_creation` split, never assumed."""
        self.assertEqual(coldwake.read(self.write(assistant(T0, write=5, read=90000, ttl="5m"))).ttl_s, 300.0)
        self.assertEqual(coldwake.read(self.write(assistant(T0, write=5, read=90000, ttl="1h"))).ttl_s, HOUR)

    def test_a_call_that_only_read_the_cache_takes_the_ttl_of_the_last_write(self):
        path = self.write(assistant(T0 - 100, write=800, read=90000, ttl="5m"), assistant(T0, read=90800))
        ctx = coldwake.read(path)
        self.assertEqual((ctx.last_call_s, ctx.ttl_s), (T0, 300.0))

    def test_no_write_in_the_tail_leaves_the_ttl_unknown(self):
        self.assertIsNone(coldwake.read(self.write(assistant(T0, read=90000))).ttl_s)

    def test_a_call_that_asked_for_a_tool_is_a_turn_still_running(self):
        """F9: the session is in a long tool, not idle."""
        self.assertFalse(coldwake.read(self.write(assistant(T0, write=5, read=90000, stop="tool_use"))).turn_ended)
        self.assertFalse(coldwake.read(self.write(assistant(T0, write=5, read=90000, stop=None))).turn_ended)

    def test_a_compaction_after_the_last_call_is_the_context(self):
        """C1, F6: the summary replaced the conversation; its size is the boundary's."""
        path = self.write(assistant(T0, write=5, read=400000), user(T0 + 10, "/compact"),
                          boundary(T0 + 70, 13489))
        ctx = coldwake.read(path)
        self.assertEqual((ctx.tokens, ctx.last_call_s, ctx.turn_ended), (13489, T0 + 70, True))

    def test_a_call_after_a_compaction_is_the_context_again(self):
        path = self.write(boundary(T0 - 100, 13489), assistant(T0, write=4000, read=13000))
        self.assertEqual(coldwake.read(path).tokens, 2 + 4000 + 13000)

    def test_only_the_tail_is_read_and_a_cut_line_is_skipped(self):
        """C6: a long transcript costs at most `max_bytes`; the line the cut lands in is not
        parsed."""
        late = json.dumps(assistant(T0, write=5, read=70000)) + "\n"
        path = self.write(assistant(T0 - 9, write=5, read=1), raw="x" * 5000 + "\n")
        with open(path, "a", encoding="utf-8") as handle:
            handle.write(late)
        ctx = coldwake.read(path, max_bytes=len(late) + 10)
        self.assertEqual(ctx.tokens, 2 + 5 + 70000)

    def test_lines_that_are_not_entries_are_skipped(self):
        path = self.write(assistant(T0, write=5, read=70000),
                          raw='not json "usage"\n[1, "usage"]\n{"type": "assistant", "message": {"usage": 3}}\n')
        self.assertEqual(coldwake.read(path).tokens, 2 + 5 + 70000)

    def test_a_bad_count_or_stamp_is_no_reading(self):
        bad_count = assistant(T0, write=5, read=70000)
        bad_count["message"]["usage"]["cache_read_input_tokens"] = -1
        bad_stamp = assistant(T0, write=5, read=70000)
        bad_stamp["timestamp"] = "yesterday"
        for entry in (bad_count, bad_stamp):
            with self.subTest(entry=entry["timestamp"]):
                self.assertIsNone(coldwake.read(self.write(entry)))

    def test_no_transcript_is_no_reading(self):
        self.assertIsNone(coldwake.read(None))
        self.assertIsNone(coldwake.read(str(self.dir / "absent.jsonl")))
        self.assertIsNone(coldwake.read(self.write(user(T0))), "no call yet")


class AssessTest(unittest.TestCase):
    def ctx(self, **kw) -> coldwake.Context:
        values = {"tokens": 150000, "last_call_s": T0, "ttl_s": HOUR, "turn_ended": True}
        values.update(kw)
        return coldwake.Context(**values)

    def test_the_threshold_lives_in_one_place(self):
        """C2, D1: 30 % of a 200k window, in tokens."""
        self.assertEqual(coldwake.THRESHOLD_TOKENS, 60000)

    def test_a_large_context_past_its_cache_is_a_cold_wake(self):
        verdict = coldwake.assess(self.ctx(), now_s=T0 + HOUR + 5)
        self.assertEqual(verdict, coldwake.Verdict(tokens=150000, idle_s=HOUR + 5, ttl_s=HOUR))

    def test_the_bounds_are_inclusive(self):
        verdict = coldwake.assess(self.ctx(tokens=coldwake.THRESHOLD_TOKENS), now_s=T0 + HOUR)
        self.assertIsNotNone(verdict)

    def test_what_is_not_a_cold_wake(self):
        """C2: anything not known to be large, idle and cold keeps today's delivery."""
        cases = {
            "the cache is still warm": (self.ctx(), T0 + HOUR - 1),
            "the context is small": (self.ctx(tokens=coldwake.THRESHOLD_TOKENS - 1), T0 + 9 * HOUR),
            "the ttl is unknown": (self.ctx(ttl_s=None), T0 + 9 * HOUR),
            "a turn is running": (self.ctx(turn_ended=False), T0 + 9 * HOUR),
            "a clock behind the call": (self.ctx(), T0 - 5),
            "no reading": (None, T0 + 9 * HOUR),
        }
        for name, (ctx, now) in cases.items():
            with self.subTest(name):
                self.assertIsNone(coldwake.assess(ctx, now_s=now))

    def test_a_threshold_can_be_given(self):
        self.assertIsNone(coldwake.assess(self.ctx(), now_s=T0 + 2 * HOUR, threshold=150001))

    def test_the_log_line_carries_numbers_only(self):
        """C3: a fixed template; no text, sender or team can reach it."""
        verdict = coldwake.Verdict(tokens=150000, idle_s=5400.4, ttl_s=HOUR)
        self.assertEqual(
            coldwake.log_line("3f9c2a71", verdict),
            "cold wake: delivery 3f9c2a71 woke 150000 tokens of context 5400 s after its last model "
            "call, past its 3600 s prompt cache; compact-before-wake would apply (observe only)")
        with self.assertRaises(ValueError):
            coldwake.log_line("not a nonce", verdict)


if __name__ == "__main__":
    unittest.main()
