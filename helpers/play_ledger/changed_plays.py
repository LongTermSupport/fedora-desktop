"""Which plays run here have to run again, for `run.bash --changed` (Plan 00141).

A play has to run again when any of its inputs changed between the commit it last ran
from and the working tree, when that last run did not succeed, or when it ran from a
checkout with one of its inputs uncommitted. Its inputs are its own file and every checkout path it
deploys, as `helpers/self_update/affected_plays.py` reads them from the play text. The
group_vars file every play loads is judged a variable at a time instead: a play has to run
again when a variable of it that changed is one the play's input files name
(`helpers/play_ledger/group_vars.py` gives the rule). A
run with an input uncommitted is run again because what it deployed may be an edit since
reverted, which no diff can show. Which paths were uncommitted is recorded per run, so a
stray file no play reads stales nothing; a record that cannot name them all (one from before
they were kept, or a list cut at its limit) stales the play.

Run it:

    python3 -m helpers.play_ledger.changed_plays [--repo-root DIR]

stdout carries only marker lines, in the order the plays should run: the order
`playbook-main.yml` imports them, then the plays it does not import, by path. Without
`--all`, stderr says why each RUN play runs, one `changed-plays: <play>: <reasons>` line
each, for the person reading `run.bash --changed`.

    RUN <play>                  run it again
    UNRESOLVED <play> <where>   not changed as far as can be told, but it holds a reference
                                the mapper cannot follow, so a change there would be missed
    GONE <play>                 it ran here, and the checkout no longer has it
    NEVER <play>                it has no ledger record here, and none of the files it
                                deploys shows that it ran here: not run (issue #323)
    UNJUDGED <play>             it has no ledger record here, and deploys no file that can
                                be looked for, so whether it ran here cannot be told: not run

A removed play that `retired-plays.json` maps to a successor is not GONE: the successor
is RUN until a successful run of it, at a commit without the old play, has taken it over.

A host play of the checkout with no ledger record here at all (it ran before the ledger
existed, or not through run.bash) is RUN when a file its copy and template tasks deploy is
on this host and shows that it ran, for its inputs may have changed since and nothing
records when it last ran; its reason line names those files. `deployed_path` says which
`dest:` values can be looked for. A file found counts only when no other host play deploys
it too (which put it there cannot be told) and no package owns it, or rpm reports its
digest changed from the package's own: a package's file as shipped is there because the
package is (`package_state`; when rpm cannot answer, the file does not count). With no
such file the play is NEVER, or UNJUDGED when it deploys nothing that can be looked for:
running every play this host has no record of would install what the owner never chose.
With `--all` these are `PLAY unrecorded`, `PLAY never` and `PLAY unjudged`.

The blind spot: a play that only installs packages, enables services or edits files in
place (no copy or template `dest:` this can resolve) leaves nothing this looks for, so on
a host where it ran before the ledger it is UNJUDGED, never RUN, however stale it is.

Only host plays count, the ones under `playbooks/imports/`. A probe under untracked/ or a
play under `playbooks/dev/`, which works on the repo, is never mentioned. Nor is a play
the repo never tracked at the commit it ran from. Exit 0 is an answer, empty or not.
Exit 2 is no answer: a broken or unreadable ledger, or a git call that failed. Then
stdout is empty, because a partial list offered as the whole one would leave plays
silently unrun.

`judge` is THE definition of which plays have to run again (Plan 00214, issue #110). The
health report's play-freshness check (`check_freshness`) calls it too, so the report and
`run.bash --changed` cannot name different plays.
"""

from __future__ import annotations

import argparse
import os
import posixpath
import pwd
import re
import shlex
import subprocess
import sys
from collections import Counter
from collections.abc import Callable, Collection
from dataclasses import dataclass, field
from typing import TextIO

from helpers.play_ledger import git_history, group_vars, ledger, retired, store
from helpers.self_update import affected_plays

EXIT_OK = 0
EXIT_NO_ANSWER = 2

