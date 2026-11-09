# Cross-network review blockers B1 and B2 (PR 128): builder report

Branch `fix/crossnet-b1-b2-wire`, from the local head of `spike/v2-cross-network` (8994d156d).
Commits are local only: nothing was pushed or merged.

| Commit      | What                                                              |
| ----------- | ----------------------------------------------------------------- |
| `542b81a4e` | B1: the team backup carries the CA, and a restore keeps it        |
| `4cb83c947` | B2 (S16): the production System serves the team CA (`host.CAPEM`) |

## B1: the CA goes into the backup and comes back on a restore

What changed:

- **`backup/tar.go`**
  - With a CA, the tar is manifest **version 2**: `ca.crt` (0644) and `ca.key` (0600) follow
    the six members of version 1. The member table now holds a mode for each member.
  - A team with no CA yet is written as version 1.
  - `Decode` reads six or eight members. The manifest version must match the count. The CA pair
    must be one `pki.ParseCA` accepts, and its error does not quote the content.
  - `Contents.CA` is a `[]host.BackupFile`.
  - `Encode` refuses CA files that are not exactly the certificate then the key, at their modes.
- **`backup/take.go`**: `readSources` reads the CA through `host.CABackupFiles`. A CA the host
  cannot use fails the backup at its `sources` step.
- **`host/tls.go`**
  - `RestoreCA` now returns `(written bool, err)`.
  - The new `CheckRestoreCA` makes the same judgement without writing.
  - The refusal for another CA now also tells the operator what to do: remove both CA files and
    restore again, then reissue every bundle the other CA issued.
- **`restore`**
  - `Tar.CA` is new.
  - Validation calls `host.CheckRestoreCA`. A backup holding another CA is refused (exit 70)
    before anything is recorded or staged.
  - The clean-up step (RS8) calls `host.RestoreCA`. This is safe to repeat on a resume: the same
    CA is left as it is. When it writes, it adds the line `CHANGED ca restored from backup N`
    and a note to run `team apply` so the leaf is made again from the restored CA.
  - A backup with no CA gets a note saying the team's CA is left as it is.
- **`admin/wire`**: the restore engine's `Tar` now passes `c.CA` on.
- **Text corrected in the branch**:
  - `docs/agent-bus-cross-network.md`, the CA bullets.
  - The `paths.CAKeyFile` comment.
  - A new "As built" paragraph under X13 in `SPEC-cross-network.md`.

**Old backups (X13 does not cover this; I chose the behaviour, the coordinator should confirm).**

- A version 1 backup restores exactly as before and leaves the team's CA, or its lack of one, as
  it is. The answer's notes say so.
- Why: the existing `RestoreCA` contract already treated "no files" this way, and refusing would
  make every backup taken before this change impossible to restore.

**Restoring onto a freshly re-applied host.**

- A restore needs an applied team, and that apply makes a new CA. So the restore is refused at
  validation, naming both CA files, and the operator must remove them and restore again.
- That is the "a restore never replaces a CA" rule taken literally.
- If the owner wants a restore to replace a freshly made CA by itself, that is a decision to
  take, not something I built.

Red, then green:

- **Red** (`untracked/scratch/b1-red.txt`): the tests did not build, because the API and fields
  did not exist yet:
  - `s.CA undefined (type Sources has no field or method CA)`
  - `c.CA undefined (type Contents has no field or method CA)`
  - `undefined: CheckRestoreCA`
  - `assignment mismatch: 2 variables but RestoreCA returns 1 value`
  - `unknown field CA in struct literal of type Tar`
- **Green**, by package:
  - **backup**:
    - `TestEncodeCarriesTheCA`
    - `TestABackupWithoutTheCAHoldsNone`
    - `TestEncodeRefusesABadCA`
    - `TestDecodeRefusesABadCATar`
    - `TestTakeCarriesTheTeamCA`: take a backup, then restore it into a clean root; the CA
      fingerprint is equal.
    - `TestTakeOfNoCAOrABadCA`
  - **restore**:
    - `TestRunRestoresTheBackupsCA`: restore into a team with no CA; the fingerprint is equal,
      and the CHANGED line and the note are there.
    - `TestRunRefusesABackupOfAnotherCA`
    - `TestRunKeepsTheTeamsCA`
  - **host**: the `RestoreCA` and `CheckRestoreCA` cases, plus the new
    `TestRestoreOfABackupWithoutTheCA` (split out of a test to keep its complexity under the
    lint limit).
