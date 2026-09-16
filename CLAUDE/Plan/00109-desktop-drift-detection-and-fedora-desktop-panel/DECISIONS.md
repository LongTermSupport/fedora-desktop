# Plan 00109 — Technical Decisions

Extracted from PLAN.md, which is read in full every session and was approaching the
size at which edits are blocked. Nothing here is history: these are the decisions the
code still rests on, and Decision 3 is still open.

## Decision 1: Detection and handoff, never unattended repair

**Context**: The failure mode is a host silently diverging from the repo. The
tempting fix is to auto-run stale plays.
**Options considered**: (A) auto-run stale plays on detection — self-healing, but a
play that prompts, reboots, or enrols a MOK key cannot run unattended, and an
unattended Ansible run on a desktop at login is a way to lose a working machine.
(B) detect, report, offer a one-click assisted fix.
**Decision**: B. The whole incident was survivable; what made it expensive was not
knowing *why*. Information is the deliverable.
**Date**: 2026-09-11

## Decision 2: The ledger is per-play host state, and its failures are recorded not raised

**Context**: every Phase 2 check compares against the ledger, so a silently wrong
ledger makes every check downstream silently wrong.
**Decision**: one record per **play**, append-only JSONL under
`$XDG_STATE_HOME/fedora-desktop/play-ledger/`, written by a callback plugin. Ansible
**swallows exceptions raised inside a callback**, so a write failure leaves a `BROKEN`
sentinel and Phase 2 reports FAIL while it exists — an unfailable hook turned into a
failable check. No backfill.
**Record shape, hook limits, the no-backfill reasoning**:
[DESIGN-play-ledger.md](DESIGN-play-ledger.md) §§1–4.
**Date**: 2026-09-14

## Decision 3 (OPEN — owner's): nothing detects that the detector itself was never run

**Context**: both of this plan's plays are opt-in and deliberately not imported by
`playbook-main.yml`. A host that ran `playbook-main.yml` and never ran them has a
populated ledger, a clean freshness verdict, and **no health surface at all** —
`freshness` is silent about plays never run here by design (Task 1.3), and
`ledger_presence` fires only on an *empty* ledger. That is this plan's own premise
("plays under `optional/` are run by hand, once, and then forgotten") applied to the
plan's own output.
**Options**: (A) leave it — opt-in means opt-in, and a host whose owner never enabled
detection has not lost anything they had; (B) have `ledger_presence`, which already
reads the ledger, report that neither play has a record here; (C) import them from
`playbook-main.yml`, which makes them not opt-in.
**Decision**: NOT TAKEN. Recorded because it was found by review and was nowhere in
this plan or its DESIGN files — the gap being unwritten is the part that was wrong,
independently of which option is right.
