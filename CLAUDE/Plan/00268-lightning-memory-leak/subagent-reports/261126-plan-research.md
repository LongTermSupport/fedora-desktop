# Plan 00268 research report (sub-agent, 2026-11-26)

## Files

- `PLAN.md`: filled in, In Progress, six phases (evidence, reproduce, isolate, fix upstream,
  teamchat's side if it is the trigger, verify live and decide the cap); owner decisions D1
  (traced launch through IaC) and D2 (keep the cap).
- `RESEARCH.md`: the incident (unchecked, relayed), Lightning (checked), teamchat's traffic
  (checked in code and the v2 protocol design), hypotheses H1-H7, reproduction design, profiling.
- `laptop-commands.md`: read-only commands for the laptop's host seat, steps 1-9, plus the one
  traced launch that is NOT read-only and goes through IaC.
- `CLAUDE/Plan/README.md`: Active Plans row.

## Key findings

- Upstream: `lightning-matrix/lightning` (Qt 6/QML C++ over matrix-rust-sdk 0.18, GPL-3.0); PRs on
  GitHub, applied by the maintainer. Only memory issue: #98 (4 GB, not reproduced; indexer fixed in
  0.10.7). 0.10.7 (2026-11-21) fixed an idle trim that never ran without input; the laptop's
  version on 25 Nov is unknown.
- The reported rate (about 200 MiB/min, over 200x the quieter instance) cannot come from
  upstream's measured per-message costs (22 MiB/1,000 source rows; 0.5 MB/row delegates before
  the row window) at any rate teamchat's limits allow. Leading hypotheses: H1 a client loop that a
  teamchat pattern starts; H2 per-event work that grows with history (n squared). H3: the idle
  trim is blocked while a thread panel is open or the window is focused, so it amplifies H1/H2.
- teamchat's distinctive load: a thread per answered ping, question threads, `m.read` fan-in per
  human message (one per delivering agent), presence on every seat start or stop, HTML bodies up
  to 16 KiB, mentions on pings. No typing, near-zero edits (retitles only). Plan 00267's new
  `teamchat.receipt` events will add reports-room traffic.

## Overlap checked

Plan 00254 (Lightning default client: launcher, upstream route; this plan does not change its
tasks), Plan 00267 (receipts traffic), Plan 00181 (container memory limits, no overlap).
`find-plan lightning` and `find-plan memory` turned up nothing else.

## Not done

Nothing was run on any host. The traffic probe, load generator and sampler are tasks, not built.
