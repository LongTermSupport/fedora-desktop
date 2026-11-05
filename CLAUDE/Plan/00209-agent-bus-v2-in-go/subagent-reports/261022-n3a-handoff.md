# n3a handoff 1 (Plan 00209 N3, slice n3a)

Worktree `/workspace/untracked/worktrees/bus-v2-n3a`, branch `feat/bus-v2-n3a` (cut from
`feat/bus-v2-n3` at f6e4ad62). The first builder stopped at the context budget after the reading
and the design, with one green step committed. This file is the design so the next builder need
not re-read the chapters; read SPEC-n3.md sections 1, 2, 9 (gaps 1-4, 8) and 10 yourself (about
250 lines), and the design sections below only where a point is unclear.

## Done

- `6b7b1c9c` PA1: `paths/member.go` (+ `member_test.go`, registered in `separation_test.go`'s
  `sampleNames` and `zero_test.go`'s `zeroDerivations` and the method test's `valid` map, which
  gained `int64`): `FloorAccountData` (`teamchat.floor`), `Seat.StoreAside(ms)` (panics on
  ms \<= 0; prefix `store.json.aside-` unexported), `InboxSocket(runtimeDir, pid)`
  (`<runtime>/cc-socks/<pid>.sock`, in `notDerived` since it is Claude Code's name),
  `GuestUnitFile(u)`, `GuestUnitWantsLink(u)`, `GuestRuntimeParents(u, uid)`
  (`/run/user/<uid>/teamchat/run` and `/run/teamchat-session-<u>/teamchat/run`). paths 100%.

## Left, in this order (commit after each green step)

01. **Gap 8, `internal/teamchat/reexec`** (test helper for real second processes): `Main(m)` for
    `TestMain`, and a `Start(t, helperName)` that re-runs the test binary
    (`os.Executable()`, `-test.run=^TestHelperProcess$`, env naming the helper) through
    `execx.OS{}.Run` in a goroutine with `Stdin`/`Stdout` pipes (the child prints `ready` and
    its pid, waits for a line on stdin to release, exits; the test may `unix.Kill(pid, SIGKILL)`).
    `project_seam_exec` forbids os/exec outside execx; `project_seam_commands` needs a
    `dynamicNames` row for the run-time Name: add `{pkg: "internal/teamchat/reexec", fn: "Start", reason: "the test binary itself, re-run as a second process (SPEC-n3 gap 8)"}` to
    `src-go/qa/rules/table.go` (check the rule's fixture tests still pass: `go -C src-go/qa test ./rules/`).
    `cmdmaintest` is under `qa/internal` and cannot be imported (gap 8).
02. **`member/homefs`**: one fd-rooted directory type for the member's own files, modelled on
    `seat/fs.go` (unexported there; do not refactor seat): open a root (`O_DIRECTORY|O_NOFOLLOW`,
    owned by the uid; mode 0700 required for teamchat dirs, but a home and `~/.config` need only
    be the uid's), `openat2` with `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS` below
    it, mkdirAll (0700, fchmod explicitly, each existing one the uid's 0700 else refused), create
    `O_EXCL|O_NOFOLLOW` then rename, replace (temp `O_EXCL` + fsync + rename + dir fsync), read
    (`O_NONBLOCK`, fstat regular, cap), entries, unlink, renameat2 (`RENAME_NOREPLACE`). GU1:
    "opens through no link from the home's directory fd", "names every leaf mode", "refuses a
    path owned by another uid".
03. **`member/lock`** (LK1-LK4): `KeeperLock`, `LinkLock`, and `SeatLock` only from
    `(*KeeperLock).TakeSeat(team, seat)` (LK2); a separate `SeatHold` (take, close-only release)
    for the removals (`seat set-aside`, `guest remove`, GU3's identity change), which cannot reach
    the store writer. Files opened `O_NOFOLLOW|O_CLOEXEC|O_CREAT` 0600 from a homefs dir fd;
    `LOCK_EX|LOCK_NB` tried 3 times 50 ms apart on a `Clock{Sleep}` seam; held after the third is
    `*BusyError` (`errors.Is(err, ErrBusy)`); keeper/link released `LOCK_UN` then close, seat by
    close only; `SeatLock.Dup()` for the link (never `LOCK_UN`); `SeatLock.WriteRecord(data)`
    writes through the held description (ftruncate, pwrite at 0, fsync) so lock imports nothing of
    state. No code unlinks or renames `seat.lock` (LK3). Tests: a second process (reexec) holding
    `LOCK_SH`; "for 60 ms" is the fake clock's Sleep releasing the child after the first try (no
    test sleeps); past three tries `Busy`; a `SIGKILL`ed holder frees it; each for `TakeSeat` and
    the hold.
04. **`member/state`** (MS1, MS2, gap 4): strict codecs (exact keys, `v` 1, integers only, no
    duplicate key: `proto.Parse` then `json.Decoder.DisallowUnknownFields`, keys lowercase as
    `seat.decodeStrict` does; no file value quoted in an error, B10) for `store.json` (B 2.3.2
    schema and every cap: 2048 pending of 320 B, 1024 ledger, 256 pings of 3 KiB and 16 targets,
    1024 receipts, 32 awaiting, 16 missed, 64 sending, 64 gate keys; file cap 2 MiB; a test
    proves each list cap fires before the file cap), `session.json` (B 2.3.3), `keeper.json`,
    `report.json`, `status.json` (B 2.9), `conversation.json`, `session.pid` (B 2.4.1 step 4),
    the seat lock record (B 2.5: `{"v":1,"run","started_ms"}` and with `launch_failed_ms`), and
    `desired.json` (C 3.7.5: `v, session, generation, written_ms, seats[{team,seat}], farewell[{team,seat,reason}], inbound`). Reads through `teamchat/state.ReadFile`, writes
    through `teamchat/state.WriteFile` (0600, gid -1) (TB4). Gap 4: move `seat.DecodeLockRecord`
    and `LockRecord` here with an encoder (`LockRecord{Run paths.RunID, StartedMS int64, LaunchFailedMS *int64}`), and switch `seat/release.go` `runOf` and `health/online.go` to it;
    delete `seat/lockrecord_test.go`'s copy (move the cases). `state.WriteLockRecord(*lock.SeatLock, LockRecord)`. MS2: the store writer `StoreWriter` made only from a `*lock.SeatLock`, one atomic
    replace per change; `pending` refuses an agent item once it holds cap-256 entries, HUMAN and
    TIMEOUT may use the last 256; every other full list refuses growth (`ErrFull`), `ledger` and
    `missed` roll (oldest dropped). Import graph: member/state -> member/lock -> member/homefs;
    seat and health -> member/state; member/state must NOT import seat (cycle).
05. **seat gap 3 and member/bundle** (MS3): `seat.DecodeMemberBundle(data)` accepting exactly the
    two shapes (a guest's `Bundle`, a session's `HeldBundle`). Delete `seat.PlaceGuest` and
    `removeGuestClaims` and their tests (gap 2: replaced by GU3; no production caller; keep
    `KeepGuestToken`). `member/bundle` (its own package because seat imports member/state):
    `ReadPair(seatDir)`: seat.json, token, seat.json again; counts only when both reads are the
    same inode and bytes, else retried once at once, then a typed "torn" result (never a permanent
    78); token the uid's, 0600, `seat.ParseSecret`; 78 `bundle` for a password bundle, two held
    seats sharing a user id or a token, a `team`/`seat` that is not the directory it is in, a
    `user_id` differing from the store's. The token never reaches argv, a log or an error.
06. **`member/exits` and `member/records`** (CL3, CL4), leaf packages so cli and guest share them:
    exits = B 2.2.4's table as data (exit, meaning, code set, class, next step) and an `*Error`
    `{Exit, Code, Team *TeamID, Message}`; `Message` built only from a template plus checked
    names (a name failing its grammar renders `?`); `Write(stderr, err)` writes the error object
    as the last stderr line, `kind` first then `v` (n3a decision: same order as every record,
    B's example has `v` first). records = every B 2.2.2 kind as a typed struct (`human`, `peer`,
    `ping`, `timeout` with `restored`; `notice`, `fact`, `outbox`, `sent`, `pending`,
    `question`, `history`, `verified`, `more`, `team`, `member`, `room`, `runtime`,
    `team-failed`, and `version`, below), encoded as `{"kind":..,"v":1,<fields>}` (splice the
    kind and v before the marshalled body; `SetEscapeHTML(false)`). CL3 test: a body with tabs,
    newlines and a forged `teamchat delivery` line is one record line read back equal.
    CL4: a multi-team partial answer is exit 5 with `team-failed` records.
07. **`member/cli`** (CL1-CL6): the verb table as data, B 2.2.1's every verb and flag (`--team`
    on every agent-facing verb but `version` and `help`); flags only after the verb (a flag before
    it is 64; the one exception is a lone `teamchat --version`); `--flag value` only. Unbuilt verbs
    exit 1 `internal` naming the slice: `run`, `link`, `recv`, `wait`, `inbox`, `status`, `verify`,
    `history`, `seat disconnected`, `reach` (in no slice of the spec: say so in the report) n3c;
    `reply`, `say`, `ping` n3d; `hook`, `status-line` n3e; `report`, `ask`, `thread`,
    `questions` n3f. CL2's "the unbuilt list is empty" test goes under a build tag
    (`teamchatrelease`) that Task 3.3's build runs; record it. CL5 `ParseLaunch`: exactly one of
    `--claude`/`--plain`; `--kind ccy|lxc|host|other`; `--desired DIR`, `--state DIR`, `--seat T/S`
    (repeatable, only with `--state` and no `--desired`), `--resume-recorded`, and `--kit DIR`
    (the re-exec form of B 2.4.1 step 2, a decision); `--` then CMD; under `--claude` the first
    argument whose base name is `claude`, at most one bare `--` and only directly before it,
    `--settings`/`--plugin-dir`/another `--` anywhere 64, `--resume-recorded` with
    `--resume`/`--continue` 64; under `--plain` CMD is opaque (a decision: the probe's
    `/bin/sh -c '...'` line). Test the launch lines: C 3.9.4's ExecStart after tmux's own
    arguments, ccy's `teamchat run --claude --kind ccy --desired /run/teamchat/membership -- <supervisor> --arm -- claude`, host `cc`'s `... --kind host --desired <home>/.config/teamchat/membership -- claude`,
    the probe's `run --plain --kind host --state <dir> --seat <team>/probe -- /bin/sh -c '...' probe <target> <team>`
    (C 3.3 line 458), B 2.11's kinds table. CL6 `ResolveTeam(flag, ledgerTeam, active)`: single-
    team verbs `reply`, `verify` (ledger), `say`, `ping`, `report`, `ask`, `thread`; with two
    active teams and none it is 64 `ambiguous-team` listing the teams with descriptions passing
    `proto.ValidDescription`. `--version` prints the bare digest (the kit check, B 2.4.1 step 2);
    `version` prints a `version` record `{"kind":"version","v":1,"digest":..,"records":1}`
    (decision; B 2.2.1 says digest and record version); both exit 1 without a built digest.
    `help`, `help <verb>`, `help exit-codes` (text from the same data). Hook events: `teamchat hook session-start|user-prompt-submit|stop|session-end` (decision).
08. **`member/guest`** (GU1-GU5) and **`seat set-aside`** (SA1): an identity seam (uid, gid,
    groups, passwd lookup of `--user`; home from passwd, opened as the homefs root), refuses uid 0,
    any supplementary group but the user's gid, a `--user` that is not the caller by uid and name;
    umask 077 first; prints only `CHANGED<TAB><what>` with `what` from a fixed exported set per op
    (export the sets for n3b's TB2.4 parser); exits 0, 64, 75 (remove, place-bundle), 78 with the
    error object as the last stderr line. B's 78 code set gains the check names for GU2 (`home`,
    `gh`, `tmux`, `claude`, `kernel`, `reach`) and `identity` (a decision; propose the design text).
    GU2 stdin `{"v":1,"team","base_url","plain_http_hosts"}` (team = `--team`); `gh`, `tmux`,
    `claude` executable regular files on exactly `<home>/.local/bin:/usr/local/bin:/usr/bin:/bin`;
    `gh auth status --hostname github.com` through `execx` (Fake in tests); pidfd and inotify
    through a seam; reach = `matrix.NewMember` (placeholder token as seat's `checkToken`) and
    `Versions` within 3 s, tested on fakehs. GU3 stdin `seat.DecodeIssued`; password 78; absent
    seat dir: remove `<team>.new-*`, make `<team>.new-<claim>/` with token then seat.json, rename
    `RENAME_NOREPLACE`; existing with the same `user_id` and seat: keep store and `seat.lock`,
    replace token then seat.json (only if the bytes differ); else (or undecodable) take the
    `SeatHold` (75 if held), set the store aside as SA1, place, release last. GU4: decode with the
    desired codec, `session` must be `lxc-<user>`, write canonical bytes to
    `~/.config/teamchat/membership/desired.json` (0700/0600), same bytes print nothing. GU5: hold,
    unlink every entry of the seat dir but `seat.lock` (a directory entry is refused), release;
    nothing removed prints nothing. SA1 `seat set-aside --team T [--state DIR]` (`--state` added:
    the owner's sweep acts on a ccy session directory on the host; default the home layout's
    state root): hold (75 if held), rename `store.json` to `StoreAside(now)`, release; no store is
    success with no line. Tests: LK3's "against a held seat leaves `seat.lock` the same inode",
    with a reexec holder.
09. **`member/kit`** (KT1): `Assemble(into string, kit paths.Kit, binary io.Reader)`: `bin/teamchat`
    0755, `plugin/teamchat/.claude-plugin/plugin.json` and `paths.Kit.PluginHooks()`
    (`plugin/teamchat/hooks/hooks.json`) naming `<kit>/bin/teamchat hook <event>` for
    SessionStart, UserPromptSubmit, Stop, SessionEnd only; `settings.json` with
    `{"statusLine":{"type":"command","command":"<kit>/bin/teamchat status-line","refreshInterval":25}}`,
    no inbound key, no SessionEnd timeout; directories 0755, files 0644, modes by fchmod; refuse
    a kit path with any character outside `[A-Za-z0-9/._-]` (Claude Code runs the command through
    a shell). Build JSON with `proto.Obj` so tagliatelle has no camelCase struct tags.
10. **`cmd/teamchat`**: thin `main.go` over `member/cli`, `var SourceDigest string` set by
    `-ldflags "-X main.SourceDigest=<digest>"` as teamchat-admin does; `go-deps.allowlist` gains
    `keepout	go.yaml.in/yaml/v3	./cmd/teamchat	<reason>` (CL1; godbus's row is n3b's).
    `.golangci.yml` tagliatelle snake overrides for `internal/teamchat/member/state`,
    `member/records`, `member/exits` (and gosec G304/G302 test exclusions where the tests build
    hostile trees, as `seat/.*_test.go` has).
11. PLAN.md Task 3.2 and 3.3 progress lines in the same commit as the code; the report
    `subagent-reports/261022-n3a.md`; the Opus review loop (`261022-n3a-review.md`, `-review-2`,
    `-review-3`).

## Gotchas met

- `sed` is denied; so is piping to `tail`/`head` (redirect to a file under `untracked/scratch/`
  and grep it). `x, _ := f()` is denied by the Write hook even for a non-error second value: use
  named results.
- `qa-changed.bash` is red only on govulncheck GO-2026-6809; measure with
  `go test -race -cover ./internal/teamchat/...` and `golangci-lint run` from `src-go`, with and
  without `--build-tags tuwunel`.
- No `time.Now` in tests (`project_testclock`); clocks are seams.
- `paths`' tests hold every exported name: register a new one in `sampleNames` and, for a
  function of a guarded type, `zeroDerivations`.
- `lxc-*` programs are owned by `internal/lxcfreeze` (n3b's concern); `gh` is unowned; `tmux` is
  owned by `internal/tmuxccy`, so GU2 only stats it, never runs it.