- One test failure on the way: `TestTakeCarriesTheTeamCA` first failed because the clean root
  had no `AdminState` directory. A restore only ever runs on an applied team, so the test now
  creates that directory.

## B2 (S16): `host.CAPEM` is the production team CA

What changed:

- **`admin/wire`**: `System.TeamCA` and `SeatBundles.CA` are now `host.CAPEM(root, team)`, read
  at each issue. `teamCANotMade` is deleted. `ErrTeamCANotMade` is now `host.ErrCANotMade`, a
  new sentinel that `CAPEM` wraps when a team has neither CA file.
- **`SPEC-cross-network.md`**: the build order records S16 as built in PR 128. Until now the
  S16 text was only on `feat/crossnet-s14-int`.

Red, then green:

- I first replaced wire_test's pin of the old behaviour (`TestTeamCAIsNotMadeYet`) with
  `TestTeamCAIsTheHostsCA`.
- **Red** (`untracked/scratch/b2-red.txt`): `System.TeamCA of an applied team = the team CA is not made yet; want the CA certificate the host keeps`. The same failure appeared for
  `SeatBundles.CA` and for a CA without its key.
- **Green**: an applied team gets exactly its certificate; a team with no CA gets
  `ErrTeamCANotMade`; a lone certificate gets a refusal naming `ca.key`.
- `host` `TestCAPEM` now also checks `errors.Is(err, ErrCANotMade)`.

**Not proven: an LXC reach fact through the production wiring.** The reach path calls
`sys.TeamCA` (`admin/lxc.go:251`), which is now the wired provider. Its behaviour with a
provider is covered by the existing admin tests, such as
`TestLXCRosterReachCarriesTheTeamCA`. But no test drives a whole reach through `wire`.

## QA results (exact)

| Check                                                                              | B1                                                                                                                                                                                                 | B2                                         |
| ---------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------ |
| `./scripts/qa-changed.bash`                                                        | passed, 14 changed files: docs, go-qa (both modules, diff mode), teamchat-keeps-off-v1                                                                                                             | passed, 6 changed files: same three checks |
| `go test -race -count=1 ./internal/teamchat/... ./cmd/teamchat/...`                | all pass except one run of `member/wiring TestClaudeStopBlocks` under load; it passed 3 of 3 alone and does not import backup, restore or host (the known wiring wait issue, coordinator answer 4) | pass                                       |
| `golangci-lint ./internal/teamchat/...`, untagged and `--build-tags busfailpoints` | 0 issues                                                                                                                                                                                           | 0 issues                                   |

- The project rules ran inside the go-qa run that qa-changed made (the analyzers stage, diff
  mode).
- I did not run go-qa's separate `project-rules-tagged` stage.
- I did not run qa-all, as briefed.

## Defence Before Fix

**Class: production-unreachable code kept alive by its tests.**

- These are functions that are built, tested and documented as working, but that no `main`
  ever reaches.
- The hazard: the PR, the docs and the tests all say the feature works, every gate is green,
  and the feature does nothing in the shipped binary.
- B1 and B2 are both instances of it.

**Why go-qa missed it.** go-qa's dead-code stage runs `deadcode -test -json ./...` (go-qa-ci
`internal/runners/deadcode.go:84`). The `-test` flag counts test mains as roots, so a function
called only by its tests counts as live.

**Proof of red.** I ran deadcode v0.52.0 from go-qa's own tool cache, over `./cmd/...` and
without `-test`.

- At base 8994d156d it reported:
  - `host/tls.go:187 CAPEM`
  - `host/tls.go:209 CABackupFiles`
  - `host/tls.go:224 RestoreCA`
  - `host/tls.go:249 backupCA`
- With `-test ./...`, go-qa's mode, it reported 0 findings.
- After B1 the total fell from 243 to 240; after B2 to 239. No `host/tls.go` finding is left.

