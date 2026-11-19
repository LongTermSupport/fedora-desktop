# Audit: ADMIN (root admin program and the homeserver control child)

## Scope

Read in full from the working tree: `src-go/cmd/teamchat-admin/main.go`; in
`src-go/internal/teamchat/admin`: `run.go`, `class.go`, `process.go`, `exit.go`, `child.go`,
`execchild.go`, `answerops.go`, `serve.go`, `serveview.go`, `servecarry.go`, `carrychild.go`,
`carrygate.go`, `verbs.go`, `human.go`, `rotate.go`, `bundles.go`, `hostfiles.go`, `lxc.go`,
`wiring.go`, `adapters.go`, `backupverbs.go`, `restorechild.go`; `admin/wire/wire.go` and
`wire/lxc.go`; all of `ctl/` (`ctl.go`, `carry.go`, `restore.go`); `apply/member.go` and the
credential and password paths of `apply/human.go` and `apply/files.go`. Read only in outline
(function list and the lock, exit and stdin lines): `sealverb.go`, `restoreverb.go` (its
`printLost` in full), `restoreengine.go`, `unitsverb.go`, `serverestore.go`, `apply/apply.go`.
Followed out of area only to confirm a finding: `execx`, `state`, `teamlock`, `stdinread`,
`health/health.go`, `host/lxc/apply.go`, `matrix/admin.go`. Tests run and green:
`go test ./internal/teamchat/admin/... ./internal/teamchat/ctl/... ./internal/teamchat/apply/... ./cmd/teamchat-admin/...`.
Every finding below was confirmed by reading the cited lines; none was reproduced on a live
host (no homeserver runs in this container), and that is said again where it matters.

Counts: 0 CRITICAL, 1 HIGH, 3 MEDIUM, 7 LOW.

## Findings

### ADMIN-1 (HIGH): the child sends the admin token to whatever listens on the loopback port

- **Where.** `src-go/internal/teamchat/ctl/ctl.go:97` (`base := "http://127.0.0.1:<port>"`),
  `:115` (`anon.WithToken(token)`), `:206` (`Register(ctx, regKey, ...)`), `:216`
  (`LoginDevice` with the admin password); `ctl/restore.go:184-188` (staging client, same
  shape); `admin/serveview.go:124` (member token copies, same base).
- **What is wrong.** The `teamchat-ctl` child reaches the homeserver by plain HTTP on
  `127.0.0.1:<port>` and authenticates itself with the team's admin token as a bearer. Nothing
  authenticates the listener: no TLS, no unix socket, no check that the team's unit is active
  or that the socket belongs to it. The declared port may be any of 1 to 65535
  (`paths/ids.go:201-204`), so in practice an unprivileged port any local process may bind.
- **Who asks while the server may be down.** The health tick calls the child's `team.view`
  on every tick whatever the homeserver's state (`health/health.go:235-242`: the view is asked
  after `homeserver` and `terminator`, which return an error only for a fault of their own).
  Member, role and human verbs ask the child with no unit check at all
  (`admin/verbs.go:346-351`, `admin/human.go:88-92`).
- **Failure scenario.** The homeserver of team A is down (crashed, OOM-killed, in a restart
  gap, or stopped by an operator). Any local process, under any uid, binds
  `127.0.0.1:<port of A>` first; the unit then cannot bind and stays down. The next health
  tick (or the next member verb) opens the token file and sends `Authorization: Bearer <admin token>` to that process. The holder of the admin token can reset every member's and human's
  password, mint tokens and read every room. On a first bootstrap the same listener would be
  sent the registration key and the fresh admin password (`ctl.go:206`, `:216`); `team apply`
  waits for the unit to be active first (`host/apply.go:871-884`), which narrows that case but
  does not authenticate the peer.
- **Not verified.** I did not reproduce this on a host, and I did not measure how long the
  port is free during a Tuwunel restart. The absence of any listener check is certain; the
  width of the window is not.
- **Suggested fix.** Authenticate the server to the child. Cheapest that fits the code: root
  passes the team CA certificate in each request (it already reads it for bundles,
  `admin/wire/wire.go:94`) and the child dials the TLS terminator on loopback with that CA
  pinned; a squatter has no leaf key. Alternative: have Tuwunel listen on a unix socket in a
  directory only `teamchat-ctl` can enter for the admin path (the design notes
  `unix_socket_path` is unused, DESIGN-v2-C-host.md:402). Until then, at least refuse to send
  the token unless the unit is active and its main pid owns the listening socket.
- **Known weakness?** No. Section 8 of `weaknesses.md` (credential and trust handling) does not
  list it, and the design text has no entry for it (searched DESIGN-v2-C-host.md for squat,
  bind and loopback token: nothing).

### ADMIN-2 (MEDIUM): `human password` resets the password, then can fail without answering it

