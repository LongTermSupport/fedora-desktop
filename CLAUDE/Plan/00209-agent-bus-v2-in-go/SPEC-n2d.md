# SPEC: N2 slice n2d, the member operations, the admin verbs and `team apply`'s host steps (Plan 00209 Task 2.3)

Built on n2a (`SPEC-n2a.md`) and n2b (`SPEC-n2b.md`). Sources: area C's 3.3 (the verbs, the child,
the exit codes), 3.4 (the team lock), 3.5.2 (`team apply`), 3.5.3 (`team remove`), 3.5.1 (DC18's
`team approve`), 3.6.1 (`pending`), 3.6.3 (members, C20's roll-forward), 3.6.4 (roles) and A 7.4
(the admin's notices). Each expectation is numbered and has a Go test written first and seen to
fail. Everything here is inert code: nothing installs, runs as root or is started by a unit.

The packages:

- `src-go/internal/teamchat/apply`: the member operations and `role apply` beside n2b's bootstrap,
  over the same roster file, the same rooms step and one roll-forward.
- `src-go/internal/teamchat/admin`: the root verbs, the child's operations, and the seams root
  calls through.
- `src-go/internal/teamchat/host`: `team apply`'s steps 1-9 and 11, `team remove`, and the team
  lock (section 4).

**Out of this slice**, by the brief: plays, units, sudoers, install, seats and passwords (gates A
and B); the seat store (n2c), so the bundle is built through an interface; health, status
records, backups and Element profiles, each a seam where a step calls it; starting the child
(n2a-D6), so root reaches the child through an interface whose test form serves the request
in-process over the real codecs; `member` verbs for the probe handle (D's `migrate` verbs).

## 1. The member operations (C 3.6.3, 3.6.1)

- **MO1. One record.** Every operation writes `pending` (`op`, `step`, `since_ms`, and `handle`
  for a `member.<verb>`) before its first homeserver call, moves `step` forward in the same write
  as the roster change it records, and clears it after its last call. The ops: `member.add`,
  `member.return`, `member.reissue`, `member.park`, `member.retire`, `member.set`, `role.apply`
  (no handle), and n2b's `team.apply`.

- **MO2. The steps**, each idempotent:

  | Op        | From               | Steps                                                                                                 | To        |
  | --------- | ------------------ | ----------------------------------------------------------------------------------------------------- | --------- |
  | `add`     | none               | `claim` (state `claimed`, counter raised); `account`; `credential`; `rooms`; `notice` `added`; `role` | `active`  |
  | `return`  | `parked`, `lost`   | `account` (`lost` only: created); `credential`; `rooms`; `notice` `added`; `role`                     | `active`  |
  | `reissue` | `active`           | `credential`                                                                                          | `active`  |
  | `park`    | `active`           | `revoke`; `rooms`; `notice` `parked`                                                                  | `parked`  |
  | `retire`  | `active`, `parked` | `rooms` (kicked everywhere); `deactivate`; `notice` `removed`                                         | `retired` |
  | `set`     | `active`, `parked` | `rooms` (`human_text` changed)                                                                        | same      |

  `account` creates the account with a random password that is not kept when there is none; one
  that exists deactivated is refused (70). `credential` is C 3.6.3's issue: `password` resets
  the password to a fresh random one with device logout and answers it; `token` resets it to one
  not kept and logs in once as the seat device `TCSEAT`, answering the token, and keeps a copy of
  it as `ctl/<team>/guest/<handle>.token` (0600); a password issue, a revoke, `park` and
  `deactivate` remove the copy (n2d-D18). `revoke` is a reset to a password not kept. Each sets
  `cred_seq` (the write's `seq`) and `cred_ms`; an issue sets `cred` to its kind and clears
  `needs`, a revoke sets `cred` to null (n2d-D19). An account found deactivated at the `account`
  step ends the operation: the handle is recorded `retired` and `pending` cleared in one write,
  so no later verb is blocked; the same verb asked again is refused (70), and another verb that
  ends it this way in its roll-forward says so in `notes` and runs as asked (n2d-D17). `role`
  promotes the member when it is `coordinator_wanted` (MO6).

- **MO3. Refusals before any change** (70): `add` of a handle that is `active` (the seat is
  taken, perhaps just now by another checkout; choose again; `reissue` only for a bundle this
  checkout lost), `parked` or `lost` (naming `return`), or `retired` (never returns); a verb on a
  handle in no state it takes; the probe handle; `--kind` other than the prefix's type; `retire`
  of the declared or the recorded coordinator; `set --human-text false` of the declared or the
  recorded coordinator; a team with no main room (not applied); `add` or `return` of a handle
  whose account is there deactivated; an `add` asked again for a pending add's handle with
  another `human_text`.

- **MO4. `--seat next`** is the lowest number from 1 no member of that prefix in the roster holds,
  whatever its state, so a retired handle is never chosen (n2d-D3). A number whose account is
  there deactivated is recorded `retired` (`member <h> retired`) and passed (n2d-D21). The
  counter is raised to the seat's number when that is higher, and never lowered.

- **MO5. Roll forward, never undo (C20).** Any operation first completes the `pending` record it
  finds, from its step:

  - the same verb asked again for the same handle completes it and answers the credential,
    issuing again (a reset) when the credential step had already run; `add --seat next` asked
    again on the pending add's prefix is the same verb for its handle (n2d-D15); a `set` asked
    again with the other value completes the first and then runs as asked;
  - any other verb completes a pending `add`, `return` or `reissue` to `active` with its
    credential revoked, answers no credential and sets `needs` to `reissue`;
  - a pending `park`, `retire`, `set` or `role.apply` is completed as it was asked;
  - a pending `team.apply` (a room create or the rooms step, n2b-D8) is completed by the rooms
    step, which adopts a room created but not recorded; a `team.apply` role step by MO6.
    `team apply` does the same: n2b-D22's refusal is replaced by this roll-forward.

- **MO6. Roles.** Each demotion and promotion is a step of its own, `role demoted <handle>` or
  `role promoted <handle>`, written with the roster's `coordinator` change; then the rooms step;
  then the `role` notice. A resumed role step finishes its notice and then converges the
  coordinator to the applied team's again (C17: a demotion is never deferred). `role.apply` is
  this over `team.json`'s coordinator with n2b's AP9 rules. `add` and `return` promote their
  handle when it is `coordinator_wanted`; one A refuses (`human_text` false) leaves the add done
  and `coordinator_wanted` set, and the answer says so.

- **MO7. The admin's notices (A 7.4)**, in the main room, composed by `proto.Composer.Notice` as
  the admin over the roster just written: `added` (role), `parked`, `removed`, `role` (the new
  role). `to` is the active humans and the non-parked coordinator, never the subject. The body is
  one fixed sentence per event (n2d-D5). The transaction ID is `tc` and 32 hex digits of a
  SHA-256 over the op, the handle, `since_ms` and the step, so a re-post after a lost answer or a
  crash is stored once (M22). None is posted for the probe.

- **MO8. Answers.** Each op answers its `CHANGED` lines (`member <handle> claimed|account created|account adopted|issued|revoked|parked|retired|deactivated|human_text <bool>`, `notice <event> <handle>`, the rooms engine's and the role lines), the handle, and for `add`, `return` and
  `reissue` asked by the caller the credential (kind and secret, never in an error or a line).
  A failure keeps what changed before it and names the op and the step: a roll-forward's
  failure names the pending op, not the caller's.

## 2. The child's operations and the root verbs (C 3.3, 3.5.1)

- **CV1. Served ops.** The child serves `member.add|return|reissue|park|retire|set`, `role.apply`
  and `team.apply` (bootstrap). Its args carry what only root can read: `team_file` (the applied
  `team.json` object, decoded by `declare.DecodeTeamFile`) and for `team.apply` the input and the
  approval. The admin token and the team's files are the child's own (`ctl/<team>/`). Each op's
  args are decoded strictly (exact keys).
- **CV2. The answer** gains `credential` (`{"kind":"password|token","secret":...}`, only with
  `ok` and only for `add`, `return`, `reissue`) and `notes` (one-line problems for the status
  record, such as a refused promotion). A secret is printable ASCII without a space, 1-4096
  bytes, and no error or line quotes it.
- **CV3. The root verbs** `member add|return|reissue|park|retire|set` and `role apply` parse
  their flags strictly (64 on a bad one), take the team lock (75 busy, naming the holder), read
  `team.json`, ask the child, print every `CHANGED<TAB><line>` for the play, or one JSON document
  for ccy-teams' verbs: `{"v":1,"changed":[...]}`, with `bundle` from the seat side's `Bundles`
  seam for an answer that carries a credential. The child's error class maps to C 3.3's exit
  code (CC6). Root checks an `ok` answer against the request before acting on it: the handle
  asked for (for `add`, the prefix's, numbered by the seat unless it is next; none for
  `role apply`), and a credential of the kind asked for exactly when the verb issues one;
  anything else is 1 with nothing on stdout and no bundle (n2d-D16). The document is printed
  whenever the child answered, a failure included, and its bundle is valid whatever the exit
  code; stdout is empty when no answer came (64, 75, a team not applied, a child that did not
  start) (n2d-D20).
- **CV4. `team approve --team T [--only F --add|--remove|--set V]`** reads the one-team input on
  stdin, calls `declare.Approve`, and writes `approved.json` through `declare.FileApprovals`
  with the owner group's gid from the seam `OwnerGroup` (the group file and the password-sudo
  boundary are install work, gate A). It prints `CHANGED<TAB>approved <field>`.
- **CV5. `team apply`** and **`team remove`** run section 4's steps, step 10 being the child's
  `team.apply`.
- **CV6. End to end on fakehs**: each verb through the real flag parser, the lock, the codecs
  and an in-process child; a crash at each step of each operation (a fault on the step's call,
  or the answer of a landed call lost), and the next run rolling it forward with the right
  credential rule and one notice. With `-tags tuwunel`, `add`, `role` and `remove` (`retire`)
  against the pinned Tuwunel.

## 3. Decisions where the design is silent

| ID      | Decision                                                                                                                                                                                                                                                                                                                                                                                |
| ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n2d-D1  | The member operations live in `apply` beside bootstrap: they share the roster file, its writer, the derived roster, the rooms step and the roll-forward, which must see every op's steps.                                                                                                                                                                                               |
| n2d-D2  | A role change is a step of its own (`role demoted <h>`), written with the `coordinator` change, so a crash between the rooms step and the notice still posts the notice once.                                                                                                                                                                                                           |
| n2d-D3  | `--seat next` skips every number the prefix holds in any state: C 3.6.3's "no active, parked or lost member" would offer a retired number, which `add` must refuse.                                                                                                                                                                                                                     |
| n2d-D4  | `retire` refuses the recorded coordinator as well as the declared one, so no retire makes a rooms step demote without a `role` notice.                                                                                                                                                                                                                                                  |
| n2d-D5  | The notices' bodies: `<handle> was added to the team as <role>`, `<handle> was parked by the owner`, `<handle> was removed from the team by the owner`, `<handle> is now the <role>`. A gives the typed object and no body text.                                                                                                                                                        |
| n2d-D6  | `--kind` must be the prefix's type: the handle carries the kind, and two values for one fact could disagree.                                                                                                                                                                                                                                                                            |
| n2d-D7  | The child receives `team.json` and the approval in its request: both are root's files, which the child cannot read (C 3.2.2's modes).                                                                                                                                                                                                                                                   |
| n2d-D15 | A pending add is retried by `add` of its handle, or by `add --seat next` on its prefix: `lxc apply`'s retry asks for next again, and treating it as another verb leaked a seat (review 1, finding 1).                                                                                                                                                                                   |
| n2d-D16 | Root checks the child's handle and credential against the request before the bundle: the answer is the one thing root parses that a less-privileged process wrote (review 1, finding 4).                                                                                                                                                                                                |
| n2d-D17 | An account found deactivated at the `account` step (after the refusals' look) records the handle `retired` and clears `pending`: only `retire` deactivates, so the handle can never come back, there is nothing to roll forward, and a refused step that stays pending would block every verb. A roll-forward that ends this way is a note, not the caller's failure (reviews 1 and 2). |
| n2d-D18 | The child keeps the guest token copy (C 3.6.3) through `apply.Deps.GuestDir` (`paths.GuestTokenDir`, its own tree), and removes it with the credential, so the health tick never holds a dead token. The production `Opener` sets it.                                                                                                                                                   |
| n2d-D21 | `--seat next` records a free number whose account is deactivated as `retired` and passes it: `lxc apply` cannot choose another number, and refusing would repeat on every try (review 2, finding 2). An explicit seat is refused with no write.                                                                                                                                         |
| n2d-D19 | A revoke sets `cred` to null and `cred_seq` to the write's `seq`, so a restore that decides by `cred` and `cred_seq` (C 3.6.1, 3.12.2) never takes a revoked credential as live.                                                                                                                                                                                                        |
| n2d-D20 | A member verb whose child answered prints its document whatever the exit code, and its bundle is valid: a failed status refresh after an issue exits 1 and must not lose the credential (n2d-D11). Stdout is empty when no answer came.                                                                                                                                                 |

## 4. `team apply`'s host steps, `team remove` and the team lock (C 3.4, 3.5.2, 3.5.3)

`host.Apply(ctx, Deps, declare.Input)` is steps 1-9 and 11, with step 10 the function the
verb supplies (`Deps.Bootstrap`); `host.Remove(ctx, Deps, team, purge)` is `team remove`. Both
answer the CHANGED lines, those before a failure included, and fail with `*host.Error` (verb,
step, drift). Every host effect is a seam of `Deps`: systemd, firewalld, the network, the
readiness probe, the status record, the backup, the bootstrap and the approvals; files are read
and written under `Deps.Root`.

- **HS1. The lock file.** `TakeLock` opens `<LockDir>/team-<team>.lock`, 0600, never through a
  link, in a directory of mode 0700 (made 0700 when missing, refused otherwise), and while held
  the file holds the line `<verb> <team> pid=<n> since=<ms>`; `Release` clears the line and
  keeps the file. A verb that is not one printable word is refused.
- **HS2. Busy and wait.** A held lock with no wait is a `*BusyError` at once, carrying the
  holder's line; a wait polls on the clock it is given and ends at its bound (a play waits
  `PlayWait`, 10 minutes); a holder that lets go during the wait hands the lock over; two
  teams' locks are independent. Apply and Remove fail at step `lock` wrapping the BusyError.
- **HS3. The refusals (step 2)**, each drift (exit 70), before anything changes: a
  `server_name` other than the one `server-name` fixed at the first apply (even when that first
  apply failed after fixing it); an unapproved declaration (`declare.Hold`); a listen address
  that is a wildcard, not an address, on no interface or on more than one; a port another v2
  team's `units.json` records, or that a socket of anything but this team's unit listens on.
  Entries of `root/` that are not team ids or not directories are not teams.
- **HS4. The render (step 3)** is C 3.2.3's config, 3.2.4's drop-in and declare's canonical
  `team.json`, in memory: every config key is one the pinned Tuwunel documents, bare keys
  before tables; loopback is bound and allowed once; an IPv6 listen address is allowed as a
  /128; a team with a present LXC member also admits the live bridge's subnet (a bridge that
  cannot be read fails `render`); each listen interface's device unit is systemd's escaped
  name. A registration key that is not 128 lowercase hex, no pinned version, or a `MemoryHigh`
  or `cache_capacity_modifier` that is not a positive value is refused.
- **HS5. The digest sets and the five-way answer.** The restart set covers the base unit,
  `ExecStart` and the config; the reload set covers the cgroup lines and `[Unit]`; each changes
  only with what it covers. No drop-in is a first start; no `units.json` or a changed restart
  set a restart; a changed reload set a reload; a changed `team.json` a write alone; else
  nothing. `units.json` round-trips and is read strictly.
- **HS6. First-apply files (step 4) and the unit (step 7).** A first apply (no `server-name`)
  makes `root/<team>/` and `ctl/<team>/` 0700 (the latter owned by teamchat-ctl), and creates
  `config.toml` (0600, its new registration key from `Deps.Rand`) and `server-name` (0600) by
  link, never replacing a file. Step 7 writes a config or drop-in (0644, its directory 0755)
  that differs from the rendered one, then restarts a running unit on a first start or a
  restart, starts a stopped one (even when nothing changed), and enables it. Every directory's
  mode and owner are set after the mkdir, whatever the umask, also on one that is already there
  (a crash between the mkdir and the chmod), through a descriptor opened without following a
  link, so the control user cannot steer root's chmod through `ctl/` (C 3.2.2). A created
  file's directory is synced after the link.
- **HS7. The firewall (step 5).** One rule per allow_from source in the zone that routes it;
  `firewall.json` holds the union while rules are added and removed, adds come before removes,
  each converged on its own in the permanent and the runtime configuration (a rule lost from
  one alone is added back there), then the new set alone is recorded. A removal that fails
  stops the apply with the union still recorded. A changed allow_from is a reload, never a
  restart. `firewall.json` round-trips in order and is read strictly.
- **HS8. The backup (step 6).** A team with a database whose restart set changes is backed up
  through `Deps.Backup` under the held lock, before anything is rewritten or restarted; an
  inactive homeserver is first started on the version `units.json` records (its drop-in
  written from that record when a remove deleted it) and waited for. A database with neither
  a drop-in nor `units.json` fails `backup`; a failed backup stops the apply.
- **HS9. Readiness (step 8).** Up to 60 s on the given clock for the unit to be active and its
  `server_version` to answer; activating and reloading are waited for; any other state fails
  at once, the journal tail given only as explanation.
- **HS10. Bound addresses (step 9).** The unit's sockets on the port are loopback and the
  listen addresses exactly, or `addresses` fails.
- **HS11. Records and the stamp (step 11).** `team.json` and `units.json` are written when they
  differ, then the stamp (0644 in the 0755 status directory) last, then `Status.Refresh`, told
  `first` when there was no stamp before. Two stamps that differ only in `applied_ms` are the
  same, so an unchanged apply does not rewrite it.
- **HS12. Idempotence.** A second apply with nothing changed reports nothing and changes
  nothing; a second remove reports nothing.
- **HS13. Each failure named.** A failure of any step fails the verb as `<verb>, step <step>:`,
  is not drift, keeps the lines before it (Bootstrap's included), stops every step after it,
  and releases the lock; no failed apply writes a stamp.
- **HS14. `team remove [--purge]`.** Under the lock (`team.remove`): a team with none of
  `root/<team>/`, `ctl/<team>/`, the drop-in or the stamp is a success with no lines and no
  calls. Otherwise the unit is stopped and disabled (each only if needed); the rules
  `firewall.json` records, and only those, are removed in both configurations, then the
  record; the drop-in is deleted (its directory only when empty) with a daemon-reload; the
  stamp is deleted; `Status.Remove` is called (a migration-origin record's pointer is the
  seam's to keep). With `--purge` also the homeserver's state (`systemctl clean`, when there
  is one), `ctl/<team>/`, `approved.json` and `root/<team>/` last; the status directory stays.
  A failed remove is finished by the next.
- **HS15. A reload is owed until it has succeeded.** Whether systemd must reload does not
  depend on whether the drop-in's bytes on disk match the render: a reload can fail, or a
  crash come, after the write. `team apply` reloads before it starts or restarts the unit
  (for the backup in step 6 too) whenever it wrote a drop-in, or `units.json` does not record
  this render's digests (a reload, restart or first start; a changed base unit included).
  `units.json` and the stamp are written only after that reload succeeded, so neither is
  current before it has. `team remove` reloads on every remove of an installed team, whether
  or not the drop-in was still there.

Decisions where the design is silent (n2d-H):

| ID      | Decision                                                                                                                                                                                      |
| ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n2d-H1  | Apply and Remove take the team lock themselves (`team.apply`, `team.remove`); root, Fedora and decoding the input are the verb's.                                                             |
| n2d-H2  | The approval gate (`declare.Hold`) is also held at step 2, so no firewall or listen change of an unapproved declaration lands before bootstrap's own gate.                                    |
| n2d-H3  | `team.json` is `in.Team.Canonical()`, equal to the bootstrap's approved TeamFile whenever the gate holds (lists as sets, scalars equal).                                                      |
| n2d-H4  | The registration key is inline in `config.toml`, read back from the installed config; a first apply makes it from `Deps.Rand` and creates the config by link in step 4, with `server-name`.   |
| n2d-H5  | A first apply is one with no `server-name`; the status record's `first` is no stamp before this apply.                                                                                        |
| n2d-H6  | Step 7 starts a unit it finds stopped even when nothing changed.                                                                                                                              |
| n2d-H7  | `units.json` absent with a drop-in present is a restart; no drop-in is a first start.                                                                                                         |
| n2d-H8  | The step 6 backup start writes the old drop-in from `units.json` (version, port, listen) with no device ordering and no allow_from; a database with neither a drop-in nor `units.json` fails. |
| n2d-H9  | Rules record their port, so a rule of an old port is still removed; a CHANGED line is per rule, whichever configuration changed.                                                              |
| n2d-H10 | `MemoryHigh` and `cache_capacity_modifier` come from `Deps.Pin` (H-C1 not yet measured).                                                                                                      |
| n2d-H11 | The old `tuwunel-<version>` binary deletion (C 3.2.3) is not built here.                                                                                                                      |
| n2d-H12 | Every directory root makes is chmodded to its mode after the mkdir, so the umask of the shell that ran the verb never decides it.                                                             |
| n2d-H13 | "Not installed" is none of `root/<team>/`, `ctl/<team>/`, the drop-in or the stamp; it is asked under the lock, as `installed`, a step of its own.                                            |
| n2d-H14 | Remove deletes the drop-in directory only when it is empty: another file there is the owner's override and stays.                                                                             |
| n2d-H15 | `--purge` also deletes `approved.json`: a purged team that comes back is approved again. `root/<team>/` goes last, so a purge that failed is still installed and the next one finishes it.    |
| n2d-H16 | The purge's `systemctl clean` runs only when the homeserver's private state directory is there, so a purge after a plain remove of a team never started reports nothing for it.               |
