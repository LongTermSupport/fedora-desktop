# Fixture for .semgrep/speech-to-text.yml (Python rules). `ruleid:` marks a line that must
# fire, `ok:` a line that must stay silent.

import contextlib
import io
import sys
import typing
from contextlib import redirect_stderr, redirect_stdout
from typing import IO, Optional, Protocol, TextIO


def installed_by_directory(model_dir):
    # The originating instance in wsi-model-manager: any snapshot at all is "installed".
    # ruleid: model-present-without-weights
    snapshots = model_dir / 'snapshots'
    return snapshots.exists() and any(snapshots.iterdir())


def cpu_percent(proc_root):
    # A word, not a cache path: two /proc stat snapshots taken an interval apart.
    # ok: model-present-without-weights
    snapshots = {"before": proc_root, "after": proc_root}
    return snapshots["after"]


def installed_by_weights(model_dir):
    # ok: model-present-without-weights
    snapshots = model_dir / 'snapshots'
    return snapshots.is_dir() and any((rev / 'model.bin').is_file() for rev in snapshots.iterdir())


def test_copy_that_cannot_start_fails_at_once(self, time, copy):
    started = time.monotonic()
    copy()
    # ruleid: test-wall-clock-ceiling-under-a-second
    self.assertLess(time.monotonic() - started, 0.5, "no wait for the deadline")
    # ruleid: test-wall-clock-ceiling-under-a-second
    assert time.monotonic() - started <= 0.9
    # ok: test-wall-clock-ceiling-under-a-second
    self.assertLess(time.monotonic() - started, 1.0, "a second is ten times what it bounds")
    # ok: test-wall-clock-ceiling-under-a-second
    self.assertLess(abs(started), 0.5, "not a difference: no elapsed time")


def test_term_closes_the_microphone_after_the_grace(self, r):
    gap = r["mic_stopped"] - r["terms"][0]
    # ruleid: test-wall-clock-ceiling-under-a-second
    self.assertLess(gap, 0.9, "microphone stayed open after the grace")
    # ruleid: test-wall-clock-ceiling-under-a-second
    self.assertLess(r["mic_stopped"] - r["terms"][1], 0.4)
    # ok: test-wall-clock-ceiling-under-a-second
    self.assertGreaterEqual(gap, 0.25, "a floor: lateness only adds to it")


def test_keepalives_keep_it_recording(self, time):
    # The originating instance: a keepalive every 0.1 s against a 0.3 s heartbeat.
    s = self.start(heartbeat_seconds=0.3)
    for _ in range(5):
        # ruleid: test-deadline-under-ten-steps
        time.sleep(0.1)
        s.keepalive()


def test_keepalives_with_ten_steps_of_heartbeat(self, time):
    s = self.start(heartbeat_seconds=1.0)
    for _ in range(15):
        # ok: test-deadline-under-ten-steps
        time.sleep(0.1)
        s.keepalive()


def wait_until(predicate, time, timeout=0.3):
    # Not a test: a helper's default is judged where a test passes its own value.
    while not predicate():
        # ok: test-deadline-under-ten-steps
        time.sleep(0.1)


# stream-stand-in-not-a-text-stream: the originating instance is login_report's _Sink,
# handed to check_freshness as stdout and stderr; _Locked is pingbus's _LockedStream.
class _Sink:
    def __init__(self):
        self._chunks = []

    def write(self, text):
        self._chunks.append(text)


class _Locked:
    def __init__(self, stream):
        self._stream = stream

    def write(self, text: str) -> None:
        self._stream.write(text)


class _Proper(io.TextIOBase):
    def write(self, text: str) -> int:
        return len(text)


class _Buffer(io.StringIO):
    def write(self, text: str) -> int:
        return super().write(text)


class Writer(Protocol):
    def write(self, text: str) -> int: ...


class Report:
    def __init__(self, path):
        self.path = path

    def write(self, text: str) -> None:
        self.path.write_text(text)


