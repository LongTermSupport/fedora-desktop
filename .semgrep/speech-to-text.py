# Fixture for .semgrep/speech-to-text.yml (Python rules). `ruleid:` marks a line that must
# fire, `ok:` a line that must stay silent.

import os
import shutil


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


class FileJobMarkerLast:
    # The originating instance in wsi-stream-server: FILE_PROGRESS reads _state, so a poller
    # told "done" can still find the marker that says the job runs, and the late unlink can
    # delete the marker of a job started in the gap.
    def _run(self, marker, update_activity):
        state, error = "done", None
        with self._lock:
            self._state, self._error = state, error
        # ruleid: completion-published-before-cleanup
        marker.unlink(missing_ok=True)
        # ruleid: completion-published-before-cleanup
        update_activity()


class FileJobMarkerFirst:
    def _run(self, marker, update_activity):
        state, error = "done", None
        # ok: completion-published-before-cleanup
        marker.unlink(missing_ok=True)
        # ok: completion-published-before-cleanup
        update_activity()
        with self._lock:
            self._state, self._error = state, error


class SessionAbortedFirst:
    def abort(self):
        with self._lock:
            self.state = "aborted"
        # ruleid: completion-published-before-cleanup
        self._proc.kill()
        with self._lock:
            # ruleid: completion-published-before-cleanup
            shutil.rmtree(self.session_dir)


class SessionFinishedEvent:
    def end(self):
        self._finished.set()
        # ruleid: completion-published-before-cleanup
        os.remove(self.pid_path)


class SessionCleanedFirst:
    def fail(self):
        with self._lock:
            # ok: completion-published-before-cleanup
            self._proc.terminate()
            self.state = "failed"
        self._finished.set()


class StopRequested:
    # A stop REQUEST, not a completion: the thread it stops is joined before the file goes.
    def close(self):
        self.stop.set()
        self.thread.join(5)
        # ok: completion-published-before-cleanup
        self.path.unlink(missing_ok=True)


class OtherAttribute:
    def rotate(self):
        self.count = 0
        # ok: completion-published-before-cleanup
        self.path.unlink(missing_ok=True)
