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

- [ ] ⬜ **Task 5.1**: Decide whether any of this is mechanically checkable. Candidates: every
  playbook in `playbook-main.yml` appears in `docs/playbooks.md`; every `CLAUDE/*.md` has an index
  row; internal `#anchor` links resolve. **Decide, do not assume** — a link checker that silently
  matches nothing is exactly the failure this repo already has a written rule about
  (`CLAUDE/QA.md:38`).

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
