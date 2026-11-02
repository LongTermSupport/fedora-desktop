Verdict: FIX-FIRST

# Plan 00209 N1 Tasks 1.1 and 1.2: adversarial review round 3 of feat/bus-v2-n1-paths (db3c8350..HEAD)

## What I ran

- `src-go`: `go test -race -cover ./internal/teamchat/...` passes, paths at 100%.
  `golangci-lint run ./internal/teamchat/...` reports 0 issues.
- `src-go/qa`: `go test -race -cover ./teamchatseparate/ ./cmd/qa-teamchat-separate/ ./changed/`
  passes (100%, 77.3% (the module floor, same as round 2), 93.9%).
  `golangci-lint run ./teamchatseparate/... ./cmd/qa-teamchat-separate/...` reports 0 issues.
- `./scripts/qa-teamchat-separate.bash` exits 0 over 5 v2 files, 14 names and 0 allowed references.
- Two temporary probe tests, one in `paths` and one in `teamchatseparate` (`zz_probe_test.go`).
  Both are deleted, and `git status --short` is clean apart from this report.
- `git grep` of every other `feat/bus-v2*` branch's in-scope files for v1 names.

## Round 2 findings, checked in code

- 01 fixed. Every id is a `struct{ s string }`, so no conversion from a string compiles.
  `UnmarshalText` goes through Parse, a failed decode leaves the target unchanged, and
  `MarshalText` refuses the zero value. Tests cover a bad value for each id and a lossless round
  trip. One gap remains, the zero value: see finding 01 below.
- 02 fixed. `['"]/` is in the prefix, and the two repository lines are test cases (`scope_test.go:83-84`).
  A K7 red case is in `run_test.go:314`.
- 03 fixed. `judgeFile` applies `path.Clean` before both lookups. The red cases cover
  `teamchat/../`, a climb out to `playbooks/`, a climb out of the root, and the shared file reached
  through `../`. One edge remains: see finding 04 below.
- 04 fixed. `vars` is in ScanPaths, InScope and the regex, the routing has `vars/*teamchat*`, and
  `vars/version-pins.yml` is a red case.
- 05 fixed. `.yaml` plays are covered in scope, in routing and in tests.
- 06 fixed. A digest is `^[0-9a-f]{16,64}$`, `current` and `..` are refused, and
  `HostKitDir(d) != HostKitCurrent()` is tested.
