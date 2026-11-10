# Issue 237: the container version gate did not guard the builder stage's Go

Addresses #237. Not plan work of its own; filed here because Plan 00209 owns the ccy image.

Commits on the work branch:

- `e38ef1271`: the defence, red
- `be8141162`: the fix, green
- the review follow-up, which carries this report's final text: unreadable builder forms
  rejected, and the `CCY-IMAGE-INPUT` tag (see "Conformance review" below)

The stop guard put a park commit, `8729e8dca`, between the fix and the follow-up. Its own push
was refused by the QA push gate. Nothing was pushed.

# Defence Before Fix report

## Specification followed

- SPEC.md 1.1.0, DETECTOR-SPEC.md 1.0.0, TOOLING-SPEC.md 0.2.0 and project-prompt.md. The
  refresh script fetched all four.

## Toolchain

- The detector is the project's own blocking bash suite,
  `scripts/test-ccy-container-version-hook.bash`. It is registered in `scripts/qa-gates.tsv`
  as `ccy-container-version-hook`, and `qa-changed.bash` and `qa-all.bash` run it. It runs the
  real pre-commit hook against throwaway repositories.
- CLAUDE.md lists semgrep and Go analyzers as the Defence Before Fix tools. Neither fits this
  class. The defect is a gap between what the image is built from and what the hook guards,
  and only running the hook against changes can show it.
- The project has no manifest declaration and no known-gap record. No listing command exists
  for this suite. The calibration assumed: every non-test file the builder stage's Go build
  reads is an input.

## Defect and class

- **Instance.** `files/var/local/claude-yolo/Dockerfile:42-72`: the `ccy-builder` stage builds
  `./cmd/ccy-start-claude`, `./cmd/pingbus-status-line` and `./cmd/teamchat` from src-go.
  `scripts/git-hooks/pre-commit` guarded only the COPY sources and the fixed list
  `{Dockerfile, helpers/pingbus, helpers/agent_bus, vars/fzf-pin.yml}`.
- **Class.** An input the image is built from that the version check does not derive.
- **Hazard.** The change is committed with no container bump. Running containers keep the old
  binary, and nothing reports it.

## Rule

- **Where.** Two new sections of the suite.

- **Section 1: a synthetic module.** It has a builder stage with one `go build` continued over
  lines, a vendored module, an embedded asset, a test file, a nested `qa` module and an
  unrelated package.

  - Every file the stage's commands build from must be rejected without a container bump:
    command main, library, embedded file, vendored file, `vendor/modules.txt` and the module
    file.
  - The same change passes when the container version is bumped.
  - The test file, the qa module and the unrelated package must not be judged.

- **Section 2: the real tree.** The suite reads the real Dockerfile's builder-stage `go build`
  commands itself, in Python and independently of the hook. It asserts there are 3, asks
  `go list -deps` (vendor mode) for their packages and probes the real hook with:

  - one source file per package, vendored packages included;
  - every assembly file and every embedded file;
  - the module and sum files.

  It expects every probe rejected, and a test file of a closure package not judged.

- **Isolating the container gate.** Each case bumps CCY_VERSION so that only the container gate
  decides. The message checked is `container version bump required`.

- **Excluded as hazard-free.**

  - `*_test.go`: not compiled into the binaries.
  - `src-go/qa/`: a separate module, built into no image binary.
  - Packages no builder command imports.

- **Wider rule considered: "all of src-go".** This is the issue's fallback. It was not built
  because it would demand a bump for host-only tools and for tests. The coordinator's brief
  also said tests and qa must not demand a bump.

  - The brief is not the owner's decision. The teamchat digest (decision 1 below) is the one
    known way a test-only or qa-only change still reaches the image.
  - The blocking rule is the build-closure rule, which is narrower. Whether it stays that
    narrow is referred.

- **Unreadable builder forms are rejected.** These are `go install`, `go run`, `go generate`, a
  `go build` with no `./` package, and Go inside another command such as `sh -c "go build ..."`.
  They are rejected with `COMMIT REJECTED [CCY-IMAGE-INPUT]: the ccy-builder stage ... runs Go in a form this hook cannot read`, so no build form is left unguarded without a message.
  Commands split on `;`, `&&` and `||`.

## Proof

