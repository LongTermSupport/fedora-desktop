# Review 1: acceptance-s1 fix (branch fix/acceptance-s1-cwd, commit cc1904d3b)

Diff reviewed: `git diff origin/F44...HEAD`, covering `acceptance-s1.bash`, `_acceptance-s1.inc.bash`,
`_s1-shim.bash` and the meta-deploy.list comment. `bash -n` and `shellcheck -x` pass on all three scripts.
I also ran the fixture member.json through a JSON parser and it loads.

## Verdict: MERGE

Each of the three goals is met, checked against the Go source. None of the findings can make a check
pass when it should fail. Two of them can produce a misleading message or report a verdict against the
wrong cause. Each one-line fix is worth making on this branch if it is touched again; none blocks the merge.

## Goal 1: S3 opens run outside any checkout

- New binary. `run.once` calls `offerJoinHere` on the first pass only. That calls `Checkouts.Find(ctx, "")`
  (hostcheckout.Find, then gitx.ProjectHere). For a cwd in no work tree it returns `Checkout{}` (Top ""),
  and the offer returns nil with no question. ProjectHere runs `git rev-parse --is-inside-work-tree` with
  LC_ALL=C. It treats as "not in a tree" only an exit error whose stderr starts with
  `fatal: not a git repository`.
  - `outside_checkout` makes the same probe: same LC_ALL=C, same prefix, and the same inherited
    environment (GIT_DIR and so on). The only difference is that it captures stdout and stderr
    together.
  - Leaving the checkout also removes the `GithubRemote` and `TeamsFor` declare.py runs, which would
    otherwise have broken the "exactly one declare status" count. It also drops `addWorkTree`'s seats.
- Old binary (c4cab1419, the first parent of 082363498). `offers.go` and `hostcheckout.go` are
  byte-identical to HEAD. `picker.go` differs by 5 lines, none in the start-up order. `gitx.ProjectHere`
  is unchanged. So the offer is suppressed for both binaries.
- Old opens still start at least 2 declare.py runs without the checkout: `status`, then `limited`, then
  `undescribed`, all before the table.
- Both binaries open from one cwd. `rmdir` cleans it up on every path through `check_open`, including
  failures inside `open_rounds`. A non-empty folder fails the run, which is correct.

## Goal 2: what the message for a question before the table names

The message names the screen (`gum confirm` or `gum choose`, read from dir/ui) and its arguments (from
ui.argv, now written by the shim).

The causes it gives match `pickerSession` and `once` exactly. There are four gum questions before the
table when the cwd is in no checkout:

- `offerCreate`: "Create a team", asked when there are no teams (and again in `once` when there are no
  rows, which means the same thing).
- `offerPathLimits`: "Folder limit".
- `offerDescriptions`: "Description".
- `offerUpdate`: "Update", asked for ApplyOutdated or ApplyUnbuilt teams.

`offerJoinHere` and `offerCreateHere` are correctly excluded. No cause is missed and none is misnamed.
A sudo password prompt (`collectWithSudo`, then `SudoReady`) is not a gum screen, so it never stamps
ui and cannot reach this message.

## Goal 3: S4 on a fixture

Each part was traced through the production path:

- **Registry.** `registry.Locate` honours `CCY_STATE_DIR` (`<base>/sessions`). The record (header,
  name, dir, launcher, prefix, restore=no) passes `ReadFile`'s strict parse. prefix=ccy passes
  `discover.IsLauncher`, so `addRecord` adds `top` to `s.Checkouts`. `addCheckout` does not filter on git.
- **Team.** `addStamped` adds every top-level `*.json` in AppliedDir. The default
  `/var/lib/agent-bus-applied` and the `CCY_TEAMS_APPLIED_DIR` override match `config.go:42`. The
  script's name regex equals `seat.teamNameRE`.
- **Seats.**
  - `readSeats` stats `top/.claude/ccy/pingbus/seats`. `SeatList(dir)` resolves the folder with
    `EvalSymlinks` and does not ask git, so a folder that is not a checkout is accepted.
  - `seat.List` checks that `top` and `.claude/ccy` are real directories owned by the uid. The script
    creates them as the desktop user, so they pass.
  - Seat names "1" and "2" match `seatNameRE`. A missing seat.lock is taken as not held.
- **member.json.** Every field passes `parseMember` and `checkTail`:
  - the keys are all known;
  - protocol is 1, and team equals the directory name;
  - server_name is a DNS name;
  - the user_id localpart matches `handleRE` (`acceptance-s1.1+fixture.host`);
  - admin is `@admin:<server>`;
  - room is `!` plus 43 characters;
  - token_file is `token`;
  - plain_http_hosts is canonical;
  - `http://127.0.0.1:8008` has an IP host listed in plain_http_hosts, with no path, query or
    credentials;
  - the limits keys are known.
- **Refusal of seat 2.** 61 produces
  `seat 2@<team>: team <team>: <top>/.claude/ccy/pingbus/seats/<team>/2/member.json: limits.report_per_hour must be an integer from 1 to 60`.
  That is `List`'s failure prefix, then `readMember`'s `where`, then `checkLimits`. `pathText` leaves
  ASCII unchanged. `agentbus.SeatList` writes the line to Err (stderr). The grep string matches it
  byte for byte, provided PLAN_RUN_DIR has no symlink in it. It is built from `pwd -P`, so it has none.
