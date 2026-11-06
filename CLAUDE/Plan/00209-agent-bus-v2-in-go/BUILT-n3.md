# N3 slices: what was built, and the reviews it passed

Moved out of `PLAN.md` (Tasks 3.2 and 3.3) so the plan stays within its size limit; the text is
unchanged. The tick of each task, and what is still missing, stay in `PLAN.md`.

## Task 3.2: the `teamchat` binary

- **n3a built** on `feat/bus-v2-n3a` (SPEC-n3 sections 1, 2, 9 and 10): `cmd/teamchat`, the
  CLI, the member files' codecs and store writer, the seat and run locks, the kit's layout
  and the guest operations. Coverage: bundle 93.1, cli 99.1, exits 98.9, guest 93.8, homefs
  93.5, kit 93.3, lock 93.3, records 100, state 99.7, reexec 92.3, paths 100, seat 91.0,
  health 91.9; `cmd/teamchat` 50.0 (its `main` only). Three Opus review rounds
  (`subagent-reports/261022-n3a-review*.md`): FIX-FIRST, FIX-FIRST, MERGE. Report and the
  design-text fixes it proposes: `subagent-reports/261022-n3a.md`.

- **n3c built** on `feat/bus-v2-n3c` and merged into `feat/bus-v2-n3` (SPEC-n3 section 4,
  decisions n3c-D1 to n3c-D24, W-D1 to W-D3): the keeper (`run`), the link, receiving, the
  client verbs (`wait`, `status`, `ledger`), the ends (EN1-EN4), the failpoints and the
  busfailpoints gate stage. Coverage: bundle 93.5, cli 99.1, client 91.5, ends 97.3, engine
  94.3, exits 98.9, guest 93.8, homefs 93.3, keeper 90.7, kit 93.3, link 91.1, lock 93.0,
  records 100, state 99.7, team 100, teamsync 94.5, wire 94.1, wiring 100, proto 98.3, matrix
  98.9, fakehs 93.3. Two Opus review rounds and a narrow check
  (`subagent-reports/261022-n3c-review*.md`, `261022-n3c-check.md`), then the qa-reviewer
  (`261022-n3c-qa-review.md`, FIX-BEFORE-MERGE). Its blocker, an e2e test failing under load,
  was a product race (a `back` posted after the run's `disconnected`), fixed with one
  lifecycle turn per team and rule `project_lifecycle_turn` (`261022-n3c-race.md`); its docs
  findings are fixed (`261022-n3c-docs.md`). A keeper test seen failing under load at the n3b
  merge read a log line before its turn ended: fixed with a turn barrier and rule
  `project_keeper_log_barrier` (`261023-n3c-owed-flake.md`). Guest `TestRegister` could not
  run twice: the op registry is its own type, and the failpoints test stage runs every test
  twice (`261023-n3-guestreg.md`). n3d and n3e are in review or being built; n3f is left.

- **n3d reviewed** on `feat/bus-v2-n3d` (the sends, the outbox and its recovery, TIMEOUTs,
  the forge check; rows n3d-D1 to D34): Opus reviews A and B (`261022-n3d-review-A.md`,
  `-B.md`), both fixed (`261022-n3d-fixa.md`, `-fixb.md`) and checked
  (`261023-n3d-narrow.md`); the qa-reviewer's findings (`261023-n3d-qa-review.md`) fixed
  (`261023-n3d-fixc.md`) and checked READY TO MERGE (`261023-n3d-fixc-narrow.md`). The store's `Version` is not raised for n3d's new keys: a store written by n3c or an
  earlier n3d part is refused whole rather than migrated, because no such store exists outside
  tests (review A n3; GATE A means none is deployed). Found here and fixed on
  `feat/bus-v2-n3-guestreg`: `guest`'s `TestRegister` could not run twice (`-count=2`).

- **n3d's caps measured as written** on `fix/store-size-as-written`
  (`261105-fix-storesize-opus-report.md`): the store's entry sizes, `MaxRosterCache` and
  `MaxSession` were derived from A's compact form while encoding/json writes `<`, `>`, `&`,
  U+2028 and U+2029 longer; the store now measures every entry through its one encoder, a send
  whose entry does not fit as written is refused 10 `size` at the verb, and the two file caps
  hold their largest values escaped. Not yet reviewed.

## Task 3.3: LXC members

- The guest side of B 2.13 (GU1-GU5, SA1) is built in n3a; the host side (C 3.9) is n3b's.
- **n3b built** on `feat/bus-v2-n3b` and merged into `feat/bus-v2-n3` (SPEC-n3 section 3,
  decisions n3b-D1 to n3b-D4 and n3-D21 to D30), as inert code: `teamchat-admin lxc apply`
  and `lxc restart` (C 3.9), the session unit as code (`host/lxc/unit`), the guest's facts,
  root and bus (`host/lxc/guest*`), `lxccmd` and the LXC record (`lxcstate`), the L3 probe in
  the health tick (n3b-D4), and the keeper's `--kind lxc --key`. An offline member never
  fails anything (n3b-D1, the owner's rule); a container holds any number of members, each
  (team, container, checkout, seat) (n3b-D2). Coverage: execx 94.8, admin 96.2, admin/wire
  91.5, apply 91.8, declare 96.0, health 93.8, host 93.3, host/lxc 91.9, guestbus 97.8,
  guestfacts 99.6, guestroot 95.9, lxccmd 97.8, lxcstate 96.5, unit 100, cli 98.9, guest
  93.7, keeper 90.8, paths 99.7, reexec 90.5. Reviews A, B and 2, the parts' review rounds
  and a check (`subagent-reports/261022-n3b-*.md`), then the qa-reviewer
  (`261022-n3b-qa-review.md`, FIX-BEFORE-MERGE), fixed in fix5 and fix6. No play task or unit
  is installed (gate A).
- The install build must run `go test -tags teamchatrelease ./internal/teamchat/member/cli/`,
  the CL2 gate, before it installs the binary.
- Tagged analyzer stages (n3-tagvet): `project-rules-tagged` runs the project rules over the
  packages built with each of the four tags; before them a finding in a tagged-only file passed the gate (`subagent-reports/261023-n3-tagvet.md`).
