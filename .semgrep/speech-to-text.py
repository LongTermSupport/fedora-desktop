# Fixture for .semgrep/speech-to-text.yml (Python rules). `ruleid:` marks a line that must
# fire, `ok:` a line that must stay silent.


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
