# Plan 00201 spec: many teams per session, team descriptions, one chooser for ccy and cc

Task 1.2. Built on [RESEARCH-current-design.md](RESEARCH-current-design.md) (cited as R§n)
and Plan 00161's DESIGN.md. Placeholders only: teams `alpha`, `beta`, `gamma`; checkout
`example-org/proj`; handles `proj.1+local.podman`. Every expectation is numbered so a test
can name it. "stderr" and "stdout" follow CLAUDE/StderrHygiene.md: stdout is the payload.

Exit codes are the existing ones: ccy-teams 0 / 1 refusal or failure / 64 usage / 130
cancelled (`src-go/internal/teams/usage.txt`); declare.py 0 / 64 / 78 / 130; agent-bus and
pingbus per docs/agent-bus-protocol.md §14 (4 refused, 64 usage, 75 busy, 78 config).

## Owner decisions

Each has a recommendation. The spec below is written as if every recommendation is taken;
a different answer changes only the expectations it names.

| #   | Decision                                                                                              | Recommendation                                                                                                                                                                                                                                              | Affects       |
| --- | ----------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------- |
| Q1  | Is a description **required**?                                                                        | Required for every team `ccy-teams create` makes; **optional in the team file** so existing teams and other projects' team files stay valid. A team without one shows `(no description)`, and the ccy-teams picker asks once per team to write one.         | S-A1, S-A6    |
| Q2  | Does the description reach the **agents** (pingbus, SessionStart), or only the host (ccy, ccy-teams)? | Both, **through the room topic**, not a team-record key (revised after review: a new record key makes every older member exit 10). Old readers ignore the topic, so no upgrade-first ordering.                                                              | S-A8 to S-A12 |
| Q3  | Should the team room's **Element topic** be the description?                                          | Yes: `bootstrap` sets the topic to the description on every apply (the fixed sentence when there is none). Element is where the humans see teams.                                                                                                           | S-A9          |
| Q4  | Choosing a team that does not cover the checkout: what are the choices?                               | Three, default **Add**: "Add example-org/proj to beta's scope", "Join without adding (beta will treat example-org/proj references as external)", "Leave beta out". Joining without adding is an explicit, named choice, not a fallback.                     | S-B7          |
| Q5  | When does the chooser appear?                                                                         | Whenever an attended launch has `--teams` with no value or a team alone, **unless** the machine has exactly one team and it covers the checkout (then it is taken, as today). With two or more teams the owner always sees the list, covering teams on top. | S-B2          |
| Q6  | How does `pingbus send`/`say` pick a team when the session is on several?                             | Infer it when unambiguous: from `--re` (`send` only: an event belongs to one team's room) or from `--to` (every name resolves in exactly one active team). Otherwise exit 64 listing the active teams with their descriptions. `--team` always wins.        | S-C8, S-C9    |
| Q7  | Show the session's teams in the **status line**?                                                      | Yes (revised after review): the member kit's `pingbus status-line` shows `👥 alpha,beta` from the per-session record, below the checkout's own status line, in any checkout (S-C6). It is the one place visible during the whole session.                   | S-C6          |
| Q8  | Does `recv` print a ready reply command for PINGs too, or only for PEER and HUMAN items?              | PEER and HUMAN only (what was asked). A PING's answer is a verb the agent must choose; the skill already says how.                                                                                                                                          | S-D1          |
| Q9  | `cc --teams`: does cc pick its seat silently (today) or show the seat and role chooser as ccy does?   | Show the same chooser: one flow, one behaviour. The only cc-specific step is the Element consent, shown inside the flow.                                                                                                                                    | S-E3          |
| Q10 | `ccy-teams for-checkout --choose`: keep or remove once the launchers use the new flow?                | Remove `--choose` (YAGNI); keep `for-checkout` without it, which prints the covering teams for scripts.                                                                                                                                                     | S-B14         |

## A. Team descriptions

Declared in the `agent_bus_teams` entry (host_vars) as `description:`, written into the team
file by the play unchanged (`play-agent-bus.yml:183` passes every key, R§7).

- **S-A1** `teamfile.parse_team_file` accepts an optional `description` (OPTIONAL_KEYS). Valid:
  a string of 1 to 120 characters, equal to itself stripped, with no character of Unicode
  category Cc or Cf (so no newline, tab or bidi control). Otherwise `TeamFileError`
  naming `description` and the rule. Absent is valid.
- **S-A2** The grammar lives in `helpers/pingbus/protocol.py` (`is_description`,
  `DESCRIPTION_MAX = 120`), the one home of shared grammars; teamfile and the record parser
  import it.
- **S-A3** `TeamFile.as_dict` carries `description` when set, so it is in the config digest:
  a description change makes the team `outdated (config)` until applied.
- **S-A4** Applying a description change **does not restart the homeserver**: the installer
  prints no `CHANGED restarted agent-bus-hs@<team>.service`; `tuwunel.toml` and the drop-in
  are byte-identical before and after (R§7, `agent-bus-install:805-828`). Tested by the
  installer test with a team file differing only in `description`.
- **S-A5** `declare.py create` takes `--description TEXT` and requires it ("create needs
  --description …", exit 64); `check --description TEXT` validates it alone (exit 64 naming
  the rule). The entry is written as `description: <JSON string>` (flow style, as the other
  keys).
- **S-A6** `declare.py describe --file F --team T --description TEXT` sets or replaces the
  description of a present team, every other line byte for byte as it was, read back and
  refused (78, nothing written) otherwise, the backup kept as `create` keeps it. Prints
  `CHANGED<TAB>team T description` or `UNCHANGED<TAB>T`. A vaulted or unreadable entry:
  78 naming the line.
- **S-A7** `declare.py declared` and `overview` print a seventh `TEAM` field: the
  description as a JSON string, or a bare `-` when there is none (quoting keeps a
  description that is itself `-` distinct from "none"). `declare.Client` parses 7 fields;
  `declare.Team` gains `Description`.
- **S-A8** (revised) The description is **not** a key of the team record: the record's keys and
  its trust check are unchanged, because a receiver treats an unknown key as untrusted (exit 10),
  which would break every member on an older image the moment a team set a description. The
  admin carries it in `m.room.topic`, which old readers ignore. The syncer reads the topic into
  `about.json` only from an event sent by `admin`, only if it passes S-A2's grammar and is not the
  fixed sentence; otherwise the team has no description and nothing is lost. A record written by
  the admin for a team with a description still validates under the old key set (tested).
- **S-A9** `bootstrap` sets the room's `m.room.topic` to the description, or to the fixed
  `ROOM_TOPIC` when there is none, only when it differs (idempotent; `CHANGED topic` when set).
- **S-A10** `pingbus status` prints, after a trusted team's `TEAM` line and only when the cached
  topic gives a description, `ABOUT<TAB>1<TAB><team><TAB><description as JSON string>` (new line
  type in section 15). An untrusted team prints none.
- **S-A11** SessionStart adds per team with a description one line
  `agent-bus: team <team> is for: <description>`, checked by S-A2's rule before it is filled.
- **S-A12** The skill's status section names the `ABOUT` line.
- **S-A13** ccy-teams board: a team row's MEMBER cell is the description (or
  `(no description)`), cut to the column's 36 characters with `…` in the picker and in
  `--list` on a terminal; `--list` piped prints it whole. The team's Enter menu header shows
  it whole.
