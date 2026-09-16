"""The machine-readable form of the login report (Plan 00109, Tasks 3.2 and 4.1).

One producer, two consumers that cannot share a delivery:

* the **GNOME panel** on a desktop, which reads this file and renders sections;
* a **login-shell message** on a server, where `notify-send` has no session bus to
  reach and `graphical-session.target` never activates — so the desktop surface ends
  its play there and a server would otherwise get no drift reporting at all.

Splitting *when the check runs* from *when the user is told* is what makes the second
one workable. A `git fetch` on every SSH login would add latency to every login and can
hang; reading a cached document costs nothing. The check runs on its own schedule, this
file is what it leaves behind, and both consumers read it.

Three rules, each the answer to a way a status surface stops being one:

1. **Three states, distinct in the data.** `ok`, `findings`, `unavailable`. A neutral
   icon over an empty menu is what a healthy host looks like — and also what a missing
   file, an unparseable one and a crashed producer look like. Collapsing those is this
   plan's incident rebuilt one layer up, in the UI.
2. **`unavailable` is read from `Finding.checked`**, the producing check's own answer,
   never inferred from the wording. Substring-matching the prose was tried: it covered
   seven of the messages the checks emit and misfiled six.
3. **An absent document is ignorance, not health.** `container-watch` falls back to an
   empty findings list and is right to, because its subject is live processes. These
   facts are not live — a dead DKMS module for the running kernel stays true — so
   absence here has to read as "not checked".

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-panel.md
"""

from __future__ import annotations

import json
import os
import tempfile
from collections.abc import Callable

from helpers.host_health import probe_results

#: Bumped when the shape changes in a way a reader must notice. A consumer that finds a
#: schema it does not know reports `unavailable` rather than rendering what happens to
#: parse — see `read`.
SCHEMA_VERSION = 1

#: Checked, nothing to report.
OK = "ok"
#: Checked, and here is what is wrong.
FINDINGS = "findings"
#: NOT checked. Never a quiet state: a consumer must not show a neutral icon for it.
UNAVAILABLE = "unavailable"

#: A section's own statement of the population it examined, carried even when the
#: section is clean. Named here rather than spelled at each site: the acceptance gate
#: keys on it, and a key spelled twice is a key that can disagree with itself.
COVERAGE_KEY = "coverage"

#: The section id used to report on the document itself when it cannot be read. Named
#: rather than empty, so the consumer renders a reason instead of an absence.
SELF_SECTION = "status"

#: The one section whose findings are true only of the boot they were collected in:
#: DKMS state against the kernel that was running, and units that failed during it.
#:
#: Declared here, beside the schema, rather than in the producer that names the section
#: — because it is a CONSUMER that needs it. A finding like "no DKMS module installed
#: for the running kernel 7.1.9" is written at collection time and read later; after a
#: reboot it names a kernel that is no longer running while still reading as a claim
#: about now. `login_message` demotes this section on a kernel mismatch for that reason.
#: The other three sections — the ledger, play freshness, installed-vs-pinned — survive
#: a reboot unchanged, so demoting them too would be its own overclaim.
BOOT_SCOPED_SECTION = "post-boot-health"

#: The one Python source of truth for the file name. The panel is a second process in a
#: second language and has to find the same file; a mismatch does not announce itself,
#: because a panel looking at the wrong path reports `unavailable` for ever, which reads
#: exactly like a producer that has never run. Task 4.5 owes a gate comparing the
#: extension's literal against this.
FILE_NAME = "host-status.json"


def path(state_dir: str) -> str:
    """Where the document lives, given this host's `fedora-desktop` state directory.

    Host state, not repo state — it must not be committable, and it must survive a
    re-clone, for the same reason the ledger must. `ledger.state_dir` resolves it, and
    `GLib.get_user_state_dir()` applies the identical XDG rule on the reading side.
    """
    return os.path.join(state_dir, FILE_NAME)


def collected_kernel(document: object) -> str:
    """The kernel this document was collected under, or `""` when it does not say.

    Read defensively once, here, so a consumer naming the kernel in a message does not
    re-implement the guard: the document comes off disk and may be from another version,
    truncated or hand-edited.
    """
    if not isinstance(document, dict):
        return ""
    kernel = document.get("kernel")
    return kernel if isinstance(kernel, str) else ""


