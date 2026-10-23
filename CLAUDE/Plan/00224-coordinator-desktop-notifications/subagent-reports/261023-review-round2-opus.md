# Plan 00224 review, round 2 (Opus, adversarial)

Scope: `git diff c15620d06..HEAD` (the whole plan), with the round-1 fixes in
`git diff 207244ac5..HEAD`. Plan only: every finding is against the text, checked in the
repository.

## Round-1 findings: are they resolved in the text?

| R1  | Resolved?                    | Where / note                                                                                                                                                                                                                                                                     |
| --- | ---------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B1  | In design, yes; in route, no | DESIGN 3.2:102-132 moves the handle to a host-only seat map and identity record and states the shared-checkout residual honestly. Task 2.4 has the rewritten-`member.json` test, and so does 7.1. But the launch-time writer it names cannot see a new seat's handle (N2 below). |
| M1  | No                           | Task 4.1 / DESIGN 4:271-276 now say the mount arguments travel in "the output the launcher already consumes". They cannot: see N1.                                                                                                                                               |
| M2  | Yes                          | Task 5.1: marked `blockinfile`, `optional,create=dir`, removed with the last member, the owner's lines tested untouched. One new concern (m-c).                                                                                                                                  |
| M3  | Yes                          | Target `var/lib/agent-notify`, premise PLAN:57-61 names the masking.                                                                                                                                                                                                             |
| M4  | Yes                          | DESIGN 3.3:168 and 184-189; Task 2.2 tests `../` and stem mismatch.                                                                                                                                                                                                              |
| M5  | Yes                          | DESIGN 3.3:194-202; Task 2.6.                                                                                                                                                                                                                                                    |
| m1  | Yes                          | Task 3.1, DESIGN 4:257.                                                                                                                                                                                                                                                          |
| m2  | Yes                          | Task 2.7 (tsv row, layout line, allowlist), Task 4.2 (Dockerfile link; matches `Dockerfile:439-440`).                                                                                                                                                                            |
| m3  | Yes, but too strict          | See m-a.                                                                                                                                                                                                                                                                         |
| m4  | Yes                          | DESIGN 3.2:154-159, D13.                                                                                                                                                                                                                                                         |
| m5  | Yes                          | DESIGN 3.1:71-75; Task 2.7.                                                                                                                                                                                                                                                      |
| m6  | Yes                          | DESIGN 3.2:93-95. But see m-b.                                                                                                                                                                                                                                                   |
| m7  | Yes                          | DESIGN 3.4:206.                                                                                                                                                                                                                                                                  |
| m8  | Yes                          | DESIGN 3.3:169-182.                                                                                                                                                                                                                                                              |
| m9  | Yes                          | DESIGN 3.5:226, 3.6:244-249; Task 3.1.                                                                                                                                                                                                                                           |
| m10 | Yes                          | DESIGN 3.4:212, 220.                                                                                                                                                                                                                                                             |
| m11 | Yes                          | DESIGN 4:271.                                                                                                                                                                                                                                                                    |
| m12 | Yes                          | JOURNAL 11:26 decision entry.                                                                                                                                                                                                                                                    |

## BLOCKER

None.

## MAJOR

**N1. The launcher cannot take extra container arguments from membership-init's output, so
`lib/agent-bus-membership.bash` must change. This is the round-1 M1 fix, and the premise it
rests on is false.** `membership-init` prints exactly one line, the directory
(`src-go/internal/teams/membership/membership.go:182`, `m.Host.Print(m.Host.Stdout, dir+"\n")`).
The bash captures it as a scalar and builds the arguments itself:
`dir=$(ccy-teams internal membership-init ...)` (`agent-bus-membership.bash:62`), then
`CCY_SEAT_RUN_ARGS+=(-v "$dir:/run/ccy/membership:ro${CCY_MOUNT_RELABEL:+,Z}" -e PINGBUS_MEMBERSHIP=/run/ccy/membership)`
(`:67`). Nothing printed there can become a second `-v` or `-e`. A "sibling verb whose output
the existing call already consumes" (DESIGN 4:273-274) does not exist either. The `cc --kind=cc`
path reads the same stdout contract.

So Task 4.1's "gains no line and no function" and "CCY version bump if any launcher or `lib/`
file changes" (PLAN:154-159) are wrong as written. The cheap honest route: extend the existing
array assignment on line 67 with the spool's `-v` (rw, same relabel) and `-e NOTIFY_OWNER_SPOOL`.
That is one statement and no new line, with the path derived as membership's is (or printed
on a second line, which then needs a second capture). This keeps the file inside its hard
limits (about 57 code lines, 4 functions now), and `qa-bash-limits` proves it. The
CCY version bump is then certain, not conditional. Say this in Task 4.1 and DESIGN 4, and drop
the claim that the bash is untouched.

