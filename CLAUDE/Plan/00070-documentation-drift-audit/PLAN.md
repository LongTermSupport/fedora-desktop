# Plan 00070: documentation drift audit

**Status**: In Progress
**Created**: 2026-07-31
**Owner**: joseph
**Priority**: Medium

## Overview

A read-only audit of the project's documentation against the tree it describes found **17 confirmed
defects and 6 suspected**. Full report: [reports/docs-drift-audit.md](reports/docs-drift-audit.md).

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

## Tasks

### Phase 1 — Audit

- [x] ✅ **Task 1.1**: Audit `CLAUDE.md`, `CLAUDE/`, `.claude/rules/`, `docs/`, `README.md` and the
  nested `*/CLAUDE.md` files → `reports/docs-drift-audit.md`. 17 confirmed, 6 suspected.
- [x] ✅ **Task 1.2**: Independently re-verify findings before recording them. **11 of 17
  re-verified against the tree**; the rest are marked ○ in the report. Finding 16 was deliberately
  **not** verified — it concerns a credential file, and reading it is not something an audit needs.

### Phase 2 — Fix the four harmful docs

- [ ] ⬜ **Task 2.1**: Replace the `inventory_dir` pattern in all four docs (finding 1).
- [ ] ⬜ **Task 2.2**: Point `helpers/CLAUDE.md:76` at `./scripts/qa-helper-tests.bash` (finding 5).
- [ ] ⬜ **Task 2.3**: Fix the NordVPN play's `set` → `replace`, and reconcile the third workflow in
  `docs/nordvpn-installation.md` (finding 4).
- [ ] ⬜ **Task 2.4**: Reconcile `extensions/CLAUDE.md` with `CLAUDE/QA.md:71` (finding 6).

### Phase 3 — Fix the omissions and staleness

- [ ] ⬜ **Task 3.1**: Add `play-mask-intel-lpmd.yml` to `docs/architecture.md` and
  `docs/playbooks.md` (finding 2); add the four missing optional playbooks (finding 9).
- [ ] ⬜ **Task 3.2**: Index `PlanJournalling.md` and `PlanScriptStandards.md` in `CLAUDE.md`
  (finding 3).
- [ ] ⬜ **Task 3.3**: Correct the two mischaracterised playbooks, and check whether the
  `Enable LXD Copr Repository` task name is itself wrong (finding 10).
- [ ] ⬜ **Task 3.4**: Rewrite `docs/fast-file-manager.md` — delete the non-existent tracker feature,
  document the recently-used-files fix (finding 11).
- [ ] ⬜ **Task 3.5**: Fix the three broken links (findings 13, 17).
- [ ] ⬜ **Task 3.6**: Document the seventh QA gate (finding 8); regenerate the Whisper model table
  (finding 12); add the missing directories to the architecture tree (finding 15); reword the vault
  password file description (finding 16).
- [ ] ⬜ **Task 3.7**: Add a `provisioning_profile` / `scope` section to `docs/architecture.md`
  (finding 14).

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

- [ ] ⬜ **Task 5.1**: Decide whether any of this is mechanically checkable. Candidates: every
  playbook in `playbook-main.yml` appears in `docs/playbooks.md`; every `CLAUDE/*.md` has an index
  row; internal `#anchor` links resolve. **Decide, do not assume** — a link checker that silently
  matches nothing is exactly the failure this repo already has a written rule about
  (`CLAUDE/QA.md:38`).

## Technical Decisions

### Decision 1 — the `@`-import question is the owner's, not this plan's

Finding 7 says `CLAUDE.md`'s 22 `@CLAUDE/*.md` imports contradict the doc-organisation policy stated
in the same file, at a measured cost of ~108,900 bytes inlined into every session.

**This plan does not act on it.** Changing `CLAUDE.md`'s structure changes what every future session
sees, and it was a peer review — not the owner — that raised it. Recorded for the owner's decision.
**Date**: 2026-07-31

## Dependencies

- **Blocks**: nothing.
- **Unrelated, found while running the gate** — recorded in Plan 00068's journal, each needing its
  own plan: `ruff` is unpinned while its gate's strictness is defined by whichever version is
  installed, and `scripts/qa-ansible.bash:70-76` flags a comment that documents the *removal* of
  `ignore_errors: true`.

## Success Criteria

- [x] ✅ Every finding is recorded with a `path:line` citation and a verification marker.
- [ ] ⬜ The four harmful docs no longer teach a command or pattern that fails.
- [ ] ⬜ Every confirmed finding is fixed or declined with a stated reason.
- [ ] ⬜ QA passes (`./scripts/qa-all.bash`) — note it is **already red** at HEAD for two unrelated
  reasons; those must not be conflated with this plan's changes.

## Risks & Mitigations

| Risk                                                      | Impact | Probability | Mitigation                                                            |
| --------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------- |
| A "fix" is written from the doc rather than from the code | H      | M           | Every fix reads the source first — the audit's own method             |
| Fixed docs re-drift on the next code change               | H      | H           | Task 5.1 — mechanical checks, where they can be made to actually fail |
| A finding is wrong and a correct doc gets "fixed"         | M      | L           | 11 of 17 re-verified; the other 6 marked ○ and re-checked before edit |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00070-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Audit report — `reports/docs-drift-audit.md`
