# Plan survey — seven "In Progress" plans with goals owed (branch F44, 2026-09-12)

Ledger source: `.claude/hooks-daemon/untracked/goal-ledger.json`.
HEAD at time of survey: `58529e4` (2026-09-12).
All seven folders are in `CLAUDE/Plan/` — **none** has moved to `CLAUDE/Plan/Completed/`.
All seven appear under `## Active Plans` in `CLAUDE/Plan/README.md`.

## Conventions (from `CLAUDE/PlanWorkflow.md` + `CLAUDE/core/PlanWorkflow.core.md`)

- Allowed `**Status**:` tokens: `Not Started`, `In Progress`, `Complete`, `Blocked`,
  `Cancelled`, `Superseded`, `Dormant`. Any **terminal** status (Complete, Cancelled,
  Superseded) "requires the plan folder to move into the archive directory (`Completed/`,
  or `Cancelled/` where configured) in the **same commit** that sets the status --
  alongside the README index row update and statistics recount."
- Task icons: `⬜` Not Started, `🔄` In Progress, `✅` Completed, `🚫` Blocked,
  `❌` Cancelled, `⏸️` On Hold, `👁️` Needs Review.
- Task form: `- [ ] ⬜ **Task N.M**: title`. Sub-items are indented checkboxes.
- CCY rule (`CLAUDE/PlanWorkflow.md:363-365`):
  "> **CCY container reminder**: if the project path is `<repo>/`, you are in a CCY
  > container — **edit and commit only, never run Ansible playbooks**. Deployment and
  > live testing happen on the HOST."
- Completion checklist step 0 is a pending-release holding-area gate. **This repo has no
  `CLAUDE/UPGRADES/` holding area** (verified absent), so that gate is satisfied by a
  success criterion stating there are no release-bound consequences.
- Plan QA gate: `./scripts/qa-all.bash` (six stages). No pytest/coverage gate in this repo.

---

## 1. 00063 — headless `run.bash` server & cloud provisioning

**Path**: `<repo>/CLAUDE/Plan/00063-headless-run-bash-server-cloud-provisioning`
**Title**: `# Plan 00063: Headless `run.bash` — Server & Cloud Provisioning`
**Declared status**: `**Status**: In Progress`  (Created 2026-07-23, Owner joseph, Priority Medium)

**Completion state**: 13 tasks ticked / **7 outstanding** (5 × `🔄`, 2 × `⬜`).
Success Criteria: **0 of 8** ticked.

Next outstanding tasks, verbatim:

> `- [ ] 🔄 **Task 2.2**: Secret file-pointer plumbing (D2). Done: `hl_resolve_secret`
>   with the V3.10 guardrails, literal-env `unset` before the first child, the
>   `set -u`-safe `HL_SECRET_FILES` trap, stderr-clean `headless_fail`. Pending:
>   delete-after-use and ssh-agent teardown are HOST-verified in Phase 3.`

> `- [ ] ⬜ **Task 3.1**: On a real or VM Fedora Server or Cloud box, run `run.bash`
>   headless via env and confirm end-to-end provisioning with zero prompts.`

The other four `🔄` tasks (2.3, 2.4, 2.5, 2.8) each say some variant of
"Code complete; HOST-verified in Phase 3" / "end-to-end execution assertions are
HOST-only (Phase 3)".

**Last activity**: newest JOURNAL day-file `00063-Journal-26-09-02.md` → **2026-09-02**.
Last commit touching the folder: **2026-09-02**, `03ccaaf5 Plan tree: slim 13 oversized
plans into lean PLAN.md + PLAN_archive.md` — i.e. no substantive work since July; the
September touch is the repo-wide slimming pass.

**Genuinely actionable in this container?** **No.** Phase 3 is literally headed
`### Phase 3: Verification (HOST, not the CCY container)`. It needs a real or VM Fedora
Server / Cloud machine to provision. There is no editing work left that would move a
checkbox.

**Decision gate**: `DECISIONS.md` present; decisions 1–6 all resolved (Task 1.6 closed the
last by deferring the `RUN_BASH_GITHUB_ACCOUNTS=none` path to Plan 00082, now Complete).
Nothing awaiting user input.

---

## 2. 00068 — ccy CI runner variant

