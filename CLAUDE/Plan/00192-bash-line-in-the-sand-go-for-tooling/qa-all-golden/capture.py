"""Golden capture: run the shipped bash qa-all over a world of stub gates and record it.

Plan 00192, qa-all port. Test tooling for a program that has been removed: it runs
scripts/qa-all.bash and scripts/lib/qa-helper-summary.bash as they were at a git revision, so
the goldens in src-go/qa/all/testdata/golden/ can be re-derived and audited.

    python3 -I capture.py [--rev REV] [--out DIR]

The world is a scripts/ directory holding the legacy qa-all.bash and a stub for every gate it
invokes, plus a stub python3 for the two module gates. The gates and their order come from
scripts/qa-gates.tsv (the Go port's table, which the bash must agree with); the one gate the
port removes on purpose, helper-counts-reader, is cut out of the legacy text before it runs
(SPEC-qa-all.md section 9), and nothing else in it is touched. Every stub logs its name and
arguments and answers from files in a per-scenario directory: its stdout, its stderr, its exit
status, the JSON report a stage writes, the counts file the helper suite writes. A record is the
command line, stdout, stderr, the exit status, the stubs' log and the machine report, with
temporary paths written as placeholders. The same world and stub semantics are implemented
again in Go (golden_test.go); the two must agree, and every difference must be a numbered bug
of SPEC-qa-all.md section 10.

The legacy script writes its report to the fixed path /tmp/qa-results.json (SPEC-qa-all.md bug
B7), which any other QA run on the same machine shares and clobbers, so a record taken beside
one is garbage. The capture therefore changes that one assignment in the legacy text to read
QA_RESULTS_JSON, with the old path as the default, and gives each scenario its own path; nothing
else about the report changes.
"""

import argparse
import json
import re
import shutil
import subprocess
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parents[4]
TESTDATA = REPO / "src-go/qa/all/testdata"
DEFAULT_REV = "e4f6615c"  # F44 with the qa-changed merge: the bash as SPEC-qa-all.md read it
LEGACY = ["scripts/qa-all.bash", "scripts/lib/qa-helper-summary.bash"]
REPORT_ASSIGN = 'JSON_OUT="/tmp/qa-results.json"'
SYSTEM = ["bash", "sh", "cat", "cp", "date", "mktemp", "rm", "dirname", "basename", "grep", "awk", "jq",
          "env", "sort", "head", "tr", "wc", "sleep"]
TIMEOUT = 120

STUB = """#!/usr/bin/bash
name="$(basename "$0" .bash)"
echo "$name $*${QA_JSON_OUT:+ json}" >> "$STUB_LOG"
[[ -f "$STUB_DIR/$name.out" ]] && cat "$STUB_DIR/$name.out"
[[ -f "$STUB_DIR/$name.err" ]] && cat "$STUB_DIR/$name.err" >&2
if [[ -n "${QA_JSON_OUT:-}" && -f "$STUB_DIR/$name.report" ]]; then cp "$STUB_DIR/$name.report" "$QA_JSON_OUT"; fi
while (($#)); do
    case "$1" in
        --counts-file) counts="$2"; shift ;;
        --counts-token) token="$2"; shift ;;
    esac
    shift
done
if [[ -n "${counts:-}" && -f "$STUB_DIR/$name.counts" ]]; then
    filled="$(cat "$STUB_DIR/$name.counts")"
    printf '%s' "${filled//%TOKEN%/${token:-}}" > "$counts"
fi
code=0
[[ -f "$STUB_DIR/$name.code" ]] && code="$(cat "$STUB_DIR/$name.code")"
exit "$code"
"""

PYTHON = """#!/usr/bin/bash
module="$2"
echo "python3 $*" >> "$STUB_LOG"
[[ -f "$STUB_DIR/py-$module.out" ]] && cat "$STUB_DIR/py-$module.out"
[[ -f "$STUB_DIR/py-$module.err" ]] && cat "$STUB_DIR/py-$module.err" >&2
code=0
[[ -f "$STUB_DIR/py-$module.code" ]] && code="$(cat "$STUB_DIR/py-$module.code")"
exit "$code"
"""

