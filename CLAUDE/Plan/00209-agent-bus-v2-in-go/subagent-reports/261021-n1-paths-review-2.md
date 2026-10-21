Verdict: FIX-FIRST

# Plan 00209 N1 Tasks 1.1 and 1.2: adversarial review round 2 of feat/bus-v2-n1-paths (db3c8350..HEAD)

What I ran: `go test -race -cover` on internal/teamchat/... (paths 100%), qa/teamchatseparate
(100%), qa/cmd/qa-teamchat-separate (77.3%, the same as its sibling cmd packages, and the floor
is per module), qa/changed and qa/all. All pass. `golangci-lint run` reports 0 issues in both
modules. `./scripts/qa-teamchat-separate.bash` exits 0 over 5 files, 14 names and 0 rows. I
made two temporary probe tests (`zz_probe_test.go`, one in paths and one in teamchatseparate)
and deleted both; `git status --short` is clean afterwards.

## Findings

01. **major**: `src-go/internal/teamchat/paths/ids.go:18-48` (all the id types), and the package
    doc `paths.go:140-142`. The guarantee that "only ParseTeamID makes one" does not hold for
    decoded values. `TeamID`, `RunID`, `CheckoutKey`, `GuestUser`, `SetID`, `SessionName`,
    `Digest`, `Kit` and the rest are plain named string types. Any package can write
    `paths.TeamID("ctl")`, and `encoding/json` (or a YAML decoder) fills one with no check at
    all. A probe showed it: `json.Unmarshal({"teams":["ctl","../../etc"],"run":"../x"})` into
    `[]TeamID` and `RunID` gives `err=nil`, `HomeserverUser = "teamchat-ctl"` (CtlUser),
    `AdminState = "/var/lib/teamchat-admin/../../etc"`, and
    `KeeperJSON = "/tmp/teamchat/../x/keeper.json"`. Decoding is how team ids will usually
    arrive: desired.json, team.json, seat.json, the bundle, and Ansible-parsed host_vars. Once
    a v2 struct gives a field the type `paths.TeamID`, round 1's blocker (team `ctl`) is back,
    and path traversal comes with it, while the type tells the reader the value was checked.
    No consumer exists yet, so this is the cheapest moment to change the kind of type.
    **Fix**:

    - Make each id an opaque struct, e.g. `type TeamID struct{ s string }`, built only by its
      Parse function. That rules out conversion.
    - Give each id a `MarshalText` method and an `UnmarshalText` method (pointer receiver)
      that runs its Parse, so a decode fails on a bad value.
    - Keep `String()`.
    - Add tests: `json.Unmarshal` of `"ctl"`, `"../x"`, `""` and `"Ops"` into `TeamID` fails
      wrapping `ErrInvalid`; a round trip of a valid id is lossless; the same for `RunID` and
      `CheckoutKey`.
    - Make `Kit` opaque as well, or document that it is built only by `HostKitDir` and the two
      constants (a struct cannot be a `const`, so those become functions or vars).

02. **minor**: `src-go/qa/teamchatseparate/scope.go:65` (`reference`). K7 misses a path built by
    string concatenation, a shape this repository already uses
    (`playbooks/imports/optional/common/play-vm-test-lab.yml:188`
    `root_dir ~ '/files/home/...'`, `play-speech-to-text.yml:787`
    `root_dir + '/files/home/...'`). A probe returned `[]` for both shapes. A v2 play written
    that way names an out-of-scope file and the gate does not see it. **Fix**: add a quote
    followed by `/` to the prefix alternation:
    `(?:^|[\s"'=:,\[(]|\./|\}\}/|['"]/)((?:tasks|files)/...)`. Add both repository lines as
    cases in `TestReferencesAreTheTasksAndFilesPathsALineNames`, and a K7 red case in
    run_test.go.

