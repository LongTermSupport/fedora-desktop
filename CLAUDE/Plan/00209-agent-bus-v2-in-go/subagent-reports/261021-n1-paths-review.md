Verdict: FIX-FIRST

# Plan 00209 N1 Tasks 1.1 and 1.2: adversarial review of feat/bus-v2-n1-paths (db3c8350..HEAD)

Scope read: D 3.1-3.8, 4.1, 4.2; DESIGN-v2 section 4; A 2.2; B header, 2.3.3, 2.4.1; C's host
paths (grep). Ran `go test -race` over internal/teamchat/..., qa/teamchatseparate,
qa/cmd/qa-teamchat-separate, qa/changed, qa/all (all pass) and the wrapper (exit 0, 5 files).
Ran the gate against temporary untracked probes, which were deleted afterwards (`git status`
clean).

## Findings

01. **blocker**: `src-go/internal/teamchat/paths/ids.go:46` with `paths.go:65,149`. Team id `ctl`
    makes the homeserver user equal the credential user. `ParseTeamID("ctl")` is accepted, and
    `HomeserverUser("ctl")` is `teamchat-ctl`, which is `CtlUser`. D 3.5 makes the homeserver a
    `DynamicUser=` named `teamchat-<team>`. systemd's documented behaviour is that when a static
    user of that name already exists, it is used and no dynamic user is allocated. So team
    `ctl`'s homeserver would run as `teamchat-ctl` and could read `/var/lib/teamchat-ctl/*`,
    which holds every team's admin token. A 2.2 reserves no id, and no test checks a
    team-derived name against a fixed name. Team ids are fixed at creation and never changed, so
    this has to be closed before the first team exists. This package is the cheapest place to
    close it.
    Fix: give `ParseTeamID` a reserved set (at least `ctl`), refused with `ErrInvalid`. Add a
    test that strips `teamchat-` from every fixed name (users, units, binaries, dirs), feeds
    each remainder that parses as a team id through every team-derived function, and fails on
    any equality with a fixed name. Then feed the reservation back to A 2.2 and D 3.5 as a design
    note.

02. **major**: `paths.go:186` (`AdminState`). A team named `lxc` collides with a directory C
    already uses. C writes `/var/lib/teamchat-admin/lxc/<container>/<user>.json` (C ~1736).
    That is `AdminState("lxc")` for a team whose id is `lxc`, which the grammar accepts. The
    package avoided the same clash for sets with `_sets/` (`paths.go:210`), but it does not name
    C's LXC records at all, so nothing caught this one.
    Fix: name the LXC guest record in paths under an underscore directory (`_lxc/<container>/ <user>.json`) and send the change to C. Add a test asserting that every fixed child of a
    per-team root (`AdminStateRoot`, `StatusRoot`, `CtlStateRoot`, `HomeserverStateRoot`) fails
    `ParseTeamID`.

03. **major**: `paths.go:1-17` (package doc) and the `Run` and `Seat` types. The package does not
    hold every name, though its doc says it does. The doc claims "the one source of every name
    and path v2 uses" and "No other v2 package writes a name of its own", but names the designs
    give are missing:

    - B 2.4.1: `<v2-run>/session.pid`. It is the keeper's core record, read by the leftover
      sweep and by C's kit pruning (B 2.13, C).
    - B 2.12: the plugin's `hooks.json`.
    - C: `root/<team>/config.toml` and `restore.block` (both used in unit text), `team.json`,
      `teams/<team>/approved.json`, `teams/<team>/lxc-online.json`, and
      `/usr/local/share/teamchat/resolv.conf` (the only file `ShareDir` exists for).
    - C: `/usr/local/lib/teamchat/teamchat-admin-<digest>`, the probe state
      `~/.local/state/teamchat/probe/<team>/`, and the guest unit's
      `XDG_RUNTIME_DIR=/run/teamchat-session-<user>`.
    - C 3.7: the seat-store pool internals (`pool/<key>/checkout.json`, `<team>/<seat>/`,
      `held/`, `released.json`, `password`, the `.new-<claim>` staging names,
      `released/sessions/` and `released/pool/`).
    - D 3.7: the docs, test and VM-scenario names.

    Each missing name is a name the later packages will spell for themselves, outside the part-3
    v1 check. Fix: add them (session.pid, hooks.json and C's unit-text files at least, since
    those cross an area boundary). Otherwise narrow the doc and Task 1.1 to "every name that
    crosses an area boundary", and say which package owns the rest.