**Path**: `<repo>/CLAUDE/Plan/00068-ccy-ci-runner-variant`
**Title**: `# Plan 00068: Make ccy fully non-interactive so CI can invoke it`
**Declared status**: `**Status**: In Progress`  (Created 2026-07-29, Owner joseph, Priority High)

**Completion state**: 8 tasks ticked / **2 outstanding** (both `⬜`).
Success Criteria: **8 of 9** ticked.

Outstanding tasks, verbatim:

> `- [ ] ⬜ **Task 3.4**: The CI tool surface (Decision 9). No longer gated: `--disallowedTools`
>   composes with `--dangerously-skip-permissions`, measured 2026-08-10. Remaining work:
>   1. Specify the per-event surfaces: `push`/`pull_request` (may run `ci.bash` and read; no write,
>      commit or push) and `issues`/`issue_comment` (narrower, no `ci.bash`).
>   2. Derive every layer from one per-class list, and assert the tool names absent from the
>      session, never a tool count.
>   3. Prefer an allowlist wherever the vocabulary is not ours; a denylist fails open on a typo.`

> `- [ ] ⬜ **Task 4.1**: Create the implementation plan.`

Remaining success criterion: `- [ ] ⬜ The CI tool surface is specified per event.`

**Last activity**: newest JOURNAL day-file `00068-Journal-26-09-02.md` → **2026-09-02**.
Last commit touching the folder: **2026-09-02**, `03ccaaf5` (the same slimming commit).
Real work stopped 2026-08-10.

**Genuinely actionable in this container?** **Yes — fully.** The plan states
"This plan specifies; it does not build. Implementation is handed to a separate plan
(Task 4.1)." Both remaining tasks are document authoring: write the per-event tool-surface
spec (into `reports/` and/or `DECISIONS.md`), then scaffold the successor plan with
`CLAUDE/Plan/mkplan.bash` (next number is 00110 per the git counter). No host, no Ansible,
no hardware. Phase 4 is explicitly headed "Ungated since 2026-08-10: the scope is the
owner's and the restriction mechanism is settled by measurement (E9)."

**Decision gate**: **Yes — two live owner questions.** `DECISIONS.md` `## 8. Open decisions — owner`:

> `2. **`ccy.env` sourcing** (`entrypoint.sh:269-274`) executes shell from the checkout. Acceptable
>    under trusted-automation-only, or gated off in non-interactive mode? A CI agent that may not
>    write the repo can still be handed arbitrary shell from the repo.`
> `3. **The `<home>/.claude` → `<repo>` symlink** (`:183-195`) puts session state in the job
>    checkout. Same question.`

Item 1 is struck through (closed 2026-08-01). Neither open item blocks Task 3.4 or 4.1, but
the successor plan inherits them. Proof obligations B1–B4 and C1/C2 also remain `⬜`
(interactive / needs a host listener).

---

## 3. 00079 — Podman container control (`podfreeze`)

**Path**: `<repo>/CLAUDE/Plan/00079-podman-container-control`
**Title**: `# Plan 00079: Podman container control — freeze/thaw by container, network, and CCY group`
**Declared status**: `**Status**: In Progress`  (Created 2026-08-19, Owner joseph, Priority Medium)

**Completion state**: 20 tasks ticked / **2 outstanding**.
Success Criteria: **0 of 7** ticked — deliberately; Task 3.3b says
"Every success criterion below stays unticked until it does."

Outstanding tasks, verbatim:

> `- [ ] 🚫 **Task 3.3b**: **Blocked — HOST ACTION.** Re-run
>   `CLAUDE/Plan/00079-podman-container-control/deploy.bash` on the HOST. Task 3.3d
>   changed `podfreeze` again, so the host binary is stale by design and
>   `acceptance.bash` will refuse to vouch for it (`acceptance.bash:134-140`). This
>   is the only outstanding review finding: **no acceptance run has ever executed
>   against the reviewed code** — runs 3, 4 and 5 all predate the fix commit, so
>   checks 13, 13b and the COVERAGE line have never run against what they were
>   written for.`

> `- [ ] ⬜ **Task 3.4**: Mark plan Complete, move to `Completed/`, update README
>   index + statistics in the same commit`

**Last activity**: newest JOURNAL day-file `00079-Journal-26-09-10.md` → **2026-09-10**.
Last commit touching the folder: **2026-09-11**, `d45da38 QA: unblock the two gates that
have been red since 2026-09-08`.

