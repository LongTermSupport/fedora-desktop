# Plan 00161 design review: scope and order

Lens: is DESIGN.md + PROTOCOL.md the smallest design that meets issue #59's hard rules;
what can be cut or deferred; is the build order right; are units one agent each; what does
the issue require that no unit builds or tests. Inputs: the issue text, PLAN.md, DESIGN.md,
PROTOCOL.md, and the six `261006-research-*` reports. Nothing was edited.

Counts: 2 blocker, 9 should-fix, 8 nit.

## Blockers

### B1. The first real ping between two sessions happens at U23, the last unit

- **Problem.** Every Matrix-facing unit (U07, U08, U09, U13, U15) is built and "fully
  verified" against `fake_homeserver.py`, a fake written from the research report, not from
  observed Tuwunel behaviour. Tuwunel is first run in U16 (wave 7 of 10), and two members
  first exchange a ping in U23. U16 is also gated on U15 (the warden) and U11 (the full
  zipapp), so the homeserver cannot even be deployed until almost everything is written.
  The research flags several facts the fake will encode as assumptions:
  `PUT /_synapse/admin/v2/users` uses `users.create`, not `full_register` ("to verify:
  is the default push-rules or profile setup skipped" [tuwunel §3]); room version 12
  creators and `additional_creators` with the power-level override; what stripped state an
  invite carries (the worker's creator check depends on it); `client_sync_timeout_min = 0`
  (H4); the 429 body shape. H1–H5 probe podman and Flatpak only; none probes the
  Matrix flows.
- **Scenario.** Tuwunel's stripped invite state omits the `m.room.create` sender, or v12
  rejects the override because a creator appears in `users`. U08's creator check, U09's
  `room create`, U13's member creation and U15's warden all pass against the fake, and all
  fail on the host in U16/U23. Every one of those branches is reopened at the end, after
  the units that depend on them have merged.
- **Fix.** Make a walking skeleton the first milestone and record the fake from reality:
  1. Extend U00 with a host probe H6 that starts a throwaway Tuwunel (as H4 already does)
     and runs, with `curl`, the exact calls the design uses: shared-secret register,
     `PUT v2/users`, `users/{id}/login`, `createRoom` with the v12 override and initial
     state, invite (capture the stripped state the invitee sees), join, send of the custom
     ping type, `/sync` with filter and `timeout=0`, a limited sync + `/messages`. Save the
     response bodies (tokens and IDs scrubbed) as fixtures under `tests/helpers/pingbus/`;
     U07's fake replays them. U07 then `Needs: U00`.
  2. Insert milestone **M1 "host-to-host ping"** right after U09-core and U13: a minimal
     play (Quadlet + network + bootstrap, no warden, no desktop) and a host-run script
     that adds two `--type host` members, creates a room, sends `review`, receives it with
     `wait`, answers `ack`. Send- and receive-side validation and the forge check are in
     M1 (hard rules); limits beyond the duplicate check, the warden, `tail`/`peers`, the
     desktop play and the plugin follow.
  3. Milestone **M2 "ccy-to-ccy ping"** (U18 + U19 + U17) next, then M3 warden, then M4
     desktop and acceptance. Split U16 so its homeserver part does not need U15
     (`agent-team-warden@` enabled by the warden unit's own task, or in a later
     play edit).

### B2. The Stop-hook guard rests on a mechanism nobody has probed, and it is found out last

- **Problem.** Issue §6 requires a `Stop` hook. The design delivers it only as a Claude Code
  plugin copied into `/root/.claude/plugins/` outside any marketplace (D13). The wake
  research marks two things "to verify": that `hooks/hooks.json` of such a plugin is
  loaded at all, and that `hook_registration_checker` does not flag it. DESIGN §11 puts
  this in the host column, but no H-probe covers it and no unit owns it before U23. There
  is no named fallback.
- **Scenario.** Claude Code loads the plugin's skill and LSP (the phpantom-lsp precedent
  proves only that) but not its hooks without a marketplace entry. U17's contract test
  passes (it checks JSON shape), U19 ships it, and U23 shows the session goes idle with
  pings pending. The wake design and the ccy entrypoint both need rework after the version
  bumps have shipped.
- **Fix.** Add probe H7 to U00: in a throwaway ccy session on the host, install a
  one-hook test plugin the way U19 will (copy + `enabledPlugins` merge) and record whether
  its `Stop` and `UserPromptSubmit` hooks fire and whether the daemon's checker reports
  it. Name the fallback in DESIGN §6 now (the research's third route: entrypoint `jq`-merge
  of user-level hooks into `/root/.claude/settings.json`, removed on opt-out), and make
  U17 `Needs: U00`.

## Should-fix

### S1. Units too large for one agent

- **Problem.** U09 is nine commands (`send`, `recv`, `wait`, `inbox`, `show`, `status`,
  `peers`, `tail`, `room create|join|leave|list`) plus one test per exit code. U16 is the
  helper deploy, the zipapp build and publish, `tcpdump`, per-team dirs, secrets, two
  Quadlets, readiness, the `ss` assertion, bootstrap, the warden enable, drift pairs and a
  version pin. U18 is flag parsing and persistence, slot paths, calling `add-member`,
  SELinux staging, a second network with an H1-dependent fallback, exclusion from five
  network code paths, docker refusal and a version bump, all in a 3000+ line launcher.
  U07 is both the fake homeserver and the Matrix client.
- **Scenario.** An agent runs out of context mid-unit, or delivers U09 with `send/wait`
  tested and `peers/tail` thin; review cannot tell which.
- **Fix.** Split: U09a `send`/`recv`/`wait` (the M1 path); U09b `room *`; U09c the human
  report views. U16a helpers + zipapp deploy; U16b per-team homeserver; warden enable with
  U15. U18a host opt-in, slot and credential; U18b second network. U07a fake (from H6
  fixtures); U07b client.

### S2. U18 and U19 each bump ccy, and both touch `files/var/local/claude-yolo/`

- **Problem.** U18 changes `claude-yolo`/`lib/*` and bumps `CCY_VERSION`; U19 changes
  `Dockerfile`/`entrypoint.sh` (same tree, so the bump rule fires again) and bumps the
  container version. Two branches, two releases, two changelog entries for one feature;
  a release with U18 alone ships `--team` against an image with no `pingbus`.
- **Scenario.** U18 merges first; a user runs `ccy --team x`, gets a mount and a network
  but no CLI or plugin in the container.
- **Fix.** Either one unit for both, or U19 on top of U18's branch with a single combined
  bump and changelog entry, merged together.

### S3. Room creation has no defined credential path, and sits in the wrong tool

- **Problem.** `pingbus room create --as-human NAME` runs "on the host with a human
  credential", but pingbus's only config is `member.json` (PROTOCOL §11, unknown keys are an
  error), which has no human token. The human tokens live in agent-team's tree
  (`~/.config/agent-teams/<team>/humans/<name>.token`). Nothing specifies how pingbus finds
  them, and a host-only, human-credential command is shipped inside the member zipapp
  that goes into every container.
