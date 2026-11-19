# Audit: cross-cutting (dead code, tests, docs against code, launch, v1 dependence)

**Scope.** All of `src-go/internal/teamchat` (74 packages, 84,154 production lines and 125,326
test lines), `src-go/cmd/teamchat`, `src-go/cmd/teamchat-admin`, the ccy launch path
(`src-go/internal/ccystart`, `src-go/internal/teams/teamchatlaunch`, the launcher's bash
libraries), `playbooks/imports/play-teamchat.yml`, Plan 00209 `PLAN.md`, Plan 00246 `PLAN.md` and
`SPEC-cross-network.md`, and `docs/agent-bus*.md`. Abbreviations: `tc/` is
`src-go/internal/teamchat/`, `P209` and `P246` are the two plans' `PLAN.md`, `XN` is
`docs/agent-bus-cross-network.md`, `SPEC` is Plan 00246's `SPEC-cross-network.md`.

**Tools run.**

- `deadcode` (golang.org/x/tools v0.52.0, built from the module cache, no download) over
  `./cmd/...`: 104 unreachable functions under `tc/`. With `-test` it reports 0.
- `go list -deps ./cmd/...` against `go list ./internal/teamchat/...`.
- `go vet -tags tuwunel ./internal/teamchat/...`: clean.
- `go test -tags tuwunel -run Tuwunel` on `member/wiring` and `matrix`: every test SKIP.
- `go test -tags teamchatrelease -run TestNoVerbIsUnbuilt ./internal/teamchat/member/cli/`: FAIL.
- `go test` (default tags) on `member/wiring`, `member/cli`, `declare/...`, `clock`,
  `failpoint`, `member/kit`, `installplay`: all pass; `member/wiring` took 242 s.

Nothing was run under systemd, on a host or against a real Tuwunel. Counts: 5 HIGH, 5 MEDIUM,
4 LOW.

## Findings

### XCUT-1 (HIGH) The shipped skill and the coordinator's hook text tell agents to run three verbs that are not built

- `tc/member/cli/run.go:58-60` lists `ask`, `thread` and `questions` as unbuilt; `:101-102`
  answers them with an internal error ("is not built in this binary; it lands with slice n3f").
- `tc/member/hooktext/hooktext.go:36` (used at `:178`) tells every coordinator at session start:
  "ask the humans with teamchat ask ..., and follow up with teamchat questions".
- `tc/member/kit/skill.txt:63-65` and `:89-91` document the same three verbs as working.
- The guard exists and is red: `tc/member/cli/release_test.go:9-13` fails today on exactly these
  three. No gate runs it (`src-go/CLAUDE.md:357-360` says so; `play-teamchat.yml:370` builds
  with no test step and no tag).
- **Failure.** A v2 coordinator follows its own start-of-session instruction, runs
  `teamchat ask`, and gets an internal error. A coordinator has no built way to put a numbered
  question to a human.
- **Fix.** Either build n3f-c2b to n3f-e, or remove the three verbs from the hook text and the
  skill until they exist. Put the release test in a go-qa stage so an unbuilt verb cannot ship.

### XCUT-2 (HIGH) The "site must not be a hostname" refusal (X26) has no production caller

- `tc/declare/sitecheck.go:34` (`Declaration.CheckSites`) and `tc/declare/machinehost.go:9`
  (`MachineHostname`) are reached only from `tc/declare/site_test.go` and `machinehost_test.go`.
  `deadcode` reports both unreachable; a text search of `src-go` finds no other caller.
- The play passes `teamchat_site` straight into the declaration
  (`playbooks/imports/play-teamchat.yml:123`) with no check of its own.
- Docs say it is enforced: `XN:145-147` ("is refused"), `SPEC:225-227` (X26), `P246:124`
  ("S10 are built").
