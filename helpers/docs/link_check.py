"""Documentation-integrity checks: link targets, heading anchors, catalogues.

Stdlib-only (helpers/CLAUDE.md). Pure functions on content strings so they are
testable without touching the filesystem; the tree walk lives at the bottom.

Why this exists
---------------
Plan 00070 audited the docs by hand across five read-only passes. A mechanical
sweep afterwards found four defects those passes had missed, and showed that a
hand-written finding ("two broken links") had undercounted the real number.
The three checks here are exactly the ones that would each have caught a real
finding in that audit:

  - anchor/link resolution  -> findings 13, 17, 19, 21, 22
  - playbook catalogue      -> finding 2
  - topic-file index        -> finding 3

SCOPE: core docs only. `CLAUDE/Plan/**` is deliberately excluded — a core gate
that sweeps plan content becomes a core->plan dependency, so archiving a plan
could change core CI's verdict. Plan markdown is linted by the plan QA tooling,
which is where that belongs.
"""

import json
import os
import re
import subprocess
import sys

# [text](target), but not images (![...]).
_LINK = re.compile(r"(?<!!)\[(?P<text>[^\]]*)\]\((?P<target>[^)\s]+)\)")
_HEADING = re.compile(r"^(?P<hashes>#{1,6})\s+(?P<text>.*?)\s*#*$")
_FENCE = re.compile(r"^\s*(?:```|~~~)")
_SCHEME = re.compile(r"^[a-zA-Z][a-zA-Z0-9+.-]*:")
_IMPORT = re.compile(r"^\s*-\s*import_playbook:\s*\S*?(?P<name>play-[\w.-]+\.ya?ml)\s*$")

# Everything github-slugger deletes outright. Notably includes "/" and ";" —
# they vanish rather than becoming hyphens — and every dash-like character that
# is not the plain ASCII hyphen.
_DROP = re.compile(r"[^\w\s-]", re.UNICODE)


def slug(text):
    """Approximate GitHub's heading-anchor slug.

    Each space becomes its own hyphen; runs are NOT collapsed. A heading with
    " — " therefore yields a double hyphen, because the em-dash is deleted and
    both surrounding spaces survive to become hyphens.
    """
    t = re.sub(r"`([^`]*)`", r"\1", text)
    t = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", t)
    t = t.replace("**", "").replace("__", "")
    t = t.strip().lower()
    t = _DROP.sub("", t)
    return re.sub(r"\s", "-", t)


def _uncoded_lines(content):
    """Yield (lineno, line) for lines outside fenced code blocks."""
    in_fence = False
    for lineno, line in enumerate(content.splitlines(), 1):
        if _FENCE.match(line):
            in_fence = not in_fence
            continue
        if not in_fence:
            yield lineno, line


def headings(content):
    """Return the set of anchor slugs the document defines."""
    found = set()
    for _, line in _uncoded_lines(content):
        match = _HEADING.match(line)
        if not match:
            continue
        base = slug(match.group("text"))
        if not base:
            continue
        if base not in found:
            found.add(base)
            continue
        n = 1
        while f"{base}-{n}" in found:
            n += 1
        found.add(f"{base}-{n}")
    return found


def links(content):
    """Return [(lineno, target)] for every non-external, non-image link."""
    out = []
    for lineno, line in _uncoded_lines(content):
        for match in _LINK.finditer(line):
            target = match.group("target")
            if _SCHEME.match(target) or target.startswith("//"):
                continue
            out.append((lineno, target))
    return out


def imported_playbooks(content):
    """Return the play filenames `playbook-main.yml` imports, in order."""
    out = []
    for line in content.splitlines():
        match = _IMPORT.match(line)
        if match:
            out.append(match.group("name"))
    return out


def missing_mentions(names, haystack):
    """Return the names that do not appear anywhere in haystack."""
    return [name for name in names if name not in haystack]


#: A gate invoked as a script. Matched by its FILENAME after any variable- or
#: path-shaped prefix — `$SCRIPT_DIR/x.bash`, `${SCRIPT_DIR}/x.bash` and
#: `$REPO_ROOT/scripts/x.bash` are all the same gate, and keying on one spelling would
#: exempt the other two from the inventory without saying so.
_QA_SCRIPT_GATE = re.compile(
    r"[$][{]?[A-Za-z_][A-Za-z0-9_]*[}]?(?:/[A-Za-z0-9._-]+)*/(?P<name>[A-Za-z0-9._-]+\.bash)"
)
#: A gate invoked as a module, e.g. `python3 -m helpers.gnome.check_panel_contract`.
_QA_MODULE_GATE = re.compile(r"-m\s+(?P<name>helpers[A-Za-z0-9_.]+)")