- **Scenario.** U09's author either invents a lookup into agent-team's directory
  (coupling the member CLI to the host layout) or leaves `room create` unusable; M1/U23
  cannot create a room.
- **Fix.** Move it to `agent-team room create <team> --as-human <name> ...` (host-only,
  already reads the agent-team tree and `team.json`), reusing `helpers.pingbus.matrix`.
  pingbus keeps `room join|leave|list`. This also shrinks the member surface.

### S4. A member's allowlists and humans are frozen at its first launch

- **Problem.** `member.json` carries `repos`, `path_prefixes`, `humans`, `warden`,
  `forge_api` and `limits`, and DESIGN §5 says the credential is "created once ... and
  reused for ever after". No unit regenerates `member.json` when `team.json` changes.
- **Scenario.** The owner adds a third repository to a team's `repos` and re-runs the
  play. Every existing ccy slot refuses pings referencing it (`allowlist`, exit 4 on send,
  drop on receive) until someone deletes the slot credential, which allocates a new `<n>`
  and breaks the "resumed session keeps its number" rule.
- **Fix.** Keep only the token (and handle) persistent per slot; have the launcher (U18)
  render `member.json` from the current `team.json` on every launch. Add a test that a
  changed `team.json` reaches the next launch.

### S5. The acceptance run has no fixture team and leaves permanent accounts behind

- **Problem.** P1, P4 and P5 need a scripted session (bootstrap, room create, join, 20
  pings, ack, warden command), and P1 needs real refs that resolve at the forge. No unit
  defines which team, which members, which room and which forge refs it uses. Registry
  entries are "never deleted" and `<n>` is never reused.
- **Scenario.** Each `acceptance.bash` run adds members to a real team, burning counters
  and leaving deactivated accounts and rooms in the team's history; or it fails because
  the owner's team allowlists do not contain the ref the script uses.
- **Fix.** U22 declares a dedicated acceptance team (its own `agent_teams` entry with a
  reserved name, created and torn down by `deploy.bash`/`acceptance.bash` through the
  play), with `--type host` members, and a fixed public ref in this repository on its
  allowlist. State the forge token source for the host run.

### S6. The issue's privacy check names a firewall-zone check the design does not do

- **Problem.** Issue: "Check IP forwarding and the firewall zone of every bridge it is
  published on." P2 records `ip_forward` and checks the bind, but checks no firewall zone
  and not `route_localnet` (with it set, a `127.0.0.1` bind can be routed to from outside).
