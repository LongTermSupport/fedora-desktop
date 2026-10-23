"""Wrong-product proofs for Task 2.22 step 4, in a scratch worktree (argv[1]) of the branch tip.

Each row breaks the product in the scratch copy (a constant renamed, or its value changed so the
product disagrees with what a session carries), runs the checker's own reader or verdict there,
records whether the checker refused, and restores the file. Prints a markdown table."""
import json
import os
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(sys.argv[1]).resolve()
M7 = "CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/m7_check.py"


def run(argv, cwd=ROOT, env=None):
    # Two mutations of one file can share a size and an mtime second, so a cached .pyc of the
    # first would be read for the second: drop every cache before each run.
    for cache in ROOT.glob("helpers/**/__pycache__"):
        for pyc in cache.iterdir():
            pyc.unlink()
    done = subprocess.run(argv, cwd=cwd, capture_output=True, text=True, check=False, env=env, timeout=900)
    return done.returncode, (done.stdout + done.stderr).strip().splitlines()


def mutate(rel, old, new):
    path = ROOT / rel
    text = path.read_text(encoding="utf-8")
    assert text.count(old) == 1, (rel, old)
    path.write_text(text.replace(old, new), encoding="utf-8")
    return lambda: path.write_text(text, encoding="utf-8")


rows = []


def product_value(rel, name):
    """The value the product names, read as the converted checkers read it."""
    code, out = run(["python3", "-I", M7, "product-value", rel, name])
    assert code == 0 and len(out) == 1, (rel, name, out)
    return out[0]


def value_row(rel, name, old, new, checkers, key=None):
    """Rename the constant: the reader every converted checker calls must exit non-zero."""
    restore = mutate(rel, old, new)
    try:
        code, out = run(["python3", "-I", M7, "product-value", rel, key or name])
    finally:
        restore()
    rows.append((f"`{rel}` {key or name} renamed", checkers, f"product-value exit {code}: {out[-1][:90] if out else ''}",
                 "REJECTED" if code != 0 else "ACCEPTED (bad)"))


value_row("helpers/pingbus/seat.py", "CHECKOUT_SEATS", "\nCHECKOUT_SEATS = ", "\nCHECKOUT_SEATZ = ",
          "00203 M3/M4/M6, 00194 gb_paths, 00201 M7")
value_row("helpers/pingbus/seat.py", "SESSIONS_DIR", "\nSESSIONS_DIR = ", "\nSESSIONS_DIRZ = ", "00203 M2/M3/M4/M6")
value_row("helpers/pingbus/membership.py", "APPLIED_FILE", "\nAPPLIED_FILE = ", "\nAPPLIED_FILEZ = ", "00203 M6")
value_row("helpers/pingbus/config.py", "ELEMENT_ACCEPTED_FILE", "\nELEMENT_ACCEPTED_FILE = ", "\nELEMENT_ACCEPTED_FILEZ = ",
          "00203 M6, 00211 stage 2")
value_row("helpers/pingbus/config.py", "ELEMENT_FLATPAK_DIR", "\nELEMENT_FLATPAK_DIR = ", "\nELEMENT_FLATPAK_DIRZ = ",
          "00211 stage 2")
value_row("helpers/pingbus/config.py", "STATE_DIR", "\nSTATE_DIR = ", "\nSTATE_DIRZ = ", "00211 stage 2")
value_row("helpers/pingbus/inbox.py", "CONSUMED_DIR", "\nCONSUMED_DIR = ", "\nCONSUMED_DIRZ = ", "00211 stage 2")
value_row("helpers/agent_bus/render.py", "STATE_ROOT", "\nSTATE_ROOT = ", "\nSTATE_ROOTZ = ",
          "00203 M3/M4/robust/triage-hs, 00201 M7, 00194")
value_row("helpers/agent_bus/health.py", "INSTALL_STATE_ROOT", "\nINSTALL_STATE_ROOT = ", "\nINSTALL_STATE_ROOTZ = ",
          "00203 robust, 00201 M7, 00194")
value_row("helpers/agent_bus/checkout.py", "APPLIED_DIR", "\nAPPLIED_DIR = ", "\nAPPLIED_DIRZ = ", "00201 M7, 00194, 00209 S1")
value_row("helpers/pingbus/statusline.py", "SESSIONS_DIR_TEMPLATE", "\nSESSIONS_DIR_TEMPLATE = ",
          "\nSESSIONS_DIR_TEMPLATEZ = ", "00201 M7, 00194")
value_row("helpers/pingbus/protocol.py", "TEAM_NAME_PATTERN", "\nTEAM_NAME_PATTERN = ", "\nTEAM_NAME_PATTERNZ = ", "00209 S1")
value_row("src-go/internal/bus/seat/seat.go", "seatsDir", "\tseatsDir = ", "\tseatsDirZ = ", "00209 S1")
value_row("helpers/pingbus/membership.py", "MEMBERSHIP_VAR", "\nMEMBERSHIP_VAR = ", "\nMEMBERSHIP_VARZ = ",
          "00203 m2-check, 00213 C5")
