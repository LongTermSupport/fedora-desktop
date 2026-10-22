# n3b P4 handoff: steps (a) and (b) built and gated (Plan 00209 N3, slice n3b, part 4)

Two P4 builders have worked here. This file is what the next builder needs for steps (c) and
(d). Read it with the brief (`261022-n3b-handoff.md` sections 0, 2a, 6 and 7) and the P1-P3
reports' "Exported API as built" sections.

## State

- Branch `feat/bus-v2-n3b-p4`, worktree `untracked/worktrees/bus-v2-n3b-p4`. The package
  `src-go/internal/teamchat/host/lxc` holds step (a), the phases, and step (b), `lxc restart`.
- `go test -race -cover ./internal/teamchat/host/lxc/` passes at **91.0%**.
- `golangci-lint run` is clean on the package with and without `--build-tags tuwunel`, with
  no `//nolint`. `.golangci.yml` has one hunk for the package's tests
  (`internal/teamchat/host/lxc/[a-z_]+_test\.go`, gosec G101, G301, G302, G304, G306: fixture
  modes, fixture paths and seat handles), beside P1's guestroot hunk.
- qa-changed is green on each commit.

## Files

- `lxc.go`: the types (below).
- `apply.go`: `Apply`, step 01 (`validate`), the offline classifier and the present phase
  (steps 02, 03, 05-10).
- `kit.go`: step 04 (the host kit walk, the guest kit, the old-digest sweep).
- `absent.go`: the absent phase (LX8).
- `restart.go`: `Restart` (SU3).
- Tests: `world_test.go` (fakes and fixture guest trees), `apply_test.go`, `kit_test.go`,
  `absent_test.go`, `faults_test.go` (the fault paths), `restart_test.go`, `lxc_test.go`.

## The exported API as built

```go
type Phase string
const (PhaseAbsent Phase = "absent"; PhasePresent Phase = "present")

type Target struct { Container paths.ContainerName; User paths.GuestUser }
type Request struct {
	Phase     Phase
	OnlyTeams []paths.TeamID          // nil: every team
	Members   []Target                // --member C/U scope (n3b-D1); nil: every entry
	Decl      declare.Declaration
}

type LineKind string
const (Changed LineKind = "CHANGED"; Skipped LineKind = "SKIPPED"; Deferred LineKind = "DEFERRED")
type Line struct { Kind LineKind; Team paths.TeamID; User paths.GuestUser; Container paths.ContainerName; Why string }
func (l Line) String() string        // <kind>\t<team>\t<user>@<container>\t<why>
type Report struct { Lines []Line; Summary string } // printed LXC-SUMMARY\t<Summary>

type Roster interface {
	Members(ctx context.Context, team paths.TeamID) (map[string]string, error)
	Join(ctx context.Context, team paths.TeamID, j Join) (Joined, error)
	Park(ctx context.Context, team paths.TeamID, handle string) error
	Reach(ctx context.Context, team paths.TeamID) ([]byte, error)
}
type Join struct { Op string; Prefix proto.Prefix; Seat int; HumanText bool; Handle string }
type Joined struct { Handle string; Bundle []byte }

type Containers interface { Containers; State; InitPID; Addresses; Config; Guest } // lxccmd.LXC's
type Root interface {       // *guestroot.Root satisfies it
	Alive() error; Close() error; Stat(p string) (guestroot.Info, error)
	ReadFile(p string, limit int64) ([]byte, guestroot.Info, bool, error)
	ReadDir(p string, valid func(string) bool, limit int) ([]string, bool, error)
	ReadProcStat(pid int) ([]byte, bool, error); ProbeLock(p string) (bool, []byte, error)
	DialSocket(ctx context.Context, p string) (*net.UnixConn, error)
	InitExe() (guestroot.Info, error); Writer() *guestroot.Writer
}
type V1Holdback interface { Holds(ctx context.Context, r Root, u paths.GuestUser, team paths.TeamID) (bool, error) }
type Clock interface { Now() time.Time; Sleep(ctx context.Context, d time.Duration) error }

type Deps struct {
	Root, LockDir string
	Clock     Clock
	LXC       Containers
	Pin       func(ctx context.Context, container string) (Root, error)
	Bus       func(ctx context.Context, r Root) (guestbus.Systemd, error)
	Roster    Roster
	Approvals declare.Approvals
	Origin    func(ctx context.Context, team paths.TeamID) (string, error)
	V1        V1Holdback
	Kit       paths.Digest
	HostKit   string
	HostUIDs  map[int]bool
	OwnerGID  int
	Bridge    string
	Refresh   func(ctx context.Context, team paths.TeamID) error
}

const (SeatFreeWait = 60 * time.Second; SeatFreeGap = time.Second)

type OfflineError struct { Container paths.ContainerName; Why string } // exit 69; names lxc-start

func Apply(ctx context.Context, d Deps, req Request) (Report, error)
func Restart(ctx context.Context, d Deps, container paths.ContainerName, user paths.GuestUser) error
```

