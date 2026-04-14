# Plan 032: Compression Helpers CLI

**Status**: 🔄 In Progress (awaiting host deployment)
**Created**: 2026-04-14
**Owner**: joseph
**Priority**: Medium
**Type**: Feature Implementation

## Overview

Provide two simple CLI commands, `compress` and `uncompress`, that give an
opinionated, fast, safe wrapper around the best-in-class unified archive tool
for Fedora. `compress` defaults to `.tar.xz` (best general-purpose ratio on
Linux) and switches to `.zip` via a flag for cross-platform interop.
`uncompress` auto-detects format by extension and **always extracts into a
dedicated directory** — never dumping files into the current working
directory (tarbomb protection, regardless of how the archive was built).

Research confirms we should not build this from scratch. The Rust tool
[`ouch`](https://github.com/ouch-org/ouch) already handles every backend we
care about (xz, zip, gz, bz2, zst, 7z, tar chains, etc.), is actively
maintained, is distributed as a single static musl binary, and selects
backends by file extension. The only things missing for this user's workflow
are (a) a shorter `compress`/`uncompress` UX with xz-by-default, and
(b) enforced always-extract-into-a-folder behaviour. Both are tiny bash
wrappers around `ouch`.

## Goals

- `compress PATH` → produces `PATH.tar.xz` (file or folder, same command)
- `compress --zip|--gz|--bz2|--zst|--7z PATH` → switches backend via a simple
  per-algorithm flag. **Exactly one** algorithm flag allowed; passing more
  than one is a hard error (fail fast)
- `uncompress ARCHIVE` → auto-detects format from extension, extracts into a
  new folder named after the archive, never floods CWD
- Zero manual steps: delivered and version-pinned via Ansible playbook
- Binary + wrappers installed system-wide; available to all users

## Non-Goals

- Not writing a compression library — delegating to `ouch`
- Not supporting every obscure format — xz and zip are the required pair; any
  other format `ouch` supports comes along for free on the decompress side
- Not building a GUI
- Not writing our own format auto-detection — `ouch` already does this well
- Not a package of other/broader archive utilities (no file manager integration)

## Context & Background

**Research findings** (full detail in `research.md`):

| Candidate      | Verdict                                                                                                                         |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `ouch` (Rust)  | **Chosen.** Actively maintained (release April 2025), handles xz + zip + many more, extension-driven, single static musl binary |
| `atool` (Perl) | Fedora-packaged but dormant upstream since 2016                                                                                 |
| `7z`/`p7zip`   | In Fedora repos but 7z-centric UX, not a clean abstraction                                                                      |
| `dtrx`         | Extract-only, does not compress                                                                                                 |

`ouch` is not in Fedora repos or any COPR, so deployment is via GitHub
release static binary (idempotent `get_url` + `creates:` in Ansible).

**Why the wrappers are still needed on top of `ouch`:**

1. `ouch compress folder/ out.tar.xz` works — but the user wants `compress folder/`
   and have `.tar.xz` chosen automatically. No flag guessing each invocation.
2. `ouch decompress` by default extracts siblings into CWD for `.zip` and other
   non-tar formats — this is the tarbomb footgun the user wants to prevent.
   `ouch`'s `--dir` flag solves it; the wrapper enforces it always.

## Tasks

### Phase 1: Binary Deployment

- [x] ✅ Create `playbooks/imports/optional/common/play-compression-helpers.yml`
  - [x] ✅ Task: `unarchive` the `ouch` musl static tarball from GitHub release into
    `/opt/ouch-<version>/` with `creates:` for idempotency, then symlink to
    `/usr/local/bin/ouch`
  - [x] ✅ File modes `0755`, owner/group `root`
  - [x] ✅ Version pinned via `ouchVersion: "0.6.1"` variable
  - [x] ✅ Verify step runs `ouch --version` + asserts the output matches the pin
  - [x] ✅ Preflight asserts `ncompress` package not installed (prevents
    `/usr/bin/{compress,uncompress}` PATH conflict)
  - [x] ✅ Playbook is executable (`chmod +x`) and has the `#!/usr/bin/env ansible-playbook` shebang

### Phase 2: Wrapper Scripts

- [x] ✅ Write `files/usr/local/bin/compress` (bash) — all subtasks done,
  shellcheck clean, e2e tested with ouch 0.6.1

- [x] ✅ Write `files/usr/local/bin/uncompress` (bash) — all subtasks done,
  shellcheck clean, e2e tested. Refuses preexisting target, `--force` replaces,
  nested `src/src/` left as-is per Decision 5

### Phase 3: Deployment & QA

- [x] ✅ Add wrapper install tasks to `play-compression-helpers.yml`
  (copy both scripts to `/usr/local/bin/`, mode `0755`, owner/group `root`)
- [x] ✅ Run `./scripts/qa-all.bash` — passed (238 files checked, 0 failures)
- [ ] ⬜ Commit with plan reference (awaiting user go-ahead)

### Phase 4: User Testing (on HOST, not in container)

- [ ] ⬜ Deploy: `ansible-playbook playbooks/imports/optional/common/play-compression-helpers.yml`
- [ ] ⬜ Test compress folder → xz
- [ ] ⬜ Test compress folder → zip
- [ ] ⬜ Test compress single file
- [ ] ⬜ Test uncompress .tar.xz (must land in its own folder)
- [ ] ⬜ Test uncompress .zip (must land in its own folder — the tarbomb-protection case)
- [ ] ⬜ Test overwrite refusal (existing output + existing target folder)

## Technical Decisions

### Decision 1: Wrap `ouch` vs write from scratch

**Context**: Could write pure-bash wrappers around `tar`/`xz`/`zip` directly.
**Options**:

1. Wrap `ouch` — one dep, auto extension detection, handles edge cases
2. Pure bash around core utils — no new deps, but we reinvent format detection
   and lose zstd/7z/etc. for free

**Decision**: Wrap `ouch`. The core value the user wants (auto-detect on
uncompress, consistent interface) *is* what `ouch` already does. Writing it
ourselves would be reinvention. `ouch` is a 3MB static binary — trivial cost.
**Date**: 2026-04-14

### Decision 2: Language — bash vs python

**Context**: Project uses both; bash for thin wrappers, python for logic.
**Decision**: **Bash**. Both wrappers are \<30 lines of arg parsing + one
`ouch` invocation. Python would be overkill and add a runtime dependency
where none is needed.
**Date**: 2026-04-14

### Decision 3: Binary source — COPR vs GitHub release vs cargo

**Context**: Not in Fedora repos; not in COPR per repology.
**Decision**: **GitHub release static musl binary via `get_url`**. Zero
runtime deps, reproducible (pinned tag), idempotent via `creates:`. Avoids
pulling in the Rust toolchain just to install one tool.
**Date**: 2026-04-14

### Decision 4: Default format — `.tar.xz` vs `.tar.zst`

**Context**: zstd compresses faster and decompresses faster; xz compresses
tighter. User explicitly said xz.
**Decision**: **`.tar.xz`** per user request. `--zst` flag could be added
later as an extension; not in scope.
**Date**: 2026-04-14

### Decision 5: Flatten single-root-folder on uncompress?

**Context**: If `myproj.tar.xz` contains `myproj/...`, our wrapper creates a
`myproj/` directory and `ouch` extracts into it, producing `myproj/myproj/...`.
**Decision**: **Option 1 — accept the nesting.** Predictable, safe, no magic,
no surprises. User confirmed "1 for safety".
**Date**: 2026-04-14

### Decision 6: Multi-algo flag handling on `compress`

**Context**: Supporting multiple algo flags (`--xz`, `--zip`, `--gz`, `--bz2`,
`--zst`, `--7z`) raises the question of what to do if more than one is passed.
**Decision**: **Fail fast.** Passing more than one algo flag exits with code
2 and prints the conflicting flags. No "last flag wins", no silent preference.
Per the project's #1 hard rule.
**Date**: 2026-04-14

## Success Criteria

- [ ] `compress myfolder` produces `myfolder.tar.xz` in CWD
- [ ] `compress --zip myfolder` produces `myfolder.zip` in CWD
- [ ] `compress --gz myfolder` produces `myfolder.tar.gz`
- [ ] `compress --7z myfolder` produces `myfolder.7z`
- [ ] `compress --xz --zip myfolder` FAILS with exit 2 and lists both flags
- [ ] `compress myfile.txt` produces `myfile.txt.tar.xz`
- [ ] `uncompress anything.{tar.xz,zip,tar.gz,7z,tar.zst}` creates a single
  new folder and extracts into it — CWD gets exactly one new entry
- [ ] Running either command twice against same target refuses to overwrite
  unless `--force`
- [ ] Playbook is idempotent (second run is a no-op)
- [ ] `./scripts/qa-all.bash` passes

## Risks & Mitigations

| Risk                                                                                                | Impact | Probability | Mitigation                                                                                                                       |
| --------------------------------------------------------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `ouch` release format changes (binary naming, tarball structure)                                    | Medium | Low         | Pin version via variable; update deliberately                                                                                    |
| `ouch` goes unmaintained in future                                                                  | Medium | Low         | Bash wrappers are small; could swap backend to `atool` or native tools if needed                                                 |
| Users already have conflicting `compress` binary (old SVR4 `compress(1)` is in `ncompress` package) | High   | Low-Med     | Playbook check: assert `ncompress` not installed or document precedence; `/usr/local/bin` typically wins over `/usr/bin` in PATH |
| `uncompress` name collision with legacy `uncompress(1)` (also from `ncompress`)                     | High   | Low-Med     | Same as above — `/usr/local/bin` precedence; document in play                                                                    |

## Timeline

- Phase 1: Binary deployment (playbook)
- Phase 2: Wrapper scripts
- Phase 3: QA + commit
- Phase 4: Host deployment + manual verification

## Notes & Updates

### 2026-04-14

- Plan created. Research (see `research.md`) identified `ouch` as the
  backend. User added uncompress requirement after initial request —
  incorporated as Phase 2 task and tarbomb-protection success criterion.
- Decision gate 5 resolved: **option 1** (accept nesting, no magic flatten).
  User rationale: "1 for safety".
- Decision 6 added: `compress` fails fast on >1 algo flag.
- Phases 1–3 implemented in session. Wrappers shellcheck-clean, e2e tested
  against ouch 0.6.1 in a tmp sandbox (not deployed to container system):
  - `compress src` → `src.tar.xz` ✓
  - `compress --zip src` → `src.zip` ✓
  - `compress --7z src` → `src.7z` ✓
  - `compress --xz --zip src` → exit 2, lists both flags ✓
  - `uncompress src.zip` → creates `./src/` containing archive contents ✓
  - `uncompress` refuses preexisting target folder, `--force` replaces ✓
- `./scripts/qa-all.bash` passed (238 files).
- **Next**: user reviews + commits, then deploys on HOST (not in CCY
  container) with `ansible-playbook playbooks/imports/optional/common/play-compression-helpers.yml`,
  then Phase 4 host testing.
