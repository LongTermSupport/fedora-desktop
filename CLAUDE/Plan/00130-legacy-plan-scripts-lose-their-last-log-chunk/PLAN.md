# Plan 00130: legacy plan scripts lose their last log chunk

**Status**: In Progress
**Created**: 2026-09-16
**Owner**: joseph
**Priority**: Medium

## Overview

`CLAUDE/PlanWorkflow.md` used to instruct every plan script to write its run log
with `mkdir -p "$PLAN_DIR/logs"` and `exec > >(tee "$LOG") 2>&1`. Both halves are
wrong, and `CLAUDE/PlanScriptStandards.md` R4 forbids them. The document has been
corrected (`78fe841a`), so no NEW script will carry the pattern — but **ten scripts
across seven still-active plans**, of 53 examined, were written from the old
instruction and still do. `triage.bash` establishes that by glob; the "nine across
eight" this plan was filed with was hand-counted and was wrong in both figures.

A `>(…)` process substitution cannot be waited on. The shell exits, the `tee` is
still draining, and the final buffered chunk can be lost — which is the part of the
file a failed run is read for. A script can therefore die, and its log can end
mid-sentence before saying why, with nothing anywhere indicating the report is
truncated. Second, a plan-local `logs/` tree is gitignored, so `git mv` into
`Completed/` leaves it behind as an untracked orphan at the old path; Plan 00099 had
exactly that, an empty file in a directory nothing would ever remove.

This plan converts the live ones. Scripts under `CLAUDE/Plan/Completed/` are
deliberately out of scope: they will not run again, and editing an archived plan's
tooling makes its recorded history disagree with what it ran.

## Goals

- Every plan script under `CLAUDE/Plan/NNNNN-*/` that writes a run log uses
  `plan_start_log auto`, and none creates a plan-local `logs/` directory.
- A gate fails on a re-introduction, so this cannot be a one-off tidy that decays —
  the pattern came back once already, from a document that has since been fixed.
- No plan-local `logs/` directory remains in the active plan tree.
- `CLAUDE/Plan/meta-deploy.bash` takes **one** consent for the whole batch. **It already
  does, and this plan's original claim that it did not was wrong** — see the
  correction below; the goal is kept so a future conversion cannot reintroduce the
  problem unnoticed.

## Non-Goals

- Converting scripts under `CLAUDE/Plan/Completed/`. They will not run again.
- A full `_planlib.inc.bash` conversion of each script. Several of these scripts
  hand-roll the repo-root walk, the prompts and the ansible invocation too, and R1–R14
  would have things to say about all of it. That is a larger job and each plan's owner
  should judge it; this plan fixes the defect that silently destroys evidence.

## Correction: the batch was never blocked on a prompt

This plan was filed asserting that `meta-deploy.bash` could not be one consent because a
pre-library `deploy.bash` prompts for itself, and naming Plan 00075's as the case. Both
halves are false, established by reading every in-progress plan's scripts:

| `deploy.bash`                     | sources library | actually prompts |
| --------------------------------- | --------------- | ---------------- |
| 00075                             | no              | **no**           |
| 00099, 00109, 00112, 00122, 00124 | yes             | yes              |

Plan 00075's is the **only** `deploy.bash` in the tree that prompts for nothing at all,
and every script that does prompt sources the library, so `PLAN_ASSUME_YES` answers it.
`meta-deploy.bash` was testing "does not source the library" as a proxy for "will block
on input" — so its warning fired on the one script that could not block, and was silent
about the property it claimed to check. That is this repo's recurring defect: a check
whose result is indistinguishable from not having checked. The runner now requires both
halves — a `read` prompt **and** no library — and `--list` prints no exception.

The log-chunk defect below is real and unaffected. It was simply never what stood
between the operator and a one-shot run.

## Tasks

### Phase 1: Establish

- [x] ✅ **Task 1.1**: `triage.bash` — enumerates every script under
  `CLAUDE/Plan/NNNNN-*/` carrying the pattern, and every plan-local `logs/` directory
  with whether it is empty. By glob, not hand-listed, and it states how many scripts it
  EXAMINED as well as what it found — "no occurrences" and "the glob matched nothing"
  print identically otherwise, and a leg fails when the glob matches nothing.
  Result: **10 occurrences across 10 live scripts of 53 examined** — 00062, 00066,
  00075, 00079 (×4), 00080, 00098 (×2). Three LIVE `logs/` directories remain (00079,
  00080, 00098), all non-empty; eight more are in the archived tree and out of scope.
  Comment lines are excluded, or an already-converted script that documents its own
  conversion would be counted as unconverted; the pattern's first character is bracketed
  so the scanner does not match its own text.
- [x] ✅ **Task 1.2**: Recorded per script by the same run. **None of the ten sources
  `_planlib.inc.bash`** — every one needs the R1 bootstrap first, so there is no
  one-line-swap subset and the conversion is the same shape ten times over.

### Phase 2: Convert

