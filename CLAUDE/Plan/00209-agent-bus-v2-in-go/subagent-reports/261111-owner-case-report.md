# Defence Before Fix report: a GitHub name refused for its case (project_identity_fold)

## Specification followed

Method specification 1.1.0 (cached copy, as the dbf skill's refresh script printed it), with
DETECTOR-SPEC 1.0.0 and TOOLING-SPEC 0.2.0 (cached).

## Toolchain

The Go project rules in `src-go/qa/rules`, run by go-qa-ci's analyzers stage through
`src-go/qa/driver` (`go vet -vettool`), catalogued in `CLAUDE/GoProjectRules.md`. No
`defence-before-fix` manifest declaration was found; none was added. The listing is the rule
table `rules.All()` and the catalogue; an identifier resolves through
`go vet -vettool=<driver> help project_identity_fold` and the catalogue section. The single-rule
harness is `analysistest` (`go test ./rules/ -run IdentityFold`) and
`go vet -vettool=<driver> -project_identity_fold ./...`. The only excuse the project allows is a
row with its reason in `src-go/qa/rules/table.go`; that is the calibration followed.

## Defect and class

Originating instance: `src-go/internal/teamchat/declare/values.go` `node.trust()` passed a
declaration's forge section to `proto.DecodeForge`, whose owner grammar
(`proto/refs.go` `ownerPattern`) is lower case only. `teamchat-admin team approve` and
`team apply` (the play's step 5, and `--set trust=`) therefore refused a trust org written as
GitHub shows it (`ExampleOrg`).

Class: a GitHub owner or OWNER/REPO that a person, a flag or a play supplies, handed to
teamchat's lower-case-only grammar without the ASCII fold to the canonical form first.
Hazard: GitHub matches names case-insensitively and shows them in mixed case, so a valid name
is refused with a refusal that does not say case was the problem (or, compared exactly, would
be treated as another owner).

The design keeps the canonical form lower case (DESIGN-v2-A 9.1: "A sender lowercases
OWNER/REPO before sending", ASCII only per A 2.3); the fix keeps the wire and stored form and
folds at the input boundary only.

## Rule

`project_identity_fold` (`src-go/qa/rules/identityfold.go`, doc string and catalogue section
in `CLAUDE/GoProjectRules.md`). Outside package `internal/teamchat/proto`, in non-test files, it
flags a use of `proto.ValidOwner`, `proto.ValidOwnerRepo`, `proto.ParseRef` or
`proto.DecodeForge` not called with a `proto.LowercaseRefRepo(...)` first argument, and a value
written to `proto.Ping.Ref`, `proto.Forge.Org`, `proto.Forge.Exclude` or `proto.ForgeRepo.Repo`
that is not such a call, `""` or `nil`. Message:
`proto.X given a name that is not folded first (project_identity_fold): ...` /
`a name written to proto.T.F that is not folded first (project_identity_fold): ...`.

Excluded as hazard-free: proto's own uses (its canonical decoders of roster, envelopes and
verdicts, which read what was folded when sent); test files; allowance rows (one:
`member/state.validRef`, the member's own state file, written folded).

Next wider rule considered and not built: every lower-case-only GitHub grammar in the tree,
v1's `internal/bus/declare` (`isOwnerRepo`/`checkedRepo`) included. Its query entry points
(`TeamsFor`, `Choices`) take only `GithubRemote`'s already-folded answer (checked: find,
picker, chooser, create wizard), and ccy-teams' create wizard folds `--org`/`--repo` and its
answers, so no person's input reaches them in Go. v1's Python is outside a Go rule (below).

## Proof

Red on the fixture (`src-go/qa/rules/testdata/identityfold/...`, kept as the rule's test) and on
the real tree, committed alone as `c1b86fabe` before any fix:

```
go vet -vettool=<driver> -project_identity_fold ./...   (src-go)
internal/teamchat/declare/values.go:141:18: proto.DecodeForge given a name that is not folded first ...
internal/teamchat/member/state/store.go:463:17: proto.ParseRef given a name that is not folded first ...
internal/teamchat/member/engine/reply.go:92:11: a name written to proto.Ping.Ref that is not folded first ...
internal/teamchat/member/engine/send.go:131:11: a name written to proto.Ping.Ref that is not folded first ...
```

## Independent search

Dispatched before the rule existed, without its pattern:
`subagent-reports/261111-owner-case-search.md`. It found the same three Go hazard sites the
rule found (trust decode, ping and reply `--ref`; `LowercaseRefRepo` had no production caller)
and classified `store.go validRef` as a canonical stored read, which is the allowance row. The
rule missed nothing the search found in Go, so it was not widened. Text search alone found the
grammar sites, the missing fold caller and every existing fold; reading alone established which
inputs are a person's and which are canonical (the three paths into `trust()`, the `--ref` flow,
`GithubRemote` feeding v1's queries). The search also found the v1 Python instances below.

## Sweep and count

Scope: all first-party Go in both modules (`src-go`, `src-go/qa`), vendor and testdata excluded,
untagged and with every build tag (`busfailpoints,tuwunel,teamchatrelease,ccyteamstest,difftest`).
Four findings; three carry the hazard, one is a canonical read (allowance row). Python, bash and
playbooks were searched by the independent searcher: no bash or playbook validates a GitHub
name; Python instances are in v1 only (below).

## Fixes

Each examined individually:

- `proto.DecodeForgeInput` (new, `proto/roster.go`): folds `org`, `repos[].repo`, `exclude[]` in
  ASCII, then validates; a name that is no GitHub name in any case is refused as
  `org is not a GitHub owner: 1-39 ASCII letters, digits and hyphens, ...` (or the OWNER/REPO
  form, with the field index), never echoing the value. Branches and path prefixes keep their
  case. `declare/values.go` `trust()` uses it (covers `team apply`, `team approve`, `--set trust=` and the stored `approved.json`, where folding a canonical value is a no-op).
- `member/engine/send.go` (`ping`) and `reply.go` (`answerPing`): `p.Ref = proto.LowercaseRefRepo(*a.Ref)`, as A 9.1 says a sender does.
- `member/state/store.go validRef`: allowance row with its reason; no code change.

No instance was suppressed or left with the hazard in place.

## Decisions referred to the owner

The v1 Python agent bus carries the same class, outside the Go rule's language, and is not fixed
here (count fixed 3 Go, remaining v1 Python 2 sites plus one Go v1 compare):

- `helpers/agent_bus/teamfile.py` (`_org`, `_owner_repo`, `_exclude`) refuses a mixed-case
  `org`/`repo` from the `agent_bus_teams` play variable ("must be a lower-case GitHub owner");
  `helpers/agent_bus/declare.py` `--org`/`--repo` (and `_checked_repo`) pass a direct CLI value
  to it unfolded. ccy-teams' wizard folds before calling, so only the play variable and a direct
  CLI run are exposed; the refusal does name lower case.
- `src-go/internal/bus/declare/queries.go` `standing()` compares host_vars text exactly with the
  folded checkout owner (the searcher's "unsure"; it mirrors the Python, which refuses the
  mixed-case value at apply).

What stopped me: v1 is the bus Plan 00209 replaces; the Go v1 port is held to the Python by
parity oracles, so changing v1's contract is the owner's call, and a Python defence would be a
semgrep rule in `.semgrep/` with its fixture. To fix: fold with ASCII lower in `teamfile.py`'s
three readers and `declare.py`'s flag handling, a semgrep rule for a lower-case owner grammar
reached without `.lower()`, and the Go port's `checkedRepo`/`standing` changed in step with its
parity test. The Go rule is merged at full width for its language; nothing in it is narrowed.

## Permanence

`IdentityFold` is in `rules.All()` (driver-registered, so the go-qa analyzers stage and the
tagged `project-rules-tagged` stage run it; every finding fails the gate). Targeted QA:
`./scripts/qa-changed.bash --base HEAD~1` over the red commit and the fix: every check passed
(docs, the ccy suites the version bump names, and go-qa's fmt, golangci, analyzers with this
rule, deadcode, the failpoints lint and vet, project-rules-tagged, cross-build) except go-qa's
`vuln` runner, which fails repo-wide on GO-2026-6859 (Go stdlib, needs go1.26.11, an owner
decision pending); that failure is not from this change. Because go-qa stops at `vuln`, its test
runner did not run, so `go test -race -shuffle=on ./internal/teamchat/...` and
`go test -race ./rules/` were run directly.

## Toolchain and detector gaps

go-qa-ci expects one documentation page per analyzer under its `docs_dir`; the project's
catalogue stands in for it (a known, recorded gap in `GoProjectRules.md`). No DBF manifest
declaration exists in the project.

## The original defect

Fixed as above. Tests, each seen red before the fix: `proto`
`TestDecodeForgeInputFoldsGitHubNames` and `TestDecodeForgeInputRefusalNamesWhatIsAllowed`;
`declare` `TestDecodeFoldsTrustNames` (refused "not a valid forge section (teamchat roster
invalid: forge)" before), `TestDecodeTrustRefusalNamesWhatIsAllowed`, approve case "set the
trust as GitHub shows it" and refusal "trust unchanged in another case"; `engine`
`TestAPingsReferenceIsSentFolded` and the typed `done --ref` in `TestSD1_2...` (refused `ref`
before; now a duplicate of the canonical answer). CCY 3.118.3 / container 2.84.2 with a
`docs/ccy-changelog.md` entry; DESIGN-v2-C 3.8 notes the input fold. Not changed: a reference
that is malformed in any case is still refused with the generic "the send was refused (ref)",
which does not say what a reference may be.