### Errors, and the exits admin must map them to

- `*host.Error{Verb: "lxc apply", Step, Drift}` for `Apply`; `Verb: "lxc restart"` for
  `Restart`. Drift is 70 (a refusal: `validate`, a wrong guest fact, a TB1 refusal), else 1.
  The existing `ExitCodeOf` mapping applies.
- A busy team lock is `teamlock.ErrBusy` inside a `host.Error{Step: host.StepLock}`: 75.
- `*declare.RefusedError` (an unapproved input): 70.
- `*lxc.OfflineError`: **69**. `ExitCodeOf` does not map it yet; step (d) adds it. `Apply`
  returns it only for a `--member` target, `Restart` for its one target. `Apply`'s error is
  every failed member's error, joined, so `errors.AsType` finds it in the join; the mapping
  must decide which exit wins when a join holds both an `OfflineError` and another error. The
  brief does not say; this is an open question for the lead, not decided here.

### `Restart` as built (SU3)

1. `LXC.Containers` failing is `host.Error{Step: "state"}` (1). The container not listed is
   `OfflineError{Why: "offline: container missing"}`; `State` failing or not `RUNNING`
   (stopped, frozen) is `OfflineError{Why: "offline: container stopped"}`.
2. `Pin`: `guestroot.ErrMoved` or `ErrGone` is
   `OfflineError{Why: "offline: container stopped during the restart"}`; any other error is
   `host.Error{Step: "pin"}`.
3. The unit file (`paths.GuestUnitFile(user)`) read through the root: none is refused by name
   (`host.Error{Step: "unit", Drift: true}`, "has no session unit ...; lxc apply places it");
   a TB1 refusal (a link) is the same step, Drift.
4. `Bus` failing to dial (not a refusal) is
   `OfflineError{Why: "offline: the guest's systemd is unreachable"}`; a refused socket is
   `host.Error{Step: "restart", Drift: true}`.
5. `ResetFailed` then `Restart`, then `Close`. `guestbus.ErrGone` is the container stopped
   during the restart (69); any other error (`guestbus.ErrSystemd`: the start refused) is
   `host.Error{Step: "restart"}` (1).

No team lock is taken: the restart changes no record and no membership. No line is printed:
the admin side prints its own success line.

## Signatures changed from the brief, with the reasons

- `Root` is an interface (the `*guestroot.Root` methods the phases use), and `Deps.Pin` returns
  `Root`, `Deps.Bus` takes `Root`, and `V1Holdback.Holds` takes `Root`. Reason: a fixture root
  (`guestroot.OpenDir`) has no init, so `InitExe` errors on it; the test wraps the fixture root
  to answer the systemd inode. Production passes `*guestroot.Root`, which satisfies it.
  The probe's `Guests.Pin` (step c) may keep `*guestroot.Root` as the brief says, or take this
  `Root`: the probe needs only `ProbeLock`.