def is_boot_stale(document: object, *, running_kernel: str) -> bool:
    """Whether this document describes a boot other than the one now running.

    A property of the DOCUMENT, so it lives with the document rather than in whichever
    consumer noticed it first. There are two declared consumers and a predicate
    implemented in one of them is a question the other silently never asks — see
    `BOOT_SCOPED_SECTION` for what turns on the answer.

    Both sides must be known. `_cannot_read` carries `kernel: ""` and has already
    explained itself; an empty running kernel means "could not tell". Reporting a
    mismatch from either would be a finding manufactured out of ignorance, which is the
    inverse of this plan's rule and just as wrong.
    """
    collected = collected_kernel(document)
    return bool(collected and running_kernel and collected != running_kernel)


def unreadable_reasons(document: object) -> list[str]:
    """Every way this document's STRUCTURE could not be read, each named.

    `read` already turns absent, unparseable and unknown-schema into `unavailable`, on
    the rule that ignorance is not health. This covers the gap immediately after it: a
    document that parses, declares a schema this reader knows, and then carries sections
    it cannot interpret. A consumer that defensively substitutes "nothing" for each of
    those reads a malformed document as a clean host — and `SCHEMA_VERSION` guards only
    the top-level integer, so nothing else flags it.

    The sharpest case is a section whose `state` says `findings` while its `findings` is
    not a list: the document says something is wrong and the reader prints nothing.

    **Never raising and never going silent are not in conflict.** `collect` already
    shows the third option — a producer that raises becomes an `unchecked` finding
    naming its section — and this is the same answer for a reader.
    """
    if not isinstance(document, dict):
        return ["the host status is not a document this reader can interpret, so "
                "nothing in it has been read"]

    sections = document.get("sections")
    if not isinstance(sections, dict):
        return ["the host status file's sections could not be read, so no check's "
                "result has been read from it"]
    if not sections:
        # NO LEGITIMATE ORIGIN. `collect` guarantees a key per producer — four even when
        # every one of them raises — and `_cannot_read` emits one. So a document with no
        # sections is version skew, a truncation or a hand-edit, which is the population
        # this function exists for. Treating it as "no findings" is the same trade as
        # every other shape here, and on this surface it reads as a healthy host.
        return ["the host status names no checks at all, so nothing has been "
                "established about this host"]

    reasons: list[str] = []
    for name, entry in sections.items():
        if not isinstance(entry, dict):
            reasons.append(
                f"the {name} section could not be read, so nothing is known about "
                "that check"
            )
            continue
        for key in ("findings", "unchecked"):
            value = entry.get(key)
            if value is None:
                continue
            if not isinstance(value, list):
                reasons.append(
                    f"the {name} section's {key} could not be read, so what it "
                    "reported is not known"
                )
            elif any(not isinstance(item, str) for item in value):
                reasons.append(
                    f"the {name} section's {key} holds entries this reader cannot "
                    "show, so what they said is not known"
                )
    return reasons


def section(findings: list[probe_results.Finding], *, coverage: str = "") -> dict:
    """One section: its state, both groups kept apart, and what it drew them from.

    When a section has faults *and* things nobody could check, the state is `findings`
    — something known-wrong outranks something unknown — but the unchecked list is
    still carried. Dropping it there would show a partial picture as a complete one,
    which is the failure this plan exists for.

    `coverage` is the population the section actually examined, stated in its own
    numbers and carried even when the section is clean. Without it a consumer can only
    infer coverage from the ABSENCE of a complaint, and a section that examined nothing
    is then indistinguishable from one that examined everything and found it well — the
    defect this plan is named for, one level up. The key is omitted entirely when a
    section does not state one, so "this section reports no coverage" stays a different
    fact from "this section compared nothing".

    **The panel deliberately does not read this key, and `check_panel_contract` does not
    demand that it does.** The key exists for a machine consumer that must assert on a
    number — the acceptance gate does exactly that. A human reading the panel already
    gets the signal a different way: incomplete coverage produces an `unchecked` finding,
    which the panel renders and which moves the section to `unavailable`. Adding a
    second rendering of the same fact would put a sentence under every healthy section
    at every login. Written down because a document key nothing reads is normally a
    defect here, and this one is a decision.
    """
    broken = [finding.text for finding in findings if finding.checked]
    unchecked = [finding.text for finding in findings if not finding.checked]
    if broken:
        state = FINDINGS
    elif unchecked:
        state = UNAVAILABLE
    else:
        state = OK
    entry = {"state": state, "findings": broken, "unchecked": unchecked}
    if coverage:
        entry[COVERAGE_KEY] = coverage
    return entry