**N2. At launch, the identity record is written before the seat it must name exists, and no
named writer has the handle afterwards.** The order in the launcher is fixed:
`ccy_seats_take` calls `ccy_membership_args` (so membership-init) first, then
`agent-bus seat take` (`lib/agent-bus-seats.bash:81-87`). Task 4.1 has membership-init "write
the identity record". For a seat that `seat take` is about to mint, the seat map has no entry
yet, so the identity record lacks it. `seat take`'s output goes to stderr (`:87`, `>&2`), so the
handle Task 3.2 wants it to "report" reaches no Go writer unless bash is changed.
`agent-bus-seats.bash` is not on the allowlist and is at the 4-function cap.

Hot control has the same gap in another form. The writer exists and is Go
(`src-go/internal/teams/hot/hot.go:591`, `d.Generation++`). But `hot/add.go:280` discards
`SeatTakeAllocated`'s output, and `add.go:287-296` takes the new seat's handle from
`seatHeld`, which is the checkout's seat list. That is exactly the untrusted source B1 removed.
An existing seat chosen in a hot add takes its handle from the checkout too
(`add.go:194, 209`).

The plan needs to name the route. Options:

- the seat map is written by `seat take` itself on the host (Python, the minting side), and
  membership-init and hot only read it;
- or the identity record is written by the Go step that already runs after `seat take` at
  launch (`ccy-teams describe-seats --block`, `agent-bus-seats.bash:89`), or by a new
  `ccy-teams internal` call in that same statement;
- in either case, hot add uses the seat map, never `seatHeld`'s handle.

Task 2.5 / 3.2 should test the ordering: a launch that mints a seat ends with that seat's handle
in the identity record.

**N3. An LXC member cannot read the result the relay writes.** The member directory is
`0700 <uid>` with an access ACL for the owner (DESIGN 3.2:146-147; Task 5.1). The relay runs as
the owner, so `<stem>.result` is created owned by the owner's uid, with a mode the plan never
states (DESIGN 3.3:188). The client in the guest runs as the member's uid, which differs from the
owner's (D13 refuses the case where it does not). With a 0600 file and no default ACL, the
client cannot open its result, waits 10 s and exits 69. Every LXC notice then fails acceptance,
even when it was shown.

This does not arise for ccy, where the container's root is the owner. State the result's
access. Two options: an `fchown`/`fchmod` to the request's owner uid on the `O_EXCL` file
(the relay can `fchown` only as root, so this probably means mode 0644), or a tmpfiles `A+`
default ACL granting the member `r`. Add the case to Task 2.6 and to Task 1.1's LXC premise
(the client reads the relay's result).

## MINOR

- **m-a.** The `joined`-only rule (DESIGN 3.2:90, 3.4:211; Task 2.4) refuses a real member whose
  container was down at the last play run. `lxcrecord.go:22-27` marks it `stopped` until the
  next run. Accept `stopped` as well as `joined` (the roster check still applies), or say why
  not.
- **m-b.** A launch in a plain terminal with `--teams` gets a membership directory named after
  its container but **no** `ccy-session` label (`agent-bus-membership.bash:54`, and the header
  at `:13-15`). DESIGN 3.2:93-95 counts such sessions in, yet 3.2:134-138 and Task 2.4 need a
  running container *labelled* `ccy-session=<session>`. Such a coordinator is always refused.
  Either check liveness by container name when there is no label (the launcher's own
  `ps --filter name=^…$`, `:99`), or state that plain-terminal sessions are out of scope.
- **m-c.** The mount block's add/remove must be computed from the unfiltered
  `agent_bus_lxc_members`. The member phase iterates `agent_bus_lxc_member_list`, which
  `agent_bus_only_teams` narrows (`play-agent-bus.yml:76-77`). A run limited to one team would
  see another team's container as having no members left and remove its mount. DESIGN 4:267
  names the right variable; Task 5.1 should say so and test a filtered run.
- **m-d.** D12's default trusts the checkout once, through the remote the host derives from the
  checkout's `.git/config`, which is also container-writable. The window is the session's first
  launch after deploy, not "before deploy". The text (DESIGN 3.2:115-118) should say so. Given
  the owner's "never trust the container", consider making the safe alternative (re-take
  every pre-plan coordinator seat) the default, at least for coordinator seats.
- **m-e.** Task 4.1 says "A test in `scripts/test-ccy-agent-bus.bash` comes first". After N1 the
  bash changes, so that test should pin the new `-v`/`-e` arguments, not only the Go output.

## Checked and fine

- `podman ps` by label from a user unit. The relay's environment has `XDG_RUNTIME_DIR`
  (graphical-session user unit), which is all rootless podman needs. The label exists for every
  named session (`agent-bus-membership.bash:54`).
- The Dockerfile kit link (`Dockerfile:426-440`) matches Task 4.2. `entrypoint.sh` and
  `session-lifecycle.bash` are on the ratchet (`bash-limits.allowlist:56,63`), as the plan says.
- tmpfiles: a `d` line plus a separate `a+` line for the ACL is valid tmpfiles.d syntax. Its
  parents default to 0755 root, which both the relay and the guest user can traverse.
- The PLAN overview, Goals and Success criteria agree with DESIGN 3.x after the round-1 edits.
  No install-specific names were found.

VERDICT: CHANGES
