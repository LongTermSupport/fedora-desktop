# Task 15.3: what "active" meant in ccy-teams, and liveness from the bus

Member handles from the owner's report are replaced by placeholders here.

## The owner's question: how "active" was defined and refreshed

- **What it read.** A member's STATE came from `agent-bus list <team>` (run as root through
  `sudo -n`, `src-go/internal/agentbus/agentbus.go` `Client.List`), whose `MEMBER` line ends
  in `active|parked`. That field is written by `helpers/agent_bus/admin.py` `list_members` from
  the team registry only: `'parked' if reg.is_parked(handle) else 'active'`. So "active" meant
  "has a handle in the registry and is not parked", nothing more.
- **How it became the label.** `src-go/internal/teams/board/snapshot.go` `addMemberRows` (at
  b83158ca8, lines 246-253) set `activity[handle] = "active"` for every member and `parked`
  for a parked one; `collect.go` `seatRow` (line 551) and `addListedRows` copied it into STATE,
  and `table.go` `wordColour` (line 261) painted "active" green. The graph
  (`board/graph.go` `stateOf`, lines 211-243) printed it as "membership active" and decided
  online from the tmux session for a local seat, and "unknown" for every other member.
- **When it refreshed.** On every pass of the board (each `--list`, `--graph`, and each redraw
  of the picker), `agent-bus list` runs once per team that is up. The word was re-read each
  time but could never change by itself: a member stays "active" until it is parked, so a
  member whose session stopped hours ago stayed green.
- **What pingbus status reads instead.** `helpers/pingbus/cli.py` `_member_status` (line 2694):
  each member's `agent_bus.status` state event in the team room (`until`, Unix ms), from the
  session's synced room view; `listening` if `until > now`, `stale` otherwise, `-` with none. A
  listening session publishes it with a 10-minute lease renewed every 5 minutes
  (`WATCH_STATUS_TTL_MS`, `WATCH_STATUS_REFRESH_MS`, cli.py 1807-1808), so a session that dies
  reads stale at most 10 minutes later.

## What it shows now

- `agent-bus list` prints, after each `MEMBER` line, `LIVE <handle> listening|stale|-`,
  judged from the member's `agent_bus.status` in the team room exactly as `pingbus status`
  judges it (`admin._liveness`: the same `protocol.read_status` and `until > now` that
  `check-rooms` already used). The `MEMBER` line is unchanged, so `checkout.py`,
  `agent_bus_lxc/members.py` and older ccy-teams binaries read it as before.
- `agentbus.ParseRoster` reads `LIVE` lines into `Member.Live` (strict: an unknown word, a
  handle no `MEMBER` names, or a second line is an error).
- STATE (`--list`, the picker, menu headings): `listening` (green), `stale` and
  `not listening` (yellow), `parked` (the registry's state, kept and named as such), and
  `unknown` (red) when it could not be read; the team row's WHERE says why (need sudo,
  homeserver down, or "liveness unknown: agent-bus list reports none (the agent-bus play
  updates it)" for a wrapper older than `LIVE` lines).
- `--graph` uses the same source for every kind of member: class `listening`, `offline`
  (stale, no status, parked, deferred) or `unknown` with why; a seat also shows its session.
- Help (`picker/help.txt`), `usage.txt`, `agent-bus list`'s help, `docs/agent-bus.md` and
  `docs/ccy-changelog.md` (CCY 3.104.17) define the states.

Anonymised example: members `pingbus status` called stale (say `repo-a.1+local.podman`,
`repo-b.2+local.host`) now read `stale`; those it called listening (`repo-c.1+local.podman`)
read `listening`.

## Defence Before Fix

Method specification 1.1.0 (cached copy from the dbf skill's refresh script). Toolchain:
go-qa-ci's analyzers driver with the project rules (`src-go/qa/rules`); no manifest
declaration found.

- **Class and hazard.** A status word a person reads as running ("active", "online",
  "listening", "alive") spelled where no live source decides it; the person believes a
  session runs when it does not.
- **Rule.** `project_liveness_word` (`src-go/qa/rules/livenessword.go`, catalogue entry in
  `CLAUDE/GoProjectRules.md`): such a literal outside `internal/agentbus`, `internal/systemd`
  and `qa/rules`, in non-test files; struct tags and imports not flagged; excuses are rows of
  `livenessAllowances` with a reason. Blocking in the Go gate (`rules.All`).
- **Order, disclosed.** The rule was written before the independent searcher was dispatched,
  so the search is not independent in time; it was kept away from the rule's files and the
  catalogue and never saw the rule.
- **Red.** Fixture `testdata/src/fix/internal/badlive` (with owner and allowance packages).
  Real tree at b9fef585c (the defence alone, red by design): 6 findings, snapshot.go (the
  original), table.go, graph.go, lxcadd/adder.go, stt/recording.go, teams/teamtest/world.go.
- **Sweep and fixes.** Go module and `qa` module, both build tag sets. Fixed: snapshot.go,
  table.go, graph.go (from `agentbus.Liveness`), lxcadd (`agentbus.WordNotParked`). Excused
  with reasons: stt (the phone app's `ACTIVE` manifest word, read, never shown) and teamtest
  (the fake `systemctl is-active` answer). Green afterwards.
- **Independent search** (`261102-active-state-search.md`): found the original; beyond the
  rule's reach it found `files/var/local/claude-yolo/lib/docker-health.bash`, whose
  `ccy --top` STATUS said "active" when the container could not be inspected; fixed (now
  "unknown"). `play-qobuz.yml`'s "running" was cleared (the unit's ActiveState is asserted
  first). The lease delay (listening up to 10 minutes after a session dies) belongs to the
  source and is the same in `pingbus status`.
- **Referred to the owner.** `agent-bus list`'s `MEMBER` field still prints the park word
  `active` to a person who runs it (Python, outside the Go rule). It is documented as meaning
  only not parked, and the `LIVE` line beside it gives the liveness; renaming the wire word
  touches four readers deployed by two plays, so it is the owner's decision. No Python or
  shell detector was built: a word list there would flag correct uses (systemd, gh) by the
  dozen.
- **Next wider rule, not built.** Adding "up" and "running": both are used for things a live
  source decides (a homeserver unit, a tmux session), so it would be wider than the hazard.
- **Not done:** the conformance reviewer was not dispatched (this agent's context budget was
  spent); the coordinator may run it on b9fef585c..tip.

## Tests

Test first: Python `test_list_says_whether_each_member_is_listening_as_pingbus_status_does`
(red, then green); Go `TestParseRosterReadsLiveness`, the malformed-LIVE cases,
`TestCollectShowsLivenessFromTheBusAndUnknownWithWhy`, the graph golden and
`TestMermaidShowsAnUnreportedMemberAsUnknownWithWhy` (red on the missing `Row.Live`, then
green). `scripts/test-ccy-teams-command.bash` passes with its fake `agent-bus list` given `LIVE`
lines.

## Deploy

`changed-plays` in `CLAUDE/Plan/meta-deploy.list`: `play-agent-bus.yml` (the `LIVE` lines) and
`play-claude-yolo.yml` (ccy-teams, CCY 3.104.17). Task 15.4 is the host check.
