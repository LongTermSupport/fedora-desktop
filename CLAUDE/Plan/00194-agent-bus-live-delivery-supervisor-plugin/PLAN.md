# Plan 00194: Agent bus messages delivered live into a ccy session

**Status**: In Progress
**Created**: 2026-10-14
**Owner**: joseph
**Priority**: High

## Overview

A ccy session launched into a team (`ccy --teams`) is told its seat at session start by the
pingbus Claude Code plugin, and `pingbus watch`, started detached by that plugin, is meant to
wake an idle session when a ping or a team message arrives. What reaches the agent is a
count ("agent-bus: N pending"), by design (Plan 00161: hook output carries fixed templates
and counts only); the agent then has to run `pingbus` to read the messages.

The owner wants messages to arrive in the session in real time, with the agent doing
nothing: something running beside the session delivers them. The hooks daemon's ccy
supervisor already takes ccy-specific plugins (`ccy_lifecycle.py`, shipped in the image) and
injects at idle points, which is what that mechanism was built for. This plan designs and
builds a ccy supervisor plugin that delivers the team's traffic into the session as it
arrives, without weakening the bus's trust model: a human's message in the room is an
instruction, another agent's text is a colleague's note to weigh, never one to obey.

## Goals

- A team message or ping reaches a ccy session within seconds, busy or idle, as text the
  agent reads without running anything. The supervisor types it at once and Claude Code
  queues it during a turn; the one wait is a half-typed line in the input box.
- Human text and agent text stay distinguishable to the receiving agent, as the protocol
  requires; nothing an agent sends gains the authority of the human at the keyboard.
- One delivery path, fail fast: every kind is typed by the supervisor in one envelope. The
  Claude Code inbox socket is not used, and a `--teams` launch without a relay-capable
  supervisor refuses to start. The supervisor plugin owns `pingbus watch`.
- Harness-agnostic team: the bus (room, `pingbus` CLI, kinds, envelope) is defined by
  pingbus; delivery into a session is a thin per-harness adapter.

## Non-Goals

- cc sessions on the host (not supervised by the ccy supervisor); they keep the current wake.
- Changing the bus protocol's message types.

## Tasks

### Phase 1: Design