- **Failure.** An owner sets `teamchat_site` to the machine's real hostname. Nothing refuses it,
  the hostname becomes part of every handle on that machine, and handles are permanent (X28) and
  appear in public forge text. This is the leak the public-repository rule exists to stop.
- **Fix.** Call `CheckSites(declare.MachineHostname)` in `team apply` and `lxc apply`, where the
  declaration is read, with a test that drives the verb, not the function. The likely intended
  home was `declare check`, which is unbuilt (XCUT-7).

### XCUT-3 (HIGH) The health unit's own IP filter blocks its terminator probe on every non-loopback address

- `tc/unitfiles/unitfiles.go:157-160`: `teamchat-health.service` has `IPAddressDeny=any` and
  `IPAddressAllow=localhost`, with a comment that a drop-in "may add" the listen addresses.
- No code or play writes that drop-in (searched `tc/` and `play-teamchat.yml` for a health
  drop-in path: none). `P246:183-186` (Task 8.10, open) confirms it is not rendered.
- The tick probes each listen address through the terminator: `tc/health/reach.go:44`, wired in
  production at `tc/admin/wire/wire.go:116`.
- **Failure.** For any team that declares a non-loopback network (every cross-network team and
  every LXC team, whose url is the bridge address), each timer tick records "the TLS terminator
  does not answer on <address>" although it is serving. `teamchat-admin health` run by hand as
  root is outside the unit's filter and disagrees. A permanent false alarm hides a real outage.
- **Confidence.** Verified by reading the unit text and the wiring; not run under systemd.
- **Fix.** Have `team apply` render the health unit's drop-in from the applied listen addresses
  (Task 8.10), and add a `madeUnits`-style test that every probed address is in the allow list.

### XCUT-4 (HIGH) No test against a real homeserver ever runs

- 12 `tuwunel_test.go` files plus `tc/member/wiring/e2e_tuwunel_test.go` (5 end-to-end tests)
  carry `//go:build tuwunel`. No go-qa stage, script or workflow runs tests with that tag:
  `src-go/qa/go-qa.toml:87-93` only runs the project rules over that build, and
  `src-go/CLAUDE.md:361-366` says the tests are "checked by hand".
- When the tag is given they still do not run: `tc/matrix/tuwunelhs/tuwunelhs.go:35-36` skips
  when `FAKEHS_TUWUNEL_BIN` is unset. Confirmed here: every test reports SKIP and the package
  reports `ok`. No Tuwunel binary is in the ccy image or CI (searched the Dockerfile and
  workflows). This is the skip-if-absent pattern the project's fail-fast rule forbids.
- `P209:263-264` (Task 2.5, not started) still lists "the M44 real-Tuwunel test, and Tuwunel's
  answers for a locked and a deactivated account recorded into fakehs" as to do.
- **Failure.** Every protocol guarantee (sync resume, transaction-id idempotency, presence
  timers, 429 handling, restore) is proven only against `fakehs`, a 2,980-line model written by
  the same authors. A Tuwunel upgrade, or a place where the model differs, breaks members with
  all gates green.
- **Fix.** Install the pinned Tuwunel in the ccy image and CI through the playbooks, make a
  missing binary a failure under the tag, and add a `teamchat-tuwunel-test` stage to go-qa.

### XCUT-5 (HIGH) There is no in-repository end-to-end proof of a real member exchanging messages

- The strongest default-tag proof is `tc/member/wiring/e2e_*_test.go`: the real `teamchat`
  binary (the test binary re-run), a fake `claude`, and `fakehs` over loopback HTTP
  (`tc/member/wiring/e2e_test.go:15`).
- The VM-lab scenarios the plans name (`teamchat-coexist`, `teamchat-pilot`,
  `teamchat-migrate`) exist only as text in Plan 00209; no script or manifest row implements
  them.
- X30 (the second-machine run) has not run: Plan 00246 journal
  `JOURNAL/2026-11-13/153302Z-finding-246b34.md` records the triage failing and both entries
  taken off `meta-deploy.list`.
