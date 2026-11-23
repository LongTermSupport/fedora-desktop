# Proposal: split the tool projects out of fedora-desktop

The owner's words: "the fedora-desktop repo has become a monorepo for all kinds of projects now
and it is probably time to split these out into their own repos with their own focussed CI.
fedora desktop can then just orchestrate pins, and a change to ccy (for example) does not
require CI run of all the other fedora-desktop stuff."

This is a proposal. Nothing is built. Each claim is marked **FACT** (read from the repository or
from GitHub's own run records), **INFERENCE** (reasoned from facts) or **UNKNOWN**.

## 1. Where CI time goes now

### How CI is wired

- **FACT** One workflow, `.github/workflows/qa.yml`, decides a scope in a `base` job, then runs
  everything in **one** job, `qa`, on one `ubuntu-latest` runner (`qa.yml:72`, `qa.yml:110`).
- **FACT** A `ci/<topic>` push runs `qa-changed.bash` in diff mode against the protected line's
  tip (`qa.yml:320-327`). A push to `F44` runs the full `qa-all.bash` (`qa.yml:313-319`), after
  the move, fixed forward if red. A docs-only push runs nothing (`qa.yml:36-47`).
- **FACT** `qa-all.bash` runs every row of `scripts/qa-gates.tsv` in order, serially (the file
  header). There are 129 gate rows; 41 are `ccy-*` suites.
- **FACT** Both QA steps carry a 120-minute limit because the Go gate alone hit the old
  30-minute limit once teamchat landed (`qa.yml:308-318`).

### Measured, from GitHub's run records (the last 40 QA runs, mid-November 2026)

| Run kind                                   | Wall time of the run      |
| ------------------------------------------ | ------------------------- |
| Full `qa-all` on `F44`                     | 61 to 82 min, usually ~70 |
| Diff gate, `ci/` branch, touching `src-go` | 35 to 48 min              |
| Diff gate, `ci/` branch, no `src-go`       | 2 to 5 min                |

One full `F44` run (2026-11-22), split by gate (**FACT**, from the stage-line timestamps):

| Gate                          | Share  |
| ----------------------------- | ------ |
| `go` (go-qa over src-go)      | 38.7 m |
| `helper-tests` (Python)       | 12.6 m |
| `ccy-teams-command`           | 3.2 m  |
| `bash` (shellcheck stage)     | 2.9 m  |
| all other ~125 gates together | ~15 m  |
| tool install + checkout       | ~1.3 m |

One diff run on a `ci/` branch the same day: `go` in diff mode took **32 m** of its 42 m.

### Why it is slow

1. **FACT** The Go gate is half the full run and most of a Go diff run. It runs
   `go test -race -shuffle=on` over the whole module plus a second pass over
   `internal/teamchat/...` with `-tags busfailpoints -count=2` (`src-go/qa/go-qa.toml:69-78`),
   plus lint, vet and project rules over four tagged builds (`go-qa.toml`, stage
   `project-rules-tagged`).
2. **FACT** teamchat is half of all Go code: 212,840 of 431,761 lines under `src-go/internal`
   (799 files). `internal/teams` (the `ccy --teams` launcher) is another 56,927.
   **INFERENCE** teamchat's tests are the largest part of the 38 minutes. **UNKNOWN** the exact
   split per package: go-qa does not print package times into the CI log. Task 1.1 measures it.
3. **FACT** CI never restores a Go build or test cache. Every run logs
   `Restore cache failed: Dependencies file is not found ... Supported file pattern: go.mod`,
   because `actions/setup-go` looks for `go.sum` at the repository root, and it is in `src-go/`
   (`qa.yml:83-86`, `qa.yml:155-158`). `go-qa.toml:74-75` says "go's test cache keeps an
   unchanged teamchat a cache hit"; in CI that never happens.
4. **FACT** `helper-tests` ran 5,774 Python tests in 753 s, one process. **FACT** The v1 bus
   (`tests/helpers/pingbus` 22,318 lines, `agent_bus` 8,502, `agent_bus_lxc` 3,338) is about half
   of all helper test code. **INFERENCE** retiring v1 roughly halves this gate.
5. **FACT** Everything is serial in one job: the Go gate, the Python suite and the bash suites
   never overlap.

## 2. The candidate repositories

Import facts come from `go list -mod=vendor -deps` over each command (run in this checkout).

### Shared base (prerequisite for any Go split)

- **FACT** Every candidate imports a small set of shared adapters: `execx`, `term`, `fsx`,
  `gitx`, `shellword`, `stdinread`, `ui`, `sudo`, `systemd`, `duration`. `stdinread` and
  `leakcheck` import `teamchat/clock`, an inverted edge.
- **INFERENCE** Any split first needs these in their own module (say `go-base`), imported by
  version. Otherwise each new repo copies them, and they drift. `teamchat/clock` moves into that
  base.

### A. teamchat (agent bus v2): best candidate, after v2 settles

- **What:** `src-go/internal/teamchat/**`, `cmd/teamchat`, `cmd/teamchat-admin`; the plays
  `playbooks/imports/play-teamchat.yml`, `tasks/teamchat-*.yml`; the gate `teamchat-keeps-off-v1`
  and the teamchat-specific project rules in `src-go/qa/rules`.
- **Imports out of teamchat (FACT):** only `term`, `execx`, `stdinread`, `systemd`, `ui`,
  `duration`. Nothing ccy-specific.
- **Imports into teamchat (FACT):** `cmd/teamchat`, `cmd/teamchat-admin`, and `internal/teams/*`
  (board, lxcsession, seatsweep, shutdown, system, teamchatlaunch, teamtest) use `paths`,
  `proto`, `seat`, `status`, `sweep`, `member/*`, `host/lxc/lxcstate`. So `ccy-teams` depends on
  teamchat as a library.
- **What plays consume (FACT):** binaries built on the controller from `src-go`
  (`tasks/teamchat-build-go-commands.yml`), stamped with the git tree id of `src-go` as
  `SourceDigest` (`play-teamchat.yml:333-359`); the ccy image builds `teamchat` in its builder
  stage (`files/var/local/claude-yolo/Dockerfile:50`, `:88`).
- **Separable:** high for the code, medium for the wiring. Needs a public API for the packages
  `internal/teams` uses (Go `internal/` cannot be imported from another module), a new
  `SourceDigest` meaning (a release tag), and the image fetching a released binary.
- **CI win (INFERENCE):** the biggest of any split. teamchat's race and failpoint runs leave
  fedora-desktop's gate.

### B. ccy (claude-yolo): largest coupling, do last

- **What:** `files/var/local/claude-yolo/` (bash launcher, 3,910 lines; Dockerfile;
  entrypoint; plugins), Go `cmd/claude-yolo`, `ccy-sessions`, `ccy-start-claude`, `ccy-teams`,
  `ccy-token-usable`, `ccy-network-policy`, `cc-claude`, `cc-project-dir`, `ccc`, and packages
  `ccylaunch`, `ccystart`, `sessions`, `sessclose`, `tokens`, `netpolicy`, `allowedhosts`,
  `tmuxccy`, `engine`, `teams/**`; the 41 `ccy-*` gates of `scripts/qa-gates.tsv`.
- **Imports (FACT):** `claude-yolo` pulls `agentbus`, `bus/*`, `pingbus/pyjson`, `declare`,
  `teams/*`, `teamchat/clock`. `ccy-teams` pulls nearly all of teamchat.
- **What plays consume (FACT):** `play-claude-yolo.yml` builds the commands from `src-go`
  through `tasks/build-go-command.yml` (lines 225, 350, 365, 607-656); the image is built from a
  clone of this repository at `CCY_SOURCE_COMMIT`, scoped by `image-source-paths`.
- **Separable:** low today. v1 (`agentbus`, `bus`, `pingbus`) is wired into the launcher, and
  `teams` binds it to teamchat. **INFERENCE** after v1 is removed (Plan 00209 Task 6.5) and
  teamchat is its own module, ccy becomes a normal consumer of teamchat and the base.
- **Note:** the CCY version-bump rule, the merge-commit gate and the pre-commit hooks move with it.

### C. pingbus and agent-bus v1: do not split

- **What (FACT):** Python `helpers/pingbus`, `helpers/agent_bus`, `helpers/agent_bus_lxc`; Go
  `internal/agentbus`, `bus/*`, `pingbus/*`, `cmd/pingbus-status-line`; `play-agent-bus.yml`.
  `helpers/pingbus/cli.py:1-5` is the v1 protocol client; Plan 00209 line 12 calls this v1.
- **Plan (FACT):** Plan 00209 Task 6.5 removes v1 "and the v1 code out of the repository".
- **Recommendation:** delete, not move. Splitting code that is due for removal costs a repo,
  a CI and a pin for nothing.

### D. The QA tooling (`src-go/qa`): keep, already half split

- **FACT** `src-go/qa` is its own Go module (`src-go/qa/go.mod:1`) and imports nothing from
  `src-go/internal`. Its engine, go-qa, is already an external repository
  (`github.com/LongTermSupport/go-qa-ci`, `src-go/qa/go.mod:6`).
- **FACT** Most of its 78 rule files and its gates judge this repository's own conventions
  (plays, bash limits, plan folders, ccy merge commits).
- **Recommendation:** keep it here. Move rule files that only judge teamchat (or ccy) with that
  code. Move any rule that is generic up to go-qa-ci when someone needs it elsewhere.

### E. Others found

- **md-viewer (FACT): already split.** `play-md-viewer.yml:24` pins a commit;
  `tasks/build-go-command.yml:48-66` builds it with `go install <module>@<version>`;
  `vars/version-pins.yml:77-86` tracks it. This is the working precedent.
- **GNOME extensions (`extensions/`, 5 extensions):** separable, but cheap in CI (the `js`
  stage) and bound to this repo's plays and the panel contract gate. Keep.
- **Python helpers:** "Python that Ansible loads stays Python" (CLAUDE.md). They are the plays'
  own code. Keep.
- **hooks-daemon project handlers (`.claude/hooks/handlers/`):** policy for this repository
  only; their gate is already exempt in CI. Keep.
- **Small Go tools** (`wsi`, `stt`, `watermark`, `open`, `convert`, `reclaim`, `nord`, freeze
  tools, rclone tools, `vmtest`, `watchlist`, `release-plan`): each needs only the base. Each is
  small. **INFERENCE** their tests are a small slice of the Go gate. Splitting them gains little
  CI time and adds many repos. Keep, unless the owner wants them published on their own.

## 3. How fedora-desktop would pin and orchestrate

Three ways:

1. **Go module versions, built on the host** (`go install module/cmd/x@vX.Y.Z`).
   **FACT** `tasks/build-go-command.yml` already supports this (`go_command_module`), and
   md-viewer uses it. Integrity from `go.sum` and sum.golang.org. Needs Go on the host (it
   already has it, `play-golang.yml`). Breaks the "every byte compiled is in the checkout"
   offline rule of Plan 00204 D2 for those commands (md-viewer already does this).
2. **Release binaries with checksums**, downloaded by plays. The repo's usual pattern for
   upstream tools (`vars/fzf-pin.yml`; `.qa-versions`; pinned sha256 per arch). Needs a release
   workflow per new repo. Best for the ccy image and remote LXC/teamchat hosts, which have no
   checkout and no Go.
3. **git submodules.** Keeps one tree, but every tool ends up inside fedora-desktop's CI again,
   and submodules confuse worktrees and agents. Not recommended.

**Recommendation:** tagged releases (semver tags) in each new repo, consumed two ways:

- the host builds commands with `go_command_module: <module>/cmd/<x>@vX.Y.Z` (option 1, as
  md-viewer); and
- the ccy image and remote teamchat hosts download the release binary checked against a pinned
  sha256 (option 2).
- Each pin is a play var listed in `vars/version-pins.yml`, so `scripts/check-pinned-versions.bash`
  and the `update-versions` skill report drift with no new tooling.
- This mirrors the hooks-daemon model (`.claude/hooks-daemon.yaml:21`, `expected_version`): the
  consumer names one exact version; the tool repo owns its own release and CI.

## 4. CI for each new repo

- One workflow per repo: go-qa (format, lint, vet, project rules, race tests, coverage) and
  govulncheck, with the Go cache keyed on that repo's `go.sum`.
- teamchat keeps its `busfailpoints` stage and tagged-rule runs there.
- A release workflow: tag, build static binaries per arch, publish checksums.
- fedora-desktop CI then checks only: the pin manifest is valid, plays parse, and (optional) a
  cheap smoke step that `go install`s the pinned versions.

## 5. Migration order (least coupled first)

0. **Interim CI fixes (section 7).** No split needed. Measure again.
1. **Go base module** (`execx`, `term`, `fsx`, `gitx`, `shellword`, `stdinread`, `ui`, `sudo`,
   `systemd`, `duration`, `clock`). Smallest, imported by everything; prerequisite.
2. **teamchat**, with the public API `internal/teams` needs. Biggest CI win.
3. **ccy**, after v1 is gone; it then imports base and teamchat by version.
4. Small tools only if the owner wants them public on their own.

## 6. History

- **FACT** `git filter-repo` and `git filter-branch` are denied to agents (rule
  R-GIT-FILTER-HISTORY); only a human may run them, on a fresh clone with a backup ref.
- **Option H1 (recommended): no history rewrite.** Create the new repo fresh. Its first commit
  copies the tree and names the fedora-desktop commit it came from ("imported from
  fedora-desktop at <sha>; history there"). fedora-desktop removes the code in one commit that
  names the new repo. History stays in fedora-desktop, readable with `git log -- <path>`.
- **Option H2:** a human runs `git filter-repo --path ...` on a fresh clone to carry history.
  Costlier, owner-only. Also risks carrying old install-specific text into a new public repo
  without the scrubbing this repo has done since.

## 7. Cheaper interim wins (no split)

Each is a change to `qa.yml` or the gate wiring only. **INFERENCE** on size of each win;
Task 1.1 measures.

1. **Turn the Go cache on.** Set `cache-dependency-path: src-go/go.sum` (and
   `src-go/qa/go.sum`) on both `setup-go` steps. Unchanged packages then hit the test cache,
   which `go-qa.toml:74-75` already counts on. Likely the largest single win, for one line.
   Caveat: go-qa's built-in runner may force `-count=1` (**UNKNOWN**; check go-qa-ci).
2. **Split the one `qa` job into parallel jobs**: Go gate; helper tests; everything else.
   Wall time becomes the slowest job (about the Go gate) instead of the sum. Needs the gate
   table to support a subset (a `--only`/`--except` on qa-all, or a column in `qa-gates.tsv`),
   with a final job that requires all three.
3. **Shard the Go gate**: teamchat in its own job, the rest in another.
4. **Run helper tests in parallel** (several unittest processes by package).
5. **Retire v1 (Plan 00209 Task 6.5).** Deletes about half the helper tests and the v1 Go.
6. **Path-filter the diff gate further**: qa-changed already routes by file
   (`scripts/qa-routing.tsv`). Check why a small Go change still costs 32 minutes: go-qa's diff
   mode may still run all race tests of dependents (**UNKNOWN**).

**INFERENCE** 1 + 2 together could bring the full run near the Go gate's own time, and the
cache could cut that for changes outside teamchat. This may remove most of the pain without a
split. The split then becomes a question of ownership and publishing, not CI time.

## 8. Interaction with the live v2 work

- **FACT** Plan 00209 is In Progress. Task 5.1 (host `cc` members on v2) is open
  (`00209/PLAN.md:447`). Phase 6 (migration, Task 6.4 cut-over, Task 6.5 v1 removal) is open
  (`00209/PLAN.md:452-490`). Plan 00257 (cross-host LXC members) is active in `internal/teamchat`
  and `internal/teams`.
- **Recommendation:** no code moves out of `src-go` until v2 cut-over and v1 removal
  (Task 6.5) are done. Moving teamchat now would turn every v2 change into a two-repo change and
  a pin bump, during its busiest phase.
- **What can go first, touching no v2 code:**
  - all of section 7 (CI wiring only);
  - Task 1.1 measurement;
  - Plan 00209 Task 6.5 itself, which shrinks the problem;
  - the base-module design (a document; no code moves).

## 9. Owner decisions

| #   | Question                                   | Recommended answer                                                      |
| --- | ------------------------------------------ | ----------------------------------------------------------------------- |
| D1  | Try the interim CI wins first and measure? | Yes. Decide the split on the new numbers.                               |
| D2  | Which repos, if any?                       | Base, teamchat, then ccy. Not v1, not QA, not helpers or extensions.    |
| D3  | When?                                      | After v2 cut-over and v1 removal (Plan 00209 Task 6.5).                 |
| D4  | Pin model?                                 | Semver tags; `go install` on hosts, checksummed binaries for the image. |
| D5  | History?                                   | H1, fresh repo with a pointer commit. No filter-repo.                   |
| D6  | Same GitHub owner and visibility?          | Same owner as fedora-desktop and md-viewer, public.                     |
| D7  | Keep Plan 00204 D2 offline-vendor rule?    | Waive it for pinned own modules, as md-viewer already does.             |