def through_a_tuple(run):
    out, err = _Sink(), _Sink()
    # ruleid: stream-stand-in-not-a-text-stream
    run(stdout=out, stderr=err)


def through_a_name(run):
    sink = _Sink()
    # ruleid: stream-stand-in-not-a-text-stream
    run(stderr=sink)


def direct(run):
    # ruleid: stream-stand-in-not-a-text-stream
    run(stdout=_Sink())
    # ruleid: stream-stand-in-not-a-text-stream
    print("x", file=_Locked(sys.stdout))


def over_an_optional_parameter(helper, err: TextIO | None = None):
    # ruleid: stream-stand-in-not-a-text-stream
    err = _Locked(err)
    helper(err)


def through_other_stream_names(run):
    # ruleid: stream-stand-in-not-a-text-stream
    run(out_stream=_Sink(), err_target=None)
    # ruleid: stream-stand-in-not-a-text-stream
    run(diagnostics=_Sink())


def over_other_annotations(a: typing.TextIO, b: Optional[TextIO], c: "TextIO", d: IO[str]):
    # ruleid: stream-stand-in-not-a-text-stream
    a = _Locked(a)
    # ruleid: stream-stand-in-not-a-text-stream
    b = _Locked(b)
    # ruleid: stream-stand-in-not-a-text-stream
    c = _Locked(c)
    # ruleid: stream-stand-in-not-a-text-stream
    d = _Locked(d)
    return a, b, c, d


def bare_redirects(work):
    # ruleid: stream-stand-in-not-a-text-stream
    with redirect_stdout(_Sink()):
        work()
    # ruleid: stream-stand-in-not-a-text-stream
    with redirect_stderr(_Sink()):
        work()


def through_the_other_names(run, first, second, third):
    # ruleid: stream-stand-in-not-a-text-stream
    run(log=_Sink())
    # ruleid: stream-stand-in-not-a-text-stream
    run(errors=_Sink())
    # ruleid: stream-stand-in-not-a-text-stream
    run(err_target=_Sink())
    a, b, c, err = first, second, third, _Sink()
    # ruleid: stream-stand-in-not-a-text-stream
    run(a, b, c, stderr=err)


def through_names_ending_in_stream_or_sink(run):
    # ruleid: stream-stand-in-not-a-text-stream
    run(log_stream=_Sink())
    # ruleid: stream-stand-in-not-a-text-stream
    run(event_sink=_Sink())
    # ok: stream-stand-in-not-a-text-stream
    run(streamer=_Sink())


def over_a_parameter(err: TextIO, helper):
    # ruleid: stream-stand-in-not-a-text-stream
    err = _Locked(err)
    helper(err)


def onto_sys():
    # ruleid: stream-stand-in-not-a-text-stream
    sys.stderr = _Sink()
    # ruleid: stream-stand-in-not-a-text-stream
    sys.stdout = _Sink()


def redirected(work):
    # ruleid: stream-stand-in-not-a-text-stream
    with contextlib.redirect_stdout(_Sink()):
        work()
    # ruleid: stream-stand-in-not-a-text-stream
    with contextlib.redirect_stderr(_Sink()):
        work()


def through_longer_tuples(run, first, second):
    a, b, out = first, second, _Sink()
    # ruleid: stream-stand-in-not-a-text-stream
    run(a, b, stdout=out)
    c, err = first, _Sink()
    # ruleid: stream-stand-in-not-a-text-stream
    run(c, stderr=err)


def proper(run, err: TextIO):
    # ok: stream-stand-in-not-a-text-stream
    run(stdout=_Proper(), stderr=_Buffer())
    # ok: stream-stand-in-not-a-text-stream
    err = _Proper()
    run(stderr=err)


def not_a_stream(path, helper):
    # ok: stream-stand-in-not-a-text-stream
    report = Report(path)
    helper(report)