SAMPLES = {
    "toolchain": "TOOLCHAIN-OK 6 tools match .qa-versions\n",
    "go": "GO-OK 39 package(s), all tested: fmt, vet, test -race\n",
    "bash-limits": "BASH-LIMITS-OK 446 bash files: 109 allowlisted\n",
    "nokill-containerwatch": "✓ no-kill gate: 3 container-watch file(s) clean — reporting-only confirmed\n",
    "planlib-tests": "test-planlib: PASSED (library version 1.2.0)\n",
    "ssh-suspend-guard": "VERDICT: PASS\n",
    "extension-compat": "All 3 extension(s) cover the GNOME Shell that Fedora 44 ships\n",
    "panel-contract": "7 constant(s) agree between the panel and the producer\n",
    "plan-script-logging": "12 plan script(s) examined, no offences\n",
    "version-pins": "vars/version-pins.yml: VERSION-PINS-OK 20 rows\n",
    "vmtest-manifest": (
        "vars/vm-test-scenarios.yml: scenarios=8 runnable=8 bridge=6 host_only=2 bases=2\n"
        "8 scenario(s) agree with their guest checker\n"
        "3 scenario fixture(s) run before a declared reboot\n"
    ),
}
CLEAN_COUNTS = "token=%TOKEN%\ntests=10\nskipped=1\nmodules=2\ntracked=2\n"
BAD_REPORT = ('{"type":"x","status":"fail","summary":{"total":3,"passed":2,"failed":1},'
              '"failures":[{"file":"a.sh","errors":["boom"]}]}')


def clean_report(total):
    """A passing stage's report."""
    return json.dumps({"type": "x", "status": "pass", "summary": {"total": total, "passed": total, "failed": 0},
                       "failures": []})


def sh(args, cwd, env=None):
    """Run a command, failing loudly; its stdout."""
    res = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True, check=False, timeout=TIMEOUT)
    if res.returncode != 0:
        raise SystemExit(f"{args} failed ({res.returncode}): {res.stderr}")
    return res.stdout.strip()


def load_table():
    """The gate table: (kind, name, argv, readers) per row, in order."""
    rows = []
    for line in (REPO / "scripts/qa-gates.tsv").read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.startswith("#"):
            continue
        fields = line.split("\t")
        rows.append({"kind": fields[0], "name": fields[1], "argv": fields[2].split(), "readers": fields[4:]})
    return rows


def stub_key(row):
    """What the stub names its files by: the script's base name, or py-<module>."""
    if row["argv"][0].startswith("scripts/"):
        return Path(row["argv"][0]).name[: -len(".bash")]
    return "py-" + row["argv"][2]


def default_reply(row, index):
    """The gate answering as a passing one."""
    name, kind = row["name"], row["kind"]
    if kind == "stage":
        return {"out": f"✓ {name}: 4 files OK\n", "report": clean_report(4)}
    if kind == "helpers":
        return {"counts": CLEAN_COUNTS}
    if kind == "drift":
        return {"out": "✓ deployed-drift: 4 scripts in sync\n"}
    if name in SAMPLES:
        return {"out": SAMPLES[name]}
    return {"out": f"passed: {index + 1} failed: 0\n"}


def failing(row):
    """The gate failing: status 1 with a diagnostic, and for a stage a report counting a failure."""
    reply = {"code": 1, "err": f"{row['name']}: boom\n"}
    if row["kind"] == "stage":
        reply.update({"out": f"✗ {row['name']}: 1/3 files failed\n", "report": BAD_REPORT})
    if row["kind"] == "helpers":
        reply["out"] = "some test output\n"
    return reply