- [x] ✅ **Task 2.1**: All ten converted. The gate written for Task 3.1 is the check:
  `PLAN-SCRIPT-LOGGING-OK: <n> plan script(s) examined, no offences` — the population
  tracks the active tree and shrinks as plans archive (51 when this was written, 45 now),
  so "no offences" is the assertion and the count is not. Convert each script
  to `plan_start_log auto`, removing the
  `LOG=`/`mkdir -p` lines **and any consumer of `$LOG`**. That last part is not
  optional: the identical conversion in Plan 00099 removed `LOG=` and left one
  `echo "Full report: $LOG"`, and under `set -u` the script then died on its own last
  line on every run — `shellcheck -x` CLEAN and `qa-all.bash` green throughout,
  because no gate executes plan scripts.

- [ ] 🧑 **Task 2.2 — PARTLY DONE, the rest needs the HOST**: Run each converted script
  far enough to prove it reaches its own last line. Linting is exactly what missed this
  class before, so this cannot be discharged by `shellcheck`.

  **Done in the container**: 00062's `triage.bash` ran to completion, exit 0, and the
  drain was verified directly — the final chunk, closing banner through last line, is
  present in the run log under `untracked/plan-runs/`. That is the defect's actual
  symptom, measured, on a converted script.

  **Done on the HOST by the 2026-09-17 batch run**: 00098's `triage.bash` (twice, as the
  before/after bracket) and its `acceptance.bash`. The acceptance run log ends with the
  full closing banner through its own last line — the `Full report:` path — so the script
  reached its end AND the log drained. Both halves, measured, on a converted script.

  **Done in the container on 2026-09-17**: 00079's `unit-test-selection.bash` — the one
  script in this set that runs anywhere, by its own header. `VERDICT: PASS`, exit 0, and
  the log drained: 5,959 bytes ending on its own last line, byte-identical to the terminal.

  Getting there took a fix, and the fix is the argument for this task. The script had been
  **dead**, silently, and no gate could have said so. `podfreeze` grew a shared freeze
  library resolved from `${BASH_SOURCE[0]}` and sourced *above* this test's cut marker, so
  sourcing the cut copy out of a flat `mktemp` file sent that resolver looking in
  `/lib/freeze`, where it called `exit 1` before one function was defined. The cut file now
  goes into a temp directory shaped like the tool's own tree — `bin/` beside
  `lib/freeze/` — so the real resolver runs unmodified against the repo's real library.
  Linting cannot find this, which is this task's whole premise.

  **Still owed, all HOST**: 00066, 00079 (×3: `triage`, `deploy`, `acceptance`) and 00080.
  00075's is moot — that plan archived on 2026-09-17. Run directly, one path each:

  ```bash
  ./CLAUDE/Plan/00066-ftp-camera-airbnb-wifi-and-hotspot-triage/triage.bash
  ./CLAUDE/Plan/00080-ccy-session-network-isolation/triage.bash
  ./CLAUDE/Plan/00079-podman-container-control/{triage,deploy,acceptance}.bash
  ```

  **00066's was not actually host-gated** — its header said HOST-ONLY and nothing enforced
  it. That matters *here* rather than only there: run in the container it would not have
  errored, every probe would have reported "absent", and it would have reached its last
  line — discharging this task with a report that was a confident wrong answer about a
  machine it never touched. It now calls `plan_require_host` (R2), placed ahead of the
  `camera` user lookup, which would otherwise have caught the container by accident and
  blamed an undeployed play, sending the reader to run Ansible in the one place this repo
  forbids it. Falsified both ways: refuses in the container, `--help` still works.

  Not through `meta-deploy.bash`, simply because they are not in its list. (This used to
  say it "runs once, over every In Progress plan, and these belong to closed ones". Both
  halves are false: the list is a hardcoded `PLANS=()` array of five — as Task 2.4 below
  says twelve lines on — and of these, 00080 is In Progress while 00066 and 00079 are
  Blocked. None is Complete.) A selection flag was briefly added here to reach them and then
  removed: it turned a batch runner into a longer way of typing a path, and a wrapper that
  can also run one thing is a second selection mode to reason about for no gain.

  **The check-0 caveat recorded here was stale and is withdrawn.** It named 00079's
  `acceptance.bash`; the anchored-grep defect was in its `unit-test-selection.bash`, and
  Plan 00079 fixed it the same day — the pattern is start-anchored now, with a
  more-than-one-match branch so dropping `$` cannot silently cut elsewhere. Re-checked
  against the current `podfreeze`: exactly one matching line. Nothing blocks these runs.

  **Not done, and cannot be here**: the rest stop early by design rather than by defect —
  `plan_require_host` refuses. All five now carry that guard, the last of them
  (`00066/triage.bash`) added by this plan, so the missing-tool branches this used to also
  list are unreachable: nothing gets far enough to look for a `camera` user or a podman.
  Stopping at a guard proves the bootstrap and `plan_start_log` work; it does not reach the
  last line. Those need a host run.

  **This is the one thing standing between 00130 and Complete.** Nothing else is owed.

