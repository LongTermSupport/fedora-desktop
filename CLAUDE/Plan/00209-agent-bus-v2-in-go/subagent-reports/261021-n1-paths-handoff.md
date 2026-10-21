# Plan 00209 N1 Tasks 1.1-1.2 (task name "paths"): handoff

The builder stopped at its context budget, after review round 1 (FIX-FIRST) and part of the fix.
Worktree `/workspace/untracked/worktrees/bus-v2-n1-paths`, branch `feat/bus-v2-n1-paths`. Rules
binding the next agent: `/workspace/untracked/scratch/n1-common.txt`.

## Done (committed)

- `2b5d80e3` Task 1.1: `src-go/internal/teamchat/paths` (`ids.go`, `paths.go`, tests). 100%
  coverage. `separation_test.go` is D 4.1 part 3: it parses the package, requires every exported
  const and string-returning func or method to be sampled, and checks each sampled value against
  `src-go/qa/teamchatseparate/v1-names.txt` (holds, prefix both ways) and D 3.1's derivation rule.
- `1b46e982` Task 1.2: gate `teamchat-keeps-off-v1`: `src-go/qa/teamchatseparate` (K1-K6 in the
  package doc, 100% coverage), `src-go/qa/cmd/qa-teamchat-separate`, `scripts/qa-teamchat-separate.bash`,
  rows in `scripts/qa-gates.tsv`, `scripts/qa-routing.tsv` (`also` rows and the perfile-gate list),
  `src-go/qa/changed/checks.go` (+ checks_test, classify_test), `src-go/qa/all/addedgates_test.go`
  and `testdata/wording/teamchat-keeps-off-v1.txt`, `CLAUDE/QA.md`, `src-go/CLAUDE.md`. Red and
  green proven on the real tree through the wrapper. PLAN.md Tasks 1.1 and 1.2 are 🔄.
- Review round 1: `261021-n1-paths-review.md` (FIX-FIRST, 1 blocker, 3 major, 8 minor).
- The WIP commit with this handoff: finding 01 fixed in `ids.go` (`reservedTeamIDs`, refused by
  `ParseTeamID`, tested); `KitDigest` renamed `Digest` (also names the teamchat-admin copy);
  `DisplayName` + `ParseDisplayName` (A 2.2) added and tested, not yet used.

## Left, by review finding

- 01 (rest): add to `separation_test.go` a test that strips `teamchat-` from every path component
  of every fixed (const) sampled value, cuts at the first character outside `[a-z0-9-]`, and
  requires `ParseTeamID` to refuse each remainder; it holds `reservedTeamIDs` complete.
- 02: add `AdminLXCDir = AdminStateRoot + "/_lxc"` (C's `/var/lib/teamchat-admin/lxc/` moved
  under an underscore, as `_sets` is); a test that every first segment under a per-team root
  (`HomeserverStateRoot`, `HomeserverPrivateStateRoot`, `AdminStateRoot`, `CtlStateRoot`,
  `StatusRoot+"/teams"`) that is not a sample team id fails `ParseTeamID`. Design fix for C 3.9.
- 03: add `Run.PID()` (`session.pid`, B 2.4.1), the plugin hooks file
  (`<kit>/plugin/teamchat/hooks/hooks.json`, as v1's plugin lays it out), `ServerConfig(t)`
  (`AdminState/config.toml`), `RestoreBlock(t)` (`restore.block`), `TeamDeclaration(t)`
  (`team.json`), `ApprovedFile(t)` and `LXCOnlineFile(t)` (under `StatusTeamDir`),
  `ResolverStub` (`ShareDir/resolv.conf`), `AdminBinaryCopy(d Digest)`
  (`LibDir/teamchat-admin-<digest>`), `ProbeState(stateHome, t)` (`<state>/teamchat/probe/<t>`),
  `GuestRuntimeDir(u)` (`/run/teamchat-session-<u>`), `Store.PoolCheckoutRecord(k)`
  (`checkout.json`). Narrow the package doc: later tasks add the file names inside these
  directories here. Add each to `sampleNames` and `paths_test.go`.
- 04: `scope.go` playbooks rule: any file whose base name is `play-teamchat*.yml` anywhere under
  `playbooks/imports/`; routing glob `playbooks/imports/*play-teamchat*.yml`; scope test for
  `playbooks/imports/optional/common/play-teamchat-element.yml`.
- 05: add `/ccy/membership` to `v1-names.txt` and to `TestTheShippedListIsD41s`; propose the D 4.1
  list change in the report.
- 06: fail fast on an out-of-scope reference: in each scoped `.yml`/`.yaml`, every
  `tasks/...yml` or `files/...` path must be `InScope` or on a short shared list with a reason
  (`tasks/build-go-command.yml`); else a finding.
- 07: a row that covers hits on more than one line is a finding (D 4.1: one row per reference).
- 08: design drift only: record in the final report (testdata fixtures replaced by in-memory and
  real-git trees; D 3.7's declared-team check is C's runtime validator; D 4.1 part 2 belongs to
  Task 6.1).
- 09: make the kit a type (`type Kit string`, methods `Binary`, `PluginDir`, `PluginHooks`,
  `Settings`; `HostKitDir` returns `Kit`; `HostKitCurrent`, `CCYKitDir` typed); make
  `ElementLauncherName` take `DisplayName`.
- 10: `checkBase` refuses `/`; `StateHome` refuses an `XDG_STATE_HOME` that cleans to `/`.
- 11: completeness: classify every exported func, method and var; fail on any not sampled and not
  on an explicit not-a-name list (`Parse*`, `CCYLayout`, `HomeLayout`, `SeatStore`, `Layout.Seat`,
  `Layout.Run`, `TeamID.String`, `ErrInvalid`).
- 12: a shared corpus `src-go/qa/teamchatseparate/v1-names-corpus.tsv` (input, expected hit or
  clean) read by both `separation_test.go` and `names_test.go`.

Then: `go test -race` and `golangci-lint run` for both modules' packages, `./scripts/qa-changed.bash`,
commit, review round 2 (an opus sub-agent writing `261021-n1-paths-review-2.md`), at most 3
rounds, commit each review file, mark PLAN.md Tasks 1.1-1.2 ✅ at MERGE, and write
`261021-n1-paths.md` (built, coverage, decisions a-f of the review prompt plus 01's reserved ids
and 02's `_lxc`, design-text fixes: A 2.2 reserved ids, C 3.9 `_lxc`, D 3.7, D 4.1 list and
fixtures, Task 6.1 analyzer). No version bump was demanded by the hooks.