#: A line that loads a library into the current shell instead of running a gate.
#: `source x` and `. x` are the two spellings.
_SOURCE_LINE = re.compile(r"^\s*(?:source|\.)\s")


def qa_gates(content):
    """Every gate `qa-all.bash` invokes, named as `CLAUDE/QA.md` names it in a row.

    Script gates by filename, module gates by their full dotted path — which is what the
    table's first cell already writes. Reducing a module gate to its last component
    matched the document by substring in one direction and disagreed with it in the
    other, so the reverse check could never have been clean.

    A `source`d library is not a gate and is skipped. It runs nothing and emits no verdict
    line, so the table has no true row to give it — and the alternative, adding one to
    satisfy this check, is how an inventory stops describing what it inventories.
    """
    invocations = "\n".join(
        line for line in content.splitlines() if not _SOURCE_LINE.match(line)
    )
    names = {match.group("name") for match in _QA_SCRIPT_GATE.finditer(invocations)}
    names |= {match.group("name") for match in _QA_MODULE_GATE.finditer(invocations)}
    return sorted(names)


#: A gate's own row in one of the two tables: `| `name` | what it checks |`. Scoped to
#: the leading cell so that prose naming a gate is not read as a row claiming it.
_QA_DOC_ROW = re.compile(r"^\|\s*`(?P<name>[A-Za-z0-9._-]+)`\s*\|")


def documented_gates(qa_doc):
    """The gates `CLAUDE/QA.md` claims in its tables, by the name in the first cell."""
    return sorted({
        match.group("name")
        for match in (_QA_DOC_ROW.match(line) for line in qa_doc.splitlines())
        if match
    })


def check_qa_gate_inventory_in(*, qa_all, qa_doc):
    """The pure half: every gate in `qa_all` has a row in `qa_doc`.

    DERIVED, not re-enumerated. This table had drifted by roughly five entries while
    stating two counts that were both wrong, and each previous repair swapped one stale
    list for a fresher one — which is why it went stale again. `CLAUDE/AgentNotes.md`
    names the rule: replacing a stale enumeration with a fresher enumeration is not the
    fix; deriving the set is.
    """
    names = qa_gates(qa_all)
    if not names:
        return [{
            "file": "scripts/qa-all.bash", "line": 0, "target": "-",
            "problem": "parsed 0 gate invocations — discovery is broken, not the doc",
        }]
    # Compared against the ROWS, not against the document's text. A substring search
    # over the whole file is satisfied by prose that merely mentions a gate, which is
    # not the same as the table claiming it — and it is the table this check is for.
    documented = set(documented_gates(qa_doc))
    findings = [
        {
            "file": "CLAUDE/QA.md", "line": 0, "target": name,
            "problem": "gate is run by qa-all.bash but has no row in this document",
        }
        for name in names if name not in documented
    ]
    # BOTH DIRECTIONS. A one-way check leaves a row for a gate that no longer runs
    # standing for ever — and a documented gate nobody executes is the exact failure
    # this document narrates below its own table, where two gates were listed here and
    # not run by `qa-all.bash` at all.
    findings += [
        {
            "file": "CLAUDE/QA.md", "line": 0, "target": name,
            "problem": "this document lists the gate but qa-all.bash does not run it",
        }
        for name in sorted(documented - set(names))
    ]
    return findings


def check_qa_gate_inventory(repo_root):
    """Every gate `qa-all.bash` runs must have a row in `CLAUDE/QA.md`."""
    return check_qa_gate_inventory_in(
        qa_all=_read(os.path.join(repo_root, "scripts/qa-all.bash")),
        qa_doc=_read(os.path.join(repo_root, "CLAUDE/QA.md")),
    )


_EXCLUDE_ANYWHERE = ("node_modules/",)
_EXCLUDE_PREFIX = (
    "CLAUDE/Plan/",
    "untracked/",
    ".claude/hooks-daemon/",
    ".claude/ccy/",
    ".claude/skills/",
    ".claude/agents/",
    ".ansible/",
    "roles/vendor/",
)


