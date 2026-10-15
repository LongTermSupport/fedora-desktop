# Plan 00201 research: the current design, and where it assumes one team per session

Task 1.1. Every claim cites the file and line it was read from at the commit this document
was written on. Placeholders only: teams `alpha`, `beta`; repository `example-org/proj`.

## Verdict in one paragraph

The bus itself was designed for several teams per session and mostly is: the `--teams`
flag takes a list of `<seat>@<team>` items, `pingbus seat exec` claims each, and the member
client's multi-team model (one watcher, a thread per team, every output line naming its
team) is in place. What forces "one team" is the **launcher's chooser path**:
`ccy --teams` with no value asks `ccy-teams for-checkout --choose`, which prints exactly one
team or hard-stops, and the ccy-teams picker offers to start a session in one team at a
time. Descriptions exist nowhere. Trust scope is not checked when a seat is made, so a
seat in a team that does not cover its checkout is created silently and then cannot have
its own repository's references trusted.

## 1. The ccy launcher's `--teams` path

| Piece                                                                                                                                                                                                                           | Where                                                             | One team per session?                                                                           |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| Flag parsing: `--teams` once, value is the whole list; an option in the value's place means "bare"                                                                                                                              | `files/var/local/claude-yolo/claude-yolo:627-658`, `:727-740`     | No: the value is a list                                                                         |
| Help text names the list form and the team-alone form                                                                                                                                                                           | `claude-yolo:210-221`                                             | No                                                                                              |
| Bare `--teams`: `TEAMS_LIST=$(ccy-teams for-checkout --choose) \|\| exit $?`                                                                                                                                                    | `claude-yolo:842-844`                                             | **Yes.** `--choose` prints exactly one team; any non-zero exit (no team covers) ends the launch |
| `ccy_seats_check` (`agent-bus seat check`, `--resolved` when unattended) prints the canonical list                                                                                                                              | `lib/agent-bus-seats.bash:43-61`, called at `claude-yolo:849`     | No                                                                                              |
| `ccy_seats_resolve`: each team-alone item goes through `ccy-teams choose-seat <team>`                                                                                                                                           | `lib/agent-bus-seats.bash:68-86`, called at `claude-yolo:859-863` | No: loops over every item. One chooser per team, one after another                              |
| `ccy_seats_take` (`agent-bus seat take`), then `-e PINGBUS_SEATS=<list> --label ccy-seats=<list>`                                                                                                                               | `lib/agent-bus-seats.bash:96-113`                                 | No                                                                                              |
| The canonical list replaces the given one in the arguments, for restart and restore                                                                                                                                             | `lib/agent-bus-seats.bash:119-135`, `claude-yolo:865-873`         | No                                                                                              |
| Quick Launch: shows `Team:` via `ccy-teams describe-seats`, replays `LAST_TEAMS`, records the seats in the session record                                                                                                       | `claude-yolo:1232`, `:1270-1279`, saved at `:3465`                | No: `LAST_TEAMS` is the canonical list                                                          |
| tmux: a `--teams` launch is offered only detached sessions sharing one of its seats                                                                                                                                             | `lib/tmux-session.bash:303-335`, `:483-487`                       | No (`ccy_tmux_seats_shared` compares lists)                                                     |
| Session registry keeps `--teams` for restore                                                                                                                                                                                    | `lib/session-registry.bash:112-136`                               | No                                                                                              |
| Entrypoint: refuses `PINGBUS_*` set by `ccy.env`/`ccy.env.local` or a launcher-passed `PINGBUS_TEAMS`/`PINGBUS_HOME`; with `PINGBUS_SEATS` links pingbus, adds `--plugin-dir` and `--settings`, prefixes `pingbus seat exec --` | `files/var/local/claude-yolo/entrypoint.sh:549-596`               | No                                                                                              |
| Bash size ratchet: `claude-yolo` is at its allowance (2839 code lines), `agent-bus-seats.bash` at 81                                                                                                                            | `scripts/bash-limits.allowlist:70`, `:72`                         | n/a: **any new chooser logic must live in Go (ccy-teams), not in these files**                  |

