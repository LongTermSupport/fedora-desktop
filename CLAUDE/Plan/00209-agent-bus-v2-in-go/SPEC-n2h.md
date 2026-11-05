# SPEC: N2 slice n2h, the verbs no slice gave a behaviour (Plan 00209 Task 2.4, part)

**Status: draft, not yet reviewed.** The plan's rule is review to MERGE, then a red test per
expectation; no slice builds from this until the review's findings are settled. The decisions in
section 8 are defaults for the coordinator to confirm or change, and section 9 lists what this
spec does not close.

Built on n2a to n2g (`SPEC-n2a.md` to `SPEC-n2g.md`). Sources: area C's 3.3 (verbs and exit
codes), 3.5.1 step 2 and 3.8 (`declare check`, DC18), 3.5.4 (`check`), 3.6.5 (humans, `redact`,
DC9, DC20), 3.11.3 ("the login flow re-reads", `status`), 3.14 (the "approval gate" and "redact"
rows), area A's 4.2 and 12.4, `SPEC-matrix.md` M49, and the code these verbs sit beside:
`admin` (`rootRuns`, `ExitCodeOf`, `memberVerb`, `Serve`, the answer ops), `status`, `declare`,
`matrix` (`Admin`, `Client.Redact`, `Client.GetEvent`), `units`, `hostnet`, `host`, `backup`.
Each expectation is numbered and has a Go test written first and seen to fail.

**What this slice is.** SPEC-n2a CA1 lists these verbs and their caller classes and gives none a
behaviour; today each exits 1 "not built yet" (n2a-D1). This spec gives the behaviour of
`declare check` (its v2 half), `status`, `check`, `human password|devices|logout|lock|unlock` and
`redact`, so that each can be built test first in its own slice:

| Slice | Verb and section     | Class  |
| ----- | -------------------- | ------ |
| S2    | `declare check` (DC) | anyone |
| S3    | `status` (ST)        | anyone |
| S4    | `check` (CK)         | root   |
| S5    | `human` (HU)         | root   |
| S6    | `redact` (RD)        | root   |

**What it is not (owner gates).** Gate A: no play, task, unit file, tmpfiles entry, meta-deploy
entry or anything that installs a `teamchat` name or path. Gate B: nothing gives a process a pool
seat's password. A human's password (HU2) is not a pool seat's: it is printed once and never
kept. v1 is untouched. `declare check`'s v1 half (the `agent_bus_teams` ports, the v1-name rule
and the set records) lives in `migrate/v1`, which is not built, and is not this spec (DC8).

## 1. Common to every verb here (HV)

- **HV1. Registration.** Each verb is one entry of `rootRuns()`'s table (the table also holds the
  anyone verbs `element check|apply`; VERIFIED in `verbs.go`), so CA3's class check runs first
  and exit 77 is unchanged. Once a verb is built, `unbuilt` in `run_test.go` names it as built,
  and the table test of CA3 keeps passing for every class.
- **HV2. Seams.** Each verb takes the `System` through `Env.system()` (WR1): a verb that fails to
  wire is 78. A verb touches only the seams its section names. `declare check` and `status` use
  nothing a non-root caller cannot read: a test gives them a `System` whose every root seam
  (`Child`, `Bundles`, `OwnerGroup`, `Open`, `Pin`, `CtlIDs`, `Systemd`, `Firewall`, the lock
  directory) fails the test if called.
- **HV3. Output.** stdout is the payload only; every diagnostic is one line on stderr as
  `teamchat-admin: <message>` (CA7). A report verb whose job is to print for a human (`status`,
  `check`, `declare check`) prints its report on stdout. A verb that ccy-teams runs and parses
  (`human *`, `redact`) prints exactly one JSON document, `"v": 1` first, then one newline, and
  nothing else on stdout.