- **Table.** `--list` into a file has width 0, so there is no narrow split and each row is one line.
  The seat row's cells are Team, then `Label`, which is the whole handle (plus " (coordinator)" at
  most). `addMemberRows` adds seat rows whether or not the team's homeserver is up. So the handle
  and the team really are on one line.
- **Vacuous pass.** Seat 2 must be refused with exactly the fixture's path, which proves the binary
  read the fixture. Seat 1 must be present on stdout. Live checkouts that `tmux ls` adds are read
  beside the fixture, but every match is keyed on the fixture's path or its unique handle, so they
  cannot collide. `addDesired` keeps only records' sessions, which here means only the fixture's.
  The `--list` exit code is reported and not judged. That is right, because seat 2 makes the table
  Incomplete, which exits 1.
- **The report** says it plainly: "S4 on a fixture, not a live seat …", and "judged on the fixture
  alone" when no live seat sets the limit.

## Findings

1. **Low-medium. `_acceptance-s1.inc.bash:276`, `acceptance-s1.bash:37-39`, and the meta-deploy.list
   comment.** All three say ccy-teams asks "on every open until it is answered". That is not how the
   source behaves:

   - "Not now" (and Esc) answers Folder limit and Description, and they come back on the next open.
     Only "now" or "never" stops them.
   - Update comes back after a "no" until the teams are applied as declared.
   - Create a team comes back until a team exists.

   The remedy line at :280 is right, so this only misdescribes the trigger. **Fix:** "…asks before its
   table on every open: for a team limited to some folders, until it is lifted or you choose never
   (Folder limit); for a team with no description, until one is written or you choose never
   (Description); while teams are out of date or declared and not built, until they are applied
   (Update: answer yes, or run ccy-teams apply --all / play-agent-bus.yml); and with no team at all,
   until one is made (Create a team)." Make the header and meta-deploy.list comment say the same.

2. **Low. `_acceptance-s1.inc.bash:404-415`.** AppliedDir is 0750, root-owned, with the stamp group
   (see `files/usr/local/sbin/agent-bus-install` and `test-agent-bus-install.bash:607`). If the
   desktop user cannot enter it, the glob stays literal, `-f` is false, and the script prints "no team
   is applied on this machine". That names the wrong cause.

   ccy-teams itself would fail `addStamped` on that host ("could not be read"). So the true cause is
   a permission problem, and it should fail (1), not come back NOT JUDGED (2).

   **Fix:** before the loop, add
   `if [[ -e "${applied}" && ! -r "${applied}" || -e "${applied}" && ! -x "${applied}" ]]; then printf '%s cannot be read by %s: …\n' …; return 1; fi`.

3. **Low. `_acceptance-s1.inc.bash:425-440`.** If `cd "${cwd}"` fails inside the subshell, `code` is
   1 and is reported as "--list (exit 1)". The greps then run on missing files and return 2, which
   `if` reads as "not found". The result is "not refused with v1's words" and "not on the table". The
   failure is not silent (bad=1), but it is reported against the wrong thing.

   **Fix:** check that the output files exist after the run, e.g.
   `[[ -f "${fx}/stdout" && -f "${fx}/stderr" ]] || { printf 'the fixture --list did not run (exit %s)\n' "${code}" >&2; return 1; }`.
   Alternatively, test `cd` before starting the binary.

4. **Low. `acceptance-s1.bash:45-49` (EXIT CODES).** The causes listed for NOT JUDGED (2) do not
   include the new one from `outside_checkout`: "TMPDIR is inside a git work tree, or GIT_DIR is set".
   That path returns 2 from S3 and from the S4 fixture. **Fix:** add it to the list in the header.

5. **Note, no change needed. S3's seat-list proof.** Old opens now count seat lists only for the
   checkouts that tmux sessions and session records name, not the cwd's checkout. On a host with no
   recorded or live ccy session with seats, S3's process count comes back NOT JUDGED. The message at
   :349-350 already names that cause, so this is correct behaviour.

6. **Note.** `outside_checkout` returns 2 when git exits 0 but prints `false` (a cwd inside a .git
   directory), while ProjectHere would answer false and make no offer. This is over-strict but safe,
   and with `mktemp` it cannot happen in practice.

## Checks with nothing to report

- **Bash.** `cwd="$(…)" || return $?` keeps the function's own status, because a bare assignment
  takes the status of its command substitution. The open runs in a subshell with `&&`, so a failed
  `cd` never starts the binary. No `local` masks a status; each `local` is declared apart from the
  substitution. The globs are guarded by `-f`.
- **Temp folders.** No early return leaks one: `check_open` and `fixture_report_per_hour` both
  `rmdir` before every later return. An interrupt (Ctrl-C) can leave one empty folder in TMPDIR,
  which is acceptable.
- **Stderr hygiene.** `outside_checkout` puts only the path on stdout, and every diagnostic goes to
  stderr. `say_report` keeps its established role.
- **Public repo.** No install-specific names. Comments describe current state.