def in_scope(rel_path):
    """True when this gate owns the given repo-relative markdown path.

    Core docs only. `CLAUDE/Plan/**` is excluded on principle: a core gate that
    sweeps plan content becomes a core->plan dependency, so archiving a plan
    could change core CI's verdict without a core file changing. Plan markdown
    has its own linting.
    """
    if not rel_path.endswith(".md"):
        return False
    if any(part in rel_path for part in _EXCLUDE_ANYWHERE):
        return False
    if rel_path.startswith(_EXCLUDE_PREFIX):
        return False

    if rel_path in ("README.md", "CLAUDE.md"):
        return True
    if rel_path.startswith(("docs/", ".claude/rules/")):
        return True
    # Top-level topic files only — CLAUDE/Plan/ was already excluded above.
    if rel_path.startswith("CLAUDE/"):
        return True
    # Any directory's own CLAUDE.md (helpers/, playbooks/, extensions/, ...).
    return rel_path.endswith("/CLAUDE.md")


def _read(path):
    with open(path, encoding="utf-8") as handle:
        return handle.read()


# Trees holding a DIFFERENT repository, vendored into this checkout. A link landing inside
# one is out of scope: the file is not ours, we could not fix a broken link there, and an
# edit would be re-rendered by that repo's own installer on the next upgrade.
#
# DECLARED RATHER THAN DETECTED, and that is not laziness. In CI the vendored tree is simply
# absent, so nothing on disk distinguishes it from a typo — probing for a `.git` would give
# one answer here and another there, which is the divergence this gate exists to remove.
#
# NOTHING CHECKS THAT A DECLARED ROOT IS REALLY A VENDORED REPOSITORY, and this comment used
# to claim otherwise — it cited a `check_vendored_declaration` that was written, measured
# (it reported eleven, all of them gitignored acceptance-run fixtures) and deleted, leaving
# the citation behind. A claim printed where a measurement belongs, in the paragraph
# answering the obvious objection to a declared exemption list. So, plainly: adding a root
# here exempts every link under it, and only review stops that. What IS checked is the other
# direction — a repo nobody declared gets its links reported, with `nested_repository_for`
# naming it where the machine can see it.
#
# Every root must also appear in `_EXCLUDE_PREFIX` above, or this gate would sweep another
# repository's markdown as if it were ours. Asserted by
# `test_every_vendored_root_is_also_excluded_from_the_scan`.
#
# Parents, not leaves. `roles/vendor/` covers a role vendored tomorrow without a code change.
_VENDORED_ROOTS = (
    ".claude/hooks-daemon/",
    "roles/vendor/",
)



def repo_relative(repo_root, resolved):
    """`resolved` as a repo-relative path, or None when it escapes the root."""
    rel = os.path.relpath(resolved, repo_root)
    if rel == ".." or rel.startswith(".." + os.sep):
        return None
    return rel


def vendored_root_for(rel_target):
    """The declared vendored tree containing `rel_target`, or None.

    The root ITSELF counts, which `startswith` alone missed: the roots carry a trailing
    slash, so a link to `../hooks-daemon` matched nothing, and it missed the ignore branch
    too because `.gitignore`'s rule is directory-only and git cannot tell that a path which
    does not exist is a directory. It came out as `target does not exist` — a hard CI
    failure that passes wherever the tree happens to be installed, which is the exact
    divergence this classification replaced. "Parents, not leaves" did not cover the parent.

    The trailing slash is kept rather than stripped from the comparison, because dropping it
    would exempt every sibling whose name merely begins the same way (`roles/vendor-extra/`).
    So: equal to the root, or under it.
    """
    if rel_target is None:
        return None
    for root in _VENDORED_ROOTS:
        if rel_target == root.rstrip("/") or rel_target.startswith(root):
            return root
    return None


def tracked_paths(repo_root):
    """Everything this repository tracks: `(files, directories)`, repo-relative.

    THE QUESTION IS TRACKEDNESS, not ignored-ness, and the difference is a real divergence
    rather than a wording preference. The first version asked `git check-ignore`, which is
    machine-independent but answers something narrower: a file sitting on this disk that was
    never `git add`ed and matches no ignore rule is *not* ignored, so it passed here and
    failed in CI where it simply does not exist. Cause A's exact shape, inside the
    classification built to remove it. The finding has always said "not tracked by this
    repository"; this is the check finally asking that.

    The index is in every clean checkout, so CI gets the same answer — the property
    `check-ignore` was chosen for is kept.

    Directories are derived because `ls-files` lists files, and a link to `docs/` is a link
    to something this repo plainly owns.

    A tree git cannot answer for raises rather than returning an empty set: "nothing is
    tracked" would be a confident verdict derived from a check that did not run, and it
    would condemn every link in the repository.
    """
    # check=False: the returncode is inspected on the next line, and a non-zero exit here is
    # an error rather than a result — unlike a probe, there is no meaningful failure mode.
    proc = subprocess.run(
        ["git", "-C", repo_root, "ls-files", "-z"],
        capture_output=True, text=True, check=False,
    )
    if proc.returncode != 0:
        raise RuntimeError(
            f"git ls-files failed in {repo_root} (exit {proc.returncode}): "
            f"{proc.stderr.strip()} — cannot tell which link targets this repository "
            f"tracks, so no link verdict here would mean anything"
        )
    files = {path for path in proc.stdout.split("\0") if path}
    # `.` is the repository itself, which `ls-files` naturally never lists and which is
    # obviously ours. Without it, a link resolving to the repo root read as "not tracked by
    # this repository" — a false failure about the repository.
    directories = {"."}
    for path in files:
        parent = os.path.dirname(path)
        while parent:
            directories.add(parent)
            parent = os.path.dirname(parent)
    return files, directories