- **Red.** The suite reported `passed: 134  failed: 72`. All 72 failures were the
  `'0 no' (wanted '1 yes')` kind: a builder input passed with no bump.
  - 7 of them were synthetic.
  - 65 were in the real tree: every first-party package of the three commands; vendored
    `golang.org/x/sys/unix` (including `asm_linux_amd64.s`), goldmark and x/text;
    `internal/teamchat/member/kit/skill.txt`; go.mod and go.sum.
- **Commit.** The red proof is commit `e38ef1271`, which changes only the test.

## Independent search

- **Searcher.** `defence-before-fix:independent-searcher` was dispatched before the rule was
  written and kept out of the test and `scripts/git-hooks/lib/`. Its notes are in the untracked
  `untracked/scratch/issue237-search.md`.
- **What it confirmed.** It confirmed the src-go gap, including go.mod (the toolchain line is
  read at build), go.sum, vendor/ and embedded files. It also confirmed that the reachable set
  needs the import graph, not a path prefix.
- **What it found as guarded.** Every COPY source, the kit files, the pingbus zipapp sources and
  the files the play stages.
- **Other gates.** Other Dockerfiles have no version gate, and no other hand-listed bump gate
  exists.
- **Beyond the rule.** It found two things the rule does not cover. Both are listed under the
  decisions referred to the owner below.
- **Text search versus reading.** Text search found the `-X main.SourceDigest` ldflag. Reading
  found that the digest is the tree id of all of src-go.

## Sweep and count

- **Scope.** The whole real tree, through the real-tree section. This counts every file the
  Dockerfile's builder stage builds from:

  - 61 packages of the three commands;
  - their assembly and embedded files;
  - the module files.

- **Count.** The closure is:

  - 61 packages;
  - 296 compiled Go files, 1 assembly file and 1 embedded file;
  - go.mod and go.sum.

  That is 300 inputs. All 300 were unguarded before the fix: the old hook guarded no path under
  src-go, so the probes, one source file per package, were a sample of an exhaustive fact.
  The 65 real probes ran against the hook, red before the fix and green after it.
  `go-staged.bash` judges a file by its package directory, so one file per package exercises
  that package's whole entry.

- **The other Go gate.** The CCY_VERSION gate over the launcher's Go front already derived its
  inputs with `go list`. In the same class it lacked only `vendor/modules.txt`, which is now
  guarded through the shared library.

## Fixes

- **New `scripts/git-hooks/lib/go-staged.bash PACKAGE... -- FILE...`.** It prints the staged
  files a build of those src-go packages reads:

  - `go list -deps` in vendor mode, judged by import path (safe under a symlinked checkout);
  - embedded files;
  - `go.mod`, `go.sum` and `vendor/modules.txt`.

  It skips tests and `src-go/qa/`. It runs Go only when a candidate is staged, and a Go that
  cannot answer rejects the commit.

- **New `scripts/git-hooks/lib/ccy-image-staged.bash CONTEXT FILE...`.** It holds the
  container gate's whole input set:

  - the COPY-source logic, moved out of the hook unchanged;
  - the builder stage's `go build` packages, read from the staged Dockerfile with its
    continuation lines joined and handed to go-staged.

- **`lib/ccy-staged.bash`.** It now calls go-staged instead of its own copy of the closure code.

- **The pre-commit hook.** It calls ccy-image-staged and shrinks from 273 to 245 code lines.
  Its ratchet record was lowered with `qa-bash-limits.bash --update`. The new libraries are
  within the bash limits.

- **Docs.**

  - `CLAUDE/ContainerRules.md`, `.claude/rules/ccy-version-bump.md` (the drift the searcher
    found) and the `CLAUDE/QA.md` row were updated.
  - The test names the module and sum files by assembled names, as it already did, so
    qa-changed does not pick it for a qa-module change. test-qa-changed.bash had caught this.

- **Nothing suppressed or narrowed.**

## Decisions referred to the owner