- The only real exchange on record is "seen outside this repository" (`P209:416-420`).
- Tasks 2.5, 3.4 and 4.4 (all acceptance) are not started (`P209:263`, `:379`, `:410`).
- **Fix.** One scripted lab scenario that installs the play, adds one LXC member and one ccy
  seat, sends in both directions and reads history, run before any further feature work.

### XCUT-6 (MEDIUM) Homeserver suspend and resume is untested end to end

- Member-side freeze is well covered on the fake clock (`Suspend`/`StepWall` in 13 test files,
  for example `tc/member/teamsync/resume_test.go`).
- The owner's case, the server frozen while members stay awake, has no test that names it.
  `fakehs` offers `Stop`, `Restart` and `Gate` (`tc/matrix/fakehs/http.go:106`, `:128`,
  `gate.go:24`), which model a refused or slow request, not a server that holds every open
  connection silent for hours and then answers them all at once with its wall clock jumped.
  The only `SIGSTOP` in the tree freezes the member's link, not the server
  (`tc/member/wiring/e2e_claude_test.go:247-290`).
- **Fix.** A `tuwunel`-tagged test that `SIGSTOP`s the real server during an open `/sync` and an
  in-flight send, advances past the request deadline, `SIGCONT`s, and asserts one delivery and
  no duplicate. Add a `fakehs` freeze mode (accept, hold all, release all) for the default build.

### XCUT-7 (MEDIUM) Docs against code: what is built (W9, confirmed and extended)

The code decides. The launcher, the kit and the play exist; Plan 00209 is right and Plan 00246
and the cross-network doc are stale.

| Claim                                                            | Where                                    | Code says                                                                                                             |
| ---------------------------------------------------------------- | ---------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| "the host play ... and the v2 launcher are ... not built"        | `XN:9-13`                                | False. `play-teamchat.yml` (751 lines), `cmd/ccy-start-claude`, `teams/teamchatlaunch/verbs.go:85-92`                 |
| "no command takes a pool seat into a session yet"                | `XN:141-142`, `P246:164-165`             | False. `ccy-teams internal seat-take` (`teamchatlaunch/verbs.go:90`, `:239`), called by `lib/agent-bus-seats.bash:76` |
| "Stages B and C ... are not built"                               | `P246:216-218`                           | False. Stage B is `tasks/teamchat-ccy-kit.yml`; stage C as above. `P209:390-403` marks both done                      |
| "once the play and the launcher exist"                           | `XN:210`, `P246:143-145`                 | Both exist                                                                                                            |
| "Not built: a health probe ... through the terminator"           | `XN:211-212`, `P246:130`                 | Built: `tc/health/reach.go:44`. `P246:134` says so in the same paragraph. It is broken in the unit (XCUT-3)           |
| X26: a hostname site "is refused"                                | `XN:145-147`, `SPEC:225-227`, `P246:124` | Not enforced (XCUT-2)                                                                                                 |
| `declare check` is "today a row of the verb table with no code"  | `P246:183`                               | True (`tc/admin/run.go:172`, nil run); also `declare.Check` is dead (`tc/declare/gate.go:219`)                        |
| "`human lock` and `human unlock` are not built yet"              | `XN:161`                                 | True                                                                                                                  |
| Task 3.2: release test "fails on ask, thread and questions only" | `P209:319-320`                           | True, verified by running it                                                                                          |
| Success criterion: "no Python started by v2 on the host"         | `P209:518`                               | Not met for humans: Element and Lightning trust goes through v1's play and Python (XCUT-9)                            |
| "Stage A ... has never run"                                      | `P246:215-217`                           | Still true in this repository; nothing imports the play (XCUT-8)                                                      |