- 07 fixed. `perTeamRoots` is derived from one team's names in both layouts.
  `TestPerTeamRootsAreDerived` pins the eight expected roots, so the derivation cannot quietly
  find none. `TestThePerTeamRootCheckDiscriminates` can fail.
  - The decision to reserve `membership` (and to hold the reserved list to the fixed-name tails
    plus the per-team roots' fixed children) is sound. The ccy session's `/run/teamchat/{seats,membership}`
    and the host's `/run/teamchat/<team>` are in different mount namespaces, so a real collision
    needs both in one view. Reserving anyway costs nothing.
  - D 5.10 already gives a v1 team whose name is not "free as a v2 id" a short id. A v1 team
    named `membership`, `admin` and so on therefore still migrates. D 3.5:213 should say that a
    reserved id is not free; that edit belongs with the deferred design edits.
- 09 fixed. The wrapper header now matches the CLAUDE/QA.md row on scope and on exit 1.
- 08 and 10 are deferred, and the deferral is sound.
  - The three rows are written out verbatim, with their reasons, in `261021-n1-paths-handoff-2.md:68-81`.
  - `feat/bus-v2-n1` still holds exactly the three lines round 2 named
    (`replay_test.go:26,28,205`), and nothing else in that branch's `teamchat/` tree holds a v1 name.
  - The deferral fails closed. A merge without the rows turns the gate red (3 findings), and a
    row landed here would be stale. It cannot slip through unseen.
  - `feat/bus-v2-n1-proto` holds no v1 name.

## Findings

1. **major**: `src-go/internal/teamchat/paths/ids.go:18-50` and every derivation in `paths.go`
   (`HomeserverUser`, `AdminState`, `ServerUnit`, `StatusTeamDir`, `Layout.Seat`, `Layout.Run`,
   `Store.Session`, `HostKitDir`, ...). The zero value of each id is still an unchecked id. The
   package doc (`paths.go:9-11`) promises "a derived name never holds an unchecked string", and
   the zero value breaks that promise.

   Any package can write `var t paths.TeamID`. More importantly, `encoding/json` leaves the zero
   value, with `err == nil`, for a field that is missing or `null`, because `UnmarshalText` is
   never called. Decoding is how ids will arrive (desired.json, team.json, seat.json, the
   bundle).

   A probe decoded `{}` and `{"team":null,"run":null,"s":null}`. Both gave `err=nil`, and the
   derived names were:

   - `AdminState = "/var/lib/teamchat-admin/"`, the root of every team's records.
   - `StatusTeamDir = "/var/lib/teamchat-status/teams/"`.
   - `ServerUnit = "teamchat-server@.service"`, which is exactly `ServerUnitTemplate`.
   - `HomeserverUser = "teamchat-"`.
   - `TeamLock = "/run/teamchat-admin/team-.lock"`.
   - `CCYLayout().Seat(zero).Dir = "/run/teamchat/seats/"`, the whole seats mount.
   - `Run(zero).Dir = "/tmp/teamchat/"`, every session's runtime directory.
   - `Store.Session(zero) = ".../seats/sessions/"`, every session's seats.

   `Kit{}` is the same: `Binary() = "/bin/teamchat"`, `Settings() = "/settings.json"`. A leftover
   sweep or a team removal that runs `os.RemoveAll` on any of these deletes every team or
   session, not one. Round 2's finding 01 was major for the same class.

   **Fix**:

   - Give each id type, and `Kit`, an unexported accessor. For example:
     `func (t TeamID) text() string { if t.s == "" { panic("paths: TeamID used before it was parsed") }; return t.s }`.
   - Make every derivation use the accessor instead of `.s`. A zero id is a programming error,
     so it fails loudly at the point of use.
   - Say so in the package doc. A decoded struct's missing field reaches this panic unless the
     decoder requires the field.
   - Add a test that every exported function and method taking an id or a `Kit` panics on the
     zero value. Derive the list from the `exportedIdents` classification so that a new
     derivation cannot skip it.
   - Add a test that `json.Unmarshal("{}")` into an id-bearing struct, followed by a derivation,
     panics.

   If panics are unwanted, the alternative is for every derivation to return `(string, error)`.
   That is a wider API change for the same guarantee.

2. **minor**: `paths.go:363-369, 425-426, 445-446, 490-491` (`Layout`, `Seat`, `Run`, `Store`).
   These are exported structs with exported string fields, so derived names can be built from any
   string. Examples: `paths.Seat{Dir: "/etc"}.Token()`, `paths.Store{Root: "/"}.Pool()` (gives
   `//pool`), and `paths.Layout{}.Seat(ops).Dir` (gives `/ops`).
   The package's checks (`checkBase`, the id grammars) are bypassed. Nothing outside the package
   needs to construct these directly: the tests build them only through `CCYLayout`,
   `HomeLayout`, `Layout.Seat`, `Layout.Run` and `SeatStore`.
   **Fix**: unexport the fields (`dir`, `root`, `state`, ...) and expose read-only accessor
   methods (`Dir()`, `Root()`, `State()`, ...), so that these values come only from the
   constructors. Do it now, while there are no consumers.

3. **minor**: `ids.go:52-53, 147-152`. `Port` is still a plain `uint16`. `paths.Port(0)` compiles,
   and a JSON `0` or a missing field decodes with no check, so `ParsePort` is advisory. Unlike
   the other ids, nothing in `paths` derives a name from a port yet, which is why this is minor.
   **Fix**: make it `struct{ n uint16 }` with `UnmarshalJSON`/`UnmarshalText` through `ParsePort`,
   an `Int()` accessor and the zero-value refusal of finding 01. Or document that `Port` is not
   guarded, and keep it out of every decoded v2 struct.

4. **minor**: `src-go/qa/teamchatseparate/scope.go:73` (`strings.TrimRight(m[1], ".")`). The trim
   runs before `path.Clean`, so a reference ending in `/..` loses its `..`. For example,
   `src: "{{ root_dir }}/files/teamchat/.."` (a copy of the whole `files/` tree) comes out as
   `files/teamchat/`. That is in scope, and the reference passes K7. The probe confirmed it.
   **Fix**: drop a trailing `.` only when it is a single sentence period, that is, when the
   reference does not end in `/.` or `..`. Then apply `path.Clean`. Add the case above to the red
   cases of `TestK7APlayNamingAFileOutsideTheScopeFails`.

5. **minor**: `scope.go:62` (`reference`). The prefix alternation misses a path after a shell
   variable, a backtick or a `;`:

   - `cp "$ROOT/files/x"` and `"${ROOT}/files/x"` in a `shell:` block, both confirmed `[]` by
     the probe;
   - `vars/x.yml` after a backtick;
   - the second path of `files/x;tasks/y`.

   No play or task file in the repository uses these shapes today (git grep over
   `playbooks/imports` and `tasks`), so this is low risk. A v2 shell block is still the obvious
   place for one.
   **Fix**: add `\$\{?\w+\}?/`, a backtick and `;` to the prefix alternation, with one
   `TestReferences...` case each.

## Checked and found sound

- The reserved-id derivation (`TestReservedTeamIDsAreExactlyTheFixedNamesTails`) combines the
  tails with the fixed children. It refuses every reserved id through `ParseTeamID`, and it fails
  if the hand list drifts in either direction.
- The other team-derived names stay apart from each other and from the fixed names: `FenceTable`,
  `ServerName`, `ElementConfigDir`, `ElementLauncher` and `TeamLock`. The `teamchat-session-<u>`
  names are in other namespaces.
- K7:
  - It catches the `{{ root_dir }}/`, `../`, `~ '/`, `+ '/` and folded-scalar continuation forms.
  - A path that a template builds is judged up to its first `{{`, never skipped.
  - A URL (`example.com/files/x`) is not a reference.
  - The host_vars and group_vars mentions that the repository's plays hold are not matched.
- Routing:
  - The `qa/changed` glob's star crosses `/`, so `files/*teamchat*` covers
    `files/opt/claude-yolo/optional/teamchat/...` and the subdirectory plays.
  - `qa/internal/proc` and `qa/internal/rootpath` are not routed to this gate. A change to them
    reruns qa-go-qa, which runs the command package's real-work-tree test. Acceptable.
- The tests can fail: each K-test has its red case, the reserved and per-team-root tests have
  discriminating cases, and the id decode test covers every id kind with bad values.
- Nothing install-specific is in the diff. The tests use `/home/u`, `/run/user/1000` and
  `ops.teamchat.internal`.
