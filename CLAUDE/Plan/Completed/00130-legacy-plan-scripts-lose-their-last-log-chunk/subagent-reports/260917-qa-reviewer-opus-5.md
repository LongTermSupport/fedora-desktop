# QA Review — Plan 00130 working-tree changes (00066, 00079, 00112, 00109, 032, 00130)

**Verdict**: FIX-BEFORE-MERGE — nothing here breaks a user or leaks anything; two
placement/accuracy items and one foot-gun should be closed before 00130 is marked Complete.

Note: the tree moved mid-review. The first `git status` showed `helpers/containerwatch/cli.py`
plus an untracked `tests/helpers/containerwatch/test_cpu_sampler.py`; those were committed as
`5652110b` (Plan 00132) during the review. The tree is now exactly the 00130 scope, so there is
no cross-plan bundling risk. Re-establish before committing.

## Blocking

None.

## Should fix

### 1. The temp-tree fix is correct — but it invests in a file that should not be in a plan folder

`CLAUDE/Plan/00079-podman-container-control/unit-test-selection.bash` vs `scripts/test-podfreeze.bash`

The fix will not rot (see "Checked and clean"). The problem is one level up: this is a permanent
regression suite for a **shipped tool**, living in a plan folder, and `CLAUDE/Plan/CLAUDE.md` says
permanent QA belongs in `scripts/`.

Evidence, not inference:

- Every function the plan test exercises is also exercised by `scripts/test-podfreeze.bash`:
  `ccy_names`, `build_network_map`, `network_names`, `count_in_state`, `target_effect`,
  `row_verb`, `infer_action`, `identity_values`, `identity_names`, `identity_matches`,
  `select_identity`, `unlabelled_ccy_names`, `identity_axis_discriminates` — all 13, in both.