Also: 19 rows of `teamchat-admin`'s verb table print "is not built yet" (`tc/admin/run.go:87-88`;
built rows are `tc/admin/verbs.go:104-120`): `migrate` (12 verbs), `human lock`, `human unlock`,
`redact`, `check`, `footprint`, `declare check`, `status`. No user document lists them. `restore`
is built and refused in production (`tc/admin/wire/wire.go:157-160`). There is no user guide for
v2 at all: `docs/agent-bus.md` and `docs/agent-bus-protocol.md` never mention `teamchat`.

**Fix.** Rewrite `XN`'s status paragraph and section "What is proven", and `P246`'s "Install
path" paragraph, from the table above. Keep one "built / wired / run on a host" table in one
file and point the others at it.

### XCUT-8 (MEDIUM) How a v2 member is launched today, and what is missing

What the code supports:

1. **Host side.** `play-teamchat.yml` builds and installs `teamchat-admin`, the units and the
   teams. No playbook imports it and `meta-deploy.list` has no entry for it (searched
   `playbooks/*.yml`, `playbooks/imports/*.yml`, `CLAUDE/Plan/meta-deploy.list`); it is run by
   name against an inventory host (`P246:179-180`, issue 227 item 5).
2. **ccy session (built).** `ccy` with a v2 team: `lib/agent-bus-membership.bash:67`
   (`ccy-teams internal launch-args`) and `lib/agent-bus-seats.bash:41`, `:76` (`seat-check`,
   `seat-take`), then `entrypoint.sh:571` execs `ccy-start-claude`, which runs
   `teamchat run --claude --kind ccy --desired /run/teamchat/membership -- claude`
   (`src-go/internal/ccystart/ccystart.go:12-17`, `:204`). The pool seat must first be issued
   by `teamchat-admin member add` and placed by `teamchat seat place-issued`, by hand (W6).
3. **LXC member (built).** `teamchat-admin lxc apply` from the play writes one systemd unit per
   seat that starts `tmux ... teamchat run --claude --kind lxc --key ... --resume-recorded -- claude`
   (`tc/host/lxc/unit/unit.go:63-67`, called at `tc/host/lxc/apply.go:1286`). Same host as the
   homeserver only (W3).
4. **Script (built).** `teamchat run --plain --kind host --state DIR --seat T/S -- teamchat say ...`
   (`XN:134-135`).
5. **Host `cc` session.** Refused; Plan 00209 Task 5.1, not started.
6. **A session on a v1 team and a v2 team at once.** Refused (exit 64); Task 4.5, not started.

Not built around the launch: the ccy-teams board and team creation for v2 (Task 4.3), so a v2
team is declared by editing host_vars and running the play by name.

### XCUT-9 (MEDIUM) Is v1 still needed by anything v2 depends on

- **Go code: no.** `go list -deps ./cmd/teamchat ./cmd/teamchat-admin` contains no
  `internal/bus`, `internal/teams`, `internal/lightning` or `internal/matrixclients` package.
  `tc/v1names` is 17 lines: one `InaccessiblePaths` unit line that hides v1's state from v2's
  units (`tc/unitfiles/unitfiles.go:161`, `tc/host/sandbox.go:55`).
- **Launch: shared, not dependent.** `ccy-teams` serves both systems; the launcher libraries
  that carry a v2 launch are still named `agent-bus-*.bash`. `P209:426-428` records that a list
  with no v1 team no longer needs the `agent-bus` command.
- **Humans: yes, v2 depends on v1 here.** A person reads a v2 `https` team in Element or
  Lightning only after v1's play `playbooks/imports/optional/common/play-agent-bus-element.yml`
  places the team CA, driven by v1's variables (`agent_bus_element_teams`,
  `agent_bus_element_ca_store`) and v1's Python (`helpers/agent_bus/element.py`,
  `helpers/agent_bus/declare.py element-remote`); see `XN:166-199`.
- **Consequence.** v1 cannot be removed (Plan 00209 Task 6.5) until the Element and Lightning
  CA placement has a v2 home. That is not listed in D 7.2's removal steps as far as `P209`
  shows.