def build_scenarios(table):
    """The corpus."""
    names = [r["name"] for r in table]
    by_name = {r["name"]: r for r in table}
    out = [scen("all-pass"), scen("help", args=["--help"]), scen("unknown-flag", args=["--typo"])]
    out += [scen("fail-" + r["name"], replies={r["name"]: failing(r)}) for r in table]
    for r in table:
        if r["kind"] == "stage":
            out.append(scen("exit2-" + r["name"], replies={r["name"]: {"code": 2, "err": "no tool\n"}}))
    out += [
        scen("exit2-toolchain", replies={"toolchain": {"code": 2, "err": "toolchain: missing\n"}}),
        scen("exit2-go", replies={"go": {"code": 2, "err": "go: no golangci-lint\n"}}),
        scen("two-gates-fail", replies={"go": failing(by_name["go"]), names[20]: failing(by_name[names[20]])}),
        scen("gate-and-stage-fail", replies={"bash": failing(by_name["bash"]), "release": failing(by_name["release"])}),
        scen("stage-then-abort", replies={"go": failing(by_name["go"]), "patterns": {"code": 2, "err": "no semgrep\n"}}),
        scen("stale-report-then-abort", replies={"js": {"code": 2}}, stale=True),
        scen("stale-report-then-pass", stale=True),
        scen("no-count", replies={"release": {"out": "Summary: 3 passed, 0 failed\n"}}),
        scen("no-count-empty", replies={"release": {"out": ""}}),
        scen("detail-miss-go", replies={"go": {"out": "all fine\n"}}),
        scen("detail-miss-toolchain", replies={"toolchain": {"out": ""}}),
        scen("detail-miss-manifest-second", replies={"vmtest-manifest": {"out": SAMPLES["vmtest-manifest"].split("\n")[0] + "\n"}}),
        scen("detail-last-line-wins", replies={"go": {"out": "GO-OK 1 package(s)\nGO-OK 39 package(s) again\n"}}),
        scen("count-last-line-wins", replies={"release": {"out": "passed: 3 failed: 0\nlater passed: 4 of passed: 9 failed: 0\n"}}),
        scen("drift-skipped", replies={"deployed-drift": {"out": "⚠ deployed-drift: skipped (CCY container); 2 templates\n"}}),
        scen("drift-empty", replies={"deployed-drift": {"out": ""}}),
        scen("drift-fails", replies={"deployed-drift": {"code": 1, "out": "✗ deployed-drift: ccy differs\n"}}),
        scen("drift-exit2", replies={"deployed-drift": {"code": 2, "err": "ERROR: source directory not found\n"}}),
        scen("helpers-stdout-noise", replies={"helper-tests": {"out": "forged ✓ line\n"}}),
        scen("helpers-fails-with-output", replies={"helper-tests": {"code": 1, "out": "out line\n", "err": "err line\n"}}),
        scen("helpers-counts-empty", replies={"helper-tests": {"counts": ""}}, empty_counts=True),
        scen("helpers-counts-no-skipped", replies={"helper-tests": {"counts": "token=%TOKEN%\ntests=3\nmodules=1\ntracked=1\n"}}),
        scen("helpers-counts-wrong-token", replies={"helper-tests": {"counts": "token=other\ntests=3\nskipped=0\nmodules=1\ntracked=1\n"}}),
        scen("helpers-counts-duplicate", replies={"helper-tests": {"counts": "token=%TOKEN%\ntests=3\ntests=4\nskipped=0\nmodules=1\ntracked=1\n"}}),
        scen("helpers-counts-single", replies={"helper-tests": {"counts": "token=%TOKEN%\ntests=1\nskipped=0\nmodules=1\ntracked=1\n"}}),
        scen("helpers-counts-non-numeric", replies={"helper-tests": {"counts": "token=%TOKEN%\ntests=x\nskipped=0\nmodules=1\ntracked=1\n"}}),
        scen("stage-report-empty-shifts", replies={"python": {"report": ""}}, empty_report="python"),
        scen("stage-report-malformed", replies={"python": {"report": "{"}}),
        scen("stage-report-no-summary", replies={"python": {"report": '{"failures":[]}'}}),
        scen("stage-exit0-with-failures", replies={"docs": {"report": BAD_REPORT}}),
        scen("stage-fail-without-failures", replies={"js": {"code": 1, "out": "✗ js: crashed\n", "report": clean_report(0)}}),
        scen("stage-stderr-passthrough", replies={"ansible": {"out": "✓ ansible: ok\n", "err": "a warning\n"}}),
        scen("stage-failures-merge", replies={"bash": failing(by_name["bash"]), "python": failing(by_name["python"])}),
        scen("hard-gate-stderr-only", replies={"release": {"code": 1, "err": "only stderr\n\n\n"}}),
        scen("hard-gate-no-output-fail", replies={"release": {"code": 1}}),
        scen("many-fail", replies={r["name"]: failing(r) for r in table[::7]}),
    ]
    return out


def scen(name, args=(), replies=None, stale=False, empty_counts=False, empty_report=""):
    """One scenario: a name, the command line, and per-gate overrides of the passing replies."""
    out = {"name": name, "args": list(args), "replies": replies or {}}
    if stale:
        out["stale"] = True
    if empty_counts:
        out["emptyCounts"] = True
    if empty_report:
        out["emptyReport"] = empty_report
    return out


def build_world(root, table, legacy):
    """The world: scripts/ with the legacy bash and a stub per gate script, and a bin/ of tools."""
    scripts = root / "scripts"
    (scripts / "lib").mkdir(parents=True)
    for rel, text in legacy.items():
        path = root / rel
        path.write_text(text)
    for row in table:
        if row["argv"][0].startswith("scripts/"):
            path = root / row["argv"][0]
            path.write_text(STUB)
            path.chmod(0o755)
    bindir = root.parent / "bin"
    bindir.mkdir()
    for name in SYSTEM:
        found = shutil.which(name)
        if found:
            (bindir / name).symlink_to(found)
    py = bindir / "python3"
    py.write_text(PYTHON)
    py.chmod(0o755)
    return bindir


