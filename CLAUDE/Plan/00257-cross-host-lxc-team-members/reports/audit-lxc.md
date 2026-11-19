# Audit: LXC (host/lxc and member/guest)

Scope. Every production file was read in full, on branch `plan-xhost-lxc-members`:
`src-go/internal/teamchat/host/lxc` (`lxc.go`, `apply.go`, `absent.go`, `kit.go`, `probe.go`,
`restart.go`, `rotated.go`), its subpackages `guestroot`, `guestbus`, `guestfacts`, `lxccmd`,
`lxcstate`, `unit`, and `src-go/internal/teamchat/member/guest`. About 6,500 production lines
and 9,000 test lines on the host side, 1,050 and 1,870 on the guest side. Callers were read where
a finding needed them (`admin/lxc.go`, `admin/rotate.go`, `admin/wire/lxc.go`, `execx`, `clock`,
`unitfiles`). Tests run: `go test ./internal/teamchat/host/lxc/... ./internal/teamchat/member/guest/...`,
all eight packages pass. Test files were sampled, not read in full.

Paths below are relative to `src-go/internal/teamchat/` unless they start with `src-go/`.

What holds up. The openat2 discipline in `guestroot` is sound: every open below the guest uses
`RESOLVE_IN_ROOT|NO_SYMLINKS|NO_MAGICLINKS` (`host/lxc/guestroot/root.go:52`), writes add
`NO_XDEV`, each directory is checked by `fstat` of the opened descriptor and never by path
(`write.go:206-223`), non-regular inodes are never opened for reading (`read.go:264-283`), and
every guest read is capped. No escape, symlink race or unbounded read was found. Guest text is
kept out of host errors as designed. The findings are about what surrounds that core.

No CRITICAL finding.

## HIGH

### LXC-1 The LXC record is read before the team lock and written after it (lost update)

- Where: `host/lxc/apply.go:349-358` (read in `validate`, no lock), `apply.go:1061-1069` (the
  lock covers joins and bundle placement only), `apply.go:1128-1132` and `absent.go:239-245`
  (written under the lock from the stale copy), `apply.go:1367-1379` (`finish` writes again
  with no lock). `lxcstate.WriteRecord` (`host/lxc/lxcstate/lxcstate.go:267-289`) has no
  version or compare step.
- Wrong: a run holds one in-memory copy of the record for its whole life and writes it back
  whole. The present phase is long (per container: reads, kit, a `guest check` of up to 60 s
  per user and team), so the window is minutes.
- Scenario: the play's present phase is running. The owner runs `team rotate-ca`, which takes
  the team lock and marks every LXC seat `deferred` with `WhyCARotated`
  (`admin/rotate.go:127-150`, `host/lxc/rotated.go:13-24`). The play's `finish` then writes its
  older copy, in which those seats are `joined`. The mark is gone, so no later apply reissues
  them (`needsReissue`, `apply.go:120-126`). Their placed bundles hold the old CA, the record
  says `joined`, and those members cannot reach the bus until someone notices. A play run and a
  `lxc apply --member` run racing each other lose each other's entries the same way.
- Fix: read the record under the team lock and hold the lock from that read to the last write,
  or re-read and merge per entry under the lock in `recordJoins`, `parks` and `finish`. Add a
  test that changes the record file between `validate` and `finish`.
- Known weakness: no.

### LXC-2 A session unit that hit its start limit stays down, and the record still says joined

- Where: `host/lxc/unit/unit.go:77-78,91` (`StartLimitIntervalSec=600`, `StartLimitBurst=6`,
  `Restart=always`, `RestartSec=10`), `host/lxc/apply.go:1309-1313`,
  `host/lxc/guestbus/guestbus.go:7-8`.
- Wrong: six exits inside ten minutes put the unit in `failed` for good. For a seat recorded
  `joined` whose unit text is unchanged, `placeUnit` returns before it speaks to systemd at
  all, so apply never sees the failed state and never calls `ResetFailed`. Only a person
  running `lxc restart` recovers it (`restart.go:154-159`). Separately, a job call returns when
  systemd accepts the job, not when it finishes, so "unit started" and `joined` are recorded
  for a unit that then fails to start.
- Scenario: anything that makes the session exit repeatedly for a minute (a bad token after a
  reissue, a homeserver outage if the session exits on one) leaves the seat permanently down
  with the host record green. This is the "stuck session" the owner ruled out.
