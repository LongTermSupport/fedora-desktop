# Task 5.1: host `cc` members on v2 (seam 56)

The design of PLAN.md Task 5.1. A team's host seat is a `cc --teams` session on the host. Today
that launch is agent-bus (v1) only, so no team with a host seat can leave v1. This document says
how a host `cc` launch joins a teamchat (v2) team, what it reuses from the ccy launch, the
conversation-record defect class it must fix first, the build in test-first order, and the
acceptance on the host.

Every claim is marked **FACT** (read in code or plan text, with file:line), **INFERENCE** or
**UNKNOWN**. Paths are relative to the repository root.

## 1. How a host `cc --teams` launch works on v1 today

1. **FACT.** `cc` (`files/var/local/claude-code/cc`) moves to the git top level
   (`cc:42-43`), re-executes itself inside tmux and keeps a session-registry record with prefix
   `cc` (`cc:60-75`), then sources `cc-teams.bash` and splits `--teams` / `--bus` from claude's
   arguments (`cc:88-91`).
2. **FACT.** `cc_teams_prepare` (`cc-teams.bash:152-168`) refuses unless the v1 kit
   (`/usr/local/share/agent-bus/kit`), `agent-bus` and `ccy-teams` are installed
   (`:155-159`). It then runs `ccy-teams choose-teams --type=host` and `agent-bus seat check`
   (`:87-93`), makes `<checkout>/.claude/ccy/` (`:108-118`), runs `agent-bus seat take --type=host` (one `sudo agent-bus add-member` per new seat) and `describe-seats --block`
   (`:97-101`).
3. **FACT.** Settings: the v1 kit's `settings.json` merged by `jq` with cc's Matrix-client deny
   rules, `cc-teams-settings.json` (`:124-128`).
4. **FACT.** The command (`:135-148`): `ccy-teams internal membership-init --kind=cc` writes
   generation 1 of the desired membership under `$XDG_RUNTIME_DIR/pingbus/membership/<session>/`,
   then `env PINGBUS_MEMBERSHIP=<dir> <kit>/pingbus seat exec --root <checkout>/.claude/ccy/pingbus/seats --home <tmp>/pingbus-home -- /var/local/claude-code/cc-claude --plugin-dir <kit>/plugin/pingbus --settings <tmp>/settings.json --append-system-prompt <line> <claude args>`. The session name is
   the tmux session, or `cc.<pid>` outside tmux (`:138-140`).
5. **FACT.** `cc` does not `exec` the command, so its EXIT trap can restore a parked credential
   and remove the private directory (`cc:173-188`, `:375-385`). `pingbus seat exec` holds the
   seat for the session and is the pingbus watcher; the stop hooks are the pingbus plugin's.
6. **FACT.** The record ends with the session: the tmux pane's trampoline removes the registry
   record when `cc` returns, and keeps it when the pane is killed (reboot), so a restore can
   relaunch it (`files/var/local/claude-yolo/lib/session-registry.bash:595-616`).
7. **FACT.** The conversation record is v1's
   `<checkout>/.claude/ccy/pingbus/sessions/<session>/conversation.json`, written by the pingbus
   SessionStart hook (`src-go/internal/conversation/conversation.go:1-9`, `:89-118`). For a `cc`
   record the checkout is the git top level around its folder (`:120-134`).
8. **FACT.** Restore does not answer cc's token chooser; it only clears the restore marker, so a
   restored cc waits at the chooser for a person (`cc:77-83`).
9. **FACT.** `cc` and `cc-teams.bash` are on the bash ratchet (`scripts/bash-limits.allowlist:46-47`):
   they may shrink, never grow.

## 2. How a ccy launch on v2 works today, and what a host launch reuses

**FACT, the ccy v2 launch** (`LAUNCH-ccy.md:27-41`, `:61-103`):

- The seat is a pool seat placed by `teamchat seat place-issued --checkout <dir>`, keyed on the
  checkout's real path (`src-go/internal/teamchat/member/cli/place.go:24-28`, `:60-62`).
