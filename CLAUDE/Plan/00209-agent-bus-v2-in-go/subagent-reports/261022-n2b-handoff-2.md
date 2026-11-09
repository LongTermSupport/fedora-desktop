# N2 slice n2b: handoff 2

Worktree `/workspace/untracked/worktrees/bus-v2-n2b`, branch `feat/bus-v2-n2b`. The second
builder stopped at its context budget with every code item of the slice built. Read
`SPEC-n2b.md` (the contract) and the first handoff, `261022-n2b-handoff.md`, for the gotchas.

## Done (committed, each with qa-changed red only on govulncheck GO-2026-6809)

| Commit   | What                                                                                  | Coverage                             |
| -------- | ------------------------------------------------------------------------------------- | ------------------------------------ |
| a8235bbb | `state` to the floor: a `chown` seam, temporary names from `crypto/rand.Text`         | state 90.7%                          |
| 732252e3 | `declare/gate.go`, AG1-AG7                                                            | declare 95.6%                        |
| 1f48f939 | `declare/hostvars`, HV1-HV3; `go.yaml.in/yaml/v3` v3.0.5 vendored, its allowlist row  | hostvars 96.3%                       |
| 694172a9 | `qa/godeps` keepout kind (GD1-GD3), the row, `SPEC-qa-go-deps.md` G14                 | godeps 98.1%                         |
| cb6517d9 | `apply`, AP1-AP12; `paths.RosterFile`, `paths.RoomsFile`; `admin.ExitCodeOf` mappings | apply 92.9%, admin 97.3%, paths 100% |

golangci-lint (`golangci-lint run ./internal/teamchat/...` from `src-go`, and with
`--config ../.golangci.yml` from `src-go/qa`) is clean. `qa-go-deps` passes; govulncheck reports
nothing against the YAML module (its one module finding is the existing `golang.org/x/text`
GO-2026-6629, not called). `go list -deps` over `./cmd/teamchat-admin` holds no YAML.

## Left

1. The review loop: one adversarial Opus reviewer over `git diff 3d7d78b6..HEAD` (plus the
   first builder's `c09c379e`), against SPEC-n2b and C 3.5.1, 3.6.1-3.6.5, 3.8, DC17, DC18. It
   writes `subagent-reports/261022-n2b-review.md` (MERGE or FIX-FIRST, numbered). Fix, loop, at
   most 3 rounds, commit each review file.
2. `subagent-reports/261022-n2b.md`: what was built, coverage, decisions, design-text fixes.
3. PLAN.md progress lines for Tasks 2.3 and 2.4.

## Decisions taken this session (for the report)

- **n2b-D12.** The roster file's one intent state is `claimed` (a handle `member add` claimed
  before its account exists): C 3.6.1 says "the intent states below" and names none. A member
  `kind` is `^[a-z][a-z0-9-]{0,31}$`; `cred` is `password`, `token` or null; a member's `needs`
  is null or `reissue`, a human's null or `password`.
- **n2b-D13.** Reading the two files is its own failure step, `files` (AP11's list has none for
  it).
- **n2b-D14.** `apply.Run` judges every declared human's account (admin or deactivated is 70)
  before the first change, so a refusal changes nothing. An account that exists but is not in the
  file is recorded `active` with `revoked_seq` 0 and prints nothing.
- **n2b-D15.** The roster `rev` the engine wrote is stored even when the engine then fails (C
  3.6.1: the file's rev follows the server's acceptance).
- **n2b-D16.** `DeriveRoster` validates with A's `Roster.Validate` using placeholder room IDs and
  `rev` at least 1, so a roster A refuses (no active human, say) stops before any room is
  created; the engine's own decode before the write stays.
- **n2b-D17.** AP12's "the roster event rendered from the approved team while a different `trust`
  sits in the input" cannot happen through `Run` (the gate refuses any difference), so it is
  `TestAsApproved` over `asApproved`, which takes the gated scalars from the approval.
- **n2b-D18.** AP12's VerifyMain needs a member to verify as: the new-team test checks the files
  and the second run; VerifyMain runs once members are in the file (the wanted coordinator's
  promotion test).
- **n2b-D19.** `hostvars` refuses a merge key and an alias key at the top level too (either could
  carry a v2 key not written as one), more than one YAML document, and a `host_vars/<host>/`
  directory beside the file. It reads `teamchat_address`, `teamchat_teams`,
  `teamchat_lxc_members` and `teamchat_element_teams`; `teamchat_migrations` is D's and ignored
  here. A plain scalar is judged by PyYAML's YAML 1.1 resolver (Ansible's) and the YAML 1.2 core
  schema, so `192.0.2.10` stays a string.
- **n2b-D20.** An approve refusal is `declare.RefusedError` for every case (a bad op or value, a
  change that changes nothing, a mismatch), all exit 70 through `admin.ExitCodeOf`.
- **n2b-D21.** A one-change `lxc_members --add` of a container and user already present (with the
  other `human_text`) is refused: that is `--set`.
- The tagged Tuwunel test of AP12 is not built (not cheap within this budget): say so in the
  report, or add it from `rooms/tuwunel_test.go`'s `realAdmin`.

## Design-text fixes to propose

- C 3.6.1: name the member intent states (D12).
- SPEC-n2b AP11: add the `files` step (D13); AP12: the two test reshapes (D17, D18).
- C 3.8 / HV2: the top-level merge and alias key refusals (D19).
