"""Which variables of the group_vars file changed, and which of them a play reads (Plan 00192
Task 3.11), for `changed_plays.judge`.

Every play loads `environment/localhost/group_vars/desktop.yml`, so the file is an input of no
one play: counting it would rerun every play after any edit to it. Instead a change to it makes
a play stale when a variable that changed is one the play reads.

**Which variables changed.** The file's top-level keys, each with its block (the key's line and
the indented lines under it, whole-line comments and blank lines left out), are compared
between the commit the play last ran from and the working tree. A key whose block differs, or
that is in only one of the two, changed. A key whose value names a changed key, as a whole
word, changed too, transitively (`provisioning_profile` is computed from
`_systemd_default_target`). A file present on one side only changes every key it has. A line at
the left margin that is not a key, a comment, a blank or a document marker raises: a variable
the reader cannot see would be a change it silently drops.

**Which a play reads.** A changed name read as a whole word (not inside a longer name) in the
text of the play's inputs, as `affected_plays.play_inputs` gives them: its own file, every task,
vars, template and other file it names, and every file under a directory it names. A Go package
it builds is not read: Go code never sees an Ansible variable. Undecodable (binary) files are
left out. A file that names the variable but does not template it (a copied file, a comment)
reruns the play anyway: the rule errs towards a rerun, never a miss.

No YAML parser: helpers are stdlib-only (helpers/CLAUDE.md).
"""

from __future__ import annotations

import os
import re
import subprocess
from collections.abc import Iterable, Mapping

from helpers.self_update.affected_plays import Inputs

GROUP_VARS = "environment/localhost/group_vars/desktop.yml"

_KEY = re.compile(r"^([A-Za-z_][A-Za-z0-9_]*)\s*:(?:\s|$)")
_DOCUMENT_MARKER = re.compile(r"^(?:---|\.\.\.)\s*$")


def _word(name: str) -> re.Pattern[str]:
    return re.compile(rf"(?<![A-Za-z0-9_]){re.escape(name)}(?![A-Za-z0-9_])")


def top_level(text: str) -> dict[str, str]:
    """Each top-level key, mapped to its block without comment and blank lines."""
    blocks: dict[str, list[str]] = {}
    current: str | None = None
    for number, line in enumerate(text.splitlines(), start=1):
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if line[0] in " \t":
            if current is None:
                raise ValueError(f"{GROUP_VARS}:{number}: an indented line under no key: {stripped}")
            blocks[current].append(line.rstrip())
            continue
        if _DOCUMENT_MARKER.match(line):
            current = None
            continue
        key = _KEY.match(line)
        if key is None:
            raise ValueError(f"{GROUP_VARS}:{number}: not a variable, so its change cannot be judged: {stripped}")
        current = key.group(1)
        if current in blocks:
            raise ValueError(f"{GROUP_VARS}:{number}: {current} is set twice")
        blocks[current] = [line.rstrip()]
    return {key: "\n".join(lines) for key, lines in blocks.items()}


def changed_names(old: str | None, new: str | None) -> set[str]:
    """The variables that differ between two texts of the file (None: the file is absent)."""
    before = top_level(old) if old is not None else {}
    after = top_level(new) if new is not None else {}
    changed = {key for key in before.keys() | after.keys() if before.get(key) != after.get(key)}
    grew = True
    while grew:
        grew = False
        for key in (before.keys() | after.keys()) - changed:
            values = before.get(key, "") + "\n" + after.get(key, "")
            if any(_word(name).search(values) for name in changed):
                changed.add(key)
                grew = True
    return changed


def names_read(names: Iterable[str], text: str) -> list[str]:
    """The names `text` holds as whole words, sorted."""
    return sorted(name for name in names if _word(name).search(text))


def _read(path: str) -> str | None:
    try:
        with open(path, encoding="utf-8") as handle:
            return handle.read()
    except UnicodeDecodeError:
        return None


def play_text(repo_root: str, inputs: Inputs) -> str:
    """The text of every input file of a play that exists and decodes."""
    paths = {path for path in inputs.exact if os.path.isfile(os.path.join(repo_root, path))}
    for prefix in inputs.prefixes:
        top = os.path.join(repo_root, prefix)
        for dirpath, _dirs, files in os.walk(top):
            paths.update(os.path.relpath(os.path.join(dirpath, name), repo_root) for name in files)
    texts = (_read(os.path.join(repo_root, path)) for path in sorted(paths))
    return "\n".join(text for text in texts if text is not None)


def text_at(repo_root: str, commit: str, *, env: Mapping[str, str] | None = None) -> str | None:
    """The file at `commit`, or None when that commit has none. Raises when git cannot say."""
    if not commit or commit.startswith("-"):
        raise ValueError(f"{commit!r} is not a commit")
    listed = subprocess.run(
        ["git", "-C", repo_root, "ls-tree", "--full-tree", "--name-only", commit, "--", GROUP_VARS],
        capture_output=True, text=True, check=False, env=env,
    )
    if listed.returncode != 0:
        raise ValueError(f"git ls-tree {commit} failed: {listed.stderr.strip()}")
    if GROUP_VARS not in listed.stdout.splitlines():
        return None
    shown = subprocess.run(
        ["git", "-C", repo_root, "show", f"{commit}:{GROUP_VARS}"],
        capture_output=True, text=True, check=False, env=env,
    )
    if shown.returncode != 0:
        raise ValueError(f"git show {commit}:{GROUP_VARS} failed: {shown.stderr.strip()}")
    return shown.stdout


def text_now(repo_root: str) -> str | None:
    """The file in the working tree, or None when it is absent."""
    path = os.path.join(repo_root, GROUP_VARS)
    if not os.path.isfile(path):
        return None
    with open(path, encoding="utf-8") as handle:
        return handle.read()