- **HV4. Exit codes** are C 3.3's one table through `ExitCodeOf` (CA7): 0, 1, 64, 69, 70, 75, 77,
  78\. A bad flag, a missing required flag, a flag given twice, an unknown flag or a stray
  argument is 64 (`parseFlags`'s rule, `usageError`), decided before any seam is touched.
- **HV5. The root change verbs** (`human *`, `redact`) share `memberVerb`'s shape: flags, then the
  team lock (HV6), then the applied `team.json` (a team with none is refused 70, "team <t> is not
  applied: run team apply first", as `readTeamFile` does), then the child's one request, the
  child's answer checked against the request (HV7), the status record where a section says so,
  and the answer. The lock is released on every path; a failure of release is joined to the
  verb's error.
- **HV6. The team lock** for `human *` and `redact` is `teamlock.Take(ctx, Host.LockDir, team, <verb name>, SeatWait, Host.Clock, Host.Guard)` (n2h-D15): a busy lock is 75 naming the holder's
  line, a pending restore refuses with 70 (RG1). The verb name in the holder line is `human` for
  the five human verbs and `redact` for `redact`.
- **HV7. The child's answer is checked** as `asked` checks a member verb's: for these ops it
  names no handle, no credential, no main room and no notes (any of them is a lie, `childLie`, and
  the verb does nothing more); a `human` or `redact` object is accepted only with `ok`, only for
  its own op, with exactly its keys (HU13, RD8); an answer of another op's shape is refused. A
  failed answer is its class's exit code (CC6); its `changed` lines before a failure are printed
  in the JSON document of HV3 where a section says so.
- **HV8. After the child, before the failure.** When the child's work succeeded and the status
  record then fails to refresh, the verb still prints its one JSON document (a password printed
  once is never lost), then reports the failure ("the status record: ...") and exits 1, as
  `memberWork` does. No secret is in any error, in any stderr line, in the status record or in
  any file the verb writes.

## 2. `declare check`, the v2 half (DC, anyone, C 3.5.1 step 2, 3.2.5, 3.8, DC18)

The unprivileged judge the play runs before any root task. It reads the declaration the play
parsed, the owner's approvals and the host's listening sockets, writes nothing, and takes no lock.

- **DC1. Input.** No flag and no argument (anything else is 64). The declaration is one JSON
  document on stdin, at most 1 MiB, decoded by `declare.Decode` (DR1 to DR6): exactly the keys
  `teamchat_address`, `teamchat_teams`, `teamchat_lxc_members`, `teamchat_element_teams`. An
  unreadable stdin is 1; a declaration that fails the decode (DR2's error, which names the field
  by its path and never echoes a value) is 70, with the decode's message on stderr, nothing on
  stdout, and no later step run (n2h-D6).
- **DC2. Approvals.** Each present team's `approved.json` is loaded with
  `declare.FileApprovals{Path: Host.Root + paths.ApprovedFile(team)}` (read as the caller; the
  file is 0640 root:owner group). A team with no file is `nil`, which is not an error (it differs
  in its existence). A file that exists and cannot be read or decoded (AG2: strict, at most 256
  KiB, its team the one asked for) is 78, naming the file, nothing on stdout.
- **DC3. SECURITY lines.** For every present team, in declaration order, every difference
  `declare.Check` finds is printed on stdout as
  `SECURITY<TAB><team><TAB><field><TAB><old> -> <new>` (AG3's text, every value one line), and
  nothing else is ever on stdout. An absent team is not compared (n2b-D4). A clean declaration
  prints nothing on stdout and nothing on stderr and exits 0.
- **DC4. The exit code of a SECURITY line is 70** (n2h-D6): a rule said no. After all lines are
  printed, one line per team with a difference goes to stderr: `team <team>: the declaration's security fields are not approved: ccy-teams approve <team>` (the text of
  `declare.RefusedError`). The play shows the stdout lines in a task of its own and fails on the
  code; it tells a SECURITY refusal from another by the `SECURITY` lines, which no other
  refusal prints.
- **DC5. Ports that are listening (n2h-D7).** The caller is not root and cannot read another
  account's `/proc/<pid>/fd`, so `declare check` cannot attribute a listener to a unit
  (`hostnet.Network.Listeners` fails on the first process it may not open: VERIFIED in
  `holder`). It reads the socket tables alone: `hostnet.Network` gains `Ports()`, the set of
  ports with a socket in state `0A` read from `/proc/net/tcp` and `tcp6` (NW2's parser, no holder
  walk, no error for a process it cannot see). A present team whose `port` is in that set and
  which has no stamp (`host.ReadStamp` finds none, read as the caller) is refused, one stderr
  line `team <team>: port <n> is already listening and the team is not applied`, exit 70, no
  `SECURITY` line for that reason. A team with a stamp is not judged here: whether the listener
  is its own `teamchat-server@<team>` is `team apply` step 2's, as root (C 3.2.5).
- **DC6. Order and completeness.** A decode failure (DC1) ends the verb. Otherwise DC5's
  refusals and DC3's lines are all produced, whatever the order of the teams, and the code is 70
  if there is any of either. A `Ports()` or `ReadStamp` failure is 78 naming the file or table.
- **DC7. Nothing is written.** A test runs the verb over a tree of read-only files and finds
  every file byte for byte unchanged, no lock file and no directory made.
- **DC8. Not in this half.** `agent_bus_teams` is not a key of DR1, so a stdin that carries it
  is refused by DC1's decode (an unknown key). The v1 half, which reads the `team` and `port` of
  each v1 entry and the set records, is `migrate/v1`'s slice: it adds the key to the input, reads
  the records, and adds its refusals before DC5 (n2h-D8). Until then the play passes none.

## 3. `status` (ST, anyone, C 3.11.3)

- **ST1. Arguments.** `status [<team>...]`: positional team ids, each passing
  `paths.ParseTeamID`, none repeated; any flag (an argument starting `--`) or a bad or repeated
  id is 64. With no id the teams are those of ST2.

- **ST2. Which teams.** Every directory named by a team id under
  `Host.Root + paths.StatusRoot + "/teams"` that holds a stamp (`host.ReadStamp`), in team id
  order; no teams directory is no team (n2h-D11). It reads neither `team.json` nor `units.json`
  (root-only), so it is not `health.Stamps.Stamped`, which reads `team.json` (VERIFIED in
  `health/files.go`). A named team is shown whether or not it has a stamp. A stamp that cannot be
  read is a team problem (ST6), never the verb's failure.

- **ST3. The record.** Each team's record is `status.Read(Host.Root + paths.StatusFile(team), team)` and `status.Shown(rec, found, err, Host.Clock.Now())` (SR4, SR5). A missing record is
  the problem "no status record yet", an unreadable or invalid one "the status record cannot be
  read" (its error on stderr, one line, never on stdout), an expired one "health has not run
  since <time>" and `Current` false; each carries `FixHealth`. Nothing is written and no lock is
  taken.

- **ST4. The report, one team.** Printed on stdout, every value as the record holds it (SR1
  allows no control character in any of them), in this order and these exact shapes:

  ```
  team <team> "<name>" origin <origin> <health>
    homeserver <unit> since <t> restarts <n> probe <probe>
    members:
      <handle> <kind> <role> <state> <online> rooms <purpose>=<membership>,...
        problem: <what> (fix: <fix>)
    humans:
      <name> <state> devices <n> last-seen <t> rooms <purpose>=<membership>,...
    backup: newest <id> at <t> | none; last run <ok|failed|none>
    disk: <pct>% free (<bytes> bytes)
    restore: step <step> backup <n> pre <m>
    problems:
      <what> (fix: <fix>)
    events:
      <t> <what>
  ```

  `<health>` is `ok` when the shown record has no team problem, else `PROBLEMS`. `<t>` is UTC
  RFC 3339 to the second, `never` for a zero time. `<online>` is `online` or `offline` for an LXC
  member and `-` for any other. Rooms are in purpose order `main`, `reports`, `questions`, a room
  the account is not in absent. A member's own problems are printed under its row (SR1's
  `member`); `problems:` holds the team's. The `restore:` line is printed only when the record
  has one, the `problems:` and `events:` blocks only when non-empty, `members:` and `humans:`
  always (an empty one prints no row). A record that is missing or unreadable prints the `team`
  line with health `PROBLEMS` and its `problems:` block alone, and never a stale or guessed
  value.

- **ST5. Several teams** are printed in the order given (ST2's when none), one blank line between
  two reports. With no team (ST2 finds none and none was named) the report is the one line
  `no teams are applied` and the code is 0.

- **ST6. The exit code** is 1 when any team shown has a team problem: a problem with no `member`,
  which includes every problem `status.Shown` adds (missing, unreadable, expired). A member's own
  problem is printed and never changes the code (DESIGN-v2-FRAME.md, offline members never
  block; `View.TeamProblems`). Otherwise 0. A team that has a problem does not stop the teams
  after it.

- **ST7. Failure of the verb itself** is a failed write of stdout (1, CA7) or an unreadable
  status directory that is not an absent one (78, naming the path).

## 4. `check` (CK, root, C 3.5.4)

`check --team T` prints the facts an acceptance run reads, changes nothing, and takes no lock
(so a change in flight may show between two of its steps; the verb is a probe, not a gate).

- **CK1. Arguments.** `--team T` exactly once (HV4). A team that is not applied (no `team.json`)
  is 70, "team <t> is not applied: run team apply first", before any line.

- **CK2. Lines.** Each fact is one stdout line
  `CHECK<TAB><fact><TAB><ok|FAIL|info><TAB><value>`, the value one line (no control character, at
  most 512 bytes, cut with `...`), in the order of CK3 to CK8. A fact that cannot be judged is a
  `FAIL` line naming why, never a skipped line. The exit code is 1 when any line is `FAIL` and 0
  otherwise (C 3.5.4); the child's, the bus's or the firewall's failure is such a line, not
  another exit code (n2h-D10). Only a refusal before the first line (CK1, a `team.json` that
  cannot be decoded: 78) is another code.

- **CK3. The unit.** `unit.active` is `ok` when systemd's `ActiveState` of
  `paths.ServerUnit(team)` is `active`, else `FAIL` with the state word; `unit.enabled` is `ok`
  when `UnitFileState` is `enabled` or `enabled-runtime` (SD4, through `host.Systemd.State`);
  `unit.restarts` is `info` with `NRestarts` (the health tick's `Units.State`).

- **CK4. The sandbox (gap 3).** `units.Manager` gains `Sandbox(ctx, unit) (map[string]string, error)` (n2h-D9): for a unit name SD8 accepts, it loads the unit as `State` does and reads,
  with `org.freedesktop.DBus.Properties.Get` on `org.freedesktop.systemd1.Service`, each
  property of one fixed table and no other, checking each D-Bus type strictly (SD5's rule: a
  wrong type is `ErrMalformed`, never a zero; a unit that is not loaded is `ErrNoSuchUnit`), and
  answers name to canonical text, a boolean as `yes` or `no`, a string as it is. The table is one
  source in `units`:

  | Type    | Properties                                                                                                                                                                                                                                      |
  | ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
  | boolean | `DynamicUser`, `NoNewPrivileges`, `PrivateTmp`, `PrivateDevices`, `PrivateIPC`, `ProtectKernelTunables`, `ProtectKernelModules`, `ProtectKernelLogs`, `ProtectClock`, `RestrictRealtime`, `RestrictSUIDSGID`, `LockPersonality`, `IPAccounting` |
  | string  | `User`, `ProtectSystem`, `ProtectHome`, `ProtectProc`, `ProcSubset`                                                                                                                                                                             |

  These D-Bus types are UNVERIFIED against a real systemd: the scratch-bus test (SD9) exports
  them as this table says, and a host check reads the real ones before the slice is called done;
  a wrong type corrects the table, never the check. Each property is a fact `sandbox.<Name>`:
  `ok` when its text equals the value of the same-named line of `host.TeamSandbox(team)` (the
  base unit's own lines, n2g-D17, so the check cannot drift from the sandbox), else `FAIL` with
  `<got> (want <want>)`. A `TeamSandbox` line whose name is not in the table is not compared,
  and one `info` line `sandbox.unchecked` names all of them, space separated, so no directive is
  silently unjudged. A unit systemd cannot load makes one `FAIL` line `sandbox` ("no such unit")
  in place of the rows. `SocketBindAllow`, `IPAddressAllow` and `IPAddressDeny` are structured
  properties and stay unchecked here; the drop-in (CK5) and the bound addresses (CK7) carry
  those facts.

- **CK5. The installed files.** `dropin` and `config` are `ok` when the installed drop-in
  (`paths.ServerDropInFile`) and config (`paths.ServerConfig`) produce the digests `units.json`
  records for them, `FAIL` when a file is missing, unreadable or its digest differs (the value
  names which key), and `FAIL` with "no units.json" when the record is missing. Each file is read
  by the trust rule (`state.ReadFile`, root-owned). `host` exports the one read-only function
  that recomputes a recorded digest from the installed file, so `render` and the check cannot
  disagree. A `units.json` that does not decode is 78.

- **CK6. The firewall.** For each rule of `firewall.json`, two facts
  `firewall.<zone>.<source>.permanent` and `...runtime`: `ok` when `Firewall.Query` answers
  present, `FAIL` when it answers absent or fails (the value names the exit code or "absent").
  With no `firewall.json` one `info` line `firewall` says "no rules recorded".

- **CK7. The bound addresses** (`bound`): `ok` when the addresses the unit's listeners hold on
  the team's port are exactly loopback and `listen` (`host`'s `boundAs`, exported for this), else
  `FAIL` with `<got> not <want>`; `info` lines `bound.addr` list each one. A `Listeners()`
  failure is the `FAIL` line.

- **CK8. The rooms.** The child is asked `team.view` as the health tick asks it (CO4,
  `ChildViewer`). Each problem of the view with no `member` is a `FAIL` line `rooms`, value
  `<what> (fix: <fix>)`; each problem with a `member` is an `info` line `rooms.member.<handle>`.
  A view with none prints one `ok` line `rooms` with value `converged`. A child that fails, or
  answers unreachable, is one `FAIL` line `rooms` naming the class (never a skipped fact).

- **CK9. Nothing is written** (as DC7), and no external program but the ones the seams already
  run is started. A test over every seam failing in turn finds one `FAIL` line for that fact and
  every other fact still printed.

## 5. `human` (HU, root, C 3.6.5, DC9, DC16)

The five verbs share HV5. Flags: `--team T --name N`, both required, `N` passing
`proto.ValidHumanName` (a bad name is 64). The child serves five ops, args exactly
`{team_file, name}` (a `team_file` the applied `team.json`, CV1's rule):

| Verb             | Child op         | Reads                   | Writes                                         |
| ---------------- | ---------------- | ----------------------- | ---------------------------------------------- |
| `human password` | `human.password` | the account, the roster | the account's password and devices; the roster |
| `human logout`   | `human.logout`   | the account, the roster | the account's password and devices; the roster |
| `human devices`  | `human.devices`  | the account's devices   | nothing                                        |
| `human lock`     | `human.lock`     | the account, the roster | the account's lock; the roster                 |
| `human unlock`   | `human.unlock`   | the account, the roster | the account's lock; the roster                 |

- **HU1. Who may be named (n2h-D16).** `N` must be a name in the applied `team.json`'s `humans`
  and a human the roster holds as `enrolled` or `locked`, with an account on the homeserver.
  Anything else (not declared, `removed`, no account) is refused 70, naming the human and why: a
  removed human is brought back by the declaration (DC9), never by this verb. `human unlock` of
  a human who is `enrolled` and unlocked, and `human lock` of one already `locked`, are not
  refusals (HU9).
- **HU2. `human password`.** The child makes 32 characters from `[A-Za-z0-9]`, each drawn
  uniformly from `crypto/rand` without modulo bias (n2h-D14), sets it with
  `Admin.ResetPassword` (a password reset with device logout: every device of the account is
  signed out), and answers it once. The verb prints `{"v": 1, "team": "T", "name": "N", "changed": [...], "password": "<32>"}`. The password is never stored: not in the roster, the
  status record, the child's directory, a log or an error (HV8), and it is in no stderr line. A
  password the child answers that is not 32 characters of that alphabet is a lie (HV7): nothing
  is printed.
- **HU3. The roster write comes first (n2h-D14).** Before the homeserver call the child writes
  the roster (atomic writer, `seq` raised) with the human's `revoked_seq` set to that write's
  `seq` (C 3.6.1: the `seq` of the last password set or reset with device logout), so a crash or
  a failed reset can only make a later restore over-revoke, never under-revoke. When the human's
  `needs` was `password` (set by a restore), a second roster write after the successful reset
  clears it. A `ResetPassword` that fails leaves `revoked_seq` raised and `needs` as it was, and
  the verb fails with the child's class (69 for `ErrUnreachable`, else 1).
- **HU4. `human logout`** is HU2 with a password that is not printed and not kept (n2h-D13): a
  fresh random one of HU2's kind, discarded, so every device is signed out and nobody can sign in
  until `human password` (what bootstrap step 3 does for a new human). It writes the roster as
  HU3 does, leaves `needs` as it is, and prints `{"v": 1, "team": "T", "name": "N", "changed": [...]}`, no `password` key.
- **HU5. `human devices`.** The child reads `Admin.Devices` for the account and answers the
  devices; the verb prints `{"v": 1, "team": "T", "name": "N", "devices": [{"id": "<id>", "last_seen_ms": <n>|null}, ...]}`, sorted by `id`, `null` for a device the server has no
  `last_seen_ts` for. It writes no roster. Then, under the lock it holds, it refreshes the status
  record (`Status.Refresh`, which re-reads that human's device count and newest sign-in through
  the view, VW2): C 3.11.3's "`human devices` rewrites that human's entry under the team lock",
  so the board shows a sign-in at once. A device id is printed here and nowhere else (the status
  record holds only a count).
- **HU6. `human lock` (n2h-D12).** `Admin.SetLocked(true)`, and the roster records the lock as
  the human's state `locked` (a new human state beside `enrolled` and `removed`: the roster
  grammar in `apply`, the status record's human states, `DeriveRoster` treating `locked` as
  `enrolled` for room membership, and `restore`'s merge, which RM7 already says must carry a
  locked human to `lock`). A locked human stays joined to its rooms: a lock stops sign-in, it
  kicks no one. `team apply` bootstrap step 3 no longer unlocks a declared human whose roster
  state is `locked`, and converges its account to locked when it is not; only `human unlock`, or
  removing and re-adding the name in the declaration (DC9), ends a lock. The write order is the
  roster first, then the account, so a crash is completed by running the verb again or by the
  next apply.
- **HU7. `human unlock`** is the reverse and the order is the account first, then the roster, so
  a crash leaves the account unlocked and the roster `locked`, which the next apply converges to
  locked again: the failure is always towards locked.
- **HU8. Answers of lock and unlock** are `{"v": 1, "team": "T", "name": "N", "changed": [...]}`.
- **HU9. Idempotent.** A human already in the state asked for (locked and asked to lock,
  unlocked and asked to unlock) makes no homeserver write, writes no roster, and answers an
  empty `changed` and exit 0. A `human password` or `logout` is never a no-op.
- **HU10. The status record.** After the child answers, and in every verb but `devices` after
  the roster has been written, the verb refreshes the status record under its lock (C 3.11.3:
  every change verb, `human devices`); HV8 governs its failure.
- **HU11. Roster seq.** Every roster write raises `seq` by one and never lowers it, a failed call
  included (C 3.6.1); the verbs never touch `counters`, `members`, `coordinator` or `pending`. A
  roster whose `pending` is set is completed first by the verb's lock rule, as for member verbs
  (C 3.6.3, roll forward).
- **HU12. The login flow's poll.** `human devices` takes the team lock for each call with
  `SeatWait`, so a poll every 2 s of C 3.11.3's flow queues behind a change and never races one;
  a busy lock is 75.
- **HU13. The child's answers.** `human.password` answers `ok` with a `human` object
  `{"password": "<32>"}`, `human.devices` with `{"devices": [{"id": <string>, "last_seen_ms": <int>|null}, ...]}`, the other three only `changed`. Root decodes strictly (CC6's rules):
  `human` only with `ok`, only for those two ops, exactly those keys, a device id 1 to 255
  printable ASCII bytes without a space, at most 256 devices, ids distinct, `last_seen_ms` 0 or
  more. `answerFits` gains these rows, and `validateFailure` still refuses them in a failed
  answer.
- **HU14. Failure classes.** An account the child cannot find is class `refused`; a homeserver
  that did not answer usably or kept rate-limiting is `unreachable`; the child's own files,
  `config`; the rest `failed`. A failed answer's `changed` lines are printed in the JSON document
  first (HV7).
- **HU15. Tests.** Each verb against `fakehs`, with a failure injected before each of its calls
  and each of its roster writes, then the verb run again, ending in the same state with no
  second valid password; the password never appears in any byte the verb writes (a scan of every
  file of the fake tree, the status record and stderr); `-tags tuwunel` runs HU2 and HU4 against
  a real Tuwunel (a login with the old password is refused after, the new one signs in) and HU6
  (a locked account's login is refused: D's RK10 says Tuwunel's lock semantics are unproven, so
  this is the proof or the finding).

## 6. `redact` (RD, root, C 3.6.5, DC20, A 4.2, A 12.4, M49)

`redact --team T --event ID --reason secret`. The one way a message leaves the live database,
used when a human pasted a secret into a room from Element.

- **RD1. Flags.** `--team`, `--event` and `--reason`, each required, each given once (HV4).
  `--event` passes `proto.ValidEventID`; a bad one is 64.
- **RD2. The reason is the fixed word `secret`** (n2h-D2): another word is refused 70 ("redact
  removes a leaked secret only") before the lock is taken, and the child refuses a request whose
  `reason` is not `secret` (class `refused`) as its own check. The verb is not a moderation tool.
- **RD3. The room is found from the event (gap 1, n2h-D1).** The request carries no room. The
  child reads the team's rooms record and asks `GetEvent` for the event in each room it names, in
  A's purpose order (`main`, `reports`, `questions`), a room with no ID skipped, stopping at the
  first that holds it. A `not found` (`ErrNotFound`) moves to the next room; any other failure
  stops the op and is its class (unreachable 69, else 1). If the answer carries `room_id` it
  must be the room asked, else the op fails (class `failed`, "the homeserver answered another
  room's event"). An event in none of the rooms is refused 70, "event <id> is not in any room of
  team <t>", and nothing is changed.
- **RD4. Only a message (n2h-D4).** The event must have type `m.room.message` and no `state_key`;
  any other event (a state event such as the roster or the power levels, a membership, a
  redaction) is refused 70 naming its type, because redacting it would damage the team and no
  member can send one (A 4.2). The sender is not judged: a human's, an agent's or the admin's
  message is redacted alike.
- **RD5. Already redacted.** An event whose answer carries `unsigned.redacted_because` as an
  object is already redacted: no redact is sent, the op answers `already` true, and the verb
  exits 0. (That a real Tuwunel marks a redacted event so is UNVERIFIED: H4 recorded no redacted
  event, M51's `fakehs` models the specification's shape; the `tuwunel`-tagged test proves it.)
- **RD6. The redaction.** `Client.Redact(ctx, room, event, "", "secret")`, a fresh transaction
  ID, by the admin client (the admin is the rooms' creator and the only account A lets redact;
  no power level changes). The client makes one try (M49). On success the answer carries the
  redaction's event ID.
- **RD7. A lost answer (n2h-D3).** When `Redact` fails transiently (`ErrUnreachable`), the child
  reads the event once more: if it is now redacted, the op succeeds with `redaction` null (the
  redaction's ID is unknown, `already` false); if it is not, or the read fails, the op fails
  (class `unreachable`, 69) and the owner's run again is the retry, which RD5 makes safe. The
  child never sends a second redact in one run. A refusal (403, `ErrForbidden`) is class
  `refused`.
- **RD8. The child's answer** to op `event.redact` (args exactly `{team_file, event, reason}`)
  is `ok` with a `redact` object `{"room": "<purpose>", "redaction": "<event id>"|null, "already": <bool>, "sent_ms": <int>}`: `sent_ms` the event's `origin_server_ts`, `room` one of
  A's purposes. Root decodes strictly: `redact` only with `ok`, only for `event.redact`, exactly
  those keys, `redaction` null or `proto.ValidEventID`, `room` a purpose, `sent_ms` 0 or more; an
  answer for another op carrying `redact`, or this op's without it, is refused (`answerFits`).
- **RD9. The verb's answer** is `{"v": 1, "team": "T", "event": "<id>", "room": "<purpose>", "redaction": "<id>"|null, "already": <bool>, "backups": [{"id": <n>, "time_ms": <t>|null}, ...], "exports": ["<path>", ...]}`.
- **RD10. The backups that still hold it (n2h-D5).** `backups` names the entries of
  `backup.List` whose time is the event's `sent_ms` or later, and every entry whose time is
  unknown (`time_ms` `null`), newest first: the verb never names fewer than hold the text.
  Tuwunel's backups are point-in-time copies of the database taken before the redaction, so each
  one taken after the event was sent keeps its text until Tuwunel prunes it (7 are kept, C
  3.12.1). The list is read without the child (`backup.List`, no lock beyond the verb's own); a
  failure to read it is 1, "the redaction was made; the backups could not be listed", and the
  JSON document is printed first with `backups` null (HV8's rule).
- **RD11. The exports (n2h-D5).** `exports` is `["<paths.MigrationExport(team)>"]` when that
  directory (`paths.MigrationDir(team) + "/v1-export"`, D 5.6) exists as a directory (not
  followed, `Lstat`), else `[]`. `paths` gains the one function that names it; the name is not
  in the v1-name list the gate reads (`v1-names.txt`, VERIFIED not to hold it), so the gate is
  unchanged. Migration is not built, so this is the only way an export can be known.
- **RD12. Said, not hidden.** When `backups` or `exports` is non-empty, one stderr line follows
  the JSON: `teamchat-admin: the event is redacted in the live database; backups <ids> and the exports above still hold its text until they are pruned; a copy a session already received stays in its conversation: treat the secret as exposed and rotate it`. Nothing else is said on
  stdout.
- **RD13. The status record is not refreshed** (n2h-D5): a redaction changes nothing the record
  holds. No roster write, no member, no human, no notice is posted, and no state of the team
  changes.
- **RD14. Tests.** Against `fakehs` (M51: `unsigned.redacted_because` set): the event found in
  the second and in the third room; found in none; a state event; an `m.room.reaction`; already
  redacted (no `PUT .../redact` sent); a lost answer where the redaction landed (success,
  `redaction` null), one where it did not (69, no second redact), and a refusal; a reason other
  than `secret`; a `room_id` that differs; backups of every age against `sent_ms`; the export
  directory present, absent and a link (a link is not an export); the stdout carrying one JSON
  document and nothing else, in every case. `-tags tuwunel`: a message redacted by the admin,
  read back as redacted, and the event's content gone.

## 7. The child's new ops and answers, in one place (CH)

- **CH1.** `admin.argKeys` gains `human.password`, `human.logout`, `human.devices`,
  `human.lock`, `human.unlock` (each `{team_file, name}`) and `event.redact` (`{team_file, event, reason}`), and `Serve` runs them through the `Opener` as it runs the member ops (CV1);
  `apply` gains the human operations, beside `RunMember`, which own the roster writes of HU3,
  HU6 and HU7 through the atomic writer. The `Admin` interface of `apply` gains `Devices`; the
  redact path (`GetEvent`, `Redact`) is the matrix client's own.
- **CH2.** `Answer` gains the two fields `Human` and `Redact` with the keys `human` and
  `redact`, encoded and decoded beside `view` and `notice` (CO3's rules: strict, one line, only
  with `ok`, only for their op); `validateFailure` refuses them in a failed answer;
  `validateOps` refuses an answer that carries more than one of `view`, `notice`, `export`,
  `epoch`, `human` and `redact`, or any of them beside a handle, a credential, a main room or
  notes.
- **CH3.** `human.password` does not use `Answer.Credential`: that type requires `main_room`
  beside it (BU1) and is a seat's credential; a human's password is its own key.

## 8. Decisions where the design is silent

Each is a default the coordinator may change; the reason and the alternative are given.

| ID      | Decision                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n2h-D1  | `redact` takes no room: the child finds it by asking each room of the rooms record for the event (RD3). Reason: C 3.6.5 and A 12.4 give the verb as `--team --event --reason` and nothing else; a team has three rooms, so the search is at most three reads, and an event ID is a hash that cannot be in two rooms. Alternative: a required `--room <purpose>` flag, which saves up to two reads and makes a wrong guess a refusal.                                                                                                                                                                                                                                                        |
| n2h-D2  | A reason other than `secret` is exit 70, not 64. Reason: C 3.6.5 and its test row say "refused", and the verb's point is that a rule says no to every other use (DC20). Alternative: 64, as a bad `--credential` value is (`flagArg`), on the view that a closed list of one is an argument grammar.                                                                                                                                                                                                                                                                                                                                                                                        |
| n2h-D3  | A lost redact answer is settled by one read after the failure and by the owner's next run, never by a second redact in the same run (RD7). Reason: M49 says the client makes one try because Tuwunel's replay behaviour is not recorded, and C 3.6.5 says the child reads whether the event is redacted before it tries again; RD5 makes the next run safe. Alternative: up to three tries each preceded by a read, which hides a transient failure inside one run.                                                                                                                                                                                                                         |
| n2h-D4  | Only `m.room.message` events without a `state_key` are redacted (RD4). Reason: members and humans can send nothing else (A 4.2), the leak is a pasted message, and an admin redaction of a state event (the roster, the power levels, a membership) would break the team through a typo in an event ID. Alternative: any event, trusting the owner's care.                                                                                                                                                                                                                                                                                                                                  |
| n2h-D5  | `redact` names backups by the event's time and the migration export by its directory, and refreshes no status record (RD9 to RD13). Reason: C 3.6.5 says the answer "names" them; a backup taken before the event was sent cannot hold it, so naming by `sent_ms` is exact and never under-reports (an unknown time is listed); the record holds nothing a redaction changes. Alternative: name every backup, or refresh the record as every change verb does.                                                                                                                                                                                                                              |
| n2h-D6  | `declare check` exits 70 for a SECURITY line and for a declaration the decode refuses; 78 for an `approved.json` it cannot read; 1 for an unreadable stdin. Reason: C 3.5.1 says only that the play "fails"; the table's 70 is "a rule said no", `declare.RefusedError` already maps to it (`ExitCodeOf`), and the play tells a SECURITY refusal by the `SECURITY` lines on stdout. Departure to note: `team apply` and `team approve` exit 1 for an input that fails `DecodeInput` (VERIFIED: `readInput` returns the error to `failWith`, and `ExitCodeOf` has no row for a declaration field error, so it is the default 1); aligning them is gap G3. Alternative: exit 1 for all of it. |
| n2h-D7  | The unprivileged `declare check` refuses a listening port only for a team with no stamp (DC5). Reason: it cannot see whose listener it is (another account's `/proc/<pid>/fd` is closed to it, VERIFIED in `hostnet.holder`), C 3.2.5's "listening and not this team's own" needs that, and `team apply` step 2 (root) already makes the exact judgement before any change. Alternative: no listener check in `declare check` at all (the collision shows at apply, later in the play), or a root-run check, which defeats "before any root task".                                                                                                                                          |
| n2h-D8  | The v2 half of `declare check` takes exactly DR1's keys; `agent_bus_teams` waits for `migrate/v1`'s slice (DC8). Reason: C 3.5.1 puts the v1 ports in the same stdin, DR1 refuses unknown keys, and C 3.8 says only `migrate/v1` names v1's keys. Alternative: accept and ignore the key now (a silent skip, refused by the fail-fast rule).                                                                                                                                                                                                                                                                                                                                                |
| n2h-D9  | `check` reads sandbox properties through a new `units.Manager.Sandbox` over a fixed table of boolean and string properties, compared with `host.TeamSandbox` (CK4), and names the directives it did not compare. Reason: `Manager` has only `State`, `Kill`, `Start`, `Stop` and the host and health reads (VERIFIED in `units/state.go` and `units/units.go`); a generic property reader would put every systemd value root decodes outside SD5's typed rule. Alternative: `systemctl show` through `execx` (n2f-D4 keeps systemctl for clean and journal only).                                                                                                                           |
| n2h-D10 | In `check`, every probe failure is a `FAIL` line and exit 1, and it takes no lock (CK2, CK9). Reason: C 3.5.4 says "exits 1 on any FAIL" and that it is what the acceptance runs; a report that stopped at the first broken probe would hide the rest. Alternative: map a probe's failure to 69 or 78.                                                                                                                                                                                                                                                                                                                                                                                      |
| n2h-D11 | `status` finds its teams from the stamps in the status tree (ST2), prints the fixed text of ST4, and exits 1 on a team problem only (ST6). Reason: it runs as the owner, who cannot read `team.json`, which `health.Stamps` needs; C 3.11.3 says "for a human" and gives no format, so ST4 pins one. Alternative: a `--json` flag; not built, since ccy-teams reads the records itself (C 3.13).                                                                                                                                                                                                                                                                                            |
| n2h-D12 | `human lock` is a new roster human state `locked` that `team apply` respects (HU6). Reason: bootstrap step 3 unlocks a declared human whose account is locked (VERIFIED in `apply.human`), so an account-only lock of a declared human would be undone silently by the next play; n2g's RM7 already expects a roster state for it. It touches built packages (`apply`'s roster grammar, `status` SR1's human states, `DeriveRoster`, `restore`'s merge): S5 owns that. Alternatives: refuse `human lock` of a declared human (the verb then has nothing to lock but a human the declaration already removes), or lock the account only and let the next apply undo it.                      |
| n2h-D13 | `human logout` replaces the password with a discarded random one, prints none and leaves `needs` as it is (HU4). Reason: C 3.6.5 calls it "a password reset with device logout, the lost-phone procedure", and v1's `logout-all` also replaced the password with a discarded one (D's table of v1 calls); `needs` is "set by a restore" (C 3.6.1). Alternative: set `needs` to `password`, so the board shows that this human has no usable password; a grammar use the design does not give.                                                                                                                                                                                               |
| n2h-D14 | A human's password is 32 characters of `[A-Za-z0-9]` from `crypto/rand` without bias, and the roster is written before the homeserver call (HU2, HU3). Reason: C 3.6.5 says "a random 32-character password" and `revoked_seq` must never be lower than the reset it records (a restore decides by it, C 3.12.2): writing first can only over-revoke. Alternative: base 32 as bootstrap's discarded passwords are (26 characters, not 32), or the account first (a crash would leave a reset the restore cannot see).                                                                                                                                                                       |
| n2h-D15 | `human *` and `redact` wait `SeatWait` (60 s) for the team lock. Reason: an owner at a terminal through password sudo is waiting, as a seat take is (C 3.4: 10 minutes is for play steps and the timer), and a busy lock names its holder at 75. Alternative: the 10 minutes of `PlayWait`.                                                                                                                                                                                                                                                                                                                                                                                                 |
| n2h-D16 | A human verb names a declared, enrolled or locked human with an account (HU1). Reason: DC9 makes the declaration the one route for removal and return, so these verbs never act on a removed or undeclared name. Alternative: allow `human devices` for a removed human, to look at a leak after removal.                                                                                                                                                                                                                                                                                                                                                                                   |

## 9. Gaps this spec does not close

- **G1. `coordinator_wanted` is not in `status.Record`.** C 3.11.3's example holds it and ST4's
  report cannot print it: `topKeys` and `Record` do not carry it (VERIFIED in `status.go`). The
  record, SR1 and the writer gain it in whichever slice builds role convergence's status;
  `status` then prints `coordinator wanted: <handle> (fix: <fix>)` under `restore:`, never as a
  problem.
- **G2. A migration's pointer-only record.** `team remove` of a migration-origin team rewrites
  `status.json` to `v`, `team`, `name`, `origin`, `set`, `migration`, `seq` and `written_ms`
  alone (C 3.5.3), which SR1's strict decode (every key) refuses; `status <team>` of such a team
  shows "the status record cannot be read" until area D's slice gives SR1 that shape.
- **G3. `team apply` and `team approve` exit 1 for an invalid declaration** while `declare check`
  exits 70 (n2h-D6). Mapping the declaration's field error to 70 in `ExitCodeOf` would align all
  three and changes the exit code those two verbs' tests pin: the coordinator's call.
- **G4. `check` and the D-Bus property types** (CK4) are UNVERIFIED on a real systemd until a
  host check reads them (`busctl introspect`, H-C2's neighbour); the table, not the check, is
  corrected by it.