03. **minor**: `scope.go:69-76` with `run.go:322-326`. A reference is judged before it is
    normalised. `tasks/teamchat/../render.yml` comes out of References as is, and
    `InScope(...)` is true, so the reference passes K7 while the file it names is out of
    scope and unread. Shared files have the same problem: `tasks/x/../build-go-command.yml`
    misses SharedFiles and is flagged, which is the safe direction. **Fix**: in `judgeFile`,
    apply `path.Clean` to each ref before both lookups. A cleaned ref that starts with `../`
    or no longer starts with `tasks/` or `files/` is a finding. Add a test for the traversal
    case.

04. **minor**: `scope.go:24,27-55,65`. `vars/` is not scanned. A v2 play that loads
    `vars_files: "{{ root_dir }}/vars/teamchat.yml"` (vars/ is where seven shared variable
    files live today) names a file the gate never reads, and K7 does not look at `vars/`
    paths. This is the same kind of hole K7 was built to close for `tasks/`. **Fix**:

    - Add `vars` to `ScanPaths`.
    - Bring `vars/` paths below a `teamchat*` component into `InScope` (the same rule as
      `files/` and `tasks/`).
    - Widen `reference` to `(?:tasks|files|vars)/`.
    - Add the routing glob `vars/*teamchat*`.
    - If v2 reads `vars/version-pins.yml`, put it in `SharedFiles` with its reason.
    - Note the wider scope in D 4.1.

05. **minor**: `scope.go:52`, which `scope_test.go:47` pins. A `play-teamchat*.yaml` is out of
    scope without any warning, so a v1 name in such a play passes. **Fix**: accept `.yaml` as
    well as `.yml`, both in `InScope` and in the routing glob, and move the case from `out`
    to `in`.

06. **minor**: `ids.go:116` (`ParseDigest` uses `segmentGrammar`) with `paths.go:219,300`.
    `ParseDigest("current")` is accepted, and `HostKitDir(current) == HostKitCurrent`, so a
    kit "digest" can be the link itself. A probe confirmed both. **Fix**: give the digest a
    grammar of its own, lowercase hex of the length C33 sets (e.g. `^[0-9a-f]{16,64}$`).
    Add a test that `HostKitDir(d)` is never `HostKitCurrent` and that `"current"` is
    refused.

07. **minor**: `separation_test.go:448-450` (`perTeamRoots`). The list misses several
    directories whose children are named by team id: `Layout.State` in both layouts
    (`Seat(t)`), `Store.Session(n)` (C 3.7 `sessions/<s>/<team>`), `Store.PoolCheckout(k)`
    (`pool/<k>/<team>`), and the host's `/run/teamchat` (`ServerRuntimeDirectory`). A fixed
    child added later under one of these (say `State + "/lock"`) would pass. **Fix**: derive
    the roots instead of listing them by hand. For every team-derived sample value of team
    `ops`, the prefix before `/ops` is a per-team root. Then run `teamLikeChild` against that
    derived set.

08. **minor**: decision (g). The decision is sound and the copy matches. The real lines on
    `feat/bus-v2-n1-matrix` are 26 (`tests/helpers/pingbus/fixtures/tuwunel`), 28
    (`probe.agent-bus.internal`) and 205 (`"agent_bus.status"`). Each literal covers its hit,
    and no other v1 name is anywhere in that file (case-insensitive grep). The red cases are
    real. However, `feat/bus-v2-n1`, the integration target, already contains that file.
    Merging this branch into it as it stands turns the gate red (3 findings) unless the merge
    commit adds the rows, and the only record of that step is commit message 516cf722.
    **Fix**: write the step where the merger will read it, in the PLAN.md Task 1.2 note or the
    final report: "the merge into feat/bus-v2-n1 adds the three `replayRows` rows to
    allowed.tsv; the merge is gated on qa-teamchat-separate exit 0". Better: merge
    `feat/bus-v2-n1` into this branch and land the rows here, proven against the real file.

