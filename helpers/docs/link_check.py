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


def check_links(repo_root, rel_paths):
    """Check every link in rel_paths. Returns a list of finding dicts."""
    findings = []
    heading_cache = {}

    for rel in rel_paths:
        path = os.path.join(repo_root, rel)
        content = _read(path)

        for lineno, target in links(content):
            filepart, _, frag = target.partition("#")

            if filepart:
                resolved = os.path.normpath(
                    os.path.join(os.path.dirname(path), filepart))
                if not os.path.exists(resolved):
                    findings.append({
                        "file": rel, "line": lineno, "target": target,
                        "problem": "target does not exist",
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

    return findings


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

    findings = check_links(repo_root, scoped)
    findings += check_playbook_catalogue(repo_root)
    findings += check_topic_index(repo_root)
    findings += check_qa_gate_inventory(repo_root)

    print(json.dumps({
        "type": "docs",
        "status": "fail" if findings else "pass",
        "scanned": len(scoped),
        "summary": {"files": len(scoped), "findings": len(findings)},
        "findings": findings,
    }, indent=2))
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