def nested_repository_for(repo_root, rel_target):
    """The nested repository containing `rel_target`, or None. A HINT, not a verdict.

    Walks UP from the target looking for a `.git`, which may be a FILE — that is what a
    linked worktree leaves behind. Bounded by `repo_root`, whose own `.git` is not a
    nested anything.

    Deliberately NOT a tree walk looking for undeclared repositories. That version was
    written, and it reported eleven — every one a gitignored acceptance-run fixture from a
    completed plan, none of them vendored. Every vendored repo is gitignored, so a walk
    either skips them all or drags in every stray clone; the population it can see is not
    the population that matters.

    This answers only for a target that is ALREADY a finding, so it costs nothing on a
    clean run and turns "not tracked by this repository" into an instruction. On a machine
    without the repo present — CI — it answers None and the finding keeps its plain
    wording, which is the right degradation: the gate still fails, and the local run is
    where the fix gets made anyway.
    """
    if rel_target is None:
        return None
    current = os.path.dirname(rel_target)
    while current:
        if os.path.exists(os.path.join(repo_root, current, ".git")):
            return current
        parent = os.path.dirname(current)
        if parent == current:
            break
        current = parent
    return None


def check_links(repo_root, rel_paths):
    """Check every link in rel_paths.

    Returns `(findings, vendored)`. `vendored` is
    `{"ok": n, "unverifiable": n, "broken": [finding-shaped dicts]}` — the three outcomes a
    link into another repository can have, counted rather than discarded, because an
    exemption nobody counts reads exactly like a check that ran and found nothing.

    A vendored target is still LOOKED AT where looking means something:

      - the repo is present and the target is there   -> `ok`, silent
      - the repo is absent                            -> `unverifiable`; nothing here could
                                                         have said, which is CI's case
      - the repo is present and the target is NOT     -> `broken`; the link is demonstrably
                                                         wrong, usually because the vendored
                                                         repo moved the file

    None of the three is a finding, because none is ours to fix, and that is the invariant
    the whole design protects: the gate's EXIT CODE must not depend on what is installed.
    What it SAYS may, and should — a machine that can see the repo can say more about it,
    and saying more never flips a verdict.

    Anchors into a vendored repo are not followed even when the repo is present. Their
    headings are theirs to rename, and a gate that went red on another repo's churn would be
    a dependency on it for a defect we could not fix.

    A target this repo ignores but nobody vendored is a FINDING, not an exemption. It is
    a link to something no clean checkout has and no other repository owns, and folding
    it in with the vendored case would trade a false failure for a silent skip.
    """
    findings = []
    vendored = {"ok": 0, "unverifiable": 0, "broken": []}
    heading_cache = {}

    documents = [(rel, _read(os.path.join(repo_root, rel))) for rel in rel_paths]
    tracked_files, tracked_dirs = tracked_paths(repo_root)

    for rel, content in documents:
        path = os.path.join(repo_root, rel)

        for lineno, target in links(content):
            filepart, _, frag = target.partition("#")

            if filepart:
                resolved = os.path.normpath(
                    os.path.join(os.path.dirname(path), filepart))
                rel_target = repo_relative(repo_root, resolved)
                vendored_root = vendored_root_for(rel_target)
                if vendored_root is not None:
                    if not os.path.exists(os.path.join(repo_root, vendored_root)):
                        vendored["unverifiable"] += 1
                    elif os.path.exists(resolved):
                        vendored["ok"] += 1
                    else:
                        vendored["broken"].append({
                            "file": rel, "line": lineno, "target": target,
                            "problem": f"broken link into the vendored repository at "
                                       f"{vendored_root.rstrip('/')}, which IS present here "
                                       f"— it has probably moved the file",
                        })
                    continue
                # EXISTENCE FIRST, trackedness second, and the order is deliberate. A typo'd
                # link is both absent and untracked; "target does not exist" is the message
                # that helps. Both are findings either way, so the VERDICT is the same on
                # both machines — a present-but-untracked target fails here and fails in CI
                # for the other reason. Same exit code, different detail, which is the
                # distinction this whole gate turns on.
                if not os.path.exists(resolved):
                    findings.append({
                        "file": rel, "line": lineno, "target": target,
                        "problem": "target does not exist",
                    })
                    continue
                if rel_target is None or not (rel_target in tracked_files
                                              or rel_target in tracked_dirs):
                    problem = "target is not tracked by this repository"
                    nested = nested_repository_for(repo_root, rel_target)
                    if nested is not None:
                        problem += (
                            f" — a repository is nested at {nested}; if it is vendored,"
                            " declare it in _VENDORED_ROOTS"
                            " (helpers/docs/link_check.py)")
                    findings.append({
                        "file": rel, "line": lineno, "target": target,
                        "problem": problem,
                    })
                    continue
            else:
                resolved = path

            if not frag or not resolved.endswith(".md"):
                continue

            if resolved not in heading_cache:
                heading_cache[resolved] = headings(_read(resolved))

            if slug(frag) not in heading_cache[resolved]:
                findings.append({
                    "file": rel, "line": lineno, "target": target,
                    "problem": "no heading matches the anchor",
                })

    return findings, vendored