- `ccy-teams internal launch-args` resolves the system (`teamchatlaunch/verbs.go:150-166`), and
  for teamchat runs `Launch.Run`: guard the mounts, choose and take every seat or none, write
  `<store>/control/<session>/desired.json` (`teamchatlaunch/launch.go:212-238`).
- `RunArgs` binds `<store>/sessions/<session>` read-write at `/run/teamchat/seats` and
  `<store>/control/<session>` read-only at `/run/teamchat/membership`, with
  `PINGBUS_MEMBERSHIP=off` and `TEAMCHAT_MEMBERSHIP` (`launch.go:178-192`; L1).
- Inside, `ccy-start-claude` execs `teamchat run --claude --kind ccy --desired /run/teamchat/membership -- <wrapper> claude ...` (`LAUNCH-ccy.md:27-33`).
- The exit is `internal bus-disconnected`: a numeric status other than 75 removes the record,
  then releases each seat; 75 keeps them (RS3); no status announces and keeps (`verbs.go:253-345`;
  L4). A relaunch or restore keeps its seats (L5). The owner's seat sweep releases a seat whose
  session is over (`LAUNCH-ccy.md:105-136`).

**Reused unchanged by a host launch (INFERENCE from the code above):** the system decision
(`ccy-teams system`, `system.One`'s refusal of a mixed list, `system/system.go:126-140`), the
pool, `Launch.choose` and the all-or-none take with L5's skip of a seat the session already holds,
`Desired`, the exit's `end` (record removed, then release; signal case kept), the seat sweep and
its units, `seat-check` and `seat-take` (already system-aware, `verbs.go:217-250`) and
`describe-seats --block`.

**What differs on the host:**

| Point                    | ccy                                                                         | host `cc` on v2 (this design)                                                                                                                                                                               |
| ------------------------ | --------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Membership               | control dir mounted read-only at `/run/teamchat/membership`                 | `--desired <store>/control/<session>` directly; no mount, so no mount guard (FACT: same uid reads the whole store; INT6 accepts that, PLAN.md:109)                                                          |
| Seat directory (state)   | `<store>/sessions/<session>` mounted at `/run/teamchat/seats`               | `--state <store>/sessions/<session>` directly (decision H1, below)                                                                                                                                          |
| Runtime and inbox socket | `/tmp/teamchat/<run>` in the container                                      | `$XDG_RUNTIME_DIR/teamchat/run/<run>`; the keeper refuses without `XDG_RUNTIME_DIR` (FACT: `teamchat/member/keeper/keeper.go:196-206`)                                                                      |
| Kit                      | `/opt/claude-yolo/optional/teamchat` in the image, with plugin and settings | `/usr/local/lib/teamchat/kit/current`; **FACT: holds only `bin/teamchat`** (`play-teamchat.yml:409-440`, `:466-475`); `run --claude` refuses a kit with no `settings.json` (`member/agent/agent.go:96-112`) |
| Wrapper                  | ccy supervisor, `<supervisor> --arm -- claude`                              | `cc-claude`, which has no wrapper form: every argument goes to claude (FACT: `internal/ccclaude/app.go:8-9`); `run --claude` needs an argument whose base name is `claude` (`member/cli/launch.go:211-215`) |
| Settings                 | the kit's, written by `run`                                                 | the kit's plus cc's deny rules; `run` refuses `--settings` in CMD (FACT: `member/cli/launch.go:198-200`, `:216-219`)                                                                                        |
| File ownership           | podman maps the owner                                                       | INFERENCE: the owner's uid throughout; the store and the session dir are the owner's 0700 (checked by `homefs Private` on `--state`, `paths/paths.go:497-509`)                                              |
| Network to the team      | pasta and the checkout's network policy                                     | INFERENCE: the host's own routing; the bundle pins the team CA. UNKNOWN: whether each team's declared member CIDRs admit the host's source address. The acceptance checks it with `teamchat reach`          |
| Relaunch status 75       | RS3 keeps the seats                                                         | no supervisor, so no 75: any numeric status ends the session (FACT: SPEC-n3.md:1033-1038)                                                                                                                   |

## 3. What seam 56, D 3.9 and gate B require

- **FACT.** Seam 56: "Host `cc` members on v2 ... refused until defined (D 3.9); B's `run` covers
  the kind" (DESIGN-v2.md:177).
- **FACT.** D 3.9: host `cc --teams` resolves systems as ccy does, and refuses a v2-live team
  (64) until the host-member launch exists; M0 refuses to migrate a team with a host member
  (DESIGN-v2-D-migration.md:363-365, :1400).
- **FACT.** B's member table gives a host `cc` member one state root and one desired root per
  user (`~/.config/teamchat/seats`, `~/.config/teamchat/membership`, DESIGN-v2-B-member.md:16-22;
  `paths.HomeLayout`, `paths/paths.go:447-464`), and its end table the host rows
  (DESIGN-v2-B-member.md:2014-2018).
- **FACT.** Gate B: a host member's seat is a pool seat. The owner chose INT6 on 2026-10-30:
  owner-uid processes may hold a pool seat's password (PLAN.md:109). Gate B is met; PLAN.md:447-449
  still reads as if it were not, and is corrected with this task.

**Decision H1. A host member uses the seat store's per-session directories, not B's per-user
roots.** B's one root per user allows one host session per user, and the v2 conversation record
lives in the state root (`paths.Layout.ConversationFile`, `paths/paths.go:526-527`), so two cc
sessions would share one record. The pool seat model (gate B) and the seat sweep work on
`<store>/sessions/<session>` and `<store>/control/<session>`. So the launch line is
`teamchat run --claude --kind host --state <store>/sessions/<session> --desired <store>/control/<session> -- /var/local/claude-code/cc-claude -- claude <args>`. FACT: `run`
accepts `--state` with `--desired` for a non-LXC kind (`member/cli/launch.go:144-197`), and the
keeper passes `--state` to the link (`keeper.go:243-251`). B's table row changes with this task.

**Decision H2. The session must be a registry session.** A cc outside tmux is `cc.<pid>` with no
registry record (`cc-teams.bash:138-140`). The sweep releases a seat whose session is over, judged
by the registry; INFERENCE: a pid-named session could have its seat judged over while it runs.
A v2 host launch with no tmux session name is refused (78), naming the tmux insulation.

## 4. The conversation-record defect class (phase6-readiness Q2.3)

**FACT.** Every host reader of a session's conversation goes through `internal/conversation`:
`sessclose/sessclose.go:244` (`ccy --stop`, shutdown's close), `sessions/restore.go:92` and
`:439` (restore and its check), `teams/restart/jobs.go:242` (team restart),
`teams/winddown/compact.go:68` (wind-down). A sweep for `conversation.json` across `src-go`,
`files`, `helpers` and `scripts` finds no other host reader. `conversation.read` reads only v1's
file (`conversation.go:89-118`). The v1 file is written only by the pingbus hook, and not under
`PINGBUS_MEMBERSHIP=off` (phase6-readiness.md:97-104). A v2 session writes its record to
`<state root>/conversation.json` (`member/link/facts.go:407-414`), which for ccy is the host's
`<store>/sessions/<session>/conversation.json` (the bind, `launch.go:187`), and for a host member
under H1 is the same path. **So no host reader can stop, restart or restore a v2 session with its
conversation**, ccy or host.

**The class:** a host reader of a session's conversation that knows only one system's record.
**The hazard:** a restore resumes a stale conversation or starts fresh; a close judges compaction
against the wrong transcript or fails.

**The fix, by Defence Before Fix** (`defence-before-fix:dbf`, rule catalogue in
CLAUDE/GoProjectRules.md):

1. **Detector:** Go analyzer `project_conversation_record` in `src-go/qa/rules`: outside the one
   locator `conversation.Locate`, no production code names a conversation record's path:
   `conversation.File`, a `"conversation.json"` literal, or `membership.SessionDir` joined with
   either. The member's own writer and reader (`teamchat/member/link`, `member/agent`,
   `member/state`, `teamchat/paths`) are the record's owner and are outside the rule's scope. Red
   on today's `conversation.read`, which joins `membership.SessionDir` and `File` itself
   (`conversation.go:97`). INFERENCE: the analyzer holds the single-reader shape; that the locator
   knows both systems is held by step 2's tests, which the rule's doc says.
2. **Locator:** `conversation.Locate(rec, sys)` returns the record's path and codec by the
   record's live system, decided by `system.Resolve` + `system.One` over the record's `--teams`
   list in its checkout (seam 55, the one decision): teamchat gives
   `paths.Store.Session(<name>)/conversation.json`, decoded by `state.DecodeConversation`
   (its `session_id` checked by `registry.SessionID`; the directory is the session's, so the name
   check is the path); agent-bus, or no list, gives v1's file as today. A mixed list is the 64
   refusal. No fallback from one file to the other: a missing v2 record is `errNotRecorded`, so
   StartPlan's fresh-if-no-conversation rule applies unchanged (`start.go:26-50`).
3. Sweep: the five callers above take the fix through `read`; none changes.
4. The rule is blocking in the Go gate, then the original is fixed (step 2).

This covers ccy v2 sessions as well as host ones; it is the prerequisite readiness step 3 names
(phase6-readiness.md:337).

## 5. Mixed lists

**FACT.** Task 4.5(a)'s mixed-list dual launch is not built: what CCY 3.122.22 built is only that
a list with no v1 team needs no `agent-bus` (PLAN.md:425-429; `system.One` still refuses a list on
both systems, `system/system.go:132-137`). **Decision H3: 5.1 refuses a mixed list (64) with
`system.One`'s text, for host as for ccy.** The owner is retiring v1, and the host seat moves to
v2 teams only. If Task 4.5 is built for ccy later, the host follows only if the owner asks.

## 6. The build, in order, test first

The bash files may not grow, so every new decision is Go behind command words the bash already
calls. Each step: the failing test first, then the code.

**Build status (branch `v2-host-cc`):** steps 1 to 10 built (CCY 3.124.1, container 2.84.51; step 9's merge of `origin/v2` found it already in the branch); the acceptance (section 8) is the host's. Step 10 as built: the host-lane deny rules listed under step 10. Step 7 as built: the keeper writes `<run>/state-root` (one absolute path) for a run with `--state`, and `reach` reads the bundle below it. Step 6 as built: a host launch lists and takes teamchat pool seats as podman does, and the Matrix consent names a teamchat seat by its pool handle (`Teamchat.Handles`, `system.Cmd.Handles`), not by `agent-bus seat choose`. Step 5 as built: `internal cc-run <tmp> <checkout> <session or empty> <cc pid> <seats> <bus> -- <claude args>`; cc-teams.bash calls `internal seat-check` and `seat-take` (system-aware) in place of agent-bus, and on teamchat the seats are taken by cc-run as the session starts, after the token chooser.

01. **The conversation class (section 4).** Test: the analyzer's fixture red on a reader that joins
    `SessionDir`+`File`, green through `Locate`; `conversation_test.go` table: {ccy, cc} × {agent-bus,
    teamchat, mixed, no list} × {record present, missing, wrong session id, no transcript}; the five
    callers' tests gain a v2 row (sessclose's with the real `Transcript`, not the fake closer
    `teams/shutdown/v2host_test.go:164` uses). Code: `src-go/qa/rules/conversationrecord.go`,
    `qa/rules/table.go`, `internal/conversation/conversation.go`, CLAUDE/GoProjectRules.md row.
02. **The host kit holds its plugin and settings.** Test: an installplay test that the `member` tag
    assembles `<kit root>/<digest>/` with `kit.Assemble` (FACT: no production caller today,
    `member/kit/kit.go:75-78`), and a kit test that the assembled hooks name
    `/usr/local/lib/teamchat/kit/<digest>/bin/teamchat`. Code: a `teamchat kit assemble --into DIR`
    verb (root, run by the play into the new digest directory before the `current` link moves),
    `playbooks/imports/play-teamchat.yml`. INFERENCE: LXC guests copy this tree
    (`host/lxc/kit.go:52-60`), so an LXC `--claude` member lacks the plugin today too; the coordinator
    is told, and the LXC acceptance (Task 3.4) checks it.
03. **`run --extra-settings FILE` (decision H4).** Test: `member/cli/launch_test.go` (only with
    `--claude`; absolute; once), `member/agent/agent_test.go` (FILE's object merged under the kit's,
    kit keys win; FILE holding `crossSessionInbound`, `hooks` or `statusLine` refused 78; a link or
    another user's file refused). Code: `member/cli/launch.go`, `member/agent/agent.go`,
    `member/keeper/prepare.go`. This changes the image's `teamchat`, so it is a container bump.
04. **`cc-claude -- claude <args>` (decision H5).** Test: `internal/ccclaude/app_test.go` C6: a
    first argument `--` must be followed by `claude`, and the rest is exec'd from there; anything
    else after `--` is 64; C1 unchanged otherwise. Code: `internal/ccclaude/app.go`.
05. **`ccy-teams internal cc-run` (both systems, one command word).** Test first in
    `teamchatlaunch` (or a new `teams/cclaunch`): (a) agent-bus: the argv and environment are
    byte for byte today's `cc_teams_settings` + `cc_teams_command` (a parity test against
    `cc-teams.bash` as frozen testdata, as `ccystart/parity_test.go` does); (b) teamchat: refuse
    without a registry session name (H2, 78); take through `Launch.Run` with no mounts; run
    H1's line as a child, with `--extra-settings <tmp>/cc-deny.json` before its `--` and the
    prompt line naming teamchat after `claude`; on its return run `end` with its
    numeric status (record removed, seats released); on SIGHUP or SIGTERM forward to the child, wait,
    and keep the seats (the signal case); (c) a mixed list is 64; (d) a relaunch of the same session
    (restore) takes no seat twice (L5). Code: the verb, `teams/usage.txt`, the dispatcher.
    `cc-teams.bash`: `cc_teams_settings` and `cc_teams_command` become one call of the verb
    (shrinks), and the kit check at `:155-159` moves into the verb per system (as 4.5(a) did for
    ccy). `cc`: the final line runs `CC_TEAMS_CMD` as now (no new line). Update the allowlist rows
    to the new, smaller counts.
06. **The chooser offers teamchat teams to a host launch.** Test: `chooser/teamchat_test.go`: a
    `--type=host` launch lists this checkout's teamchat teams and takes a pool seat as podman does.
    Code: `teamchatFor` (`chooser/teamchat.go:30-37`) returns the side for host too; the doc comment
    at `:15-18` changes. The Matrix-client consent stays asked for host on both systems
    (`chooser/consent.go`).
07. **`teamchat reach` inside a `--state` session.** FACT: `reach` reads the bundle from the home
    layout, ignoring the run's `--state` (`member/client/reach.go:63-75`). Test: a host run under
    `--state` answers `reach --team T` from that root. Code: the keeper records its state root in the
    run directory and `stateLayout` reads it there.
08. **Docs and plan text** (same commits as the code they describe):
    - `docs/ccy.md` (the `cc --teams` section, about :756-800, and the restore and `--stop` text);
    - `docs/agent-bus.md` (host members on v2), `docs/agent-bus-cross-network.md` (issuing a host
      seat: `--prefix <repo>+<site>.host`, `place-issued --checkout` at the cc checkout);
    - `LAUNCH-ccy.md` (a "host launch" section pointing here), DESIGN-v2-B-member.md:16-22 (H1),
      DESIGN-v2.md:177 (seam 56 built), DESIGN-v2-D-migration.md:363-365 and :1400, PLAN.md Tasks
      4.3, 5.1 (gate B met) and 6.4's list;
    - the `cc-teams.bash` header comment.
09. **Version bumps.** `CCY_VERSION` in `files/var/local/claude-yolo/claude-yolo` (now
    3.122.50) to the next minor with a `docs/ccy-changelog.md` entry, because ccy-teams, ccy-sessions
    and cc change; the container version too, because step 3 changes the image's `teamchat`. The
    meta-deploy list gets `play-teamchat.yml --tags member` and `play-claude-yolo.yml` in the same
    commit as the code that needs them.
10. **The host seat's lane, static half** (Plan 00263 owner decision D3, its
    [DESIGN.md](../00263-machine-qa-queue/DESIGN.md) section 3). cc's deny file,
    `files/var/local/claude-code/cc-teams-settings.json`, which cc-run hands to both systems (the
    teamchat extra settings, the agent-bus merged settings), denies a host seat the tests,
    builds, lint and QA that belong in a container: `go test`, `go build`, `go vet`,
    `golangci-lint`, `*scripts/qa-*.bash*`, `*run.bash qa*`, `npm test`, `npm run test`,
    `pytest` (and `python -m pytest`, `python3 -m pytest`), `make`, `podman build`,
    `ansible-playbook` (a host seat deploys through `meta-deploy.bash`) and `semgrep`; each
    plain command as `Bash(<cmd>)` and `Bash(<cmd> *)`. Held by `hostlane_test.go` in
    `teams/teamchatlaunch` (each rule in the file, and in what cc-run assembles on each system).
    Deny rules match the command text only (not `sh -c`, an env prefix or another path); the
    duty-aware check is the hooks daemon's handler (D3's option A), not this step. Plain `cc`
    without `--teams` gets no deny file, as before.

