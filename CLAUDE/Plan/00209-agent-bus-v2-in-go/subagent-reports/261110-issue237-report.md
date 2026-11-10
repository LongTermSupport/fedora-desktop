# Issue 237: the container version gate did not guard the builder stage's Go

Addresses #237. Not plan work of its own; filed here because Plan 00209 owns the ccy image.

Commits on the work branch:

- `e38ef1271`: the defence, red
- `be8141162`: the fix, green. This is the last work commit.

Nothing was pushed.

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
  because it would demand a bump for host-only tools and for tests. The brief also forbids a
  bump for tests and qa. See the decision referred to the owner below about the teamchat digest.

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
- **Count.** 65 real probes (one per package plus the asm, embed and module files) stood for
  every file of those packages. All were unguarded.
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
   - Open: 1. Fixed: 0.
2. **`CCY_SOURCE_COMMIT` is not a file.** The searcher was unsure about this one. The image is
   built from a commit, but a rebuild is triggered only by the label or the Dockerfile hash.
   That is what this gate exists for, and no hook can guard a commit id. Recorded, not acted on.

## Permanence

- **Blocking.** The suite is blocking: `scripts/qa-gates.tsv` gate `ccy-container-version-hook`,
  run by qa-changed, qa-all and CI.
- **Green run.** Through the project's entry point:
  `./scripts/qa-changed.bash --files <the 9 changed files>`. It printed
  `✓ suite test-ccy-container-version-hook.bash: passed: 206`,
  `✓ suite test-qa-changed.bash: passed: 191`, bash-limits, shellcheck and semgrep green, and
  `qa-changed passed`.
- **Not run.** qa-all was not run, by instruction.

## Toolchain and detector gaps

- **No stable rule identifier.** The suite has no per-rule identifier or resolver. The gate name
  `ccy-container-version-hook` and the `CLAUDE/QA.md` row are the closest equivalent.
- **Staged versus working tree.** `go list` reads the working tree, not the index. A commit that
  stages only part of an import change is judged on the working tree's import graph. The CCY
  front gate had the same limit before this change. It is not fixed here.
- **Dogfooding.** Running the suite and qa-changed found no fault in the tooling itself. Two
  rough edges were mine, and both are fixed:
  - the stand-in path for qa-go-deps in the test;
  - qa-changed's basename routing, which the existing convention covers.

## The original defect

Fixed in `be8141162`. The proof is the 65 real-tree probes and the 7 synthetic cases that were
red in `e38ef1271`; they are now green, with all 206 cases of the suite passing.
