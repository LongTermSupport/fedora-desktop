# Task 4.2 design: a human never needs a seat number (R90, R89)

Design only. Every claim about today's code cites the file and line it was read at, on `v2-smoke`.
Placeholders: `<team>`, `<checkout>`, `<repo>`.

## Today, in the code

- **What a checkout holds.** A teamchat seat lives in the owner's seat store under the checkout's
  pool, keyed by `CheckoutKey`, a hash of the checkout's real path
  (`src-go/internal/teamchat/paths/ids.go:36`, `paths.go:712`). `system.PoolSeats` lists that
  directory (`src-go/internal/teams/system/system.go:157`). The key cannot be turned back into a
  path. The paths come from `seat-checkouts`, written by the launch block and the board
  (`src-go/internal/teams/seatcheckouts/seatcheckouts.go:1-9,35`).
- **Who holds a seat.** `system.PoolState` marks a seat held when a complete holding's keeper
  runs, or its session record is kept (`system/cmd.go:149-178`).
- **What a seat ran.** One record per seat in `.claude/ccy/seat-conversations/<team>/<seat>.json`
  with `session_id`, `source` and `written_ms` (`src-go/internal/conversation/seatrecord.go:33,60-77`).
- **How the seat is chosen today.** A team named alone goes to `teamchatSeat`
  (`src-go/internal/teams/chooser/teamchat.go:85`). One free seat is taken (`:96-98`). Otherwise
  a list is shown, one row per seat, `seat <name>  <handle>  free|held`
  (`src-go/internal/teams/seatchoice/seatchoice.go:76-82`), and the default is **the lowest free
  seat** (`seatchoice.go:9,65-73`; `teamchat.go:109-111`). Nothing reads the seat records. So
  when two seats are free, the default is the lowest-numbered one, not the one this checkout last
  worked in, and the owner has to know which number that was. That is R90's defect.
- **Resume.** After the choice, `ccy_seats_resume` runs `ccy-teams internal seat-resume`
  (`files/var/local/claude-yolo/claude-yolo:893`, `lib/agent-bus-seats.bash:70-75`). It resumes
  the seats' recorded conversation, and starts a picked seat that was never used fresh
  (`src-go/internal/teams/seatresume.go:28-42`). `cc --teams` makes no such call. A grep of
  `files/var/local/claude-code/cc`, `cc-teams.bash` and `teamchatlaunch/ccrun.go` finds no
  `seat-resume` and no read of a seat record.
- **Plain `cc`** joins no team: `CC_TEAMS_GIVEN=false` unless `--teams` or `--bus` is given
  (`files/var/local/claude-code/cc-teams.bash:54-74`).
- **Restore.** The record that `ccy-sessions restore` starts is written in bash by
  `ccy_registry_write` (`files/var/local/claude-yolo/lib/tmux-session.bash:423`). Its replay
  argv holds the canonical `--teams <seat>@<team>` list (`claude-yolo:863-865`), so
  `restore --dry-run` prints a seat number in `would start <name> ...: <launcher> <args>`
  (`src-go/internal/sessions/restore.go:549`), and the session name ends in the seat
  (`docs/ccy.md:239-240`).

## 1. The detector (Defence Before Fix)

**Class:** human-facing text that asks for a seat, or shows one as the thing to type: a command
the person is told to run whose seat list is spelt out, a `<seat>` or `SEAT@TEAM` placeholder, a
seat's number used as a question row's label, or a seat number in an example.
**Hazard:** the owner has to know a seat number, which only the machine knows (R90). The launch
stops being something a person can just run.

**What counts as a seat number in text** (one regular expression, shared by both parts below):

- `--teams` followed by a list item with `@`: `<seat>@`, `SEAT@`, `S@T`, `\d+@`, `<name>@`
  where `<name>` is not the team;
- `hand-to=<seat>`, `seats conversation <seat>@`, `seats model ... <seat>@`;
- a seat-typed value spliced into text: `seatchoice.Seat.Name`, `paths.SeatName`,
  `membership.Seat.Seat`, `proto.Handle.Seat`, or a canonical seat list
  (`membership.Canonical`, `added.List`, `req.list`).

**Where it runs, in two parts, both in `src-go/qa`:**

1. A Go analyzer, `project_seat_number_text`, in `src-go/qa/rules`, built the way
   `refusaltext.go` is. It flags an argument that holds a literal matching the expression, or a
   seat-typed value, when it reaches a human sink: `say`, `note`, `usage`, `Refuse`, `Confirm`,
   `About`, `Header`, a `Gum.Choose` row, an `errors.New`/`fmt.Errorf` returned from a `Run`, or
   `fmt.Fprint*` to `Stderr`/`Stdout`. Keeper logs (`k.logf`) are not a sink (open question 2).
2. A text check, `src-go/qa/seattext`, a small Go command (bash may not grow) run by
   `qa-changed.bash` and `qa-all.bash`. It runs the same expression over `docs/**/*.md`, the
   embedded help (`src-go/**/help.txt`, `usage.txt`), and the `print_error`/`echo`/`printf`
   lines of the launchers under `files/var/local/claude-{yolo,code}/`. Comments are skipped.

