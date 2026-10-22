# N2 slice n2e: handoff 1

Branch `feat/bus-v2-n2e`, worktree `/workspace/untracked/worktrees/bus-v2-n2e`, stacked on the
N2 integration branch at 253d6b3c. The contract is [SPEC-n2e.md](../SPEC-n2e.md). The first
builder stopped at its context budget after four green commits.

## Done (each its own commit, each test seen red first)

| Commit     | What                                                                                                                                                                                                                                                        |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `1b007016` | SPEC-n2e.md                                                                                                                                                                                                                                                 |
| `fdfe2bab` | `internal/teamchat/status` (SR1-SR5): the record's strict codec (nested keys required by re-encoding and comparing), `Write` 0640 through `state`, `Next` (the seq rule, events 24 h and 32 newest), `Read` (1 MiB cap), `Shown`. 94.6%                     |
| `7930a907` | `internal/teamchat/teamlock` (TL1): flock `LOCK_EX\|LOCK_NB`, `O_NOFOLLOW`, holder line, `BusyError` (`errors.Is(err, ErrBusy)`). 93.1%                                                                                                                     |
| `76e86760` | `internal/teamchat/health` (HT1-HT6) with every seam (`Units`, `Prober`, `Viewer`, `Backups`, `Space`, `Guests`, `Announcer`, `Locks`, `Teams`), plus `StatfsSpace` and `TeamLocks`; `seat.DecodeLockRecord` exported (B 2.5's rule, used by step 6). 90.7% |

`.golangci.yml` has tagliatelle's snake_case override for `teamchat/status` and
`teamchat/health`, and a G304 exclusion for `teamchat/teamlock`.

## Left, in this order

1. **`internal/teamchat/health/view`** (VW1-VW6), the child's read-only read. Plan: from the roster
   file (`apply.DecodeRosterFile`) and `rooms.json` (`apply.DecodeRooms`), with a
   `matrix.Admin`:
   - memberships from `RoomState`'s `m.room.member` events;
   - humans' `Devices` (count, newest `LastSeen`);
   - DC16's watch: for every member `active` or `parked`, `Account` (deactivated or missing) and
     `Devices` (any id but `paths.SeatDevice`);
   - drift by running `rooms.Converge` against a client wrapper whose writes record a problem
     and write nothing (`CreateRoom` an error), with a no-op `Recorder`;
   - the questions items;
   - `pending` as a problem.
     Return `health.View`. Test on fakehs, plus a `-tags tuwunel` test for the device and
     deactivation answers (`matrix/tuwunelhs`, as `rooms/tuwunel_test.go` does).
2. **`internal/teamchat/notice`** (NT1-NT2), the disconnected announcer core both the tick's
   child and B's `seat disconnected` call. Read main backwards (`Messages`, 10 pages of 100) for
   the subject's own lifecycle notice since `since_ms`: `teamchat.notice` with `subject == sender`, `proto.MemberNotice`. Post with `proto.Composer.Notice` and `Send`, txn
   `tc-disc-<run>`. Only a transient `matrix.Error` (`Transient()`, or a cancelled context) is an
   error. Everything else is `Outcome{Posted:false, Why}`.
3. **seat** (NT3, n2e-D7): `notice.json` gains `until_ms` (posted false only). `announce` asks
   again while now < `until_ms`. At or after it the notice is recorded as not posted in time and
   the release goes on to `Retire`. Write `until_ms` = now + 10 min on the first transient
   failure. This closes the n2c re-check's low 2: a member that spends its own rate limit
   delays its token's retirement by 10 minutes at most.
4. **`internal/teamchat/sweep`** (SW1-SW5) and `seat.Store.Holdings()`. `Holdings` lists pool
   seats with a `held/` under `PoolTeam` dirs. `Sweep` takes `Sessions` (restorable) and a
   `Desired` seam (whether the session still desires the seat):
   - over: `Release`;
   - not desired: `Release{Leaving:true}`;
   - otherwise: `Announce`.
     Test the container-start cases (no lock file, an empty one, a held one) and a launch
     failure: none of them is announced.
5. **`internal/teamchat/element`** (EL1-EL5): the key set is in
   `untracked/scratch/n2e-element-digest.md` (in this worktree; v1's `config.json` keys,
   `electron-config.json`, the launcher lines). The URL comes from
   `declare.FileApprovals{Path: paths.ApprovedFile}.Load`, the name from `status.Read`'s `name`
   (n2e-D9), and the server name from `paths.ServerName`. Consent is the JSON record naming the
   consent text (n2e-D8). Writes go through a temporary file and a rename, only on change,
   refusing a non-regular target, each change printed as a `CHANGED<TAB><path>` line. It
   refuses to run as root. tagliatelle will want an override, or build `config.json` with
   `proto.Value` as `apply` does.
6. The review loop (Opus, at most 3 rounds), `subagent-reports/261022-n2e.md`, and Task 2.4's
   progress in PLAN.md.

## Decisions taken so far (beyond SPEC-n2e section 8)

- A unit state that cannot be read is a problem ("the homeserver unit's state cannot be read"),
  with `unit` recorded as `inactive` and `probe` as `not-run`. It does not fail the tick.
- A failure to read the units during the re-probe counts as an answer, so there is no restart.
- A broken `lxc-online.json` fails that team's tick (fail fast). Nothing rewrites it.
- A dropped view (busy lock, seq changed, stamp gone) skips step 6 too.
- A problem never carries an error's text. The error goes to the outcome, and from there to the
  journal.
- For an LXC entry, a parked member's ended run is marked settled without asking. A launch
  failure marks the entry settled when the failed run is the entry's run.
- `Team.Origin` is used only when there is no earlier record. After that the record's own
  `origin` is kept.

## Gotchas in this environment

- `sed` is denied. A Bash command whose text holds a `*` token (a glob, or `x * y` inside a
  heredoc) trips the secret guard. Use `Edit` for code changes.
- `qa-changed.bash` is red only on govulncheck GO-2026-6809, and that red stops go-qa's other
  runners. Run `go test -race -cover` and `golangci-lint run` (with and without
  `--build-tags tuwunel`) by hand.