- **Where.** `src-go/internal/teamchat/apply/human.go:116-134`; the child drops every secret
  from a failed answer by rule (`admin/serve.go:153-159`, `admin/human.go:147-150`).
- **What is wrong.** The order is: roster write, `ResetPassword` (which signs out every
  device, `matrix/admin.go:164`), an optional second roster write, the `rooms` converge, and
  only then `r.res.Password = pw`. A failure of the second write or of the rooms step returns
  an error after the server already holds the new password, and the password is discarded.
- **Failure scenario.** The owner runs `human password` for a locked-out person. The reset
  lands; the rooms step then fails (a rate limit, a homeserver blip, a state drift refusal).
  The verb prints `human <name> password set` among its changed lines, exits non-zero and
  prints no password. The human is now signed out of every device and nobody knows the
  password until the verb is run again and succeeds.
  `TestHumanVerbsRecoverFromEachFailure` shows a re-run recovers, so the loss is temporary,
  but the changed line is misleading and the lock-out is real while the fault lasts.
- **Suggested fix.** Treat the password as issued once `ResetPassword` returns: set
  `r.res.Password` there, and let the child answer `ok` with the password plus a note when
  only the after-steps failed (or run the rooms converge before the reset, since it does not
  depend on the new password).

### ADMIN-3 (MEDIUM): `lxc apply` throws away an issued credential when only the status refresh failed

- **Where.** `src-go/internal/teamchat/admin/lxc.go:227-234` against
  `admin/verbs.go:361-363` and `:372-380`; consumer `host/lxc/apply.go:1102-1116`.
- **What is wrong.** `memberWork` joins a status-record failure (and a bundle failure) onto
  its error but still returns the work, because the credential is already issued on the
  server. The CLI path honours that: `memberVerb` prints the document before reporting the
  error (`verbs.go:185-194`), and `TestAFailedStatusRefreshStillPrintsThePassword` pins the
  same rule for humans. `lxcRoster.Join` does not: any error returns `lxc.Joined{}`, so the
  handle and the bundle are dropped and `lxc apply` records the seat as failed.
- **Failure scenario.** `lxc apply` reissues a running guest seat (after `team rotate-ca`, or
  for a seat marked `needs reissue`). The child resets the password, which signs the seat's
  device out, and logs in a new token. The status refresh then fails for an unrelated reason.
  The new bundle is discarded, the guest keeps a token the server no longer accepts, and the
  seat is cut off until a later `lxc apply` succeeds end to end. For an `add`, the handle is
  enrolled in the roster but never written to the LXC record, which is the state
  `recordJoins` exists to prevent (`host/lxc/apply.go:1122-1124`).
- **Not verified.** I did not trace how the next `lxc apply` treats an enrolled handle that
  its record does not hold, so I cannot say whether recovery is automatic.
- **Suggested fix.** Have `Join` return the handle and bundle whenever the child answered ok
  and a bundle was built, with the secondary error beside them, and have `lxc.Apply` place the
  bundle and then report the error. Add a test with a failing `Status.Refresh`.

### ADMIN-4 (MEDIUM): nothing ties the child's life to root's, and root handles no signal

- **Where.** `src-go/cmd/teamchat-admin/main.go:24` (`context.Background()`, no
  `signal.Notify` anywhere in `admin` or the command); `admin/execchild.go:52-55` (the child
  is started with a credential only); `internal/execx/execx.go:96-100` (no `Pdeathsig`, no
  new session).