**Genuinely actionable in this container?** **No.** 3.3b runs two Ansible plays
(`play-claude-yolo.yml`, `play-podfreeze.yml`) on the host and then an acceptance suite
that creates and pauses real Podman containers. 3.4 is gated behind 3.3b.

**Decision gate**: Phase 0 is headed `### Phase 0: Host triage + decision gate`, and
Task 0.3 records `**Decision gate PASSED.** Rootless pause is viable (cgroups v2 +
systemd + crun), so the plan proceeds as designed`. Nothing awaiting user input.
Task 3.3d also raises, deliberately out of scope, a repo-wide migration: "48 of the 69
tracked plan scripts do not source `_planlib.inc.bash` … It is a repo-wide migration and
wants its own plan."

---

## 4. 00081 — secret-scanner and QA-gate coverage holes

**Path**: `<repo>/CLAUDE/Plan/00081-secret-scanner-and-qa-gate-coverage-holes`
**Title**: `# Plan 00081: Secret-scanner and QA-gate coverage holes`
**Declared status**: `**Status**: In Progress`  (Created 2026-08-20, Owner joseph, Priority High)

**Completion state**: 18 task checkboxes ticked (14 top-level) / **1 outstanding**.
Success Criteria: **7 of 7 ticked**, including
`- [x] ✅ `./scripts/qa-all.bash` passes`.

The only outstanding item, verbatim:

> `- [ ] ⬜ **Task 3.2**: Mark Complete, move to `Completed/`, update the README
>   index + statistics in the same commit. `CLAUDE/Plan/README.md`'s row still says
>   "remaining phases cover `qa-python.bash` and `qa-deployed-drift.bash`" — both
>   done — so rewrite it in that commit`

(Confirmed: that stale sentence is still present verbatim at `CLAUDE/Plan/README.md:135`.)

Task 3.1 is closed: "`qa-reviewer` over the full diff, 2026-09-10. Verdict **BLOCK** —
2 blocking, 6 fix-before-merge, 10 nits. Every one resolved; each fix verified by
reverting it and watching something go red."

**Last activity**: newest JOURNAL day-file `00081-Journal-26-09-10.md` → **2026-09-10**
(last entry `## 16:08 · note · plan-document corrections`).
Last commit touching the folder: **2026-09-10**, `0d7d1f3 Plan 00081: close the review's
remaining findings, and compile the template nothing read`.

**Genuinely actionable in this container?** **Yes — and it is the only one of the seven
that can be driven to a terminal state from here.** The remaining work is exactly the
Plan Completion Checklist: flip `**Status**:` to `Complete`, tick Task 3.2, `git mv` the
folder into `CLAUDE/Plan/Completed/`, remove the Active row, add a Completed row, rewrite
the stale description, recount the statistics, one atomic commit
(`Plan 00081: Complete - …`). No host, no Ansible, no hardware, no human decision.
Holding-area gate (step 0) is a no-op here — this repo has no `CLAUDE/UPGRADES/`.

**Decision gate**: none. No `DECISIONS.md`, no "Decision Gate" section. The Risks table
records one owner decision already taken ("Out of scope here and the owner's decision").

---

## 5. 00092 — CCY child-claude spawn mode

**Path**: `<repo>/CLAUDE/Plan/00092-ccy-child-claude-spawn-mode`
**Title**: `# Plan 00092: CCY child-claude spawn mode`
**Declared status**: `**Status**: In Progress`  (Created 2026-09-02, Owner joseph, Priority Medium)

**Completion state**: 14 task checkboxes ticked / **1 outstanding**. Phases 1–5 are
collapsed under `### Phases 1–5: complete — the feature is built and shipped through IaC`.
Success Criteria: 5 ticked / **2 open**.

Outstanding task, verbatim (head):

> `- [ ] 🚫 **Task 6.4**: **Blocked — HOST ACTION, cannot run in the container.**
>   Every step is a script in this folder, not a command to retype from chat, per
>   [PlanTriage.md](../../PlanTriage.md). All three refuse to run in the wrong place.`

Its six steps: host `triage.bash`; host `deploy.bash` (runs `play-claude-yolo.yml`);
`ccy --rebuild` (launcher now asks for container **2.36**); host `triage.bash` again;
`acceptance.bash` **inside** a container with `CCY_CHILD_CLAUDE=1`; `acceptance.bash`
inside a **later** container with the flag removed.

