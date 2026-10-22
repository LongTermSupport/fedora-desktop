# SPEC: N2 slice n2b, the declarations, the approval gate and apply (Plan 00209 Tasks 2.3, 2.4)

Built on n2a (`SPEC-n2a.md`: `teamchat-admin`'s skeleton and the rooms engine). Sources: area C's
3.8 (the declarations in host_vars), 3.5.1 (the security policy approval, DC18), 3.6.1 (the
roster), 3.6.2 steps 2 to 5 (bootstrap after the admin account), 3.6.4 (roles), 3.6.5 (humans),
DC17 (the YAML module, kept out of `teamchat-admin`) and the C 3.14 approval-gate row. Each
expectation is numbered and has a Go test written first and seen to fail. Everything here is
inert code: nothing installs, runs as root, or is wired to a verb yet.

The packages:

- `src-go/internal/teamchat/declare`: the declaration types shared with `teamchat-admin`, their
  strict JSON reader, the canonical `team.json`, and the approval gate (`approved.json`, the
  comparison and the approve logic). JSON only.
- `src-go/internal/teamchat/declare/hostvars`: the host_vars YAML reader, the one importer of
  `go.yaml.in/yaml/v3`. Only ccy-teams will import it.
- `src-go/internal/teamchat/state`: the atomic writer and the capped strict reader of C 3.6.1
  (S15).
- `src-go/internal/teamchat/apply`: the roster file, the rooms record, the roster event derived
  from them, and bootstrap steps 2 to 5 through the matrix admin client and the rooms engine.

**Out of this slice**, by the brief: plays, tasks, units, sudoers, the install, seats, bundles and
pool passwords (gates A and B); `team apply`'s steps 1 to 9 and 11 (lock, refusals against the
host, render of config and units, firewall, backup, start, readiness, the stamp); bootstrap step
1 (the admin account: the token and password files); the password-sudo boundary of
`team approve` (a seam: the verb is not wired); `declare check`'s v1 rules, which live in
`migrate/v1` (not built); DC17's godbus; the member verbs; the admin's `role` notice (apply
reports each role change for the caller to post).

## 1. The declarations (C 3.8)

