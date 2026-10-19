# Fable review: Task 3.8, add a running session from the team and seat menus

Branch `feat/teams-add-session-and-restart` at 92cf3a14 (one commit over c277edfd). Reviewed
against SPEC-team-add-and-restart.md A1-A10; Tasks 3.9-3.11 are spec-only and were read for
soundness (section 3 below), not for code.

## 1. QA run (targeted)

| Check                                                              | Result                                            |
| ------------------------------------------------------------------ | ------------------------------------------------- |
| `go test ./internal/teams/...`                                     | ok, all 22 packages                               |
| `go vet ./internal/teams/picker/`                                  | clean                                             |
| `scripts/qa-hidden-bindings.bash`                                  | HIDDEN-BINDINGS-OK                                |
| `scripts/test-ccy-teams-command.bash` (PINGBUS\_\* unset)          | passed 381, failed 0                              |
| `git merge-tree --write-tree F44 92cf3a14` (F44 = 97537444, local) | **CONFLICTS** in PLAN.md and the 26-10-19 journal |

## 2. Task 3.8 findings

### What is right

- **One flow.** `addToTeam` (sessions view) now stops after the team step and hands to
  `addSeatAndRole`; `addRunningTo` (team menu) asks the session then calls the same
  `addSeatAndRole`; `addThisSession` (seat menu) calls `addToTeam` itself. The seat question,
  the role question, the coordinator takeover logic and the `session add` argv are written
  once. The "already in" wording moved into `alreadyIn` and `teamOptions` uses it too.
- **Eligibility, team menu.** A session already in the team is refused with the full handle
  (`seatLabel`), a non-controllable one with its state; both are refusals, so they are listed
  and explained, not hidden. The test drives a refused pick then an allowed one, exercising
  the bounded re-ask.
- **cc host sessions are covered**, which answers the owner's "any member equivalent where it
  applies": `hot.Sessions` lists cc sessions (`runningCC`), `Snapshot.Live` is tmux's list and
  the registry records cc launches (`discover.IsLauncher` is `ccy` or `cc`), and a cc seat's
  board row is `KindCcy` with a `Record`. So a running cc member's row gets "Add this session
  to another team" and the team menu lists it. A10's wording "a session on ccy's tmux" is
  accurate for cc as well, since cc runs there.
- **LXC members** have `Kind` set to their type word, not `KindCcy`, so `addActions` gives
  them nothing. Correct.
- **Full handles and `ui.Heading`.** `sessionLine` and `alreadyIn` use `seatLabel`;
  headings are `flow.Here(team, addRunningCrumb).AtStep(..).About(..)`; the test pins all four
  screens' crumbs and context lines.
- **Visible rows, no key.** Both actions have `key == ""`, so they are menu lines and footer
  words only. The team footer pin in `picker_test.go` was updated deliberately.
- **Order.** `actionsFor` inserts `[lxc, adds...]` right after `actNew`; Shut down, Take
  down and Remove seat stay after. Tests assert both bounds. Nothing destructive is default.
- **The "explains why" row is not a dead end.** With no session, or none eligible,
  `k.Note` says why for each and returns to the list; no question is asked (test asserts
  `Rows` is empty).

### Must fix before merge