Open success criteria, verbatim:

> `- [ ] 🚫 `acceptance.bash` green with the flag on and with it off. **Blocked`
> `- [ ] 🔄 `qa-reviewer` returns no BLOCK or FIX-BEFORE-MERGE finding.`

**Last activity**: newest JOURNAL day-file `00092-Journal-26-09-10.md` → **2026-09-10**.
Last commit touching the folder: **2026-09-10**, `80c7e5f Plan 00092: close every
re-review finding, and stop the probes passing vacuously`.

**Genuinely actionable in this container?** **Mostly no.** Task 6.4 is host + image
rebuild. The one piece of in-container work is another `qa-reviewer` pass over the fixes
made in Tasks 6.6/6.7/6.8 (which have not themselves been reviewed — the same reasoning
that created Task 6.5). That is real but marginal, and it cannot close the plan.
Task 6.7 also records `PRESENT` cases "written but NOT yet exercised" — they can only run
in a container with the mode on, which requires the host deploy + rebuild first.

**Human prerequisite**: "Before running it: I1 will report the session transcript from
2026-09-02 unless the OAuth token is rotated first."

**Decision gate**: `DECISIONS.md` present; Decisions 1–5 all settled (Decision 4 added by
Task 6.6, Decision 5 by Task 6.8). No open gate.

---

## 6. 00104 — suspend aborts on dock unplug, never re-suspends

**Path**: `<repo>/CLAUDE/Plan/00104-suspend-aborts-on-dock-unplug-never-resuspends`
**Title**: `# Plan 00104: Suspend aborts on dock unplug and never re-suspends`
**Declared status**: `**Status**: In Progress`  (Created 2026-09-08, Owner joseph, Priority High)

**Completion state**: 37 task checkboxes ticked (9 top-level) / **11 outstanding**
(4 top-level). Success Criteria: 2 ticked / 4 open.

Next outstanding tasks, verbatim:

> `- [ ] ⬜ **Task 1.2**: Run `./triage.bash` on the HOST to capture a reproducible baseline
>   - [ ] ⬜ Confirm the report reproduces F4, F5, F7, F9, F10 from live state
>   - [ ] ⬜ Note any divergence from `TRIAGE-EVIDENCE.md` in the JOURNAL`

> `- [ ] 🔄 **Task 2.1**: Test **H1** — does anything re-evaluate on an AC change?`
>   remaining sub-item:
>   `- [ ] ⬜ **Deferred** (needs an idle moment, not a working session): the lid-closed half
>     — close the lid on AC, unplug, and observe whether it ever suspends`

Also open: `- [ ] ⬜ **Task 2.2**: Settle **P1** — does logind count evdi/DisplayLink
outputs as displays?` (needs a dock-attached `triage.bash` run) and
`- [ ] ⬜ **Task 4.2**: Reproduce the original incident and confirm recovery`.

**Last activity**: only JOURNAL day-file `00104-Journal-26-09-08.md` → **2026-09-08**.
Last commit touching the folder: **2026-09-08**, `f9d575f7 Plan 00104: mark deploy and
review complete, record qa-all as blocked`.

**Genuinely actionable in this container?** **No — hardware.** Phases 1, 2 and 4 are each
headed `(HOST)`. Task 4.2 requires physically suspending the laptop, unplugging the dock
within ~3 seconds and confirming re-suspend from the journal. Phase 3 — the one headed
`### Phase 3: Implement the fix (CCY container: edit + commit)` — is **100% complete** and
already deployed on the host (Task 4.1, `ok=18 changed=6 failed=0 skipped=2`).

**Caveat worth re-checking**: the plan records
`- [ ] QA passes (`./scripts/qa-all.bash`) — **blocked, not passed**` because
`qa-python` exits rc=2 on a ruff pin mismatch (`.ruff-version` 0.16.0 vs host 0.16.3).
Commit `d45da38` (2026-09-11) is titled "QA: unblock the two gates that have been red
since 2026-09-08", so this may now be clearable — but it must be *verified*, not assumed.

**Decision gate**: **Yes, but hardware-gated, not user-gated.** `DECISIONS.md` Decision 2
("Treat the AC→battery state change separately, and gate it on H1"):
`**Decision**: Deferred to a decision gate in Phase 3, **after** H1 and P1 are settled on the
host. Building a udev rule for a state transition that has not been demonstrated would be
speculative; if H1 is refuted, Phase 1 alone may be sufficient.` That gate cannot open until
Task 2.1's deferred lid-closed half and Task 2.2 produce host measurements.

