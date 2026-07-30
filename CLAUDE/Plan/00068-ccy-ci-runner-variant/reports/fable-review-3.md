# Plan 00068 — Hostile Review, Round 3 (fable)

**Note on timing.** The task briefed this review against commit `1fb9efd`. The plan folder was
live-edited during this session by a concurrent agent: commit `eb68526` ("Plan 00068 D5: I found
the sixth one myself...") landed mid-review, adding a **D5** correction to the ROUND 2 CORRECTIONS
block. This review is against the tree as it stood after `eb68526` (clean working tree, no further
commits observed). D5 is addressed below under "what holds up" — it is independently verified and
its core claim is correct, with one small citation slip noted.

Every `file:line` cited below was re-read directly from source in this session:
`files/var/local/claude-yolo/{claude-yolo,entrypoint.sh,lib/*.bash,Dockerfile*}`,
`playbooks/imports/play-claude-yolo.yml`, `/workspace/tasks/runner-ccy-project-image.yml`,
`/workspace/environment/dc-proxmox/group_vars/all/runner.yml`, `/workspace/docs/RUNNER-VM-DESIGN.md`,
`/workspace/untracked/repos/actions-hub/.github/workflows/ci.yml`, and `git log`/`git diff`/`git show`
against this repo and `/workspace` (lts-infra, which vendors this repo at
`untracked/repos/fedora-desktop` — confirmed by `ls /workspace`).

**Verdict up front:** the loop is **not** quiet. Two BLOCKERs, both structural, both independent of
D1–D5's own content: D1's "provision time, `claude-yolo` invoked" half is unproven and is
contradicted by the plan's own already-delivered Phase 3 design; and the propagation fix that
landed for three tasks (commit `1fb9efd`) was never applied to the fourth task D1 named by number,
reproducing — in this file, about this file — the exact defect that commit's own message describes.

---

## 1. BLOCKER — D1's "provision time: Ansible invokes `claude-yolo`, build-and-exit" is unproven, and Phase 3's own delivered design contradicts it

D1 (`PLAN.md:238–264`) resolves the "who invokes the CI entrypoint" fork with a table:

| Phase          | Who invokes                                | `claude-yolo` involved?  |
| -------------- | ------------------------------------------ | ------------------------ |
| Provision time | Ansible, to build the project image        | **yes — build-and-exit** |
| Job time       | the caller's own `podman run --entrypoint` | no                       |

The job-time row is well-supported (R3, restatement §1.1, matches the consumer's and lts-infra's
own patterns). **The provision-time row is not**, and the plan's own already-"delivered" work
contradicts it in two independent ways.

**(a) Task 3.4's own specification never invokes `claude-yolo` at provision time.**
`phase3-image-layering.md` Task 3.4 states the build shape "mirroring what already works for
`claude-yolo:latest` (`play-claude-yolo.yml:338-343`)" for all three builds (latest, `ci`, and the
project image). I read that citation directly:

```
files/var/local/claude-yolo/../../playbooks/imports/play-claude-yolo.yml:338-343
  ansible.builtin.command: >
    {{ container_engine }} build
    --build-arg DOCKERFILE_HASH={{ dockerfile_hash.stdout }}
    -t claude-yolo:latest
    /opt/claude-yolo
```

This is Ansible's `command` module calling the container engine **directly**. There is no
invocation of the `claude-yolo` launcher script anywhere in that task, today. `tasks/runner-ccy- project-image.yml` (in `/workspace`/lts-infra — the exact duplicate R10 says must be deletable)
does the same thing for the project image: `runuser -u {{ runner_user }} -- podman build --label=lts.ccy.dockerfile-sha256=… --tag=… …` (`:172-195`) — again, never calling `ccy`. Task 3.4
explicitly says it *mirrors* this existing, already-working, `claude-yolo`-free technique — it does
not say Ansible instead calls a new `ccy --build-and-exit` (or similarly named) flag. So nowhere in
the plan's actual, already-written Phase 3 design does anything invoke the capability D1's table
claims runs at provision time.

