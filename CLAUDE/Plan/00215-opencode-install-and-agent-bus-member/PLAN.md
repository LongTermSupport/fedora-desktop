# Plan 00215: opencode install and agent bus member

**Status**: In Progress (Phase 1 built in ccy, awaiting its host run; Phase 2 designed; Phase 3 waits on Plan 00209)
**Created**: 2026-10-22
**Owner**: joseph
**Priority**: Medium

## Overview

The owner wants opencode, the open-source terminal coding agent
([anomalyco/opencode](https://github.com/anomalyco/opencode), formerly `sst/opencode`), as an
extra team member for GitHub issues, plans and suggestions. That needs two things: opencode
able to run where the owner chooses, and opencode sessions able to join an agent-bus team, be
woken by deliveries and answer.

Phase 1 is the first place it runs, decided by the owner (O2, O3, O7): **inside ccy only**.
The ccy image carries the pinned opencode, and `ccy --opencode` (`-oc`) runs it instead of
Claude Code in an otherwise ordinary session. Nothing is installed at desktop level. Phase 2 is
the bus membership design, written against agent bus v2 (`teamchat`,
[Plan 00209](../00209-agent-bus-v2-in-go/)) because v1's wake path is Claude Code's own and v1 is
being replaced. Phase 3 builds that design, and **every Phase 3 task depends on Plan 00209, not
on v1**.

Facts: [research/opencode-upstream-facts.md](research/opencode-upstream-facts.md) (upstream,
with sources) and [research/agent-bus-member-facts.md](research/agent-bus-member-facts.md)
(the bus, v1 and v2). Design:
[research/opencode-bus-member-design.md](research/opencode-bus-member-design.md).

## Goals

- The ccy base image installs the pinned opencode, checksum-verified at build, and
  `ccy --opencode` changes only the command the container runs: tmux, mounts, tokens, firewall
  and the session record stay as for any launch, and a launch without the flag is unchanged.
- opencode's model provider key stored the way ccy stores its tokens and handed by name to the
  one container run, never exported; no key, a clear failure before anything starts.
- A design that lets an opencode session be a v2 team member with the same HUMAN, PEER and PING
  authority as a Claude Code member, through one member-adapter seam in v2 (O4).

## Non-Goals

- opencode at desktop level, in `cc`, in LXC guests or on localhost: later, once ccy has proven
  the idea (O2). `cc` also waits on hooks-daemon support for opencode.
- Any bus code, on v1 or v2, in this plan's Phases 1-2; an opencode member on v1.
- Moving projects from `CLAUDE.md` to `AGENTS.md` (O6): later, separate work.
- Choosing the model provider for the owner (O1).
- Any new owner-visible `teamchat` name or path: Plan 00209's Gate A is untouched.

## Tasks

### Phase 1: opencode in ccy

- [x] ✅ **Task 1.1**: Research the upstream release assets and their verification, where
  opencode keeps config and credentials, and its non-interactive and server modes
  ([research/opencode-upstream-facts.md](research/opencode-upstream-facts.md)).
- [x] ✅ **Task 1.2**: The ccy base image (`files/var/local/claude-yolo/Dockerfile`, container
  2.79.1) installs opencode 1.19.3 from `opencode-linux-x64.tar.gz`, sha256 from the release
  API's digest (`OPENCODE_VERSION`/`OPENCODE_SHA256`), version asserted at build; managed
  `/etc/opencode/opencode.json` (`share` disabled), `OPENCODE_DISABLE_AUTOUPDATE=1`, and
  `/etc/opencode/tui.json` unbinding opencode's ctrl+z suspend. The desktop play
  `play-opencode.yml`, its pin row and its docs are removed (O7).
- [x] ✅ **Task 1.3**: `ccy --opencode` / `-oc` (CCY 3.100.6), in `lib/agent-choice.bash`:
  the parse's last branch takes the flag; `--teams`, `--supervise`, `--headless` and the
  session limits are refused with it (64), and so is a bare first message (opencode's project
  argument: use `--prompt`); the session runs with no bus kit and no supervisor, a Quick Launch's
  saved team is not taken (and is kept for the next launch), and its record is marked no-restore, which `ccy-sessions restore` and
  ccy-teams' bus discovery also read from the record's `--opencode`/`-oc` (`registry.Opencode`);
  provider keys are files in `~/.claude-tokens/ccy/provider-keys/` named for opencode's variable
  (`<PROVIDER>_API_KEY`, 0600, one line), none by default, a missing or malformed one failing
  (1) before anything is asked; each key is handed by name to the container run only
  (`local -x` around the engine call). Proven by `scripts/test-ccy-agent-choice.bash` and four
  new launcher differential-harness scenarios; every existing scenario's golden is unchanged
  but the three `--help` ones. Documented in `docs/ccy.md` ("opencode").
- [ ] 🔄 **Task 1.4**: Host run: the `play-claude-yolo.yml` entry in
  `CLAUDE/Plan/meta-deploy.list`. Done when the image has rebuilt with opencode, a key file is in
  place, and `ccy --opencode` starts opencode in its tmux session; hand check that ctrl+z in it
  does not freeze the session.

### Phase 2: bus membership design (no code)

- [x] ✅ **Task 2.1**: Read the bus as it is (pingbus skill, `files/` and `src-go/`, the wake
  paths, Plan 00161) and v2 (Plan 00209 on F44 and the DESIGN and SPEC files on
  `feat/bus-v2-n3`) ([research/agent-bus-member-facts.md](research/agent-bus-member-facts.md)).
- [x] ✅ **Task 2.2**: Write the design: join, wake, deliver, reply, authority, the differences
  from a Claude Code member, the adapter seam and its shape, and where opencode runs (section 6:
  O2, O3, O5, inside ccy first, any role)
  ([research/opencode-bus-member-design.md](research/opencode-bus-member-design.md)).
- [x] ✅ **Task 2.3**: Owner decisions O1-O7 answered (below).

### Phase 3: build (depends on Plan 00209, not on v1)

Each task names the Plan 00209 work it waits for.

- [ ] ⬜ **Task 3.1**: Put the seam (design section 5) to Plan 00209 so n3e, the Claude Code
  delivery slice of N3, is built as the first adapter (O4). **Depends on** Plan 00209 Task 3.2
  (n3e) not yet built. If n3e lands first without it, this task becomes carving it out.
- [ ] ⬜ **Task 3.2**: The opencode adapter in v2's member, with a fake opencode server for the
  tests and host checks H-OC1 to H-OC5 (design section 7). **Depends on** Plan 00209 N3
  (Task 3.2) merged.
- [ ] ⬜ **Task 3.3**: The opencode member kind in ccy (O2): v2's kit in the ccy image with the
  opencode adapter, chosen by `--opencode`, which then stops refusing `--teams`; ccy-teams'
  messages that tell an opencode session it was "launched with --no-bus" name `--opencode` too. **Depends on**
  Plan 00209 N4 (Task 4.1, v2's kit in the image) and a CCY version bump.
- [ ] ⬜ **Task 3.4**: Acceptance: one opencode member on a throwaway v2 team answers a HUMAN
  message, a PEER message and a closed-verb ping, and its HUMAN items get read receipts only
  after proof. **Depends on** Tasks 3.2-3.3.

## Owner decisions

Answered by the owner; O1 and O4 are the coordinator's recommendations, recorded as the
defaults the owner accepted.

- **O1 Provider and credentials.** The owner said "you tell me". Coordinator recommendation,
  the default: opencode cannot use the Claude subscription login, so it needs a provider API
  key. Store it with ccy's existing token mechanism (`--export-token`, `--list-tokens` and the
  hand-off pattern GH_TOKEN uses) and pass it by name only to the container run that needs it;
  do not export it to every child. Which provider (Anthropic API, OpenRouter, ...) is the
  owner's billing call: make the key's env var name configurable by provider and default to
  none, failing fast with a clear message when `--opencode` is used and no key is set.
  *As built:* the key lives beside ccy's tokens (`~/.claude-tokens/ccy/provider-keys/<VAR>`)
  and is handed on as GH_TOKEN is; `--list-tokens` and `--export-token` do not show it yet (see
  Open questions).
- **O2 Where it runs.** Do not dictate where opencode runs. `cc` and `ccy` work as normal, and a
  flag `--opencode` (short `-oc`; check that the launcher's argument parser can take a
  two-letter short flag, and if not, propose the nearest form and record why) makes the session
  run opencode instead of claude. **ccy only at first.** `cc` (desktop level), LXC and localhost
  come later, once the idea is proven. `cc` support also waits on hooks-daemon support for
  opencode. *Checked:* ccy's parser matches each argument whole, with no bundling of short
  flags, so `-oc` is taken as one flag and never reaches claude (which would read it as `-o -c`).
- **O3 Worker permissions.** Workers run podman-contained, inside the ccy container.
- **O4 The seam.** Coordinator default, accepted: the v2 bus gets one plug point for "deliver a
  message to this kind of agent", with Claude Code as the first user and opencode the second,
  instead of hard-coding Claude.
- **O5 Role.** No lock-down. Coordinator or worker is the user's choice.
- **O6 Claude Code files.** Do not keep opencode reading CLAUDE.md or .claude/skills. Instead,
  projects move to AGENTS.md. Record this as a later, separate piece of work; do not migrate
  anything now. *Verified fact* ([Claude Code memory docs](https://code.claude.com/docs/en/memory.md#agents-md)):
  from v2.1.277 Claude Code reads `AGENTS.md` natively, but only when no `CLAUDE.md`,
  `CLAUDE.local.md` or `.claude/CLAUDE.md` exists in the working directory or above it; the
  `/config` setting "Project instructions" = `claude-md-and-agents-md` loads both; the
  documented fallback is a `CLAUDE.md` that contains `@AGENTS.md`, with any Claude-specific
  notes below the import. So the later move can be: `AGENTS.md` holds the shared instructions,
  and `CLAUDE.md` becomes `@AGENTS.md` plus the Claude-only parts, which keeps the
  CLAUDE.md-only features (path rules, the hooks daemon block) working. Until that move,
  opencode keeps its default of reading `CLAUDE.md` (setting `OPENCODE_DISABLE_CLAUDE_CODE`
  now would leave an opencode session in this repo with no project rules at all).
- **O7 Managed config.** No opinion. **Do not install opencode at desktop level yet.** The first
  test is podman-contained, in a ccy-type container. (The image keeps the managed config.)

## Open questions

- **Provider keys and the token commands (O1).** `--list-tokens` and `--export-token` list and
  export Claude tokens only; showing provider keys there touches the Go front's answers and
  its differential harness. Worth doing once a key is in use, or not at all?

## Success Criteria

- [ ] The image has rebuilt on the host with opencode 1.19.3, and `ccy --opencode` starts it
  with a provider key (Task 1.4).
- [x] A launch without `--opencode` is byte for byte what it was (the differential harness),
  except a `--no-bus` Quick Launch with a saved team, which no longer fails late at the seats.
- [x] The design covers join, wake, delivery, reply, authority, the differences, the seam and
  where opencode runs, and leaves Gate A untouched.
- [x] The owner has answered O1-O7.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00215-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phase 2 design, and the rework to ccy after the owner's answers, on branch `feat/opencode`.
