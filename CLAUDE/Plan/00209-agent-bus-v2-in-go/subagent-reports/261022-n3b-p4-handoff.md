# n3b P4 handoff: step (a) built, not yet gated (Plan 00209 N3, slice n3b, part 4)

The first P4 builder stopped at its context budget. This file is what the next builder needs.
Read it with the brief (`261022-n3b-handoff.md` sections 0, 2a, 6 and 7) and the P1-P3 reports'
"Exported API as built" sections.

## State

- Branch `feat/bus-v2-n3b-p4`, worktree `untracked/worktrees/bus-v2-n3b-p4`. One WIP commit
  holds step (a), the phases, in the new package `src-go/internal/teamchat/host/lxc`.
- `go test -race -cover ./internal/teamchat/host/lxc/` passes at **84.5%**: under the 90% floor.
- `golangci-lint run ./internal/teamchat/host/lxc/` is **not clean**: about 45 findings. They are
  mechanical: gofumpt, funcorder in the fakes, `err` shadowing, `hostFault` must be named
  `...Error` (errname), nilerr in `kit.go` `runsOf` (the "keep every digest" returns), gocognit
  on two tests and `runsOf`, goconst `"membership"`, revive comments on the const blocks,
  prealloc, `maps.Copy`, unparam on `mustUser` and `(*world).entry`. The gosec findings are all
  in test fixtures (0755/0644 modes, file paths): add a `.golangci.yml` hunk for
  `internal/teamchat/host/lxc/.*_test\.go` beside P1's guestroot one (G101, G301, G302, G304,
  G306), as P1 did.
- qa-changed was not run. Run it after the lint and coverage are fixed.

## Files

- `lxc.go`: the types, as the brief's API, with the changes below.
- `apply.go`: `Apply`, step 01 (`validate`), the offline classifier and the present phase
  (steps 02, 03, 05-10).
- `kit.go`: step 04 (the host kit walk, the guest kit, the old-digest sweep).
- `absent.go`: the absent phase (LX8).
- Tests: `world_test.go` (fakes and fixture guest trees), `apply_test.go`, `kit_test.go`,
  `absent_test.go`, `lxc_test.go`.

## Signatures changed from the brief, with the reasons

- `Root` is an interface (the `*guestroot.Root` methods the phases use), and `Deps.Pin` returns
  `Root`, `Deps.Bus` takes `Root`, and `V1Holdback.Holds` takes `Root`. Reason: a fixture root
  (`guestroot.OpenDir`) has no init, so `InitExe` errors on it; the test wraps the fixture root
  to answer the systemd inode. Production passes `*guestroot.Root`, which satisfies it.
- `Request.Members []Target` (`Target{Container, User}`) is the `--member C/U` scope (n3b-D1).
- `OfflineError{Container, Why}` is the explicit-target offline error. admin must map it to 69
  in `ExitCodeOf` (not done yet).
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
- **The absent phase.** Present teams only: the members of a team declared absent are not
  parked here. A left entry is not parked again. A stopped container: parked, the host copy
  rewritten, the entry `left` with `guest files pending`. A missing one: parked, entry dropped.
  The L3 wait asks `State` after each sleep, so a stop ends it at once.
- Homeserver-active is not checked at step 01: the first child call fails if it is not.

## What is left

1. Step (a)'s gate: lint clean (with and without `--build-tags tuwunel`), coverage to 90%, the
   `.golangci.yml` hunk, qa-changed green, then a real commit. Coverage gaps to fill: the
   `readHostKit` errors, `runsOf`'s branches, `leaveFailed`, the absent phase's park failure and
   no-handle leaver, `guest place-bundle` failing in a running container, `Roster.Members`
   failing, `joinedAs` lies, `Origin`/`Approvals.Load` errors, `Containers` failing.
2. Step (b): `Restart(ctx, d, container, user) error` (SU3): missing or stopped container is an
   `*OfflineError` naming `lxc-start`; no unit file is refused by name (`host.Error` Drift);
   else `ResetFailed` then `Restart` over `d.Bus`.
3. Step (c): `Guests{Root, LXC, Pin}.Probe` (LP1), per the brief section 6 "The probe", with
   section 2a's overrides: a handle the record does not name is `Stopped`, not an error; a
   missing, stopped or stopping container is `Stopped`; a 5 s bound per probe (SPEC-n3 LP1),
   and health's `probeGuests` made parallel (SPEC-n3 LP1 says it changes in this slice).
4. Step (d): the admin side and `admin/wire`, per the brief section 6 "The admin side"; map
   `*lxc.OfflineError` to 69; the LP2 fakehs test; the PLAN.md N2 caller-list line and the Task
   3.3 n3b line (section 8).
5. The final report `261022-n3b-p4.md`.