- **What is wrong.** The team lock is a `flock` on root's fd (`teamlock/teamlock.go:219`), so
  it is released the moment root dies. The child is a separate process under another uid; if
  root is killed alone (an operator's `kill`, the OOM killer, a `sudo` that dies), the child
  keeps running its operation with no lock held.
- **Failure scenario.** Root is killed during `member add`. The orphaned child carries on
  through account, credential, rooms and notice. Another verb takes the now-free lock and
  starts a second child. Both read `roster.json`, change it and rename a new file over it
  (`apply/files.go:402-425`, `state/state.go:40-52`): the later rename wins, the other's
  member state, `seq` step or `pending` record is lost, and the roster no longer matches the
  server. A terminal Ctrl-C does not cause this (both processes are in the foreground group),
  and a systemd unit stop kills the whole cgroup; it needs root to die alone.
- **Second effect.** With no handler, SIGINT while an issued credential is on the alternate
  screen (`admin/verbs.go:213-225`; `internal/term` installs no handler either) ends the
  process without leaving that screen, so the secret stays displayed.
- **Suggested fix.** Set `Pdeathsig: SIGTERM` on the child (lock the OS thread around the
  start, as Go requires), and have the child hold its own lock on a file in `ctl/<team>/` for
  the length of an op. Handle SIGINT and SIGTERM in `main` by cancelling the context, so the
  secret screen is closed and the lock released in order.

### ADMIN-5 (LOW): 19 of the 49 verbs in the table are stubs that still run the gate and the wiring

- **Where.** `admin/run.go:146-177` (the table), `:82-89` (gate, then `is not built yet`),
  `admin/verbs.go:104-120` (the verbs that exist).
- **What is wrong.** `human lock`, `human unlock`, `redact`, `check`, `footprint`,
  `declare check`, `status` and all twelve `migrate` verbs have no `run`. `usage` lists them
  as verbs. Because the carry gate runs before the nil check, `migrate fence --team T` on a
  sealed team answers the seal's refusal (70) instead of "not built" (1). Nothing in `docs/`,
  the play or `ccy-teams` calls any of them (searched), so they are dead rows today.
- **Suggested fix.** Check `v.run == nil` before the gate, and either drop the unbuilt rows
  or mark them in `usage`.
- **Known weakness?** Touches W8 (size) and W9 (docs against what is deployable).

### ADMIN-6 (LOW): the request cap is checked only by the child, after root has already acted

- **Where.** `admin/child.go:227-235` (`EncodeRequest` has no size check), `:29`
  (`maxRequest` 64 KiB), `admin/verbs.go:81` (`maxTeamFile` is also 64 KiB), `:491-496`
  (the stdin input is capped at `maxRequest`), `:555` (the bootstrap request wraps the input,
  the approval and the registration key), `admin/carrychild.go:68-70` (roster, rooms, the
  admin token and every guest token ride in the one request line).
- **Failure scenario.** A declaration that passes root's 64 KiB cap produces a `team.apply`
  request over 64 KiB once wrapped. `host.Apply` has by then written files and started units;
  the child answers `bad request` and the verb exits 1 with no hint about size. Real teams are
  far below this, hence LOW.
- **Suggested fix.** Refuse in `EncodeRequest` when the line is over `maxRequest`, and cap
  the input and `team.json` at a size that leaves room for the wrapper.

### ADMIN-7 (LOW): temporary files of the child's secrets are never swept

- **Where.** `state/state.go:41-52` (a crash between create and rename leaves
  `.<name>.<random>.tmp`); `apply/member.go:570` (guest token copies) and
  `apply/files.go:422-431` (roster, rooms) write through it; `ctl/ctl.go:107` sweeps only the
  temporaries of `admin.password` and `admin.token`.
- **What is wrong.** The `ctl` package doc promises that no copy of a secret outlives the run
  that wrote it (`ctl/ctl.go:5-10`, `:400-402`). That holds for the two files `ctl` writes
  itself, not for a member's token copy under `guest/`, whose temporary survives a kill or a
  power loss for ever. Exposure is limited to the `teamchat-ctl` account (the directory is
  0700).
- **Suggested fix.** Sweep `.*.tmp` in `ctl/<team>/` and `guest/` when the directory is
  opened, or write these files with `ctl`'s own `create`/`replace`.

### ADMIN-8 (LOW): `team approve` changes `approved.json` without the team lock

- **Where.** `admin/verbs.go:592-614` and `:642-667`.
- **What is wrong.** Every other state-changing root verb takes the team lock; `approve`
  loads, compares and saves with none, and so is judged only by the pre-lock carry gate
  (`admin/run.go:82-86`), not by the lock's guard. Two concurrent approves can lose one
  change, and an approve can land between a running `team apply`'s read of the approval and
  its bootstrap step.
- **Suggested fix.** Take the team lock with `SeatWait` around load, compare and save.

### ADMIN-9 (LOW): the production wiring is built twice for most root verbs

- **Where.** `admin/carrygate.go:26` (`e.wired()`), then `admin/verbs.go:134-158`
  (`e.system()` calls `e.Wire()` again); `admin/wire/wire.go:46-48`.
- **What is wrong.** `Env.Wire` is not memoised, so a root verb with `--team` builds two
  `System` values, and the `sync.OnceValues` seams inside them are not shared. It costs two
  reads of `/proc/self/exe` today; it becomes a real bug the day a seam holds state.
- **Suggested fix.** Wrap `Wire` in `sync.OnceValues` in `newEnv`, or have `Run` resolve the
  system once and pass it down.

### ADMIN-10 (LOW): a failed account lookup fails `version` and `units` too

- **Where.** `admin/run.go:73-76`, `admin/class.go:35-38`.
- **What is wrong.** `classOf` looks up `teamchat-ctl` before the verb's class is consulted,
  so an account database error exits 78 even for the `ClassAny` verbs, which need no class.
  The play calls `version` to decide what is installed.
- **Suggested fix.** Skip `classOf` when `v.class == ClassAny`.

### ADMIN-11 (LOW): a child killed for its ten-minute bound is reported as a plain failure

- **Where.** `admin/execchild.go:34` and `:82-85`, `admin/exit.go:28-47`.
- **What is wrong.** When `ChildWait` expires the error is the context's, which no branch of
  `ExitCodeOf` knows, so the verb exits 1. The usual cause is a homeserver that stopped
  answering, for which the table's code is 69, and the play cannot tell the two apart.
- **Suggested fix.** Map the child's deadline to `ExitUnreachable`, and name the bound in the
  message.

## Suspend and resume, in this area

Confirmed sound by reading, no finding:

- Every in-process deadline here compares `time.Now()` values that carry Go's monotonic
  reading, so a step of the wall clock on resume does not expire them: the lock wait
  (`teamlock/teamlock.go:213-226`), the child's bound (`admin/execchild.go:66-68`, a context
  timer) and the staging wait (`ctl/restore.go:192-198`).
