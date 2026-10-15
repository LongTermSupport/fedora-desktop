# Review: branch `go-qa-ci/adopt` (Plan 00191 Task 3.3)

Adversarial, read-only review of `origin/integration/261014...origin/go-qa-ci/adopt`. go-qa-ci was
read at the pinned commit `803b122ce526` (the tip of its default branch; it has no tags). Nothing
was edited, committed or pushed. A temporary worktree of the branch was used to run the Go tests
of both modules, to build the pinned `go-qa` offline from the module cache, and to run its cheap
steps (`fmt`, `record`, `suppression`, `summary`, `docs`, `analyzers`, `golangci`) over `src-go`;
all passed. `qa-all.bash` and Ansible were not run.

Severity: **BLOCK** merge-stopping; **FIX** must land before or with the merge; **NOTE** judged
acceptable, recorded so the decision is visible.

## BLOCK

### B1. The go gate rewrites source files outside CI and reports a pass

- `scripts/qa-gates.tsv:27` (`go-qa --root {root}/src-go run`),
  `src-go/qa/changed/checks.go:900` (`go-qa ... run --changed --base <ref>`).
- go-qa's `fmt` step is **autofix by default**: `internal/tools/golangci.go:90`
  `check := !e.Ctx.Fix && !e.Ctx.CI`, with `Fix = !opts.noFix` (`internal/cli/run.go:125`). Only
  `--no-fix`, or CI mode (`CI` env set, or `--ci`), makes it a check. Outside CI it runs
  `golangci-lint fmt` without `--diff`, the files are rewritten, the diff is empty, and the step
  prints `passed`.
- Scenario: a developer or agent has an unformatted `.go` file staged and runs
  `./scripts/qa-changed.bash`. The go check reformats the file **unstaged**, prints `passed`, the
  commit lands with the unformatted staged copy, and CI (where `CI=true` makes go-qa snapshot the
  tree and fail on a rewrite) goes red. Same for `qa-all.bash` locally: the gate mutates the tree it
  is judging and passes. `qa-go.bash` ran `golangci-lint fmt --diff` and failed on any diff; this is
  a lost guarantee, and the only one found.
- Fix: add `--no-fix` to both commands (`go-qa --root {root}/src-go run --no-fix` in the gates row;
  `"--no-fix"` in `runGoGate`). Update `TestTheGoCheckRunsGoQAInDiffModeAgainstTheBase`
  (`checks_test.go:959`), the fake key in `realtable_test.go:831` (`keyOf` joins every word) and
  the `TestGatesRunTheirScriptFromTheScriptsDirectory` family. State in `CLAUDE/QA.md` that the
  gate never writes.

### B2. The record's signatures are the agent's, and the agent set its own trust tier

- `src-go/qa/record.toml:7-9` (`[ratchet] decided_by = "repository-owner"`,
  `decided_on = 2026-10-15`) and every `[[exception]]` (`decided_by = "repository-owner"`,
  `added_by_model = "claude-sonnet-5-5"`); `src-go/qa/go-qa.toml:23-26`
  (`[record.trust.models] claude = "high"`).
- go-qa-ci's record model (`docs/record.md`, "Deciders", "provenance and trust") makes
  `decided_by` the owner's signature: it must be a configured decider for the ratchet, and the
  review horizon comes from the author tier. Nothing in go-qa can tell an owner-typed
  `repository-owner` from an agent-typed one, so the tool reports `19 exceptions, all valid` on a
  record no owner has read. The branch's own journal says the signatures "are the owner's to
  confirm before the merge"; the file carries no such mark.
- The tier is self-assigned, and more generous than the toolchain author's own precedent:
  go-qa-ci's `qa/go-qa.toml` puts `claude-sonnet = "medium"` (30-day review, at most three
  entries) for the same model family. This branch chose `claude = "high"` (90 days, no count cap)
  for nineteen entries written by that model in one sitting. That is the one knob the owner is
  meant to hold, turned by the party it limits.