- **F1. The merge into F44 is not clean.** F44 (97537444) merged `fix/lxc-checkout-typed`
  after this branch forked from c277edfd, and that merge touched the same plan files: PLAN.md
  (content conflict) and `JOURNAL/00203-Journal-26-10-19.md` (add/add: both sides created the
  day-file). The Go files merge cleanly. Fix: merge F44 into
  `feat/teams-add-session-and-restart`, keep both sides' PLAN.md task entries and both journal
  entry sets in time order (the F44 side's entries are earlier in the day), and run
  `go test ./internal/teams/picker/` after.
- **F2. help.txt documents a menu line that does not exist.** The new paragraph "Restart the
  team's desktop sessions is in a team row's menu (`ccy-teams restart <team>`) ..." describes
  Task 3.11, which is spec-only on this branch. A user pressing `?` is told of a line and a
  command that are not there. Remove the paragraph from this commit; it goes in with 3.11.
- **F3. The team menu's add skips the team checks the sessions view makes.** `teamOptions`
  refuses a team that is not installed (`!r.snap.Loaded[team]`) or whose homeserver is down
  (`snap.Active`); `addRunningTo` has the team already and makes neither check, so from a
  down team's row the owner answers three questions and then `ccy-teams session add` fails at
  `teamUp` ("homeserver for `<team>` is down"). Not a silent failure, but A4/A8 promise the
  same refusals by the same code, and the picker already holds the facts. Fix: before listing
  sessions, `k.Note` the same two sentences `teamOptions` uses when the team is not loaded or
  not up, and return; one test each. (A shared helper `teamRefusal(team) string` used by both
  `teamOptions` and `addRunningTo` keeps it one flow.)

### Should fix, not blocking

- **N1.** From a seat row, the add's heading reads `Teams › Running sessions › <session> › Add to a team` although the owner came from `Teams › <team> › <seat>`. The crumbs should
  say where the person is: pass a heading into `addToTeam` (as `addSeatAndRole` already takes
  one) and build it from `rowHeading(row)` for the seat route.
- **N2.** No test covers a cc session on either route (a `cc-` record in `Live`, a
  `hot.Session` with `Kind: "cc"`). The code paths are kind-agnostic so it works by
  construction, but the owner's "any member equivalent" is a claim worth one pinned test.
- **N3.** `sessionLine` pads names to the longest and joins columns with two spaces, a second
  table layout beside `hot.SessionLines` (tabwriter). The columns differ (this one adds the
  role and the full handle) so it is not a straight reuse, but a `tabwriter` over the option
  lines would align the checkout and team columns too, which the current padding does not.

## 3. Spec soundness, D8-D11 (Tasks 3.9-3.11, not built)

- **D8 (running ccy member restarts in place via its supervisor).** Sound in principle:
  `lib/restart-request.bash` already relaunches with one `--resume <session_id>` after
  stripping every conversation selector, and the lifecycle plugin's `on_idle` already honours
  `relaunch.json`. Two risks to design out in 3.11:
  - `relaunch.json` is one file with one reason today (`RELAUNCH_REASON`, first join). B4
    adds `"why": "restart"`; B7 withdraws a timed-out restart by removing the file. A first-join
    request written between the restart request and its withdrawal would be removed with it.
    Withdraw only a file whose `why` is `restart`, and have the plugin read `why` for its
    audit line.
  - "At its next idle moment" includes a session parked at a permission prompt or a question
    for 300 s or more; B7 then withdraws and marks ✗, which is right, but the plan line should
    say so ("it may be waiting at a question: attach it") as B7 already does for a start.
- **D9 (running cc left running).** Sound: F3 is true, cc has no supervisor. B5's "-" is
  the honest mark. Nothing to add.
- **D10 (resume by recorded id only, recorded by the pingbus SessionStart hook).** The
  reasoning against `--continue` (F4, F5) is correct and the id source is the right one.
  Risks:
  - Session name reuse (the M6 B1 class). `conversation.json` lives under the session's
    name; a later `--no-bus` launch under the same name writes no file, so the previous
    generation's id is still there and C4 would resume the wrong lineage. C4 must also
    require that the record's own launch carried the bus (`discover.LaunchedWithBus`), as
    `ownMembership` does, and should compare `written_ms` against the record's launch time.
  - F6 says the id changes on compact. Whether it does or not, C2 writing on every
    SessionStart is harmless; but C4's "transcript must exist" should resolve the transcript by
    the recorded id, not by newest-in-folder, or F4's shared directory bites again.
  - The alternative in D10 ("a hook for every session") is underpriced: `ccy --no-bus` is the
    owner's own escape hatch, and after a reboot those sessions are named but not restored.
    That is a change in `ccy-sessions restore`'s behaviour for existing records; the owner
    should see it as such, and the restore output must make the manual `ccy --resume` step
    unmissable.
- **D11 (`CLAUDE_CODE_DISABLE_AGENT_VIEW=1` in the image env).** Putting it in the image
  environment rather than a project's `settings.json` is right (the entrypoint merges project
  settings a project may own). Two checks for 3.10: confirm the variable name against the
  Claude Code build the image pins (grep the installed binary; the spec cites 2.1.303), and
  state what it does to a session that already has `/background` tasks running at upgrade
  time. Nothing in the tree sets it yet, consistent with the task being unbuilt.

## 4. Verdict

**Task 3.8: FIX-FIRST.**

1. F1: merge local F44 (97537444) into the branch and resolve PLAN.md and the 26-10-19
   journal conflicts.
2. F2: drop the "Restart the team's desktop sessions" paragraph from `picker/help.txt` until
   Task 3.11 lands.
3. F3: in `addRunningTo`, refuse a team that is not installed or whose homeserver is down
   before listing sessions, with `teamOptions`' own wording, tested.

N1-N3 at the author's discretion. The D8-D11 risks above belong in the spec before 3.9-3.11
are built, not in this merge.