def cut_helper_counts_reader(text):
    """Remove the one gate the port drops on purpose (helper-counts-reader) from the legacy text."""
    start = text.index("# The reader behind THIS script's own helper-tests line (Plan 00125).")
    marker = 'qa_pass_line helper-counts-reader "$helper_summary_tests"\n'
    end = text.index(marker) + len(marker)
    return text[:start] + text[end:].lstrip("\n")


def stub_world_files(stub, table, scenario):
    """Write the scenario's replies as the stubs' answer files."""
    for index, row in enumerate(table):
        reply = {**default_reply(row, index), **scenario["replies"].get(row["name"], {})}
        key = stub_key(row)
        for field, ext in [("out", "out"), ("err", "err"), ("report", "report"), ("counts", "counts")]:
            if field in reply:
                text = reply[field] if field == "counts" else reply[field]
                (stub / f"{key}.{ext}").write_text(text)
        if reply.get("code"):
            (stub / f"{key}.code").write_text(str(reply["code"]))


def normalise(text, root, report_path):
    """Temporary paths and the token out, placeholders in."""
    text = text.replace(str(report_path), "<REPORT>").replace(str(root), "<ROOT>")
    text = re.sub(r"qa-all-[0-9]+-[0-9]+", "<TOKEN>", text)
    return re.sub(r"/tmp/tmp\.[A-Za-z0-9]+", "<TMP>", text)


def run_one(scen_, table, legacy):
    """Run the bash for one scenario in a fresh world; the record."""
    with tempfile.TemporaryDirectory() as tmp:
        tmp = Path(tmp)
        root = tmp / "world"
        root.mkdir()
        bindir = build_world(root, table, legacy)
        stub = tmp / "stub"
        stub.mkdir()
        stub_world_files(stub, table, scen_)
        if scen_.get("emptyCounts"):
            (stub / "qa-helper-tests.counts").unlink(missing_ok=True)
        if scen_.get("emptyReport"):
            key = stub_key(next(r for r in table if r["name"] == scen_["emptyReport"]))
            (stub / f"{key}.report").unlink(missing_ok=True)
        log = tmp / "log"
        report = tmp / "results.json"
        if scen_.get("stale"):
            report.write_text('{"status":"pass"}')
        env = {"PATH": str(bindir), "HOME": str(tmp), "STUB_LOG": str(log), "STUB_DIR": str(stub),
               "QA_RESULTS_JSON": str(report)}
        res = subprocess.run(["bash", str(root / "scripts/qa-all.bash"), *scen_["args"]], cwd=root, env=env,
                             capture_output=True, text=True, check=False, timeout=TIMEOUT)
        merged = report.read_text() if report.exists() else None
        lines = log.read_text().splitlines() if log.exists() else []
        return {
            "args": scen_["args"],
            "stdout": normalise(res.stdout, root, report),
            "stderr": normalise(res.stderr, root, report),
            "status": res.returncode,
            "log": [normalise(line, root, report) for line in lines],
            "report": None if merged is None else normalise(merged, root, report),
        }


def legacy_files(rev):
    """The bash as it was at the revision, with the one removed gate cut out."""
    files = {rel: sh(["git", "show", f"{rev}:{rel}"], REPO) + "\n" for rel in LEGACY}
    text = cut_helper_counts_reader(files["scripts/qa-all.bash"])
    if REPORT_ASSIGN not in text:
        raise SystemExit("the legacy script no longer assigns its report path as expected")
    files["scripts/qa-all.bash"] = text.replace(REPORT_ASSIGN, 'JSON_OUT="${QA_RESULTS_JSON:-/tmp/qa-results.json}"')
    return files


def main():
    """Record every scenario from the revision into the testdata directory."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rev", default=DEFAULT_REV)
    parser.add_argument("--out", default=str(TESTDATA))
    opts = parser.parse_args()
    out = Path(opts.out)
    rev = sh(["git", "rev-parse", opts.rev], REPO)
    legacy = legacy_files(rev)
    table = load_table()
    scenarios = build_scenarios(table)
    (out / "golden").mkdir(parents=True, exist_ok=True)
    defaults = {r["name"]: default_reply(r, i) for i, r in enumerate(table)}
    (out / "scenarios.json").write_text(
        json.dumps({"revision": rev, "defaults": defaults, "scenarios": scenarios}, indent=1) + "\n")
    for scen_ in scenarios:
        record = run_one(scen_, table, legacy)
        (out / "golden" / f"{scen_['name']}.json").write_text(json.dumps(record, indent=1) + "\n")
    print(f"recorded {len(scenarios)} scenarios from {rev}")


if __name__ == "__main__":
    main()