- [x] ✅ **Task 1.1**: Establish what works today on a real ccy team session (seat told at
  start; does `pingbus watch` run and wake an idle session; how a human's Element message
  reaches the agent) and write `DESIGN.md`: the supervisor plugin API (as the installed
  hooks daemon defines it), how the plugin gets the session's messages (the inbox, the
  watcher, or the bundle directly), what it injects and how it is framed per sender kind,
  rate and size limits, the failure modes, and how it is tested without a live homeserver.
  Done: [DESIGN.md](DESIGN.md), with the owner's decisions of 2026-10-14 recorded in its
  section 0. It needs hooks-daemon plugin API 1.1 (API 1.0 cannot type text, and types only
  at idle); decision G is filed as
  [claude-code-hooks-daemon#119](https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues/119).
- [ ] ⬜ **Task 1.2**: The owner agrees the design.
  The owner gave decisions A-G; the formal tick awaits the owner. The relay code (`relay.py`,
  `sentlog.py`, the relay `PostToolUse` hook) already ships in image 2.56 and is inert until a
  launch sets `PINGBUS_DELIVERY=relay`.

### Phase 2: Build

- [x] ✅ **Task 2.0**: The pingbus side of relay delivery, which needs no hooks-daemon API,
  test first. `PINGBUS_DELIVERY=relay` (set by the launch only) switches `pingbus watch` to
  spool each re-fetched, re-validated item as a line for the plugin (`relay/spool/…`),
  consuming it only on the plugin's confirmation (`relay/relayed/…`), withdrawn when `recv`
  reads it first, with no socket write and `relay=stalled` after 120 s; `pingbus status`
  shows `relay=on|stalled|off`; `send` and `say` log to `sent.jsonl`; the hooks echo it as
  the `SENT` `systemMessage` (`hook post-tool`, and the Stop drain), count only unspooled
  items, do not block Stop on pending items and spawn no watcher; unset, nothing changes.
  The spool line format is DESIGN.md section 6. Modules `relay.py` and `sentlog.py` under
  `helpers/pingbus/`. Not yet done, by design, because they land with Task 2.1: the plugin's
  `settings.json` `crossSessionInbound`, the entrypoint, the launcher, the skill's envelope
  text. Integrated with Task 2.0b on branch `feat/agent-bus-presence`, which carries the
  one container bump (2.56, CCY 3.98.0) for both; in relay mode the goodbye is still sent
  and is not entered in the sent log or spool (tests `GoodbyeRelayTest` and
  `SentLogTest.test_the_goodbye_in_relay_mode_is_posted_and_is_not_a_delivery_to_self`).
- [x] ✅ **Task 2.0b**: Goodbye on session end. The owner: "when shutting down a ccy
  session, there should be a goodbye message sent as final message from the agent." Sent by
  pingbus from the plugin's `SessionEnd` hook, as a fixed-template plain room text from the
  seat's member (`<handle> left the team: session ended (<reason>)`, reason from the closed
  set `exit`, `logout`, `other`), not written by the model. Once per session, never on
  `/clear` or `/resume`, capped at 4 s, a failure is one stderr line. Test first. Docs:
  `docs/agent-bus-protocol.md`, `docs/agent-bus.md`. Rationale: JOURNAL of the day.
  Integrated with Task 2.0 on `feat/agent-bus-presence` (plugin 1.1.0, container 2.56). Host
  run for the image: `play-claude-yolo.yml` (meta-deploy note, queued by the coordinator at
  merge); the goodbye reaches ccy sessions only after the image rebuild.
- [ ] 🔄 **Task 2.0c**: Inject the message itself through today's inbox socket (the owner's
  decision of 2026-10-15, journal 17:10), until Task 2.1 can replace it: a nonce-marked
  envelope with kind and sender from pingbus, the body as escaped data, caps, delivered once,
  attested by the UserPromptSubmit hook. Spec: [SPEC-inject.md](SPEC-inject.md), awaiting
  review before the build. Branch `feat/bus-inject-message`.
- [ ] ⬜ **Task 2.1**: The plugin, test first, shipped in the image beside `ccy_lifecycle.py`,
  enabled for a session launched with `--teams` (CCY version bump). Waits on the
  hooks-daemon release that carries plugin API 1.1 (#119); it also drops
  `crossSessionInbound`, wires the entrypoint (`PINGBUS_DELIVERY=relay`, the refusal without
  a relay-capable supervisor) and updates the skill.
- [ ] ⬜ **Task 2.1b**: Ship the ccy-agent-bus supervisor plugin in the member kit too, so
  LXC and other supervised members get the same delivery.
- [ ] ⬜ **Task 2.2**: Fable review; qa-reviewer; meta-deploy; the owner's test from Element.

## Success Criteria

- [ ] On the host, a message the owner posts in Element mentioning a seat appears in that
  ccy session within seconds, with the agent running nothing, framed as the human's.
- [ ] Another agent's message appears in the same envelope with its own kind and no
  authority.
- [ ] Host: `ccy stop` on a running `--teams` session sends the goodbye (to the team's
  humans and coordinator, not to the other agents). SIGTERM reaches Claude Code's SessionEnd
  in 2.1.299, but whether the supervisor dies fast enough inside the container's stop grace
  is only known on the host. If it does not, set `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS`
  in the entrypoint.
- [ ] A message sent to a busy session is typed mid-turn and run when the turn ends.

## Delivery & Milestones

- Design: decisions recorded in [DESIGN.md](DESIGN.md); the API 1.1 issue is filed (#119)
  and the final agreement (Task 1.2) awaits the owner.
- The pingbus side (Task 2.0) is built; the plugin (Task 2.1) waits on the upstream release.
