# n3b handoff: the slice split into four parts (Plan 00209 N3, slice n3b)

This is a design only, and no code is written here. n3b is SPEC-n3.md section 3 (LX1-LX9, SU1-SU3,
LP1, LP2), held to the rules of section 1 (TB1-TB4, CK1). It closes gaps 10 and 13 of section 9 and
the new gaps 15-21, which this commit adds to section 9. The new decisions n3-D21 to n3-D30 go in
section 10. The slice is too big for one builder, so it is split into four parts, P1 to P4. Each
part is one worktree, one branch and one builder. Each is reviewed to MERGE on its own and merges
into `feat/bus-v2-n3b`.

The base is `feat/bus-v2-n3b` at 727087da9. It holds N1, all of N2 (n2f's production wiring
included), F44, the N3 spec and slice n3a. Every API quoted below as "built" was read from
`go doc -all` or from the source at that commit. Each signature that a part adds is that part's
own decision. A builder may change one only by writing the change, and its reason, in the part
report.

**Gate A** marks every host run of this slice. The `lxc apply` play task, the kit placed on the
host, and the unit placed in a guest are code only here. Nothing is installed, and nothing runs
on the host. The VM lab does not wait for gate A. An LXC seat is a token seat, so no part of n3b
waits for gate B.

## 0. Order

| Wave | Parts        | Needs                | Why                                                                                                                                                      |
| ---- | ------------ | -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1    | P1 ∥ P2 ∥ P3 | the base             | P1 is the guest root and the lxc programs; P2 is the guest's systemd and the unit text; P3 is the parsers and the two host records. None imports another |
| 2    | **P4**       | P1, P2 and P3 merged | the phases themselves (`host/lxc`), `lxc restart`, the L3 probe, the admin verbs and the production wiring: everything that composes wave 1              |

**P1 lands first in the sense that matters.** P4's every read and write goes through P1's
`guestroot`, and P1 carries the one QA rule change (gap 15), which the other parts' code does not
need but P4's does. P2 and P3 do not need P1. P2 takes a connected `net.Conn` and never opens a
path, and P3 parses bytes and never opens a guest path. All three start at once from the base.
P4 starts when all three have merged into `feat/bus-v2-n3b`.

**From n3c: nothing.** n3b never speaks to a member's link: the host reads `seat.lock` and
`session.pid` as files, and the guest operations are n3a's. n3c's `clock`, `failpoint` and
`member/wire` (on `feat/bus-v2-n3c-p2`) are not imported. n3b's clocks are the host's existing
`Now`/`Sleep` seams (`host.Clock`, `health.Clock`, `teamlock.Clock`), as every host package has.

n3c does touch some of the same files: `paths/paths.go` and its `separation_test.go` and
`zero_test.go`, `qa/rules/table.go`, `member/exits/exits.go` and `member/homefs`. n3b leaves
`exits` and `homefs` alone. In `paths`, P3 puts its names in a new file, `paths/lxc.go`, and
registers them at the end of each test table. In `table.go`, P1 adds whole rows next to the
lxcfreeze rows. When the slices merge, any conflict is then a choice between whole lines.

Every part keeps a 90% coverage floor per package and runs under `-race`. No test sleeps or
reads the wall clock (`project_testclock`). `os/exec` is reached only through `execx`
(`project_seam_exec`).

Gotchas from n3a and n3c that still hold:

- `sed` is denied, and so is a pipe into `head` or `tail`. Redirect the output to a file under
  `untracked/scratch/`, then read that file.
- `x, _ := f()` is denied by the Write hook, so use named results.
- `qa-changed.bash` is red only on govulncheck GO-2026-6809. Measure from `src-go` with
  `go test -race -cover ./internal/teamchat/...` and `golangci-lint run`, each with and without
  `--build-tags tuwunel`. A change to the QA rules is measured with `go -C src-go/qa test ./rules/`.
- `paths` holds every exported name in its tests. A new name goes in `sampleNames`, and a
  function of a guarded type also goes in `zeroDerivations`.

## 1. What n3b builds on, quoted

### admin (`internal/teamchat/admin`)

- The verb table already holds `lxc apply` and `lxc restart` as root verbs. Neither has a
  `run` yet (`run.go:135`), so each answers "is not built yet". P4 adds both to `rootRuns()`.
- The exit codes are `ExitUnreachable` 69, `ExitRefused` 70, `ExitBusy` 75, `ExitClass` 77 and
  `ExitConfig` 78. `ExitCodeOf(err error) term.ExitCode` maps a held team lock to 75, a child's
  failure to its class's code, drift or a refusal to 70, and an unreachable homeserver to 69.
- `type Child interface { Ask(ctx, req Request) (Answer, error) }`, with
  `Request{Op string; Team paths.TeamID; Args proto.Value}`.
- `Answer{OK bool; Changed []string; Handle string; Credential *apply.Credential; MainRoom string; Notes []string; View *health.View; Notice *health.Notice; Error *AnswerError}`.
- The child ops are `apply.OpMemberAdd|Return|Reissue|Park|Retire|Set`, `apply.OpRoleApply`,
  `admin.OpTeamView` (`team.view`) and `admin.OpMemberDisconnected` (`member.disconnected`).
- `memberVerb(op)` (`verbs.go:146`) parses `--team` and the op's flags into `[]proto.Member`. It
  then calls `teamlock.Take(ctx, sys.Host.LockDir, team, op, SeatWait, sys.Host.Clock)` and then
  `underLock(ctx, sys, op, team, args) ([]byte, error)`. `underLock` reads `team.json`, asks the
  child, checks the answer with `asked`, refreshes the status record, and has the bundle made by
  `sys.Bundles.Bundle(ctx, decl, handle, mainRoom, cred) (proto.Value, error)`.
  - `flagArg` turns `--seat next` into 0.
  - The `add` flags are `prefix, seat, kind, human-text, credential`.
  - The `return` and `reissue` flags are `handle, credential`.
  - The `park` flag is `handle`.
- `SeatBundles.Bundle` builds a `seat.Issued` (the bundle `guest place-bundle` reads on stdin)
  with `BaseURL: team.URL` and `PlainHTTPHosts: plainHTTPHosts(team.URL)`, the latter
  unexported in `bundles.go`.
- `System{Host host.Deps; Child Child; Bundles Bundles; OwnerGroup func() (int, error); Open Opener; Pin func() (host.Pin, error); CtlIDs func() (User, error); Health func() (HealthTick, error); Home func(uid int) (string, error)}`.
- `ChildAnnouncer{Child; Root}` is the production `health.Announcer`: it asks
  `member.disconnected`.

### admin/wire, units, hostnet, ctl, health and notice

- `wire.New(digest string) func() (*admin.System, error)` and `wire.Bridge = "lxcbr0"` (NW4).
  `wire.go:81` gives the health tick `Guests: health.GuestsNotBuilt{}` and
  `Announcer: admin.ChildAnnouncer{Child: child, Root: root}`. P4 replaces the first.
- `units.NewManager(dial func(context.Context) (Conn, error), r execx.Runner) *Manager` is
  systemd on the host's system bus. Its `Host()` is a `host.Systemd`, and its `Health()` is a
  `health.Units`. It accepts only teamchat homeserver unit names (`ErrUnitName`), and it has no
  read cap. n3b does not use it: the guest's systemd is a different peer, reached by P2.
- `units.Conn` is
  `interface { Call(ctx, dest string, path dbus.ObjectPath, method string, args ...any) ([]any, error); Signal(ch chan<- *dbus.Signal); Close() error }`.
  godbus is already in `go.mod` (v5.2.4) and `go-deps.allowlist` (line 45, n2f-D4).
- `hostnet.Network.Bridge() (netip.Prefix, error)` is the live subnet of the LXC bridge.
  `hostnet.TeamPort(root)` is the port a team's applied `team.json` declares.
- `ctl.Config.Opener()` gives the child's reach to a team. `ctl/<team>/guest/<handle>.token` is
  `paths.GuestTokenCopy`. The child writes it when it issues a token credential
  (`apply/member.go:569`).
- The health tick's seams:
  - `health.Guests` is
    `interface { Probe(ctx, team paths.TeamID, handle string) (GuestProbe, error) }`, with
    `GuestProbe{Stopped, Held bool; Record []byte}`.
  - `health.Announcer` is
    `interface { Disconnected(ctx, team, handle string, run paths.RunID, sinceMS int64) (Notice, error) }`.
  - `probeGuests` (`health/online.go:53`) calls `Probe` for each `f.view.Members` entry whose
    `Kind` is `status.KindLXC` (`"lxc"`).
  - `health.Clock` is `{Now() time.Time; Sleep(ctx, d) error}`.
  - `health.Stamps{Root}.Stamped(ctx)` returns `[]health.Team{ID, Name, Origin, AppliedMS}`, and
    `Origin` is `"migration"` or `"v2"`.
- `notice.Disconnected(ctx, c notice.Client, n notice.Notice) (notice.Outcome, error)` and
  `notice.TxnID(run) string` give `tc-disc-<run>`.

### host, teamlock, declare, status and state

- `host.PlayWait = 10 * time.Minute`.
- `host.Clock` is `{Now; Sleep}`.
- `host.Deps{Root, LockDir string; Clock Clock; Approvals declare.Approvals; Status Status; …}`.
- `host.Status.Refresh(ctx, team, first bool) error`.
- `teamlock.Take(ctx, dir string, team paths.TeamID, verb string, wait time.Duration, clock Clock) (*Lock, error)`
  returns `*BusyError` (`errors.Is(err, teamlock.ErrBusy)`). `(*Lock).Release() error` frees it.
- `declare.Decode(raw []byte) (Declaration, error)` reads a declaration strictly. The result is
  `Declaration{Address string; Teams []Team; LXC []LXCMember; Element []ElementTeam}`, and
  `Declaration.Inputs() []Input` gives each present team with its own LXC entries.
  - `declare.Hold(approved *Gated, in Input) error` refuses with `*RefusedError` (exit 70).
  - `declare.FileApprovals{Path func(paths.TeamID) string; GID int}.Load(team) (*Gated, error)`.
  - `declare.LXCMember` is
    `{Team paths.TeamID; Container string; User paths.GuestUser; Checkout string; Site string; Seat int; HumanText, Service bool; State State}`.
  - `declare.Team` has `ID, Name, ServerName, Port, URL, AllowFrom []string, Coordinator, …`.
  - `validContainer` is unexported (`declare/values.go:81`, `containerRe`).