- Fix (owner's step, not the agent's): run `go-qa --root src-go record list`, read the nineteen
  justifications, decide the tier in `go-qa.toml` (`medium` with a raised
  `[record.trust.medium] max_count`, or `high` by explicit choice) and re-sign `decided_on`
  /`review_by` in a commit of the owner's own. The ratchet's `decided_on` must also be the owner's
  (see F5 for why the date matters). Optionally add a `[[record.trust.cosign]]` category for
  `golangci.yml::*` rules with `min_tier = "human"`, so a configuration exclusion can never again
  be licensed by a model alone.

## FIX

### F1. gitleaks and actionlint are claimed as "tools go-qa runs"; go-qa never runs them

- `.claude/ccy/Dockerfile:26-31`, `.github/workflows/qa.yml:87-90`,
  `playbooks/imports/play-python.yml:213-217`, `.qa-versions:30-35`, `docs/playbooks.md:331`,
  `CLAUDE/QA.md` go-qa row, `CLAUDE/Plan/meta-deploy.list:34-36`.
- `grep -rl 'gitleaks\|actionlint' internal cmd` in go-qa-ci returns nothing: those two are
  pinned in `tools.lock` for go-qa-ci's **own** CI (`public-safety.yml`, `ci.yml`), not for
  `go-qa run`. The claim is false in six places and makes the host install two binaries into
  `/usr/local/bin` that nothing in this repository calls.
- Side effects of calling `install-tools.sh` with no tool list: it also manages `golangci-lint`
  from `tools.lock`, a **second pin** for a tool `.qa-versions` already pins. Today both say
  2.14.0. On a future `GO_QA_CI` bump whose `tools.lock` moves golangci-lint, the Dockerfile and
  the play **overwrite** `/usr/local/bin/golangci-lint` (GOBIN is `/usr/local/bin`), and the
  toolchain gate then fails on `GOLANGCI_LINT` — loud, but a confusing failure three layers away
  from its cause. The gitleaks CI job (`qa.yml:201`) also carries its own `VER=8.30.1`.
- Fix: name the tools: `install-tools.sh deadcode govulncheck` plus whatever F2 decides for
  `ast-grep` and `gremlins`; correct the six comments and the docs; keep golangci-lint under
  `.qa-versions` only.

### F2. ast-grep and gremlins are installed, unused, and unverified

- `internal/tools/astgrep.go:75-77`: with no `sgconfig.yml` and the (empty) bundled pack, the
  step returns `Skipped` **before** `astgrep.Locate()` is called. In this repository a missing
  `ast-grep` is therefore never noticed by `go-qa run`; the "fails with ast-grep is not installed"
  guarantee in `docs/astgrep.md` only applies once a rule exists. The pass is by design and
  honest (the summary says `skipped`), but the Dockerfile, play and CI comments present ast-grep
  as a running defence. `mutants.mode` is `off` (the default), so `gremlins` is never started.
- `scripts/qa-toolchain.bash` checks go-qa only; the qa-changed preflight (`checks.go:887`) names
  `deadcode` and `govulncheck` but not `ast-grep` or `gremlins`. So two of the six tools have no
  verifier anywhere in this repository.
- Fix: either (a) leave them out of the install list until a rule or a mutation threshold exists,
  and say so where the list is written; or (b) keep them and add them to the toolchain gate so the
  three consumers are provably identical. Add a Plan 00191 task for `mutants.mode = "diff"` with a
  threshold, so "off" is a tracked decision, not a default nobody chose.

### F3. `GO_QA_CI` and `src-go/qa/go.mod` are two pins of one commit, with no convergence check

- `.qa-versions:36` (`GO_QA_CI=803b122ce526`) and `src-go/qa/go.mod:6`
  (`v0.0.0-20261014082313-803b122ce526`). The `go-qa` binary comes from the first; the project
  driver (`src-go/qa/driver`, `driver.Main`) is built by go-qa from the second. If they diverge the
  analyzers step runs a driver from a different go-qa-ci than the binary driving it, with whatever
  protocol drift that brings. `scripts/test-qa-tool-pin-converges.bash` covers the pipx tools only.