1. **teamchat's source digest covers all of src-go.** The builder passes
   `-ldflags "-X main.SourceDigest=$(git rev-parse HEAD:src-go)"`. So every src-go change alters
   the teamchat binary's digest, tests and qa included.
   - The brief says tests and qa must not demand a bump. I followed it, so a test-only or
     qa-only commit leaves running containers with a teamchat whose digest no longer matches
     the current tree.
   - The kit check I found (`member/link/server.go:224`) compares a request's kit with the
     link's own digest. Both come from the same in-image binary, so I found no cross-build
     comparison that this breaks. I did not read every digest consumer, such as host LXC kits.
   - If the digest must match a host build of the same commit, the owner should choose between
     guarding all of src-go and changing the digest to cover only the build closure.
   - Open: 1, the digest. Fixed: all 300 closure inputs.
   - What stopped me: the brief, and the owner's authority under SPEC section 4.
   - What to try next, either of:
     - (a) widen `go-staged.bash` to every src-go file outside `src-go/qa/` for teamchat. A
       one-line case change, but it makes every test edit bump the container.
     - (b) compute the digest in the builder from the build closure, for example the hash of
       `go list -deps -f` files, instead of `rev-parse HEAD:src-go`. This keeps the rule as it
       is.
2. **`CCY_SOURCE_COMMIT` is not a file.** The searcher was unsure about this one.
   - The image is built from a commit, but a rebuild is triggered only by the label or the
     Dockerfile hash. That is what this gate exists for.
   - Count: 1. A hook cannot guard a commit id.
   - What to try next: have the launcher compare the image's recorded source commit with the
     checkout's last commit touching the closure, and warn or rebuild. That belongs to Plan
     00209's launcher work, not this gate.
3. **Staged versus working tree.** `go list` reads the working tree, not the index, so a
   partially staged import change is judged on the working tree's import graph.
   - Count: unknown, and no instance was seen. The launcher's front gate had the same limit
     before this change.
   - What to try next: run `go list` on a `git checkout-index` copy of staged src-go, as
     `staged-scan-list.bash` already does for qa-go-deps. It costs one tree copy per commit
     that stages src-go.

## Permanence

- **Blocking.** The suite is blocking: `scripts/qa-gates.tsv` gate `ccy-container-version-hook`,
  run by qa-changed, qa-all and CI.
- **Green run.** Through the project's entry point,
  `./scripts/qa-changed.bash --files <the changed files>`. Each run's output is recorded under
  that commit in "Conformance review" below.
- **Not run.** qa-all was not run, by instruction.

## Toolchain and detector gaps

- **Rule identifier.** Rejections now carry `CCY-IMAGE-INPUT`, and the `CLAUDE/QA.md` row of
  the suite names it. The project has no resolver tool for such identifiers. The tag
  is found by searching `CLAUDE/QA.md`.
- **Staged versus working tree.** Referred above, as decision 3.
- **Dogfooding.** Running the suite and qa-changed found no fault in the tooling itself. Two
  rough edges were mine, and both are fixed:
  - the stand-in path for qa-go-deps in the test;
  - qa-changed's basename routing, which the existing convention covers.

## The original defect

- **Fixed in `be8141162`.** The proof is the 65 real-tree probes and the 7 synthetic cases that
  were red in `e38ef1271`. They are now green.

## Conformance review

`defence-before-fix:conformance-reviewer` reviewed `e38ef1271` to `be8141162`.

- **Its limits.** Its sandbox refused to run bash, so it judged from the logs and the diffs.
- **Its findings, and what was done:**
  1. **The narrowing is acted on.** It is referred as decision 1, with options. The narrow rule
     stays blocking, because the wider one is the owner's call.
  2. **The count is a sample.** The full closure count was added: 300 inputs, all unguarded
     before the fix.
  3. **No stable identifier.** `CCY-IMAGE-INPUT` was added to the rejection messages and to
     QA.md.
  4. **Defeatable by Dockerfile phrasing.** Unreadable Go forms are now rejected, and `||` is
     split. Five synthetic cases cover `go install`, `go run`, `sh -c "go build ..."`, a
     package with no `./` and `|| go build`. The four rejection cases are red on `be8141162`
     by construction: the old parser printed nothing for those forms, and the hook passed with
     the versions bumped. They were added with the fix and not committed red on their own.
  5. **Staged versus working tree.** Referred as decision 3.
  6. **`CCY_SOURCE_COMMIT`.** Referred as decision 2, with a count and a next step.

The suite now passes 211 cases.
