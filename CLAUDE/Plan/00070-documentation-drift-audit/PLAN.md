# Plan 00070: documentation drift audit

**Status**: In Progress
**Created**: 2026-07-31
**Owner**: joseph
**Priority**: Medium

## Overview

A read-only audit of the project's documentation against the tree it describes found **17 confirmed
defects and 6 suspected**. Fixing them surfaced **six more** (findings 18–23), one of them in the
harmful class. Full report: [reports/docs-drift-audit.md](reports/docs-drift-audit.md).

The defect is **drift** — docs describing a past state of the code — not sloppiness. The code and
the newer docs are disciplined; the older docs were never re-read after the changes that invalidated
them.

**Nothing has been fixed.** This plan holds the findings and tracks the remediation.

## Goals

- Every confirmed finding is either fixed or explicitly declined with a stated reason.
- Every suspected finding is resolved to confirmed or dismissed.
- The docs that teach a **wrong** command stop doing so — those actively cause harm, as opposed to
  merely being incomplete.

## Non-Goals

- Rewriting any document wholesale. Each finding has a minimal remedy.
- Auditing plan bodies, vendored trees, or `untracked/`.
- Fixing the two unrelated QA-gate failures found while running the gate (see Dependencies).

## The four that actively cause harm

Ranked above the rest because a reader who follows them ends up worse off than if the doc did not
exist. Incompleteness misleads by omission; these mislead by instruction.

| #   | Doc                                  | What it teaches                          | Consequence                                     |
| --- | ------------------------------------ | ---------------------------------------- | ----------------------------------------------- |
| 1   | 4 docs incl. the copy-paste template | `root_dir: "{{ inventory_dir }}/../../"` | reintroduces a bug the style guide bans by name |
| 5   | `helpers/CLAUDE.md:76`               | `python3 -m unittest discover -s tests`  | runs **0 tests**, prints `OK`, exits 0          |
| 4   | `play-nordvpn-openvpn.yml:125-134`   | `./vault.bash set <var>`                 | always fails — the play just wrote that var     |
| 6   | `extensions/CLAUDE.md`               | `npm run lint`                           | the command `CLAUDE/QA.md:71` forbids by name   |
| 18  | `docs/playbooks.md` WARP entry       | "installs Cloudflare WARP"               | the play **uninstalls** it — found in Phase 3   |

## Tasks

### Phase 1 — Audit

- [x] ✅ **Task 1.1**: Audit `CLAUDE.md`, `CLAUDE/`, `.claude/rules/`, `docs/`, `README.md` and the
  nested `*/CLAUDE.md` files → `reports/docs-drift-audit.md`. 17 confirmed, 6 suspected.
- [x] ✅ **Task 1.2**: Independently re-verify findings before recording them. **11 of 17
  re-verified against the tree**; the rest are marked ○ in the report. Finding 16 was deliberately
  **not** verified — it concerns a credential file, and reading it is not something an audit needs.

### Phase 2 — Fix the four harmful docs

- [x] ✅ **Task 2.1**: `inventory_dir` replaced in all four docs (finding 1). Verified: zero
  occurrences remain anywhere under `docs/`.
- [x] ✅ **Task 2.2**: `helpers/CLAUDE.md` now points at `./scripts/qa-helper-tests.bash`, with an
  explicit callout that `discover` reports `Ran 0 tests … OK` and exits 0 (finding 5).
- [x] ✅ **Task 2.3**: NordVPN play now says `replace`, not `set` (finding 4). `vault.bash:173`
  **already told the user to use `replace`** — only the playbook's own message was wrong.
  `docs/nordvpn-installation.md` reconciled onto the same command, keeping the raw
  `ansible-vault encrypt_string` form as a documented alternative with its downsides named.
- [x] ✅ **Task 2.4**: `extensions/CLAUDE.md` uses the eslint binary at all three sites, matching
  `CLAUDE/QA.md:71` (finding 6). `eslint .` exits 0.

### Phase 3 — Fix the omissions and staleness

- [x] ✅ **Task 3.1**: `play-mask-intel-lpmd.yml` added to both docs (finding 2) — the numbered
  list in `architecture.md` now matches `playbook-main.yml` at all **31** entries. All four missing
  optional playbooks catalogued (finding 9).
- [x] ✅ **Task 3.2**: `PlanJournalling.md` and `PlanScriptStandards.md` indexed in `CLAUDE.md`
  (finding 3). All 15 topic files now have a row.
- [x] ✅ **Task 3.3**: Both mischaracterised playbooks corrected (finding 10). The
  `Enable LXD Copr Repository` task name **was itself wrong** — `lxd` appeared exactly once in the
  whole play, in that name — and is now `Enable LXC Copr Repository` (finding 23). Adding the
  missing catalog entries also exposed a **third** mischaracterisation, finding 18.
- [x] ✅ **Task 3.4**: `docs/fast-file-manager.md` rewritten. Zero `tracker` references remain, and
  `disable_tracker` is gone from the whole repo; the real recently-used-files fix is documented
  with its cause (finding 11).
- [x] ✅ **Task 3.5**: Broken links fixed — **four**, not three. Finding 17 undercounted;
  `installation.md#prerequisites` was also dead (finding 19).
- [x] ✅ **Task 3.6**: Seventh QA gate documented with the reason it sits outside the JSON merge
  (finding 8); Whisper table regenerated from `_whisperModels` — **12** models, not 5, and the
  cache range corrected at both sites (finding 12); `CLAUDE/`, `helpers/`, `files/opt`, `files/usr`
  added to the architecture tree (finding 15); vault password file reworded to **plaintext** with
  the reason (finding 16).
- [x] ✅ **Task 3.7**: `provisioning_profile` / `scope` section added to `docs/architecture.md`,
  including the server-biased detection and the zero-flag default (finding 14).