- Fix: assert in the toolchain gate (or in that suite) that the go.mod require's pseudo-version
  ends with `-${GO_QA_CI}`.

### F4. The Go floor is read from the wrong module

- `src-go/qa/toolchain/run.go` floors `go` on `src-go/go.mod` (`go 1.26`), while
  `src-go/qa/go.mod` and go-qa-ci itself say `go 1.26.9`. A host whose `golang` package is below
  1.26.9 passes the toolchain gate, then `go install go-qa-ci@...` under `become: true` in
  `play-python.yml:225` triggers a silent `GOTOOLCHAIN` download as root (or fails outright with
  `GOTOOLCHAIN=local`). CI already reads `src-go/qa/go.mod` (`qa.yml:79`), so the three consumers
  disagree on which directive is the floor.
- Fix: floor on the higher of the two directives, and say which in `CLAUDE/QA.md`.

### F5. The ratchet signature has a 14-day fuse, and `history_base` is tied to one branch name

- `src-go/qa/go-qa.toml:21` `history_base = "origin/F44"`; `record.toml:9`
  `decided_on = 2026-10-15`.
- `docs/record.md`, "The ratchet": creating a record is a raise from zero, judged against the
  merge base with `history_base`; a raise is accepted only while `decided_on` is at most 14 days
  old. The branch is waiting on go-qa-ci becoming public, with no date. From 2026-10-29 CI on this
  branch fails `gqaci.record.ratchet` until the ratchet is re-signed; after the merge the record
  exists at the merge base and the check is quiet again. The owner's re-sign in B2 should be done
  on the merge day.
- `origin/F44` is the current default branch under the per-version default-branch model. When
  F45 becomes default, `history_base` no longer resolves to a merge base and CI fails with the same
  finding until it is edited. Add it to the release checklist in `CLAUDE/Release.md`.

### F6. `qa-changed` is narrower than before and the document says so only in part

- `checks.go:900` runs `--changed`: per go-qa-ci's README table, `coverage` applies the 90% floor
  **to the affected packages only**, and `golangci`/`analyzers`/`test` run on the changed packages
  and their importers. `qa-go.bash` ran the whole gate for any `src-go/` change. So a pre-commit
  run can pass a change that drops the module below the floor, or fail one package that sits at
  85% inside a module at 94%. `CLAUDE/QA.md:1100` describes the scope but not the coverage
  caveat; go-qa-ci's own words are "the full run stays the gate".
- Fix: add the coverage sentence to that row, and keep `qa-all.bash` as the push gate (it is).

### F7. Documentation drift

- `CLAUDE.md:219`: the `go-code.md` row still says "the interim Go gate".
- `SPEC-qa-all.md` B33 row has three cells where B30-B32 have four (no Fix column).
- `record.toml`: "only this file are exempt" (twice) — the justification is what the owner reads.
- `src-go/qa/go-qa.toml` says nothing about `--no-fix`/`--ci`; after B1 it should.

## NOTE (judged acceptable)

- **Lost guarantees, checked one by one.** fmt: kept (but see B1). vet: kept through golangci's
  `govet: enable-all: true`, and the project driver still runs as `go vet -vettool`.
  `go test -race`: kept and stricter (`-race -shuffle=on -count=1`). Coverage floor: kept, 90%
  per module (`fail_under_statements`), measured with `-coverpkg=./...` (cross-package coverage
  now counts, so the number can move up; it cannot hide an untested package better than before
  because of the next item). golangci-lint over both modules: kept; the `src-go/qa/.golangci.yml`
  symlink works — `go-qa run -t golangci` and a direct `golangci-lint run ./...` in the nested
  module both pass with the `qa/...` path exclusions honoured. Project rules over both modules:
  kept (`modules = ["qa"]`, `-t analyzers` passed). Every-package-has-tests: kept as
  `project_package_tests` with red and green fixtures (`rules/testdata/src/fix/internal/notests`,
  `withtests`); in diff mode a new or test-stripped package is a changed `.go` file, so it is in
  scope. Gained: deadcode, govulncheck, `go mod tidy -diff`/`verify`, the bundled analyzers
  (which found three real map-order defects and nine `os.Exit` sites), and the record.