## 2. `src-go/internal/teams` (ccy-teams)

| Piece                                                                                                                                | Where                                                                          | One team per session?                                                                               |
| ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------- |
| `for-checkout`: no match is a failure with the "No team on this machine works on …" sentence                                         | `find/find.go:55-58`                                                           | **The hard stop the owner hit**                                                                     |
| `for-checkout --choose` with several matches: one `gum choose` (single select)                                                       | `find/find.go:61-62`, `:68-91`                                                 | **Yes**: `Gum.Choose` returns one string                                                            |
| No terminal / no gum with several matches: failure naming them                                                                       | `find/find.go:71-79`                                                           | n/a (correct fail-fast)                                                                             |
| Non-git or non-GitHub checkout: failure "Name the team"                                                                              | `find/find.go:94-122`                                                          | n/a                                                                                                 |
| `record-seats` writes the canonical list into the session record                                                                     | `find/record.go:16-41`                                                         | No                                                                                                  |
| `choose-seat <team>`: one team per call; prints `SEAT@TEAM`                                                                          | `choose/choose.go:80-105`                                                      | Per call yes; the launcher loops                                                                    |
| `describe-seats <list>`: every item of the list                                                                                      | `choose/choose.go:110-134`                                                     | No                                                                                                  |
| Picker's first pass: uncovered GitHub checkout → "No team … works on" plus "Create a team for …?"                                    | `picker/offers.go:173-174`, `:193-208`                                         | No join choice is offered at all for an uncovered checkout                                          |
| Picker's first pass: for each covering team with no live seat here, "Start a ccy session … as a member of `<team>`?"                 | `picker/offers.go:181-189`, `:211-220`                                         | **Yes**: `StartCcy(top, team)` execs ccy with one team, and the first yes leaves the picker         |
| `Seats.StartCcy(top, refs)` execs `ccy --teams <refs>`                                                                               | `seats/seats.go:55-62`                                                         | No: `refs` can be a list; every caller passes one                                                   |
| Ctrl-N / "New ccy session in `<team>`": seat chooser for one team                                                                    | `seats/seats.go:134-186`                                                       | **Yes** (row-scoped: it is the team row's action)                                                   |
| Create wizard ends by offering a session in the new team                                                                             | `create/wizard.go:867`                                                         | Yes, the new team only (right for that flow)                                                        |
| Board: a session record's `--teams` list is split, each seat mapped to the session                                                   | `board/snapshot.go:157-169`, `board/collect.go:132-150`                        | No: a multi-seat session shows under every team                                                     |
| Board: team row has `MEMBER` `-`; columns are fixed `%-24s %-36s …`                                                                  | `board/collect.go:359-372`, `board/table.go:35-62`                             | n/a: **no description anywhere in the table**                                                       |
| `HoldsLiveSeat(top, team)`                                                                                                           | `board/snapshot.go:107-118`                                                    | No                                                                                                  |
| `Ops.ApplyTeams`: runs the play for the named teams, checks each is `current`, then the Element play                                 | `ops/ops.go:117-147`                                                           | n/a: this is the apply step scope widening reuses                                                   |
| `ui.ChooseSpec{Multi: true}` is fzf `--multi`                                                                                        | `src-go/internal/ui/choose.go:12-40`                                           | n/a: **a multi-select primitive already exists**; `ui.Gum` has only single `Choose` (`ui/ui.go:85`) |
| `declare.Client`: `TeamsFor` returns names only; `Declared` parses a 6-field `TEAM` line; no description, no "add a repository" edit | `src-go/internal/declare/declare.go:80-87`, `:166-198`, `:284-299`, `:351-379` | n/a                                                                                                 |

## 3. Seats and the seat registry, `PINGBUS_SEATS`

- A seat is `<seat>@<team>`, one per team per session; a list naming one team twice is
  refused (`helpers/pingbus/seat.py:119-165`, `one_per_team`). Design: Plan 00161
  DESIGN.md section 5.5 (D48: "a single session could join multiple teams; only ever take
  one seat per team").
- `seat check` checks only that each team's homeserver is active here
  (`helpers/agent_bus/checkout.py:578-601`); `seat take` makes missing seats with
  `sudo agent-bus add-member … --repo=<repo> --seat=… --out=-` (`checkout.py:604-640`).
- `seat exec` claims every seat, builds `/tmp/pingbus-home/<team>` links, sets
  `PINGBUS_TEAMS` to the list's teams (`seat.py:337-355`) and prints
  `✓ agent team bus: <seat>@<team> (<handle>)[, …]` (`helpers/pingbus/cli.py:1349`).
- **None of these assume one team.** Several seats in several teams is the designed case.

## 4. The member client, `helpers/pingbus`

| Piece                                                                                                                                                         | Where                                                                                         | One team?                                                                              |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `PINGBUS_TEAMS` parsed as a list, duplicates refused                                                                                                          | `config.py:142-153`                                                                           | No                                                                                     |
| `select_teams`: `--team` alone, else all; `single` commands need `--team` when several are active: "several teams are active: name one with --team" (exit 64) | `config.py:156-170`                                                                           | No, but `send`/`say` make the agent name the team every time                           |
| `send`, `say` act on one member (`_one_member`)                                                                                                               | `cli.py:702-722`, `:738-757`                                                                  | Correctly one team per message                                                         |
| `--to` names become `@<name>:<server_name>` with no lookup; a wrong or short name fails later as `refused: target` (exit 4)                                   | `cli.py:682-699`                                                                              | n/a: **a teammate's report**: `--to <repo>.<seat>` without `+<host>.<type>` is refused |
| `say` parser: `--to NAME[,NAME...]` "human localparts, handles or full user IDs"; no example of a handle                                                      | `cli.py:1414-1418`                                                                            | n/a                                                                                    |
| `recv` prints `PING`/`HUMAN`/`PEER` lines naming the team, nothing on how to answer                                                                           | `cli.py:85-86`, `:208-235`, `:827`                                                            | No                                                                                     |
| `status`: a `TEAM` line per active team (handle, trust, wake, counts), `MEMBER` lines, then `SEAT` lines                                                      | `cli.py:1255-1320`                                                                            | No; **no description**                                                                 |
| `watch`: one lock and long-poll thread per team; a busy or failed team is dropped and the others go on                                                        | `cli.py:1062-1100`                                                                            | No                                                                                     |
| Socket notice is counts only, all teams summed                                                                                                                | `notify.py:30-57`                                                                             | No: `recv` then names each item's team                                                 |
| Hooks: SessionStart role line and seat lines per team; Stop guard per team lock                                                                               | `hooks.py:56-94`, `:124-150`, `:160-214`                                                      | No; context names `seat@team` but **not what the team is for**                         |
| Skill: "This session belongs to one or more teams"; `--team NAME` when several are active                                                                     | `files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/skills/pingbus/SKILL.md:8`, `:94-96` | No                                                                                     |

## 5. The plugin and kit

`play-claude-yolo.yml:339-393` stages `settings.json`, the plugin (`plugin.json`,
`hooks.json`, `SKILL.md`) and builds the pingbus zipapp into the image's
`/opt/claude-yolo/optional/agent-bus/`. `hooks.json` registers SessionStart, UserPromptSubmit
and Stop, each `pingbus hook …`. Nothing in the plugin names a team; it is per session, so
it serves any number of teams. A change to pingbus or the skill needs the container version
bump, `CCY_VERSION` bump (`claude-yolo:17`) and changelog entry.

## 6. The trust check

- A team trusts an `org` (every repository of that owner, any branch), and/or listed
  `repos` (optionally branch-limited), minus `exclude` (`helpers/agent_bus/teamfile.py:38-43`,
  `:200-250`; protocol `helpers/pingbus/protocol.py:461-483`).
- **Membership is not scope-checked.** `add-member` takes `--repo` only to build the handle
  (`helpers/agent_bus/admin.py:663-689`); `seat check`/`seat take` check only that the
  homeserver runs (`checkout.py:578-601`). A seat in `beta` from a checkout of
  `example-org/proj` that `beta` does not cover is created and works.
- **What breaks is references.** A ping's `ref` to a repository outside the team is
  `external` (`protocol.py:466-474`, `forge.py:595-620`): accepted only for verbs whose
  "External `ref`" column allows it, and only for a public, non-fork repository
  (`docs/agent-bus-protocol.md:591-594`, `:230`). So a member in a private uncovered
  checkout cannot send `done` or `review` naming its own commits. That is why choosing a
  team that does not cover the checkout must offer to widen its scope.
- **Defect found:** `declare.py teams_for` ignores `exclude`
  (`helpers/agent_bus/declare.py:793-804`): an org team whose `exclude` names the checkout's
  repository is reported as covering it, so `ccy --teams` picks a team that will treat that
  repository's references as untrusted. The spec fixes it with the chooser (S-B4).

## 7. `declare.py`, the team file and where a description belongs

- The host_vars entry is the declaration; `play-agent-bus.yml:183` writes
  `item | combine({'state': 'present'}) | to_nice_json` as the team file, so **any key in
  the entry reaches the installer**, and `teamfile.parse_team_file` refuses an unknown key
  (`teamfile.py:286-292`). A `description` key therefore needs teamfile support first, or
  the play fails.
- `declare.py` reads the host_vars as lines, edits by inserting lines, validates through the
  team file's own rules, backs the file up and replaces it whole (`declare.py:1-65`,
  `:529-627`). New subcommands follow `drop-limit`'s pattern: edit, read back, refuse if
  anything else changed (`declare.py:807-826`).
- **A description in the team file forces no homeserver restart.** `agent-bus-install team`
  restarts only when `tuwunel.toml`, the unit drop-in, the binary or the base unit changed
  (`files/usr/local/sbin/agent-bus-install:805-828`); `team.json` is written at `:807`
  without setting `restart`, and `render toml` uses only `server_name`, listen addresses and
  `port` (`helpers/agent_bus/render.py:53-59`). It does change the config digest
  (`teamfile.py:360-362`), so the team shows "out of date (config)" until applied, and the
  apply republishes the team record through `bootstrap` (DESIGN.md 3.4 step 6). The same
  holds for widening `repos`.
- **Getting it to members.** pingbus reads team data from the team record, published by
  the admin (`admin.py:495-512`, `:554`). The record's keys are closed: a key outside
  `TEAM_KEYS` makes the room untrusted (`protocol.py:138-140`, `:906-910`). So a
  `description` record key is a protocol change inside version 1, the same kind as
  `exclude` was (`docs/agent-bus-protocol.md:43-44`): every member must run the new pingbus
  first. In this repository the kit and the image come from one build.
- The room topic is a fixed constant set only when the room is created
  (`admin.py:61`, `:532`); Element shows it, but nothing updates it.

## 8. Where a session's teams are shown today

The launch's stderr (`seat take` CHANGED lines, `seat exec`'s `✓ agent team bus:` line),
Quick Launch's `Team:` line, SessionStart context, `pingbus status`, and the `ccy-teams`
board. No surface shows a description; nothing shows the teams in the tmux status bar.