#: The states `--all` prints. STALE is changed inputs or a dirty run; FAILED a last run that
#: did not succeed; UNRESOLVED has a reference the mapper cannot follow; CURRENT is none.
#: UNRECORDED has no ledger record here and a file of it here shows it ran; NEVER has no
#: record and none of the files it names shows that; UNJUDGED has no record and names no
#: file that can be looked for.
STATE_STALE = "stale"
STATE_FAILED = "failed"
STATE_UNRESOLVED = "unresolved"
STATE_CURRENT = "current"
STATE_UNRECORDED = "unrecorded"
STATE_NEVER = "never"
STATE_UNJUDGED = "unjudged"

#: The states `run.bash --changed` runs.
RUN_STATES = (STATE_STALE, STATE_FAILED, STATE_UNRECORDED)

#: The one template a deployed path may hold and still be looked for: the login running this.
_USER_LOGIN = re.compile(r"\{\{\s*user_login\s*\}\}")

#: What rpm says of a deployed file (`package_state`). Only UNOWNED and MODIFIED show that a
#: play put it there; UNKNOWN is rpm not answering, and never counts as either.
PACKAGE_UNOWNED = "unowned"
PACKAGE_PRISTINE = "pristine"
PACKAGE_MODIFIED = "modified"
PACKAGE_UNKNOWN = "unknown"
_EVIDENCE = (PACKAGE_UNOWNED, PACKAGE_MODIFIED)

#: One `rpm -V` line: nine test flags (the third the file digest), an optional attribute
#: letter (`c` for a config file), then the path.
_VERIFY_LINE = re.compile(r"^([.SM5DLUGTP?]{9})\s+(?:[cdglr]\s+)?(/.*)$")

#: How many changed paths (or Go packages, or commands) a reason names before it counts the rest.
PATHS_SHOWN = 3

def forget_command(repo_root: str, play: str) -> str:
    """The command that clears a failed play on a host where it cannot apply (issue #278).

    Spelled with `cd` to this checkout, because `python3 -m` resolves off the cwd. The
    line goes to the person at this terminal, so it names the real path.
    """
    return (f"cd {shlex.quote(repo_root)} && "
            f"python3 -m helpers.play_ledger.check_freshness --forget-play {shlex.quote(play)}")

MAIN_PLAYBOOK ="playbooks/playbook-main.yml"
_IMPORT_KEY = re.compile(r"^\s*-\s*(?:ansible\.builtin\.)?import_playbook:")
_IMPORT = re.compile(r"^\s*-\s*(?:ansible\.builtin\.)?import_playbook:\s*([\w./-]+\.ya?ml)\s*(?:#.*)?$")


class NoAnswer(Exception):
    """No play can be judged. `lines` say why, each one sentence, the remedy last."""

    def __init__(self, *lines: str) -> None:
        super().__init__(" ".join(lines))
        self.lines = lines


@dataclass(frozen=True)
class Judgement:
    """Every host play run here, judged. The one answer both callers read.

    `order` is every play in `states` or `gone`, in the order a catch-up runs them.
    `changed_inputs` holds, for a stale play, the changed checkout paths among its inputs
    (none when it is stale only because it ran dirty, which `dirty` says). `dirty` is the
    plays whose last run had an input uncommitted: `dirty_inputs` names those inputs, and a
    play in `dirty` but not there ran from a record that cannot name them all. `took_over`
    names, for a successor that has to run, the removed plays it takes over. `not_judged`
    is every ledgered play outside `playbooks/imports/`, which no catch-up runs.
    `changed_go` holds, for a stale play, the Go packages among its changed inputs, each
    with the commands built from it. `changed_vars` holds, for a stale play, the changed
    variables of the group_vars file it reads. `deployed` holds, for a host play with no
    ledger record here (UNRECORDED), the files of it here that show it ran. `never` is
    every host play with no record here that names files to look for, none of which shows
    that, and `unjudged` every one that names none; both are in run order, neither is in
    `order`, and no catch-up runs them.
    """

    order: tuple[str, ...]
    states: dict[str, str]
    gone: tuple[str, ...]
    unresolved: dict[str, list[str]]
    changed_inputs: dict[str, list[str]]
    dirty: frozenset[str]
    took_over: dict[str, list[str]]
    not_judged: tuple[str, ...]
    changed_go: dict[str, dict[str, frozenset[str]]] = field(default_factory=dict)
    changed_vars: dict[str, list[str]] = field(default_factory=dict)
    dirty_inputs: dict[str, list[str]] = field(default_factory=dict)
    deployed: dict[str, list[str]] = field(default_factory=dict)
    never: tuple[str, ...] = ()
    unjudged: tuple[str, ...] = ()

    @property
    def runs(self) -> list[str]:
        """The plays `run.bash --changed` runs, in the order it runs them."""
        return [play for play in self.order if self.states.get(play) in RUN_STATES]