- **Loud failure.** A missing `go-qa`: qa-all fails the go gate with `cannot run go-qa` and the
  toolchain gate too, every other gate still runs (`TestAGoQAMissingFromPathFailsTheRunLoudly`);
  qa-changed's preflight refuses to start. A missing `deadcode`/`govulncheck`: go-qa errors (`A required program that is not installed is an error, never a skipped check`,
  `internal/runners/command.go`). Dockerfile `set -eux`, CI `set -euo pipefail`, the play's
  `command` tasks all stop on a non-zero exit. The `failed_when: false` on the version probe is
  annotated and its result is consumed by the `when:`.
- **Pinning.** Exact: a 12-character commit, resolved by the Go proxy to one pseudo-version,
  verified against `sum.golang.org` and `go.sum` (`h1:EQuc...`); `tools.lock` pins the `[go]`
  tools by version (sumdb) and the release archives by sha256 that is never taken from the
  publisher's checksum file. `go-qa --version` prints the pseudo-version
  (`v0.0.0-20261014082313-803b122ce526`), the toolchain gate reads its last `-` field, and a
  locally built `go-qa` reports `0.0.0-dev` and fails the gate — correct. Verified:
  `TOOLCHAIN-OK 6 tool(s)` with the pinned binary on PATH. The module zip holds
  `scripts/install-tools.sh` and `tools.lock`, so the "install from the module download" route is
  sound; `go list -m -f {{.Dir}} mod@ver` outside a module works (checked).
- **The 90-day review is enforced**: `gqaci.record.expired` fails any entry past `review_by`
  (2027-01-13), locally and in CI. It is the signature, not the enforcement, that is the problem.
- **Golden re-pins.** B33 pins 139 of 141 qa-all scenarios; every pre-existing deviation file
  changed by exactly the go stage line (4-line diffs), and the 100-line additions are scenarios
  that previously matched the capture byte for byte. The `asGoQA` substitution synthesises the
  stub's output rather than re-recording; honest, if heavy. B48 pins 17 of 83 qa-changed
  scenarios, with the recorded `qa-go.bash` exit mapped onto `go-qa`. Neither masks a behaviour
  change beyond the one each names.
- **CCY image.** `.claude/ccy/Dockerfile` is the project image, rebuilt on its own hash
  (`CLAUDE/ContainerRules.md:48-55`); the version bump rule is for the base image and the
  launcher. No bump needed. The image build fails loudly while go-qa-ci is private
  (`go install` cannot fetch), as does the host play and CI; the `meta-deploy.list` entry says it
  cannot run before publication, and only reaches the host checkout with the merge.
- **CI while private.** `go install` exits non-zero under `set -euo pipefail` with the proxy's
  "not found" / "unknown revision" text. Clean, but it does not say "private"; a one-line
  `echo` before the install naming the dependency would save the next reader a search.
- **`CI` env.** GitHub sets `CI=true`, so there go-qa is in CI mode (ratchet strict, fmt fails on
  a rewrite). Locally it is not; B1 is the consequence, and after `--no-fix` the two differ only
  in the ratchet's in-place lowering (which writes `record.toml` when the record shrank — a gate
  writing a tracked file; acceptable only because it is a shrink, and worth a sentence in QA.md).

## Verification run

- `go test ./...` in `src-go` and `src-go/qa` of the branch worktree, offline from the module
  cache: pass.
- `go-qa` built from the pinned module offline; `go-qa --root src-go record check`: 19 valid;
  `go-qa run -t fmt -t record -t suppression -t analyzers -t docs -t summary` and `-t golangci`
  over `src-go`: all passed.
- `scripts/qa-toolchain.bash` with that `go-qa` on PATH: `TOOLCHAIN-OK 6 tool(s)`.
- Not run (tools absent in this container, by design): deadcode, govulncheck, test, coverage,
  mutants.
