"""Every decision the play-ledger callback plugin defers to a tested module.

`ansible` is not importable by the interpreter that runs this repo's tests, so
logic left inside `callback_plugins/play_ledger.py` would be logic with no tests
at all — in the component every Phase 2 drift check trusts. The plugin is
therefore a pure adapter: it translates Ansible's callback events into calls on
`RunCollector` and the functions here, and decides nothing itself.

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-play-ledger.md
"""

from __future__ import annotations

import os
from typing import Any

from helpers.play_ledger import store

#: Printed to stderr when the ledger cannot be written. Ansible swallows an
#: exception raised inside a callback, so this line and the BROKEN sentinel are
#: the only two ways the failure ever surfaces.
FAILURE_MARKER = "LEDGER-WRITE-FAILED"

#: The exact command an operator should be able to paste. `cd` included, and not as
#: decoration: `python3 -m helpers.…` resolves the package off the CURRENT directory,
#: so the bare form is a ModuleNotFoundError from anywhere but the repo root. It was
#: printed bare on every play of every failing run, and quoted bare into a public
#: issue. One constant, so the two places that tell an operator about the hole cannot
#: give different instructions.
CHECKOUT_PLACEHOLDER = "<your fedora-desktop checkout>"
CLEAR_COMMAND = (
    f"cd {CHECKOUT_PLACEHOLDER} && "
    "python3 -m helpers.play_ledger.check_freshness --clear-broken"
)

#: CLI flags that mean the run applied nothing. Recording one of these would tell
#: Phase 2 the play is fresh on a host that never received it — which is the
#: precise failure this whole plan was written to catch.
_NO_OP_FLAGS = ("check", "listtasks", "listhosts", "listtags", "syntax")


def should_record(cliargs: dict[str, Any]) -> bool:
    """Whether this invocation actually applied anything worth ledgering."""
    return not any(bool(cliargs.get(flag)) for flag in _NO_OP_FLAGS)


def source_position(origin: Any, legacy: Any) -> Any:
    """Normalise Ansible's two shapes for "where did this play come from".

    ansible-core **2.19 removed `ansible_pos`** from the parsed mapping and put the
    same fact in an `Origin` tag, which `FieldAttributeBase.load_data` stores on the
    play object as `_origin` (`path`, `line_num`, `col_num`). Reading only the old
    shape made `play_source` refuse on EVERY play, so the ledger recorded nothing and
    marked itself `BROKEN` on every run — issue #46. It had never worked on 2.19.

    Both attribute names are Ansible internals and neither is promised, so this
    trusts neither: whichever is present wins, the new shape first because it is the
    one this branch's Ansible actually sets. When neither is, the answer is `None`
    and `play_source` refuses with its own message — a recorded hole, never a guess.

    Kept here rather than in the callback because `ansible` is not importable by the
    interpreter that runs these tests, so anything left in the plugin is untested by
    construction. This function names no Ansible type: it reads duck-typed attributes
    the caller has already pulled off the play.
    """
    path = getattr(origin, "path", None)
    if isinstance(path, str) and path:
        return (path, getattr(origin, "line_num", None), getattr(origin, "col_num", None))
    return legacy or None


def play_source(position: Any) -> str:
    """The play's source file, from Ansible's `(file, line, column)` position.

    Refuses to invent a path. A record with no `play` cannot be joined to
    anything, so a missing position must become a recorded hole rather than an
    unusable row nobody notices.
    """
    if not position:
        raise ValueError(
            "the play carries no source position, so its file cannot be named; "
            "the ledger will not record a play it cannot identify"
        )
    filename = position[0]
    if not isinstance(filename, str) or not filename:
        raise ValueError(f"play source position {position!r} has no usable filename")
    return filename