- **DR1. Shape.** The input is one JSON object with exactly the keys `teamchat_address`
  (optional), `teamchat_teams`, `teamchat_lxc_members` and `teamchat_element_teams` (each a list,
  optional, absent meaning empty). A team entry has exactly C 3.8's keys: `team`, `name`,
  `state`, `purge`, `port`, `server_name`, `description`, `listen`, `allow_from`, `url`, `humans`,
  `coordinator`, `trust`. An LXC entry: `team`, `container`, `user`, `checkout`, `site`, `seat`,
  `human_text`, `service`, `state`. An Element entry: `team` alone, so a `base_url` there is
  refused as an unknown key (C 3.10, the N0 re-check's finding 5).
- **DR2. Strict.** The reader is proto's strict JSON (no repeated key, nothing after the value),
  at most 1 MiB. Any unknown key, any missing required key, any value of the wrong JSON type
  (a string where a number is wanted, and the reverse) and any null is refused. The error names
  the field by its path, `teamchat_teams[1].humans[0]`, and says what is wrong; it never echoes
  a value (A 2.2), so a declaration error can be shown wherever the play shows it.
- **DR3. Team values.** `team` passes `paths.ParseTeamID` and is unique. `state` is `present` or
  `absent`; `purge` is a boolean, allowed only with `absent`. A present team needs `port`,
  `description`, `listen`, `allow_from`, `humans` and `trust`; an absent team needs only `team`
  and `state`, and any other key it carries is still checked. `name` passes
  `paths.ParseDisplayName` (default: the id). `port` passes `paths.ParsePort` and no two teams
  share one. `server_name` is a lower-case DNS name of at least two labels, at most 253
  characters (default `paths.ServerName`). `description` passes `proto.ValidDescription`.
  `listen` is 1 to 8 distinct IP addresses, none unspecified (`0.0.0.0`, `::`), each in its
  canonical text. `allow_from` is 0 to 32 distinct CIDR prefixes, each in canonical masked text.
  `url` is `http://` or `https://`, a host (an IP or a DNS name) and an optional port, no user,
  path, query or fragment, in its canonical text; with no `url`, the URL is
  `http://<teamchat_address>:<port>`, and a present team with neither is refused. `humans` is 1
  to 16 distinct names passing `proto.ValidHumanName`, so the reserved localparts (`admin`,
  `humans`, `teamchat`, `conduit`, `tuwunel`; A 2.3, seam 18) are refused. `coordinator` passes
  `proto.ParseHandle` and is not the probe. `trust` is A's `forge` section exactly: keys `kind`,
  `org`, `repos`, `exclude`, `path_prefixes` and nothing else, decoded and validated by
  `proto.DecodeForge` (seam 16).
- **DR4. LXC values.** `team` names a team declared present. `container` is an LXC name
  (`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`); `user` passes `paths.ParseGuestUser`; `checkout` is an
  absolute, clean path other than `/`; `site` passes `proto.NormaliseSite` unchanged; `seat` is
  1 to 9999; `human_text` (default true) and `service` (default false) are booleans; `state` is
  `present` or `absent` (default `present`). No two entries share a team, container and user.
- **DR5. Element values.** `team` names a team declared present, at most once.
- **DR6. `teamchat_address`** is an IP address in canonical text, not unspecified.
- **DR7. One team's input** (what `team apply` reads on stdin and what `team approve` reads) is
  `{"team": <team entry>, "lxc_members": [<that team's LXC entries>], "address": <optional>}`,
  checked by the same rules; an LXC entry of another team is refused.
- **DR8. Canonical `team.json`.** A present team renders to one canonical JSON document with its
  defaults filled (`name`, `server_name`, `url`), keys in a fixed order, lists in their declared
  order: `{"v":1,"team":...,"name":...,"server_name":...,"port":...,"description":..., "listen":[...],"allow_from":[...],"url":...,"humans":[...],"coordinator":...,"trust":{...}}`
  (`coordinator` only when declared). It is decoded back strictly (`DecodeTeamFile`), and
  `decode(encode(t))` is `t`.

## 2. The host_vars reader (C 3.8, DC17)

- **HV1.** `hostvars.Read(data)` decodes the file to a `yaml.Node` tree and reads only the v2
  top-level keys; every other top-level key is ignored whatever it holds (`!vault` values,
  `yes`/`no`, anchors, v1's keys). A file that is not one mapping is refused. The result is the
  same `declare.Declaration` the JSON reader gives, through the same checks (DR1-DR6).
- **HV2.** Within the v2 subtrees: known keys only, duplicate keys refused, and only strings,
  integers, `true`/`false`, lists and mappings. Refused by name (the field's path): any tag
  (`!vault` named as a vaulted value), anchor or alias; a plain scalar YAML 1.1 or 1.2 reads as
  anything but a string, integer or `true`/`false` (`yes`, `no`, `on`, `off`, `y`, `n` in any
  case, `True`, `FALSE`, `null`, `~`, an empty value, a leading zero, `0x`, `0o`, `0b`, a
  sexagesimal number, a float, `.inf`, `.nan`, a timestamp, `<<`, `=`). A quoted scalar is
  always a string; a plain decimal integer is an integer.
- **HV3.** `hostvars.ReadFile(path)` reads `host_vars/<host>.yml` (at most 1 MiB) and refuses a
  directory by name, since ccy-teams could not know which file of a `host_vars/<host>/`
  directory to edit.
- **HV4.** The writer (byte splicing, C 3.8) is ccy-teams' and is not in this slice.

## 3. YAML stays out of `teamchat-admin` (DC17, security 13)

- **GD1.** `src-go/go-deps.allowlist` gains a row kind `keepout`, four fields:
  `keepout <TAB> module <TAB> package <TAB> reason`. `qa-go-deps` runs `go list -deps` over the
  package (with the gate's fixed environment) and fails naming the module and the package when
  any dependency of the package, direct or through a shared package, is in the module.
- **GD2.** A keepout row's module must also have a `module` row (a keepout of a module go.mod
  does not require guards nothing); a keepout row listed twice, or with a package that is not
  `./cmd/<name>`-shaped, is refused by the allowlist parser. A `go list` that fails is a finding
  (the build is broken), or a step that could not judge when go could not reach the proxy.
- **GD3.** The rows: `module go.yaml.in/yaml/v3` (the host_vars reader, ccy-teams' only) and
  `keepout go.yaml.in/yaml/v3 ./cmd/teamchat-admin`. govulncheck stays clean for the module.

## 4. The approval gate (DC18, C 3.5.1)

- **AG1. The gated fields** of a team: `humans`, `coordinator`, `allow_from`, `listen`, `url`
  (the resolved URL of DR3, so a changed `teamchat_address` that moves the default URL is a
  change too, and the Element profile's URL, C 3.10, is the approved one), `trust`, and
  `lxc_members` (each of the team's LXC entries as team, container, user and `human_text`),
  and the team's existence. Nothing else is gated: `name`, `description`, `port` and the LXC
  entries' `checkout`, `site`, `seat`, `service` and `state` change without approval.
- **AG2. `approved.json`** is `{"v":1,"team":...,"approved":{"humans":[...],"coordinator":..., "allow_from":[...],"listen":[...],"url":...,"trust":{...},"lxc_members":[{"container":..., "user":...,"human_text":...}]}}`, `coordinator` null when none, decoded strictly (capped at
  256 KiB) with the same value rules as the declaration. A file whose `team` is not the team
  asked for is refused.
- **AG3. Compare.** `declare.Compare(approved, input)` lists every difference, in the order of
  AG1: a team with no `approved.json` gives one difference, its existence (`team`, `- -> new`);
  a scalar field gives `<old> -> <new>` with `-` for none; a list field gives one difference per
  entry added (`- -> <entry>`) or removed (`<entry> -> -`), compared as sets; an LXC entry is
  `<container>/<user>` with `,human_text=<bool>`, and one whose `human_text` changed is a remove
  and an add; `trust` is A's compact JSON. Lists are compared as sets, so a reordering is no
  difference. Each difference prints as `SECURITY<TAB><team><TAB><field><TAB><old> -> <new>`,
  every value one line (DR2's and DR3's grammars admit no control character).
- **AG4. Check.** `declare.Check(decl, approvals)` gives the SECURITY lines of every present team
  in declaration order; an absent team is not compared (removing a team widens nobody's access).
  `team apply`'s gate, `apply.Gate`, refuses with drift (exit 70) on any difference, naming
  `ccy-teams approve <team>`, before any change.
- **AG5. Approve, whole.** `team approve --team T` with no `--only` writes the input's gated
  fields as `approved.json` (DR7 input).
- **AG6. Approve one change.** `team approve --team T --only <field>` with exactly one of
  `--add <entry>`, `--remove <entry>` or `--set <value>`:
  - `humans`, `allow_from`, `listen`, `lxc_members` take `--add` or `--remove`; `lxc_members`
    also takes `--set`, which replaces the entry with that container and user (a `human_text`
    change); `coordinator` takes `--set <handle>` or `--remove <handle>`; `url` and `trust` take
    `--set`; `team` (a create) takes `--set` with the whole `approved` object as the owner
    entered it on the board, and only when no `approved.json` exists.
  - The entry is the text AG3 prints (a human name, a CIDR, an address,
    `<container>/<user>,human_text=<bool>`), checked by the field's grammar.
  - The expected value is the approved value with exactly that change applied; a change that
    changes nothing (adding what is there, removing what is not) is refused.
  - The input's gated fields must equal the expected value in every field, the named field
    included; otherwise the approve is refused with 70, naming every difference and
    `ccy-teams approve <team>`, and writes nothing. So an entry an agent planted beside the
    owner's `--add` in the same list is never approved (the N0 re-check's finding 4).
  - Any other team field than `team` with no `approved.json` is refused: a team not yet
    approved is approved whole or by `--only team`.
- **AG7. The file.** `declare.FileApprovals` reads `approved.json` (absent is "not approved") and
  writes it through `state.WriteFile`, mode 0640, group the owner group's gid it is given. The
  password-sudo boundary and the group lookup (`/etc/teamchat/owner-group`) are the verb's, a
  later slice.

## 5. The state files (C 3.6.1, S15)

- **ST1. `state.WriteFile(path, data, mode, gid)`** creates a temporary file beside the target
  (`O_CREAT|O_EXCL|O_NOFOLLOW`, a random name), `fchmod`s it to mode, `fchown`s it to gid when gid
  is not -1, writes, fsyncs, renames it over the target, and fsyncs the directory. Any failure
  removes the temporary file and is returned; the target is either the old or the new content.
- **ST2. `state.ReadFile(path, max)`** opens with `O_NOFOLLOW`, refuses anything but a regular
  file and a file over max bytes, and answers found false for no file.

## 6. Apply (C 3.5.2 step 10, 3.6.1, 3.6.2 steps 2-5, 3.6.4, 3.6.5)

- **AP1. The roster file** `ctl/<team>/roster.json` (`paths.RosterFile`) is C 3.6.1's shape:
  `v`, `team`, `seq`, `rev`, `restore_epoch`, `counters`, `members`, `humans`, `coordinator`,
  `coordinator_wanted`, `pending`, decoded strictly (exact keys, no repeats, capped at 1 MiB;
  a member: `state`, `kind`, `human_text`, `since_ms`, `cred`, `cred_seq`, `cred_ms`, `needs`; a
  human: `state` (`active` or `removed`), `since_ms`, `revoked_seq`, `needs`; `pending`: `op`,
  `step`, `since_ms`, and `handle` only for a member operation). A missing file is a new team's
  empty roster at `seq` 0.
- **AP2. Every write of the roster file raises `seq` by one** and never lowers it; a run that
  changes nothing writes nothing.
- **AP3. The rooms record** `ctl/<team>/rooms.json` (`paths.RoomsFile`) is
  `{"v":1,"team":...,"main":...,"reports":...,"questions":...}`, each a room ID or absent,
  decoded strictly. It is the engine's `Recorder`: `Intent` writes `pending`
  (`{"op":"team.apply","step":"create <purpose>"}`) into the roster before a create, `Record`
  writes the ID into the rooms record and clears the step back to `rooms`.
- **AP4. The roster event is derived, never edited** (C 3.6.1): `apply.DeriveRoster(file, team)`
  is A's roster from the roster file and the applied `team.json`: `team`, `name`, `rev` the
  file's, `restore_epoch`, `humans` the declared humans' user IDs (only `active` ones of the
  file), `members` every `active` or `parked` member (parked true), its role `coordinator` for
  the file's `coordinator` and `worker` otherwise, its `human_text`, `forge` the declaration's
  `trust`, and A's default limits. A `retired`, `lost` or intent-state member is not in it.
  `proto.Roster.Validate` must accept the result before the rooms step runs.
- **AP5. The gate first.** `apply.Run(ctx, deps, input)` first holds the input to the approval
  (AG4) and refuses with 70 before any call. The team it applies is then the input as approved,
  rendered to `team.json` (DR8) and handed back for the caller to write in step 11; the roster
  is derived only from that (C 3.6.1).
- **AP6. One server admin** (3.6.2 step 2): `ServerAdmins` must be exactly `@admin:<server>`,
  else refused (70) naming the others.
- **AP7. Humans** (step 3): each declared human with no account gets one, created with a random
  password that is not kept and no `admin` key (`CreateAccount`), and enters the file `active`
  with `revoked_seq` the write's `seq` (`human <name> created`). A declared human whose account
  is locked, or who is `removed` in the file, is unlocked and made `active` (DC9, `human <name> unlocked`). An account that exists but is an admin or deactivated is refused (70). A human in
  the file but no longer declared becomes `removed` in the file before the rooms step (so the
  roster event drops it and the rooms engine kicks it from every room, C15), and is locked
  after it (`human <name> locked`).
- **AP8. Rooms** (step 4): `rooms.Converge` over the derived roster and the rooms record; the
  roster `rev` the engine wrote is stored in the file. The engine's CHANGED lines are passed on.
- **AP9. Roles** (step 5, 3.6.4). Before any change: a declared coordinator that is `retired` in
  the file is refused (70, C18); one whose `human_text` is false is refused (70, A's rule, S11
  4.5). Then, when the declared coordinator differs from the file's: the old one is demoted
  (the file's `coordinator` cleared) and the rooms converged; only then is the new one promoted
  (`active` or `parked`) and the rooms converged again (C17), `role <handle> demoted` and
  `role <handle> promoted`. A declared coordinator not in the file, or `lost`, is recorded as
  `coordinator_wanted` and the team runs with none (DC6). No coordinator declared clears both.
- **AP10. Idempotent.** A second run over an applied team makes no write call, no roster write
  and prints nothing.
- **AP11. Failures** name the step (`gate`, `admins`, `humans`, `rooms`, `roles`, `lock`) and keep
  the CHANGED lines and the roster file written before them; `apply.ExitCodeOf`'s mapping is
  `admin.ExitCodeOf`'s (drift 70, unreachable or rate-limited 69, else 1).
- **AP12.** End to end on fakehs: a new team from an empty roster (humans created, rooms made,
  roster event accepted by `proto.VerifyMain`), a second run that changes nothing, a human
  removed (kicked, then locked) and added back (unlocked), a coordinator changed with the
  demotion's rooms write before the promotion's, a declaration edit not approved refused before
  any call, and the roster event rendered from the approved team while a different `trust`
  sits in the input. With `-tags tuwunel`, the new team and the converged second run against
  the pinned Tuwunel through `matrix/tuwunelhs`.

## 7. Decisions where the design is silent

| ID      | Decision                                                                                                                                                  |
| ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n2b-D1  | `trust` is A's `forge` section exactly (`kind` required, no `forge_api`): C 3.8 says its example keys are v1's and the shape is A's.                      |
| n2b-D2  | The gated `url` is the resolved one, so the default URL's inputs (`teamchat_address`, `port`) are gated through it; `port` alone is not gated.            |
| n2b-D3  | A list field is compared and approved as a set of entries: a reorder is no change, and each entry is one SECURITY line and one `--add` or `--remove`.     |
| n2b-D4  | An absent team is not compared: removal narrows access. Its `approved.json` is left to `team remove` (not built).                                         |
| n2b-D5  | The one-team input carries that team's LXC entries and the address, so `team apply` can compare every gated field DC18 names, LXC members included.       |
| n2b-D6  | A coordinator whose `human_text` is false, or who is retired, is refused before any change, rather than demoting first and then failing.                  |
| n2b-D7  | A human's file state is `active` or `removed`; `removed` is locked on the server. The roster event carries only `active` declared humans.                 |
| n2b-D8  | `pending` during apply is `team.apply` with the step; room creates are its `create <purpose>` steps (the engine's intent). Apply clears it at the end.    |
| n2b-D9  | The admin's `role` notice is not posted here; apply answers each role change and the verb that posts notices (later slice) posts it.                      |
| n2b-D10 | `keepout` is a new row kind of the go-deps gate (Plan 00204's), so the DC17 rule lives in the gate the design names rather than in a test of one package. |
| n2b-D11 | The declaration error never echoes the value (A 2.2's rule for the roster), so the play may print it as it is.                                            |