### XCUT-10 (MEDIUM) 104 functions and seven packages are reachable only from tests, and the gate cannot see it

- The go-qa dead-code check passes because test callers count: `deadcode -test` reports 0,
  `deadcode ./cmd/...` reports 104 under `tc/`.
- Dead in production, with behaviour a reader would assume is live:
  - `tc/declare/sitecheck.go:34`, `machinehost.go:9` (XCUT-2); `tc/declare/gate.go:219` `Check`.
  - `tc/proto/verify.go:116` `CheckPowerLevels`; `tc/proto/limits.go:193` `NewSendGate`,
    `:293` `RetryWait`; `tc/proto/redacted.go:8` `RedactedSlot`; `tc/proto/roster.go:187`
    `DecodeForge`; `tc/proto/escape.go:118` `EscapeJSON`; `tc/proto/ids.go:87`, `:99`, `:151`.
  - `tc/member/records/records.go:283`, `:445`, `:589` (`Question.MarshalJSON`, `TeamFailedOf`,
    `WriteTeams`): the questions records of the unbuilt verbs.
  - `tc/seat/guest.go:13` `KeepGuestToken`; `tc/member/engine/statuspace.go:49` `flushStatus`
    (a status change owed at exit is never flushed); `tc/member/link/link.go:526` `heldToken`.
  - `tc/health/view/view.go:53` `Load`; `tc/host/render.go:112` `DropIns`;
    `tc/host/lxc/guestroot/root.go:221-229` `OpenDir`, `OpenDirOwning`;
    `tc/member/placeissued/read.go:43` `Read`; `tc/teamlock/teamlock.go:62` `SystemClock`.
  - `tc/paths/paths.go:166`, `:169`, `:179`, `:220`, `:278`, `:326`, `:348`, `:659`, `:778` and
    `tc/paths/restore.go:43`: names for the unbuilt migration fence and set records.
  - `tc/member/kit/kit.go:54-266`: the whole assembler (268 lines). The kit that ships is the
    tracked files under `files/opt/claude-yolo/optional/teamchat/`, copied by
    `tasks/teamchat-ccy-kit.yml`; `kit.Assemble` is only a test oracle for them.
- **Fix.** Add a go-qa stage that runs `deadcode ./cmd/...` without `-test` over `tc/` against a
  ratchet list, then delete or wire each entry.

### XCUT-11 (LOW) Test doubles and test helpers are counted and built as production packages

Linked into no binary (`go list -deps ./cmd/...`): `tc/matrix/fakehs` (2,980 lines),
`tc/reexec` (467), `tc/member/fakeinbox` (395), `tc/member/faketranscript` (262),
`tc/declare/hostvars` (209), `tc/testfiles` (124), `tc/installplay` (8); plus
`tc/clock/fake.go` (256) inside a production package and `tc/matrix/tuwunelhs` (336, tagged).
About 5,000 of the "84k production lines" in W8 are test support. `tc/declare/hostvars` is a
different case: its package doc says "ccy-teams alone is to import it" and nothing does, so the
YAML reader of v2 declarations is unused code, not a test double.

### XCUT-12 (LOW) Test-only method in a production file

`tc/units/units.go:236` `Manager.lostForTest` exists only for a test. Move it to an
`export_test.go`.

### XCUT-13 (LOW) Duplicated helpers

- 429 wait, three copies: `tc/proto/limits.go:293` (dead), `tc/matrix/client.go:526`,
  `tc/member/forge/forge.go:675`.
- 14 separate `Clock` interfaces (`tc/clock/clock.go:23` and one each in `backup/meta.go:54`,
  `health/health.go:36`, `host/apply.go:40`, `host/lxc/lxc.go:173`, `matrix/client.go:75`,
  `matrix/transport.go:37`, `member/guest/places.go:52`, `member/lock/lock.go:59`,
  `pki/pki.go:85`, `restore/deps.go:61`, `seat/seat.go:21`, `teamlock/teamlock.go:56`,
  `tlsterm/tlsterm.go:56`). Each needs its own fake, and each is a separate place to get
  wall-against-boot time wrong across a suspend.