value_row("helpers/pingbus/hooks.py", "GOODBYE_REASONS", '"prompt_input_exit": "exit"', '"prompt_input_exitz": "exit"',
          "00194 gb_judge", key="GOODBYE_REASONS[prompt_input_exit]")
value_row("helpers/pingbus/hooks.py", "TEMPLATES", '    "joined": ', '    "joinedz": ', "00203 M3", key="TEMPLATES[joined]")
value_row("helpers/pingbus/envelope.py", "KIND_LINES", '    "MEMBERSHIP": "kind: ', '    "MEMBERSHIPZ": "kind: ', "00203 M3",
          key="KIND_LINES[MEMBERSHIP]")
value_row("src-go/internal/teams/hot/remove.go", "removedHomeserverDown", "const removedHomeserverDown = ",
          "const removedHomeserverDownZ = ", "00203 M4")

# The Tuwunel pin: a pin naming no version is refused by the probe through declare.py.
restore = mutate(product_value("helpers/agent_bus/declare.py", "PIN_FILE"), "\ntuwunel_version: v", "\ntuwunel_version: \n#v")
try:
    code, out = run(["python3", "-I", "-c", "import sys; sys.path.insert(0, sys.argv[1]); import triage_probe as tp\n"
                     "try:\n    tp.pinned_tuwunel_version()\nexcept tp.ProbeError as e:\n    sys.exit(f'refused: {e}')",
                     str(ROOT / "CLAUDE/Plan/00161-agent-team-bus-matrix")])
finally:
    restore()
rows.append(("tuwunel.pin names an empty version", "00161 triage_probe", f"exit {code}: {out[-1][:90] if out else ''}",
             "REJECTED" if code != 0 else "ACCEPTED (bad)"))

# expect-plain: the product says a plain session's marker is `none`; a container carrying `off` is wrong.
plain_marker = product_value("helpers/pingbus/membership.py", "MEMBERSHIP_VAR") + "=off"
restore = mutate("helpers/pingbus/membership.py", '\nMEMBERSHIP_OFF = "off"', '\nMEMBERSHIP_OFF = "none"')
scratch = ROOT / "untracked" / "proof"
scratch.mkdir(parents=True, exist_ok=True)
(scratch / "labels.json").write_text('{"ccy": "true"}', encoding="utf-8")
(scratch / "env.json").write_text(json.dumps([plain_marker, "PATH=/bin"]), encoding="utf-8")
try:
    code, out = run(["python3", "-I", M7, "expect-plain", str(scratch / "labels.json"), str(scratch / "env.json")])
finally:
    restore()
rows.append(("membership.py MEMBERSHIP_OFF = none, container has =off", "00201 M7 P1 (expect-plain)",
             f"exit {code}: {out[-1][:90] if out else ''}", "REJECTED" if code != 0 else "ACCEPTED (bad)"))

# The gate census: verdicts.py STAGE takes only ✓ lines; a ✗ verdict is then not counted.
restore = mutate("helpers/qa_environment/verdicts.py", "(?P<symbol>[✓✗⚠])", "(?P<symbol>[✓])")
(scratch / "cap.txt").write_text("✓ ruff: ok\n✗ go: failed\n", encoding="utf-8")
census = ('repoRoot="$1"; eval "$(awk \'/^census\\(\\) \\{/,/^}/\' '
          'CLAUDE/Plan/00128-qa-tool-abort-silences-thirty-gates/probe-gate-census.bash)"; census "$2"')
try:
    code, out = run(["bash", "-c", census, "census", str(ROOT), str(scratch / "cap.txt")])
finally:
    restore()
rows.append(("verdicts.py STAGE narrowed to ✓", "00128 probe-gate-census", f"exit {code}: counts {out}",
             "FOLLOWED (counts what STAGE parses)" if out == ["ruff"] else "COPY (bad)"))

# End to end: the fake verdicts stop when a value they read at source time is gone.
for rel, old, new, plan, argv in (
        ("helpers/pingbus/inbox.py", "\nINBOX_DIR = ", "\nINBOX_DIRZ = ", "00211 stage 2 --fake",
         ["./acceptance.bash", "--stage", "2", "--fake"]),
        ("helpers/pingbus/hooks.py", '"prompt_input_exit": "exit"', '"prompt_input_exitz": "exit"', "00194 --fake",
         ["./acceptance.bash", "--fake"])):
    folder = next((ROOT / "CLAUDE/Plan").glob(plan[:5] + "-*"))
    restore = mutate(rel, old, new)
    try:
        code, out = run(argv, cwd=folder, env=dict(os.environ))
    finally:
        restore()
    tail = [line for line in out if "FAKE" in line or "FAIL" in line or "m7_check" in line]
    rows.append((f"`{rel}` {new.strip()[:28]}", plan, f"exit {code}: {(tail[-1] if tail else '')[:90]}",
                 "REJECTED" if code != 0 else "ACCEPTED (bad)"))

print("| Wrong product (scratch copy) | Checker | What it did | Verdict |")
print("| --- | --- | --- | --- |")
for row in rows:
    print("| " + " | ".join(cell.replace("|", "\\|") for cell in row) + " |")