- `status.Member{Handle, Kind, Role, State string; Online *bool; Rooms map[string]string}`.
  `status.OneLine(s)` is the printable-line check. `status.Write(path, r, gid)` is the status
  record's writer, the model for P3's.
- `state.ReadFile(path, limit) ([]byte, bool, error)` is the host's trust read: no link, a
  regular file, capped. `state.WriteFile(path, data, mode, gid) error` is the host's atomic
  write. Both are for host paths only, never below a guest root.

### seat (the owner's pool, which n3b does not call)

- `(*Store).Take(ctx, a *Alloc, t Take) (Taken, error)`.
- `(*Store).Release(ctx, r Release) error`.
- `(*Store).Announce(ctx, seat Seat, session paths.SessionName) error`.
- `(*Store).Holdings() ([]Holding, error)`.

These four are the owner's pool for ccy and host sessions. An LXC seat never enters the pool: the
child issues it with `--credential token` and keeps the guest token copy itself. So n3b uses
none of them.

What n3b does use from `seat`:

- `seat.Issued{Seat Bundle; Kind Kind; Secret Secret}`, through the bundle that admin's
  `Bundles` makes.
- `seat.DecodeMemberBundle`, which is not used for any host decision (n3-D23).

### member/guest, member/exits and member/state (n3a)

The guest ops' contracts, which TB2.4's parser must accept exactly:

- `OpCheck = Op{Verb: "guest check"}` prints nothing.
- `OpPlaceBundle = Op{Verb: "guest place-bundle", Changes: [seat, token, seat.json, store.json], Busy: true}`.
- `OpMembership = Op{Verb: "guest membership", Changes: [desired.json]}`.
- `OpRemove = Op{Verb: "guest remove", Changes: [seat], Busy: true}`.
- `OpSetAside` (`seat set-aside`) is not run by the host.

The lookups:

- `guest.ChangesOf(verb string) ([]What, bool)`.
- `guest.Ops() []Op`.
- `guest.SystemDirs` is `[/usr/local/bin /usr/bin /bin]`.
- `guest.UnitPath(home, system) string` is the unit's `PATH`.

`guest.Finish` makes an op's exit 0, 64, 75 (only when `Busy`) or 78. It makes the exit 1
`internal` for a bug or an I/O failure, and that line comes after a diagnostic line (n3a nit 8).
The error object is always the last line of stderr.

`exits.Decode(line []byte) (exits.Error, error)` reads one error object strictly: its six keys,
and an exit and code from the table. The result is `exits.Error{Exit term.ExitCode; Code string; Team *paths.TeamID; Message string}`.
The 78 codes the guest ops add are `identity`, `home`, `gh`, `tmux`, `claude`, `kernel` and
`reach`, beside B's `bundle`, `membership`, `store`, `forge-login`, `runtime` and `launch`.
`exits.ExitOf(code) (term.ExitCode, bool)`.

From `member/state`:

- `state.LockRecord{V int; Run paths.RunID; StartedMS int64; LaunchFailedMS *int64}`, read by
  `DecodeLockRecord(data)` and checked by `(LockRecord).LaunchFailed() bool`. `MaxLockRecord` is
  4 KiB.
- `state.SessionPID{V; Run; Keeper Process; Session *Process; Kit paths.Digest; Launch, Inbound, Kind string}`,
  with `Process{PID int; StartTicks int64}`, read by `DecodeSessionPID`. `MaxSessionPID` is
  1 KiB.
- `state.Desired{V int; Session paths.SessionName; Generation, WrittenMS int64; Seats []DesiredSeat; Farewell []Farewell; Inbound string}`,
  with `DesiredSeat{Team; Seat string}` and `Farewell{Team; Seat, Reason string}`.
  `(Desired).Encode()` writes it, and `DecodeDesired` reads it. GU4 refuses any `session` but
  `lxc-<user>`.

### paths

- `GuestUnitFile(u)` is `/etc/systemd/system/teamchat-session-<u>.service`.
- `GuestUnitWantsLink(u)` is its link in `multi-user.target.wants/`.
- `SessionUnit(u)` is the unit's name.
- `GuestRuntimeDir(u)` is `/run/teamchat-session-<u>`.
- `GuestRuntimeParents(u, uid) ([]string, error)` is the two run roots,
  `/run/user/<uid>/teamchat/run` and `/run/teamchat-session-<u>/teamchat/run`.
- `HostKitRoot` is `/usr/local/lib/teamchat/kit`, `HostKitDir(d) Kit` is one digest's kit, and
  `HostKitCurrent() Kit` is the `current` link. `Kit.Binary()`, `.PluginDir()`, `.Settings()` and
  `.String()` name the kit's parts.
- `MemberBinaryPath` is `/usr/local/bin/teamchat`.
- `HomeLayout(home, runtimeDir) (Layout, error)` gives `.Seat(team) Seat` with `.Lock()`,
  `.Dir()` and `.Bundle()`.
- `ParseRunID`, `ParseGuestUser`, `ParseDigest` and `ParseSessionName` are the grammars.
- `LXCRecord(t)` is `/var/lib/teamchat-status/lxc/<t>.json`.
- `AdminLXCDir` is `/var/lib/teamchat-admin/_lxc`.
- `GuestTokenCopy(t, n)`.
- `paths` has no container-name type and no path for the host's desired copy (gap 18).

### execx, the fakes, reexec and fakehs

- `execx.Runner` is `Run(ctx, Cmd) (Result, error)`.
- `Cmd{Name; Args; Env; Stdin io.Reader; Stdout, Stderr io.Writer; Credential; CleanEnv; …}`.
- `Result{Stdout, Stderr []byte; ExitCode int}`.
- `*execx.ExitError{Name, Code, Signal, Stderr}`.
- `execx.Fake` provides `On(prefix []string, r Result)`, `OnFunc(prefix, fn)`, `OnError` and
  `Calls()`.
