"""Records which plays have actually been run on this host (Plan 00109, Task 1.2).

Enabled by `ansible.cfg` (`callback_plugins` + `callbacks_enabled`), so it runs
for `./run.bash`, for a playbook executed by its shebang, and for a bare
`ansible-playbook` invocation from the repo root alike. It is defeated by
`ANSIBLE_CONFIG` pointing elsewhere — Phase 2 must therefore never claim the
ledger is complete by construction.

**This file decides nothing.** `ansible` is not importable by the interpreter
that runs this repo's tests, so every decision lives in `helpers/play_ledger/`
where it is tested, and this is the adapter that wires Ansible's events to it.
Keep it that way: logic added here is logic with no test.

Two properties are load-bearing and easy to break:

1. **Ansible swallows an exception raised inside a callback.** A ledger write
   failure therefore cannot fail the run. It is recorded instead — a `BROKEN`
   sentinel plus a stderr marker — and Phase 2 refuses to answer while the
   sentinel exists. That turns an unfailable hook into a failable check.
2. **A `--check` run applied nothing**, so it must not be ledgered. Recording it
   would report the play fresh on a host that never received it, which is the
   failure this plan exists to catch.

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-play-ledger.md
"""

from __future__ import annotations

import os
import sys

from ansible import context
from ansible.plugins.callback import CallbackBase

# The repo root, from this file's own location — never the cwd, which for a
# callback is wherever the operator happened to be. Needed on sys.path before
# the helpers package can be imported at all.
_REPO_ROOT = os.path.dirname(os.path.dirname(os.path.realpath(__file__)))
if _REPO_ROOT not in sys.path:
    sys.path.insert(0, _REPO_ROOT)

from helpers.play_ledger import collector, ledger, plugin_support, repo

DOCUMENTATION = """
    name: play_ledger
    type: notification
    short_description: Record each play run to the host play-run ledger
    description:
      - Appends one record per play to $XDG_STATE_HOME/fedora-desktop/play-ledger/runs.jsonl,
        capturing the repo commit, whether the tree was dirty, the play file's hash as
        executed, the outcome and the task-level changed count.
      - Records nothing for --check, --syntax-check or any --list-* run, none of which
        apply anything to the host.
      - On any write failure, writes a BROKEN sentinel and prints LEDGER-WRITE-FAILED to
        stderr rather than raising, because Ansible discards exceptions raised in a callback.
    requirements:
      - Enable with callbacks_enabled in ansible.cfg
"""


def _source_position(play):
    """Ansible's `(file, line, column)` for a play, from whichever shape it uses.

    TWO shapes, because 2.19 changed it and this plugin only read the old one —
    so every play became a recorded hole and the ledger marked itself BROKEN on
    every run (issue #46). `ansible_pos` on the parsed mapping is pre-2.19;
    `_origin` on the play itself (`path`, `line_num`, `col_num`) is what
    `FieldAttributeBase.load_data` sets now.

    Both are Ansible internals with no public accessor, so every read is a
    `getattr`: a future rename must degrade to the other shape or to a recorded
    hole, never raise here, where Ansible would swallow the exception.

    The CHOICE between them is `plugin_support.source_position`, where it is
    tested — `ansible` is not importable by the interpreter that runs the tests,
    so logic left in this file is logic with no test.
    """
    return plugin_support.source_position(
        getattr(play, "_origin", None),
        getattr(getattr(play, "_ds", None), "ansible_pos", None),
    )


class CallbackModule(CallbackBase):
    """Thin adapter. Every decision belongs in helpers/play_ledger/."""

    CALLBACK_VERSION = 2.0
    CALLBACK_TYPE = "notification"
    CALLBACK_NAME = "play_ledger"
    CALLBACK_NEEDS_ENABLED = True

    def __init__(self) -> None:
        super().__init__()
        self._collector: collector.RunCollector | None = None
        self._base = ""
        self._enabled = False
        self._broken = False
        self._playbook_file: str | None = None
        # Unlike the event methods below, an exception HERE is not swallowed —
        # Ansible instantiates callbacks outside that protection, so a bad
        # XDG_STATE_HOME would abort every playbook in the repo over a ledger
        # problem. Disable the ledger instead and say so.
        try:
            self._base = ledger.ledger_dir(os.environ, os.path.expanduser("~"))
            self._enabled = plugin_support.should_record(dict(context.CLIARGS))
        except Exception as error:
            self._broken = True
            self._warn(f"{plugin_support.FAILURE_MARKER}: {type(error).__name__}: {error}")

    @staticmethod
    def _warn(line: str) -> None:
        """The operator's only live signal — diagnostics to stderr, never stdout."""
        sys.stderr.write(line + "\n")
        sys.stderr.flush()

    def _fail(self, error: BaseException) -> None:
        """Record a hole and keep going. Raising here would be swallowed by Ansible."""
        self._broken = True
        self._collector = None
        self._warn(
            plugin_support.record_failure(
                self._base, error=f"{type(error).__name__}: {error}", at=repo.utc_now()
            )
        )

    def _ensure_collector(self) -> collector.RunCollector | None:
        if self._collector is None and not self._broken:
            self._collector = collector.RunCollector(
                commit=repo.head_commit(_REPO_ROOT),
                dirty=repo.is_dirty(_REPO_ROOT),
                hash_play=repo.sha256_file,
                repo_root=_REPO_ROOT,
            )
        return self._collector

    def v2_playbook_on_start(self, playbook) -> None:
        # `_file_name` is an Ansible internal; `ansible.cli.adhoc` sets it to
        # ADHOC_PLAYBOOK_FILE, and plugin_support decides what that means.
        self._playbook_file = getattr(playbook, "_file_name", None)

    def v2_playbook_on_play_start(self, play) -> None:
        if not self._enabled or self._broken:
            return
        try:
            play_path = plugin_support.play_to_record(
                self._playbook_file, _source_position(play)
            )
            if play_path is None:
                return
            running = self._ensure_collector()
            if running is None:
                return
            running.on_play_start(
                play_path=play_path,
                name=play.get_name(),
                at=repo.utc_now(),
            )
        except Exception as error:  # a callback may not raise; see the module docstring
            self._fail(error)

    def _result(self, outcome: str, result) -> None:
        if not self._enabled or self._broken or self._collector is None:
            return
        try:
            self._collector.on_result(outcome=outcome, changed=bool(result.is_changed()))
        except Exception as error:
            self._fail(error)

    def v2_runner_on_ok(self, result) -> None:
        self._result("ok", result)

    def v2_runner_on_failed(self, result, ignore_errors=False) -> None:
        # An ignored failure is not a play failure: the run continued by design, and
        # recording it as failed would make every later report distrust a good run.
        self._result("ok" if ignore_errors else "failed", result)

    def v2_runner_on_unreachable(self, result) -> None:
        self._result("unreachable", result)

    def v2_playbook_on_stats(self, stats) -> None:
        if not self._enabled or self._broken or self._collector is None:
            return
        try:
            now = repo.utc_now()
            plugin_support.write_records(
                self._base,
                self._collector.on_end(at=now),
                commit=repo.head_commit(_REPO_ROOT),
                at=now,
            )
        except Exception as error:
            self._fail(error)
