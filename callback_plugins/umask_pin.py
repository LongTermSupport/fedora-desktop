"""Pins the umask every Ansible run from this repository works under to 022.

Ansible runs with the umask of the shell that started it, and its root tasks inherit it
through sudo, which only ever adds 022 to the caller's. A task that writes a file with no
explicit mode (a cache rebuild such as `fc-cache -f`, a generated file, a `copy` with no
`mode:`) takes that umask. The host `cc` session sets 077 (Plan 00098), so a play started from
it left the system font cache readable only by root (Plan 00185), where the same play started
from a terminal (022) was correct.

Ansible loads its callbacks before it starts the first worker, and every task's process
descends from the controller, so setting the umask here reaches every task, whichever
launcher started the run: run.bash, a plan script, the self-update service or a bare
`ansible-playbook`. `ansible.cfg` enables it; the rule `ansible-umask-pinned`
(CLAUDE/QA.md) keeps it enabled, and tests/umask_pin/ (scripts/test-umask-pin.bash) proves
it against the real Ansible.
"""

from __future__ import annotations

import os

from ansible.plugins.callback import CallbackBase

DOCUMENTATION = """
    name: umask_pin
    type: notification
    short_description: Run every task under umask 022, whatever the launching shell had
    description:
      - Sets the controller's umask to 022 when Ansible loads its callbacks, before any task
        runs, so a file a task writes with no explicit mode gets the mode it has when the play
        is started from an ordinary terminal.
    requirements:
      - Enable with callbacks_enabled in ansible.cfg
"""

#: The umask a play is written for: Fedora's default for a login shell.
SYSTEM_UMASK = 0o022


class CallbackModule(CallbackBase):
    CALLBACK_VERSION = 2.0
    CALLBACK_TYPE = "notification"
    CALLBACK_NAME = "umask_pin"
    CALLBACK_NEEDS_ENABLED = True

    def __init__(self) -> None:
        super().__init__()
        inherited = os.umask(SYSTEM_UMASK)
        if inherited != SYSTEM_UMASK:
            self._display.vv(f"umask_pin: the launching shell's umask {inherited:04o} is replaced by "
                             f"{SYSTEM_UMASK:04o} for this run")