- **Scenario.** A host has `net.ipv4.conf.all.route_localnet=1` from another tool; P2 passes
  on `ss` output while the port is reachable.
- **Fix.** P2 adds: `route_localnet` is 0 for all interfaces; `firewall-cmd --get-active-zones`
  and the zone of every podman bridge interface recorded; FAIL on a zone with the port open.

### S7. The warden's own pings have no acknowledgement tracking

- **Problem.** Humans' `!halt` and `!sync` become warden pings that expect `ack`
  (PROTOCOL §5; `to` may include the warden for answers). The outbox and `TIMEOUT` logic
  live in pingbus's `inbox.py` for members; U15's tests do not cover the warden tracking
  acks or telling the human. The issue makes the ack timeout "the real delivery guarantee".
- **Scenario.** A human sends `@<agent> !halt`; the agent's waiter is dead; the human sees
  "halt sent to 1 worker" and never learns it was not received.
- **Fix.** U15 uses the same outbox for warden pings and posts an `m.notice` on each
  `TIMEOUT`; add the test. Also add to U15: the warden accepts invites only from a
  configured human creator and verifies the room marker (DESIGN §8 does not say how the
  warden joins rooms at all).

### S8. Broken tables hide a unit's dependencies and an interface

- **Problem.** Unescaped `|` inside backticks breaks two table rows. U10's row
  (`hook stop|prompt|session-start`) spills into the Tests, Needs and Where columns, so
  U10 has no stated dependencies or location; it is in wave 4 but needs at least U05
  (inbox) and U04 (it edits `cli.py`). The "token files" interface row (DESIGN §3) is
  truncated at "created \`O_EXCL".
- **Scenario.** An agent assigned U10 starts before U05 is merged and writes its own
  inbox reader; or U10 and U09 edit `cli.py` on parallel branches and conflict.
- **Fix.** Escape the pipes (`\|`) and restore both rows. State U10 `Needs: U04, U05`, and
  sequence U09 and U10 (or move the hook entry points to their own module, `hooks.py`, so
  `cli.py` only dispatches).

### S9. PLAN.md's tasks do not match the design's units

- **Problem.** PLAN.md lists Tasks 2.1–2.5 and 3.1–3.3; DESIGN §12 lists U00–U23. Task 1.1
  is still "in progress" though its research is written. There is no mapping, so plan
  state cannot be updated as units merge (Plan Commit Rule).
- **Scenario.** U13 merges; nobody can say which PLAN.md task it advances.
- **Fix.** Replace Phase 2–3 tasks with the units (or the milestones from B1, each listing
  its units), and close Task 1.1.

## Nits (cut or defer to keep it small)

- **N1. iamb (D24) can be deferred.** §9 is an optional play; `pingbus tail` already gives
  a terminal view. iamb costs a pinned binary, a `check-pinned-versions` entry and its own
  P4/P5 legs. Ship Element first.
- **N2. Element scope confinement (D25, H5) is beyond the requirement.** The issue asks for
  a check, which P4 gives. Defer the cgroup filter.
- **N3. The `m.notice` mirror (D21) may be replaceable by config.** Try Element's
  `showHiddenEventsInTimeline` setting default in the profile config before writing mirror
  code; keep D21 only if the raw view is unreadable.
- **N4. `--json` on every command is not asked for.** The issue requires one stable line
  format; drop `--json` from v1.
- **N5. Over-configurable limits.** `poll_chunk_s` and `duplicate_window_s` overrides are
  not needed; keep the issue's "rate limits and ack timeouts" overridable, fix the rest.
- **N6. `status` state `busy` has no writer.** Nothing in the design sets it; drop it or
  say who does.
- **N7. Owner question 1 gates U01.** The namespace is baked into the protocol doc and the
  validator; list "owner answer to Q1" in U01's Needs.
- **N8. No re-validation when reading the inbox.** The inbox is files under the project
  mount; `recv`/`wait`/hooks print them without re-running `validate_incoming`. Cheap to
  add, and it also applies allowlist changes (S4) to pings already stored.

## What was checked and holds

- Every hard rule has an owning unit: closed verbs and ref grammar (U01), forge check on
  send (U06), receive validation and drop reporting (U08/U09), human text only through the
  warden (U14/U15, D17), standard-library CLI plus skill (U11/U17), versioned spec with a
  contract test (U01).
- Registration closed from first boot (D3) is smaller and safer than the issue's
  token-then-close sequence.
- The `--internal` team network is not a cut candidate: members need some network to reach
  the homeserver anyway, and it gives P1 by construction.
- Wave dependencies are otherwise consistent with each unit's Needs column.