def main_order(repo_root: str) -> list[str]:
    """The plays `playbook-main.yml` imports, in its order, as repo-relative paths.

    That order carries real dependencies (play-ZZ-repo-cleanup runs after the plays that
    add COPRs), so a catch-up run follows it. An import line it cannot read raises,
    because the play it names would drop to the end of the run without a word. A missing
    file raises.
    """
    with open(os.path.join(repo_root, MAIN_PLAYBOOK), encoding="utf-8") as handle:
        text = handle.read()
    here = posixpath.dirname(MAIN_PLAYBOOK)
    order = []
    for number, line in enumerate(text.splitlines(), start=1):
        if not _IMPORT_KEY.match(line):
            continue
        match = _IMPORT.match(line)
        if match is None:
            raise ValueError(f"{MAIN_PLAYBOOK}:{number}: cannot read the play this imports: {line.strip()}")
        order.append(posixpath.normpath(posixpath.join(here, match.group(1))))
    return order


def changed_since(repo_root: str, commit: str) -> list[str]:
    """Paths that differ between `commit` and the working tree, committed or not.

    `--no-renames`, so a moved file names both ends. Untracked files are not seen: a
    play only deploys what the checkout tracks.
    """
    if not commit or commit.startswith("-"):
        raise ValueError(f"{commit!r} is not a commit")
    result = subprocess.run(
        ["git", "-C", repo_root, "diff", "--name-only", "--no-renames", "-z", commit, "--"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        raise ValueError(f"git diff {commit} failed: {result.stderr.strip()}")
    return [path for path in result.stdout.split("\0") if path]


def deployed_path(dest: str, login: str) -> str | None:
    """The host path a play's `dest:` names, or None when it gives no evidence (issue #323).

    A literal absolute path names itself, and so does one whose only template is
    `{{ user_login }}`, filled in with `login`, the login running this helper (the one
    run.bash runs the plays as). Any other template (`{{ item }}`, a directory variable, a
    filter on the login) cannot be evaluated without Ansible, so it is no evidence either
    way. Nor is a relative or `~` path, or one ending in `/`, a directory the task copies
    into, which can be there whatever ran.
    """
    path = _USER_LOGIN.sub(login, dest)
    if "{{" in path or "{%" in path or not path.startswith("/") or path.endswith("/"):
        return None
    return path


def _deployed_file(path: str) -> bool:
    """Whether `path` is on this host as something other than a directory: a directory
    such as /usr/local/bin is there whatever ran, so it is no evidence."""
    return os.path.lexists(path) and not os.path.isdir(path)


def _run_rpm(args: list[str]) -> subprocess.CompletedProcess:
    """`rpm <args>` in the C locale, whose messages `package_state` reads. `check=False`:
    rpm exits 1 both for an unowned file and for a verify that found a difference, so
    `package_state` judges every exit status itself."""
    return subprocess.run(["rpm", *args], capture_output=True, text=True, check=False,
                          env={**os.environ, "LC_ALL": "C"})


def package_state(path: str, *, rpm: Callable[[list[str]], subprocess.CompletedProcess] | None = None) -> str:
    """What rpm says of the deployed file `path`: one of the PACKAGE_ values.

    A file no package owns (`rpm -qf` says so) is UNOWNED. One a package owns is MODIFIED
    only when `rpm -Vf` flags its digest (`5`) as differing from the package's own, and
    PRISTINE otherwise: a package file left as shipped is there because the package is.
    Every other answer is UNKNOWN: no rpm, an error on stderr, an exit status neither
    command gives for a result, or a digest rpm could not read (`?`, a file this login
    cannot read). `rpm` is the test seam.
    """
    rpm = rpm or _run_rpm
    try:
        owner = rpm(["-qf", path])
        if (owner.returncode == 1 and not owner.stderr
                and owner.stdout.strip() == f"file {path} is not owned by any package"):
            return PACKAGE_UNOWNED
        if owner.returncode != 0 or owner.stderr or not owner.stdout.strip():
            return PACKAGE_UNKNOWN
        verify = rpm(["-Vf", "--nodeps", "--noscripts", path])
    except OSError:
        return PACKAGE_UNKNOWN
    if verify.returncode not in (0, 1) or verify.stderr:
        return PACKAGE_UNKNOWN
    digests = [match.group(1)[2] for match in map(_VERIFY_LINE.match, verify.stdout.splitlines())
               if match and match.group(2) == path]
    if "5" in digests:
        return PACKAGE_MODIFIED
    return PACKAGE_UNKNOWN if "?" in digests else PACKAGE_PRISTINE


def _package_state(path: str) -> str:
    return package_state(path)


def _login() -> str:
    return pwd.getpwuid(os.getuid()).pw_name


def judge(
    latest: dict[str, dict],
    *,
    repo_root: str,
    recorded: Collection[str] | None = None,
    changed_since: Callable[[str, str], list[str]] = changed_since,
    exists_now: Callable[[str, str], bool] | None = None,
    tracked_at: Callable[[str, str, str], bool] | None = None,
    inputs_of: Callable[[str, str], affected_plays.Inputs] | None = None,
    main_order: Callable[[str], list[str]] = main_order,
    retired_plays: Callable[[str], dict[str, str]] = retired.load,
    group_vars_at: Callable[[str, str], str | None] = group_vars.text_at,
    group_vars_now: Callable[[str], str | None] = group_vars.text_now,
    play_text: Callable[[str, affected_plays.Inputs], str] = group_vars.play_text,
    candidates: Callable[[str], list[str]] = affected_plays.candidate_plays,
    exists: Callable[[str], bool] = _deployed_file,
    package_state: Callable[[str], str] | None = None,
    login: Callable[[], str] = _login,
) -> Judgement:
    """Judge every host play in `latest` (the ledger folded to each play's last run), and
    every host play of the checkout with no record at all (issue #323).

    A change to the group_vars file is judged a variable at a time (`group_vars`): it makes
    stale the plays that read a variable that changed, and no other.

    `recorded` is every play the ledger holds any record of (`ledger.recorded_plays`), the
    plays `latest` leaves out (a narrowed or a forgotten one) included; it defaults to the
    plays in `latest`. A host play outside it is UNRECORDED when a file it deploys here
    shows it ran (`_unrecorded` says which do), which a run from before the ledger or not
    through run.bash leaves; else it is NEVER, or UNJUDGED when it names no file that can be
    looked for. Neither is run.

    Raises `NoAnswer` rather than return a partial judgement. The callables are test seams.
    """
    exists_now = exists_now or (lambda root, play: os.path.isfile(os.path.join(root, play)))
    tracked_at = tracked_at or (
        lambda root, commit, play: git_history.path_exists_at(root, commit, play)
    )
    read_inputs = inputs_of or affected_plays.play_inputs
    inputs_seen: dict[str, affected_plays.Inputs] = {}

    def inputs_of(root: str, play: str) -> affected_plays.Inputs:
        """Each play's inputs read once: the unrecorded plays' judgement reads every play's."""
        if play not in inputs_seen:
            inputs_seen[play] = read_inputs(root, play)
        return inputs_seen[play]

    try:
        position = {play: index for index, play in enumerate(main_order(repo_root))}
    except (OSError, ValueError) as error:
        raise NoAnswer(f"cannot read the play order from {MAIN_PLAYBOOK}: {error}") from error

    def run_order(play: str) -> tuple[bool, int, str]:
        return (play not in position, position.get(play, 0), play)

    prefix = affected_plays.CANDIDATE_DIR + "/"
    host_plays = sorted((play for play in latest if play.startswith(prefix)), key=run_order)

    gone: list[str] = []
    unresolved: dict[str, list[str]] = {}
    diffs: dict[str, list[str]] = {}
    states: dict[str, str] = {}
    changed_inputs: dict[str, list[str]] = {}
    changed_go: dict[str, dict[str, frozenset[str]]] = {}
    changed_vars: dict[str, list[str]] = {}
    vars_since: dict[str, set[str]] = {}
    dirty: set[str] = set()
    dirty_inputs: dict[str, list[str]] = {}
    for play in host_plays:
        record = latest[play]
        commit = record["commit"]
        try:
            if not exists_now(repo_root, play):
                if tracked_at(repo_root, commit, play):
                    gone.append(play)
                continue
            if commit not in diffs:
                diffs[commit] = changed_since(repo_root, commit)
            inputs = inputs_of(repo_root, play)
            if group_vars.GROUP_VARS in diffs[commit] and commit not in vars_since:
                vars_since[commit] = group_vars.changed_names(group_vars_at(repo_root, commit),
                                                              group_vars_now(repo_root))
            names = vars_since.get(commit, set())
            read = group_vars.names_read(names, play_text(repo_root, inputs)) if names else []
        except Exception as error:
            raise NoAnswer(
                f"cannot judge {play}, which last ran from {commit}: {error}",
                f"run it by name instead: ./run.bash {play}",
            ) from error
        if record["outcome"] != "ok":
            states[play] = STATE_FAILED
            continue
        changed = [path for path in diffs[commit] if affected_plays.affects(inputs, path)]
        if read:
            changed_vars[play] = read
        dirty_paths = ledger.recorded_dirty_paths(record)
        if dirty_paths is None:
            dirty.add(play)
        else:
            touched = [path for path in dirty_paths if _reads(inputs, path)]
            if touched:
                dirty.add(play)
                dirty_inputs[play] = touched
        if play in dirty or changed or read:
            states[play] = STATE_STALE
            changed_inputs[play] = changed
            go = affected_plays.go_changes(inputs, changed)
            if go:
                changed_go[play] = {package: frozenset(names) for package, names in go.items()}
            continue
        if inputs.unresolved:
            unresolved[play] = list(inputs.unresolved)
            states[play] = STATE_UNRESOLVED
        else:
            states[play] = STATE_CURRENT

    deployed, unjudged = _unrecorded(
        set(latest) if recorded is None else set(recorded), repo_root=repo_root,
        candidates=candidates, inputs_of=inputs_of, exists=exists,
        package_state=package_state or _package_state, login=login)
    for play, found in deployed.items():
        if found:
            states[play] = STATE_UNRECORDED

    try:
        gone, took_over = _retire(gone, latest=latest, repo_root=repo_root,
                                  retired_plays=retired_plays, tracked_at=tracked_at)
    except Exception as error:
        raise NoAnswer(
            f"cannot judge the removed plays against the retired-plays map: {error}"
        ) from error
    for successor in took_over:
        if states.get(successor) != STATE_FAILED:
            states[successor] = STATE_STALE
            changed_inputs.setdefault(successor, [])
            unresolved.pop(successor, None)

    return Judgement(
        order=tuple(sorted(set(states) | set(gone), key=run_order)),
        states=states,
        gone=tuple(gone),
        unresolved=unresolved,
        changed_inputs=changed_inputs,
        dirty=frozenset(dirty),
        took_over=took_over,
        not_judged=tuple(sorted(play for play in latest if not play.startswith(prefix))),
        changed_go=changed_go,
        changed_vars=changed_vars,
        dirty_inputs=dirty_inputs,
        deployed={play: found for play, found in deployed.items() if found},
        never=tuple(sorted((play for play, found in deployed.items()
                            if not found and play not in states and play not in unjudged),
                           key=run_order)),
        unjudged=tuple(sorted(unjudged - set(states), key=run_order)),
    )


def _unrecorded(
    recorded: set[str],
    *,
    repo_root: str,
    candidates: Callable[[str], list[str]],
    inputs_of: Callable[[str, str], affected_plays.Inputs],
    exists: Callable[[str], bool],
    package_state: Callable[[str], str],
    login: Callable[[], str],
) -> tuple[dict[str, list[str]], set[str]]:
    """For each host play of the checkout with no ledger record, the files of it here that
    show it ran, each once, in the order the play names them; and the plays among them
    that name no file that can be looked for (`deployed_path`). Raises `NoAnswer`.

    A file shows a run when it is here (`exists`), no other host play of the checkout
    deploys it (which of them put it there cannot be told), and no package owns it or its
    digest differs from the package's own (`package_state`).
    """
    try:
        every = candidates(repo_root)
        plays = [play for play in every if play not in recorded]
        user = login() if plays else ""
    except Exception as error:
        raise NoAnswer(f"cannot list the host plays of the checkout: {error}") from error
    if not plays:
        return {}, set()
    paths: dict[str, list[str]] = {}
    for play in every:
        try:
            resolved = (deployed_path(dest, user) for dest in inputs_of(repo_root, play).dests)
            paths[play] = [path for path in dict.fromkeys(resolved) if path is not None]
        except Exception as error:
            raise NoAnswer(
                f"cannot read the files {play} deploys, to judge the plays with no ledger "
                f"record here: {error}",
                f"run a play by name instead: ./run.bash {play}",
            ) from error
    declared = Counter(path for play in every for path in paths[play])
    found: dict[str, list[str]] = {}
    for play in plays:
        try:
            found[play] = [path for path in paths[play] if declared[path] == 1 and exists(path)
                           and package_state(path) in _EVIDENCE]
        except Exception as error:
            raise NoAnswer(
                f"cannot judge {play}, which has no ledger record here: {error}",
                f"run it by name instead: ./run.bash {play}",
            ) from error
    return found, {play for play in plays if not paths[play]}


def _reads(inputs: affected_plays.Inputs, path: str) -> bool:
    """Whether an uncommitted `path` at a play's last run could have changed what it deployed.

    The same inputs a committed change is judged against, and the group_vars file every play
    loads: which of its variables an uncommitted edit touched is not recorded, so it counts
    for every play.
    """
    return path == group_vars.GROUP_VARS or affected_plays.affects(inputs, path)


def _shown(items: list[str], noun: str = "") -> str:
    """The first PATHS_SHOWN of `items`, and a count of the rest."""
    more = len(items) - PATHS_SHOWN
    text = ", ".join(items[:PATHS_SHOWN])
    if more > 0:
        text += f" and {more} more" + (f" {noun}{'' if more == 1 else 's'}" if noun else "")
    return text


def reasons(judgement: Judgement, play: str) -> list[str]:
    """Why a stale play has to run again, one reason a line, for a person to read.

    A changed Go file is named as its package, with the commands built from it, on a line
    of its own, so other changed paths can never crowd it into a count.
    """
    lines = []
    if play in judgement.deployed:
        lines.append("never ran through run.bash here; its files are deployed: "
                     f"{_shown(judgement.deployed[play], 'file')}")
    lines += [
        f"it takes over {old}, which the checkout removed"
        for old in judgement.took_over.get(play, [])
    ]
    go = judgement.changed_go.get(play, {})
    paths = [path for path in judgement.changed_inputs.get(play, [])
             if posixpath.dirname(path) not in go]
    if paths:
        lines.append(f"changed: {_shown(paths)}")
    if go:
        commands = sorted(set().union(*go.values()))
        lines.append(f"changed Go packages: {_shown(list(go))} (built into {_shown(commands, 'command')})")
    variables = judgement.changed_vars.get(play, [])
    if variables:
        lines.append(f"changed variables of {group_vars.GROUP_VARS}: {_shown(variables, 'variable')}")
    if play in judgement.dirty_inputs:
        lines.append(f"it last ran with uncommitted changes to its inputs: "
                     f"{_shown(judgement.dirty_inputs[play])}")
    elif play in judgement.dirty:
        lines.append("it last ran from a checkout with uncommitted changes, "
                     "and the ledger does not name them all")
    return lines


def run(
    *,
    base: str,
    repo_root: str,
    stdout: TextIO,
    stderr: TextIO,
    list_all: bool = False,
    **seams: Callable,
) -> int:
    """Print the marker lines and return the exit status. `seams` go to `judge`.

    `list_all` prints every play judged instead, as `PLAY <state> <play>` (the judgement
    above, for the menu of `run.bash --rerun`), with GONE plays named and never offered.
    """
    broken = store.broken_reason(base)
    if broken is not None:
        stderr.write(f"changed-plays: the play ledger has a recorded hole, so it cannot say what ran: {broken}\n")
        return EXIT_NO_ANSWER
    try:
        lines = store.read_lines(base)
        latest = ledger.fold_latest_full(lines)
        ran = ledger.fold_latest(lines)
        recorded = ledger.recorded_plays(lines)
    except ValueError as error:
        stderr.write(f"changed-plays: the ledger cannot be read: {error}\n")
        return EXIT_NO_ANSWER

    try:
        judgement = judge(latest, repo_root=repo_root, recorded=recorded, **seams)
    except NoAnswer as no_answer:
        stderr.writelines(f"changed-plays: {line}\n" for line in no_answer.lines)
        return EXIT_NO_ANSWER

    runs = set(judgement.runs)
    for play in judgement.order:
        if play in judgement.gone:
            stdout.write(f"GONE {play}\n")
        elif list_all:
            stdout.write(f"PLAY {judgement.states[play]} {play}\n")
        elif play in runs:
            stdout.write(f"RUN {play}\n")
            why = ([f"its last full run here did not succeed; if it cannot apply on this host, "
                    f"clear it with: {forget_command(repo_root, play)}"]
                   if judgement.states[play] == STATE_FAILED else reasons(judgement, play))
            if why:
                stderr.write(f"changed-plays: {play}: {'; '.join(why)}\n")
        elif play in judgement.unresolved:
            stdout.writelines(f"UNRESOLVED {play} {where}\n" for where in judgement.unresolved[play])
    for state, plays in ((STATE_NEVER, judgement.never), (STATE_UNJUDGED, judgement.unjudged)):
        stdout.writelines(f"PLAY {state} {play}\n" if list_all else f"{state.upper()} {play}\n"
                          for play in plays)
    if not list_all:
        stderr.writelines(
            f"changed-plays: {play}: its last run here, narrowed by "
            f"{ledger.describe_narrowing(ran[play]['narrowed'])}, did not succeed; only a full "
            "run decides the play, so it is not run for that\n"
            for play in failed_narrowed_runs(ran, latest)
        )
    return EXIT_OK


def failed_narrowed_runs(ran: dict[str, dict], full: dict[str, dict]) -> list[str]:
    """The host plays whose latest run was narrowed, did not succeed, and came after their
    latest full run (issue #278). Named, never judged: the run covered part of the play."""
    prefix = affected_plays.CANDIDATE_DIR + "/"
    return [
        play for play, record in sorted(ran.items())
        if play.startswith(prefix) and not ledger.is_full_run(record)
        and record["outcome"] != "ok"
        and record.get("finished", "") > full.get(play, {}).get("finished", "")
    ]


def _retire(
    gone: list[str],
    *,
    latest: dict[str, dict],
    repo_root: str,
    retired_plays: Callable[[str], dict[str, str]],
    tracked_at: Callable[[str, str, str], bool],
) -> tuple[list[str], dict[str, list[str]]]:
    """The GONE plays left once the retired-plays map is applied, and the successors to run,
    each with the removed plays it takes over.

    A removed play the map names is not GONE: its successor took it over. It needs nothing
    once the successor's latest run succeeded at a commit without the old play: a run that
    failed early may never have reached the absorbed tasks. Until then, running the
    successor is the catch-up. The map is read only when something is gone, and checked
    against HEAD in full.
    """
    if not gone:
        return gone, {}
    mapping = retired_plays(repo_root)
    retired.validate(mapping, exists_at_head=lambda path: tracked_at(repo_root, "HEAD", path))
    left: list[str] = []
    successors: dict[str, list[str]] = {}
    for play in gone:
        successor = mapping.get(play)
        if successor is None:
            left.append(play)
            continue
        record = latest.get(successor)
        absorbed = (
            record is not None
            and record["outcome"] == "ok"
            and not tracked_at(repo_root, record["commit"], play)
        )
        if not absorbed:
            successors.setdefault(successor, []).append(play)
    return left, successors


def _repo_root_default() -> str:
    """This file is `<repo>/helpers/play_ledger/changed_plays.py`."""
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.realpath(__file__))))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="List the plays run here whose inputs changed since (run.bash --changed)."
    )
    parser.add_argument("--repo-root", default=_repo_root_default())
    parser.add_argument("--all", action="store_true",
                        help="list every play run here with its state (run.bash --rerun)")
    arguments = parser.parse_args(argv)
    base = ledger.ledger_dir(os.environ, os.path.expanduser("~"))
    return run(base=base, repo_root=arguments.repo_root, stdout=sys.stdout, stderr=sys.stderr,
               list_all=arguments.all)


if __name__ == "__main__":
    sys.exit(main())
