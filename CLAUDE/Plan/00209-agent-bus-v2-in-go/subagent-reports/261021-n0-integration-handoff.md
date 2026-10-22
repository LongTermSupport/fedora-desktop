# Plan 00209 N0 integration: handoff

Branch `feat/bus-v2-n0`, worktree `untracked/worktrees/bus-v2-n0`. The integrator stopped at its
context budget after tasks 1 and 3. A fresh agent continues from here, with the same rules as the
original brief (worktree only, no push, no qa-all, no Ansible, qa-changed before each commit,
Edit/Write only, journal only through `mkplan.bash --journal`).

## Done

- **Task 1, DESIGN-v2.md** (commits f183e567, 1f1452d3). The four area files were edited at their
  seams; `DESIGN-v2.md` holds the directions, the shape, the seam contract (56 rows, section 3),
  the names (section 4, pointing at D 3.2, which is now the full table), 17 contradictions settled
  (section 5), the resident cost (6), the owner decisions (7) and R1-R50 traced (8). Section 9 lists
  what the integrated review must check.
- **Task 3, DESIGN.md** (db63e0b5): the supersession note at the top. The body is unchanged.

Scratch inputs, all in `untracked/scratch/` of the worktree: `n0-digest-{A,B,C,D}.md` (area
digests, taken before the edits), `n0-edit-common.md` and `n0-edit-{A,B,C,D}.md` (the edit
instructions applied), `n0-trace.md` (the traceability table).

## Left

1. **Task 2, PLAN.md rewrite.** Status; the owner's directions (copy DESIGN-v2 section 1's three
   quotes); the plan of record (DESIGN-v2.md); milestones N0-N6 as checkbox tasks:
   - N0: area designs A-D, the integration, DESIGN-v2.md: done. The **N0 review** is open: it must
     check what DESIGN-v2 section 9 lists.
   - N1 onward from the design. LXC members come first in N3. The matrix client and fakehs are built
     and reviewed on `feat/bus-v2-m1-matrix` (58c7b6e1); it merges with its first caller (N2 or N3),
     not alone, and must be rebuilt off 35acceab first. Stage 1 for v1 ccy-teams is in progress on
     `feat/bus-v2-s1-declare`.
   - Put the "Later" seams of DESIGN-v2 section 3 into their milestones: room specs exported by
     `teamchat/proto` (N2), the service-account localpart HC-A6 (N2), `InaccessiblePaths=` in v2
     units checked by D's gate (N2), the LXC container's own config play (N3), the launcher's mounts
     and the refusal of a checkout containing the seat store (N4), the launcher removing the session
     record before release (N4), the system-resolving launcher and `ccy-teams migrate` (N4, N6),
     host `cc` members on v2 (N5). The send-level raise verb waits for the first feature (YAGNI).
   - From the build session's notes: in the N3 spec, say that a dropped `/sync` may be resent by
     net/http and that this is safe because a sync with a since token is idempotent; add the M44
     real-Tuwunel test and the unrecorded locked/deactivated answers to N2's acceptance (host or VM
     lab); GO-2026-6809 needs the Go toolchain bump (with the coordinator); 00213's status-line golden
     is exact Python stdout and is loosened to the contract when v2 takes the status line over;
     measure stage-1 open time against F44 b18e91f5; the branches holding 35acceab (m1-proto\*,
     m1-state\*, m1-matrix, m2-hooks\*, s1-declare) are not pushed or merged until rebuilt onto
     e918261b (s1-declare: new branch there, merge F44 b18e91f5, cherry-pick ce87e241, d342732b and
     later, skipping the f460abb7 merge).
   - The decisions table points to DESIGN-v2 section 7, and records TID as defaulted (the
     coordinator puts it to the owner in its next digest).
   - "Requirements carried from other plans": 00201 Task 2.16 (rename a team; answered by the stable
     team id, C44, home N4); Plan 00211's behaviour (reports, questions, threads, `ask`; A 10, B6);
     00194 2.0e and 2.0g (read them in Plan 00194's PLAN.md for their titles and map them to B's
     delivery requirements B18-B24).
   - Success criteria; delivery and milestones.
   - Remove every contract-era statement ("only the compatibility contract binds", "Owner direction:
     v1 is a prototype" framing that implies a contract) from PLAN.md, from DESIGN.md's intro (and
     section 11, per the build notes; the note at the top must stay, the body otherwise unchanged
     except those statements), and from BUILD-BRIEF.md. BUILD-BRIEF.md is the coordinator's: edit
     only wording the clean break makes false and list each edit in the report.
2. **Task 4.** Check `src-go/CLAUDE.md`'s "How a port is built" text (changed in 7e48e255) reads
   correctly as general guidance; probably keep it, with oracles optional; fix contract-era wording.
3. **Task 5, journal entries** (`CLAUDE/Plan/mkplan.bash --journal 00209 <category> <body-file> --title "..."`, body first in `untracked/scratch/`): owner direction 2 and its clarification (the
   separate system); the re-plan to N0-N6; the D4 and D15 answers (both accepted by the
   coordinator: D4 no relay mode, specified on v2 when the plugin API ships; D15 a keeper and a
   worker in every member); the team-id decision (TID, defaulted, coordinator agreed); the N0
   integration itself (DESIGN-v2 sections 3 and 5).
4. **The report** at `subagent-reports/261021-n0-integration.md`: the tip; the GAPs fixed (DESIGN-v2
   section 3 "Fixed" rows); the contradictions resolved (section 5); the decisions for the
   coordinator (section 7 flags: TID, OD-A7, DC1, DC4, DC5, DC15, DC16, DC17, OD-D1, OD-D5, OD-D6,
   BD15, INT3); the BUILD-BRIEF edits; anything left undone.
5. Final message to the lead under 1200 characters, as the original brief asks.
