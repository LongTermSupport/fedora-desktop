# Plan 00201 Tasks 1.1 and 1.2: research and spec (opus planner)

- Research: [RESEARCH-current-design.md](../RESEARCH-current-design.md). Spec:
  [SPEC-many-teams.md](../SPEC-many-teams.md). No production code written.
- **Several teams already work** through an explicit `ccy --teams 1@alpha,2@beta`: seats,
  claim, watcher, hooks, `recv`, `status` and the board all handle a list. It is unit-tested
  only; the 00161 host acceptance never ran two teams. The one-team assumptions are the
  bare-`--teams` path (`ccy-teams for-checkout --choose` prints one team or hard-stops) and
  the ccy-teams picker's per-team join offer. No status-line surface shows any team.
- **Trust:** a seat is never scope-checked; a member in an uncovered private checkout can
  join but cannot reference its own commits (`external` refs must be public). Defect found:
  `declare.py teams_for` ignores `exclude`.
- **Descriptions:** an optional team-file key changes the config digest but restarts
  nothing (the installer restarts only on `tuwunel.toml`, drop-in, binary or unit change).
  Reaching agents needs an optional team-record key, a version-1 widening like `exclude`.
- **Replying (a teammate's report):** `--to` has no lookup, so a short `<repo>.<seat>` is refused as
  `target`. The spec adds `REPLY` lines, short-name resolution with ambiguity refusal, and
  help epilogs.
- **cc:** `cc-teams.bash` takes one team, no chooser, silent default seat. The spec puts
  both launchers on one Go flow, `ccy-teams choose-teams --type=podman|host`; cc's Element
  consent is asked inside it. The spec tabulates the differences that stay.
- Owner decisions Q1-Q10 and milestones M1-M7 are in the spec; PLAN.md Task 1.2 waits on
  them.