def check_playbook_catalogue(repo_root):
    """Every play imported by playbook-main.yml must appear in both docs."""
    main = _read(os.path.join(repo_root, "playbooks/playbook-main.yml"))
    names = imported_playbooks(main)
    findings = []

    if not names:
        findings.append({
            "file": "playbooks/playbook-main.yml", "line": 0,
            "target": "-", "problem":
                "parsed 0 import_playbook lines — discovery is broken, "
                "not the tree",
        })
        return findings

    for doc in ("docs/playbooks.md", "docs/architecture.md"):
        content = _read(os.path.join(repo_root, doc))
        for name in missing_mentions(names, content):
            findings.append({
                "file": doc, "line": 0, "target": name,
                "problem": "core playbook is absent from this document",
            })

    return findings


def check_topic_index(repo_root):
    """Every CLAUDE/*.md topic file must be referenced from CLAUDE.md."""
    claude_md = _read(os.path.join(repo_root, "CLAUDE.md"))
    topic_dir = os.path.join(repo_root, "CLAUDE")
    names = sorted(
        n for n in os.listdir(topic_dir)
        if n.endswith(".md") and os.path.isfile(os.path.join(topic_dir, n))
    )
    findings = []

    if not names:
        findings.append({
            "file": "CLAUDE/", "line": 0, "target": "-",
            "problem": "found 0 topic files — discovery is broken",
        })
        return findings

    for name in missing_mentions(names, claude_md):
        findings.append({
            "file": "CLAUDE.md", "line": 0, "target": f"CLAUDE/{name}",
            "problem": "topic file has no row in the index",
        })

    return findings


_PRUNE_DIRS = {".git", "node_modules", "untracked", "__pycache__",
               ".venv", "venv", ".ansible"}


def collect_scope(repo_root):
    """Walk repo_root and return the sorted in-scope markdown, repo-relative."""
    found = []
    for dirpath, dirnames, filenames in os.walk(repo_root):
        dirnames[:] = [d for d in dirnames if d not in _PRUNE_DIRS]
        for filename in filenames:
            if not filename.endswith(".md"):
                continue
            rel = os.path.relpath(os.path.join(dirpath, filename), repo_root)
            if in_scope(rel):
                found.append(rel)
    return sorted(found)


def main(argv):
    repo_root = os.path.abspath(argv[1] if len(argv) > 1 else ".")
    scoped = collect_scope(repo_root)

    # A gate that scanned NOTHING must not report a pass (CLAUDE/QA.md).
    if not scoped:
        print(json.dumps({
            "type": "docs", "status": "error", "scanned": 0, "findings": [],
            "message": "found 0 in-scope markdown files — discovery is broken, "
                       "not the tree",
        }))
        return 2

    findings, vendored = check_links(repo_root, scoped)
    findings += check_playbook_catalogue(repo_root)
    findings += check_topic_index(repo_root)
    findings += check_qa_gate_inventory(repo_root)

    print(json.dumps({
        "type": "docs",
        "status": "fail" if findings else "pass",
        "scanned": len(scoped),
        "vendored": vendored,
        "summary": {
            "files": len(scoped),
            "findings": len(findings),
            "vendored_ok": vendored["ok"],
            "vendored_unverifiable": vendored["unverifiable"],
            "vendored_broken": len(vendored["broken"]),
        },
        "findings": findings,
    }, indent=2))
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