- The persistent suite is 1107 lines to the plan test's 394, with its own fixtures and cases the
  plan test lacks (e.g. `scripts/test-podfreeze.bash:615` — "an ssh-key label of `*` does not
  glob-expand against the cwd").
- `qa-all.bash` runs it on every invocation: `podfreeze: passed: 187`. Nothing ever ran the
  plan-local one, which is why it was dead.
- **The decisive one**: when 00079 completes, `git mv` takes this suite into `Completed/`, where
  00130's own Overview says scripts "will not run again" — while `podfreeze` stays shipped. A
  regression net for a live tool cannot retire with a plan.

Fix: diff the **cases** (not the counts) between the two files, port anything the persistent suite
genuinely lacks, point `acceptance.bash:162`'s check 0 at `scripts/test-podfreeze.bash`, and delete
the plan-local file. Do not compare the 47 vs 29 assertion counts and conclude anything — that is
the exact trap in AgentNotes.

The obvious objection was checked and does not hold: `acceptance.bash:162` gates properly on the
exit status and would have hard-failed on the host, so the dead test was **not** masking a green
acceptance run. It simply had not run since the library extraction.

### 2. An unchecked `mktemp -d` now writes into `/usr/bin` and `/usr/lib` as root

`unit-test-selection.bash:128-138`

The script runs under `set -uo pipefail` with errexit deliberately off (line 37, annotated).
`FUNCS_DIR="$(mktemp -d …)"` is unchecked, and `set -u` does not help because the variable is *set*
to empty. On failure:

```
mkdir -p "/bin" "/lib/freeze"          # /bin -> usr/bin, /lib -> usr/lib
ln -s  … "/lib/freeze/freeze-common.bash"
awk  … > "/bin/podfreeze-funcs.bash"
```

Confirmed uid 0 in this container and that `/bin` and `/lib` are symlinks into `/usr`. Teardown is
`rm -rf "$FUNCS_DIR"` -> `rm -rf ""`, which fails, so the debris stays. The **old** code was safe
here: an empty `FUNCS` made `awk > ""` fail harmlessly. This change introduced the hazard.

Fix: `FUNCS_DIR="$(mktemp -d -t podfreeze-funcs.XXXXXX)" || exit 1` plus a
`[ -n "$FUNCS_DIR" ] && [ -d "$FUNCS_DIR" ]` assertion before the `mkdir`. `mkdir -p` and `ln -s`
are also unchecked, but those fail loudly downstream via podfreeze's own resolver, so they are
secondary.

### 3. Two lines this diff edited assert a "change gate" that R8 removed

`CLAUDE/Plan/032-compression-helpers/deploy.bash:58`,
`CLAUDE/Plan/032-compression-helpers/PLAN.md:114-115`

The edited help text reads "`-y/--yes` consents to the change gate non-interactively", and the
edited PLAN bullet says "`deploy.bash` — runs the play, HOST-gated (R2), **one change gate**".
`032/deploy.bash` contains no `plan_confirm`, no `plan_gate_change`, no `read` — verified.
`plan_gate_change` is gone from `_planlib.inc.bash` and `PlanScriptStandards.md` R8 says
`scripts/test-planlib.bash` asserts it stays gone. `-y` is still a real flag; it just has nothing
to consent to here.

Not isolated — the lesson was never generalised:

- `CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/deploy.bash:66` — same
  sentence, same absence.
- `CLAUDE/Plan/00109-.../PLAN.md:333` — "its change gate says so before anything runs", the line
  immediately below the path edit in this diff.
- `CLAUDE/Plan/_planlib.inc.bash:295-297` — the root: `plan_mode`'s own docstring still says deploy
  "**REQUIRES** the change gate before the first ansible invocation; gather … FORBIDS the gate".
  `plan_mode` enforces no such thing. `:10` and `:571` carry the same stale wording.

Fix the library docstring first, then the two scripts and two plan files.

### 4. `CLAUDE/Plan/00130-.../PLAN.md:147-148` gives two reasons, both false

> "Not through `meta-deploy.bash`. That runs once, over every In Progress plan, and these belong to
> closed ones."

- It does **not** run over every In Progress plan. `CLAUDE/Plan/meta-deploy.bash:43-49` is a
  hand-maintained `PLANS=()` array of five entries. This plan's own Task 2.4 (line 177) says "the
  list is hardcoded" — the document contradicts itself twelve lines apart.
- They do **not** belong to closed ones. `00080` is `**Status**: In Progress`; `00066` and `00079`
  are `Blocked`. None is Complete.

The conclusion (run them by path) is right; the justification is wrong and would mislead the next
reader. The accurate reason is simply that they are not in the hardcoded list.

### 5. Plan 00130 has now edited Plan 00079's tooling twice and left 00079's PLAN.md untouched

`CLAUDE/Plan/00079-podman-container-control/PLAN.md:130`

Task 3.1 still reads "Both log to `logs/`" — 00130 Task 2.1 converted those scripts to
`plan_start_log auto` and Task 2.3 relocated the `logs/` tree. The plan file describes behaviour the
repo no longer has. Under the Plan Commit Rule this is drift the same session created.

The sharper version of this was checked and is **clean**: 00079's Task 3.3d line 166-169 ("Proved by
breaking it … the previous version left all 49 green") was performed on 2026-09-10, and the library
extraction that killed the test was Plan 00122 on 09-15/16. That falsification was done against a
live test and still stands. No tick needs revisiting.

## Nits

**6.** `00130/PLAN.md:158-161` — "the rest stop early … `plan_require_host` refuses, **or** a probe
finds no `camera` user, no reachable podman, no `lxc`." All five owed scripts —
`00066/triage`, `00079/{triage,deploy,acceptance}`, `00080/triage` — now carry `plan_require_host`,
the last of them because of this diff. The "or" branches are unreachable. The Success Criteria line
at 219 already states it correctly; line 159 is the stale one.

**7.** Counts quoted as assertions, in a plan that argues against exactly that (Task 2.4, line 178:
"the counts are not the thing to assert"):

- `:228` — "QA passes … **973 files**, green". Actual run today: `QA passed: 1002 files checked`.
- `:94` and `:194` — "`PLAN-SCRIPT-LOGGING-OK: 51 plan script(s) examined`". The gate now prints
  **45**. (Population shrinkage from archiving; none of the ten owed scripts left the active tree.)

Task 1.1's "10 of 53" is fine as a dated record of what the run found.

**8.** `unit-test-selection.bash:138` — `awk -v n="$CUT_LINE" 'NR < n - 2'`. The `- 2` is a magic
offset tied to the marker being preceded by exactly one blank line and one `# ---` rule
(`podfreeze:793-795`). If that rule line is removed the cut drops `podfreeze:792`, the closing `}`
of `freeze_hook_act`, and the source fails with an unbalanced brace — producing ~47 confusing
assertion failures rather than one named error. The persistent suite guards this with a `bash -n` on
the cut file (`scripts/test-podfreeze.bash:68-74`); the plan-local one does not. Pre-existing; moot
if finding 1 is taken.

## Checked and clean

- **Is the temp-tree fix a workaround that will rot?** No. `podfreeze:122-144`'s resolver runs
  completely unmodified and hits candidate 1 (`$dir/../lib/freeze/…`) inside the temp tree. The repo
  library is reached by symlink, so "runs the changed bytes" holds for the library as well as the tool.
- **Does the symlink open a shadowing path?** No, in both directions.
  `files/home/.local/lib/freeze/freeze-common.bash` resolves nothing relative to itself — its only
  `BASH_SOURCE` use is the line-72 direct-execution guard, where `BASH_SOURCE[0]` (temp symlink) is
  not `$0` (the test), so the guard passes correctly. The deployed `~/.local/lib/freeze/` is never in
  the candidate list unless podfreeze itself is run from `~/.local/bin`.
- **Simpler fixes that might have been missed** — both are worse: moving the cut marker above the
  `source` is wrong because the functions under test (`target_effect`, `row_verb`, `infer_action`,
  `count_in_state`) now **live in the library**; an env override for `FREEZE_LIB` would mean adapting
  a shipped tool for its own test, which `scripts/test-podfreeze.bash:8` explicitly forbids as a
  principle. The real alternative is finding 1.
- **R2 placement in 00066** — correct per `PlanScriptStandards.md:77-93` and the reference skeleton
  at `:301-313`: after arg parsing, before `plan_prime_sudo` (R3) and `plan_start_log` (R4).
  Falsified independently rather than taken on trust: bare run gives `[FATAL] refusing to run inside
  a container (found /run/.containerenv)`, exit 1; `--help` exit 0; `--bogus` exit 2. The `camera`
  lookup at `:123` is now genuinely unreachable in a container.
- **Fail-fast** — no `failed_when:`/`ignore_errors:`/`|| true`/`2>/dev/null`/`set +e` introduced.
  `unit-test-selection.bash:122-127` fails hard on an unreadable library rather than skipping, which
  is R11-correct.
- **Stderr hygiene** — the new error blocks at `:123-126` all use `>&2`; the report text stays on
  stdout under R13's orchestrator carve-out. Correct.
- **R4 teardown** — `plan_on_cleanup remove_funcs_tmpfile` retained, no hand-written EXIT trap.
  R12 — both changed scripts are `100755` in the index, `shellcheck -x` clean, `bash -n` clean.
- **Public-repo safety** — scanned all eight changed tracked files for home paths, usernames, emails,
  RFC 1918 addresses, `.local` hostnames: nothing. The only hit is the `**Owner**:` line 5 of three
  PLAN.md files carrying the operator's login — pre-existing, outside every hunk, and the repo-wide
  convention. Journal excerpts use `~/.local/...`, `octocat`, `proj_yolo` — all generic.
- **00079's log-drain claim, reproduced** — `untracked/plan-runs/00079-.../unit-test-selection/`:
  three pre-fix runs at 423/488/488 bytes ending mid-error, then three post-fix runs at **exactly
  5,959 bytes** each ending on line 96, the closing banner. One of those three was run during this
  review. The claim in `PLAN.md:118` is exact.
- **00112's evidence, audited rather than accepted** — `ok=41 changed=0 failed=0` is real at line 383
  of the batch deploy log. The recap also says `skipped=2`, which PLAN.md omits; both skips are
  "Remove a stale allowlist…" and "Remove a stale host-only list…", irrelevant to the checker, so the
  omission is not material. The `changed=0` implies convergence inference holds because
  `play-vm-test-lab.yml:176-200` deploys every `*.bash` under `files/home/.local/share/vmtest/` by
  `copy:` loop behind a non-empty assert — and the acceptance run proves it directly: "PASS the
  deployed checker is byte-identical to files/home/.local/share/vmtest/guest-acceptance-desktop.bash",
  `COVERAGE: 10 of 10`. Task 2.1's triage/deploy/deploy/triage sequence is satisfied by one batch run
  because the second pass is built into `00112/deploy.bash:88-91`, and both passes report
  `ok=15 changed=0`.
- **00130's population accounting** — complete, and re-derived rather than trusted. Task 1.1's ten:
  00062(1) + 00066(1) + 00075(1) + 00079(4) + 00080(1) + 00098(2). Discharged: 00062(1) + 00098(2) +
  00079/unit-test(1) + 00075 moot(1) = 5. Owed: 00066(1) + 00079(3) + 00080(1) = 5. 5+5 = 10. No
  member is unaccounted for.
- **Stale-path sweep** — `untracked/meta-deploy.bash` no longer exists; `CLAUDE/Plan/meta-deploy.bash`
  does. Remaining references are all in `Completed/` or `JOURNAL/` files, correctly left as history.

## Mechanical gates

- `scripts/qa-all.bash`: **PASS** — 1002 files. `plan-script-logging: 45 plan script(s) examined, no
  offences`; `podfreeze: passed: 187`; `planlib-tests: PASSED (library version 1.4.0)`. One expected
  advisory: `deployed-drift: skipped (CCY container)`.
- `hooks-daemon plan-qa --sweep`: **7 findings, 0 block, 7 advise** — exit 1. All pre-existing and
  none in a file this diff touches: one path-existence in 00046, five journal-ordering (00063,
  00109 x3, 00119), one journal-freshness list. 00130's new journal entries were not flagged.
- `ansible-playbook --syntax-check`: **not triggered** — no `playbooks/`, `tasks/`, `vars/` or
  `environment/` file is in the diff. Stated rather than skipped silently.
- `qa-helper-tests.bash`, `check_extension_compat`, extension ESLint: **not triggered** by this diff
  (the `helpers/` change left the tree as commit `5652110b`). `qa-all.bash` ran all three regardless
  and they were green — helper-tests 1720 tests / 71 modules, extension-compat and js clean.
- `shellcheck -x` + `bash -n` on both changed scripts: **clean**.
- Both changed scripts were executed end-to-end in this container to confirm behaviour, not just parse.