def build(
    *,
    sections: dict[str, list[probe_results.Finding]],
    kernel: str,
    at: str,
    handoff: str = "",
    coverage: dict[str, str] | None = None,
) -> dict:
    """The whole document. Plain JSON types throughout — JavaScript reads this.

    `coverage` maps a section id to that section's own statement of what it examined.
    Sections that state one carry it whether or not they found anything; a section
    absent from the map simply carries no such key, which is deliberately a different
    observation from stating that it compared nothing.

    `handoff` is where the Claude Code handoff file was written, or `""` when there is
    none. Carried HERE because this document is the panel's only data source: the
    handoff path is otherwise known solely to the login report, which a desktop user
    never sees, so an offer the panel cannot make is an offer that does not exist for
    them (Task 3.3).

    Always present, never conditional. A key that appears only when a handoff was
    written makes "no handoff" and "a document from before handoffs existed" the same
    observation to a reader, and this plan exists because two different things looked
    alike.
    """
    stated = coverage or {}
    return {
        "schema": SCHEMA_VERSION,
        "generated_at": at,
        "kernel": kernel,
        "handoff": handoff,
        "sections": {
            name: section(findings, coverage=stated.get(name, ""))
            for name, findings in sections.items()
        },
    }


def collect(
    producers: dict[str, Callable[[], list[probe_results.Finding]]],
) -> dict[str, list[probe_results.Finding]]:
    """Run each producer under its own guard, keyed by section id.

    Merged, not chained. A producer that raises becomes its own section's `unavailable`
    carrying the reason, and is **named**, so the reader learns which check stopped
    working rather than that something, somewhere, did. It never becomes a missing key,
    which a consumer would have to invent a meaning for, and never an empty list, which
    reads as health.

    This is the **only** implementation of that guard. `login_report.collect_sections`
    calls it rather than keeping a second copy, because the notification and the
    document must not be able to disagree about which checks ran — two copies of a rule
    this plan exists to enforce is two chances to fix only one of them.

    Insertion order is preserved, which is what keeps host health first in the report:
    something broken on this machine now outranks something that has merely drifted.
    """
    sections: dict[str, list[probe_results.Finding]] = {}
    for name, produce in producers.items():
        try:
            sections[name] = produce()
        except Exception as error:
            sections[name] = [
                probe_results.unchecked(f"the {name} check could not run: {error}")
            ]
    return sections


def write_atomic(path: str, document: dict) -> None:
    """Write via a temporary file and rename, so a reader never sees a half-written one.

    A consumer polling this path must not catch it mid-write and conclude the host is
    unparseable — which, by rule 3 above, it would report as `unavailable`. The same
    shape `helpers/containerwatch/cli.py` uses for `report.json`.
    """
    directory = os.path.dirname(path)
    os.makedirs(directory, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(dir=directory, prefix=".status-", suffix=".tmp")
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            json.dump(document, handle, indent=2)
        os.replace(temporary, path)
    except BaseException:
        if os.path.exists(temporary):
            os.unlink(temporary)
        raise


def _cannot_read(reason: str) -> dict:
    """A document describing why there is no document. Same shape, so every consumer
    renders it through the path it already has."""
    return {
        "schema": SCHEMA_VERSION,
        "generated_at": "",
        "kernel": "",
        # Empty, and it has to be: a document that could not be read cannot name a
        # handoff file, and inventing a path here would offer a command for a file this
        # branch has just finished saying nothing is known about.
        "handoff": "",
        "sections": {SELF_SECTION: section([probe_results.unchecked(reason)])},
    }


def read(path: str) -> dict:
    """The document, or a document saying why it could not be had.

    Never raises and never returns an empty-but-healthy-looking shape. The three ways
    this fails — absent, unparseable, a schema this does not know — are all reported as
    `unavailable`, because each means the same thing to a reader: nothing here has been
    established about this host.
    """
    try:
        with open(path, encoding="utf-8") as handle:
            document = json.load(handle)
    except FileNotFoundError:
        return _cannot_read(
            "no host status has been recorded yet, so nothing is known about this host"
        )
    except (OSError, ValueError) as error:
        return _cannot_read(f"the host status file could not be read: {error}")

    if not isinstance(document, dict) or document.get("schema") != SCHEMA_VERSION:
        # Rendering whatever happens to parse out of an unknown shape is how a consumer
        # reports confidently about a document it did not understand.
        return _cannot_read(
            f"the host status file declares schema {document.get('schema')!r}, "
            f"and this reader only understands {SCHEMA_VERSION}"
        )
    return document