---

## 7. 00109 — desktop drift detection and fedora desktop panel

**Path**: `<repo>/CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel`
**Title**: `# Plan 00109: desktop drift detection and fedora desktop panel`
**Declared status**: `**Status**: In Progress`  (Created 2026-09-11, Owner joseph, Priority High)

**Completion state**: 12 task checkboxes ticked (5 top-level) / **49 outstanding**
(16 top-level, spread over Phases 0–5). Success Criteria: **0 of 7**.
By far the largest remaining body of work of the seven.

Next outstanding tasks, verbatim:

> `- [ ] ⬜ **Task 0.2**: Remove orphaned DKMS source trees
>   - [ ] ⬜ Establish whether `/usr/src/evdi-1.14.{10,11,12,16}` are reclaimable and
>     who owns them (RPM-owned vs left behind) — probe goes in `triage.bash`
>   - [ ] ⬜ Add cleanup to the owning play, gated on the tree being unregistered in DKMS
>   - [ ] ⬜ Run QA, deploy on HOST, re-run `triage.bash` to confirm`

> `- [ ] 🚫 **Task 0.3**: Fix group/world-readable vault password file permissions
>   - **Blocked — human-only.** The path is protected by `secret_file_guard`; an agent
>     cannot name it in a command, a script, or a playbook task, so this cannot be
>     fixed via IaC by an agent. Flagged at session start by `secret_file_hygiene_checker`.
>     Requires the user to set owner-only permissions by hand, or to lift the guard.`

First in-container-actionable task after Phase 0:

> `- [ ] ⬜ **Task 1.1**: Design the ledger record and its location
>   - [ ] ⬜ Decide record shape: play path, repo commit at run, play file hash, run
>     outcome, timestamp. …
>   - [ ] ⬜ Decide storage location and ownership (host state, not repo state …)
>   - [ ] ⬜ Record the decision in `## Technical Decisions``

**Last activity**: only JOURNAL day-file `00109-Journal-26-09-11.md` → **2026-09-11**
(freshest of the seven). Last commit touching the folder: **2026-09-11**,
`72c347b DisplayLink: carry the background-repaint signal on a key whose loss is invisible`.

**Genuinely actionable in this container?** **Largely yes, if Phase 0 is skipped.**
Task 0.2 needs a host probe + deploy; Task 0.3 is human-only. But:
- Phase 1 Task 1.1 is pure design + a `## Technical Decisions` entry — editable here.
- Phase 1 Tasks 1.2/1.3 and Phase 2 Tasks 2.1/2.2/2.3 are script-writing (ledger callback
  plugin, freshness check, installed-vs-pinned check, wiring into `qa-all.bash`) — all
  editable here; only their "deploy on HOST / verify" tails are blocked. Note Task 2.2's
  gate is itself testable from a fixture: "must report FAIL against the 2026-09-11 state
  (evdi 1.14.16 installed, 1.15.0 pinned). A check that cannot fail against the incident it
  was built for is not a check."
- Phase 3 (health probe, notification, Claude Code handoff) and Phase 4 (scaffold
  `extensions/fedora-desktop@fedora-desktop`, ESLint clean) are also editable here.
- Phase 5 remainder is host/observation-gated (see below).

**Decision gate**: no `DECISIONS.md`, but three items await the user, embedded as tasks:
- Task 0.3 (above) — human-only, `secret_file_guard`-protected path.
- Task 5.1: `- [ ] ⬜ **Confirm with the user**: literally black, or dark blue-grey? Blue-grey
  would overturn the above and point back at the flat-colour path.`
- Task 5.4: `- [ ] ⬜ Confirm the toggle actually recovers it on this host before building
  anything around it — one manual toggle, next time the symptom appears` — i.e. it waits on
  the fault recurring in the wild. Journal: "**Not yet verified**: that the refresh clears
  the black background when the fault is actually present."

---

## Ranked recommendation

### #1 — **00081** (secret-scanner and QA-gate coverage holes) — the pick

It is the only one of the seven that can be taken from "In Progress" to a terminal state
entirely from inside this container.