## 9. How far several teams work end to end today (ccy)

`ccy --teams 1@alpha,2@beta` **already works as designed**, layer by layer:

| Layer                     | State                                                                                                                                                             | Evidence                                                                         |
| ------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| Flag and canonical list   | accepts several `SEAT@TEAM`; one team twice refused                                                                                                               | `claude-yolo:727-740`, `seat.py:119-165`                                         |
| Team alone in a list      | `--teams alpha,beta` gets one seat chooser per team, in turn                                                                                                      | `agent-bus-seats.bash:68-86`                                                     |
| Seats on the host         | `seat take` makes each missing seat                                                                                                                               | `checkout.py:604-640`                                                            |
| Claim                     | `seat exec` claims every seat, links each team, `PINGBUS_TEAMS=alpha,beta`                                                                                        | `seat.py:337-355`; unit test `tests/helpers/pingbus/test_seat.py:338`            |
| Watcher and wake          | one watcher, one lock and thread per team; counts summed in one notice                                                                                            | `cli.py:1062-1100`, `notify.py:30-57`                                            |
| Hooks                     | per-team role and seat lines; Stop guard per team                                                                                                                 | `hooks.py:124-214`; unit tests `test_hooks.py:242`, `:483`                       |
| `recv`/`status`/`history` | every line names its team                                                                                                                                         | `cli.py:827`, `:1293-1320`, `:1145`; unit test `test_cli.py:205`                 |
| `send`/`say`              | need `--team` when several are active (exit 64 otherwise)                                                                                                         | `config.py:156-170`                                                              |
| Plugin                    | team-agnostic                                                                                                                                                     | `hooks.json`                                                                     |
| Status line               | **shows no team at all**: no hooks-daemon handler or ccy status element reads the bus                                                                             | no reference to pingbus under `.claude/hooks/` or in `.claude/hooks-daemon.yaml` |
| Board                     | a multi-seat session appears under each team                                                                                                                      | `board/snapshot.go:157-169`                                                      |
| Live proof                | **never run with two teams.** The 00161 host acceptance seats three sessions in **one** team (U20); multi-team is proven only by unit tests with fake homeservers | `CLAUDE/Plan/00161-agent-team-bus-matrix/acceptance.bash:275`                    |