**Red proof.** Fixtures under `src-go/qa/rules/testdata` hold one instance of each shape
(`--teams <seat>@`, `"seat "+s.Name` in a row, `--teams "+list`) and one clean message. The text
check is run on today's tree and must list every hit below. Only then is it made blocking.

**Sweep list, today's tree** (from greps of the same shapes; the detector's own run is the final
list).

*Go, a seat to type, or a seat as a row label:*

- `teams/seatchoice/seatchoice.go:81` (row), `:132`, `:138`, `:158`, `:162`, `:164`
- `teams/chooser/teamchat.go:97`, `:155`; `chooser/chooser.go:105`; `chooser/consent.go:264`;
  `chooser/summary.go:127`, `:221`
- `teams/choose/choose.go:85` (row), `:115`, `:278`, `:349`, `:361`
- `teams/seatresume.go:183`, `:215`, `:236`, `:267`; `teams/seatconversation.go:14`, `:76`;
  `conversation/seatrecord.go:86`, `:294-296`
- `teams/teamchatlaunch/launch.go:329`; `teamchatlaunch/verbs.go:348`, `:406`;
  `teamchatlaunch/hotseats.go:192`
- `teams/seats/seats.go:99`, `:168` (`Seat <n>@<team>   new`), `:206`, `:233`, `:288`
- `teams/picker/sessions.go:292`, `:372`, `:466`; `picker/addrunning.go:115-117`
- `teams/seatadd/seatadd.go:163`, `:175`, `:182`, `:323`
- `teams/hot/role.go:236`, `:312`, `:315`; `hot/move.go:71-72`; `hot/hot.go:341`;
  `hot/add.go:297`, `:498`; `hot/texts.go:21`, `:25`
- `teams/seatmodel/seatmodel.go:223`, `:312`, `:353`; `teams/membership/membership.go:138`
- `teams/shutdown/shutdown.go:363`; `teams/winddown/winddown.go:440`; `teams/board/table.go:118`
- `sessions/restore.go:549` (the replayed argv)

*Help and launcher text:*

- `src-go/internal/teams/usage.txt:7`, `:293`, `:327`; `src-go/internal/ccylaunch/help.txt:51`, `:55`
- `files/var/local/claude-yolo/claude-yolo:243`, `:247`, `:752`, `:887`
- `files/var/local/claude-code/cc-teams.bash:66`

*Docs:*

- `docs/ccy.md:239`, `:250`, `:1142`, `:1255-1259`, `:1467`, `:1490`
- `docs/agent-bus.md:110`, `:615-616`, `:624`, `:717`, `:727`, `:760`, `:776`, `:796`, `:816`
- `docs/agent-bus-protocol.md:128`, `:839`; `docs/agent-bus-lxc-members.md:531`

`docs/ccy-changelog.md` (10 hits) is history and is not rewritten (open question 3).

## 2. Launch: the seat this checkout holds, resumed

One decision in Go, `seatchoice.Pick`, replaces `Default` (lowest free). It is shared by
`ccy --teams <team>`, a bare `ccy --teams`, `cc --teams`, and plain `cc` if the owner agrees
(open question 1). It reads three things the code already has: the pool
(`system.PoolSeats`), the holdings (`system.PoolState`), and each seat's record
(`conversation.readSeatRecord`, `seatrecord.go:246`).

**"This checkout holds a seat"** means a pool seat of the launch's kind exists under this
checkout's key. **"Its recorded conversation"** is the seat record's `session_id`, and that
conversation's transcript is in this checkout (`TranscriptOf`, `seatrecord.go:317`). Last ran is
the record's `written_ms`. A record whose transcript is gone counts as no record, and that is said.

| This checkout holds, of the launch's kind                   | What happens                                                                                                                                                                         |
| ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| no seat                                                     | Today's `newSeat` (`teamchat.go:140`), said in words: "This checkout has no seat in `<team>` yet: adding one." R91's cross-machine case is designed with it (open question 4)        |
| one seat, free                                              | Taken, its conversation resumed, no question (as today, `:96-98`)                                                                                                                    |
| several free, exactly one with a conversation               | That one is taken and resumed, and the choice is said in a line                                                                                                                      |
| several free, none ever used                                | One is taken: they are the same, so there is nothing to choose                                                                                                                       |
| several free, two or more with conversations                | A genuine choice: asked in words (below)                                                                                                                                             |
| the seat with the conversation is held by a running session | That session is re-attached (`chooser.reattach`, `teamchat.go:154`), said in words. If it is open in another terminal, the person is asked: take it over here, or start another seat |
| every seat held                                             | Asked: re-attach which session, in words, or start another seat                                                                                                                      |

**A bare `ccy --teams`** first takes the team: when this checkout's pool holds seats of exactly
one team (`system.PoolTeams`, `system.go:177`), that team is used and the list of teams is not
shown. With seats in several teams, the person is asked, each team shown with what it is for and
when this checkout last ran in it. With none, today's team list (`chooser/list.go`).

**Asking in words.** One row per seat, with no seat name and no handle:

```text
The coordinator of <team>: last ran 2 hours ago, "<conversation title>"
A worker in <team>: last ran yesterday, "<conversation title>"
Running now in another terminal (worker): re-attach it
Start a new seat in <team>
```

The role is the seat's member role, which the chooser already reads (`choose/choose.go:349-361`).
The title is the conversation's first prompt, cut to one line (open question 5). The tag Gum
returns maps back to the seat, as `seats.go:168` does today, so the seat name stays inside the
program.

**Unattended** (no terminal, a restore, a restart: `claude-yolo:874-875`). `Pick` decides without
asking every case except the genuine choice. That case refuses, with each seat described in
words and the one command that asks: run `ccy --teams <team>` at a terminal.

**`--teams <seat>@<team>` stays accepted** as input, for scripts and records. Nothing tells a
person to type it.

## 3. R89: one restore for every seat on a machine

**Command:** `ccy-sessions restore`, extended, with no new command (open question 6). The boot
unit already runs it, and it already has `--dry-run` and per-name `NAME...`
(`docs/ccy.md:322-341`). Restore then has one route, and the docs name one command.

**What it adds.** After the session records, a seat pass:

1. Every checkout in `seat-checkouts`. For each, hash it to its `CheckoutKey` and read its pool
   seats, holdings and seat records.
2. A seat with a recorded conversation that no running session holds, and that no restored
   session record already covers (matched by the seats in its replay argv,
   `tmux-session.bash:182`), is started as the plain launch: `ccy --teams <team>` (or `cc`) in
   its checkout, detached. The launch runs `Pick`, so the same seat and conversation come back,
   and the seat stays out of the argv a person reads.
3. A pool key with no checkout in `seat-checkouts`, a record whose transcript is gone, and a
   genuine choice are each named as not started, with the reason and the command that fixes it.

**Output, same words.** One line per seat, in words:

```text
would start the coordinator of <team> in <checkout>, resuming "<conversation title>" (last ran 2 hours ago)
would not start a worker of <team> in <checkout>: its conversation's transcript is gone; run ccy --teams <team> there to choose
```

`restore.go:549` stops printing the replay argv. The session name keeps its seat
(`docs/ccy.md:239`), because tmux needs a unique name, but it is shown only beside the words, never
as something to type (open question 7).

**Relation to the other routes.** The per-seat resume is still what `ccy --teams <team>` does in
one checkout, and restore runs exactly that. `ccy-teams restart` stays the v1 team restart
(`docs/agent-bus.md`). Why the session records were missing (R89, third fix) is a separate
finding, outside this design: the seat pass does not need those records.

## 4. Docs and errors rewritten

- Every Go and help line in the sweep list. A command shown to a person is `ccy --teams <team>`,
  `cc --teams <team>` or the picker (`ccy-teams`). A seat is named by its role and team. The
  conversation repair `seats conversation --set` takes `<team>` and asks which seat, in words,
  when there is more than one.
- `seatchoice.WaysOn` / `WrongKind` / `NewRow` speak in roles, and their way-on lines are the
  plain launch.
- `docs/ccy.md`: the session-name section and the examples at `:1467`, `:1490` show
  `ccy --teams <team>`. "Team seats resume their conversation" (`:1255`) says the seat is found by
  itself. The reboot section names `ccy-sessions restore` as restoring sessions **and** seats,
  first.
- `docs/agent-bus.md` `:615-616`, `:776`, `:796`: naming a seat moves to a "scripts and
  records" note. The launch examples are by team.
- `ccylaunch/help.txt:55` and `claude-yolo:247`, `:752`, `:887`, and `cc-teams.bash:66` drop the
  seat example. The no-terminal refusal says "run it at a terminal".

## 5. Open questions for the owner

1. **Should a plain `cc` in a checkout holding a host seat join it?** Today it joins nothing
   (`cc-teams.bash:54`). *Recommend yes:* the rule names `cc`, and a checkout with a seat means
   the person wants it. `cc --no-teams` would opt out.
2. **Logs and agent-facing text.** Keeper logs (`teamchat/member/keeper/*.go`, `k.logf`) and
   `docs/agent-bus-protocol.md` name seats for debugging and for agents. *Recommend:* out of the
   detector's scope, declared in the analyzer's doc. Narrowing the rule is your decision.
3. **The changelog** (`docs/ccy-changelog.md`) records past commands. *Recommend:* exempt, as
   history.
4. **R91, no seat here because the team is served on another machine.** *Recommend:* designed
   with this task. `cc` says in words that the seat lives on `<machine>` and offers to fetch it,
   with your password once, no number.
5. **The words for a seat:** role, last ran, and the conversation's first prompt. *Recommend
   yes.* The alternative, a purpose you write per seat, is a new field to keep up to date.
6. **One command:** extend `ccy-sessions restore`, or add a new `ccy-teams restore`.
   *Recommend extending,* so the boot, the docs and you all use one command.
7. **Session names** (`ccy-<team>-<project>-<seat>`) appear in tmux tabs and `ccy-sessions`.
   *Recommend:* use the role in place of the number where the role is unique in the checkout
   (`ccy-<team>-<project>-coordinator`), and keep the number only to tell two workers apart.
