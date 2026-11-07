# N2 slices: what each covers and what it leaves out

Moved out of `PLAN.md` (Tasks 2.3 and 2.4) so the plan stays within its size limit; the text is
unchanged. The tick of each task, and what is still missing, stay in `PLAN.md`.

## Task 2.1: the N2 spec, `teamchat-admin` and the child

Slice n2a (`feat/bus-v2-n2a`, `SPEC-n2a.md` CA1-CA7 and CC1-CC6, report
`subagent-reports/261021-n2a.md`) covers the command skeleton: C 3.3's verb table, the caller
classes (exit 77), every unbuilt verb exiting 1, `version`, and the child's boundary as a typed
start spec with its self-check and strict request and answer codecs. Not covered: starting the
child (it waits for `execx` to carry a credential and a replaced environment, n2a-D6).
DC17's dependencies: the YAML module (`go.yaml.in/yaml/v3`) has landed with slice n2b, with its
go-deps allowlist row and the `keepout` row that holds it out of `teamchat-admin` (Task 2.4).
`godbus/dbus/v5` landed with slice n2f (n2f-D4, the units seam over D-Bus), with its go-deps
allowlist row and the `keepout` rows that hold it out of the member binary.

## Task 2.2: the homeserver instance per team

Done, as inert code: each team's `config.toml` (C 3.2.3) and its drop-in (C 3.2.4) are
rendered by `host/render.go` (slice n2d, `SPEC-n2d.md` HS4, report
`subagent-reports/261022-n2d.md`), and `team apply` writes them (Task 2.3). Slice S7a
(`subagent-reports/261105-n2-s7-sonnet.md`) put D 4.2's `InaccessiblePaths=` line, held once as
`v1names.InaccessiblePaths`, into every unit text Go makes: the base sandbox (so a team's
sandbox and the restore's staging unit), the restore's secure unit and the backup service. A
test makes that true of every Go file that holds unit text, so a new one fails until it is
judged. Not done: the pinned Tuwunel's install and the `teamchat-server@<team>` unit template
(play tasks, so they wait for gate A, Task 2.5), and the health and seats unit texts (slice
S7b). Nothing ran on a host: the secure unit now has a mount namespace beside
`JoinsNamespaceOf`, which H-C5's restore run is what proves harmless.

## Task 2.3: install and apply, the rooms engine, members and roles

Slice n2a (`SPEC-n2a.md` RE1-RE10) covers the rooms engine of C 3.6.2 step 4 only
(`internal/teamchat/rooms`): find, adopt or create in A 5.2's order, drift refused before any
write, kicks before state before invites, the roster `rev` rule, idempotent, and accepted by
proto's trust checks, on fakehs and, under `-tags tuwunel`, against the pinned Tuwunel 1.9.4.

Slice n2b (`feat/bus-v2-n2b`, `SPEC-n2b.md` AG1-AG7, ST1-ST2 and AP1-AP12, report
`subagent-reports/261022-n2b.md`) covers, as inert code: the DC18 approval gate
(`declare`: compare, check, approve whole or one entry, `approved.json`); the state files'
atomic writer and capped reader (`state`); and `team apply`'s step 10 (`apply`): the gate
first, one server admin, the humans (created, adopted, locked, unlocked), the rooms through the
engine from the roster derived from the roster file and the approved team, `rooms.json`, and
the coordinator demoted before the new one is promoted, on fakehs and under `-tags tuwunel`
against the pinned Tuwunel 1.9.4. The reserved localparts are refused as human names (DR3,
proto's list). Not covered: the play's install tasks, `team apply`'s steps 1-9 and 11, bootstrap
step 1, `team approve`'s verb and password-sudo boundary, `team remove`, and the member verbs.
Carried to the member verbs: `team apply` refuses another operation's `pending` record
(n2b-D22); when those verbs land it becomes C 3.6.3's roll-forward.

Slice n2d (`feat/bus-v2-n2d`, `SPEC-n2d.md` MO1-MO8, CV1-CV6 and HS1-HS15, report
`subagent-reports/261022-n2d.md`) covers, as inert code: the member operations
(`add|return|reissue|park|retire|set`) and `role apply` with their `pending` roll-forward
(n2b-D22's refusal replaced), the admin's notices (n2b-D9 replaced), the guest token copy;
the team lock; `team apply`'s steps 1-9 and 11 (`host.Apply`) and `team remove [--purge]`; the
`teamchat-ctl` child's ops; and the `teamchat-admin` verbs over seams (`member *`,
`role apply`, `team approve|apply|remove`), with crash and roll-forward tests on fakehs. Not
covered: the `-tags tuwunel` round trip of the verbs (CV6's last line); the production
`Child`, `Opener`, `Bundles` and `OwnerGroup` (`cmd/teamchat-admin` wires no `Sys`); the
play's install tasks (gate A). C 3.5.2 step 1's Fedora check came with slice n2f (HF3:
`checkFedora` in `admin/hostfiles.go`, run first by `team apply`).

Slice n2f (`feat/bus-v2-n2f`, `SPEC-n2f.md`, report `subagent-reports/261022-n2f.md`) covers,
as inert code, the production side of every seam: the `teamchat-ctl` child started by
`ExecChild` (proven as two real processes), the Opener, bundles and owner group, the units
seam over D-Bus (godbus, n2f-D4), the listener and firewall probes (`hostnet`, n2f-D11), the
`health` and `element` verbs, and `main` building the real `System` (`admin/wire`). Not
covered: the play, units and tmpfiles (gate A); C 3.12's backups (n2f-D6); C 3.9.2's LXC
probe (n2f-D7). Owner residual: n2f-D12 (Decisions, above).

## Task 2.4: seats, declarations, Element profiles, health, backups and restore

Slice n2b (`SPEC-n2b.md` DR1-DR8, HV1-HV3 and GD1-GD3) covers the declarations of C 3.8: the
strict JSON reader and value rules, the one-team input and canonical `team.json`, the host_vars
YAML reader (`declare/hostvars`, `go.yaml.in/yaml/v3` vendored), and the go-deps `keepout` row
that keeps YAML out of `teamchat-admin` (DC17). An Element entry takes `team` alone, so a
`base_url` is refused (the N0 re-check's finding 5). Not covered: the host_vars writer (HV4,
ccy-teams'), `declare check`'s v1 rules, Element profiles, health, status records, backups
and restore.

Slice n2c (`feat/bus-v2-n2c`, `SPEC-n2c.md` SN1 to SP4, n2c-D1 to D13, report
`subagent-reports/261022-n2c.md`) covers the owner's side of C 3.7 in `internal/teamchat/seat`:

- the seat store's names in `paths`, and the records (`seat.json`, `held.json`, `released.json`,
  `notice.json`, the issued bundle);
- the directory-fd writes and the allocation lock;
- the credential seam with the (c') password;
- take (C 3.7.3) and release, with the disconnected notice's inputs and `Announce`;
- placement, and the guest placement with the kept guest token.

Tests run on fakehs and, under `-tags tuwunel`, against the pinned Tuwunel 1.9.4.
Not covered:

- the root side of `member add|return|reissue`;
- the hot reissue, seat choice and handles (3.7.4), `desired.json` and hot control;
- the quarantine prune;
- C 3.10, 3.11 and 3.12 (n2b covers C 3.8).

The last review round's open low (a session seat directory that two holdings of one session
name) is fixed by the n2c-D12 addendum in `SPEC-n2c.md`: a take refuses a session whose
`<team>` seat directory is already there, and a release leaves a directory that a newer
holding of the team owns (`subagent-reports/261022-n2-qafix.md`).

Slice n2e (`feat/bus-v2-n2e`, `SPEC-n2e.md`, report `subagent-reports/261022-n2e.md`) covers, as
inert code: the status records and the team lock (TL1); the health tick of C 3.11 with the
view (`health`, `health/view`), the disconnected notice (`notice`), and NT3's bound and
`Holdings` in `seat`; the owner's seat sweep (`sweep`, DC21, INT7); and Element profiles of
C 3.10 (`element`). Not covered: the child and announcer adapters (DC17; the probe adapter and
the D-Bus side are n3b's, n3b-D4 and `host/lxc/guestbus`); the units and timers; backups and
restore (C 3.12).

Real-server evidence: every `tuwunel`-tagged test was run with `-race -tags tuwunel -v` in the
container against the pinned Tuwunel 1.9.4 binary, and none skipped. That covers `matrix`
(`TestTuwunel`), `matrix/tuwunelhs`, `rooms` (`TestTuwunelConverge`), `apply`
(`TestTuwunelApply`), `seat` (`TestTuwunelTakeAndRelease`) and `health/view`
(`TestTuwunelSeatWatch`), and each passed (`subagent-reports/261022-n2-qafix.md`).
The team lock is one package, `teamchat/teamlock` (TL1), used by host, admin and health
(`subagent-reports/261022-n2-lock.md`).

Slice n2g (`feat/bus-v2-n2g`, `SPEC-n2g.md`, report `subagent-reports/261022-n2g.md`) covers
C 3.12 as inert code: backups (`backup`: BT, BK, BV, HB1), the restore engine (`restore`:
RG, RM, RS, RO, RE, the renames), the child's restore ops and the secure unit's child
(`admin`: RC, RU), the `backup`, `backups` and `restore` verbs, and their wiring (`admin/wire`).
RT1-RT4 pass against the pinned Tuwunel 1.9.4 (`-tags tuwunel`). An Opus review went
FIX-FIRST, then MERGE in round 2 (`261022-n2g-review.md`, `-review-2.md`). `backup --all`
goes on past a team that is down. Not done: `restore` stays refused in production
(`restore.ErrUnproven`, n2g-D8, until H-C5 and H-C15); the units, timers and install (gate A,
Task 2.5); the review's nits 5-11.

### The six N2 packages and their callers

**Six N2 packages had no caller; slice n2f gives four of them one** (by import:
`go list -deps ./cmd/teamchat-admin`). PR 124 then took the whole stack to F44, the last
two still without a caller.

- `health`: called. `teamchat-admin health` (`admin/wiring.go`, `admin/wire`); its run by
  `teamchat-health.{service,timer}` (C 3.11) is play work under gate A (Task 2.5).
- `health/view`: called, by the child's `team.view` op (`admin/serveview.go`).
- `notice`: called, by the child's `member.disconnected` op (`admin/serveview.go`). ccy-teams'
  `seats sweep` is its second caller, still to come.
- `element`: called. `teamchat-admin element check|apply` (`admin/wiring.go`); the optional
  Element play that runs it is gate A.
- `declare/hostvars`: still waits, on ccy-teams, its only importer, when its flows read and
  write the v2 declarations in host_vars (HV4's writer; Task 4.3).
- `sweep`: still waits, on ccy-teams' `seats sweep` (Task 4.3), run by the owner's
  `teamchat-seats.{service,timer}` (C 3.7.5; user units under gate A, Task 2.5).