- [x] ✅ **Task 2.3**: **Relocated, not removed.** All three (00079, 00080, 00098) held
  real run output — 00079's four logs go back to August. Deleting them would have
  destroyed the only record of those runs to satisfy a rule about *where* run logs live,
  so each moved to `untracked/plan-runs/<plan>/legacy-plan-local-logs/`, which is that
  rule's answer rather than its opposite. The plan folders now hold no `logs/` dir, which
  is what Task 3.1's gate asserts.

- [x] ✅ **Task 2.4**: Was "adopt `PLAN_ASSUME_YES` so the batch's one consent covers
  these scripts". **No live `deploy.bash` needs it** — see the correction above. What the
  task actually produced is the fix to `meta-deploy.bash`'s test: it now requires a `read`
  prompt AND the absence of the library, rather than inferring one from the other.
  `./CLAUDE/Plan/meta-deploy.bash --list` names no exception. (It printed "9 plans, 14
  units" when this was written; the list is hardcoded and the unit count was dropped when
  the script was simplified, so the counts are not the thing to assert — the absence of an
  exception is.) Any script this plan converts inherits `PLAN_ASSUME_YES` as a side effect
  of gaining the library, so the guarantee holds through Phase 2 rather than needing
  separate work.

### Phase 3: Make it stick

- [x] ✅ **Task 3.1**: `scripts/qa-plan-script-logging.bash`, wired into `qa-all.bash` as
  a hard, non-merged gate. It carries **both** controls in-script — a fixture with the
  pattern must be REJECTED and one without it ACCEPTED — so a scanner that stopped
  matching fails the gate rather than passing it. It also states how many scripts it
  EXAMINED, because "no offences" and "the glob matched nothing" print identically
  otherwise; examining zero is itself a failure.

  **Falsified against the real tree as well as the fixtures**, which is the stronger
  evidence: run during the conversion it reported 6 offences, then 4, then 1, then
  `PLAN-SCRIPT-LOGGING-OK: 51 plan script(s) examined, no offences`. It discriminated on
  live content, four times, rather than only against material written to be caught.
  Comment lines are excluded and that exclusion is exercised by a real file — 00099's
  converted `triage.bash` describes the old pattern twice in comments and is correctly
  not flagged.

  Scope is active plans only. `Completed/` is frozen history: rewriting a closed plan's
  scripts changes the record of what was actually run, and none of them will run again.

- [x] ✅ **Task 3.2**: **The `.gitignore` entry STAYS**, with the reasoning written beside
  it. It is genuinely what made the three orphans invisible to every `git status` that
  would have shown them — but that gap is now closed by Task 3.1's gate, which rejects a
  plan-local `logs/` dir by name and loudly. What remains is the one job the gate cannot
  do: a gate can be skipped, and if it is, an unscrubbed dump of live host state must
  still not be committable to a public repo. Removing the entry would trade a fail-safe
  for visibility that has already been restored by other means.

## Success Criteria

- [x] ✅ No script under `CLAUDE/Plan/NNNNN-*/` contains `exec > >(tee` — asserted on
  every run by `scripts/qa-plan-script-logging.bash`, not by a one-off grep
- [x] ✅ No `logs/` directory remains under `CLAUDE/Plan/NNNNN-*/` — same gate, same run
- [ ] 🧑 Each converted script has been RUN and reaches its last line — **HOST** for the
  remainder. Proven in-container, drain included, for 00062's `triage.bash` and 00079's
  `unit-test-selection.bash`; proven on the host for 00098's `triage.bash` (×2) and
  `acceptance.bash`. The rest stop at a `plan_require_host` guard, which proves the
  bootstrap but not the last line. See Task 2.2
- [x] ✅ `CLAUDE/Plan/meta-deploy.bash --list` names no script the batch consent cannot
  answer — the batch is one consent, as the operator asked for. Met, and the criterion
  now tests the property rather than a proxy for it. (Path updated: the script moved out
  of `untracked/` when it became tracked tooling)
- [x] ✅ The new gate fails on a re-introduced occurrence and passes on the clean tree —
  both controls run in-script on every invocation, and it was additionally falsified
  against the LIVE tree during the conversion: 6 offences, then 4, then 1, then none
- [x] ✅ QA passes (`./scripts/qa-all.bash`) — green. (The file count is deliberately not
  quoted: it was "973 files" here and the live run reports 1,002. The count tracks the tree,
  not this plan, so asserting it dates the criterion for no gain — Task 2.4's own lesson.)
- [ ] 🔄 `qa-reviewer` returns PASS — **run**, verdict FIX-BEFORE-MERGE with no BLOCK
  ([subagent-reports/260917-qa-reviewer-opus-5.md](subagent-reports/260917-qa-reviewer-opus-5.md)).
  Seven of its eight findings are fixed. The eighth is an owner decision, not a fix: the
  plan-local `00079/unit-test-selection.bash` duplicates `scripts/test-podfreeze.bash`,
  which covers the same 13 functions, runs on every `qa-all.bash`, and does not retire into
  `Completed/` when 00079 does. Re-run after that is settled.

## Delivery & Milestones

- Filed from Plan 00099's round-3 closing work, which fixed `PlanWorkflow.md`
  (`78fe841a`) and found the instruction had already been followed nine times.
