# Review: Task 2.1 vendored Go dependencies (branch `feat/go-vendored-deps`, 92e085d8)

Adversarial pre-merge review against local F44 (`git diff F44...92e085d8`: 56 files outside
`src-go/vendor`, 674 vendored files). Developer's report:
[261016-vendor-infra-opus.md](261016-vendor-infra-opus.md); spec:
[SPEC-qa-go-deps.md](../SPEC-qa-go-deps.md). Nothing in the branch was changed except this file.

## Verdict: MERGE

No blocking item. Every tamper I tried fails the go-deps gate with exit 1 naming the file; the
go-vuln gate is exit 2 offline (proved under `unshare -rn`), never a pass; the host build
compiles from `vendor/` with no network; the merge dry run is clean. The non-blocking items are
wording and hardening, listed with file:line below.

## What was run

| Check                                                                                    | Result                                                                                                                                           |
| ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `scripts/qa-go.bash`                                                                     | `GO-OK 61 package(s)`, coverage 91.3%, qa module 57 packages at 94.4%, project rules clean                                                       |
| `scripts/qa-go-deps.bash`                                                                | `GO-DEPS-OK 18 module(s) allowlisted (1 at a pseudo-version, 0 replaced); ... vendor/ in sync (674 files)`, 0.5 s                                |
| `scripts/qa-go-vuln.bash`                                                                | `GO-VULN-OK`, 1.8 s; govulncheck v1.9.0, DB updated 2026-10-08                                                                                   |
| both gates under `unshare -rn` (no network)                                              | go-deps exit 0 (warm module cache); go-vuln exit 2 `govulncheck could not judge (status 1)`                                                      |
| `scripts/test-ccy-container-version-hook.bash`                                           | passed 90, failed 0, including both new secret-scope probes                                                                                      |
| `qa-bash-limits`, `qa-docs`, `qa-bash`, `qa-python`, `qa-toolchain`, `qa-version-pins`   | all green on the branch                                                                                                                          |
| `qa-patterns`                                                                            | exit 2 (semgrep timeouts) on the branch AND on F44, with load 36 on 22 cores; the timed-out files are not in this diff: pre-existing, load-bound |
| `gitleaks dir .` (8.30.1, CI's pin)                                                      | 1 finding, `CLAUDE/Plan/00203-.../test_hs_journal.py:348` (generic-api-key), already on F44 (commit 3222cb1a); `src-go/vendor` alone: no leaks   |
| host-style build `GOFLAGS=-mod=vendor GOTOOLCHAIN=local GOPROXY=off` under `unshare -rn` | `go build ./cmd/ccy-teams` exit 0; `go vet ./internal/ui/frame` exit 0                                                                           |
| `git merge-tree --write-tree F44 92e085d8`                                               | clean, no conflicts                                                                                                                              |

### Tampering the go-deps gate (scratch copy of `src-go`, `--root` pointed at it)

| Tamper                                                                                            | Verdict                                                                                                                |
| ------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| extra file `vendor/github.com/rivo/uniseg/planted.txt`                                            | exit 1: `vendor/.../planted.txt is not what go mod vendor writes`                                                      |
| a line appended to `vendor/modules.txt`                                                           | exit 1: `vendor/modules.txt differs`                                                                                   |
| edited vendored `.go` with `GOFLAGS=-mod=mod GONOSUMDB='*' GOSUMDB=off GOPROXY=off GOPRIVATE='*'` | exit 1: `vendor/.../doc.go differs` (the hostile environment changed nothing)                                          |
| a go.sum `h1:` hash replaced                                                                      | exit 1: tidy and vendor both report `SECURITY ERROR ... checksum mismatch`; **`go mod verify` itself passed** (see N1) |
| empty `GOMODCACHE`, online (CI's situation)                                                       | exit 0 after downloading, verified against go.sum                                                                      |
| `vendor/` deleted                                                                                 | exit 1 with 674 findings                                                                                               |
| `GOVULNDB=http://127.0.0.1:9/` in the environment (go-vuln)                                       | exit 0: govulncheck 1.9.0 does not read that variable; only `-db` does, which the gate never passes. Not an attack.    |

## The nine attacks

1. **Secret-scan exemption** (`scripts/git-hooks/pre-commit:104`). The exemption is a pathspec,
   so any file under `src-go/vendor/` is committed unscanned by the hook. It is not content-scoped
   and does not check vendor against the module cache before applying. qa-go-deps then fails the
   tree (an extra or edited file is a finding, proved above), and `qa-changed` routes
   `src-go/vendor/*` to it (`scripts/qa-routing.tsv:292`; the routing glob crosses slashes,
   `src-go/qa/changed/discover.go:25`), so such a commit cannot pass the push gate; but the
   commit exists first. The hook is an anti-accident control, not a security boundary (it is
   bypassed by `--no-verify`), and nobody writes under `vendor/` by hand, so this is a hardening
   item, not a block (N2). The test is sufficient for what the hook does: it drives the real hook,
   red before the change, and proves the same address outside `vendor/` is still rejected. The
   version gates still see every path (`STAGED_CHANGE_LIST`, the next line, is unfiltered).
   gitleaks in CI: `gitleaks dir .` with no path filter, same version as mine; over
   `src-go/vendor` it finds nothing, so the vendored emails will not redden CI. The one gitleaks
   finding in the tree is Plan 00203's, already on F44 (N6).
2. **The go-deps gate.** Allowlist strict (G1, tested), modfile parse (G2), G3-G6 proven by
   `judge_test.go`; the pins are tags except ultraviolet; the byte-identical compare includes
   `modules.txt`. `GOFLAGS=`, `GOWORK=off`, `GOTOOLCHAIN=local` are appended last to
   `os.Environ()` (`src-go/qa/godeps/run.go:49`, `qa/internal/proc/proc.go`), and `exec` keeps the
   last value, so a caller's `-mod=mod` is overridden. `GONOSUMDB`, `GOPRIVATE`, `GOINSECURE`,
   `GOPROXY`, `GOSUMDB` are **not** pinned. They govern only the sumdb check of a download whose
   line is absent from go.sum; a module present in go.sum is verified against go.sum regardless,
   and a module absent from go.sum is itself a `tidy -diff` finding. So a hostile environment can
   at most change the wording of a failure, not produce a pass. Hardening item (N3).
3. **The go-vuln gate.** Needs the network every run: govulncheck fetches `vuln.go.dev` and
   caches nothing (no `vulndb` under `GOMODCACHE`). Offline it is exit 2 with the fetch error,
   proved. CI runners have the network; a flake there reddens the run, which is the fail-closed
   behaviour asked for. Pinned: `tool golang.org/x/vuln/cmd/govulncheck` with
   `golang.org/x/vuln v1.9.0` in `src-go/qa/go.mod` and its `h1:` lines in `src-go/qa/go.sum`;
   `go tool govulncheck -version` reports `govulncheck@v1.9.0`. Nothing installed system-wide.
4. **ultraviolet pseudo-version.** Justified: bubbletea v2.1.0 requires
   `20260811164956-006e29f97886`, lipgloss v2.0.6 requires `20261008173134-6b8d4baf91b4`, bubbles
   v2.2.1 requires `20260703014108-f5a850f9c2b7`; Charm has tagged none. MVS selects the newest,
   which is `src-go/go.mod:16`, with its own `pseudo` row (`src-go/go-deps.allowlist:25`) saying
   so and asking for a re-check at every Charm bump. Pinned by go.sum like every other module.
5. **toolchain go1.26.9.** Matches the CCY image (`.claude/ccy/Dockerfile:152`
   `ARG GO_VERSION=1.26.9`) and the container's `go version`. CI: `actions/setup-go@v5` reads the
   `toolchain` directive when present (its README: "If the toolchain directive is present, its
   version is used"), so CI gets 1.26.9 too. Auto-download: the gates and
   `tasks/build-go-command.yml:92` set `GOTOOLCHAIN=local`, so no download there. A developer
   running plain `go` on a host whose Fedora `golang` is older than 1.26.9 would, at the default
   `GOTOOLCHAIN=auto`, fetch go1.26.9 from the module proxy into `GOMODCACHE`: a network fetch
   into the user's cache, not a system-level install, and not on any path this branch adds. The
   host play's own build is unaffected (`local` and `go 1.26.0` is the floor, as before).
6. **tasks/build-go-command.yml.** `GOFLAGS` is `-mod=vendor` for a src-go build and
   `-mod=readonly` for `go_command_module` (md-viewer), line 98; the external build runs
   `go install` from `~/.cache/ccy-go-build`, outside src-go, so the new go.mod plays no part in
   it. Offline build proved above. The deployed binaries do not import `frame`, so no
   meta-deploy entry is needed; the next `play-claude-yolo.yml` builds the same bytes from
   `vendor/`.
7. **Public repo.** Grep of the added lines outside `vendor/` for home paths, usernames, hosts,
   IPs, non-example emails and container IDs: nothing install-specific. The vendored third-party
   contact addresses are the authors' own.
8. **Branch hygiene.** Noted only: local F44 is 28 commits ahead of `origin/F44` as this
   container sees it (its fetch may be stale), and F44's CI is red on every recent run for a
   pre-existing reason unrelated to this branch (`TestDefaultSession`: `mkdir /run/vsftpd: permission denied` on the runner). Nothing in this branch's one commit worsens either.
9. **Bash limits and TSV wiring.** Both wrappers are 22-line `exec`-style wrappers over
   `lib/go-tool.bash`; `BASH-LIMITS-OK` with the allowlist unchanged. `qa-gates.tsv` rows carry
   `detail:` patterns matched by the real pass lines; `qa-routing.tsv` routes the files, the
   allowlist as `covered`, and both new gates are in the `perfile-gate` list; `qa-trees.tsv`
   excludes `src-go/vendor` from the shell and python gates (qa-docs needs no exclusion: it
   judges tracked `.md` by its own scope and passed). `qa-all`'s replay has the two gates as
   added after the port (`addedgates_test.go`) with wording fixtures; `qa-changed`'s golden has
   deviation B48. qa-docs green, so QA.md's gate inventory matches the TSV.

## Non-blocking items

- **N1. `go mod verify` is misdescribed.** It compares the module cache against the hashes
  recorded at download time, not against go.sum (`go help mod verify`; proved: a tampered go.sum
  hash passed `verify` and was caught by `tidy -diff` and `vendor`). Fix the wording in
  `src-go/qa/godeps/run.go:185` ("a module in the cache does not match go.sum"),
  `SPEC-qa-go-deps.md:41` (G7 "matches go.sum"), and the `go mod verify` mentions in
  `src-go/CLAUDE.md:84,277`. The gate's verdict is right; the sentence is wrong.
- **N2. The hook exemption is path-scoped, not content-scoped** (`scripts/git-hooks/pre-commit:104`).
  Cheap hardening: when any staged path is under `src-go/vendor/`, also require
  `src-go/vendor/modules.txt` to be staged and run `scripts/qa-go-deps.bash` (0.5 s warm) before
  accepting, so the exemption applies only to a tree `go mod vendor` would write. Mind the
  60-line limit on the hook (it is on the ratchet allowlist; this would be growth, so it may
  belong in the Go side of `lib/go-tool.bash`'s tools rather than the hook).
- **N3. Pin the rest of the module-auth environment** in `goEnv` (`src-go/qa/godeps/run.go:49`):
  `GONOSUMDB=`, `GONOSUMCHECK=`, `GOPRIVATE=`, `GOINSECURE=`, `GOSUMDB=sum.golang.org`,
  `GOPROXY=https://proxy.golang.org,direct`. Not exploitable today (see attack 2) but the spec
  G10 promises "a flag from the caller's environment cannot change the verdict", and these can
  change its wording.
- **N4. Cold cache offline is exit 1, not 2.** With an empty `GOMODCACHE` and no network, `go mod tidy -diff` fails with `module lookup disabled` and the gate reports it as "go.mod or go.sum is
  not tidy" (finding, exit 1) rather than "could not judge" (exit 2). Fail-closed either way; the
  classification and the fix hint are misleading.
- **N5. go-vuln has no DB cache**, so every `qa-all` run needs `vuln.go.dev`. Fine in CCY and CI;
  worth a line in QA.md so an offline red is read correctly (the error text already says it).
- **N6. Pre-existing gitleaks finding on F44**, `CLAUDE/Plan/00203-teams-hot-control-and-robustness/test_hs_journal.py:348`
  (generic-api-key, 8.30.1 = CI's pin). Not this branch's; CI's gitleaks job passed on
  0c61144d, so it may post-date that run. Whoever next touches F44 should check it.
- **N7. qa-patterns semgrep timeouts** on files this branch does not touch, reproduced on F44 at
  load 36: an environment matter, not a regression.

## Merge conflicts against F44

`git merge-tree --write-tree --name-only F44 92e085d8` wrote tree `d4ed680d` with no conflict
entries: a clean merge.