**(b) Even if such a flag existed, the source shows it cannot skip credential resolution without a
redesign this plan never specifies.** I read `claude-yolo` linearly from the token block through the
build block: credential/token resolution (`select_token`/`create_token`, roughly `:900-1150`,
including the API `validate_token` round-trip at `:1057`) executes **unconditionally**, before *any*
build or rebuild logic — the first-time build check (`:1424`), the version-gate rebuild
(`:1436`), the project-image staleness rebuild (`:1487-1529`), and even the *existing*
`--rebuild`/`FORCE_REBUILD` handling (`:1378`) are all later in the script than token resolution.
Nothing in the codebase today branches around the token block. `round2-restatement.md:262-264`
asserts the future build-and-exit mode runs "without launching a session, without prompting, and
**without the 46 prompt sites of Task 7.3's classification on the path**" — that claim is not
demonstrated anywhere in this design-only plan and is contradicted by the current code order; no
document says how a new flag would bypass ~16 credential-related prompt sites that currently sit
upstream of every build path in the script.

**Why this matters.** R10 ("three concrete gaps block lts-infra deleting its duplicate": a
non-interactive build-and-exit mode; an image-`LABEL` build identity; a way to run a command without
the desktop entrypoint) is the stated justification for keeping Phase 2's full 46-site apparatus
CI-relevant, and Task 7.4 item 1 (token-by-value, C5) is called "**the prerequisite**, not one item
of six... before anything else this plan designs is reached" specifically because credential
resolution blocks "the default path" (`task74-capabilities.md:31-36`, `hardware-proof-checklist.md`
item E2: "Blocks every unattended item in B"). But if Task 3.4's *actual* design uses direct
Ansible-driven builds (as it explicitly says it does, and as both cited plays already do), then R10
item 1 is never exercised, items 2 (image `LABEL`, delivered via Task 3.3 Option C) and 3 (CI
entrypoint, Decision 6) appear sufficient on their own to satisfy lts-infra's stated blockers, and
`claude-yolo` is **never invoked anywhere on the CI path at all** — contradicting D1's table outright.
If that reading is right, Phase 2's elaborate CI-motivated framing (the 46-site census, the
spin/abort call-graph analysis, Task 7.5's reordering to put token-by-value ahead of Phase 2) has,
post-D1, lost its CI justification and quietly become a pure desktop-hardening exercise — a fact
none of the tracked Goals or Success Criteria re-examine.