- Fix: decide the restart policy on purpose (no start limit, or `StartLimitIntervalSec=0` with
  a longer `RestartSec`), and have apply read the unit's `ActiveState` over the same socket,
  reset a failed unit and report it. Record `joined` only for an active unit.
- Known weakness: no.

## MEDIUM

### LXC-3 `lxc-ls`, `lxc-info` and the systemd handshake have no bound

- Where: `host/lxc/lxccmd/lxccmd.go:298-311` (no timeout; only `Guest` has one,
  `lxccmd/guest.go:104`), `host/lxc/guestbus/guestbus.go:90-107` (`conn.Auth` is bounded only
  by the caller's context), `admin/lxc.go:28-59` (the verb's context has no deadline).
- Wrong: the package says every guest call is bounded. These are not.
- Scenario: a guest whose `/run/systemd/private` accepts a connection and never answers the
  SASL line (a hung or stopped PID 1 is enough, no malice needed) hangs `lxc apply` for ever.
  In the absent phase `removeUnit` runs inside the team lock (`absent.go:186`, `:393`), so
  every other verb for that team then waits out `PlayWait` (10 minutes) and fails. A hung LXC
  monitor does the same through `lxc-info`.
- Fix: give `LXC.run` a bound like `GuestWait`, and wrap `Dial` in the `CallWait` bound.

### LXC-4 A phase has no overall deadline, so slow guest answers add up to hours

- Where: `host/lxc/guestroot/bound.go:52` (10 s per call), `host/lxc/kit.go:189`, `:220-309`
  (the kit sweep reads through the unscoped root: up to 4,096 run parents, 1,024 runs each, two
  reads per run), `host/lxc/guestroot/root.go:52` (reads may cross mounts).
- Wrong: the stall memo only remembers calls that reach the full 10 s. A call that answers in
  9 s is paid in full every time.
- Scenario: `/run/user/<uid>` belongs to the guest user. With FUSE usable in the guest, that
  user mounts a slow filesystem there holding 1,024 run-id directories. Each `session.pid`
  read takes just under the bound, and the present phase for the whole host (every team and
  every container, processed in order) waits hours. Guest root can do the same anywhere.
- Fix: a per-container budget for a phase (a total bound, after which the container's seats
  fail), and a small cap on runs scanned per parent.

### LXC-5 Host faults in the absent phase and `lxc restart` are reported as "offline"