- The admin program, the child and the homeserver are on the same machine, so they freeze and
  thaw together; an operation cut short is completed or rolled forward from the `pending`
  record (`apply/member.go:159-178`, `:408-444`), and a retry of a verb whose credential was
  issued but never answered issues a fresh one (`:418-425`).
- What is stamped into files is wall-clock time (`since_ms`, `cred_ms`, the bundle's
  `issued_ms` at `admin/bundles.go:47`, and the `SinceMS` that seeds a notice's transaction
  ID at `apply/member.go:689-692`). A verb run straight after resume, before the clock is
  corrected, writes stamps in the past. Nothing in this area orders by them; the seat side only
  requires `issued_ms >= 0` (`seat/records.go:196`). Whether the health tick or a member
  compares them is for those areas' audits.
- Reissue is the one admin action that breaks a live session on purpose: every credential
  step resets the password with `logout_devices` (`matrix/admin.go:164`). ADMIN-3 is where
  that meets a lost bundle.

## Known weaknesses confirmed

- W5: there is no move verb; the member verbs are add, return, park, retire, reissue and set
  (`admin/run.go:158`).
- W6: `member add` needs all of `--prefix --seat --kind --human-text --credential`
  (`admin/verbs.go:123-131`), and the credential leaves root only inside the stdout document
  (`:364-380`), which is what forces the `sudo -S ... | place-issued` pipe.
- Section 7 of `weaknesses.md`: production restore answers `restore.ErrUnproven`
  (`admin/wire/wire.go:157-160`).

## Checked and found sound

- Secrets cross to the child on stdin only, never argv or environment
  (`admin/execchild.go:76-82`); the child's environment is `PATH` alone and it refuses any
  other start (`admin/child.go:82-101`).
- Root parses the child's answer strictly and checks it against the request before acting
  (`admin/child.go:465-481`, `admin/verbs.go:387-413`); output is capped
  (`admin/execchild.go:156-171`).
- `ctl/<team>/` is opened without following a link and must be the child's own, mode 0700
  (`ctl/ctl.go:294-308`); the admin token is linked into place, never replaced (`:366-388`).
- `restore --lost` has root read a file from the child's directory safely: no link followed,
  owner checked on the opened file, and each line re-encoded (`admin/restoreverb.go:190-219`).
- `team approve --only` cannot approve a planted field: `declare.Approve` requires the input
  to equal the approval plus that one change (`declare/gate.go:278-293`).

## Unconfirmed

- **Account lookups under a static build.** `process.go:46` and `wiring.go:133` use `os/user`.
  If `teamchat-admin` is built without cgo, lookups read `/etc/passwd` only, and a desktop
  user from sssd, LDAP or systemd-homed would fail `element check|apply` with "the home of
  uid". I did not establish how the play builds the binary.
- **Width of ADMIN-1's window.** See that finding.
- **Recovery after ADMIN-3.** See that finding.

## Bloat and debloat candidates

- The 19 stub verbs (ADMIN-5).
- `Answer.ExitCode` (`admin/child.go:352-357`) has no production caller in `admin`; root
  uses `ChildError` and `classExit` instead.
- `wire.ErrTeamCANotMade` (`admin/wire/wire.go:43`) is an alias used only by tests.
- `exactKeys` (`admin/child.go:278-280`) is a one-line wrapper of `keys`.
- `answerops.go` (492 lines) hand-writes an encoder, a decoder and a validator per answer
  shape, and `validView` builds a fake status record to borrow its validation
  (`admin/answerops.go:222-245`). One tagged-union answer type with a single schema table
  would remove most of it.
- The restore child path (`restorechild.go`, `serverestore.go`, `ctl/restore.go`, about 940
  lines plus about 1,160 of tests) is unreachable in production while restore is unproven.
  Keep it only if H-C5 and H-C15 are going to be run.
- `ctl` and `state` each implement atomic file writes (`ctl/ctl.go:366-388`,
  `ctl/restore.go:157-179`, `state/state.go:28-53`) with different temporary names and sweep
  rules; ADMIN-7 is the gap between them.