- `reexec.Start(tb, name, args...) *reexec.Process` takes `Pid`, `Kill()` and `Release()`, with
  `reexec.Main(m, helpers)` as the package's `TestMain`. It gives a real second process that
  holds a lock (P1's `Lock` tests) or is a real pid for a pidfd.
- `fakehs` is the homeserver in tests. The LP2 rows run on it through `admin.Serve` (P4).

### internal/lxcfreeze, which owns the lxc programs

- `qa/rules/table.go:63-67` gives each of `lxc-ls`, `lxc-info`, `lxc-freeze`, `lxc-unfreeze` and
  `lxc-attach` the single owner `internal/lxcfreeze`.
- `lxc-attach` is a wrapper marked `inner` (`seamcommands.go:35`), so the program after its `--`
  is judged too. That program must be a literal: a non-literal one is reported as "cannot see
  which program", and no `dynamicNames` row excuses it (`walkProgram`, `seamcommands.go:310`).
- lxcfreeze runs every lxc program through `internal/sudo`, and it imports `internal/freeze` and
  `internal/ui`, which bring in the Charm TUI libraries. `Lister.InContainer` builds
  `sudo -n lxc-attach -P <lxcpath> -n <c> --clear-env -- runuser -u <user> -- env -i PATH=… tmux …`.
- So teamchat-admin, which runs as root and must not link a TUI, cannot call lxcfreeze. Gap 15
  is how this is closed.

## 2. The facts of C 3.9 and C 3.11, digested

**One present phase** (`lxc apply --phase present`, C 3.9.2) runs as root under the play lock,
with the declaration on stdin (n3-D21):

01. Validate every acted entry before touching any container:

    - its types are valid;
    - `(team, container, user)` is unique;
    - its team is declared present, has a stamp (`host.ReadStamp`) and has its homeserver
      active;
    - every acted team's `Input` passes `declare.Hold` against its approval, else 70 (C 3.14).

    The rules of `declare` already refuse two entries of one `(container, user)` that differ in
    `service` or checkout.

02. For each container, check its state. A container that is missing or not `RUNNING` (frozen
    included) is **offline** (n3b-D1, section 2a): it is held back, never an error, unless the
    run targets that one member with `--member`. The output line for it is `SKIPPED` when the
    record holds a handle, else `DEFERRED`, with why `offline: container stopped` or
    `offline: container missing`.

03. For each running container, read without changing anything:

    - Pin its init: take the pid, `pidfd_open` it, open `/proc/<pid>/root` as a directory fd,
      then check the pidfd is alive and that the pid is unchanged (n3-D7).

    - The container's config (n3-D22). `lxc.idmap` is refused by name. Read the network link and
      `lxc.start.auto`.

    - For `service: true`:

      - `lxc.start.auto` must be 1;
      - systemd must be init: the host's `/proc/<pid>/exe` opened `O_PATH` has the device and
        inode of the guest's `/usr/lib/systemd/systemd` (n3-D17);
      - `tmux` must be in a guest `PATH` directory. Each of `/usr/local/bin`, `/usr/bin` and
        `/bin` is tried in turn by TB1. `ELOOP` or a non-regular file at one entry means "not
        here", and the next is tried. A `tmux` reachable only through a link is a miss naming the
        path (EL9's `/bin` is a link to `usr/bin`).

      A miss names the setting and says that the container's own play owns its config. The apply
      never edits the container config.

    - The guest's `/etc/passwd` (1 MiB cap) and `/etc/group` (1 MiB cap, for the unit's
      `Group=`). The uid and gid must be nonzero, and the uid must not be any host account's
      (n3-D29). The name and home must pass their grammars.

    - The checkout's `.git/config` (64 KiB). `origin` is required, and any `insteadOf` is
      refused.

    - The checkout's `.claude/ccy/ccy.env.local` (64 KiB). It must be owned by the agent uid. A
      missing file is the site `local`.

    - The handle, at the first join only (n3-D10, TB2.3). The prefix is
      `<repo>+<site>.lxc`:

      - repo is `proto.NormaliseRepo` of the last path component of origin's URL, less `.git`,
        else of the checkout directory's base name;
      - site is the entry's `site` when it is set, else the `HOOKS_DAEMON_HOSTNAME` literal of
        `ccy.env.local` through `proto.NormaliseSite`, else `local`.

      The file is parsed and never sourced. Only `[export ]NAME=literal` lines are read, and a
      second assignment or any other form is refused. When the record already holds a handle, a
      prefix that differs from the handle's is refused with "the checkout's origin changed;
      retire and add the member to change its handle". The seat is the entry's, else the
      record's, else `next`.

    - The source address. One of the guest's addresses must be inside the team's `allow_from`,
      on a link that is the host bridge, `wire.Bridge`.

    - For a team whose stamp origin is `migration`, the v1 holdback. It goes through D's adapter,
      a seam until N6, which is handed the guest root and never the bare fd. A holdback is
      `DEFERRED (v1 not yet absent)`. Until N6, the production seam fails the phase naming N6
      for such a team. A team of origin `v2` never calls it.

    Any refusal fails the phase before any change. Steps 04 to 10 run only after every running
    container passed step 03.

04. The kit (LX5, n3-D26, n3-D27). A guest lacking `/usr/local/lib/teamchat/kit/<digest>/` gets
    it from the host's kit:

    - the copy is root-owned, with directories and binaries 0755 and files 0644, every mode set
      explicitly;
    - it is written under a temporary name and renamed in with `RENAME_NOREPLACE`;
    - then `current` (target `<digest>`) and `/usr/local/bin/teamchat` (target
      `/usr/local/lib/teamchat/kit/current/bin/teamchat`) are each made by `symlinkat` to a
      temporary name and `renameat2` over the old link.

    An old digest is removed only when it is not `current` and no live session names it. A live
    session is a `session.pid` under one of the two runtime parents, whose pid's start time
    (field 22 of the guest's `/proc/<pid>/stat`) equals the recorded `StartTicks`. A forged or
    undecodable record keeps the digest.

05. `guest check --user U --team T` for each acted entry, with
    `{"v":1,"team","base_url","plain_http_hosts"}` on stdin. Any failure fails the phase before
    any roster change.

06. Roster changes, under each team's lock in turn (`teamlock.Take`, `host.PlayWait`). A join is
    `member add` (no handle in the record), `member return` (the roster says parked) or
    `member reissue` (the roster says needs reissue), each with `--credential token`, and an add
    with `--kind lxc --seat <n|next>`. The answer's bundle is kept for step 07. Roles need no
    verb of their own (n3-D28).

07. Place each new bundle: `guest place-bundle --user U`, with the `seat.Issued` JSON on stdin.
    An exit of 75 is `DEFERRED<TAB><team><TAB><user>@<container><TAB>seat held; the new identity is placed at the next apply`,
    and the phase is not failed.

08. Membership, once per user after every team's placement. Compute the user's seats: every team
    whose record entry is joined, the teams outside `--only-teams` included (taken from the host
    copy, n3-D13), less a seat whose step 07 was `DEFERRED`. When the seats differ from the
    host's copy, two writes follow, the host's copy first:

    1. `/var/lib/teamchat-admin/_lxc/<container>/<user>.json` (root 0600, dir 0700), with
       `generation` one up;
    2. then `guest membership --user U`, with the whole `desired.json` on stdin. `session` is
       `lxc-<user>`, `farewell` is empty and `inbound` is `accept`.

    A converged apply writes nothing.

09. The unit, for `service: true` (SU1, SU2):

    - an unchanged unit text with its wants link in place is left alone;
    - a new unit is written with its link, then `Reload` and `StartUnit`;
    - a changed unit is written, then `Reload` and `RestartUnit`.

    A user with no team, or whose entries now say `service: false`, has the unit stopped, its file
    and link removed, then `Reload`.

10. Write the LXC record, only after the phase succeeded and only when it changed.

**The absent phase** runs first in the play, before any team is removed. A member leaves when its
entry says `state: absent`, or when the record names a `(container, user, team)` that no entry
declares. Each leaving member is handled under its team's lock:

1. `member park` first, which revokes the token.
2. Rewrite the desired file without the seat, with `farewell` `[{team, seat, "parked"}]`, the host
   copy first.
3. If the user now holds no team, stop the unit and remove its file and link.
4. Run the L3 probe every 1 s for at most 60 s, until the seat is free.
5. `guest remove --user U --team T`.

A seat still held after step 4 is
`DEFERRED<TAB><team><TAB><user>@<container><TAB>seat still held by run <id>; token revoked`. The
run id appears only if it passes `paths.ParseRunID`, and is left out otherwise. The record entry
is kept `left` with that `why`, the phase is not failed, and steps 2 to 5 run again at the next
apply.

A stopped container's member is parked from the host, and its entry stays `left` with its guest
files pending. A missing container's member is parked, and its entry is dropped. A guest op's 75
in this phase is the held seat above.

**Output** (3.9.3). The lines are `CHANGED|SKIPPED|DEFERRED<TAB><team><TAB><user>@<container><TAB><why>`,
then one `LXC-SUMMARY<TAB><text>`. Every field is the host's own, and no guest byte is ever
copied in (TB2.4).

The record is `/var/lib/teamchat-status/lxc/<team>.json`, 0640 root:`<owner group>`, written
atomically and only on change. Its form is
`{"v":1,"team":..,"members":[{"container","user","handle","status","since_ms","why"}]}`, where
`status` is one of `joined|offline|deferred|left` (n3b-D1 renames C's `stopped`). `since_ms` stays the same while the status
and the handle stay.

**The L3 probe** (C 3.9.2, LP1):

1. Open `seat.lock` under the user's seat directory, through the guest root, by TB1's read open.
2. Take `LOCK_SH|LOCK_NB` on the reopened fd. A lock taken means the seat is free, and the lock
   is released at once. `EWOULDBLOCK` means the seat is held.
3. Read the lock's content (4 KiB cap) in both states.
4. A refused open (a link on the path, a FIFO, any non-regular file) is free with no record.
5. A stopped container's seat is not held, and is reported `Stopped`.

The probe runs inside the health unit's sandbox, which has `CAP_SYS_PTRACE` for the
`/proc/<pid>/root` open. H-C10 checks that in the VM lab.

**C 3.11.2 step 6** (LP2, already built, see gap 17): when a handle that was held at the last
tick is now free, or its container has stopped, and the handle is still `active`, the tick asks
the child for `member.disconnected`. It passes the handle, the run id kept from the last tick and
`since_ms`. A free lock that still names that run with `launch_failed_ms` is not asked. The child
reads main for the member's latest own lifecycle notice since `since_ms`. Unless that notice is
`left` or `disconnected`, the child posts A's `disconnected` as the member, with the guest token
copy and transaction id `tc-disc-<run>`. A transient failure leaves the entry unposted.

**The unit text** (C 3.9.4) is quoted whole in DESIGN-v2-C-host.md lines 1863-1887. P2 renders
it byte for byte from that text, with `<user>`, `<group>`, `<home>` and `<checkout>` filled in.

## 2a. The offline rule (n3b-D1, the owner's, non-negotiable)

> **CORRECTION**: this section is superseded by SPEC-n3.md section 3.1, which holds the rule per
> step with the code's why strings. The table below predates n3b-D2 and the amended n3-D12: the
> targets are `lxc apply --member C/K` and `lxc restart --member C/K` (the seat key), not
> `--member C/U` and `--container C --user U`; the record is also written after each team's step
> 06 joins and at the end of a failed phase, not only after a phase that succeeded; and every
> offline why carries the `offline: ` prefix. Read SPEC-n3 section 3.1, not this table.

A member that is not running is the normal lifecycle, not a fault. An offline member is shown
clearly as offline. It never makes a play, a deploy, an acceptance or a tool fail, refuse or
wait. `lxc apply` applies every member it can reach, marks each one it cannot reach offline, and
exits 0 for the team.

**Offline** means any of these:

- the container is stopped, frozen or missing;
- the container stops during the phase: the init pin moves (`guestroot.ErrMoved`), an attach
  fails and `State` is no longer `RUNNING`, or the guest's private socket is gone;
- the member's session unit is down (`failed`, start limit hit, or its start refused).

An offline member is an error only when the operator targeted that one member explicitly:
`lxc apply --member C/U` (new, repeatable, which scopes the phase to the named entries) or
`lxc restart --container C --user U`. Then it is 69, naming the container. `--only-teams`
targets teams, not members, so it never makes an offline member an error.

What is **not** offline stays a failure. A running container's own facts that are wrong are
configuration errors the owner must fix, not a member coming and going: `lxc.idmap`, no
`start.auto`, systemd not init, no tmux, a uid of 0 or a host uid, `insteadOf`, a changed
origin, the source address, the guest check's `home`, `tmux`, `claude`, `kernel` and `identity`.
So are the host's own faults: an unapproved entry, an unreadable record, a homeserver that does
not answer. Each still fails the phase by name, before any change, as C 3.9.2 says.

The rule, step by step through C 3.9:

| Step               | What could block                                                                                                               | The rule                                                                                                                                                                                                                   |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 01 validate        | nothing of a member's state is read                                                                                            | unchanged: declaration and approval only                                                                                                                                                                                   |
| 02 state           | a stopped or missing container (C: "missing fails the present phase")                                                          | offline: `SKIPPED` (handle known) or `DEFERRED`, why `offline: container stopped` or `offline: container missing`; record `offline`; exit 0                                                                                |
| 03 reads           | the container stops mid-read (pin moved, root gone)                                                                            | offline for that container, which then skips steps 04-09; the others go on. A wrong fact of a running container still fails the phase                                                                                      |
| 03 v1 holdback     | not checked for an offline container                                                                                           | it is read only from a running one                                                                                                                                                                                         |
| 04 kit             | the container stops mid-write                                                                                                  | offline; the temp directory is left for the next apply to remove (its `.new-` name is never a digest)                                                                                                                      |
| 05 guest check     | `gh` or `reach` failing (the guest cannot reach GitHub or the homeserver), or the attach failing because the container stopped | offline (why `offline: guest check <code>`): that member is not joined this phase; the rest go on. The other codes fail the phase                                                                                          |
| 06 roster          | the member is offline before its join                                                                                          | no join is made for it; a known handle keeps its seat (`SKIPPED`, C 3.9.3)                                                                                                                                                 |
| 07 place-bundle    | the container stops after the join                                                                                             | offline, why `offline: bundle not placed`; the record entry is `offline` with that why, and the next apply that finds it running reissues (the new token is unplaced, so a `reissue` from host facts, n3-D23)              |
| 07 place-bundle 75 | a held seat                                                                                                                    | `DEFERRED`, unchanged (LX6)                                                                                                                                                                                                |
| 08 membership      | the container stops after the host copy is written                                                                             | offline, why `offline: membership pending`; an entry not `joined` reruns `guest membership` at the next apply even when the host copy is converged, so the guest is never left behind its copy                             |
| 09 unit            | the guest's systemd unreachable, or `StartUnit`/`RestartUnit` refused (start limit, a failed unit)                             | offline, why `offline: unit down` with `ccy-teams restart` as the fix; never a failure. An unchanged unit that is `failed` is reported offline and left alone                                                              |
| 10 record          | written only after the phase succeeded                                                                                         | the phase succeeds with offline members, so the record is written with them as `offline`                                                                                                                                   |
| absent 1 park      | a stopped or missing container                                                                                                 | unchanged: parked from the host at once, so nothing waits                                                                                                                                                                  |
| absent 3-5         | the 60 s L3 wait                                                                                                               | waits only for a running container whose seat is held; a container that stops mid-wait ends the wait at once (probe `Stopped`) and its guest files stay pending (`left`). A seat still held is `DEFERRED`, never a failure |
| `lxc restart`      | a stopped container                                                                                                            | explicit target: 69, naming the container and `lxc-start`                                                                                                                                                                  |
| LP1 probe          | a container stopped, missing, or stopping mid-probe (pin moved, root gone, `ESRCH`)                                            | `GuestProbe{Stopped: true}`, never an error. An error only for the host's own faults (an unreadable record)                                                                                                                |
| LP1 handle         | a handle the record does not name (a member joined some other way)                                                             | `Stopped` with no record, so the tick shows it offline and asks nothing                                                                                                                                                    |
| LP2 notice         | a stopped container                                                                                                            | unchanged: a notice, posted once per run; a transient failure is retried by the next tick and never fails the tick                                                                                                         |

Places the spec or the design said otherwise, now overridden by n3b-D1:

- C 3.9.1 and 3.9.2 step 02, and SPEC-n3 LX2: "a missing one is an error in the present phase".
  It is offline now.
- C 3.9.2 step 03 ("Any refusal here fails the phase") and SPEC-n3 LX4's last sentence: this now
  holds for a running container's wrong facts only, and a container that stops mid-read is
  offline.
- C 3.9.2 step 05 ("Any failure fails the phase before any roster change") and SPEC-n3 GU2: `gh`
  and `reach` are now offline for that member alone.
- C 3.9.3: the record's `stopped` is renamed `offline` and widened to every offline cause; the
  board (Task 4.3) shows it as offline.
- C 3.9.4 and SPEC-n3 SU2: a unit whose start is refused is offline, not a failure.
- SPEC-n3 LX6: "64 or 78 fails the phase" now excludes an attach that failed because the
  container stopped.
- SPEC-n3 LX8: the 60 s wait ends at once when the container stops.
- SPEC-n3 LP1: a probe that cannot reach a guest is `Stopped`. Today's `GuestsNotBuilt`, whose
  every probe is an error that the tick records as a problem, is replaced in P4.
- C 3.14's VM-lab row "an LXC member running, then stopped, then started" gains the assertion
  that the play with one member's container stopped exits 0 and places every other member.

Consequences per part:

- **P1:** `Pin` returns `ErrMoved` for a dead or changed init, and every `Root` call on a dead
  root returns an error wrapping `guestroot.ErrGone` (new). `lxccmd.Guest` returns
  `*lxccmd.AttachError` (new) when `lxc-attach` itself failed with no error object, so P4 can
  ask `State` again. Neither type decides offline: P4 does.
- **P2:** `guestbus` errors keep systemd's class (`ErrSystemd` with the D-Bus error name, or
  `ErrGone` for a closed peer), so P4 can tell "unit down" from a malformed answer.
- **P3:** `StatusOffline` replaces `StatusStopped`. `Entry.Why` carries the offline cause.
- **P4:** an `offline(err) (why string, ok bool)` classifier and the per-step rows above. The
  `--member` flag. Tests: one per row of the table, each asserting exit 0, the `SKIPPED` or
  `DEFERRED` line, the record's `offline` entry, and that the other members were placed. The
  `--member` and `lxc restart` rows assert 69. A test pins that no step sleeps or waits for an
  offline member: the fake clock's `Sleep` is never called for one.

## 3. P1: the guest root and the lxc programs (TB1, TB3, LX3, gap 15)

The packages are new: `internal/teamchat/host/lxc/guestroot` and
`internal/teamchat/host/lxc/lxccmd`. The QA rule change is in `src-go/qa/rules`.

Import graph (arrows are imports):

```text
guestroot -> paths, x/sys/unix, std
lxccmd    -> execx, paths, member/guest (ChangesOf), member/exits (Decode), term, std
```

### guestroot

```go
// Root is one running guest's root directory, held open with its init pinned (LX3, n3-D7).
// Every read and write below it keeps TB1. Safe for one goroutine at a time.
type Root struct{ /* root dir fd, pidfd, pid; the sys seam */ }

// Pinner opens a guest's root. Proc is "/proc" on a host; Kernel is the pidfd seam.
type Pinner struct {
	Proc   string
	Kernel Kernel
}
type Kernel interface {
	PidfdOpen(pid int) (int, error)
	PidfdAlive(fd int) error // pidfd_send_signal(fd, 0): ESRCH is dead
}
type LinuxKernel struct{}

// Pin: pid from pid(ctx); pidfd_open; open <Proc>/<pid>/root O_PATH|O_DIRECTORY|O_CLOEXEC;
// pidfd alive and pid(ctx) asked again, the same. A dead or different pid is ErrMoved.
func (p Pinner) Pin(ctx context.Context, pid func(context.Context) (int, error)) (*Root, error)

// OpenDir is a Root over a plain directory, with no init: the fixture trees' constructor
// (PID 0, Alive always nil). Tests only, but exported for P4's tests.
func OpenDir(dir string) (*Root, error)

var (
	ErrMoved    = errors.New("guestroot: the container's init changed while it was pinned")
	ErrRefused  = errors.New("guestroot: refused") // wrapped with the path and what it is
	ErrTooLarge = errors.New("guestroot: over its cap")
)

// Info is a guest inode by fstat.
type Info struct {
	Mode     fs.FileMode
	UID, GID uint32
	Dev, Ino uint64
	Size     int64
}

func (r *Root) PID() int
func (r *Root) Alive() error
func (r *Root) Close() error

// Stat: openat2 O_PATH|O_NOFOLLOW by TB1's flags, fstat, close. A non-existent path wraps
// fs.ErrNotExist; ELOOP wraps ErrRefused.
func (r *Root) Stat(path string) (Info, error)

// ReadFile: TB1's read (O_PATH, fstat regular, reopen through /proc/self/fd/<n> with
// O_RDONLY|O_NONBLOCK|O_CLOEXEC, device and inode again, read at most limit; over it is
// ErrTooLarge). found is false for a path that does not exist.
func (r *Root) ReadFile(path string, limit int64) (data []byte, info Info, found bool, err error)

// ReadProc is ReadFile without RESOLVE_NO_XDEV's absence mattering: the one read that crosses
// into the guest's /proc mount, for /proc/<pid>/stat (LX5). The path must be /proc/<pid>/stat.
func (r *Root) ReadProcStat(pid int) (data []byte, found bool, err error)

// ReadDir: the directory by TB1's flags with O_DIRECTORY; each name must pass valid, and more
// than max entries is ErrTooLarge. A missing directory is found false.
func (r *Root) ReadDir(path string, valid func(string) bool, max int) (names []string, found bool, err error)

// ProbeLock is the L3 probe's open: TB1's read open, then LOCK_SH|LOCK_NB on the reopened fd
// (taken and dropped at once: free; EWOULDBLOCK: held), and the content read (cap 4 KiB) in
// both states. A refused open is ErrRefused, which the probe reads as free with no record.
func (r *Root) ProbeLock(path string) (held bool, record []byte, err error)

// DialSocket: O_PATH below the root by TB1's flags, fstat S_ISSOCK and owned by uid 0, then a
// unix connect to /proc/self/fd/<n>. Anything else is ErrRefused; nothing is connected.
func (r *Root) DialSocket(ctx context.Context, path string) (*net.UnixConn, error)

// Writer is the root-write rules (C 3.9.1): RESOLVE_NO_XDEV added, every directory on the path
// opened O_DIRECTORY|O_NOFOLLOW and fstat'ed root-owned and not group or other writable.
func (r *Root) Writer() *Writer

// MkdirAll makes each missing directory 0755 root (fchmod explicit), refusing an existing one
// by the rules above; changed says whether one was made.
func (w *Writer) MkdirAll(path string) (changed bool, err error)
// WriteFile places data at dir/name by O_CREAT|O_EXCL|O_NOFOLLOW temp, fchmod mode, fsync,
// renameat2 (replace), dir fsync; the same bytes and mode already there is changed false.
func (w *Writer) WriteFile(dir, name string, data []byte, mode fs.FileMode) (changed bool, err error)
// PlaceDir fills a fresh temp directory under parent with fill, then renameat2 RENAME_NOREPLACE
// to name; a name already there is left as it is (changed false) after its own check.
func (w *Writer) PlaceDir(parent, name string, fill func(d *Dir) error) (changed bool, err error)
// Dir is a directory being filled: Mkdir and WriteFile relative to it, modes explicit.
type Dir struct{ /* fd */ }
func (d *Dir) Mkdir(name string) (*Dir, error)
func (d *Dir) WriteFile(name string, data []byte, mode fs.FileMode) error
// Symlink makes dir/name a link to target by symlinkat to a temp name and renameat2 over it; a
// link already naming target is changed false (readlinkat, never followed).
func (w *Writer) Symlink(dir, name, target string) (changed bool, err error)
// Remove unlinks dir/name, a file or a link only; absent is removed false.
func (w *Writer) Remove(dir, name string) (removed bool, err error)
// RemoveTree removes dir/name and everything under it through directory fds, following no
// link (an old kit digest).
func (w *Writer) RemoveTree(dir, name string) (removed bool, err error)
```

Facts the builder needs:

- All the system calls go through one unexported `sys` interface: `openat2`, `openat`,
  `fstat`, `flock`, `connect`, `renameat2`, `symlinkat`, `readlinkat`, `unlinkat`, `mkdirat`,
  `fchmod`, `fchown` and `fsync`. The real one is `x/sys/unix`, and the test one records every
  call around it. That is the opener seam TB1's test names.
- `unix.Openat2(dirfd, path, &unix.OpenHow{Flags, Mode, Resolve})` is the open. `Resolve` is
  `RESOLVE_IN_ROOT|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS`, with `RESOLVE_NO_XDEV` added for a
  write (n3-D14). A path must be absolute, clean and free of `..` (n3-D14); check that before the
  call.
- The reopen is `unix.Openat(unix.AT_FDCWD, "/proc/self/fd/"+n, O_RDONLY|O_NONBLOCK|O_CLOEXEC, 0)`.
  It has no `O_NOFOLLOW` and does not go through `openat2` (TB1), because either would fail with
  `ELOOP` on the magic link. Give it a named exemption: this is the one call exempt from TB4's
  `O_NOFOLLOW`.
- The root itself is opened as `<Proc>/<pid>/root`, a host path whose magic link must be
  followed: `unix.Open(path, O_PATH|O_DIRECTORY|O_CLOEXEC, 0)`. The test for it opens a real
  `/proc/<reexec pid>/root`.
- `flock` on an `O_PATH` fd is `EBADF`, so the probe's lock goes on the reopened fd only.
- `connect` to `/proc/self/fd/<n>` of an `O_PATH` socket fd is how the guest's private socket is
  reached without resolving a path in the guest. `net.DialUnix("unix", nil, &net.UnixAddr{Name: "/proc/self/fd/<n>", Net: "unix"})`
  does that. Close the `O_PATH` fd after the connect.

Tests:

- TB1 on fixture trees in `t.TempDir()`: a FIFO, a socket and a symbolic link (relative, and
  absolute to a host path) at every path P4's steps read. These are `/etc/passwd`, `/etc/group`,
  `.git/config`, `ccy.env.local`, `seat.lock`, `session.pid`, the unit file,
  `/usr/lib/systemd/systemd`, the tmux entries, `run/systemd/private` and the kit directories.
  - Each read refuses by name.
  - A writer goroutine polls each FIFO with `O_WRONLY|O_NONBLOCK` throughout and gets `ENXIO`
    every time.
  - Through the recording `sys`, no open but an `O_PATH` one reaches a non-regular inode.
  - A `run/systemd/private` that is a link (relative, and absolute to `/run/systemd/private`) is
    refused, and nothing is connected.
- A directory entry that fails its grammar is refused before it is joined.
- The real-kernel `openat2` runs on Linux (a build tag or `runtime.GOOS` guard).
- The write rules:
  - a group-writable or non-root-owned directory on the path is refused by name (the test runs
    as root in the container; a test that cannot `fchown` skips with the reason);
  - a bind mount below the root is refused by `RESOLVE_NO_XDEV` (Linux only, where the test can
    mount; otherwise prove the flag reaches `openat2` through the recording seam);
  - a replace leaves no temporary behind, and the same bytes are not rewritten.
- `ProbeLock` uses a reexec helper holding `LOCK_EX` (held), the same helper released (free), a
  `SIGKILL`ed holder (free), and a `seat.lock` that is a link, a FIFO or a directory (refused).
- `Pin` against a reexec helper's real pid: the pidfd is alive. When the helper is killed between
  the root open and the check, the result is `ErrMoved`. A `pid` function answering a different
  second pid is `ErrMoved` too.

### lxccmd

```go
// LXC is the lxc programs as root (no sudo: teamchat-admin is root), through Runner.
type LXC struct {
	Runner     execx.Runner
	DesktopUID int // checked against the attach (TB3); P4 passes the host-account refusal's result too
}

type State string // RUNNING, STOPPED, FROZEN, and the rest lxc-info prints
const Running State = "RUNNING"

func (l LXC) Containers(ctx context.Context) ([]string, error)                      // lxc-ls -1; each name by declare's container grammar
func (l LXC) State(ctx context.Context, c string) (State, error)                    // lxc-info -n c -sH
func (l LXC) InitPID(ctx context.Context, c string) (int, error)                    // lxc-info -n c -pH; > 1 and <= pid_max
func (l LXC) Addresses(ctx context.Context, c string) ([]netip.Addr, error)         // lxc-info -n c -iH
func (l LXC) Config(ctx context.Context, c string, keys ...string) (map[string]string, error) // lxc-info -n c -c k ... (n3-D22)

// Attach is who a guest op runs as and with which kit (C 3.9.1).
type Attach struct {
	Container string
	User      paths.GuestUser
	UID, GID  int
	Home      string
	Kit       paths.Kit // HostKitDir(digest): never current
}

// GuestResult is a guest op's output as TB2.4 reads it.
type GuestResult struct {
	Changed []guest.What
}

// GuestError is a guest op that failed: its exit, and its error object's code when the last
// stderr line decoded (else Code "", Undecodable true). Exit 75 only from a Busy op.
type GuestError struct {
	Op          string
	Exit        term.ExitCode
	Code        string
	Undecodable bool
}
func (e *GuestError) Error() string // names the op, the exit and the code; never a guest byte

const GuestWait = 60 * time.Second
const MaxGuestOutput = 64 << 10

// Guest runs `lxc-attach -n C -u UID -g GID --clear-env --set-var HOME=<home> --set-var USER=<u>
// --set-var LOGNAME=<u> --set-var PATH=/usr/local/bin:/usr/bin:/bin -- <kit>/bin/teamchat
// <op words> --user <u> [args]` with stdin, bounded by GuestWait; stdout and stderr each capped
// at MaxGuestOutput; uid or gid 0, or uid == DesktopUID, refused before anything runs (TB3).
func (l LXC) Guest(ctx context.Context, a Attach, op guest.Op, args []string, stdin []byte) (GuestResult, error)
```

Facts:

- `-H` prints the value alone. `-sH` prints `RUNNING` and the like, `-pH` the init pid, and `-iH`
  one address per line. `-c KEY` prints `KEY = VALUE`, once per key. Every line is parsed by a
  closed grammar, and anything else fails the call naming the program. stderr only ever feeds an
  error's text, as lxcfreeze does.
- A container that is missing comes from `Containers` (membership in `lxc-ls -1`). `lxc-info`'s
  failure text is never parsed.
- TB2.4's parser for stdout:
  - each line must be exactly `CHANGED\t<what>`, with `what` in `guest.ChangesOf(op.Verb)`.
  - anything else fails the op by name: an empty line, a `what` outside the set, an embedded
    `\r`, an ANSI escape, a line over 256 bytes, a forged `LXC-SUMMARY` or a forged
    `CHANGED\t<team>\t…` four-field line;
  - `guest check` must print nothing.
- For stderr, only the last line is decoded, by `exits.Decode`. Only its `Exit` and `Code` are
  kept, and each must be in its closed set.
- The exit must be 0, 1, 64, 78, or 75 when `op.Busy`. Any other exit, or a signal, is a
  `GuestError` with `Undecodable`.
- A token reaches the op only on stdin (`Cmd.Stdin`). An error never quotes stdin.

### The QA rule change (gap 15)

- `commandOwners` in `qa/rules/table.go` gains `internal/teamchat/host/lxc/lxccmd` as a second
  owner of `lxc-ls`, `lxc-info` and `lxc-attach`. The comment above the table says why there are
  two jobs, as it already does for `systemctl`: the freeze tool runs them through sudo, and
  teamchat-admin runs them as root and may not link the TUI.
- The rule learns one thing. A function with a `dynamicNames` row may hand a non-literal
  **inner** program to an owned inner-wrapper, here the kit's `bin/teamchat` after
  `lxc-attach --`. Without the row, the finding stays. The row is
  `{pkg: "internal/teamchat/host/lxc/lxccmd", fn: "LXC.Guest", reason: "the host-written kit binary <kit>/bin/teamchat, its digest path resolved once by the host and never current (C 3.9.1)"}`.
  - In code, `walkProgram` is given `ex.dynamic`. When the arguments end before a literal
    program, it reports only if the wrapper is `inner` and `ex.dynamic` is false.
  - The rule's fixture tests gain a red case (no row) and a green case (the row).
- The package doc of `project_seam_commands` and `CLAUDE/GoProjectRules.md`'s row for it each
  gain a sentence on the change.

Owns: TB1, TB3, LX3, the read and write mechanics of LX4 and LX5, LX6's call, the open half of
SU2, the open half of LP1, and gap 15.

## 4. P2: the guest's systemd and the unit text (SU1's text, SU2, gaps 13 and 16)

The packages are new: `internal/teamchat/host/lxc/guestbus` and
`internal/teamchat/host/lxc/unit`.

```text
guestbus -> godbus/dbus/v5, paths, std
unit     -> paths, std            (its test also imports member/cli)
```

### guestbus

```go
// Systemd is the guest's own systemd over its private socket (C 3.9.4, DC17).
type Systemd interface {
	Reload(ctx context.Context) error
	Start(ctx context.Context, user paths.GuestUser) error
	Restart(ctx context.Context, user paths.GuestUser) error
	Stop(ctx context.Context, user paths.GuestUser) error
	ResetFailed(ctx context.Context, user paths.GuestUser) error
	Close() error
}

const (
	CallWait   = 30 * time.Second // each call's bound (SU2)
	MaxMessage = 1 << 20          // a message's whole length, header and body (n3-D25)
)

var (
	ErrTooLarge = errors.New("guestbus: a message over its cap; the connection is closed")
	ErrMalformed = errors.New("guestbus: systemd's reply is malformed")
	ErrSystemd   = errors.New("guestbus: systemd refused")
)

// Conn is the part of a peer connection the client calls: godbus's in production, a fake in
// tests.
type Conn interface {
	Call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) ([]any, error)
	Close() error
}

// Dial is the production Conn over c, an already connected socket (guestroot.DialSocket):
// c wrapped in the capped transport, godbus.NewConn on it, AUTH EXTERNAL as uid 0, no Hello
// (peer-to-peer, n3-D24).
func Dial(ctx context.Context, c net.Conn) (Conn, error)

// New is the Systemd over a Conn. Every unit is paths.SessionUnit(user); there is no other.
func New(c Conn) Systemd
```

Facts:

- The private socket is peer-to-peer. There is no bus daemon, so there is no `Hello`, no
  `NameOwnerChanged` and no sender to check. Systemd itself is the only peer.

- The methods are on `org.freedesktop.systemd1.Manager` at `/org/freedesktop/systemd1`:

  - `Reload()`;
  - `StartUnit(s name, s mode)`, `StopUnit` and `RestartUnit`, with mode `"replace"`, each
    returning `o job`;
  - `ResetFailedUnit(s name)`.

  Each reply's body is type-checked: an `ObjectPath` for the job methods and empty for the
  others. Anything else is `ErrMalformed`.

- The call waits for the method's reply only, which means the job was accepted. It does not wait
  for `JobRemoved` (n3-D24). Each call runs under `context.WithTimeout(ctx, CallWait)`.

- godbus's `NewConn(io.ReadWriteCloser, ...ConnOption)` and `(*Conn).Auth([]Auth)`, with
  `AuthExternal("0")`, are the calls (`vendor/github.com/godbus/dbus/v5/conn.go:271`,
  `auth.go:54`). Pass `dbus.WithContext(life)` as `units/bus.go` does.

- The capped transport (n3-D25) is an `io.ReadWriteCloser` that reads ahead the 16-byte fixed
  header of each message:

  - the endianness byte must be `l` or `B`, and the version byte must be 1;
  - it computes `16 + fieldsLen + pad8 + bodyLen` from the header's `uint32` body length and the
    `uint32` length of the header-field array;
  - a length over `MaxMessage` closes the connection, and every later `Read` returns
    `ErrTooLarge`.

  The bytes of the SASL exchange before `BEGIN` are lines of at most 512 bytes, also capped. The
  wrapper never changes a byte.

- `godbus` stays out of `./cmd/teamchat`: `go-deps.allowlist` gains
  `keepout	github.com/godbus/dbus/v5	./cmd/teamchat	the member binary speaks no D-Bus (SPEC-n3 CL1, gap 13)`.
  The module row's reason gains "and an LXC guest's own systemd over its private socket, peer to
  peer (teamchat/host/lxc/guestbus, SU2)".

Tests:

- On a fake `Conn`, each method sends the expected name, path, method and arguments, and each
  wrong reply type is `ErrMalformed`.
- A call that never answers ends at `CallWait`: a fake that blocks until its context is done,
  with the bound passed as a parameter that the test shortens. The test does not sleep.
- Over a `net.Pipe`, a fake systemd peer completes the SASL exchange and answers a `StartUnit`.
  Then an oversized message (body length `MaxMessage`) closes the connection with
  `ErrTooLarge`, and godbus reads none of its body. An oversized SASL line is refused too.
- A peer that sends `NameOwnerChanged` or any signal changes nothing.

### unit

```go
// Session is a guest session unit's four host-read values, each grammar-checked by Render.
type Session struct {
	User     paths.GuestUser
	Group    string
	Home     string
	Checkout string
}

// Render is C 3.9.4's unit text with the values filled in, byte for byte. A value outside its
// grammar is refused by name: group by the user-name grammar; home and checkout absolute,
// clean, not "/", and only [A-Za-z0-9/._-] (no space, no '%' specifier, no '$', no newline).
func Render(s Session) ([]byte, error)

// ExecStart is the unit's command line split on spaces (Render's grammar makes that exact).
func ExecStart(text []byte) ([]string, error)
```

Tests:

- A golden file of the rendered unit, taken from DESIGN-v2-C-host.md lines 1863-1887.
- Every grammar refusal: a space, `%`, `$`, a newline, a relative path, `..`, and `/` alone.
- The CL5 test parses the rendered `ExecStart`. It drops tmux's own arguments
  (`/usr/bin/tmux -L teamchat new-session -d -s teamchat`), then checks that `cli.ParseLaunch`
  of the words after `/usr/local/bin/teamchat run` gives `--claude`, kind `lxc`,
  `--desired <home>/.config/teamchat/membership`, `--resume-recorded` and Claude at the expected
  index (SU1, B 2.4.1). n3a's own CL5 test already parses the other launch lines.

Owns: SU1's text, SU2's client, and gap 13 (the keepout row, the allowlist reason and the capped
reader).

## 5. P3: the guest's facts and the host's two LXC records (LX4's parsing, LX9's record, gaps 18 and 19)

The packages are new: `internal/teamchat/host/lxc/guestfacts` and
`internal/teamchat/host/lxc/lxcstate`. `paths` gains the file `paths/lxc.go`. In `declare`,
`validContainer` is changed to call `paths.ParseContainerName`.

```text
guestfacts -> paths, proto, declare (LXCMember), std
lxcstate   -> paths, proto (Parse), state (ReadFile, WriteFile), member/state (Desired), std
paths      -> (unchanged)
```

### paths additions

```go
// ContainerName is an LXC container's name, declare's grammar (moved here so a path can take it).
type ContainerName struct{ /* unexported */ }
func ParseContainerName(s string) (ContainerName, error)
func (c ContainerName) String() string
func (c ContainerName) MarshalText() ([]byte, error)
func (c *ContainerName) UnmarshalText(b []byte) error

// LXCDesiredCopy is the host's copy of a guest user's desired membership (C 3.9.3).
func LXCDesiredCopy(c ContainerName, u GuestUser) string // AdminLXCDir/<c>/<u>.json
// LXCSession is a guest user's session name, lxc-<user> (C 3.7.5; GU4 checks it).
func LXCSession(u GuestUser) SessionName
```

Register each new name in `sampleNames`, `zeroDerivations` and the method test's `valid` map.
`member/guest/places.go:264` may then use `paths.LXCSession`. That edit is P3's only touch of
n3a code, and it is optional.

### guestfacts

```go
type Account struct {
	User     paths.GuestUser
	UID, GID int
	Home     string
}

const (
	MaxPasswd    = 1 << 20
	MaxCheckout  = 64 << 10
	DefaultSite  = "local"
)

// Passwd finds u in a passwd file: uid and gid nonzero, home absolute and clean (the unit's
// grammar), the name exactly u. hostUIDs is every uid the host's own passwd names (n3-D29); a
// uid among them is refused naming K15.
func Passwd(data []byte, u paths.GuestUser, hostUIDs map[int]bool) (Account, error)
// HostUIDs is the uid set of a passwd file (the host's, read by state.ReadFile).
func HostUIDs(data []byte) (map[int]bool, error)
// GroupName is the name of gid in a group file (the unit's Group=).
func GroupName(data []byte, gid int) (string, error)

// Prefix is the handle prefix of an LXC entry's first join (C 3.7.4, 3.9.2 step 03): repo from
// gitConfig's origin, else the checkout's base name; site from the entry, else envLocal's
// HOOKS_DAEMON_HOSTNAME, else local; type lxc. envLocal nil is no file.
func Prefix(m declare.LXCMember, gitConfig, envLocal []byte) (proto.Prefix, error)
// SameMember refuses a derived prefix that differs from the recorded handle's (n3-D10).
func SameMember(derived proto.Prefix, recorded proto.Handle) error

// Config is the container settings the apply reads (n3-D22): the map is lxccmd.Config's.
type Config struct {
	Link      string
	StartAuto bool
}
func ParseConfig(kv map[string]string) (Config, error) // lxc.idmap set is refused by name

// Source checks one of addrs is inside a prefix of allowFrom, on link == bridge.
func Source(addrs []netip.Addr, allowFrom []string, link, bridge string) error

// StartTicks is field 22 of a /proc/<pid>/stat, read after the last ')'.
func StartTicks(stat []byte) (int64, error)
```

Facts:

- The git config is parsed as a closed form: a `[remote "origin"]` section with one `url =`.
  A section or key carrying `insteadOf` or `pushInsteadOf` (any case, in any section) refuses the
  file. No git program runs. The repo is `proto.NormaliseRepo` of the URL's last path
  component, less `.git`. A URL ends in `/`, `:` or a component. With no origin, the repo is the
  checkout directory's base name normalised (v1's rule, RESEARCH-lxc-robustness 1.2).
- `ccy.env.local` lines are blank, `#` comments, or `[export ]NAME=literal`, where the literal
  has no `$`, backtick, quote or space. Any other form refuses the file. A second
  `HOOKS_DAEMON_HOSTNAME` is refused. The value goes through `proto.NormaliseSite`, and a result
  that fails the site grammar is refused.
- `proto.ParsePrefix`, `Prefix.Numbered(n)`, `Handle.Prefix()` and `proto.ParseHandle` are the
  handle grammar (A 2.3). This package restates none of it.

### lxcstate

```go
const (
	StatusJoined   = "joined"
	StatusOffline  = "offline" // n3b-D1: C 3.9.3's `stopped`, renamed and widened
	StatusDeferred = "deferred"
	StatusLeft     = "left"
	MaxRecord      = 1 << 20
	MaxCopy        = 64 << 10
)

// Record is /var/lib/teamchat-status/lxc/<team>.json (C 3.9.3).
type Record struct {
	V       int          `json:"v"`
	Team    paths.TeamID `json:"team"`
	Members []Entry      `json:"members"`
}
type Entry struct {
	Container paths.ContainerName `json:"container"`
	User      paths.GuestUser     `json:"user"`
	Handle    string              `json:"handle"` // "" while deferred with no handle
	Status    string              `json:"status"`
	SinceMS   int64               `json:"since_ms"`
	Why       string              `json:"why"` // status.OneLine or ""
}
func DecodeRecord(data []byte, team paths.TeamID) (Record, error) // strict, (container, user) unique, sorted
func (r Record) Encode() ([]byte, error)
func (r Record) Find(c paths.ContainerName, u paths.GuestUser) (Entry, bool)
func (r Record) ByHandle(handle string) (Entry, bool)              // LP1's map from handle to guest
// Set returns r with e in place, since_ms kept when status and handle are unchanged.
func (r Record) Set(e Entry, nowMS int64) Record
func (r Record) Drop(c paths.ContainerName, u paths.GuestUser) Record
func ReadRecord(root string, team paths.TeamID) (Record, bool, error)
// WriteRecord writes r 0640 root:gid atomically when its bytes differ from the file's.
func WriteRecord(root string, r Record, gid int) (changed bool, err error)

// Copy is the host's copy of a guest user's desired membership (C 3.9.3), root 0600.
type Copy struct {
	V          int                 `json:"v"`
	Generation int64               `json:"generation"`
	Seats      []state.DesiredSeat `json:"seats"`
	Farewell   []state.Farewell    `json:"farewell"`
}
func ReadCopy(root string, c paths.ContainerName, u paths.GuestUser) (Copy, bool, error)
func WriteCopy(root string, c paths.ContainerName, u paths.GuestUser, cp Copy) error // dir 0700 root
// Next is the copy for seats and farewell, generation one up, or the same copy when nothing
// changed (changed false: a converged apply writes nothing).
func (cp Copy) Next(seats []state.DesiredSeat, farewell []state.Farewell) (next Copy, changed bool)
// Desired is the guest's desired.json for the copy (session lxc-<user>, inbound accept).
func (cp Copy) Desired(u paths.GuestUser, writtenMS int64) state.Desired
```

Tests:

- Each codec round-trips.
- Strict refusals: unknown keys, duplicate keys, `v` other than 1, a status outside the set, two
  entries of one `(container, user)`, and a `why` that is not one line.
- `since_ms` is kept through `Set` while the status and the handle stay, and moves when either
  changes.
- `WriteRecord` reports unchanged and does not write when the bytes are the same. The modes and
  the gid are checked (via a gid seam when the test is not root).
- `Next` raises `generation` only on a change. `Desired` passes `member/state`'s `DecodeDesired`
  and GU4's session check.

Owns: the parsing of LX4, the record of LX9, the host copy of LX7, TB2.3's comparison, and gaps
18 and 19.

## 6. P4: the phases, `lxc restart`, the L3 probe and the wiring (LX1-LX9, SU1's placement, SU3, LP1, LP2, gaps 10, 17, 20 and 21)

The packages:

- new `internal/teamchat/host/lxc`;
- edits to `internal/teamchat/admin` (`verbs.go`, a new `lxc.go`, `adapters.go`);
- edits to `internal/teamchat/admin/wire`.

```text
host/lxc   -> host/lxc/{guestroot,lxccmd,guestbus,unit,guestfacts,lxcstate}, health (GuestProbe),
              declare, status (OneLine), teamlock, paths, proto, member/guest, member/state, state
admin      -> host/lxc (new edge; host/lxc imports nothing of admin)
admin/wire -> host/lxc (the production Deps and Guests)
```

### API

```go
package lxc

type Phase string
const (
	PhaseAbsent  Phase = "absent"
	PhasePresent Phase = "present"
)

// Request is one `lxc apply`: the phase, the --only-teams scope (nil: every team), and the
// declaration read from stdin (n3-D21).
type Request struct {
	Phase     Phase
	OnlyTeams []paths.TeamID
	Decl      declare.Declaration
}

type LineKind string
const (
	Changed  LineKind = "CHANGED"
	Skipped  LineKind = "SKIPPED"
	Deferred LineKind = "DEFERRED"
)
// Line is one output line; String is `<kind>\t<team>\t<user>@<container>\t<why>`, every field
// the host's own.
type Line struct {
	Kind      LineKind
	Team      paths.TeamID
	User      paths.GuestUser
	Container paths.ContainerName
	Why       string
}
func (l Line) String() string
// Report is a phase's lines and its summary (`LXC-SUMMARY\t<Summary>`).
type Report struct {
	Lines   []Line
	Summary string
}

// Roster is the child's member work for one team, called while Apply holds that team's lock;
// admin's adapter implements it (gap 20).
type Roster interface {
	// Members is the team's roster states by handle (the child's team.view).
	Members(ctx context.Context, team paths.TeamID) (map[string]string, error)
	// Join is member add|return|reissue with --credential token; the answer's handle and the
	// seat.Issued bundle (JSON) for place-bundle.
	Join(ctx context.Context, team paths.TeamID, j Join) (Joined, error)
	Park(ctx context.Context, team paths.TeamID, handle string) error
	// Reach is guest check's stdin for the team: {"v":1,"team","base_url","plain_http_hosts"}.
	Reach(ctx context.Context, team paths.TeamID) ([]byte, error)
}
type Join struct {
	Op        string // apply.OpMemberAdd, OpMemberReturn or OpMemberReissue
	Prefix    proto.Prefix
	Seat      int // 0 is next
	HumanText bool
	Handle    string // return and reissue
}
type Joined struct {
	Handle string
	Bundle []byte // never logged, never in an error
}

// Containers is lxccmd.LXC as Apply uses it.
type Containers interface {
	Containers(ctx context.Context) ([]string, error)
	State(ctx context.Context, c string) (lxccmd.State, error)
	InitPID(ctx context.Context, c string) (int, error)
	Addresses(ctx context.Context, c string) ([]netip.Addr, error)
	Config(ctx context.Context, c string, keys ...string) (map[string]string, error)
	Guest(ctx context.Context, a lxccmd.Attach, op guest.Op, args []string, stdin []byte) (lxccmd.GuestResult, error)
}

// V1Holdback is D's adapter (a seam until N6): whether the guest still runs this member on v1.
type V1Holdback interface {
	Holds(ctx context.Context, r *guestroot.Root, u paths.GuestUser, team paths.TeamID) (bool, error)
}

type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

type Deps struct {
	Root       string // host path prefix ("" on a host)
	LockDir    string // paths.AdminRunDir
	Clock      Clock
	LXC        Containers
	Pin        func(ctx context.Context, container string) (*guestroot.Root, error)
	Bus        func(ctx context.Context, r *guestroot.Root) (guestbus.Systemd, error)
	Roster     Roster
	Approvals  declare.Approvals
	Origin     func(ctx context.Context, team paths.TeamID) (string, error) // the stamp's origin
	V1         V1Holdback
	Kit        paths.Digest      // the host kit's digest (n3-D26)
	HostKit    string            // the host kit directory, read by state.ReadFile per file
	HostUIDs   map[int]bool      // n3-D29
	OwnerGID   int
	Bridge     string            // wire.Bridge
	Refresh    func(ctx context.Context, team paths.TeamID) error // host.Status.Refresh after a change
}

const (
	SeatFreeWait = 60 * time.Second // the absent phase's L3 wait (C 3.9.2 step 4)
	SeatFreeGap  = time.Second
)

func Apply(ctx context.Context, d Deps, req Request) (Report, error)
// Restart is `lxc restart --container C --user U` (SU3): a member with no unit file is refused
// by name; else ResetFailed then Restart over SU2.
func Restart(ctx context.Context, d Deps, container paths.ContainerName, user paths.GuestUser) error

// Guests is the production health.Guests (LP1).
type Guests struct {
	Root string
	LXC  interface {
		Containers(ctx context.Context) ([]string, error)
		State(ctx context.Context, c string) (lxccmd.State, error)
	}
	Pin func(ctx context.Context, container string) (*guestroot.Root, error)
}
func (g Guests) Probe(ctx context.Context, team paths.TeamID, handle string) (health.GuestProbe, error)
```

Errors map onto the admin exits through `admin.ExitCodeOf`. A `*declare.RefusedError` or a step
03 refusal is 70: the refusals are `*host.Error{Verb: "lxc apply", Step: …, Drift: true}`, so the
existing mapping applies. A busy team lock is 75. A guest op's 64 or 78, and a malformed guest
answer, fail the phase as `*host.Error{Step: "guest " + op}`, which is 1. An offline member
(n3b-D1) is never an error and the phase exits 0 for it, unless `--member C/U` targets it: then
a stopped or missing container is 69, naming the container.

### The admin side

- `lxc apply`:
  - flags `--phase absent|present` (required) and `--only-teams T,…`;
  - stdin read under a cap (1 MiB) and decoded by `declare.Decode`;
  - stdout gets the report's lines, then `LXC-SUMMARY\t…`. A phase that failed prints the
    lines it reached before its error, as `printChanged` does.
- `lxc restart`: flags `--container C --user U`, nothing on stdin.
- `System` gains `LXC func() (*lxc.Deps, error)`, built at the first verb that needs it (n2f-W1).
- Gap 20, the refactor:
  - `underLock` is split. `memberWork(ctx, sys, op, team, args) (answer []byte, a *Answer, bundle proto.Value, err error)`
    does everything but take the lock.
  - `memberVerb` keeps taking the lock, then calls `memberWork`.
  - `lxcRoster` (new, in `admin/lxc.go`) implements `lxc.Roster` by calling `memberWork` while
    `lxc.Apply` holds the lock. `Join` builds the `[]proto.Member` that `memberArgs` would have
    built, with `kind` `lxc` and `credential` `token`.
  - `Members` asks `team.view` and maps `View.Members` handle to state.
  - `Reach` uses `team.json`'s URL and `plainHTTPHosts`.
  - `asked` still checks every answer, so a lying child is refused here too.
- `admin/wire`:
  - `System.LXC` is built from `lxccmd.LXC{Runner: execx.OS{}}`, with `guestroot.Pinner{Proc: "/proc", Kernel: guestroot.LinuxKernel{}}`
    as `Pin` (through `LXC.InitPID`), and `guestroot.DialSocket` plus `guestbus.Dial` as `Bus`;
  - `HostUIDs` from the host's `/etc/passwd`, read by `state.ReadFile`;
  - `Kit` from the host kit's `current` link, read by `readlinkat` and checked by
    `paths.ParseDigest` and root ownership (n3-D26);
  - `Origin` from `health.Stamps`;
  - `V1` is a `v1NotBuilt` that fails for a migration-origin team, naming N6;
  - the health tick's `Guests` becomes `lxc.Guests{…}`, replacing `GuestsNotBuilt` (gap 10).

### Facts it needs, beyond section 2

- **Locks.** Each team's lock is `teamlock.Take(ctx, d.LockDir, team, "lxc apply", host.PlayWait, d.Clock)`.
  The present phase takes each team's lock in turn for steps 06 and 07, never two at once. Step
  08 runs under the play lock only, with no team lock held, as C says. The absent phase holds
  the member's team lock through steps 1 to 5, so the health tick (`TryLock`) drops its view
  and posts no false `disconnected`.

- **Joins.** The kind of join comes from host facts only (n3-D23):

  - no record handle: `add`;
  - roster `parked`: `return`;
  - roster `needs-reissue` or `lost`: `reissue`;
  - roster `active` with a `joined` record: nothing (C: "a running and correct member is left
    alone").
  - A `retired` handle in the record is refused by name: retire and add.

  The roster's state names are `apply`'s: read them from `apply/files.go`'s roster states, not
  restated here.

- **The kit's bytes.** The host's kit directory is walked by the host's own trust reads
  (`state.ReadFile` per file, an `os.Root` walk, no link followed). The walk takes directories
  and regular files only, at most 6 levels deep and 64 MiB in total (n3-D27). It is then written
  through `Writer.PlaceDir` into `/usr/local/lib/teamchat/kit/.new-<digest>-<random>` and renamed
  to `<digest>`. The file modes are 0755 under `bin/` and 0644 elsewhere, and the directories are
  0755 (KT1).

- **The probe** (LP1):

  1. Read the team's LXC record with `lxcstate.ReadRecord`. `ByHandle` gives the container and
     the user, and a handle not in the record is an error (the tick records it as that member's
     problem).
  2. A container that `Containers` does not list, or one whose state is not `RUNNING`, is
     `Stopped`.
  3. Otherwise, pin the guest and read its passwd (TB1, `guestfacts.Passwd`, with no host-uid
     check here: the apply already enforced it).
  4. `paths.HomeLayout(home, paths.GuestRuntimeDir(u))` gives `.Seat(team).Lock()`, and
     `Root.ProbeLock` reads it. `ErrRefused` is free with no record.
  5. A record that does not decode by `state.DecodeLockRecord` is passed up as the bytes. The
     tick already treats an undecodable record as no run id (C 3.11.2 step 6, n3a nit 7: a torn
     record).

- **LP2** is already built (gap 17). P4 adds one fakehs test through `admin.Serve` with
  `member.disconnected`. It covers one post for a run however often it is asked, none after the
  member's own `left`, and a transient failure that posts nothing and answers an error. Each
  case is added only where `serveview_test.go` and `notice_test.go` lack it, and the report says
  which. The same commit updates PLAN.md's N2 caller list: the `notice` line also names the
  health tick's LXC path through `lxc.Guests` and `ChildAnnouncer`.

### Seams and tests

- `Apply` and `Restart` are tested on fixture guest roots made by `guestroot.OpenDir(t.TempDir())`,
  with a fake `Containers` (scripted states, pids and addresses, and guest ops that record their
  stdin and answer scripted results), a fake `Roster`, a fake `guestbus.Systemd` and a fake
  clock whose `Sleep` advances it. The test that wires the real `lxccmd.LXC` over `execx.Fake`
  pins the attach argv byte for byte.

- One test per expectation:

  - **LX1:** 70 on an unapproved entry; every entry validated before any `Containers` call.
  - **LX2:** missing, stopped and frozen, in both phases. The absent phase parks the member of a
    stopped container and keeps the entry `left`; for a missing container it parks the member
    and drops the entry.
  - **LX3:** a pid that moves is refused for the phase.
  - **LX4:** each refusal, with no change made: `idmap`, `start.auto`, systemd as init by inode,
    tmux on an EL9-shaped tree with `/bin` a link to `usr/bin`, a uid of 0 and a host uid,
    `insteadOf`, a `ccy.env.local` owned by another uid, a changed origin, the source address,
    and a migration-origin team with the not-built holdback (a FIFO planted at the v1 env-file
    path stays unopened).
  - **LX5:** the kit placed, `current` and the bin link swapped, an old digest kept for a live
    `session.pid` and removed when its start time differs, a forged record keeping it, and a
    group-writable `/usr/local/lib` refused.
  - **LX6:** each guest exit (0, 1, 64, 75, 78, a signal, an undecodable last line) and each
    hostile stdout row of TB2.4; a `place-bundle` 75 is `DEFERRED` and the phase goes on.
  - **LX7:** a converged apply writes nothing and attaches only for `check`; a user's seats keep
    its teams outside `--only-teams`; a deferred seat is left out of the host copy and
    `desired.json` through step 08; the host copy is written before `guest membership`;
    `generation` never goes down.
  - **LX8:** the order park, desired, unit, probe, remove, checked by the order of the fakes'
    calls; a held seat is `DEFERRED` with the run id when it passes its grammar and without it
    when it does not; a retry at the next apply.
  - **LX9:** the output grammar, with no guest byte in any line; the record only on change;
    `since_ms` kept.
  - **SU1:** an unchanged text is not restarted; a changed one gets `Reload` and `Restart`; a new
    one gets `Reload` and `Start`; `service: false` removes the unit.
  - **SU3:** no unit is refused; otherwise `ResetFailed`, then `Restart`.
  - **LP1:** held, free, stopped, missing, and a link or a FIFO at `seat.lock`, each giving the
    expected `GuestProbe`, plus a torn record.

- Coordinator: an LXC entry named as the team's coordinator, joined in one present phase, is the
  coordinator afterwards. The fake child records the `member.add` answer's promotion (n3-D28).

- The VM lab, run once P4 merges (n3b's acceptance, gate A for the host):

  - an EL9 LXC container with `service: true`;
  - the kit, bundle, `desired.json`, unit and wants link placed;
  - `StartUnit` accepted by the guest's systemd over the private socket (H-C9), or the
    `service: false` row;
  - the probe inside the health unit's sandbox (H-C10);
  - `lxc-info -c` checked against the container's config, includes and all (n3-D22);
  - GU1-GU5 from nothing (T15).

  `teamchat run` exits 1 until n3c, so a running session is not asserted (SPEC-n3 section 3).

## 7. Decisions n3b takes (now n3-D21 to n3-D30 and n3b-D1 in SPEC-n3 section 10)

- **n3b-D1** (the owner's rule): an offline member never makes anything fail, refuse or wait.
  Section 2a defines offline, gives the rule step by step, and lists every place it overrides.

- **n3-D21:** `lxc apply` reads the whole v2 declaration, by `declare.Decode`. That gives it the
  teams step 01 checks as well as the LXC entries. Each acted team's `Input` passes
  `declare.Hold`, else 70.

- **n3-D22:** container facts come from `lxc-ls` and `lxc-info` (`-sH`, `-pH`, `-iH`, `-c`), not
  from reading the config file, so LXC's own `lxc.include` handling decides what `lxc.idmap`
  is. The VM lab checks `-c`.

- **n3-D23:** whether a member needs a join is decided from host facts only: the LXC record and
  the child's `team.view`. The guest's bundle is never read for it (TB2). A guest that lost its
  bundle is healed by `ccy-teams reissue`.

- **n3-D24:** the guest's systemd is spoken to peer to peer, with AUTH EXTERNAL as uid 0 and no
  Hello. A job call waits for its reply (the job accepted) and not for `JobRemoved`, each within
  30 s.

- **n3-D25:** SU2's capped reader judges each message by its header (fixed part, fields and
  body) before godbus reads it. A message over 1 MiB closes the connection by name.

- **n3-D26:** the kit digest is the host kit's `current` link, root-owned and checked by
  `paths.ParseDigest`. A guest's existing `<digest>/` is kept as it is: only a rename makes one,
  and it is checked root-owned and not group or other writable.

- **n3-D27:** the kit is copied by a walk of the host kit (directories and regular files, at most
  6 deep, 64 MiB in total), so n3e's skill joins the kit with no change to n3b.

- **n3-D28:** `lxc apply` runs no role verb. Team apply (play step 12) demotes and leaves the
  declared coordinator wanted. The child's `member add` and `return` promote a wanted
  coordinator as they join it (`apply`'s `promoteWanted`), so step 06's order (demotions, joins,
  promotions) holds by construction.

- **n3-D29:** K15's refusal covers every uid that the host's own `/etc/passwd` names, not only
  the desktop user's. The host has no trusted record of which account is "the desktop user",
  and any host account that shares a kernel uid with a guest agent can reach its files.

- **n3-D30:** `lxc-ls`, `lxc-info` and `lxc-attach` gain the second owner
  `internal/teamchat/host/lxc/lxccmd` (gap 15), because teamchat-admin may not link the TUI or
  sudo. The inner kit binary after `lxc-attach --` is excused by a `dynamicNames` row.

## 8. Reports

- Each part writes `subagent-reports/261022-n3b-p<N>.md` in its worktree: what was built, the
  coverage per package, each red and green seen, and every signature it changed from this
  handoff, with the reason.
- Each part then goes through Opus review rounds to MERGE: `261022-n3b-p<N>-review.md`, then
  `-review-2`, and so on.
- P4 adds the n3b line to PLAN.md Task 3.3 in the same commit as its code. The line names what is
  built and what still waits for gate A: the play task that runs `lxc apply`, the host kit, and
  any host run.
