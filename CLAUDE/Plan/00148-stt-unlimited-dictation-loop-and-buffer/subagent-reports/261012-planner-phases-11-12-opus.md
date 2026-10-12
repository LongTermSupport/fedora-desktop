# Planner report: Phases 11 and 12 (one mode; thin extension over a reloadable service)

Planning only; no code changed. Built on the coordinator's draft commit `6dfe7bf7`.

## What was written

- `RESEARCH-thin-extension.md` (new): section 1 is Phase 11 (the four recorders, every
  caller to remove by file and function, what the owner gives up, migration); section 2
  is Phase 12 (what needs the Shell and what moves, a D-Bus contract sketch, process
  supervision and cgroups, the recorders' environment, settings, Rust against Python,
  Ansible deploy, migration and rollback, tests, risks).
- `PLAN.md`: Phases 11 and 12 rewritten as concrete tasks with the owner decisions as
  open tasks; status line and a Non-Goals line updated. PLAN.md is about 34.6 KB, just
  under the 35 KB edit block, so further detail for these phases goes in the research doc.
- `README.md` row for 00148; a journal finding entry for 26-10-12.

## Owner decisions listed (open tasks)

- 11.1: only server-mode continuous dictation, and the order 11, 12, 10 (recommended yes).
- 11.2: delete batch `wsi` and `faster-whisper-transcribe` (recommended delete).
- 11.3: uninstall RealtimeSTT; also remove PyTorch and the rest if nothing else needs them.
- 11.4: retire the dead `claude-enabled` switch (recommended retire).
- 12.1: the service's language: Rust (recommended; waits for Plan 00174 Phase 2 and
  Plan 00155's crate) or Python with PyGObject.
- 12.2: a service restart during a dictation: it carries on as an unowned recording
  (recommended) or is re-adopted.

## Findings that corrected the draft

- `claude-enabled` is never consulted (dead switch in Settings).
- Server mode never sends PREPARING, so `panelSession.js`'s CAPTURING goes with Phase 11.
- Between a play run and the logout the old extension passes `--server-mode`: keep it
  accepted until Phase 12. Retired keys leave the schema only at Phase 12's logout deploy,
  with a dconf reset.
- "Mic-health" is `wsi-stream`'s `microphone_shortfall`, not extension code; it stays there.
- A service restart would kill recorders it spawned and any warm server they started
  (same cgroup): each recorder goes in its own `systemd-run --user --scope`, to be proven
  by the first integration test.
- Recorders started by a user service get the user manager's environment; a triage leg
  comes before building (Task 12.4).
- The extension's copy loop only adds files: removed files need `state: absent`.
