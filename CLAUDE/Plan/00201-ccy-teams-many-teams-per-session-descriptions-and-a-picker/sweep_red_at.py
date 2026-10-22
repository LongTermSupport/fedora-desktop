"""Task 2.22: checker-copies-cited-constant's findings, as the rule stands in this checkout, over the
plan checkers as they stand at a revision (default HEAD), read from git. The red of a widening
committed with its fixes is its count at the fix commit's parent. Run from the repository root:
`python3 CLAUDE/Plan/00201-*/sweep_red_at.py <rev>`."""
import importlib
import subprocess
import sys

sys.path.insert(0, ".")
r = importlib.import_module("tests.helpers.plan_acceptance.test_cited_constant_copy_rule")

rev = sys.argv[1] if len(sys.argv) > 1 else "HEAD"
needles = r.owned_needles(r.owned_values())
subject = r.subject_sources()
files = subprocess.run(["git", "ls-tree", "-r", "--name-only", rev, "CLAUDE/Plan"], capture_output=True, text=True,
                       check=True).stdout.split()
total = 0
for rel in files:
    parts = rel.split("/")
    if len(parts) < 4 or not r.ACTIVE_PLAN.match(parts[2]) or set(parts) & r.SKIPPED_DIRS:
        continue
    if not rel.endswith((".py", ".bash", ".sh")):
        continue
    source = subprocess.run(["git", "show", f"{rev}:{rel}"], capture_output=True, text=True, check=True).stdout
    lines = set(r.detect(rel, source, subject))
    if r._owned_scope(rel):
        lines.update(r.detect_owned(rel, source, needles))
    for n in sorted(lines):
        total += 1
        print(f"{rel.split('/')[2][:5]} {parts[-1]}:{n}: {source.splitlines()[n - 1].strip()[:110]}")
print("TOTAL", total)