- Where: `host/lxc/absent.go:320-322` (any `Pin` error, a host fault such as `EMFILE`
  included, marks the container stopped), `restart.go:58-61` (any dial error that is not a
  refusal is reported as the guest's systemd unreachable).
- Wrong: the present phase separates a gone container from a host fault
  (`apply.go:583-592`); the absent phase and `restart` do not. A host fault is shown as a
  skipped offline member with exit 0 for the phase.
- Fix: use the same `gone` test in `leaveBox` and `Restart`; fail on anything else.

### LXC-6 The handle's repository name is taken from a file the agent owns

- Where: `host/lxc/apply.go:875` (reads `<checkout>/.git/config`), `guestfacts/guestfacts.go:322-361`.
- Wrong: at first join the member's handle is `<repo>+<site>.lxc-<seat>` with `<repo>` read
  from the guest user's own git config. It is pinned afterwards (`SameMember`), so this is
  trust on first use of an untrusted value.
- Scenario: a guest agent sets its origin to another project's name before its first apply and
  joins under that project's handle. The real checkout, joining later in another container, is
  refused ("its handle is held by", `apply.go:920-923`), and peers address the impostor.
- Fix: declare the repository name in the entry (as the site already is, X25) and treat the
  guest's value as a check only.

### LXC-7 Only privileged containers are accepted, so guest root is close to host root

- Where: `host/lxc/guestfacts/guestfacts.go:247-249` (any `lxc.idmap` is refused),
  `host/lxc/guestroot/root.go:134-138` (guest root is host uid 0).
- Wrong: this is by design (K15 relies on kernel uids), but it sets the real boundary. The
  ownership checks in `guestroot` defend against the guest's ordinary users, not against guest
  root, who shares uid 0 with the host. The package comments call the guest "untrusted" without
  saying which account. Root on the homeserver host also runs `lxc-attach` into every member
  container on each play.
- Fix: state the threat model in the package doc. For Plan 00257, prefer a design in which no
  root process on the bus host reaches into a member (see "Cross-host" below).

### LXC-8 `lxc-attach` inherits the caller's session and controlling terminal

- Where: `src-go/internal/execx/execx.go:178` (`Run` never sets `Setsid`),
  `host/lxc/lxccmd/guest.go:112-123` (stdin, stdout and stderr are pipes).
- Wrong: with no terminal on its three descriptors `lxc-attach` allocates no pty, so the guest
  process stays in the operator's session. The guest user owns that process (same uid) and can
  drive it. Whether it can then inject input into the operator's terminal depends on the
  kernel's `TIOCSTI` setting, which was not checked (see Unconfirmed).
- Fix: run `lxc-attach` in a new session with no controlling terminal. This costs one field.

### LXC-9 Membership changes wait on GitHub

- Where: `member/guest/check.go:128-130` (`gh auth status`, 30 s bound),
  `host/lxc/apply.go:1020-1022`.
- Wrong: a failing `gh` check marks an already joined seat `offline` and skips its remaining
  steps for that run, including a pending reissue after a CA rotation. A GitHub outage or a
  guest with no internet route therefore delays bus repairs that have nothing to do with gh.
- Fix: make gh a warning for a seat that is already joined, or check it only at first join.

### LXC-10 Test gaps on the paths that matter most

- `guestroot`'s write and ownership tests run on fixture roots where the directory's owner
  "plays root" (`guestroot/root.go:218-247`); no test exercises `fchown` to uid 0 or the
  `UID != 0` refusal against a real pinned container. `Pin` itself is tested against a real
  process.
- No test covers a record changed by another verb during a phase (LXC-1), a unit in `failed`
  (LXC-2), or a peer that never answers `Auth` (LXC-3).
- No VM lab scenario runs `lxc apply` against a real container (`vars/vm-test-scenarios.yml`
  has none for teamchat).

## LOW

- LXC-11 `host/lxc/apply.go:1314-1317`: after a run that wrote a changed unit but failed to
  restart it, the next run sees the file unchanged and calls `Start`, which does nothing on a
  running unit. The old definition keeps running while the record says joined.
- LXC-12 `host/lxc/apply.go:1338-1350`: `removeUnit` removes the unit file before the wants
  link. If the link removal fails, the next run finds no file, returns early and leaves a
  dangling link for ever. Remove the link first.
- LXC-13 `host/lxc/absent.go:206` says each park is recorded at once; the code records them in
  one write after all parks (`absent.go:239-245`). The run converges after a crash, so only the
  comment is wrong.
- LXC-14 `admin/wire/lxc.go:93-107`: the K15 uid set is the host's `/etc/passwd` only. Uids
  from `DynamicUser=` units (the homeserver's own included), subuid ranges and directory
  services are not in it.
- LXC-15 `host/lxc/kit.go:144-147`: an existing kit directory in the guest is trusted by its
  name and owner; its content is never compared with the host kit. `readHostKit`
  (`kit.go:57-90`) does not check content against the digest either.
- LXC-16 `host/lxc/apply.go:875` with `NO_SYMLINKS`: a checkout that is a git worktree or
  submodule (`.git` is a file) is refused as "passes through something that is not a
  directory". The message does not say why.
- LXC-17 `host/lxc/guestroot/write.go:335-347`: `writeAll` loops for ever on a write that
  returns 0 with no error. The 10 s bound hides it from the caller.
- LXC-18 `admin/wire/lxc.go:155-161`: `v1NotBuilt` fails every LXC member of a
  migration-origin team. It is a wired seam whose only behaviour is an error.
- LXC-19 `member/guest/places.go:295-311`: `guest remove` leaves the seat directory and
  `seat.lock` behind in the guest for ever.
- LXC-20 `host/lxc/lxccmd/guest.go:125-128`: on the 60 s timeout `lxc-attach` is signalled,
  but the process it started in the guest may live on and finish its work later, beside a
  retry.

## Suspend and resume

- Same-host LXC members freeze and thaw with the homeserver, because they share its kernel.
  There is nothing for them to ride out, and this code adds no failure of its own.
- Every bound here is a Go timer on the monotonic clock, which stops during suspend
  (`clock/clock.go:70-72`, `context.WithTimeout`). A suspend mid-apply stretches a bound; it
  never fires one early. `waitFree`'s deadline (`absent.go:492`) carries a monotonic reading
  too. `since_ms` and `WrittenMS` are wall-clock stamps used for display.
- The one wall-clock-sensitive piece is `Probe`'s boot-clock bound (`probe.go:49`): a suspend
  during a probe fails that probe once. The next tick repeats it.
- The risk is in LXC-2. Once a member's host is not the homeserver's host, the homeserver can
  be suspended while the member runs. If the session exits on an unreachable homeserver, six
  restarts in ten minutes wedge the unit.

## Cross-host: what is same-host only, what is reusable

Same-host by construction (none of it can serve a container on another machine):

- `guestroot` (a pidfd and `/proc/<pid>/root` of a local process), and everything built on it:
  the kit write, the unit write, the L3 lock probe, the dial of the guest's systemd socket.
- `lxccmd` (the local `lxc-ls`, `lxc-info`, `lxc-attach`).
- The bridge rules: `bridgeURL` (`apply.go:407-419`), `guestfacts.Source`
  (`guestfacts.go:269-284`) and the play's failure on `WhyReach`.
- `lxcstate`: a seat is keyed by container name with no host, so two hosts' containers collide
  (`lxcstate.go:69-78`, `lxc.Target`).
- The absent phase's order (park, then wait for the seat lock through the root) and the health
  tick's presence reading both need the L3 probe.

Reusable as it stands:

- `member/guest` (check, place-bundle, membership, remove). These run inside the guest as the
  user, take their input on stdin and need no root reach. Any transport can drive them (ssh, a
  local play on the other host) in place of `lxc-attach`.
- The join exchange: `Roster.Join` returns a bundle and `place-bundle` consumes it. Nothing in
  it names a host. The reach facts (`base_url`, `tls_ca`) are host-agnostic already.
- `unit.Render` and the `guestfacts` parsers are pure functions.

Obstructive: X5 (W3), the host-less seat key, kit delivery by root write, and a presence
reading that depends on a file lock the bus host must reach.

## Unconfirmed

- U1 (with LXC-8): whether an attached guest process can inject input into the operator's
  terminal. It needs `dev.tty.legacy_tiocsti` enabled on the host and `lxc-attach` keeping the
  controlling terminal; neither was checked on a host.
- U2 (with LXC-2): whether `teamchat run` exits when the homeserver is unreachable. That is the
  member area's code. If it does, a suspended homeserver wedges remote units.
- U3: after a reissue (new token, perhaps a new CA) the host calls `Start`, not `Restart`
  (`apply.go:1310-1317`), and `place-bundle` rewrites the files in place for the same identity
  (`member/guest/places.go:183-190`). Whether a running session picks up the new token and CA
  without a restart was not traced into `member/keeper`.
- U4: Debian guests of older releases ship `/usr/local` group-writable; `Writer.walk` would
  refuse the binary link there. Not checked against a real guest.

## Bloat and debloat candidates

- If LXC containers on other hosts join as ordinary clients, same-host containers can too.
  That retires `guestroot`, `guestbus`, `lxccmd`, `lxcstate`, `guestfacts` and most of
  `apply.go`/`absent.go`/`kit.go`/`probe.go`: about 6,500 production and 9,000 test lines,
  plus root's reach into guests (LXC-7, LXC-8). `member/guest` and `unit.Render` stay.
- `apply.go` (1,415 lines) carries one `member` struct with ten status booleans (`done`,
  `failed`, `joined`, `settled`, `complete`, `heldSeat`, `recorded`, `target` and two status
  strings). The legal combinations are not stated anywhere; an explicit state type would
  remove most of the conditionals in `offline`, `fail`, `boxGone` and `settle`.
- Two parsers of passwd exist for the same file: the host's (`guestfacts.Passwd`) and the
  guest's (Go's `os/user`, `member/guest/identity.go:160-175`). They can disagree on a
  duplicated name (the host refuses, the guest takes the first).
- `V1Holdback` and `v1NotBuilt` (LXC-18) can go until N6 exists.
- `Guests.running` and `run.stopped`, and `gone` in `probe.go` and `run.gone` in `apply.go`,
  are near-duplicates.
