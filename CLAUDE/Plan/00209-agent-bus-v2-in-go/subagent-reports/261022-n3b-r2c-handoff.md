# n3b r2c handoff (Plan 00209 N3, slice n3b, part r2c: the admin verbs and the wiring)

Branch `feat/bus-v2-n3b-r2c`, worktree `untracked/worktrees/bus-v2-n3b-r2c`. The builder
stopped at its context budget after commit `c9aff2343`. Paths below are under
`src-go/internal/teamchat/`.

## Done (commit c9aff2343, qa-changed green, golangci clean with and without tuwunel)

- `admin/lxc.go`:
  - `lxc apply --phase absent|present [--only-teams T,...] [--member C/K]...`. stdin is read
    under `maxDeclaration` (1 MiB) and decoded by `declare.Decode`. stdout gets each
    `lxc.Line.String()`, then `LXC-SUMMARY\t<Summary>` (left out when `Apply` failed in
    validate and returned `Report{}`), before the error.
  - `lxc restart --container C --key K` calls `lxc.Restart`. It reads nothing on stdin and
    prints nothing on stdout.
  - `lxcDeps` takes `*System.LXC()` and adds `Roster: lxcRoster{sys}` and
    `Refresh: sys.Host.Status.Refresh(ctx, team, false)`. If `System.LXC` is nil the exit is 1.
    If it returns an error the exit is 78.
  - `lxcRoster` implements `lxc.Roster`:
    - `Members` comes from `ChildViewer.View` and maps `View.Members` handle to state.
    - `Join` is add (prefix, seat, kind `lxc`, human_text, credential `token`), return or
      reissue (handle, credential `token`), through `memberWork`. Its `Joined.Bundle` is
      `work.bundle.Compact()`. A `ChildError` of class retired wraps `apply.ErrRetired`, and
      any other op is an error.
    - `Park` is `memberWork(OpMemberPark)`.
    - `Reach` reads team.json and builds `{"v":1,"team","base_url","plain_http_hosts"}`.
- `admin/verbs.go`:
  - `System` gains `LXC func() (*lxc.Deps, error)`, and `rootRuns` gains both verbs.
  - `underLock` is now `memberWork(ctx, sys, op, team, args) (work, error)`, where
    `work{doc []byte; answer *Answer; bundle proto.Value}`. The brief's named results are
    refused by the `nonamedreturns` and `dogsled` linters. `memberVerb` keeps the lock.
- `admin/exit.go`: if the error tree holds an `*lxc.OfflineError`, `decisive(err)` walks the
  joins depth first and returns the first leaf that is not offline. The result is 69 only when
  every leaf is offline; otherwise the usual mapping applies to that leaf (n3b-D3).
- `admin/lxc_test.go`:
  - a stopped member that is not targeted exits 0, with a DEFERRED `offline: container stopped`
    line and the summary;
  - a stopped target alone exits 69;
  - a stopped target plus a running target refused at config (`lxc.start.auto` 0) exits 70;
  - `--only-teams`, every flag refusal (64), stdin over the cap, a stdin that is not a
    declaration, and an unreadable stdin;
  - the seams unwired (1) and failing (78), and an unwritable stdout;
  - restart: usage, no record (70), and a recorded offline seat in a stopped container (69);
  - the `ExitCodeOf` table;
  - the roster over the in-process child and fakehs: add, park, return, reissue, reach, an op
    that is not a join, a deactivated account returning `ErrRetired`, and a lying child.
- `admin/wire/lxc.go`:
  - `System.LXC` is `sync.OnceValues(m.lxcDeps)`, built from `lxccmd.LXC{Runner: m.runner}`.
  - `Pin` is `guestroot.Pinner{Proc, Kernel}` through `InitPID`.
  - `Bus` is `Root.DialSocket("/run/systemd/private")`, then `guestbus.Dial`, then
    `guestbus.New`.
  - `HostUIDs` is `state.ReadFile` of `/etc/passwd`, parsed by `guestfacts.HostUIDs`.
  - `Kit` comes from `currentKit`. The kit root is opened O_PATH. It must be owned by
    `kitOwner` (root in production) and not group- or other-writable. `current` must be a
    symlink owned by `kitOwner`, and its `readlinkat` target must pass `paths.ParseDigest`.
  - `HostKit` is `root+HostKitDir(kit)`, `Origin` is `health.Stamps.Origin` (new, with a test
    in `health/files_test.go`), `V1` is `v1NotBuilt` (an error naming N6), and `OwnerGID` is
    the owner group.
  - The health tick's `Guests` is `m.guests()`, which is
    `lxc.Guests{Root, LXC: lxccmd, Pin}` (gap 10).
  - The machine gains `kernel` and `kitOwner`.
- Coverage (`go test -race -cover`): admin 96.2%, admin/wire 91.5%, health 93.9%.

## Decisions to carry into the report

- `lxccmd.LXC.DesktopUID` is left 0. Every host passwd uid, the desktop user's included, is
  already refused for a guest member by `HostUIDs` (n3-D29) before any attach, and the host
  has no named source for "the desktop uid". This is open for the lead.
- `health.GuestsNotBuilt` and `ErrGuestProbeNotBuilt` now have no production caller. They are
  kept because `TestGuestsNotBuilt` also pins the tick's probe-error path. Deleting them is
  for the lead to decide.
- An invalid `--only-teams` team is 70 (`host.Error` Drift from validate) and has no summary,
  because `Apply` returns `Report{}`.

## Left

1. **The LP2 fakehs test** (brief section 6, "LP2"). Add one test in `admin/serveview_test.go`
   through `Serve` with `OpMemberDisconnected`, reusing `withMember`, `joinedWithCopy`,
   `runArgs` and `w.notices`. Cover what the existing tests lack:
   - one post however often it is asked: `TestServeMemberDisconnectedPostsTheNotice` asks twice
     but only checks `slices.Contains`; count exactly one `disconnected` notice after 3 asks;
   - none after the member's own `left`: missing in admin. `notice/notice_test.go`'s `w.post`
     shows how to compose a `left` notice as the member (`proto.VerifyMain` over its room
     state, then `proto.Composer{...}.Notice`, then `Send`);
   - a transient failure posts nothing and answers an error: the transient subtest checks the
     class but not "nothing posted", so add a check that the notices are unchanged.
     The report must say which cases were new.
2. The final report, `subagent-reports/261022-n3b-r2c.md`. Its content is the API and
   decisions above plus the LP2 note. Commit it.

PLAN.md is the lead's: it was not touched.