- `Request.Members []Target` (`Target{Container, User}`) is the `--member C/U` scope (n3b-D1).
- `OfflineError{Container, Why}` is the explicit-target offline error.
- The team lock's holder word is `lxc-apply` (`teamlock` takes `^[a-z][a-z.-]{0,31}$`); errors
  still name the verb `lxc apply`.
- No `host.ReadStamp` check: the stamp is checked through `Deps.Origin` (production
  `health.Stamps`), whose error at step 01 fails the phase.

## Decisions taken in the code

- **The owner rule over the brief.** A running member's wrong fact (LX4) or a guest op's 64, 78
  or undecodable answer fails **that member alone**: no further step, its record entry left,
  the error kept. The rest of the team still applies, and `Apply` returns the joined errors
  with the report, so the verb exits 70 (drift) or 1 after printing the lines. The brief said
  "fails the phase before any change"; the common rules' owner rule says otherwise, and wins.
- **Reissue from host facts.** A recorded handle needs a reissue when its entry's why is
  `offline: bundle not placed`, `seat held; ...` or `bundle not placed; reissued at the next apply`. The child's `team.view` does not carry `needs`, so this is the record's own fact. An
  entry marked offline while it needs a reissue keeps the `bundle not placed` why.
- **Membership rerun.** `guest membership` reruns when the copy changed, or when the member's
  prior record status was not `joined` (so a pending membership is never left behind).
- **A down unit.** `ErrSystemd`, `guestbus.ErrGone` or a Bus dial failure (`errUnreachable`) is
  `offline: unit down; ccy-teams restart starts it`; its seat stays. The next apply restarts it
  even when the text is unchanged. Whether an unchanged unit is `failed` cannot be asked:
  `guestbus.Systemd` has no unit-state call.
- **The kit.** No line is printed for the kit. A leftover `.new-` temporary is listed but never
  removed: `guestroot` refuses `.new-` names in `RemoveTree`. That gap is P1's API, to report.
  A session record that cannot be read for sure (refused, or not decoding) keeps every old
  digest (`errKeepAll` in `kit.go`); a keeper whose `/proc` stat is gone is not live.
- **The absent phase.** Present teams only: the members of a team declared absent are not
  parked here. A left entry is not parked again. A stopped container: parked, the host copy
  rewritten, the entry `left` with `guest files pending`. A missing one: parked, entry dropped.
  The L3 wait asks `State` after each sleep, so a stop ends it at once. A member that never
  joined (no handle) is dropped with `entry removed; it never joined`, without a park or an
  attach. The container list failing in the absent phase is every leaver's container stopped.
- Homeserver-active is not checked at step 01: the first child call fails if it is not.

## What is left

1. Step (c): `Guests{Root, LXC, Pin}.Probe` (LP1), per the brief section 6 "The probe", with
   section 2a's overrides: a handle the record does not name is `Stopped`, not an error; a
   missing, stopped or stopping container is `Stopped`; a 5 s bound per probe (SPEC-n3 LP1),
   and health's `probeGuests` made parallel (SPEC-n3 LP1 says it changes in this slice).
2. Step (d): the admin side and `admin/wire`, per the brief section 6 "The admin side":
   - `lxc apply` (`--phase`, `--only-teams`, `--member C/U` repeatable, stdin under 1 MiB by
     `declare.Decode`, the report's lines then `LXC-SUMMARY\t...`, printed before the error);
   - `lxc restart --container C --user U` calling `lxc.Restart`;
   - `ExitCodeOf` mapping `*lxc.OfflineError` to 69 (and the join question above);
   - `System.LXC func() (*lxc.Deps, error)`, the production `Deps` (`*guestroot.Root` from
     `guestroot.Pin`, `guestbus` over `Root.DialSocket`, the `Roster` adapter over the child,
     gap 20's `underLock` split);
   - the LP2 fakehs test; the PLAN.md N2 caller-list line and the Task 3.3 n3b line (section 8).
3. The final report `261022-n3b-p4.md`.