#: The file name `ansible.cli.adhoc` gives the in-memory Playbook it wraps an
#: `ansible <pattern> -m <module>` play in, before sending it to
#: `v2_playbook_on_start`. It is the CLI's own statement that no playbook file
#: exists, which makes it a sturdier discriminator than the play's name
#: ("Ansible Ad-Hoc"): a playbook author can choose any name, but only that CLI
#: sets this file name.
ADHOC_PLAYBOOK_FILE = "__adhoc_playbook__"


def names_playbooks(cliargs: dict[str, Any]) -> bool:
    """Whether the command line named playbook files — `ansible-playbook` and nothing else.

    `context.CLIARGS['args']` is where `ansible-playbook` puts its files, as a list.
    `ansible` puts its host pattern there as a string, and `ansible-console` leaves it
    None. So a list of non-empty strings is the positive mark of a run whose plays have
    files behind them. The shapes are asserted against the real CLIs in
    test_source_position_against_real_ansible, which fails if ansible-playbook stops
    naming its files here rather than letting every run go unrecorded.
    """
    files = cliargs.get("args")
    return (
        isinstance(files, list | tuple)
        and len(files) > 0
        and all(isinstance(name, str) and name for name in files)
    )


def play_to_record(playbook_file: Any, position: Any, *, names_playbooks: bool) -> str | None:
    """The play file to ledger, or `None` when the run has no play file at all.

    The ledger's unit is a play file at a commit. A play from `ansible -m` or
    `ansible-console` has no file, so it is skipped rather than recorded as a hole;
    otherwise any such command from the checkout marks the ledger BROKEN. Either signal
    is enough on its own: `ansible.cli.adhoc`'s marker file name, or a command line that
    named no playbook (`ansible-console` sends no playbook-start event at all, so it has
    no marker to set). A playbook run keeps the refusal `play_source` gives, and a
    missing playbook-start event there is not evidence of a fileless run.
    """
    if playbook_file == ADHOC_PLAYBOOK_FILE or not names_playbooks:
        return None
    return play_source(position)


def repo_root_from(plugin_file: str) -> str:
    """The checkout root, derived from the plugin's own location.

    Never from the cwd: a callback runs wherever the operator happened to be, and
    `playbooks/` may be reached by an absolute path from anywhere.
    """
    return os.path.dirname(os.path.dirname(os.path.realpath(plugin_file)))


def write_records(
    base: str, records: list[dict[str, Any]], *, commit: str, at: str
) -> None:
    """Create the ledger if needed, then append each record. Raises on any failure.

    The ledger is created even when there are no records: that write is what dates
    it, and without a date a later silence cannot be told from "the ledger did not
    exist when that play ran".

    A failure part-way through leaves the earlier records written, deliberately.
    They are true, and the caller's `record_failure` sentinel is what tells Phase 2
    the run is incomplete — a partial history plus a known hole beats discarding
    rows that actually happened.
    """
    store.ensure_ledger(base, commit=commit, at=at)
    for record in records:
        store.append_record(base, record)


def record_failure(base: str, *, error: str, at: str) -> str:
    """Record a ledger hole and return the line the plugin must print to stderr.

    This is the last resort, so it does not raise — a raise here would be
    swallowed by Ansible exactly like the failure it is reporting, and the
    operator would see nothing at all. If even the sentinel cannot be written,
    the returned line says so and carries the original cause.
    """
    try:
        store.mark_broken(base, error=error, at=at)
    except OSError as sentinel_error:
        return (
            f"{FAILURE_MARKER}: {error} — and the BROKEN sentinel at {base} could not "
            f"be written either ({sentinel_error}), so nothing on disk records this"
        )
    # The remedy travels with the report. Without it the operator is told the ledger
    # is broken, on every play of every run, and given nothing to do about it — which
    # is how issue #46 read on two hosts.
    return (
        f"{FAILURE_MARKER}: {error} — recorded in {store.ledger.sentinel_path(base)}. "
        f"Once the cause is fixed, clear it with: {CLEAR_COMMAND}"
    )