04. **major**: `src-go/qa/teamchatseparate/scope.go:45-47`, with the same glob in
    `scripts/qa-routing.tsv` (the new `also` row). The gate does not scan a v2 play in a
    subdirectory of `playbooks/imports/`. It counts `play-teamchat*.yml` only directly under
    `playbooks/imports/`. Most of this repo's plays are under `playbooks/imports/optional/<category>/`
    (50 of 84 files), and v1's Element play is `optional/common/play-agent-bus-element.yml`, so
    `play-teamchat-element.yml` is likely to land there. I confirmed it with a probe: an
    untracked `playbooks/imports/optional/common/play-teamchat-zzprobe.yml` holding
    `agent_bus_teams` passed with exit 0. This is the "gate that scans the wrong things" mode,
    and `ScanPaths` already lists that subtree.
    Fix: put any file whose base name matches `play-teamchat*.yml` anywhere under
    `playbooks/imports/` in scope, route the same way, and add a scope test for the
    `optional/common/` case. Also check that `paths.ElementPlayFile` matches wherever C puts the
    file.

05. **minor**: `v1-names.txt:20`. The `$XDG_RUNTIME_DIR/ccy/membership` entry only catches that
    one spelling. A probe holding `${XDG_RUNTIME_DIR}/ccy/membership` and
    `{{ xdg }}/ccy/membership` in a `files/etc/teamchat-*/` file passed, and Go's
    `filepath.Join(rt, "ccy", "membership")` would pass too. The entry follows D 4.1 word for
    word, but it does not protect what it is there to protect.
    Fix: add `/ccy/membership`, which also covers `/run/ccy/membership`. Nothing v2 or shared
    that is in scope holds it. Amend D 4.1's list to match, since TestTheShippedListIsD41s pins
    the list to the design.

06. **minor**: `scope.go:40-44`, decision (a). Scoping task files by name is acceptable only if
    the naming is a rule. As built, a v2 play that includes `tasks/render-units.yml` (no
    `teamchat` in the name) puts that file out of scope without any warning, and no rule or doc
    asks v2 task files to carry the prefix.
    Fix: either have the gate read `play-teamchat*.yml` for `include_tasks`, `import_tasks` and
    `template`/`copy` `src` targets, and fail on a target that is neither in scope nor on a
    short named shared list (`tasks/build-go-command.yml`), or write the naming rule into D 4.1
    and AnsibleStyle. The first is the fail-fast form.

07. **minor**: `allowed.go:42-55` and `run.go` `unallowed`. One allowlist row allows any number of
    occurrences. A row is keyed by (path, literal) and covers every occurrence of the literal on
    every line of the file. D 4.1 says "one line per remaining reference", but a second copy of
    an allowed reference added later passes silently. Fix: add a count, or report used rows
    with their hit count and fail when the count exceeds one unless the row declares more.

08. **minor**: the plan and design have drifted from decisions (c), (e) and (f), and none of the
    three is recorded in a tracked file:

    - (c): D 4.1 says the red proof is "a fixture under `testdata/`".
    - (e): D 3.7 says the paths unit test expands team-derived names against every declared v1
      team. It cannot, because host_vars are private. The deferral to C's validator is right,
      but the design still says otherwise.
    - (f): D 4.1 part 2, the migrate/v1 analyzer, is not built, and Task 6.1 does not mention it.

    Fix: amend D 3.7 and D 4.1 to say what was built and why, and add "D 4.1 part 2 analyzer
    (GoProjectRules.md, red/green)" to Task 6.1.

09. **minor**: `paths.go:173-179,252`. Some functions take strings that are never checked, which
    contradicts the package doc. `KitBinary`, `KitPluginDir` and `KitSettings` take a base
    directory but skip `checkBase` (`KitBinary("")` is `/bin/teamchat`). `ElementLauncherName`
    takes the display name unchecked, although the doc says "a derived name never holds an
    unchecked string". A display name ending up in a `.desktop` `Name=` line must not carry a
    newline (A 2.2). Fix: run `checkBase` (with an error return) on the kit functions, and add a
    checked `DisplayName` type or validate inside the function.

