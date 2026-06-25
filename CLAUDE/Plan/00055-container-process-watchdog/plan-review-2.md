# Plan 00055 — Audit Round 2

**Created**: 2026-06-24

**Scope**: Second adversarial technical + convention audit of `PLAN.md`,
`context.md`, and `testing.md` for Plan 00055 (Container Process Watchdog),
checking the round-1 revisions for residual gaps, regressions, and newly
introduced issues. Verified against the live fedora-desktop codebase (extension
layout, DBus precedent, `qa-all.bash` aggregator shape, helper/test
infrastructure, systemd-user precedents, playbook wiring).

## Overall Assessment

The round-1 revision pass is genuinely thorough: every one of the 12 prior
findings has been correctly resolved against the real repo, not just
hand-waved. I re-verified the load-bearing claims directly — extensions really
do live under `extensions/<uuid>/` and deploy from `{{ root_dir }}/extensions/…`
(`play-speech-to-text.yml:16-18`, `play-gnome-shell-extensions.yml:101`); the
DBus namespace really is all-lowercase `org.fedoradesktop.SpeechToText`
(`files/home/.local/bin/wsi:74-75`); `play-speech-to-text.yml` really is
imported nowhere (so the F4 "don't assume a `playbook-main.yml` import" caveat is
accurate); there really is no `.timer` anywhere in `files/` (F9 correct); helper
tests really are collected from `tests/helpers/**/test_*.py` by
`qa-helper-tests.bash`; and the only semgrep ruleset really is bash-scoped
(`.semgrep/bash-conventions.yml`), so the F5 grep-gate decision is right.

The plan is now in good shape and is safe to begin Phase 0/1. The residual
findings below are all **low severity** — they are implementation-precision notes
that would otherwise surface as friction during the build, not design errors.
The one I'd most want addressed before Phase 5 is R1 (the no-kill gate's wiring
into `qa-all.bash` is a structural edit the plan under-describes), but none block
starting work.

## Findings

| id  | severity | category              | title                                                                            | actionable |
| --- | -------- | --------------------- | -------------------------------------------------------------------------------- | ---------- |
| R1  | low      | feasibility           | No-kill gate "wired into qa-all.bash" understates a structural aggregator edit   | yes        |
| R2  | low      | repo-convention       | Extension deploy-file-list rule (extensions/CLAUDE.md) not surfaced as a task    | yes        |
| R3  | low      | technical-correctness | `cmd`/`exec_hint` in report.json can leak in-container identifiers (public-repo) | yes        |
| R4  | low      | testability           | L0 no-kill grep gate risks false-positives on its own guidance strings           | yes        |
| R5  | low      | completeness          | "Sustained" gate persists per-PID state but PID-reuse across ticks unaddressed   | yes        |

### R1 — No-kill gate "wired into qa-all.bash" understates a structural aggregator edit (low, feasibility)

**Detail**: Task 5.1 and testing.md §2 say to add the no-kill guard as "a small
`grep`-based bash gate wired into `qa-all.bash`". Verified that `qa-all.bash`
(lines 18-106) is **not** a loop over a discovered stage list — it is a hardcoded
6-stage pipeline: 6 named `mktemp` vars, a 6-path `trap` cleanup, six explicit
`rc=0 … || rc=$?` blocks, and a final `jq -s` merge whose `checks` object is keyed
by **positional index** `.[0]`…`.[5]` against exactly six temp files. Adding a 7th
gate is therefore a five-point edit (new `TMP_*`, extended `trap`, new rc-block,
new `.[6]` positional key, new `checks.nokill` entry), not a drop-in. The plan
calls it "small", which is true in spirit but will mislead an implementer who
expects a plugin-style hook.

**Recommendation**: Note in Task 5.1 that wiring the gate means editing the
`qa-all.bash` aggregator itself (add the `TMP_*` var, the `trap` path, the
`rc`-block, and the positional `jq` merge key) — or, simpler and lower-risk, fold
the no-kill grep **into the existing `qa-patterns.bash`/`qa-bash.bash` stage**
rather than adding a 7th top-level stage. Either is fine; just don't imply a
zero-touch wire-in.

### R2 — Extension deploy-file-list rule not surfaced as a task (low, repo-convention)

**Detail**: `extensions/CLAUDE.md` carries a hard rule (lines 20-46): *every* new
`.js`/`metadata.json` file added to an extension dir MUST also be added to the
playbook's "Copy Extension Files" loop, or it silently never deploys. Plan Task
3.1/3.2 create `extension.js` + `metadata.json`, and Task 4.1 writes the deploy
play, but nothing explicitly ties them together with this rule. For a brand-new
extension the whole file set is new, so this is exactly the failure mode the rule
warns about. (`play-speech-to-text.yml` uses an explicit per-file loop; if the new
play instead copies the directory like `play-gnome-shell-extensions.yml:100-102`
the rule is satisfied structurally — but the plan should say which.)

