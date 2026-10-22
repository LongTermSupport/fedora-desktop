# N2 slice n2d: handoff 2

The second builder stopped at its context budget. Branch `feat/bus-v2-n2d`, worktree
`/workspace/untracked/worktrees/bus-v2-n2d`, tip after this file's commit. The contract is
`SPEC-n2d.md`: sections 1-3 are the lead's, section 4 the host half's.

## Done

- **The host half** (ab500e32): the team lock, `host.Apply`, `host.Remove`. Report:
  `subagent-reports/261022-n2d-host.md`.
- **Member operations** (91032ce9) in `internal/teamchat/apply/member.go`, MO1-MO8.
- **Lint clean** (540e41da): `target` split (`addTarget`), the test's crash wrapper generic,
  long tests split.
- **The verbs and the child** (d37180e2), CV1-CV6, in `internal/teamchat/admin`:
  - `serve.go`: `Serve(ctx, Opener, Request) Answer`. The child serves `member.*`, `role.apply`
    and `team.apply`. Args are strict per op (`argKeys`). `team_file` is decoded by
    `declare.DecodeTeamFile`. `team.apply` carries `input`, `approved` and `reg_key`. The
    `Opener(ctx, team, regKey) (apply.Deps, error)` seam is the child's reach to its admin
    token and `ctl/<team>/` files; it is not built, and its failure is class `config` (78).
    `errorClass` is `ExitCodeOf`'s code mapped back to a class.
  - `child.go`: the answer gains `handle`, `credential` (`apply.Credential`; kind
    password|token, secret 1-4096 printable ASCII without a space, never quoted in an error)
    and `notes`. A failed answer may carry its non-empty `changed`. `ctlChild` serves through
    `e.Sys.Open` and still answers "not served by this build" without one.
  - `verbs.go`: `System{Host host.Deps, Child, Bundles, OwnerGroup, Open}` in `Env.Sys`. `Run`
    now takes `ctx` (`containedctx` forbids a context in `Env`). Member and role verbs take the
    team lock (`SeatWait` 60 s), read `root/<team>/team.json` (a missing file is 70), ask the
    child, call `Status.Refresh(ctx, team, false)`, and print one JSON document
    `{"v":1,"changed":[...],"handle":..,"notes":[..],"bundle":{..}}`, on a failure too.
    `team approve` uses `declare.Approve` and `FileApprovals` with the `OwnerGroup` gid; it
    prints one `approved <field>` per differing field and writes nothing when none differs.
    `team apply` (no flags, stdin) and `team remove --team T [--purge]` wait `host.PlayWait`
    and print `CHANGED<TAB>` lines. Flags are strict (`parseFlags`): 64. Verbs with a nil
    `Sys` fail as "not wired yet"; `cmd/teamchat-admin` passes none.
  - `exit.go`: `verbExit` maps `*host.BusyError` 75, `*ChildError` by class, `*refusal` 70 and
    `*host.Error` drift 70.
  - `host.MkdirShared` is exported (approve makes the team's status directory with it).
  - Tests: `world_test.go` (fakehs, the host fakes under a temp root, the real `ctl-child`
    verb as the in-process child, a `crashing` admin wrapper), `serve_test.go`, and
    `verbs_test.go`. The verbs tests cover approve then apply, an unapproved change refused
    and then approved by `--only`, add (password, token, seat next), the lifecycle, a
    coordinator change, a crash at each add step rolled forward by the same verb, park and
    retire crashes rolled forward by `role apply`, a bootstrap crash finished by the next
    apply, remove and purge, refusals, busy, strict flags, the owner group (78), and a failed
    status refresh that still prints the bundle.
- Coverage (`go test -race -shuffle=on -cover`): admin 95.0%, apply 90.8%, host 93.3%, the
  other teamchat packages at least 90.7%. `cmd/teamchat-admin` is at 50%: its `main` has no
  test, as before. golangci-lint finds nothing, with and without `--build-tags tuwunel`.
  qa-changed's only red is govulncheck GO-2026-6809.

## Left

1. **Optional**: one `-tags tuwunel` round trip of add, role and retire, beside
   `apply/tuwunel_test.go`, if cheap.
2. **The review loop**: one fresh Opus reviewer over `feat/bus-v2-n2b..HEAD`, against
   SPEC-n2d.md, C 3.3, 3.5 and 3.6.3, and gates A and B. At most 3 rounds; review files are
   `subagent-reports/261022-n2d-review-<n>.md`, each committed.
3. **The report** `subagent-reports/261022-n2d.md`, with the host report's decisions merged
   in, and Task 2.3's progress in PLAN.md. The report must name the n2b decisions this
   supersedes (D9, D22) and propose these design-text fixes:
   - C 3.6.3: the step names and the role step (MO2, MO6);
   - C 3.6.3: `--seat next` (n2d-D3);
   - A 7.4: the notice bodies (n2d-D5);
   - C 3.3: the child's request args (n2d-D7) and the answer's `handle`, `credential`,
     `notes` and a failure's `changed` (CV2);
   - C 3.3's example request: `seat` is a number (0 is next), not the string `"3"`.

## Decisions this builder took (for the report)

- n2d-D8: the member and role verbs always answer JSON; the team verbs print CHANGED lines.
  C 3.6.3 says every member verb answers one JSON document, and only the play runs the team
  verbs.
- n2d-D9: the answer and the JSON document carry `handle`, so a `--seat next` caller learns
  the handle it got.
- n2d-D10: a failed answer carries the lines that changed before it (MO8, HS13), and a failed
  member verb still prints its document.
- n2d-D11: the status refresh runs after the child answers and before printing. If the
  refresh or the bundle fails, the verb fails (1) but still prints the document, so an issued
  credential is never lost without a word. A failed bundle's message names `member reissue`.
- n2d-D12: member and role verbs wait `SeatWait` (60 s, a launcher's seat take, C 3.4). The
  team verbs wait `host.PlayWait`. `team approve` takes no team lock: it writes one file
  atomically, and `team apply` reads it under the lock.
- n2d-D13: `team apply` takes no `--team` flag; its team is the input's (C 3.5.2). A `team approve` whose input is another team than `--team` is 64.
- n2d-D14: the child's `Opener` failing is class `config` (78): its own files are its
  configuration.
- Left for later slices: the production `Child` (start with `ChildSpec`, n2a-D6), the
  production `Opener` (the admin token file and the first admin's registration with
  `reg_key`), `Bundles` (n2c), `OwnerGroup` (the group file, gate A), and the Fedora check of
  C 3.5.2 step 1 (install, gate A). The guest token copy `ctl/<team>/guest/<handle>.token` is
  the seat side's or `lxc apply`'s, not `credential`'s. `seatDevice` in `apply/member.go`
  duplicates n2c's `paths.SeatDevice`; replace it at merge.

## Notes for the next builder

- The coverage floor is per package, measured with `go test -race -cover` (90%). go-qa stops
  at govulncheck GO-2026-6809 (Task 1.6), the only acceptable red; say so in each commit
  message.
- `Run(ctx, Env)`: every test helper passes `t.Context()`.
