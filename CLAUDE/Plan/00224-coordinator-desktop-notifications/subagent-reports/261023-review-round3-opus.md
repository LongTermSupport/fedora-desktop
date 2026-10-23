# Plan 00224 review, round 3 (Opus, adversarial)

Scope: `git diff c15620d06..HEAD` (the whole plan), with the round-2 fixes in
`git diff b981c4974..HEAD`. Plan only. Every finding is against the text and was checked
in the repository.

## Round-2 findings: are they resolved in the text?

| R2  | Resolved? | Where / evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| --- | --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| N1  | Yes       | PLAN Task 4.1:161-170 and DESIGN 4:285-294 now extend the one `CCY_SEAT_RUN_ARGS+=(…)` statement (`agent-bus-membership.bash:67`), say membership-init still prints only the directory (`membership.go:182`), and make the CCY bump unconditional. The file has 57 code lines and 4 functions (the cap). Extending one statement in place adds no line. A `\` continuation would count as two (`bashlimits/measure.go:24`), so "on the same line" is load-bearing, and `qa-bash-limits` is named as the proof. |
| N2  | Yes       | The identity record is gone. The relay joins host-written `membership.json` with a seat map that `agent-bus seat take` writes at mint time (DESIGN 3.2:102-123, D14; Task 3.2). Verified below: `seat take` is the single mint path for checkout seats, it runs on the host as the user, and it holds the minted handle. Hot add reads the map instead of `seatHeld` (Task 3.2:151-152).                                                                                                                       |
| N3  | Yes       | DESIGN 3.3:196-199: the result is created with `O_EXCL`, then `fchmod` 0644. The 0700 member directory keeps other guest accounts out. Task 2.6:118 tests it against the umask, and PLAN premise :61-62 adds it to triage.                                                                                                                                                                                                                                                                                     |
| m-a | Yes       | DESIGN 3.2:90, 3.4:223-225; Task 2.4:102.                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| m-b | Yes       | DESIGN 3.2:140-144 (a name filter for a plain-terminal launch); Task 2.4:98-99. See m3 below for one remaining sharpening.                                                                                                                                                                                                                                                                                                                                                                                     |
| m-c | Yes       | Task 5.1:193-195 and DESIGN 4:279-281: built from the unfiltered `agent_bus_lxc_members`, with a test of a filtered run.                                                                                                                                                                                                                                                                                                                                                                                       |
| m-d | Yes       | D12 now refuses automatic adoption; the owner confirms each seat after the registry check (DESIGN 3.2:125-131, D12). The trust-on-first-launch window is gone.                                                                                                                                                                                                                                                                                                                                                 |
| m-e | Yes       | Task 4.1:169-170 pins the new `-v` and `-e` in `scripts/test-ccy-agent-bus.bash`, red first.                                                                                                                                                                                                                                                                                                                                                                                                                   |

## Factual claims verified

- **`seat take` mints through `add-member` and sees the handle.** `checkout.take_actions`
  builds one `add-member … --seat=<n> --out=-` per missing seat, with
  `AddMember(ref, identity.handle(ref), …)` (`helpers/agent_bus/checkout.py:244-249`).
  `_take` then runs it through `system.agent_bus` and places the returned bundle, whose
  `member.json` carries the handle (`checkout.py:876-899`, `emit(... action.handle)`). The
  registry rejects a handle that is already a current member (`registry.py:185-186`). So the
  handle `seat take` holds is the one the registry minted, or the parked one it returned
  (`admin.py:1088-1094`). The premise at PLAN:63-64 is in effect already settled by the code.
- **It runs on the host as the owner, before the container starts.** `ccy_seats_take` runs
  `ccy_membership_args` (membership-init), then `"$CCY_AGENT_BUS_COMMAND" seat take` with its
  output sent to stderr, then `describe-seats` (`lib/agent-bus-seats.bash`, ccy_seats_take). This
  all happens in the launcher, before `podman run`.
- **Hot add uses the same code.** `SeatTakeAllocated` runs `agent-bus seat take <list> --type=…`
  as the user in the checkout directory (`src-go/internal/agentbus/agentbus.go:172-181, 323`).
  It is called from `hot/add.go:280`. That reaches `take(..., alloc_held=True)`
  (`checkout.py:846-847`), which goes through the same `_take`, so the writer in Task 3.2 covers it.
- **No other mint path.** `add-member` is called only from `checkout.py`, by `seat take`.
  `cc-teams.bash:97` also uses `seat take`, with `--type=host`, so cc seats land in the map too.
  That is harmless, because the direct route has no gate. LXC members come from
  `lxcrecord`, not from the map.
- **Hot add's handle source.** For a new seat it comes from `seatHeld`, which reads
  `agent-bus seat list`, the checkout's seat files (`hot/add.go:287-296`, `hot.go:617-628`).
  For an existing seat it comes from the checkout list (`add.go:194, 209`). The plan names this
  correctly.
- **The launcher statement.** `agent-bus-membership.bash:62` captures one path, `:67` is the
  single mount statement, and `${session:-$container}` is the key (`:62`). These match
  DESIGN 3.2:93-95 and Task 4.1.

## BLOCKER

None.

## MAJOR

None.

## MINOR

- **m1. `session role` still takes the handle from the checkout.** Task 3.2:151-152 limits the
  "read the map" change to `hot/add.go`. But `ccy-teams session role` (H25), the usual way the
  owner makes a running session coordinator, resolves the handle through `handleOf`, which
  calls `seatHeld` and so reads the checkout's seat files (`hot/role.go:157, 230-241`).
  A worker that rewrites its `member.json` steers the owner's promotion onto whatever handle it
  names. The gate stays consistent with the registry, so this opens no notify bypass. It does
  mean the coordinator the owner meant may be refused 77, while the handle the worker named is
  promoted. Extend Task 3.2 to `role.go`'s `handleOf`, and say what both do for a seat with no
  map entry: refuse, naming adoption, or fall back with a warning. The text leaves this open,
  and an unadopted pre-plan seat is the common case right after deploy.
- **m2. Adoption should refuse a handle another map entry already holds.** D12's check is that
  the claimed handle is in the registry for "this checkout's seat number and this host's
  `<host>.<type>`" (DESIGN 3.2:126-129). A container can create a fake seat directory in its own
  checkout's seats tree. Take two clones of one repository on this host: one holds coordinator
  seat 1, and the other has a forged `seat 1/member.json` that claims `…-1`. Both pass the
  check. Adopting the forged one and then launching that clone into seat 1 would let it notify
  as the coordinator, and no real token is needed for that. This depends on an owner mistake.
  The cheap guard: adoption refuses a handle already present in the map, or held by another
  live session's membership. Add the case to Task 3.2's `ccy-teams` bullet.
- **m3. Prune races the launch.** The relay prunes "any spool directory with no running
  container, when it starts and hourly" (DESIGN 3.2:144-145). membership-init creates the spool
  before `seat take`, which may sit at a sudo prompt, and before `podman run`. A prune in that
  window removes the empty directory, and `podman run -v <missing>:…` fails the launch. Prune
  only directories older than a grace period, or whose `membership.json` is absent or older than
  the session's container. Also say that a failing `podman ps` never prunes (fail closed). The
  same applies to the liveness rule "by label, or by exact container name": choose by whether
  the key is a labelled session. Otherwise an unrelated running container whose name equals a
  dead tmux session's name passes liveness for that session's leftover spool.
- **m4. The seat map has three writers and no lock.** The writers are `seat take` (Python),
  `ccy-teams` adoption (Go) and possibly hot add. Each does read-modify-write of one
  `seats.json`. The allocation lock is per checkout (`hot/lock.go`, `checkout.allocation`), so
  launches in two checkouts can lose an update. The result fails safe: the coordinator is
  refused 77 "needs adoption", which misleads. Task 2.5's schema section should name one
  `flock` on a sibling lock file, which every writer takes. `registry.lock` is the precedent
  (`registry.py:330`).
- **m5. The checkout key must be spelled one way.** `seat take` keys by the git top level,
  which is symlink-resolved (`checkout.find_checkout`, `checkout.py:585-590`). `membership.json`
  holds the launcher's `$PWD`, which is clean and absolute but not resolved
  (`membership.go:202`, `agent-bus-membership.bash:62`). A checkout reached through a symlink
  would never match, and its coordinator would be refused 77. `hot/lock.go:91` already
  resolves for this reason. Task 2.5 should state the canonical form, and test a symlinked
  checkout.
- **m6. A ccy session on the docker engine.** ccy runs under `CCY_CONTAINER_ENGINE=docker`
  (`lib/common.bash:33`). The relay's liveness check is `podman ps`, so such a session's spool
  is pruned and every request refused. The plan says "podman ccy" throughout but never states
  that docker is out of scope. Say so, and have the client or relay report it plainly rather
  than as "not the coordinator".
- **m7. A spool only for the ccy kind.** Task 4.1 says membership-init creates the spool "for
  every session carrying the bus kit". membership-init also serves `--kind=cc`
  (`membership.go:186-196`, `cc-teams.bash`), where a spool is useless and the relay would only
  prune it again. Say "ccy kind only".
- **m8 (nit).** The decisions table lists D14 before D13 (DESIGN:328-329).

## Checked and fine

- No path the plan opens lets a container choose its identity. The ccy spool is mounted only
  into its own container. `membership.json` is mounted read-only, the seat map lives in
  `$XDG_STATE_HOME` and no container mounts it, and the role comes from `sudo -n agent-bus list`
  at request time.
- The LXC result is readable by the member: the file is 0644 inside a directory whose owner
  is the member. The relay's unlink works because the access ACL gives the owner `rwx`.
  tmpfiles applies `a+` after the `d` line for the same path, so the mask survives.
- PLAN Overview, Goals, the premises and Success criteria agree with DESIGN 3.x after the
  round-2 edits. The journal records rounds 1 and 2. No install-specific names were found.
- Placement is right. The relay lives with `play-agent-bus.yml` and the image binary with
  `play-claude-yolo.yml`. The bash change is one statement. The Python change is a small
  addition, not a rework (D14 names the Go alternative), which fits CLAUDE.md's
  standalone-Python-CLI rule.

VERDICT: MERGE