09. **minor**: `scripts/qa-teamchat-separate.bash:6-9,15`. The wrapper's header no longer
    matches the code. It says `playbooks/imports/play-teamchat*.yml`, but the gate now covers
    any play in a subdirectory. It says only "a row that allows nothing fails the gate", and
    its exit-1 line leaves out a row covering more than one line and a K7 out-of-scope
    reference. **Fix**: copy the scope and exit-1 wording from the CLAUDE/QA.md row, which is
    current.

10. **minor**: the design text still says what the code no longer does. All of these are
    promised for the final report only:

    - C 3.9 (`DESIGN-v2-C-host.md:1736`) still gives `/var/lib/teamchat-admin/lxc/`.
    - A 2.2 reserves no team id.
    - D 4.1 (`DESIGN-v2-D-migration.md:383-386`) lists no `/ccy/membership`.
    - D 4.1:379 scopes plays to `playbooks/imports/play-teamchat*.yml` with no subdirectory.
    - D 3.7:273 reads naturally as putting `play-teamchat-element.yml` beside `play-teamchat.yml`.

    On decision (c) itself: putting the Element play in optional/common, as v1 does, is
    reasonable, but D does imply a directory, so it has to be amended.

    One hazard also needs writing into A 2.2. `TestReservedTeamIDsAreExactlyTheFixedNamesTails`
    forces a new fixed `teamchat-<x>` name to reserve `<x>`, and `<x>` may already be a live
    team's id, which cannot change. A 2.2 or C's validator should say: no new fixed name may
    take a declared team id.

    **Fix**: amend those sections, or list each one with its line in the final report
    `261021-n1-paths.md` before MERGE. Add the reservation rule to A 2.2 and D 3.5.

## Checked and found sound

- Round 1: findings 01-07 and 09-12 are fixed in code as the handoff describes. Finding 08 is
  deferred to the final report, which is acceptable. 01: `reservedTeamIDs` is enforced, and a
  test derives it from the fixed names and holds it equal; I traced every fixed name with a
  `teamchat-` part. 02: `_lxc`, with a per-team-root test that can fail. 03: the names were
  added and the doc narrowed. 04: plays anywhere under `playbooks/imports/`, routing included.
  05: `/ccy/membership`, in the corpus too. 06: K7. 07: one line per row. 09: `Kit` methods
  and `DisplayName`. 10: the root is refused, `XDG_STATE_HOME=/` included. 11: every exported
  const, var, func and method is classified. 12: a shared corpus read by both matchers.
- Decision (a): the tails rule covers every place a team id becomes `teamchat-<t>` (the
  DynamicUser and the Element profile). The `teamchat-session-<u>` names live in other
  namespaces (unit, runtime dir), so no team id can collide with them. Reserving more than
  needed is safe.
- Decision (b): `_lxc` cannot meet a team id, the same reasoning as `_sets`.
  `StatusRoot/lxc/<t>.json` is outside the per-team `teams/` directory.
- Decision (d): `/ccy/membership` catches `${XDG_RUNTIME_DIR}`, `{{ }}` and Join forms, and
  `/ccy/members` stays clean. No v2 or shared file in scope holds it.
- Decision (e): apart from 02-04, the regex finds `{{ root_dir }}/`, `../`, flow lists, and
  templated paths up to `{{` (judged, never skipped). A `.go` file is not read for references,
  and the shared list holds one row with a reason.
- Decision (f): a row is counted per line and refused on a second line. A row whose literal
  matches nothing is stale. A literal must span the hit. One consequence: two identical lines
  can never both be allowed, which is acceptable since a duplicate reference should become a
  constant.
- C 3.7's seat-store internals (`held/`, `released.json`, `.new-<claim>`) are below the seat
  or carry a dot, so none can collide with a team id.
- Wiring: qa-gates row, routing (perfile-gate, shared helpers, deletions), qa-changed
  registration and tests, the addedgates wording, the src-go/CLAUDE.md paragraph and the
  CLAUDE/QA.md row all match the code.
- No other v2 branch holds a v1 name in scope, apart from the three replay lines. Nothing
  install-specific is in the diff: the tests use `/home/u`, `/run/user/1000` and
  `ops.teamchat.internal`.