**Grep confirms "build-and-exit" is asserted, never specified.** Across every report and PLAN.md,
the phrase appears exactly three times (`fable-review-2.md`, `round2-restatement.md:262`,
`PLAN.md`'s D1/R10 text) and **zero** times as an actual task deliverable with a flag name, an entry
point, or a design in Phase 2 or Phase 3. It is cited as a requirement repeatedly and designed
nowhere.

**What the plan must change.** Either (i) show, with a citation, where Ansible is meant to invoke
`claude-yolo` at provision time and specify the new mode (flag name, what it skips, how it avoids
credential resolution) — reconciling this with Task 3.4's "mirrors the existing direct build"
wording, which must then be corrected; or (ii) accept that provision-time building never touches
`claude-yolo`, retract D1's "yes — build-and-exit" cell, and go back through Phase 2/Task 7.4 to
state plainly that the CI use case no longer motivates any of it — `--non-interactive` and
token-by-value would then be exclusively desktop-scoped capabilities.

---

## 2. BLOCKER — the D1 propagation commit missed the one task D1 named by number, and the omission repeats the commit's own stated defect

Commit `1fb9efd` ("propagate D1/D3 to the ticked tasks they rescoped") added pointer notes to
Tasks 4.1, 5.1 and 5.3 specifically because, per its own message: *"A reader scanning to Task 5.1
would see 'DONE' and no hint that its deliverable does not apply to the CI path at all."*

D1's own text (`PLAN.md:260-261`) names a fourth location by number: **"Task 7.4's C7 / C8 / C10
are session-launch defects a CI job never reaches... they are no longer CI-motivated."** That
propagation never happened. Verified on the current tree:

- `PLAN.md:1084-1151` (Task 7.4) carries **no** pointer note. Its C8 sub-item heading still reads,
  verbatim, **"`--no-network` mandatory for CI (C8)"** (`PLAN.md:1115`) — a claim D1 retracts one
  screen above it in the same file.
- `PLAN.md:604-668` (Phase 2 / Tasks 2.1-2.3) also carries no pointer note, despite D1's explicit
  "Phase 2 keeps a narrower justification... the plan should stop implying the larger one"
  (`PLAN.md:262-264`). `phase2-non-interactive.md` treats the full 46-site apparatus as serving
  "an unattended launch" generically, with no split between the (now CI-relevant, per D1)
  provision-time path and the (now desktop-only, per D1) session-launch entry points
  (`show_zombie_container_tui`, `check_project_containers_startup`, `_do_compose_start`).
- `task74-capabilities.md` §3's own section title is still **"`--no-network` for CI (C8)"** — the
  underlying report is expected to stay frozen per this plan's established method (Task 6.3), so
  this one is not itself a defect, but it means the *only* place this correction could reasonably
  land — the PLAN.md task list — is exactly where it is missing.

This is precisely the brief's first-priority check ("did the corrections actually land, or were
they absorbed as prose") and precisely the recurring failure mode this plan keeps naming: a true
statement (D1's own text) that never propagated to the place a reader would actually encounter the
stale claim, so scanning Task 7.4 in isolation gives the wrong answer about what is CI-motivated.

**What the plan must change.** Add the same style of pointer note used for 4.1/5.1/5.3 to Task 7.4
(at minimum its C7, C8 and C10 sub-bullets) and to Phase 2's intro, stating plainly that C7/C8/C10
are now desktop-only defects, and that Phase 2's CI relevance is limited to whatever Finding 1 above
resolves it to (possibly none).

---

## 3. MINOR — D5's own citation is off by ~20 lines

D5 (`PLAN.md:308-331`, landed at commit `eb68526` during this review) cites
`.github/workflows/ci.yml:77` and `:79` for the sha256-vs-`LABEL` comparison. Reading
`/workspace/untracked/repos/actions-hub/.github/workflows/ci.yml` directly: the `sha256sum .claude/ccy/Dockerfile` computation is at **line 97**, and the `Config.Labels "lts.ccy.dockerfile-sha256"` read is at **line 99**. The mechanism D5 describes is accurate — only
the line numbers are wrong, by exactly the margin you'd expect from counting from a slightly
different anchor point in the same file. Not load-bearing: D5's substantive claim (the check reads
"a checkout plus the image", never host-local cache state) holds regardless of the exact line
numbers. Worth a one-line fix given the document's subject is citation discipline.

---

## What holds up

Independently re-verified in this session:

- **D5's core claim is correct.** `runner_user: "runner"` (`environment/dc-proxmox/group_vars/all/ runner.yml:114`), and `tasks/runner-ccy-project-image.yml` runs its `podman build` via
  `runuser -u "{{ runner_user }}" --` (`:90-97`, `:172-194`) — the same identity that later runs CI
  jobs (`RUNNER-VM-DESIGN.md:641`, `runuser -u "${RUNNER_USER}" -- ... run.sh`). D5's retraction of
  the "two different users" premise is correct, and its replacement argument (state outside the
  image cannot travel with the image or be read from a checkout) is sound on its own terms,
  independent of the citation slip in Finding 3.
- **The Success Criteria's 🚫 NOT MET item is accurate, not over- or under-stated.** Verified
  `git show -s --format='%s' 73396b3` begins `Plan 00066:` (this plan's number pre-renumber), and
  `git diff --name-only F44...HEAD -- files/ playbooks/` is genuinely empty. The self-assessment
  (wording violated, intent met, both stated plainly) is exactly right — no finding here.
- **Tasks 4.1, 5.1 and 5.3's pointer notes are present, accurate, and correctly describe the D1/D3
  rescoping** — this is the one place the propagation commit did what it set out to do.
- **The job-time half of D1** (caller invokes the container directly via its own
  `podman run --entrypoint`, no `claude-yolo` on the argv) is well-supported and I found no
  contradiction of it anywhere in the plan or the source.
- **Task 3.4's "arm → build → drop" egress-window description of `runner-ccy-project-image.yml`
  is an accurate paraphrase** of the real task (template squid.conf with build ACLs on, restart,
  build, `always:` drop) — it is simply describing a mechanism that does not invoke `claude-yolo`,
  which is Finding 1's point, not an inaccuracy in the paraphrase itself.
- **`claude-yolo`'s script order** (credential resolution unconditionally before any build/rebuild
  logic, including the existing `--rebuild` flag) was independently re-read from source and is
  correctly characterised where the plan does describe it (Task 7.4 item 1, hardware-proof-checklist
  E2) — the gap is that this fact was never reconciled against the "build-and-exit... without the 46
  prompt sites... on the path" claim elsewhere.

---

**MATERIAL FINDINGS: yes.**