**Recommendation**: In Task 4.1, state whether the play copies the extension
**directory** (recursive `copy:` of `{{ root_dir }}/extensions/container-watch@…/`,
which auto-includes every file — preferred, matches `workspace-names-overview`) or
an explicit per-file loop; if the latter, list `extension.js` + `metadata.json`
(+ any `prefs.js`/schemas) so no file is silently undeployed.

### R3 — `cmd`/`exec_hint` in report.json can leak in-container identifiers (low, technical-correctness)

**Detail**: This is a public repo, but `report.json` is a **runtime artifact**
under `$XDG_RUNTIME_DIR` (context.md §7), so the pre-commit secret scanner never
sees it — fine. The real risk is the **L1 fixtures and the `--inject` fixture
files**, which *are* committed and which the design fills with example
`container_name`, `cmd`, and `exec_hint` values (context.md §4b shows
`<project-a>_yolo`, a full `ugrep … /` command line, `owner_uid: 1000`). The plan
already placeholders the context doc, but the testing matrix (testing.md §3) and
any checked-in `fixtures/proc/<pid>/cmdline` + sample finding JSON must hold the
same line — a real workspace path or project name pasted into a fixture cmdline
would leak and bypass the commit scanner only if it isn't email/IP-shaped.

**Recommendation**: Add a one-line note to Task 5.2 / testing.md §3 that all
committed fixtures and `--inject` sample findings use reserved placeholders per
`CLAUDE/ExampleValues.md` (`<project-a>`, `example.com` paths, synthetic
container IDs) — never a real container name, workspace path, or argv captured
from an actual incident.

### R4 — L0 no-kill grep gate risks false-positives on its own guidance strings (low, testability)

**Detail**: The reporting-only design *deliberately* puts kill-shaped text into
the product: `exec_hint` guidance may include `kill <container-pid>` (context.md
§2c line 132: *"in the container run `kill <container-pid>`"*), and the CLI
`explain` output shows the human how to resolve it. A naive grep for `kill` /
`send_signal` / `SIG` across the reporter Python + extension JS will match those
**string literals** and red-fail the build on the very guidance the tool is
supposed to print. The guard must match **call sites** (e.g. `os.kill(`,
`.send_signal(`, `subprocess.*kill`), not substrings, and must tolerate `kill`
appearing inside a quoted hint template.

**Recommendation**: Specify in Task 5.1 / testing.md §2 that the grep targets
**executable call patterns** (`os.kill(`, `signal.SIG`, `.send_signal(`,
`Gio.Subprocess`-with-`force_exit`/`send_signal`, `pkill`/`kill ` as a spawned
argv), and explicitly **excludes** `kill` inside `exec_hint`/guidance string
literals — add a tiny self-test fixture (a file containing a benign `kill`-in-a-hint
string that must pass) so the gate's own scoping is regression-guarded, mirroring
how `.semgrep/bash-conventions.bash` self-tests the bash ruleset.

### R5 — "Sustained" gate persists per-PID state but PID-reuse across ticks unaddressed (low, completeness)

**Detail**: The PID-reuse guard from round 1 (F8) correctly covers the **two
in-invocation CPU samples** (Task 1.2: re-check `starttime` across the ~1-3 s
delta). But §4a also offers an optional **"sustained over N consecutive timer
ticks"** gate that persists the last per-PID sample in the state dir across timer
firings (minutes apart). Across that much longer gap PID reuse is far more likely,
yet nothing says the persisted-state path must also key on (or re-validate)
`starttime` — a recycled PID could inherit a prior tick's "hot" count and trip the
sustained gate for an innocent new process.

**Recommendation**: If the sustained gate is implemented (it's optional / "add if
noisy"), note that the persisted per-PID record must include `starttime` (field
22\) as part of its identity key, and a mismatch resets the consecutive-hot counter
rather than carrying it over. Add this only when/if the sustained gate is built —
flag it now so it isn't forgotten.

## Convergence

**Converged — the plan is solid; no further full revision pass is required.**
All 12 round-1 findings are correctly resolved against the verified repo reality,
and no regressions or new high/medium issues were introduced by the revisions.
The five residual findings are all **low-severity implementation-precision notes**
(QA-aggregator wiring shape, extension deploy-file rule, fixture placeholder
hygiene, grep-gate self-scoping, sustained-gate PID-reuse). They are best folded
into the relevant tasks as one-line clarifications during implementation rather
than gating another audit round. The architecture, scope (reporting-only),
attribution technique, and test-seam strategy are sound and ready for Phase 0/1.
