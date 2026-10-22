# SPEC: N2 slice n2a, `teamchat-admin`'s skeleton and the rooms engine (Plan 00209 Tasks 2.1, 2.3)

The first caller of N1's packages. Built from area C's 3.3 (the host program, its caller
classes and the `teamchat-ctl` child) and 3.6.2 step 4 (the rooms engine over area A's room
specs), and the seam rows that cite them: 13 (A gives the desired rooms as data, C's engine
applies them), 14 (three rooms at team creation, in A 5.2's write order) and 17 (the roster
event's `rev` and its validation before the write). Each expectation is numbered and has a Go
test written first and seen to fail.

The packages:

- `src-go/cmd/teamchat-admin`: a thin `main`.
- `src-go/internal/teamchat/admin`: the command: its verb table, the caller-class check, the
  exit-code table and the `teamchat-ctl` child's boundary.
- `src-go/internal/teamchat/rooms`: the rooms engine.

Every name and path comes from `teamchat/paths`; every room rule from `teamchat/proto`; every
homeserver call goes through `teamchat/matrix`'s admin client.

**Out of this slice**, by the brief: any play task, unit, sudoers file or install step (gate A);
seats, bundles and pool passwords (gate B); the roster file, `rooms.json` and the atomic writer
(`internal/teamchat/state` is not built yet), so the engine records room IDs through an
interface its caller implements; the team lock; bootstrap steps 1-3 and 5; every verb but
`version` and `ctl-child`.

## 1. `teamchat-admin`: verbs and caller classes (C 3.3)

- **CA1.** The verbs are C 3.3's table, each in exactly one caller class:

  - root: `team apply|approve|remove`, `member add|return|park|retire|reissue|set`,
    `role apply`, `human password|devices|logout|lock|unlock`, `redact`, `backup`, `backups`,
    `restore`, `health`, `lxc apply|restart`, `check`,
    `migrate preflight|stage|fence|fence-status|capture|carry|commit|accept|archive|archive-check|unfence|rollback`,
    `footprint`;
  - `teamchat-ctl`: `ctl-child`;
  - anyone else: `declare check`, `element check|apply`, `status`;
  - every class: `version`.

  A verb of two words (`team apply`) is matched before one of one word; flags after the verb
  belong to the verb and are not read by the dispatcher.

- **CA2.** The caller class is decided first, from the real and the effective uid only: root is
  both 0; `teamchat-ctl` is both the uid `paths.CtlUser` has on this host; anyone else is every
  other pair, a mixed pair included (the binary is 0755 and never setuid, so a mixed pair is not
  a path into root). A host without the `teamchat-ctl` user has no `teamchat-ctl` caller. A
  failed user lookup that is not "no such user" is exit 78.

- **CA3.** A verb asked of the wrong class exits 77 with `teamchat-admin: <verb> must be run by <class>` on stderr, where `<class>` is `root`, `teamchat-ctl` or `a user other than root and teamchat-ctl`. Nothing else runs first.

- **CA4.** No verb, or an unknown one, is exit 64 with the usage text on stderr.

- **CA5.** A verb in the table this build does not provide exits 1 with
  `teamchat-admin: <verb> is not built yet` (after the class check, so the class table is
  provable for every verb now). Decision n2a-D1.

- **CA6.** `version` prints the binary's source digest on stdout, one line. The digest is baked
  in with `-ldflags -X` (C 3.3); one that is missing or fails `paths.ParseDigest` is exit 1,
  so no build without a digest answers as if it had one.

- **CA7.** Exit codes are C 3.3's one table: 0 done, 1 failed, 64 usage, 69 homeserver
  unreachable, 70 refused, 75 busy, 77 wrong caller class, 78 configuration. Every diagnostic
  goes to stderr as `teamchat-admin: <message>`; stdout carries only the payload.

## 2. The `teamchat-ctl` child's boundary (C 3.3)

- **CC1.** Root starts the child as `<the running binary> ctl-child` with: the uid and gid of
  `paths.CtlUser`, no supplementary groups, working directory `/`, and an environment of `PATH`
  alone (`/usr/bin:/bin`). This slice builds that start as a typed value (`ChildSpec`);
  starting it needs `execx` to carry a credential and a replaced environment, and the
  `teamchat-ctl` user from the install, so the start itself waits for the slice that adds the
  first child operation. Decision n2a-D6.
- **CC2.** The child refuses any argument but `ctl-child` (exit 64), and refuses to serve when
  its environment holds anything but `PATH`, its working directory is not `/`, or it has a
  supplementary group (exit 78): a child started any other way is not the child root starts.
- **CC3.** The request is one JSON document on stdin, at most 64 KiB, decoded strictly
  (proto's strict decoder: no repeated key, nothing after the value; unknown keys refused):
  `{"v": 1, "op": "<op>", "team": "<team>", "args": {...}}`, `v` exactly 1, `op` a dotted
  lower-case word (`member.add`), `team` a `paths.ParseTeamID` team, `args` an object. A bad
  request is answered as a `failed` error (CC5) and exits 1, however long the text it quotes:
  the child's own message is made one line (CC6) and cut to 4096 bytes, ending `...`.
- **CC4.** An operation the child does not serve is answered `failed`, `op <op> is not served by this build`. This slice serves none.
- **CC5.** The answer is one JSON document on stdout:
  `{"v": 1, "ok": true, "changed": ["..."]}` or
  `{"v": 1, "ok": false, "error": {"class": "<class>", "message": "<text>"}}`, `class` one of
  `failed`, `refused`, `unreachable`, `config`. Decision n2a-D4.
- **CC6.** Root decodes the answer strictly, because it is the one thing root parses that a
  less-privileged process wrote: at most 1 MiB, no repeated or unknown key, `v` exactly 1, `ok`
  a boolean, `changed` (only with `ok` true) an array of at most 4096 one-line strings, `error`
  (only with `ok` false) exactly `class` and `message`, the message one line. One line is
  non-empty, at most 4096 bytes, and holds no control (C0, DEL, C1), no format character (Cf)
  and no U+2028 or U+2029, so nothing a reader of root's `CHANGED` lines could split on or take
  as an escape gets through. A `bundle` key is
  refused until Task 2.4 gives it a type (decision n2a-D5). Root maps the error class to its
  own exit code (1, 70, 69, 78); the child's process exit code is never passed through (I-B3).

## 3. The rooms engine (C 3.6.2 step 4; A 4, 5.2, 5.3; seams 13, 14, 17)

The engine is `rooms.Converge(ctx, admin, recorder, team, ids)`: the team (server name, team
id, A's roster as the host derived it, the description, the roster file's `rev`) and the room
IDs known so far. It answers the room IDs, the `CHANGED` lines and the roster `rev` it wrote
(0 when it wrote none).

- **RE1. Find.** The engine asks `proto.Desired(server, roster, ids, description)`. For the
  first spec with no room ID it adopts or creates the room, records its ID, and asks `Desired`
  again with the new ID, until every spec has one; so a spec naming another room's ID is never
  built before that room exists (A 5.2).
- **RE2. Adopt.** A candidate is a room the admin has joined (`JoinedRooms`) and created (its
  `m.room.create` sent by the admin) whose ID is not already known: for a side room, one whose
  admin-sent `teamchat.room` names this team and this purpose; for main, one with no
  `teamchat.room` at all (decision n2a-D7). One candidate is
  adopted (`room <purpose> adopted <id>`); more than one is refused, naming them all (exit 70);
  a side-room candidate whose link names another main room is refused (n2a-D8).
- **RE3. Create.** With no candidate the engine records the intent (`Recorder.Intent`), creates
  the room, then records its ID (`Recorder.Record`) and prints `room <purpose> created <id>`.
  The create is a private room of the spec's version, its power levels as
  `power_level_content_override`, and every other state event of the spec but the roster as
  initial state (the name and topic too, so their stored content is A's exactly, n2a-D12; main
  is created with no roster, A 5.2), with no invite: invites go out in RE7 only.
- **RE4. Read and refuse drift it must not repair.** Every room's full state is read. The engine
  refuses (exit 70), naming the room, a room whose create event the admin did not send, whose
  `room_version` is not the spec's, or that has `additional_creators` (another creator has
  unlimited power, A 4.1), or that carries `m.room.encryption` (A 4.1; it cannot be turned
  off). A recorded room the admin cannot read fails with the homeserver's error. Before any of
  this, one room ID known for two purposes is refused (step `read`), as no run could converge it.
- **RE5. Kicks first, in every room.** Each user joined or invited who is not in the spec's
  `Members`, the admin aside (`proto.MembershipDiff`), is kicked, `room <purpose> kicked <user>`.
  Every kick of every room happens before any state write of any room (C 3.6.2 step 4.2, C15).
- **RE6. Then state.** Each state event of each spec, in spec order, is written when the room
  has none of that type and key, a content that is not equal as JSON, or one the admin did not
  send (n2a-D11): `room <purpose> state <type>`. The roster event is compared without its
  `rev`; when anything else differs it is written with `rev` one above the larger of the room
  event's and the file's (`proto.NextRev`, seam 17), after `proto.DecodeRoster` accepts it; one
  it refuses is not written (exit 70).
- **RE7. Then invites.** Each user in the spec's `Invite` who is neither joined nor invited is
  invited, `room <purpose> invited <user>`.
- **RE8. Idempotent.** A converged team makes no write call and prints nothing.
- **RE9. Failures name the room and the step** (`find`, `adopt`, `create`, `record`, `read`,
  `kick`, `state`, `invite`), and say whether they are drift. `teamchat-admin` maps a failure
  to C 3.3's table (`admin.ExitCodeOf`): drift 70; a matrix failure that is `ErrUnreachable` or
  `ErrRateLimited` 69; anything else 1. The matrix client's own `ExitCode` is the member
  side's table and is never used by the host (n2a-D13). A failure still answers what changed
  before it: the room IDs, the `CHANGED` lines so far, and the roster `rev` once the server
  accepted the roster write, so the caller can print and record them (C 3.6.1, C 3.6.2). A room
  created whose ID could not then be recorded is not in that answer; the next run adopts it and
  prints `room <purpose> adopted <id>`.
- **RE10. Trusted.** The rooms the engine makes pass proto's own trust checks: `VerifyMain`
  over main's state gives the roster the engine wrote, and `VerifySide` accepts reports and
  questions (A 6.4). With `-tags tuwunel` and `FAKEHS_TUWUNEL_BIN` the same round trip (create,
  a second run that changes nothing, the trust checks) runs against a real Tuwunel, through the
  server harness M44 uses (`matrix/tuwunelhs`).

## 4. Decisions where the design is silent

| ID      | Decision                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| n2a-D1  | A verb not built yet exits 1 (failed), not 70: nothing said no, the build lacks it.                                                                                                                                                                                                                                                                                                                                      |
| n2a-D2  | A mixed real and effective uid is "anyone else" (CA2).                                                                                                                                                                                                                                                                                                                                                                   |
| n2a-D4  | The child's failure answer carries a class root maps to its own exit code; C 3.3 gives only the success shape.                                                                                                                                                                                                                                                                                                           |
| n2a-D5  | `bundle` is refused in an answer until Task 2.4 types it, so no untyped key reaches root.                                                                                                                                                                                                                                                                                                                                |
| n2a-D6  | The child's start is a typed spec; running it waits for `execx` to carry a credential and a replaced environment.                                                                                                                                                                                                                                                                                                        |
| n2a-D7  | Main is adopted as the one joined, unrecorded room the admin created with no `teamchat.room` link. C 3.6.2 says a candidate's back-link names its purpose, but main has no link (A 5.3). The admin is not in the team's rooms alone: Tuwunel joins a server admin to the server's own admin room, which its server user created, so a room the admin did not create is never a candidate (seen against the real server). |
| n2a-D8  | A side-room candidate linking another main is drift, refused: adopting it would make the team's side room point elsewhere.                                                                                                                                                                                                                                                                                               |
| n2a-D9  | A kick carries no reason: A gives none, and a fixed text would be a reason nobody chose.                                                                                                                                                                                                                                                                                                                                 |
| n2a-D11 | State content equal to the spec but sent by someone other than the admin is rewritten, because A 6.4 believes only admin-sent state.                                                                                                                                                                                                                                                                                     |
| n2a-D12 | Name and topic go in `initial_state`, not `createRoom`'s `name` and `topic` keys, which a server stores in its own content form (the topic's `m.topic` block), so a fresh room would differ from A's spec at once.                                                                                                                                                                                                       |
| n2a-D13 | `matrix.Error.ExitCode` gives v1's member-side codes (7-10), not C 3.3's; the host maps a matrix failure by its class instead.                                                                                                                                                                                                                                                                                           |