So the multi-team gap is not the bus: it is (a) no way to *choose* several teams except by
typing them, (b) no single place that says which teams a session is on (no status line,
no descriptions), (c) `send`/`say` friction, and (d) no live two-team acceptance.

## 10. `cc --teams` (the host launcher) has drifted

`files/var/local/claude-code/cc-teams.bash` (on the bash ratchet at 170 code lines with the
`ui` mark, `scripts/bash-limits.allowlist:68`; sourced at `files/var/local/claude-code/cc:78-82`,
run at `cc:188-194`; tests `scripts/test-cc-teams.bash`):

| ccy has                                                   | cc has                                                                                  |
| --------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| a list of `SEAT@TEAM` items, several teams                | exactly one team, no seat: `--teams` with `@` or `,` is refused (`cc-teams.bash:57-60`) |
| bare `--teams` finds the team (`for-checkout --choose`)   | `--teams` needs a value (`cc-teams.bash:48-51`)                                         |
| seat chooser and role offer (`choose-seat`)               | takes agent-bus's DEFAULT seat silently (`cc-teams.bash:71-93`)                         |
| restart/restore replays the canonical list                | nothing to replay (cc is not restored)                                                  |
| system prompt says nothing; the skill and SessionStart do | one `--append-system-prompt` line naming one team (`cc-teams.bash:179-183`)             |

Differences that are real, not drift, and must stay:

1. **Member type `host`, not `podman`** (`cc-teams.bash:24`, `:78`, `:99`): cc runs on the
   host, so its handle is `<repo>.<seat>+<host>.host` and its seats are distinct from ccy's.
2. **The Element consent** (`cc-teams.bash:128-164`): a cc session runs as the owner, beside
   their Element session, so the owner accepts that once per user, on `/dev/tty`, recorded in
   `~/.config/pingbus/element-accepted`; pingbus refuses a `host` member beside Element
   without it. ccy runs in a container that cannot read Element, so it has no such step.
3. **The Element deny settings** merged into the session's settings
   (`cc-teams.bash:166-175`, `files/var/local/claude-code/cc-teams-settings.json`).
4. **`seat exec --root/--home` on the host** with a private temp home
   (`cc-teams.bash:185-199`), where ccy's entrypoint does it inside the container.
5. **cc makes `.claude/ccy/` itself** with a seats-only `.gitignore` when the checkout was
   never used with ccy (`cc-teams.bash:106-126`).