`cc --bus` (a session on no team) stays v1 only: a v2 launch names its seats
(`launch.go:247-249`). See Q1.

## 7. Open questions for the owner

- **Q1. `cc --bus` on v2.** It starts a host session on no team for teams to be added later. v2
  refuses a launch with no seat. Recommended: drop `cc --bus` with v1's retirement (Task 6.5), and
  start a host session with its seats named.
- **Q2. The acceptance team.** Recommended: a scratch v2 team declared and approved for the drill
  (DC18), with one ccy seat to round-trip with, removed afterwards; not a working team.

Defaulted, not asked: H1-H5 above; the consent is kept for host v2 seats.

## 8. Acceptance on the host

Run by the host seat, after steps 1-9 are deployed (meta-deploy). `cc` needs a terminal and a
token choice (`cc:45-49`, `:236-260`), so the launch is attended: the host seat starts it in its
own tmux and answers the chooser, or the owner does. A plan script,
`acceptance-host-cc-v2.bash`, does the read-only checks before and after and is queued in
`meta-deploy.list` as `00209-agent-bus-v2-in-go/acceptance-host-cc-v2.bash <team> <checkout>`. It
exits 2 (could not establish) where a step needs the session.

1. **Before (script, read-only):** `teamchat version` and `/usr/local/lib/teamchat/kit/current/`
   holds `settings.json` and `plugin/teamchat/.claude-plugin/plugin.json`; the deployed `ccy` script's `CCY_VERSION` is the
   bumped one; `ccy-teams system <team>` run in `<checkout>` prints `teamchat`;
   `teamchat-admin status --team <team>` (or the status record) shows the host handle `enrolled`.