- **S-A14** `ccy-teams create` asks "What is this team for?" (one line, `gum input`, S-A1's
  rule checked as typed, bounded re-ask per CLAUDE/InteractiveScripts.md), shows it in the
  summary, and passes `--description`. `--yes` requires `--description` (exit 64).
- **S-A15** The ccy-teams picker's first pass offers, per present team without a description,
  "Team alpha (for any repository of example-org) has no description." with three visible
  choices: "Describe alpha now" (the cursor starts there), "Not now (ask again next time)",
  and "Never ask about alpha". Now: the input "What is team alpha for?", `declare describe`,
  then `Ops.ApplyTeams` for that team. Not now, and Esc: nothing is kept, and the next start
  asks again. Never: kept under `$XDG_STATE_HOME/ccy-teams/declined/description-<team>`,
  holding `never v2`. The path-limit offer has the same three choices ("Remove alpha's limit
  now", "Not now (ask again next time)", "Keep alpha's limit and never ask again") and its
  marker `declined/path-limit-<team>`. A marker the old yes/no prompt kept (an empty file,
  written for a no when there was no "later" to choose) is taken as not now, once: it is
  removed, said, and the offer is made again, so nobody resets state by hand. A team marked
  never has "Ask me to describe alpha again" (and "Ask me about alpha's limit again") in its
  row's menu, which removes the marker (`picker/offers.go`). Owner feedback, Plan 00203.
- **S-A16** Quick Launch's `Team:` line (`ccy-teams describe-seats`) shows each seat as
  `1@alpha (worker): <description>`.

## B. The chooser: one flow in Go for ccy and cc

A new subcommand owns every question between "the owner typed `--teams`" and "the seats are
known": `ccy-teams choose-teams --type=podman|host [LIST]`. It prints the canonical
`SEAT@TEAM[,…]` list on stdout and nothing else. Both launchers call it; neither keeps any
chooser logic in bash (the bash ratchet, R§1, forbids growing `claude-yolo` or
`cc-teams.bash`).

- **S-B1** Called only for an attended launch (stdin and stderr are terminals, no restore,
  no restart, not headless). With no terminal it is never called: the launcher keeps today's
  `seat check --resolved`, which refuses a team alone (exit 64).
- **S-B2** `LIST` empty (bare `--teams`) in a GitHub checkout: if the machine has exactly
  one present team and it covers the checkout, it is taken with the line
  `Team alpha works on example-org/proj (this checkout): <description>.` and no list is
  shown. Otherwise the team list (S-B3) is shown.
- **S-B3** The team list is one fzf screen with `--multi` (`ui.ChooseSpec{Multi: true}`,
  R§2). Rows, in order: covering teams, then the others, each in declared order:
  `alpha   covers example-org/proj            <description>`,
  `beta    would add example-org/proj         <description>`,
  `gamma   excludes example-org/proj          <description>`, then the row
  `+ Create a team for example-org/proj`. The header says "Tab marks several, Enter takes
  them (or the highlighted one), Esc leaves". A team whose homeserver is not up is shown
  with `(down)` and cannot be taken (S-B6).
- **S-B4** Coverage comes from a new `declare.py choices --file F --repo OWNER/REPO`:
  `CHOICE<TAB><team><TAB>covers|widen|excluded<TAB><description or ->` per present team.
  `covers` honours `exclude` — fixing R§6's defect: `teams_for` and `choices` both return
  `excluded`, never `covers`, for an org team excluding the repository.
- **S-B5** Not a git checkout, or no GitHub origin: the list still shows every team, all
  `would add` is replaced by `not a GitHub checkout`, and taking one joins it without
  widening (S-B7's second choice is the only one). No hard stop.
- **S-B6** Taking an `excluded` or `(down)` row: the reason is printed
  ("gamma excludes example-org/proj on purpose: edit its exclude in the host_vars to change
  that." / "beta's homeserver is down: ccy-teams apply beta.") and the list is shown again,
  the other marks kept; at most 3 rounds, then exit 1.
- **S-B7** For each taken `widen` team, one question (gum choose, default first):
  "beta trusts any repository of example-org-b. Add example-org/proj (any branch) to it?"
  with the three choices of Q4. Before asking, it says what Add does: edits the host_vars,
  runs play-agent-bus.yml for beta (sudo may ask), no homeserver restart, every beta member
  then trusts example-org/proj.
- **S-B8** Add runs `declare.py add-repo --file F --team beta --repo example-org/proj`:
  appends `{"repo": "example-org/proj"}` to `repos` (creating the key), every other line as
  it was, read back or refused (78). Refused with 64 when the repository is already trusted,
  and with 78 at `REPOS_MAX` or for a vaulted entry, naming the line to edit. Prints
  `CHANGED<TAB>team beta trusts example-org/proj`.
- **S-B9** All Adds of one run are applied by **one** `Ops.ApplyTeams(ctx, [beta,…])`, which
  checks each is `current`. If the apply fails: stderr says so and names
  `ccy-teams apply beta`, exit 1, **nothing is launched**; the host_vars edit stays (the
  board shows beta `out of date`). Widening applies no restart (S-A4's reasoning: `repos`
  is not in `tuwunel.toml` or the drop-in).
- **S-B10** "Leave beta out" drops it; if nothing is left, the list is shown again (bounded
  as S-B6). "Join without adding" keeps it, and the final summary (S-C4) marks it
  `(example-org/proj references external)`.
- **S-B11** `+ Create a team` runs the create wizard with this checkout's repository as the
  default scope; the wizard, in this mode, does not start ccy itself (no `offerJoin`) and
  returns the new team, which is taken as if marked, covering. Marked together with other
  teams, they are taken too. Cancelling the wizard returns to the list.
- **S-B12** Then, for each taken team in list order, the existing seat and role chooser
  (`choose-seat`'s flow, `--type` passed through so cc sees host seats). A team alone in a
  given `LIST` skips S-B2 to S-B11 and goes straight here, as today.
- **S-B13** Esc on any screen: nothing is printed on stdout, `No team chosen; nothing started.` on stderr, exit 0 — the launcher's existing "left the chooser" contract
  (`agent-bus-seats.bash:66-67`, `claude-yolo:861-862`). Nothing chosen before Esc is kept;
  a scope already applied stays applied (it was the owner's yes).
- **S-B14** `ccy` calls `ccy-teams choose-teams --type=podman "$TEAMS_LIST"` in place of
  `for-checkout --choose` and `ccy_seats_resolve` (`claude-yolo:842-844`, `:859-863`),
  then `ccy_seats_check` on the printed list. `ccy_seats_resolve` and `for-checkout --choose`
  are deleted; both bash files shrink.
- **S-B15** The ccy-teams picker's first pass (`picker/offers.go:148-208`): in a checkout no
  team covers, it offers "Start a ccy session here (choose its teams)?" as well as creating
  one; in a covered checkout, one question "Start a ccy session here?" replaces the
  question per team. Yes runs `ccy --teams` (bare), which shows S-B3. "Create a team for
  OWNER/REPO now?" defaults to **No**: creating a team is heavy and outward, so a stray Enter
  must not do it. Both start-a-session questions default to Yes.

## C. A session on several teams

- **C is mostly in place** (R§9): seats, claim, watcher, hooks, recv, status and the board
  already handle several. What changes is choosing, showing and addressing.
- **S-C1** Several teams are chosen at launch by S-B3's marks or by an explicit
  `--teams 1@alpha,qa2@beta` (unchanged). One seat per team (D48, unchanged).
  Every question of a team's sequence names the team and its place among the teams being
  joined, as the screen heading (`ui.Heading`, Plan 00204 D4): the scope question
  ("New session › Team 2 of 3: beta › Scope"), the seat question ("New session › Team 2 of 3:
  beta · step 1/2 Seat") and the role question ("… · step 2/2 Role"). One team is headed
  "New session › Team beta". The trail is also said on its own line before the team's
  questions, so a team's lines (the "references external" note, "Taking seat ...") sit under
  its own heading and never under another team's. In a given list, the place counts every
  item, named seats included. Owner feedback, Plan 00203. It is built once, by
  `flow.LaunchTeam`, and handed to the seat chooser through `SeatFunc`.
- **S-C2** A session's teams change only by a new launch; there is no join-while-running.
- **S-C3** Restart, restore and Quick Launch replay the whole canonical list (unchanged;
  verified by a test with two teams).
- **S-C4** After `seat take`, the launcher prints on stderr one block:
  `This session is on 2 teams:` then per seat `  alpha  seat 1 (worker)  <description>`. The
  block is printed by `ccy-teams describe-seats --block LIST` (Go), so neither launcher
  formats it.
- **S-C5** SessionStart: S-A11's lines, plus a first line `agent-bus: this session is on teams alpha, beta.` when there are two or more.
- **S-C6** (revised after review) Status line: in **any** checkout, a session launched with
  the member kit shows `👥 alpha,beta`. The kit's `settings.json` sets the status line
  command `pingbus status-line` (command-line settings outrank the project's); it runs the
  status line the session would otherwise have had, then appends the segment read from the
  per-session record SessionStart writes under the session id (S-C5's hook). A session with
  `PINGBUS_TEAMS` set and no usable record shows `👥 no record ...` in red, never nothing;
  a session on no team shows no segment (Q7). Not a project hooks-daemon handler: that
  existed only in this repository's checkouts.
- **S-C7** Wake: unchanged. One watcher, one thread and lock per team; one notice with
  summed counts; `recv` lines name the team. A team that fails is dropped by the watcher and
  the Stop guard's "no waker" names it (existing).
- **S-C8** `send` and `say` with several teams active and no `--team`: the team is inferred
  (Q6) from `--re EVENT_ID` (`send` only; `say` has no `--re`, and the reply command `recv`
  prints for `say` names its team) when exactly one active team's inbox or `consumed/` holds
  that event; else from `--to` when every name resolves (S-D2) in exactly one active team's
  cached record. Inferred: the `SENT` line names the team as now.
- **S-C9** Not inferable: exit 64,
  `several teams are active and --to/--re fit more than one (or none): name one with --team:`
  then one stderr line per active team `  alpha  <description>`.
- **S-C10** A live two-team acceptance leg (Task 2.7): one ccy session on `1@alpha,1@beta`;
  a ping from a member of each team wakes it once per item; `pingbus status` prints two
  `TEAM` and two `ABOUT` lines; a reply inferred from `--re` reaches the right room.

## D. Replying to a peer (a teammate's report)

`pingbus say --to <repo>.<seat>` was refused `refused: target` (exit 4), because `--to` is
turned into `@<name>:<server>` with no lookup (`helpers/pingbus/cli.py:682-699`), and only a
full handle `<repo>.<seat>+<host>.<type>` is a member.

- **S-D1** `recv` prints, right after each `HUMAN` and `PEER` line, a
  `REPLY<TAB>1<TAB><team><TAB><event id><TAB><command>` line, where command is
  `pingbus --team <team> say --to <short>` and `<short>` is the sender's short name (S-D2)
  when it is unambiguous in the team, else the full handle; for a human, the localpart.
  New line type in §13 and §15. `history` prints none.
- **S-D2** `--to` (send and say) resolves each name against the verified team record
  (already fetched before addressees, `cli.py:707`, `:745`), in order: a full user ID; an
  exact agent handle; a human localpart; then a **short name**, the handle part before `+`
  (`<repo>.<seat>`), which must match exactly one agent of the record.
- **S-D3** A short name matching several agents: exit 64,
  `--to proj.1 is ambiguous in team alpha: proj.1+local.podman, proj.1+lab.host`.
- **S-D4** A name matching nothing: exit 64,
  `--to proj.9: no member of team alpha is named that. Agents: … Humans: …` (handles and
  localparts from the record), instead of the old bare `refused: target` (exit 4). What the
  validator still refuses after resolution (an `@room`, `admin`, a human for a verb humans do
  not take, too many addressees counting the coordinator copy, no coordinator for
  `--to-coordinator`) stays exit 4 and says what it refused and who can be addressed:
  `refused: target: --to admin cannot be addressed by say. Agents: … Humans: …` (the sender
  left out).
- **S-D5** `say --help` and `send --help` show the forms in an epilog:
  `--to proj.1` (a seat, when one member has it), `--to proj.1+local.podman` (a handle),
  `--to alice` (a human), `--to @proj.1+local.podman:alpha.agent-bus.internal` (a user ID),
  and "`pingbus recv` prints a ready REPLY command for every message".
- **S-D6** The skill's addressing section says to copy the `REPLY` command.

## E. cc on the same flow

- **S-E1** `cc --teams` takes the ccy grammar: no value, a team alone, or
  `SEAT@TEAM[,…]` (several teams), validated by `agent-bus seat check`.
- **S-E2** Attended, cc calls `ccy-teams choose-teams --type=host [LIST]` (S-B); with no
  terminal a team alone is refused (64), as ccy.
- **S-E3** Seats and roles are chosen by S-B12 with host seats (Q9); the DEFAULT seat is
  the chooser's default, no longer taken silently.
- **S-E4** The Element consent is asked **inside** the flow, after the seats are chosen
  and before any seat is made, when `~/.config/pingbus/element-accepted` does not exist:
  the same words (`cc-teams.bash:143-154`, now naming every handle), asked on the terminal,
  default No. Declined: nothing made, nothing recorded, stderr
  `Not started. Nothing was recorded; cc --teams asks again next time.`, exit 1. The record
  is written by Go with the same rules (0700 directory, owner-only, no symlink, no
  overwrite). Asked only for `--type=host`.
- **S-E5** cc then runs `agent-bus seat take LIST --type=host`, builds the settings and
  the `seat exec` command as now, with `PINGBUS_SEATS=LIST`; the appended system-prompt line
  names every team: `You are proj.1+local.host in alpha and proj.2+local.host in beta …`.
- **S-E6** `cc-teams.bash` loses `cc_teams_seat` and `cc_teams_accept` and shrinks on the
  ratchet; `scripts/test-cc-teams.bash` covers the list form and the consent path through a
  fake `ccy-teams`.

Differences between the launchers that stay, and why (R§10):

| Difference                                       | Why                                                                                              |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| member type `host` (cc) vs `podman` (ccy)        | the handle names the encapsulation; a host session and a container session are different members |
| Element consent (cc only)                        | cc runs as the owner beside their Element session; ccy's container cannot read it                |
| Element deny settings merged in (cc only)        | the same reason; guidance for the session's file tools                                           |
| `seat exec --root/--home` with a temp home (cc)  | no container: the claim runs on the host, its session home private to that cc                    |
| cc makes `.claude/ccy/` with a seats-only ignore | a checkout never used with ccy has no `.claude/ccy/`; `seat take` needs it and P8's ignore rule  |
| restart/restore replay (ccy only)                | cc sessions are not restored; nothing to replay                                                  |
| `--append-system-prompt` line (cc only)          | ccy's entrypoint context and skill do it; cc has no entrypoint                                   |

## F. A team's settings (Task 2.15)

The owner: "In the CCY Teams menu, when looking at a specific team, I should be able to update
config for the team as well … a single update config thing … that then opens a pane that then
has all of the different things editable." Every key of a team's `agent_bus_teams` entry, and
the facts kept elsewhere, as built:

| Field                        | Edited here                  | What applying a change does                                                                                                                                                       | Validated by                                                                                    |
| ---------------------------- | ---------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `team` (name)                | no (Task 2.16)               | would need a new homeserver unit, data folder, server name, accounts, room, tokens and every session's seats; the installer refuses it (`check_unchanged`)                        | `protocol.is_team_name`; `render.check_unchanged` refuses a change                              |
| `description`                | yes                          | file and team.json only; the room topic is set by bootstrap; no restart (S-A4)                                                                                                    | `protocol.is_description` (1 to 120 characters, one line)                                       |
| `humans`                     | yes                          | bootstrap makes and invites each added person (who then sets a password); a removed one is kicked, deactivated and added to `removed_humans` (never to return, S-F14); no restart | 1 to `HUMANS_MAX` distinct `protocol.is_human_localpart`                                        |
| `org`, `repos`, `exclude`    | yes                          | the team record in the room, read live by running sessions' syncer; no restart                                                                                                    | `protocol.is_owner`, `is_owner_repo`, `is_repo_name`; org or repos needed; exclude needs an org |
| `path_prefixes`              | yes                          | the team record; no restart                                                                                                                                                       | 1 to `PATH_PREFIXES_MAX` `protocol.is_path_prefix`; empty removes the key                       |
| `allow_from`                 | yes                          | firewall rules and the unit drop-in's `IPAddressAllow`: the homeserver restarts                                                                                                   | 0 to 32 canonical CIDRs, no default route or multicast                                          |
| `port`, `listen`             | no                           | every member's bundle carries `base_url`; a change needs every member re-issued, which nothing does                                                                               | 1024 to 65535; canonical non-wildcard addresses                                                 |
| `server_name`                | no                           | Tuwunel cannot change it without wiping the database; the installer refuses it                                                                                                    | `check_server_name`                                                                             |
| `forge_api`                  | no                           | one allowed value (D66)                                                                                                                                                           | must be GitHub's API                                                                            |
| `state`, `purge`             | no: the menu's Take down     | `remove`, after its questions and backup                                                                                                                                          | `STATES`                                                                                        |
| coordinator (registry role)  | yes, at once                 | `agent-bus set-role` as root, through `seats.ChangeRole`; no host_vars change                                                                                                     | the roster; one coordinator at most                                                             |
| members, seats, LXC members  | no: their own rows and flows | seats come and go with sessions; Add an LXC container's agent is its own menu line                                                                                                | `members.parse_members`                                                                         |
| Element profile (`base_url`) | no                           | derived from listen and port by `declare element`                                                                                                                                 | -                                                                                               |
| backups                      | no                           | the daily timer and `BACKUPS_TO_KEEP` are the same for every team; `ccy-teams backup`/`restore` act on them                                                                       | -                                                                                               |

- **S-F1** A team row's menu has the visible choice **Edit settings** (no shortcut, its footer
  word "edit <team>'s settings via Enter"). It opens the settings screen.
- **S-F2** `declare.py settings --file F --team T` prints, for a present team,
  `SETTING<TAB><key><TAB><value>` per key of `SETTINGS_KEYS` (team, description, humans, org,
  repos, exclude, path_prefixes, allow_from, port, listen, server_name, forge_api,
  removed_humans): the value
  as JSON, `-` when the entry does not set it, `!vaulted` or `!invalid` when it cannot be read
  (one such value does not hide the rest). A team not declared present: 64.
- **S-F3** `declare.py set --file F --team T --value KEY JSON... [--check]` changes only
  `EDITABLE_KEYS` (description, humans, org, repos, exclude, path_prefixes, allow_from); any
  other key, a key twice, JSON that is not a string (description, org) or a list of strings
  (the rest): 64. An empty value of an optional key (org, repos, exclude, path_prefixes)
  removes it. `repos` is given as names, each keeping the branches it had.
- **S-F4** The entry as it would be is judged whole by `teamfile.parse_team_file` before
  anything is written; a broken rule is 64 naming it (`team file: <key>: <rule>`). `--check`
  stops there and prints nothing.
- **S-F5** All the changes are one write (one backup), each changed key one flow-style line in
  its place (a block list replaced whole) or a new line after `team:`, every other line byte for
  byte; read back, else 78 and nothing written. A key whose lines carry a comment the edit would
  lose: 78, nothing written. A vaulted entry: 78. Prints `CHANGED<TAB>team T: <keys>` or
  `UNCHANGED<TAB>T`. `describe` and `drop-limit` are this edit with one key.
- **S-F6** `declare.Client.Settings`, `Set` and `CheckSet` run them (Go).
- **S-F7** The screen (headed `Teams › <team> › Settings`, over the team and the changes waiting) lists the editable fields with their
  values, then `Coordinator: <handle or (none)>   (changed at once)`, then the fixed fields
  marked `(fixed)`, then `Save and apply (<n> changes | nothing changed yet)`, `Discard changes` and `Back`. Each row's header says what the field is and what applying it does, or
  why it is fixed. A pending change is shown as the row's value, marked `(changed)`.
- **S-F8** Choosing an editable field asks for it (`gum input`, headed `Teams › <team> › Settings › <field>`, starting from its value; lists are comma-separated). The answer is judged by
  `set --check` together with the other pending changes; refused, the reason is shown and it
  is asked again, three times at most. Esc leaves the field as it was. The value it already has
  is no change.
- **S-F9** Choosing a fixed field says why it cannot be changed here, and asks nothing.
- **S-F10** Save and apply with nothing pending says so. Otherwise it asks (title `… › Save and apply`), listing each change as `<field>: <old> → <new>` and, once each, what applying them
  does; No is the default when a person is removed, Yes otherwise. Yes writes every change in
  one `set` and runs `ops.ApplyTeams` for the team (play-agent-bus.yml, as `ccy-teams apply`).
  A refused `set` (64 or 78) saves nothing and stays on the screen naming why. An apply that
  fails says the change is saved in the host_vars but not applied, and that `ccy-teams apply <team>` applies it.
- **S-F11** Discard changes forgets every pending change. Back with pending changes asks first
  (title `… › Back`, No the default); No stays on the screen.
- **S-F12** The coordinator follows the two paths of Plan 00203 SPEC-hot-control.md H25, in
  the words of package `coordinator`. The row reads `Coordinator: <handle> (running | not running | parked | whether it is running is not known here)` or `(none)`; the running state is
  the picker's seat rows (`seats.SnapshotRunning`), passed in. Choosing it lists every member
  (the current one and parked ones marked), titled `… › Coordinator`. With no coordinator, the
  header offers one plainly ("<team> has no coordinator") and there is no `No coordinator` row.
  With one, the header names it and whether it runs, says choosing another member is a takeover
  that makes it a worker and that the question starts on No, and `No coordinator` is offered. A
  choice is one `seats.ChangeRole` call, given the running state: it asks the plain offer or the
  takeover (default No) itself, and a takeover's demote and promote happen inside it, with the
  holder put back if the promote fails. Nothing is demoted apart from a promote, except by `No coordinator`, which is that choice. It happens at once, not by Save and apply; the screen then
  says what changed. Members that cannot be read make the row say so.
- **S-F13** An entry with a value ccy-teams cannot read (vault-encrypted or not in the form it
  writes) shows it as such, and choosing an editable field says to change the entry by hand.
  Save and apply reads the settings again first: the summary's old values are those on disk
  then, and a value that became unreadable stops the save, naming it. A `set` that changes
  nothing says "Nothing to save: the host_vars already had these values."
- **S-F14** A removed person can never return: bootstrap deactivates their account, and
  `admin._ensure_humans` refuses a deactivated account. So the removal is recorded in the
  declaration. `set` adds every name it takes off `humans` to the entry's `removed_humans`
  (new optional team-file key, 1 to 256 human localparts, carried in team.json), and
  `teamfile.parse_team_file` refuses a team file whose `humans` names any of them ("a removed
  name can never return; choose another name"). That one rule is what `set --check`, `set`,
  the play's `render check` and `declared_config` all run, so the screen refuses the name as it
  is typed and no apply can be broken by it. `removed_humans` is shown as the fixed field
  Removed people; `set` does not take it as a `--value`. A person removed by hand-editing the
  host_vars before this key existed is still refused by the play's account check.

**Rename (Task 2.16, not built).** The name is fixed on the screen, with the reason. A rename
would have to make the new team (unit, data folder, server name, accounts, room), carry over
or re-create its history, re-issue every member's token and bundle, move every seat and
session record, re-point every running session, and take the old team down; the installer's
`check_unchanged` refuses it today. Half of that would leave sessions on a team that no longer
exists, so it is a task of its own.

## G. The play lock (Task 2.17)

The owner's defect: a description applied from ccy-teams while a meta-deploy unit held the
host's play lock. run.bash refused the play with exit 75 ("another play run is in progress"),
and ccy-teams died saying the description was in the host_vars but not applied. The lock is
`helpers/play_lock/lock.py`'s: a flock(2) on `fedora-desktop-plays.lock` in the user's runtime
directory, a holder line `pid=… what=… since=…`, and delegation to a child through an inherited
descriptor named by `FEDORA_DESKTOP_PLAY_LOCK_FD`, which run.bash proves with `lock.py held`.

- **S-G1** Every ccy-teams flow that runs a play takes the play lock first, through the Go side
  of the same lock (`internal/playlock`: same file, same refusals, same holder line). No second
  locking scheme.
- **S-G2** Write, then apply, in one hold. A flow that writes the host_vars (Save and apply,
  describe, drop a path limit, add a repository to a scope, create, lxc-add, remove, set up
  Element) takes the lock before it writes and holds it across the write and every play it runs.
  Each play is handed the hold (the descriptor and the variable), so run.bash runs it inside the
  hold instead of exiting 75. A wait that does not end in the lock writes nothing, so nothing is
  ever left saved but not applied by it. `ccy-teams apply` takes it the same way.
- **S-G3** A free lock is taken at once and nothing is said about it. The holder line names
  ccy-teams and what it is doing (`ccy-teams: save and apply team <t>'s settings`), so a run it
  refuses names it.
- **S-G4** A held lock is waited for, up to 30 minutes. The wait says who holds it: what it is,
  its pid, since when and for how long (`run.bash playbooks/imports/play-basic-configs.yml (pid N), held since HH:MM:SS UTC, for 2 minutes`), and until when it waits.
- **S-G5** On a terminal (stdin and stderr both, fzf installed) the wait is a screen titled
  `Waiting for the play lock`, with one visible choice, `Stop waiting (nothing is changed)`
  (Esc and q too, rule 14). It is drawn again when the holder changes or has held the lock
  another minute, and closes by itself the moment the lock is taken.
- **S-G6** With no terminal the wait is said on stderr, with a line each time another run takes
  the lock in turn; Ctrl-C stops it.
- **S-G7** The wait ends only in the lock taken on ccy-teams' own descriptor (flock), never in a
  check followed by a separate take.
- **S-G8** A wait that is stopped (the row, Esc, q, Ctrl-C) says "Stopped waiting for the play
  lock; nothing was changed." and the flow exits 130 where it ends the run. A wait that reaches
  its bound says it gave up and who still holds the lock, and fails (1). Each flow then says, in
  its own words, that nothing was saved and what to run once the other play run has finished:
  the settings screen keeps the unsaved changes to save again; the start-up offers (describe, a
  path limit) say they will ask again; `ccy-teams apply` names `ccy-teams apply <team>`.
- **S-G9** A play that exits 75 all the same (refused the lock) is said as that, never as a raw
  failure: "play-agent-bus.yml did not run: it was refused the play lock (exit 75, above), which
  another play run took. Once that run has finished, ccy-teams apply <team> applies it." The
  flow's own line then says the change is in the host_vars but not applied, and the board shows
  the team out of date.
- **S-G10** A lock that cannot be used (no runtime directory, one others can write, a symlink) is
  a failure naming why, not a wait.
- **S-G11** A play is never started outside the hold: one asked for without it is refused.

## Milestones (each test-first: the failing test lands before the code)

1. **M1 Descriptions declared and shown on the host** (S-A1 to S-A7, S-A13 to S-A16).
   teamfile and protocol grammar; declare `create --description`, `describe`, the 7-field
   `TEAM` line; Go `declare` parse; board cell; create wizard question; one-time picker
   offer; installer test proving no restart. No pingbus change, no container bump.
2. **M2 Descriptions reach agents** (S-A8 to S-A12). Record key, admin publish, room topic,
   `ABOUT` line, SessionStart line, skill; protocol doc; container and `CCY_VERSION` bump.
   Must deploy before any team's description is applied (Q2).
3. **M3 Replying** (S-D1 to S-D6). `REPLY` lines, short `--to`, help epilogs, skill. Ships
   with M2's container bump if ready, else its own. Independent of M1, M2 and can run in
   parallel.
4. **M4 The shared chooser, one team** (S-B1 to S-B15 without multi-select): `choose-teams`,
   `declare choices` with the `exclude` fix, `add-repo`, widening and apply, create from the
   list, ccy wired, `for-checkout --choose` and `ccy_seats_resolve` deleted.
5. **M5 Several teams** (S-B3 `--multi`, S-C1 to S-C9): marks, the launch block,
   SessionStart summary, status line handler, `send`/`say` inference.
6. **M6 cc on the shared flow** (S-E1 to S-E6): cc's list grammar, consent inside the flow,
   `cc-teams.bash` shrunk.
7. **M7 Host run and acceptance**: `meta-deploy.list` entry; the owner applies a
   description, launches from an uncovered checkout into two teams, and the S-C10 leg runs.
   qa-reviewer over the plan.