- Exactly **one** item remains, and it is repo bookkeeping: status flip, `git mv` into
  `Completed/`, README row rewrite (the row is provably stale — `README.md:135` still
  advertises "remaining phases" that Task 2.x closed), statistics recount, one atomic commit.
- **All seven success criteria are already ticked**, including `./scripts/qa-all.bash`
  passes — so the completion checklist's verification steps are pre-satisfied.
- The `qa-reviewer` BLOCK verdict from 2026-09-10 was fully resolved, each fix proved by
  reverting it and watching a gate go red.
- Zero host dependency, zero hardware, zero decision gate, zero Ansible.
- It also *removes a documented inaccuracy* from the plan index, which the plan-QA handlers
  care about — a second benefit beyond clearing the ledger entry.

Risk: essentially none. The only care needed is that the status flip, the `git mv`, the
README edits and the stats recount all land in **one** commit, per the same-commit rule.

### #2 — **00068** (ccy CI runner variant)

Best candidate if the goal is substantive work rather than closure. The plan explicitly
"specifies; it does not build", Phase 4 is ungated since 2026-08-10, and both remaining
tasks are document authoring that needs nothing but a text editor. Downsides: it produces a
specification nobody can execute yet, it has been dormant-in-fact since 2026-08-10 despite
the "In Progress" label, and its two open owner decisions (`ccy.env` sourcing, the
`<home>/.claude` symlink) will land on the successor plan on day one — so Task 4.1 arguably
wants the user's answers first.

### #3 — **00109** (desktop drift detection + panel)

The highest-value and freshest work, with Phases 1–4 genuinely buildable here. Ranked below
00068 only because it is a large greenfield effort whose every phase terminates in
"deploy on HOST", Phase 0 is blocked two different ways (host probe + human-only permission
fix), and three items wait on the user or on the fault recurring. Building four phases that
cannot be verified before handing them back is a lot of unvalidated surface. A good choice
if the coordinator wants volume; a poor one if it wants closed loops.

### #4 — **00092** (CCY child-claude spawn mode)

One task left, host-only (deploy + `ccy --rebuild` + two in-container acceptance runs).
The single in-container move is another `qa-reviewer` pass over Tasks 6.6–6.8's unreviewed
fixes; it cannot close the plan. Also needs the user to rotate the OAuth token before the
host run, or invariant I1 will fire on the 2026-09-02 transcript.

### #5 — **00079** (podman container control)

Cleanly and correctly blocked: no acceptance run has ever executed against the reviewed
code, and only a host deploy can change that. Close-out (3.4) is gated behind it. The right
action is to leave it alone and tell the user it needs one `deploy.bash` run on the host.

### #6 — **00063** (headless `run.bash`)

All code is written; every remaining checkbox is Phase 3 verification on a real or VM Fedora
Server / Cloud box. Nothing to edit, nothing to decide. Arguably its status should be
`Dormant` rather than `In Progress` — it has had no substantive commit since July.

### #7 — **00104** (suspend aborts on dock unplug)

Hardware-blocked outright: the remaining tasks require physically suspending the laptop and
unplugging a dock inside a three-second window, plus dock-attached triage runs. Its
container-side phase (Phase 3) is already complete and deployed. Only worthwhile follow-up
from here: re-check whether `d45da38` cleared the `qa-all.bash` ruff-pin block that the plan
records as "blocked, not passed" — and if so, tick that criterion honestly.

---

## Cross-cutting observations

- **Four of seven (00063, 00079, 00092, 00104) are blocked solely on the host.** Their
  "In Progress" status is accurate in the sense that work remains, but none of it can happen
  in a CCY container. If the ledger's purpose is "what can an agent pick up", these four
  should arguably read `Blocked` — the token is allowed, and the plans already mark their
  individual tasks `🚫`.
- **00081 is finished work wearing an In-Progress label** — precisely the failure mode
  `PlanWorkflow.core.md` warns about ("plans that sit `In Progress` for weeks with nothing
  left to do, hide real in-flight work among them, and make 'is the slate clean?'
  unanswerable").
- **00063 and 00068 have had no substantive commit since 2026-08-10 / July**; both were
  touched only by the 2026-09-02 slimming pass `03ccaaf5`.
- Commit `2823533` (2026-09-12, "Plans: clear the plan-QA block and the staleness nags with
  accurate statuses") did **not** change any of these seven to a non-`In Progress` status.
