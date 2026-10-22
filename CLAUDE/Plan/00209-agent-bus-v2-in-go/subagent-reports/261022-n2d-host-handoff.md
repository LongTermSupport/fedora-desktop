# n2d-host handoff (Plan 00209, N2 slice n2d, the host half)

The builder of the host half stopped at its context budget. This file is what a fresh builder
needs to finish. Worktree: `/workspace/untracked/worktrees/bus-v2-n2d-host`, branch
`feat/bus-v2-n2d-host` (cut from `feat/bus-v2-n2d`). Never write in the lead's worktree
`bus-v2-n2d`.

## Done and committed (each green: `go test -race -cover`, golangci-lint clean)

1. `paths/host.go` and `host_test.go`: `UnitsRecord`, `ServerNameFile`, `ServerDropInFile`,
   `ServerUnitTemplateFile`, `HomeserverDatabase`, `HomeserverBackups`,
   `HomeserverPrivateDatabase`. The separation and zero-value tests take them through
   `hostNames(t)` and `hostZeroDerivations()` (each a one-line hook in `separation_test.go` and
   `zero_test.go`).
2. `host/lock.go`: `TakeLock(ctx, dir, team, verb, wait, clock) (*Lock, error)`, `Lock.Release`,
   `Lock.Team`, `BusyError{Path, Holder}`, `PlayWait`, `Clock`, `SystemClock()`.
3. `host/render.go`: `Pin{Version, MemoryHigh, CacheCapacityModifier}`, `render(renderSpec)`
   (the config, the drop-in, team.json, the two digest sets), `deviceUnit`, `regKeyOf`.
   `testdata/tuwunel-1.9.4-keys.txt` is a copy of v1's vendored key list.
4. `host/records.go`: units.json, firewall.json (`Rule{Zone, Source, Port}`, `Rule.Rich`) and
   the stamp, each encoded and read back strictly.

## Committed as work in progress, NOT green on lint

`host/apply.go`, `host/apply_test.go`, `host/fakes_test.go`: `Apply(ctx, Deps, declare.Input) ([]string, error)` with steps 1-9 and 11, the seams (`Systemd`, `Firewall`, `Network`,
`Readiness`, `Status`, and `Deps.Backup` and `Deps.Bootstrap` as functions), `Error{Verb, Step, Drift, Err}`, and `UnitState`. **Every test passes** (`go test -race -cover`: 91.7% over the
package). golangci-lint reports 20 findings to fix with Edit (never `//nolint`):

- funcorder in `fakes_test.go`: move world's exported `Load` above its unexported methods, and
  fakeSystemd's `unit` below its exported methods.
- gocognit: split `(*applying).port` (put the units.json scan in its own function) and
  `converge` (put the union write and the removal loop in their own functions).
- goconst: a `const verbApply = "team apply"`.
- gosec G301 on the two `0o755` Mkdirs (the drop-in directory and the status team directory)
  and G302 on `os.Chmod(dir, 0o700)` in `mkdirOwned`. Check how the module's other packages
  make a 0755 directory without suppressing (perhaps `os.MkdirAll` in `state`, or a Chmod with
  `fs.FileMode` held in a const); `mkdirOwned` can create with 0o700 and drop the Chmod when the
  umask leaves it 0700, but test the umask case.
- govet shadow: four `err` shadows in `render` and `readInstalled`.
- cyclop: `TestFirstApply` is 17 > 15; split the mode checks into a helper.
- thelper: `ops(t)` needs `t.Helper()`.

## Left to build

1. Fix the lint above, run `./scripts/qa-changed.bash` (go-qa stops only at govulncheck
   GO-2026-6809, Task 1.6, the one acceptable red), commit.
2. `host.Remove(ctx, Deps, team paths.TeamID, purge bool) ([]string, error)` (C 3.5.3), test
   first in `remove_test.go`, reusing `newWorld` and the fakes: lock (`team.remove`); not
   installed (no AdminState, no drop-in, no stamp) is success with no output; stop (`unit stopped`) and disable (`unit disabled`); remove only the recorded firewall rules through
   `ensureRule(..., false)` in both configurations, then delete firewall.json; delete the drop-in
   and its directory, `daemon-reload` (`drop-in removed`); delete the stamp (`stamp removed`);
   `Status.Remove` (its line, if any; the migration-origin record is the seam's); with `--purge`
   also `CleanState`, `RemoveAll` of AdminState and CtlState, and approved.json. Steps
   `StepLock`, `StepUnit`, `StepFirewall`, `StepFiles`, `StepStatus`, `StepPurge` (consts exist).
   Each step's failure stops what follows; a second remove reports nothing.
3. Fill SPEC-n2d.md section 4: replace only its placeholder line with HS1-HS14 (the numbering the
   tests' comments use: HS1 lock file, HS2 busy and wait, HS3 refusals and the fixed server name,
   HS4 render, HS5 digest sets and the five-way answer, HS6 first-apply files and step 7, HS7
   firewall order, HS8 backup, HS9 readiness, HS10 bound addresses, HS11 records and stamp last,
   HS12 idempotence, HS13 each failure named and stopping, HS14 remove), then the decisions
   below as n2d-H1..., at the end of section 4 (not in section 3's table).
4. Write `subagent-reports/261022-n2d-host.md` (built, the exported API, coverage, decisions,
   design-text fixes) and commit it. No review loop: the lead runs one over the slice.

## Decisions taken (record as n2d-H1...)

- Apply takes the lock itself (verb word `team.apply`); root, Fedora and decoding the input
  are the verb's (the lead's).
- The approval gate (`declare.Hold`) is also held at step 2, so no firewall or listen change of
  an unapproved declaration lands before bootstrap's own gate.
- team.json is `in.Team.Canonical()`: equal to apply.Run's approved TeamFile whenever the gate
  holds (lists compared as sets, scalars equal).
- The registration key is inline in config.toml (`registration_shared_secret`), read back from
  the installed config; a first apply makes it from `Deps.Rand` and creates the config
  link-not-replace in step 4 with server-name.
- First apply = no server-name file. The status record's `first` = no stamp before this apply.
- Step 7 starts a unit it finds stopped even when nothing changed.
- units.json absent with a drop-in present is a restart; no drop-in is a first start.
- The step 6 backup start writes the old drop-in from units.json (version, port, listen) with
  no device ordering and no allow_from; a database with neither drop-in nor units.json fails.
- Rules record their port, so a rule of an old port is still removed. CHANGED for a rule is
  one line whichever configuration changed.
- `MemoryHigh` and `cache_capacity_modifier` come from `Deps.Pin` (H-C1 not yet measured).
- The old `tuwunel-<version>` binary deletion (C 3.2.3) is not built here.
- Design-text fix to propose: C 3.5.2 step 4 says "the registration secret (3.2.2)" while
  3.2.2 says it lives only in config.toml, written in step 7: say step 4 creates config.toml.