### Phase 4 — Resolve the suspected six

- [ ] 🔄 **Task 4.1**: Resolve S1–S6 to confirmed or dismissed.
  - [x] ✅ **S3 dismissed** — the vars are real but documented in both places the doc points at
    (`docs/headless-provisioning.md:54-55`, `run.bash:517-518`), and the doc defers explicitly.
  - [x] ✅ **S4 confirmed and escalated** into finding 12 — **every** size in the model table is
    wrong, not one, and `tiny` is out by ~1.9× (40MB documented, ~75MB real), a figure that
    propagates to the quoted cache range at `:77` and `:752`.
  - [x] ✅ **S6 dismissed** — both child docs are linked directly from `docs/README.md:295-296`;
    the intermediate index is redundant.
  - [ ] ⬜ **S1, S2, S5 need the owner's intent, not more grepping.** All three ask the same
    question: was the omission a decision or an oversight? S2 has a real cost either way — a reader
    who copy-pastes the simplified `podman run` loses `--replace`/`--name` collision handling.

### Phase 5 — Stop the drift recurring

- [x] ✅ **Task 5.1**: **Decided: yes, and built.** All three candidates are mechanically
  checkable, and each would have caught a real finding in this audit — that was the bar, not
  "seems useful". Shipped as `scripts/qa-docs.bash`, the seventh JSON-merged gate in
  `qa-all.bash`, backed by the stdlib-only helper `helpers/docs/link_check.py` with **42 unit
  tests**.

  | Check                                                                           | Would have caught  |
  | ------------------------------------------------------------------------------- | ------------------ |
  | link targets exist; `#anchor`s match a real heading                             | 13, 17, 19, 21, 22 |
  | every `playbook-main.yml` import named in both `playbooks.md`/`architecture.md` | 2                  |
  | every `CLAUDE/*.md` has an index row                                            | 3                  |

  The zero-file trap named in the task is handled: `link_check.main` exits **2** on an empty
  scope, on a zero-`import_playbook` parse, and on zero topic files, and the gate refuses to
  translate any of those into a pass.

## Technical Decisions

### Decision 1 — `@`-imports removed, but ONLY because path-triggered rules replace them

**Owner-approved and actioned.** All 22 `@CLAUDE/*.md` imports in `CLAUDE.md` became plain links,
as did the 11 in the nested `*/CLAUDE.md` files — 33 in total, zero remaining.

**The owner attached a condition, and it is the important half of this decision:** removing the
imports is only safe if `.claude/rules/*.md` with `paths:` globs make the same documents load
**on demand**, so that editing Ansible still triggers reading the Ansible rules. Without that, this
change would not have saved context — it would have silently removed guidance.

`.claude/rules/` **did not exist in this repo**; the rules I had previously seen belong to the outer
lts-infra checkout. Seven were created: `ansible-editing`, `bash-scripts`, `ccy-version-bump`,
`python-helpers`, `secrets-and-vault`, `gnome-extensions`, `qa-gates`. Each is a **thin pointer** to
the topic file that owns the fact, never a copy — one source of truth per fact still holds.

**Date**: 2026-07-31

### Decision 2 — the docs gate excludes `CLAUDE/Plan/**` on principle, not for convenience

The sweep found ~60 broken anchors in `CLAUDE/Plan/00049-full-repo-audit/` and 7 in Plan 00068's
journal. Gating on those would have made the new gate red on arrival, so the exclusion needs to
be justified by something other than that it makes the gate green.

It is: **a core gate that sweeps plan content is a core→plan dependency.** Archiving a plan
would then change core CI's verdict without a single core file changing — and plan folders are
explicitly ephemeral. Scoping core gates to core paths is the standing rule; plan markdown is
linted by the plan QA tooling, which is where that belongs.

The excluded findings are **recorded, not discarded** — see the report's "Out of scope" section.
Plan 00068's journal defects must be corrected by a new dated entry, never an edit, because
journals are append-only.

**Date**: 2026-07-31

## Dependencies

- **Blocks**: nothing.
- **Unrelated, found while running the gate** — recorded in Plan 00068's journal, each needing its
  own plan: `ruff` is unpinned while its gate's strictness is defined by whichever version is
  installed, and `scripts/qa-ansible.bash:70-76` flags a comment that documents the *removal* of
  `ignore_errors: true`.

## Success Criteria

- [x] ✅ Every finding is recorded with a `path:line` citation and a verification marker.
- [x] ✅ The **five** harmful docs no longer teach a command or pattern that fails (finding 18
  joined the original four during Phase 3).
- [x] ✅ Every confirmed finding is fixed. 17 original + 6 found while fixing = **23**, all
  closed. The only open items are S1/S2/S5, which turn on the owner's intent, not on evidence.
- [x] ✅ QA passes (`./scripts/qa-all.bash` → rc 0, 488 files). The two unrelated failures noted
  here were fixed under **Plan 00071** before this plan's changes landed, so nothing was
  conflated.
- [x] ✅ The drift is now mechanically detectable, not just fixed — `qa-docs.bash` proven to fail
  `qa-all.bash` on a broken anchor and to pass when restored.

## Risks & Mitigations

| Risk                                                      | Impact | Probability | Mitigation                                                                 |
| --------------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------------------- |
| A "fix" is written from the doc rather than from the code | H      | M           | Every fix reads the source first — the audit's own method                  |
| Fixed docs re-drift on the next code change               | H      | H           | **Closed** — `qa-docs.bash` gates every commit; 10 discriminating controls |
| A finding is wrong and a correct doc gets "fixed"         | M      | L           | 11 of 17 re-verified; the other 6 marked ○ and re-checked before edit      |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00070-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Audit report — `reports/docs-drift-audit.md`