10. **minor**: `paths.go:243` (`checkBase`) and the functions that call it. `/` passes
    `checkBase`, and `home + "/.config/..."` then gives `//.config/teamchat/...`. The same
    happens for `StateHome` when `XDG_STATE_HOME=/`, and for `SeatStore` and `AllocLock`. Fix:
    refuse `/` as a base directory, or join with `path.Join`.

11. **minor**: `separation_test.go`, `exportedStringIdents` and `funcIdents`. The completeness
    test misses some exported names. It finds only exported `const` declarations and functions
    whose first result is the identifier `string`. A later exported `var X = "/var/lib/..."`, or
    a function that returns a named string type, would not be sampled, so the part-3 checks
    would never see it. Fix: also cover `token.VAR`, and treat any result type whose underlying
    type is string as a name (go/types), or fail on any exported identifier the test cannot
    classify.

12. **minor**: `separation_test.go:59-75` against `names.go:69-77`. The list's matching rules are
    written twice, once in the test (`holds`) and once in the gate (`Find`), because the nested
    module cannot be imported. Today they agree, but they can drift apart. Fix: add a small
    shared corpus file next to `v1-names.txt` (input, name, expected hit or no hit) that both
    tests read.

## Checked and found sound

- Every name in paths.go agrees with D 3.2-3.8, the B header table, B 2.4 runtime files (bar
  session.pid), D 3.6 member paths and C's seat store and mounts. This covers kit roots and
  `current`, `/run/teamchat/{seats,membership}`, `/tmp/teamchat/<run>`,
  `$XDG_RUNTIME_DIR/teamchat/{run,alloc}`, the units and their templates,
  `StateDirectory=teamchat/%i`, the fence table, unit and file, the Element profile, directory,
  launcher and Name, host_vars, the play, tag and only-teams variable, and the env names.
- The team id grammar is A 2.2's. The 22-character id's `teamchat-<id>` is 31 characters, and a
  test checks it. RunID and CheckoutKey follow B 2.4 and C 3.7. Parse errors do not echo the
  value. `SetLock`'s `_sets/` cannot meet a team id.
- Decision (d) is sound: one safe path segment refuses `.`, `..`, `/` and a leading `-`.
- No v2 name holds, or is prefixed by, a v1 name, and none prefixes one. The part-3 test parses
  the package to enforce sampling, and its matcher discrimination tests can fail.
- Decision (b) is sound. Plain entries match in any case (EqualFold over ASCII only, refused
  otherwise). `Element- !teamchat-` is exact-case with the follow test. Matches are reported as
  byte ranges. Every hit is judged, so one uncovered hit fails the line. Findings count once
  per name per line.
- The allowlist cannot go stale: a row for a wrong path, a homed path or a literal that matches
  nothing is a finding. A covered literal must span the hit.
- Every exit-2 path is real: git failing or not a work tree, zero files in scope, a list that is
  missing, empty, malformed or duplicated, an allowlist that is missing or malformed, an
  unreadable file, unwritable streams, and an unresolvable symlink root. Nothing passes
  silently.
- The scope is what git would publish (`--cached --others --exclude-standard`) with literal
  pathspecs. The two homes match with a trailing slash. Shared code (`internal/teams`, the
  launcher) is out of scope.
- The probe showed the red and green work end to end: `Agent-Bus` and `Element-ops` were flagged,
  while `Element-teamchat-ops` was not.
- Wiring: the qa-gates.tsv row and its detail regex, the addedgates reason, the wording
  testdata, the qa-changed check (order, summary, script), routing including deletions and the
  shared helpers, the perfile-gate list, CI through qa-all, and the QA.md and src-go/CLAUDE.md
  rows are all in place. The wrapper is within the bash limits. Leaving rootpath and proc out
  of routing matches nokill.
- Nothing install-specific is in the diff. Tests use `/home/u`, `/run/user/1000` and
  `ops.teamchat.internal`.