2. **Seat** (host seat, once): `member add --prefix <repo>+<site>.host --seat 1 --credential password` piped to `teamchat seat place-issued --checkout <checkout>`, as
   docs/agent-bus-cross-network.md:116 shows.
3. **Launch:** in `<checkout>`, `cc --teams 1@<team>`; choose the token. The session's status line
   shows the team; `teamchat reach --team <team>` from its Bash tool exits 0.
4. **Round trip:** the host session posts to the ccy seat and the ccy seat answers;
   `teamchat history --team <team>` on both sides shows both directions.
5. **Record:** the script checks `<store>/sessions/<session>/conversation.json` names the session id
   of the newest transcript the session wrote.
6. **Stop:** `ccy --stop <session>`; the session compacts and exits; the script checks the seat is
   released (`ccy-teams seats sweep` prints nothing for it, the pool seat free) and the registry
   record is gone.
7. **Restore:** `ccy-sessions declare --name <session> --replace <checkout> -- --teams 1@<team>`,
   then `ccy-sessions restore --dry-run` (only this record starts), then `ccy-sessions restore`;
   answer the token chooser in its pane. The resumed session recalls a fact told it before step 6,
   and posts once more on the team.
8. **Kill:** `tmux kill-session -t <session>`; within a minute the seat is announced and kept while
   the record remains, then released once `ccy-sessions` forgets it.

Pass: every step's check holds. Fail at any step: stop, record the failure in the plan's JOURNAL,
and fix in code (no hand repair on the host).