**Sweep.** 239 functions remain unreachable from production, 160 of them in `internal/teamchat`.

- Per-package counts at base:

  | Package                                                                                       | Findings |
  | --------------------------------------------------------------------------------------------- | -------- |
  | seat                                                                                          | 47       |
  | paths                                                                                         | 25       |
  | clock                                                                                         | 20       |
  | member/kit                                                                                    | 17       |
  | proto                                                                                         | 13       |
  | member/records, declare                                                                       | 5 each   |
  | host/lxc                                                                                      | 4        |
  | teamlock, pki                                                                                 | 3 each   |
  | status, member/hooktext, member/guest, member/engine, health/view                             | 2 each   |
  | units, tlsterm, restore, member/wire, member/teamsync, member/link, member/cli, member/bundle | 1 each   |

- Outside teamchat: engine 27, execx 20, tokens 10, ttyecho 7, transcript 4, and a few others.

- The full list is in `untracked/scratch/deadcode-after-b2.txt`, with the base list in
  `untracked/scratch/deadcode-notest.txt`; both are in the worktree and not tracked.

Instances that look like this same class inside PR 128 or v2 (my reading, not verified):

- **`seat` `Store.Take`, `Store.Release`, `Store.Announce` and their helpers, plus
  `KeepGuestToken`** (S8, X7-X11, seat key and take): no production verb reaches them. The v2
  launcher is unbuilt, so this may be intended, but the PR claims S8 as built.
- **`declare` `Declaration.CheckSites`, `MachineHostname` and `Check`** (S10, site and
  hostname, X25-X28): unreachable. This looks like a third B-class instance in this PR.
- **`member/kit` `Assemble` and the whole kit build**, plus `member/records` `WriteTeams`.
- **Not defects**: test fakes kept in non-test files (`clock.Fake`, `units.Manager.lostForTest`),
  unused accessors (`pki` `NotAfter`/`Names`, `tlsterm.Server.Addrs`, `Toward.String`), and
  `paths` names not used yet.

Caveat: this deadcode run used the default build only. Code reached only from a tagged build
(`busfailpoints`, `tuwunel`, `teamchatrelease`) shows as unreachable here.

**Gate.** I added none, and I narrowed, allowlisted and suppressed nothing.

- The sweep is far from clean, so a blocking gate is the owner's decision, through the
  coordinator.
- My proposal: a go-qa `[[stage]]` that runs `deadcode ./cmd/...` without `-test`, reported per
  package, once the owner has decided the 239.

## What the PR body (and other text outside this branch) should say

- **B1**:
  - The team backup carries the team CA: manifest version 2, members `ca.crt` (0644) and
    `ca.key` (0600). The backup stays root-only, and a copy taken off the host must be encrypted.
  - A restore puts the CA back where the team has none and then asks for `team apply`. It
    refuses a backup of another CA before changing anything. A version 1 (pre-CA) backup still
    restores and keeps the team's CA.
  - The earlier claim that this already worked was wrong until `542b81a4e`.
- **B2**:
  - `teamchat-admin` now issues `https` seat bundles and LXC reach facts carrying the team CA
    (`host.CAPEM`, S16).
  - "no https bundle in the production build" is fixed. S14's reissue can now build on this.
- **Plan 00209 `SPEC-n2g.md` BK1-BK4** still describes six members, all 0600. It needs a line
  for version 2 and its two CA members. I left 00209 alone.
- **PLAN.md Task 8.8** should record B1 and B2 as fixed on `fix/crossnet-b1-b2-wire`, and S16 as
  built. I did not edit PLAN.md, and I wrote no journal entry.

## Skipped or not verified

- No end-to-end test of an LXC reach through `wire`.
- go-qa's `project-rules-tagged` stage was not run locally.
- One `member/wiring` race failure under load; I did not investigate it further.
- The behaviour for old backups and for a host re-applied before its restore is my choice and
  needs the coordinator's or owner's confirmation.
- Work method: the first edits were blocked by the cross-worktree guard because the session
  started in `cross-network`. I switched into the new worktree with `EnterWorktree`. No edit
  landed in `cross-network`.