- Atomic replace (temp file, rename) written in ten files: `carry/export.go`, `carry/stage.go`,
  `ctl/restore.go:157`, `host/lxc/guestroot/sys.go`, `member/guest/places.go:228`,
  `member/homefs/homefs.go`, `restore/fs.go`, `restore/renames.go`, `seat/fs.go:145`,
  `state/state.go`. Directory fsync in four: `host/dirs.go:103`, `state/state.go:70`,
  `carry/export.go:710`, `carry/stage.go:130`. Whether each copy fsyncs the file and the
  directory is for the per-area audits; one shared helper would make it one answer.

### XCUT-14 (LOW) The end-to-end package is slow

`tc/member/wiring` took 242 s here in the default build. The failpoint stage runs the tree
again with `-race -shuffle=on -count=2` (`src-go/qa/go-qa.toml:78`), so this one package costs
several minutes per gate run, most of it real-time waits.

## Unconfirmed

- **How go-qa's own dead-code check is invoked** (with tests or over which roots) was inferred
  from the two `deadcode` results, not read from go-qa's source.
- **XCUT-3 under a real systemd.** The filter's effect on a probe to the host's own non-loopback
  address was reasoned from systemd's documented `IPAddressDeny=` behaviour, not observed.
- **`flushStatus` never called** (`tc/member/engine/statuspace.go:49`): confirmed dead; whether a
  status change in the last second before exit is lost in a way a user sees was not traced.
- **`CheckPowerLevels` dead** (`tc/proto/verify.go:116`): the invariants themselves still run
  through `powerLevelsBroken` at `tc/proto/verify.go:96`, so trust checking is believed intact;
  only the exported wrapper is dead.
- **Element path for a v2 team** (XCUT-9) was read from `XN` and file names, not run.

## Bloat and debloat candidates

Ordered by lines saved against risk.

1. **`tc/restore` (2,600 lines)** is an engine production refuses to build
   (`tc/admin/wire/wire.go:157-160`). `tc/carry` (4,323 lines) is wired (`team seal`, `export`,
   `import`, `carry-check`), but the host move it exists for cannot finish while restore is
   refused (`P246:165-168`). Run H-C5 and H-C15 and enable restore, or park it behind a build
   tag until then.
2. **The migration surface**: 12 `migrate` verb rows with no code, the fence and set names in
   `tc/paths`, the "not built" refusals in `tc/carry/seal.go:198`, `tc/restore/validate.go:89`,
   `tc/admin/wire/lxc.go:160`. Plan 00209 Task 6.4 ("close the door on v1" without migration)
   makes most of it unlikely to be built. Delete the rows and names until a migration is planned.
3. **`tc/member/kit/kit.go`** (268 lines): replace the assembler-as-oracle with a golden-file
   test of the tracked kit, or make the play call it and drop the tracked copies. Not both.
4. **`tc/declare/hostvars`** (209 lines, 199 test): delete, or wire it into ccy-teams with Task
   4.3.
5. **The questions index** for `ask`, `thread`, `questions`: built in `tc/member/engine`,
   `tc/member/records` and `tc/member/teamsync`, with no verb that reaches it. Finish n3f or
   park it; it is carried, tested and linted on every run today.
6. **One `clock.Clock`** in place of 14 interfaces, and **one atomic-write helper** in place of
   ten (XCUT-13).
7. **Move the fakes under one `internal/teamchat/testkit/` tree** so production line counts,
   coverage and dead-code checks stop treating them as product (XCUT-11).
8. **`tc/proto` dead exports** (XCUT-10): about ten functions, deletable today.
