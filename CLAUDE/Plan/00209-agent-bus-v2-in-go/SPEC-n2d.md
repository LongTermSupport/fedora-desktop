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
  not kept and logs in once as the seat device `TCSEAT`, answering the token. `revoke` is a reset
  to a password not kept. Each sets `cred`, `cred_seq` (the write's `seq`) and `cred_ms`, and
  clears `needs`. `role` promotes the member when it is `coordinator_wanted` (MO6).

- **MO3. Refusals before any change** (70): `add` of a handle that is `active` (the seat is
  taken, perhaps just now by another checkout; choose again; `reissue` only for a bundle this
  checkout lost), `parked` or `lost` (naming `return`), or `retired` (never returns); a verb on a
  handle in no state it takes; the probe handle; `--kind` other than the prefix's type; `retire`
  of the declared or the recorded coordinator; `set --human-text false` of the declared or the
  recorded coordinator; a team with no main room (not applied).

- **MO4. `--seat next`** is the lowest number from 1 no member of that prefix in the roster holds,
  whatever its state, so a retired handle is never chosen (n2d-D3). The counter is raised to the
  seat's number when that is higher, and never lowered.

- **MO5. Roll forward, never undo (C20).** Any operation first completes the `pending` record it
  finds, from its step:

  - the same verb asked again for the same handle completes it and answers the credential,
    issuing again (a reset) when the credential step had already run;
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

- **MO8. Answers.** Each op answers its `CHANGED` lines (`member <handle> claimed|account created|issued|revoked|parked|retired|deactivated|human_text <bool>`, `notice <event> <handle>`, the rooms engine's and the role lines), the handle, and for `add`, `return` and
  `reissue` asked by the caller the credential (kind and secret, never in an error or a line).
  A failure keeps what changed before it and names the op and the step.

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
  code (CC6).
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

| ID     | Decision                                                                                                                                                                                                                         |
| ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n2d-D1 | The member operations live in `apply` beside bootstrap: they share the roster file, its writer, the derived roster, the rooms step and the roll-forward, which must see every op's steps.                                        |
| n2d-D2 | A role change is a step of its own (`role demoted <h>`), written with the `coordinator` change, so a crash between the rooms step and the notice still posts the notice once.                                                    |
| n2d-D3 | `--seat next` skips every number the prefix holds in any state: C 3.6.3's "no active, parked or lost member" would offer a retired number, which `add` must refuse.                                                              |
| n2d-D4 | `retire` refuses the recorded coordinator as well as the declared one, so no retire makes a rooms step demote without a `role` notice.                                                                                           |
| n2d-D5 | The notices' bodies: `<handle> was added to the team as <role>`, `<handle> was parked by the owner`, `<handle> was removed from the team by the owner`, `<handle> is now the <role>`. A gives the typed object and no body text. |
| n2d-D6 | `--kind` must be the prefix's type: the handle carries the kind, and two values for one fact could disagree.                                                                                                                     |
| n2d-D7 | The child receives `team.json` and the approval in its request: both are root's files, which the child cannot read (C 3.2.2's modes).                                                                                            |

## 4. `team apply`'s host steps, `team remove` and the team lock (C 3.4, 3.5.2, 3.5.3)

Written by the host builder of this slice; see its expectations below.
